package bootstrap

import "time"

// newsPollInterval is how often News Ingest polls the external news feed
// for the Fast Screener candidates and held symbols, during the TSE session
// only (FR-LUNA-1, issue #531).
const newsPollInterval = time.Minute
