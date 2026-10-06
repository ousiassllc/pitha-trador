# 機能要件: コンポーネント別機能要件（§4.1〜§4.9 パイプライン）

`docs/requirements/functional.md` §4 から分割した章。§4.1 Feature Engine / §4.2 Fast Screener / §4.3 スキャン頻度・イベント駆動 / §4.4 Jev Scout / §4.5 Jev Trader / §4.6 Policy Engine / §4.7 Risk Engine / §4.8 Entry / Exit / §4.9 状態管理。節番号・FR-ID は分割前と同一。

### 4.1 Feature Engine

市場データから以下の特徴量を算出する。

| カテゴリ | 特徴量 |
|---------|--------|
| 価格 | return_1m/3m/5m/15m/30m, high_distance_5m, low_distance_5m, session_high_distance, session_low_distance |
| VWAP | vwap, price_vs_vwap_bps, vwap_slope, vwap_cross_direction |
| 出来高 | volume_1m/5m, volume_ratio_1m/5m, turnover_1m/5m |
| ボラティリティ | atr_1m/5m, realized_vol_5m/15m, volatility_expansion_ratio |
| 板・約定（取得可能な場合） | best_bid, best_ask, spread_bps, bid_depth, ask_depth, orderbook_imbalance, buy_trade_ratio, sell_trade_ratio, trade_flow_imbalance, microprice |
| 市場コンテキスト | TOPIX/Nikkei225 return_1m/5m, sector_return_5m, stock_vs_sector_relative_strength, market_breadth |

- FR-FE-1: すべての特徴量は判定時点までのデータのみで算出する（look-ahead防止、§4.11 バックテスト（FR-BT-3）参照）
- FR-FE-2: 板・約定特徴量はkabuステーションAPIから取得できない銘柄・時間帯では欠損値として扱い、依存するJev入力/スコアから除外する。bid/askが逆転した板（bid > ask）も無効として`spread_bps`/`microprice`を欠損とし、スプレッド上限ガードが負値を素通りしないようにする
- FR-FE-3: `volume`/`turnover`はkabuステーションAPIの当日累積値である。`volume_1m/5m`・`turnover_1m/5m`は「現在の累積値 − 窓の開始時点の累積値」で算出し、足の合算はしない。Fast Screenerの`min_turnover_5m_jpy`とPolicy Engineの「板が薄い」判定は同じ`turnover_5m`（`featureengine.TurnoverOverWindow`）を用いる
- FR-FE-4: 市場コンテキストは`instruments.kind`で区別した追跡銘柄から算出する。`market_index`（TOPIX/Nikkei225等）のreturn平均を`market_return_1m/5m`、銘柄の`sector`と一致する`sector_index`のreturnを`sector_return_5m`とし、`stock_vs_sector_relative_strength = return_5m − sector_return_5m`、`market_breadth`は直近3分以内の全アクティブ株式の最新return_5mの（上昇数−下落数）/銘柄数とする。追跡銘柄が未登録・更新が3分以上停止・履歴不足の場合は欠損値とする（FR-FE-2と同じ扱い）。既定のランキング監視（FR-SCHED-9）では指数行（`market_index`と監視銘柄の`sector`の`sector_index`）も毎サイクル取り込まれるため`market_return_*`・`sector_return_5m`は算出されるが、`market_breadth`の「全アクティブ株式」のうち直近3分以内に更新されるのは監視銘柄（最大45）だけなので、監視銘柄の上昇・下落比になる（`scan.full_scan_enabled: true`のときだけ全体の比になる）
- FR-FE-5: 窓（`return_*`・`vwap_slope`・`volume_*`・`turnover_*`・`vwap_cross_direction`等）の基準バーは、窓開始時刻（判定時刻−窓幅）から「窓幅の50%（最小90秒）」以内に存在しなければならない。それより古い基準バー（前営業日の引け・昼休み前の前場最終バー・再起動/欠測をまたぐバー）しか無い場合は履歴不足として欠損値（nil）とし、ギャップを「N分リターン」として扱わない（`featureengine.windowRef`）。**`realized_vol_*`はこの列挙の対象外**で、窓開始の基準バーを要求せず、窓内の1分刻みの各マークごとに直近バーを90秒（`minRefTolerance`）以内の許容で探して1分リターンを作り（それより古いバーしか無いマークのリターンは除く）、2本以上のリターンが得られれば算出する（得られなければnil。`featureengine.realizedVol`）。このため`realized_vol_15m`は履歴が3分程度でも非nilになりうる（部分窓の値）。この値は`volatility_expansion_ratio`・Fast Screenerの`min_realized_volatility`にそのまま流れる

### 4.2 Fast Screener

- FR-FS-1: Jev API呼び出し前に、環境変数/DB設定値（`min_price`, `max_price`, `min_turnover_5m_jpy`, `max_spread_bps`, `min_volume_ratio`, `min_abs_return_5m_pct`, `min_realized_volatility`）で明らかに対象外の銘柄を除外する（全フィルターを評価して除外理由を保持し、Scanner Dashboardのスキャン状況パネル（FR-SCAN-3〜6、除外・欠損理由の表示は FR-SCAN-4/5）で銘柄別に参照できる。値が欠損の銘柄は閾値未達ではなく欠損理由として区別する。約定不能な足も同じくJev呼び出し前に除外する（issue #511）: **特別気配**（板の`BidSign`/`AskSign`が`0102`特別気配・`0108`停止前特別気配。理由`special_quote`）と**ストップ高/ストップ安**（現値が銘柄情報`UpperLimit`/`LowerLimit`に到達。理由`limit_up`/`limit_down`）は、スナップショットの`special_quote`/`price_limit`から判定され、除外理由としてScanner Dashboardに表示される（欠損ではなく`excluded`）。ショート方向が確定するのはJev Trader後のためFast Screenerでは貸借を見ず、貸借なしのショートはFR-POLICY-3で外す。`min_abs_return_5m_pct`の単位は**パーセント**（0.3 = 0.3%）で、Feature Engineの`return_5m`（小数比、0.003 = 0.3%）を×100して比較する）
- FR-FS-2: 通過銘柄に対し以下のスコアを算出し、上位N銘柄のみJev Scoutへ送る。Nは`fast_screener.top_n`で、同梱既定は20（`config/strategy.yaml`。Jev APIコストを抑える側の値）。50〜200へ引き上げる場合は`non-functional.md` §2.1のAPI呼び出し上限（Nに比例）を確認する

