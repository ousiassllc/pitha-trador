# Paper 実績 MCP: 研究ノートの記録と読み取り（issue #1095）

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-research-notes.md` §3.47〜§3.50、表は `docs/architecture/er/tables-research.md`（マイグレーション `000067`）。

## 1. 目的と位置づけ

今は最適なやり方を探す期間で、AI（あいり）がいろいろ試す。調整の実験（`tuning_experiments`、`paper-results-mcp-experiments.md`）は設定を 1 つ変えたときの記録だけで、それ以外の調べもの（データの集計・バックテスト・比較台帳の結果・外部の情報）で分かったことはアプリの中に残らない。研究ノートは、その**問い・仮説・やったこと・結果の要約・結論（支持／不支持／保留）・根拠へのリンク（実験 ID・バックテストの結果 ID・比較台帳の条件・URL）・タグ**を蓄積し、後の改善や昇格の判断に人が読めるようにする。

## 2. 決定事項

- **記録だけ**: ノートの読み書きは設定・取引・Risk・発注・執行・ブローカー・Paper のガード・Live・Jev（trader-v3 と `JEV_*`）・既存のシャドー・Feature Engine に何も影響しない。ノートを書いても `runtime_settings` や取引の表は 1 行も変わらない（§5 のテスト）。
- **書く経路は MCP の `research_note_create` / `research_note_update` だけ**。実験のツールと同じく、`mcpset.Writer`（書き込み専用の狭い接続）が書き込み許可（`paper-results-mcp.md` §7.2。オフなら `write_disabled`）の下で、1 回 1 トランザクションで書く。収集・バックグラウンドのタスクは無い（AI が呼んだときだけ記録される）。Settings 画面は**読み取りだけ**（§6）。
- **追記が基本**: 作成は版 1、更新は現在の版 + 1 を `research_note_versions` に追記する。前の版は消さず `research_note_get` で読める。`research_notes` は現在の版の写し（一覧・検索が版の表を引かないための頭）で、更新のとき同じトランザクションで版の行を足したあとに `UPDATE ... WHERE id = ? AND current_version = ?` で差し替える。
- **版の行は書き換え・削除をしない**: コードに版を `UPDATE` / `DELETE` する文は無く（`researchnote` の隔離テストが固定）、表のトリガが書き換えを拒否する（追記専用はトリガで守る）。`research_note_versions_no_update` / `_no_delete` は `UPDATE` / `DELETE` を、`research_note_versions_no_replace`（`BEFORE INSERT`）は同じ `(note_id, version)` または同じ `id` の行がすでにある `INSERT`（`INSERT OR REPLACE` / `REPLACE INTO` を含む）を拒否するので、`REPLACE` でも履歴は書き換えられない。ノート自体を削除する経路は無く、アプリの接続では版の外部キー（`research_notes.id`）が版のあるノートの削除を拒む（外部キーの強制は接続の設定による。書き込み専用の接続が外部キーを有効にしているかには頼らず、追記専用はトリガで守る）。
- **楽観ロック**: `research_note_update` は `expected_version`（読んだときの `current_version`）が現在の版と違えば `research_note_version_conflict`（409）で何も書かない。同時に 2 人が同じ `expected_version` で更新すると、ちょうど 1 人だけが成功して版 2 になり、もう 1 人は衝突を受けて読み直せる（書き手は 1 接続の `BEGIN IMMEDIATE` で直列。`notetests` の同時更新のテストが固定）。
- **量の上限**: 問い 500・仮説 1000・やったこと 3000・結果 3000 文字、根拠リンク 20 件（`ref` 300 文字）、タグ 10 件（各 40 文字）、1 ノート 50 版。表の `CHECK` が文字数・件数・結論の値・JSON の形を守る。一覧は 1 回 100 件まで（既定 20）、`research_note_get` は 50 版まで（応答は数百 KB に収まる）。

## 3. 秘密の伏せ字

ノートの本文に秘密を書き込めないよう、**保存の前に**、自由文（問い・仮説・やったこと・結果の要約・タグ・根拠の `ref`）のキーの形の文字列を `[masked]` に置き換える（作成でも更新でも。`researchnote.Fields.Clean` → `researchnote/mask`）。版にも、全ツールの応答にも、一覧にも、伏せたあとの本文だけが出る。

| 形 | 例（架空） | 結果 |
|---|---|---|
| 既知の接頭辞 | `sk-…`・`ghp_…`・`github_pat_…`・`xox[abprs]-…`（10 文字以上）・`SG.<22>.<43>`（SendGrid）・`AKIA…`・`AIza…`・`sk_live_…`。直前が英数字でなければ（`foo_sk-…` のように `_` や記号の直後でも）当たる | その文字列を `[masked]` |
| 認証ヘッダ | `Authorization: <Bearer / Basic / Token / ApiKey / Digest / 方式なし> …`。引用符で値が始まるときはその閉じ引用符まで、そうでなければ行末まで（`'Authorization: Bearer x'` のようにヘッダ全体を囲む閉じ引用符があればそこまで） | 値の全体を `[masked]`（`Authorization:` は残す） |
| Bearer | `Bearer abc…`（大文字始まり）。小文字の `bearer` は英単語でもあるので、続く語に数字があるときだけ | 方式名は残し、トークンを `[masked]`。`Basic` / `Digest` は `Authorization:` の直後でだけ扱い、文中の "the basic approach" は変えない |
| JWT / 点区切り | `eyJ….eyJ….sig`、英数字 8 文字以上 ×3 以上の点区切りで、**どの区切りにも**英字と数字がある | `[masked]`（`sagemaker.us-east-1.amazonaws.com` のようなホスト名は変えない） |
| 代入の形 | キー名が `token`・`secret`・`password`・`passwd`・`passphrase`・`credential(s)`・`authorization`・`cookie`・`signature` で**終わる**、または `api_key`・`access_key`・`secret_key`・`private_key` を含む（`OPENAI_API_KEY`・`apiKey`・`x-api-key`・`db.password` も）、または `pass`・`pw`・`pwd`・`auth`・`session`・`session_id` そのもの。区切りは `:` `=` `：` `＝`（キーの後ろの引用符、`\"api_key\": \"…\"` の JSON エスケープも可） | キーと区切りは残し、値を `[masked]`。値が引用符で始まれば閉じ引用符まで（空白を含む）、そうでなければ次の空白・引用符・`,`・`、`・`。` まで（`& ; @ ! #` も値の一部。ただし `&name=` が続くなら次の検索パラメータとして止める）。`token: Bearer abc` のように方式名を挟めば方式名も |
| 区切りなしの代入 | `token abc12345xyz`・`secret …`・`password …`・`passwd …`（空白だけで区切る） | 値が 8 文字以上で数字を含むときだけ値を `[masked]`（`token budget` は変えない） |
| 日本語のキー | `パスワード`・`トークン`・`秘密鍵`・`秘密`・`シークレット`・`APIキー`・`API キー`・`認証` に `:` `=` `：` `＝` | 値を `[masked]`。`は` で続くとき（`パスワードは Abc12345xyz です`）は、値が 8 文字以上で数字を含むときだけ（`認証は不要` は変えない） |
| URL の資格情報 | `https://user:pass@host` | パスワード部分を `[masked]` |
| PEM の秘密鍵 | `-----BEGIN … PRIVATE KEY-----` | 本体を `[masked]` |
| 長い不透明な文字列 | 32 文字以上の英数字・`+/=_.:~%-` の連なり。区切り（`/ . _ - : ~ %`）が無ければ、英字と数字の両方を含む（16 進のハッシュ・git のコミット ID 含む）、または 40 文字以上で大小文字が混ざるもの。区切りがあれば、区切りで割った 32 文字以上の不透明な 1 片をその片だけ。URL でも `/` 始まりのパスでもない連なりは、大小文字と数字が混ざる 8 文字以上の片があるか、大小文字が混ざって `+` を含むとき（base64 / base64url のトークン）全体 | `[masked]` |

