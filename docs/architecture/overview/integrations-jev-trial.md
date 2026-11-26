# アーキテクチャ設計: Jev 意思決定モデル比較のトライアル（issue #1059）

`docs/architecture/overview/integrations.md` §6（Jev API連携）から分割した節。ワイヤ層（`systemone.DecodeTrial`・`internal/service/jev/trialwire`。issue #1058）は同ファイル §6 を、テーブルは `docs/architecture/er/tables-jev-trial.md` を参照。読み取り専用 MCP ツール（`jev_trial_compare` / `jev_trial_cost`。issue #1060）は `docs/architecture/paper-results-mcp.md` を参照。

## 位置づけ

ライブの Jev（TypeSafe AI）の判断と**同じ入力**を、別のモデル（**アーム**）にも聞き、答えと費用と失敗を並べて残す、Paper 専用の**比較用の記録**。ライブの採否・発注・RAG・較正・メタモデルには**一切入らない**。既存の Jev プロンプトのシャドー（`internal/service/jev/shadow`、`jev_shadow_decisions`、`jev.shadow.*`）とは**別系統**で、表・ランナー・設定キー・API キー・HTTP クライアントを共有しない（シャドーの挙動は変えない）。

- 実装: `internal/service/jev/trial`（ランナー）・`internal/service/jevtrial` / `internal/web/handler/jevtrial` / `internal/web/organisms/jevtrial`（Settings 画面のトライアルカード。MCP の `settings_set` と同じ 6 キーを保存し、同じ監査表に `actor=gui` で残す。issue #1220）・`internal/service/jev/trialwire`（アームの HTTP クライアント）・`internal/service/jev/codexwire`（ChatGPT のサブスクのアームの Codex CLI クライアント。issue #1081）・`internal/config/jevtrialsettings`（`jev.trial.*`）・`internal/repository/judgement/trial_repo.go`（`jev_trial_decisions`）・`internal/domain/jevtrial`。
- **多数決・合議・「採用すべきアーム」・自動昇格は作らない**。MCP の出力にも無い。比較して何を選ぶかは人が決める。

## フロー

1. ライブの `jev.Trader` が trader の判断を `jev_decisions` に保存した**後**、既存の `jev.ShadowSubmitter` の継ぎ目（`t.shadow.Submit(saved, state, ragContext)`）に渡す。`trader.go` の評価ロジックと保存順は変えない。
2. アプリの組み立て（`trial.TraderOptions`。`shadow.TraderOptionsWith` を介して`shadow.TraderOptions`と同じ選択肢の並びを作る）が、既存のシャドーとトライアル（`trial.Trial`）を合成した `ShadowSubmitter`（`trial.Compose`。`trial.Multi`）を `jev.WithShadow` に渡す。各々が自分の状態・スイッチ・失敗処理を持ち、合成は何も足さない。
3. `trial.Trial.Submit` は、**Paper 以外**（`metalabelsettings.RuntimeMode`。ビルドで固定）・スイッチ `jev.trial.enabled` がオフ（既定）・アームが空のときは、**ゴルーチンも起こさず・外部呼び出しも書き込みもせず**に戻る。
4. 有効なら、アームごとに別のゴルーチンで**並列**に聞く。各アームは同じ `TraderState`・同じ RAG 文脈・自分のプロンプト版（第 1 段階は `trader-v3`＝ライブと同じ）を受け取り、**他のアームの答えを見ない**。結果は成功も失敗も `jev_trial_decisions` に 1 行ずつ残す（失敗した呼び出しは `status='failed'`＋`failure_code`＋`http_status`＋短い `error_excerpt`）。
5. 呼び出しの前に、止められていないか（トライアル全体の停止・アームの停止・429/529 の待ち・失敗上限）と、費用の上限（全体・アーム）を、保存済みの行から判定する。1 つでも該当すればそのアームは呼ばない（行は残さない）。

シャドーの構造（Paper ゲート・同時 4 本・失敗上限・panic 回収・`Close`）を踏襲する。

| 項目 | 値・動き |
|------|----------|
| 同時呼び出し | 全アームで最大 4 本（`maxInFlight`）。満杯なら**待たずにそのアームの比較を捨てる**（ログと見送りの記録）。ライブを遅らせず、上限の超過を避けるため |
| 呼び出し間隔 | **アームごと**に最小 2 秒（`jevtrialsettings.MinCallInterval`。シャドーと同じ値）。間隔内に来た比較は捨てる（見送りの記録を残す）。アームは別のキー・別の `http.Client` なので、間隔は共有しない |
| 再試行 | しない（有料の呼び出しは 1 回きり。1 回の呼び出しは最大 45 秒。アームの `http.Client` のタイムアウトは 30 秒） |
| 429 / 529 | そのアームを 15 分（`ThrottleCooldown`）止める。再試行しない。失敗上限には数えない（何も答えていない） |
| HTTP 402（残高不足） | キーは共有なので**トライアル全体**をその日（JST）止める（`jev.trial.halt`、`reason=payment_required`）。失敗上限を待たない |
| HTTP 403 | そのアームをその日止める（`jev.trial.halt.<arm>`、`reason=forbidden`。キー・モデル・プロバイダー指定が許可されていない）。失敗上限を待たない |
| 失敗上限 | そのアームの失敗した呼び出しが 1 日（JST）20 回（`DailyFailureCap`）に達したらその日そのアームを止める（`daily_failure_cap`） |
| panic | ゴルーチンごとに回収する。ライブの判断には影響しない |
| 終了 | `Close` が進行中の呼び出しを取り消して待つ。終了で取り消された呼び出しは失敗として数えず、行も残さない |

