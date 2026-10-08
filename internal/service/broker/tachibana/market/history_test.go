package market_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestFetchHistoryMapsRawAndAdjustedBars(t *testing.T) {
	c, fb, clk := setup(t)
	clk.SetNow(tt.AtJST(2026, 10, 8, 18, 0)) // history requests are held back in the daytime
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPriceHistory" {
			return http.StatusOK, nil
		}
		return http.StatusOK, ok("CLMMfdsGetMarketPriceHistory", map[string]any{"aCLMMfdsMarketPriceHistory": []map[string]string{
			// Out of order on purpose: the result is sorted oldest first.
			{"sDate": "20191010", "pDOP": "4170", "pDHP": "4300", "pDLP": "4100", "pDPP": "4200", "pDV": "1000",
				"pDOPxK": "521.25", "pDHPxK": "537.5", "pDLPxK": "512.5", "pDPPxK": "525", "pDVxK": "8000"},
			{"sDate": "20191009", "pDOP": "4260", "pDHP": "4450", "pDLP": "4000", "pDPP": "4170", "pDV": "1863400"}, // no adjusted values: raw ones
			{"sDate": "20191011", "pDOP": "", "pDHP": "", "pDLP": "", "pDPP": "", "pDV": ""},                        // no trading: dropped
			{"sDate": "bad", "pDOP": "1", "pDHP": "1", "pDLP": "1", "pDPP": "1"},                                    // no date: dropped
		}})
	})

	bars, err := market.FetchHistory(context.Background(), c, "7071")
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("bars = %+v, want 2", bars)
	}
	first, second := bars[0], bars[1]
	if first.Symbol != "7071" || first.TradeDate != "2019-10-09" || first.Open != 4260 || first.High != 4450 || first.Low != 4000 ||
		first.Close != 4170 || first.Volume != 1863400 || first.AdjClose != 4170 || first.AdjVolume != 1863400 {
		t.Errorf("first = %+v", first)
	}
	if second.TradeDate != "2019-10-10" || second.Close != 4200 || second.AdjOpen != 521.25 || second.AdjClose != 525 || second.AdjVolume != 8000 {
		t.Errorf("second = %+v", second)
	}

	req := fb.Requests()[len(fb.Requests())-1]
	if req.Body["sIssueCode"] != "7071" || req.Body["sSizyouC"] != "00" {
		t.Errorf("request = %+v, want one symbol on the 東証", req.Body)
	}
}

func TestFetchHistoryWithoutAListIsEmptyNotAnError(t *testing.T) {
	c, fb, clk := setup(t)
	clk.SetNow(tt.AtJST(2026, 10, 8, 18, 0))
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		return http.StatusOK, ok("CLMMfdsGetMarketPriceHistory", map[string]any{"sIssueCode": "1111"})
	})
	bars, err := market.FetchHistory(context.Background(), c, "1111")
	if err != nil || len(bars) != 0 {
		t.Fatalf("bars = %+v, err = %v; want none", bars, err)
	}
}

func TestMasterKeepsTheDailyBarTargetsAcrossDays(t *testing.T) {
	c, fb, clk := setup(t)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		return http.StatusOK, ok("CLMStkGetIssueSizyouMstKabu", map[string]any{"aCLMStkIssueSizyouMstKabu": []map[string]string{
			{"sIssueCode": "7203", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "01", "sZenzituOwarine": "3840.000000"},
			{"sIssueCode": "8001", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "04", "sZenzituOwarine": "100"},
			{"sIssueCode": "2150", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "02", "sZenzituOwarine": "550"},
			{"sIssueCode": "4488", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "09", "sZenzituOwarine": ""},
			{"sIssueCode": "1306", "sZyouzyouSizyou": "00", "sZyouzyouKubun": "00", "sZenzituOwarine": "2900"},
			{"sIssueCode": "1301", "sZyouzyouSizyou": "03", "sZyouzyouKubun": "01"}, // another market: ignored
		}})
	})
	m := market.NewMaster(c, clk)
	if _, err := m.DailyBarTargets(); !errors.Is(err, market.ErrMasterNotLoaded) {
		t.Fatalf("targets before the first load = %v, want ErrMasterNotLoaded", err)
	}
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The next JST day Info is stale, the targets of the last load stay available.
	clk.SetNow(tt.AtJST(2026, 10, 9, 18, 0))
	got, err := m.DailyBarTargets()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		market string
		prev   float64
	}{
		"7203": {"prime", 3840}, "8001": {"standard", 100}, "2150": {"standard", 550}, "4488": {"growth", 0}, "1306": {"other", 2900},
	}
	if len(got) != len(want) {
		t.Fatalf("targets = %+v, want %d", got, len(want))
	}
	for _, g := range got {
		w, ok := want[g.Symbol]
		if !ok || g.Market != w.market || g.PrevClose != w.prev {
			t.Errorf("target %+v, want %+v", g, w)
		}
	}
}
