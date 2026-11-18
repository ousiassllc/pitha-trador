//go:build !windows

package codexwire_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

const version = "trader-v3"

func newClient(m *mockCLI, timeout time.Duration) *codexwire.Client {
	cfg := codexwire.Config{Bin: m.Bin, Home: m.Home, Model: "gpt-6.1-sol", CallTimeout: timeout}
	return codexwire.New(cfg, codexwire.NewLogin(cfg))
}

func request() jev.TraderRequest {
	return jev.TraderRequest{QuestionVersion: version, State: jev.ScoutState{Symbol: "7203", Price: 2100}}
}

func TestTrader_SuccessMapsTheAnswersTokensAndKeepsTheRunReadOnly(t *testing.T) {
	m := newMockCLI(t)
	answers := validAnswers(t, version)
	answers["toxic_flow"] = nil // a declined question
	m.okRun(answers)

	resp, latency, bytes, err := newClient(m, 0).Trader(context.Background(), request())
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if resp.Direction == "" || resp.Regime == "" || resp.EntryQuality == "" || resp.Confidence == nil || *resp.Confidence != 0.7 {
		t.Errorf("answers not mapped: %+v", resp)
	}
	if resp.ToxicFlow != nil || len(resp.Refused) != 1 || resp.Refused[0] != "toxic_flow" {
		t.Errorf("a null answer must be a refusal, got toxic_flow=%v refused=%v", resp.ToxicFlow, resp.Refused)
	}
	if resp.InputTokens != 1200 || resp.OutputTokens != 80 || resp.ReportedCostUSD != nil {
		t.Errorf("usage = in %d out %d cost %v, want 1200, 80 and no reported cost", resp.InputTokens, resp.OutputTokens, resp.ReportedCostUSD)
	}
	if resp.ModelID != "gpt-6.1-sol" || resp.Provider != codexwire.ServedProvider || resp.ResponseID != "th-1" || resp.Body == "" {
		t.Errorf("served identity = model %q provider %q id %q body %q", resp.ModelID, resp.Provider, resp.ResponseID, resp.Body)
	}
	if latency <= 0 || bytes != len(m.read("seen_stdin")) {
		t.Errorf("latency %v, request bytes %d (stdin %d)", latency, bytes, len(m.read("seen_stdin")))
	}
	args := strings.Fields(m.read("seen_args"))
	for _, want := range []string{"exec", "--json", "--output-schema", "--ephemeral", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "read-only", "-m", "gpt-6.1-sol", "-"} {
		if !contains(args, want) {
			t.Errorf("arguments %v lack %q", args, want)
		}
	}
	if !strings.Contains(m.read("seen_stdin"), `"symbol":"7203"`) || !strings.Contains(m.read("seen_stdin"), "[direction]") {
		t.Error("the prompt (stdin) must carry the state and the questions")
	}
	if cwd := strings.TrimSpace(m.read("seen_cwd")); cwd == "" || strings.HasPrefix(cwd, m.Home) {
		t.Errorf("the run's working directory = %q, want an empty temp directory outside CODEX_HOME", cwd)
	}
}

