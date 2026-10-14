package regime

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

var t0 = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) // 09:00 JST

// bars builds one bar per minute from t0 with the given prices.
func bars(prices ...float64) []domain.Snapshot {
	out := make([]domain.Snapshot, len(prices))
	for i, p := range prices {
		out[i] = domain.Snapshot{Timestamp: t0.Add(time.Duration(i) * time.Minute), Price: p}
	}
	return out
}

// walk builds prices whose 1-minute returns follow rs.
func walk(rs ...float64) []float64 {
	p := []float64{100}
	for _, r := range rs {
		p = append(p, p[len(p)-1]*(1+r))
	}
	return p
}

func ptr[T any](v T) *T { return &v }

func TestMinuteReturns_DoesNotBridgeGapsOrLookAhead(t *testing.T) {
	b := bars(100, 101, 102, 103)
	b = append(b[:2], b[3:]...) // drop minute 2: 101 -> 103 spans two minutes
	rs := minuteReturns(b, t0, t0.Add(time.Hour))
	if len(rs) != 1 || math.Abs(rs[0].r-0.01) > 1e-12 {
		t.Fatalf("returns across a gap = %+v, want only the 100->101 return", rs)
	}

	rs = minuteReturns(bars(100, 101, 102, 103), t0, t0.Add(2*time.Minute))
	if len(rs) != 2 {
		t.Fatalf("returns up to minute 2 = %d, want 2 (bars after `to` are ignored)", len(rs))
	}
}

func TestMinuteReturns_UsesLastBarOfEachMinute(t *testing.T) {
	b := []domain.Snapshot{
		{Timestamp: t0, Price: 100},
		{Timestamp: t0.Add(time.Minute), Price: 105},
		{Timestamp: t0.Add(time.Minute + 30*time.Second), Price: 102},
	}
	rs := minuteReturns(b, t0, t0.Add(time.Hour))
	if len(rs) != 1 || math.Abs(rs[0].r-0.02) > 1e-12 {
		t.Fatalf("returns = %+v, want the minute's last price 102 against 100", rs)
	}
}

func TestVolatilityAndAutocorr_NeedEnoughReturns(t *testing.T) {
	rs := minuteReturns(bars(walk(0.001, -0.001, 0.001)...), t0, t0.Add(time.Hour))
	if _, ok := volatilityBps(rs); ok {
		t.Error("volatility computed from 3 returns, want unavailable")
	}
	if _, ok := autocorr(rs); ok {
		t.Error("autocorrelation computed from 3 returns, want unavailable")
	}
}

func TestAutocorr_SignsTrendAndMeanReversion(t *testing.T) {
	// Persistent runs of the same sign: positive autocorrelation.
	trend := minuteReturns(bars(walk(0.002, 0.002, 0.002, 0.002, 0.002, -0.002, -0.002, -0.002, -0.002, -0.002, 0.002, 0.002, 0.002, 0.002, 0.002, -0.002, -0.002, -0.002, -0.002, -0.002)...), t0, t0.Add(time.Hour))
	if ac, ok := autocorr(trend); !ok || ac <= 0.5 {
		t.Errorf("autocorr of persistent runs = %v, %v, want clearly positive", ac, ok)
	}
	// Alternating returns: strongly negative.
	reverting := minuteReturns(bars(walk(0.002, -0.002, 0.002, -0.002, 0.002, -0.002, 0.002, -0.002, 0.002, -0.002, 0.002, -0.002)...), t0, t0.Add(time.Hour))
	if ac, ok := autocorr(reverting); !ok || ac >= -0.5 {
		t.Errorf("autocorr of alternating returns = %v, %v, want clearly negative", ac, ok)
	}
	if v, ok := volatilityBps(reverting); !ok || v < 15 || v > 25 {
		t.Errorf("volatility of ±0.2%% returns = %v, %v, want ~20 bps", v, ok)
	}
}

func TestAutocorr_FlatSeriesIsUnavailable(t *testing.T) {
	flat := minuteReturns(bars(100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100), t0, t0.Add(time.Hour))
	if _, ok := autocorr(flat); ok {
		t.Error("autocorrelation of a flat series reported ok, want unavailable (no variance)")
	}
}

