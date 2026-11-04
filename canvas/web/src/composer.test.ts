import { describe, expect, it } from 'vitest'

import {
  appendComposerRef,
  appendNodeDraftRef,
  appendVideoRef,
  audioSyncPatch,
  carryNodeDraft,
  composerImageUrls,
  composerRefFromAsset,
  composerVideoRefs,
  composeSize,
  defaultVideoRefRole,
  dropNodeDraft,
  durationRangeFor,
  isReferenceRole,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  mediaSyncPatch,
  nodeDraftFor,
  parseDurationInput,
  patchNodeDraft,
  resolutionPresetsFor,
  resultNodeData,
  RATIO_PRESETS,
  RESOLUTION_PRESETS,
  rolesForKind,
  taskUrlFor,
  videoRefFromAsset,
  VIDEO_REFS_TOTAL_CAP,
  VIDEO_RESOLUTION_PRESETS,
  videoPlaceholder,
  withVideoRefRole,
  type ComposerElement,
  type ComposerNodeDraft,
  type ComposerRef,
  type TaskForNodeSync,
  type VideoComposerRef,
} from './composer'

describe('isRelayableRef', () => {
  it('accepts http(s) and content-addressed asset paths', () => {
    expect(isRelayableRef('https://img.example/x.png')).toBe(true)
    expect(isRelayableRef('http://img.example/x.png')).toBe(true)
    expect(isRelayableRef('/api/assets/5/content')).toBe(true)
  })

  it('rejects inline data URIs and junk', () => {
    expect(isRelayableRef('data:image/png;base64,AAAA')).toBe(false)
    expect(isRelayableRef('')).toBe(false)
    expect(isRelayableRef('/uploads/x.png')).toBe(false)
  })
})

describe('appendComposerRef', () => {
  const ref = (url: string): ComposerRef => ({ url })

  it('appends and preserves order', () => {
    const list = appendComposerRef(appendComposerRef([], ref('https://a/1.png')), ref('https://a/2.png'))
    expect(list.map((r) => r.url)).toEqual(['https://a/1.png', 'https://a/2.png'])
  })

  it('dedupes by url without mutating the input', () => {
    const first = appendComposerRef([], ref('https://a/1.png'))
    const again = appendComposerRef(first, ref('https://a/1.png'))
    expect(again).toBe(first)
    expect(again).toHaveLength(1)
  })

  it('caps the list at the backend limit', () => {
    // 与后端 maxImageRefs 同值(43 号票 4→9),硬编码断言防静默漂移。
    expect(MAX_COMPOSER_REFS).toBe(9)
    let list: ComposerRef[] = []
    for (let i = 0; i < MAX_COMPOSER_REFS + 2; i += 1) {
      list = appendComposerRef(list, ref(`https://a/${i}.png`))
    }
    expect(list).toHaveLength(MAX_COMPOSER_REFS)
  })

  it('refuses addresses the gateway contract cannot carry', () => {
    const list = appendComposerRef([], ref('data:image/png;base64,AAAA'))
    expect(list).toHaveLength(0)
  })
})

describe('composerImageUrls', () => {
  it('maps refs onto the wire list verbatim', () => {
    expect(composerImageUrls([{ url: '/api/assets/5/content' }, { url: 'https://a/1.png' }])).toEqual([
      '/api/assets/5/content',
      'https://a/1.png',
    ])
  })
})

describe('composeSize / 预设', () => {
  it('preset lists carry the auto option first and the requested ratios', () => {
    expect(RATIO_PRESETS[0]).toEqual({ label: '自动比例', value: '' })
    expect(RATIO_PRESETS.map((p) => p.value)).toEqual(['', '1:1', '16:9', '9:16', '4:3', '3:4'])
    expect(RESOLUTION_PRESETS.map((p) => p.value)).toEqual(['', '1k', '2k', '4k'])
  })

  it('both auto = no size (vendor default preserved)', () => {
    expect(composeSize('', '')).toBe('')
  })

  it('composes concrete pixels for every ratio × resolution pair', () => {
    for (const r of RATIO_PRESETS.slice(1)) {
      for (const res of RESOLUTION_PRESETS.slice(1)) {
        expect(composeSize(r.value, res.value), `${r.value}@${res.value}`).toMatch(/^\d+x\d+$/)
      }
    }
  })

  it('fills the unselected axis with the neutral default', () => {
    expect(composeSize('16:9', '')).toBe('1920x1080')
    expect(composeSize('', '4k')).toBe('4096x4096')
  })

  it('maps spot-check pairs to exact-ratio pixels', () => {
    expect(composeSize('4:3', '1k')).toBe('1440x1080')
    expect(composeSize('3:4', '2k')).toBe('1536x2048')
    expect(composeSize('16:9', '4k')).toBe('3840x2160')
    expect(composeSize('1:1', '2k')).toBe('2048x2048')
  })

  it('unknown combinations degrade to no size instead of inventing one', () => {
    expect(composeSize('21:9', '1k')).toBe('')
  })
})

// ---- 视频模式(24 号票)----

describe('rolesForKind', () => {
  it('maps each media kind onto its playable roles', () => {
    expect(rolesForKind('image')).toEqual(['first_frame', 'last_frame', 'reference_image'])
    expect(rolesForKind('video')).toEqual(['reference_video'])
    expect(rolesForKind('audio')).toEqual(['reference_audio'])
  })
})

