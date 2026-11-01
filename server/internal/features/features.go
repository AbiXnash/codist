// Package features holds the on/off switches for menus and features. They come from JSON files, read
// on every request, so editing a file and refreshing the page is enough.
package features

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Flags groups the switches. Anything a file leaves out stays on.
type Flags struct {
	Menus    map[string]bool `json:"menus"`
	Panels   map[string]bool `json:"panels"`
	Features map[string]bool `json:"features"`
	// Problems explains files that could not be used and names that mean nothing, so a typo is visible.
	Problems []string `json:"problems,omitempty"`
	Sources  []string `json:"sources"`
}

// Defaults is every switch that exists.
func Defaults() Flags {
	on := func(names ...string) map[string]bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	return Flags{
		Menus:    on("review", "repo", "branches", "mine", "team", "release", "incidents", "activity", "memory"),
		Panels:   on("issues", "commit", "code", "ask"),
		Features: on("explorer", "branchGraph", "branchSwitcher", "fix", "reword", "localPlan", "issueWorkflow"),
		Sources:  []string{},
	}
}

// Paths are the files in the order they apply; a later file wins.
func Paths(dataDir, repoRoot, explicit string) []string {
	paths := []string{filepath.Join(dataDir, "features.json"), filepath.Join(repoRoot, ".codist", "features.json")}
	if explicit != "" {
		paths = append(paths, explicit)
	}
	return paths
}

// Load reads the files. A broken file is reported and skipped: it must never stop the app.
func Load(paths ...string) Flags {
	f := Defaults()
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var in struct {
			Menus    map[string]bool `json:"menus"`
			Panels   map[string]bool `json:"panels"`
			Features map[string]bool `json:"features"`
		}
		if err := json.Unmarshal(b, &in); err != nil {
			f.Problems = append(f.Problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		f.Sources = append(f.Sources, p)
		f.Problems = append(f.Problems, merge(f.Menus, in.Menus, p, "menus")...)
		f.Problems = append(f.Problems, merge(f.Panels, in.Panels, p, "panels")...)
		f.Problems = append(f.Problems, merge(f.Features, in.Features, p, "features")...)
	}
	return f
}

func merge(into, from map[string]bool, path, group string) []string {
	var problems []string
	for k, v := range from {
		if _, known := into[k]; !known {
			problems = append(problems, fmt.Sprintf("%s: %s.%s is not a switch", path, group, k))
			continue
		}
		into[k] = v
	}
	sort.Strings(problems)
	return problems
}
