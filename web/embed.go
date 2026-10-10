// Package web embeds the built frontend so the server ships as one binary.
// dist/ is the Vite build output (web/dist); it must be built before `go build`
// (see the Makefile/README), as go:embed reads it at compile time.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Index returns the embedded index.html shell served for / and SPA deep links.
func Index() ([]byte, error) {
	return distFS.ReadFile("dist/index.html")
}

// DistFS returns the embedded dist/ tree rooted at its top, used to serve the
// hashed build assets under /assets.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
