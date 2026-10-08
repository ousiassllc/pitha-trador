package organisms

import (
	"fmt"
	"time"
)

// MarketDataBannerProps is MarketDataBanner's input (issues #295, #712, #739).
type MarketDataBannerProps struct {
	// Issue is the cause's machine name (marketdata.TokenIssue), "" while
	// the token is fine.
	Issue string
	// Guidance is the cause-specific remedy (marketdata.TokenStatus.Guidance).
	Guidance string
	// Persistent escalates the banner for a not_logged_in streak that keeps
	// repeating or lasting (broker.SessionStatus.Persistent): the
	// post-maintenance manual kabuステーション re-login is still pending.
	Persistent bool
	// Failures is the number of consecutive failed token issuances.
	Failures int
	// Elapsed is how long the streak has lasted.
	Elapsed time.Duration
	// Environment is the 立花 e支店 environment ("demo" / "production",
	// config.TachibanaEnvDemo / TachibanaEnvProduction) the failing session
	// belongs to, "" for kabu. When set the banner always shows which
	// environment it is about (issue #739).
	Environment string
}

// persistentElapsedStep coarsens the shown elapsed time: the banner is a
// polite live region (issue #626), so a figure that changes on every 30s
// poll would be re-announced each time.
const persistentElapsedStep = 5 * time.Minute

// formatPersistentElapsed renders d rounded down to persistentElapsedStep as
// "N分" / "N時間M分"; the escalation threshold is 5 minutes, so it is at
// least "5分".
func formatPersistentElapsed(d time.Duration) string {
	minutes := int(d.Truncate(persistentElapsedStep) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%d分", minutes)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%d時間", minutes/60)
	}
	return fmt.Sprintf("%d時間%d分", minutes/60, minutes%60)
}
