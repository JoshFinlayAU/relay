// Package web embeds the built React SPA (web/dist) into the binary.
package web

import (
	"embed"
	"io/fs"
)

// distFS holds the built SPA. `all:` includes files whose names start with '_'
// (Vite emits assets under _app / with leading-underscore chunk names sometimes).
//
//go:embed all:dist
var distFS embed.FS

// Dist returns the SPA file system rooted at dist/.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