```text
screen_score =
  w1 * normalized_volume_ratio   # = volume_ratio_5m（下記）
+ w2 * abs(return_5m)   # return_5mは小数比（%換算しない）
+ w3 * breakout_strength
+ w4 * orderbook_imbalance
+ w5 * volatility_expansion
```

  - `normalized_volume_ratio`: Feature Engineが算出する`volume_ratio_5m`（5分出来高の現在値/平均の比率。通常1.0前後）をそのまま用いる。追加の正規化・キャップ・スケーリングは行わない（実装は`screener.ScreenScore`が`weights.volume_ratio * volume_ratio_5m`を加算する）。「正規化済みの比率」という意味でのみ`normalized`と呼ぶ。履歴不足で算出できない場合は下記のとおり項ごと欠損として除外する
  - `breakout_strength`: 直前5分間（判定時点の足を除く）の高値/安値に対する現在価格のブレイク幅（高値上抜け時は`price/high - 1`、安値下抜け時は`1 - price/low`、レンジ内は`0`）。Feature Engine（`featureengine.ComputeScreenSignals`）が算出する
  - `volatility_expansion`: `volatility_expansion_ratio`（`realized_vol_5m / realized_vol_15m`）。同上
  - 履歴不足で算出できない項は0ではなく欠損として合計から除外する（FR-FE-2と同じ扱い）

- FR-FS-3: フィルター設定値・スコア重みはすべて環境変数またはDBで変更可能とする。優先順位は`config/strategy.yaml` < 環境変数`PITHA_FAST_SCREENER_*`（起動時に読込み。例: `PITHA_FAST_SCREENER_MIN_PRICE`, `PITHA_FAST_SCREENER_TOP_N`, `PITHA_FAST_SCREENER_WEIGHT_BREAKOUT_STRENGTH`）< DB `runtime_settings`の`screener.*`キー（候補更新周期ごとに読み込むため再起動不要。キー一覧は`architecture/er.md` §runtime_settings）
- FR-FS-4（起動時検証）: `config/strategy.yaml`の`fast_screener.*`は、`scan.*`（既定値補完。FR-SCAN-1/FR-SCAN-2）と異なり**補完せず検証する**。`min_price`・`max_price`・`min_turnover_5m_jpy`・`max_spread_bps`・`min_volume_ratio`・`min_abs_return_5m_pct`・`min_realized_volatility`は0より大、`max_price >= min_price`、`top_n >= 1`（0は全銘柄が`top_n_cutoff`で除外されJev Scoutが一度も走らない、負数は上位N件の切り出しで範囲外参照になるため不可）、`weights.*`は0以上かつ少なくとも1つは0より大。キー欠落・タイプミスは0として読み込まれるため、未設定も違反として検出される。違反は項目名（例: `fast_screener.top_n`）付きで全件まとめて報告し、起動に失敗する。検証は`PITHA_FAST_SCREENER_*`環境変数の適用後に行うため、環境変数経由の不正値（例: `PITHA_FAST_SCREENER_TOP_N=0`）も拒否する。`screener.Screen`は防御として`top_n <= 0`でも候補0件を返し、パニックしない（DB `runtime_settings`の`screener.top_n`のような実行時上書き経路向け） 同じ不変条件はDBの`runtime_settings`の`screener.*`上書き（FR-FS-3の最上位層）にも適用する: 候補更新（`internal/bootstrap/candidates`の`fastScreenerConfig`）は上書きが1件でもあれば全キー適用後に検証し（`config.ValidateFastScreenerOverrides`）、違反時は項目名（例: `screener.top_n`）付きのエラーで`Refresh`を失敗させ、候補リストを不正値で更新しない（`screener.top_n=0`も同じ検証で拒否する）。

### 4.3 スキャン頻度・イベント駆動

| 対象 | 周期 |
|------|------|
| 全体スキャン | 60秒ごと（`scan.full_scan_enabled: true`のときのみ。**既定オフ**。既定ではランキング監視が毎分、監視銘柄だけを対象にする。FR-SCHED-7/9） |
| 候補銘柄更新 | 15〜30秒ごと |
| ポジション保有銘柄 | 5〜15秒ごと |

- 周期は`config/strategy.yaml`の`scan.full_scan_interval_seconds`（60。`scan.full_scan_enabled: true`のときだけ使う。既定のランキング監視の周期は60秒固定）/ `scan.candidate_refresh_interval_seconds_min`・`_max`（15・30）/ `scan.held_position_interval_seconds_min`・`_max`（5・15）/ `scan.jev_scout_min_interval_seconds`（60）で設定する（括弧内は同梱の既定値、単位は秒）。各値は正の整数で、`_max >= _min`であること。kabu情報API・銘柄登録APIのプロセス全体上限は`scan.kabu_info_api_max_per_second`（既定8、公式上限10。`non-functional.md` §2.3）。設定ローダー（`internal/config/scan_defaults.go`）は、未設定または0以下のキーを同梱既定値で補完し、`_max < _min`の`_max`を`_min`に引き上げ、`kabu_info_api_max_per_second`が10を超える場合は10に切り下げる。いずれも警告ログを出す。0のままだと候補更新が待機なしで回り、全体スキャンが`@every 0s`で登録されるため（ホットループ防止）

