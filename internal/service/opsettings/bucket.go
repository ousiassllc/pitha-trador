package opsettings

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// paperCapital is the Paper operating capital in force now.
func (s *Service) paperCapital(ctx context.Context) (float64, error) {
	limits, err := papersettings.Effective(ctx, s.store, s.paperLimitsBase())
	return limits.InitialCapital, err
}

// bucketDefaultText is a bucket key's default (bucketsettings.DefaultText; the
// stock budget follows the capital). Empty when the capital cannot be read,
// which Reset's default check then rejects.
func (s *Service) bucketDefaultText(ctx context.Context, key string) string {
	capital, err := s.paperCapital(ctx)
	if err != nil {
		return ""
	}
	return bucketsettings.DefaultText(key, capital)
}

// validateBucket checks a save of a bucket budget or of the operating capital
// against the budgets' sum (bucketsettings.CheckSave, issue #824).
func (s *Service) validateBucket(ctx context.Context, key, encoded string) error {
	capital, err := s.paperCapital(ctx)
	if err != nil {
		return err
	}
	return invalidIf(bucketsettings.CheckSave(ctx, s.store, capital, key, encoded))
}
