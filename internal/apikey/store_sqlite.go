package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the api_keys and api_key_quota_log tables in
// the desktop's single app database. Same ledger semantics as MySQLStore:
// every balance change lands in the same transaction as its ledger row.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS api_keys (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	name         TEXT    NOT NULL,
	prefix       TEXT    NOT NULL,
	key_hash     TEXT    NOT NULL,
	quota_micros INTEGER NOT NULL DEFAULT 0,
	expires_at   TEXT    NULL,
	revoked_at   TEXT    NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	CONSTRAINT uniq_api_key_hash UNIQUE (key_hash)
)`

const sqliteQuotaLogSchema = `
CREATE TABLE IF NOT EXISTS api_key_quota_log (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	key_id         INTEGER NOT NULL,
	delta_micros   INTEGER NOT NULL,
	balance_micros INTEGER NOT NULL,
	reason         TEXT    NOT NULL,
	created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates both tables and the quota-log index when missing.
// 桌面库从零建表,MySQL 侧的 information_schema 迁移(expires_at 从早期
// TIMESTAMP(6) 建表改 DATETIME(6))在这边没有对应物。时间戳统一由写入方
// 用 sqlitedb.FormatTime 显式提供(默认值仅作 DDL 兜底)。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, sqliteQuotaLogSchema); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_api_key_quota_log_key ON api_key_quota_log (key_id, id)`)
	return err
}

// nowText is the canonical current timestamp bound where MySQL wrote NOW():
// 时间列是定宽 UTC TEXT,字典序即时间序,直接与列比较。
func nowText() string { return sqlitedb.FormatTime(time.Now()) }

// timePtrText renders an optional timestamp column value; nil stores SQL NULL.
func timePtrText(t *time.Time) any {
	if t == nil {
		return nil
	}
	return sqlitedb.FormatTime(*t)
}

// scanSQLiteKey maps one api_keys row. SQLite 把时间列当 TEXT 返回:可空的
// expires_at/revoked_at 先扫成 NullString 再 sqlitedb.ParseTime,保持与
// MySQL 版相同的 *time.Time 形状。
func scanSQLiteKey(scan rowScanner) (Key, error) {
	var k Key
	var expiresAt, revokedAt sql.NullString
	var createdAt, updatedAt string
	err := scan.Scan(&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &k.QuotaMicros,
		&expiresAt, &revokedAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrKeyNotFound
	}
	if err != nil {
		return Key{}, err
	}
	if expiresAt.Valid {
		t, err := sqlitedb.ParseTime(expiresAt.String)
		if err != nil {
			return Key{}, err
		}
		k.ExpiresAt = &t
	}
	if revokedAt.Valid {
		t, err := sqlitedb.ParseTime(revokedAt.String)
		if err != nil {
			return Key{}, err
		}
		k.RevokedAt = &t
	}
	if k.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Key{}, err
	}
	if k.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Key{}, err
	}
	return k, nil
}

func (s *SQLiteStore) byID(ctx context.Context, id int64) (Key, error) {
	return scanSQLiteKey(s.DB.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id))
}

