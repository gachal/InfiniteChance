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
import { defaultName } from './name'
import { defaultStoryboard } from './storyboard'
import { defaultLevel } from './level'

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

/** 絵コンテ单个 cut(53 号票):画面持素材引用(0 = 空画面;内容寻址
 * 路径由展示层按 id 拼装,素材被删落「素材不可用」占位);时长秒可空
 * (「还没定」,汇总时跳过);lines 是台词/音效多行文本;camera_moves
 * 是运镜白名单多选(前端持有,后端存串不校验);note 一行备注。序号
 * 不持久 —— 显示时生成 1..N,删插自动重排。 */
export interface StoryboardCut {
  asset_id: number
  seconds?: number
  lines: string
  camera_moves: string[]
  note?: string
}

/** 絵コンテ节点数据(53 号票,第七种节点类型「规划」组):板名 + cut
 * 列表随节点 data 存整图 JSON(预演台先例)。title 空 = 展示层回落
 * 「絵コンテ」;cuts 可删到空 —— 板是画布工作件而非库实体,删节点只删
 * 图 JSON 里的节点,素材行不动。 */
export interface StoryboardNodeData {
  title?: string
  cuts: StoryboardCut[]
}

/** ネーム单个格(54 号票,第八种节点类型「规划」组):画面持素材引用
 * (0 = 空格,「素材不可用」占位契约同絵コンテ);lines 是台词/旁白/
 * 拟音多行文本;size 是大小格两档(コマ割り节奏的轻量表达,不做真二维
 * 排版 —— 拖拽定格子框另票)。与絵コンテ的 Cut 字段集不同(无时长/
 * 运镜/备注),数据结构各自定义、不强行归一。序号不持久 —— 显示时生成
 * 页内 1..N。 */
export interface NamePanel {
  asset_id: number
  lines: string
  size: 'large' | 'small'
}

/** ネーム的页(54 号票,一级单位:「每页 ネーム」):页标题可空(不落键,
 * 展示层回落「第 N 页」);panels 是页内格子垂直流(增删移走 name.ts
 * 纯函数,格可删到空)。 */
export interface NamePage {
  title?: string
  panels: NamePanel[]
}

/** ネーム节点数据(54 号票):板名 + 页列表随节点 data 存整图 JSON。
 * title 空 = 展示层回落「ネーム」;页可删到空(板可空页)、格可删到空
 * (空页保留),删节点只删图 JSON 里的节点,素材行不动。 */
export interface NameNodeData {
  title?: string
  pages: NamePage[]
}

/** 关卡卡节点数据(55 号票,第九种节点类型「规划」组):文档型工件 ——
 * 结构化字段(名称/目标/节拍)+ 画面参考,随节点 data 存整图 JSON(53 号
 * 票「数据随身」先例)。name 缺省「新关卡」(添加即命名);goal 一句话
 * 目标、detail 详细描述都是可空字段,空时不落键;beats 是流程节拍列表
 * (有序,每拍一行文本,增删移走 level.ts 纯函数,序号显示时生成 1..N
 * 不持久);asset_ids 是画面槽的素材引用列表(概念图/地图草图,上限
 * MAX_LEVEL_ASSETS = 9,与参考图口径一致),素材行被删时槽落「素材不可
 * 用」占位。卡内直接编辑不开编辑层(53 号票定案),不参与连线(文档卡,
 * 无来源/投递语义,与角色引用节点的「不连线」同族)。 */
export interface LevelNodeData {
  name: string
  goal?: string
  detail?: string
  beats: string[]
  asset_ids: number[]
}

export type CanvasNodeData =
  | AgentNodeData
  | MediaNodeData
  | AnalysisNodeData
  | PrevizNodeData
  | CharacterNodeData
  | StoryboardNodeData
  | NameNodeData
  | LevelNodeData

export type CanvasNodeType = 'agent' | 'image' | 'video' | 'analysis' | 'previz' | 'character' | 'storyboard' | 'name' | 'level'

export function isCanvasNodeType(value: unknown): value is CanvasNodeType {
  return (
    value === 'agent' ||
    value === 'image' ||
    value === 'video' ||
    value === 'analysis' ||
    value === 'previz' ||
    value === 'character' ||
    value === 'storyboard' ||
    value === 'name' ||
    value === 'level'
  )
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
 * 绑定走节点卡上的选择器(52 号票);絵コンテ自带 1 个空 cut(53 号
 * 票,添加即可编辑);ネーム自带 1 页 1 格(54 号票,添加即可编辑);
 * 关卡卡自带「新关卡」+ 空节拍(55 号票,添加即可编辑)。 */
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
  if (type === 'storyboard') {
    return defaultStoryboard()
  }
  if (type === 'name') {
    return defaultName()
  }
  if (type === 'level') {
    return defaultLevel()
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
  storyboard: '絵コンテ',
  name: 'ネーム',
  level: '关卡',
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
