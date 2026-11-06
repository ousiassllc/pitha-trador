# デザイントークン

`overview.md` §1 のスタイリング方針。色は Ousiass 共通デザインガイド（brand-guide）のトークンだけを使う（issue #940）。`static/src/css/app.css` が `:root`（Light）と `[data-theme="dark"]`（Dark）にコアトークン（`--ink` `--bone` `--mute` `--surface` `--border` `--accent` `--accent-soft` `--focus`）と状態色（`--success` `--warning` `--danger` `--info`）を定義し、`@theme inline` で Tailwind のクラス（`text-ink` `bg-bone` `bg-surface` `border-border` `bg-accent` `text-mute` `outline-focus` `text-success` 等）にする。Tailwind 既定のパレット（`slate-*` `blue-*` など）は `--color-*: initial` で無効にしてあり、使うとクラスが生成されず `internal/web/style_test.go` が失敗する。

- ステータスのピル・インラインアラートは `border tone-success|warning|danger|info|mute`（文字＝状態色、枠＝40%、地＝10%。`app.css` の `@utility tone-*`）に形（角丸・padding）だけを足す。状態は文言でも伝える
- Lit のShadow DOMはCSSカスタムプロパティが継承されるため、`static styles` は hex を書かず `var(--ink)` `var(--border)` 等を使う
- フォーカスリングは `focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`（Lit 側は `outline: 2px solid var(--focus)`）