func (s *SQLiteStore) Create(ctx context.Context, k Key) (Key, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Key{}, err
	}
	defer tx.Rollback()

	now := nowText()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO api_keys (name, prefix, key_hash, quota_micros, expires_at, revoked_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		k.Name, k.Prefix, k.KeyHash, k.QuotaMicros, timePtrText(k.ExpiresAt), timePtrText(k.RevokedAt), now, now)
	if err != nil {
		return Key{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Key{}, err
	}
	// 初始额度与 key 同事务落账,余额与流水永远一致。
	if k.QuotaMicros > 0 {
		if err := insertQuotaLogSQLite(ctx, tx, id, k.QuotaMicros, k.QuotaMicros, ReasonInitial); err != nil {
			return Key{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Key{}, err
	}
	return s.byID(ctx, id)
}

func (s *SQLiteStore) List(ctx context.Context) ([]Key, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		k, err := scanSQLiteKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) ByHash(ctx context.Context, hash string) (Key, error) {
	return scanSQLiteKey(s.DB.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE key_hash = ?`, hash))
}

// ByID returns the key or ErrKeyNotFound.
func (s *SQLiteStore) ByID(ctx context.Context, id int64) (Key, error) {
	return s.byID(ctx, id)
}

func (s *SQLiteStore) Revoke(ctx context.Context, id int64, at time.Time) (Key, error) {
	// COALESCE 保持幂等:重复吊销不改动首次吊销时间;updated_at 也只在
	// 首次吊销时前移,对应 MySQL「值未变化就不触发 ON UPDATE」的行为。
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE api_keys SET
			revoked_at = COALESCE(revoked_at, ?),
			updated_at = CASE WHEN revoked_at IS NULL THEN ? ELSE updated_at END
		 WHERE id = ?`, sqlitedb.FormatTime(at), nowText(), id); err != nil {
		return Key{}, err
	}
	// SQLite 的 UPDATE 计「WHERE 命中」的行,幂等重放也命中;
	// 行不存在交给 byID 判定。
	return s.byID(ctx, id)
}

func (s *SQLiteStore) TopUp(ctx context.Context, id int64, deltaMicros int64, reason string) (Key, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Key{}, err
	}
	defer tx.Rollback()

	// 活跃守卫与加钱在同一条 UPDATE 里:并发的吊销/到期不可能抢在
	// 检查与入账之间,不存在 check-then-act 窗口。MySQL 的 NOW() 对应
	// 绑定 Go 侧的规范当前时间。updated_at 随有效变更前移,对应
	// MySQL 的 ON UPDATE CURRENT_TIMESTAMP(6)。
	now := nowText()
	res, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET quota_micros = quota_micros + ?, updated_at = ?
		 WHERE id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		deltaMicros, now, id, now)
	if err != nil {
		return Key{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Key{}, err
	}
	if affected == 0 {
		// 行不存在与 key 已死是两种错误;查一次区分。
		if _, err := scanSQLiteKey(tx.QueryRowContext(ctx,
			`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id)); errors.Is(err, ErrKeyNotFound) {
			return Key{}, ErrKeyNotFound
		} else if err != nil {
			return Key{}, err
		}
		return Key{}, ErrKeyNotActive
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT quota_micros FROM api_keys WHERE id = ?`, id).Scan(&balance); err != nil {
		return Key{}, err
	}
	if err := insertQuotaLogSQLite(ctx, tx, id, deltaMicros, balance, reason); err != nil {
		return Key{}, err
	}

	stored, err := scanSQLiteKey(tx.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id))
	if err != nil {
		return Key{}, err
	}
	if err := tx.Commit(); err != nil {
		return Key{}, err
	}
	return stored, nil
}

// Reserve conditionally deducts the billing pre-deduction. The balance
// guard lives in the UPDATE's WHERE clause, so check and deduct are one
// atomic row operation: when two requests race for the last affordable
// reserve, exactly one wins and the loser sees ErrInsufficientQuota — there
// is no check-then-act window to exploit.
func (s *SQLiteStore) Reserve(ctx context.Context, id int64, amountMicros int64, reason string) (int64, error) {
	if amountMicros <= 0 {
		return 0, errors.New("apikey: reserve amount must be positive")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := nowText()
	res, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET quota_micros = quota_micros - ?, updated_at = ?
		 WHERE id = ? AND quota_micros >= ?
		   AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		amountMicros, now, id, amountMicros, now)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		// 行不存在 / key 已死 / 余额不足是三种错误;查一次区分。
		k, err := scanSQLiteKey(tx.QueryRowContext(ctx,
			`SELECT `+keyColumns+` FROM api_keys WHERE id = ?`, id))
		if errors.Is(err, ErrKeyNotFound) {
			return 0, ErrKeyNotFound
		}
		if err != nil {
			return 0, err
		}
		switch k.Status(time.Now()) {
		case StatusRevoked, StatusExpired:
			return 0, ErrKeyNotActive
		default:
			return 0, ErrInsufficientQuota
		}
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT quota_micros FROM api_keys WHERE id = ?`, id).Scan(&balance); err != nil {
		return 0, err
	}
	if err := insertQuotaLogSQLite(ctx, tx, id, -amountMicros, balance, reason); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

// Adjust applies the signed settlement delta unconditionally. There is no
// balance guard on purpose: a shortfall charge may push the balance negative
// (the response was already served; the ledger keeps the honest record), and
// a refund must land even if the key was revoked while the request ran.
func (s *SQLiteStore) Adjust(ctx context.Context, id int64, deltaMicros int64, reason string) (int64, error) {
	if deltaMicros == 0 {
		var balance int64
		if err := s.DB.QueryRowContext(ctx,
			`SELECT quota_micros FROM api_keys WHERE id = ?`, id).Scan(&balance); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrKeyNotFound
			}
			return 0, err
		}
		return balance, nil
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET quota_micros = quota_micros + ?, updated_at = ? WHERE id = ?`,
		deltaMicros, nowText(), id)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, ErrKeyNotFound
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT quota_micros FROM api_keys WHERE id = ?`, id).Scan(&balance); err != nil {
		return 0, err
	}
	if err := insertQuotaLogSQLite(ctx, tx, id, deltaMicros, balance, reason); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

func (s *SQLiteStore) QuotaLog(ctx context.Context, keyID int64, limit int) ([]QuotaEntry, error) {
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
		var createdAt string
		if err := rows.Scan(&e.ID, &e.DeltaMicros, &e.BalanceMicros, &e.Reason, &createdAt); err != nil {
			return nil, err
		}
		if e.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// insertQuotaLogSQLite appends one ledger row. created_at 由 Go 侧生成规范
// TEXT,对应 MySQL 交给 DEFAULT CURRENT_TIMESTAMP(6) 生成。
func insertQuotaLogSQLite(ctx context.Context, tx *sql.Tx, keyID, deltaMicros, balanceMicros int64, reason string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO api_key_quota_log (key_id, delta_micros, balance_micros, reason, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		keyID, deltaMicros, balanceMicros, reason, nowText())
	return err
}
