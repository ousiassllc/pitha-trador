package admin_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

const tailnetIP = "100.101.102.103"

func fakeIfaces() ([]net.Addr, error) {
	return []net.Addr{&net.IPNet{IP: net.ParseIP(tailnetIP), Mask: net.CIDRMask(32, 32)}}, nil
}

// harness is a Service over a temp settings file with a recording fake Dial.
type harness struct {
	svc    *admin.Service
	path   string
	dials  []string
	dialOK bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{path: filepath.Join(t.TempDir(), settings.FileName)}
	h.svc = admin.New(admin.Options{
		Path:           h.path,
		ExePath:        func() (string, error) { return filepath.Join(filepath.Dir(h.path), "paper-results-mcp"), nil },
		Dial:           func(_ context.Context, addr string) error { h.dials = append(h.dials, addr); return h.dialError() },
		InterfaceAddrs: fakeIfaces,
	})
	return h
}

func (h *harness) dialError() error {
	if h.dialOK {
		return nil
	}
	return errors.New("connection refused")
}

func (h *harness) seed(t *testing.T, s settings.Settings) {
	t.Helper()
	if err := settings.Save(h.path, s); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) fileBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUpdateValidatesListenOnTheServerSide(t *testing.T) {
	rejected := []string{
		"0.0.0.0:48110", "[::]:48110", ":48110", "192.168.1.20:48110", "10.0.0.5:48110", "8.8.8.8:48110",
		"localhost:48110", "example.com:48110", "127.0.0.1:0", "127.0.0.1", "100.64.1.2:48110", // tailnet, not on this PC
	}
	for _, listen := range rejected {
		t.Run("reject "+listen, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, settings.Settings{Listen: settings.DefaultListen, APIKey: "pitha-mcp-k"})
			before := h.fileBytes(t)
			err := h.svc.Update(context.Background(), true, listen)
			if !errors.Is(err, settings.ErrListenNotAllowed) {
				t.Fatalf("Update(%q) = %v, want ErrListenNotAllowed", listen, err)
			}
			if string(h.fileBytes(t)) != string(before) {
				t.Fatalf("file changed after a rejected address")
			}
		})
	}
	accepted := []string{"127.0.0.1:48110", "[::1]:48110", tailnetIP + ":48110"}
	for _, listen := range accepted {
		t.Run("accept "+listen, func(t *testing.T) {
			h := newHarness(t)
			if err := h.svc.Update(context.Background(), true, listen); err != nil {
				t.Fatalf("Update(%q): %v", listen, err)
			}
			got, err := settings.Load(h.path)
			if err != nil || got.Listen != listen || !got.Enabled {
				t.Fatalf("stored = %+v (err %v)", got, err)
			}
		})
	}
}

func TestUpdateRejectsWithoutCreatingTheFile(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Update(context.Background(), true, "0.0.0.0:48110"); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Stat(h.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a rejected first save must not create the file (stat err %v)", err)
	}
}

func TestUpdateEmptyListenMeansDefaultAndKeepsKeyAndAccessFields(t *testing.T) {
	h := newHarness(t)
	h.seed(t, settings.Settings{
		Listen: "[::1]:48120", APIKey: "pitha-mcp-keep",
		VerifyAccessHeaders: true, AccessClientID: "id", AccessClientSecret: "sec",
	})
	if err := h.svc.Update(context.Background(), true, "  "); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(h.path)
	if err != nil {
		t.Fatal(err)
	}
	want := settings.Settings{
		Enabled: true, Listen: settings.DefaultListen, APIKey: "pitha-mcp-keep",
		VerifyAccessHeaders: true, AccessClientID: "id", AccessClientSecret: "sec",
	}
	if got != want {
		t.Fatalf("stored = %+v, want %+v", got, want)
	}
}

