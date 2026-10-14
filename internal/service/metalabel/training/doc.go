// Package training is the nightly walk-forward retraining of the meta-labeling
// secondary model (issue #764, parent #752), on top of the pure core of
// internal/service/metalabel.
//
// Trainer reads the triple-barrier-labeled Jev decisions (BuildRows rebuilds
// the features each one had when it was made), SplitWalkForward cuts them into
// fit < calibration < out-of-sample with purge and embargo and an OOS window
// that never starts before the LLM's training-data cutoff (arXiv:2512.23847),
// Evaluate scores the model out of sample, and the model is stored with its
// version and OOS metrics - active only when AcceptanceConfig.Judge passes. Any
// failure leaves the stored models untouched, so the previous model (or, with
// none, a skip) stays in force. internal/service/scheduler runs it daily from
// the Settings retrain time.
package training
