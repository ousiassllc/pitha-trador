package organisms

// MarketDataBannerProps is the broker connection's content for the Home
// screen's 要対応 cards (issues #295, #712, #739; produced by
// system.MarketDataHandler.BannerProps).
type MarketDataBannerProps struct {
	// Issue is the cause's machine name (marketdata.TokenIssue), "" while
	// the token is fine.
	Issue string
	// Guidance is the cause-specific remedy (marketdata.TokenStatus.Guidance).
	Guidance string
	// Persistent marks a not_logged_in streak that keeps repeating or lasting
	// (broker.SessionStatus.Persistent): the post-maintenance manual
	// kabuステーション re-login is still pending.
	Persistent bool
	// Environment is the 立花 e支店 environment ("demo" / "production",
	// config.TachibanaEnvDemo / TachibanaEnvProduction) the failing session
	// belongs to, "" for kabu (issue #739).
	Environment string
	// Notice is a non-failure heads-up of the broker (立花 e支店: 書面未読は
	// 失敗として Guidance に出る。ここは API の版数更新の予告, issue #727; the 立花 watch list fell back to a stand-in
	// because the daily bars were unusable, issue #730). It
	// is shown only while Guidance is empty.
	Notice string
}
