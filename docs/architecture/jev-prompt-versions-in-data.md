# Jev プロンプトの版と、集計・学習・RAG の扱い（昇格後）

issue #881（元: #872 / PR #876 の独立レビューの指摘）。`jev_prompt_promote`（MCP）と Settings 画面の昇格でライブの Trader の版が変わると、版（`jev_decisions.question_version`。例: `trader-v3` と `trader-v4`）の違う行が、Calibration・RAG の候補・メタモデルの学習の**同じ集計に混ざる**。本章は、その扱いを固定する（Policy の標本数の確認とメタモデルの扱いも含む。§4）。版の定義・ハッシュ・昇格の仕組みは `paper-results-mcp-jev-prompt.md`、コードは `internal/service/jev/cohort.go`。

## 1. 決定: 版をコホートでまとめ、ライブの版のコホートだけを使う

- **コホート**: 答えの意味が同じ版のまとまり。`trader-v1`・`trader-v2`・`trader-v3` は `entry-quality-v3`、`trader-v4` は `entry-quality-v4`（`jev.traderCohorts`）。メタモデルは `entry_quality` の答えを特徴量に使う（`TraderResponse.Score`）ので、`entry_quality` の定義を変えた版は別のコホートにする。共通の `stateGuide` の言い換えだけの版（`trader-v2` → `trader-v3`）は同じコホートにできる
- **ライブの版のコホートの行だけ**を、次の読み手が使う。他のコホートの行は読まない。ライブの版は `jev.ResolveLiveVersion`（昇格が保存した版、なければビルドの既定）で、読み手が呼ぶたびに決める。**読み取りに失敗したら、混ぜずにエラーにする**（学習は失敗として記録され、RAG は文脈なしで呼ぶ）
- **登録された版を足すときはコホートを決める**: `TestEveryRegisteredTraderVersionHasACohort` が、コホートの無い版を落とす。ハッシュのテスト（版と文言の結び付け）と合わせて、版を足すたびに「同じ意味か」の判断が要る
- **今のデータでは何も変わらない**: ライブが `trader-v3`（既定）の間、除外されるのは `trader-v4` の行だけで、`trader-v4` の行は昇格するまで `jev_decisions` に無い（シャドーは `jev_shadow_decisions` に書く）。過去の `trader-v1` / `trader-v2` の行は `trader-v3` と同じコホートなので、これまでどおり読む。ライブの Trader / Scout / Policy が今日出す判断は、メタモデルを除いて変わらない（コホートを記録していない既存のメタモデルは、ライブが `trader-v3` のままでも Live では使わなくなる。§4.2）

## 2. 読み手ごとの扱い

| 読み手 | 場所 | 昇格後の扱い |
|--------|------|-------------|
| RAG の類似事例（Scout・Trader 共通） | `rag.Service.Context`（`WithForeignTraderVersions`） | 他コホートの **Trader** 判断は、実結果付きの検索（`labeledDecisionFilter`）にも補充の検索にも入れない。Scout の判断は版で除かない（Scout に昇格は無い）。昇格直後はライブの版の実結果付き事例がなく、事例は Scout の判断・スナップショットで補われる（FR-RAG-4 のコールドスタートと同じ） |
| Calibration の指標（GUI・`GET /api/v1/calibration`・自己改善の日次分析） | `calibration.CalibrationRepository.ListLabeledSamples*`・`decisiontrade.List` | 他コホートの標本と、その判断が開いたポジションの信頼度帯別 PnL を除く。標本が 0 から貯まり直す |
| メタモデルの学習 | `metalabel.Repository.ListTrainingRows`・`CountLabeledDecisions` | 他コホートの行を学習に使わない。見送りの件数（FR-META-4a）も同じ行を数える。昇格直後は行が足りず `ErrInsufficientData`（見送りとして記録）になりうる |
| MCP `jev_calibration` | `query.JevCalibration` | 既定はライブの版のコホートだけを採点する。`prompt_version` を渡すと、その版の行だけを採点する。`trader_decisions_by_version` は期間の trader 判断を**全版**について版別に数え、`prompt_versions.excluded` は既定で除いた版を示す |
| MCP `metalabel_training_readiness` | `metalabeldiag.TrainingReadinessAt` | 再学習と同じ行だけ数える。除いた版は `excluded_prompt_versions` |
| Policy の「信頼度帯に標本が足りるか」の確認 | `calibration.CalibrationRepository.CountLabeledSamplesInConfidenceRange`・`calibration.Service.BucketHasSamples`（旧しきい値ルールだけが使う） | ライブの版のコホートの標本だけを数える。足りない間の扱いは §4.1（Paper だけ全版の合計に戻してよい。Live は戻さない） |
| 採用中のメタモデル | `policy.Engine.Evaluate`（`WithMetaModelCohort`）・`metalabel_models.adoption_json` の `trained_cohort` | モデルが学習に使ったコホートを持ち、ライブの版のコホートと比べる。違うときの扱いは §4.2（Paper は警告して使う。Live は使わない） |

