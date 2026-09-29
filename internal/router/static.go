package router

import (
	"net/http"
	"os"

	staticassets "github.com/ousiassllc/pitha-trador/static/src"
)

// EnvStaticDir names the env var that, when set to an existing directory,
// makes `/static/...` serve straight from disk instead of the embedded
// staticassets.FS snapshot below. `make dev` sets this to static/src so
// `bun --cwd static run dev`'s esbuild/Tailwind watch rebuilds are visible
// on the next page reload with no Go rebuild required. `wails dev`'s
// built-in file watcher only rebuilds/relaunches the Go binary on changes
// to files with a `.go` extension by default (Wails "Application
// Development" guide), so it does not react to static/src/dist's .js/.css
// output changing - an embed-only static handler would keep serving a
// stale compile-time snapshot for the rest of the `make dev` session
// without this override.
const EnvStaticDir = "PITHA_STATIC_DIR"

// staticFS serves `/static/...`. It falls back to staticassets.FS - dist/
// (esbuild/Tailwind output, components/overview.md §2) and vendor/
// (htmx.min.js) - embedded at compile time, so the same bytes ship inside a
// packaged `wails build`/`go build ./cmd/server` .exe regardless of the
// process's cwd or the source tree's location (architecture/overview.md
// §9). See EnvStaticDir above for the dev-mode disk-backed override this
// embedded snapshot is a fallback from.
func staticFS() http.FileSystem {
	if dir := os.Getenv(EnvStaticDir); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return http.Dir(dir)
		}
	}
	return http.FS(staticassets.FS)
}
