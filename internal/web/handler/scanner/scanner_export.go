package scanner

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// csvUTF8BOM makes Excel (the usual consumer on Windows) read the
// Japanese names/labels as UTF-8 instead of the ANSI code page.
const csvUTF8BOM = "\uFEFF"

// ScannerScanExportInput is the query of `GET /api/v1/scanner/scan/export`:
// the same filters as ScannerScanInput, without paging (the export holds
// every matching symbol).
type ScannerScanExportInput struct {
	Q      string `query:"q" maxLength:"64" doc:"Case-insensitive substring of symbol or name."`
	Status string `query:"status" enum:"passed,excluded,missing" doc:"Only symbols with this status."`
	Reason string `query:"reason" maxLength:"32" doc:"Only symbols out for this reason code."`
}

// ScannerScanExportOutput is the CSV attachment response.
type ScannerScanExportOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	RecordCount        int    `header:"X-Pitha-Record-Count" doc:"Number of data rows (excluding the header)."`
	Body               []byte
}

// scanExportHeader is the CSV's first row.
var scanExportHeader = []string{"symbol", "name", "market", "status", "reason_codes", "reason_labels", "scout"}

// APIScannerScanExport implements `GET /api/v1/scanner/scan/export`: the
// latest scan cycle's per-symbol verdicts, filtered like the scan list but
// unpaged, as a UTF-8 (BOM) CSV attachment. Before the first cycle it is a
// header-only CSV, mirroring `GET /api/v1/scanner/scan`'s empty response.
func (h *ScannerHandler) APIScannerScanExport(ctx context.Context, in *ScannerScanExportInput) (*ScannerScanExportOutput, error) {
	query := domain.ScanQuery{Q: in.Q, Status: domain.ScanStatus(in.Status)}
	if in.Reason != "" {
		r, ok := domain.ScreenReasonFromCode(in.Reason)
		if !ok {
			return nil, huma.Error400BadRequest("unknown reason code " + strconv.Quote(in.Reason))
		}
		query.Reason = &r
	}
	cycle, ok, err := h.scan(ctx)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(csvUTF8BOM)
	w := csv.NewWriter(&buf)
	_ = w.Write(scanExportHeader)
	var symbols []domain.ScanSymbol
	if ok {
		symbols = cycle.Matches(query)
	}
	for _, s := range symbols {
		var codes, labels []string
		for _, r := range s.Reasons.List() {
			codes, labels = append(codes, r.Code()), append(labels, r.Label())
		}
		scout := ""
		if o, has := cycle.Scout[s.Symbol]; has {
			scout = string(o)
		}
		_ = w.Write([]string{
			csvCell(s.Symbol), csvCell(s.Name), csvCell(s.Market), string(s.Status()),
			strings.Join(codes, ";"), strings.Join(labels, ";"), scout,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, huma.Error500InternalServerError("write scan csv failed", err)
	}

	name := "pitha-scan.csv"
	if ok {
		name = "pitha-scan-" + cycle.FinishedAt.UTC().Format("20060102-150405") + ".csv"
	}
	return &ScannerScanExportOutput{
		ContentType:        "text/csv; charset=utf-8",
		ContentDisposition: `attachment; filename="` + name + `"`,
		RecordCount:        len(symbols),
		Body:               buf.Bytes(),
	}, nil
}

// csvCell neutralises spreadsheet formula injection: a cell starting with
// = + - @ (or a tab/CR) is executed as a formula by Excel, and the name
// comes from an imported master file.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
