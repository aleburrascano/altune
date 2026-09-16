// Package webui embeds the built Overseer SPA (services/overseer/web, Vite build)
// so the Go binary serves it from a single container — the "outlives-the-app"
// invariant: overseer up ⇒ the site loads, regardless of go-api. The Vite build
// writes its dist here (build.outDir) and go:embed folds it into the binary. A
// committed placeholder dist/index.html keeps `go build` green even in a checkout
// where the frontend has not been built (the Go-only CI jobs); the real build and
// the Docker image overwrite it with the full app.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded SPA rooted at the dist directory, ready to serve as the
// static file root.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
