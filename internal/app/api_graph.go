package app

import (
	"net/http"
	"sync"
	"time"

	"github.com/AbiXnash/codist-java"
	"github.com/AbiXnash/codist/internal/web"
)

func (a *App) graph(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	nodes := make([]java.GraphNode, 0, len(ix.Types))
	for _, t := range ix.Types {
		nodes = append(nodes, java.GraphNodeOf(t))
	}
	web.JSON(w, map[string]any{"nodes": nodes, "edges": java.GraphEdges(ix), "source": a.ixSHA, "builtAt": time.Now().UnixMilli(), "parseErrors": 0})
}

// callsCache keeps the method calls of the current index; finding them parses every file again.
var callsCache struct {
	sync.Mutex
	sha   string
	calls []java.MethodCall
}

func (a *App) calls(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	callsCache.Lock()
	if callsCache.sha != a.ixSHA || callsCache.calls == nil {
		callsCache.sha, callsCache.calls = a.ixSHA, java.BuildCallGraph(ix.Sources, ix.Types).Calls()
	}
	calls := callsCache.calls
	callsCache.Unlock()
	web.JSON(w, calls)
}
