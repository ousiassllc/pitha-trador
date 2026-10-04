package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/coder/websocket"
)

// DefaultPushURL is kabuステーションが提供するローカルPUSH WebSocketの
// 既定エンドポイント (docs/architecture/overview.md §5, §2 技術スタック
// "リアルタイムPush")。
const DefaultPushURL = "ws://localhost:18080/kabusapi/websocket"

// PushClient subscribes to kabuステーションAPIのPUSH WebSocketを購読し、
// RegisterSymbolsで登録した銘柄の価格・板情報更新をリアルタイムで受信す
// る。これによりREST側の60秒ポーリングに依存せず、Feature Engineが各サ
// イクル開始時点の最新スナップショットを参照できる(overview.md §5)。
type PushClient struct {
	url    string
	status *StatusTracker
}

// NewPushClient returns a PushClient that dials url and reports message
// freshness to status. Pass the same StatusTracker used by a Client to
// combine REST and PUSH freshness into one view.
func NewPushClient(url string, status *StatusTracker) *PushClient {
	if status == nil {
		status = NewStatusTracker()
	}
	return &PushClient{url: url, status: status}
}

// Handler is invoked once per PUSH message (each message reports one
// registered symbol's updated Board-shaped fields).
type Handler func(Board)

// Run dials the PUSH WebSocket and delivers messages to handler until ctx
// is done or a connection error occurs, in which case it returns the
// error. Callers wanting automatic reconnection should call Run again
// (e.g. in a retry loop with backoff); each reconnect attempt marks
// affected symbols stale only once handler stops receiving updates for
// them, which downstream StatusTracker consumers already account for via
// the REST poll path's own MarkStale on failure.
func (p *PushClient) Run(ctx context.Context, handler Handler) error {
	// coder/websocket: the handshake response body never needs closing.
	conn, _, err := websocket.Dial(ctx, p.url, nil) //nolint:bodyclose
	if err != nil {
		return fmt.Errorf("marketdata: dial push websocket: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("marketdata: read push message: %w", err)
		}

		var board Board
		if err := json.Unmarshal(data, &board); err != nil {
			return fmt.Errorf("marketdata: decode push message: %w", err)
		}

		p.status.MarkFresh(board.Symbol, time.Now().UTC())
		handler(board)
	}
}
