package jquants

import (
	"context"
	"fmt"
	"log/slog"
)

// PurgeBatchRows is how many rows one transaction deletes from each table.
const PurgeBatchRows = 1000

// PurgeStore deletes the J-Quants tables in batches (repository/jquants).
type PurgeStore interface {
	PurgeBatch(ctx context.Context, limit int) (deleted map[string]int, done bool, err error)
}

// DerivedPurger deletes the rows derived from J-Quants data: the research
// horizon labels whose exit bar came from it (jquantsrelabel.Relabeler.PurgeDerived).
type DerivedPurger interface {
	PurgeDerived(ctx context.Context) (int, error)
}

// PurgeResult is what a purge deleted.
type PurgeResult struct {
	// Tables are the rows deleted per J-Quants table; DerivedLabels the research
	// label rows whose exit came from J-Quants.
	Tables        map[string]int
	DerivedLabels int
}

// Purge is the deletion J-Quants' terms ask for after cancelling: the raw data
// and what can be turned back into it. It deletes ONLY J-Quants-sourced rows: the
// research_jquants_* data tables and the coverage marks, and the research label
// rows that carry exit_source = 'jquants'. Every other source's rows (market
// snapshots, daily bars, disclosures, the recorder's own labels) are untouched
// (purge_test.go proves it). It needs neither the key nor the switch, and makes
// no HTTP request. The request ledger (counts and fixed error codes) is kept: it
// holds no market data, and deleting it would reset the day's request count.
func Purge(ctx context.Context, store PurgeStore, derived DerivedPurger) (PurgeResult, error) {
	res := PurgeResult{Tables: map[string]int{}}
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		deleted, done, err := store.PurgeBatch(ctx, PurgeBatchRows)
		for table, n := range deleted {
			res.Tables[table] += n
		}
		if err != nil {
			return res, err
		}
		if done {
			break
		}
	}
	n, err := derived.PurgeDerived(ctx)
	res.DerivedLabels = n
	if err != nil {
		return res, fmt.Errorf("jquants: purge derived labels: %w", err)
	}
	slog.InfoContext(ctx, "jquants: stored J-Quants data deleted", "tables", res.Tables, "derived_labels", res.DerivedLabels)
	return res, nil
}
