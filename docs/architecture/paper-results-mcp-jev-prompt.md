# Paper 実績 MCP: Jev プロンプトの版管理・シャドー比較・昇格（§5.1・§7.3 ⑥）

`paper-results-mcp.md` の分割章（ファイルの行数上限のため。元の文書の §5.1 と §7.3 ⑥ にあたる）。issue #872。ツールの入出力は `docs/api/endpoints/paper-results-mcp-jev-prompt.md`（API 仕様 §3.29）。

悠太さんの方針: ボットは人の手を入れず全自動で取引し、人間とアドバイザー AI（あいり）が時間をかけて調整する。良し悪しの基準は**勝つかどうかだけ**（コスト控除後の純損益と最大ドローダウン）で、好みでは決めない。まず Paper で行う。Jev の trader の判定はプロンプトで大きく変わるが、これまで試す手段は「コードを書き換えて本番に出す」しかなかった。本章はその代わりに、候補のプロンプトを**取引に使わずに**同じ入力で動かして比べ、人が明示的に昇格する仕組みを定める。

## 1. 版

- Jev のプロンプトの版は `jev_decisions.question_version`（`VARCHAR(20)`・`NOT NULL`。マイグレーション `000004`）を**そのまま正式な版**とする。`prompt_version` 列は足さない。理由: ①すべての Scout・Trader の行が最初から埋めており、過去の行も埋まっている（バックフィルが要らない）。②同じ事実を 2 列に持つと食い違える。③`DecisionRepository.Insert` が空の版を拒否するので、以後の行にも必ず入る。シャドーの表 `jev_shadow_decisions` の候補の版の列は `prompt_version` という名前にした（ライブの版は `jev_decisions.question_version` を JOIN で見る）
- 版とプロンプトの中身は**ハッシュで結ぶ**。`internal/service/jev/evalflow/prompt_version_test.go` が、登録された版ごとに質問群（各質問の Instructions と選択肢の Rubric。ワイヤ形式）の SHA-256 を固定する。文言を変えて版を上げ忘れるとテストが落ちる。登録済みの版の文言は直さず、新しい版を足す（`internal/service/jev/prompts.go`）
- `traderPrompts` が版名から質問群を引く。ライブの版と候補の版を同じプロセスで同時に組み立てられる（`TraderQuestionsFor`）。登録されていない版は送らずエラー
- ライブの版は、ビルドの既定 `TraderQuestionVersion`（`trader-v3`）か、昇格が `runtime_settings` の `jev.trader_prompt_version` に保存した登録済みの版。Trader は呼び出しごとに読み、リクエストと `jev_decisions.question_version` に同じ値を使う（昇格が呼び出しの途中に入っても、行は実際に使った版を名乗る）

## 5.1 シャドー・費用・結果の比較

シャドーは、ライブの Trader の判定（`jev_decisions`、`decision_type=trader`）が保存されるたびに、**同じ `TraderState`・同じ RAG 文脈**で候補の版を非同期に呼ぶ。

