package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

// Asking the model the same thing twice costs half a minute and gives the same answer, because the
// calls run at a temperature of 0 or close to it. Identical requests, meaning the same server, model,
// options, instructions and prompt, therefore make one call: one already running is joined, and one
// answered in the last while is reused. Anything that changes the prompt, such as the code, the
// knowledge, the memory or a suggestion from the developer, is a new request.
const (
	answerTTL  = 15 * time.Minute
	maxAnswers = 128
)

type flight struct {
	done   chan struct{}
	answer string
	err    error
}

type remembered struct {
	answer string
	at     time.Time
}

var dedupe = struct {
	sync.Mutex
	running map[[32]byte]*flight
	answers map[[32]byte]remembered
}{running: map[[32]byte]*flight{}, answers: map[[32]byte]remembered{}}

// once answers a request, making a model call only when nobody else already has.
func (o *Ollama) once(body []byte, system, user string) (string, error) {
	h := sha256.New()
	h.Write([]byte(o.URL))
	h.Write([]byte{0})
	h.Write(body)
	var key [32]byte
	h.Sum(key[:0])
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		dedupe.Lock()
		if r, ok := dedupe.answers[key]; ok {
			if time.Since(r.at) < answerTTL {
				dedupe.Unlock()
				o.reused("reused the answer from " + time.Since(r.at).Round(time.Second).String() + " ago")
				return r.answer, nil
			}
			delete(dedupe.answers, key)
		}
		f, joined := dedupe.running[key]
		if !joined {
			f = &flight{done: make(chan struct{})}
			dedupe.running[key] = f
		}
		dedupe.Unlock()

		if !joined {
			f.answer, f.err = o.exchange(body, system, user)
			dedupe.Lock()
			delete(dedupe.running, key)
			if f.err == nil {
				if len(dedupe.answers) >= maxAnswers {
					forgetOldest()
				}
				dedupe.answers[key] = remembered{f.answer, time.Now()}
			}
			dedupe.Unlock()
			close(f.done)
			return f.answer, f.err
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err() // only this caller stops waiting; the other still wants the answer
		case <-f.done:
		}
		if errors.Is(f.err, context.Canceled) || errors.Is(f.err, context.DeadlineExceeded) {
			continue // whoever made the call cancelled it, but this caller still needs an answer
		}
		o.reused("joined an identical call that was already running")
		return f.answer, f.err
	}
}

// forgetOldest makes room. The caller holds the lock.
func forgetOldest() {
	var oldest [32]byte
	var at time.Time
	for k, r := range dedupe.answers {
		if at.IsZero() || r.at.Before(at) {
			oldest, at = k, r.at
		}
	}
	delete(dedupe.answers, oldest)
}

// reused shows in the activity stream that a call was saved.
func (o *Ollama) reused(how string) {
	title := o.label.Title
	if title == "" {
		title = "Model call"
	}
	Activities.Step(title+": no model call", o.label.SHA, o.label.Path, how, 0)
}
