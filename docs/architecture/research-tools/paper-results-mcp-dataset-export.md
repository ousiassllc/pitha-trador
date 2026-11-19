# 設計: 学習用データセットの書き出し（training_dataset_export）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1094。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-dataset-export.md` §3.45、データの仕様（列・ラベルの版・分割・manifest・ハッシュ）は `docs/architecture/research-tools/training-dataset.md`。

## 1. 目的と位置づけ

Jev の判断 1 件 = 1 行の学習用データセットを、版管理された秘密を含まない形でローカルのフォルダに書き出し、MCP の応答には manifest だけを返す。外部の分析環境（手元の notebook など）が、同じ入力で同じファイルを何度でも作り直せるようにするためのもの。**研究専用で、取引には使わない**。

`internal/service/dataexport`（#773 の ZIP 書き出し。GUI からの設定・取引の書き出し）とは目的が違い、触らない。

## 2. 取引に影響しない・秘密が出ないことの担保

- **既定はオフ**: Settings の `research.export.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0。`internal/config/researchjobsettings` の `KeyExportEnabled`）がオンのときだけ書く。未設定・0・壊れた値は**オフ**で、応答は `{"enabled": false, "written": false, "reason": "export_disabled", ...}`、フォルダもファイルも作らない（`TestOffByDefaultWritesNothingAndOnWrites` が、未設定・0・壊れた値・他の研究スイッチだけオンのそれぞれでフォルダ一覧が変わらないことを固定する）。
- **設定の読み方を絞る**: スイッチは `datasetexport.switchReader` が、`research.export.enabled` 以外のキーを SQL を流す前に拒否して読む。`runtime_settings` には秘密のパスなどが入りうるので、他のキーの値はどこにも出ない。
- **DB は書き換えない**: ツールは MCP の読み取り専用トランザクション（`mode=ro`・`query_only`）の `query.Q` だけを使う。書き込む表はない（新しい列・トリガ・表もない）。書くのはファイルだけ（`TestWritesNoTableAndReadsNoBody` が、実行した SQL がすべて読み取りで、全表のハッシュが変わらず、MCP の接続への書き込みが失敗することを固定する）。
- **読む列を固定する**: `SELECT *` はなく、列は `internal/service/datasetexport` の許可リスト（`Columns`）から生成した名前付きの列だけ。生の板・気配（`bid` / `ask` / `bid_depth` / `ask_depth` / `microprice` / `raw_data_json`）、価格・出来高の水準、`state_json`・`payload_json`・エラー抜粋・`reject_reason` の自由文は SQL に出てこない。`response_json` は固定の 5 キーを `json_valid` / `json_type` / `json_extract` で取り出すだけ（本文は読まない）。取り出した値は型と列挙の検査を SQL で通し、外れた値は空欄にする。テストが、各ファイルと応答に、`response_json` / `state_json` / `raw_data_json` / `runtime_settings` の鍵 / 自由文の理由 / エラー抜粋 / 板・価格の生の値に植えた目印の文字列が現れないことを固定する。
- **書き先を引数で変えられない**: 引数は `from` / `to` / `split_at` / `embargo_days` だけ（`additionalProperties: false`、未知のキーは `invalid_argument`）。フォルダは `SELECT file FROM pragma_database_list WHERE name = 'main'` で DB ファイルの場所を読み、`<DB ファイルのフォルダ>/exports/<dataset_id>/` に決める。`dataset_id` はファイルの内容のハッシュと期間・分け方（`from`・`to`・`split_at`・`embargo_days`）から決まる。
- **参照を固定する**: 新しい表は作らない。`internal/service/datasetexport` と `exportdiag` が読む表・書くファイルは §3 のとおりで、`internal/service/datasetexport/guards/isolation_test.go` が、データセットのパッケージ名・シンボルを取引系（Policy・Risk・発注・実行・Paper ガード・Live・Jev プロンプト・RAG・Calibration・学習・自己改善・ブローカー）が参照しないこと、他のサービスが `datasetexport` を import しないことを固定する。

## 3. 構成

| 部品 | 役割 |
|---|---|
| `internal/service/paperresultsmcp/exportdiag` | ツールの薄い皮: `Description` / `SchemaProperties` / `Request`（厳密な `UnmarshalJSON` + `Validate`）/ `Run`。`research_tools.go` が `queryTool` で登録し、`writes: true`（読み取り専用ではない注釈）にする |
| `internal/service/datasetexport` | 本体。スイッチ・DB の場所・読み取り・分割・CSV・manifest・書き出し |
| `datasetexport/columns.go` / `fields.go` | 列の許可リスト（名前・元・単位・意味）と、SQL を作る固定のフィールド |
| `datasetexport/read_decisions.go` | 判断・足・銘柄を 1 回で読む（足と銘柄は一意キーの LEFT JOIN） |
| `datasetexport/read_labels.go` / `read_answers.go` | ラベル 3 種、取引候補、シャドー・お試しを、期間に 1 回ずつ**まとめて**読む（判断ごとには読まない） |
| `datasetexport/guards`（テストだけ） | 索引の使用（`EXPLAIN QUERY PLAN`）と取引側からの隔離の固定 |
| `datasetexport/split.go` | 分割・embargo・purge |
| `datasetexport/csv.go` / `parquet.go` / `manifest.go` / `store.go` | CSV と Parquet への変換、ハッシュ（期間・分け方を含む）、manifest、フォルダとファイルの書き出し |
| `datasetexport/parquetenc` | Parquet の書き込み（`parquet-go` を import する唯一の場所。書き込みの設定を固定し、同じ行から同じバイト列を作る） |

読む表と索引（`TestEveryStatementUsesAnIndex` が、実行した全 SQL の `EXPLAIN QUERY PLAN` に大きな表の全走査が無いことを固定する）:

| 読み | 表・索引 |
|---|---|
| 判断 | `jev_decisions` の `jev_decisions_type_timestamp_idx`（`decision_type`, `timestamp`）の範囲。並びが `timestamp, id` なので整列は発生しない |
| 足・銘柄 | `market_snapshots_instrument_timestamp_idx`（一意）、`instruments` の主キー |
| 取引候補 | `trade_signals_jev_decision_idx` |
| 保存済みラベル | `calibration_outcomes_decision_horizon_idx`、反実仮想は `counterfactual_labels_decision_idx` |
| 研究ラベル | `research_horizon_labels_instrument_at_idx`（`instrument_id`, `snapshot_at`） |
| シャドー・お試し | 各表の `(jev_decision_id, ...)` の一意索引（版・アームの件数は索引だけで数える） |

## 4. 書き出し

1. スイッチを読む。オフなら応答して終わる（何も書かない）。
2. 引数の分割を検査する（`datasetexport.CheckSplit`）。
3. §3 の読み取りで行を作り、ラベルの窓の終わりを行ごとに持つ（purge 用）。
4. 分割して CSV と Parquet にし、ファイルごとの `sha256` を取り、期間・分け方とファイルのハッシュから `dataset_sha256` と `dataset_id` を決める（`training-dataset.md` §10）。
5. `exports/` を `0700` で作り、その中に一時フォルダ（`.tmp-<id>-…`、`0700`）を作って、ファイル（`0600`、`O_EXCL`、`fsync`）を書き、`manifest.json` を最後に書いてから、一時フォルダを `exports/<dataset_id>/` に `rename` する（途中の状態は読み手に見えない。失敗したら一時フォルダを消す）。
6. すでに `exports/<dataset_id>/` があるとき: 各データファイル（CSV・Parquet）の `sha256` が新しい内容と一致し、`manifest.json` の `dataset_sha256` も同じで、その manifest 自身の期間・分け方・ファイルのハッシュから計算し直したハッシュも `dataset_sha256` と一致するなら**同じデータセット**（`dataset_id` は内容と期間・分け方のハッシュ）として何も書き直さず、既存の manifest を返す（`reason = "already_exported"`、`written = false`）。1 つでも違う・欠けている・通常のファイルでないなら**上書きせず**、`reason = "dataset_dir_conflict"`、`written = false` で今回の内容の manifest を返す（`TestRerunIsIdempotentAndADifferingFolderIsNeverOverwritten`）。

同じ入力（期間・分割・DB の内容）は同じバイト列・同じ `dataset_id` になり、期間か分割だけが違えば別の `dataset_id` になる（行は `timestamp, id` の昇順、数値は固定の 10 進表記、版・アームのスロットは件数の多い順・同数は名前順）。Parquet も書き込みの設定を固定し、ファイルに時刻・乱数を入れないので同じバイト列になる（0 / 1 のフラグを INT64 にする理由などは `training-dataset.md` §3）。`created_at` だけが呼び出しの時計（`manifest.json` に入るがハッシュには入らない）。

## 5. 量と時間

- 1 回の判断は最大 20000 件（`datasetexport.MaxDecisions`）。超えたら部分的なデータセットは作らず `limit_exceeded`（`query.ErrLimitExceeded`。期間を狭める）。
- 実測（合成データ。`TestTheDecisionLimitIsEnforcedAndTheFullSizeExports`）: 20000 判断（各判断に足・取引候補・保存済みラベル・研究ラベル 3 種・シャドー・お試し 1 件ずつ）の書き出しは、読み取り・CSV・Parquet・ハッシュ・書き込みを合わせて約 1.3 秒（-race なし。呼び出しの予算は 3 秒。Parquet の追加前は約 0.9 秒）。行は約 150 列で、このデータの CSV は 2 ファイル合計で約 6MB、Parquet（圧縮なし）も 2 ファイルで約 6MB（20000 行）。特徴量がすべて入る実データでは数倍になりうる [INFERENCE]。-race ではこの全量のテストは飛ばす（SQLite ドライバが約 25 倍遅くなる）。
- メモリ [INFERENCE]: 行は 1 判断あたり約 150 個の短い文字列（セル）で、20000 行で数十 MB。
- 期間の上限は標準の 366 日で足りる（判断の件数の上限が先に効く）。

## 6. 範囲外

- Parquet の圧縮・辞書の調整・ネストした型・タイムスタンプ型は扱わない（CSV と値が一致する単純な型だけ）。
- ラベルのホライズンは固定（calibration は 5・10・15・30・60 分、研究用は 30m・60m・close）。引数で変えない。
- 書き出したファイルの保持・削除は扱わない（利用者が `exports/` を管理する。再実行で増えるのは内容が違うときだけ）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1094 |
| 1.1 | 2026-11-19 | Parquet 出力（`parquetenc`・`parquet-go`）、`dataset_id` に期間と分け方を含める、既存フォルダの照合に manifest のハッシュの再計算を追加 | issue #1121・#1124 |
