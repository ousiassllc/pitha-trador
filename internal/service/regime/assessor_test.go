package regime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

type fakeInstruments struct {
	indices []domain.Instrument
	err     error
	calls   int
}

func (f *fakeInstruments) ListActiveByKind(_ context.Context, kind string) ([]domain.Instrument, error) {
	f.calls++
	if kind != domain.InstrumentKindMarketIndex {
		return nil, nil
	}
	return f.indices, f.err
}

type fakeSnapshots struct {
	byInstrument map[int64][]domain.Snapshot
	err          error
}

func (f fakeSnapshots) ListHistoryByInstrument(_ context.Context, id int64, _ int) ([]domain.Snapshot, error) {
	return f.byInstrument[id], f.err
}

// 2026-10-14 is a Wednesday and a trading day; 09:00 JST is 00:00 UTC.
var open = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

// indexBars builds 31 one-minute bars from open+1m whose returns are rs.
func indexBars(id int64, rs []float64) []domain.Snapshot {
	price := 100.0
	out := []domain.Snapshot{{InstrumentID: id, Timestamp: open, Price: price}}
	for i, r := range rs {
		price *= 1 + r
		out = append(out, domain.Snapshot{InstrumentID: id, Timestamp: open.Add(time.Duration(i+1) * time.Minute), Price: price})
	}
	return out
}

func alternating(n int) []float64 {
	rs := make([]float64, n)
	for i := range rs {
		rs[i] = 0.002
		if i%2 == 1 {
			rs[i] = -0.002
		}
	}
	return rs
}

func runs(n int) []float64 {
	rs := make([]float64, n)
	for i := range rs {
		rs[i] = 0.002
		if (i/5)%2 == 1 {
			rs[i] = -0.002
		}
	}
	return rs
}

func settingsOf(s regimesettings.Settings) SettingsSource {
	return func(context.Context) (regimesettings.Settings, error) { return s, nil }
}

func stockBar(at time.Time) domain.Snapshot {
	return domain.Snapshot{InstrumentID: 5, Symbol: "7203", Timestamp: at, Price: 100, Feature: domain.Feature{VWAP: 100}}
}

// stockHistory is a stock session at open+0..20m, whose deviation from VWAP is
// 10 bps except the last bar's `last`.
func stockHistory(last float64) []domain.Snapshot {
	out := make([]domain.Snapshot, 21)
	for i := range out {
		out[i] = domain.Snapshot{InstrumentID: 5, Timestamp: open.Add(time.Duration(i) * time.Minute), Price: 100, Feature: domain.Feature{VWAP: 100, PriceVsVWAPBps: 10}}
	}
	out[20].Feature.PriceVsVWAPBps = last
	return out
}

func newAssessor(ins *fakeInstruments, snaps fakeSnapshots, s regimesettings.Settings) *Assessor {
	return NewAssessor(ins, snaps, settingsOf(s), 3*time.Minute)
}

var topix = []domain.Instrument{{ID: 1, Symbol: "101", Kind: domain.InstrumentKindMarketIndex}}

func TestAssess_MeanRevertingMarketMeasuresVWAPZ(t *testing.T) {
	at := open.Add(20 * time.Minute)
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, alternating(20)), 5: stockHistory(-30)}}
	a := newAssessor(&fakeInstruments{indices: topix}, snaps, regimesettings.Default())

	got := a.Assess(context.Background(), stockBar(at))

	if got.Regime != marketregime.MeanRevert {
		t.Fatalf("Regime = %q, want mean_revert (alternating index returns)", got.Regime)
	}
	if got.ABArm != marketregime.ArmMomentum {
		t.Errorf("ABArm = %q, want momentum while the A/B comparison is off", got.ABArm)
	}
	if got.Autocorr == nil || *got.Autocorr >= -0.2 || got.VolatilityBps == nil || *got.VolatilityBps < 15 {
		t.Errorf("evidence autocorr=%v vol=%v, want a clearly negative autocorrelation and ~20 bps", got.Autocorr, got.VolatilityBps)
	}
	if got.VWAPZ == nil || *got.VWAPZ >= -2.9 || *got.VWAPZ <= -3.1 {
		t.Errorf("VWAPZ = %v, want ~-3 (-30 bps against a typical 10 bps)", got.VWAPZ)
	}
	if got.VWAPZThreshold != 2 {
		t.Errorf("VWAPZThreshold = %v, want the Settings value 2", got.VWAPZThreshold)
	}
}

func TestAssess_TrendingMarketSkipsVWAPZ(t *testing.T) {
	at := open.Add(20 * time.Minute)
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, runs(20)), 5: stockHistory(-30)}}
	a := newAssessor(&fakeInstruments{indices: topix}, snaps, regimesettings.Default())

	got := a.Assess(context.Background(), stockBar(at))

	if got.Regime != marketregime.Trend {
		t.Fatalf("Regime = %q, want trend (persistent index runs)", got.Regime)
	}
	if got.VWAPZ != nil {
		t.Errorf("VWAPZ = %v, want nil: it is only measured in a mean-reverting regime", *got.VWAPZ)
	}
}

