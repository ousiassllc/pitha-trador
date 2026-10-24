package dailyguardsettings_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

func codes(ws []dailyguardsettings.Warning) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Code
	}
	return out
}

func equalCodes(got []dailyguardsettings.Warning, want ...string) bool {
	g := codes(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

func TestCheckConsistency(t *testing.T) {
	ref := dailyguardsettings.RiskReference{InitialCapitalJPY: 30_000_000, MaxDailyLossPct: 1}
	resolve := func(capital, lossPct, lossJPY float64) dailyguardsettings.Resolved {
		s := dailyguardsettings.Settings{CapitalJPY: &capital, LossLimitPct: &lossPct, LossLimitJPY: &lossJPY}
		return dailyguardsettings.Resolve(s)
	}
	tests := []struct {
		name string
		r    dailyguardsettings.Resolved
		ref  dailyguardsettings.RiskReference
		want []string
	}{
		{"capital differs", resolve(500_000, 1, 0), ref, []string{dailyguardsettings.WarnCapitalMismatch}},
		{"equal capital does not warn", resolve(30_000_000, 1, 0), ref, nil},
		{"one yen apart warns (no tolerance)", resolve(30_000_001, 0, 0), ref, []string{dailyguardsettings.WarnCapitalMismatch}},
		{"capital unset is not repeated", resolve(0, 1, 0), ref, nil},
		{"reference capital unset skips both", resolve(500_000, 0, 400_000), dailyguardsettings.RiskReference{MaxDailyLossPct: 1}, nil},
		{"reference loss pct unset skips the kill switch check", resolve(30_000_000, 0, 400_000), dailyguardsettings.RiskReference{InitialCapitalJPY: 30_000_000}, nil},
		{"loss limit above the kill switch amount", resolve(30_000_000, 2, 0), ref, []string{dailyguardsettings.WarnLossLimitAboveKillSwitch}},
		{"loss limit equal to the kill switch amount does not warn", resolve(30_000_000, 1, 0), ref, nil},
		{"direct yen loss limit above the kill switch amount", resolve(0, 0, 300_001), ref, []string{dailyguardsettings.WarnLossLimitAboveKillSwitch}},
		{"disabled loss limit never warns", resolve(30_000_000, 0, 0), ref, nil},
		{"both", resolve(60_000_000, 1, 0), ref, []string{dailyguardsettings.WarnCapitalMismatch, dailyguardsettings.WarnLossLimitAboveKillSwitch}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dailyguardsettings.CheckConsistency(tc.r, tc.ref)
			if !equalCodes(got, tc.want...) {
				t.Fatalf("warnings = %v, want %v", codes(got), tc.want)
			}
		})
	}
}

// The yen limit is rounded by Resolve, so the Kill Switch amount is compared
// rounded too: float noise in capital × pct must not make an equal limit
// warn, a half-yen amount rounds up on both sides, and a real excess still
// warns.
func TestCheckConsistency_RoundingNoise(t *testing.T) {
	resolve := func(capital, lossPct, lossJPY float64) dailyguardsettings.Resolved {
		return dailyguardsettings.Resolve(dailyguardsettings.Settings{CapitalJPY: &capital, LossLimitPct: &lossPct, LossLimitJPY: &lossJPY})
	}
	ref := func(capital, pct float64) dailyguardsettings.RiskReference {
		return dailyguardsettings.RiskReference{InitialCapitalJPY: capital, MaxDailyLossPct: pct}
	}
	tests := []struct {
		name string
		r    dailyguardsettings.Resolved
		ref  dailyguardsettings.RiskReference
		want []string
	}{
		{"700,000 at 0.7% equals the kill switch", resolve(700_000, 0.7, 0), ref(700_000, 0.7), nil},
		{"1,400,000 at 0.7% equals the kill switch", resolve(1_400_000, 0.7, 0), ref(1_400_000, 0.7), nil},
		{"half-yen amount rounds up on both sides (500,050 at 1%)", resolve(500_050, 1, 0), ref(500_050, 1), nil},
		{"a real excess over the kill switch still warns", resolve(700_000, 0.8, 0), ref(700_000, 0.7), []string{dailyguardsettings.WarnLossLimitAboveKillSwitch}},
		{"one yen above the rounded kill switch warns", resolve(0, 0, 4_901), ref(700_000, 0.7), []string{dailyguardsettings.WarnLossLimitAboveKillSwitch}},
		{"direct yen equal to the rounded kill switch does not warn", resolve(0, 0, 4_900), ref(700_000, 0.7), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dailyguardsettings.CheckConsistency(tc.r, tc.ref)
			if !equalCodes(got, tc.want...) {
				t.Fatalf("warnings = %v, want %v", codes(got), tc.want)
			}
		})
	}
}

