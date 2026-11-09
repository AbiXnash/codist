// Codist reviews a Java repository on your own machine: it reads every commit, draws the code as a
// graph, explains production errors and describes the repository. It needs git, and Java only for
// the checks that run code. The model runs on a server you start yourself.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/AbiXnash/codist-review/activity"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/AbiXnash/codist-git"
	"github.com/AbiXnash/codist-model"
	"github.com/AbiXnash/codist/internal/app"
	"github.com/AbiXnash/codist/internal/config"
)

func main() {
	var level slog.Level // info unless CODIST_LOG is debug, warn or error; CODIST_LOG_JSON=1 writes JSON lines for log tools
	_ = level.UnmarshalText([]byte(cmp.Or(os.Getenv("CODIST_LOG"), "info")))
	var h slog.Handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	if os.Getenv("CODIST_LOG_JSON") != "" {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(activity.Default.Wrap(h))) // also kept for the Activity page
	activity.Watch()
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Println("codist [path]    review the repository at path (default: here)\ncodist doctor    show what is installed and what is missing")
		return
	}
	doctor := len(args) > 0 && args[0] == "doctor"
	if doctor {
		args = args[1:]
	}
	dir := "."
	for _, a := range args {
		if a != "--no-open" {
			dir = a
		}
	}
	if !git.Available() {
		fail("git is not installed. Install git and run codist again.")
	}
	repo, err := git.Open(dir)
	if err != nil {
		fail(err.Error())
	}
	cfg, err := config.Load(repo.Root)
	if err != nil {
		fail(err.Error())
	}
	for _, a := range args {
		if a == "--no-open" {
			cfg.OpenPage = false
		}
	}
	if doctor {
		os.Exit(runDoctor(cfg))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := app.New(ctx, cfg, repo)
	if err != nil {
		fail(err.Error())
	}
	defer a.Close()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		fail(fmt.Sprintf("cannot listen on %s: %v. Set PORT to use another port.", cfg.Addr, err))
	}
	srv := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	url := "http://" + ln.Addr().String()
	slog.Info("codist ready", "url", url, "repo", repo.Root, "model", cfg.Model)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
		}
	}()
	if cfg.OpenPage {
		openBrowser(url)
	}
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	wg.Wait()
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "codist:", msg)
	os.Exit(1)
}

func openBrowser(url string) {
	cmd := map[string]string{"darwin": "open", "windows": "rundll32", "linux": "xdg-open"}[runtime.GOOS]
	if cmd == "" {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command(cmd, "url.dll,FileProtocolHandler", url).Start()
		return
	}
	_ = exec.Command(cmd, url).Start()
}

// runDoctor prints what Codist found, each with what to do about it, and returns 1 if something
// required is missing.
func runDoctor(cfg config.Config) int {
	code := 0
	line := func(ok bool, what, note string) {
		mark := "ok     "
		if !ok {
			mark = "missing"
		}
		fmt.Printf("  %s  %-12s %s\n", mark, what, note)
	}
	fmt.Println("Required")
	line(git.Available(), "git", "")
	fmt.Println("Model (you run the server; Codist connects to it)")
	ok, note := model.New(cfg.ModelURL, cfg.Model, cfg.ModelCtx).Status(context.Background())
	line(ok, "model", cfg.Model+" at "+cfg.ModelURL+": "+note)
	if !ok && !cfg.ModelOff {
		code = 1
	}
	fmt.Println("Optional")
	_, javaErr := exec.LookPath("java")
	line(javaErr == nil, "java", "runs checks that execute code")
	if cfg.FromLua {
		fmt.Println("Settings from " + filepath.ToSlash(cfg.LuaSource))
	}
	return code
}
