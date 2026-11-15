# 環境構築: AI のプロバイダーと役割ごとのモデルの設定（issue #1080）

Settings「外部連携・AI」ページ（`/settings/integrations`）で、AI を呼ぶ先（プロバイダー）と、役割ごとのモデルを設定する手順。設計は`docs/architecture/overview/integrations-ai-providers.md`。

## 画面の並び

1. **AI のプロバイダー**: 接続を持つ 2 つのカード。
   - 独自 API（Jev）: `JEV_API_KEY`（必須）・`JEV_BASE_URL`（任意）・`JEV_MODEL`（任意。「役割ごとのモデル」で Jev にモデルの指定がなければこれを使う）。
   - OpenRouter: `OPENROUTER_API_KEY`・`OPENROUTER_BASE_URL`（どちらも任意。URL が空なら公開エンドポイント）。
2. **役割ごとのモデル**: Jev（売買判断）・Sol（改善提案）・Opus（提案の検証）・Luna（ニュース分類）の各行で、プロバイダーとモデルを選んで保存する。「既定に戻す」で保存した値を消す。
3. **そのほかの接続先**: Luna・Sol・Opus の役割ごとの上書き（独自 API を選んだ役割が使う）・ニュースフィード・Slack・（MCP などのカード）。

## 既定と反映

- 何も保存しなければ、全ての役割が今までと同じ接続先・キー・モデルで動く。環境変数ではなく`secrets`の既存のキー（`JEV_*`・`SOL_*`・`OPUS_*`・`LUNA_*`・`OPENROUTER_*`）をそのまま読み、書き換えない。
- OpenRouter を選ぶ役割にはモデル名（例: `provider/model`）が必要。OpenRouter のキーが無いと、その役割は動かず見送られる（独自 API のキーには切り替わらない）。
- 保存・既定に戻した内容の反映には**アプリの再起動が必要**。画面に、保存した設定と、最後の起動で実際に使い始めた組、最近の変更の履歴が出る。
- Jev（売買判断）のプロバイダー・モデルは**この画面でだけ**変えられる（MCP からは変えられない）。トライアルのアームのプロバイダー（`openrouter`／`custom`）は、モデル比較トライアルのアーム（JSON）に書く。
- 画面にキー・URL の値は出ない（設定済み／未設定の表示だけ）。

## ChatGPT のサブスク（Codex CLI のログイン。トライアルのアームだけ。issue #1081）

モデル比較トライアルのアームの`provider`に`codex_chatgpt`を指定すると、公式の Codex CLI（`codex exec`）で ChatGPT のサブスクの枠から答えを受け取る。**既定はオフ**で、AI の役割（Jev・Sol・Opus・Luna）には選べない。設計・調べた結果は`architecture/overview/integrations-codex-chatgpt.md`。

1. **ログインは人が自分でする**（pitha もエージェントもログインしない）。お試し専用の空のディレクトリを作り、そこを`CODEX_HOME`にして、公式の手順（ヘッドレスならデバイスコード）で`codex login`する。人の普段の Codex の設定（`~/.codex`）とは分ける。`auth.json`はパスワードと同じに扱い、コミット・チケット・チャットに貼らず、pitha にも読ませない。
2. pitha を起動する環境に環境変数`PITHA_CODEX_HOME=<そのディレクトリ>`（お試し専用の`CODEX_HOME`。必要なら`PITHA_CODEX_BIN`＝Codex CLI の実行ファイル。未設定は PATH 上の`codex`。どちらも秘密ではない。`internal/bootstrap/airolewire`が読む）を設定する。`PITHA_CODEX_HOME`が未設定なら Codex のアームは呼ばれない。`CODEX_API_KEY`・`OPENAI_API_KEY`は子プロセスに渡さない。
3. ChatGPT のアカウント側で、**クレジットの残高を 0 のまま・自動リロードをオフ**にしておく（枠を使い切った後にクレジットが自動で使われるのを、CLI 側では止められない）。API キー（`CODEX_API_KEY`・`OPENAI_API_KEY`）は使わない（子プロセスには渡さない）。
4. Settings「モデル比較トライアル」で「ChatGPT サブスク（Codex）を有効にする」をオンにし、1 日の呼び出し回数・トークン数の上限を決め、アーム（例 `{"id":"cx-sol","provider":"codex_chatgpt","model":"<使えるモデルの slug>","prompt_version":"trader-v3"}`）を保存する。スイッチがオフの間はアームを保存できない。
5. 画面（と MCP の`jev_trial_cost`）にログイン状態・今日の回数とトークン・止まっている理由と再開時刻が出る。上限に届いたとき・失敗したときは、再試行せずにプロバイダー全体が止まる（再開時刻が分かればその時刻まで、分からなければその日いっぱい）。

**利用条件の最終判断は悠太さん**（ChatGPT の個人向け利用規約・Usage Policies。Codex の出力は Paper のお試し専用で、ライブの発注には使わない）。使えるモデル・`--output-schema`との組み合わせ・1 回あたりの使用量は、ログインして確かめるまで**未確認**。
