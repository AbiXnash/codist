// Package config reads Codist settings from environment variables.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RepoPath      string // local checkout to watch
	RepoURL       string // or a git URL to clone and follow
	Branch        string // empty = current branch
	FetchRemote   bool   // follow origin/<branch> instead of the local branch
	FetchInterval time.Duration
	Poll          time.Duration
	DataDir       string
	GitHubToken   string
	WebhookSecret string
	RulesDir      string // extra knowledge-base folder

	OllamaURL    string
	Model        string
	SummaryModel string // optional smaller model for the commit summary
	FixModel     string // optional (bigger) model for fixes and edits
	AskModel     string // optional model for the Ask tab
	NumCtx       int
	LLMTimeout   time.Duration

	AutoReview   bool
	MaxFiles     int
	MaxFileLines int

	Addr   string
	WebDir string
}

func Load() Config {
	home, _ := os.UserHomeDir()
	return Config{
		RepoPath:      expandHome(os.Getenv("CODIST_REPO"), home),
		RepoURL:       os.Getenv("CODIST_REPO_URL"),
		Branch:        os.Getenv("CODIST_BRANCH"),
		FetchRemote:   envBool("CODIST_FETCH_REMOTE", false),
		FetchInterval: 30 * time.Second,
		Poll:          time.Duration(envInt("CODIST_POLL_MS", 3000)) * time.Millisecond,
		DataDir:       expandHome(env("CODIST_DATA_DIR", filepath.Join(home, ".codist")), home),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		WebhookSecret: os.Getenv("CODIST_WEBHOOK_SECRET"),
		RulesDir:      expandHome(os.Getenv("CODIST_RULES_DIR"), home),

		OllamaURL:    strings.TrimRight(env("OLLAMA_URL", "http://localhost:11434"), "/"),
		Model:        env("CODIST_MODEL", "qwen2.5-coder:7b"),
		SummaryModel: os.Getenv("CODIST_SUMMARY_MODEL"),
		FixModel:     os.Getenv("CODIST_FIX_MODEL"),
		AskModel:     os.Getenv("CODIST_ASK_MODEL"),
		NumCtx:       envInt("CODIST_NUM_CTX", 16384),
		LLMTimeout:   10 * time.Minute,

		AutoReview:   envBool("CODIST_AUTO_REVIEW", true),
		MaxFiles:     envInt("CODIST_MAX_FILES", 8),
		MaxFileLines: 600,

		Addr:   env("CODIST_ADDR", ":"+env("PORT", "8080")),
		WebDir: env("CODIST_WEB_DIR", "../web/dist"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
