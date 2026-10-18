package dailyguardsettings

import "math"

// Source says where a yen threshold comes from.
type Source int

const (
	// SourceNone: the rule's yen threshold is off (nothing usable stored).
	SourceNone Source = iota
	// SourcePercent: operating capital × percentage.
	SourcePercent
	// SourceDirect: the yen value stored directly (the pre-#782 setting).
	SourceDirect
)

// Rule is how one yen threshold was resolved, for the Settings screen.
type Rule struct {
	Source Source
	// Pct is the stored percentage (0 = not used), kept even when Source is
	// not SourcePercent so the screen can say the capital is missing.
	Pct float64
	// JPY is the effective threshold in yen (0 when disabled).
	JPY float64
}

// NeedsCapital reports that a percentage is stored but the operating capital
// is unset, which leaves the rule off.
func (r Rule) NeedsCapital() bool { return r.Source == SourceNone && r.Pct > 0 }

// Resolved is the effective configuration: Limits is what the risk engine
// reads, the rest explains where its yen values came from.
type Resolved struct {
	Limits            Limits
	CapitalJPY        float64
	LossLimit         Rule
	ProfitLockTrigger Rule
}

// Resolve turns the stored rows into Resolved. A yen threshold is the
// operating capital × its percentage when both are set (rounded to a whole
// yen); otherwise the direct yen row when set; otherwise off. The percentage
// wins when both forms are set. The giveback percentage and the trade count
// are used as stored; an unset one is 0, i.e. disabled.
func Resolve(s Settings) Resolved {
	r := Resolved{CapitalJPY: value(s.CapitalJPY)}
	r.LossLimit = resolveRule(r.CapitalJPY, value(s.LossLimitPct), value(s.LossLimitJPY))
	r.ProfitLockTrigger = resolveRule(r.CapitalJPY, value(s.ProfitLockTriggerPct), value(s.ProfitLockTriggerJPY))
	r.Limits = Limits{
		LossLimitJPY:          r.LossLimit.JPY,
		ProfitLockTriggerJPY:  r.ProfitLockTrigger.JPY,
		ProfitLockGivebackPct: value(s.ProfitLockGivebackPct),
	}
	if s.MaxTrades != nil {
		r.Limits.MaxTrades = *s.MaxTrades
	}
	return r
}

func resolveRule(capital, pct, directJPY float64) Rule {
	switch {
	case pct > 0 && capital > 0:
		// Divide first so a huge capital cannot overflow before the percentage
		// is applied; a non-finite result switches the rule off rather than
		// handing the risk engine an infinite limit.
		jpy := math.Round(capital / 100 * pct)
		if math.IsNaN(jpy) || math.IsInf(jpy, 0) {
			return Rule{Pct: pct}
		}
		return Rule{Source: SourcePercent, Pct: pct, JPY: jpy}
	case directJPY > 0:
		return Rule{Source: SourceDirect, Pct: pct, JPY: directJPY}
	default:
		return Rule{Pct: pct}
	}
}

func value(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
