package usage

import (
	"context"
	"database/sql"
)

// MySQLStore backs Store with the usage_logs table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS usage_logs (
	id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	key_id            BIGINT UNSIGNED NOT NULL,
	channel_id        BIGINT UNSIGNED NOT NULL,
	channel_name      VARCHAR(64)  NOT NULL,
	public_model      VARCHAR(200) NOT NULL,
	upstream_model    VARCHAR(200) NOT NULL,
	unit              VARCHAR(16)  NOT NULL,
	prompt_tokens     BIGINT      NOT NULL DEFAULT 0,
	completion_tokens BIGINT      NOT NULL DEFAULT 0,
	duration_ms       BIGINT      NOT NULL DEFAULT 0,
	status            VARCHAR(32) NOT NULL,
	charge_micros     BIGINT      NOT NULL DEFAULT 0,
	price_snapshot    JSON NULL,
	upstream_error    TEXT NULL,
	created_at        TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_usage_logs_key (key_id, id),
	KEY idx_usage_logs_channel (channel_id, id),
	KEY idx_usage_logs_model (public_model, id),
	KEY idx_usage_logs_created (created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the usage_logs table when missing. It runs at gateway
// startup and is idempotent. The composite indexes serve ticket 15's
// per-key/channel/model filters and the time-range scans.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

func (s *MySQLStore) Insert(ctx context.Context, l Log) (Log, error) {
	var snapshot, upstreamErr any
	if len(l.PriceSnapshot) > 0 {
		snapshot = l.PriceSnapshot
	}
	if l.UpstreamError != "" {
		upstreamErr = l.UpstreamError
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO usage_logs
			(key_id, channel_id, channel_name, public_model, upstream_model, unit,
			 prompt_tokens, completion_tokens, duration_ms, status, charge_micros,
			 price_snapshot, upstream_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.KeyID, l.ChannelID, l.ChannelName, l.PublicModel, l.UpstreamModel, l.Unit,
		l.PromptTokens, l.CompletionTokens, l.DurationMS, l.Status, l.ChargeMicros,
		snapshot, upstreamErr)
	if err != nil {
		return Log{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Log{}, err
	}
	l.ID = id
	if err := s.DB.QueryRowContext(ctx,
		`SELECT created_at FROM usage_logs WHERE id = ?`, id).Scan(&l.CreatedAt); err != nil {
		return Log{}, err
	}
	return l, nil
}