describe('appendVideoRef', () => {
  const img = (url: string, role: VideoComposerRef['role'] = 'first_frame'): VideoComposerRef => ({
    url,
    kind: 'image',
    role,
  })

  it('appends structured refs and keeps order', () => {
    let list = appendVideoRef([], img('https://a/1.png'))
    list = appendVideoRef(list, { url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' })
    expect(list.map((r) => r.role)).toEqual(['first_frame', 'reference_video'])
  })

  it('rejects role/kind mismatches, inline data and junk URLs', () => {
    expect(appendVideoRef([], { url: 'https://a/v.mp4', kind: 'video', role: 'first_frame' })).toHaveLength(0)
    expect(appendVideoRef([], img('data:image/png;base64,AAAA'))).toHaveLength(0)
    expect(appendVideoRef([], img(''))).toHaveLength(0)
  })

  it('dedupes by url without mutating the input', () => {
    const first = appendVideoRef([], img('https://a/1.png'))
    expect(appendVideoRef(first, img('https://a/1.png', 'last_frame'))).toBe(first)
  })

  it('caps each role at its limit', () => {
    let list: VideoComposerRef[] = []
    for (let i = 0; i < 10; i += 1) {
      list = appendVideoRef(list, img(`https://a/${i}.png`, 'reference_image'))
    }
    expect(list).toHaveLength(9)
    list = appendVideoRef(list, img('https://a/first.png'))
    list = appendVideoRef(list, img('https://a/first2.png'))
    expect(list.filter((r) => r.role === 'first_frame')).toHaveLength(1)
  })

  it('caps reference entries at the wire total, first/last frame excluded (42 号票)', () => {
    let list: VideoComposerRef[] = []
    for (let i = 0; i < VIDEO_REFS_TOTAL_CAP; i += 1) {
      list = appendVideoRef(list, img(`https://a/${i}.png`, 'reference_image'))
    }
    // 参考条目已满:再收参考视频被拒,首帧不占合计额度照常收。
    expect(
      appendVideoRef(list, { url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' }),
    ).toBe(list)
    const withFirst = appendVideoRef(list, img('https://a/first.png'))
    expect(withFirst).toHaveLength(VIDEO_REFS_TOTAL_CAP + 1)
    expect(withFirst.filter((r) => isReferenceRole(r.role))).toHaveLength(VIDEO_REFS_TOTAL_CAP)
  })
})

describe('withVideoRefRole', () => {
  it('switches an image ref between its roles', () => {
    const list: VideoComposerRef[] = [{ url: 'https://a/1.png', kind: 'image', role: 'first_frame' }]
    const switched = withVideoRefRole(list, 0, 'last_frame')
    expect(switched[0].role).toBe('last_frame')
  })

  it('refuses roles the kind cannot play and full roles', () => {
    const list: VideoComposerRef[] = [{ url: 'https://a/1.png', kind: 'video', role: 'reference_video' }]
    expect(withVideoRefRole(list, 0, 'first_frame')).toBe(list)
    const two: VideoComposerRef[] = [
      { url: 'https://a/1.png', kind: 'image', role: 'first_frame' },
      { url: 'https://a/2.png', kind: 'image', role: 'reference_image' },
    ]
    expect(withVideoRefRole(two, 1, 'first_frame')).toBe(two)
  })

  it('refuses switching into a reference role when the wire total is full (42 号票)', () => {
    let list: VideoComposerRef[] = [{ url: 'https://a/first.png', kind: 'image', role: 'first_frame' }]
    for (let i = 0; i < VIDEO_REFS_TOTAL_CAP; i += 1) {
      list = appendVideoRef(list, { url: `https://a/${i}.png`, kind: 'image', role: 'reference_image' })
    }
    // 首帧切参考图会让参考条目超合计,拒收;参考图切尾帧(不占合计)照常。
    expect(withVideoRefRole(list, 0, 'reference_image')).toBe(list)
    const freed = withVideoRefRole(list, 1, 'last_frame')
    expect(freed[1].role).toBe('last_frame')
  })
})

describe('defaultVideoRefRole', () => {
  it('defaults the first image on an empty strip to first frame (24 号票图生视频语义)', () => {
    expect(defaultVideoRefRole([], 'image')).toBe('first_frame')
    // 条上只有参考视频时图片条目仍空缺,首张图照旧默认首帧。
    const videoOnly: VideoComposerRef[] = [
      { url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' },
    ]
    expect(defaultVideoRefRole(videoOnly, 'image')).toBe('first_frame')
  })

  it('defaults images to reference image once the strip holds any image entry (44 号票防静默落首帧)', () => {
    const occupied: VideoComposerRef[] = [{ url: 'https://a/1.png', kind: 'image', role: 'first_frame' }]
    expect(defaultVideoRefRole(occupied, 'image')).toBe('reference_image')
    // 陷阱序列:首帧位的图被切成参考图后首帧空缺,下一张上传图不得再落首帧。
    const switched: VideoComposerRef[] = [{ url: 'https://a/1.png', kind: 'image', role: 'reference_image' }]
    expect(defaultVideoRefRole(switched, 'image')).toBe('reference_image')
  })

  it('pins video and audio to their fixed roles', () => {
    expect(defaultVideoRefRole([], 'video')).toBe('reference_video')
    expect(defaultVideoRefRole([], 'audio')).toBe('reference_audio')
  })
})

describe('composerVideoRefs', () => {
  it('maps refs onto the wire list verbatim', () => {
    expect(
      composerVideoRefs([
        { url: '/api/assets/5/content', kind: 'image', role: 'first_frame' },
        { url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' },
      ]),
    ).toEqual([
      { url: '/api/assets/5/content', kind: 'image', role: 'first_frame' },
      { url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' },
    ])
  })
})

describe('videoPlaceholder / 视频预设', () => {
  it('describes the mode the reference combination implies', () => {
    expect(videoPlaceholder([])).toContain('文生视频')
    expect(videoPlaceholder([{ url: 'https://a/1.png', kind: 'image', role: 'first_frame' }])).toContain('图生视频')
    expect(
      videoPlaceholder([
        { url: 'https://a/1.png', kind: 'image', role: 'first_frame' },
        { url: 'https://a/2.png', kind: 'image', role: 'last_frame' },
      ]),
    ).toContain('首尾帧')
    expect(videoPlaceholder([{ url: 'https://a/1.png', kind: 'image', role: 'reference_image' }])).toContain('多图参考')
    expect(videoPlaceholder([{ url: 'https://a/v.mp4', kind: 'video', role: 'reference_video' }])).toContain('参考视频')
  })

  it('video resolution presets carry auto first and tier strings', () => {
    expect(VIDEO_RESOLUTION_PRESETS.map((p) => p.value)).toEqual(['', '480p', '720p', '1080p'])
  })

  it('resolutionPresetsFor narrows MiniMax H3 onto its official tiers (45 号票)', () => {
    // MiniMax 官方档大写(768P/2K),与 Seedance 小写档并存;档位串以各
    // 厂商官方枚举为准直传上游,不归一。
    expect(resolutionPresetsFor('minimax-h3').map((p) => p.value)).toEqual(['', '768P', '2K'])
    expect(resolutionPresetsFor('MiniMax-H3').map((p) => p.value)).toEqual(['', '768P', '2K'])
    expect(resolutionPresetsFor('minimax-h3-max').map((p) => p.value)).toEqual(['', '480P', '768P'])
    // 其余模型回落默认档(Seedance 及未知)。
    expect(resolutionPresetsFor('seedance-2.5')).toBe(VIDEO_RESOLUTION_PRESETS)
    expect(resolutionPresetsFor('wan-video')).toBe(VIDEO_RESOLUTION_PRESETS)
  })
})

describe('durationRangeFor / parseDurationInput / 官方时长区间', () => {
  it('maps seedance generations onto their official integer ranges', () => {
    expect(durationRangeFor('doubao-seedance-2-5-260628')).toEqual({ min: 4, max: 30 })
    expect(durationRangeFor('doubao-seedance-2-0-260128')).toEqual({ min: 4, max: 15 })
    expect(durationRangeFor('wan-video')).toEqual({ min: 4, max: 15 })
  })

  it('maps MiniMax H3 generations onto their official ranges (45 号票)', () => {
    // 公开名带连字符、上游名大写,两种形态都认;h3-max 区间更窄。
    expect(durationRangeFor('minimax-h3')).toEqual({ min: 4, max: 15 })
    expect(durationRangeFor('MiniMax-H3')).toEqual({ min: 4, max: 15 })
    expect(durationRangeFor('minimax-h3-max')).toEqual({ min: 5, max: 15 })
    expect(parseDurationInput('4', durationRangeFor('minimax-h3'))).toBe(4)
    expect(parseDurationInput('4', durationRangeFor('minimax-h3-max'))).toBeNull()
  })

  it('accepts the dotted public names from ticket 25 (seedance-2.5/2.0)', () => {
    // 25 号票公开名带点;只认连字符形态会让点号名静默落保守档 4–15,
    // 2.5 的 16–30 秒被发送校验拦下 —— 两种形态都认。
    expect(durationRangeFor('seedance-2.5')).toEqual({ min: 4, max: 30 })
    expect(durationRangeFor('seedance-2.0')).toEqual({ min: 4, max: 15 })
    expect(parseDurationInput('30', durationRangeFor('seedance-2.5'))).toBe(30)
  })

  it('parses empty as auto and in-range integers as seconds', () => {
    const range = durationRangeFor('doubao-seedance-2-5-260628')
    expect(parseDurationInput('', range)).toBe('')
    expect(parseDurationInput(' 7 ', range)).toBe(7)
    expect(parseDurationInput('30', range)).toBe(30)
  })

  it('rejects decimals, out-of-range values and junk', () => {
    const range = durationRangeFor('doubao-seedance-2-0-260128')
    expect(parseDurationInput('7.5', range)).toBeNull()
    expect(parseDurationInput('20', range)).toBeNull()
    expect(parseDurationInput('3', range)).toBeNull()
    expect(parseDurationInput('abc', range)).toBeNull()
  })
})

// ---- 提示词落节点(27 号票)----

describe('resultNodeData', () => {
  it('builds placeholder media data carrying prompt and model verbatim', () => {
    const data = resultNodeData('一只戴眼镜的柴犬', 'doubao-seedream-4-0')
    expect(data).toEqual({
      url: '',
      note: '',
      prompt: '一只戴眼镜的柴犬',
      model: 'doubao-seedream-4-0',
    })
  })

  it('keeps multi-thousand-character prompts whole (no truncation, no cap)', () => {
    const long = '很长的提示词'.repeat(800)
    expect(resultNodeData(long, 'm').prompt).toHaveLength(long.length)
  })
})

describe('taskUrlFor', () => {
  it('prefers the content-addressed asset path whenever the task has an asset row', () => {
    const task = makeTask({ asset_id: 7, image_url: 'https://tmp.example/x.png' })
    expect(taskUrlFor(task)).toBe('/api/assets/7/content')
  })

  it('falls back to the vendor url by task kind', () => {
    expect(taskUrlFor(makeTask({ asset_id: 0, image_url: 'https://tmp.example/x.png' }))).toBe(
      'https://tmp.example/x.png',
    )
    expect(taskUrlFor(makeTask({ asset_id: 0, kind: 'video', video_url: 'https://tmp.example/v.mp4' }))).toBe(
      'https://tmp.example/v.mp4',
    )
    expect(taskUrlFor(makeTask({ asset_id: 0 }))).toBe('')
  })
})

describe('mediaSyncPatch', () => {
  it('returns null for non-succeeded tasks or tasks without a product url', () => {
    const data = { url: '', note: '' }
    expect(mediaSyncPatch(makeTask({ status: 'failed' }), data)).toBeNull()
    expect(mediaSyncPatch(makeTask({ status: 'running' }), data)).toBeNull()
    expect(mediaSyncPatch(makeTask({ asset_id: 0 }), data)).toBeNull()
  })

  it('writes url and asset_id for freshly succeeded tasks', () => {
    const task = makeTask({ asset_id: 9, prompt: '夜色下的港口', model: 'm-a' })
    const patch = mediaSyncPatch(task, resultNodeData('夜色下的港口', 'm-a'))
    expect(patch).toEqual({ url: '/api/assets/9/content', asset_id: 9 })
  })

  it('backfills prompt/model onto legacy nodes that predate ticket 27', () => {
    const task = makeTask({ asset_id: 9, prompt: '夜色下的港口', model: 'm-a' })
    // 27 号票之前的旧产物:url/asset_id 已对上,但节点没有提示词。
    const legacy = { url: '/api/assets/9/content', asset_id: 9, note: '' }
    expect(mediaSyncPatch(task, legacy)).toEqual({
      url: '/api/assets/9/content',
      asset_id: 9,
      prompt: '夜色下的港口',
      model: 'm-a',
    })
  })

  it('never overwrites prompt/model the node already carries', () => {
    const task = makeTask({ asset_id: 0, image_url: 'https://tmp.example/x.png', prompt: '新', model: 'm-b' })
    const data = { url: 'https://old.example/y.png', asset_id: 0, note: '', prompt: '旧', model: 'm-a' }
    expect(mediaSyncPatch(task, data)).toEqual({ url: 'https://tmp.example/x.png', asset_id: 0 })
  })

  it('returns null when everything already matches', () => {
    const task = makeTask({ asset_id: 9, prompt: '夜色下的港口', model: 'm-a' })
    const data = { url: '/api/assets/9/content', asset_id: 9, note: '', prompt: '夜色下的港口', model: 'm-a' }
    expect(mediaSyncPatch(task, data)).toBeNull()
  })
})

describe('audioSyncPatch(57 号票音频节点回填;60 号票 source 判别与配乐)', () => {
  it('returns null for non-succeeded tasks or tasks without an asset row', () => {
    const data = { asset_id: 0, text: '你好' }
    expect(audioSyncPatch(makeTask({ kind: 'tts', status: 'failed' }), data)).toBeNull()
    expect(audioSyncPatch(makeTask({ kind: 'tts', status: 'running' }), data)).toBeNull()
    expect(audioSyncPatch(makeTask({ kind: 'tts', asset_id: 0 }), data)).toBeNull()
  })

  it('backfills asset_id, source, text and model onto a fresh audio node', () => {
    const task = makeTask({ kind: 'tts', asset_id: 9, prompt: '你好,世界。', model: 'qwen-tts-plus', chars: 6 })
    // 提交时创建的节点只带 text(asset_id 占 0,model 未写)。
    expect(audioSyncPatch(task, { asset_id: 0, text: '你好,世界。' })).toEqual({
      asset_id: 9,
      source: 'tts',
      model: 'qwen-tts-plus',
      chars: 6,
    })
  })

  it('carries chars only when the task row actually landed one', () => {
    const task = makeTask({ kind: 'tts', asset_id: 9, prompt: '你好', model: 'qwen-tts-plus', chars: 0 })
    // 旧任务行/未落 chars 的任务:不写 0 进节点,保住「未落值」语义。
    expect(audioSyncPatch(task, { asset_id: 0, text: '你好' })).toEqual({
      asset_id: 9,
      source: 'tts',
      model: 'qwen-tts-plus',
    })
  })

  it('never overwrites text/model/chars the node already carries', () => {
    const task = makeTask({ kind: 'tts', asset_id: 9, prompt: '新词', model: 'm-b', chars: 9 })
    const data = { asset_id: 9, text: '旧词', model: 'm-a', chars: 3 }
    // 元数据一个不动;唯一的补丁内容是历史行的 source 判别回填。
    expect(audioSyncPatch(task, data)).toEqual({ asset_id: 9, source: 'tts' })
  })

  it('writes the music pair (prompt/model) and the music source', () => {
    const task = makeTask({ kind: 'music', asset_id: 12, prompt: '轻快的电子爵士', model: 'minimax-music', chars: 0 })
    expect(audioSyncPatch(task, { asset_id: 0, prompt: '轻快的电子爵士' })).toEqual({
      asset_id: 12,
      source: 'music',
      model: 'minimax-music',
    })
  })

  it('does not touch the text pair on a music task', () => {
    const task = makeTask({ kind: 'music', asset_id: 12, prompt: '描述', model: 'm-music' })
    // 配乐任务的 prompt 不冒充台词;行内 chars 恒 0 也不落节点。
    expect(audioSyncPatch(task, { asset_id: 0, text: '旧台词' })).toEqual({
      asset_id: 12,
      source: 'music',
      model: 'm-music',
      prompt: '描述',
    })
  })

  it('keeps source:song and backfills lyrics on a song task (62 号票)', () => {
    const task = makeTask({
      kind: 'music', asset_id: 12, prompt: 'city night run',
      model: 'vod-minimax-music-3.0', lyrics: '夜色慢慢降落',
    })
    // 'song' 是提交时的创作意图标记(62 号票),已带不覆盖;歌词从任务行
    // 回填,元数据配对家族同款。
    expect(audioSyncPatch(task, { asset_id: 0, source: 'song', prompt: 'city night run' })).toEqual({
      asset_id: 12,
      model: 'vod-minimax-music-3.0',
      lyrics: '夜色慢慢降落',
    })
  })

  it('keeps source:cover and backfills the metadata pair on a cover task (78 号票)', () => {
    const task = makeTask({
      kind: 'music', asset_id: 18, prompt: 'city pop cover',
      model: 'vod-minimax-music-3.0-cover', lyrics: '夜色在大道上慢慢降落',
    })
    // 'cover' 是「翻唱」入口的创作意图标记(78 号票),走 music 分支只补
    // 空缺 —— 产物节点上的 'cover' 原样保留;prompt/model/lyrics 按产物-
    // 元数据配对回填,ref_asset_id 是创作事实、对账不写。
    expect(
      audioSyncPatch(task, {
        asset_id: 0, source: 'cover', prompt: 'city pop cover',
        model: 'vod-minimax-music-3.0-cover', ref_asset_id: 8,
      }),
    ).toEqual({
      asset_id: 18,
      lyrics: '夜色在大道上慢慢降落',
    })
    // 歌词留空(上游自动写词)的翻唱:任务行 lyrics 也空,不落 0 值。
    const autoLyrics = makeTask({ kind: 'music', asset_id: 18, prompt: 'p', model: 'm', lyrics: '' })
    expect(
      audioSyncPatch(autoLyrics, { asset_id: 0, source: 'cover', prompt: 'p', model: 'm', ref_asset_id: 8 }),
    ).toEqual({ asset_id: 18 })
  })

  it('backfills lyrics from the task row onto a fresh music node', () => {
    const task = makeTask({ kind: 'music', asset_id: 12, prompt: 'p', model: 'm', lyrics: 'L' })
    expect(audioSyncPatch(task, { asset_id: 0 })).toEqual({
      asset_id: 12,
      source: 'music',
      model: 'm',
      prompt: 'p',
      lyrics: 'L',
    })
  })

  it('returns null when the song node already matches', () => {
    const task = makeTask({ kind: 'music', asset_id: 12, prompt: 'p', model: 'm', lyrics: 'L' })
    expect(audioSyncPatch(task, { asset_id: 12, source: 'song', prompt: 'p', model: 'm', lyrics: 'L' })).toBeNull()
  })

  it('keeps source:sfx and backfills prompt/model/seconds on an sfx task (61 号票)', () => {
    const task = makeTask({
      kind: 'sfx', asset_id: 15, prompt: '雷声轰鸣,由远及近',
      model: 'vod-kling-sfx', seconds: 5,
    })
    // 'sfx' 是工具栏「音效」入口的创作意图标记(61 号票),已带不覆盖;
    // 秒数从任务行回填(节点表单已带时不覆盖),text/chars 对不进 sfx。
    expect(audioSyncPatch(task, { asset_id: 0, source: 'sfx', prompt: '雷声轰鸣,由远及近' })).toEqual({
      asset_id: 15,
      model: 'vod-kling-sfx',
      seconds: 5,
    })
    // 全新节点(无 source):sfx 来源按 kind 落。
    expect(audioSyncPatch(task, { asset_id: 0, prompt: '雷声轰鸣,由远及近', seconds: 5 })).toEqual({
      asset_id: 15,
      source: 'sfx',
      model: 'vod-kling-sfx',
    })
  })

  it('returns null when the sfx node already matches', () => {
    const task = makeTask({ kind: 'sfx', asset_id: 15, prompt: '雷', model: 'm', seconds: 3 })
    expect(audioSyncPatch(task, { asset_id: 15, source: 'sfx', prompt: '雷', model: 'm', seconds: 3 })).toBeNull()
  })
})

/** 轻量任务对象:mediaSyncPatch / taskUrlFor 只看这几个字段。 */
function makeTask(overrides: Partial<TaskForNodeSync> = {}): TaskForNodeSync {
  return {
    kind: 'image',
    status: 'succeeded',
    prompt: '默认提示词',
    model: 'default-model',
    asset_id: 1,
    image_url: '',
    video_url: '',
    ...overrides,
  }
}

// ---- 元素 chip(71 号票):可选 element 字段只活在草稿,不出前端 ----

describe('ComposerRef/VideoComposerRef 的 element 字段(71 号票 B2)', () => {
  const element: ComposerElement = { type: 'character', entityId: 3, variantId: 7 }

  it('element 随 append 系函数原样入条(可选字段,不参与上限/去重判定)', () => {
    const list = appendComposerRef([], { url: '/api/assets/9/content', asset_id: 9, element })
    expect(list[0].element).toEqual({ type: 'character', entityId: 3, variantId: 7 })
    const vlist = appendVideoRef([], {
      url: '/api/assets/9/content',
      kind: 'image',
      role: 'reference_image',
      element,
    })
    expect(vlist[0].element).toEqual(element)
  })

  it('withVideoRefRole 换角色时 element 随条目保留(防御:UI 已对元素 chip 禁切,误调用也不丢名)', () => {
    const list: VideoComposerRef[] = [
      { url: '/api/assets/1/content', kind: 'image', role: 'reference_image', element },
    ]
    const switched = withVideoRefRole(list, 0, 'first_frame')
    expect(switched[0].element).toEqual(element)
  })

  it('上送载荷形状不变:element 不出前端(钉住,防静默外泄)', () => {
    expect(composerImageUrls([{ url: 'https://a/1.png', element }])).toEqual(['https://a/1.png'])
    const vids = composerVideoRefs([
      { url: 'https://a/1.png', kind: 'image', role: 'reference_image', element },
    ])
    expect(vids).toEqual([{ url: 'https://a/1.png', kind: 'image', role: 'reference_image' }])
    expect('element' in vids[0]).toBe(false)
  })
})

describe('composerRefFromAsset(47 号票素材库挑选)', () => {
  it('builds a content-addressed ref carrying the asset id', () => {
    expect(composerRefFromAsset({ id: 5, kind: 'image', content_url: '/api/assets/5/content' })).toEqual({
      url: '/api/assets/5/content',
      asset_id: 5,
    })
  })
})

describe('videoRefFromAsset(47 号票素材库挑选)', () => {
  it('applies the 44 号票 default role rules per item', () => {
    // 空白条多选图:第一张落首帧。
    const first = videoRefFromAsset({ id: 1, kind: 'image', content_url: '/api/assets/1/content' }, [])
    expect(first).toEqual({ url: '/api/assets/1/content', kind: 'image', role: 'first_frame', asset_id: 1 })
    // 已有图片条目:后续图落参考图;视频/音频角色固定。
    const list = [first]
    expect(videoRefFromAsset({ id: 2, kind: 'image', content_url: '/api/assets/2/content' }, list).role).toBe(
      'reference_image',
    )
    expect(videoRefFromAsset({ id: 3, kind: 'video', content_url: '/api/assets/3/content' }, list).role).toBe(
      'reference_video',
    )
    expect(videoRefFromAsset({ id: 4, kind: 'audio', content_url: '/api/assets/4/content' }, list).role).toBe(
      'reference_audio',
    )
  })
})

// ---- 节点草稿(74 号票):composer 草稿按节点 × 模式隔离 ----

describe('nodeDraftFor(74 号票存量/无草稿节点回落)', () => {
  it('falls back to the node prompt that was actually sent, with an empty upload strip', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    // 存量旧节点:从未有过草稿,输入框回落到卡片上展示的 data.prompt;
    // 产物 chip 由 composerRefs 从 data.url 派生,不进草稿的上传条。
    expect(nodeDraftFor(drafts, 'image-1', { url: '/api/assets/9/content', note: '', prompt: '夜色下的港口' })).toEqual({
      prompt: '夜色下的港口',
      refs: [],
    })
  })

  it('falls back to an empty draft when the node carries no prompt (menu-created or legacy)', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    // 右键菜单「图片/视频」条目新建节点只带 {url:'', note:''};27 号票前的旧产物
    // 也没有 prompt —— 两者都回落成空草稿。toEqual 钉住形状:草稿只有
    // prompt + refs,模型/比例/分辨率/透明/带风格/时长不入节点草稿(留在
    // 全局 sticky)。
    expect(nodeDraftFor(drafts, 'image-2', { url: '', note: '' })).toEqual({ prompt: '', refs: [] })
    expect(nodeDraftFor(drafts, 'image-3', undefined)).toEqual({ prompt: '', refs: [] })
  })
})

describe('patchNodeDraft(74 号票选中即接管)', () => {
  const legacy = { url: '/api/assets/9/content', note: '', prompt: '夜色下的港口' }

  it('takes over a legacy node: the first edit materialises the fallback plus the patch', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    // 用户先上传一张参考图、还没碰提示词 —— 接管时回落的 data.prompt 必须
    // 一并进草稿,否则输入框里的「夜色下的港口」会在第一次上传后消失。
    patchNodeDraft(drafts, 'image-1', legacy, { refs: [{ url: '/api/assets/5/content', asset_id: 5 }] })
    expect(nodeDraftFor(drafts, 'image-1', legacy)).toEqual({
      prompt: '夜色下的港口',
      refs: [{ url: '/api/assets/5/content', asset_id: 5 }],
    })
    // 再改提示词:只换 prompt,参考条原样。
    patchNodeDraft(drafts, 'image-1', legacy, { prompt: '黎明时的港口' })
    expect(nodeDraftFor(drafts, 'image-1', legacy)).toEqual({
      prompt: '黎明时的港口',
      refs: [{ url: '/api/assets/5/content', asset_id: 5 }],
    })
  })

  it('once taken over, the draft wins over whatever the node data says afterwards', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', legacy, { prompt: '' })
    // 用户把提示词清空了:切走再切回不得从 data.prompt 弹回「夜色下的港口」。
    expect(nodeDraftFor(drafts, 'image-1', legacy).prompt).toBe('')
    // 节点 data 事后变化(任务回填 prompt)也不覆盖已接管的草稿。
    expect(nodeDraftFor(drafts, 'image-1', { ...legacy, prompt: '回填的提示词' }).prompt).toBe('')
  })

  it('keeps every other node untouched: A 写一半切 B 切回,各自草稿保留', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-A', undefined, { prompt: '写了一半' })
    patchNodeDraft(drafts, 'image-B', undefined, { prompt: 'B 的草稿' })
    expect(nodeDraftFor(drafts, 'image-A', undefined).prompt).toBe('写了一半')
    expect(nodeDraftFor(drafts, 'image-B', undefined).prompt).toBe('B 的草稿')
    // 从未碰过的 C 仍按自己的 data 回落,不吃别家残留。
    expect(nodeDraftFor(drafts, 'image-C', { url: '', note: '', prompt: 'C 卡片上的' })).toEqual({
      prompt: 'C 卡片上的',
      refs: [],
    })
  })

  it('refuses to write when no node is selected (empty id): nothing to take over', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    // 对话框只在选中节点时存在,但直通入口/防御路径可能在无选中时写草稿:
    // 不得落一个 '' 键 —— 否则「未选中」读到的就是这份无主草稿。
    patchNodeDraft(drafts, '', undefined, { prompt: '无主草稿' })
    expect(drafts.size).toBe(0)
    expect(nodeDraftFor(drafts, '', undefined)).toEqual({ prompt: '', refs: [] })
  })

  it('image and video drafts of the same node live in separate maps (节点 × 模式)', () => {
    const imageDrafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    const videoDrafts = new Map<string, ComposerNodeDraft<VideoComposerRef>>()
    // 图片节点上切到视频模式写提示词,不影响同一节点的图片模式草稿。
    patchNodeDraft(imageDrafts, 'image-1', undefined, { prompt: '生图提示词' })
    patchNodeDraft(videoDrafts, 'image-1', undefined, {
      prompt: '让画面动起来',
      refs: [{ url: '/api/assets/9/content', kind: 'image', role: 'first_frame' }],
    })
    expect(nodeDraftFor(imageDrafts, 'image-1', undefined)).toEqual({ prompt: '生图提示词', refs: [] })
    expect(nodeDraftFor(videoDrafts, 'image-1', undefined).prompt).toBe('让画面动起来')
    expect(nodeDraftFor(videoDrafts, 'image-1', undefined).refs).toHaveLength(1)
  })
})

describe('dropNodeDraft(74 号票删除节点清草稿键)', () => {
  it('forgets the node draft so a later read falls back and the map does not leak', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, { prompt: '将被删除' })
    patchNodeDraft(drafts, 'image-2', undefined, { prompt: '留下' })
    dropNodeDraft(drafts, 'image-1')
    expect(drafts.has('image-1')).toBe(false)
    // ⌘Z 撤销删除后节点带着 data.prompt 回来:草稿已清,按回落显示落板的
    // 提示词(未发送的编辑与上传条随删除作废,持久化另票)。
    expect(nodeDraftFor(drafts, 'image-1', { url: '', note: '', prompt: '卡片上的' }).prompt).toBe('卡片上的')
    // 别家不受影响;删不存在的键也无害(非媒体节点、从未接管过的节点)。
    expect(nodeDraftFor(drafts, 'image-2', undefined).prompt).toBe('留下')
    dropNodeDraft(drafts, 'never-existed')
    expect(drafts.size).toBe(1)
  })
})

