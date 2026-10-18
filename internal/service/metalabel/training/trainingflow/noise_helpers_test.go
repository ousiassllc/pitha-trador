package trainingflow_test

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// memStore is an in-memory training.ModelStore.
type memStore struct {
	mu     sync.Mutex
	models []repo.Model
}

func (m *memStore) SaveModel(_ context.Context, model repo.Model) (repo.Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	model.ID = int64(len(m.models) + 1)
	m.models = append(m.models, model)
	return model, nil
}

func (m *memStore) LatestActiveModel(context.Context) (repo.Model, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best repo.Model
	found := false
	for _, c := range m.models {
		if c.Status == repo.ModelActive && (!found || !c.TrainedAt.Before(best.TrainedAt)) {
			best, found = c, true
		}
	}
	return best, found, nil
}

func (m *memStore) UpdateForward(_ context.Context, id int64, f repo.Forward) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models[id-1].Forward = f
	return nil
}

// noiseSymbols is how many instruments the synthetic decisions rotate over.
const noiseSymbols = 8

var jstZone = time.FixedZone("JST", 9*60*60)

// noiseRows returns decisions on the tradingDays JST weekdays up to trainNow,
// perDay on each, spread over the 09:00-15:00 session and rotating over
// noiseSymbols instruments. Every outcome is independent of every feature: a
// win (+30 bps) or loss (-30 bps) that is a fair coin except for a shared
// day-level shift of the win probability (sd dayShift), the correlation a cluster
// bootstrap must respect, and - only when edge > 0 - a real signal: the win
// probability rises by edge*tanh(ofi) with the order flow the model sees.
func noiseRows(tradingDays, perDay int, dayShift, edge float64, seed uint64) []repo.TrainingRow {
	rng := rand.New(rand.NewPCG(seed, 11))
	var rows []repo.TrainingRow
	day := trainNow.In(jstZone)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, jstZone)
	var perDayRows [][]repo.TrainingRow
	for len(perDayRows) < tradingDays {
		if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
			shift := dayShift * rng.NormFloat64()
			var batch []repo.TrainingRow
			for i := range perDay {
				at := day.Add(9*time.Hour + time.Duration(i)*6*time.Hour/time.Duration(perDay))
				batch = append(batch, noiseRow(rng, at, i, shift, edge))
			}
			perDayRows = append(perDayRows, batch)
		}
		day = day.AddDate(0, 0, -1)
	}
	for d := len(perDayRows) - 1; d >= 0; d-- {
		rows = append(rows, perDayRows[d]...)
	}
	for i := range rows {
		rows[i].JevDecisionID = int64(i + 1)
	}
	return rows
}

func noiseRow(rng *rand.Rand, at time.Time, i int, shift, edge float64) repo.TrainingRow {
	direction, sign := domain.JevDirectionLong, 1.0
	if rng.IntN(2) == 1 {
		direction, sign = domain.JevDirectionShort, -1.0
	}
	ofi := rng.NormFloat64()
	net := -30.0
	if rng.Float64() < 0.5+shift+edge*math.Tanh(ofi) {
		net = 30
	}
	micro, flow := sign*ofi, sign*rng.NormFloat64()
	return repo.TrainingRow{
		Symbol: fmt.Sprintf("%04d", 7000+i%noiseSymbols), JevModelID: "jev-x", DecidedAt: at, Direction: direction,
		OrderFlow: domain.OrderFlowFeatures{OFIShort: &flow, OFIMid: &flow, OFILong: &flow, MicropriceDevBps: &micro},
		StateJSON: fmt.Sprintf(`{"spread_bps":%.2f,"return_1m":%.5f}`, 3+rng.Float64(), 0.001*rng.NormFloat64()),
		ResponseJSON: fmt.Sprintf(`{"direction":%q,"entry_quality":"good","toxic_flow":%.2f,"liquidity_stressed":0.1,"continuation_probability":0.6,"confidence":%.2f}`,
			direction, 0.3*rng.Float64(), 0.6+0.2*rng.Float64()),
		HorizonMinutes: trainHorizon, NetReturnBps: net,
	}
}
