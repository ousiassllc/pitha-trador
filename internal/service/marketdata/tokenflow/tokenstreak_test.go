package tokenflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
)

// issue #712: a not_logged_in (4001007 / 4001017) streak is tracked by count
// and start time so the banner can escalate when kabuステーション stays
// logged out after the app's own retries.
func TestClient_TokenStatus_TracksFailureStreak(t *testing.T) {
	var code atomic.Int64
	var healthy atomic.Bool
	code.Store(4001007)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"Code": code.Load(), "Message": "x"})
	}))
	defer server.Close()

	clock := infolimit.NewManualClock(time.Time{})
	client := marketdata.NewClient(marketdata.Config{BaseURL: server.URL, APIPassword: "pw", Clock: clock})
	ctx := context.Background()
	start := clock.Now()

	_, _ = client.IssueToken(ctx)
	if got := client.TokenStatus(); got.Failures != 1 || !got.Since.Equal(start) {
		t.Fatalf("after first failure: %+v, want Failures 1 Since %v", got, start)
	}
	if persistent, _ := client.TokenStatus().Persistent(clock.Now()); persistent {
		t.Error("a single fresh failure must not be persistent")
	}

	<-clock.After(time.Minute)
	code.Store(4001017) // same cause (not_logged_in): the streak continues
	_, _ = client.IssueToken(ctx)
	got := client.TokenStatus()
	if got.Failures != 2 || !got.Since.Equal(start) {
		t.Fatalf("after second not_logged_in failure: %+v, want Failures 2 Since %v", got, start)
	}

	<-clock.After(5 * time.Minute)
	persistent, elapsed := got.Persistent(clock.Now())
	if !persistent || elapsed != 6*time.Minute {
		t.Errorf("Persistent after 6 min = %v, %v; want true, 6m", persistent, elapsed)
	}

	code.Store(4001013) // a different cause starts a new streak
	_, _ = client.IssueToken(ctx)
	got = client.TokenStatus()
	if got.Issue != marketdata.TokenIssueBadPassword || got.Failures != 1 || !got.Since.Equal(clock.Now()) {
		t.Errorf("after cause change: %+v, want bad_password Failures 1 Since now", got)
	}
	if persistent, _ := got.Persistent(clock.Now().Add(time.Hour)); persistent {
		t.Error("only not_logged_in escalates; bad_password must not be persistent")
	}

	healthy.Store(true)
	if _, err := client.IssueToken(ctx); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if got := client.TokenStatus(); got.Failed() || got.Failures != 0 || !got.Since.IsZero() {
		t.Errorf("after success: %+v, want the streak cleared", got)
	}
}

func TestTokenStatus_Persistent(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		status      marketdata.TokenStatus
		wantPersist bool
		wantElapsed time.Duration
	}{
		{name: "no failure", status: marketdata.TokenStatus{}},
		{name: "few recent failures", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 4, Since: now.Add(-2 * time.Minute)}, wantElapsed: 2 * time.Minute},
		{name: "repeated failures", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 5, Since: now.Add(-3 * time.Minute)}, wantPersist: true, wantElapsed: 3 * time.Minute},
		{name: "long lasting", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Failures: 2, Since: now.Add(-5 * time.Minute)}, wantPersist: true, wantElapsed: 5 * time.Minute},
		{name: "other cause never escalates", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueUnreachable, Failures: 9, Since: now.Add(-time.Hour)}},
		{name: "rejected has no streak", status: marketdata.TokenStatus{Issue: marketdata.TokenIssueRejected, Code: 4001007}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			persistent, elapsed := tt.status.Persistent(now)
			if persistent != tt.wantPersist || elapsed != tt.wantElapsed {
				t.Errorf("Persistent = %v, %v; want %v, %v", persistent, elapsed, tt.wantPersist, tt.wantElapsed)
			}
		})
	}
}
