// Package regime decides which strategy a Jev trader decision is made under
// (issue #755, parent #751): it classifies the market regime from the tracked
// market index's 1-minute returns (volatility and lag-1 autocorrelation:
// positive means trend, negative means mean reversion), assigns the decision
// to the A/B arm of the paper comparison, and - in a mean-reverting regime -
// measures the stock's deviation from VWAP as a z-score for the VWAP-reversion
// entry (internal/service/policy). The window and thresholds are Settings
// (internal/config/regimesettings).
package regime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const (
	// cacheTTL is how long an index regime is reused. The index is the same for
	// every stock, and its bars arrive once per 1-minute cycle, so a decision
	// burst shares one computation.
	cacheTTL = 30 * time.Second
	// historyBars is how many of an instrument's latest bars are read: the
	// longest window (regimesettings.MaxWindowMinutes) and a full trading day
	// for the VWAP deviation, at one bar per minute.
	historyBars = 400
)

// Instruments is the instrument-repository subset Assessor reads
// (*market.InstrumentRepository).
type Instruments interface {
	ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error)
}

// Snapshots is the snapshot-repository subset Assessor reads
// (*market.SnapshotRepository).
type Snapshots interface {
	ListHistoryByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.Snapshot, error)
}

// SettingsSource supplies the Settings at every decision, so a change applies
// without a restart.
type SettingsSource func(ctx context.Context) (regimesettings.Settings, error)

// FromStore reads the Settings from store (*system.RuntimeSettingsRepository)
// at every decision.
func FromStore(store regimesettings.Store) SettingsSource {
	return func(ctx context.Context) (regimesettings.Settings, error) {
		return regimesettings.Load(ctx, store)
	}
}

// indexLevel is the index-wide part of an assessment computed at one instant.
type indexLevel struct {
	at         time.Time
	settings   regimesettings.Settings
	volatility *float64
	autocorr   *float64
}

// Assessor builds a marketregime.Assessment for each decision. It is safe for
// concurrent use.
type Assessor struct {
	instruments Instruments
	snapshots   Snapshots
	settings    SettingsSource
	maxStale    time.Duration

	mu    sync.Mutex
	cache *indexLevel
}

// NewAssessor returns an Assessor reading the tracked market indices and the
// stock's history via instruments and snapshots, and the Settings via
// settings. maxStale is how old the latest index bar may be and still count as
// the current market (callers pass ScanConfig.SnapshotMaxAge(
// domain.MarketContextMaxAge), like the market context); non-positive falls
// back to domain.MarketContextMaxAge.
func NewAssessor(instruments Instruments, snapshots Snapshots, settings SettingsSource, maxStale time.Duration) *Assessor {
	if maxStale <= 0 {
		maxStale = domain.MarketContextMaxAge
	}
	return &Assessor{instruments: instruments, snapshots: snapshots, settings: settings, maxStale: maxStale}
}

// Assess returns the regime assessment of a decision on bar (the stock's
// latest market snapshot). It never fails: whatever cannot be read is logged
// and leaves that figure nil - a decision with no index data is
// marketregime.Unknown, which trades the plain momentum strategy - and a
// failure to read the Settings returns the zero assessment (not assessed).
func (a *Assessor) Assess(ctx context.Context, bar domain.Snapshot) marketregime.Assessment {
	s, err := a.settings(ctx)
	if err != nil {
		slog.Error("regime: load regime settings; decision is not assessed", "error", err)
		return marketregime.Assessment{}
	}
	level := a.indexLevel(ctx, s, bar.Timestamp)
	out := marketregime.Assessment{
		Regime:         Classify(level.volatility, level.autocorr, s),
		ABArm:          Arm(s.ABEnabled, bar.InstrumentID, bar.Timestamp),
		VolatilityBps:  level.volatility,
		Autocorr:       level.autocorr,
		VWAPZThreshold: s.VWAPZThreshold,
	}
	if out.Regime == marketregime.MeanRevert {
		out.VWAPZ = a.vwapZ(ctx, bar)
	}
	return out
}

// indexLevel returns the cached index-wide figures when still fresh for at and
// computed under the same Settings, else recomputes them. A failed read is not
// cached.
func (a *Assessor) indexLevel(ctx context.Context, s regimesettings.Settings, at time.Time) indexLevel {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c := a.cache; c != nil && c.settings == s && !c.at.After(at) && at.Sub(c.at) < cacheTTL {
		return *c
	}
	level := indexLevel{at: at, settings: s}
	ok := true
	indices, err := a.instruments.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex)
	if err != nil {
		slog.Warn("regime: list market index instruments", "error", err)
		ok = false
	}
	var vols, acs []float64
	for _, idx := range indices {
		bars, err := a.snapshots.ListHistoryByInstrument(ctx, idx.ID, historyBars)
		if err != nil {
			slog.Warn("regime: list index snapshots", "symbol", idx.Symbol, "error", err)
			ok = false
			continue
		}
		if !fresh(bars, at, a.maxStale) {
			continue
		}
		rs := minuteReturns(bars, at.Add(-s.Window), at)
		if v, vok := volatilityBps(rs); vok {
			vols = append(vols, v)
		}
		if c, cok := autocorr(rs); cok {
			acs = append(acs, c)
		}
	}
	level.volatility, level.autocorr = average(vols), average(acs)
	if ok {
		a.cache = &level
	}
	return level
}

// vwapZ reads the stock's session history and returns its VWAP deviation
// z-score, nil when it is unavailable (a read failure is logged).
func (a *Assessor) vwapZ(ctx context.Context, bar domain.Snapshot) *float64 {
	open, ok := marketcalendar.TSE.OpenAt(bar.Timestamp)
	if !ok {
		return nil
	}
	bars, err := a.snapshots.ListHistoryByInstrument(ctx, bar.InstrumentID, historyBars)
	if err != nil {
		slog.Warn("regime: list stock snapshots for the VWAP deviation", "symbol", bar.Symbol, "error", err)
		return nil
	}
	z, ok := VWAPZ(bars, open, bar.Timestamp)
	if !ok {
		return nil
	}
	return &z
}

// fresh reports whether the newest bar at or before at is no older than
// maxStale: a stalled index feed must not be read as the current market.
func fresh(bars []domain.Snapshot, at time.Time, maxStale time.Duration) bool {
	var newest time.Time
	for _, b := range bars {
		if !b.Timestamp.After(at) && b.Timestamp.After(newest) {
			newest = b.Timestamp
		}
	}
	return !newest.IsZero() && at.Sub(newest) <= maxStale
}

// average is the mean of vs, or nil when empty: several indices (TOPIX,
// Nikkei225) combine into one market figure.
func average(vs []float64) *float64 {
	if len(vs) == 0 {
		return nil
	}
	var sum float64
	for _, v := range vs {
		sum += v
	}
	mean := sum / float64(len(vs))
	return &mean
}
