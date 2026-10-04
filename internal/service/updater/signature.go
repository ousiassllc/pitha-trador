package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/version"
)

// signatureAssetName is checksums.txt's detached ed25519 signature: the
// base64 of the raw 64-byte signature over checksums.txt's exact bytes,
// published next to it by .github/workflows/ci.yml (issue #376). The signing
// key is a GitHub Actions secret, so a replaced release (compromised token
// or release job) cannot re-sign its own checksums.txt; the matching public
// key is embedded in the binary (internal/version.ReleasePublicKey).
const signatureAssetName = checksumsAssetName + ".sig"

// maxSignatureBytes caps the signature asset: a base64 ed25519 signature is
// 88 bytes, so anything near this limit is not a signature.
const maxSignatureBytes = 4 << 10

// resolvePublicKey picks the key checksums.txt.sig must verify under: the
// configured one, else internal/version.ReleasePublicKey. Both unset returns
// (nil, nil), which skips verification; a malformed key returns an error so
// downloads fail closed.
func resolvePublicKey(configured ed25519.PublicKey) (ed25519.PublicKey, error) {
	switch {
	case len(configured) > 0:
		return checkPublicKey(configured)
	case version.ReleasePublicKey != "":
		return parsePublicKey(version.ReleasePublicKey)
	}
	return nil, nil
}

// parsePublicKey decodes the base64 raw ed25519 public key that
// internal/version.ReleasePublicKey carries.
func parsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode release public key: %w", err)
	}
	return checkPublicKey(raw)
}

func checkPublicKey(key ed25519.PublicKey) (ed25519.PublicKey, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release public key is %d bytes, want %d", len(key), ed25519.PublicKeySize)
	}
	return key, nil
}

// verifySignature checks that sigPath (base64 ed25519 signature) is a valid
// signature of checksumsPath's content under key. Any mismatch is an
// ErrorVerification, i.e. permanent: retrying cannot fix a forged release.
func verifySignature(key ed25519.PublicKey, checksumsPath, sigPath string) error {
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksums file: %w", err)
	}
	encoded, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("read signature file: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return kindErrorf(ErrorVerification, "%s is not a base64 ed25519 signature", signatureAssetName)
	}
	if !ed25519.Verify(key, checksums, sig) {
		return kindErrorf(ErrorVerification, "signature verification failed for %s", checksumsAssetName)
	}
	return nil
}
