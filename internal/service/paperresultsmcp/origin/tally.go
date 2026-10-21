package origin

// Stat is the closed trades of one origin, as paper_performance_summary's
// by_origin lists them: plain counts and the sum of realized_pnl, nothing
// recomputed. A ratio that is undefined is nil.
type Stat struct {
	Origin           string   `json:"origin"`
	ClosedTradeCount int      `json:"closed_trade_count"`
	WinCount         int      `json:"win_count"`
	LossCount        int      `json:"loss_count"`
	WinRate          *float64 `json:"win_rate"`
	TotalPnlJPY      float64  `json:"total_pnl_jpy"`
	ExpectancyJPY    *float64 `json:"expectancy_jpy"`
}

// Tally collects closed trades by origin. The zero value is ready.
type Tally struct {
	stats map[string]*Stat
}

// Add counts one closed trade of the given origin and realized pnl.
func (t *Tally) Add(origin string, pnl float64) {
	if t.stats == nil {
		t.stats = map[string]*Stat{}
	}
	s := t.stats[origin]
	if s == nil {
		s = &Stat{Origin: origin}
		t.stats[origin] = s
	}
	s.ClosedTradeCount++
	s.TotalPnlJPY += pnl
	switch {
	case pnl > 0:
		s.WinCount++
	case pnl < 0:
		s.LossCount++
	}
}

// Stats returns one Stat per origin in All() order, zeros for an origin with
// no trade, so the shape does not depend on the data.
func (t *Tally) Stats() []Stat {
	out := make([]Stat, 0, len(All()))
	for _, o := range All() {
		s := Stat{Origin: o}
		if got := t.stats[o]; got != nil {
			s = *got
			rate := float64(s.WinCount) / float64(s.ClosedTradeCount)
			mean := s.TotalPnlJPY / float64(s.ClosedTradeCount)
			s.WinRate, s.ExpectancyJPY = &rate, &mean
		}
		out = append(out, s)
	}
	return out
}
