package universe_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/universe"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

var jpxHeader = []string{"日付", "コード", "銘柄名", "市場・商品区分", "33業種コード", "33業種区分", "17業種コード", "17業種区分", "規模コード", "規模区分"}

// jpxRow builds a data_j.xlsx row; the unused columns stay filled so a
// parser that read by position instead of header would misread.
func jpxRow(code, name, segment, sector string) []string {
	return []string{"20260930", code, name, segment, "1050", sector, "9", "輸送用機器", "1", "TOPIX Core30"}
}

// jpxWorkbook returns an xlsx holding header and rows on its first sheet.
func jpxWorkbook(t *testing.T, header []string, rows ...[]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	all := append([][]string{header}, rows...)
	for r, row := range all {
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bulkStocks returns n distinct プライム rows (numeric codes 2000..), enough
// to clear the importer's truncated-list guard.
func bulkStocks(n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = jpxRow(fmt.Sprint(2000+i), fmt.Sprintf("銘柄%d", i), "プライム（内国株式）", "電気機器")
	}
	return rows
}

func TestParseJPX_MapsStocksAndSkipsNonStockSegments(t *testing.T) {
	rows := append(bulkStocks(1000),
		jpxRow("130A", "Ｔ銘柄", "グロース（内国株式）", "-"),
		jpxRow("9999", "外国スタンダード", "スタンダード（外国株式）", "サービス業"),
		jpxRow("1306", "ETF", "ETF・ETN", "-"),
		jpxRow("8951", "REIT", "REIT・ベンチャーファンド・カントリーファンド・インフラファンド", "-"),
		jpxRow("1400", "PRO", "PRO Market", "その他金融業"),
		jpxRow("7000", "出資", "出資証券", "-"),
	)
	got, err := universe.ParseJPX(bytes.NewReader(jpxWorkbook(t, jpxHeader, rows...)))
	if err != nil {
		t.Fatalf("ParseJPX: %v", err)
	}
	if len(got) != 1002 {
		t.Fatalf("got %d instruments, want 1002 (1000 bulk + 130A + 9999; ETF/REIT/PRO/出資証券 skipped)", len(got))
	}
	byCode := map[string]domain.Instrument{}
	for _, in := range got {
		byCode[in.Symbol] = in
		if in.Kind != domain.InstrumentKindStock || !in.IsActive {
			t.Fatalf("%s: kind %q active %v, want active stock", in.Symbol, in.Kind, in.IsActive)
		}
	}
	for _, skipped := range []string{"1306", "8951", "1400", "7000"} {
		if _, ok := byCode[skipped]; ok {
			t.Errorf("%s (non-stock segment) was imported", skipped)
		}
	}
	growth := byCode["130A"]
	if growth.Market != "TSE Growth" || growth.Name != "Ｔ銘柄" || growth.Sector != nil {
		t.Errorf("130A = %+v, want TSE Growth, sector nil for \"-\"", growth)
	}
	foreign := byCode["9999"]
	if foreign.Market != "TSE Standard" || foreign.Sector == nil || *foreign.Sector != "サービス業" {
		t.Errorf("9999 = %+v, want TSE Standard / サービス業", foreign)
	}
	if byCode["2000"].Market != "TSE Prime" {
		t.Errorf("2000 market = %q, want TSE Prime", byCode["2000"].Market)
	}
}

func TestParseJPX_ReadsColumnsByHeaderName(t *testing.T) {
	header := []string{"33業種区分", "市場・商品区分", "銘柄名", "コード"}
	rows := make([][]string, 1000)
	for i := range rows {
		rows[i] = []string{"電気機器", "プライム（内国株式）", fmt.Sprintf("銘柄%d", i), fmt.Sprint(2000 + i)}
	}
	got, err := universe.ParseJPX(bytes.NewReader(jpxWorkbook(t, header, rows...)))
	if err != nil {
		t.Fatalf("ParseJPX: %v", err)
	}
	if got[0].Symbol != "2000" || got[0].Name != "銘柄0" || *got[0].Sector != "電気機器" {
		t.Errorf("first = %+v", got[0])
	}
}

func TestParseJPX_RejectsWholeFileOnAnyBadInput(t *testing.T) {
	good := bulkStocks(1000)
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"not an xlsx", []byte("symbol,name\n1,2\n"), "xlsx"},
		{"missing header column", jpxWorkbook(t, []string{"コード", "銘柄名", "市場・商品区分"}, good...), "33業種区分"},
		{"unknown segment", jpxWorkbook(t, jpxHeader, append(good, jpxRow("1111", "新区分", "新市場（内国株式）", "-"))...), "row 1002"},
		{"invalid symbol", jpxWorkbook(t, jpxHeader, append(good, jpxRow("7203.T", "トヨタ", "プライム（内国株式）", "輸送用機器"))...), "row 1002"},
		{"duplicate code", jpxWorkbook(t, jpxHeader, append(good, jpxRow("2000", "重複", "プライム（内国株式）", "-"))...), "duplicate code"},
		{"empty name", jpxWorkbook(t, jpxHeader, append(good, jpxRow("1111", "", "プライム（内国株式）", "-"))...), "name is empty"},
		{"truncated list", jpxWorkbook(t, jpxHeader, good[:10]...), "truncated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := universe.ParseJPX(bytes.NewReader(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if got != nil {
				t.Errorf("returned %d instruments alongside the error", len(got))
			}
		})
	}
}

