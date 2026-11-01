import { describe, expect, it } from 'vitest'

import type { CharacterRecord, CharacterVariantRecord, PropRecord, SceneRecord } from '@infinitechance/api'

import {
  MAX_COMPOSER_REFS,
  VIDEO_REFS_TOTAL_CAP,
  type ComposerRef,
  type VideoComposerRef,
} from './composer'
import {
  buildElementLine,
  countCharacterElements,
  CHARACTER_ELEMENT_HINT_LIMIT,
  elementEntityKey,
  ELEMENT_LINE_PREFIX,
  ELEMENT_REF_INDEX_LABEL,
  ELEMENT_TYPE_LABELS,
  insertElementLine,
  hasResolvedElement,
  mergeImageElementPick,
  mergeVideoElementPick,
  resolveElementNames,
  type ElementCatalog,
  type ElementPick,
} from './element'
import { STYLE_SEGMENT_MARK } from './ipStyle'
import { composerRefMentions, videoRefMentions } from './refmention'

// ---- 轻量目录夹具:type-only import,运行时不依赖 api 包(composer.test.ts
// 同款纪律);字段满足完整 Record 形状,经得起 vue-tsc。 ----

const variant = (id: number, name: string, characterId = 1): CharacterVariantRecord => ({
  id,
  character_id: characterId,
  name,
  note: '',
  sort: 0,
  images: [],
  created_at: '',
  updated_at: '',
})

const character = (id: number, name: string, variants: CharacterVariantRecord[]): CharacterRecord => ({
  id,
  ip_id: 1,
  name,
  alias: '',
  description: '',
  detail: '',
  cover_asset_id: 0,
  voice_id: 0,
  variants,
  created_at: '',
  updated_at: '',
})

const scene = (id: number, name: string): SceneRecord => ({
  id,
  ip_id: 1,
  name,
  alias: '',
  description: '',
  detail: '',
  cover_asset_id: 0,
  images: [],
  created_at: '',
  updated_at: '',
})

const prop = (id: number, name: string): PropRecord => ({
  id,
  ip_id: 1,
  name,
  alias: '',
  description: '',
  detail: '',
  cover_asset_id: 0,
  images: [],
  created_at: '',
  updated_at: '',
})

// 目录只含「还在库里」的实体;id 12 / 21 / 31 特意不在目录里,充当
// 「实体已删」的解析输入(退化无名断言用)。
const catalog: ElementCatalog = {
  characters: [
    character(10, '小蓝', [variant(100, '默认', 10), variant(101, '战斗服', 10)]),
    character(11, '阿红', [variant(110, '默认', 11)]),
  ],
  scenes: [scene(20, '天台')],
  props: [prop(30, '长剑')],
}

const charEl = (entityId: number, variantId?: number) => ({ type: 'character' as const, entityId, ...(variantId !== undefined ? { variantId } : {}) })
const sceneEl = (entityId: number) => ({ type: 'scene' as const, entityId })
const propEl = (entityId: number) => ({ type: 'prop' as const, entityId })

// ---- resolveElementNames(71 号票 B2/B4/B5)----

describe('resolveElementNames', () => {
  it('角色/场景/道具按 entityId 解析显示名,场景道具直接 name', () => {
    const refs: ComposerRef[] = [
      { url: 'a', element: charEl(10, 100) },
      { url: 'b', element: charEl(11, 110) },
      { url: 'c', element: sceneEl(20) },
      { url: 'd', element: propEl(30) },
    ]
    expect(resolveElementNames(refs, catalog)).toEqual(['小蓝', '阿红', '天台', '长剑'])
  })

  it('实体已删 → null(退化无名);变体已删 → null', () => {
    const refs: ComposerRef[] = [
      { url: 'a', element: charEl(99, 990) },
      { url: 'b', element: charEl(10, 100) },
      { url: 'c', element: charEl(10, 999) },
      { url: 'd', element: sceneEl(99) },
      { url: 'e', element: propEl(99) },
    ]
    expect(resolveElementNames(refs, catalog)).toEqual([null, '小蓝', null, null, null])
  })

  it('同一角色多个变体同时在条 → 名称带「·变体名」,只一个变体不带', () => {
    const refs: ComposerRef[] = [
      { url: 'a', element: charEl(10, 100) },
      { url: 'b', element: charEl(10, 101) },
    ]
    expect(resolveElementNames(refs, catalog)).toEqual(['小蓝·默认', '小蓝·战斗服'])

    const single: ComposerRef[] = [{ url: 'a', element: charEl(10, 101) }]
    expect(resolveElementNames(single, catalog)).toEqual(['小蓝'])
  })

  it('无名参考图(无 element)该位 null;角色缺 variantId 时回落角色名(防御)', () => {
    const refs: (ComposerRef | VideoComposerRef)[] = [
      { url: 'a' },
      { url: 'b', element: charEl(10) },
    ]
    expect(resolveElementNames(refs, catalog)).toEqual([null, '小蓝'])
  })

  it('视频参考条同样适用(VideoComposerRef 同形 element)', () => {
    const refs: VideoComposerRef[] = [
      { url: 'a', kind: 'image', role: 'first_frame' },
      { url: 'b', kind: 'image', role: 'reference_image', element: charEl(10, 100) },
      { url: 'c', kind: 'image', role: 'reference_image', element: sceneEl(20) },
    ]
    expect(resolveElementNames(refs, catalog)).toEqual([null, '小蓝', '天台'])
  })
})

