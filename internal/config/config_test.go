package config

import (
	"os"
	"path/filepath"
	"testing"
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
