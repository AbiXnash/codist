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
