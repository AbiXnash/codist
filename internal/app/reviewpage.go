package app

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codist/internal/git"
	"codist/internal/review"
	"codist/internal/store"
)

type commitRow struct {
	git.Commit
	Status  string // reviewed, running, queued, failed, waiting
	Counts  map[string]int
	Serious int
	Notes   int
	On      bool
}

type diffRow struct {
	Kind     string // hunk, add, del, ctx
	Old, New int
	Text     string
	Findings []store.Finding
}

type fileView struct {
	Path, Status string
	Rows         []diffRow
	Outside      []store.Finding // findings on lines the diff does not show
	Serious      int
	Java         bool
}

type detail struct {
	Commit  git.Commit
	Review  store.Review
	Has     bool // reviewed at least once
	Waiting bool
	Files   []fileView
	Counts  map[string]int
	Serious int
	Notes   int
	Took    string
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func (a *App) reviewRoutes() {
	a.web.Handle("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		rows := a.commitRows(r.URL.Query().Get("c"))
		var d *detail
		for _, c := range rows {
			if c.On {
				dd := a.detail(c.SHA)
				d = &dd
			}
		}
		a.web.Render(w, r, "review", "review", "Review", map[string]any{"Commits": rows, "Detail": d})
	})
	a.web.Handle("GET /review/list", func(w http.ResponseWriter, r *http.Request) {
		a.web.Fragment(w, "review", "list", a.commitRows(r.URL.Query().Get("c")))
	})
	a.web.Handle("GET /review/{sha}", func(w http.ResponseWriter, r *http.Request) {
		a.web.Fragment(w, "review", "detail", a.detail(r.PathValue("sha")))
	})
	a.web.Handle("POST /review/{sha}", func(w http.ResponseWriter, r *http.Request) {
		c, err := a.repo.Get(r.PathValue("sha"))
		if err != nil {
			http.Error(w, "no such commit", http.StatusNotFound)
			return
		}
		sha := c.SHA
		a.queue.push(job{sha: sha, opt: review.Options{}}, true)
		a.web.Fragment(w, "review", "detail", a.detail(sha))
	})
}

func (a *App) commitRows(selected string) []commitRow {
	log, _ := a.repo.Log(100)
	reviews := a.store.Reviews()
	counts := a.store.Counts()
	rows := make([]commitRow, 0, len(log))
	for i, c := range log {
		row := commitRow{Commit: c, Counts: counts[c.SHA], Status: "waiting"}
		if rv, ok := reviews[c.SHA]; ok {
			row.Status = rv.Status
		}
		if a.queue.waiting(c.SHA) && row.Status != "running" {
			row.Status = "queued"
		}
		for sev, n := range row.Counts {
			if sev == "low" || sev == "info" {
				row.Notes += n
			} else {
				row.Serious += n
			}
		}
		row.On = c.SHA == selected || strings.HasPrefix(c.SHA, selected) && selected != "" || selected == "" && i == 0
		rows = append(rows, row)
	}
	return rows
}

func (a *App) detail(sha string) detail {
	c, err := a.repo.Get(sha)
	if err != nil {
		return detail{Commit: git.Commit{Subject: "This commit is not in the repository."}}
	}
	d := detail{Commit: c, Counts: map[string]int{}, Waiting: a.queue.waiting(c.SHA)}
	d.Review, d.Has = a.store.GetReview(c.SHA)
	if d.Has && !d.Review.Finished.IsZero() {
		d.Took = d.Review.Finished.Sub(d.Review.Started).Round(time.Second).String()
	}
	findings := a.store.Findings(c.SHA)
	byFile := map[string][]store.Finding{}
	for _, f := range findings {
		byFile[f.Path] = append(byFile[f.Path], f)
		d.Counts[f.Severity]++
		if f.Severity == "low" || f.Severity == "info" {
			d.Notes++
		} else {
			d.Serious++
		}
	}
	changes, _ := a.repo.Changes(c.SHA)
	for _, ch := range changes {
		fv := fileView{Path: ch.Path, Status: ch.Status, Java: strings.HasSuffix(ch.Path, ".java")}
		placed := map[int64]bool{}
		at := map[int][]store.Finding{}
		for _, f := range byFile[ch.Path] {
			at[f.Line] = append(at[f.Line], f)
			if f.Severity != "low" && f.Severity != "info" {
				fv.Serious++
			}
		}
		oldLine, newLine := 0, 0
		for _, l := range strings.Split(strings.TrimRight(ch.Patch, "\n"), "\n") {
			switch {
			case strings.HasPrefix(l, "@@"):
				if m := hunkHeader.FindStringSubmatch(l); m != nil {
					oldLine, _ = strconv.Atoi(m[1])
					newLine, _ = strconv.Atoi(m[2])
				}
				fv.Rows = append(fv.Rows, diffRow{Kind: "hunk", Text: l})
			case strings.HasPrefix(l, "+"):
				row := diffRow{Kind: "add", New: newLine, Text: l[1:], Findings: at[newLine]}
				for _, f := range at[newLine] {
					placed[f.ID] = true
				}
				fv.Rows = append(fv.Rows, row)
				newLine++
			case strings.HasPrefix(l, "-"):
				fv.Rows = append(fv.Rows, diffRow{Kind: "del", Old: oldLine, Text: l[1:]})
				oldLine++
			case strings.HasPrefix(l, `\`):
			default:
				text := l
				if len(text) > 0 {
					text = text[1:]
				}
				row := diffRow{Kind: "ctx", Old: oldLine, New: newLine, Text: text, Findings: at[newLine]}
				for _, f := range at[newLine] {
					placed[f.ID] = true
				}
				fv.Rows = append(fv.Rows, row)
				oldLine++
				newLine++
			}
		}
		for _, f := range byFile[ch.Path] {
			if !placed[f.ID] {
				fv.Outside = append(fv.Outside, f)
			}
		}
		d.Files = append(d.Files, fv)
	}
	return d
}
