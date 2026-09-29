package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLog returns Gin middleware that writes one structured access-log
// record per request to the process-wide slog.Default logger once the
// handler chain has finished: method, path (never the query string, which
// may carry user input), status, latency_ms and the matched route
// pattern. Server errors (5xx) log at Error, client errors (4xx) at Warn,
// everything else at Info. `/static/...` is skipped (asset noise).
//
// Install it before Recovery so a recovered panic is logged with its final
// 500 status, and before Session so requests it rejects (403) are logged.
func RequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if isStaticPath(path) {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		slog.LogAttrs(c.Request.Context(), requestLogLevel(status), "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String("route", c.FullPath()),
			slog.Int("status", status),
			slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000))
	}
}

func requestLogLevel(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
