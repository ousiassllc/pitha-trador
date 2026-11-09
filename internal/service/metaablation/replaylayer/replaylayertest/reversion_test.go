package replaylayertest

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// A VWAP reversion candidate never meets entry_quality, so Q cannot change it:
// it enters in every cell even with quality fair, is counted apart, and the Q
// comparisons are judged on the momentum stream, where it does not appear.
func TestReversionCandidatesAreUnaffectedByQAndReportedSeparately(t *testing.T) {
	s := newScript().win(1).win(20)
	bars := s.bars(nil, 0, 40)
	res := run(t, input(bars, 1,
		decision(0, 20, quality("fair"), reversion()), // reversion, wins +20,000
		decision(0, 1), // momentum, strong, wins +20,000
	))
	for _, c := range res.Cells {
		seg := c.Segments[0]
		if seg.Reversion.Entries != 1 || c.ReversionTotals.Entries != 1 || seg.Reversion.Evaluated != 1 {
			t.Errorf("%s: reversion counts %+v, want one evaluated and entered (Q cannot drop it)", c.Name, seg.Reversion)
		}
		if len(seg.ReversionStages) == 0 || seg.ReversionStages[2].Stage != "quality" || seg.ReversionStages[2].Dropped != 0 {
			t.Errorf("%s: reversion stages %+v, want no quality drop", c.Name, seg.ReversionStages)
		}
		day := seg.Daily[0]
		if !near(day.ReversionPnLJPY, 20_000) || !near(day.MomentumPnLJPY, 20_000) || !near(day.PnLJPY, 40_000) {
			t.Errorf("%s: day %+v, want 20,000 reversion + 20,000 momentum", c.Name, day)
		}
		n := 0
		for _, tr := range seg.Trades {
			if tr.Reversion {
				n++
			}
		}
		if n != 1 || len(seg.Trades) != 2 {
			t.Errorf("%s: %d reversion of %d trades, want 1 of 2", c.Name, n, len(seg.Trades))
		}
	}
	c := comparison(t, res.Comparisons, "q_off_to_on@m_off")
	if c.BasisStream != replaylayer.StreamMomentum {
		t.Fatalf("Q comparison basis %q, want momentum", c.BasisStream)
	}
	for _, stream := range []string{replaylayer.StreamMomentum, replaylayer.StreamReversion, replaylayer.StreamTotal} {
		if d := diffs(t, c, 0, stream, replaylayer.UnitJPY); !near(d[0].Diff, 0) {
			t.Errorf("stream %s: Q diff %v, want 0 (Q changes nothing here)", stream, d[0].Diff)
		}
	}
	if d := diffs(t, c, 0, replaylayer.StreamReversion, replaylayer.UnitJPY); !near(d[0].A, 20_000) || !near(d[0].B, 20_000) {
		t.Fatalf("reversion stream %+v, want 20,000 on both sides, shown apart", d[0])
	}
}
