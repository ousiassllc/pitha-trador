.DEFAULT_GOAL := help
.PHONY: help dev generate lint test test-race build openapi-export

help: ## コマンド一覧を表示
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

# 各ターゲットの背景（環境変数の設定理由・依存関係）は docs/environment/setup.md
# の「Makefileターゲット」節を参照。
dev: ## 開発起動（wails dev + templ watch + bun watch）
	@PITHA_STRATEGY_PATH=$(CURDIR)/config/strategy.yaml \
	PITHA_RISK_PATH=$(CURDIR)/config/risk.yaml \
	PITHA_STATIC_DIR=$(CURDIR)/static/src \
	SWAGGER_ENABLED=true \
	bunx concurrently \
		"cd cmd/desktop && wails dev" \
		"templ generate --watch" \
		"bun --cwd=static run dev"

generate: ## templ生成とフロントエンドビルド（lint/test/buildの前提）
	templ generate
	bun run --cwd static build

lint: generate ## golangci-lint と biome check
	golangci-lint run
	bun run --cwd static lint

test: generate ## go test と bun test
	go test ./...
	bun --cwd=static test

test-race: generate ## go test -race（CIのtestジョブと同じ。cgo/gccが必要）
	go test -race ./...

build: generate ## Windows向けにwails build
	cd cmd/desktop && wails build -platform windows/amd64

openapi-export: ## 起動中サーバーからdocs/api/openapi.jsonを書き出す
	@mkdir -p docs/api
	@curl -sf http://127.0.0.1:48080/api/v1/openapi.json -o docs/api/openapi.json
