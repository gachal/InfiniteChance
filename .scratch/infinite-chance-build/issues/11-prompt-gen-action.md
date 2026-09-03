# 11 — 提示词生成动作与模板

**What to build:** 画布中选中节点触发「生成提示词」:canvas/server 经网关聊天接口按模板生成提示词,写入当前节点或新建提示词节点;admin 侧提供提示词模板的增删改查,模板改动即时反映到画布动作。

**Blocked by:** 10

**Status:** done

> 备注:新表 `prompt_templates`(name、template 必含 `{topic}` 占位符、enabled),网关挂管理 CRUD `/admin/prompt-templates`;canvas/server 同库只读,目录与生成按请求即时读表 —— 管理端增删改/停用立即生效(无缓存)。画布侧动作:提示词节点输入主题(或参考文本,同属 topic 字段)、选模板与 token 轨聊天模型,canvas/server 渲染模板后以服务级 key 同步调 `/v1/chat/completions`(非流式),带 `X-InfiniteChance-Source: canvas=<id> node=<节点id> gen=prompt`,用量按 token 计费入 usage_logs 与直连聊天无异;不进 canvas_tasks 队列(聊天秒级完成、无产物落库)。编辑器已打开时,模板目录在窗口重新聚焦与生成失败(模板被删/停用)时自动刷新,配合服务端按请求读表保证「即时」。前端把返回文本写回文本为空的当前节点,已有内容则落为新提示词节点并与来源连线,落图后走自动保存。目录:模板 `GET /prompt-templates`(仅启用)、聊天模型 `GET /prompt-models`(token 轨),均挂 JWT。admin-web 新增「画布管理 · 提示词模板」页(导航按规格分网关/画布两区)。

- [x] 输入主题→生成提示词并落为节点,内容遵循所选模板
- [x] admin 模板增删改后,画布侧动作即时使用新模板
- [x] 该路径用量以 token 计费在网关可见
