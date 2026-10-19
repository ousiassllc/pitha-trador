// Package adminhub is the registry behind the admin hub page (`GET /admin`,
// issue #793): every admin-type screen or function, its one-line purpose and
// the category it is listed under. It is plain data so tests can range over
// it and prove the hub reaches every Settings dialog and every main-nav page.
package adminhub

// Category groups hub entries. The slice returned by Categories is the
// display order of the hub's sections.
type Category struct {
	ID      string
	Name    string
	Summary string
}

// Entry is one hub row. Href is a page URL, or `/settings#<dialog id>` for a
// Settings dialog (pitha-modal opens the dialog a URL hash names). A function
// without a URL of its own (Kill Switch) links to the page it lives on and
// says so in Description.
type Entry struct {
	Name        string
	Description string
	Href        string
	Category    string // Category.ID
	// SwaggerOnly marks the entry shown only while `/swagger` is registered
	// (SWAGGER_ENABLED=true).
	SwaggerOnly bool
}

// Category ids.
const (
	CategoryOperations   = "operations"
	CategoryResults      = "results"
	CategoryRisk         = "risk"
	CategoryBroker       = "broker"
	CategoryIntegrations = "integrations"
	CategoryMaintenance  = "maintenance"
)

var categories = []Category{
	{CategoryOperations, "運用", "スキャン・ポジション・Kill Switch"},
	{CategoryResults, "成績・検証", "Performance・Calibration・バックテスト"},
	{CategoryRisk, "リスク・資金設定", "損失上限・売買コストの想定"},
	{CategoryBroker, "ブローカー・データ接続", "kabu・立花"},
	{CategoryIntegrations, "外部連携", "Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP"},
	{CategoryMaintenance, "ログ・エクスポート", "アップデート・バックアップ・ログ設定を含む保守"},
}

// Categories returns the hub's categories in display order.
func Categories() []Category { return append([]Category(nil), categories...) }

// Entries returns every hub entry in display order, including the
// SwaggerOnly ones; use Visible to apply the `/swagger` switch.
func Entries() []Entry { return append([]Entry(nil), entries...) }

// Section is one category with the entries listed under it.
type Section struct {
	Category Category
	Entries  []Entry
}

// Visible returns the hub's sections in category order. SwaggerOnly entries
// are dropped unless swagger is true; a category left without entries is
// omitted (none is empty today).
func Visible(swagger bool) []Section {
	var sections []Section
	for _, category := range categories {
		section := Section{Category: category}
		for _, entry := range entries {
			if entry.Category == category.ID && (swagger || !entry.SwaggerOnly) {
				section.Entries = append(section.Entries, entry)
			}
		}
		if len(section.Entries) > 0 {
			sections = append(sections, section)
		}
	}
	return sections
}

const killSwitchDescription = "全ページのヘッダー右上にあるパネルで、新規エントリーの一時停止・再開と全決済（Kill Switch）を操作します。専用の URL はないため、ヘッダーのあるページ（Scanner）を開きます。"

