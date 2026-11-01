// Package gitx runs the git CLI: cloning, fetching, history, diffs and file contents at a commit.
package gitx

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codist/internal/config"
)

type Commit struct {
	SHA         string   `json:"sha"`
	ShortSHA    string   `json:"shortSha"`
	Message     string   `json:"message"`
	FullMessage string   `json:"fullMessage"`
	Author      string   `json:"author"`
	Time        int64    `json:"time"` // unix millis
	Parents     []string `json:"parents"`
}

// FileChange is one file touched by a commit.
// AddedLines are 1-based new-file lines that were added or modified; DeletedAt are
// new-file positions where lines were removed with nothing added in their place.
type FileChange struct {
	ChangeType string `json:"changeType"` // ADD, MODIFY, DELETE, RENAME, COPY
	OldPath    string `json:"oldPath,omitempty"`
	NewPath    string `json:"newPath,omitempty"`
	Path       string `json:"path"`
	AddedLines []int  `json:"addedLines"`
	DeletedAt  []int  `json:"deletedAt"`
	Patch      string `json:"patch"`
	Binary     bool   `json:"binary,omitempty"`
}

func (f FileChange) IsJava() bool  { return strings.HasSuffix(f.Path, ".java") }
func (f FileChange) Deleted() bool { return f.NewPath == "" }

type Repo struct {
	Root     string
	branch   atomic.Pointer[string] // the branch being followed; see SwitchBranch
	checkout atomic.Pointer[checkoutAt]
	Remote   bool // following origin/<branch>
	auth     string
	mu       sync.Mutex
}

func Open(cfg config.Config) (*Repo, error) {
	r := &Repo{}
	if cfg.GitHubToken != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + cfg.GitHubToken))
		r.auth = "http.extraHeader=Authorization: Basic " + basic
	}
	switch {
	case cfg.RepoPath != "":
		abs, err := filepath.Abs(cfg.RepoPath)
		if err != nil {
			return nil, err
		}
		r.Root = abs
		if _, err := r.git("rev-parse", "--git-dir"); err != nil {
			return nil, fmt.Errorf("%s is not a git repository", abs)
		}
	case cfg.RepoURL != "":
		name := strings.TrimSuffix(filepath.Base(strings.TrimRight(cfg.RepoURL, "/")), ".git")
		r.Root = filepath.Join(cfg.DataDir, "repos", name)
		if _, err := os.Stat(filepath.Join(r.Root, ".git")); err != nil {
			slog.Info("cloning", "url", cfg.RepoURL, "into", r.Root)
			if err := os.MkdirAll(filepath.Dir(r.Root), 0o755); err != nil {
				return nil, err
			}
			args := []string{}
			if r.auth != "" {
				args = append(args, "-c", r.auth)
			}
			args = append(args, "clone", "--quiet")
			if cfg.Branch != "" {
				args = append(args, "--branch", cfg.Branch)
			}
			args = append(args, cfg.RepoURL, r.Root)
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				return nil, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
			}
		}
	default:
		return nil, fmt.Errorf("set CODIST_REPO (local checkout) or CODIST_REPO_URL (git URL to clone)")
	}

	branch := cfg.Branch
	if branch == "" {
		b, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, err
		}
		branch = strings.TrimSpace(b)
	}
	r.branch.Store(&branch)
	r.Remote = cfg.FetchRemote || cfg.RepoURL != ""
	return r, nil
}

func (r *Repo) Name() string { return filepath.Base(r.Root) }

// Branch is the branch Codist follows. It can differ from the one checked out: see Viewing.
func (r *Repo) Branch() string {
	if b := r.branch.Load(); b != nil {
		return *b
	}
	return ""
}

func (r *Repo) TrackedRef() string {
	b := r.Branch()
	if r.Remote {
		return "refs/remotes/origin/" + b
	}
	if strings.HasPrefix(b, "origin/") {
		return "refs/remotes/" + b
	}
	return "refs/heads/" + b
}

type checkoutAt struct {
	branch string
	at     time.Time
}

