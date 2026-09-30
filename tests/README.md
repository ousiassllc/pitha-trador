# tests/

クロスレイヤー・E2Eテストを配置するディレクトリの雛形。現状E2Eテストは未整備（Playwright設定・テスト・CIジョブなし）で、導入時はPlaywrightを想定する（`docs/components/runtime.md` §9参照）。

Litコンポーネントの単体テストは`static/src/components/**/*.test.ts`（`bun test` + happy-dom）にあり、このディレクトリには含めない。

Goのユニットテスト（`_test.go`）は対象パッケージと同じディレクトリに配置する（Go標準の配置規約）ため、このディレクトリには含めない。

具体的なテスト実装は各後続サブスコープで追加する。
