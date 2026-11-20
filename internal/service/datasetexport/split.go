package datasetexport

import (
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// CheckSplit validates the split of the period [from, to) (JST midnights): split_at is a JST date
// strictly inside the period (after from, before to, so both sides have days), embargo_days is
// 0..MaxEmbargoDays, and the eval side (split_at + embargo_days) still starts inside the period.
func CheckSplit(from, to time.Time, splitAt string, embargoDays int) error {
	if embargoDays < 0 || embargoDays > MaxEmbargoDays {
		return fmt.Errorf("embargo_days must be 0..%d", MaxEmbargoDays)
	}
	if splitAt == "" {
		if embargoDays != 0 {
			return fmt.Errorf("embargo_days needs split_at")
		}
		return nil
	}
	day, err := time.ParseInLocation(time.DateOnly, splitAt, query.JST)
	if err != nil {
		return fmt.Errorf("split_at must be a date YYYY-MM-DD")
	}
	if !day.After(from) || !day.Before(to) {
		return fmt.Errorf("split_at must be inside the period: after from and before to")
	}
	if !day.AddDate(0, 0, embargoDays).Before(to) {
		return fmt.Errorf("split_at + embargo_days must be before to, or the eval side is empty")
	}
	return nil
}

// splitRows applies the split rule and returns the train and eval rows (their split cell set),
// the rows inside the embargo gap and the train rows purged because a label window reaches
// the eval start:
//
//	train  decision day < split_at, and the row's label window ends before the eval start
//	eval   decision day >= split_at + embargo_days
//	embargo in between: not exported, counted
//	purged  a train row whose label window (decision time + its longest label horizon, a
//	        "close" label ending at 15:30 JST) reaches the eval start: not exported, counted
//
// No two rows of different sides share a day, and no train label reads prices from the eval period.
func splitRows(rows []*row, splitAt string, embargoDays int) (train, eval []*row, embargo, purged int) {
	splitDay, _ := time.ParseInLocation(time.DateOnly, splitAt, query.JST)
	evalDay := splitDay.AddDate(0, 0, embargoDays)
	evalStart, evalDayText := evalDay, evalDay.Format(time.DateOnly)
	for _, r := range rows {
		switch {
		case r.day < splitAt:
			if !r.windowEnd.Before(evalStart) {
				purged++
				continue
			}
			r.cells[cSplit] = SplitTrain
			train = append(train, r)
		case r.day >= evalDayText:
			r.cells[cSplit] = SplitEval
			eval = append(eval, r)
		default:
			embargo++
		}
	}
	return train, eval, embargo, purged
}