- FR-SCAN-1: 以下のいずれかを満たした銘柄は通常周期を待たず再評価する: 1分リターン急変、出来高急増、スプレッド急拡大、板インバランス急変、VWAPクロス、高値/安値ブレイク、約定フロー急変（直近2バーの`trade_flow_imbalance`の差の絶対値が`config/strategy.yaml`の`scan.event_trigger.trade_flow_imbalance_change_threshold`以上。どちらかが欠損の場合は無信号）、ニュースフラグ発生。前バー（`history[0]`）との比較で判定するスプレッド急拡大・板インバランス急変・VWAPクロス・約定フロー急変は、前バーが現在バーの90秒以内（FR-FE-5の`minRefTolerance`と同じ。`eventtrigger.MaxPrevGap`）かつ同一セッション（同一JST日の前場または後場。`marketcalendar.SameSession`）にある場合のみ評価し、前営業日の引け・昼休み前の前場最終バー・再起動/欠測をまたぐ場合は発火しない。VWAPクロスは両バーにVWAPがある（`vwap > 0`）場合のみ判定する。高値/安値ブレイクは現在バーと同一セッションの履歴バーだけを基準にし、同一セッションの履歴がなければ発火しない
- FR-SCAN-2（再評価抑制）: `abs(return_1m) < threshold AND abs(volume_ratio_5m) < threshold AND abs(spread_change) < threshold AND no_event` の場合はFR-SCAN-1の**イベント駆動の即時再評価をスキップ**し、APIコストとレイテンシを削減する。適用範囲は`internal/bootstrap/marketdatajob`の即時再評価経路（`eventtrigger.Detect`の`Signal.Triggered()`が偽なら`Scheduler.EnqueueEventReevaluation`が何も投入しない。対象は現在のFast Screener候補のみ）に限る。定期の候補更新サイクル（下記）は静穏判定を行わず、Fast Screener通過銘柄すべてを銘柄別の間引きだけでScoutへ投入する（静穏な銘柄も最大で`scan.jev_scout_min_interval_seconds`ごとに1回はJevで再評価され、判断の鮮度とRAG/Calibration用の判断履歴を保つ。コストの上限は間引きで担保する）
- 候補更新サイクル（15〜30秒周期）からの`jev-scout`ジョブ投入は銘柄ごとに間引く（非機能§2.1の上限「1分あたりN銘柄×2（Scout+Trader）」を担保する機構）: 同一銘柄に`pending`/`running`の`jev-scout`ジョブがある間、および同一銘柄の直近の`jev-scout`ジョブ完了（成功・失敗とも）から`scan.jev_scout_min_interval_seconds`（既定60秒）経過するまでは投入しない（`internal/bootstrap/candidates`の`scoutHeld`）。これにより同一銘柄のScoutは通常1分あたり1回まで（通過銘柄のTraderを含めてもN×2/分以内）。FR-SCAN-1のイベント発火による即時再評価（`Scheduler.EnqueueEventReevaluation`）はこの間隔の対象外で、通常周期を待たず投入される（ただしその`jev-scout`ジョブも上記の`pending`/`running`・直近完了の判定対象になるため、直後の候補更新サイクルでの重複投入は起きない）。ワーカーの完了書き込み失敗で`running`のまま残った孤児行は、`started_at`から固定10分超でSchedulerが`failed`へ回復する（1分ごと。`non-functional.md` §2.1）ため、その銘柄は回復後（失敗扱いの完了時刻から`scan.jev_scout_min_interval_seconds`経過後）に再び投入され、再起動まで保留され続けることはない
  - 各thresholdは`config/strategy.yaml`の`scan.event_trigger.*`（`return_1m_change_threshold` / `volume_ratio_change_threshold` / `spread_change_bps_threshold` / `orderbook_imbalance_change_threshold` / `trade_flow_imbalance_change_threshold`）で設定する。判定は`abs(値) >= threshold`で、`return_1m_change_threshold`は1分リターン(`return_1m`)の絶対値、`volume_ratio_change_threshold`は5分出来高比率(`volume_ratio_5m`)の絶対値（前バーとの差ではなく現在値の水準判定。`volume_ratio_5m`は通常1.0前後の比率のため既定2.0は「5分出来高が平均の2倍以上」を意味する）、`spread_change_bps_threshold` / `orderbook_imbalance_change_threshold` / `trade_flow_imbalance_change_threshold`は直近2バーの差の絶対値と比較する。したがって、thresholdが0以下だと全バーでFR-SCAN-1が発火しFR-SCAN-2が無効化される。キー欠落（新キー追加前の古い`strategy.yaml`等）や0以下の値は設定ローダー（`LoadStrategy` / `LoadStrategyBytes`）が同梱既定値（`config/strategy.yaml`の値）で補完し、警告ログを出す

### 4.4 Jev Scout

- FR-SCOUT-1: 1回のJev呼び出し（`POST /v1/systemone`、`architecture/overview.md` §6）で以下の質問群を評価する: `interesting_now`（`noul`型: yesの確率0〜1）, `momentum_quality`（`choice`型: weak/moderate/strong/exceptional）, `liquidity_ok`（`noul`型）, `abnormal_activity`（`noul`型）。入力は`market`（現在の市場状態）と`similar_past_cases`（RAGの類似過去事例）。応答が必須answerの欠落・型不一致・定義外のchoice・範囲外のnoulを含む場合は不正応答として失敗扱いにする
- FR-SCOUT-2: 通過条件は `interesting_now >= 0.65 AND liquidity_ok >= 0.70 AND abnormal_activity >= 0.55`（初期値。バックテスト後に調整）
- FR-SCOUT-2a（起動時検証）: `jev_scout.min_interesting_now`・`min_liquidity_ok`・`min_abnormal_activity`は`(0, 1]`であること。キー欠落・タイプミスは0として読み込まれ、`>=`比較が全銘柄で真になりFast Screener通過銘柄がすべてJev Traderへ流れる（コスト上限N×2/分の前提が崩れる）ため、補完せず項目名（例: `jev_scout.min_liquidity_ok`）付きの起動エラーとする（FR-FS-4と同じ流儀）
- FR-SCOUT-3: 入力・出力・状態ハッシュ・レイテンシ・モデルID（応答の`model`）を`jev_decisions`（decision_type=scout）に保存する。Jev APIは課金額を返さないため`request_cost`はNULLのままとする。質問セットのバージョン（現行`scout-v3`）を`question_version`に記録する

