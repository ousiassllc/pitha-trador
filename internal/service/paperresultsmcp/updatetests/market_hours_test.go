package updatetests

import (
	"testing"
	"time"
)

func jst(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.FixedZone("JST", 9*60*60))
}

const marketHoursText = "取引時間中（9:00〜15:30）のため"

// A trading day's 立会時間帯 (JST 9:00-15:30) defers update_apply with
// market_hours even though SafeGate passes and the installer is verified: the
// application is not quit and the audit row says deferred/market_hours.
func TestUpdateApplyDeferredDuringMarketHours(t *testing.T) {
	st := newStackAt(t, "v0.1.0", "v0.2.0", true, jst(2026, 11, 25, 10, 0))
	if d := checkNow(t, st); !d.Ready {
		t.Fatalf("setup: %+v, want a ready installer", d)
	}

	d := apply(t, st)
	if d.Result != "deferred" || d.Reason != "market_hours" || d.ReasonText != marketHoursText || d.RequestID == 0 {
		t.Errorf("update_apply = %+v, want deferred/market_hours", d)
	}
	if len(st.quit.calls()) != 0 {
		t.Errorf("quit called %v during market hours", st.quit.calls())
	}
	if rows := st.applyRows(t); len(rows) != 1 || rows[0].Outcome != "deferred" || rows[0].Reason != "market_hours" {
		t.Errorf("audit rows = %v, want one deferred/market_hours", rows)
	}
	if got := status(t, st); got.LastApply == nil || got.LastApply.Result != "deferred" || got.LastApply.Reason != "market_hours" {
		t.Errorf("update_status.last_apply = %+v, want deferred/market_hours", got.LastApply)
	}
}

// Outside the 立会時間帯 (here after 大引け) with SafeGate passing, update_apply is accepted.
func TestUpdateApplyAcceptedOutsideMarketHours(t *testing.T) {
	st := newStackAt(t, "v0.1.0", "v0.2.0", true, jst(2026, 11, 25, 15, 30))
	if d := checkNow(t, st); !d.Ready {
		t.Fatalf("setup: %+v, want a ready installer", d)
	}
	if d := apply(t, st); d.Result != "accepted" || len(st.quit.calls()) != 1 {
		t.Errorf("update_apply = %+v, quits %v, want accepted and one quit", d, st.quit.calls())
	}
}

// On a day the exchange is closed (勤労感謝の日, Saturday) the time of day does
// not matter: SafeGate alone decides, in both directions.
func TestUpdateApplyOnClosedDayFollowsSafeGate(t *testing.T) {
	for name, at := range map[string]time.Time{"holiday": jst(2026, 11, 23, 10, 0), "saturday": jst(2026, 11, 28, 10, 0)} {
		t.Run(name, func(t *testing.T) {
			st := newStackAt(t, "v0.1.0", "v0.2.0", true, at)
			if d := checkNow(t, st); !d.Ready {
				t.Fatalf("setup: %+v, want a ready installer", d)
			}
			st.gate.openPositions.Store(1)
			if d := apply(t, st); d.Result != "deferred" || d.Reason != "open_positions" || len(st.quit.calls()) != 0 {
				t.Fatalf("update_apply = %+v, quits %v, want deferred/open_positions (SafeGate blocks)", d, st.quit.calls())
			}
			st.gate.openPositions.Store(0)
			if d := checkNow(t, st); !d.Ready {
				t.Fatalf("setup: %+v, want a ready installer", d)
			}
			if d := apply(t, st); d.Result != "accepted" || len(st.quit.calls()) != 1 {
				t.Errorf("update_apply = %+v, quits %v, want accepted (SafeGate passes)", d, st.quit.calls())
			}
		})
	}
}

// update_status names market_hours as the reason a release is held back during
// the 立会時間帯 (Settings' wording style), and SafeGate's own kind keeps
// precedence.
func TestUpdateStatusShowsMarketHours(t *testing.T) {
	st := newStackAt(t, "v0.1.0", "v0.2.0", true, jst(2026, 11, 25, 12, 0))
	checkNow(t, st)
	if got := status(t, st); !got.Available || !got.Blocked || got.BlockedKind != "market_hours" || got.BlockedReason != marketHoursText {
		t.Errorf("update_status = %+v, want blocked by market_hours with its wording", got)
	}

	st.gate.openPositions.Store(1)
	checkNow(t, st) // the check now meets SafeGate's own condition first
	if got := status(t, st); got.BlockedKind != "open_positions" {
		t.Errorf("blocked_kind = %q, want open_positions (SafeGate before market_hours)", got.BlockedKind)
	}
}

// update_status outside the 立会時間帯 shows no market_hours.
func TestUpdateStatusHasNoMarketHoursOutsideHours(t *testing.T) {
	st := newStackAt(t, "v0.1.0", "v0.2.0", true, jst(2026, 11, 25, 18, 0))
	checkNow(t, st)
	if got := status(t, st); !got.Available || got.Blocked || got.BlockedKind != "" {
		t.Errorf("update_status = %+v, want available and not blocked", got)
	}
}
