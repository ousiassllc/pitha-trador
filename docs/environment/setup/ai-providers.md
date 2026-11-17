# 環境構築: AI のプロバイダーと役割ごとのモデルの設定（issue #1080）

Settings「外部連携・AI」ページ（`/settings/integrations`）で、AI を呼ぶ先（プロバイダー）と、役割ごとのモデルを設定する手順。設計は`docs/architecture/overview/integrations-ai-providers.md`。

## 画面の並び

1. **AI のプロバイダー**: 接続を持つ 2 つのカード。
   - 独自 API（Jev）: `JEV_API_KEY`（必須）・`JEV_BASE_URL`（任意）・`JEV_MODEL`（任意。「役割ごとのモデル」で Jev にモデルの指定がなければこれを使う）。
   - OpenRouter: `OPENROUTER_API_KEY`・`OPENROUTER_BASE_URL`（どちらも任意。URL が空なら公開エンドポイント）。
2. **役割ごとのモデル**: Jev（売買判断）・Sol（改善提案）・Opus（提案の検証）・Luna（ニュース分類）の各行で、プロバイダーとモデルを選んで保存する。「既定に戻す」で保存した値を消す。
3. **そのほかの接続先**: Luna・Sol・Opus の役割ごとの上書き（独自 API を選んだ役割が使う）・ニュースフィード・Slack・（MCP などのカード）。

## 既定と反映

- 何も保存しなければ、全ての役割が今までと同じ接続先・キー・モデルで動く。環境変数ではなく`secrets`の既存のキー（`JEV_*`・`SOL_*`・`OPUS_*`・`LUNA_*`・`OPENROUTER_*`）をそのまま読み、書き換えない。
- OpenRouter を選ぶ役割にはモデル名（例: `provider/model`）が必要。OpenRouter のキーが無いと、その役割は動かず見送られる（独自 API のキーには切り替わらない）。
- 反映のタイミング: **Sol・Opus・Luna は保存（既定に戻す）の後、次の呼び出しから反映され、再起動は要らない**。壊れた保存値は別のプロバイダーへ落とさず、その役割は動かず見送られる。**Jev（売買判断）の接続だけは再起動が必要**で、再起動まで起動時の組のまま。画面に、保存した設定と、最後の起動で実際に使い始めた Jev の組、最近の変更の履歴が出る。
- Jev（売買判断）のプロバイダー・モデルは**この画面でだけ**変えられる（MCP からは変えられない）。トライアルのアームのプロバイダー（`openrouter`／`custom`）は、モデル比較トライアルのアーム（JSON）に書く。
- 画面にキー・URL の値は出ない（設定済み／未設定の表示だけ）。

## ChatGPT のサブスク（Codex CLI のログイン。トライアルのアームだけ。issue #1081）

モデル比較トライアルのアームの`provider`に`codex_chatgpt`を指定すると、公式の Codex CLI（`codex exec`）で ChatGPT のサブスクの枠から答えを受け取る。**既定はオフ**で、AI の役割（Jev・Sol・Opus・Luna）には選べない。設計・調べた結果は`architecture/overview/integrations-codex-chatgpt.md`。

1. **ログインは人が自分でする**（pitha もエージェントもログインしない）。お試し専用の空のディレクトリを作り、そこを`CODEX_HOME`にして、公式の手順（ヘッドレスならデバイスコード）で`codex login`する。人の普段の Codex の設定（`~/.codex`）とは分ける。`auth.json`はパスワードと同じに扱い、コミット・チケット・チャットに貼らず、pitha にも読ませない。
2. pitha を起動する環境に環境変数`PITHA_CODEX_HOME=<そのディレクトリ>`（お試し専用の`CODEX_HOME`。必要なら`PITHA_CODEX_BIN`＝Codex CLI の実行ファイル。未設定は PATH 上の`codex`。どちらも秘密ではない。`internal/bootstrap/airolewire`が読む）を設定する。`PITHA_CODEX_HOME`が未設定なら Codex のアームは呼ばれない。`CODEX_API_KEY`・`OPENAI_API_KEY`は子プロセスに渡さない。
3. ChatGPT のアカウント側で、**クレジットの残高を 0 のまま・自動リロードをオフ**にしておく（枠を使い切った後にクレジットが自動で使われるのを、CLI 側では止められない）。API キー（`CODEX_API_KEY`・`OPENAI_API_KEY`）は使わない（子プロセスには渡さない）。
4. Settings「モデル比較トライアル」で「ChatGPT サブスク（Codex）を有効にする」をオンにし、1 日の呼び出し回数・トークン数の上限を決め、アーム（例 `{"id":"cx-sol","provider":"codex_chatgpt","model":"<使えるモデルの slug>","prompt_version":"trader-v3"}`）を保存する。スイッチがオフの間はアームを保存できない。
5. 画面（と MCP の`jev_trial_cost`）にログイン状態・今日の回数とトークン・止まっている理由と再開時刻が出る。上限に届いたとき・失敗したときは、再試行せずにプロバイダー全体が止まる（再開時刻が分かればその時刻まで、分からなければその日いっぱい）。

