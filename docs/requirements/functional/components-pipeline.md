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
- FR-FE-2: 板・約定特徴量はkabuステーションAPIから取得できない銘柄・時間帯では欠損値として扱い、依存するJev入力/スコアから除外する
- FR-FE-3: `volume`/`turnover`はkabuステーションAPIの当日累積値である。`volume_1m/5m`・`turnover_1m/5m`は「現在の累積値 − 窓の開始時点の累積値」で算出し、足の合算はしない。Fast Screenerの`min_turnover_5m_jpy`とPolicy Engineの「板が薄い」判定は同じ`turnover_5m`（`featureengine.TurnoverOverWindow`）を用いる
- FR-FE-4: 市場コンテキストは`instruments.kind`で区別した追跡銘柄から算出する。`market_index`（TOPIX/Nikkei225等）のreturn平均を`market_return_1m/5m`、銘柄の`sector`と一致する`sector_index`のreturnを`sector_return_5m`とし、`stock_vs_sector_relative_strength = return_5m − sector_return_5m`、`market_breadth`は直近3分以内の全アクティブ株式の最新return_5mの（上昇数−下落数）/銘柄数とする。追跡銘柄が未登録・更新が3分以上停止・履歴不足の場合は欠損値とする（FR-FE-2と同じ扱い）
- FR-FE-5: 窓（`return_*`・`vwap_slope`・`realized_vol_*`・`volume_*`・`turnover_*`・`vwap_cross_direction`等）の基準バーは、窓開始時刻（判定時刻−窓幅）から「窓幅の50%（最小90秒）」以内に存在しなければならない。それより古い基準バー（前営業日の引け・昼休み前の前場最終バー・再起動/欠測をまたぐバー）しか無い場合は履歴不足として欠損値（nil）とし、ギャップを「N分リターン」として扱わない（`featureengine.windowRef`）

### 4.2 Fast Screener

- FR-FS-1: Jev API呼び出し前に、環境変数/DB設定値（`min_price`, `max_price`, `min_turnover_5m_jpy`, `max_spread_bps`, `min_volume_ratio`, `min_abs_return_5m_pct`, `min_realized_volatility`）で明らかに対象外の銘柄を除外する（全フィルターを評価して除外理由を保持し、Scanner Dashboardのスキャン状況パネル（FR-SCAN-3〜6、除外・欠損理由の表示は FR-SCAN-4/5）で銘柄別に参照できる。値が欠損の銘柄は閾値未達ではなく欠損理由として区別する。`min_abs_return_5m_pct`の単位は**パーセント**（0.3 = 0.3%）で、Feature Engineの`return_5m`（小数比、0.003 = 0.3%）を×100して比較する）
- FR-FS-2: 通過銘柄に対し以下のスコアを算出し、上位N銘柄のみJev Scoutへ送る。Nは`fast_screener.top_n`で、同梱既定は20（`config/strategy.yaml`。Jev APIコストを抑える側の値）。50〜200へ引き上げる場合は`non-functional.md` §2.1のAPI呼び出し上限（Nに比例）を確認する

```text
screen_score =
  w1 * normalized_volume_ratio
+ w2 * abs(return_5m)   # return_5mは小数比（%換算しない）
+ w3 * breakout_strength
+ w4 * orderbook_imbalance
+ w5 * volatility_expansion
```

  - `breakout_strength`: 直前5分間（判定時点の足を除く）の高値/安値に対する現在価格のブレイク幅（高値上抜け時は`price/high - 1`、安値下抜け時は`1 - price/low`、レンジ内は`0`）。Feature Engine（`featureengine.ComputeScreenSignals`）が算出する
  - `volatility_expansion`: `volatility_expansion_ratio`（`realized_vol_5m / realized_vol_15m`）。同上
  - 履歴不足で算出できない項は0ではなく欠損として合計から除外する（FR-FE-2と同じ扱い）

