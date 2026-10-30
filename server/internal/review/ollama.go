package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama is a minimal client for /api/chat with JSON-mode output.
type Ollama struct {
	URL    string
	Model  string
	NumCtx int
	HTTP   *http.Client
}

func NewOllama(url, model string, numCtx int, timeout time.Duration) *Ollama {
	return &Ollama{URL: url, Model: model, NumCtx: numCtx, HTTP: &http.Client{Timeout: timeout}}
}

func (o *Ollama) ChatJSON(system, user string) (map[string]any, error) {
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
	resp, err := o.post(body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(out.Message.Content)
	content = strings.TrimPrefix(strings.TrimPrefix(content, "```json"), "```")
	content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		if len(content) > 300 {
			content = content[:300] + "…"
		}
		return nil, fmt.Errorf("model did not return valid JSON: %s", content)
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
	resp, err := o.post(body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Message.Content, nil
}

// post sends a chat request, retrying when Ollama is busy swapping models in or out of memory
// (it answers 500/503 while evicting one model to load another on machines with little RAM).
func (o *Ollama) post(body []byte) (*http.Response, error) {
	var last string
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
		resp, err := o.HTTP.Post(o.URL+"/api/chat", "application/json", bytes.NewReader(body))
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
