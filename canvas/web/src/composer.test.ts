import { describe, expect, it } from 'vitest'

import {
  appendComposerRef,
  composerImageUrls,
  composeSize,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  RATIO_PRESETS,
  RESOLUTION_PRESETS,
  type ComposerRef,
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
