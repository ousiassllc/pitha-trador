package bucketroutes_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// Issue #835: with a fractional operating capital and no budget row stored, the
// stock budget the Risk Engine judges with (bucketsettings.Effective over the
// capital in force, i.e. Resolve) and the one the budgets page shows are the
// same whole-yen value - not the fractional capital - and Reset stores it.
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

	// What the Risk Engine judges with, read the way it reads it per evaluation.
	limits, err := papersettings.Effective(t.Context(), f.store, config.RiskLimits{})
	if err != nil {
		t.Fatal(err)
	}
	risk, err := bucketsettings.Effective(t.Context(), f.store, limits.InitialCapital)
	if err != nil {
		t.Fatal(err)
	}
	if risk.StockJPY != 500000 {
		t.Fatalf("Risk's stock budget = %v, want the whole-yen 500000", risk.StockJPY)
	}

	body := f.do(http.MethodGet, "/settings/risk", nil).Body.String()
	shown := "株 " + dailyguardsettings.FormatYen(risk.StockJPY) + " 円 + 債券ETF 0 円"
	if !strings.Contains(body, shown) {
		t.Errorf("計算結果 panel does not show the Risk value (%q): %s", shown, body)
	}
	if !strings.Contains(body, "株 500,000 円 + 債券ETF 0 円") {
		t.Errorf("計算結果 panel missing the whole-yen stock default: %s", body)
	}
	if strings.Contains(body, "株 500,000.5 円") {
		t.Errorf("計算結果 panel shows the fractional capital as the stock budget: %s", body)
	}

	// "Reset" stores the same value the page showed.
	if rec := f.save(bucketsettings.KeyStockBudgetJPY, strconv.FormatFloat(risk.StockJPY, 'f', -1, 64)); rec.Code != http.StatusOK {
		t.Fatalf("saving the shown default: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
