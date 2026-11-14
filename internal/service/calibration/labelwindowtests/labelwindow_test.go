// Package labelwindowtests checks that calibration.LabelWindow, the pure label
// the read-only jev_evaluation tool reuses (issue #1073), is what
// calibration.Labeler.Compute labels with. It is apart from the calibration
// package's own tests to keep that directory within its size limit.
package labelwindowtests

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

// bar is one market_snapshots row: the last price plus the quotes (0: NULL).
type bar struct{ price, bid, ask float64 }

// For every shape of window (take-profit, stop-loss, timeout, NONE, no entry
// quote, data ending short, no bar after the entry) LabelWindow and Compute
// return the same outcome (but for the decision id, which only Compute knows)
// and the same gap reason.
func TestLabelWindowEqualsCompute(t *testing.T) {
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	const m = time.Minute
	cases := map[string]struct {
		direction string
		bars      map[time.Duration]bar
	}{
		"long take profit": {domain.JevDirectionLong, map[time.Duration]bar{
			0: {999, 998, 1000}, 1 * m: {1002, 1001, 1003}, 2 * m: {1004.5, 1004, 1005}, 3 * m: {900, 899, 901}, 5 * m: {1000, 999, 1001},
		}},
		"short stop loss": {domain.JevDirectionShort, map[time.Duration]bar{
			0: {1001, 1000, 1002}, 1 * m: {1002.5, 1002, 1003}, 5 * m: {990, 989, 991},
		}},
		"long timeout": {domain.JevDirectionLong, map[time.Duration]bar{
			0: {1000, 999.5, 1000}, 2 * m: {1001, 1000.5, 1001}, 5 * m: {1003, 1002.5, 1003},
		}},
		"none has no barrier fields": {domain.JevDirectionNone, map[time.Duration]bar{0: {1000, 0, 0}, 5 * m: {1003, 0, 0}}},
		"no executable ask for a long": {domain.JevDirectionLong, map[time.Duration]bar{
			0: {1000, 999, 0}, 5 * m: {1003, 1002, 0},
		}},
		"data ends short of the horizon": {domain.JevDirectionLong, map[time.Duration]bar{
			0: {1000, 999, 1000}, 1 * m: {1001, 1000, 1001},
		}},
		"no bar after the entry": {domain.JevDirectionShort, map[time.Duration]bar{0: {1000, 999, 1000}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "T", Market: "TSE Prime", IsActive: true})
			if err != nil {
				t.Fatal(err)
			}
			decisions, snapshots := judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db)
			decision, err := decisions.Insert(ctx, domain.JevDecision{
				InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: base, DecisionType: domain.JevDecisionTypeTrader,
				StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", Direction: new(tc.direction),
				Confidence: new(0.7), ModelID: "m", CreatedAt: base,
			})
			if err != nil {
				t.Fatal(err)
			}
			var snaps []domain.Snapshot
			for offset, b := range tc.bars {
				s := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: base.Add(offset), Price: b.price}
				if b.bid > 0 {
					s.Bid = new(b.bid)
				}
				if b.ask > 0 {
					s.Ask = new(b.ask)
				}
				snaps = append(snaps, s)
			}
			if _, err := snapshots.InsertBatch(ctx, snaps); err != nil {
				t.Fatal(err)
			}

			labeler := calibration.NewLabeler(decisions, snapshots, calrepo.NewCalibrationRepository(db))
			_, want, wantGap, err := labeler.Compute(ctx, decision.ID, 5)
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			window, err := snapshots.ListByInstrumentRange(ctx, inst.ID, base, base.Add(calibration.EntryWaitTolerance+5*m+time.Second))
			if err != nil {
				t.Fatal(err)
			}
			got, gotGap := calibration.LabelWindow(window, calibration.ResponseTime(decision), tc.direction, 5, barriersettings.Default())
			if gotGap != wantGap {
				t.Fatalf("gap = %q, Compute's %q", gotGap, wantGap)
			}
			got.JevDecisionID = want.JevDecisionID
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("LabelWindow = %+v, Compute = %+v", got, want)
			}
			if name == "long take profit" && (got.BarrierHit != domain.BarrierHitTakeProfit || got.NetReturnBps == nil) {
				t.Fatalf("fixture does not exercise a take-profit: %+v", got)
			}
			if name == "short stop loss" && got.BarrierHit != domain.BarrierHitStopLoss {
				t.Fatalf("fixture does not exercise a stop-loss: %+v", got)
			}
		})
	}
}
