package dataexport_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisionsnapshot"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// Secrets planted in every free-text / JSON column that can carry one; no
// archive may contain any of them (issue #770/#771 redaction).
const (
	secretToken    = "tok-LEAK-1234567890"
	secretAPIKey   = "key-LEAK-abcdef"
	secretPassword = "hunter2-LEAK"
	secretSecond   = "second-LEAK-9999"
	secretBearer   = "bearer-LEAK-zzz"
)

var (
	t0       = time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC) // 09:30 JST
	period   = dataexport.Period{From: t0.Add(-time.Hour), To: t0.Add(2 * time.Hour)}
	allKinds = []dataexport.Kind{dataexport.KindTrades, dataexport.KindDecisions}
)

type env struct {
	svc          *dataexport.Service
	decisionID   int64
	skippedID    int64
	instrumentID int64
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	instrumentID := insertInstrument(t, db, "7203")
	decisions := judgement.NewDecisionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	pairs := decisionsnapshot.New(db)
	signals := trading.NewSignalRepository(db)
	orders := trading.NewOrderRepository(db)
	positions := trading.NewPositionRepository(db)
	labels := calibration.NewCalibrationRepository(db)

	if _, err := snapshots.Insert(ctx, domain.Snapshot{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: t0, Price: 2110, Volume: 1000, Turnover: 2_110_000,
		Feature:     domain.Feature{VWAP: 2100, PriceVsVWAPBps: 47.6, Return5m: new(0.004)},
		RawDataJSON: `{"Authorization":"Bearer ` + secretBearer + `"}`,
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	confidence := 0.82
	dir := domain.JevDirectionLong
	decision, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: t0, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "h1", QuestionVersion: "trader-v3", LatencyMs: 842, ModelID: "jev-1",
		StateJSON:    `{"symbol":"7203","price":2110,"api_key":"` + secretAPIKey + `"}`,
		ResponseJSON: `{"direction":"LONG","confidence":0.82,"note":"Authorization: Bearer ` + secretBearer + `","sSecondPassword":"` + secretSecond + `"}`,
		Direction:    &dir, Confidence: &confidence,
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	// A second decision with no snapshot bar, no outcome, only a skip label
	// and a rejected signal.
	skipped, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: t0.Add(30 * time.Minute), DecisionType: domain.JevDecisionTypeScout,
		StateHash: "h2", StateJSON: `{}`, QuestionVersion: "scout-v1", ResponseJSON: `not json: token=` + secretToken, ModelID: "jev-1",
	})
	if err != nil {
		t.Fatalf("insert skipped decision: %v", err)
	}

	reject := "risk rejected: password=" + secretPassword
	signal, err := signals.Insert(ctx, domain.TradeSignal{
		InstrumentID: instrumentID, JevDecisionID: &decision.ID, Symbol: "7203", Timestamp: t0, Direction: domain.JevDirectionLong,
		Score: &confidence, PolicyVersion: "policy-v1", RiskPassed: true,
	})
	if err != nil {
		t.Fatalf("insert signal: %v", err)
	}
	if _, err := signals.Insert(ctx, domain.TradeSignal{
		InstrumentID: instrumentID, JevDecisionID: &skipped.ID, Symbol: "7203", Timestamp: t0.Add(30 * time.Minute),
		Direction: domain.JevDirectionNone, PolicyVersion: "policy-v1", RejectReason: &reject,
	}); err != nil {
		t.Fatalf("insert rejected signal: %v", err)
	}

	entry, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, TradeSignalID: &signal.ID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: t0,
	})
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	if _, err := orders.Fill(ctx, entry.ID, 2111, 55, new(1.5), t0.Add(time.Second)); err != nil {
		t.Fatalf("fill entry: %v", err)
	}
	pos, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2111, CurrentPrice: 2111, OpenedAt: t0.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	exit, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideSell, OrderType: domain.OrderTypeMarket,
		Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: t0.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("insert exit: %v", err)
	}
	if _, err := orders.Fill(ctx, exit.ID, 2121, 55, nil, t0.Add(10*time.Minute)); err != nil {
		t.Fatalf("fill exit: %v", err)
	}
	if _, err := positions.Close(ctx, pos.ID, exit.ID, 2121, 1000, "take_profit token="+secretToken, t0.Add(10*time.Minute)); err != nil {
		t.Fatalf("close position: %v", err)
	}

	if _, err := labels.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.003, MaxAdverseExcursion: -0.001,
		MaxFavorableExcursion: 0.004, WasDirectionCorrect: new(true), LabelVersion: 1,
	}); err != nil {
		t.Fatalf("insert outcome: %v", err)
	}
	if err := labels.MarkUnlabelable(ctx, skipped.ID, 15, "no bars after lunch break"); err != nil {
		t.Fatalf("mark unlabelable: %v", err)
	}

	svc := dataexport.New(
		dataexport.Trades{Orders: orders, Signals: signals, Closed: decisiontrade.New(db)},
		dataexport.Decisions{Decisions: decisions, Features: pairs, Signals: signals, Labels: labels},
	)
	return env{svc: svc, decisionID: decision.ID, skippedID: skipped.ID, instrumentID: instrumentID}
}

func insertInstrument(t *testing.T, db *sql.DB, symbol string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, 'トヨタ自動車', 'TSE Prime', 1)`, symbol)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	return id
}

// export runs one export and returns the manifest and the archive entries.
func export(t *testing.T, e env, req dataexport.Request) (dataexport.Manifest, map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	manifest, err := e.svc.Export(context.Background(), &buf, req)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	files := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = string(b)
	}
	return manifest, files
}

func csvRecords(t *testing.T, content string) []map[string]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(content)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v\n%s", err, content)
	}
	var out []map[string]string
	for _, r := range recs[1:] {
		row := make(map[string]string, len(r))
		for i, h := range recs[0] {
			row[h] = r[i]
		}
		out = append(out, row)
	}
	return out
}

func jsonlRecords(t *testing.T, content string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("parse jsonl line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func values(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
