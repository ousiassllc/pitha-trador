package settings

import "github.com/ousiassllc/pitha-trador/internal/config"

const brokerRestartNote = "保存後の反映にはアプリの再起動が必要です（起動時に一度だけ読み込みます）。"

// brokerGroup selects the broker adapter (issue #733, FR-SETTINGS-7).
var brokerGroup = opsGroup{
	id:          "broker",
	name:        "ブローカー",
	description: "市況データ・（将来の）発注の接続先ブローカーです。kabuステーション（既定）と立花証券 e支店API から選びます。自動フェイルオーバーはなく、切替は手動で再起動が必要です。",
	note:        brokerRestartNote,
	fields: []opsField{{
		key:     config.KeyBrokerProvider,
		label:   "使用するブローカー",
		hint:    "kabu: kabuステーション API / tachibana: 立花証券 e支店API。未設定なら kabu です。",
		options: []string{config.BrokerKabu, config.BrokerTachibana},
	}},
}

// tachibanaGroup holds the 立花 operational settings. 認証ID / 第二暗証番号 are
// secrets (the 立花証券 e支店 connection); the 秘密鍵 itself stays a file.
var tachibanaGroup = opsGroup{
	id:          "tachibana",
	name:        "立花証券 e支店（接続設定）",
	description: "立花証券 e支店API を使う場合の接続環境・接続先・秘密鍵ファイルの設定です。認証ID は「接続先」の「立花証券 e支店」で入力します。",
	note:        brokerRestartNote + "秘密鍵の中身は DB に保存せず、ファイルのパスだけを保存します（ファイルは OS のユーザー権限で保護してください）。",
	fields: []opsField{
		{
			key:     config.KeyTachibanaEnvironment,
			label:   "接続環境",
			hint:    "demo: デモ環境 / production: 本番環境。未設定ならデモです。本番でも現時点では発注は行いません（#55 まで）。",
			options: []string{config.TachibanaEnvDemo, config.TachibanaEnvProduction},
		},
		{
			key:         config.KeyTachibanaDemoBaseURL,
			label:       "デモ環境の接続先 URL",
			hint:        "https のみ。版数の接頭辞（e_api_v4r10 など）が変わったときに差し替えます。末尾に / を補います。",
			placeholder: config.DefaultTachibanaDemoBaseURL,
		},
		{
			key:         config.KeyTachibanaProdBaseURL,
			label:       "本番環境の接続先 URL",
			hint:        "https のみ。版数の接頭辞が変わったときに差し替えます。末尾に / を補います。",
			placeholder: config.DefaultTachibanaProdBaseURL,
		},
		{
			key:         config.KeyTachibanaDemoPrivateKeyPath,
			label:       "デモ環境の秘密鍵ファイル",
			hint:        privateKeyHint("デモ"),
			placeholder: "例: /home/user/.pitha/tachibana-demo.pem",
		},
		{
			key:         config.KeyTachibanaProdPrivateKeyPath,
			label:       "本番環境の秘密鍵ファイル",
			hint:        privateKeyHint("本番"),
			placeholder: "例: /home/user/.pitha/tachibana-prod.pem",
		},
		{
			key:   config.KeyTachibanaRequestMaxPerSecond,
			label: "1 秒あたりの最大リクエスト数",
			hint:  "1〜10 の整数です。既定は暫定値で、実機計測（#725）の結果で見直します。",
		},
		{
			key:         config.KeyTachibanaReauthTime,
			label:       "毎日の再認証時刻（JST）",
			hint:        "HH:MM 形式で 05:30〜08:00 の範囲です。立花は毎朝 05:30 頃にセッションを締めるため、それ以降に再ログインします。",
			placeholder: config.DefaultTachibanaReauthTime,
		},
	},
}

func privateKeyHint(env string) string {
	return env + "環境用の RSA 2048/4096 ビットの秘密鍵（PEM）を絶対パスで入力してください。保存時にファイルを検証します。保存済みのパスは表示しません（「設定済み」のみ）。" +
		"公開鍵だけを立花証券に登録し、秘密鍵は OneDrive / Dropbox / Google ドライブ等の同期フォルダに置かないでください。"
}
