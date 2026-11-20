# 調査メモ: ChatGPT plan usage（Sign in with ChatGPT）の直接 Responses 経路（issue #1111）

**調査だけで、実装ではない。** #1081 の Codex CLI 経路（`codex exec`。`architecture/overview/integrations-codex-chatgpt.md`）は**置き換えない**。ここでは、公式の公開文書だけを読み、ChatGPT plan usage（Sign in with ChatGPT。以下 SIWC）の Responses API を pitha が直接呼ぶ経路が使えるか、使うと何が変わるかを整理する。

- 調べた日: 2026-11-18。**公開ページを読んだだけ**（ログイン・認証つきの実行・キーの投入・有料の呼び出し・実際のエンドポイントの呼び出しはしていない。DevKit のコードも取り込んでいない）。文書は更新が速いので、判断の前に最新を読み直す。
- 書き分け: **文書にある**＝URL の本文にそう書いてある。**推測**＝文書から読み取った判断で、文書には書かれていない。**不明**＝公開情報では分からない。
- 参照した URL の一覧は末尾。本文の `[N]` はその番号。

## 結論の一覧

| 項目 | 結論 | 区分 | 根拠 |
|------|------|------|------|
| pitha（AGPL・公開リポジトリ）が OSS 向けの対象か | 「オープンソースのツール／手元で動かす個人のプロジェクト」は対象と**書いてある**。ライセンスの種類（AGPL）・公開リポジトリであることの条件は**不明** | 文書にある＋不明 | [1][2][3][4] |
| OAuth の登録（`dynamic_agent_client`）の要否 | OSS は**事前の申請なし**で、初回のサインインの中で動的に登録される（クライアントシークレット・パートナーの API キーは不要） | 文書にある | [1][5] |
| Desktop アプリから使えるか | **使える**（公式の例が Electron のデスクトップアプリ。ループバックのコールバック＋システムのブラウザ） | 文書にある | [2][5] |
| `store:false`・`stream:true` | **付ける必要がある**（HTTP の推論リクエストのたび）。付けられる | 文書にある | [6][7] |
| 上限エラー | `subscription_sharing_usage_limit_exceeded`＝**HTTP 429**。ストリーム開始後は`response.failed`としても来うる。再開時刻は**この文書からは分からない** | 文書にある＋不明 | [6][8] |
| Codex CLI との比較 | 利点（構造化エラー・アカウント側の制御）と欠点（OAuth・トークン保管を pitha が持つ・対象と規約の不確かさ・Preview）がある。§5 の表 | 文書＋推測 | §5 |
| 今の判断 | **Codex CLI 経路を置き換えない**。決める前に、§6 の「ログインしないと分からないこと」と規約・ライセンスの確認が要る | 推測 | — |

## 1. OSS・AGPL の対象になるか

文書にあること:

- ChatGPT plan usage は、オープンソースのパートナーと選ばれた非公開クライアントに提供される [1]。OSS 向けのページは「オープンソースと手元で動かすアプリ」向けと書いている [3]。
- 公式の例（クックブック）は「オープンソースのプロジェクト・**手元で動かす個人のプロジェクト**・選ばれた非公開アプリ」に使えるとし、**有料またはリモートホストのアプリは待機リスト**（申請）の対象とする [2]。
- ヘルプセンターは「対応するオープンソースのツールは、すべての ChatGPT ユーザーが使える」と書き、パートナー一覧にオープンソースの連携（OpenClaw・OpenCode・Pi・T3）を載せている [4][9]。
- 使える人（ユーザー側）は、OSS の説明では ChatGPT **Plus と Pro** [1][2]（商用アプリでは Go・Plus・Pro・Business と書かれている [4]）。

文書にないこと（**不明**）:

- 「オープンソース」の定義（OSI 承認のライセンスか・リポジトリの公開が条件か・AGPL を含むか）。公開文書にライセンスの許可リスト・審査の記述は見つからなかった。
- 初回の動的登録で OpenAI が OSS かどうかを確かめるか（登録は自動で、確かめる手順の記述は無い）。

規約まわり（文書にある。SIWC 規約 2026-11-04 施行 [10]）:

