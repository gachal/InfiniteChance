<script setup lang="ts">
// 会话历史面板(32 号票):Agent 节点「历史」按钮唤起的右侧滑出面板,
// 素材/生成记录面板同款范式;绑定单个节点(编辑器按节点换 key 重挂),
// 只读展示完整会话流 —— user/assistant 气泡、媒体缩略、助手输出全文,
// 顶部「清空会话」。不做单轮删除、不做编辑历史重发(32 号票显式收口)。
// 历史中素材被删:缩略图加载失败翻成「素材不可用」占位(14 号票先例),
// 与发送时的 404 asset_not_found 相互独立。
import { ref } from 'vue'

import type { AgentChatMessage } from '../graph'
import { useThumbFallback } from '../thumbnail'

defineProps<{
  messages: AgentChatMessage[]
}>()

const emit = defineEmits<{
  clear: []
  close: []
}>()

// 缩略图加载失败的媒体项(素材已删/地址失效):按「消息序号:媒体序号」
// 记录,翻成占位;面板按节点重挂,无需清理。49 号票起图片缩略先行:缩略
// 失败先回落原图(md.ref 是发送载荷,只换显示面),原图也失败才落占位。
const broken = ref(new Set<string>())
const thumbs = useThumbFallback()

function keyOf(messageIndex: number, mediaIndex: number): string {
  return `${messageIndex}:${mediaIndex}`
}

function markBroken(messageIndex: number, mediaIndex: number): void {
  broken.value = new Set([...broken.value, keyOf(messageIndex, mediaIndex)])
}

/** user 轮图片的显示地址:缩略优先,回落簿记录的行保持原图(md.ref)。 */
function imageSrc(ref: string): string {
  return thumbs.displayURL(ref)
}

/** 图片加载失败:缩略失败换原图重载,原图也失败才落「素材不可用」。 */
function onImageError(ref: string, messageIndex: number, mediaIndex: number): void {
  if (!thumbs.onThumbError(ref)) {
    markBroken(messageIndex, mediaIndex)
  }
}
</script>

<template>
  <aside
    class="agent-history"
    aria-label="会话历史"
  >
    <header class="panel-head">
      <h3>会话历史</h3>
      <div class="head-actions">
        <button
          class="clear"
          type="button"
          title="清空本节点的对话历史与未发送附件"
          @click="emit('clear')"
        >
          清空会话
        </button>
        <button
          class="close"
          type="button"
          @click="emit('close')"
        >
          关闭
        </button>
      </div>
    </header>

    <p
      v-if="messages.length === 0"
      class="panel-empty"
    >
      还没有对话。在节点输入框发第一条消息后,完整会话流会出现在这里。
    </p>

    <ol class="thread">
      <li
        v-for="(m, mi) in messages"
        :key="mi"
        class="turn"
        :data-role="m.role"
      >
        <div
          v-if="m.media && m.media.length > 0"
          class="media-row"
        >
          <template
            v-for="(md, di) in m.media"
            :key="di"
          >
            <img
              v-if="md.kind === 'image' && !broken.has(keyOf(mi, di))"
              :src="imageSrc(md.ref)"
              class="media-thumb"
              alt="会话附件"
              @error="onImageError(md.ref, mi, di)"
            >
            <video
              v-else-if="md.kind === 'video' && !broken.has(keyOf(mi, di))"
              :src="md.ref"
              class="media-thumb video"
              muted
              preload="metadata"
              @error="markBroken(mi, di)"
            />
            <span
              v-else
              class="media-missing"
            >素材不可用</span>
          </template>
        </div>
        <p class="bubble">
          {{ m.content }}
        </p>
      </li>
    </ol>
  </aside>
</template>

<style scoped>
.agent-history {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 340px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  background: rgba(var(--surface-panel-rgb), 0.96);
  border-left: 1px solid var(--line-10);
  z-index: var(--z-side-panel);
  overflow-y: auto;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.panel-head h3 {
  margin: 0;
  font-size: 15px;
}

.head-actions {
  display: flex;
  gap: 6px;
}

.head-actions button {
  border: 1px solid var(--line-14);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-md);
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}

.head-actions .clear {
  color: #fdba74;
  border-color: rgba(var(--orange-rgb), 0.4);
}

.head-actions button:hover {
  border-color: var(--line-32);
  color: inherit;
}

.panel-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
}

.thread {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

/* user 右对齐、assistant 左对齐:气泡按角色沉边。 */
.turn {
  display: grid;
  gap: 4px;
  justify-items: start;
}

.turn[data-role='user'] {
  justify-items: end;
}

.bubble {
  margin: 0;
  max-width: 100%;
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--wash-05);
  border: 1px solid var(--line-08);
  border-radius: var(--r-lg);
  padding: 8px 10px;
  font-size: 12px;
  line-height: 1.6;
}

.turn[data-role='user'] .bubble {
  background: rgba(var(--accent-rgb), 0.14);
  border-color: rgba(var(--accent-rgb), 0.35);
}

.media-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.media-thumb {
  width: 84px;
  height: 84px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-05);
  border: 1px solid var(--line-10);
  display: block;
}

.media-missing {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 84px;
  height: 84px;
  border-radius: var(--r-md);
  border: 1px dashed var(--line-20);
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}
</style>
