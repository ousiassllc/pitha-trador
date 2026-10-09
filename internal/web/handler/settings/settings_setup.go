package settings

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// brokerSettings is the broker selection the Setup Guard and the Setup/
// Settings screens follow: the effective runtime_settings value, read per
// request so a selection saved on `/setup` applies at once. Without
// OperationalSettings, or when the read fails, it is the kabu default (what
// the app did before the selection existed) - never a reason to lock out.
func brokerSettings(ctx context.Context, ops OperationalSettings) config.BrokerSettings {
	if ops == nil {
		return config.BrokerSettings{}
	}
	b, err := ops.Broker(ctx)
	if err != nil {
		slog.Error("settings: read broker selection; assuming kabu", "error", err)
		return config.BrokerSettings{}
	}
	return b
}

// SetupRequirementsFrom is the Setup Guard's requirements source (issue #734):
// config.RequiredSetup of the broker ops currently selects (ops may be nil).
func SetupRequirementsFrom(ops OperationalSettings) middleware.SetupRequirementsFunc {
	return func(ctx context.Context) config.SetupRequirements {
		return config.RequiredSetup(brokerSettings(ctx, ops))
	}
}

// requirements returns what setup needs for the selected broker, and that
// selection.
func (h *SettingsHandler) requirements(ctx context.Context) (config.SetupRequirements, config.BrokerSettings) {
	b := brokerSettings(ctx, h.ops)
	return config.RequiredSetup(b), b
}

func (h *SettingsHandler) requiredSecretKeys(ctx context.Context) []string {
	required, _ := h.requirements(ctx)
	return required.SecretKeys
}

// setupComplete reports whether everything the selected broker needs is
// stored: its required secrets and, for 立花, the 秘密鍵 path.
func (h *SettingsHandler) setupComplete(ctx context.Context) bool {
	required, _ := h.requirements(ctx)
	if required.PrivateKeyPathKey != "" && !required.PrivateKeyPathSet {
		return false
	}
	return len(h.unsetKeys(ctx, required.SecretKeys)) == 0
}

// setupConnectionIDs are the connections the first-run Setup screen offers
// (issue #80, #734): Jev, the selected broker's connection and the optional
// Slack. The other connections are configured later on Settings.
func setupConnectionIDs(provider string) []string {
	if provider == config.BrokerTachibana {
		return []string{"jev", "tachibana", "slack"}
	}
	return []string{"jev", "kabu", "slack"}
}

// setupOpsGroups are the operational groups the Setup screen offers: the
// broker selection always, and the 立花 connection settings (環境・秘密鍵の
// パス) once 立花 is selected.
func setupOpsGroups(provider string) []opsGroup {
	if provider == config.BrokerTachibana {
		return []opsGroup{brokerGroup, tachibanaGroup}
	}
	return []opsGroup{brokerGroup}
}

// SetupPage implements `GET /setup` (issue #80, FR-SETUP-2): the connections
// of setupConnectionIDs for the selected broker (Jev, kabuステーション or
// 立花証券 e支店, plus the optional Slack) in the same
// organisms.ConnectionList as Settings (issue #302), posting to the same
// `POST`/`DELETE /settings/:key` routes, and - with OperationalSettings - the
// broker selection and 立花 settings groups posting to `/ops-settings/:key`.
// Complete reports whether everything the selected broker needs is stored;
// `/setup` stays reachable afterwards.
func (h *SettingsHandler) SetupPage(c *gin.Context) {
	ctx := c.Request.Context()
	_, broker := h.requirements(ctx)
	var offered []connection
	for _, id := range setupConnectionIDs(broker.Provider) {
		if conn, ok := connectionByID(id); ok {
			offered = append(offered, conn)
		}
	}
	var ops []molecules.SettingGroupProps
	if h.ops != nil {
		for _, group := range setupOpsGroups(broker.Provider) {
			ops = append(ops, h.opsGroupProps(ctx, group, "", ""))
		}
	}
	shared.RenderHTML(c, http.StatusOK, pages.SetupPage(pages.SetupProps{
		Connections: h.connections(ctx, offered),
		Operational: ops,
		Complete:    h.setupComplete(ctx),
		Tachibana:   broker.Provider == config.BrokerTachibana,
		Production:  broker.Provider == config.BrokerTachibana && broker.Tachibana.Production(),
	}))
}

// refreshesSetup reports whether a successful ops save/reset of key from the
// Setup screen must reload it: the broker selection and environment decide
// which connections it offers and which keys are required.
func refreshesSetup(c *gin.Context, key string) bool {
	return settingsReturnPath(c) == "/setup" && slices.Contains([]string{config.KeyBrokerProvider, config.KeyTachibanaEnvironment}, key)
}