### 4.5 Jev Trader

- FR-TRADER-1: Scout通過銘柄に対し以下を評価する（FR-SCOUT-1と同じく`POST /v1/systemone`、入力は`market`と`similar_past_cases`）: `direction`（`choice`型: LONG/SHORT/NONE）, `regime`（`choice`型: TREND/RANGE/BREAKOUT/CHAOTIC）, `entry_quality`（`choice`型: poor/fair/good/strong/exceptional）, `toxic_flow`（`noul`型）, `liquidity_stressed`（`noul`型）, `continuation_probability`（`noul`型）。`confidence`は`direction`回答の`confidence`を用いる。質問セットのバージョンは現行`trader-v3`
- FR-TRADER-2: Jevのconfidence/probability（`noul`の値、`choice`の`confidence`）を実際の株価上昇確率とみなさない。実結果との対応はCalibrationで独自に検証する
- FR-TRADER-3: 入出力を`jev_decisions`（decision_type=trader）に保存する

### 4.6 Policy Engine

- FR-POLICY-1: LONG条件: `direction == LONG AND P(LONG) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-2: SHORT条件: `direction == SHORT AND P(SHORT) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-2a（起動時検証）: `policy.long`/`policy.short`の`min_probability`・`min_continuation_probability`・`max_toxic_flow`・`max_liquidity_stressed`は`(0, 1]`、`min_entry_quality`は`poor`/`fair`/`good`/`strong`/`exceptional`のいずれか、`policy.min_calibration_samples`は0以上（0で無効）であること。キー欠落・タイプミスは0/空として読み込まれ、`min_entry_quality`が未知値だと`poor`相当になりentry_qualityゲートが無効化されるため、補完せず項目名（例: `policy.long.min_entry_quality`）付きで全件まとめて報告し起動に失敗する。検証は`PITHA_POLICY_*`環境変数（FR-POLICY-4）の適用後に行い、環境変数経由の不正値（範囲外・未知のentry_quality）も同様に拒否する。`config/strategy.yaml`・埋め込み既定値は検証を通過する
- FR-POLICY-3: 以下のいずれかに該当する場合はNONE（取引しない）: JevがNONE、確信度不足、スプレッド過大、板が薄い（スナップショットの`turnover_5m`が`min_turnover_5m_jpy`未満。履歴不足で算出不能な場合は判定しない）、約定不能（スナップショットが特別気配（`special_quote`）またはストップ高/安（`price_limit`）。Fast Screener通過後に状態が変わった場合の再確認。LONG/SHORT双方）、貸借なしのショート（スナップショットの`lendable`が明示的にfalse、つまり銘柄情報`MarginSell`がfalseの銘柄のSHORT。`lendable`が不明（NULL）の場合は判定しない。`reject_reason`は`special_quote`/`price_limit: stop_up|stop_down`/`not_lendable`）、Risk Engine拒否、データ欠損、API異常、キャリブレーション対象外（Jev decisionのconfidenceが属する信頼度バケットのラベル付きCalibrationサンプル数が`policy.min_calibration_samples`未満。0で無効）
- FR-POLICY-4: しきい値はCalibration結果に基づき調整する。プロンプト変更より先にポリシー側のしきい値調整を優先する。しきい値の優先順位はFR-FS-3と同じ流儀で、`config/strategy.yaml`の`policy.long`/`policy.short` < 環境変数`PITHA_POLICY_LONG_*`/`PITHA_POLICY_SHORT_*`（起動時に読込み。サフィックスは`MIN_PROBABILITY`・`MIN_ENTRY_QUALITY`・`MIN_CONTINUATION_PROBABILITY`・`MAX_TOXIC_FLOW`・`MAX_LIQUIDITY_STRESSED`。例: `PITHA_POLICY_LONG_MIN_PROBABILITY`。`internal/config/strategy.go`、一覧は`environment/setup.md`）< DB `runtime_settings`の`policy.{long,short}.*`キー（`policy.long.min_probability`・`policy.short.max_toxic_flow`等。許可キーは`domain.PolicyProposalKeys`、`architecture/er.md` §runtime_settings）。DB値は自己改善の適用提案（FR-SELFIMPROVE-5）が書き込み、`policy.PolicySource`（`selfimprove.RuntimePolicy`）がシグナル判定のたびに読み込むため再起動なしで反映され、ロールバックで消えれば環境変数・yamlの値に戻る。バックテスト再生（Decide）は固有のしきい値を固定し、DB値の影響を受けない
- FR-POLICY-5: 生成したトレードシグナルを`trade_signals`に保存する（policy_version、risk_passed、reject_reasonを含む）。`policy_version`はPolicy Engineのロジック版`policy-v1`で、自己改善の適用提案（FR-SELFIMPROVE-5）のしきい値が有効な間は`policy-v1+sol-12`のように適用版を付加し（`varchar(20)`に収まる）、ロールバックで適用提案が無くなれば`policy-v1`に戻る。バックテスト再生（Decide）は常に`policy-v1`

### 4.7 Risk Engine

Risk EngineはJevより優先され、Jevから変更できない。Phase 7（実売買）移行後も人手承認を挟まず自動運用することを前提とし、その代わりPaper運用よりも厳格なLive用リミットと、後述のdead-man's switchで安全側に倒す。

