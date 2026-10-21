// Package budget gives a read tool a soft deadline shorter than the call's
// own (the server's QueryTimeout, issue #850): a tool that runs out of its soft
// budget answers with the part it has read and says so, instead of the whole
// call failing as query_timeout.
package budget

import (
	"context"
	"time"
)

// MaxMargin is the most time kept back for building and sending the response
// once the soft budget is spent.
const MaxMargin = 500 * time.Millisecond

// Soft returns ctx limited to its deadline less a margin: a quarter of the time
// left, at most MaxMargin. A ctx without a deadline is returned as it is.
func Soft(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ctx, func() {}
	}
	margin := min(MaxMargin, time.Until(deadline)/4)
	return context.WithDeadline(ctx, deadline.Add(-margin))
}

// Spent reports whether err ended a statement because soft ran out while the
// call's own ctx is still alive: the caller returns what it has. Any error
// counts once soft has expired, as the server counts any error as a timeout
// once its ctx has (the driver's interrupt error is not a ctx error). An error
// while soft is alive, or after parent expired too, is not Spent.
func Spent(parent, soft context.Context, err error) bool {
	return err != nil && soft.Err() != nil && parent.Err() == nil
}
