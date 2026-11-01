// Package webui carries the built UI inside the binary, so one file is all anyone needs to run Codist.
// `make build` copies web/dist here before compiling.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// FS is the built UI, or nil in a binary compiled without it (go run during development).
func FS() fs.FS {
	dist, err := fs.Sub(files, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return nil
	}
	return dist
}
