// Package featuremodel persists the research-only feature model of issue #1092:
// research_model_runs, one row per walk-forward training (the model's features,
// hyperparameters, coefficients and training window), and
// research_model_predictions, the model's predicted long/short net bps for every
// trader LONG/SHORT Jev decision of the predicted day (migration 000064). The
// candidate ledger reads the two tables to compare the model with Jev on the same
// decision rows.
//
// The record is for research only. Policy, Risk, order placement, the Paper guard,
// Live, the Jev prompts, RAG, the meta-model training and self-improvement never
// read it: the only users are the training component (internal/service/featuremodel,
// wired by internal/bootstrap/nightlyjobs) and the read-only candidate ledger tool.
// isolation_test.go pins that. It reads research_horizon_labels, research_label_days,
// market_snapshots and jev_decisions (SELECT only) and writes only its own two
// tables; it MUST depend only on internal/repository/sqlutil. See
// docs/architecture/research-tools/research-model.md.
package featuremodel

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// jst is the zone of the days the model is trained and predicts for. Japan has no
// DST, so a fixed zone is exact.
var jst = time.FixedZone("JST", 9*60*60)

// Features are the market_snapshots columns that make up the model input, in the
// order of every vector in this package. They are numeric, derived features of the
// bar itself: never the raw board (bid, ask, depth, raw_data_json) and never a
// Jev output. A new feature is a new model version, so the list is a constant.
var Features = []string{
	"return_1m", "return_5m", "return_15m", "return_30m", "price_vs_vwap_bps", "volume_ratio_5m",
	"orderbook_imbalance", "realized_vol_5m", "spread_bps", "market_return_5m", "sector_return_5m", "vwap_slope",
}

// featureSelect is the "m.<column>, ..." list of Features, built once.
var featureSelect = func() string {
	cols := make([]string, len(Features))
	for i, f := range Features {
		cols[i] = "m." + f
	}
	return strings.Join(cols, ", ")
}()

// Repository reads the model's inputs and writes the model's two tables.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// DayBounds is the half-open UTC range [from, to) of the JST day ("2006-01-02").
func DayBounds(day string) (from, to time.Time, err error) {
	d, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("featuremodel: day %q: %w", day, err)
	}
	return d, d.AddDate(0, 0, 1), nil
}

// scanFeatures turns the nullable feature columns into a vector: a NULL or
// non-finite value is NaN (missing; the model imputes the training mean).
func scanFeatures(raw []sql.NullFloat64) []float64 {
	x := make([]float64, len(raw))
	for i, v := range raw {
		x[i] = math.NaN()
		if v.Valid && !math.IsNaN(v.Float64) && !math.IsInf(v.Float64, 0) {
			x[i] = v.Float64
		}
	}
	return x
}

// featureDests returns the scan destinations of the feature columns.
func featureDests(raw []sql.NullFloat64) []any {
	dests := make([]any, len(raw))
	for i := range raw {
		dests[i] = &raw[i]
	}
	return dests
}

// formatDayStart is the stored text of the start of the JST day.
func formatDayStart(day string) (string, error) {
	from, _, err := DayBounds(day)
	if err != nil {
		return "", err
	}
	return sqlutil.FormatTime(from), nil
}
