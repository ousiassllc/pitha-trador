package actuals

// Check keys of Verdict.Checks, in the order they are reported.
const (
	CheckPeriodDays   = "period_days"
	CheckTrades       = "trades"
	CheckEVLowerBound = "ev_lower_bound"
	CheckMaxDrawdown  = "max_drawdown"
)

// judge applies the 合格ライン to the overall result o ("is it winning").
//
//   - period_days / trades: enough data (>= MinDays trading days, >= MinTrades
//     closed positions). Too little is 判定不能, not a failure: the sample
//     says nothing yet.
//   - ev_lower_bound: the lower end of EV (bps) must exceed MinEVLowerBps.
//     Judged only with enough data and a reliable interval, else 判定不能.
//   - max_drawdown: the observed capital-ratio drawdown must not exceed
//     MaxDrawdownPct. A breach fails at any sample size (a drawdown only
//     grows with more trades); within the cap it passes only with enough
//     data, and is 判定不能 without initial_capital.
//
// The whole is fail if any check fails, else 判定不能 if any is, else pass.
func judge(o Stats, capital float64, c Criteria) Verdict {
	enough := o.Days >= c.MinDays && o.N >= c.MinTrades
	pass := func(ok bool) Status {
		if ok {
			return StatusPass
		}
		return StatusFail
	}
	sufficient := func(ok bool) Status {
		if ok {
			return StatusPass
		}
		return StatusUndetermined
	}

	ev := Check{Key: CheckEVLowerBound, Required: c.MinEVLowerBps, Actual: o.EVBps.Lo, HasActual: o.EVBps.HasCI, Status: StatusUndetermined}
	if enough && o.EVBps.Reliable {
		ev.Status = pass(o.EVBps.Lo > c.MinEVLowerBps)
	}

	dd := Check{Key: CheckMaxDrawdown, Required: c.MaxDrawdownPct, Actual: o.MaxDrawdownPct.Value, HasActual: o.MaxDrawdownPct.Defined, Status: StatusUndetermined}
	if o.MaxDrawdownPct.Defined {
		switch {
		case o.MaxDrawdownPct.Value > c.MaxDrawdownPct:
			dd.Status = StatusFail
		case enough:
			dd.Status = StatusPass
		}
	}

	v := Verdict{Checks: []Check{
		{Key: CheckPeriodDays, Required: float64(c.MinDays), Actual: float64(o.Days), HasActual: true, Status: sufficient(o.Days >= c.MinDays)},
		{Key: CheckTrades, Required: float64(c.MinTrades), Actual: float64(o.N), HasActual: true, Status: sufficient(o.N >= c.MinTrades)},
		ev, dd,
	}}
	v.Status = StatusPass
	for _, ck := range v.Checks {
		switch ck.Status {
		case StatusFail:
			v.Status = StatusFail
			return v
		case StatusUndetermined:
			v.Status = StatusUndetermined
		}
	}
	return v
}
