package researchquotes

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// newSample is the row for quote q of the minute stamped at, last updated at
// observed. The vwap is stored only when the broker reported one (a zero is "none").
func newSample(at time.Time, q broker.Quote, observed time.Time, via string) researchquote.Sample {
	s := researchquote.Sample{
		Timestamp: at, Source: researchquote.SourceTachibana, Symbol: q.Symbol, Price: q.Price,
		Bid: q.Bid, Ask: q.Ask, High: q.High, Low: q.Low,
		Volume: &q.Volume, Turnover: &q.Turnover,
		SpecialQuote: q.SpecialQuote, Via: via, SessionPhase: researchquote.PhaseAt(at),
	}
	if q.VWAP > 0 {
		s.VWAP = &q.VWAP
	}
	if !observed.IsZero() {
		s.ObservedAt = &observed
	}
	return s
}