func TestAssess_ABEnabledAssignsAnArm(t *testing.T) {
	at := open.Add(20 * time.Minute)
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, alternating(20))}}
	s := regimesettings.Default()
	s.ABEnabled = true
	a := newAssessor(&fakeInstruments{indices: topix}, snaps, s)

	arms := map[string]bool{}
	for id := int64(1); id <= 40; id++ {
		bar := stockBar(at)
		bar.InstrumentID = id
		arms[a.Assess(context.Background(), bar).ABArm] = true
	}
	if !arms[marketregime.ArmMomentum] || !arms[marketregime.ArmRegime] || len(arms) != 2 {
		t.Fatalf("arms over 40 instruments = %v, want both momentum and regime", arms)
	}
}

func TestAssess_NoIndexDataIsUnknownMomentum(t *testing.T) {
	at := open.Add(20 * time.Minute)
	for name, ins := range map[string]*fakeInstruments{
		"no index instruments": {},
		"index list fails":     {err: errors.New("db down")},
	} {
		a := newAssessor(ins, fakeSnapshots{}, regimesettings.Default())
		got := a.Assess(context.Background(), stockBar(at))
		if got.Regime != marketregime.Unknown || got.VolatilityBps != nil || got.Autocorr != nil || got.VWAPZ != nil {
			t.Errorf("%s: assessment = %+v, want unknown with no evidence", name, got)
		}
		if got.ABArm != marketregime.ArmMomentum {
			t.Errorf("%s: ABArm = %q, want momentum", name, got.ABArm)
		}
	}
}

func TestAssess_StaleIndexIsUnknown(t *testing.T) {
	// The index's newest bar is 20 minutes older than the decision.
	at := open.Add(40 * time.Minute)
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, alternating(20))}}
	a := newAssessor(&fakeInstruments{indices: topix}, snaps, regimesettings.Default())

	if got := a.Assess(context.Background(), stockBar(at)); got.Regime != marketregime.Unknown {
		t.Fatalf("Regime = %q, want unknown for a stalled index feed", got.Regime)
	}
}

func TestAssess_DoesNotUseIndexBarsAfterTheDecision(t *testing.T) {
	// Only the 10 returns up to open+10m are visible at the decision: the later
	// alternating returns must not leak in. The visible ones are one flat
	// series, so the market is not classifiable.
	rs := make([]float64, 20)
	for i := 10; i < 20; i++ {
		rs[i] = 0.002
		if i%2 == 1 {
			rs[i] = -0.002
		}
	}
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, rs)}}
	a := newAssessor(&fakeInstruments{indices: topix}, snaps, regimesettings.Default())

	if got := a.Assess(context.Background(), stockBar(open.Add(10*time.Minute))); got.Regime != marketregime.Unknown {
		t.Fatalf("Regime = %q, want unknown: bars after the decision time must not be read (FR-FE-1)", got.Regime)
	}
}

func TestAssess_CachesIndexLevelButRereadsOnSettingsChange(t *testing.T) {
	at := open.Add(20 * time.Minute)
	snaps := fakeSnapshots{byInstrument: map[int64][]domain.Snapshot{1: indexBars(1, alternating(20))}}
	ins := &fakeInstruments{indices: topix}
	s := regimesettings.Default()
	a := NewAssessor(ins, snaps, func(context.Context) (regimesettings.Settings, error) { return s, nil }, 3*time.Minute)

	a.Assess(context.Background(), stockBar(at))
	a.Assess(context.Background(), stockBar(at.Add(10*time.Second)))
	if ins.calls != 1 {
		t.Fatalf("index instruments listed %d times for two decisions in one cache window, want 1", ins.calls)
	}

	s.AutocorrThreshold = 0.99 // the Settings changed: the cached classification must not serve
	if got := a.Assess(context.Background(), stockBar(at.Add(20*time.Second))); got.Regime != marketregime.Unknown {
		t.Fatalf("Regime after raising the threshold = %q, want unknown", got.Regime)
	}
	if ins.calls != 2 {
		t.Fatalf("index instruments listed %d times after a Settings change, want 2", ins.calls)
	}
}

func TestAssess_SettingsFailureIsNotAssessed(t *testing.T) {
	a := NewAssessor(&fakeInstruments{}, fakeSnapshots{}, func(context.Context) (regimesettings.Settings, error) {
		return regimesettings.Settings{}, errors.New("db down")
	}, 0)

	if got := a.Assess(context.Background(), stockBar(open)); got != (marketregime.Assessment{}) {
		t.Fatalf("assessment = %+v, want the zero (not assessed) value", got)
	}
}
