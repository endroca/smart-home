// Package web contains the embedded hub UI assets.
package web

import (
	"embed"
	"io/fs"
)

// embedded holds the compiled Svelte app (hub/web/dist). dist/ is committed so
// the binary can be built on the Pi without Node.
//
//go:embed dist
var embedded embed.FS

// FS is the UI filesystem rooted at dist/, so index.html and assets/ sit at "/".
var FS = mustSub(embedded, "dist")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("web: " + err.Error())
	}
	return sub
}
