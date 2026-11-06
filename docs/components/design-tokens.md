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

## 形・余白・線（issue #942）

- 角丸は `@theme` の `--radius-md|lg|xl` をガイドの `--r-md|lg|xl`（8 / 12 / 16px）に再定義し、`rounded-md`（入力・ボタン・入れ子パネル）・`rounded-lg`（カード・パネル）・`rounded-xl`（モーダル）をそのまま使う。Lit の `static styles` は 0.5rem
- 影はモーダルだけ（`shadow-modal` = `--shadow-modal` 0 1px 2px rgba(0,0,0,.06)）。カード・ツールチップ・トーストは 1px の `border-border` で区切り、`shadow*` を付けない
- 表のセルは上下 10px / 左右 12px（`px-3 py-2.5`）、compact は上下 6px / 左右 8px（`px-2 py-1.5`）。余白は 4px 基準（20px・2px・6px の `px-5` `py-0.5` `mt-1.5` は使わない。表の compact の 6px だけ例外）
- ガターは `px-4 lg:px-6`（モバイル 16px / デスクトップ 24px）。セクション間は 48px（`mt-12` / `mb-12`）
- 空状態は `border border-dashed border-border`・`px-4 py-6`・`text-center text-mute`。ハッシュ到達時の強調は `target:ring-1 target:ring-mute`

## アイコン（issue #943）

UI アイコンは [Iconoir](https://iconoir.com)（MIT、iconoir 7.12.2 の regular）の SVG を使う。文字記号（×・▲▼）で代用しない。24×24 の viewBox・`stroke-width="1.5"`・`stroke="currentColor"`・装飾なので `aria-hidden`（名前はボタンの `aria-label`）。サイズは 16（`h-4 w-4`）/ 20（`h-5 w-5`）/ 24。templ は `internal/web/atoms/icon.templ`（`IconXmark`）、Lit は `static/src/components/lib/icons.ts`（`navArrowUp` / `navArrowDown`）に同じパスを置く。足すときは両方に同じパスを足す。

## モーション（issue #944）

トークンは `static/src/components/lib/motion.ts`（fast 120 / base 180 / slow 280 / fade-in 320 / fade-out 240ms、`--ease-out` = cubic-bezier(0.2,0.8,0.2,1)、`--ease-in` = cubic-bezier(0.4,0,1,1)）。`app.css` の `@theme` が `--ease-out` をガイドの値にする（Tailwind 既定と異なる）。`prefers-reduced-motion: reduce` では移動・リップルを出さず、必要な opacity のフェードも 100ms 以下にする。

- ボタン・閉じるの押下リップル: `data-ripple` を付けた要素に `static/src/components/ripple/pitha-ripple.ts`（`layout` が読む）が、押した点から広がって消える円（280ms ease-out）を描く。`atoms.Button` / `ButtonLink`・トーストの閉じるは templ が付け、Lit のボタンは `data-ripple` を付ける（`lib/styles.ts` の `buttonStyles` が `position: relative; overflow: hidden`）。ドキュメントのリスナーが `composedPath()` で shadow root 内も拾う
- ツールチップ（`atoms.HelpMark`・Lit の `renderHelpMark`・`scanner-view.ts`）: `opacity` を 320ms ease-out でフェード（`visibility` は同時に切り替え、閉じるときはフェードが終わってから隠す）
- トースト（`pitha-htmx-errors.ts`）: 出現は opacity と 8px の `translateY` を 320ms ease-out、退出は opacity を 240ms ease-in で待ってから `remove()`。Web Animations API が無ければ即時

## 部品・オーバーレイ（issue #945）

- ボタンは主（`bg-accent text-bone`）/ サブ（透明＋1px `border-border`）/ ゴースト / 危険（`bg-danger text-bone`）の 4 種、SemiBold・`rounded-md`・disabled は opacity .4・小は高さ 32px（`min-h-8`）。テキストリンクは Regular・`underline decoration-mute`
- 即時反映の on/off は `atoms.Switch`（OS の checkbox 見た目を出さない）。保存ボタンで確定するフォーム内の同意・選択は checkbox のまま
- モーダルは `rounded-xl`・`shadow-modal`・スクラム（`--scrim`）と本体が一緒にフェード（`app.css` の `dialog[data-modal]`、開 320ms ease-out・閉 240ms ease-in、`prefers-reduced-motion` では 100ms）。確認・破壊的操作（`hx-confirm`、Kill）は `molecules.ConfirmDialog`
- トーストは画面下中央・端から 24px・高さ 40px のピル（`bg-ink text-bone`）・同時 1 つ（新しいものが置き換える）・3 秒。エラー詳細とフォーム検証はトーストにせず `atoms.InlineAlert`（フォーム内のエラー欄）に出す
- z-index は `--z-dropdown` 100（ツールチップ）/ `--z-toast` 500（`z-[var(--z-dropdown)]` のように使う）
- チャートの色は `lib/chart-theme.ts` がトークンを読んで渡す（陽線・買い = `--success`、陰線・売り = `--danger`、VWAP = `--info`、出来高 = `--mute` 50%、精度線 = `--ink`、完全較正線 = `--mute`、軸・グリッド = `--border`）。ヒートマップは `--ink` を `--surface` に混ぜた濃淡（4〜30%）と `--ink` 文字、数値併記。サンプルなしは `--bone` と破線枠
