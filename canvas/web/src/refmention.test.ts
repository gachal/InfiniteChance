import { describe, expect, it } from 'vitest'

import type { PendingAgentMedia } from './agentMedia'
import type { ComposerRef, VideoComposerRef } from './composer'
import {
  agentAttachmentBadge,
  agentAttachmentMentions,
  composerRefMentions,
  insertMentionToken,
  mentionTriggerIndex,
  videoRefBadge,
  videoRefMentions,
} from './refmention'

const ref = (url: string): ComposerRef => ({ url })

const vref = (url: string, role: VideoComposerRef['role'], kind: VideoComposerRef['kind'] = 'image'): VideoComposerRef => ({
  url,
  kind,
  role,
})

const media = (id: number, kind: PendingAgentMedia['kind'], state: PendingAgentMedia['state'], refUrl?: string): PendingAgentMedia => ({
  id,
  kind,
  state,
  ...(refUrl ? { ref: refUrl } : {}),
})

describe('composerRefMentions', () => {
  it('图片模式:参考条全部条目按序编号', () => {
    const out = composerRefMentions([ref('a'), ref('b'), ref('c')])
    expect(out.map((c) => c.token)).toEqual(['@图1', '@图2', '@图3'])
    expect(out.every((c) => c.kind === 'image')).toBe(true)
    expect(out.map((c) => c.url)).toEqual(['a', 'b', 'c'])
  })

  it('空参考条得空列表(输入 @ 不弹层)', () => {
    expect(composerRefMentions([])).toEqual([])
  })
})

describe('videoRefMentions', () => {
  it('视频模式:仅参考图角色按序编号,首帧/尾帧/视频/音频不进', () => {
    const out = videoRefMentions([
      vref('first', 'first_frame'),
      vref('r1', 'reference_image'),
      vref('last', 'last_frame'),
      vref('r2', 'reference_image'),
      vref('clip', 'reference_video', 'video'),
      vref('voice', 'reference_audio', 'audio'),
    ])
    expect(out.map((c) => c.token)).toEqual(['@图1', '@图2'])
    expect(out.map((c) => c.url)).toEqual(['r1', 'r2'])
    expect(out.every((c) => c.kind === 'image' && c.roleLabel === '参考图')).toBe(true)
  })

  it('编号即 references 下标:前面全是非参考图角色时仍从 1 起', () => {
    const out = videoRefMentions([
      vref('first', 'first_frame'),
      vref('only', 'reference_image'),
    ])
    expect(out.map((c) => c.token)).toEqual(['@图1'])
  })
})

describe('agentAttachmentMentions', () => {
  it('图片与视频按各自插入序编号', () => {
    const out = agentAttachmentMentions([
      media(1, 'image', 'ready', 'a1'),
      media(2, 'video', 'ready', 'v1'),
      media(3, 'image', 'ready', 'a2'),
    ])
    expect(out.map((c) => c.token)).toEqual(['@图1', '@视频1', '@图2'])
    expect(out.map((c) => c.kind)).toEqual(['image', 'video', 'image'])
  })

  it('上传中/失败的附件不进浮层', () => {
    const out = agentAttachmentMentions([
      media(1, 'image', 'uploading'),
      media(2, 'image', 'failed'),
      media(3, 'image', 'ready', 'a1'),
    ])
    expect(out.map((c) => c.token)).toEqual(['@图1'])
  })
})

describe('videoRefBadge', () => {
  it('仅参考图角色编角标,序号只数参考图', () => {
    const refs = [
      vref('first', 'first_frame'),
      vref('r1', 'reference_image'),
      vref('last', 'last_frame'),
      vref('r2', 'reference_image'),
    ]
    expect(videoRefBadge(refs, 0)).toBe('')
    expect(videoRefBadge(refs, 1)).toBe('1')
    expect(videoRefBadge(refs, 2)).toBe('')
    expect(videoRefBadge(refs, 3)).toBe('2')
  })

  it('index 越界返回空串', () => {
    expect(videoRefBadge([vref('r', 'reference_image')], 5)).toBe('')
  })
})

describe('agentAttachmentBadge', () => {
  it('就绪图片显示图片序号、就绪视频显示「视频N」', () => {
    const list = [
      media(1, 'image', 'ready', 'a1'),
      media(2, 'video', 'ready', 'v1'),
      media(3, 'image', 'ready', 'a2'),
    ]
    expect(agentAttachmentBadge(list, 1)).toBe('1')
    expect(agentAttachmentBadge(list, 2)).toBe('视频1')
    expect(agentAttachmentBadge(list, 3)).toBe('2')
  })

  it('上传中/失败与未知 id 不编', () => {
    const list = [media(1, 'image', 'uploading'), media(2, 'video', 'ready', 'v1')]
    expect(agentAttachmentBadge(list, 1)).toBe('')
    expect(agentAttachmentBadge(list, 9)).toBe('')
  })
})

describe('mentionTriggerIndex', () => {
  it('光标前一字符是 @ 时返回其下标', () => {
    expect(mentionTriggerIndex('@', 1)).toBe(0)
    expect(mentionTriggerIndex('画一只@', 4)).toBe(3)
    expect(mentionTriggerIndex('前文@图1 后面', 3)).toBe(2)
  })

  it('其余情况返回 -1(行首、前字符不是 @、caret 越界)', () => {
    expect(mentionTriggerIndex('abc', 0)).toBe(-1)
    expect(mentionTriggerIndex('abc', 3)).toBe(-1)
    expect(mentionTriggerIndex('@图1', 4)).toBe(-1)
  })
})

describe('insertMentionToken', () => {
  it('把触发字符替换为 token,光标落在 token 之后', () => {
    expect(insertMentionToken('画一只@', 3, '@图2')).toEqual({ text: '画一只@图2', cursor: 6 })
  })

  it('正文中段插入时保留 @ 后的原文', () => {
    expect(insertMentionToken('用@的风格画山', 1, '@图3')).toEqual({
      text: '用@图3的风格画山',
      cursor: 4,
    })
  })

  it('同一参考可重复引用', () => {
    const once = insertMentionToken('@', 0, '@图1')
    expect(once.text).toBe('@图1')
    const base = `${once.text} 再来一次@`
    const twice = insertMentionToken(base, mentionTriggerIndex(base, base.length), '@图1')
    expect(twice.text).toBe('@图1 再来一次@图1')
  })
})
