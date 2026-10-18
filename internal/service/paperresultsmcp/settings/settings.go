// Package settings reads the local settings file of the read-only Paper
// results MCP (issue #783, docs/architecture/paper-results-mcp.md §3.4) and
// validates the address it may listen on (§3.1).
//
// The file lives beside the application database under the per-user config
// directory ("%AppData%\pitha-trador\paper-results-mcp.json" on Windows, the
// production target). It is never stored in the database: the switch and the
// key must not be reachable from anything the remote connector can read.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// FileName is the settings file name inside the pitha-trador config directory.
const FileName = "paper-results-mcp.json"

// DefaultListen is the default (and the only non-tailnet) listen address.
const DefaultListen = "127.0.0.1:48110"

// Settings is the content of paper-results-mcp.json. The zero value is not
// the default: use Default, which carries DefaultListen.
type Settings struct {
	// Enabled opens the listener. Default false: nothing is bound.
	Enabled bool `json:"enabled"`
	// Listen is the address to bind; see ValidateListen for the allowed values.
	Listen string `json:"listen"`
	// APIKey is the MCP's own bearer key. Required: empty means no listener.
	APIKey string `json:"api_key"`

	// The fields below are the optional Cloudflare Access origin check
	// (architecture §3.5). They default to off.
	VerifyAccessHeaders bool   `json:"verify_access_headers"`
	AccessClientID      string `json:"access_client_id"`
	AccessClientSecret  string `json:"access_client_secret"`
}

// Default returns the settings used when the file is absent: disabled.
func Default() Settings {
	return Settings{Listen: DefaultListen}
}

// DefaultPath returns the conventional settings file location,
// os.UserConfigDir()'s "pitha-trador/paper-results-mcp.json" - the same
// directory bootstrap.DefaultDBPath uses.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("paper-results-mcp: resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "pitha-trador", FileName), nil
}

// ErrInsecurePermissions reports a settings file readable by other users.
var ErrInsecurePermissions = errors.New("paper-results-mcp: settings file must be readable by the owner only (0600)")

// Load reads the settings file at path. A missing file is not an error: it
// returns Default (disabled). A file that cannot be read, is not valid JSON,
// has unknown fields or (on Unix) is readable by group/others returns
// Default and an error, so the caller does not listen. The error never
// carries the file's content (it holds the API key).
func Load(path string) (Settings, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return Default(), nil
	case err != nil:
		return Default(), fmt.Errorf("paper-results-mcp: stat settings file: %w", err)
	}
	// Windows has no meaningful Unix mode bits; its per-user %AppData% ACL
	// is the protection there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return Default(), ErrInsecurePermissions
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Default(), fmt.Errorf("paper-results-mcp: read settings file: %w", err)
	}
	s := Default()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		// json errors can quote offending input; report the kind only.
		return Default(), errors.New("paper-results-mcp: settings file is not valid JSON of the expected shape")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Default(), errors.New("paper-results-mcp: settings file has trailing content")
	}
	if s.Listen == "" {
		s.Listen = DefaultListen
	}
	return s, nil
}
