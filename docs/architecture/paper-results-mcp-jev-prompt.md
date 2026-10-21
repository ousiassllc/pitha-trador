# Paper 実績 MCP: Jev プロンプトの版管理・シャドー比較・昇格（§5.1・§7.3 ⑥）

`paper-results-mcp.md` の分割章（ファイルの行数上限のため。元の文書の §5.1 と §7.3 ⑥ にあたる）。issue #872。ツールの入出力は `docs/api/endpoints/paper-results-mcp-jev-prompt.md`（API 仕様 §3.23）。

悠太さんの方針: ボットは人の手を入れず全自動で取引し、人間とアドバイザー AI（あいり）が時間をかけて調整する。良し悪しの基準は**勝つかどうかだけ**（コスト控除後の純損益と最大ドローダウン）で、好みでは決めない。まず Paper で行う。Jev の trader の判定はプロンプトで大きく変わるが、これまで試す手段は「コードを書き換えて本番に出す」しかなかった。本章はその代わりに、候補のプロンプトを**取引に使わずに**同じ入力で動かして比べ、人が明示的に昇格する仕組みを定める。

## 1. 版

- Jev のプロンプトの版は `jev_decisions.question_version`（`VARCHAR(20)`・`NOT NULL`。マイグレーション `000004`）を**そのまま正式な版**とする。`prompt_version` 列は足さない。理由: ①すべての Scout・Trader の行が最初から埋めており、過去の行も埋まっている（バックフィルが要らない）。②同じ事実を 2 列に持つと食い違える。③`DecisionRepository.Insert` が空の版を拒否するので、以後の行にも必ず入る。シャドーの表 `jev_shadow_decisions` の候補の版の列は `prompt_version` という名前にした（ライブの版は `jev_decisions.question_version` を JOIN で見る）
- 版とプロンプトの中身は**ハッシュで結ぶ**。`internal/service/jev/evalflow/prompt_version_test.go` が、登録された版ごとに質問群（各質問の Instructions と選択肢の Rubric。ワイヤ形式）の SHA-256 を固定する。文言を変えて版を上げ忘れるとテストが落ちる。登録済みの版の文言は直さず、新しい版を足す（`internal/service/jev/prompts.go`）
- `traderPrompts` が版名から質問群を引く。ライブの版と候補の版を同じプロセスで同時に組み立てられる（`TraderQuestionsFor`）。登録されていない版は送らずエラー
- ライブの版は、ビルドの既定 `TraderQuestionVersion`（`trader-v3`）か、昇格が `runtime_settings` の `jev.trader_prompt_version` に保存した登録済みの版。Trader は呼び出しごとに読み、リクエストと `jev_decisions.question_version` に同じ値を使う（昇格が呼び出しの途中に入っても、行は実際に使った版を名乗る）

## 5.1 シャドー・費用・結果の比較

シャドーは、ライブの Trader の判定（`jev_decisions`、`decision_type=trader`）が保存されるたびに、**同じ `TraderState`・同じ RAG 文脈**で候補の版を非同期に呼ぶ。

- **取引に使わない**: 結果は別表 `jev_shadow_decisions`（マイグレーション `000048`）だけに保存する。Policy・メタモデル・Risk・発注・RAG の索引・Calibration の集計・メタモデルの学習は `jev_decisions` だけを読む。テスト（`shadow_isolation_test.go` ほか）が、これらがシャドーの名前を参照しないこと、シャドーの表に書くのはシャドーの repository だけであること、シャドーの行が RAG と `jev_decisions` に入らないことを固定する
- **ライブに影響しない**: 呼び出しは別の goroutine で、同時実行は 4 まで（満杯なら比較を捨ててログだけ）。失敗はログだけ。ローリングのエラー率（`jev_api_down` の Kill Switch）にも数えない
- **Paper だけ**: `metalabelsettings.RuntimeMode` が Paper のビルドだけが動く。Live では何も呼ばない（テストで固定）
- **既定は無効**: `runtime_settings` の `jev.shadow.enabled`（既定 0）と候補の版 `jev.shadow.candidate_version`。Settings 画面（「外部連携」→ Jev プロンプト）から切り替える。人が有効にするまで費用は発生しない。MCP から有効にする手段は無い
- **費用**: 各行の `request_cost` は、API が報告した入力トークン数 × 公表単価（Jev は 1M 入力トークンあたり 0.042 USD、出力は無料。2026-10-22 時点の公式の価格表）の**見積り**。API は課金額を返さない（請求書と突き合わせていない）。合計は JST の日ごとと累計で、Settings 画面と `jev_shadow_cost` で見られる
- **1 日の費用の上限**: `jev.shadow.daily_cost_cap_usd`（保存が無ければ `jevshadowsettings.ProposedDailyCostCapUSD`）。その日（JST）のシャドーの費用がこれに達したら、その日のシャドーを止めて理由（固定コード `daily_cost_cap`・日・使った額・上限）を `jev.shadow.halt` に残す。翌日（JST）に再開する。ライブの判定は止まらない（テストで固定）。**この上限の値は暫定の案で未承認**（根拠は issue #872 の PR）。コード上の置き場は `ProposedDailyCostCapUSD` の 1 か所で、承認済みとして扱わない

