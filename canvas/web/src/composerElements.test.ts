// 元素装配层(71 号票)纯函数测试:编辑器把 element.ts 的合并判定
// 接进「选中产物 chip(计算值)+ 上传草稿」双源参考条时的折算,以及对话框
// 的提示判定与发送拼装。合并结果一律经真实的 mergeImage/VideoElementPick
// 产出,测试的是装配缝而非自造结果形状。
import { describe, expect, it } from 'vitest'

import type { ComposerRef, VideoComposerRef } from './composer'
import {
  applyElementPickToDraft,
  composeSendPrompt,
  elementPickNotice,
  elementPickRejectNotice,
  hasFrameReferenceConflict,
  mentionInsertText,
  presetNeedsConfirm,
  presetPromptFor,
} from './composerElements'
import { mergeImageElementPick, mergeVideoElementPick, type ElementPick } from './element'
import { GAMEPLAY_PRESETS } from './presets'
import { composerRefMentions, videoRefMentions } from './refmention'

const img = (url: string, asset_id?: number, element?: ComposerRef['element']): ComposerRef => ({
  url,
  asset_id,
  element,
})

const vref = (
  url: string,
  role: VideoComposerRef['role'],
  element?: VideoComposerRef['element'],
): VideoComposerRef => ({ url, kind: 'image', role, element })

const charPick = (url: string, entityId: number, variantId: number): ElementPick => ({
  url,
  asset_id: Number(url.replace(/\D+/g, '')) || undefined,
  element: { type: 'character', entityId, variantId },
})

describe('applyElementPickToDraft(合并结果落回上传草稿,71 号票 B3:命中条按实体键+地址在草稿定位)', () => {
  it('added:新元素追加进草稿末尾(视频模式恒参考图)', () => {
    const draft = [vref('/api/assets/1/content', 'reference_image')]
    const selection = vref('/api/assets/9/content', 'first_frame')
    const visible = [selection, ...draft]
    const result = mergeVideoElementPick(visible, charPick('/api/assets/2/content', 7, 70))
    expect(result.action).toBe('added')
    const next = applyElementPickToDraft(draft, visible, result)
    expect(next).toEqual([
      draft[0],
      {
        url: '/api/assets/2/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 2,
        element: { type: 'character', entityId: 7, variantId: 70 },
      },
    ])
  })

  it('replaced:按实体定位草稿条目原位换图 —— 选中产物 chip 不写进草稿,被它挡住的隐藏草稿条目不被误换', () => {
    // 草稿首条与选中产物同图 → 在生效条上被去重隐藏;按「下标 - 1」偏移
    // 折算会错换这条隐藏条目,实体定位不会。
    const hidden = img('/api/assets/9/content', 9)
    const element = img('/api/assets/3/content', 3, { type: 'character', entityId: 7, variantId: 70 })
    const draft = [hidden, element]
    const selection = img('/api/assets/9/content', 9)
    const visible = [selection, element]
    const result = mergeImageElementPick(visible, charPick('/api/assets/4/content', 7, 70))
    expect(result.action).toBe('replaced')
    const next = applyElementPickToDraft(draft, visible, result)
    expect(next).toEqual([
      hidden,
      img('/api/assets/4/content', 4, { type: 'character', entityId: 7, variantId: 70 }),
    ])
  })

  it('removed:同实体同图 = 取消,从草稿移除该元素条目', () => {
    const element = vref('/api/assets/3/content', 'reference_image', {
      type: 'character',
      entityId: 7,
      variantId: 70,
    })
    const other = vref('/api/assets/5/content', 'reference_image')
    const draft = [element, other]
    const result = mergeVideoElementPick(draft, charPick('/api/assets/3/content', 7, 70))
    expect(result.action).toBe('removed')
    expect(applyElementPickToDraft(draft, draft, result)).toEqual([other])
  })

  it('rejected:草稿原样返回(同一引用,调用方据此不写状态)', () => {
    const full = Array.from({ length: 9 }, (_, i) => img(`/api/assets/${i + 1}/content`, i + 1))
    const result = mergeImageElementPick(full, charPick('/api/assets/99/content', 7, 70))
    expect(result.action).toBe('rejected')
    expect(applyElementPickToDraft(full, full, result)).toBe(full)
  })
})

