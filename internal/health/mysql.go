package health

import (
	"context"
	"database/sql"

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
