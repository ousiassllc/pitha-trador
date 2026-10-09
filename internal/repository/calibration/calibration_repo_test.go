package calibration_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
)

// newCalibrationFixtures opens a fresh test database with one instrument
// already created, for calibration_repo_test.go's fixtures.
func newCalibrationFixtures(t *testing.T) (*calibration.CalibrationRepository, *judgement.DecisionRepository, int64) {
	t.Helper()
	db := newTestDB(t)
	instID := insertInstrument(t, db, "7203", "トヨタ自動車")
	return calibration.NewCalibrationRepository(db), judgement.NewDecisionRepository(db), instID
}

// insertTraderDecision inserts a decision_type=trader jev_decisions row.
func insertTraderDecision(t *testing.T, decisions *judgement.DecisionRepository, instID int64, timestamp time.Time, direction string) domain.JevDecision {
	t.Helper()
	confidence := 0.82
	saved, err := decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instID, Symbol: "7203", Timestamp: timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
		Direction: &direction, Confidence: &confidence, ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert trader decision: %v", err)
	}
	return saved
}

func TestCalibrationRepository_InsertAndGet(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		want      *bool
	}{
		{"direction correct", domain.JevDirectionLong, ptr(true)},
		{"direction none", domain.JevDirectionNone, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcomes, decisions, instID := newCalibrationFixtures(t)
			ctx := context.Background()
			decision := insertTraderDecision(t, decisions, instID, time.Now().UTC(), tc.direction)

			created, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
				JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.41,
				MaxAdverseExcursion: -0.1, MaxFavorableExcursion: 0.55, WasDirectionCorrect: tc.want,
			})
			if err != nil {
				t.Fatalf("Insert: %v", err)
			}
			if created.ID == 0 || created.CreatedAt.IsZero() {
				t.Fatalf("Insert() = %+v, want ID/CreatedAt populated", created)
			}

			got, err := outcomes.Get(ctx, created.ID)
			if err != nil {
				t.Fatalf("Get(%d): %v", created.ID, err)
			}
			if got.FutureReturn != 0.41 || (got.WasDirectionCorrect == nil) != (tc.want == nil) {
				t.Fatalf("Get(%d) = %+v, want FutureReturn=0.41 and WasDirectionCorrect nil=%v", created.ID, got, tc.want == nil)
			}
			if tc.want != nil && *got.WasDirectionCorrect != *tc.want {
				t.Fatalf("Get(%d).WasDirectionCorrect = %v, want %v", created.ID, *got.WasDirectionCorrect, *tc.want)
			}
		})
	}
}

func TestCalibrationRepository_InsertDuplicateDecisionHorizonFails(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	decision := insertTraderDecision(t, decisions, instID, time.Now().UTC(), domain.JevDirectionLong)
	outcome := domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.1, WasDirectionCorrect: ptr(true),
	}
	if _, err := outcomes.Insert(ctx, outcome); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if _, err := outcomes.Insert(ctx, outcome); err == nil {
		t.Fatalf("second Insert with the same (jev_decision_id, horizon_minutes) succeeded, want a UNIQUE constraint error")
	}
}

func TestCalibrationRepository_PendingLabels(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	now := base.Add(6 * time.Minute)

	// due: 5-minute horizon elapsed, not yet labeled.
	due := insertTraderDecision(t, decisions, instID, base, domain.JevDirectionLong)
	// notDueYet: horizon has not elapsed.
	notDueYet := insertTraderDecision(t, decisions, instID, now.Add(-1*time.Minute), domain.JevDirectionLong)
	// alreadyLabeled: due, but already has a calibration_outcomes row.
	alreadyLabeled := insertTraderDecision(t, decisions, instID, base, domain.JevDirectionLong)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: alreadyLabeled.ID, HorizonMinutes: 5, FutureReturn: 0.1, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed existing outcome: %v", err)
	}
	// scoutDecision: decision_type=scout, must never be labeled.
	if _, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: instID, Symbol: "7203", Timestamp: base, DecisionType: domain.JevDecisionTypeScout,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
	}); err != nil {
		t.Fatalf("insert scout decision: %v", err)
	}

	pending, err := outcomes.PendingLabels(ctx, []int{5}, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("PendingLabels: %v", err)
	}
	ids := make(map[int64]bool)
	for _, p := range pending {
		ids[p.JevDecisionID] = true
	}
	if !ids[due.ID] || ids[notDueYet.ID] || ids[alreadyLabeled.ID] {
		t.Fatalf("PendingLabels = %+v, want only due decision %d (not notDueYet %d or alreadyLabeled %d)", pending, due.ID, notDueYet.ID, alreadyLabeled.ID)
	}
}

func TestCalibrationRepository_ListLabeledSamples(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()

	longCorrect := insertTraderDecision(t, decisions, instID, now, domain.JevDirectionLong)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: longCorrect.ID, HorizonMinutes: 5, FutureReturn: 0.41, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed long outcome: %v", err)
	}
	// NONE-direction decisions have no predicted direction to grade and
	// must be excluded from the aggregation samples.
	noneDecision := insertTraderDecision(t, decisions, instID, now.Add(time.Minute), domain.JevDirectionNone)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: noneDecision.ID, HorizonMinutes: 5, FutureReturn: 0.10,
	}); err != nil {
		t.Fatalf("seed none outcome: %v", err)
	}

	samples, err := outcomes.ListLabeledSamples(ctx, nil)
	if err != nil {
		t.Fatalf("ListLabeledSamples: %v", err)
	}
	if len(samples) != 1 || samples[0].Direction != domain.JevDirectionLong || samples[0].Confidence != 0.82 ||
		samples[0].FutureReturn != 0.41 || !samples[0].WasDirectionCorrect {
		t.Fatalf("ListLabeledSamples() = %+v, want exactly the LONG outcome", samples)
	}
}

