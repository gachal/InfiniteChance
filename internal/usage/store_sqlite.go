package usage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the usage_logs table in the desktop's single
// app database. Same table shape and query semantics as MySQLStore's, minus
// the MySQL dialect: timestamps are canonical fixed-width RFC3339 UTC TEXT
// (see sqlitedb), price_snapshot is stored verbatim as TEXT.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS usage_logs (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	key_id            INTEGER      NOT NULL,
	channel_id        INTEGER      NOT NULL,
	channel_name      TEXT         NOT NULL,
	public_model      TEXT         NOT NULL,
	upstream_model    TEXT         NOT NULL,
	unit              TEXT         NOT NULL,
	prompt_tokens     INTEGER      NOT NULL DEFAULT 0,
	completion_tokens INTEGER      NOT NULL DEFAULT 0,
	duration_ms       INTEGER      NOT NULL DEFAULT 0,
	status            TEXT         NOT NULL,
	charge_micros     INTEGER      NOT NULL DEFAULT 0,
	price_snapshot    TEXT NULL,
	upstream_error    TEXT NULL,
	source            TEXT NULL,
	created_at        TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX IF NOT EXISTS idx_usage_logs_key ON usage_logs (key_id, id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_channel ON usage_logs (channel_id, id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_model ON usage_logs (public_model, id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_created ON usage_logs (created_at)`

// EnsureSchema creates the usage_logs table and its indexes when missing.
// 桌面库从零建起(表内自带 source 列与全部索引),MySQL 侧基于
// information_schema 的 source 列加宽迁移在这里没有对应物。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLiteStore) Insert(ctx context.Context, l Log) (Log, error) {
	var snapshot, upstreamErr, source any
	if len(l.PriceSnapshot) > 0 {
		snapshot = string(l.PriceSnapshot) // JSON 列按 TEXT 原样存
	}
	if l.UpstreamError != "" {
		upstreamErr = l.UpstreamError
	}
	if l.Source != "" {
		source = l.Source
	}
	// created_at 由 Go 侧打点、显式写入:列上的 strftime 默认值只有 6 位
	// 小数秒,sqlitedb.ParseTime 读不回,时间一律用定宽 FormatTime。
	created := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO usage_logs
			(key_id, channel_id, channel_name, public_model, upstream_model, unit,
			 prompt_tokens, completion_tokens, duration_ms, status, charge_micros,
			 price_snapshot, upstream_error, source, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.KeyID, l.ChannelID, l.ChannelName, l.PublicModel, l.UpstreamModel, l.Unit,
		l.PromptTokens, l.CompletionTokens, l.DurationMS, l.Status, l.ChargeMicros,
		snapshot, upstreamErr, source, created)
	if err != nil {
		return Log{}, err
	}
	l.ID, err = res.LastInsertId()
	if err != nil {
		return Log{}, err
	}
	l.CreatedAt, err = sqlitedb.ParseTime(created)
	if err != nil {
		return Log{}, err
	}
	return l, nil
}

// sqliteWhere compiles f into the WHERE clause shared by List and Summary:
// the aggregations reconcile against the log list because both run the exact
// same predicates. 与 MySQL 版同一批谓词;时间端点转成定宽 UTC TEXT 后做
// 字符串比较——定宽 RFC3339 的字典序即时间序,>= / < 语义不变。
func sqliteWhere(f Filter) (string, []any) {
	conds := []string{"TRUE"}
	var args []any
	if f.From != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, sqlitedb.FormatTime(*f.From))
	}
	if f.To != nil {
		conds = append(conds, "created_at < ?")
		args = append(args, sqlitedb.FormatTime(*f.To))
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
func (s *SQLiteStore) List(ctx context.Context, f Filter, limit, offset int) (Page, error) {
	whereSQL, args := sqliteWhere(f)

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
		l, err := scanSQLiteLog(rows)
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
// —— 只是展示标签,对账以 id 为准。自然日按 UTC 取整:date() 直接吃定宽
// RFC3339 UTC TEXT,与 MySQL 版「会话时区(compose 默认 UTC)的自然日」
// 语义一致。
func (s *SQLiteStore) Summary(ctx context.Context, d Dimension, f Filter) ([]Bucket, error) {
	whereSQL, args := sqliteWhere(f)
	// 比较式 SUM(status = ?) 在 SQLite 里同样是 0/1 条件计数;空集时
	// COALESCE 兜 0。status 谓词的参数在 SELECT 列表里,先于 WHERE 的
	// 占位符。
	facts := `COUNT(*) AS requests, COALESCE(SUM(status = ?), 0) AS errors,` +
		` COALESCE(SUM(charge_micros), 0) AS charge`
	var query string
	switch d {
	case ByDay:
		query = `SELECT date(created_at) AS day, ` + facts +
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

func scanSQLiteLog(scanner interface{ Scan(dest ...any) error }) (Log, error) {
	var l Log
	var snapshot, upstreamErr, source, createdAt sql.NullString
	if err := scanner.Scan(&l.ID, &l.KeyID, &l.ChannelID, &l.ChannelName, &l.PublicModel,
		&l.UpstreamModel, &l.Unit, &l.PromptTokens, &l.CompletionTokens, &l.DurationMS,
		&l.Status, &l.ChargeMicros, &snapshot, &upstreamErr, &source, &createdAt); err != nil {
		return Log{}, err
	}
	if snapshot.Valid {
		l.PriceSnapshot = []byte(snapshot.String)
	}
	l.UpstreamError = upstreamErr.String
	l.Source = source.String
	var err error
	if l.CreatedAt, err = sqlitedb.ParseTime(createdAt.String); err != nil {
		return Log{}, err
	}
	return l, nil
}
