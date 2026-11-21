package training

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training/walkforward"
)

// DefaultWalkForwardConfig is the split decided in issue #862 (see FR-META-7): an
// OOS window that holds at least the default bar's samples and days (DefaultAcceptanceConfig) within DefaultOOSMaxWindow, the
// newest quarter of the rest for calibration, a 30-minute embargo (half the
// longest triple-barrier timeout, 60 minutes) and at least 100 fit and 30
// calibration rows (the logistic regression has 8 features and a bias: 9
// parameters, so 100 rows is above the rule of thumb of 10 rows per parameter).
func DefaultWalkForwardConfig() walkforward.WalkForwardConfig {
	bar := DefaultAcceptanceConfig()
	return walkforward.WalkForwardConfig{
		OOSMinRows: bar.MinOOSSamples, OOSMinDays: bar.MinOOSDays, OOSMaxWindow: DefaultOOSMaxWindow,
		CalibrationFraction: 0.25, Embargo: 30 * time.Minute,
		MinFit: 100, MinCalibration: 30,
	}
}
