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
	id             VARCHAR(64)  NOT NULL PRIMARY KEY,
	canvas_id      BIGINT UNSIGNED NOT NULL,
	node_id        VARCHAR(128) NOT NULL,
	kind           VARCHAR(16)  NOT NULL,
	prompt         TEXT         NOT NULL,
	model          VARCHAR(200) NOT NULL,
	size           VARCHAR(64)  NOT NULL DEFAULT '',
	seconds        BIGINT       NOT NULL DEFAULT 0,
	image_ref      MEDIUMTEXT   NULL,
	status         VARCHAR(16)  NOT NULL,
	attempts       BIGINT       NOT NULL DEFAULT 0,
	error          TEXT         NULL,
	asset_id       BIGINT UNSIGNED NULL,
	image_url      MEDIUMTEXT   NULL,
	video_url      MEDIUMTEXT   NULL,
	remote_task_id VARCHAR(64)  NOT NULL DEFAULT '',
	created_at     TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at     TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	           ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_canvas_tasks_canvas (canvas_id, created_at),
	KEY idx_canvas_tasks_status (status, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// migrations widens tables created by earlier tickets in place (12 号票的
// 图生视频列):CREATE TABLE IF NOT EXISTS never widens an existing table,
// an already-deployed canvas DB must upgrade without a rebuild. Idempotent.
var migrations = []struct{ column, ddl string }{
	{"seconds", "ALTER TABLE canvas_tasks ADD COLUMN seconds BIGINT NOT NULL DEFAULT 0"},
	{"image_ref", "ALTER TABLE canvas_tasks ADD COLUMN image_ref MEDIUMTEXT NULL"},
	{"video_url", "ALTER TABLE canvas_tasks ADD COLUMN video_url MEDIUMTEXT NULL"},
	{"remote_task_id", "ALTER TABLE canvas_tasks ADD COLUMN remote_task_id VARCHAR(64) NOT NULL DEFAULT ''"},
}

// EnsureSchema creates the canvas_tasks table when missing and widens an
// existing one in place. It runs at canvas-server startup and is idempotent.
// The status index is the worker's queue scan; the canvas index serves the
// per-canvas task list.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, m := range migrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'canvas_tasks' AND COLUMN_NAME = ?`,
			m.column).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, m.ddl); err != nil {
			return err
		}
	}
	return nil
}

const taskColumns = `id, canvas_id, node_id, kind, prompt, model, size, seconds,
	image_ref, status, attempts, error, asset_id, image_url, video_url,
	remote_task_id, created_at, updated_at`

func (s *MySQLStore) Create(ctx context.Context, t Task) (Task, error) {
	if t.Status == "" {
		t.Status = StatusQueued
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvas_tasks (id, canvas_id, node_id, kind, prompt, model, size, seconds, image_ref, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.CanvasID, t.NodeID, t.Kind, t.Prompt, t.Model, t.Size,
		t.Seconds, t.ImageRef, t.Status)
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

// FinalizeSuccess makes the image artifact and the terminal state land
// together: one transaction inserts the asset row and moves the task to
// succeeded with that asset's id. The task guard is inside the UPDATE, so a
// retry that resurrected the row between claim and finalize wins over this
// close-out.
func (s *MySQLStore) FinalizeSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error) {
	return s.finalizeSuccess(ctx, id, a,
		`UPDATE canvas_tasks SET status = ?, asset_id = ?, image_url = ?
		 WHERE id = ? AND status = ?`)
}

// FinalizeVideoSuccess is the video twin: same transaction, the delivered
// address lands in video_url and the asset row carries kind video.
func (s *MySQLStore) FinalizeVideoSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error) {
	return s.finalizeSuccess(ctx, id, a,
		`UPDATE canvas_tasks SET status = ?, asset_id = ?, video_url = ?
		 WHERE id = ? AND status = ?`)
}

func (s *MySQLStore) finalizeSuccess(ctx context.Context, id string, a asset.Asset, update string) (Task, bool, error) {
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

	res, err = tx.ExecContext(ctx, update, StatusSucceeded, assetID, a.URL, id, StatusRunning)
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

// FinalizeCanceled closes a running task canceled — the worker's answer to a
// poll that observed the gateway task canceled (the HTTP handler usually got
// there first; the guard makes the loser a no-op).
func (s *MySQLStore) FinalizeCanceled(ctx context.Context, id string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ? WHERE id = ? AND status = ?`,
		StatusCanceled, id, StatusRunning)
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

// Cancel closes one active task of the canvas as canceled (12 号票):guarded
// on queued/running so a worker finishing in parallel decides the winner —
// exactly one of cancel and finalize lands. The caller cancels the gateway
// task separately (the handler, before flipping this row).
func (s *MySQLStore) Cancel(ctx context.Context, id string, canvasID int64) (Task, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?
		 WHERE id = ? AND canvas_id = ? AND status IN (?, ?)`,
		StatusCanceled, id, canvasID, StatusQueued, StatusRunning)
	if err != nil {
		return Task{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, err
	}
	if n == 0 {
		// 0 行:要么没有这一行,要么它已终态 —— 查一次把两者分开。
		if _, err := s.Get(ctx, id); err != nil {
			return Task{}, err // ErrNotFound
		}
		return Task{}, ErrNotCancelable
	}
	return s.Get(ctx, id)
}

// AttachRemote records the gateway task handle on a running video task,
// before polling starts. ok=false reports the row left running while the
// submit was in flight (the user canceled): the caller must cancel the
// just-created gateway task and let the row's canceled state stand.
func (s *MySQLStore) AttachRemote(ctx context.Context, id, remoteTaskID string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET remote_task_id = ? WHERE id = ? AND status = ?`,
		remoteTaskID, id, StatusRunning)
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
	var errMsg, imageURL, videoURL, imageRef sql.NullString
	var assetID sql.NullInt64
	if err := row.Scan(&t.ID, &t.CanvasID, &t.NodeID, &t.Kind, &t.Prompt, &t.Model,
		&t.Size, &t.Seconds, &imageRef, &t.Status, &t.Attempts, &errMsg, &assetID,
		&imageURL, &videoURL, &t.RemoteTaskID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Task{}, err
	}
	t.Error = errMsg.String
	t.AssetID = assetID.Int64
	t.ImageURL = imageURL.String
	t.VideoURL = videoURL.String
	t.ImageRef = imageRef.String
	return t, nil
}
