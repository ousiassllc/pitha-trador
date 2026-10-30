package disclosureskip_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const sym = "7203"

// jstAt is a JST wall-clock time.
func jstAt(month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(2026, month, day, hour, minute, second, 0, marketcalendar.JST)
}

// closedDays is a fake TSE calendar: weekdays are trading days except the
// listed dates, so a test pins its holidays instead of relying on the real
// holiday rules.
type closedDays map[[2]int]bool

func (c closedDays) IsTradingDay(t time.Time) bool {
	t = t.In(marketcalendar.JST)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	return !c[[2]int{int(t.Month()), t.Day()}]
}

var weekdaysOnly = closedDays{}

func disclosure(title string, at time.Time) disclosureskip.Disclosure {
	return disclosureskip.Disclosure{Symbol: sym, ID: title + at.String(), PublishedAt: at.UTC(), Title: title}
}

// completeFetch is a successful fetch that covers any window.
func completeFetch(at time.Time) disclosureskip.Fetch {
	return disclosureskip.Fetch{Symbol: sym, At: at.UTC(), OK: true}
}

const (
	tanshin  = "2026年3月期 第2四半期（中間期）決算短信〔日本基準〕（連結）"
	revision = "業績予想の修正に関するお知らせ"
)

// 2026-10-14 is a Wednesday: the preceding trading day is Tuesday 10-13.
func TestEvaluate_DisclosureAfterTheDecisionIsNotAMatch(t *testing.T) {
	decision := jstAt(10, 14, 14, 50, 0)
	future := disclosure(tanshin, jstAt(10, 14, 15, 30, 0))
	got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{future}, []disclosureskip.Fetch{completeFetch(decision.Add(time.Hour))})
	if got.Verdict != disclosureskip.VerdictNoSkip || len(got.Matches) != 0 {
		t.Fatalf("verdict = %q, matches = %v; a 15:30 disclosure must not match a 14:50 decision", got.Verdict, got.Matches)
	}
	// One second after the decision is already the future; at the decision it is not.
	for _, tc := range []struct {
		name string
		at   time.Time
		want disclosureskip.Verdict
	}{
		{"same second as the decision", decision, disclosureskip.VerdictSkip},
		{"one second after the decision", decision.Add(time.Second), disclosureskip.VerdictNoSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{disclosure(tanshin, tc.at)}, []disclosureskip.Fetch{completeFetch(decision.Add(time.Hour))})
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", got.Verdict, tc.want)
			}
		})
	}
}

func TestEvaluate_PreviousTradingDayFrom1500MatchesNextDay(t *testing.T) {
	decision := jstAt(10, 14, 10, 0, 0) // Wednesday; the previous trading day is Tuesday 10-13
	for _, tc := range []struct {
		name string
		at   time.Time
		want disclosureskip.Verdict
	}{
		{"15:00:00 of the previous trading day is in", jstAt(10, 13, 15, 0, 0), disclosureskip.VerdictSkip},
		{"15:30 of the previous trading day is in", jstAt(10, 13, 15, 30, 0), disclosureskip.VerdictSkip},
		{"14:59:59 of the previous trading day is out", jstAt(10, 13, 14, 59, 59), disclosureskip.VerdictNoSkip},
		{"14:59 of the previous trading day is out", jstAt(10, 13, 14, 59, 0), disclosureskip.VerdictNoSkip},
		{"the decision's own day is in", jstAt(10, 14, 8, 30, 0), disclosureskip.VerdictSkip},
		{"two trading days back is out", jstAt(10, 12, 16, 0, 0), disclosureskip.VerdictNoSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{disclosure(tanshin, tc.at)}, []disclosureskip.Fetch{completeFetch(decision.Add(time.Hour))})
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q (window from %s)", got.Verdict, tc.want, got.WindowFrom)
			}
		})
	}
}