describe('carryNodeDraft(74 号票发送成功随迁,2026-11-03 用户裁决)', () => {
  it('copies the source draft wholesale onto the product node key at success time', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, {
      prompt: '夜色下的港口',
      refs: [{ url: '/api/assets/5/content', asset_id: 5 }],
    })
    // 产物节点自动选中时随迁:composer 显示与发送前完全一致(prompt + refs
    // 整体复制,不再显示落板合成文 + 空参考条)。
    carryNodeDraft(drafts, 'image-1', 'image-2')
    expect(nodeDraftFor(drafts, 'image-2', undefined)).toEqual({
      prompt: '夜色下的港口',
      refs: [{ url: '/api/assets/5/content', asset_id: 5 }],
    })
    // 此后编辑只落产物节点草稿,不回写来源。
    patchNodeDraft(drafts, 'image-2', undefined, { prompt: '黎明时的港口' })
    expect(nodeDraftFor(drafts, 'image-1', undefined).prompt).toBe('夜色下的港口')
  })

  it('carries the latest source state: waiting-period edits migrate at success', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, { prompt: '初稿' })
    // 用户在等待期继续改来源草稿(点回来源节点再编辑),随迁取「成功时刻」
    // 的最新值 —— 在 carry 调用前再写一次即模拟该时序。
    patchNodeDraft(drafts, 'image-1', undefined, { prompt: '等待期的改稿' })
    carryNodeDraft(drafts, 'image-1', 'image-2')
    expect(nodeDraftFor(drafts, 'image-2', undefined).prompt).toBe('等待期的改稿')
  })

  it('does not materialise a draft when the source has none', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    // 来源节点在等待期被删(草稿键已清)或从未接管过:无草稿可迁,不产出
    // 草稿 —— 产物节点走回落路径(落板的合成文)。
    carryNodeDraft(drafts, 'image-1', 'image-2')
    expect(drafts.size).toBe(0)
    expect(nodeDraftFor(drafts, 'image-2', undefined)).toEqual({ prompt: '', refs: [] })
  })

  it('refuses an empty target id: no orphan key for the unselected state', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, { prompt: '有主草稿' })
    carryNodeDraft(drafts, 'image-1', '')
    expect(drafts.size).toBe(1)
    expect(drafts.has('')).toBe(false)
  })

  it('image and video maps migrate separately (勿串)', () => {
    const imageDrafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    const videoDrafts = new Map<string, ComposerNodeDraft<VideoComposerRef>>()
    patchNodeDraft(imageDrafts, 'image-1', undefined, { prompt: '生图草稿' })
    patchNodeDraft(videoDrafts, 'image-1', undefined, {
      prompt: '让画面动起来',
      refs: [{ url: '/api/assets/9/content', kind: 'image', role: 'first_frame' }],
    })
    // image 发送迁 image Map、video 发送迁 video Map:各自调各自 Map 即天然
    // 分轨,这里钉住同键不同 Map 互不污染。
    carryNodeDraft(imageDrafts, 'image-1', 'image-2')
    carryNodeDraft(videoDrafts, 'image-1', 'video-1')
    expect(nodeDraftFor(imageDrafts, 'image-2', undefined).prompt).toBe('生图草稿')
    expect(nodeDraftFor(imageDrafts, 'video-1', undefined)).toEqual({ prompt: '', refs: [] })
    expect(nodeDraftFor(videoDrafts, 'video-1', undefined).refs).toHaveLength(1)
    expect(nodeDraftFor(videoDrafts, 'image-2', undefined)).toEqual({ prompt: '', refs: [] })
  })
})

