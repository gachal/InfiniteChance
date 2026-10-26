package voice

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// MySQLStore backs Store with the voices table on the canvas database
// (57 号票)。时间列靠表默认值(CURRENT_TIMESTAMP(6) + ON UPDATE),读回
// 即 time.Time;被引检查跨读同库的 characters 表(只读,canvastask 读
// assets 的同款先例)。
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

// MySQLStore 与 SQLiteStore 同构实现 voice.Store。
var _ Store = (*MySQLStore)(nil)

// 空起步(57 号票):voice_id 唯一索引落表内 UNIQUE KEY(MySQL 形态;
// SQLite 侧用独立 CREATE UNIQUE INDEX,两边索引名一致)。status 存上游
// 原样态字;source_prompt 可空(design 的描述 / clone 的样本地址出处)。
const voicesSchema = `
CREATE TABLE IF NOT EXISTS voices (
	id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	voice_id         VARCHAR(255) NOT NULL,
	channel_id       BIGINT UNSIGNED NOT NULL,
	target_model     VARCHAR(200) NOT NULL,
	name             VARCHAR(255) NOT NULL,
	source_mode      VARCHAR(16)  NOT NULL,
	source_prompt    TEXT         NULL,
	sample_asset_id  BIGINT UNSIGNED NULL,
	preview_asset_id BIGINT UNSIGNED NULL,
	status           VARCHAR(16)  NOT NULL,
	created_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	UNIQUE KEY uk_voices_voice_id (voice_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the voices table when missing. Idempotent; 空起步
// 无迁移(存量部署里 57 号票之前没有 voices 表)。
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, voicesSchema)
	return err
}

const mysqlVoiceColumns = `id, voice_id, channel_id, target_model, name, source_mode,
	source_prompt, sample_asset_id, preview_asset_id, status, created_at, updated_at`

func scanMySQLVoice(row interface{ Scan(...any) error }) (Voice, error) {
	var v Voice
	var sourcePrompt sql.NullString
	var sampleAssetID, previewAssetID sql.NullInt64
	err := row.Scan(&v.ID, &v.VoiceID, &v.ChannelID, &v.TargetModel, &v.Name,
		&v.SourceMode, &sourcePrompt, &sampleAssetID, &previewAssetID, &v.Status,
		&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return Voice{}, err
	}
	v.SourcePrompt = sourcePrompt.String
	v.SampleAssetID = sampleAssetID.Int64
	v.PreviewAssetID = previewAssetID.Int64
	return v, nil
}

func (s *MySQLStore) Create(ctx context.Context, in CreateInput) (Voice, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO voices (voice_id, channel_id, target_model, name, source_mode, source_prompt, sample_asset_id, preview_asset_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.VoiceID, in.ChannelID, in.TargetModel, in.Name, in.SourceMode,
		nullString(in.SourcePrompt), nullInt64(in.SampleAssetID), nullInt64(in.PreviewAssetID), in.Status)
	if err != nil {
		return Voice{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Voice{}, err
	}
	return s.Get(ctx, id)
}

func (s *MySQLStore) List(ctx context.Context) ([]Voice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mysqlVoiceColumns+`
		FROM voices ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Voice
	for rows.Next() {
		v, err := scanMySQLVoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Voice, error) {
	v, err := scanMySQLVoice(s.DB.QueryRowContext(ctx,
		`SELECT `+mysqlVoiceColumns+` FROM voices WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Voice{}, ErrVoiceNotFound
	}
	return v, err
}

// Update applies the non-nil fields; 先探存在(MySQL 同值更新算 0 affected,
// 缺失语义靠探活路径对齐)。updated_at 由 ON UPDATE 自动推进。
func (s *MySQLStore) Update(ctx context.Context, id int64, u *VoiceUpdate) (Voice, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Voice{}, err
	}
	sets := []string{}
	args := []any{}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if len(sets) == 0 {
		return s.Get(ctx, id)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE voices SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Voice{}, err
	}
	return s.Get(ctx, id)
}

// BoundCharacters counts character rows binding this voice (只读跨表,
// voices 删除防护的数据源)。
func (s *MySQLStore) BoundCharacters(ctx context.Context, id int64) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM characters WHERE voice_id = ?`, id).Scan(&count)
	return count, err
}

// Delete refuses while a character still binds the voice(ErrVoiceInUse
// —— 解绑后才可删,不级联解绑);上游删除由 handler 先行。
func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
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

// nullString / nullInt64 keep the nullable columns honest:0 与空串落 NULL,
// 读回时归零,行内不会出现「0 号素材」这类假引用。
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
