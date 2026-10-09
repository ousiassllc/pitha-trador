package setupflow_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

type fakeSession struct {
	name   string
	status broker.SessionStatus
}

func (f fakeSession) Capabilities() broker.Capabilities { return broker.Capabilities{Name: f.name} }
func (f fakeSession) Status() broker.SessionStatus      { return f.status }

func cardEngine(secrets memSecrets, runtime memRuntime, session settings.BrokerSession) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := settings.NewSettingsHandler(secrets).WithOperationalSettings(opsettings.New(runtime, strategy(), "/data/logs"))
	if session != nil {
		h.WithBrokerSession(session)
	}
	engine := gin.New()
	engine.GET("/settings", h.Page)
	engine.POST("/settings/:key", h.Save)
	return engine
}

func testID(body, id string) bool { return strings.Contains(body, `data-testid="`+id+`"`) }

func TestTachibanaCard_ShowsTheEnvironmentAlways(t *testing.T) {
	// Nothing stored, kabu selected: the card still names the environment.
	body := get(cardEngine(memSecrets{}, memRuntime{}, nil), "/settings")
	if !hasCard(body, "tachibana") || !testID(body, "connection-badge-tachibana-environment") || !strings.Contains(body, "デモ環境") {
		t.Errorf("the default card must show the デモ environment; body=%s", body)
	}
	if testID(body, "connection-badge-tachibana-no-orders") {
		t.Error("the no-orders notice belongs to 本番 only")
	}
	if testID(body, "connection-badge-tachibana-selected") {
		t.Error("kabu is selected; the card must not claim to be the used broker")
	}
}

func TestTachibanaCard_ProductionSaysNoOrdersUntilIssue55(t *testing.T) {
	runtime := memRuntime{config.KeyBrokerProvider: `"tachibana"`, config.KeyTachibanaEnvironment: `"production"`}
	body := get(cardEngine(memSecrets{}, runtime, nil), "/settings")
	for _, id := range []string{"connection-badge-tachibana-environment", "connection-badge-tachibana-no-orders", "connection-badge-tachibana-selected"} {
		if !testID(body, id) {
			t.Errorf("production card lacks %s", id)
		}
	}
	if !strings.Contains(body, "本番環境") || !strings.Contains(body, "発注は行いません（#55 まで）") {
		t.Errorf("production card must say 本番環境 and that no orders are placed; body=%s", body)
	}
}

func TestTachibanaCard_ShowsSessionStateOnlyWhileTheTachibanaAdapterRuns(t *testing.T) {
	login := time.Date(2026, 10, 8, 5, 35, 0, 0, time.UTC)
	status := broker.SessionStatus{
		LoggedInAt: login, NextReauth: login.Add(24 * time.Hour), APIVersion: "v4r10", DocumentsUnread: true, VersionRetiring: true,
	}
	runtime := memRuntime{config.KeyBrokerProvider: `"tachibana"`}

	body := get(cardEngine(memSecrets{}, runtime, fakeSession{name: "tachibana", status: status}), "/settings")
	for id, want := range map[string]string{
		"connection-detail-tachibana-last-login":       "2026-10-08 14:35:00 JST",
		"connection-detail-tachibana-next-reauth":      "2026-10-09 14:35:00 JST",
		"connection-detail-tachibana-api-version":      "v4r10",
		"connection-detail-tachibana-documents-unread": "書面が未読",
		"connection-detail-tachibana-version-retiring": "版数更新が予告",
	} {
		i := strings.Index(body, `data-testid="`+id+`"`)
		if i < 0 {
			t.Errorf("card lacks %s", id)
			continue
		}
		if end := strings.Index(body[i:], "</span>"); !strings.Contains(body[i:i+end], want) {
			t.Errorf("%s = %q, want it to contain %q", id, body[i:i+end], want)
		}
	}

	// kabu keeps running until the restart: its status is not 立花's.
	body = get(cardEngine(memSecrets{}, runtime, fakeSession{name: "kabu", status: status}), "/settings")
	if strings.Contains(body, "connection-detail-tachibana-") {
		t.Error("a kabu session must not be shown as the 立花 session")
	}
	body = get(cardEngine(memSecrets{}, runtime, fakeSession{name: "tachibana"}), "/settings")
	if !strings.Contains(body, "最終ログイン: —") || !strings.Contains(body, "API 版数: 不明") {
		t.Error("a session that never logged in must show placeholders")
	}
	if strings.Contains(body, "connection-detail-tachibana-documents-unread") {
		t.Error("notices appear only when the broker raised them")
	}
}

// issue #738: the card, its modal and the whole page never echo credentials,
// the key path, the stored second password or anything of the session beyond
// the fixed fields (Guidance may hold a broker-side URL).
func TestTachibanaCard_NeverEchoesSecretsAndShowsOnlyConfigured(t *testing.T) {
	secrets := memSecrets{
		config.KeyTachibanaDemoAuthID:         "AUTHID-demo-1234",
		config.KeyTachibanaProdAuthID:         "AUTHID-prod-5678",
		config.KeyTachibanaDemoSecondPassword: "SECOND-pw-9999",
	}
	runtime := memRuntime{
		config.KeyBrokerProvider:              `"tachibana"`,
		config.KeyTachibanaDemoPrivateKeyPath: `"/home/u/KEYPATH-demo.pem"`,
	}
	session := fakeSession{name: "tachibana", status: broker.SessionStatus{
		Issue: broker.SessionIssueBadPassword, Code: 10005, Guidance: "https://virtual.example.jp/VIRTUAL-URL-TOKEN", APIVersion: "v4r10",
	}}
	engine := cardEngine(secrets, runtime, session)

	pages := map[string]string{"settings page": get(engine, "/settings")}
	save := httptest.NewRequest(http.MethodPost, "/settings/"+config.KeyTachibanaDemoAuthID, strings.NewReader("value=AUTHID-new-0000"))
	save.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	save.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, save)
	pages["save response"] = rec.Body.String()

	for name, body := range pages {
		for _, secret := range []string{"AUTHID-", "SECOND-pw", "KEYPATH-demo", "VIRTUAL-URL", "virtual.example.jp"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s shows %q", name, secret)
			}
		}
	}
	body := pages["settings page"]
	for _, key := range []string{config.KeyTachibanaDemoAuthID, config.KeyTachibanaProdAuthID, config.KeyTachibanaDemoSecondPassword} {
		i := strings.Index(body, `data-testid="secret-field-row-`+key+`"`)
		if i < 0 {
			t.Fatalf("no row for %s", key)
		}
		if row := body[i : i+strings.Index(body[i:], "</form>")]; !strings.Contains(row, "設定済み") {
			t.Errorf("%s row does not say 設定済み", key)
		}
	}
}
