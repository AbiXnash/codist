package review

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"codist/internal/gitx"
	"codist/internal/javaidx"
)

// RepoReport describes the repository itself: its services, APIs, how they call each other, who
// works on what, where it changes most and where it costs most.
type RepoReport struct {
	Name         string             `json:"name"`
	Head         string             `json:"head"`
	Branch       string             `json:"branch"`
	Files        int                `json:"files"`
	JavaFiles    int                `json:"javaFiles"`
	TestFiles    int                `json:"testFiles"`
	Lines        int                `json:"lines"`
	Types        int                `json:"types"`
	Methods      int                `json:"methods"`
	Commits      int                `json:"commits"`
	Contributors []gitx.Contributor `json:"contributors"`
	Languages    map[string]int     `json:"languages"` // files per extension
	Roles        map[string]int     `json:"roles"`
	Services     []RepoService      `json:"services"`
	Endpoints    []RepoEndpoint     `json:"endpoints"`
	Calls        []RepoCall         `json:"calls"`
	Hotspots     []RepoHotspot      `json:"hotspots"`
	Costly       []RepoMethod       `json:"costly"`
	CostlyTotal  int                `json:"costlyTotal"`
	Largest      []RepoClass        `json:"largest"`
	Issues       map[string]int     `json:"issues"` // open by kind
	Urgency      map[string]int     `json:"urgency"`
	Severity     map[string]int     `json:"severity"`
}

type RepoService struct {
	Name      string         `json:"name"`
	Port      string         `json:"port,omitempty"`
	Kind      string         `json:"kind"` // service or library
	Files     int            `json:"files"`
	Lines     int            `json:"lines"`
	Types     int            `json:"types"`
	Tests     int            `json:"tests"`
	Endpoints int            `json:"endpoints"`
	Calls     []string       `json:"calls"`
	CalledBy  []string       `json:"calledBy"`
	UsesLibs  []string       `json:"usesLibs"`
	Deps      []string       `json:"deps"`
	Open      map[string]int `json:"open"` // by severity
	Commits   int            `json:"commits"`
}

type RepoEndpoint struct {
	Service string `json:"service"`
	Route   string `json:"route"`
	Handler string `json:"handler"`
	Class   string `json:"class"`
	Node    string `json:"node"`
	Callers int    `json:"callers"` // Feign clients calling it
}

type RepoCall struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Client string   `json:"client"`
	Node   string   `json:"node"`
	Routes []string `json:"routes"`
}

type RepoHotspot struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
	Open    int    `json:"open"`
	Lines   int    `json:"lines"`
}

type RepoMethod struct {
	Class  string       `json:"class"`
	Node   string       `json:"node"`
	Method string       `json:"method"`
	File   string       `json:"file"`
	Line   int          `json:"line"`
	Cost   javaidx.Cost `json:"cost"`
}

type RepoClass struct {
	Name, Node, File string
	Lines, Members   int
}

var serverPort = regexp.MustCompile(`(?m)^\s*server\.port\s*[=:]\s*(\S+)|^server:\s*\n\s+port:\s*(\S+)`)

