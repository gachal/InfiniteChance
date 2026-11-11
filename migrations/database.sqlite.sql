-- InfiniteChance 全库 schema 基线(SQLite,桌面单文件库)—— 82 号票留底,本文件冻结不改。
--
-- 生成方法(2026-11-11):临时文件库按 desktop/runtime.go 装配顺序跑齐 17 个
-- store 的 EnsureSchema,再从 sqlite_master 取各表 CREATE TABLE 原文与独立
-- CREATE INDEX 语句(sqlite_master 所存即所建 —— 存储文本已由 SQLite 归一
-- 剥离 IF NOT EXISTS 与分号,此处按可执行标准补行尾分号;拒绝手工翻译)。
-- 表顺序 = 装配顺序:网关域 10 表在前、画布域 12 表在后,出处见各表注释;
-- 索引随所属表分组、组内按索引名排序。
--
-- 历史不对称保留:assets 4 列(object_key/content_type/size_bytes/last_used_at)、
-- canvas_tasks 2 列(seconds/image_ref)只活在建表 DDL、无迁移条目 —— 桌面库
-- 诞生即含这些列,基线直接 = 建表 DDL,不从 MySQL 迁移清单照搬(CONTEXT.md
-- 「迁移留底」词条)。
--
-- 冻结声明:本文件落档后不再修改;未来结构 = 本基线 + migrations/ 下
-- yyMMdd_*.sqlite.sql 增量链(与 MySQL 侧同名 .sql 成对)。文件内与文件序上
-- 遵守「列迁移先于引用它的索引」纪律(asset 包为范例)。
--
-- 文件从不执行:运行时机制仍是各 store 的 EnsureSchema 双方言自愈,无 runner、
-- 无记账表。internal/migrations 的形状对账测试把「基线 + 增量链」真实执行后与
-- 全部 EnsureSchema 终形逐表比对表/列/索引名集合,持续证明基线与运行时同形。

-- admin_accounts(internal/auth)
CREATE TABLE admin_accounts (
	id            INTEGER      NOT NULL PRIMARY KEY,
	username      TEXT         NOT NULL,
	password_hash TEXT         NOT NULL,
	created_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	CONSTRAINT uniq_admin_username UNIQUE (username)
);

-- channels(internal/channel)
CREATE TABLE channels (
	id           INTEGER      PRIMARY KEY AUTOINCREMENT,
	name         TEXT         NOT NULL,
	type         TEXT         NOT NULL,
	base_url     TEXT         NOT NULL,
	api_key      TEXT         NOT NULL,
	model_map    TEXT         NOT NULL,
	capabilities TEXT         NULL,
	config       TEXT         NULL,
	priority     INTEGER      NOT NULL DEFAULT 0,
	weight       INTEGER      NOT NULL DEFAULT 0,
	enabled      INTEGER      NOT NULL DEFAULT 1,
	created_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- api_keys(internal/apikey)
CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	name         TEXT    NOT NULL,
	prefix       TEXT    NOT NULL,
	key_hash     TEXT    NOT NULL,
	quota_micros INTEGER NOT NULL DEFAULT 0,
	expires_at   TEXT    NULL,
	revoked_at   TEXT    NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	CONSTRAINT uniq_api_key_hash UNIQUE (key_hash)
);

