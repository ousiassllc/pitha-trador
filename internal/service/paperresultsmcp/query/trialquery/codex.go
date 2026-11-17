package trialquery

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// SubscriptionCost is the calls of the ChatGPT subscription (cost_source
// subscription, issue #1081): no USD at all (they are not in any USD figure), so
// they are counted by calls and tokens. A failed call has no token counts.
type SubscriptionCost struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CodexHaltInfo is the Codex provider's recorded stop: the reason code and the time
// it ends only (no text of the CLI's output). Active is true while Until is ahead.
type CodexHaltInfo struct {
	Reason     string `json:"reason"`
	At         string `json:"at"`
	Until      string `json:"until"`
	UntilKnown bool   `json:"until_known"`
	Active     bool   `json:"active"`
}

// CodexLoginInfo is the last login check of the Codex CLI the app made (the state
// only; a credential is never read and none is returned). State is "" when the
// app has not checked yet.
type CodexLoginInfo struct {
	State     string `json:"state"`
	CheckedAt string `json:"checked_at"`
}

// CodexLimits is the ChatGPT subscription provider's switch, login, caps and stop:
// what a person sees on the Settings screen. The caps are calls and tokens per JST
// day (not USD).
type CodexLimits struct {
	Enabled          bool           `json:"enabled"`
	Login            CodexLoginInfo `json:"login"`
	CallsToday       int            `json:"calls_today"`
	DailyCallCap     int            `json:"daily_call_cap"`
	TokensToday      int            `json:"tokens_today"`
	DailyTokenCap    int            `json:"daily_token_cap"`
	MinCallIntervalS int            `json:"min_call_interval_seconds"`
	Halt             *CodexHaltInfo `json:"halt"`
}

// readCodexLimits reads the provider's keys (already loaded in cfg) and today's
// usage from jev_trial_decisions.
func readCodexLimits(ctx context.Context, q query.Q, cfg jevtrialsettings.CodexSettings, from, to, now time.Time) (CodexLimits, error) {
	l := CodexLimits{
		Enabled: cfg.Enabled, DailyCallCap: cfg.CallCap, DailyTokenCap: cfg.TokenCap,
		MinCallIntervalS: int(jevtrialsettings.CodexMinCallInterval.Seconds()),
	}
	if cfg.Login != nil {
		l.Login = CodexLoginInfo{State: cfg.Login.State, CheckedAt: cfg.Login.At}
	}
	if h := cfg.Halt; h != nil {
		l.Halt = &CodexHaltInfo{Reason: h.Reason, At: h.At, Until: h.Until, UntilKnown: h.UntilKnown, Active: h.ActiveAt(now)}
	}
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0)), 0) FROM jev_trial_decisions
WHERE provider = ? AND created_at >= ? AND created_at < ?`,
		jevtrialsettings.ProviderCodexChatGPT, sqlutil.FormatTime(from), sqlutil.FormatTime(to)).Scan(&l.CallsToday, &l.TokensToday)
	if err != nil {
		return CodexLimits{}, fmt.Errorf("query: today's Codex usage: %w", err)
	}
	return l, nil
}
