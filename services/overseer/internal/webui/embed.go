// Package webui embeds the built Overseer SPA (services/overseer/web, Vite build)
// so the Go binary serves it from a single container — the "outlives-the-app"
// invariant: overseer up ⇒ the site loads, regardless of go-api. The Vite build
// writes its dist here (build.outDir) and go:embed folds it into the binary.
//
// The dist contents (index.html + hashed assets) are gitignored and rebuilt in
// CI/Docker; only an empty dist/.gitkeep is committed, so `go:embed all:dist`
// always matches at least one file and `go build` stays green in a Go-only
// checkout where Vite has not run. Nothing the build generates is tracked, so a
// local build never dirties git — no committed placeholder to accidentally commit.
// A checkout with no real build embeds only .gitkeep: FS() succeeds but carries no
// index.html, so the shell serves 404 at "/" (App logs "embedded SPA unavailable",
// not fatal — the JSON API still serves).
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
