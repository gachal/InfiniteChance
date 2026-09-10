package canvastask

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the canvas_tasks table in the desktop's
// single app database; FinalizeSuccess shares the asset insert through the
// same transaction. Same table shape as MySQLStore's minus the MySQL engine
// clauses; timestamps are canonical TEXT (see sqlitedb).
type SQLiteStore struct {
	DB     *sql.DB
	Assets *asset.SQLiteStore
}

func NewSQLiteStore(db *sql.DB, assets *asset.SQLiteStore) *SQLiteStore {
	return &SQLiteStore{DB: db, Assets: assets}
}

// SQLiteStore 与 MySQLStore 同构实现 canvastask.Store(worker 即经此接口
// 消费,NewWorker 无需改动)。
var _ Store = (*SQLiteStore)(nil)

// 时间列说明见 canvas/store_sqlite.go:TEXT 定宽 RFC3339 纳秒 UTC,字典序
// =时间序(Claim 的 FIFO 排序依赖它);表默认值只作兜底,store 的全部写
// 入都显式给 sqlitedb.FormatTime 生成的定宽值。
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS canvas_tasks (
	id             TEXT    NOT NULL PRIMARY KEY,
	canvas_id      INTEGER NOT NULL,
	node_id        TEXT    NOT NULL,
	kind           TEXT    NOT NULL,
	prompt         TEXT    NOT NULL,
	model          TEXT    NOT NULL,
	size           TEXT    NOT NULL DEFAULT '',
	seconds        INTEGER NOT NULL DEFAULT 0,
	image_ref      TEXT    NULL,
	status         TEXT    NOT NULL,
	attempts       INTEGER NOT NULL DEFAULT 0,
	error          TEXT    NULL,
	asset_id       INTEGER NULL,
	image_url      TEXT    NULL,
	video_url      TEXT    NULL,
	remote_task_id TEXT    NOT NULL DEFAULT '',
	created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// sqliteIndexes 是 MySQL 版表内 KEY idx_… 的 SQLite 对应物,名字沿用
// MySQL 版:status 索引是 worker 的队列扫描,canvas 索引服务画布任务列表。
var sqliteIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_canvas_tasks_canvas ON canvas_tasks (canvas_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_canvas_tasks_status ON canvas_tasks (status, created_at)`,
}

// EnsureSchema creates the canvas_tasks table and its indexes when missing.
// The desktop database starts from scratch, so the MySQL-side
// information_schema column migration has no counterpart here: 新建表自带
// 全部列(视频四列已在表里)。Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
	}
	for _, ddl := range sqliteIndexes {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Create(ctx context.Context, t Task) (Task, error) {
	if t.Status == "" {
		t.Status = StatusQueued
	}
	now := sqlitedb.FormatTime(time.Now())
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvas_tasks (id, canvas_id, node_id, kind, prompt, model, size, seconds, image_ref, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.CanvasID, t.NodeID, t.Kind, t.Prompt, t.Model, t.Size,
		t.Seconds, t.ImageRef, t.Status, now, now)
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, t.ID)
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (Task, error) {
	t, err := scanSQLiteTask(s.DB.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM canvas_tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

func (s *SQLiteStore) ListByCanvas(ctx context.Context, canvasID int64, limit int) ([]Task, error) {
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
		t, err := scanSQLiteTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Claim hands one queued task to this worker: FIFO pick, state transition
// and attempt bump land as one conditional UPDATE, so racing workers each
// take a different row and no task is ever claimed twice. 与 MySQL 版同一
// 写法:SELECT 拾取 → 条件 UPDATE → 受影响行数定胜负。SQLite 的 UPDATE
// 由单写者串行化,输家的 WHERE 命不中已易主的行,RowsAffected 为 0,与
// MySQL 的 affected 计数同语义;created_at 定宽 TEXT 字典序=时间序,
// ORDER BY created_at, id 的 FIFO 顺序不变。
func (s *SQLiteStore) Claim(ctx context.Context) (Task, error) {
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
			`UPDATE canvas_tasks SET status = ?, attempts = attempts + 1, updated_at = ?
			 WHERE id = ? AND status = ?`,
			StatusRunning, sqlitedb.FormatTime(time.Now()), id, StatusQueued)
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
func (s *SQLiteStore) RequeueRunning(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, updated_at = ? WHERE status = ?`,
		StatusQueued, sqlitedb.FormatTime(time.Now()), StatusRunning)
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
func (s *SQLiteStore) FinalizeSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error) {
	return s.finalizeSuccess(ctx, id, a,
		`UPDATE canvas_tasks SET status = ?, asset_id = ?, image_url = ?, updated_at = ?
		 WHERE id = ? AND status = ?`)
}

