package scanner

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// UniverseImporter lets the Scanner Dashboard offer the 銘柄マスタ未投入
// guidance and the operator-confirmed JPX import (issue #508).
// bootstrap/universe.Importer implements it. With none wired (the
// router default) the scan panel keeps its plain "no cycle yet" empty state
// and `POST /scanner/universe/import` 404s.
type UniverseImporter interface {
	// Empty reports whether no active stock is registered (nothing would
	// be scanned).
	Empty(ctx context.Context) (bool, error)
	// ImportJPX downloads JPX's 東証上場銘柄一覧 and registers its stocks,
	// returning how many it held. A failure leaves the master unchanged.
	ImportJPX(ctx context.Context) (int, error)
}

// SetUniverseImporter wires the master-import source (see UniverseImporter).
func (h *ScannerHandler) SetUniverseImporter(imp UniverseImporter) { h.universe = imp }

// universeEmpty reports whether the scan panel should show the 銘柄マスタ
// 未投入 state. A failed lookup is logged and treated as "not empty": the
// regular panel is the safe fallback and the import is never offered on a
// guess.
func (h *ScannerHandler) universeEmpty(ctx context.Context) bool {
	if h.universe == nil {
		return false
	}
	empty, err := h.universe.Empty(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "handler: scanner universe emptiness check", "error", err)
		return false
	}
	return empty
}

// UniverseImport implements `POST /scanner/universe/import` (docs/api/
// endpoints.md §3): the operator's consent to download JPX's 東証上場銘柄一覧
// and register its stocks. It answers with the refreshed scan panel
// (`#scan-panel`): the import's result, or - on failure, with the master
// unchanged - a fixed per-kind reason (never the error text, issue #700)
// plus the manual-CSV route, as a normal 200 so HTMX
// swaps it. It only runs while the master has no active stock (409
// otherwise), so an operator-supplied master is never overwritten.
func (h *ScannerHandler) UniverseImport(c *gin.Context) {
	ctx := c.Request.Context()
	if h.universe == nil {
		shared.RespondActionError(c, http.StatusNotFound, "銘柄マスタの自動取得は利用できません。")
		return
	}
	empty, err := h.universe.Empty(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "handler: scanner universe import check", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "銘柄マスタの状態を確認できませんでした。")
		return
	}
	if !empty {
		shared.RespondActionError(c, http.StatusConflict, "銘柄マスタは既に投入済みです。")
		return
	}

	imported, importErr := h.universe.ImportJPX(ctx)
	if importErr != nil {
		slog.ErrorContext(ctx, "handler: scanner universe import from JPX failed", "error", importErr)
	}
	panel, err := h.scanPanel(ctx, domain.ScanQuery{}, false)
	if err != nil {
		slog.ErrorContext(ctx, "handler: scanner universe import scan summary", "error", err)
	}
	panel.UniverseImported = imported
	if importErr != nil {
		panel.UniverseImportError = universeImportReason(importErr)
	}
	shared.RenderHTML(c, http.StatusOK, organisms.ScanPanel(panel))
}

// universeImportReason maps an ImportJPX failure onto a fixed operator-facing
// text. The error itself (URL/DNS, SQLite, ParseJPX detail) is logged by the
// caller and never shown (issue #700).
func universeImportReason(err error) string {
	switch {
	case errors.Is(err, domain.ErrJPXConnect):
		return "JPXに接続できませんでした（ネットワークを確認してください）。"
	case errors.Is(err, domain.ErrJPXFormat):
		return "JPXの銘柄一覧を解釈できませんでした（JPX側で公開URLや形式が変わった可能性があります）。"
	case errors.Is(err, domain.ErrJPXSave):
		return "銘柄マスタへの保存に失敗しました。"
	default:
		return "原因を特定できませんでした。詳細はログを確認してください。"
	}
}
