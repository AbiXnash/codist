package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abxengg/codist-review/store"
)

func TestLuaConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codist"), 0o755)
	os.WriteFile(filepath.Join(dir, ".codist", "config.lua"), []byte(`
model = { url = "http://gpu-box:11434/", name = "qwen2.5-coder:7b", context = 4096 }
poll_ms = 1000 * 5
local skip = { "target/", "generated/" }
table.insert(skip, "docs/")
ignore = skip
`), 0o644)
	data := t.TempDir() // the file names a model on another machine, which needs the person's trust
	t.Setenv("CODIST_DATA_DIR", data)
	if err := store.Trust(data, dir); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.ModelURL != "http://gpu-box:11434" || c.Model != "qwen2.5-coder:7b" || c.ModelCtx != 4096 || c.PollMs != 5000 {
		t.Errorf("not read from Lua: %+v", c)
	}
	if !c.Ignored("svc/generated/A.java") || c.Ignored("svc/src/A.java") || len(c.Ignore) != 3 {
		t.Errorf("ignore list: %v", c.Ignore)
	}
}

func TestLuaCannotTouchTheMachine(t *testing.T) {
	for _, src := range []string{`os.execute("true")`, `io.open("/etc/passwd")`, `require("os")`, `dofile("/etc/passwd")`} {
		c := Defaults(".")
		if err := c.applyLua(src); err == nil {
			t.Errorf("%s ran", src)
		}
	}
}

func TestHistory(t *testing.T) {
	if Defaults(".").History != 50 {
		t.Error("the model reads the last 50 commits by default")
	}
	c := Defaults(".")
	if err := c.applyLua("history = 0"); err != nil || c.History != 0 {
		t.Errorf("history = 0 means every commit: %d %v", c.History, err)
	}
	t.Setenv("CODIST_HISTORY", "7")
	c.applyEnv()
	if c.History != 7 {
		t.Errorf("env: %d", c.History)
	}
}

func TestBehaviourSetting(t *testing.T) {
	if Defaults(".").Behaviour {
		t.Error("running the project's code is off unless turned on")
	}
	c := Defaults(".")
	if err := c.applyLua("behaviour = true"); err != nil || !c.Behaviour {
		t.Errorf("lua: %v %v", c.Behaviour, err)
	}
	t.Setenv("CODIST_BEHAVIOUR", "off")
	c.applyEnv()
	if c.Behaviour {
		t.Error("env off")
	}
	t.Setenv("CODIST_BEHAVIOUR", "1")
	c.applyEnv()
	if !c.Behaviour {
		t.Error("env on")
	}
}

// Values that would spin the watcher or silence the model fall back to the defaults, and extra host names come
// from the environment only, never from the repository's own file.
func TestUnusableValuesAndHosts(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codist"), 0o755)
	os.WriteFile(filepath.Join(dir, ".codist", "config.lua"), []byte("poll_ms = 0\nmax_files = -2\nhosts = { 'evil.example' }\n"), 0o644)
	t.Setenv("CODIST_HOSTS", "Codist.internal, box")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.PollMs != 3000 || c.MaxFiles != 8 {
		t.Errorf("poll %d, files %d", c.PollMs, c.MaxFiles)
	}
	if len(c.Hosts) != 2 || c.Hosts[0] != "codist.internal" || c.Hosts[1] != "box" {
		t.Errorf("hosts %q", c.Hosts)
	}
}

// What the repository's file may not decide alone: running its code, and a model on another machine. The
// environment, which is the person's, still can.
func TestRepositoryFileNeedsTrust(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	t.Setenv("CODIST_DATA_DIR", data)
	os.MkdirAll(filepath.Join(dir, ".codist"), 0o755)
	write := func(s string) { os.WriteFile(filepath.Join(dir, ".codist", "config.lua"), []byte(s), 0o644) }
	write("behaviour = true\nmodel = { url = 'http://models.example:11434', name = 'm' }\nhistory = 7\n")
	c, _ := Load(dir)
	if c.Behaviour || c.ModelURL != "http://localhost:11434" || !c.Held || c.Model != "m" || c.History != 7 {
		t.Errorf("untrusted: %+v", c)
	}
	write("model = { url = 'http://127.0.0.1:9999' }\n") // this machine: nothing to agree to
	if c, _ := Load(dir); c.Held || c.ModelURL != "http://127.0.0.1:9999" {
		t.Errorf("a local model: %+v", c)
	}
	write("history = 3\n")
	t.Setenv("CODIST_BEHAVIOUR", "1")
	t.Setenv("CODIST_MODEL_URL", "http://gpu-box:11434")
	if c, _ := Load(dir); c.Held || !c.Behaviour || c.ModelURL != "http://gpu-box:11434" {
		t.Errorf("the environment decides: %+v", c)
	}
	for host, want := range map[string]bool{"localhost": true, "LOCALHOST:8080": true, "app.localhost": true, "127.0.0.1:1": true, "[::1]:8080": true,
		"http://localhost:5173": true, "box": true, "evil.example": false, "http://evil.example": false, "null": false, "": false, "localhost.evil.example": false} {
		if got := Local(host, []string{"box"}); got != want {
			t.Errorf("Local(%q) = %v", host, got)
		}
	}
}
