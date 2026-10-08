package opsettings_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func writePEM(t *testing.T, dir, name, blockType string, der []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func rsaKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSave_PrivateKeyPathAcceptsRSA2048And4096InPKCS1AndPKCS8(t *testing.T) {
	dir := t.TempDir()
	k2048, k4096 := rsaKey(t, 2048), rsaKey(t, 4096)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(k4096)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"pkcs1-2048": writePEM(t, dir, "a.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k2048)),
		"pkcs8-4096": writePEM(t, dir, "b.pem", "PRIVATE KEY", pkcs8),
	} {
		store := &fakeStore{rows: map[string]string{}}
		if err := newService(store).Save(context.Background(), config.KeyTachibanaDemoPrivateKeyPath, path); err != nil {
			t.Errorf("%s: Save = %v, want nil", name, err)
		}
		if _, ok := store.rows[config.KeyTachibanaDemoPrivateKeyPath]; !ok {
			t.Errorf("%s: path was not stored", name)
		}
	}
}

func TestSave_PrivateKeyPathRejectsUnusableFilesWithoutStoringOrEchoingContent(t *testing.T) {
	dir := t.TempDir()
	small := rsaKey(t, 1024)
	pub2048, err := x509.MarshalPKIXPublicKey(&rsaKey(t, 2048).PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	certKey := rsaKey(t, 2048)
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"}, NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &certKey.PublicKey, certKey)
	if err != nil {
		t.Fatal(err)
	}
	notPEM := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(notPEM, []byte("SECRET-LOOKING-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}
	garbage := writePEM(t, dir, "garbage.pem", "RSA PRIVATE KEY", []byte("not der SECRET-LOOKING-CONTENT"))

	tests := map[string]string{
		"missing":    filepath.Join(dir, "nope.pem"),
		"directory":  dir,
		"not pem":    notPEM,
		"garbage":    garbage,
		"public key": writePEM(t, dir, "pub.pem", "PUBLIC KEY", pub2048),
		"cert":       writePEM(t, dir, "cert.pem", "CERTIFICATE", certDER),
		"rsa 1024":   writePEM(t, dir, "small.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(small)),
		"ec":         writePEM(t, dir, "ec.pem", "PRIVATE KEY", ecDER),
		"relative":   "key.pem",
	}
	for name, path := range tests {
		store := &fakeStore{rows: map[string]string{}}
		err := newService(store).Save(context.Background(), config.KeyTachibanaProdPrivateKeyPath, path)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: Save = %v, want *InvalidValueError", name, err)
			continue
		}
		if len(store.rows) != 0 {
			t.Errorf("%s: the store was written: %v", name, store.rows)
		}
		if strings.Contains(err.Error(), "SECRET-LOOKING-CONTENT") {
			t.Errorf("%s: the error echoes the file content: %v", name, err)
		}
	}
}

func TestGet_PrivateKeyPathWarnsAboutSyncFoldersAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	syncDir := filepath.Join(root, "OneDrive - Corp", "keys")
	if err := os.MkdirAll(syncDir, 0o700); err != nil {
		t.Fatal(err)
	}
	synced := writePEM(t, syncDir, "k.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rsaKey(t, 2048)))
	local := writePEM(t, root, "k.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rsaKey(t, 2048)))
	ctx := context.Background()

	svc := newService(&fakeStore{rows: map[string]string{}})
	if err := svc.Save(ctx, config.KeyTachibanaDemoPrivateKeyPath, synced); err != nil {
		t.Fatalf("Save into a sync folder must succeed (warning only): %v", err)
	}
	if v, _ := svc.Get(ctx, config.KeyTachibanaDemoPrivateKeyPath); !strings.Contains(v.Warning, "同期フォルダ") || !v.Overridden {
		t.Errorf("sync-folder key = %+v, want a sync-folder warning", v)
	}
	if err := svc.Save(ctx, config.KeyTachibanaProdPrivateKeyPath, local); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.Get(ctx, config.KeyTachibanaProdPrivateKeyPath); v.Warning != "" {
		t.Errorf("local key warning = %q, want none", v.Warning)
	}
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.Get(ctx, config.KeyTachibanaProdPrivateKeyPath); v.Warning == "" {
		t.Error("a stored key file that vanished must carry a warning")
	}
}

func TestSave_BrokerSettingsAreStoredAsNormalizedJSONScalars(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()
	for key, raw := range map[string]string{
		config.KeyBrokerProvider:               "Tachibana",
		config.KeyTachibanaEnvironment:         "production",
		config.KeyTachibanaDemoBaseURL:         "https://demo.example.com/e_api_v4r11",
		config.KeyTachibanaRequestMaxPerSecond: "5",
		config.KeyTachibanaReauthTime:          "6:05",
	} {
		if err := svc.Save(ctx, key, raw); err != nil {
			t.Fatalf("Save %s: %v", key, err)
		}
	}
	want := map[string]string{
		config.KeyBrokerProvider:               `"tachibana"`,
		config.KeyTachibanaEnvironment:         `"production"`,
		config.KeyTachibanaDemoBaseURL:         `"https://demo.example.com/e_api_v4r11/"`,
		config.KeyTachibanaRequestMaxPerSecond: `5`,
		config.KeyTachibanaReauthTime:          `"06:05"`,
	}
	for key, w := range want {
		if store.rows[key] != w {
			t.Errorf("row %s = %s, want %s", key, store.rows[key], w)
		}
	}
	if v, _ := svc.Get(ctx, config.KeyTachibanaRequestMaxPerSecond); v.Current != "5" || v.Default != "1" || !v.Overridden {
		t.Errorf("request rate = %+v", v)
	}
	if v, _ := svc.Get(ctx, config.KeyBrokerProvider); v.Current != "tachibana" || v.Default != "kabu" {
		t.Errorf("provider = %+v", v)
	}
}

func TestSave_BrokerSettingsRejectBadValues(t *testing.T) {
	for key, raw := range map[string]string{
		config.KeyBrokerProvider:               "sbi",
		config.KeyTachibanaEnvironment:         "live",
		config.KeyTachibanaDemoBaseURL:         "http://insecure.example.com/",
		config.KeyTachibanaRequestMaxPerSecond: "11",
		config.KeyTachibanaReauthTime:          "09:00",
	} {
		store := &fakeStore{rows: map[string]string{}}
		err := newService(store).Save(context.Background(), key, raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) || len(store.rows) != 0 {
			t.Errorf("Save(%s, %q) = %v rows=%v, want *InvalidValueError and no write", key, raw, err, store.rows)
		}
	}
}

func TestLoadBroker_DefaultsWhenNothingStoredAndStoredValuesOtherwise(t *testing.T) {
	ctx := context.Background()
	got, err := opsettings.LoadBroker(ctx, &fakeStore{rows: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != config.BrokerKabu || got.Tachibana.Environment != config.TachibanaEnvDemo || got.Tachibana.Production() ||
		got.Tachibana.BaseURL() != config.DefaultTachibanaDemoBaseURL || got.Tachibana.ProdBaseURL != config.DefaultTachibanaProdBaseURL ||
		got.Tachibana.RequestMaxPerSecond != 1 || got.Tachibana.ReauthTime != "05:35" || got.Tachibana.PrivateKeyPath() != "" {
		t.Errorf("defaults = %+v", got)
	}

	store := &fakeStore{rows: map[string]string{
		config.KeyBrokerProvider:               `"tachibana"`,
		config.KeyTachibanaEnvironment:         `"production"`,
		config.KeyTachibanaProdPrivateKeyPath:  `"/keys/prod.pem"`,
		config.KeyTachibanaRequestMaxPerSecond: `7`,
		config.KeyTachibanaReauthTime:          `"06:00"`,
	}}
	got, err = opsettings.LoadBroker(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != config.BrokerTachibana || !got.Tachibana.Production() || got.Tachibana.PrivateKeyPath() != "/keys/prod.pem" ||
		got.Tachibana.RequestMaxPerSecond != 7 || got.Tachibana.ReauthTime != "06:00" {
		t.Errorf("stored = %+v", got)
	}
}

func TestLoadBroker_MalformedRowFallsBackToDefaultAndStoreErrorFails(t *testing.T) {
	ctx := context.Background()
	got, err := opsettings.LoadBroker(ctx, &fakeStore{rows: map[string]string{
		config.KeyBrokerProvider:               `not json`,
		config.KeyTachibanaEnvironment:         `"staging"`,
		config.KeyTachibanaRequestMaxPerSecond: `99`,
	}})
	if err != nil {
		t.Fatalf("a malformed row must not fail start-up: %v", err)
	}
	if got.Provider != config.BrokerKabu || got.Tachibana.Environment != config.TachibanaEnvDemo || got.Tachibana.RequestMaxPerSecond != 1 {
		t.Errorf("malformed rows = %+v, want the defaults", got)
	}
	if _, err := opsettings.LoadBroker(ctx, &fakeStore{getErr: errors.New("boom")}); err == nil {
		t.Error("a store read failure must be returned")
	}
}