止めた記録は翌日（JST）に自然に失効し、翌日から再開する。ライブの判断は止まらない。

### 見送りの記録（`jev_trial_skips`。issue #1062）

呼ばなかった比較（見送り）は、`jev_trial_decisions` に行を残さない。そのままでは、見送られた判断が比較の cohort（選んだアームすべてに聞いた判断）から静かに抜け、欠測率にも入らないので、**ライブの判断ごと・アームごとに 1 行、理由つきで `jev_trial_skips` に残す**（`trial.Trial.recordSkip`。`(jev_decision_id, arm_id)` で一意）。

| 理由コード | いつ |
|-----------|------|
| `concurrency_full` | 同時実行枠（全体で最大 4 本）が満杯 |
| `min_interval` | そのアームの最短間隔（2 秒）の内 |
| `daily_cap_overall` / `daily_cap_arm` | 全体・アームの日次の費用上限に到達（保存済みの行で到達、または飛んでいる呼び出しの予約で余白が無い）。上限で止まった記録が出た後の見送りも、記録の理由でこちらに分類する |
| `key_not_set` | OpenRouter のキーが無い（`Configured()` が false） |
| `prompt_version_unregistered` | アームのプロンプト版が登録済みの Trader 版でない |
| `stopped_payment_required` / `stopped_forbidden` / `stopped_failure_cap` / `stopped_other` | 402 の全体停止・403 のアーム停止・失敗回数の上限・この版が知らない理由の停止記録 |
| `throttled` | 429/529 の待機中 |
| `provider_disabled` / `not_logged_in` / `stopped_usage_limit` / `stopped_provider_failed` / `daily_cap_calls` / `daily_cap_tokens` / `provider_busy` | ChatGPT のサブスク（`codex_chatgpt`）のアームだけ: Codex 用のスイッチがオフ・CLI が ChatGPT でログインしていない・使用量の上限（文面が一致したとき）で停止中・失敗で停止中・1 日の呼び出し回数／トークン数の上限・前の判断の Codex がまだ動いている。意味は`overview/integrations-codex-chatgpt.md` |
| `check_failed` | 停止記録・費用が読めない、リクエストの大きさを求められない、設定を全部は読めなかった（アームは分かる）ため呼ばなかった |

- 見送りの行は、答え・費用・status を持たず、成功・失敗の行としては扱わない。費用の上限の判定（`counted_cost_usd` の合計）にも入らない。書き込みは**書ければ書く**（失敗はログだけ。見送り自体は変わらない）。
- 記録しないもの: お試しが無効・アームが空・Paper 以外（呼ぶつもりが無い）、設定を読めずアームが分からない場合、シャットダウン（`Close`）で取り消された呼び出し。
- 読み方（`jev_trial_compare` の `fairness.skipped`、`jev_trial_cost` の `limits.skipped_today`）は `paper-results-mcp-jev-trial.md`。**欠測率の分母には入れない**（行が無く、成功・失敗・欠測のいずれでもない。別の指標 `asked_share` で見せる）。ライブの `jev_decisions` と決定経路は変えない。

## 費用の数え方と上限

1 回の呼び出しの費用は、**出所を区別**して `jev_trial_decisions` に残す。

| `cost_source` | `cost_usd` | 上限に数える額（`counted_cost_usd`） |
|---------------|-----------|--------------------------------------|
| `reported` | 応答の `usage.cost`（報告値。最優先） | 報告値 |
| `list_price_estimate` | アームの定価（`list_price_usd_per_mtok`、1M 入力トークンあたり）× 入力トークン数（報告値が無ければリクエストのバイト数からの推定） | **処理先を固定していないアーム（`provider_only` なし）は、定価と `WorstCaseUSDPerMtok` の高い方**の単価 × 入力トークン数。`provider_only` で固定したアームは定価のまま |
| `list_price_estimate`（チャットのアーム。issue #1224） | 入力トークン × `list_price_usd_per_mtok` ＋ 出力トークン × `list_price_output_usd_per_mtok`（トークンは応答の `usage.prompt_tokens` / `completion_tokens`。入力が無ければ本文の大きさからの推定。出力が無ければ 0 として推定額に入れる） | 入力トークン × チャット用の入力計上単価 ＋ 出力トークン × チャット用の出力計上単価（`Arm.ChatCountingUSDPerMtok()`。**処理先を固定していないアームは `max(定価, チャット用最悪単価)`**、固定したアームだけ設定した定価）。応答に出力トークンが無ければ出力は**出力上限トークン（`max_tokens`＝1024）**で数える（0 円にしない） |
| `subscription` | **NULL**（ChatGPT のサブスク。USD は発生しない。issue #1081） | **0**（USD の上限には数えない。代わりにプロバイダー単位の 1 日の呼び出し回数・トークン数の上限。`overview/integrations-codex-chatgpt.md`） |
| `unknown` | **NULL（0 ではない）** | 最悪値側の単価 `jevtrialsettings.WorstCaseUSDPerMtok`（0.24 USD/百万トークン。第 1 段階のモデルのうち最も高い Clef の定価）× 入力トークン数 |

