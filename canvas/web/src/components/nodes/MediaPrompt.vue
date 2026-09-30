<script setup lang="ts">
// 媒体卡片的提示词区(27 号票):产物下方展示节点随身携带的 prompt 与
// model —— 默认截断约 3 行,点击文本展开全文(超长文本区内滚动,卡片
// 不被数千字撑爆),复制按钮取全文。字段缺省的历史节点(回填前/无任务
// 节点)整个区域不渲染,向后兼容;失败任务的节点同样显示(重试时可见
// 当时生成的是什么)。
import { computed, ref } from 'vue'

const props = defineProps<{
  prompt?: string
  model?: string
}>()

const hasPrompt = computed(() => (props.prompt ?? '').trim().length > 0)

const expanded = ref(false)

// 复制成功是转瞬的反馈:两秒后回到「复制」(与分析节点同款交互)。
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined
async function copyText(): Promise<void> {
  if (!props.prompt) {
    return
  }
  try {
    await navigator.clipboard.writeText(props.prompt)
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    /* 剪贴板不可用(非安全上下文等):静默,按钮原样 */
  }
}
</script>

<template>
  <div
    v-if="hasPrompt"
    class="node-prompt"
  >
    <div class="prompt-head">
      <span class="label">提示词</span>
      <small
        v-if="model"
        :title="model"
      >{{ model }}</small>
      <button
        class="copy"
        type="button"
        @click="copyText"
      >
        {{ copied ? '已复制' : '复制' }}
      </button>
    </div>
    <p
      class="text"
      :class="{ expanded }"
      :title="expanded ? '点击收起' : '点击展开全文'"
      @click="expanded = !expanded"
    >
      {{ prompt }}
    </p>
  </div>
</template>

<style scoped>
.node-prompt {
  margin-top: 8px;
}

.prompt-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.prompt-head .label {
  flex-shrink: 0;
  font-weight: 600;
  font-size: 12px;
  color: #8b91a7;
}

/* 模型名小字随行:超长模型名省略,完整名悬浮可见。 */
.prompt-head small {
  flex: 1;
  min-width: 0;
  color: #8b91a7;
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.copy {
  flex-shrink: 0;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 8px;
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
}

.copy:hover {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

/* 收起态截断约 3 行;展开态放开行数、限高滚动(超长提示词不撑爆卡片)。 */
.text {
  margin: 4px 0 0;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
  font-size: 12px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px 8px;
  cursor: pointer;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  overflow: hidden;
}

.text.expanded {
  display: block;
  -webkit-line-clamp: unset;
  max-height: 240px;
  overflow-y: auto;
}
</style>
