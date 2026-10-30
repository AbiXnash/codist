// Package incident turns production logs into a root cause analysis: errors are parsed from the
// logs, mapped to the code through the graph and git blame, the commits that most likely caused
// them are ranked, and the local model explains the cause with evidence. Logs never leave the machine.
package incident

import (
	"bufio"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Frame is one "at pkg.Class.method(File.java:42)" line.
type Frame struct {
	Class  string `json:"class"`
	Method string `json:"method"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// ErrorEvent is one distinct error in the logs, with how often it occurred.
type ErrorEvent struct {
	Exception string   `json:"exception"`
	Message   string   `json:"message"`
	Logger    string   `json:"logger,omitempty"` // class that logged it, for errors without a stack trace
	Level     string   `json:"level,omitempty"`
	Frames    []Frame  `json:"frames"`
	CausedBy  []string `json:"causedBy,omitempty"` // "Exception: message" of each cause, outermost first
	Count     int      `json:"count"`
	FirstTime string   `json:"firstTime,omitempty"`
	LastTime  string   `json:"lastTime,omitempty"`
	Signature string   `json:"signature"`
}

var (
	frameLine     = regexp.MustCompile(`^\s*at\s+([\w$.]+)\.([\w$<>]+)\(([^:)]*)(?::(\d+))?\)`)
	exceptionLine = regexp.MustCompile(`((?:[a-zA-Z_$][\w$]*\.)+[A-Z][\w$]*(?:Exception|Error|Throwable|Failure))(?::\s?(.*))?$`)
	causedByLine  = regexp.MustCompile(`^\s*Caused by:\s*((?:[a-zA-Z_$][\w$]*\.)*[A-Z][\w$]*)(?::\s?(.*))?$`)
	timestamp     = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?)`)
	levelLine     = regexp.MustCompile(`\b(ERROR|FATAL|SEVERE|WARN)\b`)
	// Logback / Spring Boot default: "2026-10-30 ... ERROR 123 --- [app] [nio-8080-exec-1] c.d.s.o.OrderService : message"
	springLogger = regexp.MustCompile(`\]\s+([\w$.]+)\s+:\s+(.*)$`)
	// Common pattern: "... ERROR com.demo.Foo - message"
	dashLogger = regexp.MustCompile(`\b(?:ERROR|FATAL|SEVERE|WARN)\b\s+(?:\[[^\]]*\]\s+)?([a-z][\w$]*(?:\.[\w$]+)+)\s+-\s+(.*)$`)
	numbers    = regexp.MustCompile(`\b[0-9a-f]{8,}\b|\b\d+\b`)
)

const maxLogBytes = 8 << 20

