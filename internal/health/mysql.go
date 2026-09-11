package health

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQL pings the sql connection pool.
type MySQL struct {
	DB *sql.DB
}

func (m MySQL) Ping(ctx context.Context) error { return m.DB.PingContext(ctx) }

// OpenMySQL opens the MySQL connection pool for the given DSN. The pool is
// lazy: failures surface through Ping, not here.
func OpenMySQL(dsn string) (*sql.DB, error) {
	return sql.Open("mysql", dsn)
}

// ConfigurePool bounds the pool so bursts don't open unbounded connections
// and idle ones are recycled before MySQL's wait_timeout drops them. maxIdle
// above maxOpen is clamped down to maxOpen.
func ConfigurePool(db *sql.DB, maxOpen, maxIdle int, maxLifetime time.Duration) {
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		idle := maxIdle
		if maxOpen > 0 && idle > maxOpen {
			idle = maxOpen
		}
		db.SetMaxIdleConns(idle)
	}
	if maxLifetime > 0 {
		db.SetConnMaxLifetime(maxLifetime)
	}
}
