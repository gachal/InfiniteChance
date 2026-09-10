package videotask

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the video_tasks table in the desktop's single
// app database. Same table shape and transition semantics as MySQLStore's,
// minus the MySQL dialect: timestamps are canonical fixed-width RFC3339 UTC
// TEXT (see sqlitedb), price_snapshot is stored verbatim as TEXT.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS video_tasks (
	id               TEXT         NOT NULL PRIMARY KEY,
	key_id           INTEGER      NOT NULL,
	channel_id       INTEGER      NOT NULL,
	channel_name     TEXT         NOT NULL,
	public_model     TEXT         NOT NULL,
	upstream_model   TEXT         NOT NULL,
	upstream_task_id TEXT         NOT NULL,
	status           TEXT         NOT NULL,
	upstream_status  TEXT NULL,
	size             TEXT         NOT NULL DEFAULT '',
	seconds          INTEGER      NOT NULL DEFAULT 0,
	video_url        TEXT NULL,
	error            TEXT NULL,
	reserved_micros  INTEGER      NOT NULL DEFAULT 0,
	charge_micros    INTEGER      NOT NULL DEFAULT 0,
	price_snapshot   TEXT NULL,
	created_at       TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at       TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX IF NOT EXISTS idx_video_tasks_key ON video_tasks (key_id, id);
CREATE INDEX IF NOT EXISTS idx_video_tasks_status ON video_tasks (status, created_at)`

// EnsureSchema creates the video_tasks table and its indexes when missing.
// 桌面库从零建起,MySQL 侧没有要迁移的老形态。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLiteStore) Create(ctx context.Context, t Task) (Task, error) {
	var snapshot, errMsg any
	if len(t.PriceSnapshot) > 0 {
		snapshot = string(t.PriceSnapshot) // JSON 列按 TEXT 原样存
	}
	if t.Error != "" {
		errMsg = t.Error
	}
	// MySQL 版靠 DEFAULT CURRENT_TIMESTAMP 与 ON UPDATE 打点;SQLite 的
	// strftime 默认值只有 6 位小数秒,ParseTime 读不回,时间戳一律由 Go
	// 侧以定宽 FormatTime 显式写入(创建时两个时间戳同值)。
	stamped := sqlitedb.FormatTime(time.Now())
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO video_tasks
			(id, key_id, channel_id, channel_name, public_model, upstream_model,
			 upstream_task_id, status, upstream_status, size, seconds,
			 reserved_micros, charge_micros, price_snapshot, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.KeyID, t.ChannelID, t.ChannelName, t.PublicModel, t.UpstreamModel,
		t.UpstreamTaskID, t.Status, t.UpstreamStatus, t.Size, t.Seconds,
		t.ReservedMicros, t.ChargeMicros, snapshot, errMsg, stamped, stamped)
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, t.ID)
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (Task, error) {
	t, err := s.scanTask(s.DB.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM video_tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

// Update applies patch under a status guard that is part of the UPDATE
// itself — the WHERE clause decides the winner, not the read that preceded
// it. SQLite 的 RowsAffected 计「WHERE 命中」的行(MySQL 只计「被更改」),
// 0 在这里严格等于任务已离开 expect 状态:竞态判定语义不变,胜者才计费。
// updated_at 显式刷新,补上 MySQL ON UPDATE CURRENT_TIMESTAMP 的语义。
func (s *SQLiteStore) Update(ctx context.Context, id string, expect []Status, p Patch) (Task, bool, error) {
	var upstreamStatus, videoURL, errMsg, charge any
	if p.UpstreamStatus != nil {
		upstreamStatus = *p.UpstreamStatus
	}
	if p.VideoURL != nil {
		videoURL = *p.VideoURL
	}
	if p.ErrMsg != nil {
		errMsg = *p.ErrMsg
	}
	if p.ChargeMicros != nil {
		charge = *p.ChargeMicros
	}
	query := `UPDATE video_tasks SET
			status = ?,
			upstream_status = COALESCE(?, upstream_status),
			video_url = COALESCE(?, video_url),
			error = COALESCE(?, error),
			charge_micros = COALESCE(?, charge_micros),
			updated_at = ?
		 WHERE id = ? AND status IN (` + placeholders(len(expect)) + `)`
	args := []any{p.Status, upstreamStatus, videoURL, errMsg, charge,
		sqlitedb.FormatTime(time.Now()), id}
	for _, st := range expect {
		args = append(args, string(st))
	}
	res, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return Task{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, false, err
	}
	if n == 0 {
		t, err := s.Get(ctx, id)
		return t, false, err
	}
	t, err := s.Get(ctx, id)
	return t, true, err
}

func (s *SQLiteStore) scanTask(row *sql.Row) (Task, error) {
	var t Task
	var upstreamStatus, videoURL, errMsg sql.NullString
	var snapshot, createdAt, updatedAt string
	if err := row.Scan(&t.ID, &t.KeyID, &t.ChannelID, &t.ChannelName, &t.PublicModel,
		&t.UpstreamModel, &t.UpstreamTaskID, &t.Status, &upstreamStatus, &t.Size,
		&t.Seconds, &videoURL, &errMsg, &t.ReservedMicros, &t.ChargeMicros,
		&snapshot, &createdAt, &updatedAt); err != nil {
		return Task{}, err
	}
	t.UpstreamStatus = upstreamStatus.String
	t.VideoURL = videoURL.String
	t.Error = errMsg.String
	if len(snapshot) > 0 {
		t.PriceSnapshot = []byte(snapshot)
	}
	var err error
	if t.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Task{}, err
	}
	if t.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Task{}, err
	}
	return t, nil
}
