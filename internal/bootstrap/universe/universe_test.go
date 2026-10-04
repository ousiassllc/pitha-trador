package universe_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/universe"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func TestParse_ReadsColumnsByHeaderAndDefaultsKindToStock(t *testing.T) {
	csv := "\uFEFFName, Symbol ,market,kind,sector\n" +
		"トヨタ自動車,7203,TSE Prime,,輸送用機器\n" +
		"TOPIX,101,INDEX,market_index,\n" +
		"電気機器指数,1020,INDEX,sector_index,電気機器\n"

	got, err := universe.Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Parse returned %d rows, want 3: %+v", len(got), got)
	}
	toyota := got[0]
	if toyota.Symbol != "7203" || toyota.Name != "トヨタ自動車" || toyota.Kind != domain.InstrumentKindStock ||
		toyota.Sector == nil || *toyota.Sector != "輸送用機器" || !toyota.IsActive {
		t.Fatalf("stock row = %+v", toyota)
	}
	if got[1].Kind != domain.InstrumentKindMarketIndex || got[1].Sector != nil {
		t.Fatalf("market_index row = %+v, want kind=market_index and nil sector", got[1])
	}
	if got[2].Kind != domain.InstrumentKindSectorIndex || *got[2].Sector != "電気機器" {
		t.Fatalf("sector_index row = %+v", got[2])
	}
}

func TestParse_RejectsInvalidFilesWithTheOffendingLine(t *testing.T) {
	cases := map[string]struct{ csv, want string }{
		"empty file":             {"", "empty file"},
		"header only":            {"symbol,name,market\n", "no instrument rows"},
		"missing column":         {"symbol,name\n7203,トヨタ\n", `missing required column "market"`},
		"empty symbol":           {"symbol,name,market\n,トヨタ,TSE Prime\n", "line 2: symbol is empty"},
		"symbol too long":        {"symbol,name,market\n12345678901,x,TSE Prime\n", "line 2"},
		"empty name":             {"symbol,name,market\n7203,,TSE Prime\n", "line 2"},
		"empty market":           {"symbol,name,market\n7203,トヨタ,\n", "line 2"},
		"unknown kind":           {"symbol,name,market,kind\n7203,トヨタ,TSE Prime,etf\n", `unknown kind "etf"`},
		"sector_index no sector": {"symbol,name,market,kind\n1020,指数,INDEX,sector_index\n", "sector is required"},
		"duplicate symbol":       {"symbol,name,market\n7203,a,TSE Prime\n6758,b,TSE Prime\n7203,c,TSE Prime\n", "line 4: duplicate symbol"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := universe.Parse(strings.NewReader(tc.csv))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A Shift_JIS/CP932 file keeps ASCII headers/symbols/kinds valid, so only an
// explicit UTF-8 check stops the mojibake name/sector from being stored.
func TestParse_RejectsNonUTF8WithLineAndByte(t *testing.T) {
	// "トヨタ自動車" / "輸送用機器" in Shift_JIS.
	const toyotaSJIS = "\x83\x67\x83\x88\x83\x5e\x8e\xa9\x93\xae\x8e\xd4"
	const sectorSJIS = "\x97\xa6\x91\x97\x97\x70\x8b\x40\x8a\xed"
	csv := "symbol,name,market,sector,kind\n" +
		"101,TOPIX,INDEX,,market_index\n" +
		"7203," + toyotaSJIS + ",TSE Prime," + sectorSJIS + ",stock\n"

	got, err := universe.Parse(strings.NewReader(csv))
	if err == nil || !strings.Contains(err.Error(), "line 3, byte 6: file is not valid UTF-8") {
		t.Fatalf("Parse = %+v, %v; want a line 3, byte 6 UTF-8 error", got, err)
	}
	if got != nil {
		t.Fatalf("Parse returned rows %+v alongside the error; the whole file must be rejected", got)
	}
	if _, err := universe.Parse(strings.NewReader("\xEF\xBB\xBF" + csv)); err == nil {
		t.Fatal("Parse accepted a BOM-prefixed Shift_JIS file")
	}
}

func TestParse_AcceptsUTF8WithAndWithoutBOM(t *testing.T) {
	const body = "symbol,name,market\n7203,トヨタ自動車,TSE Prime\n"
	for name, in := range map[string]string{"no BOM": body, "BOM": "\xEF\xBB\xBF" + body} {
		t.Run(name, func(t *testing.T) {
			got, err := universe.Parse(strings.NewReader(in))
			if err != nil || len(got) != 1 || got[0].Name != "トヨタ自動車" {
				t.Fatalf("Parse = %+v, %v", got, err)
			}
		})
	}
}

func TestSyncFile_PopulatesCleanDBIdempotentlyAndKeepsIsActive(t *testing.T) {
	ctx := context.Background()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := market.NewInstrumentRepository(conn)

	path := filepath.Join(t.TempDir(), "universe.csv")
	body := "symbol,name,market,sector,kind\n7203,トヨタ自動車,TSE Prime,輸送用機器,stock\n6758,ソニーグループ,TSE Prime,電気機器,\n101,TOPIX,INDEX,,market_index\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	n, changed, err := universe.SyncFile(ctx, repo, path)
	if err != nil || n != 3 || changed != 3 {
		t.Fatalf("first SyncFile = (%d, %d, %v), want (3, 3, nil)", n, changed, err)
	}
	stocks, _ := repo.ListActiveByKind(ctx, domain.InstrumentKindStock)
	indexes, _ := repo.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex)
	if len(stocks) != 2 || len(indexes) != 1 {
		t.Fatalf("after sync: %d stocks, %d market indexes, want 2 and 1", len(stocks), len(indexes))
	}

	sony, _ := repo.GetBySymbol(ctx, "6758")
	sony.IsActive = false
	if _, err := repo.Update(ctx, sony); err != nil {
		t.Fatalf("Update: %v", err)
	}
	n, changed, err = universe.SyncFile(ctx, repo, path)
	if err != nil || n != 3 || changed != 0 {
		t.Fatalf("second SyncFile = (%d, %d, %v), want (3, 0, nil)", n, changed, err)
	}
	if got, _ := repo.GetBySymbol(ctx, "6758"); got.IsActive {
		t.Fatalf("re-sync re-activated an excluded instrument: %+v", got)
	}
}

func TestSyncFile_InvalidFileLeavesDBUntouched(t *testing.T) {
	ctx := context.Background()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := market.NewInstrumentRepository(conn)

	path := filepath.Join(t.TempDir(), "universe.csv")
	if err := os.WriteFile(path, []byte("symbol,name,market,kind\n7203,トヨタ,TSE Prime,\n6758,ソニー,TSE Prime,etf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := universe.SyncFile(ctx, repo, path); err == nil {
		t.Fatal("SyncFile accepted an invalid file")
	}
	if all, _ := repo.ListActive(ctx); len(all) != 0 {
		t.Fatalf("invalid file inserted %d rows, want 0", len(all))
	}
}
