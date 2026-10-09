package market

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const (
	clmIssueMarket = "CLMStkGetIssueSizyouMstKabu"
	clmIssueMaster = "CLMStkGetIssueMstKabu"
	// marketTSE is 上場市場 00 (東証), the only market the adapter reads.
	marketTSE = "00"
	// lendableCode is sSinyouC 1 (貸借銘柄).
	lendableCode = "1"

	retryMin = time.Minute
	retryMax = 10 * time.Minute
)

// ErrMasterNotLoaded is returned by Master.Info while today's master has not
// been loaded (the morning fetch is pending or failed). Callers treat the
// flags as unknown and ask again later (symbolcache does not cache errors).
var ErrMasterNotLoaded = errors.New("tachibana: today's master is not loaded yet")

// Master is the in-memory 株式銘柄市場マスタ (値幅 and 貸借区分 per symbol). It
// is fetched once per JST day and never refetched on the same day: the master
// does not change while the system is open (専用ページ 3-4), and a
// refetch is a large request the broker asks not to see in the daytime. The
// daily trigger is the morning login (Session's OnLogin hook); a process that
// starts later in the day loads it once at that login.
type Master struct {
	client *tachibana.Client
	clock  tachibana.Clock

	loadMu   sync.Mutex // serializes Load
	retrying atomic.Bool

	mu      sync.Mutex // guards day, info and targets
	day     string
	info    map[string]broker.SymbolInfo
	targets []domain.DailyBarTarget // the last loaded master's symbols, kept across days
}

// NewMaster returns an empty Master reading through client.
func NewMaster(client *tachibana.Client, clock tachibana.Clock) *Master {
	return &Master{client: client, clock: tachibana.OrReal(clock)}
}

// Load fetches the market master unless today's is already loaded (then it
// does nothing and sends no request). The request goes to the MASTER virtual
// URL at the master priority, behind sessions and quotes.
func (m *Master) Load(ctx context.Context) error {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()
	today := tachibana.DayKey(m.clock.Now())
	if m.loadedFor(today) {
		return nil
	}
	var resp struct {
		Rows []row `json:"aCLMStkIssueSizyouMstKabu"`
	}
	if err := m.client.Call(ctx, tachibana.TargetMaster, tachibana.PriorityMaster, clmIssueMarket, nil, &resp); err != nil {
		return err
	}
	info := make(map[string]broker.SymbolInfo, len(resp.Rows))
	targets := make([]domain.DailyBarTarget, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		code := r.text("sIssueCode")
		if code == "" || r.text("sZyouzyouSizyou") != marketTSE {
			continue
		}
		info[code] = symbolInfo(r)
		t := domain.DailyBarTarget{Symbol: code, Market: segmentOf(r.text("sZyouzyouKubun"))}
		if prev := r.num("sZenzituOwarine"); prev != nil {
			t.PrevClose = *prev
		}
		targets = append(targets, t)
	}
	m.mu.Lock()
	m.day, m.info, m.targets = today, info, targets
	m.mu.Unlock()
	slog.Info("tachibana: market master loaded", "symbols", len(info))
	return nil
}

// LoadWithRetry runs Load until it succeeds or ctx ends, backing off from one
// to ten minutes between failures (a login that was lost, or the broker
// busy). Concurrent calls collapse into one loop. It is the Session OnLogin
// hook.
func (m *Master) LoadWithRetry(ctx context.Context) {
	if !m.retrying.CompareAndSwap(false, true) {
		return
	}
	defer m.retrying.Store(false)
	wait := retryMin
	for {
		err := m.Load(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		slog.Warn("tachibana: market master fetch failed; retrying", "retry_in", wait.String(), "error", err)
		if tachibana.Sleep(ctx, m.clock, wait) != nil {
			return
		}
		wait = min(wait*2, retryMax)
	}
}

func (m *Master) loadedFor(day string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.day == day
}

// Info returns symbol's 値幅 and 貸借区分 from today's master:
// ErrMasterNotLoaded before the morning fetch, tachibana.ErrNoData for a
// symbol the master does not list.
func (m *Master) Info(symbol string) (broker.SymbolInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.day != tachibana.DayKey(m.clock.Now()) {
		return broker.SymbolInfo{}, ErrMasterNotLoaded
	}
	info, ok := m.info[symbol]
	if !ok {
		return broker.SymbolInfo{}, fmt.Errorf("%w: symbol %s is not in the master", tachibana.ErrNoData, symbol)
	}
	return info, nil
}

// DailyBarTargets returns the symbols of the last loaded master, each with its
// 市場区分 and 前日終値: the universe the nightly daily-bar batch narrows down
// with the Settings. Unlike Info it is not tied to today's load, because the
// listing barely changes between days and the master is fetched once in the
// morning, not again for the night batch. ErrMasterNotLoaded before the first
// load.
func (m *Master) DailyBarTargets() ([]domain.DailyBarTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.targets == nil {
		return nil, ErrMasterNotLoaded
	}
	return append([]domain.DailyBarTarget(nil), m.targets...), nil
}

// symbolInfo maps one master row: sSinyouC 1 is 貸借銘柄; sNehabaMax/Min are
// the day's price limits (a non-positive value means none).
func symbolInfo(r row) broker.SymbolInfo {
	lendable := r.text("sSinyouC") == lendableCode
	return broker.SymbolInfo{
		Lendable:   &lendable,
		UpperLimit: positive(r.num("sNehabaMax")),
		LowerLimit: positive(r.num("sNehabaMin")),
	}
}

func positive(f *float64) *float64 {
	if f == nil || *f <= 0 {
		return nil
	}
	return f
}

// Issue is one row of the 全銘柄マスタ (CLMStkGetIssueMstKabu).
type Issue struct {
	Code      string
	Name      string
	ShortName string
	// TradingUnit is the 売買単位 (0 when unknown).
	TradingUnit float64
	// Halted is true while 売買停止Ｃ is 9 (停止中).
	Halted bool
	// Market is 優先市場 (00 = 東証).
	Market string
	// SectorCode is the 業種コード.
	SectorCode string
}

// FetchIssues fetches the 全銘柄マスタ at the master priority. Nothing in the
// adapter calls it yet: it is there for the universe auto-import to come.
func FetchIssues(ctx context.Context, client *tachibana.Client) ([]Issue, error) {
	var resp struct {
		Rows []row `json:"aCLMStkIssueMstKabu"`
	}
	if err := client.Call(ctx, tachibana.TargetMaster, tachibana.PriorityMaster, clmIssueMaster, nil, &resp); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		code := r.text("sIssueCode")
		if code == "" {
			continue
		}
		issue := Issue{
			Code: code, Name: r.text("sIssueName"), ShortName: r.text("sIssueNameRyaku"),
			Halted: r.text("sBaibaiTeisiC") == "9", Market: r.text("sYusenSizyou"), SectorCode: r.text("sGyousyuCode"),
		}
		if unit := r.num("sBaibaiTani"); unit != nil {
			issue.TradingUnit = *unit
		}
		issues = append(issues, issue)
	}
	return issues, nil
}