- **上限の判定は、処理元が分からないうちは高い方の単価で数える**。OpenRouter は処理元（プロバイダー）を切り替えうるため、`unknown` は 0 円と数えず最悪値側で数える。第 1 段階の定価は、Clef-flash が 0.038、Clef 系が 0.192〜0.24 USD/百万トークン（2026-11-13 時点の公表値）。実請求との一致は未確認。
- **定価の設定に頼らない数え方**（issue #1065）: 応答が費用を報告しないとき、上限に数える単価は**設定した `list_price_usd_per_mtok` ではなく `Arm.CountingUSDPerMtok()`** で決まる。定価が無い（0・省略）なら `WorstCaseUSDPerMtok`（0.24）。定価があっても、`provider_only` で処理先を固定していないアームは `max(定価, 0.24)` で数える（処理先が呼び出しごとに替わりうるため、低い定価を書いても上限は甘くならない）。`provider_only` で固定したアームだけが、設定した定価のまま数える（処理先が分かっているので、その定価が単価）。`cost_usd`（推定額）は定価のままで、上限に数えた額は `counted_cost_usd`（`jev_trial_cost` の `list_price_estimate.counted_usd`）に別に残る。既定値（全体 0.10・アーム 0.05・最悪単価 0.24）は変えない。
- **止まる条件の保持と失敗時の側**: 402/403 の停止はメモリにも保持し、記録の書き込みに失敗してもその日のうちは再び呼ばない（費用の上限は保存済みの行から毎回求め直すので、これは 402/403 だけの追加）。設定・停止記録・失敗カウンタの**読み取りに失敗したときは、そのアームを呼ばない**（「記録なし」を「止まっていない」と読まない）。失敗カウンタを読めないときは、低い値で上書きしないよう数えない。`error_excerpt` は 200 バイトまでで、設定済みの OpenRouter キー文字列そのもの（応答に含まれる部分一致のすべて。キーはメモリ上だけで持ち、ログにも書かない）と、`sk-…` 形のキー・`Bearer …` を伏せ字（`[redacted]`）にする（ベンダーが認証情報をエラー本文に返す場合に備える。設定済みのキーでも `sk-…` 形でもない形の認証情報までは伏せられない）。画面の保存は、無効化を先に、有効化を最後に書く（途中で失敗してもオンのまま残らない）。
- **応答が返らなかった呼び出し**（`status='failed'`）の費用は常に `unknown`（`cost_usd` は NULL）。4xx の応答（413 を除く）と 529 は、モデルが動く前に断られ課金されない**前提**（前提であって測定ではない）で、上限には 0 円で数える（失敗回数の上限と 402/403/429/529 の停止が歯止めになる）。5xx・413・タイムアウト・通信エラー・不正な応答はモデルが動いた後かもしれず課金された可能性があるので、リクエストの推定入力トークン数 × 最悪値側の単価で数える。
- **チャットのアーム（`endpoint=/v1/chat/completions`）の数え方**（issue #1224）: 上の入力だけの数え方は第 1 段階の意思決定モデル向けで、**一般チャットモデルは出力にも定価が付き、入力定価も 0.24 を大きく超える**（2〜8 USD/百万トークン）。そのためチャットのアームだけ、入力と出力の両方を見る。`/v1/systemone`・`/alpha/decisions` のアームの数え方と `WorstCaseUSDPerMtok`（0.24）は変えない。
  - **定価は 2 つとも必須**: `list_price_usd_per_mtok`（入力）と `list_price_output_usd_per_mtok`（出力）。0・省略は保存できない。出力の定価は chat のエンドポイントだけで使える。
  - **チャット用の最悪単価**（`jevtrialsettings.ChatWorstInputUSDPerMtok` = 8、`ChatWorstOutputUSDPerMtok` = 40 USD/百万トークン）: 比べる一般チャットモデルのうち公表定価が最も高いもの（`openai/gpt-6.2-astra`。`anthropic/claude-opus-5.5` は 4 / 20、`google/gemini-4-argon` は 2 / 10）。**出典: OpenRouter のモデル一覧（`GET /api/v1/models` の `pricing.prompt` / `pricing.completion`）、確認日 2026-11-26**。上限判定のための数え方で、実請求との一致は未確認。これより高いモデルを載せるときは、この定数を上げてから載せる。
  - **失敗時**: 4xx（413 を除く）と 529 は従来どおり 0 円。それ以外（5xx・413・タイムアウト・通信エラー・不正な応答）は、そのアームの**予約相当**（推定入力トークン × 入力計上単価 ＋ 出力上限トークン × 出力計上単価）で数える。
  - **報告費用が最優先**: 応答の `usage.cost`（出力込み）があれば `reported`。
  - **既定の上限との関係**: チャットの 1 呼び出しの予約は、入力の推定（本文の大きさ ÷ 2 バイト/トークン。`shadow.BytesPerTokenFloor`。14 KB の本文で約 7,400 トークン）× 8 USD/M ＋ 1024 × 40 USD/M で **約 0.1 USD** になる。既定の全体上限 0.10・アーム上限 0.05 のままでは予約だけで上限に当たり、**呼ばれずに `daily_cap_overall` / `daily_cap_arm` で見送られる**（既定の上限値はコードで変えない）。チャットのアームを動かすときは、`jev.trial.daily_cost_cap_usd` とアームの `daily_cost_cap_usd` を、人が上げる。