// checkedOut is CurrentBranch, remembered for a moment because file reads ask for every file.
func (r *Repo) checkedOut() string {
	if c := r.checkout.Load(); c != nil && time.Since(c.at) < time.Second {
		return c.branch
	}
	b := r.CurrentBranch()
	r.checkout.Store(&checkoutAt{b, time.Now()})
	return b
}

// Viewing says Codist follows a branch that is not checked out. Everything then comes from that
// branch's commits, never from the working tree, and nothing can be changed.
func (r *Repo) Viewing() bool {
	return !r.Remote && r.checkedOut() != r.Branch()
}

// SwitchBranch follows another branch. Nothing is checked out and no file changes.
func (r *Repo) SwitchBranch(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("no such branch: %q", name)
	}
	prev := r.Branch()
	r.branch.Store(&name)
	if r.Head() == "" {
		r.branch.Store(&prev)
		return fmt.Errorf("no such branch: %q", name)
	}
	return nil
}

// HistoryCommit is one commit in the branch graph.
type HistoryCommit struct {
	SHA     string   `json:"sha"`
	Parents []string `json:"parents"`
	Author  string   `json:"author"`
	At      int64    `json:"at"`
	Subject string   `json:"subject"`
	Refs    []string `json:"refs"`
}

// History is the commits of the named branches, or of all of them, children before parents.
// Names that are not real branches are ignored, so nothing else reaches the git command line.
func (r *Repo) History(names []string, limit int) []HistoryCommit {
	known := map[string]bool{}
	for _, b := range r.ListBranches() {
		known[b.Name] = true
	}
	args := []string{"log", "--topo-order", "-n", strconv.Itoa(limit), "--format=%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1e"}
	picked := 0
	for _, n := range names {
		if !known[n] {
			continue
		}
		picked++
		if r.Remote {
			args = append(args, "refs/remotes/origin/"+n)
		} else if strings.HasPrefix(n, "origin/") {
			args = append(args, "refs/remotes/"+n)
		} else {
			args = append(args, "refs/heads/"+n)
		}
	}
	if picked == 0 {
		args = append(args, "--branches", "--remotes=origin")
	}
	out, err := r.git(args...)
	commits := []HistoryCommit{}
	if err != nil {
		return commits
	}
	for _, rec := range strings.Split(out, "\x1e") {
		p := strings.SplitN(strings.TrimSpace(rec), "\x1f", 6)
		if len(p) < 6 {
			continue
		}
		at, _ := strconv.ParseInt(p[3], 10, 64)
		c := HistoryCommit{SHA: p[0], Parents: strings.Fields(p[1]), Author: p[2], At: at * 1000, Subject: p[4], Refs: []string{}}
		for _, d := range strings.Split(p[5], ",") {
			d = strings.TrimSpace(d)
			if i := strings.Index(d, " -> "); i >= 0 {
				d = d[i+4:]
			}
			if r.Remote {
				d = strings.TrimPrefix(d, "origin/")
			}
			if d != "" && d != "HEAD" && d != "origin/HEAD" && !strings.HasPrefix(d, "tag: ") {
				c.Refs = append(c.Refs, d)
			}
		}
		commits = append(commits, c)
	}
	return commits
}

// BranchRef is one branch in the switcher.
type BranchRef struct {
	Name       string `json:"name"`
	Remote     bool   `json:"remote"`
	SHA        string `json:"sha"`
	Message    string `json:"message"`
	By         string `json:"by"`
	At         int64  `json:"at"`
	CheckedOut bool   `json:"checkedOut"`
}

