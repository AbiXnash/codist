package review

import (
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/javaidx"
)

type Analyzer struct {
	Repo  *gitx.Repo
	Graph *graph.Service
}

// Analyze maps a commit's diff onto types. Each changed file is parsed as it was at that
// commit, so line numbers match the diff even when the working tree has moved on.
func (a *Analyzer) Analyze(sha string) (Analysis, error) {
	c, err := a.Repo.Commit(sha)
	if err != nil {
		return Analysis{}, err
	}
	files, err := a.Repo.Diff(c)
	if err != nil {
		return Analysis{}, err
	}
	changed := []ChangedType{}
	ids := map[string]bool{}

	for _, f := range files {
		if !f.IsJava() {
			continue
		}
		if f.Deleted() {
			if len(c.Parents) == 0 {
				continue
			}
			if src, ok := a.Repo.ReadFile(c.Parents[0], f.OldPath); ok {
				for _, t := range javaidx.TypesIn([]byte(src), f.OldPath) {
					changed = append(changed, ChangedType{ID: t.ID, File: f.OldPath, Status: "deleted", Members: []string{}})
					ids[t.ID] = true
				}
			}
			continue
		}
		src, ok := a.Repo.ReadFile(c.SHA, f.NewPath)
		if !ok {
			continue
		}
		added := f.ChangeType == "ADD"
		touched := append(append([]int{}, f.AddedLines...), f.DeletedAt...)
		within := func(start, end int) bool {
			for _, l := range touched {
				if l >= start && l <= end {
					return true
				}
			}
			return false
		}
		for _, t := range javaidx.TypesIn([]byte(src), f.NewPath) {
			if !added && !within(t.StartLine, t.EndLine) {
				continue
			}
			members := []string{}
			seen := map[string]bool{}
			for _, m := range t.Members {
				if (added || within(m.StartLine, m.EndLine)) && !seen[m.Name] {
					members = append(members, m.Name)
					seen[m.Name] = true
				}
			}
			status := "modified"
			if added {
				status = "added"
			}
			changed = append(changed, ChangedType{ID: t.ID, File: f.NewPath, Status: status, Members: members})
			ids[t.ID] = true
		}
	}
	return Analysis{Commit: c, Files: files, Changed: changed, Impacted: a.Graph.Dependents(ids)}, nil
}
