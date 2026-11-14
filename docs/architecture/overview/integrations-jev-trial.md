# アーキテクチャ設計: Jev 意思決定モデル比較のトライアル（issue #1059）

`docs/architecture/overview/integrations.md` §6（Jev API連携）から分割した節。ワイヤ層（`systemone.DecodeTrial`・`internal/service/jev/trialwire`。issue #1058）は同ファイル §6 を、テーブルは `docs/architecture/er/tables-jev-trial.md` を参照。読み取り専用 MCP ツール（`jev_trial_compare` / `jev_trial_cost`。issue #1060）は `docs/architecture/paper-results-mcp.md` を参照。

## 位置づけ

ライブの Jev（TypeSafe AI）の判断と**同じ入力**を、別のモデル（**アーム**）にも聞き、答えと費用と失敗を並べて残す、Paper 専用の**比較用の記録**。ライブの採否・発注・RAG・較正・メタモデルには**一切入らない**。既存の Jev プロンプトのシャドー（`internal/service/jev/shadow`、`jev_shadow_decisions`、`jev.shadow.*`）とは**別系統**で、表・ランナー・設定キー・API キー・HTTP クライアントを共有しない（シャドーの挙動は変えない）。

- 実装: `internal/service/jev/trial`（ランナー）・`internal/service/jevtrial` / `internal/web/handler/jevtrial` / `internal/web/organisms/jevtrial`（Settings 画面のトライアルカード。オンにできる唯一の経路）・`internal/service/jev/trialwire`（アームの HTTP クライアント）・`internal/config/jevtrialsettings`（`jev.trial.*`）・`internal/repository/judgement/trial_repo.go`（`jev_trial_decisions`）・`internal/domain/jevtrial`。
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
| `unknown` | **NULL（0 ではない）** | 最悪値側の単価 `jevtrialsettings.WorstCaseUSDPerMtok`（0.24 USD/百万トークン。第 1 段階のモデルのうち最も高い Clef の定価）× 入力トークン数 |

- **上限の判定は、処理元が分からないうちは高い方の単価で数える**。OpenRouter は処理元（プロバイダー）を切り替えうるため、`unknown` は 0 円と数えず最悪値側で数える。第 1 段階の定価は、Clef-flash が 0.038、Clef 系が 0.192〜0.24 USD/百万トークン（2026-11-13 時点の公表値）。実請求との一致は未確認。
- **定価の設定に頼らない数え方**（issue #1065）: 応答が費用を報告しないとき、上限に数える単価は**設定した `list_price_usd_per_mtok` ではなく `Arm.CountingUSDPerMtok()`** で決まる。定価が無い（0・省略）なら `WorstCaseUSDPerMtok`（0.24）。定価があっても、`provider_only` で処理先を固定していないアームは `max(定価, 0.24)` で数える（処理先が呼び出しごとに替わりうるため、低い定価を書いても上限は甘くならない）。`provider_only` で固定したアームだけが、設定した定価のまま数える（処理先が分かっているので、その定価が単価）。`cost_usd`（推定額）は定価のままで、上限に数えた額は `counted_cost_usd`（`jev_trial_cost` の `list_price_estimate.counted_usd`）に別に残る。既定値（全体 0.10・アーム 0.05・最悪単価 0.24）は変えない。
- **止まる条件の保持と失敗時の側**: 402/403 の停止はメモリにも保持し、記録の書き込みに失敗してもその日のうちは再び呼ばない（費用の上限は保存済みの行から毎回求め直すので、これは 402/403 だけの追加）。設定・停止記録・失敗カウンタの**読み取りに失敗したときは、そのアームを呼ばない**（「記録なし」を「止まっていない」と読まない）。失敗カウンタを読めないときは、低い値で上書きしないよう数えない。`error_excerpt` は 200 バイトまでで、設定済みの OpenRouter キー文字列そのもの（応答に含まれる部分一致のすべて。キーはメモリ上だけで持ち、ログにも書かない）と、`sk-…` 形のキー・`Bearer …` を伏せ字（`[redacted]`）にする（ベンダーが認証情報をエラー本文に返す場合に備える。設定済みのキーでも `sk-…` 形でもない形の認証情報までは伏せられない）。画面の保存は、無効化を先に、有効化を最後に書く（途中で失敗してもオンのまま残らない）。
- **応答が返らなかった呼び出し**（`status='failed'`）の費用は常に `unknown`（`cost_usd` は NULL）。4xx の応答（413 を除く）と 529 は、モデルが動く前に断られ課金されない**前提**（前提であって測定ではない）で、上限には 0 円で数える（失敗回数の上限と 402/403/429/529 の停止が歯止めになる）。5xx・413・タイムアウト・通信エラー・不正な応答はモデルが動いた後かもしれず課金された可能性があるので、リクエストの推定入力トークン数 × 最悪値側の単価で数える。
- 上限は**全体**（`jev.trial.daily_cost_cap_usd`、既定 0.10 USD）と**アームごと**（`Arm.daily_cost_cap_usd`、省略時 0.05 USD）の両方を判定する。どちらかが保存済みの行の `counted_cost_usd` の 1 日（JST）合計に達したら、全体なら `jev.trial.halt`（`daily_cost_cap`）、アームなら `jev.trial.halt.<arm>`（`arm_cost_cap`）を記録して止める。翌日（JST）に再開する。
- 保存に失敗した（課金されたかもしれない）呼び出しは、失敗として日次カウンタに数え、上限の歯止めにする。