func TestCheckConsistency_MessagesNameTheFigures(t *testing.T) {
	capital, pct := 60_000_000.0, 1.0
	r := dailyguardsettings.Resolve(dailyguardsettings.Settings{CapitalJPY: &capital, LossLimitPct: &pct})
	got := dailyguardsettings.CheckConsistency(r, dailyguardsettings.RiskReference{InitialCapitalJPY: 30_000_000, MaxDailyLossPct: 1})
	if len(got) != 2 {
		t.Fatalf("warnings = %+v", got)
	}
	for i, wants := range [][]string{
		{"運用資金 60,000,000 円", "リスク設定の運用資金 30,000,000 円", "ポジションサイズ"},
		{"600,000 円", "300,000 円", "Kill Switch", "強制決済が発動します"},
	} {
		for _, want := range wants {
			if !strings.Contains(got[i].Message, want) {
				t.Errorf("warning %d lacks %q: %s", i, want, got[i].Message)
			}
		}
	}
}

func TestLiveReadiness(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	n := func(v int) *int { return &v }
	capital := dailyguardsettings.Settings{CapitalJPY: f(500_000)}
	tests := []struct {
		name string
		s    dailyguardsettings.Settings
		want []string
	}{
		{"all disabled", dailyguardsettings.Settings{}, []string{dailyguardsettings.WarnLiveLossLimit, dailyguardsettings.WarnLiveProfitLock, dailyguardsettings.WarnLiveMaxTrades}},
		{"all enabled", dailyguardsettings.Settings{CapitalJPY: f(500_000), LossLimitPct: f(1), ProfitLockTriggerPct: f(0.5), ProfitLockGivebackPct: f(50), MaxTrades: n(20)}, nil},
		{"only loss limit disabled", dailyguardsettings.Settings{ProfitLockTriggerJPY: f(2500), ProfitLockGivebackPct: f(50), MaxTrades: n(5)}, []string{dailyguardsettings.WarnLiveLossLimit}},
		{"only profit lock disabled", dailyguardsettings.Settings{LossLimitJPY: f(5000), MaxTrades: n(5)}, []string{dailyguardsettings.WarnLiveProfitLock}},
		{"only max trades disabled", dailyguardsettings.Settings{LossLimitJPY: f(5000), ProfitLockTriggerJPY: f(2500), ProfitLockGivebackPct: f(50)}, []string{dailyguardsettings.WarnLiveMaxTrades}},
		{"max trades 0 is disabled", dailyguardsettings.Settings{LossLimitJPY: f(5000), ProfitLockTriggerJPY: f(2500), ProfitLockGivebackPct: f(50), MaxTrades: n(0)}, []string{dailyguardsettings.WarnLiveMaxTrades}},
		{"profit lock without giveback", dailyguardsettings.Settings{LossLimitJPY: f(5000), ProfitLockTriggerJPY: f(2500), MaxTrades: n(5)}, []string{dailyguardsettings.WarnLiveProfitLock}},
		{"profit lock without trigger", dailyguardsettings.Settings{LossLimitJPY: f(5000), ProfitLockGivebackPct: f(50), MaxTrades: n(5)}, []string{dailyguardsettings.WarnLiveProfitLock}},
		{"percent without capital leaves the rule disabled", dailyguardsettings.Settings{LossLimitPct: f(1), ProfitLockTriggerJPY: f(2500), ProfitLockGivebackPct: f(50), MaxTrades: n(5)}, []string{dailyguardsettings.WarnLiveLossLimit}},
		{"capital alone enables nothing", capital, []string{dailyguardsettings.WarnLiveLossLimit, dailyguardsettings.WarnLiveProfitLock, dailyguardsettings.WarnLiveMaxTrades}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dailyguardsettings.LiveReadiness(dailyguardsettings.Resolve(tc.s))
			if !equalCodes(got, tc.want...) {
				t.Fatalf("warnings = %v, want %v", codes(got), tc.want)
			}
		})
	}
}

func TestLiveReadiness_MessagesExplainWhy(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for name, tc := range map[string]struct {
		s    dailyguardsettings.Settings
		code string
		want string
	}{
		"percent needs capital":    {dailyguardsettings.Settings{LossLimitPct: f(1)}, dailyguardsettings.WarnLiveLossLimit, "運用資金が未設定"},
		"giveback only":            {dailyguardsettings.Settings{ProfitLockGivebackPct: f(50)}, dailyguardsettings.WarnLiveProfitLock, "発動額が無効"},
		"trigger only":             {dailyguardsettings.Settings{ProfitLockTriggerJPY: f(100)}, dailyguardsettings.WarnLiveProfitLock, "許容戻し率が 0"},
		"neither profit lock half": {dailyguardsettings.Settings{}, dailyguardsettings.WarnLiveProfitLock, "両方が必要"},
	} {
		var msg string
		for _, w := range dailyguardsettings.LiveReadiness(dailyguardsettings.Resolve(tc.s)) {
			if w.Code == tc.code {
				msg = w.Message
			}
		}
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message = %q, want it to contain %q", name, msg, tc.want)
		}
	}
}