// ListBranches is every branch that can be followed, the most recently changed first.
func (r *Repo) ListBranches() []BranchRef {
	out, err := r.git("for-each-ref", "--sort=-committerdate", "--format=%(refname)%1f%(objectname)%1f%(authorname)%1f%(committerdate:unix)%1f%(subject)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return []BranchRef{}
	}
	cur := r.checkedOut()
	local := map[string]bool{}
	type row struct{ ref, sha, by, at, msg string }
	var rows []row
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		p := strings.SplitN(l, "\x1f", 5)
		if len(p) < 5 {
			continue
		}
		rows = append(rows, row{p[0], p[1], p[2], p[3], p[4]})
		if n, ok := strings.CutPrefix(p[0], "refs/heads/"); ok {
			local[n] = true
		}
	}
	refs := []BranchRef{}
	for _, c := range rows {
		b := BranchRef{SHA: c.sha, By: c.by, Message: c.msg}
		sec, _ := strconv.ParseInt(c.at, 10, 64)
		b.At = sec * 1000
		if n, ok := strings.CutPrefix(c.ref, "refs/heads/"); ok {
			if r.Remote {
				continue // a mirror follows origin's branches
			}
			b.Name, b.CheckedOut = n, n == cur
		} else {
			n := strings.TrimPrefix(c.ref, "refs/remotes/origin/")
			if n == "HEAD" {
				continue
			}
			if r.Remote {
				b.Name = n
			} else if local[n] {
				continue
			} else {
				b.Name = "origin/" + n
			}
			b.Remote = true
		}
		refs = append(refs, b)
	}
	return refs
}

func (r *Repo) Tracking() string {
	if r.Remote {
		return "origin/" + r.Branch()
	}
	return r.Branch()
}

