// Package sqlitedb centralizes the SQLite dialect decisions shared by the
// desktop stores: driver open pragmas, unique-violation detection and the
// timestamp representation. Timestamps are stored as fixed-width
// RFC3339-nano UTC TEXT so lexicographic order equals chronological order —
// the canvas task FIFO claim relies on it — and date()/strftime() work in
// SQL for the usage rollups.
package sqlitedb

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// TimeLayout is the canonical TEXT representation for TIMESTAMP columns:
// always 9 fractional digits and UTC, e.g. 2026-09-10T07:08:09.123456789Z.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime renders t as the canonical storage form.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// ParseTime decodes a TEXT timestamp column back into a time.Time so SQLite
// stores can return the same Go types their MySQL counterparts do.
func ParseTime(s string) (time.Time, error) {
	return time.Parse(TimeLayout, s)
}

// pragmaQuery rides on the DSN so every pooled connection gets the pragmas:
// busy_timeout and foreign_keys are connection-scoped, so applying them once
// via db.Exec left every pool connection after the first without them
// (spurious SQLITE_BUSY, unenforced FKs). synchronous=NORMAL is the WAL
// pairing that drops fsync pressure without losing crash safety beyond a
// power-cut transaction.
const pragmaQuery = "_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)"

// pragmaDSN appends the pragma query to dsn, whatever form it came in
// (plain path, file: URI, :memory:, already-parameterized).
func pragmaDSN(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + pragmaQuery
}

// Open opens dsn with the pragmas a single-writer desktop app wants: WAL
// journaling (readers don't block the writer), a generous busy timeout and
// enforced foreign keys — on every connection of the pool.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", pragmaDSN(dsn))
	if err != nil {
		return nil, err
	}
	// 网关 + 画布双服务与 worker 共用这一个库:WAL 下并发读安全,写锁
	// 竞争由 busy_timeout 化解;封顶连接数防止突发把文件锁打崩。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	return db, nil
}

// IsUniqueViolation reports whether err is a UNIQUE/PRIMARY KEY constraint
// failure — the SQLite counterpart of the MySQL 1062 checks that normalize
// duplicate inserts into store sentinel errors.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code()&0xff == 19 // SQLITE_CONSTRAINT
	}
	// Fallback for rewrapped errors that lost the driver type.
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "PRIMARY KEY constraint failed")
}