### 飛んでいる呼び出しと、上限を超え得る幅（issue #1065）

日次上限は保存済みの行の `counted_cost_usd` 合計で判定するが、**まだ行になっていない呼び出し（最大 `maxInFlight`＝4 件）を、呼ぶ前にメモリ上で予約して上限に数える**（`trial/reserve.go`）。

- **予約額**: その呼び出しのリクエスト本文の大きさ（`trialwire.RequestBytes`。実際に送る本文と同じ組み立て）から推定した入力トークン数（`shadow.EstimateInputTokens`）× `WorstCaseUSDPerMtok`。定価の設定に依らない（失敗した呼び出しを数える額と同じ）。
- **判定**: 保存済みの合計 ＋ 飛んでいる呼び出しの予約の合計（この呼び出しの分を含む）が、全体の上限とアームの上限を超えるなら、その呼び出しは見送る（行も停止記録も残さない。飛んでいる呼び出しが上限の内に収まる余地は残るので、停止はしない）。保存済みの合計だけで上限に達していれば、従来どおり停止を記録する。
- **返す時**: 行を保存した（または失敗として数えた）後に予約を返す。保存と返却の間は二重に数える側に倒す。予約はメモリ上だけで、再起動で消える（再起動後は飛んでいる呼び出しが無い）。
- **超え得る幅**: 予約は入力トークンの推定（最悪値側の単価）だけを見るので、上限を超えるのは、**応答が報告した費用（`usage.cost`。出力トークンを含む）が、予約額より大きいとき、その差の分**に限られる。1 件ごとの差は出力トークン分（トライアルの出力は短い）で、飛んでいる最大 4 件の差の合計が、全体の上限を超え得る最大の幅。アームの上限は、そのアームの飛んでいる分の差だけ超え得る（1 アームに同時に飛ぶのは最大 4 件）。幅はこれより広がらない。402 は新しい呼び出しを止めるが、飛んでいる呼び出しは打ち消さない（既存シャドーと同じ考え方）。
- **数え方の限界**: 1 日（JST）の境界をまたぐ呼び出しは、予約を、その呼び出しを許可した時点の日の上限に数える。複数プロセスで動かした場合は予約を共有しない（想定しない）。


## 設定

すべて `runtime_settings` のキーで、**既定はオフ・空**。**書けるのはアプリの Settings 画面だけ**で、MCP の `settings_set`（`mcpwritesettings` のホワイトリスト）には入れず、自己改善ループ（Sol/Opus）の `policy.*` にも入らない（`evalflow/shadow_isolation_test.go`・`isolationflow` のテストで固定）。トライアルは費用がかかるため、MCP 経由で有効化できない。

