import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { RELYRIC_STRENGTH_DEFAULT, RELYRIC_STRENGTH_MAX, RELYRIC_STRENGTH_MIN, RELYRIC_STRENGTH_STEP } from '../../music'

/**
 * 同曲换词创作态「旋律保持强度」滑杆的存在性钉(80 号票):tsc/构建过
 * ≠ UI 存在,关键 UI 改动按 GroupNode.test.ts 的源码断言范式钉属性与
 * 交互 —— 滑杆挂在 relyric 创作态表单、参数走共享常量(防手抄漂移)、
 * 拖动即时回写节点 data(草稿保留纪律)、缺省物化为创作事实、payload
 * 恒显式送 cover_strength。
 */
const source = readFileSync(fileURLToPath(new URL('./AudioNode.vue', import.meta.url)), 'utf8')

/**
 * 提交链 payload 纪律钉(80 号票定案 8):cover_strength 恒显式送 —— 与
 * 79 的「prompt 非空才送」不同,数值型没有空串歧义,缺省 0.55 也送。
 * submitRelyricTask 内联在编辑器视图里(79 先例,无纯逻辑缝),视图又无
 * 既有测试形状,按本文件同款源码断言钉住(与 AudioNode 的 80 号票钉同
 * 居一处,便于同票回看)。
 */
const editorSource = readFileSync(fileURLToPath(new URL('../../views/CanvasEditorView.vue', import.meta.url)), 'utf8')

describe('CanvasEditorView relyric 提交链 cover_strength(80 号票)', () => {
  it('payload 恒显式送 cover_strength,缺值以共享缺省常量兜底(不手抄 0.55)', () => {
    expect(editorSource.includes('cover_strength: data.strength ?? RELYRIC_STRENGTH_DEFAULT')).toBe(true)
    expect(editorSource.includes("import { RELYRIC_STRENGTH_DEFAULT } from '../music'")).toBe(true)
  })
})

describe('AudioNode 旋律保持强度滑杆(80 号票)', () => {
  it('relyric 创作态挂了 range 滑杆:min/max/step 全走共享常量', () => {
    expect(source.includes('type="range"')).toBe(true)
    expect(source.includes('aria-label="旋律保持强度"')).toBe(true)
    expect(source.includes(':min="RELYRIC_STRENGTH_MIN"')).toBe(true)
    expect(source.includes(':max="RELYRIC_STRENGTH_MAX"')).toBe(true)
    expect(source.includes(':step="RELYRIC_STRENGTH_STEP"')).toBe(true)
    expect(source.includes('RELYRIC_STRENGTH_MIN')).toBe(true)
    expect(source.includes('RELYRIC_STRENGTH_MAX')).toBe(true)
    expect(source.includes('RELYRIC_STRENGTH_STEP')).toBe(true)
  })

  it('滑杆参数与共享常量同源(0.3–0.9 步进 0.05 缺省 0.55,票面定案 5)', () => {
    expect(RELYRIC_STRENGTH_MIN).toBe(0.3)
    expect(RELYRIC_STRENGTH_MAX).toBe(0.9)
    expect(RELYRIC_STRENGTH_STEP).toBe(0.05)
    expect(RELYRIC_STRENGTH_DEFAULT).toBe(0.55)
  })

  it('旁边数值实时显示 + 语义文案「越高越贴原曲旋律」在场', () => {
    expect(source.includes('relyricStrength.toFixed(2)')).toBe(true)
    expect(source.includes('越高越贴原曲旋律')).toBe(true)
  })

  it('拖动即时回写节点 data(form-change 带 strength,草稿保留纪律)', () => {
    expect(source.includes('function onRelyricStrength')).toBe(true)
    expect(source.includes("emit('form-change', { strength: v })")).toBe(true)
  })

  it('进入 relyric 创作态物化缺省强度(创作事实随节点落,提交恒显式送的前置)', () => {
    expect(source.includes("emit('form-change', { strength: RELYRIC_STRENGTH_DEFAULT })")).toBe(true)
  })

  it('relyricFormInput 带 strength(校验面吃 0–1 防呆守卫)', () => {
    expect(source.includes('strength: props.data.strength ?? RELYRIC_STRENGTH_DEFAULT')).toBe(true)
  })

  it('滑杆 accent 走紫罗兰 token(与「人声歌曲」开关同款,面板 token 纪律)', () => {
    expect(source.includes('.strength-input')).toBe(true)
    expect(source.includes('accent-color: var(--violet)')).toBe(true)
  })
})