// FinalizeVideoSuccess is the video twin: same transaction, the delivered
// address lands in video_url and the asset row carries kind video.
func (s *SQLiteStore) FinalizeVideoSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error) {
	return s.finalizeSuccess(ctx, id, a,
		`UPDATE canvas_tasks SET status = ?, asset_id = ?, video_url = ?, updated_at = ?
		 WHERE id = ? AND status = ?`)
}

// finalizeSuccess 与 MySQL 版同一事务形状:database/sql 的事务 API 两边
// 一致,SQLite 事务同样没有隐式提交 —— 终态 UPDATE 落 0 行时素材插入随
// defer Rollback 一并作废。updated_at 由写入显式带上(SQLite 没有 ON
// UPDATE CURRENT_TIMESTAMP),与 MySQL 版的行内可见行为对齐。
func (s *SQLiteStore) finalizeSuccess(ctx context.Context, id string, a asset.Asset, update string) (Task, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url, object_key, content_type, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, a.Prompt, a.URL,
		a.ObjectKey, a.ContentType, a.SizeBytes, sqlitedb.FormatTime(time.Now()))
	if err != nil {
		return Task{}, false, err
	}
	assetID, err := res.LastInsertId()
	if err != nil {
		return Task{}, false, err
	}

	res, err = tx.ExecContext(ctx, update, StatusSucceeded, assetID, a.URL,
		sqlitedb.FormatTime(time.Now()), id, StatusRunning)
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
func (s *SQLiteStore) FinalizeFailure(ctx context.Context, id, errMsg string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		StatusFailed, errMsg, sqlitedb.FormatTime(time.Now()), id, StatusRunning)
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
func (s *SQLiteStore) FinalizeCanceled(ctx context.Context, id string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		StatusCanceled, sqlitedb.FormatTime(time.Now()), id, StatusRunning)
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

// Cancel closes one active task of the canvas as canceled:guarded on
// queued/running so a worker finishing in parallel decides the winner —
// exactly one of cancel and finalize lands. The caller cancels the gateway
// task separately (the handler, before flipping this row).
func (s *SQLiteStore) Cancel(ctx context.Context, id string, canvasID int64) (Task, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, updated_at = ?
		 WHERE id = ? AND canvas_id = ? AND status IN (?, ?)`,
		StatusCanceled, sqlitedb.FormatTime(time.Now()), id, canvasID, StatusQueued, StatusRunning)
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
func (s *SQLiteStore) AttachRemote(ctx context.Context, id, remoteTaskID string) (Task, bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET remote_task_id = ?, updated_at = ? WHERE id = ? AND status = ?`,
		remoteTaskID, sqlitedb.FormatTime(time.Now()), id, StatusRunning)
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
func (s *SQLiteStore) ResetForRetry(ctx context.Context, id string, canvasID int64) (Task, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvas_tasks SET status = ?, error = NULL, asset_id = NULL, image_url = NULL, updated_at = ?
		 WHERE id = ? AND canvas_id = ? AND status = ?`,
		StatusQueued, sqlitedb.FormatTime(time.Now()), id, canvasID, StatusFailed)
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

// scanSQLiteTask 是 scanTask 的 SQLite 版:created_at/updated_at 是 TEXT,
// 读出转回 time.Time,保持与 MySQL 版相同的 Go 返回类型。
func scanSQLiteTask(row rowScanner) (Task, error) {
	var t Task
	var errMsg, imageURL, videoURL, imageRef sql.NullString
	var createdAt, updatedAt string
	var assetID sql.NullInt64
	if err := row.Scan(&t.ID, &t.CanvasID, &t.NodeID, &t.Kind, &t.Prompt, &t.Model,
		&t.Size, &t.Seconds, &imageRef, &t.Status, &t.Attempts, &errMsg, &assetID,
		&imageURL, &videoURL, &t.RemoteTaskID, &createdAt, &updatedAt); err != nil {
		return Task{}, err
	}
	t.Error = errMsg.String
	t.AssetID = assetID.Int64
	t.ImageURL = imageURL.String
	t.VideoURL = videoURL.String
	t.ImageRef = imageRef.String
	var err error
	if t.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Task{}, err
	}
	if t.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Task{}, err
	}
	return t, nil
}
