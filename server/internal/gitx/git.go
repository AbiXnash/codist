// Package gitx runs the git CLI: cloning, fetching, history, diffs and file contents at a commit.
package gitx

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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
	Root   string
	Branch string
	Remote bool // following origin/<branch>
	auth   string
	mu     sync.Mutex
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

	r.Branch = cfg.Branch
	if r.Branch == "" {
		b, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, err
		}
		r.Branch = strings.TrimSpace(b)
	}
	r.Remote = cfg.FetchRemote || cfg.RepoURL != ""
	return r, nil
}

func (r *Repo) Name() string { return filepath.Base(r.Root) }

func (r *Repo) TrackedRef() string {
	if r.Remote {
		return "refs/remotes/origin/" + r.Branch
	}
	return "refs/heads/" + r.Branch
}

func (r *Repo) Tracking() string {
	if r.Remote {
		return "origin/" + r.Branch
	}
	return r.Branch
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
	if len(c.Parents) == 0 {
		out, err = r.git("diff-tree", "-p", "--root", "--no-commit-id", "-M", "--no-color", "-U3", c.SHA)
	} else {
		out, err = r.git("diff", "-M", "--no-color", "-U3", c.Parents[0], c.SHA)
	}
	if err != nil {
		return nil, err
	}
	return parseDiff(out), nil
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

// UserName is the git user.name configured for the repository (who signs off).
func (r *Repo) UserName() string {
	out, err := r.git("config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
