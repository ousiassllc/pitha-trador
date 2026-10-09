package market_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func masterRows() []map[string]string {
	return []map[string]string{
		{"sIssueCode": "1301", "sZyouzyouSizyou": "00", "sNehabaMin": "3140.000000", "sNehabaMax": "4540.000000", "sSinyouC": "1"},
		{"sIssueCode": "7203", "sZyouzyouSizyou": "00", "sNehabaMin": "2500.000000", "sNehabaMax": "3500.000000", "sSinyouC": "2"},
		{"sIssueCode": "9999", "sZyouzyouSizyou": "00", "sNehabaMin": "0.000000", "sNehabaMax": "", "sSinyouC": "3"},
		{"sIssueCode": "1301", "sZyouzyouSizyou": "03", "sNehabaMin": "1.000000", "sNehabaMax": "2.000000", "sSinyouC": "1"}, // another market: ignored
	}
}

func serveMaster(fb *tt.FakeBroker) {
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMStkGetIssueSizyouMstKabu" {
			return http.StatusOK, ok("CLMStkGetIssueSizyouMstKabu", map[string]any{"aCLMStkIssueSizyouMstKabu": masterRows()})
		}
		return http.StatusOK, nil
	})
}

func TestMasterLoadsOncePerDayAndServesSymbolInfo(t *testing.T) {
	c, fb, clk := setup(t)
	serveMaster(fb)
	m := market.NewMaster(c, clk)

	if _, err := m.Info("1301"); !errors.Is(err, market.ErrMasterNotLoaded) {
		t.Fatalf("Info before the morning fetch = %v, want ErrMasterNotLoaded", err)
	}
	// Loaded in the daytime (a process that starts then): once, and never again that day.
	for i := 0; i < 3; i++ {
		if err := m.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := fb.Count("CLMStkGetIssueSizyouMstKabu"); n != 1 {
		t.Fatalf("master requests = %d, want exactly 1 on the day", n)
	}

	info, err := m.Info("1301")
	if err != nil || info.Lendable == nil || !*info.Lendable || *info.UpperLimit != 4540 || *info.LowerLimit != 3140 {
		t.Errorf("1301 = %+v, %v; want 貸借, 3140〜4540 (TSE row, not the other market's)", info, err)
	}
	if info, _ := m.Info("7203"); *info.Lendable {
		t.Error("信用制度銘柄 (sSinyouC=2) must not be 貸借")
	}
	if info, _ := m.Info("9999"); info.UpperLimit != nil || info.LowerLimit != nil {
		t.Errorf("empty/zero limits must be unknown, got %+v", info)
	}
	if _, err := m.Info("0000"); !errors.Is(err, tachibana.ErrNoData) {
		t.Errorf("Info of an unlisted symbol = %v, want ErrNoData", err)
	}

	// The next JST day the master is stale until the morning login loads it again.
	clk.SetNow(tt.AtJST(2026, 10, 9, 5, 36))
	if _, err := m.Info("1301"); !errors.Is(err, market.ErrMasterNotLoaded) {
		t.Errorf("Info the next morning = %v, want ErrMasterNotLoaded", err)
	}
	if _, err := c.Login(context.Background(), tt.AuthID, fb.Key()); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(context.Background()); err != nil || fb.Count("CLMStkGetIssueSizyouMstKabu") != 2 {
		t.Errorf("next-day load: err=%v requests=%d, want 2", err, fb.Count("CLMStkGetIssueSizyouMstKabu"))
	}
}

func TestMasterLoadWithRetryRecovers(t *testing.T) {
	c, fb, clk := setup(t)
	var mu sync.Mutex
	calls := 0
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMStkGetIssueSizyouMstKabu" {
			return http.StatusOK, nil
		}
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return http.StatusOK, tt.ControlError("-2", "busy")
		}
		return http.StatusOK, ok("CLMStkGetIssueSizyouMstKabu", map[string]any{"aCLMStkIssueSizyouMstKabu": masterRows()})
	})
	m := market.NewMaster(c, clk)
	m.LoadWithRetry(context.Background())
	if _, err := m.Info("1301"); err != nil || calls != 2 {
		t.Errorf("Info = %v after %d master requests, want loaded after the retry", err, calls)
	}
}

func TestFetchIssuesDecodesShiftJIS(t *testing.T) {
	c, fb, _ := setup(t)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMStkGetIssueMstKabu" {
			return http.StatusOK, nil
		}
		return http.StatusOK, ok("CLMStkGetIssueMstKabu", map[string]any{"aCLMStkIssueMstKabu": []map[string]string{
			{"sIssueCode": "1301", "sIssueName": "極 洋", "sIssueNameRyaku": "極洋", "sBaibaiTani": "100", "sBaibaiTeisiC": " ", "sYusenSizyou": "00", "sGyousyuCode": "0050"},
			{"sIssueCode": "1302", "sIssueName": "髙島屋ｱﾙﾌｧ", "sBaibaiTeisiC": "9"},
		}})
	})
	issues, err := market.FetchIssues(context.Background(), c)
	if err != nil || len(issues) != 2 {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
	if issues[0].Name != "極 洋" || issues[0].ShortName != "極洋" || issues[0].TradingUnit != 100 || issues[0].Halted || issues[0].SectorCode != "0050" {
		t.Errorf("issue 0 = %+v", issues[0])
	}
	if !issues[1].Halted {
		t.Errorf("issue 1 = %+v, want halted", issues[1])
	}
}
