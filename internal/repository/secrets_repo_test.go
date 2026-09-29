package repository_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestSecretsRepository_Get_MissingKeyReportsNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)

	value, ok, err := repo.Get(context.Background(), "JEV_API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("Get: ok = true, want false")
	}
	if value != "" {
		t.Fatalf("Get: value = %q, want empty", value)
	}
}

func TestSecretsRepository_SetThenGet_RoundTrips(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)
	ctx := context.Background()

	if err := repo.Set(ctx, "JEV_API_KEY", "super-secret-jev-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, ok, err := repo.Get(ctx, "JEV_API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get: ok = false, want true")
	}
	if value != "super-secret-jev-key" {
		t.Fatalf("Get: value = %q, want %q", value, "super-secret-jev-key")
	}
}

func TestSecretsRepository_Set_OverwritesExistingKey(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)
	ctx := context.Background()

	if err := repo.Set(ctx, "KABU_API_PASSWORD", "first-password"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := repo.Set(ctx, "KABU_API_PASSWORD", "second-password"); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	value, ok, err := repo.Get(ctx, "KABU_API_PASSWORD")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || value != "second-password" {
		t.Fatalf("Get: value = %q, ok = %v, want %q, true", value, ok, "second-password")
	}
}

func TestSecretsRepository_Set_RejectsEmptyPlaintextAndKeepsStoredValue(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)
	ctx := context.Background()

	if err := repo.Set(ctx, "SLACK_WEBHOOK_URL", "https://hooks.slack.com/services/T/B/X"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := repo.Set(ctx, "SLACK_WEBHOOK_URL", ""); !errors.Is(err, repository.ErrEmptySecret) {
		t.Fatalf("empty Set error = %v, want repository.ErrEmptySecret", err)
	}

	value, ok, err := repo.Get(ctx, "SLACK_WEBHOOK_URL")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || value != "https://hooks.slack.com/services/T/B/X" {
		t.Fatalf("Get after rejected empty Set: value = %q, ok = %v, want the original value kept", value, ok)
	}
}

func TestSecretsRepository_Delete_RemovesOnlyTheGivenKey(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)
	ctx := context.Background()

	for key, value := range map[string]string{"SLACK_WEBHOOK_URL": "https://hooks.example.com", "JEV_API_KEY": "jev-key"} {
		if err := repo.Set(ctx, key, value); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	if err := repo.Delete(ctx, "SLACK_WEBHOOK_URL"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, "SLACK_WEBHOOK_URL"); err != nil {
		t.Fatalf("Delete of an already-unset key: %v", err)
	}

	if _, ok, err := repo.Get(ctx, "SLACK_WEBHOOK_URL"); err != nil || ok {
		t.Fatalf("Get deleted key: ok = %v, err = %v, want false, nil", ok, err)
	}
	if value, ok, err := repo.Get(ctx, "JEV_API_KEY"); err != nil || !ok || value != "jev-key" {
		t.Fatalf("Get untouched key: value = %q, ok = %v, err = %v, want jev-key, true, nil", value, ok, err)
	}
}

// TestSecretsRepository_StoresValueEncryptedAtRest is the direct proof
// for issue #57's acceptance criterion "secretsテーブルに保存した値が暗号
// 化された状態でDBファイルに格納されている（平文でSELECTしても読めない
// ことをテストで確認）": it bypasses SecretsRepository entirely and reads
// the raw encrypted_value column with a plain SQL SELECT.
func TestSecretsRepository_StoresValueEncryptedAtRest(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewSecretsRepository(db)
	ctx := context.Background()

	plaintext := "jev-plaintext-api-key"
	if err := repo.Set(ctx, "JEV_API_KEY", plaintext); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var rawValue string
	if err := db.QueryRowContext(ctx, `SELECT encrypted_value FROM secrets WHERE key = ?`, "JEV_API_KEY").Scan(&rawValue); err != nil {
		t.Fatalf("raw SELECT: %v", err)
	}
	if rawValue == plaintext {
		t.Fatalf("encrypted_value column stores the plaintext verbatim: %q", rawValue)
	}
	if strings.Contains(rawValue, plaintext) {
		t.Fatalf("encrypted_value column %q contains the plaintext %q", rawValue, plaintext)
	}
}
