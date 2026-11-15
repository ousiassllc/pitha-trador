package researchlabel

import "testing"

// A market-data cycle writes one bar per instrument, so the ids of one
// instrument are the cycle's instrument count apart. The bucket must not follow
// that stride: reading the buckets below k has to be an even k% of every
// instrument's minutes, not all of some instruments and none of others.
func TestSampleBucketSpreadsAStridedIDSequenceEvenly(t *testing.T) {
	for _, stride := range []int64{1, 2, 10, 100, 120, 200} {
		var perBucket [SampleBuckets]int
		const n = 50_000
		for i := int64(0); i < n; i++ {
			b := SampleBucket(1 + i*stride)
			if b < 0 || b >= SampleBuckets {
				t.Fatalf("stride %d: bucket %d out of range", stride, b)
			}
			perBucket[b]++
		}
		for b, c := range perBucket {
			if want := n / SampleBuckets; c < want*8/10 || c > want*12/10 {
				t.Errorf("stride %d: bucket %d holds %d of %d ids, want about %d", stride, b, c, n, want)
			}
		}
	}
	first, second := SampleBucket(42), SampleBucket(42)
	if first != second {
		t.Error("the bucket of an id is not deterministic")
	}
}
