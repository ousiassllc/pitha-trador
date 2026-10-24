package performancepanels_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}
