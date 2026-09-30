package bootstrap

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// LogDir is the directory every entrypoint's logging.RotatingWriter
// writes the daily structured JSON log file to, and the Scheduler's
// maintenance task (logging.Archiver) compresses files past their 30-day
// retention in (requirements/non-functional.md §5).
const LogDir = "logs"

// EnvBackupDir names the environment variable holding the destination
// directory of the daily SQLite backup (requirements/non-functional.md §3):
// a location outside the local application disk (external drive / synced
// cloud folder). Unset or empty disables the backup job.
const EnvBackupDir = "PITHA_BACKUP_DIR"

// jevMaxAttemptsForTest lets in-package tests cap the Jev client's attempts (no real backoff); 0 = production default.
var jevMaxAttemptsForTest int

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
// for every active instrument (FR-LUNA-1).
const newsPollInterval = time.Minute
