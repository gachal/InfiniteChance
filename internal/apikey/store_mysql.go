package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MySQLStore backs Store with the api_keys and api_key_quota_log tables.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS api_keys (
	id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name         VARCHAR(64) NOT NULL,
	prefix       VARCHAR(16) NOT NULL,
	key_hash     CHAR(64)    NOT NULL,
	quota_micros BIGINT      NOT NULL DEFAULT 0,
	expires_at   TIMESTAMP(6) NULL,
	revoked_at   TIMESTAMP(6) NULL,
	created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	UNIQUE KEY uniq_api_key_hash (key_hash)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

const quotaLogSchema = `
CREATE TABLE IF NOT EXISTS api_key_quota_log (
	id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	key_id         BIGINT UNSIGNED NOT NULL,
	delta_micros   BIGINT      NOT NULL,
	balance_micros BIGINT      NOT NULL,
	reason         VARCHAR(64) NOT NULL,
	created_at     TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_api_key_quota_log_key (key_id, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates both tables when missing. It runs at gateway startup
// and is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, quotaLogSchema)
	return err
}

const keyColumns = `id, name, prefix, key_hash, quota_micros, expires_at, revoked_at, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(scan rowScanner) (Key, error) {
	var k Key
	var expiresAt, revokedAt sql.NullTime
	err := scan.Scan(&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &k.QuotaMicros,
		&expiresAt, &revokedAt, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrKeyNotFound
	}
	if err != nil {
		return Key{}, err
	}
	if expiresAt.Valid {
		k.ExpiresAt = &expiresAt.Time
	}
	if revokedAt.Valid {
		k.RevokedAt = &revokedAt.Time
	}
	return k, nil
}

func (s *MySQLStore) byID(ctx context.Context, id int64) (Key, error) {
	return scanKey(s.DB.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id))
}

func (s *MySQLStore) Create(ctx context.Context, k Key) (Key, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Key{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO api_keys (name, prefix, key_hash, quota_micros, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.Name, k.Prefix, k.KeyHash, k.QuotaMicros, k.ExpiresAt, k.RevokedAt)
	if err != nil {
		return Key{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Key{}, err
	}
	// 初始额度与 key 同事务落账,余额与流水永远一致。
	if k.QuotaMicros > 0 {
		if err := insertQuotaLog(ctx, tx, id, k.QuotaMicros, k.QuotaMicros, ReasonInitial); err != nil {
			return Key{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Key{}, err
	}
	return s.byID(ctx, id)
}

func (s *MySQLStore) List(ctx context.Context) ([]Key, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *MySQLStore) ByHash(ctx context.Context, hash string) (Key, error) {
	return scanKey(s.DB.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE key_hash = ?`, hash))
}

// ByID returns the key or ErrKeyNotFound.
func (s *MySQLStore) ByID(ctx context.Context, id int64) (Key, error) {
	return s.byID(ctx, id)
}

func (s *MySQLStore) Revoke(ctx context.Context, id int64, at time.Time) (Key, error) {
	// COALESCE 保持幂等:重复吊销不改动首次吊销时间。
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, at, id); err != nil {
		return Key{}, err
	}
	// MySQL 只计「被更改」的行:0 可能是行不存在,也可能是已吊销
	// key 的幂等重放,统一交给 byID 判定。
	return s.byID(ctx, id)
}

func (s *MySQLStore) TopUp(ctx context.Context, id int64, deltaMicros int64, reason string) (Key, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Key{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET quota_micros = quota_micros + ? WHERE id = ?`, deltaMicros, id)
	if err != nil {
		return Key{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Key{}, err
	}
	if affected == 0 {
		return Key{}, ErrKeyNotFound
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT quota_micros FROM api_keys WHERE id = ?`, id).Scan(&balance); err != nil {
		return Key{}, err
	}
	if err := insertQuotaLog(ctx, tx, id, deltaMicros, balance, reason); err != nil {
		return Key{}, err
	}

	stored, err := scanKey(tx.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id))
	if err != nil {
		return Key{}, err
	}
	if err := tx.Commit(); err != nil {
		return Key{}, err
	}
	return stored, nil
}

func (s *MySQLStore) QuotaLog(ctx context.Context, keyID int64, limit int) ([]QuotaEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, delta_micros, balance_micros, reason, created_at
		 FROM api_key_quota_log WHERE key_id = ? ORDER BY id DESC LIMIT ?`,
		keyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []QuotaEntry
	for rows.Next() {
		var e QuotaEntry
		if err := rows.Scan(&e.ID, &e.DeltaMicros, &e.BalanceMicros, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func insertQuotaLog(ctx context.Context, tx *sql.Tx, keyID, deltaMicros, balanceMicros int64, reason string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO api_key_quota_log (key_id, delta_micros, balance_micros, reason)
		 VALUES (?, ?, ?, ?)`,
		keyID, deltaMicros, balanceMicros, reason)
	return err
}