// Repo builds the report from the graph, the tracker and git.
func Repo(repo *gitx.Repo, g *javaidx.Graph, issues []Issue) RepoReport {
	head := repo.Head()
	r := RepoReport{Name: repo.Name(), Head: head, Branch: repo.Tracking(), Contributors: repo.Contributors(), Languages: map[string]int{}, Roles: map[string]int{},
		Services: []RepoService{}, Endpoints: []RepoEndpoint{}, Calls: []RepoCall{}, Hotspots: []RepoHotspot{}, Costly: []RepoMethod{}, Largest: []RepoClass{},
		Issues: map[string]int{}, Urgency: map[string]int{}, Severity: map[string]int{}}
	for _, c := range r.Contributors {
		r.Commits += c.Commits
	}
	files := repo.TrackedFiles()
	r.Files = len(files)
	sources := repo.JavaFilesAt(head)
	lines := map[string]int{}
	services := map[string]*RepoService{}
	service := func(name string) *RepoService {
		if name == "" {
			name = r.Name
		}
		if services[name] == nil {
			services[name] = &RepoService{Name: name, Kind: "library", Open: map[string]int{}, Calls: []string{}, CalledBy: []string{}, UsesLibs: []string{}, Deps: []string{}}
		}
		return services[name]
	}
	for _, f := range files {
		if ext := strings.TrimPrefix(filepath.Ext(f), "."); ext != "" {
			r.Languages[ext]++
		}
	}
	for p, src := range sources {
		n := strings.Count(string(src), "\n") + 1
		lines[p] = n
		r.Lines += n
		r.JavaFiles++
		s := service(javaidx.ModuleOf(p))
		s.Files++
		s.Lines += n
		if strings.Contains(p, "src/test/") {
			r.TestFiles++
			s.Tests++
		}
	}
	// Ports, build files and which modules run: application config marks a service.
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasPrefix(base, "application") && (strings.HasSuffix(base, ".properties") || strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) && strings.Contains(f, "src/main/resources") {
			mod := f[:strings.Index(f, "src/main/resources")]
			s := service(strings.TrimSuffix(mod, "/"))
			if src, ok := repo.ReadFile(head, f); ok {
				if m := serverPort.FindStringSubmatch(src); m != nil {
					s.Port = m[1] + m[2]
				}
			}
		}
		if base == "pom.xml" || base == "build.gradle" || base == "build.gradle.kts" {
			mod := strings.TrimSuffix(strings.TrimSuffix(f, base), "/")
			if mod == "" {
				continue
			}
			if src, ok := repo.ReadFile(head, f); ok {
				if b, ok := ParseBuild(f, src); ok {
					s := service(mod)
					for _, d := range b.Deps {
						s.Deps = append(s.Deps, d.Artifact)
					}
				}
			}
		}
	}
	nodes := map[string]javaidx.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	callers := map[string]int{}
	seenCall := map[string]bool{}
	for _, e := range g.Edges {
		src, dst := nodes[e.Source], nodes[e.Target]
		if e.Kind == "feign" {
			for _, v := range e.Via {
				callers[dst.Module+" "+v]++
			}
			r.Calls = append(r.Calls, RepoCall{From: service(src.Module).Name, To: service(dst.Module).Name, Client: src.Name, Node: src.ID, Routes: e.Via})
			if !seenCall[src.Module+">"+dst.Module] {
				seenCall[src.Module+">"+dst.Module] = true
				a, b := service(src.Module), service(dst.Module)
				a.Calls, b.CalledBy = append(a.Calls, b.Name), append(b.CalledBy, a.Name)
			}
		} else if src.Module != dst.Module && !seenCall["lib"+src.Module+">"+dst.Module] {
			seenCall["lib"+src.Module+">"+dst.Module] = true
			a := service(src.Module)
			a.UsesLibs = append(a.UsesLibs, service(dst.Module).Name)
		}
	}
	for _, n := range g.Nodes {
		r.Types++
		r.Roles[n.Stereotype]++
		s := service(n.Module)
		s.Types++
		if n.Stereotype == "controller" {
			s.Kind = "service"
			for _, ep := range n.Endpoints {
				route, handler, _ := strings.Cut(ep, "  → ")
				s.Endpoints++
				r.Endpoints = append(r.Endpoints, RepoEndpoint{Service: s.Name, Route: route, Handler: handler, Class: n.Name, Node: n.ID, Callers: callers[n.Module+" "+pathVarRoute(route)]})
			}
		}
		if n.Stereotype == "config" && strings.Contains(strings.Join(n.Annotations, " "), "SpringBootApplication") {
			s.Kind = "service"
		}
		total := max(1, n.EndLine-n.StartLine+1)
		r.Largest = append(r.Largest, RepoClass{Name: n.Name, Node: n.ID, File: n.File, Lines: total, Members: len(n.Members)})
		for _, m := range n.Members {
			if m.Cost != nil {
				r.Methods++
				if m.Cost.LoopIO > 0 || m.Cost.LoopDepth >= 2 || m.Cost.Complexity >= 8 {
					r.Costly = append(r.Costly, RepoMethod{Class: n.Name, Node: n.ID, Method: m.Name, File: n.File, Line: m.StartLine, Cost: *m.Cost})
				}
			}
		}
	}
	open := map[string]int{}
	for _, is := range issues {
		if is.State != "open" {
			continue
		}
		open[is.Path]++
		service(javaidx.ModuleOf(is.Path)).Open[is.Severity]++
		if is.Group == "" || is.GroupRoot {
			r.Issues[is.Kind]++
			r.Urgency[is.Urgency]++
			r.Severity[is.Severity]++
		}
	}
	churn := repo.Churn()
	for p, n := range churn {
		service(javaidx.ModuleOf(p)).Commits += 0
		if _, ok := sources[p]; ok {
			r.Hotspots = append(r.Hotspots, RepoHotspot{Path: p, Commits: n, Open: open[p], Lines: lines[p]})
			service(javaidx.ModuleOf(p)).Commits += n
		}
	}
	sort.Slice(r.Hotspots, func(i, j int) bool {
		a, b := r.Hotspots[i], r.Hotspots[j]
		return a.Commits*(a.Open+1) > b.Commits*(b.Open+1) || (a.Commits*(a.Open+1) == b.Commits*(b.Open+1) && a.Path < b.Path)
	})
	if len(r.Hotspots) > 12 {
		r.Hotspots = r.Hotspots[:12]
	}
	sort.Slice(r.Costly, func(i, j int) bool {
		a, b := r.Costly[i].Cost, r.Costly[j].Cost
		if a.LoopIO != b.LoopIO {
			return a.LoopIO > b.LoopIO
		}
		if a.LoopDepth != b.LoopDepth {
			return a.LoopDepth > b.LoopDepth
		}
		return a.Complexity > b.Complexity
	})
	r.CostlyTotal = len(r.Costly)
	if len(r.Costly) > 15 {
		r.Costly = r.Costly[:15]
	}
	sort.Slice(r.Largest, func(i, j int) bool { return r.Largest[i].Lines > r.Largest[j].Lines })
	if len(r.Largest) > 10 {
		r.Largest = r.Largest[:10]
	}
	sort.Slice(r.Endpoints, func(i, j int) bool {
		if r.Endpoints[i].Service != r.Endpoints[j].Service {
			return r.Endpoints[i].Service < r.Endpoints[j].Service
		}
		return r.Endpoints[i].Route < r.Endpoints[j].Route
	})
	sort.Slice(r.Calls, func(i, j int) bool { return r.Calls[i].From+r.Calls[i].To < r.Calls[j].From+r.Calls[j].To })
	for _, s := range services {
		if s.Files > 0 || s.Types > 0 {
			sort.Strings(s.Calls)
			sort.Strings(s.CalledBy)
			sort.Strings(s.UsesLibs)
			r.Services = append(r.Services, *s)
		}
	}
	sort.Slice(r.Services, func(i, j int) bool {
		if r.Services[i].Kind != r.Services[j].Kind {
			return r.Services[i].Kind > r.Services[j].Kind
		}
		return r.Services[i].Name < r.Services[j].Name
	})
	return r
}

var repoPathVar = regexp.MustCompile(`\{[^}]*\}`)

func pathVarRoute(route string) string {
	return repoPathVar.ReplaceAllString(strings.TrimSpace(route), "{}")
}