- FR-FS-3: フィルター設定値・スコア重みはすべて環境変数またはDBで変更可能とする。優先順位は`config/strategy.yaml` < 環境変数`PITHA_FAST_SCREENER_*`（起動時に読込み。例: `PITHA_FAST_SCREENER_MIN_PRICE`, `PITHA_FAST_SCREENER_TOP_N`, `PITHA_FAST_SCREENER_WEIGHT_BREAKOUT_STRENGTH`）< DB `runtime_settings`の`screener.*`キー（候補更新周期ごとに読み込むため再起動不要。キー一覧は`architecture/er.md` §runtime_settings）

### 4.3 スキャン頻度・イベント駆動

| 対象 | 周期 |
|------|------|
| 全体スキャン | 60秒ごと |
| 候補銘柄更新 | 15〜30秒ごと |
| ポジション保有銘柄 | 5〜15秒ごと |

- 周期は`config/strategy.yaml`の`scan.full_scan_interval_seconds`（60）/ `scan.candidate_refresh_interval_seconds_min`・`_max`（15・30）/ `scan.held_position_interval_seconds_min`・`_max`（5・15）で設定する（括弧内は同梱の既定値、単位は秒）。各値は正の整数で、`_max >= _min`であること。設定ローダー（`internal/config/scan_defaults.go`）は、未設定または0以下のキーを同梱既定値で補完し、`_max < _min`の`_max`を`_min`に引き上げる。いずれも警告ログを出す。0のままだと候補更新が待機なしで回り、全体スキャンが`@every 0s`で登録されるため（ホットループ防止）

- FR-SCAN-1: 以下のいずれかを満たした銘柄は通常周期を待たず再評価する: 1分リターン急変、出来高急増、スプレッド急拡大、板インバランス急変、VWAPクロス、高値/安値ブレイク、約定フロー急変（直近2バーの`trade_flow_imbalance`の差の絶対値が`config/strategy.yaml`の`scan.event_trigger.trade_flow_imbalance_change_threshold`以上。どちらかが欠損の場合は無信号）、ニュースフラグ発生
- FR-SCAN-2（再評価抑制）: `abs(return_1m) < threshold AND abs(volume_ratio_5m) < threshold AND abs(spread_change) < threshold AND no_event` の場合はJev呼び出しをスキップし、APIコストとレイテンシを削減する
  - 各thresholdは`config/strategy.yaml`の`scan.event_trigger.*`（`return_1m_change_threshold` / `volume_ratio_change_threshold` / `spread_change_bps_threshold` / `orderbook_imbalance_change_threshold` / `trade_flow_imbalance_change_threshold`）で設定する。判定は`abs(値) >= threshold`で、`return_1m_change_threshold`は1分リターン(`return_1m`)の絶対値、`volume_ratio_change_threshold`は5分出来高比率(`volume_ratio_5m`)の絶対値（前バーとの差ではなく現在値の水準判定。`volume_ratio_5m`は通常1.0前後の比率のため既定2.0は「5分出来高が平均の2倍以上」を意味する）、`spread_change_bps_threshold` / `orderbook_imbalance_change_threshold` / `trade_flow_imbalance_change_threshold`は直近2バーの差の絶対値と比較する。したがって、thresholdが0以下だと全バーでFR-SCAN-1が発火しFR-SCAN-2が無効化される。キー欠落（新キー追加前の古い`strategy.yaml`等）や0以下の値は設定ローダー（`LoadStrategy` / `LoadStrategyBytes`）が同梱既定値（`config/strategy.yaml`の値）で補完し、警告ログを出す

### 4.4 Jev Scout

- FR-SCOUT-1: 1回のJev呼び出し（`POST /v1/systemone`、`architecture/overview.md` §6）で以下の質問群を評価する: `interesting_now`（`noul`型: yesの確率0〜1）, `momentum_quality`（`choice`型: weak/moderate/strong/exceptional）, `liquidity_ok`（`noul`型）, `abnormal_activity`（`noul`型）。入力は`market`（現在の市場状態）と`similar_past_cases`（RAGの類似過去事例）。応答が必須answerの欠落・型不一致・定義外のchoice・範囲外のnoulを含む場合は不正応答として失敗扱いにする
- FR-SCOUT-2: 通過条件は `interesting_now >= 0.65 AND liquidity_ok >= 0.70 AND abnormal_activity >= 0.55`（初期値。バックテスト後に調整）
- FR-SCOUT-3: 入力・出力・状態ハッシュ・レイテンシ・モデルID（応答の`model`）を`jev_decisions`（decision_type=scout）に保存する。Jev APIは課金額を返さないため`request_cost`はNULLのままとする。質問セットのバージョン（現行`scout-v2`）を`question_version`に記録する

