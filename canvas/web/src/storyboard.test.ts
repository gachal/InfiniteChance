import { describe, expect, it } from 'vitest'

import type { StoryboardCut } from './graph'
import {
  CAMERA_MOVE_OPTIONS,
  QUALITY_TAIL,
  SHOT_SIZE_OPTIONS,
  appendCut,
  buildCutFramePrompt,
  buildCutVideoPrompt,
  clampSecondsInput,
  defaultStoryboard,
  emptyStoryboardCut,
  formatDuration,
  moveCut,
  pruneCutVariantOrphans,
  removeCut,
  setCutVariantBinding,
  setShotSize,
  toggleCameraMove,
  totalSeconds,
} from './storyboard'

const cut = (over: Partial<StoryboardCut> = {}): StoryboardCut => ({
  ...emptyStoryboardCut(),
  ...over,
})

describe('CAMERA_MOVE_OPTIONS', () => {
  it('holds the initial whitelist of eight moves', () => {
    // 运镜白名单前端持有(53 号票):初始集八项,后端存串不校验。
    expect([...CAMERA_MOVE_OPTIONS]).toEqual(['推', '拉', '摇', '移', '跟', '固定', '手持', '变焦'])
  })
})

describe('defaultStoryboard / emptyStoryboardCut', () => {
  it('seeds a board with one empty cut', () => {
    // 添加即带 1 个空 cut(预演台「添加即可见」先例):画面空、时长未填、
    // 台词空、无运镜、无备注;板名缺省空串(展示层回落「分镜」)。
    const data = defaultStoryboard()
    expect(data.title).toBe('')
    expect(data.cuts).toHaveLength(1)
    expect(data.cuts[0]).toEqual({ asset_id: 0, lines: '', camera_moves: [] })
    expect(data.cuts[0].seconds).toBeUndefined()
    expect(data.cuts[0].note).toBeUndefined()
  })

  it('does not share the cuts array between boards', () => {
    expect(defaultStoryboard().cuts).not.toBe(defaultStoryboard().cuts)
  })

  it('83 号票:新空 cut 不落 character_variants 键(缺省回落默认变体)', () => {
    const c = emptyStoryboardCut()
    expect('character_variants' in c).toBe(false)
  })
})

describe('setCutVariantBinding(83 号票 cut→变体绑定映射的纯操作)', () => {
  it('设置绑定:换新对象,不改入参', () => {
    const seed: Record<number, number> = { 1: 21 }
    const next = setCutVariantBinding(seed, 2, 22)
    expect(next).toEqual({ 1: 21, 2: 22 })
    expect(seed).toEqual({ 1: 21 })
  })

  it('缺省映射(undefined)首次设置:从空簿建起', () => {
    expect(setCutVariantBinding(undefined, 3, 23)).toEqual({ 3: 23 })
  })

  it('清绑定(variantId ≤ 0):删该角色键;清到空整键不落(undefined)', () => {
    expect(setCutVariantBinding({ 1: 21, 2: 22 }, 1, 0)).toEqual({ 2: 22 })
    expect(setCutVariantBinding({ 1: 21 }, 1, 0)).toBeUndefined()
    expect(setCutVariantBinding(undefined, 1, 0)).toBeUndefined()
  })
})

describe('pruneCutVariantOrphans(83 号票孤儿绑定收回)', () => {
  it('角色不在出场清单的绑定条目收回(目录级悬空收回同语义)', () => {
    expect(pruneCutVariantOrphans({ 1: 21, 2: 22 }, [2])).toEqual({ 2: 22 })
  })

  it('无孤儿:返回原引用(不产生无谓落盘);缺省簿原样 undefined', () => {
    const m = { 1: 21 }
    expect(pruneCutVariantOrphans(m, [1])).toBe(m)
    expect(pruneCutVariantOrphans(undefined, [])).toBeUndefined()
  })

  it('清到空整键不落(undefined)', () => {
    expect(pruneCutVariantOrphans({ 1: 21 }, [])).toBeUndefined()
  })
})

