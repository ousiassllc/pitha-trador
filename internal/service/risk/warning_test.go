package risk_test

import (
	"context"
	"testing"
	"time"
)

func TestEngine_CheckDailyLossWarning_NoOpBelowThreshold(t *testing.T) {
	notifier := &fakeNotifier{}
	e := newEngineWithNotifier(t, fakePortfolio{dailyLossPct: 0.79}, notifier, nil) // below 80% of testLimits().MaxDailyLossPct=1.0

	if err := e.CheckDailyLossWarning(context.Background()); err != nil {
		t.Fatalf("CheckDailyLossWarning: %v", err)
	}
	if len(notifier.dailyLoss) != 0 {
		t.Fatalf("dailyLoss calls = %d, want 0 (below 80%% threshold)", len(notifier.dailyLoss))
	}
}

func TestEngine_CheckDailyLossWarning_NotifiesAtThreshold(t *testing.T) {
	notifier := &fakeNotifier{}
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	e := newEngineWithNotifier(t, fakePortfolio{dailyLossPct: 0.85}, notifier, func() time.Time { return now }) // 85% of MaxDailyLossPct=1.0

	if err := e.CheckDailyLossWarning(context.Background()); err != nil {
		t.Fatalf("CheckDailyLossWarning: %v", err)
	}
	if len(notifier.dailyLoss) != 1 {
		t.Fatalf("dailyLoss calls = %d, want 1", len(notifier.dailyLoss))
	}
	if notifier.dailyLoss[0].currentPct != 0.85 {
		t.Errorf("currentPct = %v, want 0.85", notifier.dailyLoss[0].currentPct)
	}
}

func TestEngine_CheckDailyLossWarning_NotifiesOnlyOncePerUTCDay(t *testing.T) {
	notifier := &fakeNotifier{}
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	e := newEngineWithNotifier(t, fakePortfolio{dailyLossPct: 0.9}, notifier, func() time.Time { return now })
	ctx := context.Background()

	if err := e.CheckDailyLossWarning(ctx); err != nil {
		t.Fatalf("CheckDailyLossWarning (1st): %v", err)
	}
	if err := e.CheckDailyLossWarning(ctx); err != nil {
		t.Fatalf("CheckDailyLossWarning (2nd, same day): %v", err)
	}
	if len(notifier.dailyLoss) != 1 {
		t.Fatalf("dailyLoss calls = %d, want 1 (deduped within the same UTC day)", len(notifier.dailyLoss))
	}

	now = now.Add(24 * time.Hour) // next UTC day
	if err := e.CheckDailyLossWarning(ctx); err != nil {
		t.Fatalf("CheckDailyLossWarning (next day): %v", err)
	}
	if len(notifier.dailyLoss) != 2 {
		t.Fatalf("dailyLoss calls = %d, want 2 (a new UTC day re-arms the warning)", len(notifier.dailyLoss))
	}
}