func (r *Repo) git(args ...string) (string, error) {
	full := []string{"-C", r.Root, "-c", "core.quotePath=false"}
	if r.auth != "" {
		full = append(full, "-c", r.auth)
	}
	cmd := exec.Command("git", append(full, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *Repo) Fetch() {
	if !r.Remote {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.git("fetch", "--quiet", "origin"); err != nil {
		slog.Warn("fetch failed", "err", err)
	}
}

// Head is the sha of the commit we follow, or "" when the branch has no commits yet.
func (r *Repo) Head() string {
	out, err := r.git("rev-parse", "--verify", "--quiet", r.TrackedRef()+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

const logFormat = "--format=%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%B%x1e"

func parseLog(out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 6)
		if len(f) < 6 {
			continue
		}
		secs, _ := strconv.ParseInt(f[3], 10, 64)
		parents := strings.Fields(f[1])
		if parents == nil {
			parents = []string{}
		}
		commits = append(commits, Commit{
			SHA: f[0], ShortSHA: f[0][:7], Parents: parents, Author: f[2], Time: secs * 1000,
			Message: f[4], FullMessage: strings.TrimRight(f[5], "\n"),
		})
	}
	return commits
}

func (r *Repo) Log(limit int) []Commit {
	if r.Head() == "" {
		return []Commit{}
	}
	out, err := r.git("log", logFormat, "-n", strconv.Itoa(limit), r.TrackedRef())
	if err != nil {
		return []Commit{}
	}
	return parseLog(out)
}

// CommitsSince lists commits reachable from head but not from since, oldest first.
func (r *Repo) CommitsSince(since, head string, max int) []Commit {
	rangeArg := head
	if since != "" {
		rangeArg = since + ".." + head
	}
	out, err := r.git("log", logFormat, "-n", strconv.Itoa(max), rangeArg)
	if err != nil {
		return nil
	}
	c := parseLog(out)
	for i, j := 0, len(c)-1; i < j; i, j = i+1, j-1 {
		c[i], c[j] = c[j], c[i]
	}
	return c
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

func (r *Repo) Commit(sha string) (Commit, error) {
	if !shaPattern.MatchString(sha) {
		return Commit{}, fmt.Errorf("not a commit id: %q", sha)
	}
	out, err := r.git("log", "-1", logFormat, sha, "--")
	if err != nil {
		return Commit{}, err
	}
	c := parseLog(out)
	if len(c) == 0 {
		return Commit{}, fmt.Errorf("unknown commit %s", sha)
	}
	return c[0], nil
}

// Diff of a commit against its first parent, or the empty tree for a root commit.
func (r *Repo) Diff(c Commit) ([]FileChange, error) {
	var out string
	var err error
	switch len(c.Parents) {
	case 0:
		out, err = r.git("diff-tree", "-p", "--root", "--no-commit-id", "-M", "--no-color", "-U3", c.SHA)
	case 1:
		out, err = r.git("diff", "-M", "--no-color", "-U3", c.Parents[0], c.SHA)
	default:
		return r.mergeDiff(c)
	}
	if err != nil {
		return nil, err
	}
	return parseDiff(out), nil
}

// mergeDiff is what a merge commit itself adds: lines that differ from EVERY parent, which is where
// conflict resolutions (and "evil merge" edits) live. Changes that merged in cleanly were made, and
// are reviewed, in the commits of the branch they came from.
func (r *Repo) mergeDiff(c Commit) ([]FileChange, error) {
	var per [][]FileChange
	for _, p := range c.Parents {
		out, err := r.git("diff", "-M", "--no-color", "-U3", p, c.SHA)
		if err != nil {
			return nil, err
		}
		per = append(per, parseDiff(out))
	}
	var files []FileChange
	for _, f := range per[0] {
		lines := map[int]int{}
		for _, l := range f.AddedLines {
			lines[l] = 1
		}
		inAll := true
		for _, other := range per[1:] {
			var g *FileChange
			for k := range other {
				if other[k].NewPath == f.NewPath {
					g = &other[k]
				}
			}
			if g == nil {
				inAll = false
				break
			}
			for _, l := range g.AddedLines {
				if lines[l] > 0 {
					lines[l]++
				}
			}
		}
		if !inAll {
			continue
		}
		var keep []int
		for _, l := range f.AddedLines {
			if lines[l] == len(per) {
				keep = append(keep, l)
			}
		}
		if len(keep) == 0 && !f.Deleted() {
			continue
		}
		f.AddedLines = keep
		files = append(files, f)
	}
	return files, nil
}

// Operation names a merge, rebase, cherry-pick or revert in progress in the working tree, or "".
func (r *Repo) Operation() string {
	for _, op := range []struct{ path, name string }{
		{"MERGE_HEAD", "merge"}, {"rebase-merge", "rebase"}, {"rebase-apply", "rebase"},
		{"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"},
	} {
		p, err := r.git("rev-parse", "--git-path", op.path)
		if err != nil {
			continue
		}
		p = strings.TrimSpace(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.Root, p)
		}
		if _, err := os.Stat(p); err == nil {
			return op.name
		}
	}
	return ""
}

// Conflicts lists files with unresolved merge conflicts.
func (r *Repo) Conflicts() []string {
	out, err := r.git("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parseDiff(out string) []FileChange {
	files := []FileChange{}
	var cur *FileChange
	var patch strings.Builder
	newLine := 0
	inHunk, pendingDelete, addedSinceDelete := false, false, false

	flushDelete := func() {
		if pendingDelete && !addedSinceDelete && cur != nil {
			cur.DeletedAt = append(cur.DeletedAt, newLine)
		}
		pendingDelete, addedSinceDelete = false, false
	}
	finish := func() {
		if cur == nil {
			return
		}
		flushDelete()
		cur.Patch = patch.String()
		if cur.NewPath != "" {
			cur.Path = cur.NewPath
		} else {
			cur.Path = cur.OldPath
		}
		files = append(files, *cur)
		cur = nil
		patch.Reset()
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			finish()
			cur = &FileChange{ChangeType: "MODIFY", AddedLines: []int{}, DeletedAt: []int{}}
			inHunk = false
			// Fallback paths for diffs without ---/+++ lines (pure renames, mode changes).
			if parts := strings.SplitN(strings.TrimPrefix(line, "diff --git "), " b/", 2); len(parts) == 2 {
				cur.OldPath = strings.TrimPrefix(parts[0], "a/")
				cur.NewPath = parts[1]
			}
			patch.WriteString(line + "\n")
			continue
		}
		if cur == nil {
			continue
		}
		patch.WriteString(line + "\n")
		if !inHunk {
			switch {
			case strings.HasPrefix(line, "new file mode"):
				cur.ChangeType, cur.OldPath = "ADD", ""
			case strings.HasPrefix(line, "deleted file mode"):
				cur.ChangeType, cur.NewPath = "DELETE", ""
			case strings.HasPrefix(line, "rename from "):
				cur.ChangeType, cur.OldPath = "RENAME", strings.TrimPrefix(line, "rename from ")
			case strings.HasPrefix(line, "rename to "):
				cur.NewPath = strings.TrimPrefix(line, "rename to ")
			case strings.HasPrefix(line, "copy from "):
				cur.ChangeType, cur.OldPath = "COPY", strings.TrimPrefix(line, "copy from ")
			case strings.HasPrefix(line, "copy to "):
				cur.NewPath = strings.TrimPrefix(line, "copy to ")
			case strings.HasPrefix(line, "Binary files"):
				cur.Binary = true
			case strings.HasPrefix(line, "--- "):
				if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" {
					cur.OldPath = strings.TrimPrefix(p, "a/")
				}
			case strings.HasPrefix(line, "+++ "):
				if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
					cur.NewPath = strings.TrimPrefix(p, "b/")
				}
			}
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			flushDelete()
			inHunk = true
			newLine, _ = strconv.Atoi(m[1])
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			cur.AddedLines = append(cur.AddedLines, newLine)
			newLine++
			addedSinceDelete = true
		case strings.HasPrefix(line, "-"):
			if !pendingDelete {
				pendingDelete, addedSinceDelete = true, false
			}
		case strings.HasPrefix(line, " "):
			flushDelete()
			newLine++
		}
	}
	finish()
	return files
}

// ReadFile returns a file's content at a commit; ok is false when it does not exist there.
func (r *Repo) ReadFile(sha, path string) (string, bool) {
	out, err := r.git("show", sha+":"+path)
	if err != nil {
		return "", false
	}
	return out, true
}

// ReadWorking returns a file from the working tree, refusing paths outside the repository.
func (r *Repo) ReadWorking(path string) (string, bool, error) {
	if r.Viewing() {
		text, ok := r.ReadFile(r.Head(), path)
		return text, ok, nil
	}
	p := filepath.Clean(filepath.Join(r.Root, path))
	if !strings.HasPrefix(p, r.Root+string(filepath.Separator)) || strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return "", false, fmt.Errorf("path outside repository: %s", path)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false, nil
	}
	return string(b), true, nil
}