func TestEvaluate_WindowCrossesWeekendsAndHolidays(t *testing.T) {
	cases := []struct {
		name     string
		cal      disclosureskip.Calendar
		decision time.Time
		at       time.Time
		want     disclosureskip.Verdict
		from     time.Time
	}{
		{"Friday 15:00 matches Monday", weekdaysOnly, jstAt(10, 12, 9, 30, 0), jstAt(10, 9, 15, 0, 0), disclosureskip.VerdictSkip, jstAt(10, 9, 15, 0, 0)},
		{"Friday 14:59 does not match Monday", weekdaysOnly, jstAt(10, 12, 9, 30, 0), jstAt(10, 9, 14, 59, 0), disclosureskip.VerdictNoSkip, jstAt(10, 9, 15, 0, 0)},
		{"Friday 15:00 matches Tuesday after a Monday holiday", closedDays{{10, 12}: true}, jstAt(10, 13, 9, 30, 0), jstAt(10, 9, 15, 0, 0), disclosureskip.VerdictSkip, jstAt(10, 9, 15, 0, 0)},
		{"Friday 14:59 does not match Tuesday after a Monday holiday", closedDays{{10, 12}: true}, jstAt(10, 13, 9, 30, 0), jstAt(10, 9, 14, 59, 0), disclosureskip.VerdictNoSkip, jstAt(10, 9, 15, 0, 0)},
		{"the holiday itself is not the previous trading day", closedDays{{10, 12}: true}, jstAt(10, 13, 9, 30, 0), jstAt(10, 12, 15, 0, 0), disclosureskip.VerdictSkip, jstAt(10, 9, 15, 0, 0)},
		{"two holidays in a row", closedDays{{10, 12}: true, {10, 13}: true}, jstAt(10, 14, 9, 30, 0), jstAt(10, 9, 16, 0, 0), disclosureskip.VerdictSkip, jstAt(10, 9, 15, 0, 0)},
		{"a decision on a Sunday looks back to Friday 15:00", weekdaysOnly, jstAt(10, 11, 12, 0, 0), jstAt(10, 9, 15, 0, 0), disclosureskip.VerdictSkip, jstAt(10, 9, 15, 0, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureskip.Evaluate(tc.cal, sym, tc.decision, []disclosureskip.Disclosure{disclosure(tanshin, tc.at)}, []disclosureskip.Fetch{completeFetch(tc.decision.Add(time.Hour))})
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", got.Verdict, tc.want)
			}
			if !got.WindowFrom.Equal(tc.from) {
				t.Fatalf("window from %s, want %s", got.WindowFrom, tc.from)
			}
		})
	}
}

// The real TSE calendar: 2026-10-12 (Mon) is スポーツの日, so a decision on
// Tuesday 10-13 looks back to Friday 10-09 15:00.
func TestWindowStart_UsesTheTSECalendar(t *testing.T) {
	got := disclosureskip.WindowStart(marketcalendar.TSE, jstAt(10, 13, 9, 30, 0))
	if want := jstAt(10, 9, 15, 0, 0); !got.Equal(want) {
		t.Fatalf("WindowStart = %s, want %s", got, want)
	}
	got = disclosureskip.WindowStart(marketcalendar.TSE, jstAt(10, 14, 9, 30, 0))
	if want := jstAt(10, 13, 15, 0, 0); !got.Equal(want) {
		t.Fatalf("WindowStart = %s, want %s", got, want)
	}
}

func TestEvaluate_OnlyEarningsAndForecastRevisionsMatch(t *testing.T) {
	decision := jstAt(10, 14, 10, 0, 0)
	at := jstAt(10, 14, 9, 0, 0)
	for _, tc := range []struct {
		title string
		want  disclosureskip.Verdict
		kind  disclosureskip.Kind
	}{
		{tanshin, disclosureskip.VerdictSkip, disclosureskip.KindEarnings},
		{"2026年3月期 決算短信〔IFRS〕（連結）", disclosureskip.VerdictSkip, disclosureskip.KindEarnings},
		{revision, disclosureskip.VerdictSkip, disclosureskip.KindForecastRevision},
		{"通期連結業績予想の上方修正に関するお知らせ", disclosureskip.VerdictSkip, disclosureskip.KindForecastRevision},
		{"業績予想の下方修正に関するお知らせ", disclosureskip.VerdictSkip, disclosureskip.KindForecastRevision},
		{"業績予想及び配当予想の修正に関するお知らせ", disclosureskip.VerdictSkip, disclosureskip.KindForecastRevision},
		{"配当予想の修正に関するお知らせ", disclosureskip.VerdictNoSkip, ""},
		{"剰余金の配当（中間配当）に関するお知らせ", disclosureskip.VerdictNoSkip, ""},
		{"期末配当に関するお知らせ", disclosureskip.VerdictNoSkip, ""},
		{"第5回無担保社債の償還完了に関するお知らせ", disclosureskip.VerdictNoSkip, ""},
		{"株主総会招集ご通知", disclosureskip.VerdictNoSkip, ""},
	} {
		t.Run(tc.title, func(t *testing.T) {
			got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{disclosure(tc.title, at)}, []disclosureskip.Fetch{completeFetch(decision.Add(time.Hour))})
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", got.Verdict, tc.want)
			}
			if tc.want == disclosureskip.VerdictSkip && (len(got.Matches) != 1 || got.Matches[0].Kind != tc.kind || got.Matches[0].Title != tc.title) {
				t.Fatalf("matches = %+v, want one %q match of the title", got.Matches, tc.kind)
			}
		})
	}
}

func TestEvaluate_OtherSymbolsDoNotMatch(t *testing.T) {
	decision := jstAt(10, 14, 10, 0, 0)
	other := disclosure(tanshin, jstAt(10, 14, 9, 0, 0))
	other.Symbol = "6758"
	got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{other}, []disclosureskip.Fetch{completeFetch(decision.Add(time.Hour))})
	if got.Verdict != disclosureskip.VerdictNoSkip {
		t.Fatalf("verdict = %q, want no_skip", got.Verdict)
	}
}

