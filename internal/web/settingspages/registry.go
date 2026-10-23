// Package settingspages is the registry behind the Settings area (issue
// #805, which folded the former admin hub of issue #793 into Settings; the
// six pages are issue #895): the pages `GET /settings` lists, which Settings
// dialogs live on which page, and the links to the few screens outside the
// main menu (稼働ログ, 初回セットアップ, Swagger) that belong to a page's topic.
// It is plain data so tests can range over it and prove that every dialog the
// Settings screens render is reachable from the Settings menu entry.
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

// Page ids. The ids are the URLs of the pre-#895 pages: the six pages kept
// their path (`/settings/operations` is now 取引ルール, `/settings/results`
// 予測モデル, `/settings/maintenance` システム) so old bookmarks open.
const (
	PageOperations   = "operations"
	PageResults      = "results"
	PageRisk         = "risk"
	PageBroker       = "broker"
	PageIntegrations = "integrations"
	PageMaintenance  = "maintenance"
)

// LegacyBucketsPath is the former 資産ごとの予算枠 page (issue #824), folded
// into リスク・資金 by issue #895; the router answers it with a 301 to
// PageRisk.
const LegacyBucketsPath = BasePath + "/asset-buckets"

var pages = []Page{
	{PageOperations, "取引ルール", "スキャン・ポジション、候補を絞るしきい値、売買判断の基準"},
	{PageRisk, "リスク・資金", "Paper の運用資金・建玉上限・資産ごとの予算枠、損失上限・利益確保・売買コストの想定"},
	{PageResults, "予測モデル", "判断のラベル・メタラベリング・相場状態・板情報の設定"},
	{PageBroker, "証券会社・データ", "kabuステーション・立花証券 e支店の接続と監視銘柄"},
	{PageIntegrations, "外部連携・AI", "Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP（AIの注文提案の受け付け）"},
	{PageMaintenance, "システム", "稼働ログ・アップデート・エラーログ・データエクスポート・バックアップ・ログ設定・スリープ防止"},
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

// HubLinks returns the link rows the hub lists under its pages, so the screens
// outside the main menu (稼働ログ, 初回セットアップ) open within two clicks of
// the menu's 設定 entry. SwaggerOnly rows are left out: the hub does not know
// the `/swagger` switch, the 外部連携・AI page lists that one.
func HubLinks() []Link {
	var out []Link
	for _, l := range links {
		if !l.SwaggerOnly {
			out = append(out, l)
		}
	}
	return out
}

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

// dialogs is every card/dialog the Settings screens render, by page. Ids are
// the dialogs' (`modal-` + the connection / settings-group id).
var dialogs = []Dialog{
	// 取引ルール
	{PageOperations, "modal-screener", nil},
	{PageOperations, "modal-policy-long", nil},
	{PageOperations, "modal-policy-short", nil},
	{PageOperations, "modal-snapshot-age", nil},
	{PageOperations, "modal-paper-bootstrap", nil},

	// リスク・資金 (資産ごとの予算枠 was its own page until issue #895)
	{PageRisk, "modal-paper-risk", nil},
	{PageRisk, "modal-asset-buckets", nil},
	{PageRisk, "modal-daily-guard", nil},
	{PageRisk, "modal-trading_cost", nil},

	// 予測モデル
	{PageResults, "modal-triple_barrier", nil},
	{PageResults, "modal-metalabel", nil},
	{PageResults, "modal-regime", nil},
	{PageResults, "modal-orderflow", nil},

	// 証券会社・データ
	{PageBroker, "modal-broker", nil},
	{PageBroker, "modal-kabu", nil},
	{PageBroker, "modal-tachibana", nil},
	{PageBroker, "modal-tachibana-conn", nil},
	{PageBroker, "modal-tachibana-source", nil},
	{PageBroker, "modal-tachibana-nightly", nil},
	{PageBroker, "modal-tachibana-intraday", nil},
	{PageBroker, "modal-tachibana-screen", nil},

	// 外部連携・AI
	{PageIntegrations, "modal-jev", nil},
	{PageIntegrations, "modal-luna", nil},
	{PageIntegrations, "modal-sol", nil},
	{PageIntegrations, "modal-opus", nil},
	{PageIntegrations, "modal-news-feed", nil},
	{PageIntegrations, "modal-slack", nil},
	{PageIntegrations, "modal-mcp", nil},
	{PageIntegrations, "modal-jev-prompt", nil},

	// システム. The header's version link used to land on
	// `/settings#update-panel`.
	{PageMaintenance, "modal-sleep-guard", nil},
	{PageMaintenance, "modal-update", []string{"update-panel"}},
	{PageMaintenance, "modal-error-log", nil},
	{PageMaintenance, "modal-data-export", nil},
	{PageMaintenance, "modal-backup", nil},
	{PageMaintenance, "modal-logdir", nil},
	{PageMaintenance, "modal-logsettings", nil},
}

// links is every link row to a screen outside Settings, by page. The main
// menu's own screens (候補銘柄・AIの注文提案・成績, and the Kill Switch in the
// header) are not repeated here (issue #895); only screens the main menu does
// not reach are listed.
var links = []Link{
	// 証券会社・データ
	{PageBroker, "初回セットアップ", "起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。", "/setup", false},

	// 外部連携・AI
	{PageIntegrations, "API ドキュメント（Swagger）", "JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。", "/swagger", true},

	// システム
	{PageMaintenance, "稼働ログ", "システムの稼働ログとキューの状況を確認します。", "/activity", false},
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
