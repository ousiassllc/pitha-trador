package tachibanawatch

import (
	"context"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// ErrNoList is returned by Source.Candidates while no watch list has been
// decided yet (a fresh install before the first night).
var ErrNoList = errors.New("tachibanawatch: no watch list has been decided yet")

// ListReader is the part of the watch list store Source and ActiveList need.
type ListReader interface {
	AtOrBefore(ctx context.Context, date string) (domain.WatchList, bool, error)
}

// Source is the 立花 broker.CandidateSource: the watch list in use now, read
// from the store, best slot first. It never calls the broker. The list of the
// current 立会日 is used (SessionDate); when there is none yet - the nightly
// batch has not finished - the latest earlier list stays in use, so the
// morning connection never starts from nothing.
type Source struct {
	Lists ListReader
	// Clock defaults to the wall clock.
	Clock tachibana.Clock
}

// Active is the list in use now; ok is false before the first list.
func (s Source) Active(ctx context.Context) (domain.WatchList, bool, error) {
	return s.Lists.AtOrBefore(ctx, SessionDate(tachibana.OrReal(s.Clock).Now()))
}

// Candidates implements broker.CandidateSource.
func (s Source) Candidates(ctx context.Context) ([]string, error) {
	list, ok, err := s.Active(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoList
	}
	return list.Symbols(), nil
}