// entries is the hub in display order; the Settings entries are grouped by
// what the operator wants to do, not by the order of the Settings page.
var entries = []Entry{
	// 運用
	{"Scanner", "スキャンの絞り込み状況と候補銘柄・Jev の判断を確認します（候補から銘柄詳細へ進めます）。", "/scanner", CategoryOperations, false},
	{"Watchlist", "立花の監視リスト（どの銘柄をどう選んだか）を確認します。", "/watchlist", CategoryOperations, false},
	{"Activity", "システムの稼働ログとキューの状況を確認します。", "/activity", CategoryOperations, false},
	{"Settings", "接続先・運用設定・システム（アップデート・ログ）を一つの画面にまとめた設定画面です。個々の項目は下の各カテゴリから直接開けます。", "/settings", CategoryOperations, false},
	{"Kill Switch", killSwitchDescription, "/scanner#kill-switch", CategoryOperations, false},
	{"Fast Screener", "候補を絞り込むしきい値・上位件数・スコア重みを編集します。", "/settings#modal-screener", CategoryOperations, false},
	{"Policy Engine（ロング）", "ロングのシグナルになるための確率・エントリー品質などのしきい値を編集します。", "/settings#modal-policy-long", CategoryOperations, false},
	{"Policy Engine（ショート）", "ショートのシグナルになるための確率・エントリー品質などのしきい値を編集します。", "/settings#modal-policy-short", CategoryOperations, false},
	{"データの鮮度（ランキング監視）", "Jev の判断と Paper エントリーが最新の足をどこまで古くてよしとするかの上限（秒）を編集します。", "/settings#modal-snapshot-age", CategoryOperations, false},

	// 成績・検証
	{"Performance", "決済済みポジションの実績（Paper）と評価指標を確認します。", "/performance", CategoryResults, false},
	{"バックテスト（Walk Forward）", "期間を指定して Walk Forward バックテストを実行し、コスト感度を確認します（Performance 画面の下部）。", "/performance#backtest", CategoryResults, false},
	{"Calibration", "Jev の確率予測が実際の結果とどれだけ合っているかをヒートマップで確認します。", "/calibration", CategoryResults, false},
	{"キャリブレーションのラベル（トリプルバリア）", "判断に正解・不正解のラベルを付ける利確・損切り幅と時間切れを編集します。", "/settings#modal-triple_barrier", CategoryResults, false},
	{"メタラベリング", "入る／見送るを決める二次モデルの設定と最低 EV を編集します。", "/settings#modal-metalabel", CategoryResults, false},
	{"相場状態（レジーム）判定", "相場状態（トレンド／平均回帰）の判定と、VWAP 回帰モードへの切替を編集します。", "/settings#modal-regime", CategoryResults, false},
	{"板の需給（OFI）", "板の需給（Order Flow Imbalance）を集計する窓の幅を編集します。", "/settings#modal-orderflow", CategoryResults, false},

	// リスク・資金設定
	{"1日の損失上限・利益確保", "当日の損益・取引回数が上限に達したら新規エントリーを止める条件を編集します。", "/settings#modal-daily-guard", CategoryRisk, false},
	{"売買コストの想定", "手数料・スリッページ・貸株料の想定を編集します（Paper・バックテスト・ラベルが共通で参照します）。", "/settings#modal-trading_cost", CategoryRisk, false},

	// ブローカー・データ接続
	{"初回セットアップ", "起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。", "/setup", CategoryBroker, false},
	{"ブローカー", "市況データと発注の接続先を kabuステーション／立花証券 e支店から選びます。", "/settings#modal-broker", CategoryBroker, false},
	{"kabuステーション", "kabuステーション API（時価・発注）の API パスワードを設定します（必須）。", "/settings#modal-kabu", CategoryBroker, false},
	{"立花証券 e支店（認証情報）", "立花証券 e支店 API の認証ID（デモ・本番）などを設定します。", "/settings#modal-tachibana", CategoryBroker, false},
	{"立花証券 e支店（接続設定）", "立花証券 e支店 API の接続環境・接続先・秘密鍵ファイルを設定します。", "/settings#modal-tachibana-conn", CategoryBroker, false},
	{"立花 監視銘柄ソース", "翌営業日の監視銘柄の候補ソースと銘柄リストを編集します。", "/settings#modal-tachibana-source", CategoryBroker, false},
	{"立花 夜間日足バッチ", "引け後に全銘柄の日足を取得する夜間バッチの設定を編集します。", "/settings#modal-tachibana-nightly", CategoryBroker, false},
	{"立花 日中の EVENT 接続・REST 時価補助", "日中の EVENT 接続と REST 時価補助の設定を編集します。", "/settings#modal-tachibana-intraday", CategoryBroker, false},
	{"立花 スクリーニング", "日足から監視銘柄を選ぶ指標・重み・上位件数を編集します。", "/settings#modal-tachibana-screen", CategoryBroker, false},

	// 外部連携
	{"Jev", "売買判断を行う AI（Jev）の API キー・接続先・モデルを設定します。", "/settings#modal-jev", CategoryIntegrations, false},
	{"Luna", "ニュース分類の AI（Luna）の接続を設定します（既定は Jev）。", "/settings#modal-luna", CategoryIntegrations, false},
	{"Sol", "しきい値の変更案を作る AI（Sol）の接続を設定します（既定は Jev）。", "/settings#modal-sol", CategoryIntegrations, false},
	{"Opus", "Sol の提案を検証する AI（Opus）の接続を設定します（既定は Jev）。", "/settings#modal-opus", CategoryIntegrations, false},
	{"ニュースフィード", "ニュースの取得元（既定はやのしん TDnet WebAPI）を設定します。", "/settings#modal-news-feed", CategoryIntegrations, false},
	{"Slack", "通知の送信先となる Slack の Incoming Webhook URL を設定します。", "/settings#modal-slack", CategoryIntegrations, false},
	{"MCP", "Paper 成績を読み取り専用で外部の AI クライアントへ公開する MCP サーバーの有効化・API キーを設定します。", "/settings#modal-mcp", CategoryIntegrations, false},
	{"API ドキュメント（Swagger）", "JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。", "/swagger", CategoryIntegrations, true},

	// ログ・エクスポート
	{"アップデート", "現在のバージョンと最終確認結果を確認し、新しいバージョンを確認します。", "/settings#modal-update", CategoryMaintenance, false},
	{"エラーログ", "調査・共有のためにエラーログをダウンロードします。", "/settings#modal-error-log", CategoryMaintenance, false},
	{"データエクスポート", "取引ログ・判断ログ・計測ログを期間を指定して ZIP でダウンロードします。", "/settings#modal-data-export", CategoryMaintenance, false},
	{"バックアップ先", "データベースの日次バックアップ・週次アーカイブの保存先ディレクトリを設定します。", "/settings#modal-backup", CategoryMaintenance, false},
	{"ログディレクトリ", "日次 JSON ログとエラーログの保存先ディレクトリを設定します。", "/settings#modal-logdir", CategoryMaintenance, false},
	{"ログのレベルと保持期間", "ログの最低レベルと、圧縮せずに置いておく日数を設定します。", "/settings#modal-logsettings", CategoryMaintenance, false},
}
