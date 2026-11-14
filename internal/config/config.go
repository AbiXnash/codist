// Package config reads Codist's settings: defaults, then the repository's .codist/config.lua,
// then environment variables. Lua is used because a team can compute settings (per-module
// limits, ignore lists) instead of copying values around, and gopher-lua is pure Go.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Config is everything Codist can be told.
type Config struct {
	Repo    string   // repository root
	DataDir string   // where Codist keeps its database
	Addr    string   // listen address, 127.0.0.1 only by default: there is no login
	Hosts   []string // host names besides this machine's that pages may reach Codist by (CODIST_HOSTS); never from the repository

	ModelURL  string // the model server the user runs, e.g. Ollama
	Model     string // model name on that server
	ModelCtx  int    // context window asked for
	ModelOff  bool   // review with rules only
	PollMs    int    // how often to look for new commits
	MaxFiles  int    // files per commit the model reviews; rules see all of them
	History   int    // how many recent commits the model reads in the background; 0 means all. Code checks cover every commit
	Behaviour bool   // run the changed methods before and after each recent commit; off unless turned on, since it runs the project's code
	Ignore    []string
	OpenPage  bool
	FromLua   bool // a config.lua was found and read
	LuaSource string
}

// Defaults are what Codist uses with no configuration at all.
func Defaults(repo string) Config {
	home, _ := os.UserHomeDir()
	return Config{
		Repo:     repo,
		DataDir:  filepath.Join(home, ".codist"),
		Addr:     "127.0.0.1:8080",
		ModelURL: "http://localhost:11434",
		Model:    "qwen2.5-coder:14b",
		ModelCtx: 8192,
		PollMs:   3000,
		MaxFiles: 8,
		History:  50, // a laptop model takes minutes per commit, so a long history would take days
		Ignore:   []string{"target/", "build/", "node_modules/", ".idea/"},
		OpenPage: true,
	}
}

// Load builds the configuration for repo.
func Load(repo string) (Config, error) {
	c := Defaults(repo)
	path := filepath.Join(repo, ".codist", "config.lua")
	if src, err := os.ReadFile(path); err == nil {
		if err := c.applyLua(string(src)); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
		c.FromLua, c.LuaSource = true, path
	}
	c.applyEnv()
	return c, nil
}

// applyLua runs the script in a sandbox with no file, OS or module access, then reads the globals
// it set. A script is configuration, so it must not be able to touch the machine.
func (c *Config) applyLua(src string) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.TabLibName, lua.OpenTable}, {lua.StringLibName, lua.OpenString}, {lua.MathLibName, lua.OpenMath}} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	for _, unsafe := range []string{"dofile", "loadfile", "load", "loadstring", "require", "collectgarbage"} {
		L.SetGlobal(unsafe, lua.LNil)
	}
	if err := L.DoString(src); err != nil {
		return err
	}
	str := func(name string, into *string) {
		if v, ok := L.GetGlobal(name).(lua.LString); ok {
			*into = string(v)
		}
	}
	num := func(name string, into *int) {
		if v, ok := L.GetGlobal(name).(lua.LNumber); ok {
			*into = int(v)
		}
	}
	if m, ok := L.GetGlobal("model").(*lua.LTable); ok {
		if v, ok := m.RawGetString("url").(lua.LString); ok {
			c.ModelURL = string(v)
		}
		if v, ok := m.RawGetString("name").(lua.LString); ok {
			c.Model = string(v)
		}
		if v, ok := m.RawGetString("context").(lua.LNumber); ok {
			c.ModelCtx = int(v)
		}
		if v, ok := m.RawGetString("enabled").(lua.LBool); ok {
			c.ModelOff = !bool(v)
		}
	}
	str("addr", &c.Addr)
	num("poll_ms", &c.PollMs)
	num("max_files", &c.MaxFiles)
	num("history", &c.History)
	if v, ok := L.GetGlobal("behaviour").(lua.LBool); ok {
		c.Behaviour = bool(v)
	}
	if t, ok := L.GetGlobal("ignore").(*lua.LTable); ok {
		c.Ignore = nil
		t.ForEach(func(_, v lua.LValue) {
			if s, ok := v.(lua.LString); ok {
				c.Ignore = append(c.Ignore, string(s))
			}
		})
	}
	return nil
}

// applyEnv lets a single run override the files without editing them.
func (c *Config) applyEnv() {
	set := func(key string, into *string) {
		if v := os.Getenv(key); v != "" {
			*into = v
		}
	}
	set("CODIST_ADDR", &c.Addr)
	set("CODIST_DATA_DIR", &c.DataDir)
	set("CODIST_MODEL_URL", &c.ModelURL)
	set("CODIST_MODEL", &c.Model)
	if v := os.Getenv("PORT"); v != "" {
		c.Addr = "127.0.0.1:" + v
	}
	if v, err := strconv.Atoi(os.Getenv("CODIST_HISTORY")); err == nil && v >= 0 {
		c.History = v
	}
	if v, err := strconv.Atoi(os.Getenv("CODIST_POLL_MS")); err == nil && v > 0 {
		c.PollMs = v
	}
	if v := strings.ToLower(os.Getenv("CODIST_MODEL_REVIEW")); v == "false" || v == "0" || v == "off" {
		c.ModelOff = true
	}
	switch strings.ToLower(os.Getenv("CODIST_BEHAVIOUR")) {
	case "1", "true", "on":
		c.Behaviour = true
	case "0", "false", "off":
		c.Behaviour = false
	}
	if os.Getenv("CODIST_NO_BROWSER") != "" {
		c.OpenPage = false
	}
	if v := os.Getenv("CODIST_HOSTS"); v != "" {
		c.Hosts = strings.Split(strings.ToLower(strings.ReplaceAll(v, " ", "")), ",")
	}
	if c.PollMs < 1 { // zero would spin the watcher
		c.PollMs = 3000
	}
	if c.MaxFiles < 1 { // zero would have the model read nothing, silently
		c.MaxFiles = 8
	}
	c.ModelURL = strings.TrimRight(c.ModelURL, "/")
}

// Ignored says whether a repository path is excluded.
func (c Config) Ignored(path string) bool {
	for _, p := range c.Ignore {
		if strings.HasPrefix(path, p) || strings.Contains(path, "/"+p) {
			return true
		}
	}
	return false
}
