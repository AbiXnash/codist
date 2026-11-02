package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"codist/internal/config"
)

const usage = `Codist: a local code reviewer for Java and Spring Boot.

  codist [path]              review the git repository at path (default: this folder)
  codist setup               get the local model ready
  codist doctor              check what is installed and what is missing
  codist export-prompts      copy the prompts out for editing
  codist export-training f   write training examples to a .jsonl file
  codist help                show this

Flags: --no-open (do not open the browser). Everything else: https://github.com/AbiXnash/codist/tree/master/docs/wiki
`

// repoFromArgs finds the repository to review: an argument, CODIST_REPO, or the current folder.
func repoFromArgs(cfg *config.Config, args []string) error {
	path := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			path = a
		}
	}
	if path == "" && (cfg.RepoPath != "" || cfg.RepoURL != "") {
		return nil
	}
	if path == "" {
		path = "."
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		abs, _ := filepath.Abs(path)
		return fmt.Errorf("%s is not a git repository. Run codist inside one, or: codist /path/to/repo", abs)
	}
	cfg.RepoPath = strings.TrimSpace(string(out))
	return nil
}

// listen takes the configured port, or the next free one unless a port was asked for by name.
func listen(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err == nil || os.Getenv("PORT") != "" || os.Getenv("CODIST_ADDR") != "" {
		return l, err
	}
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err
	}
	first, _ := strconv.Atoi(port)
	for p := first + 1; p <= first+20; p++ {
		if next, e := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p))); e == nil {
			return next, nil
		}
	}
	return nil, err
}

// openBrowser opens the UI when a person started Codist from a terminal.
func openBrowser(url string, args []string) {
	for _, a := range args {
		if a == "--no-open" {
			return
		}
	}
	if os.Getenv("CODIST_NO_BROWSER") != "" {
		return
	}
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return
	}
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	_ = exec.Command(cmd, url).Start()
}

// memoryGB is the machine's RAM, or 0 when it cannot be read.
func memoryGB() int {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		return int(n >> 30)
	}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	kb, _ := strconv.ParseInt(f[1], 10, 64)
	return int(kb >> 20)
}

// recommendedModel fits the machine: the 14B model needs about 10 GB free while it runs, and 16 GB is
// its practical minimum. Smaller machines get smaller models.
func recommendedModel(cfg config.Config) string {
	if os.Getenv("CODIST_MODEL") != "" {
		return cfg.Model
	}
	switch gb := memoryGB(); {
	case gb > 0 && gb < 12:
		return "qwen2.5-coder:3b"
	case gb > 0 && gb < 15:
		return "qwen2.5-coder:7b"
	}
	return cfg.Model
}

// installedModels asks Ollama; an error means it is not running.
func installedModels(url string) ([]string, error) {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	names := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		names[i] = m.Name
	}
	return names, nil
}

func has(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

const ollamaHelp = "install it from https://ollama.com (macOS: brew install ollama), then run: ollama serve"

// doctor reports what works. Only git is required: without the model Codist still runs its rules.
func doctor(cfg config.Config) int {
	line := func(ok bool, name, detail string) {
		mark := "ok     "
		if !ok {
			mark = "missing"
		}
		fmt.Printf("  %s  %-18s %s\n", mark, name, detail)
	}
	version := func(cmd string, args ...string) (string, bool) {
		out, err := exec.Command(cmd, args...).CombinedOutput()
		if err != nil {
			return "", false
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return first, true
	}
	failed := 0
	fmt.Println("Required")
	v, ok := version("git", "--version")
	line(ok, "git", v)
	if !ok {
		failed++
	}
	fmt.Println("For AI review, fixes and Ask (rules work without them)")
	models, err := installedModels(cfg.OllamaURL)
	if err != nil {
		line(false, "Ollama", ollamaHelp)
	} else {
		line(true, "Ollama", cfg.OllamaURL)
		want := recommendedModel(cfg)
		if has(models, want) {
			line(true, "model", want)
		} else {
			line(false, "model", want+": run codist setup")
		}
	}
	fmt.Println("Optional, for more findings on Java projects")
	v, ok = version("java", "-version")
	line(ok, "Java", v)
	v, ok = version("mvn", "-v")
	line(ok, "Maven (PMD, SpotBugs)", v)
	_, err = exec.LookPath("jdtls")
	line(err == nil, "jdtls", "the Eclipse Java language server (macOS: brew install jdtls)")
	if gb := memoryGB(); gb > 0 {
		fmt.Printf("\nThis machine has %d GB of memory; recommended model: %s\n", gb, recommendedModel(cfg))
	}
	return failed
}

// setup downloads the model through Ollama, showing progress.
func setup(cfg config.Config) int {
	models, err := installedModels(cfg.OllamaURL)
	if err != nil {
		fmt.Println("Ollama is not running at " + cfg.OllamaURL + ": " + ollamaHelp)
		fmt.Println("Codist still works without it, using its rules and analysers only.")
		return 1
	}
	want := recommendedModel(cfg)
	if has(models, want) {
		fmt.Println(want + " is ready.")
		return 0
	}
	fmt.Println("Downloading " + want + "…")
	body, _ := json.Marshal(map[string]any{"name": want, "stream": true})
	resp, err := http.Post(cfg.OllamaURL+"/api/pull", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Println("Could not reach Ollama:", err)
		return 1
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	last := ""
	for {
		var p struct {
			Status    string `json:"status"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			fmt.Println("\nDownload failed:", err)
			return 1
		}
		if p.Error != "" {
			fmt.Println("\nDownload failed:", p.Error)
			return 1
		}
		text := p.Status
		if p.Total > 0 {
			text = fmt.Sprintf("%s %d%%", p.Status, p.Completed*100/p.Total)
		}
		if text != last {
			fmt.Printf("\r%-70.70s", text)
			last = text
		}
	}
	fmt.Println("\n" + want + " is ready. Now run: codist")
	if want != cfg.Model {
		fmt.Println("This machine suits the smaller model, so set: export CODIST_MODEL=" + want)
	}
	return 0
}