describe('appendCut / removeCut / moveCut', () => {
  it('appends an empty cut and returns a new array', () => {
    const seed = [cut({ lines: '开场' })]
    const next = appendCut(seed)
    expect(next).toHaveLength(2)
    expect(next[1]).toEqual({ asset_id: 0, lines: '', camera_moves: [] })
    expect(seed).toHaveLength(1)
  })

  it('removes a cut by index and allows emptying the board', () => {
    // 板是画布工作件非库实体:cut 可删到空(与角色变体「最后不可删」相反)。
    const seed = [cut({ lines: 'a' }), cut({ lines: 'b' })]
    expect(removeCut(seed, 0).map((c) => c.lines)).toEqual(['b'])
    const once = removeCut(seed, 0)
    expect(removeCut(once, 0)).toEqual([])
    expect(seed).toHaveLength(2)
  })

  it('moves a cut within bounds and no-ops at the edges', () => {
    const seed = [cut({ lines: '1' }), cut({ lines: '2' }), cut({ lines: '3' })]
    expect(moveCut(seed, 0, -1)).toBe(seed)
    expect(moveCut(seed, 2, 1)).toBe(seed)
    expect(moveCut(seed, 9, 1)).toBe(seed)
    expect(moveCut(seed, 1, -1).map((c) => c.lines)).toEqual(['2', '1', '3'])
    expect(moveCut(seed, 1, 1).map((c) => c.lines)).toEqual(['1', '3', '2'])
    expect(seed.map((c) => c.lines)).toEqual(['1', '2', '3'])
  })
})

describe('toggleCameraMove', () => {
  it('adds a move to an empty selection in order', () => {
    const next = toggleCameraMove(emptyStoryboardCut(), '推')
    expect(next.camera_moves).toEqual(['推'])
  })

  it('removes an already selected move without mutating the input', () => {
    const seed = cut({ camera_moves: ['推', '手持'] })
    const next = toggleCameraMove(seed, '推')
    expect(next.camera_moves).toEqual(['手持'])
    expect(seed.camera_moves).toEqual(['推', '手持'])
  })
})

describe('SHOT_SIZE_OPTIONS(67 号票)', () => {
  it('holds the whitelist of five shot sizes', () => {
    // 景别白名单前端持有(67 号票):远/全/中/近/特写,后端存串不校验。
    expect([...SHOT_SIZE_OPTIONS]).toEqual(['远', '全', '中', '近', '特写'])
  })
})

describe('setShotSize(67 号票)', () => {
  it('sets the shot on an empty cut (single select)', () => {
    // 景别单选:一个镜头一个景别,与运镜多选区分(67 号票定案 2)。
    const next = setShotSize(emptyStoryboardCut(), '中')
    expect(next.shot).toBe('中')
  })

  it('replaces the previous shot when picking another size', () => {
    const seed = cut({ shot: '远' })
    const next = setShotSize(seed, '特写')
    expect(next.shot).toBe('特写')
  })

  it('clears the shot when clicking the selected size again', () => {
    const seed = cut({ shot: '近' })
    const next = setShotSize(seed, '近')
    expect(next.shot).toBeUndefined()
  })

  it('does not mutate the input cut', () => {
    const seed = cut({ shot: '全' })
    setShotSize(seed, '远')
    expect(seed.shot).toBe('全')
  })
})

