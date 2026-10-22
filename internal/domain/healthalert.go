package domain

import "time"

// Health alert codes (issue #802): fixed codes, never free text, so the app's
// banner and the Paper results MCP (collection_status "alerts") depend on the
// code alone. The rules that raise them are in internal/service/healthalert.
const (
	// HealthAlertKabuNotLoggedIn: kabuステーション not logged in (token issuance fails with
	// 4001007); HealthAlertKabuUnreachable (issue #863): it refuses the connection (not
	// running). Both on a trading day 08:45 JST to the close, at 08:45 or after 5 minutes.
	HealthAlertKabuNotLoggedIn = "kabu_not_logged_in"
	HealthAlertKabuUnreachable = "kabu_unreachable"
	// HealthAlertMarketDataIdle: no market-data job finished (neither
	// succeeded nor failed) for 5 minutes inside a trading session - the app is
	// not running or the worker has stalled.
	HealthAlertMarketDataIdle = "market_data_idle"
)

// HealthAlert is one active alert: its fixed code and when the condition
// began.
type HealthAlert struct {
	Code  string
	Since time.Time
}
