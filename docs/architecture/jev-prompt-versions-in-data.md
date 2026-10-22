# Jev プロンプトの版と、集計・学習・RAG の扱い（昇格後）

issue #881（元: #872 / PR #876 の独立レビューの指摘）。`jev_prompt_promote`（MCP）と Settings 画面の昇格でライブの Trader の版が変わると、版（`jev_decisions.question_version`。例: `trader-v3` と `trader-v4`）の違う行が、Calibration・RAG の候補・メタモデルの学習の**同じ集計に混ざる**。本章は、その扱いを固定する。版の定義・ハッシュ・昇格の仕組みは `paper-results-mcp-jev-prompt.md`、コードは `internal/service/jev/cohort.go`。

## 1. 決定: 版をコホートでまとめ、ライブの版のコホートだけを使う

- **コホート**: 答えの意味が同じ版のまとまり。`trader-v1`・`trader-v2`・`trader-v3` は `entry-quality-v3`、`trader-v4` は `entry-quality-v4`（`jev.traderCohorts`）。メタモデルは `entry_quality` の答えを特徴量に使う（`TraderResponse.Score`）ので、`entry_quality` の定義を変えた版は別のコホートにする。共通の `stateGuide` の言い換えだけの版（`trader-v2` → `trader-v3`）は同じコホートにできる
- **ライブの版のコホートの行だけ**を、次の読み手が使う。他のコホートの行は読まない。ライブの版は `jev.ResolveLiveVersion`（昇格が保存した版、なければビルドの既定）で、読み手が呼ぶたびに決める。**読み取りに失敗したら、混ぜずにエラーにする**（学習は失敗として記録され、RAG は文脈なしで呼ぶ）
- **登録された版を足すときはコホートを決める**: `TestEveryRegisteredTraderVersionHasACohort` が、コホートの無い版を落とす。ハッシュのテスト（版と文言の結び付け）と合わせて、版を足すたびに「同じ意味か」の判断が要る
- **今のデータでは何も変わらない**: ライブが `trader-v3`（既定）の間、除外されるのは `trader-v4` の行だけで、`trader-v4` の行は昇格するまで `jev_decisions` に無い（シャドーは `jev_shadow_decisions` に書く）。過去の `trader-v1` / `trader-v2` の行は `trader-v3` と同じコホートなので、これまでどおり読む。ライブの Trader / Scout / Policy が今日出す判断は変わらない

## 2. 読み手ごとの扱い

| 読み手 | 場所 | 昇格後の扱い |
|--------|------|-------------|
| RAG の類似事例（Scout・Trader 共通） | `rag.Service.Context`（`WithForeignTraderVersions`） | 他コホートの **Trader** 判断は、実結果付きの検索（`labeledDecisionFilter`）にも補充の検索にも入れない。Scout の判断は版で除かない（Scout に昇格は無い）。昇格直後はライブの版の実結果付き事例がなく、事例は Scout の判断・スナップショットで補われる（FR-RAG-4 のコールドスタートと同じ） |
| Calibration の指標（GUI・`GET /api/v1/calibration`・自己改善の日次分析） | `calibration.CalibrationRepository.ListLabeledSamples*`・`decisiontrade.List` | 他コホートの標本と、その判断が開いたポジションの信頼度帯別 PnL を除く。標本が 0 から貯まり直す |
| メタモデルの学習 | `metalabel.Repository.ListTrainingRows`・`CountLabeledDecisions` | 他コホートの行を学習に使わない。見送りの件数（FR-META-4a）も同じ行を数える。昇格直後は行が足りず `ErrInsufficientData`（見送りとして記録）になりうる |
| MCP `jev_calibration` | `query.JevCalibration` | 既定はライブの版のコホートだけを採点する。`prompt_version` を渡すと、その版の行だけを採点する。`trader_decisions_by_version` は期間の trader 判断を**全版**について版別に数え、`prompt_versions.excluded` は既定で除いた版を示す |
| MCP `metalabel_training_readiness` | `metalabeldiag.TrainingReadinessAt` | 再学習と同じ行だけ数える。除いた版は `excluded_prompt_versions` |

