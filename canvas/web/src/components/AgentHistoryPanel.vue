<script setup lang="ts">
// 会话历史面板(32 号票):Agent 节点「历史」按钮唤起的右侧滑出面板,
// 素材/生成记录面板同款范式;绑定单个节点(编辑器按节点换 key 重挂),
// 只读展示完整会话流 —— user/assistant 气泡、媒体缩略、助手输出全文,
// 顶部「清空会话」。不做单轮删除、不做编辑历史重发(32 号票显式收口)。
// 历史中素材被删:缩略图加载失败翻成「素材不可用」占位(14 号票先例),
// 与发送时的 404 asset_not_found 相互独立。
import { ref } from 'vue'

import type { AgentChatMessage } from '../graph'

defineProps<{
  messages: AgentChatMessage[]
}>()

const emit = defineEmits<{
  clear: []
  close: []
}>()

// 缩略图加载失败的媒体项(素材已删/地址失效):按「消息序号:媒体序号」
// 记录,翻成占位;面板按节点重挂,无需清理。
const broken = ref(new Set<string>())

function keyOf(messageIndex: number, mediaIndex: number): string {
  return `${messageIndex}:${mediaIndex}`
}

function markBroken(messageIndex: number, mediaIndex: number): void {
  broken.value = new Set([...broken.value, keyOf(messageIndex, mediaIndex)])
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
              :src="md.ref"
              class="media-thumb"
              alt="会话附件"
              @error="markBroken(mi, di)"
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
  background: rgba(13, 21, 36, 0.96);
  border-left: 1px solid rgba(255, 255, 255, 0.1);
  z-index: 6;
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
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: transparent;
  color: #aab1c5;
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}

.head-actions .clear {
  color: #fdba74;
  border-color: rgba(251, 146, 60, 0.4);
}

.head-actions button:hover {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

.panel-empty {
  margin: 0;
  color: #8b91a7;
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
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  padding: 8px 10px;
  font-size: 12px;
  line-height: 1.6;
}

.turn[data-role='user'] .bubble {
  background: rgba(122, 162, 247, 0.14);
  border-color: rgba(122, 162, 247, 0.35);
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
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  display: block;
}

.media-missing {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 84px;
  height: 84px;
  border-radius: 8px;
  border: 1px dashed rgba(255, 255, 255, 0.2);
  color: #8b91a7;
  font-size: 11px;
  text-align: center;
}
</style>