- 上限は**全体**（`jev.trial.daily_cost_cap_usd`、既定 0.10 USD）と**アームごと**（`Arm.daily_cost_cap_usd`、省略時 0.05 USD）の両方を判定する。どちらかが保存済みの行の `counted_cost_usd` の 1 日（JST）合計に達したら、全体なら `jev.trial.halt`（`daily_cost_cap`）、アームなら `jev.trial.halt.<arm>`（`arm_cost_cap`）を記録して止める。翌日（JST）に再開する。
- 保存に失敗した（課金されたかもしれない）呼び出しは、失敗として日次カウンタに数え、上限の歯止めにする。

### 飛んでいる呼び出しと、上限を超え得る幅（issue #1065）

日次上限は保存済みの行の `counted_cost_usd` 合計で判定するが、**まだ行になっていない呼び出し（最大 `maxInFlight`＝4 件）を、呼ぶ前にメモリ上で予約して上限に数える**（`trial/reserve.go`）。

- **予約額**: その呼び出しのリクエスト本文の大きさ（`trialwire.RequestBytes`。実際に送る本文と同じ組み立て）から推定した入力トークン数（`shadow.EstimateInputTokens`）× `WorstCaseUSDPerMtok`。定価の設定に依らない（失敗した呼び出しを数える額と同じ）。**チャットのアームは、実際に送るチャット本文の大きさ（`trialwire.ChatRequestBytes`。`trial.requestBytesFor` がアームのエンドポイントで選ぶ）から推定した入力トークン × 入力計上単価 ＋ 出力上限トークン（`trialwire.ChatMaxOutputTokens`＝1024）× 出力計上単価**（上の「チャットのアームの数え方」）。定価の設定だけでは小さくならない（処理先を固定していないアームはチャット用最悪単価以上）。Codex 側の予約は従来どおり System One 本文の大きさのまま（範囲外）。
- **判定**: 保存済みの合計 ＋ 飛んでいる呼び出しの予約の合計（この呼び出しの分を含む）が、全体の上限とアームの上限を超えるなら、その呼び出しは見送る（行も停止記録も残さない。飛んでいる呼び出しが上限の内に収まる余地は残るので、停止はしない）。保存済みの合計だけで上限に達していれば、従来どおり停止を記録する。
- **返す時**: 行を保存した（または失敗として数えた）後に予約を返す。保存と返却の間は二重に数える側に倒す。予約はメモリ上だけで、再起動で消える（再起動後は飛んでいる呼び出しが無い）。
- **超え得る幅**: 予約は入力トークンの推定（最悪値側の単価）だけを見るので、上限を超えるのは、**応答が報告した費用（`usage.cost`。出力トークンを含む）が、予約額より大きいとき、その差の分**に限られる。チャットのアームは予約に出力上限（`max_tokens`）分を含むので、この差は、報告費用がチャット用最悪単価での予約を超えるとき（処理先が最悪単価より高いモデルだったとき）に限られる。1 件ごとの差は出力トークン分（トライアルの出力は短い）で、飛んでいる最大 4 件の差の合計が、全体の上限を超え得る最大の幅。アームの上限は、そのアームの飛んでいる分の差だけ超え得る（1 アームに同時に飛ぶのは最大 4 件）。幅はこれより広がらない。402 は新しい呼び出しを止めるが、飛んでいる呼び出しは打ち消さない（既存シャドーと同じ考え方）。
- **数え方の限界**: 1 日（JST）の境界をまたぐ呼び出しは、予約を、その呼び出しを許可した時点の日の上限に数える。複数プロセスで動かした場合は予約を共有しない（想定しない）。


## 設定

すべて `runtime_settings` のキーで、**既定はオフ・空**。**操作者が変えるのは 6 キー**（`jev.trial.enabled`・`jev.trial.arms`・`jev.trial.daily_cost_cap_usd`・`jev.trial.codex.enabled`・`jev.trial.codex.daily_call_cap`・`jev.trial.codex.daily_token_cap`。`jevtrialsettings.OperatorKeys()`）で、Settings 画面と MCP の `settings_set`（`mcpwritesettings` のホワイトリスト）の両方から書ける（issue #1220。以前は「オンにできるのは Settings 画面だけ」としていたが、調整は MCP 経由が基本という方針に合わせて改めた）。同じ検証（`jevtrial.ParseArms`・`ValidateCap`・`ValidateCodex`）を保存前に掛け、MCP からの保存は書き込み許可（`settings_write_enable`）に従い、どちらの経路の保存も `mcp_settings_audit` に残る（画面は `actor=gui`）。既定の上限（全体 0.10 USD・アーム 0.05 USD・Codex 30 回／500000 トークン）と、ランナーの上限・失敗回数の上限・呼び出し間隔・停止記録は変えず、停止記録・失敗回数・429/529 の記録・Codex のログイン記録のキーは MCP に出さない。自己改善ループ（Sol/Opus）の `policy.*` には入らない（`shadowflow/shadow_isolation_test.go`・`isolationflow` のテストで固定）。API キーは MCP から読み書きしない。

