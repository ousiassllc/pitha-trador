package runnertest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	o   Options
	db  *sql.DB
}

func ptr[T any](v T) *T { return &v }

func (f *fixture) check(what string, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("fixture %s: %v", what, err)
	}
}

// at is minute m after 09:00 JST of day n.
func at(n, m int) time.Time { return Day(n).Add(9*time.Hour + time.Duration(m)*time.Minute) }

// Minutes after 09:00 of the eight decisions of an evaluation day (the morning
// and the afternoon session) and of the bars.
var decisionMinutes = []int{10, 40, 70, 100, 220, 250, 280, 310}

func barMinutes() []int {
	var ms []int
	for m := 0; m < 150; m++ {
		ms = append(ms, m)
	}
	for m := 210; m < 330; m++ {
		ms = append(ms, m)
	}
	return ms
}

func (f *fixture) instrument() {
	_, err := market.NewInstrumentRepository(f.db).Create(f.ctx, domain.Instrument{Symbol: "7203", Name: "synthetic", Market: "TSE Prime", IsActive: true})
	f.check("instrument", err)
}

// spec is one synthetic decision: i is its index within the day; even indices
// are "good" quality and odd "fair" (the Q axis); i%4 < 2 has a positive order
// flow the model takes and a winning label, the others a negative one it skips.
type spec struct {
	day, minute int
	quality     string
	version     string
	ofi         float64
	label       float64
	labeled     bool
}

func (s spec) win() bool { return s.ofi > 0 }

func daySpecs(day int, version string) []spec {
	var out []spec
	for i, m := range decisionMinutes {
		s := spec{day: day, minute: m, quality: domain.JevEntryQualityGood, version: version, ofi: 2, label: 20, labeled: true}
		if i%2 == 1 {
			s.quality = domain.JevEntryQualityFair
		}
		if i%4 >= 2 {
			s.ofi, s.label = -2, -25
		}
		out = append(out, s)
	}
	return out
}

// decisions inserts the training (days 1 and 3), original OOS (6, 7) and
// evaluation decisions with their labels, and one evaluation-window decision of
// another cohort.
func (f *fixture) decisions() {
	var specs []spec
	for _, d := range []int{1, 3, 6, 7} {
		specs = append(specs, daySpecs(d, "trader-v3")[:4]...)
	}
	for _, d := range EvalDays {
		specs = append(specs, daySpecs(d, "trader-v3")...)
	}
	specs = append(specs, spec{day: 10, minute: 340, quality: domain.JevEntryQualityGood, version: "trader-v4", ofi: 2, label: 20, labeled: true})
	decisions, labels := judgement.NewDecisionRepository(f.db), calibration.NewCalibrationRepository(f.db)
	for _, s := range specs {
		resp := fmt.Sprintf(`{"direction":"LONG","regime":"trend","entry_quality":%q,"toxic_flow":0.1,"liquidity_stressed":0.1,"continuation_probability":0.9,"confidence":0.8}`, s.quality)
		d, err := decisions.Insert(f.ctx, domain.JevDecision{
			InstrumentID: 1, Symbol: "7203", Timestamp: at(s.day, s.minute), DecisionType: domain.JevDecisionTypeTrader,
			StateHash: fmt.Sprintf("h%d-%d", s.day, s.minute), StateJSON: `{"spread_bps":3,"return_1m":0.001}`,
			QuestionVersion: s.version, ResponseJSON: resp, Direction: ptr(domain.JevDirectionLong), Confidence: ptr(0.8), ModelID: "m1",
			OrderFlow: domain.OrderFlowFeatures{OFIShort: ptr(s.ofi), OFIMid: ptr(s.ofi), OFILong: ptr(s.ofi), MicropriceDevBps: ptr(s.ofi)},
		})
		f.check("decision", err)
		if !s.labeled {
			continue
		}
		hit := domain.BarrierHitTakeProfit
		if !s.win() {
			hit = domain.BarrierHitStopLoss
		}
		_, err = labels.Insert(f.ctx, domain.CalibrationOutcome{
			JevDecisionID: d.ID, HorizonMinutes: 10, BarrierHit: hit, NetReturnBps: ptr(s.label), LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		})
		f.check("label", err)
	}
}

