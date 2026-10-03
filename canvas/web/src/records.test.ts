import { describe, expect, it } from 'vitest'

import type { CanvasTask } from '@infinitechance/api'

import {
  canInsertRecord,
  formatRecordTime,
  recordKindLabel,
  recordStatusLabel,
  recordThumbURL,
  truncatePrompt,
} from './records'

function task(overrides: Partial<CanvasTask>): CanvasTask {
  return {
    id: 'ct_abc',
    canvas_id: 7,
    node_id: 'image-1-1',
    kind: 'image',
    prompt: '一只猫',
    model: 'img-m',
    size: '1024x1024',
    ratio: '',
    background: '',
    seconds: 0,
    status: 'succeeded',
    attempts: 1,
    error: '',
    asset_id: 5,
    image_url: 'https://img.example/a.png',
    video_url: '',
    created_at: '2026-09-30T08:05:00Z',
    updated_at: '2026-09-30T08:06:00Z',
    ...overrides,
  }
}

describe('recordThumbURL(28 号票)', () => {
  it('成功且有素材引用 → 内容寻址地址', () => {
    expect(recordThumbURL(task({}))).toBe('/api/assets/5/content')
  })

  it('无素材引用回落厂商地址:图片走 image_url,视频走 video_url', () => {
    expect(recordThumbURL(task({ asset_id: 0 }))).toBe('https://img.example/a.png')
    expect(
      recordThumbURL(task({ asset_id: 0, kind: 'video', video_url: 'https://v.example/a.mp4', image_url: '' })),
    ).toBe('https://v.example/a.mp4')
  })

  it('两者皆空 → 空串(由渲染层显示占位)', () => {
    expect(recordThumbURL(task({ asset_id: 0, image_url: '' }))).toBe('')
  })
})

describe('canInsertRecord(28 号票)', () => {
  it('成功且有素材引用的行可插入', () => {
    expect(canInsertRecord(task({}))).toBe(true)
  })

  it('非成功态不可插入(失败/取消/排队/生成中)', () => {
    for (const status of ['queued', 'running', 'failed', 'canceled'] as const) {
      expect(canInsertRecord(task({ status }))).toBe(false)
    }
  })

  it('成功但无素材引用不可插入(没有可复用的持久地址)', () => {
    expect(canInsertRecord(task({ asset_id: 0 }))).toBe(false)
  })
})

describe('recordStatusLabel / recordKindLabel', () => {
  it('状态与种类的中文文案', () => {
    expect(recordStatusLabel('queued')).toBe('排队中')
    expect(recordStatusLabel('running')).toBe('生成中')
    expect(recordStatusLabel('succeeded')).toBe('已成功')
    expect(recordStatusLabel('failed')).toBe('失败')
    expect(recordStatusLabel('canceled')).toBe('已取消')
    expect(recordKindLabel('image')).toBe('图片')
    expect(recordKindLabel('video')).toBe('视频')
  })
})

describe('truncatePrompt', () => {
  it('短文本原样返回', () => {
    expect(truncatePrompt('一只猫', 20)).toBe('一只猫')
  })

  it('超长文本截断加省略号,长度恒不超上限', () => {
    const long = '喵'.repeat(50)
    const out = truncatePrompt(long, 20)
    expect(out).toHaveLength(20)
    expect(out.endsWith('…')).toBe(true)
  })

  it('空提示词给出占位文案', () => {
    expect(truncatePrompt('', 20)).toBe('(无提示词)')
  })
})

describe('formatRecordTime', () => {
  it('RFC3339 时间格式化为 月-日 时:分(按浏览本地时区渲染)', () => {
    const iso = '2026-09-30T08:05:00Z'
    // 期望值按同一 Date 的本地字段拼出:断言锁定的是格式(分隔符、补零、
    // 字段选择),不把测试绑死在某个时区上。
    const d = new Date(iso)
    const pad = (n: number) => String(n).padStart(2, '0')
    const expected = `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
    expect(formatRecordTime(iso)).toBe(expected)
  })

  it('无法解析的时间返回原串', () => {
    expect(formatRecordTime('')).toBe('')
  })
})
