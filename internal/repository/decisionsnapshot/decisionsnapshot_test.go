package decisionsnapshot_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisionsnapshot"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func TestRepository_EachInRange(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	pairs := decisionsnapshot.New(db)
	base := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)

	toyota, err := instruments.Create(ctx, domain.Instrument{Symbol: "7203", Name: "トヨタ", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	sony, err := instruments.Create(ctx, domain.Instrument{Symbol: "6758", Name: "ソニー", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	snap := func(inst domain.Instrument, at time.Time, price float64) domain.Snapshot {
		s, err := snapshots.Insert(ctx, domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, Price: price, Volume: 1, Turnover: price,
			Feature: domain.Feature{VWAP: price, Return5m: new(0.004)}, RawDataJSON: `{"raw":true}`,
		})
		if err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
		return s
	}
	decide := func(inst domain.Instrument, at time.Time) domain.JevDecision {
		d, err := decisions.Insert(ctx, domain.JevDecision{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatalf("insert decision: %v", err)
		}
		return d
	}

	// Same minute, two instruments: each decision must get its own bar.
	toyotaBar := snap(toyota, base, 2000)
	sonyBar := snap(sony, base, 13000)
	snap(toyota, base.Add(time.Minute), 2001) // a later bar no decision was made on
	toyotaDecision := decide(toyota, base)
	sonyDecision := decide(sony, base)
	decide(toyota, base.Add(5*time.Minute)) // decision whose bar does not exist
	decide(toyota, base.Add(time.Hour))     // outside the period

	var got []decisionsnapshot.Pair
	collect := func(d decisionsnapshot.Pair) error { got = append(got, d); return nil }
	if err := pairs.EachInRange(ctx, base, base.Add(30*time.Minute), collect); err != nil {
		t.Fatalf("EachByDecisionRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("pairs = %+v, want the two decisions that have a bar in the period", got)
	}
	want := map[int64]domain.Snapshot{toyotaDecision.ID: toyotaBar, sonyDecision.ID: sonyBar}
	for _, g := range got {
		w, ok := want[g.DecisionID]
		if !ok || g.Snapshot.ID != w.ID || g.Snapshot.Price != w.Price || g.Snapshot.Symbol != w.Symbol {
			t.Errorf("decision %d paired with snapshot %+v, want %+v", g.DecisionID, g.Snapshot, w)
		}
		if g.Snapshot.Feature.Return5m == nil || *g.Snapshot.Feature.Return5m != 0.004 {
			t.Errorf("decision %d features not read: %+v", g.DecisionID, g.Snapshot.Feature)
		}
		if g.Snapshot.RawDataJSON != "" {
			t.Errorf("decision %d: raw_data_json exported: %q", g.DecisionID, g.Snapshot.RawDataJSON)
		}
	}

	stop := errors.New("stop")
	if err := pairs.EachInRange(ctx, base, base.Add(30*time.Minute), func(decisionsnapshot.Pair) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("err = %v, want it to wrap fn's error", err)
	}
}
