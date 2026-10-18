package mcptests

import (
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
	}
	srv.call(t, "risk_rejections", period).data(t, "risk_rejections", &d)
	if d.RiskPassed != 2 || d.OtherRej != 2 {
		t.Fatalf("passed=%d other=%d, want 2 / 2", d.RiskPassed, d.OtherRej)
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
			if j.Succeeded != 2 || j.Failed != 1 || j.LatestSuccess == nil || *j.LatestSuccess != "2026-10-03T00:00:00Z" {
				t.Errorf("market-data = %+v", j)
			}
		case "jev-trader":
			// Nothing finished in the period, but the latest success overall is shown.
			if j.Succeeded != 0 || j.Failed != 0 || j.LatestSuccess == nil || *j.LatestSuccess != "2026-10-17T00:00:00Z" {
				t.Errorf("jev-trader = %+v", j)
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
}
