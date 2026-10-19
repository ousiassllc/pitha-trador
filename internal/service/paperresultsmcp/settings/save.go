package settings

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// KeyPrefix starts every generated API key, so a pasted key is
// recognisable as this MCP's (and secret scanners can match it).
const KeyPrefix = "pitha-mcp-"

// apiKeyEntropyBytes is the random part's size: 256 bits.
const apiKeyEntropyBytes = 32

// GenerateAPIKey returns a new random API key: KeyPrefix followed by 32
// bytes from crypto/rand in unpadded URL-safe base64 (header- and
// copy-paste-safe). The returned error never carries a key.
func GenerateAPIKey() (string, error) {
	raw := make([]byte, apiKeyEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("paper-results-mcp: generate API key: no random source")
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// Save writes s to path atomically: the JSON goes to a temporary file in the
// same directory (mode 0600), is flushed to disk and then renamed over path,
// so a reader - the MCP process loading it at start - sees either the old
// or the new file, never a partial one. The parent directory is created
// (0700) when missing. s is written as-is: Load accepts the result, and
// fields the caller did not change (the Cloudflare Access options) survive
// when it starts from a Load of the current file. Errors never carry the
// file's content (it holds the API key).
func Save(path string, s Settings) (err error) {
	body, err := json.MarshalIndent(s, "", "  ") //nolint:gosec // G117: writing the API key into its own 0600 settings file is this function's purpose
	if err != nil {
		return errors.New("paper-results-mcp: encode settings")
	}
	body = append(body, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("paper-results-mcp: create settings directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, FileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("paper-results-mcp: create temporary settings file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	// CreateTemp already uses 0600 on Unix; chmod makes that independent of
	// the platform default.
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("paper-results-mcp: restrict temporary settings file: %w", err)
	}
	if _, err = tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("paper-results-mcp: write settings file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("paper-results-mcp: sync settings file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("paper-results-mcp: close settings file: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("paper-results-mcp: replace settings file: %w", err)
	}
	return nil
}