describe('buildCutVideoPrompt(67 号票;73 号票段序按官方公式重排)', () => {
  it('assembles note, shot, camera moves and lines into labeled sections', () => {
    // 画面/台词/运镜/景别 → 视频提示词预填(67 号票定案 4);73 号票段序
    // 画面描述 → 景别 → 运镜 → 台词/音效 → 画质约束尾段,画面描述(note,
    // 66 号票 save_storyboard 产物同字段)信息密度最高排基调段之后的首位,
    // 尾段恒拼在最后。
    const full = buildCutVideoPrompt(
      cut({
        note: '雨夜巷口,林小满回头',
        shot: '中',
        camera_moves: ['推', '摇'],
        lines: '「你来了。」\n(雨声)',
      }),
    )
    expect(full).toBe(
      `画面描述:雨夜巷口,林小满回头\n景别:中\n运镜:推、摇\n台词/音效:「你来了。」\n(雨声)\n${QUALITY_TAIL}`,
    )
  })

  it('skips empty sections but always appends the quality tail', () => {
    expect(buildCutVideoPrompt(cut({ camera_moves: ['固定'] }))).toBe(`运镜:固定\n${QUALITY_TAIL}`)
    expect(buildCutVideoPrompt(cut({ shot: '特写' }))).toBe(`景别:特写\n${QUALITY_TAIL}`)
    expect(buildCutVideoPrompt(cut({ lines: '  雨夜巷口  ' }))).toBe(`台词/音效:雨夜巷口\n${QUALITY_TAIL}`)
  })

  it('emits the picture note alone as its own trimmed section', () => {
    // 只有备注(画面内容描述)也成立:trim 后成节,工具产物与人工 cut 同规。
    expect(buildCutVideoPrompt(cut({ note: '  转场空镜  ' }))).toBe(`画面描述:转场空镜\n${QUALITY_TAIL}`)
  })

  it('answers the quality tail alone when the cut has no textual material', () => {
    // 全空 cut 不再返回空串(73 号票定案 D.1/D.4 调整旧口径):尾段硬拼
    // 无开关,输出至少含尾段一行 —— 对话框提示词以画质约束打底。
    expect(buildCutVideoPrompt(emptyStoryboardCut())).toBe(QUALITY_TAIL)
  })
})

describe('buildCutFramePrompt(分镜画面直通生成)', () => {
  it('assembles note and shot for the image composer', () => {
    // 画面描述(信息密度最高)排基调段之后的首位,景别作构图指引随后,
    // 尾段收尾;图片提示词不拼运镜/台词(那是视频语汇)。cut→画面直通与
    // cut→视频直通同族。
    expect(
      buildCutFramePrompt(cut({ note: '雨夜巷口,林小满回头', shot: '中' })),
    ).toBe(`画面描述:雨夜巷口,林小满回头\n景别:中\n${QUALITY_TAIL}`)
  })

  it('skips empty sections, trims the note and keeps the tail', () => {
    expect(buildCutFramePrompt(cut({ shot: '特写' }))).toBe(`景别:特写\n${QUALITY_TAIL}`)
    expect(buildCutFramePrompt(cut({ note: '  转场空镜  ' }))).toBe(`画面描述:转场空镜\n${QUALITY_TAIL}`)
  })

  it('answers the quality tail alone when the cut has no textual material', () => {
    // 全空 → 尾段一行(73 号票定案 D.5 生图直通同吃尾段)。
    expect(buildCutFramePrompt(emptyStoryboardCut())).toBe(QUALITY_TAIL)
  })
})

