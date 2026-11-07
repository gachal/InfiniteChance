// 全景节点(81 号票)的存在性钉:按 GroupNode.test.ts / AudioNode.test.ts
// 的源码断言范式,「tsc/构建过 ≠ UI 存在」—— 关键 UI 与交互链直接钉住
// 源码:360° 角标、参考 chip 的场景图注记与「直接环视」按钮、无描述框、
// 不渲染把手,以及编辑器提交链的固定契约(scene_type/prompt 空串/内容寻
// 址参考、产物回填走 panoramaSyncPatch、截图上传仅入素材库)。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { PANO_DEFAULT_MODEL, PANO_SCENE_TYPE, isPanoRatio, panoRatioDeviation } from '../../panorama'

const nodeSource = readFileSync(fileURLToPath(new URL('./PanoramaNode.vue', import.meta.url)), 'utf8')
/** 模板段(掐掉 script 注释):选项/控件的存在性钉看渲染面,不看注释。 */
const nodeTemplate = nodeSource.slice(nodeSource.indexOf('<template>'))
const editorSource = readFileSync(fileURLToPath(new URL('../../views/CanvasEditorView.vue', import.meta.url)), 'utf8')
const viewerSource = readFileSync(fileURLToPath(new URL('../PanoramaViewer.vue', import.meta.url)), 'utf8')
const drawerSource = readFileSync(fileURLToPath(new URL('../AssetDrawer.vue', import.meta.url)), 'utf8')
const graphSource = readFileSync(fileURLToPath(new URL('../../graph.ts', import.meta.url)), 'utf8')

describe('PanoramaNode 卡面(81 号票)', () => {
  it('卡头与产物态都带 360° 角标(卡面不跑 WebGL,2:1 缩略)', () => {
    expect(nodeSource.includes('badge-360')).toBe(true)
    expect(nodeSource.includes('360°')).toBe(true)
    // 卡面只有 <img> 缩略,不出现 three.js/WebGL。
    expect(nodeSource.includes('three')).toBe(false)
    expect(nodeSource.includes('WebGL')).toBe(false)
  })

  it('参考图 chip 旁带「须为场景图」注记(人像/物体会被上游拒绝)', () => {
    expect(nodeSource.includes('须为场景图')).toBe(true)
    expect(nodeSource.includes('人像/物体会被上游拒绝')).toBe(true)
  })

  it('参考图 ≈2:1 时出「直接环视」按钮,判定走共享纯函数(不手抄阈值)', () => {
    expect(nodeSource.includes('直接环视')).toBe(true)
    expect(nodeSource.includes('isPanoRatio')).toBe(true)
    expect(nodeSource.includes('from \'../../panorama\'')).toBe(true)
  })

  it('无描述框/无尺寸比例/无透明选项(上游固定 8K 2:1,不吃提示词)', () => {
    expect(nodeTemplate.includes('<textarea')).toBe(false)
    // 钉模板不钉注释:透明/尺寸选项若存在必然带「透明」文案与比例组合表
    // 引用;裸词 transparent 会误伤样式表的 background: transparent。
    expect(nodeTemplate.includes('透明')).toBe(false)
    expect(nodeTemplate.includes('composeSize')).toBe(false)
    expect(nodeTemplate.includes('RATIO_PRESETS')).toBe(false)
  })

  it('不渲染把手与 + 按钮(不参与任何连线)', () => {
    expect(nodeSource.includes('<NodePorts')).toBe(false)
    expect(nodeSource.includes('<Handle')).toBe(false)
    expect(nodeSource.includes('@vue-flow/core')).toBe(false)
  })

  it('产物态整卡点击进查看层、表单区不进(@click.stop)', () => {
    expect(nodeSource.includes('emit(\'view\'')).toBe(true)
    expect(nodeSource.includes('class="pano-form"')).toBe(true)
  })

  it('产物校验提示:偏离 2:1 超 2% 时卡面提示行(照常可看可重生成)', () => {
    expect(nodeSource.includes('panoRatioDeviation')).toBe(true)
    expect(nodeSource.includes('重新生成')).toBe(true)
  })

  it('模型缺省物化走共享常量(不手抄 hunyuan-world-panorama)', () => {
    expect(nodeSource.includes('PANO_DEFAULT_MODEL')).toBe(true)
    expect(PANO_DEFAULT_MODEL).toBe('hunyuan-world-panorama')
  })
})

describe('编辑器全景提交链(81 号票固定契约)', () => {
  it('createInput:kind=image + prompt 恒空串 + scene_type 常量 + 参考图按内容寻址恰好 1 条', () => {
    expect(editorSource.includes('scene_type: PANO_SCENE_TYPE')).toBe(true)
    expect(PANO_SCENE_TYPE).toBe('3d_panorama')
    // prompt 恒空串(上游不吃提示词);image_urls 恒恰 1 条(参考场景图)。
    expect(editorSource.includes('prompt: \'\',')).toBe(true)
    expect(editorSource.includes('image_urls: [assetContentURL')).toBe(true)
  })

  it('产物回填走 panoramaSyncPatch(原地替换),节点类型分派在 syncTaskToCanvas', () => {
    expect(editorSource.includes("node.type === 'panorama'")).toBe(true)
    expect(editorSource.includes('panoramaSyncPatch(')).toBe(true)
  })

  it('查看层入口 = 产物态整卡点击(编辑器级单实例,z 90 族编辑层档)', () => {
    expect(editorSource.includes('PanoramaViewer')).toBe(true)
    expect(viewerSource.includes('Teleport')).toBe(true)
    expect(viewerSource.includes('var(--z-editor-layer)')).toBe(true)
  })

  it('查看层截图:导出 JPEG 经 /api/assets/upload 落 uploads 档,仅入素材库不落节点', () => {
    expect(editorSource.includes('onPanoramaCapture')).toBe(true)
    expect(editorSource.includes("uploadAsset(file, 'image', uploadIPId.value)")).toBe(true)
    // 截图不落节点:capture 链没有 addNodes/insertAssetRef。
    expect(editorSource.includes('function onPanoramaCapture')).toBe(true)
  })

  it('直接环视入口在素材抽屉画廊(≈2:1 图行,AssetPicker 同族)', () => {
    expect(drawerSource.includes('直接环视')).toBe(true)
    expect(drawerSource.includes('isPanoRatio')).toBe(true)
    expect(drawerSource.includes('PanoramaViewer')).toBe(true)
  })

  it('graph 注册完整:类型值/初始数据/中文标签三件套在册', () => {
    expect(graphSource.includes("panorama: '全景'")).toBe(true)
    expect(graphSource.includes('ref_asset_id: 0')).toBe(true)
  })
})

describe('2:1 判定边界复钉(与 panorama.test.ts 同源常量)', () => {
  it('7680×3840 命中、16:9 拒绝(实现裁量:边界带数值容差)', () => {
    expect(isPanoRatio(7680, 3840)).toBe(true)
    expect(isPanoRatio(1920, 1080)).toBe(false)
    expect(panoRatioDeviation(7680, 3840)).toBe(0)
  })
})