- アプリの名前を偽らない。トークンの永続保管は**ローカルでユーザーの管理下**に限る（リモート・マネージドな環境に置かない）。
- ChatGPT plan usage の追加条件: アカウントの複数化・分割・ローテーションによる上限回避、トークンや枠の共有・転売、**ある人の契約で別の人の要求を満たす**ことを促さない。要求は**ユーザーの手元の実行環境（またはそのユーザーだけが管理するリモート）**から出す。バックグラウンド処理は**明示の同意**を取る。接続したアプリ以外には使わない（汎用の API として提供しない）。ユーザーが自分の契約で使うために、**アプリの代金を取らない**。
- 規約は DevKit の「適用されるライセンス」に従うことも求める [10]。DevKit（サンプルと SDK）のライセンスは **Noncommercial License v1.0**（非商用の目的に限る）[11]。

推測（文書にはない）:

- pitha は個人が手元で動かす単独利用の公開リポジトリなので、規約の「ユーザーの手元の実行環境」「ユーザーのためだけ」の趣旨とは合う。ただし AGPL との関係、バックグラウンド（ライブの判断ごと）での呼び出しにあたる**明示の同意**の取り方は、公開文書だけでは判断できない。
- DevKit のコードは非商用ライセンスなので、AGPL のリポジトリには**取り込まない**方が安全（AGPL は商用利用を妨げない）。仕様から自前で書く場合に規約の「SIWC ソフトウェアの改変・リバースエンジニアリング禁止」に触れるかは**不明**。法務の判断は悠太さん。

## 2. OAuth の登録と、Desktop アプリからの利用

文書にあること [5]:

- 初回は `client_id=dynamic_agent_client` で認可に進み、`ext_agent_host_id`（ホストごとの安定した不透明な ID。`urn:uuid:…` など）と `agent_name_hint`（アプリの実際の名前）を付ける。コールバックで**発行された `client_id`**（`oaiapp_…`）が返るので、アカウントごとに保存して以後それを使う（`dynamic_agent_client` は保存しない）。
- **クライアントシークレットもパートナーの API キーも不要**（公開クライアント＋PKCE S256）。リダイレクト先は**ループバック**の `http://127.0.0.1:<ポート>/auth/callback`（`localhost` は不可。ポートだけ変えてよい）。システムのブラウザを開く。
- スコープは `openid profile email` と、plan usage 用の `offline_access resource.invoke chatgpt.tokens.use.direct`。`resource` は `https://api.openai.com/v1`。ID トークンを検証し、`chatgpt.tokens.use.direct` が付与されたことを確かめてから推論に進む。
- トークン: アクセストークン 1 時間、リフレッシュトークン 30 日（更新のたびに新しい 30 日）[13]。更新は直列にする [12]。保管は所有者だけが読めるローカルファイル（Unix は `0600`）[5]。
- ヘッドレス・VM では、手元で OAuth を済ませて認証情報を転送する手順がある（転送したセッションの使用量のホスト別の帰属と失効は**まだ使えない**）[14]。
- 事前の「client ID の申請」ページは、**商用パートナーのサインイン**（待機リスト）の話 [15]。OSS の plan usage の動的登録とは別。

Desktop アプリ（文書にある）: 公式の例は**Electron のデスクトップアプリ**（Paste Perfect）で、メインプロセスがブラウザのサインイン・保管・API 呼び出しを持つ [2]。手元の実行環境で動くことが規約の前提 [10]。

推測: pitha は Go のローカルサーバーなので、`127.0.0.1` のリスナーとシステムのブラウザは用意できるはず。ただしサインインの画面・複数アカウントの扱い・ホスト ID の永続化・リフレッシュの直列化を pitha が持つ設計になり、今の「pitha はトークンを持たない」方針（#1081）を変える。

## 3. `store:false`・`stream:true`

文書にあること [6][7]:

