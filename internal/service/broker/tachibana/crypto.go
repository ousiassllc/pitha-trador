package tachibana

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
)

// maxKeyFileBytes bounds what LoadPrivateKey reads (an RSA 4096 PEM is
// ~3.3 KiB).
const maxKeyFileBytes = 64 << 10

var (
	// ErrKeyUnreadable means the 秘密鍵 file is missing, too large or not
	// RSA PEM. It never carries the file's content.
	ErrKeyUnreadable = errors.New("tachibana: private key file is unreadable or not an RSA PEM key")
	// ErrDecryptURL means a virtual URL could not be decrypted with the
	// private key: it is not the pair of the public key registered on the
	// 利用設定 screen of this environment.
	ErrDecryptURL = errors.New("tachibana: cannot decrypt the virtual URL with the private key")
)

// LoadPrivateKey reads the RSA private key (PEM, PKCS#8 "PRIVATE KEY" or
// PKCS#1 "RSA PRIVATE KEY") at path. The returned errors never include the
// key material.
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrKeyUnreadable
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFileBytes+1))
	if err != nil || len(data) > maxKeyFileBytes {
		return nil, ErrKeyUnreadable
	}
	return ParsePrivateKey(data)
}

// ParsePrivateKey parses a PEM RSA private key.
func ParsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, ErrKeyUnreadable
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			return key, nil
		}
	case "PRIVATE KEY":
		if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			if key, ok := parsed.(*rsa.PrivateKey); ok {
				return key, nil
			}
		}
	}
	return nil, ErrKeyUnreadable
}

// decryptVirtualURL base64-decodes v and decrypts it with RSA-OAEP (SHA-256
// for both the hash and MGF1, as the broker's Python sample does). The
// plaintext must be an http(s) or ws(s) URL; neither it nor the ciphertext appears in
// the error.
func decryptVirtualURL(key *rsa.PrivateKey, v string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return "", ErrDecryptURL
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), nil, key, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptURL
	}
	raw := strings.TrimRight(string(plain), "\r\n ")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !slices.Contains([]string{"https", "http", "wss", "ws"}, u.Scheme) {
		return "", ErrDecryptURL
	}
	return raw, nil
}
