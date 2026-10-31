// 通用键盘守卫(38 号票)的测试:isTypingTargetEl 判定口径
// (68 号票评审自 canvasHelp.test.ts 迁入,与 keyboardGuard.ts 同源)。
import { describe, expect, it } from 'vitest'

import { isTypingTargetEl } from './keyboardGuard'

const el = (tagName: string, isContentEditable = false): { tagName: string; isContentEditable: boolean } =>
  ({ tagName, isContentEditable })

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
