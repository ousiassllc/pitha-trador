package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// secretsEncryptionKeySeed derives secretsEncryptionKey below via
// sha256.Sum256. It is a fixed, source-committed string chosen once for
// this application (issue #57 "決定事項": "真にランダムな鍵をどこか外部
// ファイルに保存する設計にはしない") - it is NOT a secret in any
// meaningful sense, since it ships inside every compiled pitha-trador
// binary. See secretsEncryptionKey's own doc comment for exactly what
// this buys (and does not buy).
const secretsEncryptionKeySeed = "pitha-trador secrets store v1"

// secretsEncryptionKey is the AES-256 key EncryptSecret/DecryptSecret use
// to encrypt internal/repository/system.SecretsRepository's secrets-table rows
// (JEV_API_KEY, JEV_BASE_URL, KABU_API_PASSWORD, SLACK_WEBHOOK_URL).
//
// Threat model - read this before assuming more than it provides: this
// encryption exists ONLY to keep those values out of plaintext if the
// SQLite *database file itself* is copied, backed up, or shared by
// accident (attached to a support ticket, synced to cloud storage,
// committed to a repo by mistake). It provides NO protection against an
// attacker who can run, inspect, or disassemble the compiled
// pitha-trador binary: the key is derived from secretsEncryptionKeySeed,
// a string compiled into that very same binary, so anyone holding the
// .exe can recover it. A truly external, non-embedded key (OS keychain,
// HSM, a separate key file the operator manages) was considered and
// rejected for this single-user desktop app - issue #57's "決定事項"
// records that trade-off explicitly so nobody later treats this as
// binary-analysis-resistant protection it was never meant to be.
var secretsEncryptionKey = sha256.Sum256([]byte(secretsEncryptionKeySeed))

// EncryptSecret AES-256-GCM-encrypts plaintext with secretsEncryptionKey,
// prepending a freshly generated random nonce to the ciphertext and
// base64-encoding the result so it fits the secrets table's
// encrypted_value TEXT column (db/migrations' create_secrets_table).
func EncryptSecret(plaintext string) (string, error) {
	gcm, err := newSecretsGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("config: generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret reverses EncryptSecret: base64-decodes encoded, splits
// off the leading nonce, and AES-256-GCM-decrypts the remainder.
func DecryptSecret(encoded string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("config: decode base64: %w", err)
	}
	gcm, err := newSecretsGCM()
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("config: ciphertext shorter than nonce")
	}
	nonce, ciphertext := sealed[:nonceSize], sealed[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("config: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// newSecretsGCM builds the AES-256-GCM cipher.AEAD both EncryptSecret and
// DecryptSecret use.
func newSecretsGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(secretsEncryptionKey[:])
	if err != nil {
		return nil, fmt.Errorf("config: build AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("config: build GCM mode: %w", err)
	}
	return gcm, nil
}