// Parse extracts distinct errors from raw logs. Repeated errors with the same exception and
// the same top frames are merged and counted.
func Parse(raw string) []ErrorEvent {
	if len(raw) > maxLogBytes {
		raw = raw[len(raw)-maxLogBytes:] // the end of a log is where incidents are
	}
	var events []*ErrorEvent
	var cur *ErrorEvent
	var curTime, pendingLevel, pendingLogger, pendingMsg string

	flush := func() {
		if cur != nil {
			events = append(events, cur)
			cur = nil
		}
	}
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if m := timestamp.FindStringSubmatch(line); m != nil {
			curTime = m[1]
		}
		if m := frameLine.FindStringSubmatch(line); m != nil {
			if cur == nil { // a stack trace whose header we missed
				cur = &ErrorEvent{Exception: "UnknownException", Count: 1, FirstTime: curTime}
			}
			n, _ := strconv.Atoi(m[4])
			cur.Frames = append(cur.Frames, Frame{Class: m[1], Method: m[2], File: m[3], Line: n})
			continue
		}
		if m := causedByLine.FindStringSubmatch(line); m != nil && cur != nil {
			cur.CausedBy = append(cur.CausedBy, strings.TrimSpace(m[1]+": "+m[2]))
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "... ") && strings.HasSuffix(strings.TrimSpace(line), " more") {
			continue
		}
		if lv := levelLine.FindStringSubmatch(line); lv != nil {
			flush()
			pendingLevel, pendingLogger, pendingMsg = lv[1], "", ""
			if m := springLogger.FindStringSubmatch(line); m != nil {
				pendingLogger, pendingMsg = m[1], m[2]
			} else if m := dashLogger.FindStringSubmatch(line); m != nil {
				pendingLogger, pendingMsg = m[1], m[2]
			}
			// Some formats put the exception on the same line as the level.
			if m := exceptionLine.FindStringSubmatch(line); m != nil && !strings.Contains(pendingMsg, m[1]+" ") {
				cur = &ErrorEvent{Exception: m[1], Message: strings.TrimSpace(m[2]), Logger: pendingLogger, Level: pendingLevel, Count: 1, FirstTime: curTime}
				continue
			}
			if pendingLevel != "WARN" || pendingLogger != "" {
				cur = &ErrorEvent{Message: strings.TrimSpace(pendingMsg), Logger: pendingLogger, Level: pendingLevel, Count: 1, FirstTime: curTime}
				if cur.Message == "" {
					cur.Message = strings.TrimSpace(line)
				}
			}
			continue
		}
		if m := exceptionLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !strings.HasPrefix(strings.TrimSpace(line), "at ") {
			if cur != nil && cur.Exception == "" && len(cur.Frames) == 0 {
				cur.Exception, cur.Message = m[1], firstNonEmpty(strings.TrimSpace(m[2]), cur.Message)
				continue
			}
			flush()
			cur = &ErrorEvent{Exception: m[1], Message: strings.TrimSpace(m[2]), Level: pendingLevel, Logger: pendingLogger, Count: 1, FirstTime: curTime}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Any other line ends a stack trace.
		if cur != nil && len(cur.Frames) > 0 {
			flush()
		}
	}
	flush()

	// Merge repeats.
	bySig := map[string]*ErrorEvent{}
	var order []string
	for _, e := range events {
		if e.Exception == "" && e.Message == "" {
			continue
		}
		e.Signature = signature(e)
		if prev, ok := bySig[e.Signature]; ok {
			prev.Count++
			prev.LastTime = firstNonEmpty(e.FirstTime, prev.LastTime)
			continue
		}
		e.LastTime = e.FirstTime
		bySig[e.Signature] = e
		order = append(order, e.Signature)
	}
	out := make([]ErrorEvent, 0, len(order))
	for _, s := range order {
		out = append(out, *bySig[s])
	}
	// Errors with stack traces first, then by frequency.
	sort.SliceStable(out, func(i, j int) bool {
		if (len(out[i].Frames) > 0) != (len(out[j].Frames) > 0) {
			return len(out[i].Frames) > 0
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func signature(e *ErrorEvent) string {
	parts := []string{e.Exception, e.Logger}
	for i, f := range e.Frames {
		if i == 3 {
			break
		}
		parts = append(parts, f.Class+"."+f.Method)
	}
	if len(e.Frames) == 0 {
		parts = append(parts, numbers.ReplaceAllString(e.Message, "#"))
	}
	return strings.Join(parts, "|")
}

// Tokens are the words used to compare incidents: exception names, project frames and the
// message with ids and numbers removed.
func Tokens(events []ErrorEvent, isProject func(class string) bool) map[string]bool {
	out := map[string]bool{}
	for _, e := range events {
		if e.Exception != "" {
			out["ex:"+e.Exception[strings.LastIndex(e.Exception, ".")+1:]] = true
		}
		for _, c := range e.CausedBy {
			name, _, _ := strings.Cut(c, ":")
			out["ex:"+name[strings.LastIndex(name, ".")+1:]] = true
		}
		for _, f := range e.Frames {
			if isProject(f.Class) {
				out["at:"+simpleClass(f.Class)+"."+f.Method] = true
			}
		}
		if e.Logger != "" {
			out["log:"+simpleClass(e.Logger)] = true
		}
		for _, w := range strings.Fields(strings.ToLower(numbers.ReplaceAllString(e.Message, " "))) {
			w = strings.Trim(w, `.,:;"'()[]{}`)
			if len(w) > 3 {
				out["w:"+w] = true
			}
		}
	}
	return out
}

// simpleClass turns com.demo.shop.OrderService$$SpringCGLIB$$0 or Foo$1 into OrderService / Foo.
func simpleClass(fqn string) string {
	s := fqn[strings.LastIndex(fqn, ".")+1:]
	if i := strings.Index(s, "$"); i > 0 {
		s = s[:i]
	}
	return s
}

// OuterClass strips inner-class, lambda and proxy suffixes from a frame's class name.
func OuterClass(fqn string) string {
	if i := strings.Index(fqn, "$"); i > 0 {
		return fqn[:i]
	}
	return fqn
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
