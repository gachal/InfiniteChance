/**
 * 画布桌宠的纯函数层(92 号票):点击/拖动手势判定(按下位移阈值)、位置
 * 与收起态的按画布 localStorage 存取(键含画布 id,不进库不同步多端)、
 * 建议 chips 的「换一批」轮换、缺省对话模型初值回落。形象渲染与指针事件
 * 在 CanvasPet.vue —— 组件持状态,这里持可测的决策逻辑。
 */

/** 按下位移阈值(px):累计位移不超过它视为点击,超过视为拖动。6px ≈
 * 一枚光标的手抖容忍度,手感留浏览器点验收(票面裁量,实现注记钉值)。 */
export const PET_DRAG_THRESHOLD_PX = 6

/** 桌宠形象边长(px):组件样式与位置计算同值(缺省形象 CSS 定宽高,
 * 动图 <img> 同框)。 */
export const PET_SIZE = 72

/** 画布内边距:缺省驻位(右下)与位置记忆回读的留边。 */
export const PET_EDGE_MARGIN = 24

/** 助手面板首屏建议 chips 每批条数(「换一批」按此轮换)。 */
export const PET_CHIP_PAGE_SIZE = 4

/** localStorage 键前缀(键尾拼画布 id —— 位置与收起态按画布记忆)。位置键
 * v2:旧键值因「零边界回读」挂载 bug 从未生效(回读时被夹成原点),换键
 * 让既有浏览器落回新缺省位,不背旧包袱(收起态键不受影响)。 */
export const PET_POSITION_KEY_PREFIX = 'infinitechance.canvas.pet.position.v2.'
export const PET_COLLAPSED_KEY_PREFIX = 'infinitechance.canvas.pet.collapsed.'

export interface PetPoint {
  x: number
  y: number
}

export interface PetBounds {
  width: number
  height: number
}

/** 手势判定:按下后累计位移是否超过拖动阈值(欧氏距离;压线不算拖,
 * 超过才算 —— 「拖一下只挪一两个像素」仍是一次点击)。 */
export function petGestureExceedsDrag(dx: number, dy: number, threshold: number = PET_DRAG_THRESHOLD_PX): boolean {
  return Math.hypot(dx, dy) > threshold
}

/** 缺省驻位:画布右下(留 PET_EDGE_MARGIN 边)。画布比桌宠还小(极端
 * 布局)时不出负位,至少让内边距兜住。 */
export function defaultPetPosition(bounds: PetBounds): PetPoint {
  return {
    x: Math.max(PET_EDGE_MARGIN, bounds.width - PET_SIZE - PET_EDGE_MARGIN),
    y: Math.max(PET_EDGE_MARGIN, bounds.height - PET_SIZE - PET_EDGE_MARGIN),
  }
}

/** 拖动夹取:位置留在画布区内(右/下边界按桌宠身位收,画布比桌宠小时
 * 夹到原点)。 */
export function clampPetPosition(p: PetPoint, bounds: PetBounds): PetPoint {
  const clamp = (v: number, max: number): number => Math.min(Math.max(0, v), Math.max(0, max))
  return {
    x: clamp(p.x, bounds.width - PET_SIZE),
    y: clamp(p.y, bounds.height - PET_SIZE),
  }
}

// ---- 按画布的 localStorage 存取(隐私模式下 try/catch 静默退化,会话内
// 仍可用,只是不记忆 —— 与 auth.ts persistToken 同款纪律)----

export function petPositionKey(canvasId: number): string {
  return `${PET_POSITION_KEY_PREFIX}${canvasId}`
}

export function petCollapsedKey(canvasId: number): string {
  return `${PET_COLLAPSED_KEY_PREFIX}${canvasId}`
}

/** 回读桌宠位置:缺值/坏 JSON 回缺省驻位(右下),越界值夹回画布内。 */
export function loadPetPosition(canvasId: number, bounds: PetBounds): PetPoint {
  try {
    const raw = localStorage.getItem(petPositionKey(canvasId))
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<PetPoint>
      if (
        typeof parsed?.x === 'number' &&
        Number.isFinite(parsed.x) &&
        typeof parsed?.y === 'number' &&
        Number.isFinite(parsed.y)
      ) {
        return clampPetPosition({ x: parsed.x, y: parsed.y }, bounds)
      }
    }
  } catch {
    // 隐私模式 / 坏 JSON:走缺省位。
  }
  return defaultPetPosition(bounds)
}

export function savePetPosition(canvasId: number, p: PetPoint): void {
  try {
    localStorage.setItem(petPositionKey(canvasId), JSON.stringify(p))
  } catch {
    // 静默:记忆不可用不影响桌宠本体。
  }
}

/** 回读收起态:只在显式存过 '1' 时收起(缺省唤出)。 */
export function loadPetCollapsed(canvasId: number): boolean {
  try {
    return localStorage.getItem(petCollapsedKey(canvasId)) === '1'
  } catch {
    return false
  }
}

export function savePetCollapsed(canvasId: number, collapsed: boolean): void {
  try {
    if (collapsed) {
      localStorage.setItem(petCollapsedKey(canvasId), '1')
    } else {
      localStorage.removeItem(petCollapsedKey(canvasId))
    }
  } catch {
    // 静默:同上。
  }
}

/** 桌宠形象读端点 URL(canvas 面 GET /pet/object,故意不挂鉴权 —— <img>
 * 带不了 Authorization;键只认配置登记值)。canvas-web 的 API 反代在 /api,
 * <img> 不经 ApiClient,直接拼同源路径。 */
export function petObjectURL(key: string): string {
  return `/api/pet/object?key=${encodeURIComponent(key)}`
}

// ---- 面板接缝(92 号票定案)----

/** 「换一批」轮换:第 page 批 = chips 循环左移 page*pageSize 后的头部
 * pageSize 条(chips 不超过一批时恒回原批,组件据此隐藏换一批按钮);
 * 不改入参,页码/负页码按长度回绕。 */
export function petChipPage(chips: string[], page: number, pageSize: number = PET_CHIP_PAGE_SIZE): string[] {
  if (pageSize <= 0 || chips.length === 0) {
    return []
  }
  if (chips.length <= pageSize) {
    return [...chips]
  }
  const start = ((page * pageSize) % chips.length + chips.length) % chips.length
  const out: string[] = []
  for (let i = 0; i < Math.min(pageSize, chips.length); i += 1) {
    out.push(chips[(start + i) % chips.length])
  }
  return out
}

/** 新会话对话模型初值(票面:配置值不在目录时回落首项;目录为空回空串,
 * 面板侧已有「无可用模型」禁态)。 */
export function petDefaultModel(configured: string, catalog: string[]): string {
  if (configured && catalog.includes(configured)) {
    return configured
  }
  return catalog[0] ?? ''
}
