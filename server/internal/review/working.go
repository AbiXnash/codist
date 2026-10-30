package review

import (
	"strings"

	"codist/internal/gitx"
)

// WorkingSHA stands for the uncommitted changes in the working tree wherever a commit is expected.
const WorkingSHA = "working"

// Working is the instant review of uncommitted changes: the rules and knowledge base run on every
// changed Java file and report what touches the lines you changed. No model is involved, so it
// keeps up as you type.
type Working struct {
	Files    []FileReview      `json:"files"`
	Changes  []gitx.FileChange `json:"changes"`
	Findings int               `json:"findings"`
}

func WorkingReview(repo *gitx.Repo, rules RuleContext, ignores *Ignores) Working {
	out := Working{Files: []FileReview{}, Changes: []gitx.FileChange{}}
	if repo.Remote || rules == nil {
		return out
	}
	changes, err := repo.WorkingChanges()
	if err != nil {
		return out
	}
	ctx, kb, ok := rules()
	if !ok {
		return out
	}
	r := Review{SHA: WorkingSHA, Status: StatusDone}
	for _, c := range changes {
		c.Patch = "" // the UI asks for diffs separately
		out.Changes = append(out.Changes, c)
		if c.Deleted() || !c.IsJava() {
			continue
		}
		src, exists, err := repo.ReadWorking(c.NewPath)
		if err != nil || !exists {
			continue
		}
		changed := map[int]bool{}
		for _, l := range c.AddedLines {
			changed[l] = true
		}
		fr := FileReview{Path: c.NewPath, Findings: []Finding{}}
		for _, f := range RuleScan(c.NewPath, []byte(src), ctx, kb) {
			for l := f.Line; l <= max(f.Line, f.EndLine); l++ {
				if changed[l] {
					f.OnChangedLine = true
					fr.Findings = append(fr.Findings, f)
					break
				}
			}
		}
		if len(fr.Findings) > 0 {
			r.Files = append(r.Files, fr)
		}
	}
	r = AssignGroups(ignores.Decorate(r))
	for _, fr := range r.Files {
		for _, f := range fr.Findings {
			if !f.Ignored {
				out.Findings++
			}
		}
	}
	out.Files = r.Files
	return out
}

// workingReview adapts the working-tree review for the fix planner.
func (x *Fixer) workingReview() Review {
	w := WorkingReview(x.repo, x.Rules, x.Ignores)
	return Review{SHA: WorkingSHA, Status: StatusDone, Files: w.Files}
}

// notFixable are findings that ask you to check something rather than change it: a "fix" would only
// undo your change (and the next review would report the undo).
var notFixable = map[string]string{
	"shared-constant-changed": "this is a heads-up to check the classes that read the constant, not a defect; mark it as checked instead",
}

func isWorking(sha string) bool { return strings.EqualFold(sha, WorkingSHA) }
