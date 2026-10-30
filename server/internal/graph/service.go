// Package graph holds the current code graph and rebuilds it when the code changes.
package graph

import (
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/javaidx"
)

type Service struct {
	repo    *gitx.Repo
	hub     *events.Hub
	current atomic.Pointer[javaidx.Graph]
	mu      sync.Mutex
}

func New(repo *gitx.Repo, hub *events.Hub) *Service {
	s := &Service{repo: repo, hub: hub}
	s.current.Store(&javaidx.Graph{Nodes: []javaidx.Node{}, Edges: []javaidx.Edge{}, Source: "none"})
	return s
}

func (s *Service) Graph() *javaidx.Graph { return s.current.Load() }

// Reindex rebuilds the graph. Local mode reads the working tree so the graph follows edits
// before they are committed; remote mode reads the tracked commit.
func (s *Service) Reindex() {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := time.Now()
	var sources map[string][]byte
	label := "working tree"
	if s.repo.Remote {
		head := s.repo.Head()
		sources = s.repo.JavaFilesAt(head)
		label = "empty"
		if head != "" {
			label = head[:7]
		}
	} else {
		sources = s.repo.JavaFilesWorking()
	}
	g := javaidx.Index(sources, label)
	s.current.Store(&g)
	slog.Info("indexed", "types", len(g.Nodes), "links", len(g.Edges), "files", len(sources),
		"parseErrors", g.ParseErrors, "took", time.Since(start).Round(time.Millisecond))
	s.hub.Publish("graph", map[string]any{"nodes": len(g.Nodes), "edges": len(g.Edges), "builtAt": g.BuiltAt})
}

// Dependents are the types that link to any of ids (one hop, reverse edges), excluding ids.
func (s *Service) Dependents(ids map[string]bool) []string {
	out := map[string]bool{}
	for _, e := range s.Graph().Edges {
		if ids[e.Target] && !ids[e.Source] {
			out[e.Source] = true
		}
	}
	return sortedKeys(out)
}

// Neighbours are the types id links to or that link to it.
func (s *Service) Neighbours(id string) []javaidx.Node {
	g := s.Graph()
	ids := map[string]bool{}
	for _, e := range g.Edges {
		if e.Source == id {
			ids[e.Target] = true
		}
		if e.Target == id {
			ids[e.Source] = true
		}
	}
	var out []javaidx.Node
	for _, n := range g.Nodes {
		if ids[n.ID] {
			out = append(out, n)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
