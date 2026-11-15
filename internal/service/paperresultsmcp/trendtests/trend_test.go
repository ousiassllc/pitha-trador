package trendtests

import (
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/evalrecord/evalfixture"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedWeek is the fixture (nets in bps, see evalfixture's scenario table). Cohort
// model-1/trader-v3 over five days, model-2/trader-v4 on the first two:
//
//	10-05  id1 LONG up r5 +1 passed  llm +30 | id2 SHORT up r5 +1  llm -30            -> n 2, llm sum 0, random 0
//	10-06  id3 LONG down r5 -1 llm -30 | id4 SHORT down r5 -1 llm +30 | id5 LONG mild  -> n 3, llm 0, random -10
//	10-07  id6 SHORT dip llm 0 (random -10) | id7 LONG up passed llm +30             -> n 2, llm 30, random -10
//	10-08  id8 LONG dip llm -20 (random -10) | id9 LONG without bars (excluded)       -> n 1 of 2, llm -20, random -10
//	10-09  id10 SHORT mild llm -20 (random -10)                                       -> n 1, llm -20, random -10
//
// model-2/trader-v4: 10-05 id11 LONG down (llm -30), 10-06 id12 SHORT down (llm +30).
func seedWeek(t *testing.T, w *sql.DB) {
	t.Helper()
	const m1, v3, m2, v4 = "model-1", "trader-v3", "model-2", "trader-v4"
	evalfixture.Seed(t, w,
		evalfixture.Spec{ID: 1, At: at(5, 0), Direction: "LONG", Model: m1, Version: v3, Scenario: evalfixture.Up, R5: f(1), Passed: true},
		evalfixture.Spec{ID: 2, At: at(5, 20), Direction: "SHORT", Model: m1, Version: v3, Scenario: evalfixture.Up, R5: f(1)},
		evalfixture.Spec{ID: 11, At: at(5, 40), Direction: "LONG", Model: m2, Version: v4, Scenario: evalfixture.Down, R5: f(1), Instr: 2},
		evalfixture.Spec{ID: 3, At: at(6, 0), Direction: "LONG", Model: m1, Version: v3, Scenario: evalfixture.Down, R5: f(-1)},
		evalfixture.Spec{ID: 4, At: at(6, 20), Direction: "SHORT", Model: m1, Version: v3, Scenario: evalfixture.Down, R5: f(-1)},
		evalfixture.Spec{ID: 5, At: at(6, 40), Direction: "LONG", Model: m1, Version: v3, Scenario: evalfixture.Mild},
		evalfixture.Spec{ID: 12, At: at(6, 50), Direction: "SHORT", Model: m2, Version: v4, Scenario: evalfixture.Down, R5: f(-1), Instr: 2},
		evalfixture.Spec{ID: 6, At: at(7, 0), Direction: "SHORT", Model: m1, Version: v3, Scenario: evalfixture.Dip},
		evalfixture.Spec{ID: 7, At: at(7, 20), Direction: "LONG", Model: m1, Version: v3, Scenario: evalfixture.Up, R5: f(1), Passed: true},
		evalfixture.Spec{ID: 8, At: at(8, 0), Direction: "LONG", Model: m1, Version: v3, Scenario: evalfixture.Dip, R5: f(-1)},
		evalfixture.Spec{ID: 9, At: at(8, 20), Direction: "LONG", Model: m1, Version: v3},
		evalfixture.Spec{ID: 10, At: at(9, 0), Direction: "SHORT", Model: m1, Version: v3, Scenario: evalfixture.Mild},
	)
}

func near(a, b float64) bool { return math.Abs(a-b) < eps }

func wantFloat(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || !near(*got, want) {
		t.Errorf("%s = %v, want %v", what, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// The series reads the day rows the recorder wrote: counts, win rate, the means of every arm,
// llm minus random and the label settings of each day, with the exclusion of id9 showing as
// excluded (a decision without bars) and no row at all for days that were not recorded.
func TestTrend_DailyRowsAreTheRecordedDays(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	recordPending(t, recorder(w, 9))
	d := srv.call(t, nil).data(t)

	if len(d.Series) != 2 {
		t.Fatalf("series = %d, want 2 (one per model/version): %+v", len(d.Series), d.Series)
	}
	s := seriesOf(t, d, "model-1", "trader-v3")
	if s.HorizonMinutes != 10 || s.FirstDay != "2026-10-05" || s.LastDay != "2026-10-09" || s.DaysRecorded != 5 || len(s.Daily) != 5 {
		t.Fatalf("series = %+v", s)
	}
	if s.Decisions != 10 || s.Evaluated != 9 || s.Excluded != 1 {
		t.Errorf("totals decisions %d evaluated %d excluded %d, want 10/9/1", s.Decisions, s.Evaluated, s.Excluded)
	}
	d8 := s.Daily[3]
	if d8.Day != "2026-10-08" || d8.Decisions != 2 || d8.Evaluated != 1 || d8.Excluded != 1 || d8.Entered != 0 {
		t.Errorf("10-08 = %+v", d8)
	}
	wantFloat(t, "10-08 llm", d8.LLMMeanNetBps, -20)
	wantFloat(t, "10-08 random", d8.RandomMeanNetBps, -10)
	wantFloat(t, "10-08 llm-random", d8.LLMMinusRandomMeanBps, -10)
	wantFloat(t, "10-08 win rate", d8.WinRate, 0)
	d7 := s.Daily[2]
	if d7.Evaluated != 2 || d7.Entered != 1 {
		t.Errorf("10-07 = %+v", d7)
	}
	wantFloat(t, "10-07 llm", d7.LLMMeanNetBps, 15)
	wantFloat(t, "10-07 random", d7.RandomMeanNetBps, -5)
	wantFloat(t, "10-07 llm-random", d7.LLMMinusRandomMeanBps, 20)
	wantFloat(t, "10-07 win rate", d7.WinRate, 0.5)
	d5 := s.Daily[0]
	wantFloat(t, "10-05 momentum", d5.MomentumMeanNetBps, 30) // both rows have return_5m > 0: the LONG leg (+30) each
	wantFloat(t, "10-05 contrarian", d5.ContrarianMeanNetBps, -30)
	if ls := d5.LabelSettings; ls.HorizonMinutes != 10 || ls.TakeProfitBps != 30 || ls.StopLossBps != 30 || ls.BarrierCostBps != 10 {
		t.Errorf("label settings = %+v", ls)
	}
	if s.LabelSettingsChanged || len(s.LabelSettingsChangedOn) != 0 {
		t.Errorf("settings did not change but label_settings_changed = %v %v", s.LabelSettingsChanged, s.LabelSettingsChangedOn)
	}
	// Cumulative: llm sum 0+0+30-20-20 = -10 over 9 decisions; random -10*4 = -40 -> mean -40/9; diff mean 30/9.
	if c := s.Cumulative.LLM; c.N != 9 || c.Days != 5 {
		t.Errorf("cumulative llm = %+v", c)
	}
	wantFloat(t, "cumulative llm mean", s.Cumulative.LLM.MeanNetBps, -10.0/9)
	wantFloat(t, "cumulative llm-random mean", s.Cumulative.LLMMinusRandom.MeanDiffBps, 30.0/9)
	if v4 := seriesOf(t, d, "model-2", "trader-v4"); v4.DaysRecorded != 2 || v4.Daily[0].Day != "2026-10-05" || v4.Daily[1].Day != "2026-10-06" {
		t.Errorf("v4 series = %+v", v4)
	}
}

// The cumulative statistics are the very numbers jev_evaluation's statistics give on the rows the days
// were made of: mean, bootstrap interval, win rate, detectable minimum, days (llm), and mean, interval
// (llm minus random).
func TestTrend_CumulativeEqualsSummarizeOfTheUnderlyingRows(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	recordPending(t, recorder(w, 9))
	d := srv.call(t, nil).data(t)
	s := seriesOf(t, d, "model-1", "trader-v3")

	rows, _, err := evaldiag.Rows(t.Context(), w, query.Range{From: at(5, 0).Add(-9 * time.Hour), To: at(10, 0).Add(-9 * time.Hour)}, evaldiag.Request{ModelID: "model-1"})
	if err != nil || len(rows) != 9 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	var llm, diff []evaldiag.Point
	for _, r := range rows {
		llm = append(llm, evaldiag.Point{Day: r.Day, Value: r.LLM})
		diff = append(diff, evaldiag.Point{Day: r.Day, Value: r.LLM - r.Random})
	}
	want, wantDiff := evaldiag.Summarize(llm, false), evaldiag.Summarize(diff, false)
	got := s.Cumulative.LLM
	if got.N != want.N || got.Days != want.Days || want.CI == nil || got.CI == nil {
		t.Fatalf("cumulative llm = %+v, want %+v", got, want.Metrics)
	}
	wantFloat(t, "mean", got.MeanNetBps, *want.MeanNetBps)
	wantFloat(t, "ci lower", &got.CI.Lower, want.CI.Lower)
	wantFloat(t, "ci upper", &got.CI.Upper, want.CI.Upper)
	wantFloat(t, "win rate", got.WinRate, *want.WinRate)
	wantFloat(t, "detectable minimum", got.DetectableMinimumEdgeBps, *want.DetectableMinimumEdgeBps)
	if len(got.DaysToDetect) != len(want.DaysToDetect) || len(got.Daily) != 0 {
		t.Errorf("days_to_detect %v daily %v", got.DaysToDetect, got.Daily)
	}
	g := s.Cumulative.LLMMinusRandom
	if g.N != wantDiff.N || g.Days != wantDiff.Days || g.CI == nil {
		t.Fatalf("llm-random = %+v, want %+v", g, wantDiff.Metrics)
	}
	wantFloat(t, "diff mean", g.MeanDiffBps, *wantDiff.MeanNetBps)
	wantFloat(t, "diff ci lower", &g.CI.Lower, wantDiff.CI.Lower)
	wantFloat(t, "diff ci upper", &g.CI.Upper, wantDiff.CI.Upper)
	wantFloat(t, "diff detectable", g.DetectableMinimumDifferenceBps, *wantDiff.DetectableMinimumEdgeBps)
}

// A label-setting change between two recorded days flags the series (and names the day), a series
// whose days all used the same settings is not flagged, and the day rows keep the settings of their day.
func TestTrend_LabelSettingsChangeIsFlagged(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	r := recorder(w, 9)
	for range 2 { // 10-05 and 10-06 under the default settings
		if _, err := r.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	testkit.Setting(t, w, "calibration.barrier_cost_bps", "14")
	recordPending(t, r)
	d := srv.call(t, nil).data(t)
	s := seriesOf(t, d, "model-1", "trader-v3")
	if !s.LabelSettingsChanged || len(s.LabelSettingsChangedOn) != 1 || s.LabelSettingsChangedOn[0] != "2026-10-07" {
		t.Fatalf("v3: changed %v on %v, want true on 10-07", s.LabelSettingsChanged, s.LabelSettingsChangedOn)
	}
	if s.Daily[1].LabelSettings.BarrierCostBps != 10 || s.Daily[2].LabelSettings.BarrierCostBps != 14 || s.Daily[4].LabelSettings.BarrierCostBps != 14 {
		t.Errorf("settings per day = %+v", s.Daily)
	}
	// The record is of its day: the first days keep their numbers (a SHORT mild is -20 at cost 10, -24 at 14).
	wantFloat(t, "10-09 llm at the new cost", s.Daily[4].LLMMeanNetBps, -24)
	if v4 := seriesOf(t, d, "model-2", "trader-v4"); v4.LabelSettingsChanged || len(v4.LabelSettingsChangedOn) != 0 {
		t.Errorf("v4 had no change but is flagged: %+v", v4)
	}
}

// The period, the three filters and the recording block: a narrower period reads fewer days but the
// recording block is the whole table's; the horizon filter separates series of different horizons.
func TestTrend_FiltersAndPeriod(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	recordPending(t, recorder(w, 9))
	testkit.MustExec(t, w, `INSERT INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
		decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps)
		VALUES ('2026-10-06', 'model-1', 'trader-v3', 20, 30, 30, 10, 4, 3, 1, 2, 60, 30, 0, 0)`)

	all := srv.call(t, nil).data(t)
	if len(all.Series) != 3 {
		t.Fatalf("series = %d, want 3 (v3 at 10 and 20 minutes, v4)", len(all.Series))
	}
	h20 := srv.call(t, map[string]any{"horizon_minutes": 20}).data(t)
	if len(h20.Series) != 1 || h20.Series[0].HorizonMinutes != 20 || h20.HorizonMinutes == nil || *h20.HorizonMinutes != 20 {
		t.Fatalf("horizon filter = %+v", h20)
	}
	wantFloat(t, "20-minute llm mean", h20.Series[0].Daily[0].LLMMeanNetBps, 20)
	wantFloat(t, "20-minute llm-random", h20.Series[0].Daily[0].LLMMinusRandomMeanBps, 10)
	if got := srv.call(t, map[string]any{"prompt_version": "trader-v4"}).data(t); len(got.Series) != 1 || got.Series[0].QuestionVersion != "trader-v4" {
		t.Errorf("prompt_version filter = %+v", got.Series)
	}
	if got := srv.call(t, map[string]any{"model_id": "model-2"}).data(t); len(got.Series) != 1 || got.Series[0].ModelID != "model-2" {
		t.Errorf("model_id filter = %+v", got.Series)
	}
	if got := srv.call(t, map[string]any{"model_id": "nobody"}).data(t); got.Series == nil || len(got.Series) != 0 {
		t.Errorf("no match = %+v, want an empty (non-null) list", got.Series)
	}
	narrow := srv.call(t, map[string]any{"from": "2026-10-07", "to": "2026-10-09", "horizon_minutes": 10, "model_id": "model-1"}).data(t)
	if len(narrow.Series) != 1 || narrow.Series[0].DaysRecorded != 2 || narrow.Series[0].FirstDay != "2026-10-07" || narrow.Series[0].LastDay != "2026-10-08" {
		t.Fatalf("narrow period = %+v", narrow.Series)
	}
	if r := narrow.Recording; r.DaysRecorded != 5 || r.FirstRecordedDay == nil || *r.FirstRecordedDay != "2026-10-05" || *r.LastRecordedDay != "2026-10-09" {
		t.Errorf("recording = %+v, want the whole table (5 days, 10-05..10-09)", r)
	}
}

// recording tells whether the record is on and how long it is kept, from the two research settings
// (default off / 365 days), and is empty (null days) when nothing is recorded.
func TestTrend_RecordingReflectsTheSettings(t *testing.T) {
	srv, w := newServer(t)
	d := srv.call(t, nil).data(t)
	r := d.Recording
	if r.Enabled || r.RetentionDays != 365 || r.DaysRecorded != 0 || r.FirstRecordedDay != nil || r.LastRecordedDay != nil || d.Series == nil || len(d.Series) != 0 {
		t.Fatalf("empty/default = %+v series %v", r, d.Series)
	}
	testkit.Setting(t, w, "research.eval.enabled", "1")
	testkit.Setting(t, w, "research.eval.retention_days", "45")
	if r := srv.call(t, nil).data(t).Recording; !r.Enabled || r.RetentionDays != 45 {
		t.Errorf("recording = %+v, want enabled with 45 days", r)
	}
	testkit.Setting(t, w, "research.eval.retention_days", "0") // damaged: the default applies
	if r := srv.call(t, nil).data(t).Recording; r.RetentionDays != 365 {
		t.Errorf("damaged retention = %d, want the default 365", r.RetentionDays)
	}
}
