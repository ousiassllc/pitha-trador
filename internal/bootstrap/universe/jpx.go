package universe

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const (
	// JPXListURL is the 東証上場銘柄一覧 workbook JPX publishes (monthly,
	// replaced in place). It is only fetched after the operator consents on
	// the Scanner Dashboard (Importer.ImportJPX); JPXListPageURL is the page
	// that links it and carries JPX's disclaimer.
	JPXListURL     = "https://www.jpx.co.jp/markets/statistics-equities/misc/tvdivq0000001vg2-att/data_j.xlsx"
	JPXListPageURL = "https://www.jpx.co.jp/markets/statistics-equities/misc/01.html"

	// minJPXStocks rejects a truncated or restructured list: the real one
	// holds ~3,700 stocks, so far fewer means the content is not what the
	// importer was written for.
	minJPXStocks = 1000

	// maxJPXUnzipBytes caps the decompressed size excelize may inflate
	// (zip-bomb guard; the real workbook is ~0.3 MB compressed).
	maxJPXUnzipBytes = 64 << 20
)

// Header names of data_j.xlsx's columns, looked up by name so a reordering
// is harmless and a rename fails loudly.
const (
	jpxColCode    = "コード"
	jpxColName    = "銘柄名"
	jpxColSegment = "市場・商品区分"
	jpxColSector  = "33業種区分"
)

// jpxStockMarkets maps the 市場・商品区分 of the segments holding ordinary
// stocks (内国・外国株式) to instruments.market.
var jpxStockMarkets = map[string]string{
	"プライム（内国株式）":   "TSE Prime",
	"プライム（外国株式）":   "TSE Prime",
	"スタンダード（内国株式）": "TSE Standard",
	"スタンダード（外国株式）": "TSE Standard",
	"グロース（内国株式）":   "TSE Growth",
	"グロース（外国株式）":   "TSE Growth",
}

// jpxExcludedSegments are the 市場・商品区分 that are listed but not
// scan/trade targets for this app: ETF/ETN, REIT and fund units, PRO Market
// (professional investors only) and 出資証券. Any other unknown segment
// rejects the file instead of being silently dropped (a new JPX segment
// should be decided on, not guessed).
var jpxExcludedSegments = map[string]bool{
	"ETF・ETN": true,
	"REIT・ベンチャーファンド・カントリーファンド・インフラファンド": true,
	"PRO Market": true,
	"出資証券":       true,
}

// ParseJPX reads JPX's 東証上場銘柄一覧 workbook (data_j.xlsx, first sheet)
// into stock instruments: コード→symbol, 銘柄名→name, 市場・商品区分→market
// (jpxStockMarkets), 33業種区分→sector ("-" = none). ETF・ETN, REIT, PRO
// Market and 出資証券 rows are skipped; index rows (TOPIX etc.) are not part
// of the list. Like Parse, any invalid row (bad symbol, unknown segment,
// duplicate code) fails the whole file, naming the 1-based sheet row.
func ParseJPX(r io.Reader) ([]domain.Instrument, error) {
	f, err := excelize.OpenReader(r, excelize.Options{UnzipSizeLimit: maxJPXUnzipBytes, UnzipXMLSizeLimit: maxJPXUnzipBytes})
	if err != nil {
		return nil, fmt.Errorf("not a readable xlsx workbook: %w", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("workbook has no sheet")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheets[0], err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("sheet %q is empty", sheets[0])
	}
	cols := map[string]int{}
	for i, h := range rows[0] {
		cols[strings.TrimSpace(h)] = i
	}
	for _, required := range []string{jpxColCode, jpxColName, jpxColSegment, jpxColSector} {
		if _, ok := cols[required]; !ok {
			return nil, fmt.Errorf("header: missing required column %q (JPX changed the list layout?)", required)
		}
	}

	var out []domain.Instrument
	seen := map[string]int{}
	for i, rec := range rows[1:] {
		line := i + 2
		cell := func(name string) string {
			if c := cols[name]; c < len(rec) {
				return strings.TrimSpace(rec[c])
			}
			return ""
		}
		segment := cell(jpxColSegment)
		if segment == "" && cell(jpxColCode) == "" {
			continue // blank trailing row
		}
		if jpxExcludedSegments[segment] {
			continue
		}
		market, ok := jpxStockMarkets[segment]
		if !ok {
			return nil, fmt.Errorf("row %d: unknown %s %q (JPX added or renamed a segment)", line, jpxColSegment, segment)
		}
		in := domain.Instrument{
			Symbol: cell(jpxColCode), Name: cell(jpxColName), Market: market,
			Kind: domain.InstrumentKindStock, IsActive: true,
		}
		if sector := cell(jpxColSector); sector != "" && sector != "-" {
			in.Sector = &sector
		}
		if err := checkIdentity(in); err != nil {
			return nil, fmt.Errorf("row %d: %w", line, err)
		}
		if first, dup := seen[in.Symbol]; dup {
			return nil, fmt.Errorf("row %d: duplicate code %q (first on row %d)", line, in.Symbol, first)
		}
		seen[in.Symbol] = line
		out = append(out, in)
	}
	if len(out) < minJPXStocks {
		return nil, fmt.Errorf("only %d stock rows (want at least %d): the list is truncated or its layout changed", len(out), minJPXStocks)
	}
	return out, nil
}
