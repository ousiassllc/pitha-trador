// Package settingspages is the registry behind the Settings area (issue
// #805, which folded the former admin hub of issue #793 into Settings): the
// pages `GET /settings` lists, which Settings dialogs live on which page, and
// the links to the screens outside Settings (Scanner, Performance, ...) that
// belong to a page's topic. It is plain data so tests can range over it and
// prove that every dialog the Settings screens render and every main-nav page
// is reachable from the Settings menu entry.
//
// Adding a setting: put its card in the owning handler list, then add its
// dialog id to the page it belongs to in dialogs below. A new page is one more
// pages row plus its dialogs/links.
package settingspages

// BasePath is the Settings hub; a page lives at BasePath + "/" + Page.ID.
const BasePath = "/settings"

// Page is one page under Settings: the big item the hub lists and that opens
// a page of detailed settings.
type Page struct {
	ID      string
	Name    string
	Summary string
}

// Path is the page's URL.
func (p Page) Path() string { return BasePath + "/" + p.ID }

// Page ids.
const (
	PageOperations   = "operations"
	PageResults      = "results"
	PageRisk         = "risk"
	PageBroker       = "broker"
	PageIntegrations = "integrations"
	PageMaintenance  = "maintenance"
)

var pages = []Page{
	{PageOperations, "運用", "スキャン・ポジション・Kill Switch、候補を絞るしきい値"},
	{PageResults, "成績・検証", "Performance・Calibration・バックテスト、判断のラベル・相場状態の設定"},
	{PageRisk, "リスク・資金", "Paper の運用資金・建玉上限、損失上限・利益確保・売買コストの想定"},
	{PageBroker, "ブローカー・データ", "kabuステーション・立花証券 e支店の接続と監視銘柄"},
	{PageIntegrations, "外部連携", "Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP"},
	{PageMaintenance, "ログ・エクスポート", "アップデート・エラーログ・データエクスポート・バックアップ・ログ設定"},
}

// Pages returns the Settings pages in display order.
func Pages() []Page { return append([]Page(nil), pages...) }

// PageByID returns the page with the given id.
func PageByID(id string) (Page, bool) {
	for _, p := range pages {
		if p.ID == id {
			return p, true
		}
	}
	return Page{}, false
}

// Dialog places one Settings dialog (`<dialog id=ModalID>`, opened by a
// card's button) on a page. Aliases are other hash targets inside that dialog
// that old links use (`/settings#update-panel`); they resolve to the same
// page.
type Dialog struct {
	Page    string // Page.ID
	ModalID string
	Aliases []string
}

// Targets are the URL-hash ids that land on the dialog.
func (d Dialog) Targets() []string { return append([]string{d.ModalID}, d.Aliases...) }

// Link is a link row on a page to a screen outside Settings (or a function
// without a URL of its own such as the Kill Switch, which links to the page
// it lives on and says so in Description).
type Link struct {
	Page        string // Page.ID
	Name        string
	Description string
	Href        string
	// SwaggerOnly marks the link shown only while `/swagger` is registered
	// (SWAGGER_ENABLED=true).
	SwaggerOnly bool
}

// Dialogs returns every Settings dialog with its page, in display order.
func Dialogs() []Dialog { return append([]Dialog(nil), dialogs...) }

// DialogsOf returns the dialogs of the page in display order.
func DialogsOf(pageID string) []Dialog {
	var out []Dialog
	for _, d := range dialogs {
		if d.Page == pageID {
			out = append(out, d)
		}
	}
	return out
}

// PageOfTarget returns the page holding the dialog a URL hash id names (a
// dialog id or one of its aliases).
func PageOfTarget(id string) (Page, bool) {
	for _, d := range dialogs {
		for _, target := range d.Targets() {
			if target == id {
				return PageByID(d.Page)
			}
		}
	}
	return Page{}, false
}

// AllLinks returns every link row, including the SwaggerOnly ones; use
// LinksOf to apply the `/swagger` switch.
func AllLinks() []Link { return append([]Link(nil), links...) }

// LinksOf returns the page's link rows in display order. SwaggerOnly links
// are dropped unless swagger is true.
func LinksOf(pageID string, swagger bool) []Link {
	var out []Link
	for _, l := range links {
		if l.Page == pageID && (swagger || !l.SwaggerOnly) {
			out = append(out, l)
		}
	}
	return out
}

