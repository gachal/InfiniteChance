package videotask

import (
	"context"
	"database/sql"
)

// MySQLStore backs Store with the video_tasks table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS video_tasks (
	id               VARCHAR(64)  NOT NULL PRIMARY KEY,
	key_id           BIGINT UNSIGNED NOT NULL,
	channel_id       BIGINT UNSIGNED NOT NULL,
	channel_name     VARCHAR(64)  NOT NULL,
	public_model     VARCHAR(200) NOT NULL,
	upstream_model   VARCHAR(200) NOT NULL,
	upstream_task_id VARCHAR(255) NOT NULL,
	status           VARCHAR(16)  NOT NULL,
	upstream_status  VARCHAR(64)  NULL,
	size             VARCHAR(64)  NOT NULL DEFAULT '',
	seconds          BIGINT       NOT NULL DEFAULT 0,
	video_url        TEXT         NULL,
	error            TEXT         NULL,
	reserved_micros  BIGINT       NOT NULL DEFAULT 0,
	charge_micros    BIGINT       NOT NULL DEFAULT 0,
	price_snapshot   JSON NULL,
	created_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	                 ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_video_tasks_key (key_id, id),
	KEY idx_video_tasks_status (status, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the video_tasks table when missing. It runs at
// gateway startup and is idempotent. The key index serves per-key task
// listings, the status index terminal-state sweeps (ticket 15's audit
// views build on both).
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const taskColumns = `id, key_id, channel_id, channel_name, public_model, upstream_model,
	upstream_task_id, status, upstream_status, size, seconds, video_url, error,
	reserved_micros, charge_micros, price_snapshot, created_at, updated_at`

func (s *MySQLStore) Create(ctx context.Context, t Task) (Task, error) {
	var snapshot, errMsg any
	if len(t.PriceSnapshot) > 0 {
		snapshot = t.PriceSnapshot
	}
	if t.Error != "" {
		errMsg = t.Error
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO video_tasks
			(id, key_id, channel_id, channel_name, public_model, upstream_model,
			 upstream_task_id, status, upstream_status, size, seconds,
			 reserved_micros, charge_micros, price_snapshot, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.KeyID, t.ChannelID, t.ChannelName, t.PublicModel, t.UpstreamModel,
		t.UpstreamTaskID, t.Status, t.UpstreamStatus, t.Size, t.Seconds,
		t.ReservedMicros, t.ChargeMicros, snapshot, errMsg)
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, t.ID)
}

func (s *MySQLStore) Get(ctx context.Context, id string) (Task, error) {
	t, err := s.scanTask(s.DB.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM video_tasks WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Task{}, ErrNotFound
	}
	return t, err
}

// Update applies patch under a status guard that is part of the UPDATE
// itself — the WHERE clause decides the winner, not the read that preceded
// it. RowsAffected 0 means the task already left the expected states: the
// current row comes back with won=false, and the caller must not bill.
func (s *MySQLStore) Update(ctx context.Context, id string, expect []Status, p Patch) (Task, bool, error) {
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
			charge_micros = COALESCE(?, charge_micros)
		 WHERE id = ? AND status IN (` + placeholders(len(expect)) + `)`
	args := []any{p.Status, upstreamStatus, videoURL, errMsg, charge, id}
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

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func (s *MySQLStore) scanTask(row *sql.Row) (Task, error) {
	var t Task
	var upstreamStatus, videoURL, errMsg sql.NullString
	var snapshot []byte
	if err := row.Scan(&t.ID, &t.KeyID, &t.ChannelID, &t.ChannelName, &t.PublicModel,
		&t.UpstreamModel, &t.UpstreamTaskID, &t.Status, &upstreamStatus, &t.Size,
		&t.Seconds, &videoURL, &errMsg, &t.ReservedMicros, &t.ChargeMicros,
		&snapshot, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Task{}, err
	}
	t.UpstreamStatus = upstreamStatus.String
	t.VideoURL = videoURL.String
	t.Error = errMsg.String
	t.PriceSnapshot = snapshot
	return t, nil
}
