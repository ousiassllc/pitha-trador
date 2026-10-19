package mcptests

import (
	"fmt"
	"slices"
	"testing"
)

type tradeRow struct {
	ID          int      `json:"id"`
	Symbol      string   `json:"symbol"`
	Side        string   `json:"side"`
	Quantity    int      `json:"quantity"`
	EntryPrice  float64  `json:"entry_price"`
	OpenedAt    string   `json:"opened_at"`
	ExitPrice   *float64 `json:"exit_price"`
	ClosedAt    string   `json:"closed_at"`
	ExitReason  *string  `json:"exit_reason"`
	RealizedPnl float64  `json:"realized_pnl"`
	FeesJPY     *float64 `json:"fees_jpy"`
}

type tradesData struct {
	Trades     []tradeRow `json:"trades"`
	NextCursor *string    `json:"next_cursor"`
}

func TestPaperTrades(t *testing.T) {
	srv := seededServer(t, nil)
	var all tradesData
	srv.call(t, "paper_trades", period).data(t, "paper_trades", &all)
	var ids []int
	for _, tr := range all.Trades {
		ids = append(ids, tr.ID)
	}
	// closed_at descending: 5, 4, 3, 2, 1.
	if !slices.Equal(ids, []int{5, 4, 3, 2, 1}) || all.NextCursor != nil {
		t.Fatalf("ids = %v next=%v", ids, all.NextCursor)
	}
	first, second := all.Trades[4], all.Trades[3] // ids 1 and 2
	if first.Symbol != "7203" || first.Side != "LONG" || first.Quantity != 100 || first.EntryPrice != 1000 ||
		first.RealizedPnl != 1000 || first.OpenedAt != "2026-09-30T00:00:00Z" || first.ClosedAt != "2026-10-01T00:30:00Z" ||
		first.ExitReason == nil || *first.ExitReason != "take_profit" {
		t.Fatalf("trade 1 = %+v", first)
	}
	near(t, "trade1 exit_price", first.ExitPrice, 1010)
	near(t, "trade1 fees", first.FeesJPY, 30) // entry 10 + exit 20
	wantNull(t, "trade2 exit_price", second.ExitPrice)
	near(t, "trade2 fees", second.FeesJPY, 15) // entry order only
	wantNull(t, "trade3 fees", all.Trades[2].FeesJPY)

	// Paging: limit 2 -> [5,4] + cursor -> [3,2] + cursor -> [1], no cursor.
	var got []int
	cursor := ""
	for range 5 {
		a := args(map[string]any{"limit": 2})
		if cursor != "" {
			a["cursor"] = cursor
		}
		var p tradesData
		srv.call(t, "paper_trades", a).data(t, "paper_trades", &p)
		for _, tr := range p.Trades {
			got = append(got, tr.ID)
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if !slices.Equal(got, []int{5, 4, 3, 2, 1}) {
		t.Fatalf("paged ids = %v", got)
	}
}

func TestMetalabelHistory(t *testing.T) {
	srv := seededServer(t, nil)
	var d struct {
		Models []struct {
			Version               string   `json:"version"`
			Status                string   `json:"status"`
			RejectReason          string   `json:"reject_reason"`
			RejectReasonTruncated bool     `json:"reject_reason_truncated"`
			TrainedAt             string   `json:"trained_at"`
			TrainSamples          int      `json:"train_samples"`
			OosSamples            int      `json:"oos_samples"`
			OosAUC                *float64 `json:"oos_auc"`
			OosEV                 *float64 `json:"oos_expected_value_bps"`
		} `json:"models"`
		PredictionCounts struct {
			Take int `json:"take"`
			Skip int `json:"skip"`
		} `json:"prediction_counts"`
		RetrainSkips []struct {
			SkippedAt    string `json:"skipped_at"`
			ReasonCode   string `json:"reason_code"`
			UsableRows   int    `json:"usable_rows"`
			RequiredRows int    `json:"required_rows"`
		} `json:"retrain_skips"`
	}
	srv.call(t, "metalabel_history", period).data(t, "metalabel_history", &d)
	if len(d.Models) != 2 || d.Models[0].Version != "v2" || d.Models[1].Version != "v1" {
		t.Fatalf("models (trained_at desc, period only) = %+v", d.Models)
	}
	rejected, active := d.Models[0], d.Models[1]
	if rejected.Status != "rejected" || !rejected.RejectReasonTruncated ||
		len([]rune(rejected.RejectReason)) != 200 || rejected.TrainedAt != "2026-10-05T00:00:00Z" {
		t.Fatalf("rejected model = %+v", rejected)
	}
	wantNull(t, "rejected oos_auc", rejected.OosAUC)
	wantNull(t, "rejected oos_ev", rejected.OosEV)
	near(t, "oos_auc", active.OosAUC, 0.61)
	near(t, "oos_ev", active.OosEV, 3.5)
	if active.RejectReasonTruncated || active.TrainSamples != 400 || active.OosSamples != 100 {
		t.Fatalf("active model = %+v", active)
	}
	if d.PredictionCounts.Take != 3 || d.PredictionCounts.Skip != 2 {
		t.Fatalf("prediction counts = %+v, want take 3 skip 2", d.PredictionCounts)
	}
	// Retrain skips (issue #799): newest first, only the period; "必要130件中 87件"
	// is usable 87 of required 130; an unknown stored code is returned as "other".
	if len(d.RetrainSkips) != 2 {
		t.Fatalf("retrain skips = %+v, want the 2 in the period", d.RetrainSkips)
	}
	if k := d.RetrainSkips[0]; k.ReasonCode != "other" || k.UsableRows != 12 || k.RequiredRows != 130 || k.SkippedAt != "2026-10-04T18:00:00Z" {
		t.Fatalf("newest retrain skip = %+v, want the unknown code normalized to other, 12 of 130", k)
	}
	if k := d.RetrainSkips[1]; k.ReasonCode != "too_few_rows_before_oos" || k.UsableRows != 87 || k.RequiredRows != 130 {
		t.Fatalf("older retrain skip = %+v, want too_few_rows_before_oos 87 of 130", k)
	}
}

func TestRiskRejections(t *testing.T) {
	srv := seededServer(t, nil)
	var d struct {
		RiskPassed int `json:"risk_passed_count"`
		OtherRej   int `json:"other_reject_count"`
		Buckets    []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"buckets"`
		OtherBreakdown []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"other_reject_breakdown"`
	}
	srv.call(t, "risk_rejections", period).data(t, "risk_rejections", &d)
	if d.RiskPassed != 2 || d.OtherRej != 10 {
		t.Fatalf("passed=%d other=%d, want 2 / 10", d.RiskPassed, d.OtherRej)
	}
	// Issue #800: the non-risk rejections by fixed code, no numbers or free
	// text; the breakdown always adds up to other_reject_count.
	var breakdown []string
	sum := 0
	for _, b := range d.OtherBreakdown {
		breakdown = append(breakdown, fmt.Sprintf("%s=%d", b.Reason, b.Count))
		sum += b.Count
	}
	wantBreakdown := []string{
		"jev_none=2", "meta_skip:no_active_model=2", "entry_quality_below_threshold=1", "meta_model_unavailable=1",
		"meta_skip:ev_not_above_minimum=1", "meta_skip:other=1", "other=1", "unknown=1",
	}
	if !slices.Equal(breakdown, wantBreakdown) {
		t.Fatalf("other_reject_breakdown = %v, want %v", breakdown, wantBreakdown)
	}
	if sum != d.OtherRej {
		t.Fatalf("breakdown sums to %d, want other_reject_count %d", sum, d.OtherRej)
	}
	var got []string
	for _, b := range d.Buckets {
		got = append(got, b.Reason+"="+string(rune('0'+b.Count)))
	}
	want := []string{"daily_loss_limit=2", "unknown=2", "max_daily_trades=1"}
	if !slices.Equal(got, want) {
		t.Fatalf("buckets = %v, want %v", got, want)
	}
}

func TestKillSwitchHistory(t *testing.T) {
	srv := seededServer(t, nil)
	var d struct {
		Events []struct {
			ID          int     `json:"id"`
			TriggeredAt string  `json:"triggered_at"`
			Reason      string  `json:"reason"`
			ResolvedAt  *string `json:"resolved_at"`
			ResolvedBy  *string `json:"resolved_by"`
		} `json:"events"`
	}
	srv.call(t, "kill_switch_history", period).data(t, "kill_switch_history", &d)
	if len(d.Events) != 2 {
		t.Fatalf("events = %+v", d.Events)
	}
	e2, e1 := d.Events[0], d.Events[1] // newest first
	if e2.ID != 2 || e2.Reason != "operator_manual" || e2.TriggeredAt != "2026-10-05T03:00:00Z" ||
		e2.ResolvedAt != nil || e2.ResolvedBy != nil {
		t.Fatalf("unresolved event = %+v", e2)
	}
	if e1.ID != 1 || e1.Reason != "daily_loss_limit" || e1.ResolvedAt == nil || *e1.ResolvedAt != "2026-10-02T04:00:00Z" ||
		e1.ResolvedBy == nil || *e1.ResolvedBy != "auto" {
		t.Fatalf("resolved event = %+v", e1)
	}
}

func TestCollectionStatus(t *testing.T) {
	srv := seededServer(t, nil)
	var d struct {
		LatestSnapshotAt *string  `json:"latest_snapshot_at"`
		MissingDates     []string `json:"missing_session_dates"`
		Jobs             []struct {
			Queue         string  `json:"queue"`
			Succeeded     int     `json:"succeeded_count"`
			Failed        int     `json:"failed_count"`
			LatestSuccess *string `json:"latest_succeeded_finished_at"`
		} `json:"jobs"`
		DailyBarFailed int `json:"daily_bar_run_failed_count"`
		LabelSkips     int `json:"label_skip_count"`
		JobFailures    struct {
			Total int `json:"total"`
			ByDay []struct {
				Date  string `json:"date"`
				Count int    `json:"count"`
			} `json:"by_day"`
			ByQueue []struct {
				Queue string `json:"queue"`
				Count int    `json:"count"`
			} `json:"by_queue"`
			ByErrorCode []struct {
				Code  string `json:"code"`
				Count int    `json:"count"`
			} `json:"by_error_code"`
			ByDayAndCode []struct {
				Date  string `json:"date"`
				Code  string `json:"code"`
				Count int    `json:"count"`
			} `json:"by_day_and_error_code"`
			SkippedTotal   int `json:"skipped_total"`
			SkippedByQueue []struct {
				Queue string `json:"queue"`
				Count int    `json:"count"`
			} `json:"skipped_by_queue"`
		} `json:"job_failures"`
		Retention struct {
			SucceededJobDays int    `json:"succeeded_job_days"`
			FailedJobDays    int    `json:"failed_job_days"`
			Note             string `json:"note"`
		} `json:"retention"`
	}
	srv.call(t, "collection_status", period).data(t, "collection_status", &d)
	if d.LatestSnapshotAt == nil || *d.LatestSnapshotAt != "2026-10-16T06:00:00Z" {
		t.Fatalf("latest_snapshot_at = %v", d.LatestSnapshotAt)
	}
	// Trading days 10-01..10-09 minus the days with snapshots (01, 02, 05, 06).
	if want := []string{"2026-10-07", "2026-10-08", "2026-10-09"}; !slices.Equal(d.MissingDates, want) {
		t.Fatalf("missing = %v, want %v", d.MissingDates, want)
	}
	var queues []string
	for _, j := range d.Jobs {
		queues = append(queues, j.Queue)
		switch j.Queue {
		case "market-data":
			if j.Succeeded != 2 || j.Failed != 3 || j.LatestSuccess == nil || *j.LatestSuccess != "2026-10-03T00:00:00Z" {
				t.Errorf("market-data = %+v", j)
			}
		case "jev-trader":
			// Nothing finished in the period, but the latest success overall is shown.
			if j.Succeeded != 0 || j.Failed != 0 || j.LatestSuccess == nil || *j.LatestSuccess != "2026-10-17T00:00:00Z" {
				t.Errorf("jev-trader = %+v", j)
			}
		case "jev-scout":
			if j.Succeeded != 0 || j.Failed != 2 || j.LatestSuccess != nil {
				t.Errorf("jev-scout = %+v, want 0 succeeded / 2 failed", j)
			}
		case "outcome-labeling":
			if j.Succeeded != 2 || j.Failed != 0 { // terminal skips are succeeded jobs
				t.Errorf("outcome-labeling = %+v, want 2 succeeded / 0 failed", j)
			}
		case "analytics":
			if j.Succeeded != 1 || j.Failed != 0 {
				t.Errorf("analytics = %+v, want 1 succeeded / 0 failed", j)
			}
		default:
			if j.Succeeded != 0 || j.Failed != 0 || j.LatestSuccess != nil {
				t.Errorf("%s = %+v, want zeros", j.Queue, j)
			}
		}
	}
	if want := []string{"market-data", "feature-calc", "jev-scout", "jev-trader", "outcome-labeling", "analytics"}; !slices.Equal(queues, want) {
		t.Fatalf("queues = %v", queues)
	}
	if d.DailyBarFailed != 2 || d.LabelSkips != 2 {
		t.Fatalf("daily_bar_failed=%d label_skips=%d, want 2 / 2", d.DailyBarFailed, d.LabelSkips)
	}

	// Issue #801: the failures by JST day, queue and broker error code (the
	// code is the only thing parsed out of last_error), and the skip counts.
	f := d.JobFailures
	if f.Total != 5 {
		t.Fatalf("job_failures.total = %d, want 5 (3 market-data + 2 jev-scout; the 09-01 one is outside)", f.Total)
	}
	var byDay, byQueue, byCode, byDayCode []string
	for _, x := range f.ByDay {
		byDay = append(byDay, fmt.Sprintf("%s=%d", x.Date, x.Count))
	}
	for _, x := range f.ByQueue {
		byQueue = append(byQueue, fmt.Sprintf("%s=%d", x.Queue, x.Count))
	}
	for _, x := range f.ByErrorCode {
		byCode = append(byCode, fmt.Sprintf("%s=%d", x.Code, x.Count))
	}
	for _, x := range f.ByDayAndCode {
		byDayCode = append(byDayCode, fmt.Sprintf("%s/%s=%d", x.Date, x.Code, x.Count))
	}
	for name, c := range map[string]struct{ got, want []string }{
		"by_day":                {byDay, []string{"2026-10-03=1", "2026-10-06=3", "2026-10-07=1"}},
		"by_queue":              {byQueue, []string{"market-data=3", "jev-scout=2"}},
		"by_error_code":         {byCode, []string{"4002006=2", "-62=1", "4001007=1", "unknown=1"}},
		"by_day_and_error_code": {byDayCode, []string{"2026-10-03/unknown=1", "2026-10-06/4002006=2", "2026-10-06/4001007=1", "2026-10-07/-62=1"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("job_failures.%s = %v, want %v", name, c.got, c.want)
		}
	}
	if f.SkippedTotal != 2 || len(f.SkippedByQueue) != 1 || f.SkippedByQueue[0].Queue != "outcome-labeling" || f.SkippedByQueue[0].Count != 2 {
		t.Errorf("skipped = %d %+v, want 2 outcome-labeling terminal skips (a deferred note is not one)", f.SkippedTotal, f.SkippedByQueue)
	}
	if d.Retention.SucceededJobDays != 7 || d.Retention.FailedJobDays != 30 || d.Retention.Note == "" {
		t.Errorf("retention = %+v, want succeeded 7 / failed 30 days with a note", d.Retention)
	}
}
