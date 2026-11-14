package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abxengg/codist-git/testrepo"
	"github.com/abxengg/codist/internal/config"
)

// TestMain lets a test run the program itself: CODIST_RUN_MAIN=1 runs main with the given arguments.
func TestMain(m *testing.M) {
	if os.Getenv("CODIST_RUN_MAIN") == "1" {
		os.Args = append([]string{"codist"}, strings.Fields(os.Getenv("CODIST_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func run(t *testing.T, args string, env ...string) (string, int) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), append([]string{"CODIST_RUN_MAIN=1", "CODIST_ARGS=" + args}, env...)...)
	out, _ := cmd.CombinedOutput()
	return string(out), cmd.ProcessState.ExitCode()
}

func TestHelp(t *testing.T) {
	out, code := run(t, "help")
	if code != 0 || !strings.Contains(out, "codist doctor") {
		t.Errorf("%d %s", code, out)
	}
}

func TestOutsideARepository(t *testing.T) {
	out, code := run(t, t.TempDir())
	if code != 1 || !strings.Contains(out, "not inside a git repository") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorCommand(t *testing.T) {
	tr := testrepo.New(t)
	os.MkdirAll(tr.Dir+"/.codist", 0o755)
	os.WriteFile(tr.Dir+"/.codist/config.lua", []byte(`model = { url = "http://127.0.0.1:1" }`), 0o644)
	out, code := run(t, "doctor "+tr.Dir)
	// A missing model is reported, not an error: the code checks run without one. The file names a model on this
	// machine, so nothing waits for "codist trust".
	if code != 0 || !strings.Contains(out, "missing  model") || !strings.Contains(out, "Settings from") || strings.Contains(out, "codist trust") {
		t.Errorf("%d %s", code, out)
	}
	os.WriteFile(tr.Dir+"/.codist/config.lua", []byte(`model = {`), 0o644)
	if out, code := run(t, tr.Dir); code != 1 || !strings.Contains(out, "config.lua") {
		t.Errorf("a broken config: %d %s", code, out)
	}
}

func TestDoctor(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"models":[{"name":"m"}]}`)
	}))
	defer s.Close()
	cfg := config.Defaults(t.TempDir())
	cfg.ModelURL, cfg.Model = s.URL, "m"
	runDoctor(cfg) // all there
	cfg.Model = "other"
	runDoctor(cfg) // the model is missing, which is said and is not an error
}

// A repository's own file cannot turn on running its code, or name a model on another machine, until the person
// runs "codist trust" there.
func TestTrustCommand(t *testing.T) {
	tr := testrepo.New(t)
	data := "CODIST_DATA_DIR=" + t.TempDir()
	os.MkdirAll(tr.Dir+"/.codist", 0o755)
	os.WriteFile(tr.Dir+"/.codist/config.lua", []byte(`behaviour = true
model = { url = "http://models.example:11434" }`), 0o644)
	out, code := run(t, "doctor "+tr.Dir, data)
	if code != 0 || !strings.Contains(out, "codist trust") || !strings.Contains(out, "http://localhost:11434") {
		t.Errorf("before trust: %d %s", code, out)
	}
	if out, code := run(t, "trust "+tr.Dir, data); code != 0 || !strings.Contains(out, "every setting") {
		t.Errorf("trust: %d %s", code, out)
	}
	out, code = run(t, "doctor "+tr.Dir, data)
	if code != 0 || strings.Contains(out, "codist trust") || !strings.Contains(out, "http://models.example:11434") {
		t.Errorf("after trust: %d %s", code, out)
	}
}

// The server starts, answers, and stops cleanly on an interrupt.
func TestServe(t *testing.T) {
	tr := testrepo.New(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CODIST_RUN_MAIN=1", "CODIST_ARGS="+tr.Dir+" --no-open", "PORT=0", "CODIST_DATA_DIR="+t.TempDir(),
		"CODIST_MODEL_URL=http://127.0.0.1:1")
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(out.String(), "codist ready") })
	done := make(chan error, 1)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		// Windows cannot send an interrupt to another process. The server started and is ready, so stop it.
		cmd.Process.Kill()
		cmd.Wait()
		t.Skipf("interrupt not supported here (%v); checked that the server starts", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit: %v\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("did not stop within 30s of an interrupt\n%s", out.String())
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
