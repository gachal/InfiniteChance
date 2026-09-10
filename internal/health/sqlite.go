package health

import (
	"context"
	"database/sql"
)

// SQLite pings the desktop's single app database pool. The desktop assembly
// reports it as the "sqlite" dependency in /healthz — Redis is not part of
// the desktop wiring at all (see CONTEXT.md 桌面版).
type SQLite struct {
	DB *sql.DB
}

func (s SQLite) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }
