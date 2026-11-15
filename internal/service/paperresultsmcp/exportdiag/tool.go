// Package exportdiag is the read-only analysis behind the Paper results MCP's training_dataset_export tool.
package exportdiag

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description.
const Description = "scaffold"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any { return map[string]any{} }

// Request is the tool's arguments as decoded from the call.
type Request struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// Run is the tool.
func Run(_ context.Context, _ query.Q, _ query.Range, _ time.Time, _ Request) (any, int, error) {
	return map[string]any{}, 0, nil
}
