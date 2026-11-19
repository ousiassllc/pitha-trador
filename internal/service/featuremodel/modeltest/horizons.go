package modeltest

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// horizonScale is how the planted signal of the 30-minute labels is scaled at the
// other horizons: the 60-minute labels carry twice the net bps and the close labels
// the opposite sign, so a model that learns a horizon from another horizon's labels
// predicts the wrong size or sign.
var horizonScale = map[string]float64{"30m": 1, "60m": 2, "close": -1}

// HorizonScale is the factor of the 30-minute labels' net bps at the horizon
// ("30m", "60m" or "close") in the fixture.
func HorizonScale(horizon string) float64 {
	s, ok := horizonScale[horizon]
	if !ok {
		panic(fmt.Sprintf("modeltest: unknown horizon %q", horizon))
	}
	return s
}

// SeedHorizons adds the 60-minute and close labels of the given days, derived from
// the days' seeded 30-minute labels: the same bars and sample buckets, the nets
// multiplied by HorizonScale, the exit 60 minutes after the 30-minute exit (60m) or
// at 15:30 JST of the same day (close, 06:30Z). A day that has its completion mark
// gets labeled_60m and labeled_close set to its row count. Labels that exist already
// are left as they are, so it can be repeated after more bars were added.
func SeedHorizons(t *testing.T, db *sql.DB, days ...string) {
	t.Helper()
	for _, day := range days {
		for _, h := range []struct{ name, exit string }{
			{"60m", `strftime('%Y-%m-%dT%H:%M:%S', exit_at, '+30 minutes') || substr(exit_at, 20)`},
			{"close", `strftime('%Y-%m-%dT06:30:00', snapshot_at) || substr(exit_at, 20)`},
		} {
			s := HorizonScale(h.name)
			// h.exit is one of the constant expressions above (never input), so the query is built by a replace.
			q := strings.Replace(`INSERT OR IGNORE INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at,
	entry_bid, entry_ask, entry_price, exit_price, spread_bps, long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version)
SELECT snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, ?, EXIT_EXPR,
	entry_bid, entry_ask, entry_price, exit_price, spread_bps, long_gross_bps * ?, long_cost_bps, long_net_bps * ?, short_gross_bps * ?, short_cost_bps, short_net_bps * ?, label_version
FROM research_horizon_labels WHERE day = ? AND horizon = '30m' ORDER BY snapshot_id`, "EXIT_EXPR", h.exit, 1)
			if _, err := db.ExecContext(t.Context(), q, h.name, s, s, s, s, day); err != nil {
				t.Fatalf("seed %s labels of %s: %v", h.name, day, err)
			}
		}
		if _, err := db.ExecContext(t.Context(), `UPDATE research_label_days SET labeled_60m = labeled_30m, labeled_close = labeled_30m WHERE day = ?`, day); err != nil {
			t.Fatalf("mark horizons of %s: %v", day, err)
		}
	}
}

// AllHorizonsDB is HistoryDB with the 60-minute and close labels of every day too.
func AllHorizonsDB(t *testing.T, start string, n, rows int) *sql.DB {
	t.Helper()
	return openTemplate(t, fmt.Sprintf("history-all-horizons/%s/%d/%d", start, n, rows), func(db *sql.DB) {
		SeedHorizons(t, db, SeedHistory(t, db, start, n, rows)...)
	})
}