- **変えないもの**: `max_tokens: 4096`・`token_count: 12`（キー名が `token` で終わらない）、`pass_rate: 0.8`、GitHub の URL（`https://github.com/ousiassllc/pitha-trador/issues/1095`）、パス（`db/migrations/000067_create_research_notes`）、snake_case の識別子（`jev_evaluation_horizon_30m_cohort_llm_vs_baseline`）、UUID、ホスト名、`2026-11-15T10:00:00+09:00` のような日時、普通の英文。URL・パスの中の 32 文字未満のトークンは見分けられず変えない（32 文字以上なら、その片だけを伏せる）。
- **見えない文字**: 伏せる前に Unicode の書式文字（カテゴリ Cf。ゼロ幅スペース・ゼロ幅結合子・語結合子・BOM・ソフトハイフン・双方向制御）を取り除く（`sk-\u200bproj-…` や `Bearer abc\u200b123` で規則をすり抜けられないため）。取り除いた結果が保存される（ZWJ で連結した絵文字は分かれて見える）。
- **空白類**: 区切り（`:` `=` `：` `＝`・`Bearer` など）の後ろの空白は、ASCII の空白・タブに加えて Unicode の空白（`\p{Zs}`。NBSP・IME の全角スペース U+3000 など）も読む。- **全角英数**: 規則は、文章を NFKC 相当に畳んだ写し（全角の英数字・記号 `ＡＰＩ_ＫＥＹ＝`・`ｓｋ－`・`Ｂｅａｒｅｒ`・全角スペースなど、NFKC で ASCII になる文字を ASCII にしたもの）に当てる。そのため全角のキー名・接頭辞・`Bearer` も半角と同じ規則で `[masked]` になる。伏せるのは見つけた範囲だけで、元の文章の同じ範囲（畳むと長さが変わる文字があっても、元の文字の境界で）を置き換える。伏せなかった部分は書いたまま（全角のまま）保存され、普通の日本語や「変えないもの」の例（`max_tokens`・GitHub の URL・日時）は新たに伏せない。検査の失敗の理由は値を引用しない（全角でも同じ）。実装は `researchnote/mask/doc.go`（`golang.org/x/text/unicode/norm`）。
- 方針は粗めで、誤検出（`session: running` や、`Bearer token` の `token` や、`passwd` の値を伏せる）は許し、漏れを許さない。同じ入力は何度掛けても同じ結果（冪等。更新で変えない項目を再び掛けても変わらない）。
- 文字数の上限は伏せる前（処理量の上限）と伏せたあと（`[masked]` のほうが長い場合）の両方で確かめる。検査の失敗の理由は値を引用しない。
- タグは伏せたあとに小文字にして重複を除く（接頭辞の判定が大文字小文字を区別するため）。
- `mcp_settings_audit` には書かない（実験のツールと同じ）。書いた人（`actor` = `mcp`）・接続元（`client`）・時刻は各版の行に残る。ログには失敗のコードしか出さない。

