package labelcompare

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Response is experiment_compare's data with the label-based side added: the
// whole trade-based Comparison, embedded so that every one of its members keeps
// its name and value, and the new label_based member next to them.
type Response struct {
	experiment.Comparison
	LabelBased Result `json:"label_based"`
}

// Attach computes the label-based side of c, the comparison of exp made at now
// (c's own windows are used), and returns both. The trade-based c is returned
// as it is.
func Attach(ctx context.Context, q query.Q, exp experiment.Experiment, now time.Time, c experiment.Comparison) (Response, error) {
	before, after := exp.Windows(now)
	res, err := Compute(ctx, q, InputOf(c, before, after))
	if err != nil {
		return Response{}, err
	}
	return Response{Comparison: c, LabelBased: res}, nil
}
