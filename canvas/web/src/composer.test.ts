import { describe, expect, it } from 'vitest'

import {
  appendComposerRef,
  appendVideoRef,
  audioSyncPatch,
  composerImageUrls,
  composerRefFromAsset,
  composerVideoRefs,
  composeSize,
  defaultVideoRefRole,
  durationRangeFor,
  isReferenceRole,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  mediaSyncPatch,
  parseDurationInput,
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
