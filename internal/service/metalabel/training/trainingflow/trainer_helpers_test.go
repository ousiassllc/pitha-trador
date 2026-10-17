package trainingflow_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

var trainNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

const trainHorizon = 15

// trainingRows returns n stored decisions, one every 20 minutes ending a few
// minutes before trainNow. A LONG/SHORT decision wins when its signed OFI is
// high (the signal), so a model can learn it.
func trainingRows(n int, seed uint64) []repo.TrainingRow {
	rng := rand.New(rand.NewPCG(seed, 7))
	rows := make([]repo.TrainingRow, n)
	for i := range rows {
		at := trainNow.Add(-30 * time.Minute).Add(-time.Duration(n-1-i) * 20 * time.Minute)
		direction, sign := domain.JevDirectionLong, 1.0
		if i%2 == 1 {
			direction, sign = domain.JevDirectionShort, -1.0
		}
		ofi := rng.NormFloat64()
		net := -20.0
		if rng.Float64() < 1/(1+math.Exp(-2*ofi)) {
			net = 30
		}
		micro, flow := sign*ofi, sign*ofi
		rows[i] = repo.TrainingRow{
			JevDecisionID: int64(i + 1), Symbol: "7203", DecidedAt: at, Direction: direction,
			OrderFlow: domain.OrderFlowFeatures{OFIShort: &flow, OFIMid: &flow, OFILong: &flow, MicropriceDevBps: &micro},
			StateJSON: fmt.Sprintf(`{"spread_bps":%.2f,"return_1m":%.5f}`, 3+rng.Float64(), 0.001*rng.NormFloat64()),
			ResponseJSON: fmt.Sprintf(`{"direction":%q,"entry_quality":"good","toxic_flow":%.2f,"liquidity_stressed":0.1,"continuation_probability":0.6,"confidence":%.2f}`,
				direction, 0.3*rng.Float64(), 0.6+0.2*rng.Float64()),
			HorizonMinutes: trainHorizon, NetReturnBps: net,
		}
	}
	return rows
}

type fakeSource struct {
	rows []repo.TrainingRow
	err  error
	from time.Time
	to   time.Time
}

func (f *fakeSource) ListTrainingRows(_ context.Context, from, to time.Time) ([]repo.TrainingRow, error) {
	f.from, f.to = from, to
	return f.rows, f.err
}

type failingSaver struct{ err error }

func (f failingSaver) SaveModel(context.Context, repo.Model) (repo.Model, error) {
	return repo.Model{}, f.err
}

func (failingSaver) LatestActiveModel(context.Context) (repo.Model, bool, error) {
	return repo.Model{}, false, nil
}

func (failingSaver) UpdateForward(context.Context, int64, repo.Forward) error { return nil }

// lenientAcceptance is a bar the small, strongly learnable fixtures of these tests
// clear: the default bar needs hundreds of OOS takes (see the noise simulation
// and the adoption tests, which use the defaults).
func lenientAcceptance() training.AcceptanceConfig {
	c := training.DefaultAcceptanceConfig()
	c.MinOOSSamples, c.MinTakes, c.MinOOSDays = 100, 40, 3
	return c
}

func settingsOf(mut func(*metalabelsettings.Settings)) training.SettingsLoader {
	return func(context.Context) (metalabelsettings.Settings, error) {
		s := metalabelsettings.Default()
		s.LLMCutoff = "2026-09-01"
		if mut != nil {
			mut(&s)
		}
		return s, nil
	}
}

// newTrainer returns a Trainer on the lenient bar and without change limits
// (both overridable by opts); the adoption tests pass the real ones.
func newTrainer(src training.TrainingSource, store training.ModelStore, settings training.SettingsLoader, opts ...training.TrainerOption) *training.Trainer {
	opts = append([]training.TrainerOption{
		training.WithClock(func() time.Time { return trainNow }),
		training.WithAcceptance(lenientAcceptance()), training.WithChangeLimits(changeguard.Limits{}),
	}, opts...)
	return training.NewTrainer(src, store, settings, opts...)
}

// captureLogs routes slog to a buffer for the test and returns it.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