| キー | 既定 | 意味 |
|------|------|------|
| `jev.trial.enabled` | `0`（オフ） | トライアルの有効。Paper のときだけ効く |
| `jev.trial.arms` | `[]`（空） | アームの JSON 配列として保存する（Settings 画面ではアームごとのフォームで編集し、保存時にこの配列へ変換する）。空なら何も呼ばない |
| `jev.trial.daily_cost_cap_usd` | 0.10 USD | 全アーム合計の 1 日（JST）の上限 |
| `jev.trial.halt` | なし | トライアル全体の 1 日の停止記録（全体上限・HTTP 402） |
| `jev.trial.codex.enabled` | `0`（オフ） | ChatGPT のサブスク（`codex_chatgpt`）のアームのスイッチ。オフの間はアームを保存できず、呼ばない（issue #1081） |
| `jev.trial.codex.daily_call_cap` / `jev.trial.codex.daily_token_cap` | 30 回／500,000 トークン | Codex の 1 日（JST）の呼び出し回数・トークン数（入力＋出力）の上限。USD ではない |
| `jev.trial.codex.halt` / `jev.trial.codex.login` | なし | Codex プロバイダー全体の停止記録（理由コード・再開時刻）・最後のログイン確認の状態（アプリが書く） |
| `jev.trial.halt.<arm>` / `jev.trial.failures.<arm>` / `jev.trial.throttle.<arm>` | なし | アームごとの停止記録・失敗回数・429/529 の記録。`jev.shadow.*` とは重ならない |

**Settings 画面のアームのフォーム**: アームは 1 アーム 1 カードのフォーム（アーム名・提供元〈openrouter・custom・codex_chatgpt から選ぶ。openrouter 以外では、送り先・データの扱い・ZDR は保存せず、データの扱いの既定値「学習に使わない提供元だけ」はハンドラが落とす。codex_chatgpt では 1 日の上限・定価も使えない〉・モデル・プロンプトの版〈このビルドに登録された版から選ぶ〉・1 日の上限・定価・送り先の提供元〈カンマ区切り〉・データの扱い〈既定は学習に使わない提供元だけ〉・ZDR・API の送り先〈既定は /v1/systemone。/alpha/decisions と /v1/chat/completions（一般チャットモデル。issue #1224）を選べる〉・出力の定価〈チャットの送り先のときだけ。必須〉）で編集し、「アームを追加」「削除」で行を増減する（htmx。追加・削除は編集部分だけを差し替え、打ちかけの行は残る。保存するまでは何も書かない）。保存すると、ハンドラ（`web/handler/jevtrial`）が行（`arm-<n>-<項目>`）を`[]jevtrialsettings.Arm`に集め、`service/jevtrial.ValidateArms`（中身は、保存済みの JSON を読むときと同じ`jevtrialsettings.ValidateArms`＋登録済みのプロンプトの版）に通す。**検証の本体は 1 か所**で、必須項目・id の文字種と重複・最大 8 件・各項目の範囲・提供元名・`data_collection`の値は JSON でもフォームでも同じ厳しさ。1 つでも不正なら何も保存せず、`#jev-trial-error`に日本語の理由を出す。保存済みのアームが壊れているときは、画面は「アームなし」として扱う旨を示し、保存で置き換わる。API キーは設定の有無（設定済み／未設定）だけを出し、値は画面にも応答にも出さない。

アーム（`jev.trial.arms` の要素）の形:

| フィールド | 意味 |
|------------|------|
| `id` | アーム名（`jev_trial_decisions.arm_id`。英小文字・数字・`.`・`_`・`-`、48 文字まで） |
| `provider` | `openrouter`（既定の使い方。OpenRouter のキーとアームの `model` で呼ぶ）・`custom`（独自 API の URL とキー。`provider_only`・`data_collection`・`zdr` は付けられない。issue #1080、`overview/integrations-ai-providers.md`）・`codex_chatgpt`（ChatGPT のサブスク。公式の Codex CLI のログイン。USD では数えない。`provider_only`・`data_collection`・`zdr`・`daily_cost_cap_usd`・`list_price_usd_per_mtok`は付けられない。Codex 用のスイッチ`jev.trial.codex.enabled`がオフの間は保存できない。issue #1081、`overview/integrations-codex-chatgpt.md`） |
| `model` | 送るモデル id |
| `prompt_version` | 登録済みの Trader プロンプト版（例 `trader-v3`） |
| `daily_cost_cap_usd` | このアームの 1 日の上限（省略・0 は 0.05 USD） |
| `list_price_usd_per_mtok` | 1M 入力トークンあたりの定価。0・省略は不明（応答が費用を報告しなければ `unknown`）。上限には、`provider_only` を指定していないアームなら `max(定価, 最悪単価 0.24)` で数える（低い定価を書いても上限は甘くならない） |
| `list_price_output_usd_per_mtok` | 1M 出力トークンあたりの定価。**`endpoint` が `/v1/chat/completions` のアームだけ**使え、そのアームでは必須（`list_price_usd_per_mtok` も 0 より大きい値が必須）。他のアームに付けると保存できない（issue #1224） |
| `endpoint` | 任意。このアームが呼ぶ API のパス（`/v1/systemone`・`/alpha/decisions`・`/v1/chat/completions`の 3 つだけ。**`/v1/chat/completions` は OpenRouter のホストから見た `/api/v1/chat/completions` ではない**: 基底 URL が `https://openrouter.ai/api` なので、`/api/…` を許すと `…/api/api/v1/…` になる。ホワイトリスト外の値・任意の URL は保存できない）。省略・空は `/v1/systemone`（ライブの Jev と同じ。既定は変えない）。`trial.NewCallers` が `trialwire.Config.Endpoint` へ渡す（アームごと。設定を変えるとキャッシュ済みのクライアントは作り直す）。`codex_chatgpt` では付けられない（HTTP API を呼ばない）。`/alpha/decisions` が実際に非 Jev モデルを受けるかは実測スパイク #1063 の結果次第で、この項目は選べるようにしただけ（実際の呼び出し・キーの投入はしない）。ライブの `jev.Client`・`JEV_*`・厳格検証は触らない（issue #1064） |
| `provider_only` / `data_collection` / `zdr` | OpenRouter のルーティング指定（`provider.only`〈指定時はフォールバック無し〉・`provider.data_collection`・`provider.zdr`） |