**利用条件の最終判断は悠太さん**（ChatGPT の個人向け利用規約・Usage Policies。Codex の出力は Paper のお試し専用で、ライブの発注には使わない）。使えるモデル・`--output-schema`との組み合わせ・1 回あたりの使用量は、ログインして確かめるまで**未確認**。

### ChatGPT のサブスク: 運用手順の詳細（issue #1110）

上の 1〜5 の手順を、迷わず実行するための確認手順。新しい設定や呼び出しは足していない（pitha の動きは上と同じ）。根拠の実装と公式文書の URL は`architecture/overview/integrations-codex-chatgpt.md` §5。「未確認」「推測」は、公式の文書か実装で確かめられていない点。

**A. 専用の`CODEX_HOME`にデバイスコードでログインする**（上の 1。人が自分の端末でする）

1. お試し専用の**空のディレクトリ**を作る（例: `mkdir -m 700 <専用ディレクトリ>`）。人の普段の`~/.codex`・リポジトリの中とは別の場所にする。`CODEX_HOME`に指すディレクトリは先に存在している必要がある（公式）。
2. ChatGPT のセキュリティ設定（個人のアカウント）で、デバイスコードでのログインを有効にする（ワークスペースのアカウントは管理者の権限設定。公式ではベータ）。
3. `CODEX_HOME=<専用ディレクトリ> codex login --device-auth`を実行し、表示されたリンクをブラウザで開いてサインインし、ワンタイムコードを入れる。ブラウザが使える端末なら`CODEX_HOME=<専用ディレクトリ> codex login`でもよい。**API キーでのログイン（`--with-api-key`）はしない**（従量課金になる）。
4. `CODEX_HOME=<専用ディレクトリ> codex login status`で、ChatGPT でのログインと出ることを確かめる（pitha も同じコマンドで、出力に`chatgpt`を含み`api key`を含まないときだけ呼ぶ。出力は保存しない）。
5. 専用ディレクトリに`config.toml`は置かない（pitha は`--ignore-user-config`で動かすので読まれない。後述 D のとおり、保存先を変える設定を置くと、ログインした場所と pitha が見る場所がずれうる）。

**B. `auth.json`の扱い**（上の 1 の再掲。実装は変えない）: `<専用ディレクトリ>/auth.json`はアクセストークンを含むので、パスワードと同じに扱う。コミット・チケット・チャット・ログに貼らない（`cat`で画面に出さない。確認は`ls -l`の存在と権限だけ）。pitha は`auth.json`を**開かず**、トークンも持たない。貼った・漏れたかもしれないと思ったときは、`CODEX_HOME=<専用ディレクトリ> codex logout`で消して、ログインし直す。

**C. 子プロセスに渡る環境変数と、渡さないものの確認**（上の 2。`internal/service/jev/codexwire`の`allowedEnv`が実際の許可リスト）

| 渡るもの | 備考 |
|----------|------|
| `PATH`・`HOME`・`LANG`・`LC_ALL`・`LC_CTYPE`・`TZ`・`TMPDIR` | pitha の環境に設定があるものだけ |
| Windows のみ: `SYSTEMROOT`・`SYSTEMDRIVE`・`USERPROFILE`・`TEMP`・`TMP`・`APPDATA`・`LOCALAPPDATA` | CLI の起動に要る。設定があるものだけ |
| `CODEX_HOME` | **常に`PITHA_CODEX_HOME`の値**（pitha の環境に別の`CODEX_HOME`があっても上書き） |

これ以外は、`CODEX_API_KEY`・`OPENAI_API_KEY`・`CODEX_ACCESS_TOKEN`・`OPENROUTER_API_KEY`・`JEV_API_KEY`などを含め、**pitha の環境にあっても渡さない**。`PITHA_CODEX_BIN`・`PITHA_CODEX_HOME`は pitha が読むだけで、子プロセスには`CODEX_HOME`としてだけ渡る。確認のしかた:

- テストで固定している: `go test ./internal/service/jev/codexwire -run TestTrader_ChildEnvironmentIsAnAllowListWithTheDedicatedHome`（モックの CLI。ログイン・キーは使わない）。
- 動いている端末で確かめる（Linux。**値は出さず名前だけ**）: Codex のアームが動いている間に`tr '\0' '\n' < /proc/<codex の PID>/environ | cut -d= -f1`。子プロセスは 1 回の呼び出しの間（最大 150 秒）しか居ないので、PID は`pgrep -f 'codex exec'`で探す。上の表にない名前が出たら、許可リストが崩れている（バグとして報告する）。macOS・Windows の手順は確認していない（`ps eww`などは値まで出るので使わない）。
- そもそも pitha を起動するシェルに、API キーを置かない。