func TestEvaluate_MatchesAreListedOldestFirstWithTheirKinds(t *testing.T) {
	decision := jstAt(10, 14, 12, 0, 0)
	got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{
		disclosure(revision, jstAt(10, 14, 11, 0, 0)),
		disclosure("配当予想の修正に関するお知らせ", jstAt(10, 14, 10, 0, 0)),
		disclosure(tanshin, jstAt(10, 13, 15, 30, 0)),
	}, nil)
	if got.Verdict != disclosureskip.VerdictSkip || len(got.Matches) != 2 {
		t.Fatalf("result = %+v, want a skip with two matches", got)
	}
	if got.Matches[0].Kind != disclosureskip.KindEarnings || got.Matches[1].Kind != disclosureskip.KindForecastRevision {
		t.Fatalf("matches = %+v, want earnings then forecast_revision", got.Matches)
	}
}

// A feed hole is "unknown", never "no skip"; a disclosure that is known to
// exist stays a skip whatever the coverage.
func TestEvaluate_FeedGapsAreUnknown(t *testing.T) {
	decision := jstAt(10, 14, 10, 0, 0)
	from := disclosureskip.WindowStart(weekdaysOnly, decision) // Tuesday 10-13 15:00
	lateEnough := decision.Add(disclosureskip.FetchLag)
	for _, tc := range []struct {
		name    string
		fetches []disclosureskip.Fetch
		want    disclosureskip.Verdict
		reason  disclosureskip.UnknownReason
	}{
		{"no fetch recorded", nil, disclosureskip.VerdictUnknown, disclosureskip.ReasonNoFetch},
		{"only a failed fetch", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: false}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonNoFetch},
		{"only a fetch of another symbol", []disclosureskip.Fetch{{Symbol: "6758", At: lateEnough, OK: true}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonNoFetch},
		{"only a fetch from before the decision", []disclosureskip.Fetch{completeFetch(decision.Add(-time.Minute))}, disclosureskip.VerdictUnknown, disclosureskip.ReasonNoFetch},
		{"a fetch too soon after the decision (index lag)", []disclosureskip.Fetch{completeFetch(lateEnough.Add(-time.Second))}, disclosureskip.VerdictUnknown, disclosureskip.ReasonNoFetch},
		{"cut off by the item limit before the window start", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Truncated: true, Oldest: from.Add(time.Hour)}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonIncompleteFetch},
		{"cut off by the item limit exactly at the window start", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Truncated: true, Oldest: from}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonIncompleteFetch},
		{"cut off with no dated item", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Truncated: true}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonIncompleteFetch},
		{"an item without a publication time", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Undated: 1}}, disclosureskip.VerdictUnknown, disclosureskip.ReasonIncompleteFetch},
		{"a fetch exactly FetchLag after the decision covers", []disclosureskip.Fetch{completeFetch(lateEnough)}, disclosureskip.VerdictNoSkip, ""},
		{"a cut-off response reaching back before the window covers", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Truncated: true, Oldest: from.Add(-time.Second)}}, disclosureskip.VerdictNoSkip, ""},
		{"an uncut response covers whatever its oldest item", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: true, Oldest: from.Add(time.Hour)}}, disclosureskip.VerdictNoSkip, ""},
		{"a failure followed by a success covers", []disclosureskip.Fetch{{Symbol: sym, At: lateEnough, OK: false}, completeFetch(lateEnough.Add(time.Minute))}, disclosureskip.VerdictNoSkip, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, nil, tc.fetches)
			if got.Verdict != tc.want || got.Reason != tc.reason {
				t.Fatalf("verdict = %q (%q), want %q (%q)", got.Verdict, got.Reason, tc.want, tc.reason)
			}
			if (got.CoveredBy != nil) != (tc.want == disclosureskip.VerdictNoSkip) {
				t.Fatalf("CoveredBy = %v for verdict %q", got.CoveredBy, got.Verdict)
			}
		})
	}

	t.Run("a known disclosure is a skip even without coverage", func(t *testing.T) {
		got := disclosureskip.Evaluate(weekdaysOnly, sym, decision, []disclosureskip.Disclosure{disclosure(tanshin, jstAt(10, 14, 9, 0, 0))}, nil)
		if got.Verdict != disclosureskip.VerdictSkip || got.CoveredBy != nil {
			t.Fatalf("result = %+v, want a skip without coverage", got)
		}
	})
}

func TestClassify(t *testing.T) {
	for title, want := range map[string]disclosureskip.Kind{
		tanshin: disclosureskip.KindEarnings,
		"（訂正）決算短信の一部訂正": disclosureskip.KindEarnings,
		revision: disclosureskip.KindForecastRevision,
		"業績予想の修正（上方）に関するお知らせ":  disclosureskip.KindForecastRevision,
		"配当予想の修正に関するお知らせ":      "",
		"業績予想と実績値との差異に関するお知らせ": "",
		"決算説明会開催のお知らせ":         "",
		"":                     "",
	} {
		kind, ok := disclosureskip.Classify(title)
		if kind != want || ok != (want != "") {
			t.Errorf("Classify(%q) = %q, %v; want %q", title, kind, ok, want)
		}
	}
}
