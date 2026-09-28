package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloadAndVerify downloads installerAsset and checksumAsset to a fresh
// temp directory, verifies installerAsset's SHA256 against the matching
// line in checksumAsset's `sha256sum` output, and returns the installer's
// local path once verified. It removes the downloaded files (and the
// temp directory) on any error - including a checksum mismatch - so a
// failed/tampered download never lingers on disk (issue #65: "不一致な
// ら更新を中止しエラーログのみ").
func (c *Checker) downloadAndVerify(ctx context.Context, installerAsset, checksumAsset Asset) (path string, err error) {
	dir, err := os.MkdirTemp("", "pitha-trador-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()

	checksumsPath := filepath.Join(dir, checksumsAssetName)
	if err = c.downloadTo(ctx, checksumAsset.BrowserDownloadURL, checksumsPath); err != nil {
		return "", fmt.Errorf("download %s: %w", checksumAsset.Name, err)
	}

	installerPath := filepath.Join(dir, installerAsset.Name)
	if err = c.downloadTo(ctx, installerAsset.BrowserDownloadURL, installerPath); err != nil {
		return "", fmt.Errorf("download %s: %w", installerAsset.Name, err)
	}

	var wantHash string
	wantHash, err = readChecksum(checksumsPath, installerAsset.Name)
	if err != nil {
		return "", err
	}
	var gotHash string
	gotHash, err = sha256File(installerPath)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", installerAsset.Name, err)
	}
	if !strings.EqualFold(wantHash, gotHash) {
		err = fmt.Errorf("checksum mismatch for %s: want %s, got %s", installerAsset.Name, wantHash, gotHash)
		return "", err
	}

	return installerPath, nil
}

// downloadTo streams url's response body to dest.
func (c *Checker) downloadTo(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = io.Copy(f, resp.Body)
	return err
}

// readChecksum finds name's hash in a `sha256sum`-formatted file (each
// line: "<hash>  <filename>", optionally "*<filename>" in binary mode).
func readChecksum(checksumsPath, name string) (string, error) {
	f, err := os.Open(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("open checksums file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		hash, fileName := fields[0], strings.TrimPrefix(fields[1], "*")
		if filepath.Base(fileName) == name {
			return hash, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums file: %w", err)
	}
	return "", fmt.Errorf("no checksum entry for %s", name)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
