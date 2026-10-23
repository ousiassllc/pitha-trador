package bucketscope_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

type noTrades struct{}

func (noTrades) List(context.Context) ([]domain.DecisionTrade, error) { return nil, nil }

func ptr[T any](v T) *T { return &v }

// With the live version's cohort scoped on the outcome repository,
// BucketHasSamples counts that cohort only; the service over AllVersions() is the
// one count over every version (Paper's fallback, policy/calibrationgate).
// trader-v1..v3 are one cohort (3 samples), trader-v4 another (1).
func TestService_BucketHasSamples_CountsTheLiveVersionsCohortAndAllVersionsOnRequest(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := calrepo.NewCalibrationRepository(db)
	base := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	for i, version := range []string{"trader-v1", "trader-v3", "trader-v3", "trader-v4"} {
		d, err := judgement.NewDecisionRepository(db).Insert(ctx, domain.JevDecision{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: base.Add(time.Duration(i) * time.Minute),
			DecisionType: domain.JevDecisionTypeTrader, StateHash: version + string(rune('a'+i)), StateJSON: "{}",
			QuestionVersion: version, ResponseJSON: "{}", Direction: ptr(domain.JevDirectionLong), Confidence: ptr(0.65), ModelID: "m",
		})
		if err != nil {
			t.Fatalf("insert %s decision: %v", version, err)
		}
		if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{JevDecisionID: d.ID, HorizonMinutes: 5, WasDirectionCorrect: ptr(true)}); err != nil {
			t.Fatalf("insert outcome: %v", err)
		}
	}
	svc, all := calibration.NewService(outcomes, noTrades{}), calibration.NewService(outcomes.AllVersions(), noTrades{})

	for _, tc := range []struct {
		live                  string
		min                   int
		wantOwn, wantAllCount bool
	}{
		{"trader-v3", 3, true, true},
		{"trader-v3", 4, false, true},
		{"trader-v3", 5, false, false},
		{"trader-v4", 1, true, true},
		{"trader-v4", 2, false, true},
		{"trader-v4", 4, false, true},
		{"trader-v4", 5, false, false},
	} {
		live := tc.live
		outcomes.SetForeignTraderVersions(func(context.Context) ([]string, error) { return jev.ForeignVersions(live), nil })
		if own, err := svc.BucketHasSamples(ctx, 0.65, tc.min); err != nil || own != tc.wantOwn {
			t.Errorf("live %s: BucketHasSamples(min %d) = %v, %v, want %v", tc.live, tc.min, own, err, tc.wantOwn)
		}
		if all, err := all.BucketHasSamples(ctx, 0.65, tc.min); err != nil || all != tc.wantAllCount {
			t.Errorf("live %s: AllVersions BucketHasSamples(min %d) = %v, %v, want %v", tc.live, tc.min, all, err, tc.wantAllCount)
		}
	}

	outcomes.SetForeignTraderVersions(func(context.Context) ([]string, error) { return nil, errors.New("settings unreadable") })
	if _, err := svc.BucketHasSamples(ctx, 0.65, 1); err == nil {
		t.Error("BucketHasSamples with an unreadable live version: want an error, never a pass")
	}
}
