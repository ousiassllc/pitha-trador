package trainingflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// Issue #776: a placeholder LLM cutoff means the OOS look-ahead protection does
// nothing, so a model learned under it is stored as rejected (never replacing
// the active one) and the nightly log warns.
func TestTrainer_PlaceholderCutoffRejectsTheModelAndKeepsTheActiveOne(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	old, err := newTrainer(&fakeSource{rows: trainingRows(3000, 21)}, store, settingsOf(nil)).Train(ctx)
	if err != nil || old.Status != repo.ModelActive {
		t.Fatalf("first Train = %+v, %v", old, err)
	}

	logs := captureLogs(t)
	placeholder := settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoff = metalabelsettings.DefaultLLMCutoff })
	rejected, err := newTrainer(&fakeSource{rows: trainingRows(3000, 22)}, store, placeholder,
		training.WithClock(func() time.Time { return trainNow.Add(time.Hour) })).Train(ctx)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	// The metrics themselves would pass: only the cutoff blocks the adoption.
	if rejected.Status != repo.ModelRejected || rejected.RejectReason != training.RejectLLMCutoffPlaceholder {
		t.Fatalf("model = %q (%s), want rejected for the placeholder cutoff", rejected.Status, rejected.RejectReason)
	}
	if rejected.OOSAUC == nil || *rejected.OOSAUC < 0.6 {
		t.Errorf("the rejected model lost its OOS metrics: %+v", rejected)
	}
	got, found, err := store.LatestActiveModel(ctx)
	if err != nil || !found || got.ID != old.ID {
		t.Fatalf("LatestActiveModel = %+v, %v, %v, want the earlier active model %d", got, found, err, old.ID)
	}
	for _, want := range []string{"LLM cutoff is not confirmed", "cutoff_status=placeholder", "reject_reason=llm_cutoff_placeholder"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs.String())
		}
	}
}

func TestTrainer_PlaceholderCutoffWithNoActiveModelLeavesNoneInForce(t *testing.T) {
	store := newStore(t)
	saved, err := newTrainer(&fakeSource{rows: trainingRows(3000, 23)}, store, settingsOf(func(s *metalabelsettings.Settings) {
		s.LLMCutoff = metalabelsettings.DefaultLLMCutoff
	})).Train(context.Background())
	if err != nil || saved.Status != repo.ModelRejected || saved.RejectReason != training.RejectLLMCutoffPlaceholder {
		t.Fatalf("Train = %+v, %v", saved, err)
	}
	if _, found, err := store.LatestActiveModel(context.Background()); err != nil || found {
		t.Fatalf("LatestActiveModel found=%v err=%v, want no model in force", found, err)
	}
}

// A real cutoff removes the warning, and the OOS window starts at the cutoff.
func TestTrainer_RealCutoffAdoptsWithoutWarning(t *testing.T) {
	logs := captureLogs(t)
	saved, err := newTrainer(&fakeSource{rows: trainingRows(3000, 24)}, newStore(t), settingsOf(func(s *metalabelsettings.Settings) {
		s.LLMCutoff, s.LLMCutoffModel = "2026-10-12", "jev-x"
	})).Train(context.Background())
	if err != nil || saved.Status != repo.ModelActive {
		t.Fatalf("Train = %+v, %v", saved, err)
	}
	if want := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC); !saved.OOSStart.Equal(want) {
		t.Errorf("OOS start = %v, want the cutoff day %v", saved.OOSStart, want)
	}
	if strings.Contains(logs.String(), "LLM cutoff is not confirmed") {
		t.Errorf("a confirmed cutoff still logged the warning:\n%s", logs.String())
	}
}

func TestTrainer_JevModelChangeRejectsUntilTheCutoffIsReconfirmed(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	rows := trainingRows(3000, 25)
	for i := range rows {
		rows[i].JevModelID = "jev-new"
	}
	confirmedFor := func(model string) training.SettingsLoader {
		return settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoffModel = model })
	}

	rejected, err := newTrainer(&fakeSource{rows: rows}, store, confirmedFor("jev-old")).Train(ctx)
	if err != nil || rejected.Status != repo.ModelRejected || rejected.RejectReason != training.RejectLLMModelChanged {
		t.Fatalf("Train = %+v, %v, want rejected: the OOS decisions came from another Jev model", rejected, err)
	}
	// Re-confirming the cutoff for the model Jev now answers with lifts the block.
	adopted, err := newTrainer(&fakeSource{rows: rows}, store, confirmedFor("jev-new"),
		training.WithClock(func() time.Time { return trainNow.Add(time.Hour) })).Train(ctx)
	if err != nil || adopted.Status != repo.ModelActive {
		t.Fatalf("Train = %+v, %v, want active once the cutoff is re-confirmed", adopted, err)
	}
	// A cutoff with no recorded model (unverified) only warns.
	unverified, err := newTrainer(&fakeSource{rows: rows}, newStore(t), confirmedFor("")).Train(ctx)
	if err != nil || unverified.Status != repo.ModelActive {
		t.Fatalf("Train = %+v, %v, want active for an unverified (warn-only) cutoff", unverified, err)
	}
}

// Issue #780 (decided 2026-10-19): an unverified cutoff (a real date, no
// recorded Jev model) adopts with a warning in Paper but stops the adoption in
// the pre-Live check, keeping the model in force.
func TestTrainer_UnverifiedCutoffWarnsInPaperAndBlocksBeforeLive(t *testing.T) {
	unverified := settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoffModel = "" })
	ctx := context.Background()
	rows := trainingRows(3000, 26)

	for name, opts := range map[string][]training.TrainerOption{
		"default is Paper": nil,
		"explicit Paper":   {training.WithMode(metalabelsettings.ModePaper)},
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			saved, err := newTrainer(&fakeSource{rows: rows}, newStore(t), unverified, opts...).Train(ctx)
			if err != nil || saved.Status != repo.ModelActive || saved.RejectReason != "" {
				t.Fatalf("Train = %+v, %v, want active with a warning in Paper", saved, err)
			}
			if !strings.Contains(logs.String(), "cutoff_status=unverified") {
				t.Errorf("Paper adoption did not log the unverified warning:\n%s", logs.String())
			}
		})
	}

	t.Run("Live", func(t *testing.T) {
		store := newStore(t)
		active, err := newTrainer(&fakeSource{rows: rows}, store, settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoffModel = "jev-x" }),
			training.WithMode(metalabelsettings.ModeLive)).Train(ctx)
		if err != nil || active.Status != repo.ModelActive {
			t.Fatalf("a confirmed cutoff must still adopt in Live: %+v, %v", active, err)
		}
		blocked, err := newTrainer(&fakeSource{rows: rows}, store, unverified, training.WithMode(metalabelsettings.ModeLive),
			training.WithClock(func() time.Time { return trainNow.Add(time.Hour) })).Train(ctx)
		if err != nil || blocked.Status != repo.ModelRejected || blocked.RejectReason != training.RejectLLMCutoffUnverified {
			t.Fatalf("Train = %+v, %v, want rejected (llm_cutoff_unverified) in Live", blocked, err)
		}
		if got, found, err := store.LatestActiveModel(ctx); err != nil || !found || got.ID != active.ID {
			t.Errorf("LatestActiveModel = %+v, %v, %v, want the earlier model %d kept in force", got, found, err, active.ID)
		}
	})
}