### 4.5 Jev Trader

- FR-TRADER-1: Scout通過銘柄に対し以下を評価する（FR-SCOUT-1と同じく`POST /v1/systemone`、入力は`market`と`similar_past_cases`）: `direction`（`choice`型: LONG/SHORT/NONE）, `regime`（`choice`型: TREND/RANGE/BREAKOUT/CHAOTIC）, `entry_quality`（`choice`型: poor/fair/good/strong/exceptional）, `toxic_flow`（`noul`型）, `liquidity_stressed`（`noul`型）, `continuation_probability`（`noul`型）。`confidence`は`direction`回答の`confidence`を用いる。質問セットのバージョンは現行`trader-v2`
- FR-TRADER-2: Jevのconfidence/probability（`noul`の値、`choice`の`confidence`）を実際の株価上昇確率とみなさない。実結果との対応はCalibrationで独自に検証する
- FR-TRADER-3: 入出力を`jev_decisions`（decision_type=trader）に保存する

### 4.6 Policy Engine

- FR-POLICY-1: LONG条件: `direction == LONG AND P(LONG) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-2: SHORT条件: `direction == SHORT AND P(SHORT) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-3: 以下のいずれかに該当する場合はNONE（取引しない）: JevがNONE、確信度不足、スプレッド過大、板が薄い（スナップショットの`turnover_5m`が`min_turnover_5m_jpy`未満。履歴不足で算出不能な場合は判定しない）、Risk Engine拒否、データ欠損、API異常、キャリブレーション対象外（Jev decisionのconfidenceが属する信頼度バケットのラベル付きCalibrationサンプル数が`policy.min_calibration_samples`未満。0で無効）
- FR-POLICY-4: しきい値はCalibration結果に基づき調整する。プロンプト変更より先にポリシー側のしきい値調整を優先する
- FR-POLICY-5: 生成したトレードシグナルを`trade_signals`に保存する（policy_version、risk_passed、reject_reasonを含む）

### 4.7 Risk Engine

Risk EngineはJevより優先され、Jevから変更できない。Phase 7（実売買）移行後も人手承認を挟まず自動運用することを前提とし、その代わりPaper運用よりも厳格なLive用リミットと、後述のdead-man's switchで安全側に倒す。

| 項目 | Paper初期値 | Live初期値（Phase 7） |
|------|-----------|----------------------|
| max_position_per_symbol_pct | 2.0 | 1.0 |
| max_total_exposure_pct | 20.0 | 10.0 |
| max_daily_loss_pct | 1.0 | 0.5 |
| max_trade_loss_pct | 0.25 | 0.15 |
| max_open_positions | 5 | 3 |
| max_spread_bps | 30 | 20 |
| max_consecutive_losses | 4 | 3 |
| cooldown_after_loss_minutes | 5 | 10 |
| force_flat_before_market_close_minutes | 10 | 15 |
| heartbeat_timeout_minutes（Live専用） | 対象外 | 120 |

- FR-RISK-1: 上記制限のいずれかに抵触する場合、新規取引を拒否する
  - 「口座資産に対する%」の各上限（max_position_per_symbol_pct / max_total_exposure_pct / max_daily_loss_pct / max_trade_loss_pct）の分母は`config/risk.yaml`の`initial_capital`（想定資金・円。Paper初期値3,000万円、Liveは実運用資金を設定必須）とする。総エクスポージャ・銘柄エクスポージャは保有中ポジションの評価額（数量×現在値）、日次損失率は当日（JST）にクローズしたポジションの実現損益と保有中ポジションの含み損益の合計損失を分母で割った値
  - `initial_capital`が未設定（0以下）、またはRisk Engineが判定に必要な状態（ポジション・注文・最新スナップショット・スプレッド）を読み取れない場合は、判定をスキップせず`risk_engine_error`で拒否する（fail-closed。スプレッド欠損は「データ欠損」、FR-POLICY-3）
  - `max_trade_loss_pct`はポジションサイジングで強制する（§4.8 FR-ENTRY-3）。1単元（100株）でもStop Lossに掛かった時の損失が上限を超える場合は`max_trade_loss_pct`理由で拒否する
