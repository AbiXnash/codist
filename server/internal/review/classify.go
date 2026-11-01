package review

import "strings"

// Class says what kind of issue a finding is and what to do about it, so a list of findings can be
// triaged: what it is (Kind), how soon (Urgency) and how big the change is (Change).
type Class struct {
	Kind    string `json:"kind"`    // Bug, Security, Optimization, Better log, Readability, Dead code, Poor coding, Test
	Urgency string `json:"urgency"` // Hotfix, Fix soon, Quick fix, Cleanup
	Change  string `json:"change"`  // Minor, Major
}

var kindByRef = map[string]string{
	"commented-out-code": "Dead code", "unused-local": "Dead code", "unused-field": "Dead code", "unused-method": "Dead code",
	"unused-import": "Dead code", "unused-class": "Dead code", "empty-block": "Dead code", "unreachable-code": "Dead code",
	"constant-condition": "Bug", "identical-branches": "Bug", "bug-comment": "Bug",
	"console-output": "Better log", "duplicate-log": "Better log", "log-helper-method": "Readability", "better-log": "Better log",
	"logging-in-loop": "Better log", "literal-instead-of-constant": "Readability", "simplify-boolean-return": "Readability",
	"better-method": "Readability", "field-injection": "Poor coding", "high-complexity": "Poor coding", "long-method": "Poor coding",
	"too-many-parameters": "Poor coding", "nested-loops": "Optimization", "query-in-loop": "Optimization",
	"list-lookup-in-loop": "Optimization", "remote-call-in-loop": "Optimization",
	"uninjected-dependency": "Bug", "copy-paste-wrong-constant": "Bug", "merge-conflict-marker": "Bug",
}

var kindByCategory = map[string]string{
	"bug": "Bug", "concurrency": "Bug", "jpa": "Bug", "security": "Security", "performance": "Optimization", "logging": "Better log",
	"style": "Readability", "design": "Poor coding", "spring": "Poor coding", "testing": "Test", "edit": "Poor coding",
}

// Fixing these means changing the design or several call sites, not a line.
var majorRefs = map[string]bool{
	"high-complexity": true, "long-method": true, "nested-loops": true, "query-in-loop": true, "remote-call-in-loop": true,
	"singleton-mutable-state": true, "check-then-act": true, "lost-update": true, "tx-self-invocation": true, "entity-in-api": true,
	"missing-transaction": true, "too-many-parameters": true, "n-plus-one": true, "missing-authorization": true,
}

// Classify triages one finding. places is how many places of the code the issue spans.
func Classify(f Finding, places int) Class {
	c := Class{Kind: kindByRef[f.Ref]}
	if c.Kind == "" && strings.Contains(f.Ref, ":") { // analysers
		c.Kind = kindByCategory[f.Category]
	}
	if c.Kind == "" {
		c.Kind = kindByCategory[strings.ToLower(f.Category)]
	}
	if c.Kind == "" {
		c.Kind = "Bug"
	}
	instant := Instant(f)
	switch {
	case f.Severity == "critical" || (f.Severity == "high" && c.Kind == "Security"):
		c.Urgency = "Hotfix"
	case f.Severity == "high":
		c.Urgency = "Fix soon"
	case instant || c.Kind == "Dead code" || c.Kind == "Better log":
		c.Urgency = "Quick fix"
	case f.Severity == "medium":
		c.Urgency = "Fix soon"
	default:
		c.Urgency = "Cleanup"
	}
	c.Change = "Minor"
	if (majorRefs[f.Ref] || places >= 5) && !instant {
		c.Change = "Major"
	}
	return c
}