describe('全片基调段与画质约束尾段(73 号票)', () => {
  it('pins the quality tail to the verbatim constraint literal', () => {
    // 尾段逐字钉死(半角逗号/分号;末句为无字幕/无 Logo/无水印官方约束词;
    // 音乐与否交给 lines 的()段,尾段不写)。
    expect(QUALITY_TAIL).toBe('高清,细节丰富,电影质感,色彩自然;保持无字幕,不要生成Logo和水印')
  })

  it('puts the brief segment between the style segment and the picture note', () => {
    // 段序公式:画面风格 → 全片基调 → 画面描述 → 景别 → 运镜 → 台词/音效
    // → 尾段;基调段标签半角冒号,brief trim 后文本前拼。
    const brief = '东方神话高空都市。视觉基调:冷金撞青。场景与空间坐标:钟楼之巅,双线对峙。音效总纲:底层风声。'
    expect(
      buildCutVideoPrompt(
        cut({ note: '雨夜巷口', shot: '近', camera_moves: ['推'], lines: '「你来了。」' }),
        brief,
        '赛博朋克霓虹,高对比冷暖撞色',
      ),
    ).toBe(
      '画面风格:赛博朋克霓虹,高对比冷暖撞色\n'
      + `全片基调:${brief}\n`
      + '画面描述:雨夜巷口\n景别:近\n运镜:推\n台词/音效:「你来了。」\n'
      + QUALITY_TAIL,
    )
  })

  it('keeps the multi-line brief verbatim inside its segment', () => {
    // brief 多行原样:只有首尾 trim,段内换行不折叠(基调三段各自成行)。
    const brief = '视觉基调:水墨晕染,留白。\n场景与空间坐标:江南雨巷,小满居左,镜头右侧让位。\n音效总纲:底层雨声,中层足音。'
    expect(buildCutVideoPrompt(cut({ note: '雨夜巷口' }), brief)).toBe(
      `全片基调:${brief}\n画面描述:雨夜巷口\n${QUALITY_TAIL}`,
    )
  })

  it('always assembles the brief regardless of style; whitespace-only brief is skipped', () => {
    // brief 非空恒拼、不受 composer「带 IP 风格」开关管(开关只管 ips.style
    // 段,brief 是 cut 板数据本身);空白 brief 与旧板缺省一样跳过该段。
    const c = cut({ note: '雨夜巷口' })
    expect(buildCutVideoPrompt(c, '基调文本')).toBe(`全片基调:基调文本\n画面描述:雨夜巷口\n${QUALITY_TAIL}`)
    expect(buildCutFramePrompt(c, '基调文本', '')).toBe(`全片基调:基调文本\n画面描述:雨夜巷口\n${QUALITY_TAIL}`)
    expect(buildCutVideoPrompt(c, '   \n  ')).toBe(`画面描述:雨夜巷口\n${QUALITY_TAIL}`)
    expect(buildCutFramePrompt(c, '', '水墨')).toBe(`画面风格:水墨\n画面描述:雨夜巷口\n${QUALITY_TAIL}`)
  })

  it('answers style segment + tail for an empty cut with brief absent', () => {
    // 全空 cut:无风格 = 仅尾段一行;有风格 = 风格段 + 尾段(73 号票口径)。
    expect(buildCutVideoPrompt(emptyStoryboardCut())).toBe(QUALITY_TAIL)
    expect(buildCutFramePrompt(emptyStoryboardCut(), '', '赛博朋克霓虹,高对比冷暖撞色')).toBe(
      '画面风格:赛博朋克霓虹,高对比冷暖撞色\n' + QUALITY_TAIL,
    )
  })

  it('mirrors the frame prompt section order with the video prompt', () => {
    // 生图直通同吃(73 号票定案 D.5):画面风格 → 全片基调 → 画面描述 →
    // 景别 → 尾段,两直通口径一致(只少运镜/台词两段)。
    const brief = '视觉基调:冷金撞青。\n音效总纲:底层风声。'
    const c = cut({ note: '钟楼之巅,双线对峙', shot: '远' })
    expect(buildCutFramePrompt(c, brief, '水墨')).toBe(
      `画面风格:水墨\n全片基调:${brief}\n画面描述:钟楼之巅,双线对峙\n景别:远\n${QUALITY_TAIL}`,
    )
  })
})

describe('直通预填拼 IP 画面风格段(69 号票;73 号票标记换形)', () => {
  const style = '赛博朋克霓虹,高对比冷暖撞色'

  it('prefixes the cut frame prompt with the style segment', () => {
    // 风格段放 prompt 前部、逐字来自 ips.style,随提交落任务行可审计。
    expect(buildCutFramePrompt(cut({ note: '雨夜巷口,林小满回头', shot: '中' }), '', style)).toBe(
      `画面风格:赛博朋克霓虹,高对比冷暖撞色\n画面描述:雨夜巷口,林小满回头\n景别:中\n${QUALITY_TAIL}`,
    )
  })

  it('prefixes the cut video prompt with the style segment', () => {
    expect(
      buildCutVideoPrompt(
        cut({ note: '雨夜巷口', shot: '近', camera_moves: ['推'], lines: '「你来了。」' }),
        '',
        style,
      ),
    ).toBe(
      `画面风格:赛博朋克霓虹,高对比冷暖撞色\n画面描述:雨夜巷口\n景别:近\n运镜:推\n台词/音效:「你来了。」\n${QUALITY_TAIL}`,
    )
  })

  it('keeps the style on an empty cut (now with the mandatory tail)', () => {
    // 73 号票后全空 cut = 风格段 + 尾段两行(旧口径只答风格段)。
    expect(buildCutFramePrompt(emptyStoryboardCut(), '', style)).toBe(
      '画面风格:赛博朋克霓虹,高对比冷暖撞色\n' + QUALITY_TAIL,
    )
    expect(buildCutVideoPrompt(emptyStoryboardCut(), '', style)).toBe(
      '画面风格:赛博朋克霓虹,高对比冷暖撞色\n' + QUALITY_TAIL,
    )
  })

  it('leaves both prompts byte-identical when the IP has no style', () => {
    // 无风格 = 不拼风格段(尾段照旧恒拼):与 69 号票换形前仅差风格标记。
    const c = cut({ note: '转场空镜', shot: '远', camera_moves: ['摇'] })
    expect(buildCutFramePrompt(c, '', '')).toBe(buildCutFramePrompt(c))
    expect(buildCutVideoPrompt(c, '', '   ')).toBe(buildCutVideoPrompt(c))
    expect(buildCutFramePrompt(c)).toBe(`画面描述:转场空镜\n景别:远\n${QUALITY_TAIL}`)
  })
})