- FR-RISK-2: 以下のいずれかでKill Switch（新規取引停止）を発動する: 日次損失上限到達、連敗上限到達、市場データ停止、Jev API連続失敗、Broker API異常、想定外ポジション発生、約定差異検知、DB書き込み失敗が一定回数継続、operator_heartbeat_timeout（Live専用、FR-RISK-6参照）、オペレーターによる手動Kill（`POST /api/v1/system/kill`。reason=`operator_manual`）
- FR-RISK-3: Kill Switch発動時、必要に応じて保有ポジションをクローズする（強制決済の対象reasonは`architecture/overview/flows.md` §10.3。オペレーターの手動Killも UC-11 の「強制決済」として全ポジションをクローズする）
- FR-RISK-4: Kill SwitchはUI（Wailsアプリ）とサーバー内部処理の両方から操作可能とする。Phase 7の発注確定・Kill Switch操作に人手の追加認証は要求しない（完全自動運用）
- FR-RISK-5: すべてのRisk拒否・Kill Switch発動・自動再開を監査ログ（`kill_switch_events`・`kill_switch_resolutions`、追記専用）に記録する。手動Killは`operator_manual`として`kill_switch_events`に、手動Resumeによる解除は`resolved_by=manual`として`kill_switch_resolutions`に残る（Kill Switchを伴わない手動Pause/Resumeはslogの監査行`risk: audit: manual pause|resume`に記録する）
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

- market_data_down: kabuステーションAPIの板取得（`GetBoard`）が5回連続で失敗（成功1回で復旧、自動再開）
- jev_api_down: Jev APIの直近呼び出しエラー率がしきい値（既定50%、直近20件、最小5件）以上（しきい値未満に戻るか、5分間呼び出しが無ければ復旧、自動再開）。Slack通知（§5.2）と同じ信号を使う
- broker_api_error: kabuステーションAPIがHTTP 5xxを5回連続で返す（手動再開のみ）。4xx・通信エラーは対象外（通信エラーは市場データ停止側で扱う）
- db_write_failure: SQLiteの書き込みがストレージ起因（BUSY/LOCKED/READONLY/IOERR/FULL/CANTOPEN/CORRUPT/NOTADB。NOTADBはDBファイルがSQLite形式でない状態で、破損の一種として扱う）で5回連続失敗（手動再開のみ）。制約違反は対象外
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
  - その銘柄で損失クローズ（実現損益<0）した時刻から`cooldown_after_loss_minutes`（FR-RISK-1）の間。Executionのメモリ上の銘柄別ゲートであり、Risk Engineの判定とは別に働く（プロセス再起動で解除される）
- FR-ENTRY-6（見送りの扱い）: 上記ゲートによる拒否は、Policy → `paperexec`経路ではエラーではなく「見送り」として扱う。再試行しても同じ理由で拒否されるため、jev-traderジョブは失敗にせず、`paper entry skipped`としてログに残して正常終了する。ゲート以外のEntryエラーはジョブ失敗とする
- FR-ENTRY-7（入力検証）: `Enter`は発注前に、シグナル方向がLONG/SHORTであること、Risk Engine通過済み（`risk_passed`）であること、数量>0、価格が有限かつ>0、指値価格を指定する場合は有限かつ>0、指値注文では指値価格が指定されていることを検証し、違反は注文を作らずに拒否する。板価格の欠損（0）が約定・時価更新・決済価格にならないよう、`TryFillPending`・`OnSnapshot`・`Close`の価格も同様に検証する
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
