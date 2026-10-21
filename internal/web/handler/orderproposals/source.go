package orderproposals

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
)

// Source supplies the screens. internal/bootstrap/proposalview's Reader
// implements it over the application's database.
type Source interface {
	// List returns the proposals matching f, newest received first, and the
	// cursor of the next page ("" on the last).
	List(ctx context.Context, f proposal.ListFilter) ([]proposal.Item, string, error)
	// Get returns one proposal in full, or proposal.ErrNotFound.
	Get(ctx context.Context, id int64) (proposal.State, error)
}

// StaticSource is the idle Source (no proposal), the default of
// internal/router.New() until a real one is wired in.
type StaticSource struct{}

// List implements Source.
func (StaticSource) List(context.Context, proposal.ListFilter) ([]proposal.Item, string, error) {
	return nil, "", nil
}

// Get implements Source.
func (StaticSource) Get(context.Context, int64) (proposal.State, error) {
	return proposal.State{}, proposal.ErrNotFound
}
