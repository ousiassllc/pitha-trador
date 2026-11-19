package actuals

import "time"

// JSTDay numbers the JST calendar day containing t: the cluster key of
// ClusterMean.
func JSTDay(t time.Time) int64 { return jstDay(t) }

// ClusterMean is the mean of values with the 95% interval that is robust to
// correlation within a day (each distinct entry of days is one cluster; the
// interval is the cluster-robust mean ± t(G-1)·SE), as the adoption criteria
// compute their own metrics. values[i] belongs to days[i]. The interval is
// missing (HasCI false) with fewer than two days, and Reliable is false: this
// is the interval alone, not a verdict on it.
func ClusterMean(values []float64, days []int64) Metric {
	return meanMetric(values, days, Criteria{})
}