func TestTrader_ChildEnvironmentIsAnAllowListWithTheDedicatedHome(t *testing.T) {
	t.Setenv("CODEX_API_KEY", "key-from-parent")
	t.Setenv("OPENAI_API_KEY", "key-from-parent")
	t.Setenv("CODEX_ACCESS_TOKEN", "token-from-parent")
	t.Setenv("OPENROUTER_API_KEY", "key-from-parent")
	t.Setenv("CODEX_HOME", "/the/persons/own/codex/home")
	m := newMockCLI(t)
	m.okRun(validAnswers(t, version))

	if _, _, _, err := newClient(m, 0).Trader(context.Background(), request()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	env := m.read("seen_env")
	for _, secret := range []string{"key-from-parent", "token-from-parent", "CODEX_API_KEY", "OPENAI_API_KEY", "CODEX_ACCESS_TOKEN", "OPENROUTER_API_KEY", "/the/persons/own"} {
		if strings.Contains(env, secret) {
			t.Errorf("the child's environment holds %q", secret)
		}
	}
	if !strings.Contains(env, "CODEX_HOME="+m.Home+"\n") {
		t.Errorf("the child's CODEX_HOME is not the dedicated one:\n%s", env)
	}
}

func TestTrader_FailureIsJudgedByTurnFailedAndExitCodeOnly(t *testing.T) {
	good := validAnswers(t, version)
	for name, setup := range map[string]func(*mockCLI){
		"turn.failed with exit 1": func(m *mockCLI) { m.failedRun("model is at capacity", 1) },
		"turn.failed with exit 0": func(m *mockCLI) { m.failedRun("model is at capacity", 0) },
		"non-zero exit after a clean turn": func(m *mockCLI) {
			m.okRun(good)
			m.exit(3)
		},
		"no turn.completed": func(m *mockCLI) {
			m.stdout(`{"type":"thread.started","thread_id":"th-3"}`)
			m.exit(0)
		},
		"no output and exit 1": func(m *mockCLI) { m.stdout(); m.exit(1) },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMockCLI(t)
			setup(m)
			_, _, _, err := newClient(m, 0).Trader(context.Background(), request())
			var ce *codexwire.CallError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want a *CallError", err)
			}
			if ce.UsageLimit {
				t.Error("a failure that matches no usage-limit text must not be labelled one")
			}
			if strings.Contains(err.Error(), "capacity") {
				t.Errorf("the CLI's text reached the error: %q", err)
			}
		})
	}
}

func TestTrader_AnErrorEventAloneIsNotAFailure(t *testing.T) {
	m := newMockCLI(t)
	msg := `{"type":"error","message":"rate limit exceeded: retrying"}`
	m.okRun(validAnswers(t, version))
	m.put("stdout", msg+"\n"+m.read("stdout"))
	if _, _, _, err := newClient(m, 0).Trader(context.Background(), request()); err != nil {
		t.Fatalf("a retried error event followed by a completed turn is a success: %v", err)
	}
}

func TestTrader_UsageLimitTextLabelsTheFailureAndGivesTheResetTime(t *testing.T) {
	m := newMockCLI(t)
	reset := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	m.failedRun("You've hit your usage limit. Upgrade to Pro, or try again at "+reset.Format("Jan 2, 2006 3:04 PM")+".", 1)
	_, _, _, err := newClient(m, 0).Trader(context.Background(), request())
	var ce *codexwire.CallError
	if !errors.As(err, &ce) || !ce.UsageLimit {
		t.Fatalf("err = %v, want a usage-limit *CallError", err)
	}
	if !ce.Until.Equal(reset) {
		t.Errorf("Until = %v, want %v", ce.Until, reset)
	}
	if strings.Contains(err.Error(), "Upgrade") {
		t.Errorf("the CLI's text reached the error: %q", err)
	}

	m.failedRun("You've hit your usage limit.", 1) // no time given
	_, _, _, err = newClient(m, 0).Trader(context.Background(), request())
	if !errors.As(err, &ce) || !ce.UsageLimit || !ce.Until.IsZero() {
		t.Errorf("a limit without a time: err = %v until = %v, want a usage-limit error with no time", err, ce.Until)
	}
}