- **取引に使わない**: 結果は別表 `jev_shadow_decisions`（マイグレーション `000047`）だけに保存する。Policy・メタモデル・Risk・発注・RAG の索引・Calibration の集計・メタモデルの学習は `jev_decisions` だけを読む。テスト（`shadow_isolation_test.go` ほか）が、これらがシャドーの名前を参照しないこと、シャドーの表に書くのはシャドーの repository だけであること、シャドーの行が RAG と `jev_decisions` に入らないことを固定する
- **ライブに影響しない**: 呼び出しは別の goroutine で、同時実行は 4 まで（満杯なら比較を捨ててログだけ）。失敗はログだけ。ローリングのエラー率（`jev_api_down` の Kill Switch）にも数えない。アプリの終了（`Services.Stop`）は `Trader.Close` で飛行中のシャドーの呼び出しを取り消して待つ（終了後に出された比較は捨てる）
- **Paper だけ**: `metalabelsettings.RuntimeMode` が Paper のビルドだけが動く。Live では何も呼ばない（テストで固定）。`RuntimeMode` は const で起動後に変わらないので、シャドーは起動時に 1 回だけモードを見ている。**実行時にモードを切り替える設計にするなら、`Submit` のたびに読む**こと（その変更は本 issue の範囲外）
- **既定は無効**: `runtime_settings` の `jev.shadow.enabled`（既定 0）と候補の版 `jev.shadow.candidate_version`。Settings 画面（「外部連携」→ Jev プロンプト）から切り替える。人が有効にするまで費用は発生しない。MCP から有効にする手段は無い
- **費用**: 各行の `request_cost` は、API が報告した入力トークン数 × 公表単価（Jev は 1M 入力トークンあたり 0.042 USD、出力は無料。2026-10-22 時点の公式の価格表）の**見積り**。API は課金額を返さない（請求書と突き合わせていない）。合計は JST の日ごとと累計で、Settings 画面と `jev_shadow_cost` で見られる。**応答に `usage` が無い呼び出しは 0 USD と数えない**（0 だと費用の上限をすり抜けて走り続ける）。リクエスト本文のバイト数を保守的な比（`shadow.BytesPerTokenFloor` = 2 バイト / トークン。実測のトークン化より低め＝多めに見積もる）で割った入力トークン数の**推定**で費用に入れ、行に `cost_is_estimate = 1` を付けて実測と区別し、GUI・`jev_shadow_cost` に「うち見積り」の件数・費用を出す。比は実際のトークナイザや請求との突き合わせをしていない仮定である
- **1 日の費用の上限**: `jev.shadow.daily_cost_cap_usd`（保存が無ければ `jevshadowsettings.DefaultDailyCostCapUSD` = 0.10 USD）。その日（JST）のシャドーの費用がこれに達したら、その日のシャドーを止めて理由（固定コード `daily_cost_cap`・日・使った額・上限）を `jev.shadow.halt` に残す。翌日（JST）に再開する。ライブの判定は止まらない（テストで固定）。**この上限の値（0.10 USD）はユーザー決定の確定値**（根拠は issue #872 の PR）。コード上の置き場は `DefaultDailyCostCapUSD` の 1 か所で、Settings 画面で上げ下げできる。GUI は、保存した値が無いあいだ入力欄を空にして既定値を placeholder で見せる（空のまま保存しても既定値を「保存した値」にしない）。MCP の `cap_source`（`default` = 既定値 / `stored` = 保存した値）も同じ事実を返す
- **1 日の失敗回数の上限**: 費用の合計は**保存に成功した呼び出し**の `request_cost` しか足せない。ベンダーが課金しても保存されない呼び出し（応答の検証失敗で `usage` が捨てられる、タイムアウト、保存の失敗）は合計に入らず、候補が毎回不正な応答を返すと費用 0 のまま課金され続ける。そこで**失敗した呼び出しの回数**も数え（`jev.shadow.failures`。JST の日ごと）、`jevshadowsettings.DailyFailureCap`（20 回。確定（ユーザー決定）。設定ではなく定数 1 か所）に達したらその日のシャドーを止め、理由（固定コード `daily_failure_cap`・失敗回数・上限）を `jev.shadow.halt` に残す。未記録の課金額はこの回数で抑える。ライブの判定は止まらない（テストで固定）
- **Jev API のレート制限の共有と、シャドーの 429/529 の扱い（issue #883）**: シャドーはライブの Scout・Trader（と Luna・Sol・Opus）と**同じ Jev の API クライアント・同じ API キー**で動くので、API の制限（キー＝組織単位の「1 秒あたりのリクエスト数」と「1 秒あたりのトークン数」。実際の値は `console.typesafe.ai/rate-limits`。`integrations.md` §6）を共有する。ライブの呼び出しがシャドーのせいで 429 や遅延を受けないよう、シャドーは次のように**譲る**（方式はすべて実装者の選択で、理由は issue #883 の PR に書いた）。①**待たず・再試行せず止まる**: シャドーの呼び出し（`Client.ShadowTrader`）は 429（レート制限）・529（過負荷）を受けたら再試行も待ち（バックオフ）もせず 1 回で失敗にする（ライブは従来どおり最大 4 試行で待つ。ほかの再試行できる失敗＝通信エラー・5xx は従来どおりシャドーも再試行する）。②**止める時間（クールダウン）**: 受けたらシャドーは `jevshadowsettings.ThrottleCooldown`（15 分。定数）のあいだ何も送らない。再開後は 1 回だけ試し、また 429/529 ならまた止まる。③**ライブが受けた 429/529 でも止まる**: `Client.LastThrottle` が Scout・Trader・Ask の最後の 429/529 を覚えており（シャドー自身のものは入らない）、シャドーは次の比較の前にそれを見て、クールダウン内なら送らない。④**呼び出しの間隔**: シャドーの呼び出しは `jevshadowsettings.MinCallInterval`（2 秒。定数）より短い間隔では出さず、その比較は捨てる（遅らせない。ライブの判定は出る）。同時実行 4 のままなので、シャドーがライブのキーに足すのは**最大でも 0.5 リクエスト/秒**。⑤**費用の上限・失敗回数の上限とは別**: 429/529 は応答が返らず課金もされない呼び出しなので、失敗回数（`DailyFailureCap`）にも 1 日の停止（`jev.shadow.halt`）にも数えない。**止めた理由の表示**: `runtime_settings` の `jev.shadow.throttle`（日・その日の回数・状態コード 429/529・固定コード `api_rate_limited` / `api_overloaded`・出どころ `shadow`（シャドー自身の呼び出し）/ `live`（ライブの呼び出し）・時刻・再開の時刻）に残し、Settings 画面（「外部連携」→ Jev プロンプトの費用の欄の `jev-prompt-throttle`）と `jev_shadow_cost`（`throttle_active`・`last_throttle`・`throttles_today`・`throttle_cooldown_seconds`・`min_call_interval_seconds`）が見せる。ライブの判断・Kill Switch の判定（ローリングのエラー率）は変わらない（`ShadowTrader` はエラー率に数えない・`LastThrottle` の記録はライブの挙動に影響しない。テストで固定: `evalflow/shadow_throttle_test.go`）。
- **間隔で捨てた比較は記録されない（issue #883）**: 上の④（2 秒の最小間隔）で捨てた比較は、`jev_shadow_decisions` にも失敗回数にも停止の記録にも**残らず**、数える項目も作らない。したがって `jev_shadow_compare` の標本は、ライブの判定の数より**間引かれる**（短い間に続いた判定は 2 秒に 1 件ほどしか比べない）。標本が小さいことを「ライブとの差がない」と読まず、比べた行数を見て判断する。
- **本番でシャドーを有効にしない（issue #883）**: 上の譲る仕組みは**フェイクの API でしか検証していない**。実 API で、シャドーを有効にしてもライブの 429 が増えないことは未確認（本番の Windows PC・実 API が要る）。Settings の既定は**無効**のまま（変えない）。issue #883 が完了するまで、本番の Windows PC でシャドーを有効にしない。完了後に有効にするときは、`integrations.md` §6 の「シャドーで 429 が増えるかの確認方法」に従い、有効にする前後の 429 の件数を比べる