const killSwitchDescription = "全ページのヘッダー右上にあるパネルで、新規エントリーの一時停止・再開と全決済（Kill Switch）を操作します。専用の URL はないため、ヘッダーのあるページ（Scanner）を開きます。"

// dialogs is every card/dialog the Settings screens render, by page. Ids are
// the dialogs' (`modal-` + the connection / settings-group id).
var dialogs = []Dialog{
	// 運用
	{PageOperations, "modal-screener", nil},
	{PageOperations, "modal-policy-long", nil},
	{PageOperations, "modal-policy-short", nil},
	{PageOperations, "modal-snapshot-age", nil},
	{PageOperations, "modal-paper-bootstrap", nil},

	// 成績・検証
	{PageResults, "modal-triple_barrier", nil},
	{PageResults, "modal-metalabel", nil},
	{PageResults, "modal-regime", nil},
	{PageResults, "modal-orderflow", nil},

	// リスク・資金
	{PageRisk, "modal-paper-risk", nil},
	{PageRisk, "modal-daily-guard", nil},
	{PageRisk, "modal-trading_cost", nil},

	// ブローカー・データ
	{PageBroker, "modal-broker", nil},
	{PageBroker, "modal-kabu", nil},
	{PageBroker, "modal-tachibana", nil},
	{PageBroker, "modal-tachibana-conn", nil},
	{PageBroker, "modal-tachibana-source", nil},
	{PageBroker, "modal-tachibana-nightly", nil},
	{PageBroker, "modal-tachibana-intraday", nil},
	{PageBroker, "modal-tachibana-screen", nil},

	// 外部連携
	{PageIntegrations, "modal-jev", nil},
	{PageIntegrations, "modal-luna", nil},
	{PageIntegrations, "modal-sol", nil},
	{PageIntegrations, "modal-opus", nil},
	{PageIntegrations, "modal-news-feed", nil},
	{PageIntegrations, "modal-slack", nil},
	{PageIntegrations, "modal-mcp", nil},

	// ログ・エクスポート. The header's version link used to land on
	// `/settings#update-panel`.
	{PageMaintenance, "modal-update", []string{"update-panel"}},
	{PageMaintenance, "modal-error-log", nil},
	{PageMaintenance, "modal-data-export", nil},
	{PageMaintenance, "modal-backup", nil},
	{PageMaintenance, "modal-logdir", nil},
	{PageMaintenance, "modal-logsettings", nil},
}

// links is every link row to a screen outside Settings, by page.
var links = []Link{
	// 運用
	{PageOperations, "Scanner", "スキャンの絞り込み状況と候補銘柄・Jev の判断を確認します（候補から銘柄詳細へ進めます）。", "/scanner", false},
	{PageOperations, "Watchlist", "立花の監視リスト（どの銘柄をどう選んだか）を確認します。", "/watchlist", false},
	{PageOperations, "Activity", "システムの稼働ログとキューの状況を確認します。", "/activity", false},
	{PageOperations, "Kill Switch", killSwitchDescription, "/scanner#kill-switch", false},

	// 成績・検証
	{PageResults, "Performance", "決済済みポジションの実績（Paper）と評価指標を確認します。", "/performance", false},
	{PageResults, "バックテスト（Walk Forward）", "期間を指定して Walk Forward バックテストを実行し、コスト感度を確認します（Performance 画面の下部）。", "/performance#backtest", false},
	{PageResults, "Calibration", "Jev の確率予測が実際の結果とどれだけ合っているかをヒートマップで確認します。", "/calibration", false},

	// ブローカー・データ
	{PageBroker, "初回セットアップ", "起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。", "/setup", false},

	// 外部連携
	{PageIntegrations, "API ドキュメント（Swagger）", "JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。", "/swagger", true},
}

// HashRoutes maps every URL-hash id a Settings dialog answers to (dialog ids
// and aliases) to the path of the page holding it. The hub embeds it so a
// link from before the pages existed (`/settings#modal-jev`,
// `/settings#update-panel`) is forwarded to `/settings/<page>#<id>`.
func HashRoutes() map[string]string {
	routes := map[string]string{}
	for _, d := range dialogs {
		page, _ := PageByID(d.Page)
		for _, target := range d.Targets() {
			routes[target] = page.Path()
		}
	}
	return routes
}
