package screener_test

import (
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// universeInputs builds the ~4,000-instrument scan universe
// (non-functional.md §2.1) with a realistic spread of outcomes: most
// instruments fail a numeric filter, a few percent pass.
func universeInputs(n int) []screener.Input {
	inputs := make([]screener.Input, n)
	for i := range inputs {
		in := baseInput()
		in.InstrumentID = int64(i + 1)
		in.Symbol = fmt.Sprintf("%04d", i+1)
		switch i % 10 {
		case 1:
			in.Snapshot.Price = 50 // min_price
		case 2:
			in.Turnover5mJPY = 100 // min_turnover
		case 3:
			in.Snapshot.SpreadBps = nil // missing
		case 4:
			in.Snapshot.Feature.VolumeRatio5m = f(0.5)
		case 5, 6, 7, 8:
			in.Snapshot.Feature.Return5m = f(0.01)
		}
		inputs[i] = in
	}
	return inputs
}

func BenchmarkRun_Universe4000(b *testing.B) {
	cfg, inputs := testCfg(), universeInputs(4000)
	b.ReportAllocs()
	for b.Loop() {
		_ = screener.Run(cfg, inputs)
	}
}
