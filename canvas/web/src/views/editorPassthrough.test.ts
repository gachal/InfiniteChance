// 83 号票零迁移证据链钉子:整图 JSON 的未知字段透传 —— snapshot /
// normalizeNodes 原样带节点 data,character_variants 随节点 data 存取零迁移
// (「tsc/build 过 ≠ 字段不被将来的白名单剥掉」,按 PanoramaNode.test.ts 的
// 源码断言范式钉住);同时钉两条消费路径的接线与 Agent 域不透传(83 号票
// Out of Scope:save_storyboard 草稿逐字段挑拣,不得顺手带上绑定映射)。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const editorSource = readFileSync(
  fileURLToPath(new URL('./CanvasEditorView.vue', import.meta.url)),
  'utf8',
)
const agentToolsSource = readFileSync(
  fileURLToPath(new URL('../agentTools.ts', import.meta.url)),
  'utf8',
)
const layerSource = readFileSync(
  fileURLToPath(new URL('../components/StoryboardEditorLayer.vue', import.meta.url)),
  'utf8',
)

/** 摘一个顶层函数的源码段(到第一个零列闭括号),断言按函数 scoped,
 * 防「别处清过、本函数漏了」误绿(PanoramaNode.test.ts 同款)。 */
function fnSource(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  expect(start).toBeGreaterThan(-1)
  const bodyEnd = src.indexOf('\n}', start)
  return src.slice(start, bodyEnd)
}

describe('整图 JSON 未知字段透传(83 号票零迁移证据链)', () => {
  it('snapshot 原样透传节点 data(保存无字段白名单,character_variants 免费落库)', () => {
    expect(editorSource.includes('data: n.data as CanvasNodeData')).toBe(true)
  })

  it('normalizeNodes 原样透传节点 data(加载不丢 character_variants)', () => {
    const body = fnSource(editorSource, 'normalizeNodes')
    expect(body.includes('data: node.data ?? initialData(type)')).toBe(true)
    // 归一只在类型值层(normalizeNodeType),不触碰 data 内字段。
    expect(body.includes('normalizeNodeType')).toBe(true)
  })

  it('cut→画面直通接线:pickCutCharacterRefs 传 cut.character_variants(绑定优先)', () => {
    const body = fnSource(editorSource, 'onStoryboardGenerateFrame')
    expect(body.includes('pickCutCharacterRefs(')).toBe(true)
    expect(body.includes('cut.character_variants')).toBe(true)
  })

  it('cut→视频直通接线:buildCutVideoPrefill 收整只 cut(绑定随 Pick 透传)', () => {
    expect(
      editorSource.includes('buildCutVideoPrefill(cut, ipCharacters.value, VIDEO_REFS_TOTAL_CAP)'),
    ).toBe(true)
  })

  it('Agent 域不透传 character_variants(83 号票 Out of Scope,save_storyboard 契约不动)', () => {
    expect(agentToolsSource.includes('character_variants')).toBe(false)
  })
})

describe('cut 编辑层变体下拉接线(83 号票 A2)', () => {
  it('出场角色 chip 带逐角色变体下拉与回落态徽记,解析走共享纯函数', () => {
    expect(layerSource.includes('cast-variant')).toBe(true)
    expect(layerSource.includes('cast-fallback')).toBe(true)
    expect(layerSource.includes('resolveCutVariant')).toBe(true)
    expect(layerSource.includes('setCutVariantBinding')).toBe(true)
  })

  it('角色标注被目录收回时孤儿绑定条目随之收回(变体已删但角色在场的不清洗)', () => {
    expect(layerSource.includes('pruneCutVariantOrphans')).toBe(true)
  })
})

describe('角色配色段接线(84 号票 B2/B3)', () => {
  it('cut→画面直通:配色行经 buildPaletteEntries/Line 解析并传 buildCutFramePrompt', () => {
    const body = fnSource(editorSource, 'onStoryboardGenerateFrame')
    expect(body.includes('buildPaletteEntries(')).toBe(true)
    expect(body.includes('buildPaletteLine(')).toBe(true)
    expect(body.includes('cut.character_variants')).toBe(true)
    expect(body.includes('buildCutFramePrompt(cut, board?.brief ?? \'\', ipStyle.value, paletteLine)')).toBe(true)
  })

  it('cut→视频直通:配色行经 prefill 带回传 buildCutVideoPrompt(有画面恒空)', () => {
    const body = fnSource(editorSource, 'onStoryboardGenerateVideo')
    expect(body.includes('prefill.paletteLine')).toBe(true)
    expect(
      body.includes('buildCutVideoPrompt(cut, board?.brief ?? \'\', ipStyle.value, prefill.paletteLine)'),
    ).toBe(true)
  })

  it('角色设定图预填:facts 带变体配色(84 号票 B1 前端同形)', () => {
    const body = fnSource(editorSource, 'onCharacterGenerateSheet')
    expect(body.includes('palette: p.variant.palette || undefined')).toBe(true)
  })
})