**第 1 段階のアーム案（設定例。ドキュメントにだけ書く。既定の配列は空で、アプリは何も登録しない）**:

```json
[
  {"id": "or-clef-flash-v3", "provider": "openrouter", "model": "cloudflare/clef-flash",     "prompt_version": "trader-v3", "list_price_usd_per_mtok": 0.038},
  {"id": "or-clef-v3",       "provider": "openrouter", "model": "cloudflare/clef",           "prompt_version": "trader-v3", "list_price_usd_per_mtok": 0.24},
  {"id": "or-gemini-d1-v3",  "provider": "openrouter", "model": "google/gemini-decision-1",  "prompt_version": "trader-v3"}
]
```

Gemini Decision 1 は定価が公表されていないため `list_price_usd_per_mtok` を省く（応答の `usage.cost` が無ければ `unknown`＝最悪値側で数える。0 円と数えない）。Clef 系の定価は 0.192〜0.24 USD/百万トークンで、上限判定は高い方（0.24）で数える。`or-clef-flash-v3` の定価 0.038 は推定額（`cost_usd`）にだけ効き、`provider_only` を付けていないので、上限には 0.24 で数える。

### チャット Completions のアーム（`endpoint=/v1/chat/completions`。issue #1224）

OpenRouter（と OpenAI 互換の `custom`）の**一般チャットモデル**（例: `anthropic/claude-opus-5.5`・`openai/gpt-6.2-astra`・`google/gemini-4-argon`）を送り先にするための経路。`/v1/systemone` と `/alpha/decisions` は意思決定モデル専用で、チャットモデルに投げると全件 `http_4xx` になる（2026-11-26 のお試しで 3 モデルが各 20 回失敗し、失敗上限で自動停止した）ため、別の送り先として足した。別のフィールド（`wire_format`）は足さない: **送り先のパスが形式を一意に決める**。既存の意思決定モデルの経路（`/v1/systemone`・`/alpha/decisions`）とCodex の経路は変えない。

- **リクエスト**（`trialwire/chat.go`。プロトコル版 `chat-completions-v1`＝`trialwire.ChatProtocolVersion`。プロンプト版ではなく**ワイヤの版**で、Codex の `codex-exec-v1` に当たる。レイアウトを変えたら上げる）: `POST {BaseURL}/v1/chat/completions`。本文は System One の `{state, questions}` ではなく、`messages`（system: 固定の前置文、user: 質問群と状態）・`response_format`（`json_schema`、`strict: true`、スキーマ名に版を含む）・`max_tokens`＝1024（`trialwire.ChatMaxOutputTokens`。予約の出力側の材料）・`temperature: 0`（受け付けないモデルは OpenRouter が無視する）。質問は**登録済みの Trader プロンプト版**（第 1 段階は `trader-v3`）のもので、**新しいプロンプト版は作らない**。質問の文面・状態の JSON・スキーマ（質問 id が必須キー、拒否は `null`、choice は `{choice, confidence}`、noul は 0〜1 の数、`additionalProperties: false`）は Codex と共通のコード（`trialwire.QuestionsPrompt` / `StatePrompt` / `AnswerSchema`）で、Codex の送る内容は 1 バイトも変わらない（`codexwire` のテストが固定）。OpenRouter のアームには既存どおりルーティング指定（`provider.only` / `allow_fallbacks` / `data_collection` / `zdr`）を載せ、**`provider.require_parameters: true`** を足す（`response_format` を無視する提供元へ回さない。構造化出力が無ければ自由文が返って課金され読めないため）。`custom` には載せない。有料の呼び出しの再試行はしない。
- **レスポンス**: `choices[0].message.content` を、質問 id をキーにした JSON オブジェクトとして読む（`trialwire.AnswersToSystemOne` が System One の答えセットに書き直し、`systemone.DecodeTrial` → `trialwire.FromResult` で検証。ライブの `systemone.Decode` は使わない）。**パース失敗・欠けた質問・聞いていない質問・範囲外・未定義の choice・`content` が無い・`model` が無い・本文が JSON でない応答は `systemone.ErrInvalidResponse`**（`failure_code=invalid_response`）。コードフェンスや自由文からの取り出しはしない。質問単位の `null` は拒否（`TypeRefusal`）で、全問拒否は `status=refused`・一部は `partial`、拒否・欠けた答えは NULL のまま（`NONE` や 0 で埋めない）。HTTP エラーの分類（402/403/413/429/529/4xx/5xx）と、キー・`Bearer`・`sk-…` の伏せ字は既存のまま。
- **記録**: `served_model` / `served_provider` / `response_id` は応答の `model` / `provider` / `id`。トークンは `usage.prompt_tokens` / `usage.completion_tokens`、報告費用は `usage.cost`（OpenRouter のチャット Completions の現行ドキュメント。`usage: {include: true}` は廃止済みで何もしないので送らない。使用量は常に応答に入る。確認日 2026-11-26）。応答 JSON 全体を `response_json` に残す。新しい列は足さない（プロトコル版はアームの `endpoint` から分かる）。
- **予約と費用**: 上の「チャットのアームの数え方」。`trial/run.go` の予約サイズは、チャットのアームでは**実際に送るチャット本文**の大きさ（`trial.requestBytesFor`）。リプレイ（`jevreplay`）の見積もりも同じ（`trial.RawRequestBytesFor` / `trial.ReservedUSD(arm, size)`）で、チャットのアームは出力が呼ぶ前に分からないので定価の見積もり（`ListPriceUSD`）を出さない。
- **`max_tokens`＝1024 は未計測**: 答えのオブジェクトは 150 トークン前後だが、推論してから答えるモデルは推論トークンもここに数えられる。足りないモデルは本文が途中で切れ、`invalid_response` になる（出力の費用が上限を超えて増えるのではなく、失敗として残る）。