| 項目 | Paper初期値 | Live初期値（Phase 7） |
|------|-----------|----------------------|
| max_position_per_symbol_pct | 2.0 | 1.0 |
| max_total_exposure_pct | 20.0 | 10.0 |
| max_daily_loss_pct | 1.0 | 0.5 |
| max_trade_loss_pct | 0.25 | 0.15 |
| max_open_positions | 5 | 3 |
| max_same_direction_positions | 3 | 2 |
| market_adverse_return_5m_pct | 0.2 | 0.15 |
| max_spread_bps | 30 | 20 |
| max_consecutive_losses | 4 | 3 |
| cooldown_after_loss_minutes | 5 | 10 |
| force_flat_before_market_close_minutes | 10 | 15 |
| heartbeat_timeout_minutes（Live専用） | 対象外 | 120 |

- FR-RISK-1: 上記制限のいずれかに抵触する場合、新規取引を拒否する
  - 「口座資産に対する%」の各上限（max_position_per_symbol_pct / max_total_exposure_pct / max_daily_loss_pct / max_trade_loss_pct）の分母は`config/risk.yaml`の`initial_capital`（想定資金・円。Paper初期値3,000万円、Liveは実運用資金を設定必須）とする。総エクスポージャ・銘柄エクスポージャは保有中ポジションの評価額（数量×現在値）、日次損失率は当日（JST）にクローズしたポジションの実現損益と保有中ポジションの含み損益の合計損失を分母で割った値
  - `initial_capital`が未設定（0以下）、またはRisk Engineが判定に必要な状態（ポジション・注文・最新スナップショット・スプレッド）を読み取れない場合は、判定をスキップせず`risk_engine_error`で拒否する（fail-closed。スプレッド欠損は「データ欠損」、FR-POLICY-3）
  - 同じ方向に重ねない制約（相関・市場逆行）: モメンタム・出来高急増・ブレイクアウトは同じ地合いで同じ方向の銘柄に寄るため、銘柄ごとの上限だけでは相場要因でまとめて負けるのを防げない。Risk Engineは新規エントリーの方向（LONG/SHORT）と同じ向きの保有ポジション数を読み、(1)`max_same_direction_positions`以上なら`max_same_direction_positions`理由で拒否する（反対方向の保有は数えない）、(2)同方向を1件以上保有中に、市場全体が保有方向と逆行している（`market_adverse_return_5m_pct`（%）以上、LONGなら下落・SHORTなら上昇）なら`market_adverse_to_direction`理由で拒否する。市場全体の動きは既存の`market_return_5m`（`kind=market_index`の追跡銘柄、TOPIX等。§4.1 FR-FE-4）を使い、先物専用の新規データ取得は要しない。`market_return_5m`が算出不能（指標未追跡・足の欠損）の場合、または同方向の保有が無い場合は市場逆行では拒否しない（相関はポジション間の重なりを抑える制約のため）。判定は最新スナップショット（`market_snapshots`）の値で行い、拒否理由はスプレッド等と同じく`trade_signals.reject_reason`に`risk_engine_rejected: <理由>`（例: `risk_engine_rejected: market_adverse_to_direction`。理由が空なら`risk_engine_rejected`のみ）として記録される
  - `max_trade_loss_pct`はポジションサイジングで強制する（§4.8 FR-ENTRY-3）。1単元（100株）でもStop Lossに掛かった時の損失が上限を超える場合は`max_trade_loss_pct`理由で拒否する
  - `config/risk.yaml`の各上限は起動時（`LoadRisk`/`LoadRiskBytes`）に検証し、範囲外の項目があれば項目名（例: `paper.max_daily_loss_pct`）を含むエラーで起動を失敗させる（違反は全件まとめて報告する）。`risk.yaml`/`strategy.yaml`は未知のキーをキー名付きエラーとして拒否する（strict decode）。キーの欠落は0として読み込まれ、`max_daily_loss_pct`=0・`max_consecutive_losses`=0は損失も連敗も無い状態でKill Switchを発動し続けるため（手動再開しても1分周期の判定で即再発動する）、「上限なし」の意味では扱わない。`max_position_per_symbol_pct`/`max_total_exposure_pct`/`max_daily_loss_pct`/`max_trade_loss_pct`/`max_spread_bps`/`market_adverse_return_5m_pct`は正（>0）、`max_open_positions`/`max_same_direction_positions`/`max_consecutive_losses`は1以上、`cooldown_after_loss_minutes`/`force_flat_before_market_close_minutes`は0以上、`heartbeat_timeout_minutes`はPaperは0以上・Liveは1以上（0・欠落ではFR-RISK-6のデッドマンスイッチが無警告で無効化されるため）。`initial_capital`はこの検証の対象外（上記の未設定扱い）。Paperは常に検証し、Liveは`live`セクションに1つでも値が定義されている場合のみ検証する（`live`セクションが無い場合はLive未運用として未検証）
