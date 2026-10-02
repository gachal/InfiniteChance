import { describe, expect, it } from 'vitest'

import {
  AGENT_MEDIA_CAPS,
  type PendingAgentMedia,
  hasAgentMediaSlot,
  isAgentMediaUploading,
  nextAgentMediaId,
  readyAgentMediaRefs,
} from './agentMedia'

function item(partial: Partial<PendingAgentMedia>): PendingAgentMedia {
  return { id: nextAgentMediaId(), kind: 'image', state: 'ready', ...partial }
}

describe('hasAgentMediaSlot', () => {
  it('allows up to 4 images and 1 video per message', () => {
    let list: PendingAgentMedia[] = []
    for (let i = 0; i < AGENT_MEDIA_CAPS.image; i++) {
      expect(hasAgentMediaSlot(list, 'image')).toBe(true)
      list = [...list, item({ kind: 'image' })]
    }
    // 第 5 张图拒收;视频空位不受图片占满影响(混合允许)。
    expect(hasAgentMediaSlot(list, 'image')).toBe(false)
    expect(hasAgentMediaSlot(list, 'video')).toBe(true)
    list = [...list, item({ kind: 'video' })]
    expect(hasAgentMediaSlot(list, 'video')).toBe(false)
  })

  it('counts uploading and failed chips toward the caps', () => {
    const list = [item({ kind: 'video', state: 'uploading' }), item({ kind: 'video', state: 'failed' })]
    // 失败 chip 占位:不移除就补不进新视频,避免发送时悄悄超限。
    expect(hasAgentMediaSlot(list, 'video')).toBe(false)
  })
})

describe('readyAgentMediaRefs', () => {
  it('collects only ready chips into the server media shape', () => {
    const list = [
      item({ kind: 'image', ref: '/api/assets/6/content', assetId: 6 }),
      item({ kind: 'video', state: 'uploading' }),
      item({ kind: 'image', state: 'failed', error: '上传失败' }),
      item({ kind: 'video', ref: '/api/assets/5/content', assetId: 5 }),
    ]
    expect(readyAgentMediaRefs(list)).toEqual([
      { ref: '/api/assets/6/content', kind: 'image' },
      { ref: '/api/assets/5/content', kind: 'video' },
    ])
  })
})

describe('isAgentMediaUploading', () => {
  it('blocks send while any upload is in flight', () => {
    expect(isAgentMediaUploading([item({ state: 'ready' })])).toBe(false)
    expect(isAgentMediaUploading([item({ state: 'ready' }), item({ state: 'uploading' })])).toBe(true)
  })
})
