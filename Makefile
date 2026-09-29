.PHONY: dev lint test build openapi-export generate

# PITHA_STRATEGY_PATH/PITHA_RISK_PATH point at the repo's own config/*.yaml
# (absolute, via $(CURDIR), since `wails dev` runs with cmd/desktop as its
# cwd) so editing either file and restarting `make dev` picks up the change
# immediately: internal/bootstrap.Run's env-var step (issue #59) takes
# precedence over its compiled-in embedded default, which is only a
# build-time snapshot and would otherwise make `make dev` no longer
# reflect config/risk.yaml edits without a rebuild.
#
# PITHA_STATIC_DIR similarly points at static/src so internal/router's
# `/static/...` route serves straight from disk instead of its go:embed
# snapshot: `wails dev`'s file watcher only rebuilds the Go binary on `.go`
# changes by default, so without this override `bun run dev`'s esbuild/
# Tailwind watch output would never become visible short of restarting
# `make dev` (issue #59's embed made this a regression - static assets used
# to be read from disk on every request, live, before that fix).
dev:
	@PITHA_STRATEGY_PATH=$(CURDIR)/config/strategy.yaml \
	PITHA_RISK_PATH=$(CURDIR)/config/risk.yaml \
	PITHA_STATIC_DIR=$(CURDIR)/static/src \
	bunx concurrently \
		"cd cmd/desktop && wails dev" \
		"templ generate --watch" \
		"bun --cwd=static run dev"

# templ generate produces *_templ.go and `bun run --cwd static build`
# produces static/src/dist/{css,js} - both are `.gitignore`'d, and
# static/src/embed.go's `//go:embed dist vendor` (issue #59) refuses to
# compile at all until dist/ exists with actual files in it, so `go
# vet`/golangci-lint/go test/wails build all fail outright on a clean
# checkout without this having run first (or silently run against a
# stale dist/*_templ.go left over from a previous `make dev`/build
# otherwise, as happened with issue #74's `internal/web/style_test.go`
# locally testing green against Tailwind CSS output older than the
# .templ source it was meant to catch drift against). CI's lint/test/
# build jobs each run these two steps first for the same reason; `lint`/
# `test`/`build` below depend on this target so every local invocation
# - and lefthook's pre-commit/pre-push hooks, which shell out to `make
# generate` - stays in the same order.
generate:
	templ generate
	bun run --cwd static build

lint: generate
	golangci-lint run
	bunx biome check static/

test: generate
	go test ./...
	bun --cwd=static test

build: generate
	cd cmd/desktop && wails build -platform windows/amd64

openapi-export:
	@mkdir -p docs/api
	@curl -sf http://127.0.0.1:48080/api/v1/openapi.json -o docs/api/openapi.json