- FR-RISK-2: 以下のいずれかでKill Switch（新規取引停止）を発動する: 日次損失上限到達、連敗上限到達、市場データ停止、Jev API連続失敗、Broker API異常、想定外ポジション発生、約定差異検知、DB書き込み失敗が一定回数継続、operator_heartbeat_timeout（Live専用、FR-RISK-6参照）、オペレーターによる手動Kill（`POST /api/v1/system/kill`。reason=`operator_manual`）
- FR-RISK-3: Kill Switch発動時、必要に応じて保有ポジションをクローズする（強制決済の対象reasonは`architecture/overview/flows.md` §10.3。強制決済は通知（Wails・Slack）より先に実行し、Slackの遅延・不達で決済を遅らせない。通知は決済が失敗しても試行し、通知失敗はログのみ。オペレーターの手動Killも UC-11 の「強制決済」として全ポジションをクローズする）
- FR-RISK-4: Kill SwitchはUI（Wailsアプリ）とサーバー内部処理の両方から操作可能とする。Phase 7の発注確定・Kill Switch操作に人手の追加認証は要求しない（完全自動運用）
- FR-RISK-5: Kill Switch発動・自動再開・解除を監査ログ（`kill_switch_events`・`kill_switch_resolutions`、追記専用）に記録する。通常のRisk拒否（`max_open_positions`・`max_total_exposure_pct`・`max_spread_bps`・`market_adverse_to_direction`・`max_trade_loss_pct`・`cooldown_after_loss`等、FR-RISK-1）は`Engine.Check`が`(false, reason)`を返すだけで`kill_switch_events`には書かず、理由を`trade_signals.reject_reason`（Policy Engineが保存）とslogに残す。手動Killは`operator_manual`として`kill_switch_events`に、手動Resumeによる解除は`resolved_by=manual`として`kill_switch_resolutions`に残る（Kill Switchを伴わない手動Pause/Resumeはslogの監査行`risk: audit: manual pause|resume`に記録する）
- FR-RISK-6（dead-man's switch、Live専用）: Wailsアプリの認証済みUIリクエストを「操作者ハートビート」として記録する。立会時間中に`heartbeat_timeout_minutes`（初期値120分）を超えてハートビートが途絶した場合（最後のハートビートがその営業日の寄り付き前なら、寄り付き（9:00 JST）からの経過時間で判定する。立会時間外（昼休みを含む）は判定しない）、Risk Engineは自動的に新規エントリーを停止する（保有ポジションのExitルールは継続）。オペレーターがUIを再度操作した時点でこの停止理由は自動解消する（解消判定は「発動時刻より後の本物のハートビートが記録され、かつそれが`heartbeat_timeout_minutes`以内」であること。発動判定用の寄り付きクランプは解消判定には使わず、寄り付き前の時間帯でも操作者不在のまま自動解消しない）。画面が自動で発火する再同期・ポーリング・WebSocket再接続はオペレーターの操作ではないためハートビートに数えない（`architecture/overview/flows.md` §10.4）
- FR-RISK-7（Kill Switch再開の自動/手動分類）: `kill_switch_events.reason`により再開方法を分ける

| 発動理由 | 再開方法 |
|---------|---------|
| market_data_down（データ復旧確認後） | 自動再開 |
| jev_api_down（API復旧確認後） | 自動再開 |
| operator_heartbeat_timeout（ハートビート再検知） | 自動再開 |
| daily_loss_limit（日次損失上限到達） | 手動再開のみ |
| unexpected_position（想定外ポジション） | 手動再開のみ |
| fill_discrepancy（約定差異） | 手動再開のみ |
| consecutive_losses（連敗上限到達） | 手動再開のみ |
| db_write_failure（DB書き込み失敗継続） | 手動再開のみ |
| broker_api_error（Broker API異常） | 手動再開のみ |
| operator_manual（オペレーターの手動Kill） | 手動再開のみ |

`cooldown_after_loss`（連敗後クールダウン）は`kill_switch_events`に記録しない時間ベースの新規取引ゲート（FR-RISK-1）であり、本表の対象外（下記判定基準を参照）。

- 再開ベースライン（FR-RISK-2/FR-RISK-7）: `consecutive_losses`・`daily_loss_limit`は手動再開のみだが、発動の原因となったクローズ済みポジションは履歴に残り続ける。再開直後に同じ履歴で再発動して復帰不能になるのを防ぐため、手動Resumeがこれらのイベントを解除した時刻を`runtime_settings`にベースラインとして記録する（`system.loss_streak_baseline_at`・`system.daily_loss_baseline_at`）。`fill_discrepancy`のResumeも同様に`system.fill_discrepancy_baseline_at`を記録し、逆方向の孤児約定照合はResume時刻より前にFILLEDとなった注文を対象外とする（直近15分の照合窓内の同じ孤児注文で再発動して再度全ポジションを強制決済するのを防ぐ。ポジション起点の突合は対象外）。以後、連敗数・クールダウン起点（`cooldown_after_loss`）はベースライン以降にクローズしたポジションのみ、日次損失率の実現損益もベースライン以降にクローズした分のみを対象とする（保有中ポジションの含み損は常に算入するため、実際に損失が拡大すれば再発動する）。2つのベースラインは独立で、連敗Resumeは当日の実現損失を帳消しにせず、日次損失Resumeは連敗数をリセットしない。手動Kill・Pauseなど他理由のResumeはどちらも動かさない。ベースラインより後に上限回数の敗北（連敗）または上限を超える損失（日次）が再び発生した場合のみ再発動する。連敗数は営業日をまたいで持ち越す（日次リセットしない）が、Resumeでリセットされる。

FR-RISK-2/FR-RISK-7の検知・自動再開は、Schedulerが1分周期で実行する（`internal/service/scheduler`の`WithRiskMonitor`/`WithAutoResumer`）。判定基準は以下。

