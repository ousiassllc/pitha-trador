// Package proposalview is the 注文案 screens' data source (issue #829): the
// read model the Paper results MCP's order_proposals_list / order_proposal_get
// use (internal/service/paperresultsmcp/proposal), read from the application's
// own database. The screens and the MCP tools therefore show the same values.
package proposalview

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
)

// Reader implements internal/web/handler/orderproposals.Source over DB. It only
// runs SELECT statements.
type Reader struct {
	DB *sql.DB
}

// List returns the proposals matching f, newest received first, and the cursor
// of the next page.
func (r Reader) List(ctx context.Context, f proposal.ListFilter) ([]proposal.Item, string, error) {
	return proposal.List(ctx, r.DB, f)
}

// Get returns one proposal in full, or proposal.ErrNotFound.
func (r Reader) Get(ctx context.Context, id int64) (proposal.State, error) {
	return proposal.Get(ctx, r.DB, id)
}
