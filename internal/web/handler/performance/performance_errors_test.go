package performance_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// errorItems returns the text of every backtest-error list item.
func errorItems(t *testing.T, body string) []string {
	t.Helper()
	var items []string
	for _, m := range regexp.MustCompile(`<li data-testid="backtest-error-item">([^<]*)</li>`).FindAllStringSubmatch(body, -1) {
		items = append(items, m[1])
	}
	return items
}

func TestPerformanceHandler_Page_ValidationErrorsAreJapanese(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		want  string
	}{
		"fold days":        {"from=2026-09-01&to=2026-09-10&forward_days=0", "forward_days は 1〜366 の整数で指定してください。"},
		"bad from":         {"from=2026-13-01&to=2026-09-10", "from は YYYY-MM-DD 形式の日付で指定してください。"},
		"bad to":           {"from=2026-09-01&to=oops", "to は YYYY-MM-DD 形式の日付で指定してください。"},
		"to before from":   {"from=2026-09-10&to=2026-09-01", "to は from 以降の日付で指定してください。"},
		"range too long":   {dayQuery(1831, "&forward_days=366"), "from〜to の期間は最大 1830 日までです（指定: 1831 日）。"},
		"range too short":  {"from=2026-09-01&to=2026-09-03", "2026-09-01〜2026-09-03 は Training/Validation/Forward を 5+2+1 日で1フォールド実行するには短すぎます。"},
		"too many folds":   {dayQuery(1003, "&training_days=1&validation_days=1&forward_days=1"), "フォールド数が 1001 になり、上限 1000 を超えます。"},
		"non-numeric days": {"from=2026-09-01&to=2026-09-10&training_days=%3Cb%3Ex", "training_days は 1〜366 の整数で指定してください。"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := servePerformance(t, &recordingBacktestRunner{}, "/performance?"+tc.query)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("body should contain %q: %s", tc.want, body)
			}
			if strings.Contains(body, " must ") || strings.Contains(body, "got \"") {
				t.Errorf("body still shows an English validation message: %s", body)
			}
		})
	}
}

func TestPerformanceHandler_Page_ShowsEachValidationErrorAsOwnListItem(t *testing.T) {
	rec := servePerformance(t, &recordingBacktestRunner{}, "/performance?from=bad&to=worse&training_days=0")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	want := []string{
		"training_days は 1〜366 の整数で指定してください。",
		"from は YYYY-MM-DD 形式の日付で指定してください。",
		"to は YYYY-MM-DD 形式の日付で指定してください。",
	}
	got := errorItems(t, rec.Body.String())
	if len(got) != len(want) {
		t.Fatalf("error items = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("error item %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPerformanceHandler_Page_TimeoutMessageIsJapanese(t *testing.T) {
	rec := servePerformance(t, &recordingBacktestRunner{err: context.DeadlineExceeded}, "/performance?from=2026-09-01&to=2026-09-10")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	got := errorItems(t, rec.Body.String())
	want := "バックテストが 60秒 でタイムアウトしました。期間を短くしてください。"
	if len(got) != 1 || got[0] != want {
		t.Errorf("error items = %q, want [%q]", got, want)
	}
}
