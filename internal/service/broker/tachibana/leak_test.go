package tachibana_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// SourceMentions reports whether any production file of the 立花 adapter tree
// (this package and its subpackages, tests and test support excluded)
// mentions s.
func SourceMentions(t *testing.T, s string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "tachibanatest" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), s) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// SyncBuffer is an io.Writer safe for slog from several goroutines.
type SyncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *SyncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func captureLogs(t *testing.T) *SyncBuffer {
	t.Helper()
	buf := &SyncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func TestTransportErrorKeepsCauseButNotURL(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	secrets := fb.Secrets()
	fb.Server().Close()
	err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	for _, s := range secrets {
		if strings.Contains(err.Error(), s) {
			t.Errorf("transport error contains %q: %v", s, err)
		}
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		t.Error("the *url.Error (which holds the full URL) must not stay reachable")
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("the underlying cause must stay reachable: %v", err)
	}
}

func TestDecryptErrorsCarryNoMaterial(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb, other := tt.New(t, clk), tt.New(t, clk)
	if _, err := tachibana.DecryptVirtualURL(fb.Key(), "!!!not base64!!!"); !errors.Is(err, tachibana.ErrDecryptURL) {
		t.Fatal(err)
	}
	_, err := tachibana.DecryptVirtualURL(other.Key(), fb.Encrypt("https://example.invalid/"+tt.SecretTag))
	if !errors.Is(err, tachibana.ErrDecryptURL) || strings.Contains(err.Error(), tt.SecretTag) {
		t.Errorf("wrong-key decrypt error = %v", err)
	}
	if _, err := tachibana.DecryptVirtualURL(fb.Key(), fb.Encrypt("not a url")); !errors.Is(err, tachibana.ErrDecryptURL) {
		t.Errorf("a plaintext that is no URL must be refused, got %v", err)
	}
	if _, err := tachibana.LoadPrivateKey(filepath.Join(t.TempDir(), "missing.pem")); !errors.Is(err, tachibana.ErrKeyUnreadable) {
		t.Errorf("missing key error = %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("-----BEGIN PRIVATE KEY-----\nQUJD\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tachibana.LoadPrivateKey(bad); !errors.Is(err, tachibana.ErrKeyUnreadable) || strings.Contains(err.Error(), "QUJD") {
		t.Errorf("malformed key error = %v", err)
	}
}

func TestVirtualURLsRedactThemselves(t *testing.T) {
	u := tachibana.NewVirtualURLs("https://secret.example/abc", "wss://secret.example/ws")
	logs := captureLogs(t)
	slog.Info("x", "u", u)
	for _, s := range []string{fmt.Sprint(u), fmt.Sprintf("%+v", u), fmt.Sprintf("%#v", u), logs.String()} {
		if strings.Contains(s, "secret.example") {
			t.Errorf("virtual URLs leaked through formatting: %s", s)
		}
	}
}

// Logging in, requesting and failing leave no virtual URL, 認証ID or key in
// the logs or in error strings.
func TestClientNeverLogsOrReturnsSecrets(t *testing.T) {
	logs := captureLogs(t)
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	var errs []string
	for _, resp := range []tt.Responder{
		func(tt.Request, int) (int, map[string]any) { return 500, map[string]any{} },
		func(tt.Request, int) (int, map[string]any) { return 200, tt.ControlError("6", "p_no") },
		func(tt.Request, int) (int, map[string]any) { return 200, tt.ControlError("-2", "busy") },
	} {
		fb.Respond(resp)
		if err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote); err != nil {
			errs = append(errs, err.Error())
		}
	}
	slog.Info("urls", "urls", c.SessionURLs())
	_ = c.Logout(context.Background())
	for _, secret := range fb.Secrets() {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs contain %q:\n%s", secret, logs.String())
		}
		for _, e := range errs {
			if strings.Contains(e, secret) {
				t.Errorf("error %q contains %q", e, secret)
			}
		}
	}
}

// Orders are out of scope (issue #55): no order CLMID and no 第二暗証番号
// exist anywhere in the adapter.
func TestNoOrderPathAndNoSecondPassword(t *testing.T) {
	for _, name := range []string{"CLMKabuNewOrder", "CLMKabuCorrectOrder", "CLMKabuCancelOrder", "SecondPassword", "sSecondPassword"} {
		if SourceMentions(t, name) {
			t.Errorf("%s is referenced in the adapter", name)
		}
	}
}
