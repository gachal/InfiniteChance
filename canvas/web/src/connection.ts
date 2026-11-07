/**
 * 连线合法矩阵(30 号票):画布连线的前端轻校验 —— 只约束「新建立的连
 * 线」;历史画布已存边不清洗不校验(展示容忍),后端整图落库不校验(单
 * 管理员,矩阵错漏改代码即可,不上锁)。
 */

import { NODE_TYPE_LABEL, type CanvasNodeType } from './graph'

/**
 * 合法矩阵(源类型 → 可达目标类型),语义收敛为十一种(CONTEXT.md「连线」):
 * - agent → image/video        提示词投递通道(29 号票推模式)
 * - video → agent              反推来源(13 号票)
 * - image/video → analysis     分析来源(17 号票)
 * - image/video → image/video  迭代来源(21 号票)
 * - previz → image             渲染帧落点(39 号票,落画布成图片节点)
 * - storyboard → image         分镜画面落点(53 号票,落图节点成图片节点)
 * - storyboard → video         cut→视频直通来源边(67 号票:落空视频节点
 *                              + 来源边,对话框预填后人工确认发送)
 * - name → image               格画面落点(54 号票,storyboard 同族来源语义)
 * - voice → audio              合成来源(57 号票:台词合成的产出边,絵コンテ→图片同族)
 * - character → image          设定图生成来源边(70 号票,52 号票口径修订:
 *                              「生成设定图」落空图片节点 + 来源边,composer
 *                              预填后人工确认发送;边只表达生成来源,锚定
 *                              写库表不落边)
 * - character → video          登场视频生成来源边(71 号票:「生成登场视频」
 *                              落空视频节点 + 来源边,composer 预填该角色
 *                              元素与「单角色登场」玩法预设后人工确认发送;
 *                              口径同 character → image,边只表达生成来源)
 * analysis 不作为任何连线的源;previz/storyboard/name 不作为任何连线的目标
 * (输入是用户摆的场景/cut/格的画面,回填走素材引用不走边);character 仅
 * 可作生成来源边的源(52 号票「不作任何连线端点」由 70 号票修订、71 号票
 * 扩视频,预演台只作源同族:右 + 连图片/视频节点、左侧不渲染把手,不可作
 * 任何边的目标 —— 「落图」的素材插入语义保留,历史已存边不清洗不校验);level
 * 也不作任何连线的源与目标(55 号票:关卡卡是文档卡,卡内直接编辑,无
 * 来源/投递语义,组件不渲染把手);audio
 * 不作为任何连线的源(57 号票:音频是合成产物,不派生新链);script 也不
 * 作任何连线的源与目标(64 号票:剧本是文本载体卡,内容经 Agent 工具读写
 * 不靠边,66 号票 read_source 分段消费,与 level 的「不连线」同族,组件
 * 不渲染把手);group 同样不作任何连线的源与目标(72 号票:分组是组织
 * 装饰容器,不是内容节点,纯组织语义不影响生成链);panorama 也不作任何
 * 连线的源与目标(81 号票:参考经节点表单 chip 持有、产物回填节点自身,
 * 均不靠边,与 script/level 的「不连线」同族,组件不渲染把手);其余组合
 * (agent→agent、voice→image 等)拒连。
 */
export const EDGE_MATRIX: Record<CanvasNodeType, readonly CanvasNodeType[]> = {
  agent: ['image', 'video'],
  image: ['image', 'video', 'analysis'],
  video: ['agent', 'image', 'video', 'analysis'],
  analysis: [],
  previz: ['image'],
  character: ['image', 'video'],
  storyboard: ['image', 'video'],
  name: ['image'],
  level: [],
  voice: ['audio'],
  audio: [],
  script: [],
  group: [],
  panorama: [],
}

/** source 类型到 target 类型能否建立新连线;未知/缺失类型一律拒绝。 */
export function canConnect(source: string | undefined, target: string | undefined): boolean {
  if (source === undefined || target === undefined) {
    return false
  }
  return (EDGE_MATRIX[source as CanvasNodeType] ?? []).includes(target as CanvasNodeType)
}

/** 非法连线提示里的「合法组合」清单(运行时从 EDGE_MATRIX 展开,节点中文
 * 标签复用 graph.NODE_TYPE_LABEL):矩阵加边文案自动跟上,不再手抄清单 ——
 * 手抄版漏过「分镜→视频」「角色→视频」两条(71 号票评审补漏)。顺序 =
 * 矩阵键序 × 各自目标序,机械展开即全部合法组合。 */
export function legalEdgeLabels(): string {
  const parts: string[] = []
  for (const source of Object.keys(EDGE_MATRIX) as CanvasNodeType[]) {
    for (const target of EDGE_MATRIX[source]) {
      parts.push(`${NODE_TYPE_LABEL[source]}→${NODE_TYPE_LABEL[target]}`)
    }
  }
  return parts.join('、')
}
