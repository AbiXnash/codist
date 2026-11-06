// Package git reads a repository through the git command. Everything Codist needs from git goes
// through Repo, so a pure-Go backend can replace the command where git is not installed.
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Repo is one repository on disk.
type Repo struct {
	Root string
}

// Commit is one commit, newest first in lists.
type Commit struct {
	SHA     string    `json:"sha"`
	Short   string    `json:"short"`
	Author  string    `json:"author"`
	Time    time.Time `json:"time"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	Parents []string  `json:"parents"`
}

// FileChange is one file a commit changed, with the new-side line numbers it added or changed.
type FileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"` // added, modified, deleted, renamed
	Added   []int  `json:"added"`
	Patch   string `json:"patch"`
}

// Open finds the repository that contains dir.
func Open(dir string) (*Repo, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", dir)
	}
	return &Repo{Root: filepath.Clean(strings.TrimSpace(string(out)))}, nil
}

// Available says whether the git command can be run.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Root, "-c", "core.quotepath=off"}, args...)...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

// Head is the commit checked out, or "" in an empty repository.
func (r *Repo) Head() string {
	out, err := r.run("rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Branch is the checked-out branch name, or "detached".
func (r *Repo) Branch() string {
	out, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(out) == "HEAD" {
		return "detached"
	}
	return strings.TrimSpace(out)
}

const sep, end = "\x1f", "\x1e"

// Log lists up to limit commits reachable from HEAD, newest first.
func (r *Repo) Log(limit int) ([]Commit, error) {
	out, err := r.run("log", "-n", strconv.Itoa(limit), "--format=%H"+sep+"%an"+sep+"%at"+sep+"%P"+sep+"%s"+sep+"%b"+end)
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, rec := range strings.Split(out, end) {
		f := strings.Split(strings.TrimLeft(rec, "\n"), sep)
		if len(f) < 6 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, Commit{SHA: f[0], Short: f[0][:min(7, len(f[0]))], Author: f[1], Time: time.Unix(at, 0),
			Parents: strings.Fields(f[3]), Subject: f[4], Body: strings.TrimSpace(f[5])})
	}
	return cs, nil
}

// Get reads one commit.
func (r *Repo) Get(sha string) (Commit, error) {
	out, err := r.run("log", "-n", "1", "--format=%H"+sep+"%an"+sep+"%at"+sep+"%P"+sep+"%s"+sep+"%b"+end, sha, "--")
	if err != nil {
		return Commit{}, err
	}
	f := strings.Split(strings.TrimSuffix(strings.TrimSpace(out), end), sep)
	if len(f) < 6 {
		return Commit{}, fmt.Errorf("no commit %s", sha)
	}
	at, _ := strconv.ParseInt(f[2], 10, 64)
	return Commit{SHA: f[0], Short: f[0][:7], Author: f[1], Time: time.Unix(at, 0), Parents: strings.Fields(f[3]),
		Subject: f[4], Body: strings.TrimSpace(f[5])}, nil
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// Changes lists what a commit changed against its first parent, or against nothing for the first commit.
func (r *Repo) Changes(sha string) ([]FileChange, error) {
	args := []string{"show", "--format=", "--no-color", "-M", "-U3", "--no-ext-diff", sha}
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	var files []FileChange
	var cur *FileChange
	var body strings.Builder
	newLine := 0
	flush := func() {
		if cur != nil {
			cur.Patch = body.String()
			files = append(files, *cur)
		}
		body.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &FileChange{Status: "modified"}
			continue
		case cur == nil:
			continue
		case strings.HasPrefix(line, "new file mode"):
			cur.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			cur.OldPath, cur.Status = strings.TrimPrefix(line, "rename from "), "renamed"
		case strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" && cur.OldPath == "" {
				cur.OldPath = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(line, "+++ "):
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				cur.Path = strings.TrimPrefix(p, "b/")
			} else {
				cur.Path = cur.OldPath
			}
		case strings.HasPrefix(line, "@@"):
			if m := hunkRe.FindStringSubmatch(line); m != nil {
				newLine, _ = strconv.Atoi(m[1])
			}
			body.WriteString(line + "\n")
		case strings.HasPrefix(line, "+"):
			cur.Added = append(cur.Added, newLine)
			newLine++
			body.WriteString(line + "\n")
		case strings.HasPrefix(line, "-"):
			body.WriteString(line + "\n")
		case strings.HasPrefix(line, " "):
			newLine++
			body.WriteString(line + "\n")
		case strings.HasPrefix(line, `\`):
			body.WriteString(line + "\n")
		}
	}
	flush()
	return files, nil
}

// FileAt reads a file as it was at a commit.
func (r *Repo) FileAt(sha, path string) ([]byte, bool) {
	out, err := r.run("show", sha+":"+path)
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// Files lists every tracked file at a commit.
func (r *Repo) Files(sha string) ([]string, error) {
	out, err := r.run("ls-tree", "-r", "--name-only", sha)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// Churn counts, for each file, how many of the last limit commits changed it.
func (r *Repo) Churn(limit int) map[string]int {
	out, err := r.run("log", "-n", strconv.Itoa(limit), "--format=", "--name-only")
	counts := map[string]int{}
	if err != nil {
		return counts
	}
	for _, p := range strings.Split(out, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			counts[p]++
		}
	}
	return counts
}

// LastChange is the commit that last changed one line of a file at HEAD.
func (r *Repo) LastChange(path string, line int) (Commit, bool) {
	out, err := r.run("blame", "--porcelain", "-L", fmt.Sprintf("%d,%d", line, line), "HEAD", "--", path)
	if err != nil || len(out) < 40 {
		return Commit{}, false
	}
	c, err := r.Get(out[:40])
	return c, err == nil
}

// Count is how many commits HEAD has.
func (r *Repo) Count() int {
	out, err := r.run("rev-list", "--count", "HEAD")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}
