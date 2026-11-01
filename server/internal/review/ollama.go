package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codist/prompts"
)

// Ollama is a minimal client for /api/chat with JSON-mode output.
type Ollama struct {
	URL    string
	Model  string
	NumCtx int
	HTTP   *http.Client
	ctx    context.Context
	label  Label
}

// For labels the calls of this client for the activity stream.
func (o *Ollama) For(l Label) *Ollama {
	if o == nil {
		return nil
	}
	c := *o
	c.label = l
	return &c
}

// record starts an activity entry for a model call; the returned func completes it.
func (o *Ollama) record(system, user string) func(response string, err error, promptTok, replyTok int) {
	l := o.label
	if l.Kind == "" {
		l.Kind, l.Title = "model", "Model call"
	}
	a := Activities.begin(Activity{Kind: l.Kind, Title: l.Title, Model: o.Model, SHA: l.SHA, Path: l.Path, Incident: l.Incident, Knowledge: l.Knowledge, System: system, User: user})
	return func(response string, err error, promptTok, replyTok int) {
		text := ""
		if err != nil {
			text = err.Error()
		}
		Activities.finish(a, response, text, promptTok, replyTok)
	}
}

// With returns a client whose requests stop when ctx is cancelled (the developer pressed Cancel).
func (o *Ollama) With(ctx context.Context) *Ollama {
	if o == nil {
		return nil
	}
	c := *o
	c.ctx = ctx
	return &c
}

func NewOllama(url, model string, numCtx int, timeout time.Duration) *Ollama {
	return &Ollama{URL: url, Model: model, NumCtx: numCtx, HTTP: &http.Client{Timeout: timeout}}
}

// ChatJSON asks for a JSON object. An answer that does not parse is sent back once with the
// retry-invalid-json prompt; small models usually get it right the second time.
func (o *Ollama) ChatJSON(system, user string) (map[string]any, error) {
	out, err := o.chatJSON(system, user)
	if bad, ok := err.(errInvalidJSON); ok {
		return o.chatJSON(system, user+"\n\n"+prompts.Render("retry-invalid-json", map[string]any{"Error": bad.reason}))
	}
	return out, err
}

type errInvalidJSON struct{ reason, content string }

func (e errInvalidJSON) Error() string { return "model did not return valid JSON: " + e.content }

func (o *Ollama) chatJSON(system, user string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      o.Model,
		"stream":     false,
		"format":     "json",
		"keep_alive": "30m",
		"options":    map[string]any{"temperature": 0.1, "num_ctx": o.NumCtx, "num_predict": 1500},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	answer, err := o.once(body, system, user)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(answer)
	content = strings.TrimPrefix(strings.TrimPrefix(content, "```json"), "```")
	content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		if len(content) > 300 {
			content = content[:300] + "…"
		}
		return nil, errInvalidJSON{reason: err.Error(), content: content}
	}
	return parsed, nil
}

// Problem is "" when Ollama is reachable and has the model, otherwise a readable reason.
func (o *Ollama) Problem() string {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(o.URL + "/api/tags")
	if err != nil {
		return "Ollama is not reachable at " + o.URL
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "Unexpected response from Ollama"
	}
	for _, m := range tags.Models {
		if m.Name == o.Model {
			return ""
		}
	}
	return "Model " + o.Model + " is not pulled. Run: ollama pull " + o.Model
}

// JSON helpers: small models return numbers as strings and strings as objects.

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprint(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func num(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		if _, err := fmt.Sscan(t, &n); err == nil {
			return n
		}
	}
	return def
}

// ChatText is a plain (non-JSON) completion, used when the answer is a whole file.
func (o *Ollama) ChatText(system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      o.Model,
		"stream":     false,
		"keep_alive": "30m",
		"options":    map[string]any{"temperature": 0, "num_ctx": o.NumCtx},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	return o.once(body, system, user)
}

// exchange makes one real model call and records it.
func (o *Ollama) exchange(body []byte, system, user string) (string, error) {
	done := o.record(system, user)
	resp, err := o.post(body)
	if err != nil {
		done("", err, 0, 0)
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		done("", err, 0, 0)
		return "", err
	}
	done(out.Message.Content, nil, out.PromptEvalCount, out.EvalCount)
	return out.Message.Content, nil
}

// post sends a chat request, retrying when Ollama is busy swapping models in or out of memory
// (it answers 500/503 while evicting one model to load another on machines with little RAM).
func (o *Ollama) post(body []byte) (*http.Response, error) {
	var last string
	for attempt := 0; attempt < 3; attempt++ {
		ctx := o.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*3) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := o.HTTP.Do(req)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			last = err.Error()
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		resp.Body.Close()
		last = resp.Status + " " + strings.TrimSpace(string(msg))
		if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusServiceUnavailable {
			break
		}
	}
	return nil, fmt.Errorf("ollama (%s): %s", o.Model, last)
}
