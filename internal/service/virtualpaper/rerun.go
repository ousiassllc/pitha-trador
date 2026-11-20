package virtualpaper

import (
	"context"
	"errors"
	"fmt"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
)

// RerunFailedDay makes the failed day of profile id re-run-eligible at now, in
// the transaction db is part of (the caller checks the MCP write switch in the
// same transaction). It takes only that day's mark away; the recorder's next run
// then replays the day like any pending day, and stores done, empty or failed
// again. Nothing replays here and no failed day is ever reset by itself: a
// failure stays failed until this is called, so the maintenance task cannot loop
// on one.
//
// Errors (*InvalidError, never carrying a stored value): an unknown profile
// (field profile_id), a stopped profile (its days are not recorded any more), a
// day that is not a "2006-01-02" date, a day without a mark, a day whose mark is
// done or empty, a failed day older than the recorder's back-fill window
// (BacklogDays), which would never be replayed.
func RerunFailedDay(ctx context.Context, db repo.DB, now time.Time, id int64, day string) error {
	p, err := repo.GetProfile(ctx, db, id)
	if errors.Is(err, repo.ErrNotFound) {
		return &InvalidError{"profile_id", "その profile_id のプロファイルはありません"}
	}
	if err != nil {
		return err
	}
	date, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil || date.Format(time.DateOnly) != day {
		return &InvalidError{"day", "JST の日付（YYYY-MM-DD）です"}
	}
	if p.Status != repo.StatusActive {
		return &InvalidError{"profile_id", "停止したプロファイルの日は記録されないので戻せません"}
	}
	d, err := repo.GetDay(ctx, db, id, day)
	if errors.Is(err, repo.ErrDayNotFound) {
		return &InvalidError{"day", "その日の記録（印）はありません。印の無い日は次の日次記録が再生します"}
	}
	if err != nil {
		return err
	}
	if d.Status != repo.DayFailed {
		return &InvalidError{"day", fmt.Sprintf("failed の日だけ戻せます（この日は %s です）", d.Status)}
	}
	today, err := time.ParseInLocation(time.DateOnly, now.In(jst).Format(time.DateOnly), jst)
	if err != nil {
		return err
	}
	if day < today.AddDate(0, 0, -BacklogDays).Format(time.DateOnly) {
		return &InvalidError{"day", fmt.Sprintf("今日から %d 日より前の日は再生されないので戻せません", BacklogDays)}
	}
	if _, err := repo.ResetFailedDay(ctx, db, id, day); err != nil {
		return err
	}
	return nil
}