func TestUpdateRewritesAnUnusableFileFromDefaults(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.path, []byte(`{"api_key": "OLD", "bogus": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := h.svc.View(context.Background()); v.LoadError != admin.LoadUnusableFile || v.State != admin.StateStopped {
		t.Fatalf("View of a bad file = %+v", v)
	}
	if err := h.svc.Update(context.Background(), true, "127.0.0.1:48111"); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(h.path)
	if err != nil || got.APIKey != "" || got.Listen != "127.0.0.1:48111" || !got.Enabled {
		t.Fatalf("rewritten = %+v (err %v)", got, err)
	}
}

func TestRegenerateKeyStoresANewKeyAndPreservesTheRest(t *testing.T) {
	h := newHarness(t)
	h.seed(t, settings.Settings{Enabled: true, Listen: "[::1]:48120", APIKey: "pitha-mcp-old", AccessClientID: "id"})
	key, err := h.svc.RegenerateKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != key || key == "pitha-mcp-old" || got.Listen != "[::1]:48120" || !got.Enabled || got.AccessClientID != "id" {
		t.Fatalf("stored = %+v (returned key %q)", got, key)
	}
	second, err := h.svc.RegenerateKey(context.Background())
	if err != nil || second == key {
		t.Fatalf("regenerate returned the same key twice (err %v)", err)
	}
}

func TestViewDefaultsAreOffAndNeverDial(t *testing.T) {
	h := newHarness(t)
	v := h.svc.View(context.Background())
	if v.Enabled || v.KeyConfigured || v.State != admin.StateStopped || v.Reason != admin.ReasonDisabled || v.LoadError != admin.LoadOK {
		t.Fatalf("missing file view = %+v", v)
	}
	if v.Listen != settings.DefaultListen || v.URL != "http://127.0.0.1:48110/" {
		t.Fatalf("listen/url = %q / %q", v.Listen, v.URL)
	}
	if len(h.dials) != 0 {
		t.Fatalf("dialled %v with the MCP off", h.dials)
	}
}

func TestViewStates(t *testing.T) {
	cases := []struct {
		name   string
		cfg    settings.Settings
		dialOK bool
		state  admin.State
		reason admin.Reason
		dials  int
	}{
		{"listening", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110", APIKey: "k"}, true, admin.StateListening, "", 1},
		{"not running", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110", APIKey: "k"}, false, admin.StateStopped, admin.ReasonNotRunning, 1},
		{"no key", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110"}, true, admin.StateNoKey, "", 0},
		{"disabled with key", settings.Settings{Listen: "127.0.0.1:48110", APIKey: "k"}, true, admin.StateStopped, admin.ReasonDisabled, 0},
		{"stored address not allowed", settings.Settings{Enabled: true, Listen: "0.0.0.0:48110", APIKey: "k"}, true, admin.StateStopped, admin.ReasonListenNotAllowed, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.dialOK = tc.dialOK
			h.seed(t, tc.cfg)
			v := h.svc.View(context.Background())
			if v.State != tc.state || v.Reason != tc.reason {
				t.Fatalf("state = %s/%s, want %s/%s", v.State, v.Reason, tc.state, tc.reason)
			}
			if len(h.dials) != tc.dials {
				t.Fatalf("dials = %v, want %d", h.dials, tc.dials)
			}
			if tc.dials == 1 && h.dials[0] != tc.cfg.Listen {
				t.Fatalf("dialled %q, want %q", h.dials[0], tc.cfg.Listen)
			}
			if v.KeyConfigured != (tc.cfg.APIKey != "") || v.AuthHeader != admin.MaskedAuthHeader {
				t.Fatalf("key flag/header = %v / %q", v.KeyConfigured, v.AuthHeader)
			}
		})
	}
}

func TestViewOfAnInsecureFileIsStoppedWithoutDialling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits")
	}
	h := newHarness(t)
	h.dialOK = true
	body := `{"enabled": true, "listen": "127.0.0.1:48110", "api_key": "SECRET-IN-FILE"}`
	if err := os.WriteFile(h.path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := h.svc.View(context.Background())
	if v.State != admin.StateStopped || v.Reason != admin.ReasonSettingsUnreadable || v.LoadError != admin.LoadInsecure || v.KeyConfigured {
		t.Fatalf("view = %+v", v)
	}
	if len(h.dials) != 0 {
		t.Fatalf("dialled %v for an unusable file", h.dials)
	}
	if s := fmt.Sprintf("%+v", v); strings.Contains(s, "SECRET-IN-FILE") {
		t.Fatalf("view leaks the key: %s", s)
	}
}

func TestViewReportsTheExecutableLocation(t *testing.T) {
	h := newHarness(t)
	if v := h.svc.View(context.Background()); v.ExeExists || v.ExePath == "" {
		t.Fatalf("absent exe: %+v", v)
	}
	exe := h.svc.View(context.Background())
	if err := os.WriteFile(exe.ExePath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := h.svc.View(context.Background()); !v.ExeExists {
		t.Fatalf("present exe not found: %+v", v)
	}
	// A directory of that name is not the executable.
	_ = os.Remove(exe.ExePath)
	if err := os.Mkdir(exe.ExePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if v := h.svc.View(context.Background()); v.ExeExists {
		t.Fatalf("a directory counted as the executable: %+v", v)
	}
}

func TestDefaultExePathIsBesideTheRunningExecutable(t *testing.T) {
	got, err := admin.DefaultExePath()
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if filepath.Dir(got) != filepath.Dir(self) || filepath.Base(got) != admin.ExeName() {
		t.Fatalf("DefaultExePath = %q (executable %q)", got, self)
	}
}