describe('clampSecondsInput', () => {
  it('treats empty or non-numeric input as unset', () => {
    // 时长可空:清空/乱写失焦后回到未填,汇总时跳过。
    expect(clampSecondsInput('')).toBeUndefined()
    expect(clampSecondsInput('  ')).toBeUndefined()
    expect(clampSecondsInput('abc')).toBeUndefined()
  })

  it('clamps negatives to zero and overflows to one hour', () => {
    expect(clampSecondsInput('-5')).toBe(0)
    expect(clampSecondsInput('99999')).toBe(3600)
  })

  it('rounds fractional input to whole seconds', () => {
    expect(clampSecondsInput('12.6')).toBe(13)
    expect(clampSecondsInput('90')).toBe(90)
  })
})

describe('totalSeconds / formatDuration', () => {
  it('sums only the non-empty durations', () => {
    // 时长非空求和:未填的 cut 不计入,不是 0 也不是错误。
    const cuts = [cut({ seconds: 30 }), cut(), cut({ seconds: 95 })]
    expect(totalSeconds(cuts)).toBe(125)
    expect(totalSeconds([])).toBe(0)
  })

  it('formats M:SS with padded seconds', () => {
    expect(formatDuration(0)).toBe('0:00')
    expect(formatDuration(65)).toBe('1:05')
    expect(formatDuration(600)).toBe('10:00')
    expect(formatDuration(3600)).toBe('60:00')
  })
})

// ---- 84 号票 B2/B3:角色配色段进 sections 首位(风格段顶最前)----

describe('buildCutFramePrompt / buildCutVideoPrompt 配色段(84 号票)', () => {
  const LINE_SINGLE = '角色配色:小蓝:发色:#1A2E'
  const LINE_MULTI = '角色配色:\n小蓝:发色:#1A2E\n阿红:发色:#C0392B'

  it('配色段非空:图片直通拼在风格段之后、全片基调段之前', () => {
    expect(
      buildCutFramePrompt(cut({ note: '雨夜巷口' }), '基调文本', '水墨', LINE_SINGLE),
    ).toBe(
      `画面风格:水墨\n${LINE_SINGLE}\n全片基调:基调文本\n画面描述:雨夜巷口\n${QUALITY_TAIL}`,
    )
  })

  it('配色段非空:视频直通同拼(多角色标记行 + 逐角色一行)', () => {
    expect(
      buildCutVideoPrompt(cut({ note: '雨夜巷口' }), '', '', LINE_MULTI),
    ).toBe(`${LINE_MULTI}\n画面描述:雨夜巷口\n${QUALITY_TAIL}`)
  })

  it('配色段空串:整段跳过,行为与 73 号票逐字节一致(存量调用不传第 4 参)', () => {
    expect(buildCutFramePrompt(cut({ note: '雨夜巷口' }))).toBe(`画面描述:雨夜巷口\n${QUALITY_TAIL}`)
    expect(buildCutVideoPrompt(cut({ note: '雨夜巷口' }))).toBe(`画面描述:雨夜巷口\n${QUALITY_TAIL}`)
  })

  it('段不设总截断(票面 B4):多角色行数随出场角色数,拼装不再截', () => {
    const many = ['角色配色:', ...Array.from({ length: 6 }, (_, i) => `角色${i + 1}:发色:#1A2E`)].join('\n')
    const got = buildCutVideoPrompt(cut({ note: '雨夜巷口' }), '', '', many)
    expect(got.startsWith(`${many}\n画面描述:雨夜巷口`)).toBe(true)
  })
})