describe('appendNodeDraftRef(74 号票定案 6:上传参考归属 = 上传发起时选中节点)', () => {
  it('appends onto the captured node draft, not whoever is selected at completion', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, { prompt: '写了一半' })
    // 上传发起时选中 image-1,上传完成时选中已切到 image-2:条目仍落 image-1。
    const ok = appendNodeDraftRef(
      drafts,
      'image-1',
      undefined,
      { url: '/api/assets/7/content', asset_id: 7 },
      appendComposerRef,
    )
    expect(ok).toBe(true)
    expect(nodeDraftFor(drafts, 'image-1', undefined).refs).toEqual([
      { url: '/api/assets/7/content', asset_id: 7 },
    ])
    expect(nodeDraftFor(drafts, 'image-1', undefined).prompt).toBe('写了一半')
    // image-2 从未收到条目。
    expect(nodeDraftFor(drafts, 'image-2', undefined).refs).toEqual([])
  })

  it('materialises the fallback takeover when the captured node had no draft yet', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    const legacy = { url: '/api/assets/9/content', note: '', prompt: '卡片上的' }
    // 从未碰过草稿的存量节点:上传即接管,回落的 data.prompt 一并物化。
    appendNodeDraftRef(drafts, 'image-1', legacy, { url: '/api/assets/7/content', asset_id: 7 }, appendComposerRef)
    expect(nodeDraftFor(drafts, 'image-1', legacy)).toEqual({
      prompt: '卡片上的',
      refs: [{ url: '/api/assets/7/content', asset_id: 7 }],
    })
  })

  it('rejects through the absorb discipline and refuses an empty node id', () => {
    const drafts = new Map<string, ComposerNodeDraft<ComposerRef>>()
    patchNodeDraft(drafts, 'image-1', undefined, {
      refs: [{ url: '/api/assets/7/content', asset_id: 7 }],
    })
    // 去重/形状纪律仍归收编函数:重复 URL 整条拒收,不落键不改草稿。
    const dup = appendNodeDraftRef(drafts, 'image-1', undefined, { url: '/api/assets/7/content', asset_id: 7 }, appendComposerRef)
    expect(dup).toBe(false)
    expect(nodeDraftFor(drafts, 'image-1', undefined).refs).toHaveLength(1)
    // 空 id(未选中)= 无家可归属。
    const orphan = appendNodeDraftRef(drafts, '', undefined, { url: '/api/assets/8/content' }, appendComposerRef)
    expect(orphan).toBe(false)
    expect(drafts.size).toBe(1)
  })
})