- market_data_down: kabuステーションAPIの板取得（`GetBoard`）が5回連続で失敗（成功1回で復旧、自動再開）。数えるのはフィード側の失敗（トークン未保持・通信エラー・HTTP 5xx・認証/トークン系の4xx＝401/403と`4001007`/`4001008`/`4001009`/`4001013`/`4001017`）のみ。銘柄単位の4xx（`4002001`銘柄が見つからない、廃止・停止銘柄など）と429/`4001006`は数えず、当該銘柄をstaleにするだけ（無効銘柄が並んでも全体停止にしない。issue #532）
- jev_api_down: Jev APIの直近呼び出しエラー率がしきい値（既定50%、直近20件、最小5件）以上（しきい値未満に戻るか、5分間呼び出しが無ければ復旧、自動再開。復旧後の最初の呼び出しは古い失敗窓を破棄して新しい窓で評価し、窓が最小5件に達しエラー率がしきい値以上になるまで再発動しない）。Slack通知（§5.2）と同じ信号を使う
- broker_api_error: kabuステーションAPIがHTTP 5xxを5回連続で返す（手動再開のみ）。4xx・通信エラーは対象外（通信エラーは市場データ停止側で扱う）
- db_write_failure: SQLiteの書き込みがストレージ起因（BUSY/LOCKED/READONLY/IOERR/FULL/CANTOPEN/CORRUPT/NOTADB。NOTADBはDBファイルがSQLite形式でない状態で、破損の一種として扱う）で5回連続失敗（手動再開のみ）。制約違反は対象外。書き込みはExec系に加え、トランザクションの`BEGIN`（`_txlock=immediate`の書き込みロック取得失敗）と、読み取り専用でないトランザクションの`COMMIT`の成否も計上する。`RETURNING`付きクエリ（孤児ジョブ回復`FailOrphanedRunning`のみ）は計上対象外
- unexpected_position / fill_discrepancy: Paper Tradingでは外部Brokerが無いため、保有中ポジションを起点となる`paper_orders`の約定記録と突合する。起点注文が存在しない・未約定・銘柄/売買方向が不一致なら`unexpected_position`、約定数量・約定価格がポジションと不一致、または指値を超えた約定なら`fill_discrepancy`（手動再開のみ）。逆方向の照合として、直近15分内（約定直後の1分は猶予）にFILLEDとなった注文がどのポジションのEntry/Exit注文にもなっていない場合も`fill_discrepancy`とする
- cooldown_after_loss: クールダウンはRisk Engineの時間ベースの新規取引ゲート（FR-RISK-1、`kill_switch_events`には記録しない）であり、経過で自動的に解除される
- daily_loss_limit / consecutive_losses: 新規シグナルに対するRisk Engineの判定（`Check`）に加え、この1分周期の検知でも同じ判定（再開ベースライン考慮）を行う。シグナルが出なくても、日次損失（含み損込み）が上限に達した、または連敗上限に達した時点でKill Switchの発動・通知・保有ポジション強制決済（FR-RISK-3）を行う（手動再開のみ）
- 強制決済の再試行: 強制決済対象reason（`architecture/overview/flows.md` §10.3）のKill Switchが未解除のまま保有ポジションが残っている場合、同じ1分周期で全ポジションの強制決済を再実行する（発動時の一度きりの決済が一時的に失敗しても、Killed状態のまま建玉が残り続けないようにする。決済は冪等）
- 日次損失上限の80%到達時のSlack通知（`non-functional.md` §5.2）も同じ1分周期の検知で行う

### 4.8 Entry / Exit

- FR-ENTRY-1: 成行想定Paper Entry・指値Paper Entryの両方を選択可能とする。現状はExecutionエンジン（`internal/service/execution`）が両方を実装しており（`EntryRequest.OrderType`/`LimitPrice`、`Config.PreferLimit`、PENDING指値の約定処理`TryFillPending`）、本番経路（Policy → `paperexec` → Execution）は`OrderType`/`LimitPrice`を指定しないため常に成行想定で発注する。`Config.PreferLimit`は`config/*.yaml`・環境変数・`runtime_settings`のいずれにも設定キーが無く既定のfalse（成行）固定であり、運用で指値Entryを選ぶ手段は未提供（本番経路への配線は実売買移行時にFR-ENTRY-2と併せて行う）
- FR-ENTRY-2: 実売買へ移行する場合は原則として指値を優先する（Paper Tradingの現行運用は上記のとおり成行想定）
- FR-ENTRY-3（ポジションサイジング）: Paper Entryの発注数量は、次の3つの上限株数の最小値を単元（100株）単位に切り下げた値とする。1単元にも満たない場合は発注せず、拘束した制限（`max_trade_loss_pct` / `max_position_per_symbol_pct` / `max_total_exposure_pct`）を理由に見送る
  - 1トレード最大損失: `initial_capital × max_trade_loss_pct ÷ (価格 × stop_loss_pct)`
  - 銘柄上限: `initial_capital × max_position_per_symbol_pct ÷ 価格`
  - 総エクスポージャ残枠: `initial_capital × (max_total_exposure_pct − 現在の総エクスポージャ率) ÷ 価格`
  - `GET /api/v1/symbols/{symbol}`の`risk.allowed_position_pct`は、直近価格で同サイジングを行った結果の数量が占める`initial_capital`比（%、発注不可なら0）
- FR-ENTRY-4（約定の原子性）: Entry注文の約定（`paper_orders`のFILLED化）とポジション作成（`positions`）は単一のDBトランザクションで行う。失敗時は注文をFILLEDにせず、約定済み注文がどのポジションにも紐付かない状態（孤児約定）はRisk Engineの照合が`fill_discrepancy`として検知する（§4.7）
- FR-ENTRY-5（銘柄単位のEntryゲート）: Executionエンジンの`Enter`は、次のいずれかに該当する銘柄への新規Entryを受け付けない（`ErrOutsideTradingSession` / `ErrPositionAlreadyOpen` / `ErrPendingOrderExists` / `ErrSymbolInCooldown`）。Risk Engineの判定（FR-RISK-1）とは別の、Execution自身の不変条件である
  - 立会時間外、または引け前強制決済の開始時刻以降（Calendar設定時。直ちに強制決済されるEntryを避ける）
  - 保有中のポジションがある（`positions`の部分UNIQUEインデックス、`architecture/er/tables-trading.md`。同一銘柄の同時ポジションは1つ）
  - 未約定（PENDING）のEntry注文がある（先の注文でポジションができると2本目は約定し得ないため、重複させない）
    - 成行注文は発注直後に約定するため、1分以上PENDINGのまま残った成行注文（INSERTと約定の間のクラッシュ等による孤児）はゲートせず、`Enter`/`OnSnapshot`がREJECTEDへ遷移させる（指値注文の重複拒否は従来どおり）。約定失敗時のREJECTEDはctxキャンセルと切り離して実行する
  - その銘柄で損失クローズ（実現損益<0）した時刻から`cooldown_after_loss_minutes`（FR-RISK-1）の間。Executionのメモリ上の銘柄別ゲートであり、Risk Engineの判定とは別に働く（プロセス再起動で解除される）
