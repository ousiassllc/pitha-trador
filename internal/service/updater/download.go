package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// downloadAndVerify downloads assets.installer and assets.checksums to a
// fresh temp directory, verifies assets.checksums' ed25519 signature
// (assets.signature) when a release public key is configured (issue #376),
// verifies the installer's SHA256 against the matching line in
// checksums.txt's `sha256sum` output, and returns the installer's local
// path once verified. It removes the downloaded files (and the temp
// directory) on any error - including a signature or checksum mismatch -
// so a failed/tampered download never lingers on disk (issue #65: "不一致な
// ら更新を中止しエラーログのみ").
func (c *Checker) downloadAndVerify(ctx context.Context, assets releaseAssets) (installerPath string, err error) {
	installerAsset := assets.installer
	switch {
	case c.publicKeyErr != nil:
		return "", withKind(ErrorVerification, c.publicKeyErr)
	case c.publicKey == nil:
		slog.Warn("updater: no release public key configured; installer authenticity is checked against checksums.txt only")
	case assets.signature.Name == "":
		return "", kindErrorf(ErrorVerification, "release has no %s, refusing unsigned installer", signatureAssetName)
	}

	ctx, cancel := context.WithTimeout(ctx, c.downloadTimeout)
	defer cancel()

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
	if err = c.downloadTo(ctx, assets.checksums, checksumsPath, maxChecksumsBytes); err != nil {
		return "", fmt.Errorf("download %s: %w", assets.checksums.Name, err)
	}

	if c.publicKey != nil {
		sigPath := filepath.Join(dir, signatureAssetName)
		if err = c.downloadTo(ctx, assets.signature, sigPath, maxSignatureBytes); err != nil {
			return "", fmt.Errorf("download %s: %w", assets.signature.Name, err)
		}
		if err = verifySignature(c.publicKey, checksumsPath, sigPath); err != nil {
			return "", err
		}
	}

	installerPath = filepath.Join(dir, filepath.Base(installerAsset.Name))
	if err = c.downloadTo(ctx, installerAsset, installerPath, maxInstallerBytes); err != nil {
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
		err = kindErrorf(ErrorVerification, "checksum mismatch for %s: want %s, got %s", installerAsset.Name, wantHash, gotHash)
		return "", err
	}

	return installerPath, nil
}

// checkDownloadURL rejects any asset URL outside prefix (the repository's
// own release-download path), so a tampered release JSON cannot point the
// unattended installer download at an arbitrary host.
func checkDownloadURL(raw, prefix string) error {
	want, err := url.Parse(prefix)
	if err != nil {
		return fmt.Errorf("parse download prefix: %w", err)
	}
	got, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse download url: %w", err)
	}
	if got.Scheme != want.Scheme || got.Host != want.Host || got.User != nil ||
		!strings.HasPrefix(path.Clean(got.Path), strings.TrimSuffix(want.Path, "/")+"/") {
		return fmt.Errorf("download url %q is outside %s", raw, prefix)
	}
	return nil
}

// downloadTo streams asset's body to dest after validating its URL, and
// fails once the body exceeds maxBytes (or asset.Size, when GitHub reports
// one) instead of filling the disk.
func (c *Checker) downloadTo(ctx context.Context, asset Asset, dest string, maxBytes int64) error {
	if err := checkDownloadURL(asset.BrowserDownloadURL, c.downloadURLPrefix); err != nil {
		return withKind(ErrorVerification, err)
	}
	if asset.Size > maxBytes {
		return kindErrorf(ErrorVerification, "asset size %d exceeds limit %d", asset.Size, maxBytes)
	}
	if asset.Size > 0 {
		maxBytes = asset.Size
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return withKind(ErrorNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp, "asset download"); err != nil {
		return err
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return withKind(ErrorNetwork, err)
	}
	if n > maxBytes {
		return kindErrorf(ErrorVerification, "response body exceeds limit %d bytes", maxBytes)
	}
	if asset.Size > 0 && n != asset.Size {
		return kindErrorf(ErrorVerification, "response body is %d bytes, want %d", n, asset.Size)
	}
	return nil
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
	return "", kindErrorf(ErrorVerification, "no checksum entry for %s", name)
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