// JavaFilesAt returns every .java file at a commit, read in one git process.
func (r *Repo) JavaFilesAt(sha string) map[string][]byte {
	files := map[string][]byte{}
	if sha == "" {
		return files
	}
	list, err := r.git("ls-tree", "-r", "--name-only", sha)
	if err != nil {
		return files
	}
	var paths []string
	for _, p := range strings.Split(list, "\n") {
		if strings.HasSuffix(p, ".java") {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return files
	}

	cmd := exec.Command("git", "-C", r.Root, "cat-file", "--batch")
	var input strings.Builder
	for _, p := range paths {
		input.WriteString(sha + ":" + p + "\n")
	}
	cmd.Stdin = strings.NewReader(input.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return files
	}
	if err := cmd.Start(); err != nil {
		return files
	}
	rd := bufio.NewReader(stdout)
	for _, p := range paths {
		header, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		f := strings.Fields(header)
		if len(f) < 3 || f[1] != "blob" {
			continue // "missing"
		}
		size, _ := strconv.Atoi(f[2])
		buf := make([]byte, size+1) // content plus trailing newline
		if _, err := io.ReadFull(rd, buf); err != nil {
			break
		}
		files[p] = buf[:size]
	}
	_ = cmd.Wait()
	return files
}

var skipDirs = map[string]bool{".git": true, "target": true, "build": true, "out": true, "node_modules": true, ".idea": true, ".gradle": true}

func (r *Repo) walkJava(fn func(rel string, path string, d fs.DirEntry)) {
	_ = filepath.WalkDir(r.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != r.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".java") {
			rel, _ := filepath.Rel(r.Root, path)
			fn(filepath.ToSlash(rel), path, d)
		}
		return nil
	})
}

