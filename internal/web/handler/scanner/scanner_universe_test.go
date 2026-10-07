package scanner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/universe"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
)

// fakeImporter is a UniverseImporter whose master is empty until a
// successful ImportJPX.
type fakeImporter struct {
	empty       bool
	emptyErr    error
	importCount int
	importErr   error
	imports     int
}

func (f *fakeImporter) Empty(context.Context) (bool, error) { return f.empty, f.emptyErr }

func (f *fakeImporter) ImportJPX(context.Context) (int, error) {
	f.imports++
	if f.importErr != nil {
		return 0, f.importErr
	}
	f.empty = false
	return f.importCount, nil
}

func universeEngine(imp scanner.UniverseImporter, src scanner.CandidateSource) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := scanHandler(src)
	if imp != nil {
		h.SetUniverseImporter(imp)
	}
	engine.GET("/scanner", h.Page)
	engine.GET("/scanner/scan", h.ScanView)
	engine.POST("/scanner/universe/import", h.UniverseImport)
	return engine
}

func serveUniverse(engine *gin.Engine, method, target string) (int, string) {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestScanPanel_EmptyMaster_ShowsGuidanceAndImportOfferWithoutImporting(t *testing.T) {
	imp := &fakeImporter{empty: true}
	for _, src := range []scanner.CandidateSource{scanSource{}, scanSource{cycle: scanFixture(), ok: true}} {
		code, body := serveUniverse(universeEngine(imp, src), http.MethodGet, "/scanner/scan")
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		for _, want := range []string{"銘柄マスタが未投入です", `data-testid="scan-universe-csv-guide"`, "PITHA_UNIVERSE_PATH", "config/universe.csv", `hx-post="/scanner/universe/import"`, "JPXから取得して投入する", `href="https://www.jpx.co.jp/term-of-use/"`} {
			if !strings.Contains(body, want) {
				t.Errorf("panel missing %q", want)
			}
		}
		if strings.Contains(body, `data-testid="scan-empty"`) || strings.Contains(body, "最初のサイクルが完了すると") {
			t.Error("panel kept the 'no cycle yet' empty state")
		}
		if strings.Contains(body, `data-testid="scan-funnel"`) {
			t.Error("panel rendered a funnel for an empty master")
		}
	}
	if imp.imports != 0 {
		t.Errorf("ImportJPX ran %d times while only rendering; the import needs the operator's POST", imp.imports)
	}
}

func TestScanPanel_PopulatedMasterOrNoImporter_KeepsTheRegularPanel(t *testing.T) {
	cases := map[string]scanner.UniverseImporter{
		"master has stocks": &fakeImporter{empty: false},
		"emptiness unknown": &fakeImporter{empty: true, emptyErr: errors.New("db down")},
		"no importer wired": nil,
	}
	for name, imp := range cases {
		t.Run(name, func(t *testing.T) {
			_, body := serveUniverse(universeEngine(imp, scanSource{}), http.MethodGet, "/scanner/scan")
			if !strings.Contains(body, `data-testid="scan-empty"`) {
				t.Error("regular empty state missing")
			}
			for _, banned := range []string{"銘柄マスタが未投入です", "scan-universe-import", "universe/import"} {
				if strings.Contains(body, banned) {
					t.Errorf("panel offered %q though the master is not known to be empty", banned)
				}
			}
		})
	}
}

func TestUniverseImport_SuccessRegistersAndExplainsNextCycle(t *testing.T) {
	imp := &fakeImporter{empty: true, importCount: 3707}
	code, body := serveUniverse(universeEngine(imp, scanSource{}), http.MethodPost, "/scanner/universe/import")
	if code != http.StatusOK || imp.imports != 1 {
		t.Fatalf("status %d, imports %d; want 200 and one import", code, imp.imports)
	}
	for _, want := range []string{`data-testid="scan-universe-imported"`, `id="scan-universe-imported"`, "3707 銘柄", "再起動は不要"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
	if strings.Contains(body, "scan-universe-import-error") || strings.Contains(body, "銘柄マスタが未投入です") {
		t.Error("response still shows the empty-master state after a successful import")
	}
}

func TestUniverseImport_FailureShowsFixedMessageAndLogsTheCause(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		cause string
		want  string
	}{
		{"connect failed", fmt.Errorf("%w: %w", universe.ErrJPXConnect, errors.New(`Get "https://www.jpx.co.jp/x.xlsx": dial tcp 127.0.0.53:53: lookup failed`)), "dial tcp", "JPXに接続できませんでした"},
		{"format changed", fmt.Errorf("%w: %w", universe.ErrJPXFormat, errors.New(`header: missing required column "code"`)), "missing required column", "JPX側で公開URLや形式が変わった可能性"},
		{"save failed", fmt.Errorf("%w: %w", universe.ErrJPXSave, errors.New("sqlite: disk I/O error")), "disk I/O error", "銘柄マスタへの保存に失敗しました"},
		{"unclassified", errors.New("boom: internal detail"), "internal detail", "原因を特定できませんでした"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			imp := &fakeImporter{empty: true, importErr: tc.err}
			code, body := serveUniverse(universeEngine(imp, scanSource{}), http.MethodPost, "/scanner/universe/import")
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 so HTMX swaps the panel", code)
			}
			for _, want := range []string{`data-testid="scan-universe-import-error"`, tc.want, "変更していません", "PITHA_UNIVERSE_PATH", "scan-universe-import\"", `id="scan-universe-import"`} {
				if !strings.Contains(body, want) {
					t.Errorf("response missing %q", want)
				}
			}
			if strings.Contains(body, tc.cause) {
				t.Errorf("response leaks the low-level cause %q", tc.cause)
			}
			if !strings.Contains(logs.String(), tc.cause) {
				t.Errorf("slog missing the cause %q: %s", tc.cause, logs.String())
			}
		})
	}
}

func TestUniverseImport_RefusedWhenMasterExistsOrUnavailable(t *testing.T) {
	populated := &fakeImporter{empty: false}
	if code, _ := serveUniverse(universeEngine(populated, scanSource{}), http.MethodPost, "/scanner/universe/import"); code != http.StatusConflict || populated.imports != 0 {
		t.Errorf("populated master: status %d, imports %d; want 409 and no download", code, populated.imports)
	}
	unknown := &fakeImporter{emptyErr: errors.New("db down")}
	if code, _ := serveUniverse(universeEngine(unknown, scanSource{}), http.MethodPost, "/scanner/universe/import"); code != http.StatusInternalServerError || unknown.imports != 0 {
		t.Errorf("unknown state: status %d, imports %d; want 500 and no download", code, unknown.imports)
	}
	if code, _ := serveUniverse(universeEngine(nil, scanSource{}), http.MethodPost, "/scanner/universe/import"); code != http.StatusNotFound {
		t.Errorf("no importer: status %d, want 404", code)
	}
}