// ---- 元素 pick 合并(71 号票 B3)----

describe('elementEntityKey', () => {
  it('角色按 entityId+variantId 区分;场景/道具按类型+entityId;跨类型同 id 不互撞', () => {
    expect(elementEntityKey(charEl(10, 100))).toBe(elementEntityKey(charEl(10, 100)))
    expect(elementEntityKey(charEl(10, 100))).not.toBe(elementEntityKey(charEl(10, 101)))
    expect(elementEntityKey(sceneEl(20))).not.toBe(elementEntityKey(propEl(20)))
    expect(elementEntityKey(sceneEl(20))).not.toBe(elementEntityKey(charEl(20, 200)))
  })
})

describe('mergeImageElementPick(图片模式)', () => {
  const pick = (url: string, element: ElementPick['element'], assetId = 1): ElementPick => ({
    url,
    asset_id: assetId,
    element,
  })

  it('新实体 → added,element 随条目入条', () => {
    const out = mergeImageElementPick([], pick('/api/assets/5/content', charEl(10, 100), 5))
    expect(out.action).toBe('added')
    expect(out.refs).toEqual([{ url: '/api/assets/5/content', asset_id: 5, element: charEl(10, 100) }])
    expect(out.index).toBe(0)
  })

  it('同实体不同图 → replaced:原位换图保持一元素一图,长度不变', () => {
    const list: ComposerRef[] = [
      { url: 'a', asset_id: 1 },
      { url: '/api/assets/2/content', asset_id: 2, element: charEl(10, 100) },
    ]
    const out = mergeImageElementPick(list, pick('/api/assets/9/content', charEl(10, 100), 9))
    expect(out.action).toBe('replaced')
    expect(out.index).toBe(1)
    expect(out.refs).toEqual([
      { url: 'a', asset_id: 1 },
      { url: '/api/assets/9/content', asset_id: 9, element: charEl(10, 100) },
    ])
    expect(out.refs).toHaveLength(2)
  })

  it('同实体同图再点 → removed(取消)', () => {
    const list: ComposerRef[] = [
      { url: 'a', asset_id: 1 },
      { url: '/api/assets/2/content', asset_id: 2, element: charEl(10, 100) },
    ]
    const out = mergeImageElementPick(list, pick('/api/assets/2/content', charEl(10, 100), 2))
    expect(out.action).toBe('removed')
    expect(out.index).toBe(1)
    expect(out.refs).toEqual([{ url: 'a', asset_id: 1 }])
  })

  it('同一张图已被其他条目占用(无名或别的实体)→ rejected,原条不动', () => {
    const list: ComposerRef[] = [{ url: '/api/assets/1/content', asset_id: 1 }]
    expect(
      mergeImageElementPick(list, pick('/api/assets/1/content', charEl(10, 100), 1)).action,
    ).toBe('rejected')
    const withElement: ComposerRef[] = [
      { url: '/api/assets/1/content', asset_id: 1, element: charEl(11, 110) },
    ]
    expect(
      mergeImageElementPick(withElement, pick('/api/assets/1/content', charEl(10, 100), 1)).action,
    ).toBe('rejected')
  })

  it('上限守卫照旧:条满拒收(返回原数组引用);不可上送地址拒收', () => {
    let list: ComposerRef[] = []
    for (let i = 0; i < MAX_COMPOSER_REFS; i += 1) {
      list = mergeImageElementPick(list, pick(`/api/assets/${i}/content`, charEl(10 + i, 100 + i), i)).refs
    }
    expect(list).toHaveLength(MAX_COMPOSER_REFS)
    const full = list
    expect(mergeImageElementPick(full, pick('/api/assets/99/content', sceneEl(20))).action).toBe('rejected')
    expect(mergeImageElementPick(full, pick('/api/assets/99/content', sceneEl(20))).refs).toBe(full)
    expect(mergeImageElementPick([], pick('data:image/png;base64,AA', charEl(10, 100))).action).toBe('rejected')
  })

  it('场景/道具按 entityId 匹配同实体;不同类型同 id 互不干扰', () => {
    const list: ComposerRef[] = [{ url: '/api/assets/1/content', asset_id: 1, element: sceneEl(20) }]
    expect(mergeImageElementPick(list, pick('/api/assets/3/content', sceneEl(20), 3)).action).toBe('replaced')
    // 场景 id 20 与道具 id 20、角色 id 20 是三个不同实体。
    expect(mergeImageElementPick(list, pick('/api/assets/4/content', propEl(20))).action).toBe('added')
    expect(mergeImageElementPick(list, pick('/api/assets/6/content', charEl(20, 200))).action).toBe('added')
  })
})

