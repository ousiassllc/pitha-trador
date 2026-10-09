package opsettings

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxKeyFileBytes bounds what validatePrivateKeyFile reads: an RSA 4096 PEM
// is ~3.3 KiB, so anything far beyond that is not a key file.
const maxKeyFileBytes = 64 << 10

// validatePrivateKeyFile checks that path is a readable PEM file holding an
// RSA 2048/4096 private key (issue #733): not a missing file, not a public
// key or certificate, not another algorithm. The reasons never include the
// file's content.
func validatePrivateKeyFile(path string) error {
	data, err := readKeyFile(path)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return &InvalidValueError{Reason: "PEM 形式のファイルではありません"}
	}
	switch {
	case strings.Contains(block.Type, "PUBLIC KEY"):
		return &InvalidValueError{Reason: "公開鍵が指定されています。秘密鍵の PEM ファイルを指定してください"}
	case block.Type == "CERTIFICATE":
		return &InvalidValueError{Reason: "証明書が指定されています。秘密鍵の PEM ファイルを指定してください"}
	case block.Type == "ENCRYPTED PRIVATE KEY" || block.Headers["Proc-Type"] != "":
		return &InvalidValueError{Reason: "パスフレーズで暗号化された秘密鍵には対応していません"}
	}
	key, err := parseRSAPrivateKey(block)
	if err != nil {
		return err
	}
	if bits := key.N.BitLen(); bits != 2048 && bits != 4096 {
		return &InvalidValueError{Reason: "RSA 2048 または 4096 ビットの鍵を指定してください"}
	}
	return nil
}

func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &InvalidValueError{Reason: "ファイルを開けません（存在しない、または読み取り権限がありません）"}
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, &InvalidValueError{Reason: "通常のファイルではありません"}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFileBytes+1))
	if err != nil {
		return nil, &InvalidValueError{Reason: "ファイルを読み取れません"}
	}
	if len(data) > maxKeyFileBytes {
		return nil, &InvalidValueError{Reason: "秘密鍵ファイルとしては大きすぎます"}
	}
	return data, nil
}

// parseRSAPrivateKey parses a PKCS#1 ("RSA PRIVATE KEY") or PKCS#8
// ("PRIVATE KEY") block as an RSA key.
func parseRSAPrivateKey(block *pem.Block) (*rsa.PrivateKey, error) {
	notKey := &InvalidValueError{Reason: "RSA 秘密鍵として読み取れません"}
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, notKey
		}
		return key, nil
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, notKey
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, &InvalidValueError{Reason: "RSA 以外の鍵です。RSA 2048 または 4096 ビットの鍵を指定してください"}
		}
		return key, nil
	}
	return nil, notKey
}

// syncFolderMarkers are lower-case path-component prefixes of the common
// cloud-sync folders (OneDrive, Dropbox, Google Drive, iCloud Drive, ...).
var syncFolderMarkers = []string{
	"onedrive", "dropbox", "google drive", "googledrive", "google ドライブ",
	"icloud", "mobile documents", "box sync", "pcloud",
}

// syncFolderWarning returns a hint when path lies under a cloud-sync folder,
// where the private key would be copied to a third party.
func syncFolderWarning(path string) string {
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		part = strings.ToLower(part)
		for _, marker := range syncFolderMarkers {
			if strings.HasPrefix(part, marker) {
				return "このパスはクラウド同期フォルダ（OneDrive / Dropbox / Google ドライブ等）配下に見えます。秘密鍵が外部サービスへ複製されるため、同期されない場所に置くことを推奨します。"
			}
		}
	}
	return ""
}

// privateKeyWarning is Value.Warning of a stored 秘密鍵 path: the file is
// gone now (the content is only validated on save, not on every render),
// and/or it sits in a sync folder.
func privateKeyWarning(path string) string {
	var warnings []string
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		warnings = append(warnings, "保存済みのパスに鍵ファイルが見つかりません。存在しない間、立花への接続はできません。")
	}
	if w := syncFolderWarning(path); w != "" {
		warnings = append(warnings, w)
	}
	return strings.Join(warnings, " ")
}
