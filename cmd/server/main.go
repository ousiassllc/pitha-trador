// Command server runs the pitha-trador backend headlessly (no Wails, no
// native window) by listening directly with net/http. It shares the exact
// same Gin engine as cmd/desktop via internal/router, which has no
// dependency on Wails (docs/architecture/overview.md §9).
//
// This entrypoint is intended for environments where the Wails/WebView2
// native shell cannot run (CI, headless test environments, ...).
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

const defaultAddr = ":8080"

func main() {
	addr := os.Getenv("PITHA_SERVER_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	engine := router.New()

	srv := &http.Server{
		Addr:              addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("pitha-trador server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
