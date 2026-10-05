package bootstrap

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// EnvBackupDir names the environment variable holding the destination
// directory of the daily SQLite backup (requirements/non-functional.md §3):
// a location outside the local application disk (external drive / synced
// cloud folder). Unset or empty disables the backup job.
const EnvBackupDir = "PITHA_BACKUP_DIR"

// defaultTokenRefreshInterval is how often Services.Start reissues the
// kabuステーションAPI token (marketdata.Client.Start). The API does not
// publish an exact token TTL (docs/architecture/overview.md §5), so 20
// minutes is a conservative guess: well before any plausible expiry, well
// above fullScanInterval (60s).
const defaultTokenRefreshInterval = 20 * time.Minute

// defaultKabuExchange is the kabuステーションAPI market code every
// instrument is queried under: the target universe is TSE-listed equities
// only, so instruments.Market (free text such as "TSE Prime") is not
// translated per row.
const defaultKabuExchange = marketdata.ExchangeTSE

// newsPollInterval is how often News Ingest polls the external news feed
// for the Fast Screener candidates and held symbols, during the TSE session
// only (FR-LUNA-1, issue #531).
const newsPollInterval = time.Minute