### 結果の比較の計算方法（`jev_shadow_compare`）

`calibration_outcomes`（`jev_decision_id` で結ぶ。`JevCalibration` と同じ結び方）は**ライブの方向**に対してラベルされている。`was_direction_correct`・`barrier_hit`・`net_return_bps` はライブの方向に対する値で、`future_return` はライブの判定時点の参照価格から**ライブのラベルを終わらせた足**までの価格変化率（%）である。その足は、ライブの方向の利確・損切りのバリアに触れた足か、ライブが `NONE`（またはバリアに触れない）ならホライゾンの終端である。

したがって側の方向で `future_return` に符号を付けて採点できるのは、**終端の足がライブの方向に依存しない行**だけである: その側の方向がライブの方向と同じ（その方向自身のバリアで終わる）か、ライブが `NONE`（ホライゾンの終端）の行。

1. 採点できる行（`comparable`）: その側の方向が `LONG` / `SHORT` で、ライブと同じ方向かライブが `NONE`。符号付きリターン = `LONG` は `+future_return`、`SHORT` は `-future_return`。`NONE` は取引ではないので `samples` には数えるが `directional` には数えない
2. **方向が食い違う行は採点しない**（`excluded_disagreement` = `directional` − `comparable`。ライブ側は常に 0）。理由: `future_return` はライブのバリアで止まるので、ライブの結末がシャドーの結果を決めてしまう。**偏りの向き**: ライブ LONG が損切りで終われば、反対向きのシャドー SHORT は必ず hit になり、ライブ LONG が利確で終われば必ず miss になる。食い違う行を符号反転だけで採点すると、hit_rate と平均リターンがライブの結末に引きずられて偏る（応答の `outcome_note` にも同じ注意を固定の文で返す）
3. `hit_rate` = 符号付きリターンが 0 より大きい行 / `comparable`。`avg_directional_return_pct` = `comparable` の符号付きリターンの平均。**ライブ側にも同じ式を使う**ので、2 つは同じ物差しで並ぶ。ただしシャドーの側は食い違う行を除くため、比べられるのは「ライブと同じ向きで入った行」と「ライブが入らなかった行」に限られる（シャドーだけが反対向きに入った判断の良し悪しは、この表からは分からない）
4. この再計算はコストを引かない総リターンで、ライブのラベルの参照価格と終端の足を流用した近似である。どのエントリーをどのプロンプトが採るかを比べる材料であって、シャドーの取引の損益ではない（シャドーは取引しない）。実際の純損益とドローダウンは、昇格のあと #871 の実験で評価する
5. ラベルが記録した値（`recorded_*`）は、その側の方向がライブの方向と同じ行だけに限って別に出す。`label_version` と `horizon_minutes` ごとに分け、混ぜない（FR-CAL-4）

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
| 1.0 | 2026-10-22 | 新規作成（元の文書の版 1.23 で追加）。版（`question_version` を正式な版にした理由とハッシュによる固定）、シャドー・費用・上限、結果の比較の計算方法、昇格（書き手の 6 つ目の変更） | issue #872 |
| 1.1 | 2026-10-22 | 結果の比較: 方向がライブと食い違う行は採点しない（偏りの向きを明記）。1 日の失敗回数の上限、Settings 画面の費用上限の入力欄（既定値を保存した値にしない）、終了時のシャドーの取り消し、`RuntimeMode` が const であることの注意を追記 | issue #872 レビュー対応 |
| 1.2 | 2026-10-22 | 費用の上限 0.10 USD・失敗回数の上限 20 回・単価 0.042 USD/1M 入力トークン・同時実行 4 をユーザー決定の確定値にした（「暫定の案・未承認」を廃止）。`usage` が無い応答を、リクエストの大きさから推定した入力トークン数で費用に入れる（0 USD と数えない）。`cost_is_estimate` と「うち見積り」を追加 | issue #872 ユーザー決定 |
| 1.3 | 2026-10-22 | シャドーが Jev の API キーとレート制限をライブと共有することへの対策を追加（429/529 を待たず・再試行せず止まる 15 分のクールダウン、ライブが受けた 429/529 でも止まる、呼び出しの最小間隔 2 秒、止めた理由の `jev.shadow.throttle` と Settings 画面・`jev_shadow_cost` での表示）。実 API で未確認のため、本番でシャドーを有効にしない旨と Settings 既定の無効を明記 | issue #883 |
