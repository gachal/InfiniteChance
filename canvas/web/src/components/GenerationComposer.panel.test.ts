// 90 号票:生成对话框无边框重排 + 参数面板化的结构钉子(readFileSync
// 范式,组件无挂载测试,AssetPicker.multiselect.test.ts 同款)。钉三段
// 结构、无边框语义、参数面板两形态、时长滑杆×自动档、¥ 胶囊、展开两档;
// 纯逻辑行为面在 composerPanel.test.ts / useComposerPlacement.test.ts。
// 改结构必须连本钉同改;89 号票 hints 钉(GenerationComposer.hints.test.ts)
// 的提示行文案与出口不受本票影响,两钉并存。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const composerSource = readFileSync(
  fileURLToPath(new URL('./GenerationComposer.vue', import.meta.url)),
  'utf8',
)
const viewSource = readFileSync(
  fileURLToPath(new URL('../views/CanvasEditorView.vue', import.meta.url)),
  'utf8',
)

/** 摘 const 声明段(到下一个空行),用于 computed 箭头段。 */
function declSource(src: string, name: string): string {
  const start = src.indexOf(`const ${name}`)
  expect(start).toBeGreaterThan(-1)
  const end = src.indexOf('\n\n', start)
  return src.slice(start, end)
}

describe('三段结构重排(90 号票:动作行 → 输入区 → meta 行)', () => {
  it('顶部动作行:@ 引用 / + 上传 / 库 聚左,展开 toggle 居右', () => {
    expect(composerSource.includes('class="composer-actions"')).toBe(true)
    expect(composerSource.includes('aria-label="插入 @ 引用参考"')).toBe(true)
    expect(composerSource.includes('aria-label="上传参考"')).toBe(true)
    expect(composerSource.includes('aria-label="从素材库挑选参考"')).toBe(true)
    expect(composerSource.includes('class="expand-toggle"')).toBe(true)
    expect(composerSource.includes('@click="insertAtMention"')).toBe(true)
  })

  it('@ 引用按钮复用既有浮层语义:光标处插 @ 后走 syncMention,零新触发判定', () => {
    const body = declSource(composerSource, 'insertAtMention')
    expect(body.includes("emit('update:prompt'")).toBe(true)
    expect(body.includes('syncMention()')).toBe(true)
    // 触发/键盘判定仍全在既有链路:syncMention 与 onPromptKeydown 原样在。
    expect(composerSource.includes('function syncMention')).toBe(true)
    expect(composerSource.includes('function onPromptKeydown')).toBe(true)
  })

  it('参考条收编:ref-add / ref-library 不再在条内,+ / 库 上移动作行(可用面逐字保留)', () => {
    expect(composerSource.includes('class="ref-add"')).toBe(false)
    expect(composerSource.includes('class="ref-library"')).toBe(false)
    const body = declSource(composerSource, 'addRefDisabled')
    expect(body.includes('props.uploading || !videoRefsVacant.value')).toBe(true)
    expect(body.includes('props.uploading || refsFull.value || props.disabledReason')).toBe(true)
  })

  it('大留白输入区:textarea 120/240 两档、resize 手柄移除、宽恒 560 不动', () => {
    expect(composerSource.includes('min-height: 120px')).toBe(true)
    expect(composerSource.includes('min-height: 240px')).toBe(true)
    expect(composerSource.includes('resize: vertical')).toBe(false)
    expect(composerSource.includes('width: 560px')).toBe(true)
    // 展开档挂在 composer 容器类上(placement 两态与样式档同源)。
    expect(composerSource.includes(":class=\"{ disabled: disabledReason !== '', expanded }\"")).toBe(true)
    expect(composerSource.includes('.composer.expanded textarea')).toBe(true)
  })

  it('底部 meta 行:模型下拉 + 参数摘要 + ¥ 胶囊 + 圆形发送单行收纳', () => {
    expect(composerSource.includes('class="composer-meta"')).toBe(true)
    expect(composerSource.includes('class="param-summary"')).toBe(true)
    expect(composerSource.includes('class="cost-pill"')).toBe(true)
    expect(composerSource.includes('class="send"')).toBe(true)
  })

  it('89 号票 hairline 分组退役:composer-foot 与内部 border-top 一并不在', () => {
    expect(composerSource.includes('composer-foot')).toBe(false)
    expect(composerSource.includes('border-top')).toBe(false)
    // 独立预估行退役(¥ 胶囊接棒),meta 行内不再有 estimate 段落。
    expect(composerSource.includes('class="estimate"')).toBe(false)
  })

  it('无边框语义:壳体与内部控件的 line-control 描边清零,填充代描边', () => {
    expect(composerSource.includes('border: 1px solid var(--line-control)')).toBe(false)
    expect(composerSource.includes('background: rgba(var(--surface-panel-rgb), 0.98)')).toBe(true)
    // 投影弱化为单层极淡(token 词汇表内最轻的一档),玻璃受光面退场。
    expect(composerSource.includes('box-shadow: var(--shadow-control);')).toBe(true)
    expect(composerSource.includes('--glass-highlight')).toBe(false)
    expect(composerSource.includes('--shadow-overlay')).toBe(false)
    // 发送键 89 号票 hover/active 微动效保留。
    expect(composerSource.includes('.send:hover:not(:disabled)')).toBe(true)
    expect(composerSource.includes('.send:active:not(:disabled)')).toBe(true)
  })

  it('中间件安置:vref-hint / 绑定行预览 / sheet-types 在输入区与 meta 行之间,顺序不变', () => {
    const order = [
      'class="prompt-wrap"',
      'class="vref-hint"',
      'class="prompt-meta"',
      'class="composer-meta"',
    ]
    const at = order.map((s) => composerSource.indexOf(s))
    expect(at.every((i) => i > -1)).toBe(true)
    expect([...at].sort((a, b) => a - b)).toEqual(at)
  })
})