**そのままにしたもの**（混在ではなく、読み手の目的が版を問わないため）: ラベル付け（`calibration_outcomes` を全判断に付ける）、判断履歴・Activity・データエクスポート（版を列に出す）、`experiment_compare`（版・モデルが混在したら交絡として報告する。FR-MCP-35）、`jev_shadow_compare`（版ごとに並べる）。

## 3. 昇格の前後

1. 昇格前（ライブ `trader-v3`）: これまでと同じ行を読む。
2. 昇格（ライブ `trader-v4`）: 次の呼び出しから `trader-v4` の判断が `jev_decisions` に入る。上の読み手は `trader-v3` 系の行を読まなくなり、`trader-v4` の行だけで貯め直す。`trader-v3` 系の行は消えない。
3. 戻す（ライブ `trader-v3` に昇格）: `trader-v3` 系の行がまた読み手に入り、`trader-v4` の行が外れる。

## 4. Live に効く 2 つの扱い（決定済み）

昇格直後の Policy とメタモデルの扱いは、ライブの判断に効くので #881 の初版では決めず、そのままにしていた。**ユーザーが次のとおり決めた**（実装済み）。

### 4.1 信頼度帯の標本数の確認: 版（コホート）ごとに数える

- **「版」はライブの Trader の版のコホート**（§1。`trader-v1`〜`v3` は `entry-quality-v3` で 1 つ、`trader-v4` は `entry-quality-v4`）。版名の完全一致ではない。理由: 本番に残る `trader-v1` / `trader-v2` の行は `trader-v3` と答えの意味が同じで、完全一致にするとライブが `trader-v3` のままでも今日の標本数が減り、今日のライブ判断が変わる。標本の集計（Calibration・学習・RAG）と同じコホートで数えれば、信頼度帯の標本数も同じ根拠になる。
- **数え方**: `CountLabeledSamplesInConfidenceRange` は `ListLabeledSamples*` と同じ `SetForeignTraderVersions` の範囲（ライブの版と別のコホートの版を除く）だけを数える。ライブの版を読めないときは混ぜずにエラーにする。全版の合計は、同じ DB を指しコホートの範囲を持たない別のリポジトリ（`CalibrationRepository.AllVersions()`）と、それを包む `calibration.Service` で数える。
- **新しい版の標本が足りない間**:
  - **Paper だけ**、全版の合計で判断を続けてよい（`policy.WithCalibrationScope`・`policy/calibrationgate`。`metalabelsettings.RuntimeMode`）。そのとき、判断の記録（`trade_signals.reject_reason`。Policy の判断が永続化する唯一の文字列）の末尾に注記 `calibration_all_versions: judged on all-versions total; live version <版> has fewer than <n> labeled samples in the confidence bucket` を足し、「全版の合計で判断した」ことを残す（`judged on all-versions total` が、そう判断した印）。注記のつなぎは、元の理由に詳細があれば `; `、理由がコードだけ（`special_quote`・`missing_data`・`jev_none`・`not_lendable`・`vwap_z_unavailable`・`not_calibrated` や、`risk_engine_rejected: <Risk コード>`）なら `: `（元の理由の末尾の `:` の後に続けるので、`risk_rejections` の内訳が最初の `:`（Risk 拒否は 2 つ目の `:`）の前を取るとき、元の理由のコードのまま同じ内訳に入る。`other` に落ちない）。通過した判断（`risk_passed = 1`）は注記だけになる。
  - **Live は全版の合計に戻さない**: ライブの版の標本が足りなければ通らず、既存の `not_calibrated`（「標本が足りない」と同じ理由の系統）で、版名を入れて `not_calibrated: live version <版> has fewer than <n> labeled samples in the confidence bucket` と書く。Paper 以外のモード・モード未設定も Live と同じ（全版の合計を読みもしない）。Live の条件はどこも緩めていない。
