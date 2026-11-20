package dailybars_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// The research night through the real 立花 adapter against the fake broker
// (kabu would be the selected broker; the collection needs only the adapter):
// the morning login loads the master, the evening night requests one history
// per universe symbol and the bars land in daily_bars.
func TestResearchNightThroughTheAdapterAndTheFakeBroker(t *testing.T) {
	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 6, 0)) // the morning login loads the master; nothing advances the clock behind the test
	fb := tt.New(t, clk)
	history := func(date, closing string) map[string]string {
		return map[string]string{"sDate": date, "pDOP": closing, "pDHP": closing, "pDLP": closing, "pDPP": closing, "pDV": "1000"}
	}
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		switch req.Body["sCLMID"] {
		case "CLMStkGetIssueSizyouMstKabu":
			return http.StatusOK, map[string]any{"sCLMID": "CLMStkGetIssueSizyouMstKabu", "p_errno": "0", "sResultCode": "0",
				"aCLMStkIssueSizyouMstKabu": []map[string]string{
					{"sIssueCode": "7203", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "01", "sZenzituOwarine": "3000"},
					{"sIssueCode": "8001", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "01", "sZenzituOwarine": "3000"},
					{"sIssueCode": "4488", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "09", "sZenzituOwarine": "300"}, // growth: out of the universe
				}}
		case "CLMMfdsGetMarketPriceHistory":
			return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPriceHistory", "p_errno": "0", "sResultCode": "0",
				"aCLMMfdsMarketPriceHistory": []map[string]string{history("20261007", "100"), history("20261008", "101")}}
		}
		return http.StatusOK, nil
	})
	a := adapter.New(adapter.Config{Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: fb.Server().Client(), Clock: clk})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); a.Run(ctx) }()
	tt.Eventually(t, func() bool { _, err := a.DailyBarTargets(ctx); return err == nil })

	f := newResearchFixture(t, night1) // the research night runs on its own clock; the adapter's moves to the evening
	clk.Set(night1)
	// The adapter's request queue paces itself with timers of its clock: a pump
	// moves that clock on (a bounded hour; the session closes at 03:30).
	pumped := make(chan struct{})
	go func() {
		for i := 0; i < 3600; i++ {
			select {
			case <-pumped:
				return
			case <-time.After(time.Millisecond):
				clk.Advance(time.Second)
			}
		}
	}()
	defer close(pumped)
	r := dailybars.NewResearch(dailybars.ResearchConfig{
		Settings: func(context.Context) (tachibanasource.TachibanaSourceSettings, error) { return f.settings, nil },
		Acquire:  func(context.Context) (dailybars.Source, error) { return a, nil },
		Bars:     f.bars, Store: f.store, Status: f.status, Clock: clk,
	})
	f.runDueCtx(t, ctx, r)

	want := []string{"7203/2026-10-07/100", "7203/2026-10-08/101", "8001/2026-10-07/100", "8001/2026-10-08/101"}
	if got := f.stored(t, "4488", "7203", "8001"); !reflect.DeepEqual(got, want) {
		t.Fatalf("daily_bars = %v, want %v", got, want)
	}
	if n := fb.Count("CLMMfdsGetMarketPriceHistory"); n != 2 {
		t.Fatalf("history requests = %d, want 2", n)
	}
	if run := f.run(t, "2026-10-08"); run.Status != domain.DailyBarRunSucceeded || run.Symbols != 2 || run.SavedBars != 4 {
		t.Fatalf("run = %+v", run)
	}
	if got := f.state(t); got != researchstatus.StateOK {
		t.Fatalf("status = %q", got)
	}
}
