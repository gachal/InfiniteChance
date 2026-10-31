/**
 * 通用键盘守卫(38 号票键盘纪律;68 号票评审自 canvasHelp.ts 迁入):
 * 输入框/文本域/下拉/可编辑区聚焦时,画布级 window 快捷键一律让位给
 * 浏览器原生行为(文本编辑的撤销归输入框自己)。判定口径改动只动
 * 这里一处;视图(CanvasEditorView)与帮助链路同源导入本模块。
 */

/** 输入聚焦守卫:目标是输入框/文本域/下拉/可编辑区时算「输入中」。
 * CanvasEditorView.isTypingTarget 的同形状纯函数,供视图委托。 */
export function isTypingTargetEl(
  el: { tagName: string; isContentEditable: boolean } | null | undefined,
): boolean {
  if (!el) {
    return false
  }
  return (
    el.tagName === 'INPUT' ||
    el.tagName === 'TEXTAREA' ||
    el.tagName === 'SELECT' ||
    el.isContentEditable
  )
}
