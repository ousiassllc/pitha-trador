package replaylayertest

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// TestMain silences the Policy Engine's per-rejection warnings: a replay runs
// it for every bar.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

var jst = time.FixedZone("JST", 9*60*60)

// day0 is 09:00 JST of the first synthetic trading day.
var day0 = time.Date(2026, 3, 2, 9, 0, 0, 0, jst)

// at is minute m of the synthetic day (days after day0).
func at(day, m int) time.Time { return day0.AddDate(0, 0, day).Add(time.Duration(m) * time.Minute) }

func ptr[T any](v T) *T { return &v }

// dayMidnight is 00:00 JST of the synthetic day.
func dayMidnight(day int) time.Time { return time.Date(2026, 3, 2+day, 0, 0, 0, 0, jst) }

// script is one synthetic day of 1-minute bars: every price is 1000 and every
// spread 10 bps unless overridden. A "win at m" entered at minute m exits by
// take profit two bars later at 1020 (+2% = +200 bps, +20,000 yen at 1,000
// shares); a "loss at m" exits by stop loss at 980 (-200 bps, -20,000 yen).
type script struct {
	prices  map[int]float64
	spreads map[int]float64
}

func newScript() *script { return &script{prices: map[int]float64{}, spreads: map[int]float64{}} }

func (s *script) win(m int) *script  { s.prices[m+2] = 1020; return s }
func (s *script) loss(m int) *script { s.prices[m+2] = 980; return s }
func (s *script) spread(m int, bps float64) *script {
	s.spreads[m] = bps
	return s
}

// bars appends the script's day (n bars) to series as look-ahead-safe snapshots
// (Bid = Ask = price: a fill is at the bar price, so returns are hand-computable).
func (s *script) bars(series []domain.Snapshot, day, n int) []domain.Snapshot {
	for i := 0; i < n; i++ {
		price, spread := 1000.0, 10.0
		if p, ok := s.prices[i]; ok {
			price = p
		}
		if sp, ok := s.spreads[i]; ok {
			spread = sp
		}
		ts := at(day, i)
		history := series[max(0, len(series)-featureengine.HistoryLookbackBars):]
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts, Current: featureengine.Reading{Price: price, VWAP: price, Bid: ptr(price), Ask: ptr(price)}, History: history,
		})
		feature.OrderFlowFeatures = policytest.Feature().OrderFlowFeatures
		series = append(series, domain.Snapshot{
			InstrumentID: 1, Timestamp: ts, Price: price, Bid: ptr(price), Ask: ptr(price), SpreadBps: ptr(spread), Feature: feature,
		})
	}
	return series
}

var nextID int64

// decision is a LONG decision at minute m of day that clears every guard of the
// fixture thresholds; opts change it.
func decision(day, m int, opts ...func(*domain.JevDecision)) domain.JevDecision {
	nextID++
	d := domain.JevDecision{
		ID: nextID, InstrumentID: 1, Timestamp: at(day, m), Direction: ptr(domain.JevDirectionLong),
		EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.5),
		ContinuationProbability: ptr(0.70), ToxicFlow: ptr(0.10), LiquidityStressed: ptr(0.10),
	}
	for _, o := range opts {
		o(&d)
	}
	return d
}

func quality(q string) func(*domain.JevDecision) {
	return func(d *domain.JevDecision) { d.EntryQuality = ptr(q) }
}
func toxic(v float64) func(*domain.JevDecision) {
	return func(d *domain.JevDecision) { d.ToxicFlow = ptr(v) }
}
func continuation(v float64) func(*domain.JevDecision) {
	return func(d *domain.JevDecision) { d.ContinuationProbability = ptr(v) }
}
func noDirection() func(*domain.JevDecision) {
	return func(d *domain.JevDecision) { d.Direction = ptr(domain.JevDirectionNone) }
}

// reversion makes the decision a VWAP reversion candidate (LONG: price far
// below VWAP) whatever Jev's own direction is.
func reversion() func(*domain.JevDecision) {
	return func(d *domain.JevDecision) {
		d.MarketRegime = marketregime.Assessment{Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VWAPZ: ptr(-3.0)}
	}
}

// model scores p = 0.5 (EV +5 bps) at a 10 bps spread and p ~ 0.12 (EV < 0) at a
// 20 bps spread: a spread-20 candidate is the one the M axis skips.
func model() *metalabel.Model {
	m := policytest.LRModel(2, 0, 0, 0, 0, -0.2)
	return &m
}

func guards() policy.Thresholds {
	g := config.PolicyDirectionThresholds{
		// MinProbability 0.99 is above every fixture confidence (0.5): were the old
		// confidence threshold used by any cell, nothing would enter.
		MinProbability: 0.99, MinEntryQuality: domain.JevEntryQualityStrong,
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: g, Short: g}, MaxSpreadBps: 50}
}

func riskConfig() simrisk.Config {
	return simrisk.Config{StopLossPct: 1, Limits: config.RiskLimits{
		InitialCapital: 10_000_000, MaxPositionPerSymbolPct: 10, MaxTotalExposurePct: 50,
		MaxDailyLossPct: 5, MaxTradeLossPct: 0.5, MaxOpenPositions: 2, MaxSameDirectionPositions: 2,
		MarketAdverseReturn5mPct: 0.3, MaxSpreadBps: 30, MaxConsecutiveLosses: 3, CooldownAfterLossMinutes: 5,
	}}
}

// input is the fixture Input over [day 0, day days): bars of one instrument and
// the given decisions, the registered minimum EV 2 bps.
func input(bars []domain.Snapshot, days int, decisions ...domain.JevDecision) replaylayer.Input {
	return replaylayer.Input{
		From: dayMidnight(0), To: dayMidnight(days),
		Instruments: []replaylayer.Instrument{{InstrumentID: 1, Symbol: "7203", Snapshots: bars}},
		Decisions:   decisions, Thresholds: guards(), Risk: riskConfig(),
		Exit:  backtest.ExitRule{StopLossPct: 1, TakeProfitPct: 1, MaxHolding: 10 * time.Minute},
		Model: model(), Meta: metalabel.DecideConfig{MinEVBps: 2, KellyFraction: metalabel.DefaultKellyFraction},
	}
}

func run(t *testing.T, in replaylayer.Input) replaylayer.Result {
	t.Helper()
	res, err := replaylayer.Run(t.Context(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// cell finds a cell's result by name.
func cell(t *testing.T, res replaylayer.Result, name string) replaylayer.CellResult {
	t.Helper()
	for _, c := range res.Cells {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cell %q", name)
	return replaylayer.CellResult{}
}

// comparison finds a comparison by name.
func comparison(t *testing.T, cs []replaylayer.ComparisonResult, name string) replaylayer.ComparisonResult {
	t.Helper()
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no comparison %q", name)
	return replaylayer.ComparisonResult{}
}

// diffs is the comparison's series of stream/unit on segment 0 as plain diffs.
func diffs(t *testing.T, c replaylayer.ComparisonResult, segment int, stream, unit string) []replaylayer.DailyDiff {
	t.Helper()
	for _, s := range c.Segments[segment].Series {
		if s.Stream == stream && s.Unit == unit {
			return s.Days
		}
	}
	t.Fatalf("no series %s/%s", stream, unit)
	return nil
}

func near(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}
