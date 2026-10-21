//go:build !race

package metalabeltests

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// serverTimeout is the MCP's own read timeout in the tests: 0 keeps the real
// default (paperresultsmcp.QueryTimeout, 3 s).
const serverTimeout = 0

// readBudgets are the wall-clock budgets of the perf tests: the real read
// timeout of the MCP for each call.
var readBudgets = struct{ days30, days90, days366 time.Duration }{
	paperresultsmcp.QueryTimeout, paperresultsmcp.QueryTimeout, paperresultsmcp.QueryTimeout,
}