func TestCalibrationRepository_ListLabeledSamplesSince(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	cutoff := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

	old := insertTraderDecision(t, decisions, instID, cutoff.Add(-time.Hour), domain.JevDirectionLong)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: old.ID, HorizonMinutes: 5, FutureReturn: 0.41, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed old outcome: %v", err)
	}
	recent := insertTraderDecision(t, decisions, instID, cutoff.Add(time.Hour), domain.JevDirectionShort)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: recent.ID, HorizonMinutes: 5, FutureReturn: -0.30, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed recent outcome: %v", err)
	}

	samples, err := outcomes.ListLabeledSamplesSince(ctx, cutoff, nil)
	if err != nil {
		t.Fatalf("ListLabeledSamplesSince: %v", err)
	}
	if len(samples) != 1 || samples[0].Direction != domain.JevDirectionShort || samples[0].FutureReturn != -0.30 {
		t.Fatalf("ListLabeledSamplesSince(%s) = %+v, want exactly the recent SHORT outcome", cutoff, samples)
	}
}

func TestCalibrationRepository_ListByDecisionIDs(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()
	first := insertTraderDecision(t, decisions, instID, now, domain.JevDirectionLong)
	second := insertTraderDecision(t, decisions, instID, now.Add(time.Minute), domain.JevDirectionNone)
	unlabeled := insertTraderDecision(t, decisions, instID, now.Add(2*time.Minute), domain.JevDirectionShort)
	other := insertTraderDecision(t, decisions, instID, now.Add(3*time.Minute), domain.JevDirectionLong)

	for _, o := range []domain.CalibrationOutcome{
		{JevDecisionID: second.ID, HorizonMinutes: 5, FutureReturn: 0.1},
		{JevDecisionID: first.ID, HorizonMinutes: 10, FutureReturn: 0.2, WasDirectionCorrect: ptr(true)},
		{JevDecisionID: first.ID, HorizonMinutes: 5, FutureReturn: 0.3, WasDirectionCorrect: ptr(true)},
		{JevDecisionID: other.ID, HorizonMinutes: 5, FutureReturn: 0.9, WasDirectionCorrect: ptr(true)},
	} {
		if _, err := outcomes.Insert(ctx, o); err != nil {
			t.Fatalf("seed outcome: %v", err)
		}
	}

	got, err := outcomes.ListByDecisionIDs(ctx, []int64{second.ID, unlabeled.ID, first.ID})
	if err != nil {
		t.Fatalf("ListByDecisionIDs: %v", err)
	}
	type key struct {
		decision int64
		horizon  int
	}
	var gotKeys []key
	for _, o := range got {
		gotKeys = append(gotKeys, key{o.JevDecisionID, o.HorizonMinutes})
	}
	want := []key{{first.ID, 5}, {first.ID, 10}, {second.ID, 5}}
	if !reflect.DeepEqual(gotKeys, want) {
		t.Fatalf("ListByDecisionIDs keys = %v, want %v (requested decisions only, ordered by decision then horizon; unlabeled/unrequested omitted)", gotKeys, want)
	}
	if got[2].WasDirectionCorrect != nil {
		t.Errorf("NONE decision's WasDirectionCorrect = %v, want nil", *got[2].WasDirectionCorrect)
	}

	empty, err := outcomes.ListByDecisionIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("ListByDecisionIDs(nil) = %v, %v, want no rows and no error", empty, err)
	}
}

// A pair marked unlabelable drops out of PendingLabels for that horizon
// only - not for the decision's other horizons - and marking is idempotent
// and writes no calibration_outcomes row (issue #481, FR-CAL-4).
func TestCalibrationRepository_MarkUnlabelableExcludesPairFromPendingLabels(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 15, 25, 0, 0, time.UTC)
	now := base.Add(21 * time.Minute)
	decision := insertTraderDecision(t, decisions, instID, base, domain.JevDirectionLong)

	for range 2 {
		if err := outcomes.MarkUnlabelable(ctx, decision.ID, 15, "close"); err != nil {
			t.Fatalf("MarkUnlabelable: %v", err)
		}
	}

	pending, err := outcomes.PendingLabels(ctx, []int{5, 15}, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("PendingLabels: %v", err)
	}
	if len(pending) != 1 || pending[0].HorizonMinutes != 5 || pending[0].JevDecisionID != decision.ID {
		t.Fatalf("PendingLabels = %+v, want only the 5m pair (15m is unlabelable)", pending)
	}
	samples, err := outcomes.ListLabeledSamples(ctx, nil)
	if err != nil {
		t.Fatalf("ListLabeledSamples: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("ListLabeledSamples = %+v, want none (a skip is not a label)", samples)
	}
}
