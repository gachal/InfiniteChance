# 13 — 视频反推提示词

**What to build:** 对已有视频(视频节点或素材)反推提示词:canvas/server 经网关多模态聊天接口分析视频,产出的提示词落为新的提示词节点,可直接衔接生图/生视频动作形成闭环。

**Blocked by:** 12

**Status:** done

> 备注:与「生成提示词」(11 号票)同属同步聊天动作、不进 `canvas_tasks`。`POST /canvases/:id/reverse-prompt`(`{node_id?, video_url, model}` → `{text}`,挂 JWT 会话):视频以 OpenAI 兼容多模态内容分节 `{"type":"video_url","video_url":{"url":…}}` 随固定指令(画面与运动,无模板依赖)进 `/v1/chat/completions` —— 网关 messages 以 RawMessage 原样透传,零网关改动;用量按 token 计费入 usage_logs,来源标记 `gen=video-prompt` 与 11 号票 `gen=prompt` 区分。`video_url` 接受厂商 http(s) 地址(原样透传)或素材内容寻址路径 `/api/assets/{id}/content`(服务端经素材表解出,要求 kind=video);厂商回 b64 落库的 data: URI 内联视频在边界即拒(400 `video_inline_unsupported`,12 号票「data URI 进不了网关媒体契约」同款决策)。发起前同 11 号票先查 token 轨计价(`model_not_priced` 即拒);模型不支持视频输入由上游裁决、4xx 原样透出。前端:视频节点产物落位后出现「反推提示词」入口(复用 `GET /prompt-models` 目录),结果恒落为新提示词节点并与视频节点连线(落图逻辑与 11 号票派生分支共用 `landPromptNode`),由该节点直接发起生图形成闭环。「(或素材)」的独立入口随素材库管理面(14 号票)落地——素材引用已通过内容寻址解析路径就绪。

- [x] 选择视频→反推→生成提示词节点,内容能描述该视频的画面与运动
- [x] 反推产出的提示词可直接用于生图(闭环可用)
- [x] 该路径用量以 token 计费在网关可见