**D. 認証がキーリング保存になっていないことの確認**（pitha は保存先を確かめず、強制もしない）

Codex CLI の設定`cli_auth_credentials_store`は`file`（`CODEX_HOME`の`auth.json`）・`keyring`（OS の資格情報ストア。使えなければ失敗）・`auto`（使えれば OS のストア、無ければファイル）・`ephemeral`（そのプロセスのメモリだけ）。公式文書には既定値の記述が見当たらない（**未確認**）。公開ソース（openai/codex）では既定は`file`。

1. `ls -l <専用ディレクトリ>/auth.json`で、ファイルがあり、権限が自分だけであることを確かめる（保存先がファイルである証拠。中身は開かない）。
2. 専用ディレクトリに`config.toml`が無い（あっても`cli_auth_credentials_store`が無い、または`file`）ことを確かめる。`keyring`・`auto`・`ephemeral`にしない。`ephemeral`ではプロセスごとにログインが消え、`keyring`だと pitha の呼び出し（`--ignore-user-config`）が見る保存先と合わなくなりうる（**推測**。ログインしての確認は未実施）。
3. OS のキーチェーンのツールで、Codex の項目が増えていないことを見る。公開ソースの項目名は`Codex Auth`（公式文書には無い。**未検証**）。人が`~/.codex`で別にログインしているものは別の保存先なので、混同しない。

**E. ChatGPT のアカウント側（上の 3。pitha では強制できない）**

- 公式の説明: プランの枠を使い切ると、残高のあるクレジットが**自動で使われる**。自動リロード（残高が下限を割ると支払い方法に自動課金してクレジットを買い足す機能）が別にある（[クレジット](https://help.openai.com/en/articles/12642688-using-credits-for-flexible-usage-in-chatgpt-personal-plans)）。
- 個人のアカウント（Plus・Pro など）: ChatGPT の**Settings > Usage**（Codex アプリでは**Usage & Billing**）で、クレジットの**残高が 0**であること（買っていない）と、**自動リロードがオフ**であることを見る。自動リロードの切り替えは、使えるアカウントにだけ出る。オフにするボタンの文言・場所の細部は公開文書にないので**未確認**（画面を見て悠太さんが確かめる）。
- Business などのワークスペースのアカウント: 公式の別の記事（[Business のクレジットと支出の管理](https://help.openai.com/en/articles/20001155-managing-credits-and-spend-controls-in-chatgpt-business)）にある。画面の場所は本書では**未確認**。
- pitha が止められるのは自分の 1 日の回数・トークンの上限だけで、**クレジットの消費は止められない**。pitha はアカウント側の残高・自動リロードを読めないので、この確認は人（悠太さん）が画面でする。

**F. モデルが退役してアームが失敗したとき**（上の 4。ライブの役割には使わない）

1. 症状: 退役した slug の呼び出しは失敗として扱われ、プロバイダー全体が止まる（画面の「止まっている理由」が失敗。見送りの理由は`stopped_provider_failed`）。**エラーの文面は保存・表示しない**ので、画面だけでは退役かほかの失敗かは分からない。公式のモデルのページ（[モデル](https://learn.chatgpt.com/docs/models)）の退役の案内で、その slug が使えるかを見る。
2. 直し方: Settings「モデル比較トライアル」の「アーム（JSON）」で、該当アームの`model`を、使えるモデルの slug に書き換えて保存する。pitha は`model`の長さ（1〜100 文字）しか検証せず、**その slug が使えるかは確かめない**。Codex のアームを保存するには、同じフォームの「ChatGPT サブスク（Codex）を有効にする」がオンのままである必要がある。
3. 反映: トライアルのアームの設定はライブの判断ごとに読み直され、`model`が変わると呼び出し側も作り直されるので、**アプリの再起動は要らず、次のライブの判断から**新しい slug で動く（AI の役割の設定の反映とは別）。
4. 停止は残る: 保存しても、すでに入った停止（`jev.trial.codex.halt`）は**解除されない**（解除の操作は用意していない）。画面に出る再開時刻（時刻が分からなければその日いっぱい（JST））まで待つ。再開後にまた失敗するなら、slug が合っていないので 2 に戻る。
5. `codex_chatgpt`はトライアルのアームにだけ書ける。AI の役割（Jev・Sol・Opus・Luna）には選べず、ライブの Jev・Risk・発注・Paper ガードには関わらない。