func (r *Repo) JavaFilesWorking() map[string][]byte {
	if r.Viewing() {
		return r.JavaFilesAt(r.Head())
	}
	files := map[string][]byte{}
	r.walkJava(func(rel, path string, _ fs.DirEntry) {
		if b, err := os.ReadFile(path); err == nil {
			files[rel] = b
		}
	})
	return files
}

// Fingerprint is a cheap hash of paths, sizes and mtimes, used to notice edits in the working tree.
func (r *Repo) Fingerprint() string {
	if r.Viewing() {
		return "viewing:" + r.Head()
	}
	h := sha1.New()
	r.walkJava(func(rel, _ string, d fs.DirEntry) {
		info, err := d.Info()
		if err != nil {
			return
		}
		h.Write([]byte(rel))
		_ = binary.Write(h, binary.LittleEndian, info.Size())
		_ = binary.Write(h, binary.LittleEndian, info.ModTime().UnixNano())
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// BuildFingerprint covers the build files, which issues can point at too (dependency findings).
func (r *Repo) BuildFingerprint() string {
	if r.Viewing() {
		return "viewing:" + r.Head()
	}
	var b strings.Builder
	for _, p := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		if info, err := os.Stat(filepath.Join(r.Root, p)); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// CurrentBranch is the branch checked out in the working tree.
func (r *Repo) CurrentBranch() string {
	out, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// WriteWorking replaces a file in the working tree, refusing paths outside the repository.
func (r *Repo) WriteWorking(path, content string) error {
	p := filepath.Clean(filepath.Join(r.Root, path))
	if !strings.HasPrefix(p, r.Root+string(filepath.Separator)) || strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return fmt.Errorf("path outside repository: %s", path)
	}
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), info.Mode().Perm())
}

// CommitFiles commits only the given paths, leaving anything else that is staged or modified alone.
func (r *Repo) CommitFiles(paths []string, message string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	args := append([]string{"commit", "--quiet", "-m", message, "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return "", err
	}
	out, err := r.git("rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// UnifiedDiff is a git-style patch between two versions of one file.
func UnifiedDiff(path, before, after string) (string, error) {
	dir, err := os.MkdirTemp("", "codist-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte(before), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(b, []byte(after), 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "-U3", a, b)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 { // 1 = files differ
			return "", err
		}
	}
	// git prints the temp paths as "a" + /tmp/.../a; show the repository path instead.
	patch := strings.NewReplacer("a"+a, "a/"+path, "b"+b, "b/"+path, a, "a/"+path, b, "b/"+path).Replace(string(out))
	return patch, nil
}

// CommitsTouching lists commits that changed path, newest first.
func (r *Repo) CommitsTouching(path string) []string {
	out, err := r.git("log", "--format=%H", r.TrackedRef(), "--", path)
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// BlameLine is who last changed a line, at the tracked branch.
type BlameLine struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
}

// Blame returns the commit that last changed line n of path on the tracked branch.
func (r *Repo) Blame(path string, n int) (BlameLine, bool) { return r.BlameAt(r.TrackedRef(), path, n) }

// BlameAt returns the commit that last changed line n of path as of rev.
func (r *Repo) BlameAt(rev, path string, n int) (BlameLine, bool) {
	out, err := r.git("blame", "--porcelain", "-L", fmt.Sprintf("%d,%d", n, n), rev, "--", path)
	if err != nil {
		return BlameLine{}, false
	}
	var b BlameLine
	for i, l := range strings.Split(out, "\n") {
		switch {
		case i == 0:
			if f := strings.Fields(l); len(f) > 0 {
				b.SHA = f[0]
			}
		case strings.HasPrefix(l, "author "):
			b.Author = strings.TrimPrefix(l, "author ")
		case strings.HasPrefix(l, "author-time "):
			secs, _ := strconv.ParseInt(strings.TrimPrefix(l, "author-time "), 10, 64)
			b.Time = secs * 1000
		case strings.HasPrefix(l, "summary "):
			b.Summary = strings.TrimPrefix(l, "summary ")
		}
	}
	return b, b.SHA != "" && !strings.HasPrefix(b.SHA, "0000000")
}

// CreateWorking writes a new file into the working tree (creating folders) and stages it.
func (r *Repo) CreateWorking(path, content string) error {
	p := filepath.Clean(filepath.Join(r.Root, path))
	if !strings.HasPrefix(p, r.Root+string(filepath.Separator)) || strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return fmt.Errorf("path outside repository: %s", path)
	}
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return err
	}
	_, err := r.git("add", "--", path)
	return err
}

// RemoveNew undoes CreateWorking.
func (r *Repo) RemoveNew(path string) {
	_, _ = r.git("rm", "--cached", "--quiet", "--", path)
	_ = os.Remove(filepath.Join(r.Root, path))
}

// ExistsWorking reports whether a working-tree file exists.
func (r *Repo) ExistsWorking(path string) bool {
	_, err := os.Stat(filepath.Join(r.Root, filepath.Clean(path)))
	return err == nil
}

// Tags lists tags, newest first.
func (r *Repo) Tags() []string {
	out, err := r.git("tag", "--sort=-creatordate")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// Range lists the commits in (from, to], oldest first. An empty from means all history.
func (r *Repo) Range(from, to string, max int) []Commit {
	spec := to
	if from != "" {
		spec = from + ".." + to
	}
	out, err := r.git("log", logFormat, "--reverse", "-n", strconv.Itoa(max), spec)
	if err != nil {
		return nil
	}
	return parseLog(out)
}

// DiffRange is the combined diff between two commits.
func (r *Repo) DiffRange(from, to string) ([]FileChange, error) {
	out, err := r.git("diff", "-M", "--no-color", "-U3", from, to)
	if err != nil {
		return nil, err
	}
	return parseDiff(out), nil
}

// WorkingChanges is what the working tree changes against the last commit: tracked edits plus new,
// untracked files (every line counts as added).
func (r *Repo) WorkingChanges() ([]FileChange, error) {
	if r.Viewing() {
		return []FileChange{}, nil // those changes belong to the branch that is checked out
	}
	out, err := r.git("diff", "-M", "--no-color", "-U3", "HEAD")
	if err != nil {
		return nil, err
	}
	files := parseDiff(out)
	untracked, _ := r.git("ls-files", "--others", "--exclude-standard")
	for _, p := range strings.Split(untracked, "\n") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		src, ok, _ := r.ReadWorking(p)
		if !ok {
			continue
		}
		n := strings.Count(src, "\n")
		if !strings.HasSuffix(src, "\n") {
			n++
		}
		lines := make([]int, n)
		for i := range lines {
			lines[i] = i + 1
		}
		files = append(files, FileChange{ChangeType: "ADD", NewPath: p, Path: p, AddedLines: lines})
	}
	return files, nil
}

// Contributor is one author's share of the history.
type Contributor struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

// Contributors lists the authors of the tracked branch, most commits first.
func (r *Repo) Contributors() []Contributor {
	out, err := r.git("shortlog", "-sn", "--no-merges", r.TrackedRef())
	if err != nil {
		return nil
	}
	var cs []Contributor
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.SplitN(strings.TrimSpace(l), "\t", 2); len(f) == 2 {
			n, _ := strconv.Atoi(strings.TrimSpace(f[0]))
			cs = append(cs, Contributor{Name: f[1], Commits: n})
		}
	}
	return cs
}

// Churn counts, per file, how many commits changed it.
func (r *Repo) Churn() map[string]int {
	out, err := r.git("log", "--no-merges", "--name-only", "--format=", r.TrackedRef())
	if err != nil {
		return nil
	}
	c := map[string]int{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			c[l]++
		}
	}
	return c
}

