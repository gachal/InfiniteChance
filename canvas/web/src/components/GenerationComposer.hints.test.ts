// 85 号票 C:>4 角色提示行泛化的源码钉子(readFileSync 范式)。C1 文案去
// 厂商语境(Seedance → 多角色同框),C2 图片模式同出提示行 —— 两模式一致,
// 仅提示不拦发送;≤4 不出现(条件为 count > LIMIT)。改文案或退回单模式
// 必须同改本钉。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const composerSource = readFileSync(
  fileURLToPath(new URL('./GenerationComposer.vue', import.meta.url)),
  'utf8',
)

/** 摘 const 声明段(到下一个空行),用于 computed 箭头段。 */
function declSource(src: string, name: string): string {
  const start = src.indexOf(`const ${name}`)
  expect(start).toBeGreaterThan(-1)
  const end = src.indexOf('\n\n', start)
  return src.slice(start, end)
}

describe('>4 角色提示行(85 号票 C:文案泛化 + 两模式一致)', () => {
  it('C1 文案不绑厂商语境:多角色同框稳定性下降,Seedance 字样不出现在本行', () => {
    expect(
      composerSource.includes(
        '出场角色超过 {{ CHARACTER_ELEMENT_HINT_LIMIT }} 个,多角色同框稳定性会下降',
      ),
    ).toBe(true)
    expect(composerSource.includes('Seedance 等模型稳定性')).toBe(false)
  })

  it('C2 图片模式同样出提示:tooManyCharacters 不再挂 isVideo 门,按当前模式各自计数', () => {
    const body = declSource(composerSource, 'tooManyCharacters')
    expect(body.includes('isVideo.value &&')).toBe(false)
    expect(body.includes('countCharacterElements(props.videoRefs, props.videoRefNames ?? [])')).toBe(true)
    expect(body.includes('countCharacterElements(props.refs, props.refNames ?? [])')).toBe(true)
    expect(body.includes('> CHARACTER_ELEMENT_HINT_LIMIT')).toBe(true)
  })

  it('只提示不拦发送:沿用 vref-hint 小字族(71 号票 B7 出口不动)', () => {
    expect(composerSource.includes('v-if="tooManyCharacters"')).toBe(true)
    expect(composerSource.includes('class="vref-hint"')).toBe(true)
  })
})
