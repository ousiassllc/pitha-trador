package config

import (
	"fmt"
	"slices"
)

// Default scan.ranking_measure values (shipped config/strategy.yaml): every
// 詳細ランキング種別 1〜7 (値上がり率〜売買代金急増) on 東証全体/プライム/
// スタンダード/グロース, once a minute.
const DefaultRankingMeasureIntervalSeconds = 60

var (
	// DefaultRankingMeasureTypes are the ranking types measured when
	// scan.ranking_measure.types is empty.
	DefaultRankingMeasureTypes = []int{1, 2, 3, 4, 5, 6, 7}
	// DefaultRankingMeasureExchanges are the ExchangeDivision values measured
	// when scan.ranking_measure.exchanges is empty.
	DefaultRankingMeasureExchanges = []string{"T", "TP", "TS", "TG"}
)

const (
	minRankingType = 1
	maxRankingType = 15
)

// rankingExchangeDivisions are the ExchangeDivision values of GET /ranking
// (kabu_STATION_API.yaml).
var rankingExchangeDivisions = []string{"ALL", "T", "TP", "TS", "TG", "M", "FK", "S"}

// RankingMeasureConfig is scan.ranking_measure: the opt-in measurement loop
// that periodically calls kabuステーション GET /ranking and logs only
// measurement values (件数・duration_ms・CurrentPriceTime・HTTP/kabuコード).
// The ranking's prices are never stored or logged (kabu利用規約, issue #652).
// Disabled by default.
type RankingMeasureConfig struct {
	Enabled bool `yaml:"enabled"`
	// IncludeOutsideSession also measures outside 東証立会時間 (e.g. 8:55〜9:10
	// to time how long the ranking is empty before the open). Default false:
	// measure only during the session.
	IncludeOutsideSession bool `yaml:"include_outside_session"`
	IntervalSeconds       int  `yaml:"interval_seconds"`
	// Types are the 種別 values (1〜15) fetched each cycle.
	Types []int `yaml:"types"`
	// Exchanges are the ExchangeDivision values (T/TP/TS/TG/ALL/M/FK/S)
	// fetched each cycle, for every type.
	Exchanges []string `yaml:"exchanges"`
}

// withRankingMeasureDefaults fills the unset interval/types/exchanges with
// the shipped defaults so an enabled measurement never runs with an empty
// request set or a 0 interval.
func withRankingMeasureDefaults(cfg *RankingMeasureConfig) {
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = DefaultRankingMeasureIntervalSeconds
	}
	if len(cfg.Types) == 0 {
		cfg.Types = slices.Clone(DefaultRankingMeasureTypes)
	}
	if len(cfg.Exchanges) == 0 {
		cfg.Exchanges = slices.Clone(DefaultRankingMeasureExchanges)
	}
}

// validate rejects types and exchanges kabuステーション would answer with a 400.
func (c RankingMeasureConfig) validate(key string) []error {
	var errs []error
	for _, t := range c.Types {
		if t < minRankingType || t > maxRankingType {
			errs = append(errs, fmt.Errorf("%s.types must be within %d-%d (got %d)", key, minRankingType, maxRankingType, t))
		}
	}
	for _, e := range c.Exchanges {
		if !slices.Contains(rankingExchangeDivisions, e) {
			errs = append(errs, fmt.Errorf("%s.exchanges must be one of %v (got %q)", key, rankingExchangeDivisions, e))
		}
	}
	return errs
}
