package pages

import "github.com/ousiassllc/pitha-trador/internal/web/molecules"

// The tab bars of the two main-menu entries that span several pages (issue
// #895). Each tab is its own URL; the page being shown passes its own tab as
// current.

// Tab ids: candidateTabs / resultTabs mark the tab with the id current.
const (
	tabCandidates  = "candidates"
	tabWatchlist   = "watchlist"
	tabActuals     = "actuals"
	tabBacktest    = "backtest"
	tabCalibration = "calibration"
)

// BacktestTabHref is the バックテスト tab of the 成績 page. The old
// `/performance#backtest` link is forwarded to it by pitha-performance-tabs.
const BacktestTabHref = "/performance?tab=backtest"

// candidateTabs is the 候補銘柄 tab bar: 候補 (Scanner) and 監視リスト.
func candidateTabs(current string) []molecules.Tab {
	return []molecules.Tab{
		{Label: "候補", Href: "/scanner", Current: current == tabCandidates},
		{Label: "監視リスト", Href: "/watchlist", Current: current == tabWatchlist},
	}
}

// resultTabs is the 成績 tab bar: 実績, バックテスト and 予測の精度.
func resultTabs(current string) []molecules.Tab {
	return []molecules.Tab{
		{Label: "実績", Href: "/performance", Current: current == tabActuals},
		{Label: "バックテスト", Href: BacktestTabHref, Current: current == tabBacktest},
		{Label: "予測の精度", Href: "/calibration", Current: current == tabCalibration},
	}
}
