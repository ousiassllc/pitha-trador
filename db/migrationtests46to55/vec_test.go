package migrationtests46to55

// These tests migrate through 000043, which creates sqlite-vec vec0 virtual
// tables. The module is registered process-wide by this blank import (the
// application does the same in internal/repository/sqlitedb), and has to be
// linked into this test binary even though no test refers to it by name.
import _ "modernc.org/sqlite/vec"
