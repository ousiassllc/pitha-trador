package bucketroutes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
)

// Issue #844: the refusal shown on the Settings screen has no internal
// package prefix.
func TestRefusalMessageHasNoPackagePrefix(t *testing.T) {
	f := newFixture(t)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"sum over the capital": f.save(bucketsettings.KeyBondETFBudgetJPY, "300000"),
		"not a whole yen":      f.save(bucketsettings.KeyStockBudgetJPY, "1.5"),
		"reset over the sum":   f.reset(bucketsettings.KeyStockBudgetJPY),
	} {
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, body = %s; want 400", name, rec.Code, body)
		}
		if strings.Contains(body, "bucketsettings:") {
			t.Errorf("%s: refusal shows the package prefix: %s", name, body)
		}
	}
}
