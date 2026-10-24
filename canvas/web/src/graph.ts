/**
 * 画布图文档的编辑器侧形状:vue-flow 的运行时对象与持久化 JSON 之间的
 * 边界。持久化只保留语义字段(id/type/position/data、连线的端点),
 * vue-flow 的内部装饰(尺寸、事件、选中态)不落库。
 */

import type {
  AgentChatMedia,
  AgentChatMessage,
  PrevizCamera,
  PrevizObject,
} from '@infinitechance/api'

import { defaultPrevizScene } from './previz'
import type { CanvasTypeId } from './canvasTypes'

// 对话轮次的线类型与服务端契约共用一份定义(packages/api),此处再导出
// 供节点组件与编辑器引用。
export type { AgentChatMessage, AgentChatMedia }
export type { PrevizCamera, PrevizObject, PrevizObjectKind } from '@infinitechance/api'

/** Agent 节点数据(29 号票,提示词节点的升级取代):text 是当前提示词
 * 草稿(生成落点,可手编,也是投递的内容);skill_id 是 `/` 选中的技能
 * (目录刷新收回,悬空按未选处理);messages 是多轮对话历史,20 轮上限
 * 超出截断最旧 —— 换技能 = 开新会话(清历史重算)。character_ids 是
 * 注入的角色(52 号票,按选择序,显式 chip 不做自动检测);character_section
 * 是本会话已固化的「【角色设定】」段(52 号票历史固化):首轮发送时服务端
 * 按当时角色数据渲染并随响应带回,持久进节点 data,后续轮次经
 * agentCharacterInjection 原样回传、服务端逐字复用 —— 会话中途角色改名/
 * 改设定乃至删除都不影响已发生对话;chip 变更/换技能/新会话清空固化段,
 * 下一轮重新按最新角色数据渲染。 */
export interface AgentNodeData {
  text: string
  skill_id?: number
  messages?: AgentChatMessage[]
  character_ids?: number[]
  character_section?: string
}

/** 本轮请求的角色注入载荷(52 号票历史固化):有固化段只回传固化段
 * (character_ids 不上送 —— 服务端逐字复用,不取角色目录,角色后改
 * 不影响已发生对话);无固化段且有选择时上送 character_ids(新会话或
 * 注入配置变更后的首轮,服务端取最新数据渲染);两者皆无 = 不注入。 */
export function agentCharacterInjection(
  characterIds: number[],
  characterSection: string | undefined,
): { character_ids?: number[]; character_section?: string } {
  if (characterSection) {
    return { character_section: characterSection }
  }
  if (characterIds.length > 0) {
    return { character_ids: characterIds }
  }
  return {}
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

/** 预演台节点数据(39 号票):三维场景(对象列表 + 机位列表)随节点
 * data 存整图 JSON(27 号票「数据随身」先例),自动保存免费获得;
 * active_camera_id 是当前渲染机位;latest 是最新渲染帧的素材引用
 * (uploads/ 档,经 /api/assets/upload 落库,媒体区展示、可点灯箱、
 * 可「落画布」成图片节点),未渲染过时缺省。 */
export interface PrevizNodeData {
  objects: PrevizObject[]
  cameras: PrevizCamera[]
  active_camera_id?: string
  latest?: { url: string; asset_id: number }
}

/** 角色引用节点数据(52 号票):只存角色 id **不存快照** —— one source
 * of truth 彻底贯彻,角色改名/换封面/加变体后节点随目录刷新即变;0 =
 * 未绑定(新节点待选)。渲染数据由编辑器按画布 ip_id 拉角色目录供给,
 * 目录里找不到(已删/跨 IP)落「角色不可用」占位,可删节点或换绑。 */
export interface CharacterNodeData {
  character_id: number
}

export type CanvasNodeData =
  | AgentNodeData
  | MediaNodeData
  | AnalysisNodeData
  | PrevizNodeData
  | CharacterNodeData

export type CanvasNodeType = 'agent' | 'image' | 'video' | 'analysis' | 'previz' | 'character'

export function isCanvasNodeType(value: unknown): value is CanvasNodeType {
  return (
    value === 'agent' ||
    value === 'image' ||
    value === 'video' ||
    value === 'analysis' ||
    value === 'previz' ||
    value === 'character'
  )
}

/** 节点类型的「适用画布类型」声明(50 号票机制落位):'all' = 通用节点,
 * 全部画布类型可见;数组 = 特色节点,仅列出的类型在添加面板出现。落地时
 * 现有五类全部通用 —— 行为暂无差异,是有意的中间态而非遗漏;特色节点
 * (53-55 号票)挂靠时在此收窄。 */
export const NODE_TYPE_APPLICABILITY: Record<CanvasNodeType, 'all' | CanvasTypeId[]> = {
  agent: 'all',
  image: 'all',
  video: 'all',
  analysis: 'all',
  previz: 'all',
  // 角色跨媒介是本质属性(52 号票 grilling 定案):引用节点全画布类型可用。
  character: 'all',
}

/** 某画布类型可用的节点类型清单:通用节点恒在,特色节点按声明过滤。 */
export function nodeTypesForCanvasType(canvasType: string): CanvasNodeType[] {
  const all: CanvasNodeType[] = ['agent', 'image', 'video', 'analysis', 'previz', 'character']
  return all.filter((t) => {
    const a = NODE_TYPE_APPLICABILITY[t]
    return a === 'all' || a.includes(canvasType as CanvasTypeId)
  })
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
 * (文本后填),不从工具栏直接添加;预演台自带默认场景(一个人形 +
 * 一个机位),添加即可见可渲染;角色节点 character_id = 0(未绑定),
 * 绑定走节点卡上的选择器(52 号票)。 */
export function initialData(type: CanvasNodeType): CanvasNodeData {
  if (type === 'agent' || type === 'analysis') {
    return { text: '' }
  }
  if (type === 'previz') {
    const scene = defaultPrevizScene()
    return { objects: scene.objects, cameras: scene.cameras, active_camera_id: scene.activeCameraId }
  }
  if (type === 'character') {
    return { character_id: 0 }
  }
  return { url: '', note: '' }
}

export const NODE_TYPE_LABEL: Record<CanvasNodeType, string> = {
  agent: 'Agent',
  image: '图片',
  video: '视频',
  analysis: '分析',
  previz: '预演台',
  character: '角色',
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
