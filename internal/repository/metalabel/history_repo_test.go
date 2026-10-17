package metalabel_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func TestRepository_ListActiveModelsReturnsActiveOnesOldestFirst(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 17, 0, 0, 0, time.UTC) }
	for _, m := range []metalabel.Model{
		sampleModel("v-new", metalabel.ModelActive, day(14)),
		sampleModel("v-rejected", metalabel.ModelRejected, day(13)),
		sampleModel("v-old", metalabel.ModelActive, day(10)),
	} {
		if _, err := r.InsertModel(ctx, m); err != nil {
			t.Fatalf("InsertModel(%s): %v", m.Version, err)
		}
	}

	got, err := r.ListActiveModels(ctx)
	if err != nil {
		t.Fatalf("ListActiveModels: %v", err)
	}
	if len(got) != 2 || got[0].Version != "v-old" || got[1].Version != "v-new" {
		t.Fatalf("ListActiveModels = %v, want [v-old v-new] (active only, oldest first)", versions(got))
	}
}

func TestRepository_ListActiveModelsIsEmptyWithoutModels(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	got, err := r.ListActiveModels(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("ListActiveModels = %v, %v; want none", got, err)
	}
}

func versions(models []metalabel.Model) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.Version
	}
	return out
}
