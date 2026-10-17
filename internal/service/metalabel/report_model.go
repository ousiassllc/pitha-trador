package metalabel

import (
	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// reportOf maps a stored model to its Calibration-screen report (everything but
// the active version and the Settings cutoff).
func reportOf(m repo.Model) metamodel.Report {
	bins := make([]metamodel.CalibrationBin, len(m.OOSCalibration))
	for i, b := range m.OOSCalibration {
		bins[i] = metamodel.CalibrationBin{MeanPredicted: b.MeanPredicted, ObservedRate: b.ObservedRate, Count: b.Count}
	}
	report := metamodel.Report{
		Version: m.Version, Algorithm: m.Algorithm, Status: m.Status, RejectReason: m.RejectReason,
		TrainedAt: m.TrainedAt, TrainStart: m.TrainStart, TrainEnd: m.TrainEnd, OOSStart: m.OOSStart, OOSEnd: m.OOSEnd,
		TrainSamples: m.TrainSamples, OOSSamples: m.OOSSamples, OOSTakes: m.OOSTakes, OOSDays: m.OOSDays,
		OOSEVCI: interval(m.OOSEVCI), OOSAUCCI: interval(m.OOSAUCCI),
		OOSAUC: m.OOSAUC, OOSEVBps: m.OOSExpectedValueBps, OOSCalibration: bins,
		Forward: metamodel.Forward{
			EvaluatedAt: m.Forward.EvaluatedAt, From: m.Forward.From, Samples: m.Forward.Samples, Takes: m.Forward.Takes,
			Days: m.Forward.Days, EVBps: m.Forward.EVBps, EVCI: interval(m.Forward.EVCI), Degraded: m.Forward.Degraded,
		},
	}
	// A row stored before the adoption record existed has empty criteria.
	if c := m.Adoption.Criteria; c.MinOOSSamples > 0 {
		report.Criteria = &metamodel.Criteria{
			MinOOSSamples: c.MinOOSSamples, MinTakes: c.MinTakes, MinOOSDays: c.MinOOSDays,
			MinEVLowerBps: c.MinEVLowerBps, MinAUCLower: c.MinAUCLower, Alpha: c.Alpha, Resamples: c.Resamples,
		}
	}
	if c := m.Adoption.Comparison; c != nil {
		report.Comparison = &metamodel.Comparison{
			ActiveVersion: c.ActiveVersion, Comparable: c.Comparable, Note: c.Note, PairedSamples: c.PairedSamples,
			CandidateBps: c.CandidateBps, ActiveBps: c.ActiveBps, DiffBps: c.DiffBps, DiffCI: interval(c.DiffCI),
		}
	}
	if g := m.Adoption.Gate; g != nil {
		report.Gate = &metamodel.Gate{Reason: g.Reason, Detail: g.Detail, Until: g.Until}
	}
	return report
}

func interval(iv *repo.Interval) *metamodel.Interval {
	if iv == nil {
		return nil
	}
	return &metamodel.Interval{Low: iv.Low, High: iv.High}
}
