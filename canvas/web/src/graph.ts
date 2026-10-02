/**
 * 画布图文档的编辑器侧形状:vue-flow 的运行时对象与持久化 JSON 之间的
 * 边界。持久化只保留语义字段(id/type/position/data、连线的端点),
 * vue-flow 的内部装饰(尺寸、事件、选中态)不落库。
 */

import type { AgentChatMedia, AgentChatMessage } from '@infinitechance/api'

// 对话轮次的线类型与服务端契约共用一份定义(packages/api),此处再导出
// 供节点组件与编辑器引用。
export type { AgentChatMessage, AgentChatMedia }

/** Agent 节点数据(29 号票,提示词节点的升级取代):text 是当前提示词
 * 草稿(生成落点,可手编,也是投递的内容);skill_id 是 `/` 选中的技能
 * (目录刷新收回,悬空按未选处理);messages 是多轮对话历史,20 轮上限
 * 超出截断最旧 —— 换技能 = 开新会话(清历史重算)。 */
export interface AgentNodeData {
  text: string
  skill_id?: number
  messages?: AgentChatMessage[]
}

/** 图片/视频节点数据:生成产物或占位;url 为空表示还没有产物。14 号票起
 * url 普遍为素材内容寻址路径(/api/assets/{id}/content),asset_id 是素材
 * 库引用 —— 跨画布复用同一素材 = 引用同一个 id,素材被删时节点据此显示
 * 占位而非报错。27 号票起 prompt/model 随身携带:提交任务时写入结果节点
 * (失败任务同样保留),历史节点由任务行回填 —— 产物与生成它的提示词永久
 * 配对;字段缺省的历史节点不显示提示词区,向后兼容。29 号票起也是 Agent
 * 节点投递的落点(仅覆盖 prompt,产物引用不动)。 */
export interface MediaNodeData {
  url?: string
  asset_id?: number
  note?: string
  prompt?: string
  model?: string
}

/** 分析节点数据(17 号票):对来源媒体(视频/图片)的结构化理解,文本
 * 只读展示;model 记录生成这次分析所用的聊天模型(原地重新分析时默认
 * 选中)。来源派生关系由连线表达,不冗余存引用。 */
export interface AnalysisNodeData {
  text: string
  model?: string
}

export type CanvasNodeData = AgentNodeData | MediaNodeData | AnalysisNodeData

export type CanvasNodeType = 'agent' | 'image' | 'video' | 'analysis'

export function isCanvasNodeType(value: unknown): value is CanvasNodeType {
  return value === 'agent' || value === 'image' || value === 'video' || value === 'analysis'
}

/** 读旧图的就地迁移(29 号票):类型值 prompt 映射为 agent —— 历史
 * prompt 节点的 text 直接成为 Agent 节点文本区内容,无迁移损失;历史
 * 连线不清洗。未知类型同样回落 Agent(文本生产者是画布的万能落点)。 */
export function normalizeNodeType(value: unknown): CanvasNodeType {
  if (value === 'prompt') {
    return 'agent'
  }
  return isCanvasNodeType(value) ? value : 'agent'
}

/** 新节点的初始数据。分析节点由视频/图片节点上的「分析」动作创建
 * (文本后填),不从工具栏直接添加。 */
export function initialData(type: CanvasNodeType): CanvasNodeData {
  return type === 'agent' || type === 'analysis' ? { text: '' } : { url: '', note: '' }
}

export const NODE_TYPE_LABEL: Record<CanvasNodeType, string> = {
  agent: 'Agent',
  image: '图片',
  video: '视频',
  analysis: '分析',
}

/** 历史上限 20 轮(29 号票):一轮 = 一条 user + 一条 assistant,超出
 * 截断最旧;服务端按同宽上限裁决。 */
export const AGENT_MAX_HISTORY_MESSAGES = 40

/** 追加一轮对话并按上限截断最旧(29 号票):生成成功后由编辑器写入节点
 * data,返回新数组(不改动入参);32 号票起 user 轮可携媒体附件(发送
 * 成功的引用进历史,下一轮起随 history 全量重发,旧轮滚出截断即停止
 * 重发)。 */
export function appendAgentTurn(
  history: AgentChatMessage[],
  user: string,
  assistant: string,
  media?: AgentChatMedia[],
): AgentChatMessage[] {
  const next = [
    ...history,
    { role: 'user' as const, content: user, ...(media && media.length > 0 ? { media } : {}) },
    { role: 'assistant' as const, content: assistant },
  ]
  return next.length > AGENT_MAX_HISTORY_MESSAGES
    ? next.slice(next.length - AGENT_MAX_HISTORY_MESSAGES)
    : next
}

/** 对话轮数(展示用):按 user 消息计数。 */
export function agentRoundCount(history: AgentChatMessage[]): number {
  return history.filter((m) => m.role === 'user').length
}