## 4. 検索

全文検索（FTS）は使わない。`research_notes_list` の `query` は、問い・仮説・やったこと・結果の要約と、タグ 1 つ 1 つ（`json_each(tags)` の値）に対する `LIKE '%…%' ESCAPE '\'` で、`%` `_` `\` は先に `\` で打ち消して普通の文字として探す。タグは JSON 文字列全体ではなく値ごとに比べるので、`"` や `[` のような JSON の記号だけの `query` がタグの列に当たることはない。英字の大文字小文字は区別しない（SQLite の既定）。`tag` は `json_each(tags)` で完全一致、期間は `updated_at` の範囲（JST の日付 `[from, to)` を UTC の固定 9 桁に直して文字列で比べる）。並びは `updated_at DESC, id DESC`。

## 5. テスト（受け入れ条件との対応）

| 条件 | テスト |
|---|---|
| MCP から書き込み・一覧・検索・更新ができ、更新前の版が残る | `notetests` の `TestCreateListSearchUpdateKeepsEarlierVersions`、`TestUpdateWithStaleExpectedVersionIsAConflict`、`TestNoteWritesFollowTheWriteSwitch`、`TestSearchTreatsLikeWildcardsAsPlainCharacters`、`TestInvalidRequestsStoreNothing`、`TestVersionsPerNoteAreBounded` |
| キーの形の文字列が伏せて保存される | `notetests` の `TestKeyShapedStringsAreMaskedBeforeStoring`（行・応答・版・監査表・検索）、`TestRefusalsDoNotEchoTheCallersText`、`researchnote/mask` の単体テスト、`researchnote` の `TestClean*` |
| 取引・Risk・発注・Jev・設定に差分がない | `notetests` の `TestNotesChangeNoSettingAndNoTradingState`（研究ノートの 2 表以外のすべての表を書き込み・読み取りの前後で比べる）、`TestNoteWritePathIsNarrow`（実行した書き込み文が 3 種類だけで、版の `UPDATE` / `DELETE` はトリガが拒否）、`researchnote/isolation_test.go`（表名・パッケージを参照できるファイルを限り、`researchnote` が import するのは SQL の補助と伏せ字だけ。Policy・Risk・発注・ブローカー・Jev・Feature Engine・設定のコードを呼べない） |
| マイグレーション | `db/migrationtests/migration_000067_test.go`（66↔67、既存の表に触れない、CHECK・UNIQUE・トリガ・外部キー） |
| Settings の一覧 | `internal/router/mcproute/research_notes_test.go`、`internal/web/handler/mcpsettings/research_notes_test.go`、検索・タグ・結論の絞り込みが `research_notes_list` と同じノートを出すこと・条件に合わないノートが出ないこと・20 件の上限・伏せた本文だけが出ること・書き込みの経路が無いこと（`internal/web/handler/mcpsettings/research_notes_filter_test.go`） |

