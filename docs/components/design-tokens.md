# デザイントークン

`overview.md` §1 のスタイリング方針。色は Ousiass 共通デザインガイド（brand-guide）のトークンだけを使う（issue #940）。`static/src/css/app.css` が `:root`（Light）と `[data-theme="dark"]`（Dark）にコアトークン（`--ink` `--bone` `--mute` `--surface` `--border` `--accent` `--accent-soft` `--focus`）と状態色（`--success` `--warning` `--danger` `--info`）を定義し、`@theme inline` で Tailwind のクラス（`text-ink` `bg-bone` `bg-surface` `border-border` `bg-accent` `text-mute` `outline-focus` `text-success` 等）にする。Tailwind 既定のパレット（`slate-*` `blue-*` など）は `--color-*: initial` で無効にしてあり、使うとクラスが生成されず `internal/web/style_test.go` が失敗する。

- ステータスのピル・インラインアラートは `border tone-success|warning|danger|info|mute`（文字＝状態色、枠＝40%、地＝10%。`app.css` の `@utility tone-*`）に形（角丸・padding）だけを足す。状態は文言でも伝える
- Lit のShadow DOMはCSSカスタムプロパティが継承されるため、`static styles` は hex を書かず `var(--ink)` `var(--border)` 等を使う
- フォーカスリングは `focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`（Lit 側は `outline: 2px solid var(--focus)`）

## フォントとタイポグラフィ（issue #941）

- 本文は `Sora, "Noto Sans JP", sans-serif`（`font-sans`）、コードと桁を揃える数値は `"JetBrains Mono", ui-monospace, monospace`（`font-mono`）。いずれも SIL OFL
- フォントは `@fontsource-variable/{sora,noto-sans-jp,jetbrains-mono}`（`static/package.json` の devDependencies、版は `bun.lock`）から `static/esbuild.config.mjs` が `static/src/dist/fonts/` へ複製し、`fonts.css`（`@font-face`）を `layout` の `<head>` で読む。同一オリジンなので CSP は `font-src 'self'` のまま。canvas（`pitha-price-chart`）は CSS を継承しないため `createChart` の `layout.fontFamily` に明示する
- 太さは Regular(400) と SemiBold(600) だけ。SemiBold は見出し・ボタン・表の見出し行に限る。ナビ・リンク・ラベル・値に `font-medium` / `font-bold` / 本文中の `font-semibold` を使わない（現在のナビ項目は `text-ink` の色で示す）
- 表本文は `text-sm`、見出し行は `text-xs font-semibold`。数値列は `text-right font-mono tabular-nums`
- 行間は `@theme` の `--text-*--line-height`（xs 1.6 / sm 1.7 / base 1.8 / lg 1.7）と `body { line-height: 1.8 }`。サイズは Tailwind の 12/14/16/18/24px を使い、スケール外の `text-xl`（20px）・`text-3xl`（30px）・`text-[10px]` は使わない