### 結果の比較の計算方法（`jev_shadow_compare`）

`calibration_outcomes`（`jev_decision_id` で結ぶ。`JevCalibration` と同じ結び方）は**ライブの方向**に対してラベルされている。`was_direction_correct`・`barrier_hit`・`net_return_bps` はライブの方向に対する値で、`future_return` はライブの判定時点の参照価格からラベルの終端までの価格変化率（%）である。シャドーの方向がライブと違う行では、その値をそのままシャドーの結果にできないので、次のように計算し直す。

1. 側（ライブ／シャドー）の方向で `future_return` に符号を付ける: `LONG` は `+future_return`、`SHORT` は `-future_return`。`NONE` は取引ではないので `samples` には数えるが `directional` には数えない
2. `hit_rate` = 符号付きリターンが 0 より大きい行 / `directional`。`avg_directional_return_pct` = 符号付きリターンの平均。**ライブ側にも同じ式を使う**ので、2 つは同じ物差しで並ぶ
3. この再計算はコストを引かない総リターンで、ライブのラベルの参照価格と終端の足を流用した近似である。分布を比べる材料であって、シャドーの取引の損益ではない（シャドーは取引しない）。実際の純損益とドローダウンは、昇格のあと #871 の実験で評価する
4. ラベルが記録した値（`recorded_*`）は、その側の方向がライブの方向と同じ行だけに限って別に出す（シャドーの側は一致した行だけ）。`label_version` と `horizon_minutes` ごとに分け、混ぜない（FR-CAL-4）

`entry_quality` の分布（5 水準の件数と割合）・`direction` の一致率・シャドーの費用は、同じ入力の組（ライブの判定とそのシャドー）について並べる。ライブの `entry_quality` は `jev_decisions.response_json` の `entry_quality`（`jev_decisions` に列は無い）から 1 文字列だけ取り出す。

## 7.3 ⑥ 昇格（`jev_prompt_promote`）

書き手（`mcpset.Writer`）が実行する 6 つ目の変更。ライブの版を候補の版に切り替える**明示的な操作**で、1 トランザクションに次の 2 つだけを書く: `runtime_settings` の `jev.trader_prompt_version` の upsert と、昇格の監査 `jev_prompt_promotions` への 1 行（誰が＝`mcp` と接続元 IP／`gui`、いつ、前の版、後の版）。専用の表にした理由は、`mcp_settings_audit` の `CHECK`（操作名）と `settings_diff` の再構成が、設定のキーと値の組を前提にしているため。

- 書き込み許可のスイッチ（§7.2）に従う。オフなら `write_disabled` で何も書かない
- 版は登録済みで、かつ現在のライブの版と違うものだけ
- **自動では昇格しない**。MCP の `jev_prompt_promote` と Settings 画面の昇格ボタン（`POST /settings/jev-prompt/promote`。セッション Cookie と CSRF が必要）だけが呼ぶ
- 自己改善ループ（Sol/Opus）からはできない。`domain.ValidatePolicyChanges` は `policy.*` のキーだけを受け付け、`jev.trader_prompt_version`・`jev.shadow.*`・`prompt_version` などを拒否する（境界は変えず、テストで固定）。シャドーの設定キーは `mcpwritesettings` のホワイトリストにも無い（`settings_set` は `key_not_allowed`）

## 最初の候補（trader-v4。昇格されない）

`entry_quality` の定義だけを変えた候補版 `trader-v4`（`questions_trader_v4.go`）を登録した。ライブの版 `trader-v3` は変えない。シャドーは既定で無効で、昇格は人の明示的な操作だけ。差分と理由は issue #872 の PR に「候補の案（人がレビューして決める）」として書く。その版のシャドーの分布は実際の LLM が要るので、本番の Windows PC で人がシャドーを有効にして初めて分かる。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-22 | 新規作成（元の文書の版 1.15 で追加）。版（`question_version` を正式な版にした理由とハッシュによる固定）、シャドー・費用・上限、結果の比較の計算方法、昇格（書き手の 6 つ目の変更） | issue #872 |