## 6. Settings 画面（読み取り専用）

MCP のカード（書き込みの節の下）に「研究ノート」の節を出す（`GET /settings/mcp/research-notes`。`internal/web/handler/mcpsettings/research_notes.go`、`internal/web/organisms/mcp/research_notes.templ`）。新しい順に 20 件、結論のバッジ（支持・不支持・保留）、問い、更新時刻（JST）、版の番号を並べ、開くと仮説・結果の要約（先頭 200 文字）・根拠リンクの件数・タグを見られる。書き込みのフォームも経路も無い。ゲートが読み取りを持たない構成（一部のテスト）ではこの節も経路も出ない。

### 6.1 検索と絞り込み（issue #1132）

一覧の上に GET のフォーム（検索語・タグ・結論）を置く（`GET /settings/mcp/research-notes?q=…&tag=…&conclusion=…`。HTMX で節だけを差し替え、保存や更新はしない）。条件は `research_notes_list` の `query` / `tag` / `conclusion` と同じ意味で、画面も MCP も同じ `researchnote.List` を通すので、同じ条件なら同じノートが同じ順で出る（§4）。一覧のタグは、そのタグで絞り込むリンクになる。

- 上限は変えない。絞り込んでも 20 件までで、ページングや「続き」は無い。20 件に達したときは「20 件まで表示」と出し、検索語・タグ・結論で絞るよう促す。
- 検索語は 200 文字、タグは 40 文字まで（前後の空白は除く）、結論は `supported` / `unsupported` / `pending` のどれか。外れた値は 422 とインラインの理由で断り、何も出さない（MCP が `invalid_argument` で断るのと同じ範囲）。
- 期間（`from` / `to`）は画面には足さない（MCP からだけ）。
- 表示するのは保存済みの（§3 で伏せたあとの）本文だけ。生の鍵の形の文字列は画面に出ず、それで検索しても当たらない。

## 7. 実装の置き場所

| 内容 | 場所 |
|---|---|
| 語彙・検査・伏せ字の呼び出し・SQL | `internal/service/paperresultsmcp/researchnote`（`note.go` / `store.go` / `read.go`）、伏せ字 `researchnote/mask` |
| 書き込み（`mcpset.Writer.InTx` 上の 2 トランザクション `CreateNote` / `UpdateNote`）と Settings 用の一覧（`Gate` が埋め込む `researchnote.Lister`。読み取り専用トランザクション） | `internal/service/paperresultsmcp/researchnote/notetool/write.go`・`internal/service/paperresultsmcp/researchnote/lister.go`（`mcpset/` に研究ノートの処理は置かない。`mcpset/gate.go` は `Lister` を埋め込むだけ） |
| ツール定義 | `internal/service/paperresultsmcp/researchnote/notetool`（4 ツールのスキーマ・説明・引数検査・エラー写像。サーバー本体は import しない）。ディスパッチ表への登録は `internal/service/paperresultsmcp/ext_tools.go`（`notetool.Defs()` を `tooldef.Def` として他の研究ツールと同じく表の項目に変換する） |
| エラーコード | `research_note_not_found`（404）・`research_note_version_conflict`（409）。`errors.go` と `paper-results-mcp-errors.md`。ツールの失敗は `query.CodedError`（`ErrorCode` / `ErrorDetail`）を実装した `notetool.Error` で返し、サーバーの `classify` が写す |
| 表 | `db/migrations/000067_create_research_notes.{up,down}.sql` |

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1095 |
| 1.1 | 2026-11-15 | §3 の伏せ字の規則を実装に合わせて書き直し（値の範囲・キー名の語彙・日本語のキー・認証ヘッダ・変えないもの・見えない文字）、§2 の追記専用はトリガ（`BEFORE INSERT` の `_no_replace` を含む）で守ること・同時更新は 1 人だけが成功することを明記、§4 の検索はタグを値ごとに比べると明記 | issue #1095 レビュー修正 |
