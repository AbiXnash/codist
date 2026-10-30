package review

import (
	"fmt"
	"regexp"
	"strings"
)

// Some rules have a known, mechanical fix. Applying it directly is faster than the model and
// cannot invent anything: the result is still shown as a diff for approval.

var (
	wrongConstTitle  = regexp.MustCompile(`^([A-Z][A-Za-z0-9]*)\.([A-Z][A-Z0-9_]*) is "([^"]*)" but used in ([A-Za-z0-9]+)$`)
	wrongConstDomain = regexp.MustCompile(`belongs to "([a-z0-9]+)`)
)

// autofixWrongConstant handles copy-paste-wrong-constant: it adds a constant for the using class's
// area next to the original (keeping the original, which other classes use) and points the flagged
// lines at it.
func (x *Fixer) autofixWrongConstant(members []placed) ([]edit, string, bool) {
	var usage []placed
	var typ, field, value, user, domain string
	for _, m := range members {
		if m.f.Ref != "copy-paste-wrong-constant" {
			continue
		}
		t := wrongConstTitle.FindStringSubmatch(m.f.Title)
		d := wrongConstDomain.FindStringSubmatch(m.f.Detail)
		if t == nil || d == nil {
			return nil, "", false
		}
		if typ != "" && (t[1] != typ || t[2] != field) {
			return nil, "", false // several different constants: leave it to the model
		}
		typ, field, value, user, domain = t[1], t[2], t[3], t[4], d[1]
		usage = append(usage, m)
	}
	if len(usage) == 0 {
		return nil, "", false
	}

	// The class that declares the constant.
	ownerPath := ""
	sources := x.repo.JavaFilesWorking()
	for p := range sources {
		if strings.HasSuffix(p, "/"+typ+".java") {
			ownerPath = p
		}
	}
	if ownerPath == "" {
		return nil, "", false
	}
	owner := string(sources[ownerPath])
	upper := strings.ToUpper(domain)
	newName := upper + "_" + field
	if strings.HasPrefix(field, strings.ToUpper(strings.TrimSuffix(value, "_"))+"_") {
		newName = upper + strings.TrimPrefix(field, strings.ToUpper(value)) // CUSTOMER_PREFIX -> PRODUCT_PREFIX
	}

	// A constant with the right value already exists: point the usages at it, add nothing.
	for name, lit := range constants(owner) {
		if lit == `"`+upper+`"` && name != field {
			newName = name
		}
	}
	if newName == field {
		return nil, "", false
	}

	var edits []edit
	decl := regexp.MustCompile(`(?m)^([ \t]*)((?:public |protected |private )?static final \w+ )` + regexp.QuoteMeta(field) + `(\s*=\s*)"` + regexp.QuoteMeta(value) + `";[ \t]*$`)
	if !regexp.MustCompile(`\b` + newName + `\b`).MatchString(owner) {
		loc := decl.FindStringSubmatchIndex(owner)
		if loc == nil {
			return nil, "", false
		}
		line := owner[loc[0]:loc[1]]
		added := decl.ReplaceAllString(line, "${1}${2}"+newName+"${3}\""+upper+"\";")
		updated := owner[:loc[1]] + "\n" + added + owner[loc[1]:]
		edits = append(edits, edit{path: ownerPath, original: owner, proposed: updated})
	}

	ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(typ+"."+field) + `\b`)
	byFile := map[string][]int{}
	var order []string
	for _, u := range usage {
		if _, ok := byFile[u.path]; !ok {
			order = append(order, u.path)
		}
		byFile[u.path] = append(byFile[u.path], u.f.Line)
	}
	for _, p := range order {
		src, ok := sources[p]
		if !ok {
			return nil, "", false
		}
		lines := strings.Split(string(src), "\n")
		for _, n := range byFile[p] {
			if n >= 1 && n <= len(lines) {
				lines[n-1] = ref.ReplaceAllString(lines[n-1], typ+"."+newName)
			}
		}
		if out := strings.Join(lines, "\n"); out != string(src) {
			edits = append(edits, edit{path: p, original: string(src), proposed: out})
		}
	}
	if len(edits) == 0 {
		return nil, "", false
	}
	what := fmt.Sprintf("Added %s.%s = \"%s\"", typ, newName, upper)
	if len(edits) == 0 || edits[0].path != ownerPath {
		what = fmt.Sprintf("Switched to the existing %s.%s = \"%s\"", typ, newName, upper)
	}
	msg := fmt.Sprintf("fix(%s): use a %s log prefix instead of %s.%s\n\n%s.%s is \"%s\" but %s belongs to %q.\n%s and kept %s.%s for the classes that use it.\n\nFound by Codist (rule copy-paste-wrong-constant).",
		user, upper, typ, field, typ, field, value, user, domain, what, typ, field)
	if !strings.Contains(strings.ToUpper(field), "LOG") {
		msg = strings.Replace(msg, "log prefix", "constant", 1)
	}
	return edits, msg, true
}