// TrackedFiles lists the files at the tracked branch.
func (r *Repo) TrackedFiles() []string {
	out, err := r.git("ls-tree", "-r", "--name-only", r.TrackedRef())
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// Run executes a git command in the repository.
func (r *Repo) Run(args ...string) (string, error) { return r.git(args...) }

// Pushed reports whether a commit is already on a remote branch, so rewriting it would diverge.
func (r *Repo) Pushed(sha string) bool {
	out, err := r.git("branch", "-r", "--contains", sha)
	return err == nil && strings.TrimSpace(out) != ""
}

// Reword replaces a commit's message without touching any file: the commit and those after it are
// recreated with the same trees, authors and dates, then the branch moves in one atomic step. It
// returns old -> new ids. Merges in the range, and commits already pushed, are refused.
func (r *Repo) Reword(sha, message string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Remote {
		return nil, errors.New("this repository is a read-only mirror")
	}
	if b, _ := r.git("rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(b) != r.Branch() {
		return nil, fmt.Errorf("switch to %s first", r.Branch())
	}
	head := r.Head()
	if _, err := r.git("merge-base", "--is-ancestor", sha, head); err != nil {
		return nil, errors.New("this commit is not on the current branch")
	}
	if r.Pushed(sha) {
		return nil, errors.New("this commit is already pushed: rewording it would rewrite shared history, so Codist will not do it")
	}
	// The commit and everything after it; a root commit means the whole branch.
	var list string
	var err error
	if parents, perr := r.git("rev-list", "--parents", "-n", "1", sha); perr == nil && len(strings.Fields(parents)) > 1 {
		list, err = r.git("rev-list", "--reverse", "--ancestry-path", strings.Fields(parents)[1]+".."+head)
	} else {
		list, err = r.git("rev-list", "--reverse", head)
	}
	if err != nil {
		return nil, err
	}
	mapping := map[string]string{}
	newHead := ""
	for _, c := range strings.Fields(list) {
		info, err := r.git("show", "-s", "--format=%T%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%ce%x1f%cI%x1f%B", c)
		if err != nil {
			return nil, err
		}
		f := strings.SplitN(info, "\x1f", 9)
		if len(f) != 9 {
			return nil, fmt.Errorf("cannot read commit %s", c)
		}
		parents := strings.Fields(f[1])
		if len(parents) > 1 {
			return nil, errors.New("there is a merge after this commit; reword it with git rebase instead")
		}
		msg := strings.TrimRight(f[8], "\n") + "\n"
		if c == sha {
			msg = strings.TrimSpace(message) + "\n"
		}
		args := []string{"-C", r.Root, "commit-tree", f[0], "-F", "-"}
		for _, p := range parents {
			if n, ok := mapping[p]; ok {
				p = n
			}
			args = append(args, "-p", p)
		}
		cmd := exec.Command("git", args...)
		cmd.Stdin = strings.NewReader(msg)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME="+f[2], "GIT_AUTHOR_EMAIL="+f[3], "GIT_AUTHOR_DATE="+f[4],
			"GIT_COMMITTER_NAME="+f[5], "GIT_COMMITTER_EMAIL="+f[6], "GIT_COMMITTER_DATE="+f[7])
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("recreating %s: %w", c[:7], err)
		}
		newHead = strings.TrimSpace(string(out))
		mapping[c] = newHead
	}
	if newHead == "" {
		return nil, errors.New("nothing to reword")
	}
	if _, err := r.git("update-ref", "-m", "codist: reword "+sha[:7], "refs/heads/"+r.Branch(), newHead, head); err != nil {
		return nil, fmt.Errorf("the branch moved meanwhile, nothing was changed: %w", err)
	}
	return mapping, nil
}

// CommitPaths stages the given paths and commits exactly those, leaving other changes alone.
func (r *Repo) CommitPaths(paths []string, message string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.git(append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := r.git(append([]string{"commit", "--quiet", "-m", message, "--"}, paths...)...); err != nil {
		return "", err
	}
	out, err := r.git("rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// UserName is the git user.name configured for the repository (who signs off).
func (r *Repo) UserName() string {
	out, err := r.git("config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
