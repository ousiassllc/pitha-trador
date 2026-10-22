package settings

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// SubPage returns the handler of `GET /settings/<page.ID>` (issue #805): the
// page's connections, setting groups, system entries and links, selected from
// the settingspages registry by dialog id. Every connection and setting group
// must be placed on exactly one page (settings_pages_test.go), so a new card
// is only reachable once the registry names its dialog.
//
// swagger shows the `/swagger` link (SWAGGER_ENABLED=true).
//
// Only the connections and groups on the page are read, so an operator on 運用
// never waits for the 立花 session probe of ブローカー.
func (h *SettingsHandler) SubPage(page settingspages.Page, swagger bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		dialogs := settingspages.DialogsOf(page.ID)
		ids := make([]string, len(dialogs))
		for i, d := range dialogs {
			ids[i] = d.ModalID
		}
		props := pages.SettingsProps{
			Page:        page,
			Dialogs:     ids,
			Connections: h.connections(ctx, connectionsOn(ids)),
			Operational: h.opsGroupsOn(ctx, ids),
			MCP:         h.mcp,
			JevPrompt:   h.jevPrompt,
			Links:       settingspages.LinksOf(page.ID, swagger),
		}
		shared.RenderHTML(c, http.StatusOK, pages.SettingsPage(props))
	}
}

// connectionsOn returns the connections whose dialog is in modalIDs, in
// display order.
func connectionsOn(modalIDs []string) []connection {
	var out []connection
	for _, conn := range settingsConnections {
		if slices.Contains(modalIDs, "modal-"+conn.id) {
			out = append(out, conn)
		}
	}
	return out
}

// opsGroupsOn builds the setting groups whose dialog is in modalIDs, in
// display order; nil without OperationalSettings.
func (h *SettingsHandler) opsGroupsOn(ctx context.Context, modalIDs []string) []molecules.SettingGroupProps {
	if h.ops == nil {
		return nil
	}
	var out []molecules.SettingGroupProps
	for _, group := range opsGroups {
		if slices.Contains(modalIDs, "modal-"+group.id) {
			out = append(out, h.opsGroupProps(ctx, group, "", ""))
		}
	}
	return out
}

// Page implements `GET /settings`: the Settings hub listing the pages under
// it (issue #805, internal/web/settingspages). Each page is served by
// SubPage.
func (h *SettingsHandler) Page(c *gin.Context) {
	shared.RenderHTML(c, http.StatusOK, pages.SettingsHubPage(settingspages.Pages()))
}

// settingsReturnPath is where a non-HTMX Save/Delete goes back to: `/setup`
// when the form was submitted from the Setup screen, the Settings page the
// form was on (issue #805) when the Referer is one, `/settings` otherwise.
// Only these fixed paths are ever returned, never the raw Referer.
func settingsReturnPath(c *gin.Context) string {
	ref, err := url.Parse(c.GetHeader("Referer"))
	if err != nil {
		return settingspages.BasePath
	}
	if ref.Path == "/setup" {
		return "/setup"
	}
	for _, page := range settingspages.Pages() {
		if ref.Path == page.Path() {
			return page.Path()
		}
	}
	return settingspages.BasePath
}