- **ライブが `trader-v3` の間は今日と同じ**: `trader-v4` の行は昇格するまで `jev_decisions` に無く、`trader-v1`〜`v3` は 1 つのコホートなので、数える行は変わらない（足りない間の理由に版名が付く）。
- **この確認が効く範囲**: 確信度帯の標本数ゲート（`not_calibrated`）は、FR-POLICY-6 のとおり Live のメタ判定（`Engine.Evaluate` のモデル判定）には使われない。使うのは旧しきい値ルールだけ（Paper ブートストラップ。FR-POLICY-7。Paper だけ）と、バックテストの比較専用モード `Decide`。この build は Paper だけを動かす。Live のメタ判定は確信度帯の標本数をどこでも参照しない（版ごとに数える変更は Live のメタ判定の通る・通らないを変えない。「新しい版の標本が足りない間 Live は通さない」は、標本数を使う旧しきい値ルールの Live 側＝`Decide` の比較専用モードでだけ成り立つ）。Live のメタ判定に新しい標本数の条件は足していない（足すと、ライブが `trader-v3` のままでも今日の Live 判断が変わるため）。

### 4.2 メタモデル: 学習に使ったコホートを持たせる

- **記録**: 学習（`training.Trainer`）は、ライブの版のコホートを行の読み取りの前後で確かめ、モデルに `trained_cohort` として記録する（`metalabel_models.adoption_json`。**マイグレーションなし**。マイグレーション 000048 は #898 が使い、連番の検査が欠番を禁じる）。読めない・読み取りの途中で変わったときは、何も保存せず失敗にする（行と記録のコホートがずれたモデルを作らない）。
- **比較**: 判断のたびに `policy.Engine.Evaluate` が、採用中のモデルの学習コホートをライブの版のコホートと比べる（`policy.WithMetaModelCohort`）。
  - **一致**: これまでと同じ。
  - **不一致・Paper**: 警告ログ（`policy: decision judged on a fallback or a mismatching meta model (Paper)`。`note` に下の注記）を出し、モデルは使い続ける。判断が候補をモデルに通したなら、判断の記録の `reject_reason` の末尾に `; meta_model_cohort_mismatch: meta model <版> was trained on cohort <A>, live version <版> is cohort <B>` を足す（つなぎは上のとおり）。コホートを記録していないモデルでは、同じ位置に `meta model <版> has no recorded training version (cohort), live version <版> is cohort <B>` と書く。
  - **不一致・Live**: モデルを使わず、採用中のモデルが無いのと別に、既存の「キャリブレーションされていない」の経路（`not_calibrated: meta model … was trained on cohort …`、記録のないモデルは `not_calibrated: meta model … has no recorded training version …`。版名つき）で見送る。モデルは採点せず、予測も記録しない。Paper 以外のモード・モード未設定も同じ。
  - **ライブの版のコホートを読めない**: Live は `meta_model_unavailable` で見送る（推測しない）。Paper はエラーログを出してモデルを使う。
  - **コホートの確認が配線されていない**（`policy.WithMetaModel` だけで `policy.WithMetaModelCohort` が無い、または読み取り元が nil）: モードが無いのと同じ（Paper 以外）に扱い、モデルを使わず `meta_model_unavailable` で見送る（確認なしでモデルを使わない）。本番は `metalabeljob.PolicyOption` が両方を配線する。
