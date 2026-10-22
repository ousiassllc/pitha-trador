package entryqualitypanel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/entryqualitypanel"
)

type values map[string]string

func (v values) Get(_ context.Context, key string) (opsettings.Value, error) {
	s, ok := v[key]
	if !ok {
		return opsettings.Value{}, errors.New("no such key " + key)
	}
	return opsettings.Value{Key: key, Current: s}, nil
}

// This build runs Paper: the Paper value is the one in use, the Live one is
// shown beside it as unused.
func TestLoad_PaperRunsPaperValue(t *testing.T) {
	for side, want := range map[string]string{"policy-long": "fair", "policy-short": "good"} {
		items := entryqualitypanel.Load(context.Background(), values{
			"policy.long.min_entry_quality": "strong", "policy.short.min_entry_quality": "strong",
			"policy.paper.long.min_entry_quality": "fair", "policy.paper.short.min_entry_quality": "good",
		}, side)
		byKey := map[string]string{}
		for _, it := range items {
			byKey[it.Key] = it.Value + "|" + it.Detail
		}
		if byKey["mode"] != "Paper|" || byKey["effective"] != want+"|（Paper の値）" ||
			byKey["paper"] != want+"|← 判定に使われています" || byKey["live"] != "strong|使われていません" {
			t.Errorf("%s: panel = %v", side, byKey)
		}
	}
}

func TestLoad_ReadFailureIsShown(t *testing.T) {
	items := entryqualitypanel.Load(context.Background(), values{"policy.long.min_entry_quality": "strong"}, "policy-long")
	if len(items) != 1 || items[0].Key != "error" || !items[0].Off {
		t.Errorf("items = %+v, want the single unreadable line", items)
	}
}
