import { onScopeDispose, ref, type Ref } from 'vue'

/** 轻提示(38 号票评审):删除可撤销提示、非法连线反馈共用,4 秒自动消退。
 * 58 号票阶段一自 CanvasEditorView 抽出;呈现层在 EditorToast.vue。 */
export function useEditorToast(): {
  notice: Ref<string>
  show: (text: string) => void
} {
  const notice = ref('')
  let timer: ReturnType<typeof setTimeout> | undefined

  function show(text: string): void {
    notice.value = text
    if (timer !== undefined) {
      clearTimeout(timer)
    }
    timer = setTimeout(() => {
      notice.value = ''
      timer = undefined
    }, 4000)
  }

  onScopeDispose(() => {
    if (timer !== undefined) {
      clearTimeout(timer)
    }
  })

  return { notice, show }
}
