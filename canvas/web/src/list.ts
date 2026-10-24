/**
 * 有序列表的按下标删移与挑选面板跟随(53-55 号票评审修复):絵コンテ
 * cut / ネーム页格 / 关卡节拍的「删除 + 相邻交换位移」逐字节同形,抽成
 * 纯函数共享(数组换新不改入参纪律);编辑层素材挑选面板(AssetPicker)
 * 钉住的行下标在删/移后要跟随同一行,不能静默改绑别的行 —— 跟随判定同
 * 样是纯函数,StoryboardEditorLayer 与 NameEditorLayer 共用一套语义。
 */

/** 按下标删除一项:返回新数组不改入参;越界原内容返回(调用方自守,
 * 与各域模块的 remove* 原实现逐字节同形)。 */
export function removeAt<T>(items: readonly T[], index: number): T[] {
  return items.filter((_, i) => i !== index)
}

/** 上移/下移一项(delta = -1 | 1,与相邻项交换):越界或空表原样返回
 * (引用相等 = 未动;编辑层排序用按钮不做拖拽,40 号票手势纪律)。
 * 入参不标 readonly:未动时要原引用返回,调用方以引用相等识别未动。 */
export function moveItem<T>(items: T[], index: number, delta: -1 | 1): T[] {
  const target = index + delta
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) {
    return items
  }
  const next = [...items]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}

/** 删除一行后挑选面板钉住的行下标跟随:删的正是面板行 → 关面板(null);
 * 删的行在面板行之前 → 面板行前移一位;删的行在后面 → 不动。(评审修复:
 * 只处理「删的正是面板行」时,删更早的行会静默改绑下一行。) */
export function followRemove(picker: number | null, removed: number): number | null {
  if (picker === null || picker === removed) {
    return null
  }
  return picker > removed ? picker - 1 : picker
}

/** 相邻交换(moveItem)后挑选面板钉住的行下标跟随:面板在被移行 → 跟到
 * 新位置;面板在被换下来的相邻行 → 同步换位;其余不动。picker 非空由
 * 调用方先行短路(未动时以 moveItem 的引用相等短路,不走这里)。 */
export function followMove(picker: number, index: number, delta: -1 | 1): number {
  if (picker === index) {
    return index + delta
  }
  if (picker === index + delta) {
    return index
  }
  return picker
}
