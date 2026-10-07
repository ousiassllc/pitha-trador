package secretsflow_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestEncryptSecret_DecryptSecret_RoundTrips(t *testing.T) {
	plaintext := "jev-api-key-value"

	encrypted, err := config.EncryptSecret(plaintext)
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if strings.Contains(encrypted, plaintext) {
		t.Fatalf("EncryptSecret output %q contains the plaintext %q", encrypted, plaintext)
	}

	decrypted, err := config.DecryptSecret(encrypted)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("DecryptSecret = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptSecret_ProducesDifferentCiphertextEachTime(t *testing.T) {
	first, err := config.EncryptSecret("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	second, err := config.EncryptSecret("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if first == second {
		t.Fatalf("EncryptSecret returned identical ciphertext twice; nonce is not being randomized")
	}
}

func TestDecryptSecret_RejectsTamperedCiphertext(t *testing.T) {
	encrypted, err := config.EncryptSecret("kabu-password")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	tampered := "AA" + encrypted[2:]

	if _, err := config.DecryptSecret(tampered); err == nil {
		t.Fatal("DecryptSecret: expected an error for tampered ciphertext, got nil")
	}
}
