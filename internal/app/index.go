package app

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/abxengg/codist-java"
)

func wanted(p string) (isJava, isBuild bool) {
	b := path.Base(p)
	isJava = strings.HasSuffix(p, ".java")
	isBuild = b == "pom.xml" || b == "build.gradle" || b == "build.gradle.kts" ||
		strings.HasPrefix(b, "application.") && strings.Contains(p, "src/main/resources/")
	return
}

// loadIndex reads every Java file and the build and Spring files at a commit. The checked-out
// branch is read from disk, which is fastest; another branch is read from git in one batch.
func (a *App) loadIndex(ref, sha string) *java.Index {
	start := time.Now()
	files, err := a.repo.Files(sha)
	if err != nil {
		slog.Warn("cannot list files", "ref", ref, "err", err)
		return java.Build(nil, nil)
	}
	var keep []string
	for _, p := range files {
		if j, b := wanted(p); (j || b) && !a.cfg.Ignored(p) {
			keep = append(keep, p)
		}
	}
	var contents map[string][]byte
	if ref == "HEAD" {
		contents = map[string][]byte{}
		for _, p := range keep {
			if src, err := os.ReadFile(filepath.Join(a.repo.Root, filepath.FromSlash(p))); err == nil {
				contents[p] = src
			}
		}
	} else {
		contents = a.repo.ReadAll(sha, keep)
	}
	sources, other := map[string][]byte{}, map[string][]byte{}
	for p, src := range contents {
		if j, _ := wanted(p); j {
			sources[p] = src
		} else {
			other[p] = src
		}
	}
	ix := java.Build(sources, other)
	slog.Info("indexed", "ref", ref, "types", len(ix.Types), "modules", len(ix.Modules), "took", time.Since(start).Round(time.Millisecond))
	return ix
}

// index returns the code of what the interface shows, rebuilding it when that commit changes.
func (a *App) index() *java.Index {
	ref := a.viewRef()
	sha := a.repo.Resolve(ref)
	a.ixMu.Lock()
	defer a.ixMu.Unlock()
	if a.ix == nil || a.ixSHA != sha {
		a.ix, a.ixSHA = a.loadIndex(ref, sha), sha
	}
	return a.ix
}

// source reads a file as the interface shows it: at a commit, or from the view.
func (a *App) source(path, sha string) ([]byte, bool) {
	if sha == "" && !a.viewing() {
		b, err := os.ReadFile(filepath.Join(a.repo.Root, filepath.FromSlash(path)))
		return b, err == nil
	}
	if sha == "" {
		sha = a.viewRef()
	}
	return a.repo.FileAt(sha, path)
}
