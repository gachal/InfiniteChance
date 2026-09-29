import { describe, expect, it } from 'vitest'

import {
  appendComposerRef,
  composerImageUrls,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  SIZE_PRESETS,
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

describe('SIZE_PRESETS', () => {
  it('starts with the omit-size default', () => {
    expect(SIZE_PRESETS[0]).toEqual({ label: '默认尺寸', value: '' })
  })

  it('carries unique non-empty values', () => {
    const values = SIZE_PRESETS.slice(1).map((p) => p.value)
    expect(new Set(values).size).toBe(values.length)
    expect(values.every((v) => v.length > 0)).toBe(true)
  })
})
