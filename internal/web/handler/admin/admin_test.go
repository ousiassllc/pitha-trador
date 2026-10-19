package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/adminhub"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/admin"
)

func render(t *testing.T, swagger bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/admin", admin.NewHandler(swagger).Page)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// The page is titled 管理 and lists every entry's name and href under its own
// category heading, in category order.
func TestPage_ListsEveryEntryUnderItsCategory(t *testing.T) {
	body := render(t, true)
	if !strings.Contains(body, "<title>管理 - pitha-trador</title>") {
		t.Errorf("page title is not 管理")
	}
	if !strings.Contains(body, "<h1 class=\"text-2xl font-bold text-slate-900\">管理</h1>") {
		t.Errorf("no 管理 heading")
	}
	cats := adminhub.Categories()
	headingAt := make([]int, len(cats)+1)
	for i, c := range cats {
		headingAt[i] = strings.Index(body, ">"+c.Name+"</h2>")
		if headingAt[i] < 0 {
			t.Fatalf("category heading %q not rendered", c.Name)
		}
		if i > 0 && headingAt[i] < headingAt[i-1] {
			t.Errorf("category %q rendered before %q", c.Name, cats[i-1].Name)
		}
	}
	headingAt[len(cats)] = len(body)
	for i, c := range cats {
		section := body[headingAt[i]:headingAt[i+1]]
		for _, e := range adminhub.Entries() {
			if e.Category != c.ID {
				continue
			}
			if !strings.Contains(section, ">"+e.Name+"</a>") || !strings.Contains(section, `href="`+e.Href+`"`) || !strings.Contains(section, e.Description) {
				t.Errorf("category %q lacks entry %q (%s) with its description", c.Name, e.Name, e.Href)
			}
		}
	}
}

// /swagger is offered only while the route is registered.
func TestPage_SwaggerLinkFollowsSwitch(t *testing.T) {
	if strings.Contains(render(t, false), `href="/swagger"`) {
		t.Error("hub links /swagger while Swagger is disabled")
	}
	if !strings.Contains(render(t, true), `href="/swagger"`) {
		t.Error("hub lacks /swagger while Swagger is enabled")
	}
}