- **コホートを記録していない既存のモデル（ユーザーの決定。実装者の決めを置き換えた）**: 何のコホートで学習したか分からないので、「版が一致しない」ものとして扱う。昇格前のコホート（`entry-quality-v3`）とはみなさない（`jev.LegacyModelCohort` は無い）。ライブが `trader-v3` のままでも一致しない。Live（モード未設定・コホートの確認が未配線・読み取り元が nil も同じ）は使わず、`not_calibrated: meta model <版> has no recorded training version (cohort), live version <版> is cohort <B>` で見送る（採点も予測の記録もせず、Risk Engine を呼ばない）。Paper は警告ログを出してモデルを使い続け、同じ内容を `meta_model_cohort_mismatch` の注記として判断の記録に残す。新しい学習は `trained_cohort` を記録するので、その学習が採用されれば Live も使える。**つまり、この変更のあと Live は、今ある学習済みのメタモデルを、新しい学習が `trained_cohort` を記録して採用されるまで使わない**（Live は Paper のビルドでは選べない）。
- 採用中のモデルの前向き評価（`forward_json`）は、これまでどおりライブの版の行で見る。

### 4.3 昇格を本番で使ってよいか

- **本番での昇格は、この変更を含むビルドが本番で動くまで見送る**（人が操作しない。#881 の前提のとおり）。このビルドより前では、昇格すると上の読み手が版を混ぜ、標本数の確認とメタモデルも旧版のものを使い続ける。
- **このビルドで安全になること**: 昇格しても、RAG の事例・Calibration の指標と PnL・メタモデルの学習・`jev_calibration` / `metalabel_training_readiness` に他のコホートの判断は混ざらない。Policy の標本数は新しい版の標本で数え（Paper だけ全版の合計に戻して記録を残す）、旧版で学習したメタモデルは Live で使われない。
- **昇格直後に起きること（Live）**: 旧コホートで学習したメタモデルは使われず、新しい学習が `trader-v4` の行で採用されるまで Live のメタ判定は `not_calibrated` になる（入らない側に倒れる）。昇格する人はこれを知っておく。Paper は警告つきで続く。

## 5. 棄却した案

- **版名の完全一致で分ける**: 一番単純だが、本番に残る `trader-v1` / `trader-v2` の行が、今日の学習・RAG から消える（ライブの判断が今日変わる）。`stateGuide` の言い換えだけの版まで別に数えることにもなる。
- **全版を混ぜたまま、版を表示するだけ**: 今の状態。昇格後に `entry_quality` の意味が違う行で学習する。
- **昇格のたびに過去の行を消す・別の表へ移す**: 戻せなくなり、`jev_shadow_compare` や実験の比較が使う履歴も失う。
- **Policy の標本数の確認を、Paper も含めて常にライブの版だけで数える**: 一貫するが、昇格直後は標本が 0 になり、Paper の取引が貯まるまで止まりうる。そのため Paper だけ全版の合計に戻してよいことにし（記録を残す）、Live は戻さない（§4.1）。
- **版名の完全一致で標本数を数える**（Policy）: 本番に残る旧版の行で今日の標本数が減る（§1 と同じ理由）。
- **メタモデルを版ごとに複数持ち、判断のたびに合う版を選ぶ**: モデルの採用の仕組み（最新の 1 つ）を変える。学習コホートを持たせて比べるだけにした（§4.2）。
- **メタモデルの学習コホートを新しい列に持つ**: マイグレーションが要る（000048 は #898 が使う）。既存の JSON 列 `adoption_json` に持つ。

