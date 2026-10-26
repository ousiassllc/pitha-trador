# #904 スクリーンショットの撮り方

条件は #903 に合わせる（幅 1366px・ダミー SQLite DB・ダミーの接続状態（取引時間外）・フルページ）。**本番の DB・設定は使っていない**（一時ディレクトリのダミー DB のみ）。

1. 変更前 = origin/main `43a75002`、変更後 = `sweep/issues-904` 先端を、それぞれ自分の worktree（`/mnt/nvme/ousiassllc/pitha-trador-sweep-904`）から `go build -o /tmp/pitha904/server-{before,after} ./cmd/server`（変更前は `git stash`/別チェックアウトではなく、PR #903 マージ直後の `43a75002` を同 worktree の先頭コミット前の状態でビルド）。
2. 空の DB を作る: `XDG_CONFIG_HOME=<tmp> PITHA_SERVER_ADDR=127.0.0.1:<port> ./server-before` を一度起動してマイグレーションを走らせ、止める。
3. `seed.py <db> <enc-bin>` でダミーデータを入れる（必須の秘密は `enc-bin`＝アプリと同じ鍵種で暗号化したダミー値。銘柄・Jev 判断・シグナル・Paper 注文・ポジション・Kill Switch 履歴・注文提案・メタモデル・ジョブ・調整の実験・見送り理由の全コードなど）。
4. `run-app.sh <binary> <port> <workdir>` でダミー DB のコピーを使ってアプリを起動する（変更前 18904・変更後 18905。撮影後に必ず停止）。
5. eval の `browser`（Chromium、viewport 1366×900）で各画面を開いて `page.screenshot({fullPage: true})`。
   - `before/<画面>.png`・`after/<画面>.png`: 全 19 画面（ホーム・候補銘柄・監視リスト・銘柄詳細・注文提案の一覧／詳細（約定・拒否）・成績（実績／バックテスト）・予測の精度・稼働ログ・Setup・設定ハブと 6 ページ）。
   - `help-before/<画面>.png`・`help/<画面>.png`: 「?」にフォーカスして本文を開いた状態（ホーム・銘柄詳細・注文提案の詳細・設定（外部連携・運用）・成績・候補銘柄・予測の精度）。
6. 画面のテキスト（テキストノードと title/aria-label/placeholder/hx-confirm/shadow DOM）も同時にダンプして変更前後を diff し、対照表（`../issue-904-table.md`）の「変更前の表示」を実描画から取った。

変更のない画面（監視リスト・設定ハブ・メンテナンスなど）の画像も同条件で撮ってあるが、PR には変更した画面だけを載せる。
変更前後で時刻・スキャンの最終サイクルなど実行中の値が少し違うのは、アプリを実際に動かして撮っているため（コードの差ではない）。