describe('mergeVideoElementPick(视频模式)', () => {
  const pick = (url: string, element: ElementPick['element'], assetId = 1): ElementPick => ({
    url,
    asset_id: assetId,
    element,
  })

  it('角色元素恒 reference_image:条上已有首帧时不走 44 号票默认角色规则', () => {
    const list: VideoComposerRef[] = [{ url: 'first', kind: 'image', role: 'first_frame' }]
    const out = mergeVideoElementPick(list, pick('/api/assets/5/content', charEl(10, 100), 5))
    expect(out.action).toBe('added')
    expect(out.refs[1]).toEqual({
      url: '/api/assets/5/content',
      kind: 'image',
      role: 'reference_image',
      asset_id: 5,
      element: charEl(10, 100),
    })
  })

  it('同实体替换:原位换图,角色仍恒 reference_image;同图取消 → removed', () => {
    const list: VideoComposerRef[] = [
      { url: '/api/assets/2/content', kind: 'image', role: 'reference_image', asset_id: 2, element: charEl(10, 100) },
    ]
    const replaced = mergeVideoElementPick(list, pick('/api/assets/9/content', charEl(10, 100), 9))
    expect(replaced.action).toBe('replaced')
    expect(replaced.refs[0].role).toBe('reference_image')
    expect(replaced.refs[0].url).toBe('/api/assets/9/content')

    const removed = mergeVideoElementPick(list, pick('/api/assets/2/content', charEl(10, 100), 2))
    expect(removed.action).toBe('removed')
    expect(removed.refs).toEqual([])
  })

  it('场景/道具元素同样恒 reference_image(不占首帧位)', () => {
    const out = mergeVideoElementPick([], pick('/api/assets/1/content', sceneEl(20)))
    expect(out.refs[0].role).toBe('reference_image')
  })

  it('上限守卫照旧:reference_image 满员 / 参考合计满 → rejected(返回原数组引用)', () => {
    let list: VideoComposerRef[] = [{ url: 'first', kind: 'image', role: 'first_frame' }]
    for (let i = 0; i < VIDEO_REFS_TOTAL_CAP; i += 1) {
      list = mergeVideoElementPick(list, pick(`/api/assets/${i}/content`, charEl(10 + i, 100 + i), i)).refs
    }
    expect(list.filter((r) => r.role === 'reference_image')).toHaveLength(VIDEO_REFS_TOTAL_CAP)
    const full = list
    expect(mergeVideoElementPick(full, pick('/api/assets/99/content', sceneEl(20))).action).toBe('rejected')
    expect(mergeVideoElementPick(full, pick('/api/assets/99/content', sceneEl(20))).refs).toBe(full)
  })

  it('重复地址与不可上送地址整条拒收', () => {
    const list: VideoComposerRef[] = [{ url: '/api/assets/1/content', kind: 'image', role: 'reference_image' }]
    expect(mergeVideoElementPick(list, pick('/api/assets/1/content', charEl(10, 100))).action).toBe('rejected')
    expect(mergeVideoElementPick([], pick('data:image/png;base64,AA', charEl(10, 100))).action).toBe('rejected')
  })
})

