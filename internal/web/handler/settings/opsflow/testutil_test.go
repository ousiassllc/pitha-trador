package opsflow_test

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// fakeSecretsStore is an in-memory settings.SecretsStore.
type fakeSecretsStore struct{ values map[string]string }

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{values: map[string]string{}}
}

func (f *fakeSecretsStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeSecretsStore) Set(_ context.Context, key, plaintext string) error {
	f.values[key] = plaintext
	return nil
}

func (f *fakeSecretsStore) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	return nil
}

// settingsRouter wires the Settings page the way internal/router.New does.
func settingsRouter(h *settings.SettingsHandler) *gin.Engine {
	engine := gin.New()
	engine.GET("/settings", h.Page)
	return engine
}