describe('elementPickRejectNotice(拒收原因文案,沿用参考条满员范式)', () => {
  it('同一张图已在条上(无名图或别的实体占着)→ 说明已在条上,不误报满员', () => {
    const visible = [img('/api/assets/3/content', 3)]
    expect(elementPickRejectNotice('image', visible, '/api/assets/3/content')).toBe('该图已在参考条上')
  })

  it('图片模式满员 → 参考图上限文案', () => {
    const full = Array.from({ length: 9 }, (_, i) => img(`/api/assets/${i + 1}/content`, i + 1))
    expect(elementPickRejectNotice('image', full, '/api/assets/99/content')).toBe(
      '参考图已满员(最多 9 张)',
    )
  })

  it('视频模式三类参考合计满员 → 沿用合计满员文案(元素恒参考图,不按首帧算)', () => {
    const refs = [
      vref('/api/assets/90/content', 'first_frame'),
      ...Array.from({ length: 9 }, (_, i) => vref(`/api/assets/${i + 1}/content`, 'reference_image')),
    ]
    expect(elementPickRejectNotice('video', refs, '/api/assets/99/content')).toBe(
      '参考条已满员(参考图/参考视频/参考音频合计最多 9 条)',
    )
  })

  it('地址不可进网关契约(data: URI 等)→ 地址文案', () => {
    expect(elementPickRejectNotice('image', [], 'data:image/png;base64,xx')).toBe(
      '该图地址不能作为参考',
    )
  })
})

describe('elementPickNotice(元素点选的轻提示)', () => {
  it('replaced 报出实时解析的显示名', () => {
    expect(elementPickNotice('replaced', '小蓝')).toBe('已替换「小蓝」的参考图')
  })

  it('replaced 但名字已解析不出(目录竞态)→ 泛称,不出空引号', () => {
    expect(elementPickNotice('replaced', null)).toBe('已替换该元素的参考图')
  })

  it('added / removed 不出提示(chip 出现即反馈;取消沿用既有无提示)', () => {
    expect(elementPickNotice('added', '小蓝')).toBe('')
    expect(elementPickNotice('removed', '小蓝')).toBe('')
  })
})

describe('composeSendPrompt(发送拼装,71 号票 B5:风格段 → 绑定行 → 正文)', () => {
  const blue = { type: 'character' as const, entityId: 7, variantId: 70 }
  const roof = { type: 'scene' as const, entityId: 3 }

  it('无元素:与 styledSendPrompt 逐字节一致(存量行为不变)', () => {
    const refs = [img('/api/assets/1/content', 1)]
    expect(composeSendPrompt('一只猫', false, '水墨', refs, [null], 'image')).toBe('一只猫')
    expect(composeSendPrompt('一只猫', true, '水墨', refs, [null], 'image')).toBe('画面风格:水墨\n一只猫')
  })

  it('带风格开关:绑定行插在风格段那一行之后、正文之前', () => {
    const refs = [img('/api/assets/1/content', 1, blue), img('/api/assets/2/content', 2, roof)]
    expect(composeSendPrompt('两人对视', true, '水墨', refs, ['小蓝', '天台'], 'image')).toBe(
      '画面风格:水墨\n出场元素:小蓝@图片1(角色)、天台@图片2(场景)\n两人对视',
    )
  })

  it('风格开关关:绑定行置于最前', () => {
    const refs = [img('/api/assets/1/content', 1, blue)]
    expect(composeSendPrompt('登场', false, '水墨', refs, ['小蓝'], 'image')).toBe(
      '出场元素:小蓝@图片1(角色)\n登场',
    )
  })

  it('视频模式编号只数参考图(首帧不占序),与 chip 角标同口径', () => {
    const refs = [
      vref('/api/assets/9/content', 'first_frame'),
      vref('/api/assets/1/content', 'reference_image', blue),
    ]
    expect(composeSendPrompt('登场', false, '', refs, [null, '小蓝'], 'video')).toBe(
      '出场元素:小蓝@图片1(角色)\n登场',
    )
  })

  it('多行 IP 风格(编辑框允许换行):绑定行插在整段风格之后,不切开风格文本', () => {
    const refs = [img('/api/assets/1/content', 1, blue)]
    expect(composeSendPrompt('登场', true, '水墨\n低饱和', refs, ['小蓝'], 'image')).toBe(
      '画面风格:水墨\n低饱和\n出场元素:小蓝@图片1(角色)\n登场',
    )
  })

  it('直通预填已把多行风格段写进草稿、开关关着:同样插在整段风格之后', () => {
    const refs = [img('/api/assets/1/content', 1, blue)]
    expect(composeSendPrompt('画面风格:水墨\n低饱和\n登场', false, '水墨\n低饱和', refs, ['小蓝'], 'image')).toBe(
      '画面风格:水墨\n低饱和\n出场元素:小蓝@图片1(角色)\n登场',
    )
  })

  it('提示词里已有同一绑定行(预填/手抄)不叠加', () => {
    const refs = [img('/api/assets/1/content', 1, blue)]
    const typed = '出场元素:小蓝@图片1(角色)\n登场'
    expect(composeSendPrompt(typed, false, '', refs, ['小蓝'], 'image')).toBe(typed)
  })
})

