package labellayer_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest/manifesttest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

const cohort = "jev-v2"

var day = 24 * time.Hour

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

// candidate is a labelled LONG decision that clears every stage but the ones
// the arguments make it fail: quality, the model's verdict (via the OFI the
// test model scores) and the toxic-flow guard.
func candidate(id int64, at time.Time, quality string, modelTakes, toxic bool, net float64) labellayer.Row {
	ofi, tox := 5.0, 0.1
	if !modelTakes {
		ofi = -5
	}
	if toxic {
		tox = 0.9
	}
	return labellayer.Row{
		ID: id, DecidedAt: at, Cohort: cohort, Direction: domain.JevDirectionLong, NetReturnBps: net,
		Eligibility: eligibility.Input{
			ID: id, Labeled: true,
			Policy: policy.Input{
				SpreadBps: f(3), Turnover5mJPY: f(1e7),
				Decision: &domain.JevDecision{
					Direction: s(domain.JevDirectionLong), Confidence: f(0.9), EntryQuality: s(quality),
					ContinuationProbability: f(0.9), ToxicFlow: f(tox), LiquidityStressed: f(0.1),
				},
			},
			Features: metalabel.Features{OFIShort: f(ofi), OFIMid: f(1), OFILong: f(1), MicropriceDevBps: f(1), SpreadBps: f(3), Return1m: f(0.001), JevScore: f(0.5), TDnetScore: f(0)},
		},
	}
}

// First independent day: T0 + 10 days (09:00 JST onward).
func independentAt(d, minute int) time.Time {
	return manifesttest.T0.Add(time.Duration(10+d)*day + 9*time.Hour + time.Duration(minute)*time.Minute)
}

// dayNets are the known nets of the four candidates of each independent day.
var dayNets = [3][4]float64{{10, -20, -30, 40}, {20, 10, -10, -50}, {-5, 15, 25, -35}}

// syntheticRows is the independent population: per day r1 (strong, M take),
// r2 (fair, M take), r3 (strong, M skip), r4 (fair, M skip) and r5 (strong, M
// take, toxic flow: a common eligible candidate no cell takes), plus rows that
// are not candidates, not recomputable, reversion, not independent or reference.
func syntheticRows() []labellayer.Row {
	var rows []labellayer.Row
	id := int64(0)
	next := func() int64 { id++; return id }
	for d, nets := range dayNets {
		rows = append(rows,
			candidate(next(), independentAt(d, 0), "strong", true, false, nets[0]),
			candidate(next(), independentAt(d, 1), "fair", true, false, nets[1]),
			candidate(next(), independentAt(d, 2), "strong", false, false, nets[2]),
			candidate(next(), independentAt(d, 3), "fair", false, false, nets[3]),
			candidate(next(), independentAt(d, 4), "strong", true, true, 100),
		)
	}
	spread := candidate(next(), independentAt(0, 10), "strong", true, false, 77)
	spread.Eligibility.Policy.SpreadBps = f(50)
	unknown := candidate(next(), independentAt(1, 10), "strong", true, false, 66)
	unknown.Eligibility.Features.Return1m = nil
	none := candidate(next(), independentAt(1, 11), "strong", true, false, 55)
	none.Eligibility.Policy.Decision.Direction = s(domain.JevDirectionNone)
	rev := candidate(next(), independentAt(2, 10), "poor", true, false, 44)
	rev.Eligibility.Policy.Decision.MarketRegime = marketregime.Assessment{Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VWAPZ: f(3)}
	rows = append(rows, spread, unknown, none, rev)

	w := manifesttest.Valid().Windows
	trained := candidate(next(), w.TrainStart.Add(time.Hour), "strong", true, false, 999)
	gap := candidate(next(), w.OOSStart.Add(-time.Hour), "strong", true, false, 999)
	other := candidate(next(), independentAt(0, 20), "strong", true, false, 999)
	other.Cohort = "jev-v1"
	refTake := candidate(next(), w.OOSStart.Add(time.Hour), "strong", true, false, 12)
	refSkip := candidate(next(), w.OOSStart.Add(2*time.Hour), "strong", false, false, -8)
	return append(rows, trained, gap, other, refTake, refSkip)
}

func testModel() *metalabel.Model {
	m := policytest.LRModel(0, 1) // sigmoid(OFI short): +5 takes, -5 skips
	return &m
}

func frozenManifest(t *testing.T, change func(*manifest.Manifest)) (manifest.Manifest, string) {
	t.Helper()
	m := manifesttest.Valid()
	m.Model = manifest.ModelRef{Version: testModel().Version, ParamsSHA256: manifest.ParamsHash("stored-params")}
	if change != nil {
		change(&m)
	}
	if err := m.Freeze(m.CreatedAt); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return m, manifest.ParamsHash("stored-params")
}

func input(t *testing.T) labellayer.Input {
	t.Helper()
	m, h := frozenManifest(t, nil)
	return labellayer.Input{Manifest: m, Model: testModel(), ParamsSHA256: h, Cohort: cohort, Rows: syntheticRows()}
}
