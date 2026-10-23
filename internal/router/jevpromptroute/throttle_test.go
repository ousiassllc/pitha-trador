package jevpromptroute_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// The panel says why the shadow stopped on the Jev API's 429/529, when it
// resumes, and nothing while there is nothing to say (issue #883).
func TestPanelShowsWhyTheShadowStoppedOnA429(t *testing.T) {
	a := newApp(t)
	if body := a.get("/settings/jev-prompt-panel").Body.String(); strings.Contains(body, `data-testid="jev-prompt-throttle"`) {
		t.Fatalf("a throttle note without any stop recorded\n%s", body)
	}

	set := func(value string) {
		t.Helper()
		if err := a.settings.Set(context.Background(), jevshadowsettings.KeyThrottle, value, now); err != nil {
			t.Fatal(err)
		}
	}
	// now is 2026-10-22T03:00:00Z = 12:00 JST. A stop at 11:55 JST lasts to 12:10 JST.
	set(`{"date":"2026-10-22","count":2,"status":429,"reason":"api_rate_limited","source":"shadow","at":"2026-10-22T02:55:00Z","until":"2026-10-22T03:10:00Z"}`)
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{
		`data-testid="jev-prompt-throttle"`, "シャドーの呼び出しに 429（レート制限）を返しました", "12:10", "まで止まっています", "待たず・再試行せず", "ライブの判断は続いています", "今日の停止回数: 2 回",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("active throttle: panel lacks %q\n%s", want, body)
		}
	}

	// A 529 that a live call got, already over: it says so and that the shadow runs again.
	set(`{"date":"2026-10-22","count":1,"status":529,"reason":"api_overloaded","source":"live","at":"2026-10-22T02:00:00Z","until":"2026-10-22T02:15:00Z"}`)
	body = a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{"ライブの呼び出しに 529（過負荷）を返しました", "シャドーは再開しています", "今日の停止回数: 1 回"} {
		if !strings.Contains(body, want) {
			t.Errorf("past throttle: panel lacks %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "まで止まっています") {
		t.Error("an expired stop is shown as still stopping")
	}

	// A stop of an earlier day that is over has nothing to show.
	set(`{"date":"2026-10-21","count":3,"status":429,"reason":"api_rate_limited","source":"shadow","at":"2026-10-21T02:00:00Z","until":"2026-10-21T02:15:00Z"}`)
	if body = a.get("/settings/jev-prompt-panel").Body.String(); strings.Contains(body, `data-testid="jev-prompt-throttle"`) {
		t.Errorf("yesterday's finished stop is shown\n%s", body)
	}
}