-- api_key_quota_log(internal/apikey)
CREATE TABLE api_key_quota_log (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	key_id         INTEGER NOT NULL,
	delta_micros   INTEGER NOT NULL,
	balance_micros INTEGER NOT NULL,
	reason         TEXT    NOT NULL,
	created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_api_key_quota_log_key ON api_key_quota_log (key_id, id);

-- model_prices(internal/pricing)
CREATE TABLE model_prices (
	public_model TEXT    NOT NULL PRIMARY KEY,
	unit         TEXT    NOT NULL,
	config       TEXT    NOT NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- usage_logs(internal/usage)
CREATE TABLE usage_logs (
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
CREATE INDEX idx_usage_logs_channel ON usage_logs (channel_id, id);
CREATE INDEX idx_usage_logs_created ON usage_logs (created_at);
CREATE INDEX idx_usage_logs_key ON usage_logs (key_id, id);
CREATE INDEX idx_usage_logs_model ON usage_logs (public_model, id);

-- video_tasks(internal/videotask)
CREATE TABLE video_tasks (
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
CREATE INDEX idx_video_tasks_key ON video_tasks (key_id, id);
CREATE INDEX idx_video_tasks_status ON video_tasks (status, created_at);

-- music_tasks(internal/musictask)
CREATE TABLE music_tasks (
	id               TEXT         NOT NULL PRIMARY KEY,
	key_id           INTEGER      NOT NULL,
	channel_id       INTEGER      NOT NULL,
	channel_name     TEXT         NOT NULL,
	public_model     TEXT         NOT NULL,
	upstream_model   TEXT         NOT NULL,
	upstream_task_id TEXT         NOT NULL,
	status           TEXT         NOT NULL,
	upstream_status  TEXT NULL,
	audio_url        TEXT NULL,
	error            TEXT NULL,
	reserved_micros  INTEGER      NOT NULL DEFAULT 0,
	charge_micros    INTEGER      NOT NULL DEFAULT 0,
	price_snapshot   TEXT NULL,
	created_at       TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at       TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_music_tasks_key ON music_tasks (key_id, id);
CREATE INDEX idx_music_tasks_status ON music_tasks (status, created_at);

-- prompt_templates(internal/prompttemplate)
CREATE TABLE prompt_templates (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT      NOT NULL,
	description TEXT      NOT NULL DEFAULT '',
	template    TEXT      NOT NULL,
	target      TEXT      NOT NULL DEFAULT 'any',
	enabled     INTEGER   NOT NULL DEFAULT 1,
	created_at  TEXT      NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT      NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- settings(internal/settings)
CREATE TABLE settings (
	name       TEXT NOT NULL PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- canvases(internal/canvas)
CREATE TABLE canvases (
	id         INTEGER      NOT NULL PRIMARY KEY AUTOINCREMENT,
	name       TEXT         NOT NULL,
	graph      TEXT         NOT NULL,
	version    INTEGER      NOT NULL DEFAULT 1,
	ip_id      INTEGER      NOT NULL DEFAULT 0,
	type       TEXT         NOT NULL DEFAULT 'animation',
	created_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- assets(internal/asset)
CREATE TABLE assets (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	kind         TEXT    NOT NULL,
	canvas_id    INTEGER NOT NULL,
	task_id      TEXT    NOT NULL DEFAULT '',
	model        TEXT    NOT NULL DEFAULT '',
	prompt       TEXT    NULL,
	url          TEXT    NOT NULL,
	object_key   TEXT    NOT NULL DEFAULT '',
	content_type TEXT    NOT NULL DEFAULT '',
	size_bytes   INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT    NOT NULL DEFAULT '',
	ip_id        INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	last_used_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_assets_canvas ON assets (canvas_id, id);
CREATE INDEX idx_assets_hash ON assets (content_hash);
CREATE INDEX idx_assets_ip ON assets (ip_id, id);
CREATE INDEX idx_assets_kind ON assets (kind, id);
CREATE INDEX idx_assets_last_used ON assets (last_used_at, id);
CREATE INDEX idx_assets_object_key ON assets (object_key);
CREATE INDEX idx_assets_task ON assets (task_id);

-- ips(internal/canvas(IPStore))
CREATE TABLE ips (
	id              INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	name            TEXT    NOT NULL,
	description     TEXT    NOT NULL DEFAULT '',
	cover_asset_id  INTEGER NOT NULL DEFAULT 0,
	style           TEXT    NULL,
	created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);

-- characters(internal/character)
CREATE TABLE characters (
	id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	ip_id       INTEGER NOT NULL,
	name        TEXT    NOT NULL,
	alias       TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	voice_id    INTEGER NULL,
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_characters_ip ON characters (ip_id, id);

-- character_variants(internal/character)
CREATE TABLE character_variants (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	character_id INTEGER NOT NULL,
	name         TEXT    NOT NULL,
	note         TEXT    NOT NULL DEFAULT '',
	sort         INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_character_variants_character ON character_variants (character_id, sort, id);

-- character_images(internal/character)
CREATE TABLE character_images (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	character_id INTEGER NOT NULL,
	variant_id   INTEGER NOT NULL,
	asset_id     INTEGER NOT NULL,
	label        TEXT    NOT NULL DEFAULT '',
	sort         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_character_images_character ON character_images (character_id, sort, id);
CREATE INDEX idx_character_images_variant ON character_images (variant_id);

-- scenes(internal/scene)
CREATE TABLE scenes (
	id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	ip_id       INTEGER NOT NULL,
	name        TEXT    NOT NULL,
	alias       TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_scenes_ip ON scenes (ip_id, id);

-- scene_images(internal/scene)
CREATE TABLE scene_images (
	id       INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	scene_id INTEGER NOT NULL,
	asset_id INTEGER NOT NULL,
	label    TEXT    NOT NULL DEFAULT '',
	sort     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_scene_images_scene ON scene_images (scene_id, sort, id);

-- props(internal/prop)
CREATE TABLE props (
	id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	ip_id       INTEGER NOT NULL,
	name        TEXT    NOT NULL,
	alias       TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_props_ip ON props (ip_id, id);

-- prop_images(internal/prop)
CREATE TABLE prop_images (
	id       INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	prop_id  INTEGER NOT NULL,
	asset_id INTEGER NOT NULL,
	label    TEXT    NOT NULL DEFAULT '',
	sort     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_prop_images_prop ON prop_images (prop_id, sort, id);

-- voices(internal/voice)
CREATE TABLE voices (
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
);
CREATE UNIQUE INDEX uk_voices_voice_id ON voices (voice_id);

-- canvas_tasks(internal/canvastask)
CREATE TABLE canvas_tasks (
	id             TEXT    NOT NULL PRIMARY KEY,
	canvas_id      INTEGER NOT NULL,
	node_id        TEXT    NOT NULL,
	ip_id          INTEGER NOT NULL DEFAULT 0,
	kind           TEXT    NOT NULL,
	prompt         TEXT    NOT NULL,
	model          TEXT    NOT NULL,
	size           TEXT    NOT NULL DEFAULT '',
	ratio          TEXT    NOT NULL DEFAULT '',
	background     TEXT    NOT NULL DEFAULT '',
	scene_type     TEXT    NOT NULL DEFAULT '',
	seconds        INTEGER NOT NULL DEFAULT 0,
	lyrics         TEXT    NULL,
	instrumental   INTEGER NOT NULL DEFAULT 1,
	audio_ref      TEXT    NULL,
	cover_strength REAL    NULL,
	image_ref      TEXT    NULL,
	image_refs     TEXT    NULL,
	video_refs     TEXT    NULL,
	status         TEXT    NOT NULL,
	attempts       INTEGER NOT NULL DEFAULT 0,
	error          TEXT    NULL,
	asset_id       INTEGER NULL,
	image_url      TEXT    NULL,
	video_url      TEXT    NULL,
	remote_task_id TEXT    NOT NULL DEFAULT '',
	voice_spec     TEXT    NULL,
	created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
);
CREATE INDEX idx_canvas_tasks_canvas ON canvas_tasks (canvas_id, created_at);
CREATE INDEX idx_canvas_tasks_status ON canvas_tasks (status, created_at);

