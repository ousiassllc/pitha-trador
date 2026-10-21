package bucketroutes_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// Issue #835: with a fractional operating capital and no budget row stored, the
// budgets page shows the whole-yen stock default (the value the Risk Engine
// judges with and Reset stores), not the fractional capital.
func TestPageStockDefaultIsRoundedDownForAFractionalCapital(t *testing.T) {
	f := newFixture(t)
	if err := f.store.Set(t.Context(), papersettings.KeyInitialCapitalJPY, "500000.5", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, key := range bucketsettings.Keys() {
		if err := f.store.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
	body := f.do(http.MethodGet, "/settings/asset-buckets", nil).Body.String()
	if !strings.Contains(body, "株 500,000 円 + 債券ETF 0 円") {
		t.Errorf("計算結果 panel missing the whole-yen stock default: %s", body)
	}
	if strings.Contains(body, "株 500,000.5 円") {
		t.Errorf("計算結果 panel shows the fractional capital as the stock budget: %s", body)
	}

	// "Reset" stores the same value the page showed.
	if rec := f.save(bucketsettings.KeyStockBudgetJPY, "500000"); rec.Code != http.StatusOK {
		t.Fatalf("saving the shown default: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
