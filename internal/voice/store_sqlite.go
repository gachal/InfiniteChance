package voice

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the voices table in the desktop's single app
// database (57 号票)。时间列说明见 canvas 包 store_sqlite.go:定宽 TEXT,
// 写入显式给 FormatTime。
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

// SQLiteStore 与 MySQLStore 同构实现 voice.Store。
var _ Store = (*SQLiteStore)(nil)

// 表与 MySQL 版同形(SQLite 方言)。voice_id 的唯一索引按双方言纪律落
// 独立 CREATE UNIQUE INDEX(MySQL 是表内 UNIQUE KEY,两边索引名一致 ——
// EnsureSchema 文档同款承诺)。
const sqliteVoicesSchema = `
CREATE TABLE IF NOT EXISTS voices (
	id               INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	voice_id         TEXT    NOT NULL,
	channel_id       INTEGER NOT NULL,
	target_model     TEXT    NOT NULL,
	name             TEXT    NOT NULL,
	source_mode      TEXT    NOT NULL,
	source_prompt    TEXT    NULL,
	sample_asset_id  INTEGER NULL,
	preview_asset_id INTEGER NULL,
	status           TEXT    NOT NULL,
	created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

var sqliteIndexes = []string{
	`CREATE UNIQUE INDEX IF NOT EXISTS uk_voices_voice_id ON voices (voice_id)`,
}

// EnsureSchema creates the voices table and its unique index when missing.
// Idempotent; 空起步无迁移。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteVoicesSchema); err != nil {
		return err
	}
	for _, ddl := range sqliteIndexes {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}

const sqliteVoiceColumns = `id, voice_id, channel_id, target_model, name, source_mode,
	source_prompt, sample_asset_id, preview_asset_id, status, created_at, updated_at`

// scanSQLiteVoiceRow 按列序扫描一行,TEXT 时间经 ParseTime 还原。
func scanSQLiteVoiceRow(row interface{ Scan(...any) error }) (Voice, error) {
	var v Voice
	var sourcePrompt sql.NullString
	var sampleAssetID, previewAssetID sql.NullInt64
	var createdAt, updatedAt string
	if err := row.Scan(&v.ID, &v.VoiceID, &v.ChannelID, &v.TargetModel, &v.Name,
		&v.SourceMode, &sourcePrompt, &sampleAssetID, &previewAssetID, &v.Status,
		&createdAt, &updatedAt); err != nil {
		return Voice{}, err
	}
	v.SourcePrompt = sourcePrompt.String
	v.SampleAssetID = sampleAssetID.Int64
	v.PreviewAssetID = previewAssetID.Int64
	var err error
	if v.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Voice{}, err
	}
	if v.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Voice{}, err
	}
	return v, nil
}

func (s *SQLiteStore) Create(ctx context.Context, in CreateInput) (Voice, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO voices (voice_id, channel_id, target_model, name, source_mode, source_prompt, sample_asset_id, preview_asset_id, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.VoiceID, in.ChannelID, in.TargetModel, in.Name, in.SourceMode,
		nullString(in.SourcePrompt), nullInt64(in.SampleAssetID), nullInt64(in.PreviewAssetID), in.Status,
		now, now)
	if err != nil {
		return Voice{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Voice{}, err
	}
	return s.Get(ctx, id)
}

func (s *SQLiteStore) List(ctx context.Context) ([]Voice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sqliteVoiceColumns+`
		FROM voices ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Voice
	for rows.Next() {
		v, err := scanSQLiteVoiceRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Voice, error) {
	v, err := scanSQLiteVoiceRow(s.DB.QueryRowContext(ctx,
		`SELECT `+sqliteVoiceColumns+` FROM voices WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Voice{}, ErrVoiceNotFound
	}
	return v, err
}

// Update applies the non-nil fields; 先探存在,updated_at 显式带上
// (SQLite 没有 ON UPDATE CURRENT_TIMESTAMP)。
func (s *SQLiteStore) Update(ctx context.Context, id int64, u *VoiceUpdate) (Voice, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Voice{}, err
	}
	sets := []string{"updated_at = ?"}
	args := []any{sqlitedb.FormatTime(time.Now())}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE voices SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Voice{}, err
	}
	return s.Get(ctx, id)
}

// BoundCharacters counts character rows binding this voice (只读跨表)。
func (s *SQLiteStore) BoundCharacters(ctx context.Context, id int64) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM characters WHERE voice_id = ?`, id).Scan(&count)
	return count, err
}

// Delete refuses while a character still binds the voice;上游删除由
// handler 先行。
func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if bound, err := s.BoundCharacters(ctx, id); err != nil {
		return err
	} else if bound > 0 {
		return ErrVoiceInUse
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM voices WHERE id = ?`, id)
	return err
}