// ---- 绑定行构建(71 号票 B5)----

describe('buildElementLine', () => {
  it('图片模式:编号按参考条全部条目序(与 composerRefMentions 同口径),无名图不进', () => {
    const refs: ComposerRef[] = [
      { url: 'u1', element: charEl(10, 100) },
      { url: 'u2' },
      { url: 'u3', element: charEl(11, 110) },
      { url: 'u4', element: sceneEl(20) },
    ]
    const names = resolveElementNames(refs, catalog)
    expect(names).toEqual(['小蓝', null, '阿红', '天台'])
    expect(buildElementLine(refs, names, 'image')).toBe(
      '出场元素:小蓝@图1(角色)、阿红@图3(角色)、天台@图4(场景)',
    )
  })

  it('视频模式:只数 reference_image(与 videoRefMentions 同口径),首帧/尾帧/视频不占号', () => {
    const refs: VideoComposerRef[] = [
      { url: 'f', kind: 'image', role: 'first_frame' },
      { url: 'r1', kind: 'image', role: 'reference_image', element: charEl(10, 100) },
      { url: 'l', kind: 'image', role: 'last_frame' },
      { url: 'clip', kind: 'video', role: 'reference_video' },
      { url: 'r2', kind: 'image', role: 'reference_image', element: sceneEl(20) },
      { url: 'r3', kind: 'image', role: 'reference_image', element: propEl(30) },
    ]
    const names = resolveElementNames(refs, catalog)
    expect(buildElementLine(refs, names, 'video')).toBe(
      '出场元素:小蓝@图1(角色)、天台@图2(场景)、长剑@图3(道具)',
    )
  })

  it('实体已删退化的 chip 不进绑定行,但仍占编号位', () => {
    const refs: ComposerRef[] = [
      { url: 'u1', element: charEl(12, 120) },
      { url: 'u2', element: charEl(10, 100) },
      { url: 'u3', element: sceneEl(21) },
    ]
    const names = resolveElementNames(refs, catalog)
    expect(names).toEqual([null, '小蓝', null])
    expect(buildElementLine(refs, names, 'image')).toBe('出场元素:小蓝@图2(角色)')
  })

  it('无元素 / 全部退化无名 = 空串(行为逐字节不变)', () => {
    expect(buildElementLine([{ url: 'u1' }, { url: 'u2' }], [null, null], 'image')).toBe('')
    expect(buildElementLine([], [], 'image')).toBe('')
    const degraded: VideoComposerRef[] = [
      { url: 'u1', kind: 'image', role: 'reference_image', element: charEl(12, 120) },
    ]
    expect(buildElementLine(degraded, resolveElementNames(degraded, catalog), 'video')).toBe('')
  })

  it('多变体补名直接取调用方解析结果,不再二次加工(resolve+build 串联)', () => {
    const refs: ComposerRef[] = [
      { url: 'u1' },
      { url: 'u2', element: charEl(10, 101) },
      { url: 'u3', element: charEl(10, 100) },
    ]
    const names = resolveElementNames(refs, catalog)
    expect(names).toEqual([null, '小蓝·战斗服', '小蓝·默认'])
    expect(buildElementLine(refs, names, 'image')).toBe(
      '出场元素:小蓝·战斗服@图2(角色)、小蓝·默认@图3(角色)',
    )
  })

  it('编号与 @浮层 token 同一字面(漂移钉:refmention 或本常量改任一侧测试即破)', () => {
    expect(ELEMENT_REF_INDEX_LABEL).toBe('图')
    const imgStrip: ComposerRef[] = [{ url: 'a' }, { url: 'b' }]
    expect(composerRefMentions(imgStrip).map((c) => c.token)).toEqual(['@图1', '@图2'])
    const vstrip: VideoComposerRef[] = [
      { url: 'f', kind: 'image', role: 'first_frame' },
      { url: 'r1', kind: 'image', role: 'reference_image' },
      { url: 'r2', kind: 'image', role: 'reference_image' },
    ]
    expect(videoRefMentions(vstrip).map((c) => c.token)).toEqual(['@图1', '@图2'])
  })

  it('行前缀与类型标签字面钉住(不用【】)', () => {
    expect(ELEMENT_LINE_PREFIX).toBe('出场元素:')
    expect(ELEMENT_TYPE_LABELS).toEqual({ character: '角色', scene: '场景', prop: '道具' })
    expect(ELEMENT_LINE_PREFIX.includes('【')).toBe(false)
    expect(Object.values(ELEMENT_TYPE_LABELS).some((l) => l.includes('【'))).toBe(false)
  })
})