func TestClassify(t *testing.T) {
	s := regimesettings.Settings{AutocorrThreshold: 0.2, MinVolatilityBps: 1}
	cases := []struct {
		name     string
		vol, ac  *float64
		wantKind string
	}{
		{"trend", ptr(5.0), ptr(0.3), marketregime.Trend},
		{"trend at threshold", ptr(5.0), ptr(0.2), marketregime.Trend},
		{"mean revert", ptr(5.0), ptr(-0.3), marketregime.MeanRevert},
		{"mean revert at threshold", ptr(5.0), ptr(-0.2), marketregime.MeanRevert},
		{"inside band", ptr(5.0), ptr(0.1), marketregime.Unknown},
		{"too quiet", ptr(0.5), ptr(-0.9), marketregime.Unknown},
		{"no volatility", nil, ptr(0.9), marketregime.Unknown},
		{"no autocorrelation", ptr(5.0), nil, marketregime.Unknown},
	}
	for _, c := range cases {
		if got := Classify(c.vol, c.ac, s); got != c.wantKind {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.wantKind)
		}
	}
}

// vwapBars builds a session of 12 bars, one per minute from open, whose
// PriceVsVWAPBps are dev (earlier bars) and last.
func vwapBars(open time.Time, dev, last float64) []domain.Snapshot {
	out := make([]domain.Snapshot, 12)
	for i := range out {
		out[i] = domain.Snapshot{Timestamp: open.Add(time.Duration(i) * time.Minute), Feature: domain.Feature{VWAP: 100, PriceVsVWAPBps: dev}}
	}
	out[11].Feature.PriceVsVWAPBps = last
	return out
}

func TestVWAPZ(t *testing.T) {
	b := vwapBars(t0, 10, 30)
	z, ok := VWAPZ(b, t0, b[11].Timestamp)
	if !ok || math.Abs(z-3) > 1e-9 {
		t.Fatalf("VWAPZ = %v, %v, want 3 (30 bps over the earlier bars' rms of 10)", z, ok)
	}

	// Below VWAP: negative.
	b = vwapBars(t0, 10, -25)
	if z, ok := VWAPZ(b, t0, b[11].Timestamp); !ok || math.Abs(z+2.5) > 1e-9 {
		t.Errorf("VWAPZ below VWAP = %v, %v, want -2.5", z, ok)
	}
}

func TestVWAPZ_Unavailable(t *testing.T) {
	b := vwapBars(t0, 10, 30)
	if _, ok := VWAPZ(b[:5], t0, b[4].Timestamp); ok {
		t.Error("VWAPZ with 4 earlier bars reported ok, want unavailable")
	}
	if _, ok := VWAPZ(vwapBars(t0, 0, 30), t0, b[11].Timestamp); ok {
		t.Error("VWAPZ of a stock that never left VWAP reported ok, want unavailable")
	}
	// Bars before the open (the previous session) do not count.
	if _, ok := VWAPZ(b, t0.Add(time.Hour), t0.Add(2*time.Hour)); ok {
		t.Error("VWAPZ with no bar since the open reported ok")
	}
	b[11].Feature.VWAP = 0
	if _, ok := VWAPZ(b, t0, b[11].Timestamp); ok {
		t.Error("VWAPZ with the latest bar lacking VWAP reported ok")
	}
}

func TestVWAPZ_IgnoresBarsAfterDecisionTime(t *testing.T) {
	b := vwapBars(t0, 10, 30)
	b = append(b, domain.Snapshot{Timestamp: b[11].Timestamp.Add(time.Minute), Feature: domain.Feature{VWAP: 100, PriceVsVWAPBps: 500}})
	z, ok := VWAPZ(b, t0, b[11].Timestamp)
	if !ok || math.Abs(z-3) > 1e-9 {
		t.Fatalf("VWAPZ = %v, %v, want 3: a later bar must not be seen (FR-FE-1)", z, ok)
	}
}

func TestArm(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	for i := range 50 {
		if got := Arm(false, int64(i), at.Add(time.Duration(i)*time.Minute)); got != marketregime.ArmMomentum {
			t.Fatalf("Arm with the comparison off = %q, want momentum", got)
		}
	}

	if Arm(true, 7, at) != Arm(true, 7, at.Add(30*time.Second)) {
		t.Error("the same instrument and minute landed in different arms")
	}
	counts := map[string]int{}
	for i := range 2000 {
		counts[Arm(true, int64(i%40), at.Add(time.Duration(i)*time.Minute))]++
	}
	if counts[marketregime.ArmMomentum] < 800 || counts[marketregime.ArmRegime] < 800 {
		t.Errorf("arm split = %v over 2000 decisions, want roughly even", counts)
	}
}
