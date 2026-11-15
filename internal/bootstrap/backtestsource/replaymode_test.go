package backtestsource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// Issue #874: a replay judges an entry by Live's min_entry_quality (strong) or
// by Paper's own (good) as the replay mode says. recordLiveBars records every
// decision as strong; replayEnv re-records them as quality.
func replayEnv(t *testing.T, quality string) (testEnv, backtest.Period, time.Time) {
	t.Helper()
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	storeTakingModel(t, env, base.Add(-time.Hour))
	recordLiveBars(t, env, inst, base, 30)
	response, err := json.Marshal(map[string]any{
		"entry_quality": quality, "continuation_probability": 0.8, "toxic_flow": 0.1, "liquidity_stressed": 0.1,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := env.Conn.ExecContext(context.Background(), `UPDATE jev_decisions SET response_json = ?`, string(response)); err != nil {
		t.Fatalf("re-record the entry quality: %v", err)
	}
	return env, backtest.Period{Start: base, End: base.Add(30 * time.Minute)}, base
}

// strategyPolicy is the shipped strategy.yaml policy.* baseline (Live: strong).
func strategyPolicy(t *testing.T) config.PolicyConfig {
	t.Helper()
	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	return strategy.Policy
}

// inMode is env's Source replaying in mode (every other input shared).
func inMode(env testEnv, mode ReplayMode) *Source {
	s := *env.Source
	s.replay.Mode = mode
	return &s
}

func replayTrades(t *testing.T, s *Source, period backtest.Period) []backtest.Trade {
	t.Helper()
	configs, err := s.RunConfigs(context.Background(), period)
	if err != nil {
		t.Fatalf("RunConfigs: %v", err)
	}
	trades, err := backtest.ShadowBacktestPortfolioTrades(context.Background(), configs, period)
	if err != nil {
		t.Fatalf("ShadowBacktestPortfolioTrades: %v", err)
	}
	return trades
}

func TestReplayMode_PaperPassesWhatPaperPassesAndLiveDoesNot(t *testing.T) {
	env, period, _ := replayEnv(t, domain.JevEntryQualityGood)
	wf := backtest.WalkForwardConfig{
		Start: period.Start, End: period.End,
		TrainingPeriod: 10 * time.Minute, ValidationPeriod: 10 * time.Minute, ForwardPeriod: 10 * time.Minute,
	}

	if got := replayTrades(t, inMode(env, ReplayLive), period); len(got) != 0 {
		t.Fatalf("Live replay: %d trades on good decisions, want 0 (Live needs strong)", len(got))
	}
	if got := replayTrades(t, env.Source, period); len(got) != 0 {
		t.Fatalf("default replay: %d trades on good decisions, want 0 (the default is the Live replay)", len(got))
	}
	paper := inMode(env, ReplayPaper)
	if got := replayTrades(t, paper, period); len(got) == 0 {
		t.Fatal("Paper replay: 0 trades on good decisions, want entries (Paper's min_entry_quality is good)")
	}

	// The Walk Forward backtest (Performance page) replays in the same mode.
	live, err := inMode(env, ReplayLive).RunWalkForward(context.Background(), wf)
	if err != nil {
		t.Fatalf("Live RunWalkForward: %v", err)
	}
	papered, err := paper.RunWalkForward(context.Background(), wf)
	if err != nil {
		t.Fatalf("Paper RunWalkForward: %v", err)
	}
	if live.Combined.TradeCount != 0 || papered.Combined.TradeCount == 0 {
		t.Errorf("Walk Forward trades: Live %d, Paper %d; want 0 and >0", live.Combined.TradeCount, papered.Combined.TradeCount)
	}
}

func TestReplayMode_PaperFollowsThePaperSetting(t *testing.T) {
	env, period, base := replayEnv(t, domain.JevEntryQualityGood)
	settings := system.NewRuntimeSettingsRepository(env.Conn)
	for _, key := range paperpolicysettings.Keys() {
		if err := settings.Set(context.Background(), key, `"strong"`, base); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	if got := replayTrades(t, inMode(env, ReplayPaper), period); len(got) != 0 {
		t.Fatalf("Paper replay with Paper min_entry_quality strong: %d trades on good decisions, want 0", len(got))
	}
}

// TestReplayMode_LiveReplayResultsDoNotChange pins the Live replay: the default
// (no mode chosen) and the explicit Live mode replay exactly the same trades,
// and no Paper min_entry_quality changes them.
func TestReplayMode_LiveReplayResultsDoNotChange(t *testing.T) {
	env, period, base := replayEnv(t, domain.JevEntryQualityStrong)
	want := replayTrades(t, env.Source, period)
	if len(want) == 0 {
		t.Fatal("default replay: 0 trades on strong decisions; the comparison would be vacuous")
	}
	settings := system.NewRuntimeSettingsRepository(env.Conn)
	for _, key := range paperpolicysettings.Keys() {
		if err := settings.Set(context.Background(), key, `"poor"`, base); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	if got := replayTrades(t, inMode(env, ReplayLive), period); !reflect.DeepEqual(got, want) {
		t.Errorf("explicit Live replay = %+v, want the default replay's %+v", got, want)
	}
	if got := replayTrades(t, inMode(env, ReplayPaper), period); len(got) < len(want) {
		t.Errorf("Paper replay on strong decisions = %d trades, want at least the Live replay's %d (poor is looser)", len(got), len(want))
	}
}

func TestReplayMode_PaperNeedsRuntimeSettings(t *testing.T) {
	env, period, _ := replayEnv(t, domain.JevEntryQualityGood)
	s := inMode(env, ReplayPaper)
	s.replay.MetaSettings = nil
	if _, err := s.RunConfigs(context.Background(), period); err == nil {
		t.Fatal("RunConfigs without the runtime settings: nil error, want one")
	}
}

func TestParseReplayMode(t *testing.T) {
	for in, want := range map[string]ReplayMode{"": ReplayLive, "live": ReplayLive, "paper": ReplayPaper} {
		if got, err := ParseReplayMode(in); err != nil || got != want {
			t.Errorf("ParseReplayMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseReplayMode("demo"); err == nil {
		t.Error("ParseReplayMode(demo): nil error, want one")
	}
	if _, err := (&Source{replay: Replay{Mode: "demo"}}).ReplayPolicy(context.Background(), strategyPolicy(t)); err == nil {
		t.Error("ReplayPolicy with an unknown mode: nil error, want one")
	}
}

// TestReplayMode_ProposalEvaluationUsesTheSameMode runs Governor.EvaluateProposal
// over the real Source on good decisions. Under the Paper replay a proposal on
// Live's min_entry_quality (even raised to exceptional) does not touch Paper's,
// so baseline and candidate both enter; under the Live replay the candidate
// (good) enters where the baseline (strong) does not.
func TestReplayMode_ProposalEvaluationUsesTheSameMode(t *testing.T) {
	change := func(newValue string) []domain.PolicyChange {
		return []domain.PolicyChange{{Key: domain.PolicyKeyLongMinEntryQuality, OldValue: `"strong"`, NewValue: newValue}}
	}
	evaluate := func(t *testing.T, mode ReplayMode, changes []domain.PolicyChange) assist.BacktestComparison {
		t.Helper()
		env, _, base := replayEnv(t, domain.JevEntryQualityGood)
		ctx := context.Background()
		opus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"verdict":"reject","reason":"only the stored comparison matters here"}`))
		}))
		t.Cleanup(opus.Close)
		proposals := judgement.NewProposalRepository(env.Conn)
		g := selfimprove.NewGovernor(proposals, system.NewRuntimeSettingsRepository(env.Conn), env.Positions, inMode(env, mode), strategyPolicy(t),
			selfimprove.WithOpus(assist.NewOpus(assist.NewClient(assist.Config{Label: "opus", BaseURL: opus.URL, MaxAttempts: 1}))),
			selfimprove.WithNow(func() time.Time { return base.Add(30 * time.Minute) }))
		changesJSON, err := domain.EncodePolicyChanges(changes)
		if err != nil {
			t.Fatalf("EncodePolicyChanges: %v", err)
		}
		proposal, err := proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
		if err != nil {
			t.Fatalf("Insert proposal: %v", err)
		}
		if _, err := g.EvaluateProposal(ctx, proposal.ID); err != nil {
			t.Fatalf("EvaluateProposal: %v", err)
		}
		stored, err := proposals.Get(ctx, proposal.ID)
		if err != nil || stored.BacktestResultJSON == nil {
			t.Fatalf("Get proposal: %+v, err %v; want the stored backtest comparison", stored, err)
		}
		var cmp assist.BacktestComparison
		if err := json.Unmarshal([]byte(*stored.BacktestResultJSON), &cmp); err != nil {
			t.Fatalf("decode backtest result: %v", err)
		}
		return cmp
	}

	paper := evaluate(t, ReplayPaper, change(`"exceptional"`))
	if paper.BaselineTradeCount == 0 || paper.CandidateTradeCount != paper.BaselineTradeCount {
		t.Errorf("Paper replay: baseline %d trades, candidate %d; want equal and > 0 (the Live key does not touch Paper)", paper.BaselineTradeCount, paper.CandidateTradeCount)
	}
	live := evaluate(t, ReplayLive, change(`"good"`))
	if live.BaselineTradeCount != 0 || live.CandidateTradeCount == 0 {
		t.Errorf("Live replay: baseline %d trades, candidate %d; want 0 and > 0 (good admits the good decisions)", live.BaselineTradeCount, live.CandidateTradeCount)
	}
}
