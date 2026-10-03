package handler_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

var (
	refreshButtonTag = regexp.MustCompile(`<button[^>]*data-testid="scan-refresh"[^>]*>`)
	disabledAttr     = regexp.MustCompile(`\sdisabled[\s=>]`)
)

func fixedClock(y int, m time.Month, d, hh, mm int) func() time.Time {
	return func() time.Time { return time.Date(y, m, d, hh, mm, 0, 0, time.FixedZone("JST", 9*60*60)) }
}

func TestScannerHandler_ScanPanel_OffSessionNotice(t *testing.T) {
	saturdayNight := fixedClock(2026, 10, 3, 21, 0)
	sources := map[string]handler.CandidateSource{
		"with cycle":   scanSource{cycle: scanFixture(), ok: true},
		"before cycle": scanSource{},
	}
	for name, src := range sources {
		for _, target := range []struct {
			url string
			hx  bool
		}{{"/scanner", false}, {"/scanner/scan", true}, {"/scanner/scan?open=0", true}} {
			t.Run(name+" "+target.url, func(t *testing.T) {
				_, body := getScanAt(t, src, saturdayNight, target.url, target.hx)
				for _, want := range []string{`data-testid="scan-offhours"`, "東証の立会時間外", "Jev Scout", "保存済みデータ", "2026-10-05(月) 09:00 JST"} {
					if !strings.Contains(body, want) {
						t.Errorf("off-session panel missing %q", want)
					}
				}
				// The refresh button stays an enabled hx-get button.
				btn := refreshButtonTag.FindString(body)
				if btn == "" || !strings.Contains(btn, "hx-get=") || disabledAttr.MatchString(btn) {
					t.Errorf("refresh button must be present, hx-get and not disabled off-session; got %q", btn)
				}
			})
		}
	}
}

func TestScannerHandler_ScanPanel_NoNoticeDuringSession(t *testing.T) {
	for _, src := range []handler.CandidateSource{scanSource{cycle: scanFixture(), ok: true}, scanSource{}} {
		for _, hx := range []bool{false, true} {
			target := "/scanner"
			if hx {
				target = "/scanner/scan"
			}
			_, body := getScanAt(t, src, fixedClock(2026, 9, 29, 10, 0), target, hx)
			if strings.Contains(body, "scan-offhours") || strings.Contains(body, "scan-resume-at") {
				t.Errorf("%s: notice shown during the morning session", target)
			}
		}
	}
}

func TestScannerHandler_ScanPanel_LunchBreakShowsAfternoonOpen(t *testing.T) {
	_, body := getScanAt(t, scanSource{cycle: scanFixture(), ok: true}, fixedClock(2026, 9, 29, 12, 0), "/scanner/scan", true)
	if !strings.Contains(body, `data-testid="scan-offhours"`) || !strings.Contains(body, "2026-09-29(火) 12:30 JST") {
		t.Errorf("lunch break: want notice with 12:30 next open, got %q", body)
	}
}

func TestScannerHandler_ScanPanel_NextOpenSkipsHolidayAndUsesJST(t *testing.T) {
	// 2026-10-09 (Fri) 07:30 UTC = 16:30 JST; Mon 10/12 is a national holiday.
	utc := func() time.Time { return time.Date(2026, 10, 9, 7, 30, 0, 0, time.UTC) }
	_, body := getScanAt(t, scanSource{}, utc, "/scanner/scan", true)
	if !strings.Contains(body, "2026-10-13(火) 09:00 JST") {
		t.Errorf("want next open Tue 2026-10-13 09:00 JST, got %q", body)
	}
}
