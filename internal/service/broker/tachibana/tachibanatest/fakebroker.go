package tachibanatest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const (
	// AuthID is the 認証ID every test uses (it must never reach logs).
	AuthID = "AUTHID-SECRET-1234"
	// SecretTag is embedded in every virtual URL the fake issues (it must
	// never reach logs).
	SecretTag = "VIRTUALURL-SECRET" //nolint:gosec // G101: a test marker, not a credential
)

// Request is one request the fake broker received.
type Request struct {
	Path   string
	Method string
	Body   map[string]string
	At     time.Time // the test clock's time at receipt
}

// Responder scripts one answer: the HTTP status and the JSON body. A nil body
// falls back to the default (a login gets fresh virtual URLs, anything else an
// empty success). loginN is the 1-based login counter (0 for non-logins).
type Responder func(req Request, loginN int) (status int, body map[string]any)

// FakeBroker is an httptest TLS server speaking the REQUEST I/F: Shift-JIS
// JSON in and out.
type FakeBroker struct {
	t     *testing.T
	srv   *httptest.Server
	key   *rsa.PrivateKey
	clock tachibana.Clock

	mu         sync.Mutex
	reqs       []Request
	inFlight   int
	maxIn      int
	respond    Responder
	logins     int
	loginExtra map[string]any
}

// New starts a fake broker; clk stamps the requests it records.
func New(t *testing.T, clk tachibana.Clock) *FakeBroker {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &FakeBroker{t: t, key: key, clock: clk}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// Server is the underlying TLS server (its Client() trusts the certificate).
func (f *FakeBroker) Server() *httptest.Server { return f.srv }

// Key is the private key paired with the "registered public key".
func (f *FakeBroker) Key() *rsa.PrivateKey { return f.key }

// BaseURL is the API base URL (with the version prefix).
func (f *FakeBroker) BaseURL() string { return f.srv.URL + "/e_api_v4r10/" }

// KeyFile writes the private key (PKCS#8 PEM) to a temp file.
func (f *FakeBroker) KeyFile() string {
	der, err := x509.MarshalPKCS8PrivateKey(f.key)
	if err != nil {
		f.t.Fatal(err)
	}
	p := filepath.Join(f.t.TempDir(), "key.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// Settings are the 立花 settings pointing at this fake (demo environment).
func (f *FakeBroker) Settings() config.TachibanaSettings {
	return config.TachibanaSettings{
		Environment:         config.TachibanaEnvDemo,
		DemoBaseURL:         f.BaseURL(),
		DemoPrivateKeyPath:  f.KeyFile(),
		RequestMaxPerSecond: 10,
		ReauthTime:          config.DefaultTachibanaReauthTime,
	}
}

// Credentials are the test 認証ID.
func Credentials() config.TachibanaCredentials { return config.TachibanaCredentials{AuthID: AuthID} }

// Secrets are the strings that must never appear in a log line, an error or
// a notice: the 認証ID, the virtual URL marker, the key material.
func (f *FakeBroker) Secrets() []string {
	der, _ := x509.MarshalPKCS8PrivateKey(f.key)
	return []string{AuthID, SecretTag, base64.StdEncoding.EncodeToString(der)[:40], "BEGIN PRIVATE KEY"}
}

// Encrypt encrypts plain for the registered public key (RSA-OAEP SHA-256, base64).
func (f *FakeBroker) Encrypt(plain string) string {
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &f.key.PublicKey, []byte(plain), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ct)
}

// Respond installs r (nil restores the defaults).
func (f *FakeBroker) Respond(r Responder) {
	f.mu.Lock()
	f.respond = r
	f.mu.Unlock()
}

// SetLoginExtra merges m into every successful login answer (nil clears).
func (f *FakeBroker) SetLoginExtra(m map[string]any) {
	f.mu.Lock()
	f.loginExtra = m
	f.mu.Unlock()
}

// Requests returns a copy of everything received so far.
func (f *FakeBroker) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}

// Count is how many requests with sCLMID clm were received.
func (f *FakeBroker) Count(clm string) int {
	n := 0
	for _, r := range f.Requests() {
		if r.Body["sCLMID"] == clm {
			n++
		}
	}
	return n
}

// MaxInFlight is the highest number of overlapping requests seen.
func (f *FakeBroker) MaxInFlight() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxIn
}

// ControlError is the answer for a control-level failure (p_errno != 0).
func ControlError(errno, text string) map[string]any {
	return map[string]any{"p_errno": errno, "p_err": text, "sCLMID": "", "sResultCode": ""}
}

func (f *FakeBroker) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	sjis, err := japanese.ShiftJIS.NewDecoder().Bytes(raw)
	if err != nil {
		http.Error(w, "bad sjis", http.StatusBadRequest)
		return
	}
	var body map[string]string
	if err := json.Unmarshal(sjis, &body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req := Request{Path: r.URL.Path, Method: r.Method, Body: body, At: f.clock.Now()}

	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.inFlight++
	f.maxIn = max(f.maxIn, f.inFlight)
	loginN := 0
	if body["sCLMID"] == "CLMAuthLoginRequest" {
		f.logins++
		loginN = f.logins
	}
	respond, extra := f.respond, f.loginExtra
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.inFlight--; f.mu.Unlock() }()

	status, out := http.StatusOK, map[string]any(nil)
	if respond != nil {
		status, out = respond(req, loginN)
	}
	if out == nil {
		out = f.defaultAnswer(req, loginN, extra)
	}
	time.Sleep(time.Millisecond) // widen the window in which overlapping requests would be seen
	enc, err := japanese.ShiftJIS.NewEncoder().Bytes(mustJSON(out))
	if err != nil {
		f.t.Errorf("encode: %v", err)
	}
	w.WriteHeader(status)
	_, _ = w.Write(enc)
}

func (f *FakeBroker) defaultAnswer(req Request, loginN int, extra map[string]any) map[string]any {
	if req.Body["sCLMID"] != "CLMAuthLoginRequest" {
		return map[string]any{"sCLMID": req.Body["sCLMID"] + "Ack", "p_errno": "0", "sResultCode": "0"}
	}
	n := strconv.Itoa(loginN)
	out := map[string]any{
		"sCLMID": "CLMAuthLoginAck", "p_errno": "0", "sResultCode": "0", "sKinsyouhouMidokuFlg": "0",
		"sUrlRequest":              f.Encrypt(f.srv.URL + "/" + SecretTag + "/req/" + n),
		"sUrlMaster":               f.Encrypt(f.srv.URL + "/" + SecretTag + "/master/" + n),
		"sUrlPrice":                f.Encrypt(f.srv.URL + "/" + SecretTag + "/price/" + n),
		"sUrlEvent":                f.Encrypt(f.srv.URL + "/" + SecretTag + "/event/" + n),
		"sUrlEventWebSocket":       f.Encrypt("wss" + f.srv.URL[len("https"):] + "/" + SecretTag + "/ws/" + n),
		"sUpdateInformWebDocument": "", "sUpdateInformAPISpecFunction": "",
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
