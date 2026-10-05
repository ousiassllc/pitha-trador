package assist_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func TestAPIError_ErrorLengthIndependentOfBodySize(t *testing.T) {
	err := &assist.APIError{StatusCode: http.StatusServiceUnavailable, Body: strings.Repeat("<div>down</div>\n", 60_000)}

	msg := err.Error()
	if len(msg) > 300 {
		t.Fatalf("Error() length = %d, want a bounded excerpt (<= 300)", len(msg))
	}
	if strings.ContainsAny(msg, "\r\n") {
		t.Fatalf("Error() = %q, want single-line", msg)
	}
	if !strings.Contains(msg, "status 503") {
		t.Fatalf("Error() = %q, want the status code kept", msg)
	}
}