// bars inserts the evaluation days' 1-minute snapshots as look-ahead-safe bars
// (features computed by featureengine from the preceding bars). A win at
// minute m lifts the price to 1015 for four bars from m+2, a loss lowers it to
// 990, so a winning entry exits by take profit and a losing one by stop loss.
func (f *fixture) bars() {
	if f.o.NoSnapshots {
		return
	}
	ofiAt := map[[2]int]float64{}
	price := map[[2]int]float64{}
	for _, d := range EvalDays {
		for _, s := range daySpecs(d, "") {
			ofiAt[[2]int{d, s.minute}] = s.ofi
			level := 1015.0
			if !s.win() {
				level = 990
			}
			for k := 2; k < 6; k++ {
				price[[2]int{d, s.minute + k}] = level
			}
		}
	}
	snaps := market.NewSnapshotRepository(f.db)
	var series []domain.Snapshot
	for _, d := range EvalDays {
		if d == f.o.MissingBarDay {
			continue
		}
		for _, m := range barMinutes() {
			key := [2]int{d, m}
			p := 1000.0
			if v, ok := price[key]; ok {
				p = v
			}
			ofi := ofiAt[key]
			ts := at(d, m)
			cum := float64(m + 1)
			feature := featureengine.Compute(featureengine.Input{
				Timestamp: ts, Current: featureengine.Reading{Price: p, VWAP: p, Volume: int64(cum) * 5000, Turnover: cum * 5e6, Bid: ptr(p), Ask: ptr(p)},
				History: series[max(0, len(series)-featureengine.HistoryLookbackBars):],
			})
			feature.OrderFlowFeatures = domain.OrderFlowFeatures{OFIShort: ptr(ofi), OFIMid: ptr(ofi), OFILong: ptr(ofi), MicropriceDevBps: ptr(ofi)}
			s := domain.Snapshot{
				InstrumentID: 1, Symbol: "7203", Timestamp: ts, Price: p, Bid: ptr(p), Ask: ptr(p), SpreadBps: ptr(3.0),
				Volume: int64(cum) * 5000, Turnover: cum * 5e6, Feature: feature,
			}
			stored, err := snaps.Insert(f.ctx, s)
			f.check("snapshot", err)
			series = append(series, stored)
		}
	}
}

// model stores a rejected model with parameters scoring p = sigmoid(1.5 *
// ofi_short + bias): a positive order flow is taken, a negative one skipped.
func (f *fixture) model(version string, trainedAt time.Time, bias float64) string {
	return f.modelSpec(ModelSpec{Version: version, TrainedAt: trainedAt, Bias: bias, ParamsJSON: f.o.ParamsJSON})
}

func (f *fixture) modelSpec(s ModelSpec) string {
	version, trainedAt := s.Version, s.TrainedAt
	lr := policytest.LRModel(s.Bias, 1.5)
	params, err := lr.ParamsJSON()
	f.check("model params", err)
	if s.ParamsJSON != "" {
		params = s.ParamsJSON
	}
	oosEnd := Day(8)
	if !s.OOSEnd.IsZero() {
		oosEnd = s.OOSEnd
	}
	m := repo.Model{
		Version: version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: repo.ModelRejected, RejectReason: "adoption bar not met", TrainedAt: trainedAt,
		TrainStart: Day(0), TrainEnd: Day(5), OOSStart: Day(6), OOSEnd: oosEnd,
		TrainSamples: 8, OOSSamples: 8, FeatureNames: metalabel.FeatureNames(), ParamsJSON: params,
	}
	if !f.o.NoCohort {
		m.Adoption.TrainedCohort = "entry-quality-v3"
	}
	_, err = repo.NewRepository(f.db).InsertModel(f.ctx, m)
	f.check("model", err)
	return version
}