| キー | 既定 | 意味 |
|------|------|------|
| `jev.trial.enabled` | `0`（オフ） | トライアルの有効。Paper のときだけ効く |
| `jev.trial.arms` | `[]`（空） | アームの JSON 配列。空なら何も呼ばない |
| `jev.trial.daily_cost_cap_usd` | 0.10 USD | 全アーム合計の 1 日（JST）の上限 |
| `jev.trial.halt` | なし | トライアル全体の 1 日の停止記録（全体上限・HTTP 402） |
| `jev.trial.halt.<arm>` / `jev.trial.failures.<arm>` / `jev.trial.throttle.<arm>` | なし | アームごとの停止記録・失敗回数・429/529 の記録。`jev.shadow.*` とは重ならない |

アーム（`jev.trial.arms` の要素）の形:

| フィールド | 意味 |
|------------|------|
| `id` | アーム名（`jev_trial_decisions.arm_id`。英小文字・数字・`.`・`_`・`-`、48 文字まで） |
| `provider` | 第 1 段階は `openrouter` のみ |
| `model` | 送るモデル id |
| `prompt_version` | 登録済みの Trader プロンプト版（例 `trader-v3`） |
| `daily_cost_cap_usd` | このアームの 1 日の上限（省略・0 は 0.05 USD） |
| `list_price_usd_per_mtok` | 1M 入力トークンあたりの定価。0・省略は不明（応答が費用を報告しなければ `unknown`）。上限には、`provider_only` を指定していないアームなら `max(定価, 最悪単価 0.24)` で数える（低い定価を書いても上限は甘くならない） |
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
| トライアルのキーは自己改善ループ（`ValidatePolicyChanges`）・MCP の `settings_set`・汎用の運用設定のどれでも書けず、`jev.shadow.*` と重ならない | 同 `TestTrialKeysAreOutsideEveryAutomaticWritePath` |
| ランナー・ワイヤ層は取引・学習のパッケージを import せず、ライブ Jev のシークレットを読まない | 同 `TestTrialRunnerImportsNoTradingPackageAndReadsNoLiveJevSecret` |
| ライブの判断経路（Jev 本体・プロンプト・判断の保存）・Risk・発注・RAG・較正・メタモデルは、トライアル側（`internal/service/jevtrial` / `internal/service/jev/trial` / `internal/service/jev/trialwire`）を、直接にも他のパッケージ経由でも import しない。トライアル側を import してよいのは、許可リスト（`internal/bootstrap`・`internal/web/handler/jevtrial`・`internal/web/handler/settings`・`internal/service/jev/trial`。理由はテストに明記）の中だけで、リストに載るのに import しなくなった項目もテストが落ちる | 同 `TestLivePathDoesNotImportTheTrial`（import グラフは `internal/` と `cmd/` の本番ソースから読む） |
| トライアル側のパッケージは、取引・学習のパッケージに加え、発注（`execution`・`broker`）と注文案の経路（`proposalexec`・`proposalstore`・`paperresultsmcp/proposal`）を直接 import しない（推移的には見ない。ワイヤ層はライブ Jev のプロンプトのパッケージの上に載り、それが Risk の上にあるため） | 同 `TestTrialPackagesImportNoTradingOrOrderPlacementPackage` |
| 上の 2 つの検査自体が、違反を見つけて失敗できること（合成した import グラフで確認） | 同 `TestImportGuardCheckersFailOnViolations` |
| `OPENROUTER_*` は必須キーでなく、Setup Guard も求めない | 同 `TestOpenRouterSecretsAreNotRequired`、`internal/config/secretsflow` |
| 設定が既定のとき、トライアルは動かず、追加の外部呼び出しもない／Paper 以外では動かない／アームは互いの答えを見ない／成功も失敗も行に残る／費用の出所と上限／402・403・429・529 の停止 | `internal/service/jev/trialflow`（ランナーの振る舞いのテスト） |

シャドーの同種のテスト（`internal/service/jev/evalflow/shadow_isolation_test.go`）は変更していない（`evalflow` ディレクトリが行数の上限に達していたため、トライアル分は同じ形で新ディレクトリ `isolationflow` に置いた）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-13 | 新規作成。トライアルのフロー・費用の数え方・上限・設定・隔離のテストを `integrations.md` §6 から分割して記述 | issue #1059 |
| 1.1 | 2026-11-14 | 隔離の保証の表に、ライブ側からトライアル側への import を許可リスト方式で禁じるテストと、トライアル側が発注に触れないことのテストを追加 | issue #1069 |