**そのままにしたもの**（混在ではなく、読み手の目的が版を問わないため）: ラベル付け（`calibration_outcomes` を全判断に付ける）、判断履歴・Activity・データエクスポート（版を列に出す）、`experiment_compare`（版・モデルが混在したら交絡として報告する。FR-MCP-35）、`jev_shadow_compare`（版ごとに並べる）。

## 3. 昇格の前後

1. 昇格前（ライブ `trader-v3`）: これまでと同じ行を読む。
2. 昇格（ライブ `trader-v4`）: 次の呼び出しから `trader-v4` の判断が `jev_decisions` に入る。上の読み手は `trader-v3` 系の行を読まなくなり、`trader-v4` の行だけで貯め直す。`trader-v3` 系の行は消えない。
3. 戻す（ライブ `trader-v3` に昇格）: `trader-v3` 系の行がまた読み手に入り、`trader-v4` の行が外れる。

## 4. 昇格を本番で使ってよいか

- **本番での昇格は、この変更を含むビルドが本番で動くまで見送る**（人が操作しない。#881 の前提のとおり）。このビルドより前では、昇格すると上の読み手が版を混ぜる。
- **このビルドで安全になること**: 昇格しても、RAG の事例・Calibration の指標と PnL・メタモデルの学習・`jev_calibration` / `metalabel_training_readiness` に、他の版の判断は混ざらない。
- **このビルドでも変わらないこと（昇格する人が知っておくこと）**。混在ではなく、旧版で作られた判断材料が新しい版の判断に使われ続ける問題で、ライブの判断を変えるため #881 では決めていない（判断待ち）:
  1. Policy の「信頼度帯に標本が足りるか」の確認（`CountLabeledSamplesInConfidenceRange`）は版で分けない。分けると昇格直後は標本が 0 になり、Paper の取引が貯まるまで止まりうる。
  2. 採用中のメタモデルは昇格しても外れない。`trader-v3` の `entry_quality` で学習したモデルが、新しい学習が採用されるまで `trader-v4` の答えを採点する（採用中のモデルの前向き評価も、ライブの版の行で見る）。

## 5. 棄却した案

- **版名の完全一致で分ける**: 一番単純だが、本番に残る `trader-v1` / `trader-v2` の行が、今日の学習・RAG から消える（ライブの判断が今日変わる）。`stateGuide` の言い換えだけの版まで別に数えることにもなる。
- **全版を混ぜたまま、版を表示するだけ**: 今の状態。昇格後に `entry_quality` の意味が違う行で学習する。
- **昇格のたびに過去の行を消す・別の表へ移す**: 戻せなくなり、`jev_shadow_compare` や実験の比較が使う履歴も失う。
- **`CountLabeledSamplesInConfidenceRange`（Policy）も版で分ける**: 一貫するが、昇格直後に Paper の取引が止まりうる。Live の判断を変えるので判断待ちとした（§4）。
- **メタモデルの有効・無効を版で切り替える**: Live の判断（メタモデルの採用）に触れる。判断待ち（§4）。

## 6. テスト

- 版とコホート: `evalflow/cohort_test.go`（全登録版にコホートがある・ライブの版ごとの除外・昇格で除外が入れ替わる・読み取り失敗はエラー）
- RAG: `rag/version_scope_test.go`（`trader-v3` と `trader-v4` の実結果付き・無しの判断と Scout 判断を同じ距離に索引し、ライブの版ごとに返る事例を固定）
- Calibration: `repository/calibration/calibration_samples_test.go`（4 つの `ListLabeledSamples*` が除く・Policy の件数は除かない・読み取り失敗はエラー）
- 学習: `repository/metalabel/training_repo_test.go`（`ListTrainingRows`・`CountLabeledDecisions`）
- MCP: `mcptests/jev_versions_test.go`（`jev_calibration` の既定・`prompt_version`・昇格後）、`metalabeltests/readiness_cases_test.go`（`metalabel_training_readiness`）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-22 | 新規作成。版のコホート、読み手ごとの昇格後の扱い、昇格を本番で使ってよいかの範囲、棄却案 | issue #881 |
