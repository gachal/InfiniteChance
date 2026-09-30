import { describe, expect, it } from 'vitest'

import {
  appendComposerRef,
  appendVideoRef,
  composerImageUrls,
  composerVideoRefs,
  composeSize,
  durationRangeFor,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  mediaSyncPatch,
  parseDurationInput,
  resultNodeData,
  RATIO_PRESETS,
  RESOLUTION_PRESETS,
  rolesForKind,
  taskUrlFor,
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
    for (let i = 0; i < 6; i += 1) {
      list = appendVideoRef(list, img(`https://a/${i}.png`, 'reference_image'))
    }
    expect(list).toHaveLength(4)
    list = appendVideoRef(list, img('https://a/first.png'))
    list = appendVideoRef(list, img('https://a/first2.png'))
    expect(list.filter((r) => r.role === 'first_frame')).toHaveLength(1)
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
})

describe('durationRangeFor / parseDurationInput / 官方时长区间', () => {
  it('maps seedance generations onto their official integer ranges', () => {
    expect(durationRangeFor('doubao-seedance-2-5-260628')).toEqual({ min: 4, max: 30 })
    expect(durationRangeFor('doubao-seedance-2-0-260128')).toEqual({ min: 4, max: 15 })
    expect(durationRangeFor('wan-video')).toEqual({ min: 4, max: 15 })
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