## 6. テスト

- 版とコホート: `evalflow/cohort_test.go`（全登録版にコホートがある・ライブの版ごとの除外・昇格で除外が入れ替わる・読み取り失敗はエラー）
- RAG: `rag/version_scope_test.go`（`trader-v3` と `trader-v4` の実結果付き・無しの判断と Scout 判断を同じ距離に索引し、ライブの版ごとに返る事例を固定）
- Calibration: `repository/calibration/calibration_samples_test.go`（4 つの `ListLabeledSamples*` と Policy の件数が除く・全版の件数は除かない・読み取り失敗はエラー）、`service/calibration/bucketscope/bucketscope_test.go`（`trader-v1`〜`v4` を仕込み、ライブの版ごとの標本数と全版の合計）
- 学習: `repository/metalabel/training_repo_test.go`（`ListTrainingRows`・`CountLabeledDecisions`）
- Policy の標本数（§4.1）: `policy/calibflow/calibflow_test.go`（Paper: v4 の標本が足りず合計が足りるとき合計で判断して記録に残す・v4 が足りるとき・どちらも足りないとき・ライブが `trader-v3` のとき今日と同じ）、`policy/calibflow/breakdown_test.go`（注記つきの理由が `risk_rejections` の実際の内訳クエリで元のバケットに入り `other` に落ちない）、`policy/calibrationgate/calibrationgate_test.go`（Live は全版の合計を読まず `not_calibrated`・Paper 以外のモードも同じ・版名が理由に入る）
- メタモデル（§4.2）: `policy/metaflow/meta_cohort_test.go`（v3 で学習・ライブ v4: Paper は警告して使い Live は `not_calibrated`、記録のないモデルはライブ v3・v4 のどちらでも不一致（Paper は警告して注記つきで使い、Live は `not_calibrated`）、読めないとき、コホートの確認が未配線・読み取り元が nil のときは Paper 以外でモデルを使わない）、`policy/metacohort/metacohort_test.go`（モード・読み取り失敗・記録なしは Paper・Live とも不一致・一致・不一致の表）、`training/trainingflow/trained_cohort_test.go`（学習が記録する・読めない/途中で変わると失敗）、`repository/metalabel/repo_test.go`（`adoption_json` の往復）、`policy/decisionnote/decisionnote_test.go`（注記の連結と 255 バイト）
- MCP: `mcptests/jev_versions_test.go`（`jev_calibration` の既定・`prompt_version`・昇格後）、`metalabeltests/readiness_cases_test.go`（`metalabel_training_readiness`）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-22 | 新規作成。版のコホート、読み手ごとの昇格後の扱い、昇格を本番で使ってよいかの範囲、棄却案 | issue #881 |
| 1.1 | 2026-10-23 | §4 を「判断待ち」から決定済みに書き換え。Policy の標本数の確認をライブの版のコホートで数え（Paper だけ全版の合計に戻してよく、記録に残す。Live は戻さない）、メタモデルに学習コホートを持たせて比べる（Paper は警告して使い、Live は使わない）。記録のないモデルは昇格前のコホートとみなす | issue #881（ユーザーの決定） |
| 1.2 | 2026-10-23 | §4.2 のコホートを記録していない既存のモデルの扱いを、ユーザーの決定で「昇格前のコホートとみなす」から「版が一致しない（記録なし＝不一致）」に改めた。Live は、ライブが `trader-v3` のままでも使わず `not_calibrated` で見送り、新しい学習が `trained_cohort` を記録して採用されるまで続く。Paper は警告して注記つきで使う。§1 の「何も変わらない」にその例外を追記。`jev.LegacyModelCohort` を削除 | issue #881（ユーザーの決定） |
