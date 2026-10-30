// Command codist watches a Java / Spring Boot git repository, keeps a live graph of its
// types and reviews every new commit with deterministic checks and a local LLM (Ollama).
package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codist/internal/api"
	"codist/internal/config"
	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/incident"
	"codist/internal/javaidx"
	"codist/internal/knowledge"
	"codist/internal/review"
	"codist/internal/watch"
	"codist/prompts"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := config.Load()

	prompts.Configure(filepath.Join(cfg.DataDir, "prompts"), "")
	if len(os.Args) > 1 && os.Args[1] == "export-training" {
		exportTraining(cfg)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "export-prompts" {
		// codist export-prompts [dir]: copies the built-in prompts out for editing (default <data-dir>/prompts).
		dir := filepath.Join(cfg.DataDir, "prompts")
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		n, err := prompts.Export(dir)
		if err != nil {
			slog.Error("export failed", "err", err)
			os.Exit(1)
		}
		slog.Info("exported prompts; edit them there, Codist picks up changes without a restart", "files", n, "dir", dir)
		return
	}

	repo, err := gitx.Open(cfg)
	if err != nil {
		slog.Error("cannot open repository", "err", err)
		os.Exit(1)
	}
	slog.Info("watching", "repo", repo.Root, "tracking", repo.Tracking())
	prompts.Configure(filepath.Join(cfg.DataDir, "prompts"), filepath.Join(repo.Root, ".codist", "prompts"))

	hub := events.NewHub()
	g := graph.New(repo, hub)
	store, err := review.NewStore(cfg.DataDir, repo.Name())
	if err != nil {
		slog.Error("cannot open review store", "err", err)
		os.Exit(1)
	}
	llm := review.NewOllama(cfg.OllamaURL, cfg.Model, cfg.NumCtx, cfg.LLMTimeout)
	analyzer := &review.Analyzer{Repo: repo, Graph: g}
	reviews := review.NewService(cfg, repo, g, analyzer, llm, store, hub)
	reviews.Ignores = review.NewIgnores(cfg.DataDir, repo.Name(), repo.Root)
	fixLLM, askLLM := llm, llm
	if cfg.FixModel != "" {
		fixLLM = review.NewOllama(cfg.OllamaURL, cfg.FixModel, cfg.NumCtx, cfg.LLMTimeout)
	}
	if cfg.AskModel != "" {
		askLLM = review.NewOllama(cfg.OllamaURL, cfg.AskModel, cfg.NumCtx, cfg.LLMTimeout)
	}
	watcher := watch.New(cfg, repo, g, reviews, hub)
	watcher.Start()

	srv := &api.Server{Cfg: cfg, Repo: repo, Graph: g, Analyzer: analyzer, Reviews: reviews,
		Store: store, LLM: llm, Hub: hub, Watcher: watcher, Fixer: review.NewFixer(repo, fixLLM, askLLM, store)}
	srv.Fixer.Ignores = reviews.Ignores
	srv.Tracker = review.NewTracker(repo, store, reviews.Ignores)
	rules := func() (javaidx.Context, *knowledge.Base, bool) {
		gr := g.Graph()
		if gr == nil {
			return javaidx.Context{}, nil, false
		}
		return javaidx.ProjectContext(gr), reviews.Knowledge(), true
	}
	srv.Tracker.Rules = rules
	srv.Fixer.Rules = rules
	srv.Fixer.Busy = reviews.Busy
	srv.Fixer.Known = func() map[string]bool {
		out := map[string]bool{}
		for _, n := range g.Graph().Nodes {
			out[n.ID] = true
		}
		return out
	}
	srv.Fixer.OnReady = func(sha, key string) { hub.Publish("fix-ready", map[string]string{"sha": sha, "key": key}) }
	reviews.OnDone = srv.Fixer.Prefetch
	srv.SignOffs = review.NewSignOffs(cfg.DataDir, repo.Name())
	srv.Incidents = incident.NewEngine(repo, g, askLLM, srv.Tracker, reviews, incident.NewStore(cfg.DataDir, repo.Name()), hub)
	httpServer := &http.Server{Addr: cfg.Addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	if p := llm.Problem(); p != "" {
		slog.Warn(p)
	}
	slog.Info("codist ready", "url", "http://localhost"+cfg.Addr, "model", cfg.Model)
	if err := httpServer.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// exportTraining writes the knowledge base as chat-format JSONL for LoRA fine-tuning:
//
//	codist export-training [file.jsonl] [extra rule dirs...]
func exportTraining(cfg config.Config) {
	out := os.Stdout
	args := os.Args[2:]
	if len(args) > 0 && strings.HasSuffix(args[0], ".jsonl") {
		f, err := os.Create(args[0])
		if err != nil {
			slog.Error("cannot create file", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		out, args = f, args[1:]
	}
	dirs := append([]string{filepath.Join(cfg.DataDir, "rules")}, args...)
	if cfg.RulesDir != "" {
		dirs = append(dirs, cfg.RulesDir)
	}
	kb, err := knowledge.Load(dirs...)
	if err != nil {
		slog.Error("cannot load rules", "err", err)
		os.Exit(1)
	}
	n, err := kb.ExportTraining(out, review.FilePrompt())
	if err != nil {
		slog.Error("export failed", "err", err)
		os.Exit(1)
	}
	slog.Info("exported training examples", "examples", n, "rules", len(kb.Rules))
}
