package review

import (
	"sort"
	"strings"
	"sync"
	"time"

	"codist/internal/gitx"
)

// PersonStats is how much one person has written, measured from git.
type PersonStats struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
	Added   int    `json:"added"`   // lines added over all their commits
	Removed int    `json:"removed"` // lines removed over all their commits
	Files   int    `json:"files"`   // distinct files they changed
	Owned   int    `json:"owned"`   // lines in the code today that git blame gives to them
}

// PeopleReport is everyone on the followed branch.
type PeopleReport struct {
	Head       string        `json:"head"`
	People     []PersonStats `json:"people"`
	TotalLines int           `json:"totalLines"`
	TookMs     int64         `json:"tookMs"`
}

// codeFile says whether a file counts as code rather than a lockfile, image or generated output.
func codeFile(path string) bool {
	for _, skip := range []string{"/target/", "/build/", "/node_modules/", "/dist/", ".min."} {
		if strings.Contains("/"+path, skip) {
			return false
		}
	}
	for _, ext := range []string{".java", ".kt", ".groovy", ".kts", ".gradle", ".xml", ".yml", ".yaml", ".properties", ".sql", ".sh", ".js", ".ts", ".py", ".go", ".html", ".css"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// maxBlameFiles keeps a huge repository from blaming for minutes; history totals still cover everything.
const maxBlameFiles = 3000

// People measures everyone, blaming files concurrently. Names follow git, so one person with two
// spellings counts twice unless the repository has a .mailmap.
func People(repo *gitx.Repo) PeopleReport {
	start := time.Now()
	head := repo.Head()
	stats := map[string]*PersonStats{}
	at := func(name string) *PersonStats {
		if stats[name] == nil {
			stats[name] = &PersonStats{Name: name}
		}
		return stats[name]
	}
	for _, c := range repo.AuthorHistory(codeFile) {
		p := at(c.Author)
		p.Commits = c.Commits
		p.Added, p.Removed, p.Files = c.Added, c.Removed, c.Files
	}
	var files []string
	for _, f := range repo.TrackedFiles() {
		if codeFile(f) {
			files = append(files, f)
		}
	}
	if len(files) > maxBlameFiles {
		files = files[:maxBlameFiles]
	}
	var mu sync.Mutex
	total := 0
	parallel(len(files), func(i int) {
		owned := repo.BlameCounts(head, files[i])
		mu.Lock()
		defer mu.Unlock()
		for name, n := range owned {
			at(name).Owned += n
			total += n
		}
	})
	report := PeopleReport{Head: head, People: []PersonStats{}, TotalLines: total, TookMs: time.Since(start).Milliseconds()}
	for _, p := range stats {
		report.People = append(report.People, *p)
	}
	sort.Slice(report.People, func(i, j int) bool {
		a, b := report.People[i], report.People[j]
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		return a.Name < b.Name
	})
	return report
}
