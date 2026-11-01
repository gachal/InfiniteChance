/**
 * 玩法预设(71 号票 C 节):视频模式的提示词模板代码常量 —— 不进技能库、
 * 不调 LLM、无管理面;选中即把带元素名的模板写进提示词框,只写提示词、
 * 不动时长/比例/分辨率/模型,人工确认发送(花钱操作总案纪律:纯预填)。
 * LLM 扩写走 Agent 节点 + 技能的既有路径。
 *
 * 模板语法:`{角色1}`/`{角色2}`/`{场景}` 为填名占位,`[ ]` 包住的是可选
 * 从句(首批全部含 `{场景}`,缺场景元素时整段省略)。必需槽(角色)缺失
 * 留可见占位「〔角色1〕」,不静默吞名。模板文本不用【】(避开厂商字幕
 * 渲染风险,与绑定行同款纪律)。四段字面量来自 71 号票 C5 定稿,
 * presets.test.ts 逐字节钉住 —— 改任何一段文案必须同改测试。
 */
import type { ComposerRef, VideoComposerRef } from './composer'

/** 四段模板定稿(71 号票 C5 第 5 条逐字节;测试钉住防静默漂移)。 */
export const GAMEPLAY_PRESET_TEMPLATES = [
  '{角色1}[在{场景}]登场:空镜开场,{角色1}从画面深处缓步走出,低角度仰拍缓慢推近,风吹动衣摆与发丝;{角色1}站定,转身直视镜头,摆出标志性姿势定格。节奏先慢后快,电影感光影,{角色1}的样貌与服装和参考保持一致。',
  '{角色1}[在{场景}]原地站立,姿态自然放松;镜头以{角色1}为中心水平环绕一周,依次呈现正面、侧面与背面,最后回到正面中近景;光线均匀柔和,{角色1}的样貌、服装与配饰细节和参考保持一致。',
  '{角色1}与{角色2}[在{场景}]同框:两人并肩而行、边走边交谈,{角色1}侧头看向{角色2},{角色2}笑着回应;中景双人镜头,镜头缓慢跟随并轻微环绕,自然光,氛围轻松;两人的样貌与服装分别和各自参考保持一致,不要混淆。',
  '{角色1}与{角色2}[在{场景}]对峙:两人隔着一段距离对视,风起尘扬,镜头在两人面部特写之间来回切换,气氛紧绷;随后两人同时冲向对方,交锋瞬间转慢动作,冲击波四散,画面定格在交锋一刻。高对比电影感光影,两人的样貌与服装分别和各自参考保持一致,不要混淆。',
] as const

export type PresetKey = 'solo-entrance' | 'solo-showcase' | 'duo-interaction' | 'duo-showdown'

/** 下拉里的一条玩法预设:key 给 wiring 层做选择记录(纯写入动作,不存
 * 「当前预设」状态),label 是下拉显示名。 */
export interface GameplayPreset {
  key: PresetKey
  label: string
  template: string
}

/** 首批四个预设(定稿顺序)。 */
export const GAMEPLAY_PRESETS: readonly GameplayPreset[] = [
  { key: 'solo-entrance', label: '单角色登场', template: GAMEPLAY_PRESET_TEMPLATES[0] },
  { key: 'solo-showcase', label: '单角色造型展示', template: GAMEPLAY_PRESET_TEMPLATES[1] },
  { key: 'duo-interaction', label: '双角色同框互动', template: GAMEPLAY_PRESET_TEMPLATES[2] },
  { key: 'duo-showdown', label: '双角色对决', template: GAMEPLAY_PRESET_TEMPLATES[3] },
]

/** 填名槽位(71 号票 C3):缺省/空串 = 缺。 */
export interface PresetSlots {
  role1?: string | null
  role2?: string | null
  scene?: string | null
}

const present = (s: string | null | undefined): string | null =>
  s != null && s.trim() !== '' ? s.trim() : null

/** 可见占位(必需槽缺失时不静默吞名,用户一眼看到缺什么)。 */
const PLACEHOLDER_ROLE1 = '〔角色1〕'
const PLACEHOLDER_ROLE2 = '〔角色2〕'
const PLACEHOLDER_SCENE = '〔场景〕'

/** 可选从句:`[…]` 包住且含 `{场景}` 的片段。 */
const OPTIONAL_CLAUSE = /\[([^[\]]*\{场景\}[^[\]]*)\]/g

/** 模板填名纯函数:必需槽(角色)缺失留可见占位;可选从句缺场景整段省略、
 * 有场景去方括号并填名。替换走函数形式,名字里的 `$` 序列不会被当作
 * 替换模式解释。纯函数,不改入参。 */
export function fillPreset(template: string, slots: PresetSlots): string {
  const role1 = present(slots.role1)
  const role2 = present(slots.role2)
  const scene = present(slots.scene)
  let out = template.replace(OPTIONAL_CLAUSE, (_match, content: string) =>
    scene === null ? '' : content.replace('{场景}', () => scene),
  )
  out = out
    .replaceAll('{场景}', () => scene ?? PLACEHOLDER_SCENE)
    .replaceAll('{角色1}', () => role1 ?? PLACEHOLDER_ROLE1)
    .replaceAll('{角色2}', () => role2 ?? PLACEHOLDER_ROLE2)
  return out
}

/** 从元素 chip 序解析填名槽位:`{角色1}`/`{角色2}` = 第 1/2 个「出得了名」
 * 的角色元素显示名(退化无名的 chip 不占槽,后面的顶上),`{场景}` = 第 1
 * 个场景元素;道具与多余元素不进模板正文(照进绑定行)。显示名由调用方
 * 经 resolveElementNames 解析后传入(编辑器实时解析,这里不连 API)。 */
export function presetSlotsFromElements(
  refs: readonly (ComposerRef | VideoComposerRef)[],
  names: readonly (string | null)[],
): PresetSlots {
  const slots: PresetSlots = {}
  refs.forEach((r, i) => {
    const el = r.element
    const name = names[i]
    if (!el || name == null || name === '') {
      return
    }
    if (el.type === 'character') {
      if (slots.role1 === undefined) {
        slots.role1 = name
      } else if (slots.role2 === undefined) {
        slots.role2 = name
      }
    } else if (el.type === 'scene' && slots.scene === undefined) {
      slots.scene = name
    }
  })
  return slots
}