// recordingRepo is an in-memory universe.Repository.
type recordingRepo struct {
	stocks   []domain.Instrument
	upserted [][]domain.Instrument
	err      error
}

func (r *recordingRepo) Upsert(_ context.Context, ins []domain.Instrument) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	r.upserted = append(r.upserted, ins)
	r.stocks = append(r.stocks, ins...)
	return len(ins), nil
}

func (r *recordingRepo) ListActiveByKind(context.Context, string) ([]domain.Instrument, error) {
	return r.stocks, nil
}

func serve(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestImporter_ImportJPX_UpsertsStocksFromTheList(t *testing.T) {
	srv := serve(t, http.StatusOK, jpxWorkbook(t, jpxHeader, bulkStocks(1200)...))
	repo := &recordingRepo{}
	imp := universe.NewImporter(repo, srv.Client()).WithURL(srv.URL)

	empty, err := imp.Empty(context.Background())
	if err != nil || !empty {
		t.Fatalf("Empty before import = %v, %v; want true", empty, err)
	}
	n, err := imp.ImportJPX(context.Background())
	if err != nil {
		t.Fatalf("ImportJPX: %v", err)
	}
	if n != 1200 || len(repo.upserted) != 1 || len(repo.upserted[0]) != 1200 {
		t.Fatalf("imported %d, upsert calls %d: want one call with 1200", n, len(repo.upserted))
	}
	empty, err = imp.Empty(context.Background())
	if err != nil || empty {
		t.Errorf("Empty after import = %v, %v; want false", empty, err)
	}
}

func TestImporter_ImportJPX_FailureLeavesTheMasterUntouched(t *testing.T) {
	good := jpxWorkbook(t, jpxHeader, bulkStocks(1200)...)
	tests := []struct {
		name   string
		status int
		body   []byte
		want   string
	}{
		{"URL moved", http.StatusNotFound, []byte("not found"), "HTTP 404"},
		{"server error", http.StatusInternalServerError, nil, "HTTP 500"},
		{"HTML instead of the workbook", http.StatusOK, []byte("<html>maintenance</html>"), "形式"},
		{"layout changed", http.StatusOK, jpxWorkbook(t, []string{"code", "name"}, []string{"1", "a"}), "形式"},
		{"oversized body", http.StatusOK, bytes.Repeat([]byte("x"), 17<<20), "大きい"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := serve(t, tc.status, tc.body)
			repo := &recordingRepo{}
			_, err := universe.NewImporter(repo, srv.Client()).WithURL(srv.URL).ImportJPX(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.Is(err, universe.ErrJPXFormat) {
				t.Fatalf("err = %v, want ErrJPXFormat containing %q", err, tc.want)
			}
			if len(repo.upserted) != 0 {
				t.Errorf("master was written despite the failure: %d upserts", len(repo.upserted))
			}
		})
	}

	t.Run("network down", func(t *testing.T) {
		srv := serve(t, http.StatusOK, good)
		url := srv.URL
		srv.Close()
		repo := &recordingRepo{}
		_, err := universe.NewImporter(repo, nil).WithURL(url).ImportJPX(context.Background())
		if err == nil || !strings.Contains(err.Error(), "接続できません") || !errors.Is(err, universe.ErrJPXConnect) {
			t.Fatalf("err = %v, want ErrJPXConnect", err)
		}
		if len(repo.upserted) != 0 {
			t.Error("master was written despite the failure")
		}
	})
}

func TestImporter_ImportJPX_ReportsRepositoryFailure(t *testing.T) {
	srv := serve(t, http.StatusOK, jpxWorkbook(t, jpxHeader, bulkStocks(1200)...))
	repo := &recordingRepo{err: fmt.Errorf("disk full")}
	_, err := universe.NewImporter(repo, srv.Client()).WithURL(srv.URL).ImportJPX(context.Background())
	if err == nil || !strings.Contains(err.Error(), "disk full") || !errors.Is(err, universe.ErrJPXSave) {
		t.Fatalf("err = %v, want ErrJPXSave wrapping the repository error", err)
	}
}
