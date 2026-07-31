package webserver

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist holds the built web/ frontend (see web/vite.config.ts's outDir,
// which writes straight into this directory). It's committed to the repo
// like the npm/ wrapper's other build artifacts, so `go build
// ./cmd/sessionhub` always succeeds without requiring Node — run `npm run
// build` inside web/ after any frontend change to refresh it.
//
//go:embed all:dist
var distFS embed.FS

// staticHandler serves the SPA build and falls back to index.html for any
// path it doesn't recognize, so client-side routes (and simple reloads on a
// deep link) resolve to the app shell instead of a 404.
func staticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "web panel assets not embedded: "+err.Error(), http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, statErr := fs.Stat(sub, path); statErr != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