**チャットのアームの設定例**（ドキュメントにだけ書く。上限は人が上げる。定価は 2026-11-26 の OpenRouter の公表値）:

```json
[
  {"id": "or-gpt-6-2-astra-v3", "provider": "openrouter", "model": "openai/gpt-6.2-astra", "prompt_version": "trader-v3",
   "endpoint": "/v1/chat/completions", "list_price_usd_per_mtok": 8, "list_price_output_usd_per_mtok": 40, "daily_cost_cap_usd": 0.5,
   "provider_only": ["openai"]}
]
```

### データの扱い（`data_collection`）

OpenRouter への指定は、**既定で `data_collection=deny`**（プロンプトを学習・保存に使うプロバイダーへ回さない）。このため、使えるプロバイダー（処理元）が狭まり、**応答の可用性・レイテンシ・費用にどれだけ影響するかは未計測**（トレードオフとして残す）。`allow` にできるのは、人がアームごとに明示したときだけ（`zdr` は Zero Data Retention のエンドポイントに限る指定）。

### 任意のシークレット

`OPENROUTER_API_KEY`（トライアル専用の API キー。ライブの `JEV_API_KEY` とは別）と `OPENROUTER_BASE_URL`（任意。既定は `https://openrouter.ai/api`）は Settings 画面で入れる任意キー。**未設定でもアプリの起動は失敗せず**（トライアルが動かないだけ）、**Setup Guard の必須キーには入らない**（`config.RequiredSecretKeys()` は `JEV_API_KEY`・`KABU_API_PASSWORD` のまま）。キーはプロセス内のメモリにだけ置き、ログ・エラー・DB の行・MCP の出力に出さない。トライアルは `JEV_API_KEY` / `JEV_BASE_URL` / `JEV_MODEL` を読まない（テストで固定）。

## 記録の意味づけ

- **`rag_includes_live_model_answers`**: リクエストに RAG の過去事例を載せた行は 1。過去事例にはライブ Jev の過去の答えが入っているため、**完全な独立条件ではない**。除外せず、件数と内訳を出す（`jev_trial_compare`）。
- **`llm_cutoff_status`**: 既定 `unverified`。各モデルの学習データの終了日は公表されていないため、日付を推測で入れない。評価は、**そのモデルの一般公開日以降**の局面だけで行う（Gemini Decision 1: 2026-11-11、Clef 系: 2026-10-01）。公開日より前の局面は、学習に入っていた可能性を排除できない。
- **`truncation_suspected`**: 報告された入力トークン数に対しリクエストが大きすぎる（バイト数 ÷ トークン数が 6 超。この閾値は未計測の目安）行に立てる**疑いの印**。入力が切り詰められたと断定するものではなく、`request_bytes`・`state_bytes`・`input_tokens` を材料として残し、後から判断する。疑いのある行は除外せず、件数と内訳で出す。Clef 系の切り詰めが実測できるまで、同じ入力での公平な比較とは扱わない。
- **`served_model` / `served_provider`**: OpenRouter が実際に処理したモデル・プロバイダー。送ったモデル（`requested_model`）と別に残す。
- **NONE と欠測**: 拒否・欠落した質問の答えは NULL（`NONE` や 0 で補わない）。`NONE` はモデルが「方向なし」と答えたことで、欠測とは別。MCP の指標の定義（欠測率・proxy の扱い）は `docs/architecture/paper-results-mcp.md`。

## 隔離の保証と、それを固定するテスト

トライアルの答えが取引・学習の経路に入らないことを、テストで構造的に固定する。

