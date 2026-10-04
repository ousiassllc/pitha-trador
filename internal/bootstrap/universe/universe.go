// Package universe loads the scan-universe master (the instruments table)
// from an operator-supplied CSV file.
//
// kabuステーションAPI has no endpoint that lists the TSE-listed stocks, so
// the master cannot be fetched from the broker (functional.md §7 MVP完了条件,
// environment/setup.md「銘柄マスタの投入」). The operator supplies it as a
// CSV (typically converted from JPX's 東証上場銘柄一覧) and SyncFile upserts
// it at start-up.
package universe

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// maxSymbolLen is the instruments.symbol column width (varchar(10)).
const maxSymbolLen = 10

// utf8BOM is stripped from the file start: Excel's "CSV UTF-8" export adds it.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Upserter is the instruments repository surface SyncFile uses
// (*market.InstrumentRepository).
type Upserter interface {
	Upsert(ctx context.Context, ins []domain.Instrument) (int, error)
}

// SyncFile parses the CSV at path (see Parse) and upserts it into the
// instruments table, returning the number of inserted or changed rows. The
// sync is idempotent: re-running with an unchanged file changes nothing and
// never alters an existing instrument's is_active flag.
func SyncFile(ctx context.Context, repo Upserter, path string) (instruments, changed int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("universe: read %s: %w", path, err)
	}
	ins, err := Parse(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("universe: parse %s: %w", path, err)
	}
	changed, err = repo.Upsert(ctx, ins)
	if err != nil {
		return 0, 0, fmt.Errorf("universe: sync %s: %w", path, err)
	}
	return len(ins), changed, nil
}

// Parse reads the universe CSV: a header row naming the columns, then one
// row per instrument. Columns (order free, names case-insensitive):
//
//	symbol  required  証券コード/指数コード (≤10文字, ファイル内で一意)
//	name    required  銘柄名
//	market  required  市場区分 (例: TSE Prime)
//	sector  optional  業種 (kind=sector_index では必須)
//	kind    optional  stock (既定) / market_index / sector_index
//
// Any invalid row fails the whole file (error names the 1-based line), so a
// half-valid master is never applied.
func Parse(r io.Reader) ([]domain.Instrument, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	cr := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("empty file: header row symbol,name,market[,sector,kind] is required")
	}
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	cols := map[string]int{}
	for i, h := range header {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, required := range []string{"symbol", "name", "market"} {
		if _, ok := cols[required]; !ok {
			return nil, fmt.Errorf("header: missing required column %q", required)
		}
	}

	var out []domain.Instrument
	seen := map[string]int{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := cr.FieldPos(0)
		in, err := parseRow(rec, cols)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if first, dup := seen[in.Symbol]; dup {
			return nil, fmt.Errorf("line %d: duplicate symbol %q (first on line %d)", line, in.Symbol, first)
		}
		seen[in.Symbol] = line
		out = append(out, in)
	}
	if len(out) == 0 {
		return nil, errors.New("no instrument rows")
	}
	return out, nil
}

func parseRow(rec []string, cols map[string]int) (domain.Instrument, error) {
	field := func(name string) string {
		if i, ok := cols[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	in := domain.Instrument{
		Symbol: field("symbol"), Name: field("name"), Market: field("market"),
		Kind: field("kind"), IsActive: true,
	}
	if sector := field("sector"); sector != "" {
		in.Sector = &sector
	}
	switch {
	case in.Symbol == "":
		return in, errors.New("symbol is empty")
	case len(in.Symbol) > maxSymbolLen:
		return in, fmt.Errorf("symbol %q exceeds %d characters", in.Symbol, maxSymbolLen)
	case in.Name == "":
		return in, fmt.Errorf("symbol %q: name is empty", in.Symbol)
	case in.Market == "":
		return in, fmt.Errorf("symbol %q: market is empty", in.Symbol)
	}
	switch in.Kind {
	case "":
		in.Kind = domain.InstrumentKindStock
	case domain.InstrumentKindStock, domain.InstrumentKindMarketIndex:
	case domain.InstrumentKindSectorIndex:
		if in.Sector == nil {
			return in, fmt.Errorf("symbol %q: sector is required for kind %s", in.Symbol, in.Kind)
		}
	default:
		return in, fmt.Errorf("symbol %q: unknown kind %q (want stock, market_index or sector_index)", in.Symbol, in.Kind)
	}
	return in, nil
}
