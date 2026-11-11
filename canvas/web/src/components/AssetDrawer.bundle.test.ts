// 成套生成的装配钉子(83 号票 B):按 PanoramaNode.test.ts 的源码断言范式
// 钉住装配纪律 ——「成套生成」入口在变体节、默认勾选走共享常量(不手抄三类
// 字面)、前端循环串行逐笔 createIPTask(端点零改动,不新建批量端点)、参考
// 图发起时统一冻结一次(不串联后续笔)、逐笔独立失败不回滚、对话框内逐笔
// 进度列表、成功逐笔锚定照 70 号票机制(createCharacterImage,label = 类型)。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const drawerSource = readFileSync(
  fileURLToPath(new URL('./AssetDrawer.vue', import.meta.url)),
  'utf8',
)

/** 摘一个顶层函数的源码段(到第一个零列闭括号),断言按函数 scoped。 */
function fnSource(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  expect(start).toBeGreaterThan(-1)
  const bodyEnd = src.indexOf('\n}', start)
  return src.slice(start, bodyEnd)
}

describe('成套生成入口与对话框(83 号票 B)', () => {
  it('变体节有「成套生成」入口(仅角色)', () => {
    expect(drawerSource.includes('成套生成')).toBe(true)
  })

  it('默认勾选走共享常量 BUNDLE_DEFAULT_TYPES(不手抄三类字面)', () => {
    expect(drawerSource.includes('BUNDLE_DEFAULT_TYPES')).toBe(true)
    expect(drawerSource.includes("from '../sheetBundle'")).toBe(true)
  })

  it('端点零改动:逐笔走 client.createIPTask,不出现手拼任务端点', () => {
    const body = fnSource(drawerSource, 'submitBundle')
    expect(body.includes('client.createIPTask')).toBe(true)
    // 不新建批量端点:全文件无手写 /tasks 拼串(api 封装统一在 packages/api)。
    expect(drawerSource.includes("'/tasks")).toBe(false)
    expect(drawerSource.includes('`/ips/')).toBe(false)
  })

  it('串行纪律:循环逐笔 await,逐笔独立 try/catch(单笔失败不回滚其余笔)', () => {
    const body = fnSource(drawerSource, 'submitBundle')
    expect(body.includes('await client.createIPTask')).toBe(true)
    expect(body.includes('try {')).toBe(true)
    expect(body.includes('catch')).toBe(true)
  })

  it('发起时统一冻结参考图(不串联后续笔):冻结在循环之外', () => {
    const body = fnSource(drawerSource, 'submitBundle')
    const loopStart = body.indexOf('for (')
    expect(loopStart).toBeGreaterThan(-1)
    const frozen = body.slice(0, loopStart)
    expect(frozen.includes('latestVariantImage')).toBe(true)
    // 冻结后的值在循环内使用,循环内不再重取变体图。
    expect(frozen.includes('const bundleRefAssetId')).toBe(true)
    expect(body.slice(loopStart).includes('latestVariantImage')).toBe(false)
  })

  it('逐笔进度列表:每行一类型一状态(bundleRows 渲染)', () => {
    expect(drawerSource.includes('bundleRows')).toBe(true)
    expect(drawerSource.includes('class="bundle-row"')).toBe(true)
  })

  it('逐笔锚定照 70 号票机制:成功后 createCharacterImage(变体 + label = 类型)', () => {
    const body = fnSource(drawerSource, 'pollBundle')
    expect(body.includes('client.getIPTask')).toBe(true)
    expect(body.includes('createCharacterImage')).toBe(true)
  })

  it('总价预估走共享纯函数 bundleEstimateText(一次人工确认)', () => {
    expect(drawerSource.includes('bundleEstimateText')).toBe(true)
  })
})
