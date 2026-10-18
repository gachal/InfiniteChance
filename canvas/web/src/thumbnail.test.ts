import { describe, expect, it } from 'vitest'

import { thumbnailURLOf, useThumbFallback } from './thumbnail'

describe('thumbnailURLOf', () => {
  it('从内容寻址路径派生缩略地址', () => {
    expect(thumbnailURLOf('/api/assets/42/content')).toBe('/api/assets/42/thumb')
  })

  it('带查询串的内容寻址路径同样派生', () => {
    expect(thumbnailURLOf('/api/assets/7/content?download=1')).toBe('/api/assets/7/thumb')
  })

  it('厂商地址、data: URI 与其余路径返回 null(保持原图)', () => {
    expect(thumbnailURLOf('https://vendor.example/img.png')).toBeNull()
    expect(thumbnailURLOf('data:image/png;base64,aGk=')).toBeNull()
    expect(thumbnailURLOf('/api/assets/42/thumb')).toBeNull()
    expect(thumbnailURLOf('')).toBeNull()
  })
})

describe('useThumbFallback', () => {
  it('displayURL 缺省派生缩略,onThumbError 后回落原图', () => {
    const thumbs = useThumbFallback()
    expect(thumbs.displayURL('/api/assets/42/content')).toBe('/api/assets/42/thumb')

    expect(thumbs.onThumbError('/api/assets/42/content')).toBe(true)
    expect(thumbs.displayURL('/api/assets/42/content')).toBe('/api/assets/42/content')
  })

  it('显式传入 thumbnail_url 时直吃字段(AssetRecord 直挂点)', () => {
    const thumbs = useThumbFallback()
    expect(thumbs.displayURL('/api/assets/9/content', '/api/assets/9/thumb')).toBe('/api/assets/9/thumb')
    expect(thumbs.displayURL('/api/assets/9/content', '')).toBe('/api/assets/9/thumb')
    expect(thumbs.onThumbError('/api/assets/9/content')).toBe(true)
    expect(thumbs.displayURL('/api/assets/9/content', '/api/assets/9/thumb')).toBe('/api/assets/9/content')
  })

  it('无缩略地址(厂商地址)的 onThumbError 返回 false 且不记簿', () => {
    const thumbs = useThumbFallback()
    expect(thumbs.displayURL('https://vendor.example/img.png')).toBe('https://vendor.example/img.png')
    expect(thumbs.onThumbError('https://vendor.example/img.png')).toBe(false)
    expect(thumbs.displayURL('https://vendor.example/img.png')).toBe('https://vendor.example/img.png')
  })

  it('已回落的行重复 onThumbError 返回 false(原图自身失败交给调用方)', () => {
    const thumbs = useThumbFallback()
    const url = '/api/assets/1/content'
    expect(thumbs.onThumbError(url)).toBe(true)
    expect(thumbs.onThumbError(url)).toBe(false)
  })

  it('不同原图地址互不传染', () => {
    const thumbs = useThumbFallback()
    expect(thumbs.onThumbError('/api/assets/1/content')).toBe(true)
    expect(thumbs.displayURL('/api/assets/2/content')).toBe('/api/assets/2/thumb')
  })
})
