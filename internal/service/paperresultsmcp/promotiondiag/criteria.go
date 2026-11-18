package promotiondiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// settingsReader is a read-only runtime_settings view limited to the five research.promotion.*
// keys: runtime_settings can hold secret paths, so any other key is refused before SQL runs.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !promotionsettings.IsKey(key) {
		return "", false, fmt.Errorf("promotiondiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("promotiondiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

func valuesOf(c promotionsettings.Criteria) CriteriaValues {
	return CriteriaValues{c.MinCommonRows, c.MinDays, c.MinDiffCILowerBps, c.MinPositiveDayPct, c.MaxAdjustedP}
}

// effectiveCriteria is the criteria in force: the floors, tightened by the stored settings (a
// stored value looser than its floor is ignored by promotionsettings.Effective), then by the call.
// A per-call value looser than the criterion in force is an invalid argument.
func effectiveCriteria(ctx context.Context, q query.Q, req Request) (Criteria, promotionsettings.Criteria, error) {
	floor := promotionsettings.Floor()
	cur, err := promotionsettings.Effective(ctx, settingsReader{q})
	if err != nil {
		return Criteria{}, cur, err
	}
	source := map[string]string{}
	for _, key := range promotionsettings.Keys() {
		source[key] = "floor"
	}
	if cur.MinCommonRows != floor.MinCommonRows {
		source[promotionsettings.KeyMinCommonRows] = "setting"
	}
	if cur.MinDays != floor.MinDays {
		source[promotionsettings.KeyMinDays] = "setting"
	}
	if cur.MinDiffCILowerBps != floor.MinDiffCILowerBps {
		source[promotionsettings.KeyMinDiffCILowerBps] = "setting"
	}
	if cur.MinPositiveDayPct != floor.MinPositiveDayPct {
		source[promotionsettings.KeyMinPositiveDayPct] = "setting"
	}
	if cur.MaxAdjustedP != floor.MaxAdjustedP {
		source[promotionsettings.KeyMaxAdjustedP] = "setting"
	}
	for _, o := range req.overrides() {
		before := cur
		if err := cur.Tighten(o.key, o.v); err != nil {
			return Criteria{}, cur, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		if cur != before {
			source[o.key] = "call"
		}
	}
	return Criteria{
		Effective: valuesOf(cur), Floor: valuesOf(floor), Source: source, MinCoverage: MinCoverage, Note: criteriaNote,
	}, cur, nil
}
