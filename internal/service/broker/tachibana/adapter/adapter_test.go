package adapter_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestCapabilitiesAndNoRanking(t *testing.T) {
	fb := tt.New(t, tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0)))
	s := fb.Settings()
	s.RequestMaxPerSecond = 4
	a := adapter.New(adapter.Config{Settings: s, Credentials: tt.Credentials(), HTTPClient: fb.Server().Client()})

	caps := a.Capabilities()
	if caps.Name != config.BrokerTachibana || caps.MaxStreamSymbols != 120 || caps.Ranking || caps.RequestsPerSecond != 4 {
		t.Errorf("capabilities = %+v", caps)
	}
	if _, err := a.Candidates(context.Background()); err == nil {
		t.Error("Candidates must report that there is no ranking")
	}
}

// The adapter picks the selected environment's 認証ID and 秘密鍵 and never
// carries a 第二暗証番号 in production.
func TestEnvironmentSelectsCredentialsAndURL(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	s := fb.Settings()
	s.Environment = config.TachibanaEnvProduction
	s.ProdBaseURL = fb.BaseURL()
	s.ProdPrivateKeyPath = s.DemoPrivateKeyPath
	secrets := config.Secrets{TachibanaDemoAuthID: "DEMO-ID", TachibanaProdAuthID: tt.AuthID}
	a := adapter.New(adapter.Config{
		Settings: s, Credentials: secrets.TachibanaCredentials(s.Environment), HTTPClient: fb.Server().Client(), Clock: clk,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fb.Requests()[0].Body["sAuthId"]; got != tt.AuthID {
		t.Errorf("logged in with %q, want the production 認証ID", got)
	}
	cancel()
	a.Run(ctx)
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// Across a whole life (login, requests, broker errors, an unreachable server,
// logout) the 認証ID, the virtual URLs and the private key never reach a log
// line, an error, the session status or a notice.
func TestNoSecretsInLogsErrorsStatusOrNotices(t *testing.T) {
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	var (
		mu      sync.Mutex
		notices []string
	)
	a := adapter.New(adapter.Config{
		Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: fb.Server().Client(), Clock: clk,
		Notifier: session.NotifierFunc(func(_ context.Context, n session.Notice) {
			mu.Lock()
			notices = append(notices, n.Message)
			mu.Unlock()
		}),
	})
	fb.SetLoginExtra(map[string]any{"sUpdateInformAPISpecFunction": "20261101", "sUpdateInformWebDocument": "20261101"})
	var texts []string
	collect := func(err error) {
		if err != nil {
			texts = append(texts, err.Error())
		}
		texts = append(texts, a.Status().Guidance)
	}

	ctx, cancel := context.WithCancel(context.Background())
	collect(a.Start(ctx))
	call := func() error {
		return a.Client().Call(ctx, tachibana.TargetPrice, tachibana.PriorityWatchQuote, "CLMTest", nil, nil)
	}
	collect(call())
	for _, resp := range []tt.Responder{
		func(tt.Request, int) (int, map[string]any) { return 500, map[string]any{} },
		func(tt.Request, int) (int, map[string]any) { return 200, tt.ControlError("-2", "busy") },
		func(tt.Request, int) (int, map[string]any) { return 200, tt.ControlError("8", "clock") },
	} {
		fb.Respond(resp)
		collect(call())
	}
	fb.Respond(nil)
	secrets := fb.Secrets()
	fb.Server().CloseClientConnections()
	fb.Server().Close() // unreachable: the transport error would carry the virtual URL
	collect(call())
	cancel()
	a.Run(ctx)

	check := func(what, text string) {
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("%s contains the secret %q:\n%s", what, s, text)
			}
		}
	}
	check("logs", logs.String())
	for _, s := range texts {
		check("error/status text", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notices) == 0 {
		t.Fatal("the version announcement raised no notice, so the notice path was not exercised")
	}
	for _, n := range notices {
		check("notice", n)
	}
}
