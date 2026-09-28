package risk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// countingNotifier records how many times each risk.Notifier method was
// called, failing every call with failWith when it is non-nil.
type countingNotifier struct {
	triggered, resumed, dailyLossCalls int
	failWith                           error
}

func (n *countingNotifier) KillSwitchTriggered(context.Context, domain.KillSwitchEvent, bool) error {
	n.triggered++
	return n.failWith
}

func (n *countingNotifier) KillSwitchAutoResumed(context.Context, domain.KillSwitchEvent) error {
	n.resumed++
	return n.failWith
}

func (n *countingNotifier) DailyLossWarning(context.Context, float64, float64) error {
	n.dailyLossCalls++
	return n.failWith
}

func TestMultiNotifier_FailingChannelDoesNotSuppressOthers(t *testing.T) {
	slackDown := errors.New("slack webhook unreachable")
	failing := &countingNotifier{failWith: slackDown}
	healthy := &countingNotifier{}
	multi := risk.NewMultiNotifier(failing, healthy)
	ctx := context.Background()

	err := multi.KillSwitchTriggered(ctx, domain.KillSwitchEvent{Reason: domain.KillReasonMarketDataDown}, true)
	if !errors.Is(err, slackDown) {
		t.Errorf("KillSwitchTriggered error = %v, want it to wrap the failing channel's error", err)
	}
	_ = multi.KillSwitchAutoResumed(ctx, domain.KillSwitchEvent{Reason: domain.KillReasonMarketDataDown})
	_ = multi.DailyLossWarning(ctx, 2.5, 3.0)

	if healthy.triggered != 1 || healthy.resumed != 1 || healthy.dailyLossCalls != 1 {
		t.Errorf("healthy channel calls = (triggered %d, resumed %d, dailyLoss %d), want 1 each despite the earlier channel failing",
			healthy.triggered, healthy.resumed, healthy.dailyLossCalls)
	}
}

func TestMultiNotifier_JoinsEveryChannelError(t *testing.T) {
	first := errors.New("first channel down")
	second := errors.New("second channel down")
	multi := risk.NewMultiNotifier(&countingNotifier{failWith: first}, &countingNotifier{failWith: second})

	err := multi.DailyLossWarning(context.Background(), 2.5, 3.0)
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Errorf("DailyLossWarning error = %v, want both channel errors joined", err)
	}
}
