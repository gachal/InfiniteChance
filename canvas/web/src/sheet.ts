/**
 * 角色设定图生成的纯逻辑(70 号票):五类型模板与服务端拼装的前端同形
 * (类型模板 + 角色四件套 + 变体名/服装描述 + IP 风格段,≤2000 runes)、
 * B3 参考图默认(变体已存设定图默认带最近一张)与 cut 出场角色参考挑选
 * (C:按 character_ids 序取默认变体,优先视图类 > 任意图 > 封面投影,
 * 无图静默跳过,上限截断计数)。83 号票:取图规则改「cut 绑定变体优先 →
 * 默认变体回落」(resolveCutVariant),存量画布(无映射)行为逐字节不变。
 *
 * 模板字面量与服务端 internal/canvastask/iptask.go 逐字一致,两侧测试各自
 * 钉住同一组字面量防漂移 —— 改任何一段文案必须两侧同改。入口一(composer
 * 预填)在前端拼装;入口二(抽屉对话框)由服务端拼装,本模块只供预览与
 * 类型切换的重拼。
 */
import type { CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import { styleSegment } from './ipStyle'

/** 设定图六类型(70 号票五类型;84 号票补「光照色板」,label 白名单七类
 * 去「其他」),固定序,默认三视图。 */
export const SHEET_TYPES = ['三视图', '多视图', '表情集', '色板', '光照色板', '姿态'] as const

export type SheetType = (typeof SHEET_TYPES)[number]

/** 类型 → prompt 模板(与服务端 sheetTypeTemplates 同字面量;色板模板
 * 84 号票补瞳色/线色,光照色板 = 昼光/夜色/黄昏/逆光四格配色对照)。 */
export const SHEET_TYPE_TEMPLATES: Record<SheetType, string> = {
  三视图: '角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风',
  多视图: '角色设定图·多视图:同一角色的全身多角度视图对照(正面、四分之三、侧面、背面),白底,各视角比例一致,标准设定图画风',
  表情集: '角色设定图·表情集:同一角色头部的多种表情对照(喜、怒、哀、惊等),白底,构图整齐,表情差异清晰,标准设定图画风',
  色板: '角色设定图·色板:该角色的配色色板展示(发色、肤色、瞳色、线色、服装主色与辅色色块并列),白底,标注干净,标准设定图画风',
  光照色板: '角色设定图·光照色板:同一角色的昼光、夜色、黄昏、逆光四格配色对照,同站姿同构图,各格光照差异清晰,白底,标注干净,标准设定图画风',
  姿态: '角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风',
}

/** 拼装后的 prompt 截断上限(与服务端 MaxSheetPromptRunes 同值)。 */
export const MAX_SHEET_PROMPT_RUNES = 2000

/** 拼装的角色事实(name 必给,其余可空整行跳过)。palette 是变体配色
 * 自由文本(84 号票 B1:非空才落「配色:」行,插服装行之后)。 */
export interface SheetCharacterFacts {
  name: string
  alias?: string
  description?: string
  detail?: string
  variantName: string
  variantNote?: string
  palette?: string
}

/** 设定图 prompt 拼装(与服务端 BuildSheetPrompt 同形):类型模板 + 四件套
 * + 变体名/服装/配色(84 号票 B1 五类型全拼,非空才拼),IP 风格段(69 号
 * 票 styleSegment,有则拼)放最前单独成行,最后按 rune 截断。未知类型跳过
 * 模板行。 */
export function buildSheetPrompt(
  type: string,
  facts: SheetCharacterFacts,
  style = '',
): string {
  const lines: string[] = []
  const tpl = (SHEET_TYPE_TEMPLATES as Record<string, string>)[type]
  if (tpl) {
    lines.push(tpl)
  }
  lines.push(`角色:${facts.name}`)
  if (facts.alias) {
    lines.push(`别名:${facts.alias}`)
  }
  if (facts.description) {
    lines.push(`简介:${facts.description}`)
  }
  if (facts.detail) {
    lines.push(`详细设定:${facts.detail}`)
  }
  lines.push(`造型:${facts.variantName}`)
  if (facts.variantNote) {
    lines.push(`服装:${facts.variantNote}`)
  }
  if (facts.palette) {
    lines.push(`配色:${facts.palette}`)
  }
  const body = lines.join('\n')
  const segment = styleSegment(style)
  const prompt = segment === '' ? body : `${segment}\n${body}`
  const runes = [...prompt]
  return runes.length <= MAX_SHEET_PROMPT_RUNES ? prompt : runes.slice(0, MAX_SHEET_PROMPT_RUNES).join('')
}

/** 变体下最近一张设定图(id 最大 = 创建序最近,B3 的「最近一张」);
 * 空变体返回 null。 */
export function latestVariantImage(
  variant: Pick<CharacterVariantRecord, 'images'>,
): CharacterVariantRecord['images'][number] | null {
  let latest: CharacterVariantRecord['images'][number] | null = null
  for (const im of variant.images) {
    if (latest === null || im.id > latest.id) {
      latest = im
    }
  }
  return latest
}

/** cut 注入挑出的一条角色参考。 */
export interface SheetRefPick {
  characterId: number
  assetId: number
  /** 命中的默认变体 id;角色无任何变体时缺省(取图只走封面投影)。
   * cut→视频注入以它带元素 chip 的 variantId(71 号票),cut→图片直通
   * 不消费此字段。 */
  variantId?: number
}

/** cut 注入取「默认变体」的规则(71 号票 D1 从 pickCutCharacterRefs 抽出):
 * 名为「默认」的变体,改名后回落首个;角色无任何变体返回 undefined。 */
export function defaultCharacterVariant(
  c: Pick<CharacterRecord, 'variants'>,
): CharacterVariantRecord | undefined {
  return c.variants.find((v) => v.name === '默认') ?? c.variants[0]
}

/** cut 出场角色本次取图的变体解析(83 号票):cut 绑定变体优先
 * (character_variants 映射),绑定缺失 / 映射缺该角色、或所指变体已删时
 * 回落「默认变体」(defaultCharacterVariant);fellBack 标记「有绑定但没
 * 解析到」—— 编辑层据此显示回落态(绑定键保留在 cut 数据里,不静默清洗)。 */
export function resolveCutVariant(
  c: Pick<CharacterRecord, 'variants'>,
  boundVariantId?: number,
): { variant: CharacterVariantRecord | undefined; fellBack: boolean } {
  if (boundVariantId !== undefined) {
    const bound = c.variants.find((v) => v.id === boundVariantId)
    if (bound) {
      return { variant: bound, fellBack: false }
    }
    return { variant: defaultCharacterVariant(c), fellBack: true }
  }
  return { variant: defaultCharacterVariant(c), fellBack: false }
}

/** 按变体取图(71 号票 D1 共享函数:cut 出场角色注入与角色卡「生成登场
 * 视频」直通两处共用,保证同一角色同一变体两路取到同一张图)。优先级 =
 * 该变体最近一张视图类(label ∈ 五类型)> 该变体任意设定图 > 角色任意图
 * (cover_asset_id 封面投影);变体缺省(角色无任何变体)只走封面投影;
 * 全无图返回 0,调用方静默跳过(不报错不阻塞)。 */
export function pickVariantAssetId(
  c: Pick<CharacterRecord, 'cover_asset_id'>,
  variant?: Pick<CharacterVariantRecord, 'images'>,
): number {
  let assetId = 0
  if (variant) {
    // 视图类优先(label ∈ 五类型),取其中最近一张;再回落任意一张。
    const viewImages = variant.images.filter((im) =>
      (SHEET_TYPES as readonly string[]).includes(im.label),
    )
    const latestView = latestVariantImage({ images: viewImages }) ?? latestVariantImage(variant)
    if (latestView) {
      assetId = latestView.asset_id
    }
  }
  if (assetId === 0) {
    assetId = c.cover_asset_id // 封面投影(无图角色为 0 → 调用方静默跳过)
  }
  return assetId
}

/** cut 出场角色参考挑选(70 号票 C;83 号票改取图规则):按 character_ids
 * 序逐角色取设定图 —— 变体解析走 resolveCutVariant(**cut 绑定变体优先 →
 * 默认变体回落**,characterVariants 即 cut.character_variants 映射,可缺省),
 * 取图走 pickVariantAssetId(71 号票 D1 共享;绑定只换「取哪个变体」,
 * 视图类 > 任意图 > 封面投影的取图优先级不变);角色无任何图静默跳过
 * (不报错不阻塞,画面描述兜底)。cap 是与既有参考合并后的总上限:超出
 * 按序截断,dropped = 未带上数量(toast 文案用)。picks 附带命中变体的
 * variantId(71 号票 cut→视频注入的元素标注用;角色无变体时缺省)。
 * 不传 characterVariants 时行为与 70 号票契约逐字节一致(存量画布路径)。 */
export function pickCutCharacterRefs(
  characters: CharacterRecord[],
  characterIds: number[],
  cap: number,
  characterVariants?: Record<number, number>,
): { picks: SheetRefPick[]; dropped: number } {
  const picks: SheetRefPick[] = []
  let dropped = 0
  for (const id of characterIds) {
    const c = characters.find((x) => x.id === id)
    if (!c) {
      continue
    }
    const variant = resolveCutVariant(c, characterVariants?.[id]).variant
    const assetId = pickVariantAssetId(c, variant)
    if (assetId === 0) {
      continue
    }
    if (picks.length >= cap) {
      dropped += 1
      continue
    }
    picks.push(
      variant === undefined
        ? { characterId: id, assetId }
        : { characterId: id, assetId, variantId: variant.id },
    )
  }
  return { picks, dropped }
}
