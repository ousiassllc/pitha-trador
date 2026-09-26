.PHONY: dev lint test build openapi-export

dev:
	@bunx concurrently \
		"cd cmd/desktop && wails dev" \
		"templ generate --watch" \
		"bun --cwd=static run dev"

lint:
	golangci-lint run
	bunx biome check static/

test:
	go test ./...
	bun --cwd=static test

build:
	cd cmd/desktop && wails build -platform windows/amd64

openapi-export:
	@mkdir -p docs/api
	@curl -sf http://localhost:8080/api/v1/openapi.json -o docs/api/openapi.json
