// 85 号票 A:AssetPicker 多选快速拉取的源码钉子(组件无挂载测试,按
// PanoramaNode.test.ts / editorPassthrough.test.ts 的 readFileSync 范式)。
// 钉多选是挑选器内存态加层:单选既有行为不动,勾选集跨分组切换保持,
// 确认时按勾选序逐条上抛、由编辑器走既有合并链 —— 不加新合并逻辑。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const pickerSource = readFileSync(
  fileURLToPath(new URL('./AssetPicker.vue', import.meta.url)),
  'utf8',
)
const editorSource = readFileSync(
  fileURLToPath(new URL('../views/CanvasEditorView.vue', import.meta.url)),
  'utf8',
)

/** 摘一个顶层函数的源码段(到第一个零列闭括号),断言按函数 scoped,
 * 防「别处清过、本函数漏了」误绿(editorPassthrough.test.ts 同款)。 */
function fnSource(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  expect(start).toBeGreaterThan(-1)
  const bodyEnd = src.indexOf('\n}', start)
  return src.slice(start, bodyEnd)
}

/** 摘一个 const 声明段(到下一个空行),用于 computed/ref 箭头段。 */
function declSource(src: string, name: string): string {
  const start = src.indexOf(`const ${name}`)
  expect(start).toBeGreaterThan(-1)
  const end = src.indexOf('\n\n', start)
  return src.slice(start, end)
}

describe('AssetPicker 多选模式(85 号票 A:挑选器内存态,纯 UI 无新实体)', () => {
  it('multiSelect 是可选 prop(缺省关,其余调用点行为不变)', () => {
    expect(pickerSource.includes('multiSelect?: boolean')).toBe(true)
  })

  it('confirm 事件上抛勾选序载荷', () => {
    expect(pickerSource.includes('confirm: [picks:')).toBe(true)
    expect(fnSource(pickerSource, 'confirmPicks').includes("emit('confirm'")).toBe(true)
  })

  it('勾选集是组件内存态(Map,按 assetId),确认后清空、面板关闭即散(不持久化)', () => {
    expect(pickerSource.includes('new Map<number,')).toBe(true)
    const body = fnSource(pickerSource, 'confirmPicks')
    expect(body.includes('clear')).toBe(true)
    expect(pickerSource.includes('localStorage')).toBe(false)
  })

  it('多选模式点选只进勾选集,不 emit toggle(单选语义 = 既有行为不变)', () => {
    const body = fnSource(pickerSource, 'onPick')
    expect(body.includes('multi.value')).toBe(true)
    expect(body.includes('toggleChecked(')).toBe(true)
    // multi 分支 return 在前,单选 emit('toggle') 保留(加层不改层)。
    expect(body.includes("emit('toggle', a, element)")).toBe(true)
  })

  it('勾选态跨分组切换/翻页保持(同一挑选器会话内):setView/setKind/setScope 都不清勾选集', () => {
    expect(fnSource(pickerSource, 'setView').includes('checked')).toBe(false)
    expect(fnSource(pickerSource, 'setKind').includes('checked')).toBe(false)
    expect(fnSource(pickerSource, 'setScope').includes('checked')).toBe(false)
    expect(fnSource(pickerSource, 'loadMore').includes('checked')).toBe(false)
  })

  it('退出多选模式清空勾选集(重开重新挑)', () => {
    const body = fnSource(pickerSource, 'toggleMultiSelect')
    expect(body.includes('multi.value = !multi.value')).toBe(true)
    expect(body.includes('clearChecked()')).toBe(true)
  })

  it('模板:多选开关只在 multiSelect 时渲染,确认条带勾选计数与禁用态', () => {
    expect(pickerSource.includes('v-if="multiSelect"')).toBe(true)
    expect(pickerSource.includes('multi-bar')).toBe(true)
    expect(pickerSource.includes('已勾选 {{ checked.size }} 张')).toBe(true)
    expect(pickerSource.includes(':disabled="checked.size === 0"')).toBe(true)
  })

  it('卡片高亮在多选模式按勾选集判定,单选模式维持 selectedAssetIds(两语义不串)', () => {
    // 评审修复轮:素材平铺/资产库两视图的高亮与提示统一到 id 一层
    // (cardSelectedId/cardTitleId),不再有 AssetRecord 形状的重复条件。
    expect(pickerSource.includes('cardSelected(')).toBe(false)
    expect(pickerSource.includes('cardTitle(')).toBe(false)
    const selected = declSource(pickerSource, 'cardSelectedId')
    expect(selected.includes('multi.value')).toBe(true)
    expect(selected.includes('checked.value.has')).toBe(true)
    const titled = declSource(pickerSource, 'cardTitleId')
    expect(titled.includes('取消勾选')).toBe(true)
    expect(titled.includes('加入勾选')).toBe(true)
  })
})

describe('编辑器多选确认接线(85 号票 A:逐条走既有合并链,不加新合并逻辑)', () => {
  it('composer 挑选面板开多选并接 confirm(其余 7 个 AssetPicker 调用点不传)', () => {
    expect(editorSource.includes(':multi-select="true"')).toBe(true)
    expect(editorSource.split(':multi-select').length - 1).toBe(1)
    expect(editorSource.includes('@confirm="onComposerPickConfirm"')).toBe(true)
  })

  it('确认按勾选序逐条应用:元素载荷走 pickComposerElement(mergeElementPick 既有守卫),散图走 onComposerPickToggle', () => {
    const body = fnSource(editorSource, 'onComposerPickConfirm')
    expect(body.includes('pickComposerElement(asset, element)')).toBe(true)
    expect(body.includes('onComposerPickToggle(asset)')).toBe(true)
  })

  it('hint 说明多选能力(角色/场景/道具视图跨变体勾选一次拉入)', () => {
    expect(editorSource.includes('多选模式')).toBe(true)
  })
})