describe('hasFrameReferenceConflict(首帧/尾帧与参考互斥提示,71 号票 A3)', () => {
  const audio = (url: string): VideoComposerRef => ({ url, kind: 'audio', role: 'reference_audio' })
  const video = (url: string): VideoComposerRef => ({ url, kind: 'video', role: 'reference_video' })

  it('首帧或尾帧与任一 reference_* 同在 → 出提示', () => {
    expect(hasFrameReferenceConflict([vref('/a/1', 'first_frame'), vref('/a/2', 'reference_image')])).toBe(true)
    expect(hasFrameReferenceConflict([vref('/a/1', 'last_frame'), audio('/a/2')])).toBe(true)
    expect(hasFrameReferenceConflict([vref('/a/1', 'first_frame'), video('/a/2')])).toBe(true)
  })

  it('只有首尾帧、只有参考、或空条 → 不出(三种合法场景各自成立)', () => {
    expect(hasFrameReferenceConflict([vref('/a/1', 'first_frame'), vref('/a/2', 'last_frame')])).toBe(false)
    expect(hasFrameReferenceConflict([vref('/a/1', 'reference_image'), video('/a/2')])).toBe(false)
    expect(hasFrameReferenceConflict([])).toBe(false)
  })
})

describe('mentionInsertText(@ 浮层选中插入的文本,71 号票 B6)', () => {
  it('有名元素插纯名字(无 @、不随参考序移位),无名图维持 @图片N', () => {
    const blue = { type: 'character' as const, entityId: 7, variantId: 70 }
    const [named, plain] = composerRefMentions(
      [img('/api/assets/1/content', 1, blue), img('/api/assets/2/content', 2)],
      ['小蓝', null],
    )
    expect(mentionInsertText(named)).toBe('小蓝')
    expect(mentionInsertText(plain)).toBe('@图片2')
  })

  it('退化无名的元素 chip(实体已删)与普通参考图同形,插 @图片N', () => {
    const gone = { type: 'scene' as const, entityId: 404 }
    const [degraded] = videoRefMentions([vref('/api/assets/1/content', 'reference_image', gone)], [null])
    expect(mentionInsertText(degraded)).toBe('@图片1')
  })
})

describe('玩法预设写入(71 号票 C3/C4)', () => {
  const duo = GAMEPLAY_PRESETS.find((p) => p.key === 'duo-interaction')!
  const solo = GAMEPLAY_PRESETS.find((p) => p.key === 'solo-entrance')!

  it('按参考条元素 chip 序填名:第 1/2 个角色 + 第 1 个场景(场景排在前面也只进场景槽)', () => {
    const refs = [
      vref('/api/assets/3/content', 'reference_image', { type: 'scene', entityId: 3 }),
      vref('/api/assets/1/content', 'reference_image', { type: 'character', entityId: 7, variantId: 70 }),
      vref('/api/assets/2/content', 'reference_image', { type: 'character', entityId: 8, variantId: 80 }),
    ]
    const text = presetPromptFor(duo, refs, ['天台', '小蓝', '阿红'])
    expect(text.startsWith('小蓝与阿红在天台同框:两人并肩而行')).toBe(true)
    expect(text).not.toMatch(/[{}〔〕[\]]/)
  })

  it('没有元素:必需槽留可见占位,可选场景从句整段省略', () => {
    const text = presetPromptFor(solo, [], [])
    expect(text.startsWith('〔角色1〕登场:空镜开场,〔角色1〕从画面深处缓步走出')).toBe(true)
    expect(text).not.toContain('场景')
  })

  it('替换前确认:提示词空(含全空白)或与模板逐字相同 → 直接写入;非空且不同 → 确认', () => {
    expect(presetNeedsConfirm('', '小蓝登场')).toBe(false)
    expect(presetNeedsConfirm('  \n ', '小蓝登场')).toBe(false)
    expect(presetNeedsConfirm('小蓝登场', '小蓝登场')).toBe(false)
    expect(presetNeedsConfirm('手写的提示词', '小蓝登场')).toBe(true)
  })
})
