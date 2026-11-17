# アーキテクチャ設計: ChatGPT のサブスク（公式 Codex CLI のログイン）をトライアルのアームに使う（issue #1081）

`docs/architecture/overview/integrations-jev-trial.md`（意思決定モデル比較のトライアル）の分割章。アームの`provider`に`codex_chatgpt`を足し、公式の Codex CLI（`codex exec`）で ChatGPT のサブスクの枠から答えを受け取る。AI の役割（`internal/config/airole`。Jev・Sol・Opus・Luna）には**足さない**（トライアルのアームだけ。ライブの Jev・Risk・発注・Paper ガード・既定値・シャドーは変えていない）。テーブルは`er/tables-jev-trial.md`、MCP の読み方は`paper-results-mcp-jev-trial.md`。

## 1. 調べた結果（2026-11-15。公式の文書と公開情報の読み取りだけ。ログイン・認証つきの実行・キーの投入・有料の呼び出しはしていない）

| 項目 | 判定 | 要点（根拠） |
|------|------|------|
| 利用条件の範囲（自作アプリから呼ぶ・自動実行・投資判断・並列） | **制限あり** | ChatGPT ログインで使う Codex には ChatGPT の個人向け利用規約が適用される（[Codex を ChatGPT プランで使う](https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan)）。`codex exec`をスクリプト・CI から ChatGPT ログインで動かすのは文書にある使い方だが、信頼できる非公開の環境で、**1 台または直列のジョブだけが 1 つの`auth.json`を使う**条件つき（[CI の ChatGPT 認証](https://learn.chatgpt.com/docs/auth/ci-cd-auth)）。個人向け利用規約の「プログラム的な出力の抽出」の禁止が`codex exec`の出力に及ぶか、金融の高リスクな自動化（Usage Policies 2026-11-20 発効）に当たるかは、文書だけでは**線引きが不明**。最終判断は悠太さん（2026-11-15 13:07 JST に「規定は確認した。問題ない」と回答済み） |
| 選べるモデル | **可**（実際のアカウントで使えるモデルは**ログインしないとわからない**） | `codex exec -m <slug>`（[モデル](https://learn.chatgpt.com/docs/models)）。温度・top_p などは設定リファレンスに無い（指定できない）。モデルの入れ替わりが速い（退役したモデルは失敗する） |
| 非対話で決まった形の答え | **可**（ChatGPT ログインとの組み合わせは**ログインしないとわからない**） | `--json`（JSONL のイベント）・`--output-schema`・`-o`（[非対話モード](https://learn.chatgpt.com/docs/non-interactive-mode)）。`exec`がスキーマ違反を検出するかは文書に無いので、pitha 側で必ず再検証する |
| 上限にかかったことの見分け方 | **制限あり** | `exec`の JSONL の`error`・`turn.failed`は**文面だけ**で機械可読なコードが無い。構造化コード（`usageLimitExceeded`・再開時刻）は app-server 側にある（[app-server](https://learn.chatgpt.com/docs/app-server)）。終了コードの違いは文書に無い（公開ソースでは失敗は 1） |
| 従量課金への切り替わり | **制限あり** | API キー課金へは自動では移らない。ただし ChatGPT クレジットは残高があれば**自動で消費**され、CLI にそれを禁じる設定は無い（[クレジット](https://help.openai.com/en/articles/12642688-using-credits-for-flexible-usage-in-chatgpt-freeplusgo)）。止めるのはアカウント側（残高 0・自動リロードをオフ）で、pitha は強制できない。`CODEX_API_KEY`が環境にあると`exec`はそのキー（標準 API 料金）で動く |

ログインしないとわからない項目（使えるモデル・`--output-schema`との組み合わせ・1 回の所要時間と枠の減り方・上限時の`turn.failed`の全文と終了コード・専用`CODEX_HOME`と`--ignore-user-config`の組み合わせ）は**未確認**で、悠太さんが専用の`CODEX_HOME`に自分でログインして確かめる。

## 2. 設計（要点）

### 2.1 既定オフと有効化

- 3 段の関門がすべて要る: トライアル本体のスイッチ`jev.trial.enabled`（Paper のみ）・Codex 用のスイッチ`jev.trial.codex.enabled`（**既定 0**）・専用の`CODEX_HOME`（環境変数`PITHA_CODEX_HOME`。未設定なら`Configured()`が false で**呼ばない**。人の Codex の設定（`~/.codex`）は使わない）。
- `codex_chatgpt`のアームは、Codex のスイッチがオフの間は**保存できない**（Settings 画面の同じフォームでスイッチもオンにしたときだけ保存できる。`jevtrial.Service.Save`）。保存済みでスイッチがオフに戻れば、呼ばずに`provider_disabled`の見送りを残す。
- スイッチ・上限・停止記録・ログイン状態のキー（`jev.trial.codex.*`）は Settings 画面だけが書ける。MCP の`settings_set`・自己改善ループ・汎用の運用設定からは書けない（`isolationflow`のテスト）。

### 2.2 費用の数え方（USD では数えない）

- `cost_source=subscription`（マイグレーション000058）。`cost_usd`は NULL、`counted_cost_usd`は **0**（DB の CHECK が固定）。成功も失敗も同じ。**USD の上限（全体・アーム）には 0 として入り**、他のアームの数字を汚さない。`unknown`の最悪単価（0.24 USD/百万トークン）は使わない。
- 代わりに**プロバイダー単位**の上限を**呼び出し回数**と**トークン数（入力＋出力）**で別に持つ（既定 30 回・500,000 トークン／日（JST）。`jev.trial.codex.daily_call_cap`・`daily_token_cap`）。保存済みの行（`provider='codex_chatgpt'`）の合計で判定し、届いたらその日いっぱい止める（`call_cap`・`token_cap`）。Codex の呼び出しは 1 本ずつで行を保存してから次に進むので、予約は要らない。**上限に届く呼び出しが最後の 1 回になる（その 1 回のトークン数は呼ぶ前に分からないので、超え得る幅は 1 回分）**。
- トライアル全体の USD の停止（全体の費用上限・OpenRouter の 402）は、Codex のアームを**止めない**（USD の話だから）。

### 2.3 上限・失敗での停止（15 分で呼び直さない）

- 判定は **`turn.failed`と終了コードだけ**（`codexwire`）。成功は「終了コード 0 かつ`turn.completed`があり`turn.failed`が無い」。`error`イベントだけでは失敗にしない（再試行中にも出うる）。
- 失敗（`turn.failed`・非 0 終了・`turn.completed`なし・起動できない・タイムアウト）は**既定で止める**: **プロバイダー全体**（全 Codex アーム）を止め、**再試行しない**。止める時刻は、使用量の上限の文面に一致し再開時刻が読めればその時刻、**読めなければ JST のその日いっぱい**（`jev.trial.codex.halt`。メモリにも保持するので記録の書き込みに失敗しても止まる）。429/529 用の 15 分の待機は Codex には使わない。
- **「上限」と表示するのは文面が一致したときだけ**（`codexwire.MatchUsageLimit`。`usage_limit`）。一致しなければ`failed`（画面・MCP・行の`failure_code`は`codex_failed`／`timeout`）。文面（エラー本文）は**保存・ログ・画面・MCP のどこにも出さない**（行の`error_excerpt`は固定の語）。
- 答えが検証に通らない（`invalid_response`）のは CLI が動いた場合なので、プロバイダーは止めず、アームの失敗回数（1 日 20 回）に数える。
- 見送りの理由（`jev_trial_skips`）: `provider_disabled`・`not_logged_in`・`stopped_usage_limit`・`stopped_provider_failed`・`daily_cap_calls`・`daily_cap_tokens`・`provider_busy`。

### 2.4 同時 1 本・プロセスの扱い・認証

- **Codex のアームは同時に 1 本だけ**: 1 つのライブ判断の Codex アームは 1 つのゴルーチンで順に動かし、前の判断の Codex がまだ動いていれば**待たずに**`provider_busy`で見送る（OpenRouter のアームは待たされない）。アームごとの最短間隔は 10 秒（`CodexMinCallInterval`）。
- 子プロセスは**自前のプロセスグループ**で起動し、コンテキストの終了（タイムアウト・`Close`）で**グループごと SIGKILL**する（Windows は`taskkill /T`）。1 回の上限 150 秒（未計測の仮置き）。
- 起動は空の一時ディレクトリで`codex exec --json --output-schema <file> --ephemeral --ignore-user-config --ignore-rules --skip-git-repo-check --sandbox read-only -c web_search="disabled" -c project_doc_max_bytes=0 -m <model> -`（プロンプトは stdin。標準エラーは読まない）。組み合わせの実動作は**未確認**。
- 子プロセスの環境は**許可リスト**（`PATH`・`HOME`・ロケール・`TMPDIR`など）と、専用の`CODEX_HOME`だけ。`CODEX_API_KEY`・`OPENAI_API_KEY`・`CODEX_ACCESS_TOKEN`・`OPENROUTER_API_KEY`は、親の環境にあっても**渡らない**（テストで固定）。
- **`auth.json`を開かない**。pitha はトークンを持たず、ログイン状態は`codex login status`の終了コードと短い出力の見出し（`chatgpt`を含み`api key`を含まない）だけで判定し、出力は保存しない。状態は`chatgpt`／`logged_out`／`other_auth`（API キーのログインなど。従量課金になるので**使わない**）／`unavailable`。`chatgpt`以外は呼ばない（`not_logged_in`）。

### 2.5 質問の形

Codex に System One は無いので、FR-TRADER-1 の 6 問（`jev.TraderQuestionsFor`）をプロンプトに展開し、`--output-schema`で同じ項目を返させ、System One の答えの形に直して`systemone.DecodeTrial`で OpenRouter のアームと**同じ厳しさ**で検証する（質問に答えない＝`null`は拒否として`refused`に入る）。プロトコルの版は`codex-exec-v1`（`codexwire`）。**モデルの差に、呼び出しのプロトコルの差が混ざる（交絡）**ので、比べるときに別に扱う。Codex はエージェントなのでツールを使いうる（完全に禁じるフラグは文書に無い）。read-only サンドボックス・空の一時ディレクトリ・プロンプトの指示で抑える。

### 2.6 画面と MCP

- Settings「モデル比較トライアル」に、Codex のスイッチ・上限・今日の回数とトークン・ログイン状態・止まっている理由と再開時刻を出す（`internal/web/organisms/jevtrial/codex.templ`）。
- MCP の`jev_trial_cost`: `per_arm[].subscription`（呼び出し数・トークン。USD の数字には入らない）と`limits.codex`（`enabled`・`login.state`・`calls_today`/`daily_call_cap`・`tokens_today`/`daily_token_cap`・`halt`＝理由コードと時刻）。理由コードと時刻だけで、CLI の文面・認証情報・パスは返さない。ログイン状態は、アプリが呼ぶ前に確かめた最後の結果（アプリが書き、画面と MCP が読む）。

## 3. 構成

| 場所 | 役割 |
|------|------|
| `internal/service/jev/codexwire` | `codex exec`の呼び出し（プロセスグループ・環境の許可リスト・JSONL の解釈・判定）、`codex login status`、プロンプトとスキーマ |
| `internal/service/jev/trial/codex.go` | Codex アームの実行（同時 1 本・関門・上限・停止） |
| `internal/config/jevtrialsettings/codex.go` | `jev.trial.codex.*`のキーと型 |
| `internal/bootstrap/airolewire`（`TrialKeys`） | `PITHA_CODEX_HOME`・`PITHA_CODEX_BIN`を読む（秘密ではない） |

## 4. テスト（モックの CLI だけ。実際のログイン・認証つきの実行・キーの投入・設定のオンはしていない）

| 内容 | テスト |
|------|--------|
| 成功・拒否（null）・トークン・読み取り専用の引数と空の作業ディレクトリ | `codexwire.TestTrader_SuccessMapsTheAnswersTokensAndKeepsTheRunReadOnly` |
| 子の環境が許可リストと専用`CODEX_HOME`だけ（API キーが渡らない） | `codexwire.TestTrader_ChildEnvironmentIsAnAllowListWithTheDedicatedHome` |
| 失敗は`turn.failed`と終了コードだけで決まる／`error`イベントだけでは失敗にしない／一致しない文面は「上限」にしない | `codexwire.TestTrader_FailureIsJudgedByTurnFailedAndExitCodeOnly`・`TestTrader_AnErrorEventAloneIsNotAFailure`・`TestTrader_UsageLimitTextLabelsTheFailureAndGivesTheResetTime` |
| タイムアウト・取り消しで子と孫をグループごと止める | `codexwire.TestTrader_TimeoutKillsTheWholeProcessGroup`・`TestTrader_CancelIsNotATimeoutAndStopsTheChild` |
| ログイン状態（ChatGPT／API キー／未ログイン／CLI なし）・`auth.json`を読まない | `codexwire.TestLogin_*` |
| 既定オフ・USD 0・上限（回数・トークン）・再開時刻まで止まり再試行しない・時刻不明は JST の日の終わり・同時 1 本・Close | `trialflow.TestCodex*` |
| 実際の経路（`TraderOptions`→`codexwire`→モックの CLI）で行が`subscription`・API キーが渡らない・上限で CLI を 1 回しか起動しない | `trialflow.TestAppWiring_Codex*` |
| 画面（既定オフ・保存・理由の表示）・MCP（`subscription`・`limits.codex`） | `jevtrialroute`の`codex_test.go`・`trialtests.TestCostReportsTheSubscriptionApartFromUSDAndTheCodexLimits` |
| マイグレーション000058（行の保存・`subscription`の CHECK・巻き戻し） | `db.TestMigration000058_*` |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-15 | 新規作成。調べた結果・設計・テスト | issue #1081 |
