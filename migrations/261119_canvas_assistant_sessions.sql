-- 91 号票(画布助手):canvas_assistant_sessions 新表 —— 画布级全局对话
-- 助手的会话实体。id / canvas_id(会话绑死画布,删画布级联删会话行)/
-- title(首轮回答后由服务端生成,LLM 失败回落首条消息截断)/ mode(授权
-- 档:ask=先询问我 / full=完全授权)/ messages(JSON 一列,照画布 graph
-- 「一列文档」哲学,消息不做行级查询;持久纪律 = user 轮全文 + assistant
-- 轮最终回答全文 + 工具调用摘要行,循环中间帧不入库)/ created_at /
-- updated_at。索引照 canvas_tasks 的 canvas 维度先例(面板按画布列会话)。
--
-- 本文件是留底,从不执行(ADR 0008):运行时建表由 internal/assistant 的
-- EnsureSchema 幂等自愈(MySQL 侧)。
CREATE TABLE IF NOT EXISTS canvas_assistant_sessions (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	canvas_id  BIGINT UNSIGNED NOT NULL,
	title      VARCHAR(128) NOT NULL DEFAULT '',
	mode       VARCHAR(16)  NOT NULL DEFAULT 'ask',
	messages   JSON         NOT NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	                       ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_canvas_assistant_sessions_canvas (canvas_id, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