| 保証 | テスト |
|------|--------|
| 取引・リスク・約定・出口・メタラベル・RAG・特徴量・較正・自己改善・バックテスト・ブローカー・取引系リポジトリは、トライアルの表・リポジトリ・設定・ランナーを名前でも参照しない | `internal/service/jev/isolationflow` の `TestTrialIsNotReferencedByTradingRAGCalibrationOrTraining` |
| `jev_trial_decisions` を名指しするのは `internal/repository/judgement/trial_repo.go` だけ | 同 `TestOnlyTheTrialRepositoryTouchesTheTrialTable` |
| トライアルのキーは自己改善ループ（`ValidatePolicyChanges`）・汎用の運用設定のどれでも書けず、MCP の `settings_set` が書けるのは操作者向けの 6 キーだけ（停止・失敗・クールダウン・ログインの記録は書けない）で、`jev.shadow.*` と重ならない | 同 `TestTrialKeysAreOutsideEveryAutomaticWritePath` |
| ランナー・ワイヤ層は取引・学習のパッケージを import せず、ライブ Jev のシークレットを読まない | 同 `TestTrialRunnerImportsNoTradingPackageAndReadsNoLiveJevSecret` |
| ライブの判断経路（Jev 本体・プロンプト・判断の保存）・Risk・発注・RAG・較正・メタモデルは、トライアル側（`internal/service/jevtrial` / `internal/service/jev/trial` / `internal/service/jev/trialwire` / `internal/service/jev/codexwire`）を、直接にも他のパッケージ経由でも import しない。トライアル側を import してよいのは、許可リスト（`internal/bootstrap`・`internal/web/handler/jevtrial`・`internal/web/handler/settings`・`internal/service/jev/trial`。理由はテストに明記）の中だけで、リストに載るのに import しなくなった項目もテストが落ちる | 同 `TestLivePathDoesNotImportTheTrial`（import グラフは `internal/` と `cmd/` の本番ソースから読む） |
| トライアル側のパッケージは、取引・学習のパッケージに加え、発注（`execution`・`broker`）と注文案の経路（`proposalexec`・`proposalstore`・`paperresultsmcp/proposal`）を直接 import しない（推移的には見ない。ワイヤ層はライブ Jev のプロンプトのパッケージの上に載り、それが Risk の上にあるため） | 同 `TestTrialPackagesImportNoTradingOrOrderPlacementPackage` |
| 上の 2 つの検査自体が、違反を見つけて失敗できること（合成した import グラフで確認） | 同 `TestImportGuardCheckersFailOnViolations` |
| `OPENROUTER_*` は必須キーでなく、Setup Guard も求めない | 同 `TestOpenRouterSecretsAreNotRequired`、`internal/config/secretsflow` |
| 設定が既定のとき、トライアルは動かず、追加の外部呼び出しもない／Paper 以外では動かない／アームは互いの答えを見ない／成功も失敗も行に残る／費用の出所と上限／402・403・429・529 の停止 | `internal/service/jev/trialflow`（ランナーの振る舞いのテスト） |

シャドーの同種のテスト（`internal/service/jev/shadowflow/shadow_isolation_test.go`）は変更していない（トライアル分は同じ形で別ディレクトリ `isolationflow` に置いた）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-13 | 新規作成。トライアルのフロー・費用の数え方・上限・設定・隔離のテストを `integrations.md` §6 から分割して記述 | issue #1059 |
| 1.1 | 2026-11-14 | 隔離の保証の表に、ライブ側からトライアル側への import を許可リスト方式で禁じるテストと、トライアル側が発注に触れないことのテストを追加 | issue #1069 |
| 1.2 | 2026-11-15 | アームの`provider`に`custom`（独自 API）を追加。ライブ側からの import の許可リストに`internal/bootstrap/airolewire`（アームの接続の組み立て）を追加 | issue #1080 |
| 1.3 | 2026-11-15 | アームの`provider`に`codex_chatgpt`（ChatGPT のサブスク。USD では数えず、呼び出し回数・トークン数の上限、プロバイダー単位の停止）、`cost_source=subscription`、見送りの理由 7 件、`jev.trial.codex.*`のキーを追加。詳細は`integrations-codex-chatgpt.md`。ライブ側からの import を禁じる対象に`codexwire`を追加 | issue #1081 |
| 1.4 | 2026-11-15 | Settings のアーム入力を、JSON のテキストエリアから 1 アーム 1 カードのフォーム（追加・編集・削除）に変更。検証は保存済み JSON と同じ`jevtrialsettings.ValidateArms`に一本化し、1 つでも不正なら何も保存しない。提供元は openrouter・custom・codex_chatgpt から選べる（main の #1080・#1081 と合流） | issue #1066 |
| 1.5 | 2026-11-22 | シャドーのテストの参照を、#1173 で `evalflow/` から移された実在のパス `shadowflow/shadow_isolation_test.go` に直し、「`evalflow` ディレクトリが行数の上限に達していたため」の理由の記述を外した（実装の変更は無い） | issue #1207 |
| 1.6 | 2026-11-25 | 操作者向け 6 キーを MCP の `settings_set`／`settings_get` から読み書きできるようにした（「Settings 画面がオンにできる唯一の経路」の規則を改めた）。Settings 画面の保存も `actor=gui` で監査表に残す | issue #1220 |
