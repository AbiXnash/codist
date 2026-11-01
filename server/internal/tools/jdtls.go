package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jdtls runs the Eclipse Java language server (the engine behind STS and VS Code Java) on an export
// of the commit, over the Language Server Protocol, and collects its compiler diagnostics: real type
// resolution with the project's dependencies, so it sees what a parser cannot (unresolved symbols,
// wrong types, null analysis, dead code, unused code, deprecated API use).
//
// It imports the Maven project once per export, which takes a while, so it runs per commit like the
// other analysers and its results are cached.
func (r *Runner) jdtls(work, src string) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), jdtlsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.jdtlsBin, "-data", filepath.Join(work, "jdtls-workspace"))
	cmd.Dir = src
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	c := &lspClient{in: stdin, diagnostics: map[string][]lspDiagnostic{}, activity: make(chan struct{}, 1), ready: make(chan struct{})}
	go c.read(bufio.NewReader(stdout))

	root := "file://" + (&url.URL{Path: src}).EscapedPath()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"processId": os.Getpid(), "rootUri": root,
		"capabilities": map[string]any{"textDocument": map[string]any{"publishDiagnostics": map[string]any{}}},
		"initializationOptions": map[string]any{"settings": map[string]any{"java": map[string]any{
			"import": map[string]any{"maven": map[string]any{"enabled": true}, "gradle": map[string]any{"enabled": true}},
			"autobuild": map[string]any{"enabled": true},
		}}, "extendedClientCapabilities": map[string]any{"progressReportProvider": false, "classFileContentsSupport": false}},
	}}); err != nil {
		return nil, err
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}

	// The server is done when it reports ready and then stays quiet.
	select {
	case <-c.ready:
	case <-ctx.Done():
		return nil, fmt.Errorf("the language server did not get ready in %s", jdtlsTimeout)
	}
	quiet := time.NewTimer(jdtlsQuiet)
	defer quiet.Stop()
wait:
	for {
		select {
		case <-c.activity:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(jdtlsQuiet)
		case <-quiet.C:
			break wait
		case <-ctx.Done():
			break wait
		}
	}
	return c.findings(src), nil
}

const (
	jdtlsTimeout = 4 * time.Minute
	jdtlsQuiet   = 8 * time.Second
)

type lspDiagnostic struct {
	Severity int             `json:"severity"`
	Message  string          `json:"message"`
	Code     json.RawMessage `json:"code"`
	Range    struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
		End struct {
			Line int `json:"line"`
		} `json:"end"`
	} `json:"range"`
}

type lspClient struct {
	in          io.Writer
	mu          sync.Mutex
	diagnostics map[string][]lspDiagnostic
	activity    chan struct{}
	ready       chan struct{}
	readyOnce   sync.Once
}

func (c *lspClient) send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (c *lspClient) read(r *bufio.Reader) {
	for {
		size := 0
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				size, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		c.handle(body)
	}
}

func (c *lspClient) handle(body []byte) {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(body, &m) != nil {
		return
	}
	if m.ID != nil && m.Method != "" { // a request from the server: answer it, or it waits forever
		_ = c.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
	}
	switch m.Method {
	case "language/status":
		var s struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(m.Params, &s) == nil && s.Type == "ServiceReady" {
			c.readyOnce.Do(func() { close(c.ready) })
		}
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string          `json:"uri"`
			Diagnostics []lspDiagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			c.mu.Lock()
			c.diagnostics[p.URI] = p.Diagnostics
			c.mu.Unlock()
		}
	}
	select {
	case c.activity <- struct{}{}:
	default:
	}
}

// findings maps diagnostics to repository paths; errors and warnings only, Java sources only.
func (c *lspClient) findings(src string) []Finding {
	c.mu.Lock()
	defer c.mu.Unlock()
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		real = src
	}
	out := []Finding{}
	for uri, ds := range c.diagnostics {
		u, err := url.Parse(uri)
		if err != nil {
			continue
		}
		p := u.Path
		if !strings.HasSuffix(p, ".java") {
			continue
		}
		rel, err := filepath.Rel(real, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			if rel, err = filepath.Rel(src, p); err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
		}
		for _, d := range ds {
			if d.Severity != 1 && d.Severity != 2 {
				continue
			}
			severity, category := "low", "design"
			if d.Severity == 1 {
				severity, category = "high", "bug"
			}
			code := strings.Trim(string(d.Code), `"`)
			if code == "" || code == "null" {
				code = "compiler"
			}
			out = append(out, Finding{Tool: "jdtls", Rule: code, Category: category, Severity: severity, Path: filepath.ToSlash(rel),
				Line: d.Range.Start.Line + 1, EndLine: d.Range.End.Line + 1, Message: d.Message})
		}
	}
	return out
}
