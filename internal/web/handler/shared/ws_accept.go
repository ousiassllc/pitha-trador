package shared

import (
	"net/url"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// AcceptWebSocket upgrades c to a WebSocket.
//
// websocket.Accept only accepts an Origin whose host:port equals the
// request's Host. cmd/desktop's pages (Origin `http://wails.localhost`)
// reach the separate WebSocket listener as Host `wails.localhost:<port>`
// (middleware.WebSocketBase), which that check would refuse; so when a
// WebSocket base is configured its port-less hostname is accepted as an
// Origin too. middleware.HostGuard has already vetted the Origin's hostname
// against the allowed hosts. Without a base (cmd/server) pages share the
// socket's origin and the default check applies unchanged.
func AcceptWebSocket(c *gin.Context) (*websocket.Conn, error) {
	return websocket.Accept(c.Writer, c.Request, acceptOptions(middleware.WebSocketBaseURL(c.Request.Context())))
}

func acceptOptions(wsBase string) *websocket.AcceptOptions {
	u, err := url.Parse(wsBase)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return &websocket.AcceptOptions{OriginPatterns: []string{u.Hostname()}}
}
