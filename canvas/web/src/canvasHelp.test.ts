// 画布帮助(68 号票)内容模块的测试:测「内容即数据」的结构规范与两枚
// 快捷键纯函数。内容层测试的对象是规范不变量而非具体文案 —— 改文案/加章
// 只动 canvasHelp.ts,这里保证它不被改破(章末「边界与后续」、块非空、
// 键位表成对)。组件渲染层不测(无 @vue/test-utils,单用户 YAGNI)。
import { describe, expect, it } from 'vitest'

import {
  BOUNDARY_HEADING,
  CANVAS_HELP_DOC,
  isHelpToggleKey,
  isTypingTargetEl,
  type HelpBlock,
  type HelpChapter,
} from './canvasHelp'

const keyEvent = (over: Partial<KeyboardEvent> = {}): KeyboardEvent =>
  ({ key: 'a', ctrlKey: false, metaKey: false, altKey: false, ...over }) as KeyboardEvent

const el = (tagName: string, isContentEditable = false): { tagName: string; isContentEditable: boolean } =>
  ({ tagName, isContentEditable })

/** 章内最后一个 heading 之后是否紧跟「边界与后续」段。 */
function boundaryBlocks(chapter: HelpChapter): HelpBlock[] {
  const index = chapter.blocks.map((b) => b.kind === 'heading' && b.text).lastIndexOf(BOUNDARY_HEADING)
  return index === -1 ? [] : chapter.blocks.slice(index)
}

describe('CANVAS_HELP_DOC 结构规范', () => {
  it('文档有标题与至少一章,章 id 唯一、标题非空、块非空', () => {
    expect(CANVAS_HELP_DOC.title.length).toBeGreaterThan(0)
    expect(CANVAS_HELP_DOC.chapters.length).toBeGreaterThanOrEqual(1)
    const ids = CANVAS_HELP_DOC.chapters.map((c) => c.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const ch of CANVAS_HELP_DOC.chapters) {
      expect(ch.title.length).toBeGreaterThan(0)
      expect(ch.blocks.length).toBeGreaterThan(0)
    }
  })

  it('每章以「边界与后续」收尾:标题块后至少一条边界注记', () => {
    for (const ch of CANVAS_HELP_DOC.chapters) {
      const tail = boundaryBlocks(ch)
      expect(tail.length, `章「${ch.title}」缺「${BOUNDARY_HEADING}」小节`).toBeGreaterThan(1)
      expect(tail[0].kind).toBe('heading')
      expect(tail.slice(1).every((b) => b.kind === 'note')).toBe(true)
    }
  })

  it('所有块的文字与条目非空(列表项、键位对、注记都不得留白)', () => {
    for (const ch of CANVAS_HELP_DOC.chapters) {
      for (const b of ch.blocks) {
        if (b.kind === 'heading' || b.kind === 'p' || b.kind === 'note') {
          expect(b.text.trim().length, `${ch.title} 有空块`).toBeGreaterThan(0)
        } else if (b.kind === 'list') {
          expect(b.items.length, `${ch.title} 有空列表`).toBeGreaterThan(0)
          for (const item of b.items) {
            expect(item.trim().length).toBeGreaterThan(0)
          }
        } else {
          expect(b.entries.length, `${ch.title} 有空键位表`).toBeGreaterThan(0)
          for (const e of b.entries) {
            expect(e.keys.trim().length).toBeGreaterThan(0)
            expect(e.label.trim().length).toBeGreaterThan(0)
          }
        }
      }
    }
  })
})

describe('isHelpToggleKey', () => {
  it('? 键触发(Shift+/ 的 e.key 即 ?)', () => {
    expect(isHelpToggleKey(keyEvent({ key: '?', shiftKey: true }))).toBe(true)
  })

  it('带 ctrl/meta/alt 修饰键不触发(不劫持组合键)', () => {
    expect(isHelpToggleKey(keyEvent({ key: '?', ctrlKey: true }))).toBe(false)
    expect(isHelpToggleKey(keyEvent({ key: '?', metaKey: true }))).toBe(false)
    expect(isHelpToggleKey(keyEvent({ key: '?', altKey: true }))).toBe(false)
  })

  it('其余按键不触发', () => {
    expect(isHelpToggleKey(keyEvent({ key: '/' }))).toBe(false)
    expect(isHelpToggleKey(keyEvent({ key: 'f' }))).toBe(false)
    expect(isHelpToggleKey(keyEvent({ key: 'Escape' }))).toBe(false)
  })
})

describe('isTypingTargetEl', () => {
  it('输入框/文本域/下拉/可编辑区都算输入中', () => {
    expect(isTypingTargetEl(el('INPUT'))).toBe(true)
    expect(isTypingTargetEl(el('TEXTAREA'))).toBe(true)
    expect(isTypingTargetEl(el('SELECT'))).toBe(true)
    expect(isTypingTargetEl(el('DIV', true))).toBe(true)
  })

  it('普通元素与空目标不算输入中', () => {
    expect(isTypingTargetEl(el('DIV'))).toBe(false)
    expect(isTypingTargetEl(el('BUTTON'))).toBe(false)
    expect(isTypingTargetEl(null)).toBe(false)
    expect(isTypingTargetEl(undefined)).toBe(false)
  })
})
