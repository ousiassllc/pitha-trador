package dataexport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// Secrets planted in every free-text / JSON column that can carry one; no
// archive may contain any of them (issue #770/#771 redaction).
func TestExport_CSVArchiveWithManifest(t *testing.T) {
	e := newEnv(t)
	manifest, files := export(t, e, dataexport.Request{Kinds: allKinds, Period: period, Format: dataexport.FormatCSV})

	if manifest.SchemaVersion != dataexport.SchemaVersion || manifest.Format != dataexport.FormatCSV {
		t.Fatalf("manifest header = %+v", manifest)
	}
	var fromZip dataexport.Manifest
	if err := json.Unmarshal([]byte(files[dataexport.ManifestFile]), &fromZip); err != nil {
		t.Fatalf("manifest.json: %v\n%s", err, files[dataexport.ManifestFile])
	}
	if fromZip.SchemaVersion != 1 || !fromZip.Period.From.Equal(period.From) || !fromZip.Period.To.Equal(period.To) {
		t.Fatalf("manifest.json schema/period = %+v", fromZip)
	}

	rows := map[dataexport.Kind]int64{}
	for _, k := range fromZip.Kinds {
		rows[k.Kind] = k.Rows
	}
	// trades: 2 orders (entry + exit); decisions: 2 decisions.
	if rows[dataexport.KindTrades] != 2 || rows[dataexport.KindDecisions] != 2 {
		t.Fatalf("manifest rows = %v, want trades=2 decisions=2", rows)
	}

	// Every table file named in the manifest exists and its row count matches the CSV.
	for _, k := range fromZip.Kinds {
		for _, f := range k.Files {
			content, ok := files[f.Path]
			if !ok {
				t.Fatalf("manifest lists %s but the archive has no such entry (entries: %v)", f.Path, keys(files))
			}
			if got := int64(len(csvRecords(t, content))); got != f.Rows {
				t.Errorf("%s: manifest rows = %d, csv rows = %d", f.Path, f.Rows, got)
			}
		}
	}
}

func TestExport_TradesContainOrdersFillsClosesAndPnL(t *testing.T) {
	e := newEnv(t)
	_, files := export(t, e, dataexport.Request{Kinds: []dataexport.Kind{dataexport.KindTrades}, Period: period, Format: dataexport.FormatCSV})

	if _, ok := files["decisions/decisions.csv"]; ok {
		t.Fatalf("decisions exported though only trades were requested: %v", keys(files))
	}

	orders := csvRecords(t, files["trades/orders.csv"])
	if len(orders) != 2 || orders[0]["side"] != "BUY" || orders[0]["status"] != "FILLED" ||
		orders[0]["filled_price"] != "2111" || orders[0]["fees"] != "55" || orders[0]["slippage_bps"] != "1.5" {
		t.Fatalf("orders = %v, want entry fill then exit order", orders)
	}
	if orders[1]["side"] != "SELL" || orders[1]["filled_price"] != "2121" {
		t.Fatalf("exit order = %v", orders[1])
	}

	closed := csvRecords(t, files["trades/closed_positions.csv"])
	if len(closed) != 1 {
		t.Fatalf("closed_positions = %v, want 1 row", closed)
	}
	c := closed[0]
	if c["realized_pnl"] != "1000" || c["entry_price"] != "2111" || c["exit_price"] != "2121" || c["decision_id"] != itoa(e.decisionID) {
		t.Fatalf("closed position = %v", c)
	}
	if !strings.HasPrefix(c["return_pct"], "0.47") { // 1000 / (2111*100) * 100
		t.Errorf("return_pct = %q, want ~0.4737", c["return_pct"])
	}

	if signals := csvRecords(t, files["trades/signals.csv"]); len(signals) != 2 {
		t.Fatalf("signals = %v, want 2", signals)
	}
}

