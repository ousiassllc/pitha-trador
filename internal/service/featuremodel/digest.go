package featuremodel

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// digest hashes the ordered training rows: snapshot id, which sides exist, the
// targets and the feature values (a missing value as its own marker). The prefix
// (the model's version prefix) keeps two models' digests of the same rows apart.
type digest struct {
	h   hash.Hash
	buf [8]byte
}

func newDigest(prefix string) *digest {
	d := &digest{h: sha256.New()}
	_, _ = d.h.Write([]byte(prefix))
	return d
}

func (d *digest) u64(v uint64) {
	binary.BigEndian.PutUint64(d.buf[:], v)
	_, _ = d.h.Write(d.buf[:])
}

func (d *digest) f64(v float64, present bool) {
	if !present {
		_, _ = d.h.Write([]byte{0})
		return
	}
	_, _ = d.h.Write([]byte{1})
	d.u64(math.Float64bits(v))
}

func (d *digest) row(r repo.TrainRow) {
	_ = binary.Write(d.h, binary.BigEndian, r.SnapshotID) // hash.Hash.Write never fails
	d.f64(r.LongNet, r.HasLong)
	d.f64(r.ShortNet, r.HasShort)
	for _, x := range r.X {
		d.f64(x, !math.IsNaN(x))
	}
}

func (d *digest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }
