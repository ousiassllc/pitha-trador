// Package metahistory answers, for the backtest replay (issue #775), which
// meta-labeling model was in force at a decision time: the walk-forward
// guarantee that a replay never scores a decision with a model trained after
// it. It reads nothing but the stored metalabel_models rows - no model is
// trained or stored by a replay, and no market-derived value is added.
package metahistory

import (
	"sort"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

type entry struct {
	trainedAt time.Time
	model     *metalabel.Model
	err       error
}

// Timeline is the stored active models in the order they were trained.
type Timeline struct {
	entries []entry
}

// New builds a Timeline from the stored models. Only active models are in
// force (a rejected one never was); each is parsed once, and one this build
// cannot parse is kept as an error entry, because live would not fall back to
// an older model either (metalabel.Store.ActiveModel).
func New(stored []repo.Model) *Timeline {
	t := &Timeline{}
	for _, m := range stored {
		if m.Status != repo.ModelActive {
			continue
		}
		e := entry{trainedAt: m.TrainedAt}
		if parsed, err := metalabel.ParseModel(m); err != nil {
			e.err = err
		} else {
			e.model = &parsed
		}
		t.entries = append(t.entries, e)
	}
	sort.SliceStable(t.entries, func(i, j int) bool { return t.entries[i].trainedAt.Before(t.entries[j].trainedAt) })
	return t
}

// ModelAt returns the newest model trained strictly before at, or (nil, nil)
// when none had been yet (the replay then skips the entry, as live does
// without an active model). Its error is the parse error of that newest model.
func (t *Timeline) ModelAt(at time.Time) (*metalabel.Model, error) {
	n := sort.Search(len(t.entries), func(i int) bool { return !t.entries[i].trainedAt.Before(at) })
	if n == 0 {
		return nil, nil
	}
	return t.entries[n-1].model, t.entries[n-1].err
}

// Len is the number of models in the timeline.
func (t *Timeline) Len() int { return len(t.entries) }
