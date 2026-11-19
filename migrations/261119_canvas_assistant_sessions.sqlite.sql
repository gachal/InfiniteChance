-- 91 号票(画布助手):canvas_assistant_sessions 新表 —— 画布级全局对话
-- 助手的会话实体(SQLite 桌面侧;messages TEXT,MySQL JSON ↔ SQLite TEXT
-- 是仓库既定不对称,对账只比名字不比类型)。列语义见同主干 .sql 侧注释。
--
-- 本文件是留底,从不执行(ADR 0008):运行时建表由 internal/assistant 的
-- EnsureSchema 幂等自愈(SQLite 侧)。
CREATE TABLE IF NOT EXISTS canvas_assistant_sessions (
	id         INTEGER      NOT NULL PRIMARY KEY AUTOINCREMENT,
	canvas_id  INTEGER      NOT NULL,
	title      TEXT         NOT NULL DEFAULT '',
	mode       TEXT         NOT NULL DEFAULT 'ask',
	messages   TEXT         NOT NULL,
	created_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
