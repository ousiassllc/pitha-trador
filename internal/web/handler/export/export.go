package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// DataExporter is the subset of internal/service/dataexport.Service the
// download needs. An interface keeps the handler testable without a database.
type DataExporter interface {
	Export(ctx context.Context, w io.Writer, req dataexport.Request) (dataexport.Manifest, error)
}

// ErrExporterNotConfigured is what UnconfiguredExporter returns.
var ErrExporterNotConfigured = errors.New("export: data exporter not configured (missing router.WithDataExporter)")

// UnconfiguredExporter fails every export: an entry point that forgets
// router.WithDataExporter gets a 500 (cause in the slog), not an empty ZIP.
type UnconfiguredExporter struct{}

func (UnconfiguredExporter) Export(context.Context, io.Writer, dataexport.Request) (dataexport.Manifest, error) {
	return dataexport.Manifest{}, ErrExporterNotConfigured
}

// MaxPeriodDays is the longest period one download may cover.
const MaxPeriodDays = 366

const dateLayout = "2006-01-02"

// Handler implements `GET /api/v1/export`.
type Handler struct {
	exporter DataExporter
	now      func() time.Time
}

// NewHandler returns a Handler backed by exporter; nil (an entry point that
// forgot router.WithDataExporter) means UnconfiguredExporter.
func NewHandler(exporter DataExporter) *Handler {
	if exporter == nil {
		exporter = UnconfiguredExporter{}
	}
	return &Handler{exporter: exporter, now: time.Now}
}

// Input is `GET /api/v1/export`'s query. Huma rejects a missing parameter,
// a malformed date or a value outside an enum with 422.
type Input struct {
	From   string   `query:"from" required:"true" format:"date" doc:"First UTC date of the period (YYYY-MM-DD), included."`
	To     string   `query:"to" required:"true" format:"date" doc:"Last UTC date of the period (YYYY-MM-DD), included. At most 366 days after from."`
	Kinds  []string `query:"kinds,explode" required:"true" enum:"trades,decisions,metrics" doc:"What to export: trades, decisions and/or metrics; repeat the parameter."`
	Format string   `query:"format" default:"csv" enum:"csv,jsonl" doc:"File format of every table in the archive."`
}

// API implements `GET /api/v1/export`. Like the error log download it needs
// the valid session cookie: the archive holds the operator's trades and
// decisions, and the download counts as operator activity
// (middleware.Heartbeat). The archive is built in a temporary file, so a
// failure while reading never leaves a half-written download and the period
// is not held in memory, then streamed back as a ZIP attachment. Nothing is
// sent anywhere else (FR-EXPORT-6).
func (h *Handler) API(ctx context.Context, in *Input) (*huma.StreamResponse, error) {
	if !middleware.Authenticated(ctx) {
		return nil, huma.Error403Forbidden("missing or invalid session cookie")
	}
	req, err := buildRequest(in)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	f, err := os.CreateTemp("", "pitha-export-*.zip")
	if err != nil {
		return nil, huma.Error500InternalServerError("export failed", err)
	}
	discard := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	if _, err := h.exporter.Export(ctx, f, req); err != nil {
		discard()
		if errors.Is(err, dataexport.ErrInvalidRequest) {
			return nil, huma.Error422UnprocessableEntity("invalid export request")
		}
		return nil, huma.Error500InternalServerError("export failed", err)
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		discard()
		return nil, huma.Error500InternalServerError("export failed", err)
	}

	filename := "pitha-export-" + h.now().UTC().Format("20060102-150405") + ".zip"
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer discard()
		hctx.SetHeader("Content-Type", "application/zip")
		hctx.SetHeader("Content-Disposition", `attachment; filename="`+filename+`"`)
		hctx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		if _, err := io.Copy(hctx.BodyWriter(), f); err != nil {
			slog.WarnContext(ctx, "export: send archive", "error", err)
		}
	}}, nil
}

// buildRequest turns the query into the service request: the UTC dates
// from..to (both included) become the half-open period [from, to+1 day).
func buildRequest(in *Input) (dataexport.Request, error) {
	from, err := time.Parse(dateLayout, in.From)
	if err != nil {
		return dataexport.Request{}, fmt.Errorf("from must be a YYYY-MM-DD date")
	}
	last, err := time.Parse(dateLayout, in.To)
	if err != nil {
		return dataexport.Request{}, fmt.Errorf("to must be a YYYY-MM-DD date")
	}
	if last.Before(from) {
		return dataexport.Request{}, fmt.Errorf("from must not be after to")
	}
	if last.Sub(from) >= MaxPeriodDays*24*time.Hour {
		return dataexport.Request{}, fmt.Errorf("the period spans more than %d days", MaxPeriodDays)
	}
	if len(in.Kinds) == 0 {
		return dataexport.Request{}, fmt.Errorf("select at least one kind")
	}
	kinds := make([]dataexport.Kind, len(in.Kinds))
	for i, k := range in.Kinds {
		kinds[i] = dataexport.Kind(k)
	}
	return dataexport.Request{
		Kinds:  kinds,
		Period: dataexport.Period{From: from, To: last.AddDate(0, 0, 1)},
		Format: dataexport.Format(in.Format),
	}, nil
}