- `POST https://api.openai.com/v1/responses` に、アクセストークンを `Authorization: Bearer` で送る。ChatGPT の `backend-api` は**使わない**。
- HTTP の推論リクエストは**毎回 `store:false` と `stream:true`**。ストリームを`response.completed`まで読む（それが成功。`response.failed`・`response.incomplete`・途中切れは別に扱う）。
- `input` は配列（履歴は毎回自分で入れる）。`instructions` か developer メッセージを使う（`system` ロールのメッセージ項目は拒否される）。`previous_response_id` は HTTP では使わない。
- **使えない項目**: `background`・`conversation`・`max_output_tokens`・`max_tool_calls`・`metadata`・`moderation`・`multi_agent`・`prompt`・`prompt_cache_retention`・`safety_identifier`・`temperature`・`top_logprobs`・`top_p`・`truncation`・`user`。ホスト型のツール（画像生成・ファイル検索・Code Interpreter・ホスト型 MCP など）も使えない。
- `GET /v1/models` で、そのアカウントが使えるモデル（`slug`・`display_name`）を取れる [6]。

**不明**（文書に書かれていない）: 構造化出力（`text.format` の JSON Schema）が使えるか（使えない項目の一覧には無いが、使えるとも書かれていない）。`response.completed` に使用量（トークン数）が入るか。

## 4. 上限エラー

文書にあること [8][6]:

- 構造化されたエラー: `error.code`・`error.param` を使う。**`subscription_sharing_usage_limit_exceeded` は HTTP 429**。新しい要求を止め、ChatGPT の Settings > Usage へ誘導する。**プラン全体が空とは限らず**（アプリごとの上限もありうる）、**このコードだけから再開時刻を推測しない**。
- `subscription_sharing_usage_unavailable`（503）: 使用量を確かめられなかった。保管した認証情報はそのままに、上限つきのバックオフで再試行。
- ほかのコード: `subscription_sharing_user_not_eligible`（403・同じ要求を繰り返さない）・`…_unsupported_capability`（400・`error.param`の入力を除く）・`…_route_not_supported`（403）・`…_invalid_user`（401）・`…_user_unavailable`（503）・`chatpass_v2_*`（403）。
- ストリームが**始まった後**でも、`response.failed` のイベントとして上限エラーが来うる [6]。始まる**前**の入口の拒否は、標準の `error` オブジェクトではなく `{"detail":"…"}` の形がありうる（401・403・503。文面は診断用で、機械可読のコードとして使わない）[8]。
- 上限エラーのとき、OpenAI は**別の課金経路に切り替えない** [8]。ヘルプセンターも「アプリがプランやクレジットを使えないとき、ほかの有料の手段に自動では切り替わらない」と書く [4]。

**不明**: 429 の `Retry-After` や再開時刻のフィールドがあるか（文書に記述なし）。`error` オブジェクトの全項目。アプリの上限とプラン全体の上限をコードで見分けられるか（上の文書は「見分けられない」前提の書き方）。

## 5. Codex CLI（#1081）との比べ方

「文書」は上の URL に書いてあること、「推測」は判断。

| 観点 | 直接 Responses（SIWC） | Codex CLI（`codex exec`。今の経路） |
|------|------------------------|------------------------------------|
| 起動の重さ | 子プロセスなしで HTTP だけ（推測。計測していない） | 呼び出しごとに CLI のプロセスを起動する（1 回の上限は 150 秒の仮置き。`integrations-codex-chatgpt.md` §2.4） |
| 上限エラーの取りやすさ | **コード（429・`…usage_limit_exceeded`）で取れる**（文書 [8]） | `turn.failed` の文面しか無く、機械可読のコードは無い（`integrations-codex-chatgpt.md` §1。文面一致で判定） |
| 再開時刻 | 文書に無い（**不明**）。コードから推測しない [8] | 文面から読めたときだけ（読めなければ JST のその日いっぱい） |
| 使い過ぎの制御 | アプリごとの週あたりの上限（%）を ChatGPT 側で設定できる。**クレジットの使用は別のオプトイン（既定オフ）**で、アプリの上限が 100% でないと使えない（文書 [4]） | クレジットは残高があれば**自動で使われ**、CLI に禁じる設定は無い（`integrations-codex-chatgpt.md` §1）。アカウント側の残高 0・自動リロードオフに頼る（`setup/ai-providers.md` E） |
| 認証・秘密 | pitha が OAuth・リフレッシュ（30 日・直列化）・トークンの保管（`0600`、ローカルのみ）を**自分で持つ**（文書 [5][10]）。今の方針（pitha はトークンを持たない）と逆 | 人が専用`CODEX_HOME`にログインし、pitha は`auth.json`を開かない |
| 使える条件 | OSS／手元の個人プロジェクトは対象と書いてある [2]。**AGPL・公開の条件は不明**。ユーザー側は OSS の説明では Plus・Pro [1]。規約（バックグラウンドの明示の同意・DevKit の非商用ライセンス）の確認が要る [10][11] | ChatGPT の個人向け利用規約の範囲。自動実行は信頼できる環境・直列の条件つき（`integrations-codex-chatgpt.md` §1。最終判断は悠太さん） |
| 呼び出しの自由度 | `temperature`・`max_output_tokens` などが**使えない**。ツールは関数／カスタムのみ [7]。構造化出力は**不明** | `--output-schema` が使える（ログインして確かめるまで未確認） |
| 成熟度 | 文書は「Preview の制限」を明記 [7] | 公式の CLI（`integrations-codex-chatgpt.md` §1 の調べた範囲） |
| モデルの退役への強さ | `GET /v1/models` でアカウントごとの一覧が取れる [6]（推測: slug の入れ替えを自動化しやすい） | slug は人が Settings で替える（`setup/ai-providers.md` F） |
| 実装の量 | 登録・サインイン画面・トークン保管・SSE の解釈・エラー対応を新設（推測） | すでに実装済み（#1081） |

