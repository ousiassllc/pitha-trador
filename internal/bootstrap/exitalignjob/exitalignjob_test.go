package exitalignjob_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/exitalignjob"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	handler "github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

// The Calibration handler detects the alignment view by type assertion, so a
// CalibrationSource that stops satisfying AlignmentSource would silently turn
// the endpoint into an empty table.
var _ handler.AlignmentSource = exitalignjob.CalibrationSource{}

// The real wiring end to end: the source reads the database (decision, 1-minute
// bars, triple-barrier label, closed paper position, newest model), the cost
// setting stored in runtime_settings and the Execution Config's exit rules,
// and `GET /api/v1/calibration/exit-alignment` shows the hand-computed result.
func TestRouter_ServesExitAlignmentOfDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// One LONG decision made two hours ago, labeled by its take-profit at +30
	// bps net. The bars then run to +45 bps, peak at +80 bps (minute 12) and
	// retrace to 1001.9, where the exit's 0.5 % trailing stop (peak 1008 ->
	// 1002.96) triggers at minute 14: +19 bps gross.
	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	decision, err := judgement.NewDecisionRepository(db).Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: t0, CreatedAt: t0, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", Direction: new(domain.JevDirectionLong), ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	var bars []domain.Snapshot
	for m := 0; m <= 25; m++ {
		price := 1001.9
		switch {
		case m < 4:
			price = 1000
		case m < 12:
			price = 1004.5
		case m < 14:
			price = 1008
		}
		bars = append(bars, domain.Snapshot{InstrumentID: inst.ID, Symbol: "7203", Timestamp: t0.Add(time.Duration(m) * time.Minute), Price: price, Bid: new(price), Ask: new(price)})
	}
	if _, err := market.NewSnapshotRepository(db).InsertBatch(ctx, bars); err != nil {
		t.Fatalf("insert snapshots: %v", err)
	}
	if _, err := calibration.NewCalibrationRepository(db).Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 10, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		BarrierHit: domain.BarrierHitTakeProfit, NetReturnBps: new(30.0), EntryRefPrice: new(1000.0),
	}); err != nil {
		t.Fatalf("insert outcome: %v", err)
	}

	// A paper position closed by its trailing stop at +25 bps of its notional,
	// inside the OOS window of the newest model (EV +12 bps).
	orders, positions := trading.NewOrderRepository(db), trading.NewPositionRepository(db)
	entry, err := orders.Insert(ctx, domain.PaperOrder{InstrumentID: inst.ID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: t0})
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if entry, err = orders.Fill(ctx, entry.ID, 2000, 0, nil, t0); err != nil {
		t.Fatalf("fill order: %v", err)
	}
	opened, err := positions.Open(ctx, domain.Position{InstrumentID: inst.ID, EntryOrderID: entry.ID, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2000, CurrentPrice: 2000, OpenedAt: t0})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	if _, err := positions.Close(ctx, opened.ID, entry.ID, 2000, 500, domain.ExitReasonTrailingStop, t0.Add(30*time.Minute)); err != nil {
		t.Fatalf("close position: %v", err)
	}
	if _, err := metalabel.NewRepository(db).InsertModel(ctx, metalabel.Model{
		Version: "v1", Algorithm: "logistic", Status: metalabel.ModelActive, TrainedAt: t0.Add(time.Hour),
		TrainStart: t0.AddDate(0, 0, -30), TrainEnd: t0.Add(-time.Hour), OOSStart: t0.Add(-time.Hour), OOSEnd: t0.Add(time.Hour),
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier, FeatureNames: []string{"ofi_short"}, ParamsJSON: `{}`,
		TrainSamples: 100, OOSSamples: 50, OOSExpectedValueBps: new(12.0),
	}); err != nil {
		t.Fatalf("InsertModel: %v", err)
	}

	// A 2 bps fee each way raises the label cost from the default 10 bps to 14.
	settings := system.NewRuntimeSettingsRepository(db)
	if err := settings.Set(ctx, costsettings.KeyFeeBps, "2", time.Now()); err != nil {
		t.Fatalf("Set fee: %v", err)
	}

	exit := execution.Config{TrailingStopPct: 0.5, MaxHoldingMinutes: 20}
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithCalibrationSource(exitalignjob.NewCalibrationSource(nil, db, settings, exit)))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/calibration/exit-alignment?days=30", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	type reasonCount struct {
		Reason string `json:"reason"`
		Count  int    `json:"count"`
	}
	type distribution struct {
		Count int     `json:"count"`
		Mean  float64 `json:"mean"`
	}
	var body struct {
		Rows       []struct{ Key string } `json:"rows"`
		Comparison struct {
			HorizonMinutes int           `json:"horizon_minutes"`
			ReplayedCount  int           `json:"replayed_count"`
			LabelNetBps    distribution  `json:"label_net_bps"`
			ReplayNetBps   distribution  `json:"replay_net_bps"`
			DiffBps        distribution  `json:"diff_bps"`
			LabelTPCount   int           `json:"label_tp_count"`
			ReplayExits    []reasonCount `json:"replay_exits"`
			PaperExits     []reasonCount `json:"paper_exits"`
			CostLongBps    float64       `json:"cost_long_bps"`
		} `json:"comparison"`
		OOSPaper struct {
			ModelVersion string   `json:"model_version"`
			OOSEVBps     *float64 `json:"oos_ev_bps"`
			PaperTrades  int      `json:"paper_trades"`
			PaperMeanBps *float64 `json:"paper_mean_bps"`
			DiffBps      *float64 `json:"diff_bps"`
		} `json:"oos_paper"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

	if len(body.Rows) != 5 {
		t.Errorf("rows = %d, want the 5 label-versus-exit rows", len(body.Rows))
	}
	c := body.Comparison
	if c.HorizonMinutes != 10 || c.ReplayedCount != 1 || c.LabelTPCount != 1 {
		t.Errorf("comparison horizon/replayed/tp = %d/%d/%d, want 10/1/1", c.HorizonMinutes, c.ReplayedCount, c.LabelTPCount)
	}
	// Replay: +19 bps gross less the 14 bps label cost = +5; the label says +30.
	if !near(c.LabelNetBps.Mean, 30) || !near(c.ReplayNetBps.Mean, 5) || !near(c.DiffBps.Mean, -25) || !near(c.CostLongBps, 14) {
		t.Errorf("label/replay/diff/cost = %v/%v/%v/%v, want 30/5/-25/14", c.LabelNetBps.Mean, c.ReplayNetBps.Mean, c.DiffBps.Mean, c.CostLongBps)
	}
	if len(c.ReplayExits) != 1 || c.ReplayExits[0] != (reasonCount{Reason: domain.ExitReasonTrailingStop, Count: 1}) {
		t.Errorf("replay exits = %+v, want one trailing_stop (the configured exit rule)", c.ReplayExits)
	}
	if len(c.PaperExits) != 1 || c.PaperExits[0] != (reasonCount{Reason: domain.ExitReasonTrailingStop, Count: 1}) {
		t.Errorf("paper exits = %+v, want the closed position's trailing_stop", c.PaperExits)
	}
	o := body.OOSPaper
	if o.ModelVersion != "v1" || o.OOSEVBps == nil || *o.OOSEVBps != 12 || o.PaperTrades != 1 || o.PaperMeanBps == nil || !near(*o.PaperMeanBps, 25) || o.DiffBps == nil || !near(*o.DiffBps, 13) {
		t.Errorf("oos_paper = %+v, want v1, EV 12, 1 paper trade averaging 25 bps, diff 13", o)
	}
}
