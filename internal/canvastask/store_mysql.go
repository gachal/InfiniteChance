package canvastask

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// MySQLStore backs Store with the canvas_tasks table; FinalizeSuccess shares
// the asset insert through the same transaction.
type MySQLStore struct {
	DB     *sql.DB
	Assets *asset.MySQLStore
}

func NewMySQLStore(db *sql.DB, assets *asset.MySQLStore) *MySQLStore {
	return &MySQLStore{DB: db, Assets: assets}
}

const schema = `
CREATE TABLE IF NOT EXISTS canvas_tasks (
	id         VARCHAR(64)  NOT NULL PRIMARY KEY,
	canvas_id  BIGINT UNSIGNED NOT NULL,
	node_id    VARCHAR(128) NOT NULL,
	kind       VARCHAR(16)  NOT NULL,
	prompt     TEXT         NOT NULL,
	model      VARCHAR(200) NOT NULL,
	size       VARCHAR(64)  NOT NULL DEFAULT '',
	status     VARCHAR(16)  NOT NULL,
	attempts   BIGINT       NOT NULL DEFAULT 0,
	error      TEXT         NULL,
	asset_id   BIGINT UNSIGNED NULL,
	image_url  MEDIUMTEXT   NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	           ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_canvas_tasks_canvas (canvas_id, created_at),
	KEY idx_canvas_tasks_status (status, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the canvas_tasks table when missing. It runs at
// canvas-server startup and is idempotent. The status index is the worker's
// queue scan; the canvas index serves the per-canvas task list.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const taskColumns = `id, canvas_id, node_id, kind, prompt, model, size, status,
	attempts, error, asset_id, image_url, created_at, updated_at`

func (s *MySQLStore) Create(ctx context.Context, t Task) (Task, error) {
	if t.Status == "" {
		t.Status = StatusQueued
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvas_tasks (id, canvas_id, node_id, kind, prompt, model, size, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.CanvasID, t.NodeID, t.Kind, t.Prompt, t.Model, t.Size, t.Status)
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, t.ID)
}

func (s *MySQLStore) Get(ctx context.Context, id string) (Task, error) {
	t, err := scanTask(s.DB.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM canvas_tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

func (s *MySQLStore) ListByCanvas(ctx context.Context, canvasID int64, limit int) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM canvas_tasks WHERE canvas_id = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		canvasID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// claimAttempts bounds one Claim call's races: the pick is a plain SELECT,
// so two workers can eye the same row; the conditional UPDATE picks the
// winner, and the loser simply retries on the next-oldest queued row. A
// losing streak this long means heavy contention — better to come back on
// the next poll tick than to spin.
const claimAttempts = 8

// Claim hands one queued task to this worker: FIFO pick, state transition
// and attempt bump land as one conditional UPDATE, so racing workers each
// take a different row and no task is ever claimed twice.
func (s *MySQLStore) Claim(ctx context.Context) (Task, error) {
	for i := 0; i < claimAttempts; i++ {
		var id string
		err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM canvas_tasks WHERE status = ? ORDER BY created_at, id LIMIT 1`,
			StatusQueued).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound // 队列为空
		}
		if err != nil {
			return Task{}, err
		}
		res, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_tasks SET status = ?, attempts = attempts + 1
			 WHERE id = ? AND status = ?`,
			StatusRunning, id, StatusQueued)
		if err != nil {
			return Task{}, err
		}
		if n, err := res.RowsAffected(); err == nil && n == 1 {
			return s.Get(ctx, id)
		}
	}
	return Task{}, ErrNotFound
}

// RequeueRunning returns every running task to the queue. It runs once at
// boot: a running row can only be orphaned by a process death mid-call, and
// the safe recovery is to run the generation again (the previous attempt's
// outcome died with the process; the gateway closed out its own books).
func (s *MySQLStore) RequeueRunning(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ? WHERE status = ?`, StatusQueued, StatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FinalizeSuccess makes the artifact and the terminal state land together:
// one transaction inserts the asset row and moves the task to succeeded with
// that asset's id. The task guard is inside the UPDATE, so a retry that
// resurrected the row between claim and finalize wins over this close-out.
func (s *MySQLStore) FinalizeSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, a.Prompt, a.URL)
	if err != nil {
		return Task{}, false, err
	}
	assetID, err := res.LastInsertId()
	if err != nil {
		return Task{}, false, err
	}

	res, err = tx.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, asset_id = ?, image_url = ?
		 WHERE id = ? AND status = ?`,
		StatusSucceeded, assetID, a.URL, id, StatusRunning)
	if err != nil {
		return Task{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, false, err
	}
	if n == 0 {
		// 终态被别人(重试复活)改走:素材一行不落,原样回滚。
		t, err := s.Get(ctx, id)
		return t, false, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, false, err
	}
	t, err := s.Get(ctx, id)
	return t, true, err
}

// FinalizeFailure closes the task failed with a reason; the guard rides in
// the UPDATE like every other transition.
func (s *MySQLStore) FinalizeFailure(ctx context.Context, id, errMsg string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, error = ? WHERE id = ? AND status = ?`,
		StatusFailed, errMsg, id, StatusRunning)
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

// ResetForRetry sends one failed task of the canvas back to the queue and
// clears the failure fields, keeping the row (and its attempt history)
// intact so the same node keeps the same task id across retries.
func (s *MySQLStore) ResetForRetry(ctx context.Context, id string, canvasID int64) (Task, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, error = NULL, asset_id = NULL, image_url = NULL
		 WHERE id = ? AND canvas_id = ? AND status = ?`,
		StatusQueued, id, canvasID, StatusFailed)
	if err != nil {
		return Task{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, err
	}
	if n == 0 {
		// 0 行:要么没有这一行,要么它不在 failed 态 —— 查一次把两者分开。
		if _, err := s.Get(ctx, id); err != nil {
			return Task{}, err // ErrNotFound
		}
		return Task{}, ErrNotRetryable
	}
	return s.Get(ctx, id)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var errMsg, imageURL sql.NullString
	var assetID sql.NullInt64
	if err := row.Scan(&t.ID, &t.CanvasID, &t.NodeID, &t.Kind, &t.Prompt, &t.Model,
		&t.Size, &t.Status, &t.Attempts, &errMsg, &assetID, &imageURL,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		return Task{}, err
	}
	t.Error = errMsg.String
	t.AssetID = assetID.Int64
	t.ImageURL = imageURL.String
	return t, nil
}
