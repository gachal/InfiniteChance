package usage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	source            VARCHAR(255) NULL,
	created_at        TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_usage_logs_key (key_id, id),
	KEY idx_usage_logs_channel (channel_id, id),
	KEY idx_usage_logs_model (public_model, id),
	KEY idx_usage_logs_created (created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the usage_logs table when missing and widens an
// existing one in place (10 号票新增的 source 列):CREATE TABLE IF NOT EXISTS
// 永远不会加宽老表,网关要能对已有库原地升级。幂等。The composite indexes
// serve ticket 15's per-key/channel/model filters and the time-range scans.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	var count int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'usage_logs' AND COLUMN_NAME = 'source'`,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "ALTER TABLE usage_logs ADD COLUMN source VARCHAR(255) NULL")
	return err
}

func (s *MySQLStore) Insert(ctx context.Context, l Log) (Log, error) {
	var snapshot, upstreamErr, source any
	if len(l.PriceSnapshot) > 0 {
		snapshot = l.PriceSnapshot
	}
	if l.UpstreamError != "" {
		upstreamErr = l.UpstreamError
	}
	if l.Source != "" {
		source = l.Source
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO usage_logs
			(key_id, channel_id, channel_name, public_model, upstream_model, unit,
			 prompt_tokens, completion_tokens, duration_ms, status, charge_micros,
			 price_snapshot, upstream_error, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.KeyID, l.ChannelID, l.ChannelName, l.PublicModel, l.UpstreamModel, l.Unit,
		l.PromptTokens, l.CompletionTokens, l.DurationMS, l.Status, l.ChargeMicros,
		snapshot, upstreamErr, source)
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

// selectColumns is the shared projection for List's row scans.
const selectColumns = `id, key_id, channel_id, channel_name, public_model, upstream_model,
	unit, prompt_tokens, completion_tokens, duration_ms, status, charge_micros,
	price_snapshot, upstream_error, source, created_at`

// where compiles f into the WHERE clause shared by List and Summary: the
// aggregations reconcile against the log list because both run the exact
// same predicates.
func where(f Filter) (string, []any) {
	conds := []string{"TRUE"}
	var args []any
	if f.From != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, *f.From)
	}
	if f.To != nil {
		conds = append(conds, "created_at < ?")
		args = append(args, *f.To)
	}
	if f.KeyID > 0 {
		conds = append(conds, "key_id = ?")
		args = append(args, f.KeyID)
	}
	if f.ChannelID > 0 {
		conds = append(conds, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.Model != "" {
		conds = append(conds, "public_model = ?")
		args = append(args, f.Model)
	}
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, f.Status)
	}
	// 画布标记恒以 "canvas=" 开头(10 号票),空串即直连流量;标记是调用方
	// 自报的注记,这里只按前缀归类。
	switch f.Source {
	case SourceCanvas:
		conds = append(conds, "source LIKE ?")
		args = append(args, SourceCanvas+"=%")
	case SourceDirect:
		conds = append(conds, "(source IS NULL OR source = '')")
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// List pages the trail newest-first. Total counts the whole filtered set,
// so the UI can size the pagination from one response.
func (s *MySQLStore) List(ctx context.Context, f Filter, limit, offset int) (Page, error) {
	whereSQL, args := where(f)

	var total int64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_logs`+whereSQL, args...).Scan(&total); err != nil {
		return Page{}, err
	}

	query := `SELECT ` + selectColumns + ` FROM usage_logs` + whereSQL +
		` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.DB.QueryContext(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Logs: []Log{}, Total: total}
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return Page{}, err
		}
		page.Logs = append(page.Logs, l)
	}
	return page, rows.Err()
}

// Summary groups the trail by one dimension under the same predicates as
// List. 按天的新到旧、按模型/渠道的扣费降序,桶间并列各按名字/id 稳定排序;
// 渠道桶按 channel_id 归一(渠道改名不分桶),名字取行内快照的字典序最大值
// —— 只是展示标签,对账以 id 为准。自然日按库会话时区取整(compose 里的
// MySQL 默认 UTC)。
func (s *MySQLStore) Summary(ctx context.Context, d Dimension, f Filter) ([]Bucket, error) {
	whereSQL, args := where(f)
	// SUM(布尔) 在 MySQL 里即条件计数;空集时 COALESCE 兜 0。status 谓词的
	// 参数在 SELECT 列表里,先于 WHERE 的占位符。
	facts := `COUNT(*) AS requests, COALESCE(SUM(status = ?), 0) AS errors,` +
		` COALESCE(SUM(charge_micros), 0) AS charge`
	var query string
	switch d {
	case ByDay:
		query = `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS day, ` + facts +
			` FROM usage_logs` + whereSQL + ` GROUP BY day ORDER BY day DESC`
	case ByModel:
		query = `SELECT public_model, ` + facts +
			` FROM usage_logs` + whereSQL + ` GROUP BY public_model ORDER BY charge DESC, public_model ASC`
	case ByChannel:
		query = `SELECT channel_id, MAX(channel_name) AS channel_name, ` + facts +
			` FROM usage_logs` + whereSQL + ` GROUP BY channel_id ORDER BY charge DESC, channel_id ASC`
	default:
		return nil, fmt.Errorf("usage: unknown summary dimension %q", d)
	}
	full := append([]any{StatusUpstreamError}, args...)
	rows, err := s.DB.QueryContext(ctx, query, full...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := []Bucket{}
	for rows.Next() {
		var b Bucket
		var dest []any
		switch d {
		case ByDay:
			dest = []any{&b.Day}
		case ByModel:
			dest = []any{&b.Model}
		case ByChannel:
			dest = []any{&b.ChannelID, &b.ChannelName}
		}
		if err := rows.Scan(append(dest, &b.Requests, &b.Errors, &b.ChargeMicros)...); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

func scanLog(scanner interface{ Scan(dest ...any) error }) (Log, error) {
	var l Log
	var snapshot, upstreamErr, source sql.NullString
	if err := scanner.Scan(&l.ID, &l.KeyID, &l.ChannelID, &l.ChannelName, &l.PublicModel,
		&l.UpstreamModel, &l.Unit, &l.PromptTokens, &l.CompletionTokens, &l.DurationMS,
		&l.Status, &l.ChargeMicros, &snapshot, &upstreamErr, &source, &l.CreatedAt); err != nil {
		return Log{}, err
	}
	if snapshot.Valid {
		l.PriceSnapshot = []byte(snapshot.String)
	}
	l.UpstreamError = upstreamErr.String
	l.Source = source.String
	return l, nil
}
