package sqlutil

import (
	"context"
	"strings"
)

// In returns the condition "column IN (?, ...)" with its arguments for the
// given values, or "" and no arguments when values is empty. column is a
// package-internal SQL fragment, never user input.
func In(column string, values []string) (string, []any) {
	return inList(column, " IN ", values)
}

// NotIn is In with NOT IN: "column NOT IN (?, ...)", or "" and no arguments
// when values is empty (nothing to exclude).
func NotIn(column string, values []string) (string, []any) {
	return inList(column, " NOT IN ", values)
}

func inList(column, op string, values []string) (string, []any) {
	if len(values) == 0 {
		return "", nil
	}
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return column + op + "(?" + strings.Repeat(",?", len(values)-1) + ")", args
}

// ForeignTraderVersions returns the Jev Trader prompt versions whose
// jev_decisions rows (question_version) are NOT of the live version's cohort,
// i.e. the versions the live prompt's answers must never be aggregated or
// learned together with (issue #881). The consumers (the RAG candidate search,
// the Calibration samples, the meta-model training) leave those rows out; a row
// of any other version, including one this build has never heard of, stays in,
// so a build with a single cohort selects exactly the rows it always did.
//
// The function is the only place that knows what "the live version" is
// (internal/service/jev.LiveCohortScope); the repositories and the RAG service
// only apply the list it returns. A nil function leaves every row in.
type ForeignTraderVersions func(ctx context.Context) ([]string, error)