describe('参数面板(90 号票 Q5a/Q10a:摘要按钮 + 弹出面板两形态)', () => {
  it('模式下拉与参数 select 六件套退役:controls select 不在,模型独立下拉留 meta 行', () => {
    expect(composerSource.includes('class="controls"')).toBe(false)
    expect(composerSource.includes('class="mode"')).toBe(false)
    expect(composerSource.includes('class="ratio"')).toBe(false)
    expect(composerSource.includes('class="resolution"')).toBe(false)
    expect(composerSource.includes('class="transparent"')).toBe(false)
    expect(composerSource.includes('class="with-style"')).toBe(false)
    expect(composerSource.includes('class="model"')).toBe(true)
  })

  it('面板段序:模式段(顶部,锁定禁切)→ 比例 → 分辨率通栏 → 背景段仅图片 → 风格行 → 时长段仅视频', () => {
    // 标记全取模板里面板段独有锚点,避免误匹配脚本段/参考条分支。
    const order = [
      'aria-label="生成模式"',
      'ratio-picker',
      'aria-label="分辨率档位"',
      'aria-label="背景"',
      'v-if="styleAvailable"',
      'v-if="isVideo"',
    ]
    const at = order.map((s) => composerSource.indexOf(s))
    expect(at.every((i) => i > -1)).toBe(true)
    expect([...at].sort((a, b) => a - b)).toEqual(at)
    // 模式段锁定:图片生成 chip 在视频节点锁定时禁切(旧 select option 同款)。
    expect(composerSource.includes(':disabled="modeLocked || disabledReason !== \'\'"')).toBe(true)
  })

  it('比例两形态:图片左列竖块、视频网格 tile,形状预览按 w:h 换算', () => {
    expect(composerSource.includes('class="ratio-picker"')).toBe(true)
    expect(composerSource.includes(":class=\"{ 'as-grid': isVideo }\"")).toBe(true)
    expect(composerSource.includes('class="ratio-shape"')).toBe(true)
    expect(composerSource.includes('function ratioShapeStyle')).toBe(true)
    // 「自适应」档位显示自适应(不再用档位表的「自动比例」label)。
    expect(composerSource.includes("'自适应'")).toBe(true)
  })

  it('背景段仅两档:自动 / 透明(「不透明」不是契约值,37 号票 background 仅收 transparent)', () => {
    // 钉引号字面量:档位值里不存在「不透明」(注释里的记述不受限)。
    expect(composerSource.includes("'不透明'")).toBe(false)
    expect(declSource(composerSource, 'backgroundChips').includes("transparent: true")).toBe(true)
  })

  it('时长滑杆×自动档:自动 chip 选中 = 不传 seconds(滑杆隐藏),取消自动 = 默认 5s', () => {
    const body = declSource(composerSource, 'durationAuto')
    expect(body.includes('props.duration === \'\'')).toBe(true)
    expect(composerSource.includes('type="range"')).toBe(true)
    expect(composerSource.includes('class="duration-slider"')).toBe(true)
    expect(composerSource.includes('class="duration-number"')).toBe(true)
    expect(composerSource.includes('@blur="onDurationBlur"')).toBe(true)
    const toggle = declSource(composerSource, 'onAutoChipToggle')
    expect(toggle.includes("autoDurationDefault(durationRange.value)")).toBe(true)
    expect(toggle.includes("emit('update:duration'")).toBe(true)
    // 自动分支落回空串(不传 seconds),非自动分支才是夹紧默认值。
    expect(toggle.includes(": '')")).toBe(true)
  })

  it('¥ 胶囊:amount 短句展示、text 原句进 title,无法预估整颗隐藏', () => {
    expect(composerSource.includes('v-if="estimateInfo"')).toBe(true)
    expect(composerSource.includes(':title="estimateInfo?.text ?? \'\'"')).toBe(true)
    expect(composerSource.includes('{{ estimateInfo.amount }}')).toBe(true)
  })

  it('面板开合状态提升到编辑器:Esc 退栈归 onGlobalKeydown 浮层栈', () => {
    expect(composerSource.includes("'update:panelOpen': [value: boolean]")).toBe(true)
    expect(viewSource.includes('composerPanelOpen')).toBe(true)
    expect(viewSource.includes('if (composerPanelOpen.value) {')).toBe(true)
    // 面板是 composer 内的浮层:垫挡层点击即收(比 Esc 快一拍的鼠标路径)。
    expect(composerSource.includes('class="panel-backdrop"')).toBe(true)
  })
})
