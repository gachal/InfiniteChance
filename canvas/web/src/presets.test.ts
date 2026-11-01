import { describe, expect, it } from 'vitest'

import type { ComposerRef } from './composer'
import {
  fillPreset,
  GAMEPLAY_PRESETS,
  GAMEPLAY_PRESET_TEMPLATES,
  presetSlotsFromElements,
} from './presets'

// ---- 四段模板字面量钉子(71 号票 C5 第 5 条逐字节,防静默漂移)----
// 与 .scratch/infinite-chance/issues/71-element-reference-video.md C5 逐字节
// 一致;改任何一段文案必须同改票据与 presets.ts(预防两端漂移的钉子纪律,
// sheet.test.ts 同款)。

const PINNED_TEMPLATES = [
  '{角色1}[在{场景}]登场:空镜开场,{角色1}从画面深处缓步走出,低角度仰拍缓慢推近,风吹动衣摆与发丝;{角色1}站定,转身直视镜头,摆出标志性姿势定格。节奏先慢后快,电影感光影,{角色1}的样貌与服装和参考保持一致。',
  '{角色1}[在{场景}]原地站立,姿态自然放松;镜头以{角色1}为中心水平环绕一周,依次呈现正面、侧面与背面,最后回到正面中近景;光线均匀柔和,{角色1}的样貌、服装与配饰细节和参考保持一致。',
  '{角色1}与{角色2}[在{场景}]同框:两人并肩而行、边走边交谈,{角色1}侧头看向{角色2},{角色2}笑着回应;中景双人镜头,镜头缓慢跟随并轻微环绕,自然光,氛围轻松;两人的样貌与服装分别和各自参考保持一致,不要混淆。',
  '{角色1}与{角色2}[在{场景}]对峙:两人隔着一段距离对视,风起尘扬,镜头在两人面部特写之间来回切换,气氛紧绷;随后两人同时冲向对方,交锋瞬间转慢动作,冲击波四散,画面定格在交锋一刻。高对比电影感光影,两人的样貌与服装分别和各自参考保持一致,不要混淆。',
] as const

describe('GAMEPLAY_PRESET_TEMPLATES(71 号票 C5 定稿字面量)', () => {
  it('四段模板与票据定稿逐字节一致', () => {
    expect(GAMEPLAY_PRESET_TEMPLATES).toEqual(PINNED_TEMPLATES)
  })

  it('预设清单四条,标签与模板一一对应,语法只用 {} 占位与 [] 可选从句(不用【】)', () => {
    expect(GAMEPLAY_PRESETS.map((p) => p.label)).toEqual([
      '单角色登场',
      '单角色造型展示',
      '双角色同框互动',
      '双角色对决',
    ])
    expect(GAMEPLAY_PRESETS.map((p) => p.template)).toEqual([...PINNED_TEMPLATES])
    expect(GAMEPLAY_PRESETS.map((p) => p.key)).toEqual([
      'solo-entrance',
      'solo-showcase',
      'duo-interaction',
      'duo-showdown',
    ])
    for (const p of GAMEPLAY_PRESETS) {
      expect(p.template.includes('【')).toBe(false)
      expect(p.template.includes('】')).toBe(false)
    }
  })
})

// ---- 填名(71 号票 C3)----

describe('fillPreset', () => {
  const entrance = PINNED_TEMPLATES[0]
  const showcase = PINNED_TEMPLATES[1]
  const duo = PINNED_TEMPLATES[2]

  it('必需槽与可选从句齐备:填名并去掉方括号', () => {
    const out = fillPreset(entrance, { role1: '小蓝', scene: '天台' })
    expect(out.startsWith('小蓝在天台登场:空镜开场')).toBe(true)
    expect(out).toContain('小蓝从画面深处缓步走出')
    expect(out).toContain('小蓝站定')
    expect(out.endsWith('小蓝的样貌与服装和参考保持一致。')).toBe(true)
    expect(out.includes('[')).toBe(false)
    expect(out.includes('{')).toBe(false)
  })

  it('缺场景:可选从句整段省略,不留方括号与空位', () => {
    const out = fillPreset(entrance, { role1: '小蓝' })
    expect(out.startsWith('小蓝登场:空镜开场')).toBe(true)
    expect(out.includes('[')).toBe(false)
    expect(out.includes('在')).toBe(false)
  })

  it('缺角色:必需槽留可见占位〔角色1〕/〔角色2〕', () => {
    expect(fillPreset(entrance, { scene: '天台' }).startsWith('〔角色1〕在天台登场:')).toBe(true)
    expect(fillPreset(entrance, {}).startsWith('〔角色1〕登场:')).toBe(true)
    const duoOut = fillPreset(duo, { role1: '小蓝' })
    expect(duoOut.startsWith('小蓝与〔角色2〕同框:')).toBe(true)
  })

  it('空串名字视同缺失(占位,不输出空槽)', () => {
    expect(fillPreset(showcase, { role1: '  ' }).startsWith('〔角色1〕原地站立')).toBe(true)
  })

  it('双角色模板全槽填名;{角色2} 逐处替换', () => {
    const out = fillPreset(duo, { role1: '小蓝', role2: '阿红', scene: '天台' })
    expect(out.startsWith('小蓝与阿红在天台同框:')).toBe(true)
    expect(out).toContain('小蓝侧头看向阿红,阿红笑着回应')
    expect(out.includes('{')).toBe(false)
    expect(out.includes('[')).toBe(false)
  })

  it('造型展示模板:从句在句中时填名不断句', () => {
    const out = fillPreset(showcase, { role1: '小蓝', scene: '天台' })
    expect(out.startsWith('小蓝在天台原地站立,姿态自然放松')).toBe(true)
    expect(out).toContain('镜头以小蓝为中心水平环绕一周')
  })
})

// ---- 槽位解析(71 号票 C3)----

const el = (type: 'character' | 'scene' | 'prop', entityId: number, variantId?: number) =>
  ({ url: `u${entityId}:${variantId ?? ''}`, element: { type, entityId, variantId } }) as ComposerRef

describe('presetSlotsFromElements', () => {
  it('{角色1}/{角色2} 取第 1/2 个角色元素显示名,{场景} 取第 1 个场景;道具与多余元素不进正文', () => {
    const refs = [
      el('scene', 20),
      el('prop', 30),
      el('character', 10, 100),
      el('character', 11, 110),
      el('character', 12, 120),
      el('scene', 21),
    ]
    const names = ['天台', '长剑', '小蓝', '阿红', '已超编', '第二个场景']
    expect(presetSlotsFromElements(refs, names)).toEqual({
      role1: '小蓝',
      role2: '阿红',
      scene: '天台',
    })
  })

  it('退化无名的 chip 不占槽位(后面的有名元素顶上)', () => {
    const refs = [el('character', 99, 990), el('character', 10, 100)]
    const names: (string | null)[] = [null, '小蓝']
    expect(presetSlotsFromElements(refs, names)).toEqual({ role1: '小蓝' })
  })

  it('无元素 / 只有道具 → 全空', () => {
    expect(presetSlotsFromElements([], [])).toEqual({})
    expect(presetSlotsFromElements([el('prop', 30)], ['长剑'])).toEqual({})
  })
})