- FR-ENTRY-6（見送りの扱い）: 上記ゲートによる拒否は、Policy → `paperexec`経路ではエラーではなく「見送り」として扱う。再試行しても同じ理由で拒否されるため、jev-traderジョブは失敗にせず、`paper entry skipped`としてログに残して正常終了する。ゲート以外のEntryエラーはジョブ失敗とする
- FR-ENTRY-7（入力検証）: `Enter`は発注前に、シグナル方向がLONG/SHORTであること、Risk Engine通過済み（`risk_passed`）であること、数量>0、価格が有限かつ>0、指値価格を指定する場合は有限かつ>0、指値注文では指値価格が指定されていることを検証し、違反は注文を作らずに拒否する。板価格の欠損（0）が約定・時価更新・決済価格にならないよう、`TryFillPending`・`OnSnapshot`・`Close`の価格も同様に検証する
- FR-ENTRY-8（約定モデル: 呼値・スプレッド・手数料・滑り・立会）: Paper Entry/Exitはシグナル価格（直近価格）での全量約定にせず、`internal/service/fillmodel`の約定モデルで価格・手数料を決める。Paper Trading（`execution`）とバックテスト（FR-BT-4）は同じモデル（`fillmodel.Default`）を使う
  - 呼値単位: 約定価格は東証の呼値の単位（標準テーブル。3,000円以下1円、5,000円以下5円、30,000円以下10円、50,000円以下50円、300,000円以下100円…。TOPIX100構成銘柄・ETF等の特例テーブルは扱わない）に載せ、注文に不利な側へ丸める（買いは切り上げ、売りは切り下げ）
  - スプレッド: ザラ場の成行は板を跨ぐ（買い＝最良売気配`ask`、売り＝最良買気配`bid`）。板が無い（`bid`/`ask`がNULLまたは逆転）場合は`spread_bps`の半分を直近価格の不利側へ乗せ、`spread_bps`も無ければ直近価格とする
  - 滑り: 板の気配に加えて不利側へ`SlippageBps`（初期値2bps）を乗せる。約定価格との差を`paper_orders.slippage_bps`（直近価格に対する不利方向のbps。呼値丸め・スプレッド込み）に記録する
  - 手数料: 約定代金の`FeeBps`（初期値0bps。kabuステーションAPIを提供する証券会社の国内現物手数料が無料のため）を`paper_orders.fees`（円）に記録し、`realized_pnl`はエントリー・Exit両約定の手数料を差し引いた値とする
  - 昼休み・立会時間外: 前場11:30〜後場12:30の昼休み、大引け後、非営業日は約定しない（`marketcalendar.PhaseClosed`）。Entryは`ErrOutsideTradingSession`、手動`Close`（`POST /positions/:id/close`は409）・Kill Switchの`CloseAll`も同様に約定せずポジションを保持する（`CloseAll`は`ErrOutsideTradingSession`を含むエラーを返し、次の立会で再度決済する）。`OnSnapshot`はExit条件が成立しても昼休み中はクローズせず、次の立会の足で再評価する。PENDING指値も昼休み中は約定せず次の立会まで待つ
  - 寄り・引けの気配: 前場・後場の寄り（9:00・12:30の最初の1分足）と大引けのクロージング・オークション（15:25〜15:30）は板寄せの単一価格約定として、ザラ場の1分足とは別に扱う。スプレッドは跨がず、直近価格（気配値）に`AuctionSlippageBps`（初期値5bps）の不利な滑りと呼値丸めを適用する（`marketcalendar.Calendar.PhaseAt`）
  - 指値: 板の気配が指値を満たす（買い＝`ask`≦指値、売り＝`bid`≧指値）ときに約定する。約定価格は気配＋滑り（呼値丸め後）だが、指値より不利にはならない
- FR-EXIT-1: 以下のExit条件を併用する: 固定Stop Loss、固定Take Profit、Trailing Stop、Jev方向反転、continuation_probability低下、VWAP逆クロス、最大保有時間到達、引け前強制決済（`force_flat_before_market_close_minutes` 分前から、大引け15:30 JSTを基準に判定する。前場終了11:30は対象外）。VWAP逆クロスは「価格がVWAPの不利側へ抜けた瞬間」（前回評価時は不利側でなく、今回評価で不利側）のみ成立し、不利側に滞在しているだけでは成立しない（不利側でエントリーしたポジションは、有利側へ抜けた後に再び不利側へ抜けるまでこの条件でクローズしない）。前回評価が無い最初の評価は、建値と現在VWAPの関係を前回の関係とみなす
- FR-EXIT-2: 初期値: `stop_loss_pct=0.6`, `take_profit_pct=1.2`, `trailing_stop_pct=0.5`, `max_holding_minutes=20`
- FR-EXIT-3: Jev API不応答時も、既存ポジションはコードベースのExit Ruleで管理を継続する（Jev不応答を理由にリスク管理を停止しない）。同様に、PENDING指値の約定処理（`TryFillPending`）の失敗は、`OnSnapshot`による保有ポジションの時価更新・Exit評価を止めない（失敗はログに残し、当該注文のみ飛ばす。ポジションが既にあるため約定し得ないPENDING注文は`REJECTED`にする）

### 4.9 状態管理

各銘柄について以下を保持する。

```json
{
  "symbol": "XXXX",
  "last_price": 0,
  "last_scan_at": null,
  "last_jev_scout_at": null,
  "last_jev_trader_at": null,
  "last_signal": "NONE",
  "last_signal_confidence": 0,
  "position": null,
  "cooldown_until": null
}
```
