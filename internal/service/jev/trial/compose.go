package trial

import (
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Multi fans a live decision out to several jev.ShadowSubmitters (the Jev
// shadow and the trial), so each can sit on the Trader's single shadow seam
// without trader.go knowing about either. Each submitter keeps its own
// state, switch and failure handling; Multi adds none.
type Multi []jev.ShadowSubmitter

var _ jev.ShadowSubmitter = Multi(nil)

// Compose returns the submitters as one jev.ShadowSubmitter, in order. Nil
// entries are skipped; with none it returns nil (no shadow).
func Compose(subs ...jev.ShadowSubmitter) jev.ShadowSubmitter {
	var m Multi
	for _, s := range subs {
		if s != nil {
			m = append(m, s)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// Submit hands the decision to every submitter. Each returns at once.
func (m Multi) Submit(live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	for _, s := range m {
		s.Submit(live, state, ragContext)
	}
}

// Close closes every submitter.
func (m Multi) Close() {
	for _, s := range m {
		s.Close()
	}
}
