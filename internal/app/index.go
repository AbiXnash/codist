package app

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"codist/internal/java"
)

// loadIndex reads every tracked Java file and the build and Spring files at HEAD. Files are read
// from disk, which is much faster than asking git for each one; HEAD's file list keeps untracked
// files out.
func (a *App) loadIndex() *java.Index {
	start := time.Now()
	files, err := a.repo.Files("HEAD")
	if err != nil {
		slog.Warn("cannot list files", "err", err)
		return java.Build(nil, nil)
	}
	sources, other := map[string][]byte{}, map[string][]byte{}
	for _, p := range files {
		if a.cfg.Ignored(p) {
			continue
		}
		b := path.Base(p)
		isJava := strings.HasSuffix(p, ".java")
		isBuild := b == "pom.xml" || b == "build.gradle" || b == "build.gradle.kts" ||
			strings.HasPrefix(b, "application.") && strings.Contains(p, "src/main/resources/")
		if !isJava && !isBuild {
			continue
		}
		src, err := os.ReadFile(filepath.Join(a.repo.Root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		if isJava {
			sources[p] = src
		} else {
			other[p] = src
		}
	}
	ix := java.Build(sources, other)
	slog.Info("indexed", "types", len(ix.Types), "modules", len(ix.Modules), "links", len(ix.Links), "took", time.Since(start).Round(time.Millisecond))
	return ix
}

// index returns the current index, building it the first time.
func (a *App) index() *java.Index {
	a.ixMu.Lock()
	defer a.ixMu.Unlock()
	head := a.repo.Head()
	if a.ix == nil || a.ixHead != head {
		a.ix, a.ixHead = a.loadIndex(), head
	}
	return a.ix
}