func TestParseResetTime(t *testing.T) {
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		text string
		want time.Time
		ok   bool
	}{
		"rfc3339":        {"try again at 2026-11-15T13:00:00Z", time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC), true},
		"ordinal":        {"Try again at Nov 16th, 2026 3:04 AM.", time.Date(2026, 11, 16, 3, 4, 0, 0, time.UTC), true},
		"without a year": {"try again at Nov 17, 9:30 AM", time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC), true},
		"in the past":    {"try again at 2026-11-15T09:00:00Z", time.Time{}, false},
		"too far ahead":  {"try again at 2026-12-31T00:00:00Z", time.Time{}, false},
		"not a time":     {"try again at some point", time.Time{}, false},
		"no time":        {"You've hit your usage limit.", time.Time{}, false},
	} {
		got, ok := codexwire.ParseResetTime(tc.text, now, time.UTC)
		if ok != tc.ok || (ok && !got.Equal(tc.want)) {
			t.Errorf("%s: ParseResetTime = %v, %v; want %v, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestTrader_AnInvalidAnswerIsAnInvalidResponseNotACallFailure(t *testing.T) {
	bad := func(mut func(map[string]any)) map[string]any {
		a := validAnswers(t, version)
		mut(a)
		return a
	}
	for name, answers := range map[string]map[string]any{
		"a noul value out of range": bad(func(a map[string]any) { a["toxic_flow"] = 1.5 }),
		"a choice that is no option": bad(func(a map[string]any) {
			a["direction"] = map[string]any{"choice": "SIDEWAYS", "confidence": 0.5}
		}),
		"a missing question":  bad(func(a map[string]any) { delete(a, "regime") }),
		"an unasked question": bad(func(a map[string]any) { a["extra"] = 1 }),
	} {
		t.Run(name, func(t *testing.T) {
			m := newMockCLI(t)
			m.okRun(answers)
			_, _, _, err := newClient(m, 0).Trader(context.Background(), request())
			var ce *codexwire.CallError
			if !errors.Is(err, systemone.ErrInvalidResponse) || errors.As(err, &ce) {
				t.Fatalf("err = %v, want systemone.ErrInvalidResponse and no *CallError", err)
			}
		})
	}
	m := newMockCLI(t)
	m.stdout(`{"type":"item.completed","item":{"type":"agent_message","text":"I cannot do that"}}`, `{"type":"turn.completed","usage":{}}`)
	if _, _, _, err := newClient(m, 0).Trader(context.Background(), request()); !errors.Is(err, systemone.ErrInvalidResponse) {
		t.Errorf("a final message that is not JSON: err = %v, want systemone.ErrInvalidResponse", err)
	}
}

func TestTrader_TimeoutKillsTheWholeProcessGroup(t *testing.T) {
	m := newMockCLI(t)
	m.put("hang", "1")
	start := time.Now()
	_, _, _, err := newClient(m, 300*time.Millisecond).Trader(context.Background(), request())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("the call returned after %v: the hung child was not stopped", time.Since(start))
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(m.read("child_pid")))
	if convErr != nil {
		t.Fatalf("child pid: %v", convErr)
	}
	waitFor(t, "the grandchild to be gone", func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
}

func TestTrader_CancelIsNotATimeoutAndStopsTheChild(t *testing.T) {
	m := newMockCLI(t)
	m.put("hang", "1")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, _, err := newClient(m, time.Minute).Trader(ctx, request())
		done <- err
	}()
	waitFor(t, "the child to start", func() bool { return m.exists("child_pid") })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the call did not return after cancel")
	}
}

func TestTrader_ACLIThatCannotStartIsACallFailure(t *testing.T) {
	cfg := codexwire.Config{Bin: "/nonexistent/codex", Home: t.TempDir(), Model: "m"}
	_, _, _, err := codexwire.New(cfg, nil).Trader(context.Background(), request())
	var ce *codexwire.CallError
	if !errors.As(err, &ce) || ce.Reason != codexwire.ReasonStart {
		t.Errorf("err = %v, want a could-not-start *CallError", err)
	}
}

func TestConfigured_NeedsTheDedicatedHome(t *testing.T) {
	if codexwire.New(codexwire.Config{Model: "m"}, nil).Configured() {
		t.Error("a client without CODEX_HOME is Configured: the person's own Codex home would be used")
	}
	if !codexwire.New(codexwire.Config{Home: t.TempDir(), Model: "m"}, nil).Configured() {
		t.Error("a client with a dedicated CODEX_HOME is not Configured")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
