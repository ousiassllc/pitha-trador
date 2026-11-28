package apiroutes_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

func TestNew_OpenAPIServersPointAtAPIPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/openapi.json = %d: %s", rec.Code, rec.Body.String())
	}
	var spec struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi.json: %v", err)
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != "/api/v1" {
		t.Fatalf("openapi servers = %+v, want [{url:/api/v1}] so Try It requests hit /api/v1/*", spec.Servers)
	}
}

func TestNew_SchemaLinkAndFieldResolveUnderAPIPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/system/status = %d: %s", rec.Code, rec.Body.String())
	}

	link := rec.Header().Get("Link")
	if !strings.HasPrefix(link, "</api/v1/schemas/") || !strings.HasSuffix(link, `>; rel="describedBy"`) {
		t.Fatalf("Link header = %q, want describedBy under /api/v1/schemas/", link)
	}

	var body struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	u, err := url.Parse(body.Schema)
	if err != nil || !strings.HasPrefix(u.Path, "/api/v1/schemas/") {
		t.Fatalf("$schema = %q, want a path under /api/v1/schemas/", body.Schema)
	}

	schema := httptest.NewRecorder()
	engine.ServeHTTP(schema, httptest.NewRequest(http.MethodGet, u.Path, nil))
	if schema.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 (body=%s)", u.Path, schema.Code, schema.Body.String())
	}
}

func TestNew_OpenAPIListsMigrationPreviewUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/openapi.json = %d: %s", rec.Code, rec.Body.String())
	}
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]json.RawMessage `json:"content"`
			} `json:"requestBody"`
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi.json: %v", err)
	}
	op, ok := spec.Paths["/migration/preview"]["post"]
	if !ok {
		t.Fatalf("openapi paths lack POST /migration/preview (the gin upload route must be documented)")
	}
	if _, ok := op.RequestBody.Content["application/octet-stream"]; !ok {
		t.Errorf("POST /migration/preview request body content = %v, want application/octet-stream", op.RequestBody.Content)
	}
	for _, code := range []string{"200", "403", "409", "413", "422"} {
		if _, ok := op.Responses[code]; !ok {
			t.Errorf("POST /migration/preview lacks a %s response", code)
		}
	}
}
