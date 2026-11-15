package researchlabel

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// sideStatsSQL is one side's sums over the labeled rows of a (day, horizon): its
// row_count, sums and the spread counts research_label_day_stats keeps. %[1]s is
// long or short; the rows of the side are those whose net is not NULL.
const sideStatsSQL = `SELECT COUNT(%[1]s_net_bps), COALESCE(SUM(%[1]s_gross_bps), 0), COALESCE(SUM(%[1]s_net_bps), 0), COALESCE(SUM(%[1]s_cost_bps), 0),
	COALESCE(SUM(%[1]s_net_bps > 0), 0), COUNT(CASE WHEN %[1]s_net_bps IS NOT NULL THEN spread_bps END),
	COALESCE(SUM(CASE WHEN %[1]s_net_bps IS NOT NULL THEN spread_bps END), 0), COALESCE(SUM(spread_bps > %[1]s_cost_bps), 0),
	COALESCE(SUM(%[1]s_gross_bps - spread_bps / 2.0), 0)
FROM research_horizon_labels WHERE day = ? AND horizon = ?`

// sideStats are one row of research_label_day_stats.
type sideStats struct {
	horizon, side                           string
	rows, wins, spreadRows, spreadAboveCost int
	sumGross, sumNet, sumCost, sumSpread    float64
	sumGrossLessHalfSpread                  float64
}

// MarkDone records that the JST day is fully recorded: it sums the day's rows
// per horizon and side (research_label_day_stats) and writes those sums with
// the mark (research_label_days) in one short transaction, replacing an earlier
// mark of the day, so the sums always describe exactly the rows present when the
// day was marked. It is idempotent. snapshots is the number of bars the day had.
func (r *Repository) MarkDone(ctx context.Context, day string, snapshots int, completedAt time.Time) error {
	if _, _, err := DayBounds(day); err != nil {
		return err
	}
	var stats []sideStats
	counts := map[string]int{}
	for _, horizon := range rlabel.Horizons {
		var n int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_horizon_labels WHERE day = ? AND horizon = ?`, day, horizon).Scan(&n); err != nil {
			return fmt.Errorf("researchlabel: count %s rows of %s: %w", horizon, day, err)
		}
		counts[horizon] = n
		for _, side := range []struct{ name, col string }{{domain.JevDirectionLong, "long"}, {domain.JevDirectionShort, "short"}} {
			st := sideStats{horizon: horizon, side: side.name}
			err := r.db.QueryRowContext(ctx, fmt.Sprintf(sideStatsSQL, side.col), day, horizon).Scan(&st.rows, &st.sumGross, &st.sumNet, &st.sumCost,
				&st.wins, &st.spreadRows, &st.sumSpread, &st.spreadAboveCost, &st.sumGrossLessHalfSpread)
			if err != nil {
				return fmt.Errorf("researchlabel: sum %s %s rows of %s: %w", horizon, side.name, day, err)
			}
			if st.rows > 0 {
				stats = append(stats, st)
			}
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("researchlabel: begin mark of %s: %w", day, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM research_label_day_stats WHERE day = ?`, day); err != nil {
		return fmt.Errorf("researchlabel: clear stats of %s: %w", day, err)
	}
	for _, st := range stats {
		_, err := tx.ExecContext(ctx, `INSERT INTO research_label_day_stats (day, horizon, side, row_count, sum_gross, sum_net, sum_cost, wins,
	spread_rows, sum_spread, spread_above_cost, sum_gross_less_half_spread) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			day, st.horizon, st.side, st.rows, st.sumGross, st.sumNet, st.sumCost, st.wins, st.spreadRows, st.sumSpread, st.spreadAboveCost, st.sumGrossLessHalfSpread)
		if err != nil {
			return fmt.Errorf("researchlabel: write stats of %s: %w", day, err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at)
VALUES (?, ?, ?, ?, ?, ?)`, day, snapshots, counts[rlabel.Horizon30m], counts[rlabel.Horizon60m], counts[rlabel.HorizonClose], sqlutil.FormatTime(completedAt))
	if err != nil {
		return fmt.Errorf("researchlabel: mark %s done: %w", day, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("researchlabel: commit mark of %s: %w", day, err)
	}
	return nil
}