// ---- 发送拼装插入(71 号票 B5)----

describe('insertElementLine', () => {
  const line = '出场元素:小蓝@图1(角色)'

  it('prompt 以风格段开头 → 插在风格段那一行之后', () => {
    const styled = `${STYLE_SEGMENT_MARK}赛博霓虹,高对比\n一只猫在跳舞`
    expect(insertElementLine(styled, line)).toBe(
      `${STYLE_SEGMENT_MARK}赛博霓虹,高对比\n${line}\n一只猫在跳舞`,
    )
  })

  it('无风格段 → 置于最前', () => {
    expect(insertElementLine('一只猫在跳舞', line)).toBe(`${line}\n一只猫在跳舞`)
  })

  it('已含同一绑定行 → 原样返回不叠加(69 号票 prependStyleSegment 同款纪律)', () => {
    const already = `${line}\n一只猫在跳舞`
    expect(insertElementLine(already, line)).toBe(already)
    const styledAlready = `${STYLE_SEGMENT_MARK}风\n${line}\n一只猫在跳舞`
    expect(insertElementLine(styledAlready, line)).toBe(styledAlready)
  })

  it('绑定行空串 → 原样返回(无元素零改动)', () => {
    expect(insertElementLine('一只猫在跳舞', '')).toBe('一只猫在跳舞')
  })

  it('prompt 空 → 只回绑定行;prompt 只有风格段一行 → 追加其后', () => {
    expect(insertElementLine('', line)).toBe(line)
    expect(insertElementLine('   ', line)).toBe(line)
    expect(insertElementLine(STYLE_SEGMENT_MARK + '风', line)).toBe(`${STYLE_SEGMENT_MARK}风\n${line}`)
  })
})

// ---- 超 4 角色检测(71 号票 B7;计数只数可解析元素,见 B2)----

describe('countCharacterElements', () => {
  it('只数角色元素(变体各算一个),场景/道具/无名图不算;阈值常量钉 4', () => {
    expect(CHARACTER_ELEMENT_HINT_LIMIT).toBe(4)
    const refs: (ComposerRef | VideoComposerRef)[] = [
      { url: 'a', element: charEl(10, 100) },
      { url: 'b', element: charEl(10, 101) },
      { url: 'c', element: sceneEl(20) },
      { url: 'd', element: propEl(30) },
      { url: 'e' },
    ]
    expect(countCharacterElements(refs, resolveElementNames(refs, catalog))).toBe(2)
  })

  it('退化无名的角色 chip 不进计数(71 号票 B2:已不是有名元素),解析成功的照常进', () => {
    const refs: ComposerRef[] = [
      { url: 'a', element: charEl(12, 120) }, // 实体已删 → null
      { url: 'b', element: charEl(10, 100) },
      { url: 'c', element: charEl(10, 999) }, // 变体已删 → null
      { url: 'd', element: charEl(11, 110) },
    ]
    expect(countCharacterElements(refs, resolveElementNames(refs, catalog))).toBe(2)
  })
})

// ---- B2 退化语义的行为门(chip 角色切换的禁用判定也绑这一个出口;
// 组件无 DOM 测试基建,这里钉纯判定,模板侧只消费)----

describe('hasResolvedElement', () => {
  it('element 在场且名字解析成功 → true;退化(null/空串)或无 element → false', () => {
    expect(hasResolvedElement({ url: 'a', element: charEl(10, 100) }, '小蓝')).toBe(true)
    expect(hasResolvedElement({ url: 'a', element: charEl(12, 120) }, null)).toBe(false)
    expect(hasResolvedElement({ url: 'a', element: charEl(12, 120) }, '')).toBe(false)
    expect(hasResolvedElement({ url: 'a' }, undefined)).toBe(false)
    expect(hasResolvedElement({ url: 'a', element: sceneEl(99) }, null)).toBe(false)
  })
})
