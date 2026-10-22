package hometests

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

func openPosition(symbol string, qty int64, entry, current, unrealized float64, opened time.Time) domain.Position {
	return domain.Position{ID: int64(len(symbol)) + qty, Symbol: symbol, Side: domain.PositionSideLong, Quantity: qty,
		EntryPrice: entry, CurrentPrice: current, UnrealizedPnL: unrealized, OpenedAt: opened}
}

func closedPosition(symbol string, pnl float64) domain.Position {
	closed, p := jst(20, 9, 30), pnl
	return domain.Position{ID: 900, Symbol: symbol, Side: domain.PositionSideLong, Quantity: 100, OpenedAt: jst(20, 9, 0), ClosedAt: &closed, RealizedPnL: &p}
}

// 今日の損益 = 今日決済した分の実現損益（paper_daily_pnl の今日の値）＋保有中の含み損益
// の合計. The realized figure is the source's, the unrealized one is summed over the open
// positions only (a closed row in the list adds nothing).
func TestPnL_RealizedPlusUnrealizedOfOpenPositionsOnly(t *testing.T) {
	f := newFixture()
	f.source.realized = home.Realized{PnLJPY: 2500, ClosedCount: 3}
	f.source.health.OpenedToday = 4
	f.positions = []domain.Position{
		openPosition("7203", 100, 2500, 2530, 3000, jst(20, 9, 10)),
		openPosition("6758", 200, 1800, 1790, -2000, jst(20, 9, 40)),
		closedPosition("9984", 99999), // closed: its unrealized/realized are not part of the open sum
	}
	body := f.mustGet(t, "/home/trade-status") // warm-up: the card route is independent
	_ = body
	body = f.mustGet(t, "/home/pnl")
	for id, want := range map[string]string{
		"pnl-realized":   "+2,500円 （決済 3件）",
		"pnl-unrealized": "+1,000円 （保有中）",
		"pnl-total":      "+3,500円",
		"pnl-opened":     "4件",
	} {
		if got := inner(t, body, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestPnL_LossIsRedAndNegative(t *testing.T) {
	f := newFixture()
	f.source.realized = home.Realized{PnLJPY: -4000, ClosedCount: 2}
	f.positions = []domain.Position{openPosition("7203", 100, 2500, 2470, -3000, jst(20, 9, 10))}
	body := f.mustGet(t, "/home/pnl")
	if got := inner(t, body, "pnl-total"); got != "-7,000円" {
		t.Errorf("total = %q, want -7,000円", got)
	}
	if !strings.Contains(body, "text-red-700") {
		t.Errorf("a loss must be red: %s", body)
	}
}

// The daily loss limit room is display arithmetic on the same sum the daily
// guard measures: limit + (realized + unrealized).
func TestPnL_RemainingUntilDailyLossLimit(t *testing.T) {
	f := newFixture()
	f.settings.guard = dailyguardsettings.Resolved{Limits: dailyguardsettings.Limits{LossLimitJPY: 50000}}
	f.source.realized = home.Realized{PnLJPY: -10000, ClosedCount: 1}
	f.positions = []domain.Position{openPosition("7203", 100, 2500, 2480, -2000, jst(20, 9, 10))}
	got := inner(t, f.mustGet(t, "/home/pnl"), "pnl-loss-limit")
	if want := "1日の損失上限（-50,000円）まで あと 38,000円"; got != want {
		t.Errorf("loss limit line = %q, want %q", got, want)
	}
}

func TestPnL_NoLossLimitLineWhenTheRuleIsOff(t *testing.T) {
	f := newFixture()
	if body := f.mustGet(t, "/home/pnl"); count(body, "pnl-loss-limit") != 0 {
		t.Errorf("no limit configured, yet a line was shown: %s", body)
	}
}

// With nothing closed or open today the card shows zeros, not an error.
func TestPnL_QuietDayIsZero(t *testing.T) {
	f := newFixture()
	f.source.health.OpenedToday = 0
	body := f.mustGet(t, "/home/pnl")
	if got := inner(t, body, "pnl-total"); got != "0円" {
		t.Errorf("total = %q, want 0円", got)
	}
}

func TestPositions_EmptyState(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/home/positions")
	if got := inner(t, body, "positions-empty"); got != "保有中のポジションはありません" {
		t.Errorf("empty state = %q", got)
	}
	if count(body, "positions-table") != 0 {
		t.Errorf("no table without positions: %s", body)
	}
}

func TestPositions_OnlyOpenOnesAreListedWithLinksAndHeldTime(t *testing.T) {
	f := newFixture()
	f.now = jst(20, 11, 15)
	f.positions = []domain.Position{
		openPosition("7203", 100, 2500, 2530.5, 3050, jst(20, 9, 10)),
		closedPosition("9984", 777),
		openPosition("6758", 200, 1800, 1790, -2000, jst(19, 9, 40)),
	}
	body := f.mustGet(t, "/home/positions")
	if got := inner(t, body, "positions-count"); got != "2 件" {
		t.Errorf("count = %q, want 2 件", got)
	}
	if n := count(body, "position-row"); n != 2 {
		t.Fatalf("rows = %d, want the 2 open positions", n)
	}
	for _, want := range []string{
		`href="/symbols/7203"`, `href="/symbols/6758"`,
		">買い<", ">2,530.5<", ">2,500<", ">+3,050円<", ">-2,000円<", ">2時間5分<", ">1日1時間<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("positions table lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "9984") {
		t.Errorf("a closed position is listed: %s", body)
	}
}

func TestPage_FirstRowHasTheThreeCardsInOrder(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/")
	row := body[strings.Index(body, `data-testid="home-today-row"`):]
	a, b, c := strings.Index(row, "home-trade-status-card"), strings.Index(row, "home-pnl-card"), strings.Index(row, "home-positions-card")
	if a < 0 || b < a || c < b {
		t.Fatalf("top row order: 取引の状態=%d 今日の Paper 損益=%d 保有ポジション=%d", a, b, c)
	}
	// The row sits above 要対応 and 今日の詳細.
	if att := strings.Index(body, "home-attention-section"); att < c {
		t.Errorf("要対応 (%d) must come after the top row (%d)", att, c)
	}
	for _, want := range []string{
		`aria-current="page"`,      // ホーム is the current menu entry
		`<pitha-kill-switch-panel`, // the existing component, once
		`hx-get="/home/trade-status"`, `hx-get="/home/pnl"`, `hx-get="/home/positions"`,
		`hx-get="/home/attention"`, `hx-get="/home/details"`,
		`every 30s`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(body, "<pitha-kill-switch-panel"); n != 1 {
		t.Errorf("kill switch panels on Home = %d, want exactly 1", n)
	}
	if strings.Contains(body, `id="attention-badge"`) {
		t.Errorf("Home lists the alerts in full and must not also show the badge")
	}
}

// One card whose read fails shows its own error line; the page and every other
// card still render.
func TestPage_FailedReadFailsOnlyItsCard(t *testing.T) {
	f := newFixture()
	f.source.realizedErr = errBoom
	f.posErr = errBoom
	f.source.rejectsErr = errBoom
	f.source.proposalErr = errBoom
	f.source.tradesErr = errBoom
	f.eventsErr = errBoom
	f.scanErr = errBoom
	code, body := f.get(t, "/")
	if code != 200 {
		t.Fatalf("GET / = %d, want 200 even when reads fail", code)
	}
	for _, id := range []string{"home-pnl-card", "home-positions-card", "home-reject-reasons", "home-scan", "home-proposals", "home-trades", "home-kill-events"} {
		section := between(body, `data-testid="`+id+`"`, "</section>")
		if !strings.Contains(section, `data-testid="home-card-error"`) {
			t.Errorf("%s shows no error line: %s", id, section)
		}
	}
	for id, want := range map[string]string{
		"home-trade-status-card": sentenceAllowed, // not affected
		"home-model":             "メタモデル: 採用中 v1",
	} {
		if section := between(body, `data-testid="`+id+`"`, "</section>"); !strings.Contains(clean(section), want) {
			t.Errorf("%s = %q, want it to still show %q", id, clean(section), want)
		}
	}
	// Details fragment: the same isolation after a refresh.
	if frag := f.mustGet(t, "/home/details"); count(frag, "home-card-error") != 5 {
		t.Errorf("details fragment errors = %d, want 5 (reject, scan, proposals, trades, kill)", count(frag, "home-card-error"))
	}
}

func TestPage_ModelBlockErrorWhenHealthCannotBeRead(t *testing.T) {
	f := newFixture()
	f.source.healthErr = errBoom
	body := f.mustGet(t, "/home/details")
	if got := inner(t, body, "home-card-error"); !strings.Contains(got, "モデルの状態") {
		t.Errorf("error = %q", got)
	}
}

// between is the text after the first start marker up to the next end marker.
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}
