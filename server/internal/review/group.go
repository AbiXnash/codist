package review

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// symbolRef matches Type.CONSTANT references such as DataConstant.LOG_PREFIX in titles and code.
var symbolRef = regexp.MustCompile(`\b([A-Z][A-Za-z0-9]*)\.([A-Z][A-Z0-9_]{1,})\b`)

type located struct {
	file, idx int
	f         Finding
	path      string
}

// AssignGroups links findings that are one problem seen in several places, so they are shown,
// counted and fixed once:
//   - the same rule (or the same model title) anywhere in the commit;
//   - a finding that names Type.CONSTANT, together with findings in Type.java about that constant
//     or about the type itself (the declaration is the root cause).
//
// Ignored findings are left out. The root is the declaration when there is one, else the most severe.
func AssignGroups(r Review) Review {
	var all []located
	for i, fr := range r.Files {
		for j, f := range fr.Findings {
			if !f.Ignored {
				all = append(all, located{file: i, idx: j, f: f, path: fr.Path})
			}
		}
	}
	if len(all) < 2 {
		return r
	}

	parent := make([]int, len(all))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) { parent[find(a)] = find(b) }

	// Same rule or same model title.
	byKey := map[string]int{}
	for i, l := range all {
		if first, ok := byKey[l.f.Key()]; ok {
			union(i, first)
		} else {
			byKey[l.f.Key()] = i
		}
	}

	// Symbol links: a reference to Type.CONST joins findings in Type.java that mention CONST or Type.
	declaresRoot := map[int]bool{}
	for i, l := range all {
		// Only what the finding is about (title, explanation), not every symbol on its line: a
		// logging finding on the same line must not join the constant's group.
		for _, m := range symbolRef.FindAllStringSubmatch(l.f.Title+" "+l.f.Detail, -1) {
			typ, constant := m[1], m[2]
			for j, other := range all {
				if j == i || strings.TrimSuffix(filepath.Base(other.path), ".java") != typ {
					continue
				}
				text := other.f.Title + " " + other.f.Snippet + " " + other.f.Detail
				if strings.Contains(text, constant) || strings.Contains(text, typ) {
					union(i, j)
					declaresRoot[j] = true
				}
			}
		}
	}

	groups := map[int][]int{}
	for i := range all {
		groups[find(i)] = append(groups[find(i)], i)
	}
	sevRank := func(s string) int { return slices.Index(severities, s) }
	n := 0
	roots := make([]int, 0, len(groups))
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Ints(roots)
	for _, root := range roots {
		members := groups[root]
		if len(members) < 2 {
			continue
		}
		sort.SliceStable(members, func(a, b int) bool {
			ma, mb := all[members[a]], all[members[b]]
			if declaresRoot[members[a]] != declaresRoot[members[b]] {
				return declaresRoot[members[a]]
			}
			return sevRank(ma.f.Severity) < sevRank(mb.f.Severity)
		})
		n++
		id := fmt.Sprintf("g%d", n)
		for k, m := range members {
			f := &r.Files[all[m].file].Findings[all[m].idx]
			f.Group, f.GroupRoot = id, k == 0
		}
	}
	return r
}