func TestExport_DecisionsLinkedByDecisionID(t *testing.T) {
	e := newEnv(t)
	manifest, files := export(t, e, dataexport.Request{Kinds: []dataexport.Kind{dataexport.KindDecisions}, Period: period, Format: dataexport.FormatJSONL})
	id := float64(e.decisionID)

	decisions := jsonlRecords(t, files["decisions/decisions.jsonl"])
	if len(decisions) != 2 || decisions[0]["decision_id"] != id {
		t.Fatalf("decisions = %v", decisions)
	}
	d := decisions[0]
	if d["latency_ms"] != float64(842) || d["question_version"] != "trader-v3" || d["confidence"] != 0.82 {
		t.Fatalf("decision request/response fields = %v", d)
	}
	// request and response are nested JSON documents, not double-encoded strings.
	req, ok := d["request_state_json"].(map[string]any)
	if !ok || req["symbol"] != "7203" {
		t.Fatalf("request_state_json = %#v, want nested object", d["request_state_json"])
	}
	if resp, ok := d["response_json"].(map[string]any); !ok || resp["direction"] != "LONG" {
		t.Fatalf("response_json = %#v, want nested object", d["response_json"])
	}

	features := jsonlRecords(t, files["decisions/features.jsonl"])
	if len(features) != 1 || features[0]["decision_id"] != id || features[0]["price"] != float64(2110) ||
		features[0]["vwap"] != float64(2100) || features[0]["return_5m"] != 0.004 {
		t.Fatalf("features = %v, want the one snapshot bar of decision %v", features, id)
	}
	if _, leaked := features[0]["raw_data_json"]; leaked {
		t.Fatalf("features expose raw_data_json: %v", features[0])
	}

	signals := jsonlRecords(t, files["decisions/signals.jsonl"])
	if len(signals) != 2 || signals[0]["decision_id"] != id || signals[1]["decision_id"] != float64(e.skippedID) {
		t.Fatalf("signals = %v", signals)
	}
	if reason, _ := signals[1]["reject_reason"].(string); !strings.Contains(reason, "risk rejected") {
		t.Fatalf("reject_reason = %v, want the reject reason kept", signals[1]["reject_reason"])
	}

	outcomes := jsonlRecords(t, files["decisions/outcomes.jsonl"])
	if len(outcomes) != 1 || outcomes[0]["decision_id"] != id || outcomes[0]["horizon_minutes"] != float64(5) ||
		outcomes[0]["was_direction_correct"] != true {
		t.Fatalf("outcomes = %v", outcomes)
	}
	skips := jsonlRecords(t, files["decisions/label_skips.jsonl"])
	if len(skips) != 1 || skips[0]["decision_id"] != float64(e.skippedID) || skips[0]["reason"] != "no bars after lunch break" {
		t.Fatalf("label_skips = %v", skips)
	}

	var decisionRows int64 = -1
	for _, k := range manifest.Kinds {
		if k.Kind == dataexport.KindDecisions {
			decisionRows = k.Rows
		}
	}
	if decisionRows != 2 {
		t.Fatalf("manifest decisions rows = %d, want 2", decisionRows)
	}
}

func TestExport_PeriodIsHalfOpen(t *testing.T) {
	e := newEnv(t)
	// [t0, t0+30m): the first decision (t0) is in, the second (t0+30m) is out.
	p := dataexport.Period{From: t0, To: t0.Add(30 * time.Minute)}
	manifest, files := export(t, e, dataexport.Request{Kinds: []dataexport.Kind{dataexport.KindDecisions}, Period: p, Format: dataexport.FormatCSV})
	if got := len(csvRecords(t, files["decisions/decisions.csv"])); got != 1 || manifest.Kinds[0].Rows != 1 {
		t.Fatalf("decisions in [t0, t0+30m) = %d (manifest %d), want 1", got, manifest.Kinds[0].Rows)
	}
	if got := len(csvRecords(t, files["decisions/label_skips.csv"])); got != 0 {
		t.Fatalf("label_skips of the excluded decision exported: %d rows", got)
	}
}

func TestExport_EmptyPeriodStillWritesHeadersAndZeroCounts(t *testing.T) {
	e := newEnv(t)
	far := dataexport.Period{From: t0.AddDate(1, 0, 0), To: t0.AddDate(1, 0, 1)}
	manifest, files := export(t, e, dataexport.Request{Kinds: allKinds, Period: far, Format: dataexport.FormatCSV})
	for _, k := range manifest.Kinds {
		if k.Rows != 0 {
			t.Errorf("%s rows = %d, want 0", k.Kind, k.Rows)
		}
	}
	if !strings.HasPrefix(files["trades/orders.csv"], "order_id,") {
		t.Fatalf("empty orders.csv = %q, want a header line", files["trades/orders.csv"])
	}
}

func TestExport_RejectsInvalidRequests(t *testing.T) {
	e := newEnv(t)
	for name, req := range map[string]dataexport.Request{
		"no kinds":      {Period: period, Format: dataexport.FormatCSV},
		"unknown kind":  {Kinds: []dataexport.Kind{"metrics"}, Period: period, Format: dataexport.FormatCSV},
		"duplicate":     {Kinds: []dataexport.Kind{dataexport.KindTrades, dataexport.KindTrades}, Period: period, Format: dataexport.FormatCSV},
		"unknown fmt":   {Kinds: allKinds, Period: period, Format: "xml"},
		"empty period":  {Kinds: allKinds, Period: dataexport.Period{From: t0, To: t0}, Format: dataexport.FormatCSV},
		"negative span": {Kinds: allKinds, Period: dataexport.Period{From: t0, To: t0.Add(-time.Hour)}, Format: dataexport.FormatCSV},
	} {
		var buf bytes.Buffer
		if _, err := e.svc.Export(context.Background(), &buf, req); !errors.Is(err, dataexport.ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: wrote %d bytes before validating", name, buf.Len())
		}
	}
}

func TestExport_PropagatesCollectorError(t *testing.T) {
	boom := errors.New("boom")
	svc := dataexport.New(failingCollector{err: boom})
	var buf bytes.Buffer
	_, err := svc.Export(context.Background(), &buf, dataexport.Request{Kinds: []dataexport.Kind{"failing"}, Period: period, Format: dataexport.FormatCSV})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
}

type failingCollector struct{ err error }

func (failingCollector) Kind() dataexport.Kind { return "failing" }
func (c failingCollector) Tables() []dataexport.Table {
	return []dataexport.Table{{
		Name:    "t",
		Columns: []dataexport.Column{{Name: "a"}},
		Read: func(context.Context, dataexport.Period, func(dataexport.Row) error) error {
			return c.err
		},
	}}
}
