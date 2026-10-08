package event

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// ErrKeepAliveTimeout means no notification (not even KP) arrived for 15
// seconds.
var ErrKeepAliveTimeout = errors.New("tachibana: EVENT keep-alive timeout")

// StreamError is an ST notification: the broker reports an error and closes.
type StreamError struct {
	Errno int
	Text  string
}

func (e *StreamError) Error() string {
	return fmt.Sprintf("tachibana: EVENT error status p_errno=%d: %s", e.Errno, e.Text)
}

type readResult struct {
	data []byte
	err  error
}

// query is the connection's URL query: 時価配信あり (p_rid=22) with one row
// per symbol, resuming after the last event number seen.
func query(symbols []string, eno int64) string {
	rows := make([]string, len(symbols))
	markets := make([]string, len(symbols))
	for i := range symbols {
		rows[i] = strconv.Itoa(i + 1)
		markets[i] = "00"
	}
	return "?p_rid=22&p_board_no=1000&p_gyou_no=" + strings.Join(rows, ",") +
		"&p_issue_code=" + strings.Join(symbols, ",") + "&p_mkt_code=" + strings.Join(markets, ",") +
		"&p_eno=" + strconv.FormatInt(eno, 10) + "&p_evt_cmd=ST,KP,FD,EC,SS,US"
}

// serve runs one connection. It returns nil when the connection was ended on
// purpose (new login, planned swap), the reason otherwise; lived is how long a
// connected session lasted. Errors never carry the virtual URL.
func (f *Feed) serve(ctx context.Context, sess tachibana.EventSession, changed <-chan struct{}, symbols []string, reason string) (lived time.Duration, err error) {
	f.account(reason, len(symbols))
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	conn, resp, err := websocket.Dial(dialCtx, sess.URL+query(symbols, f.eventNo()), &websocket.DialOptions{HTTPClient: f.hc})
	cancel()
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return 0, f.client.Sanitize(err)
	}
	conn.SetReadLimit(maxFrameBytes)
	f.setSubscribed(symbols)
	started := f.clock.Now()
	defer func() { lived = f.clock.Now().Sub(started) }()

	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	msgs := make(chan readResult, 1)
	go func() {
		for {
			_, data, err := conn.Read(rctx)
			select {
			case msgs <- readResult{data, err}:
			case <-rctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	// Wait for the close handshake to finish before anything reconnects: the
	// broker finishes the old connection's cleanup before accepting a new one.
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	watchdog := f.clock.NewTimer(kpTimeout)
	defer func() { watchdog.Stop() }()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-changed:
			slog.Info("tachibana: new login; reconnecting EVENT to the new virtual URL")
			return 0, nil
		case <-f.wake:
			if f.swapAllowed() {
				slog.Info("tachibana: EVENT watch list changed; reconnecting once with the new symbols", "new_symbols", len(f.pendingAdds()))
				return 0, nil
			}
		case <-watchdog.C():
			return 0, ErrKeepAliveTimeout
		case r := <-msgs:
			if r.err != nil {
				return 0, f.client.Sanitize(r.err)
			}
			watchdog.Stop()
			watchdog = f.clock.NewTimer(kpTimeout)
			if err := f.handle(ctx, sess, symbols, string(r.data)); err != nil {
				return 0, err
			}
		}
	}
}

// handle processes one notification. A non-nil result ends the connection.
func (f *Feed) handle(ctx context.Context, sess tachibana.EventSession, symbols []string, msg string) error {
	n, err := Parse(msg)
	if err != nil {
		slog.Warn("tachibana: ignoring an unreadable EVENT message")
		return nil
	}
	now := f.clock.Now()
	switch n.Cmd {
	case "FD":
		for row, items := range n.Rows() {
			if row >= 1 && row <= len(symbols) {
				f.merge(symbols[row-1], items, now)
			}
		}
	case "ST":
		errno, _ := strconv.Atoi(n.Fields["p_errno"])
		text := n.Fields["p_err"]
		if errno == tachibana.ErrnoSessionExpired {
			// Only a lost session justifies a new login; everything else
			// reconnects to the same virtual URL.
			f.client.ReportSessionLost(ctx, sess.Gen)
		}
		return &StreamError{Errno: errno, Text: text}
	case "EC":
		f.noteEventNo(n.eventNo())
		f.rec.order(n, now)
	case "SS":
		f.noteEventNo(n.eventNo())
		f.rec.system(n, now)
	case "US":
		f.noteEventNo(n.eventNo())
		f.rec.operation(n)
	}
	return nil
}