要点: 直接経路の利点は**エラーが構造化されること**と**アカウント側で枠とクレジットをアプリ単位で制御できること**。欠点は**pitha がトークンを持つ**こと・**対象と規約に不明が残る**こと・**呼び出しの自由度が下がる**こと・Preview であること。

## 6. 残る不明点（ログインしないと分からないこと、または公開情報に無いこと）

人（悠太さん）が許可したうえで、ログインして初めて確かめられる。いずれも**今回は確かめていない**。

1. AGPL・公開リポジトリの pitha が、動的登録（`dynamic_agent_client`）で実際に通るか（登録時に拒否・審査があるか）。
2. 構造化出力（`text.format`）が使えるか。`response.completed` に使用量が入るか。トレーダーの質問 6 問を同じ厳しさで検証できるか。
3. 上限エラーの実際の全文（`error` の全項目・`Retry-After`・再開時刻の有無）と、プランの上限とアプリの上限の見分け方。
4. 実際に使えるモデルの一覧と、1 回あたりの枠の減り方。Plus の 5 時間の上限は**すべてのアプリで共有**（Pro は適用外）[12]なので、人の Codex 利用とどう競合するか。
5. 規約の「バックグラウンドは明示の同意」を、pitha のライブの判断ごとの呼び出しにどう満たすか。DevKit を使わず仕様から書くことの可否（ライセンス）。
6. `agent_name_hint`（アプリの名前）・ホスト ID の運用（再インストール・複数台）の実際の挙動。

## 7. 参照した公開 URL

すべて 2026-11-18 に読んだ公開ページ（ログイン不要）。

1. https://developers.openai.com/siwc/quickstart
2. https://developers.openai.com/cookbook/articles/sign-in-with-chatgpt
3. https://developers.openai.com/siwc/token-sharing-open-source
4. https://help.openai.com/en/articles/20001542-using-your-chatgpt-plan-in-other-apps-and-sites
5. https://developers.openai.com/siwc/token-sharing-open-source/sign-in
6. https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
7. https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations
8. https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery
9. https://learn.chatgpt.com/docs/sign-in-with-chatgpt
10. https://openai.com/policies/sign-in-with-chatgpt-terms
11. https://github.com/openai/sign-in-with-chatgpt-devkit（README の Licence 節と LICENSE: Sign-in with ChatGPT DevKit Noncommercial License v1.0）
12. https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions
13. https://developers.openai.com/siwc/token-sharing-open-source/token-reference
14. https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms
15. https://developers.openai.com/siwc/request-client-id

比較の前提として参照（`integrations-codex-chatgpt.md` と #1110 で使った）: https://learn.chatgpt.com/docs/auth ・ https://help.openai.com/en/articles/12642688-using-credits-for-flexible-usage-in-chatgpt-personal-plans

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-18 | 新規作成。公開情報だけの調査（OSS の対象・OAuth の登録・`store:false`/`stream:true`・上限エラー・Codex CLI との比較・残る不明点） | issue #1111 |
