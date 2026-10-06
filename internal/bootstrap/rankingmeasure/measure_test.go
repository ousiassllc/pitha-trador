package rankingmeasure_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingmeasure"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

type call struct {
	rankType int
	exchange string
}

type fakeRanker struct {
	calls []call
	fn    func(call) (marketdata.RankingMeasurement, error)
}

func (f *fakeRanker) MeasureRanking(_ context.Context, rankType int, exchange string) (marketdata.RankingMeasurement, error) {
	c := call{rankType, exchange}
	f.calls = append(f.calls, c)
	return f.fn(c)
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("decode log line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

func measurer(ranker *fakeRanker, open func(time.Time) bool) rankingmeasure.Measurer {
	return rankingmeasure.Measurer{
		Ranker: ranker, Types: []int{1, 2}, Exchanges: []string{"T", "TP", "TS"}, Open: open,
		Now: func() time.Time { return time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC) },
	}
}

func TestMeasurer_Cycle_MeasuresEveryTypeExchangePairAndLogsOnlyMeasurements(t *testing.T) {
	buf := captureLogs(t)
	ranker := &fakeRanker{fn: func(call) (marketdata.RankingMeasurement, error) {
		return marketdata.RankingMeasurement{Count: 50, DuplicateRanks: 2, LatestPriceTime: "09:15"}, nil
	}}
	if got := measurer(ranker, func(time.Time) bool { return true }).Cycle(context.Background()); got != 6 {
		t.Fatalf("Cycle requests = %d, want 6 (2 types x 3 exchanges)", got)
	}
	if len(ranker.calls) != 6 || ranker.calls[0] != (call{1, "T"}) || ranker.calls[5] != (call{2, "TS"}) {
		t.Errorf("calls = %v", ranker.calls)
	}
	lines := logLines(t, buf)
	if len(lines) != 6 {
		t.Fatalf("log lines = %d, want 6", len(lines))
	}
	first := lines[0]
	if first["msg"] != "rankingmeasure: ranking measured" || first["ok"] != true ||
		first["type"] != float64(1) || first["exchange"] != "T" ||
		first["result_count"] != float64(50) || first["duplicate_ranks"] != float64(2) ||
		first["current_price_time"] != "09:15" {
		t.Errorf("first line = %v", first)
	}
	if _, ok := first["duration_ms"]; !ok {
		t.Errorf("log line has no duration_ms: %v", first)
	}
	for key := range first {
		for _, banned := range []string{"price", "symbol", "volume"} {
			if strings.Contains(strings.ToLower(key), banned) && key != "current_price_time" {
				t.Errorf("log key %q leaks market data", key)
			}
		}
	}
}

func TestMeasurer_Cycle_LogsHTTPAndKabuCodeOnError(t *testing.T) {
	buf := captureLogs(t)
	ranker := &fakeRanker{fn: func(c call) (marketdata.RankingMeasurement, error) {
		if c.exchange == "TP" {
			return marketdata.RankingMeasurement{}, fmt.Errorf("wrapped: %w",
				&marketdata.APIError{StatusCode: 429, Code: marketdata.CodeAPIRateLimit})
		}
		return marketdata.RankingMeasurement{}, nil
	}}
	m := measurer(ranker, nil)
	m.Types = []int{1}
	if got := m.Cycle(context.Background()); got != 3 {
		t.Fatalf("Cycle requests = %d, want 3: one failure must not stop the rest", got)
	}
	failed := logLines(t, buf)[1]
	if failed["ok"] != false || failed["http_status"] != float64(429) ||
		failed["kabu_code"] != float64(marketdata.CodeAPIRateLimit) || failed["rate_limited"] != true {
		t.Errorf("failed line = %v", failed)
	}
}

func TestMeasurer_Cycle_SkipsOutsideSession(t *testing.T) {
	ranker := &fakeRanker{fn: func(call) (marketdata.RankingMeasurement, error) { return marketdata.RankingMeasurement{}, nil }}
	if got := measurer(ranker, func(time.Time) bool { return false }).Cycle(context.Background()); got != 0 {
		t.Errorf("Cycle requests = %d, want 0 outside the session", got)
	}
	if len(ranker.calls) != 0 {
		t.Errorf("calls = %v, want none", ranker.calls)
	}
}

func TestMeasurer_Cycle_ContinuesAfterPanic(t *testing.T) {
	captureLogs(t)
	ranker := &fakeRanker{fn: func(c call) (marketdata.RankingMeasurement, error) {
		if c.exchange == "T" {
			panic("boom")
		}
		return marketdata.RankingMeasurement{}, nil
	}}
	m := measurer(ranker, nil)
	m.Types = []int{1}
	if got := m.Cycle(context.Background()); got != 3 {
		t.Errorf("Cycle requests = %d, want 3", got)
	}
}

func TestMeasurer_Cycle_StopsWhenContextDone(t *testing.T) {
	ranker := &fakeRanker{fn: func(call) (marketdata.RankingMeasurement, error) { return marketdata.RankingMeasurement{}, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := measurer(ranker, nil).Cycle(ctx); got != 0 {
		t.Errorf("Cycle requests = %d, want 0 on a canceled ctx", got)
	}
}
