// Package changeguard limits how often the two things that move Paper trading
// results are changed: the meta-labeling model (the nightly retraining adopts a
// new one) and the Policy Engine's policy.* thresholds (the self-improvement
// flow applies a proposal) - issue #780, FR-META-7 / FR-SELFIMPROVE-10.
//
// Two rules, both counted in TSE trading days (internal/service/marketcalendar):
//
//   - Rate limit: a change of one kind is not allowed until Limits.ModelSwapDays
//     (resp. PolicyChangeDays) trading days passed since the last change of the
//     same kind.
//   - Freeze: a change of one kind is not allowed until Limits.FreezeDays
//     trading days passed since the last change of the other kind, so the
//     two never land in the same window and the effect of each stays attributable.
//
// The package only decides; the callers (training.Trainer, selfimprove.Governor)
// persist the reason a change was blocked.
package changeguard
