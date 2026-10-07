package universe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const (
	// jpxFetchTimeout bounds the whole download (the workbook is ~0.3 MB).
	jpxFetchTimeout = 60 * time.Second
	// maxJPXBytes caps the downloaded body: a response beyond it is not the
	// list (the real one is ~0.3 MB).
	maxJPXBytes = 16 << 20
)

// Repository is the instruments repository surface Importer uses
// (*market.InstrumentRepository).
type Repository interface {
	Upserter
	ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error)
}

// Importer fetches JPX's 東証上場銘柄一覧 and upserts its stocks into the
// instruments table on the operator's explicit request (the Scanner
// Dashboard's 銘柄マスタ未投入 panel, issue #508). It never runs by itself:
// nothing here is called at start-up.
type Importer struct {
	repo   Repository
	client *http.Client
	url    string

	mu sync.Mutex // one download/import at a time
}

// NewImporter returns an Importer reading JPXListURL via client (nil =
// http.DefaultClient). Tests point the URL elsewhere with WithURL.
func NewImporter(repo Repository, client *http.Client) *Importer {
	if client == nil {
		client = http.DefaultClient
	}
	return &Importer{repo: repo, client: client, url: JPXListURL}
}

// WithURL overrides the list URL (tests).
func (i *Importer) WithURL(url string) *Importer {
	i.url = url
	return i
}

// Empty reports whether no active stock is registered, i.e. nothing would
// be scanned: the only state in which the JPX import is offered.
func (i *Importer) Empty(ctx context.Context) (bool, error) {
	stocks, err := i.repo.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return false, fmt.Errorf("universe: list active stocks: %w", err)
	}
	return len(stocks) == 0, nil
}

// ImportJPX downloads and parses the list, then upserts every stock in one
// transaction, returning how many stocks the list held. A failure at any
// step (network, HTTP status, size, format, an invalid row) leaves the
// instruments table untouched. The Scheduler, PUSH feed and candidate
// refresh read the table on each cycle, so the next cycle uses the import
// without a restart (PUSH registration excepted: it happens when the
// subscription (re)connects, REST polling covers the stocks until then).
func (i *Importer) ImportJPX(ctx context.Context) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, jpxFetchTimeout)
	defer cancel()
	body, err := i.fetch(ctx)
	if err != nil {
		return 0, err
	}
	ins, err := ParseJPX(bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("%w: %w", domain.ErrJPXFormat, err)
	}
	if _, err := i.repo.Upsert(ctx, ins); err != nil {
		return 0, fmt.Errorf("%w: %w", domain.ErrJPXSave, err)
	}
	return len(ins), nil
}

func (i *Importer) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: JPXへのリクエストを作成できませんでした: %w", domain.ErrJPXConnect, err)
	}
	req.Header.Set("User-Agent", "pitha-trador")
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrJPXConnect, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: JPXが HTTP %d を返しました（一覧のURLが変更された可能性があります）", domain.ErrJPXFormat, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJPXBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: JPXの応答を読み取れませんでした: %w", domain.ErrJPXConnect, err)
	}
	if len(body) > maxJPXBytes {
		return nil, fmt.Errorf("%w: JPXの応答が想定より大きいため中止しました（一覧ではない可能性があります）", domain.ErrJPXFormat)
	}
	return body, nil
}
