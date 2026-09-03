<script setup lang="ts">
// 图片节点:文生图任务的展示面(10 号票)。任务在途时显示排队/生成中,
// 失败显示原因与原地重试,产物落位后直接呈现图片;有产物时提供图生视频
// 动作入口(12 号票)—— 以本节点产物为参考图,结果落为新的视频节点。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask } from '@infinitechance/api'

import type { MediaNodeData } from '../../graph'

const props = defineProps<{
  id: string
  type: string
  data: MediaNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 无任务)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** 可用的按秒计价视频模型(编辑器从 /video-models 拉取)。 */
  videoModels: string[]
  /** 图生视频提交在途(编辑器级状态,防止连点开多个任务)。 */
  videoGenerating: boolean
}>()

const emit = defineEmits<{
  retry: []
  'generate-video': [payload: { model: string; prompt: string; seconds: number }]
}>()

const status = computed(() => props.task?.status)

const videoModel = ref('')
watch(
  () => props.videoModels,
  (list) => {
    if (!list.includes(videoModel.value)) {
      videoModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

const videoPrompt = ref('')
const videoSeconds = ref(5)

// 图生视频只认 http(s) 产物作参考图:data: URI 进不了网关的参考图契约。
const hasHttpProduct = computed(() => (props.data.url ?? '').startsWith('http'))

const canGenerateVideo = computed(
  () =>
    hasHttpProduct.value &&
    videoModel.value !== '' &&
    videoPrompt.value.trim().length > 0 &&
    !props.videoGenerating,
)

function submitGenerateVideo(): void {
  if (!canGenerateVideo.value) {
    return
  }
  emit('generate-video', {
    model: videoModel.value,
    prompt: videoPrompt.value.trim(),
    seconds: videoSeconds.value,
  })
}
</script>

<template>
  <div
    class="node media image"
    :data-task="status ?? 'none'"
  >
    <header>图片</header>
    <div
      v-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '生成中…' }}</span>
      <small>任务在服务端进行,关闭页面不丢失</small>
    </div>
    <div
      v-else-if="status === 'failed'"
      class="failed"
      role="alert"
    >
      <p class="error">
        {{ task?.error || '生成失败' }}
      </p>
      <button
        class="retry"
        type="button"
        :disabled="retrying"
        @click="emit('retry')"
      >
        {{ retrying ? '重试中…' : '重试' }}
      </button>
    </div>
    <img
      v-else-if="data.url"
      :src="data.url"
      alt="图片节点产物"
    >
    <div
      v-else
      class="placeholder"
    >
      <span>图片占位</span>
      <small>生成结果将出现在这里</small>
    </div>

    <div
      v-if="hasHttpProduct && videoModels.length > 0"
      class="video-gen"
    >
      <select
        v-model="videoModel"
        title="视频模型"
      >
        <option
          v-for="m in videoModels"
          :key="m"
          :value="m"
        >
          {{ m }}
        </option>
      </select>
      <textarea
        v-model="videoPrompt"
        placeholder="视频内容与镜头描述…"
        rows="2"
      />
      <div class="video-gen-row">
        <select
          v-model.number="videoSeconds"
          title="时长"
        >
          <option :value="5">
            5 秒
          </option>
          <option :value="10">
            10 秒
          </option>
        </select>
        <button
          class="video-gen-btn"
          type="button"
          :disabled="!canGenerateVideo"
          title="以本图片为参考生成视频"
          @click="submitGenerateVideo"
        >
          {{ videoGenerating ? '提交中…' : '生成视频' }}
        </button>
      </div>
    </div>
    <Handle
      type="source"
      :position="Position.Right"
    />
    <Handle
      type="target"
      :position="Position.Left"
    />
  </div>
</template>

<style scoped>
.node {
  width: 200px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(74, 222, 128, 0.4);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #4ade80;
  margin-bottom: 8px;
}

.node[data-task='queued'] {
  border-style: dashed;
}

.node[data-task='running'] {
  border-color: rgba(74, 222, 128, 0.85);
}

.node[data-task='failed'] {
  border-color: rgba(255, 143, 143, 0.6);
}

img {
  display: block;
  width: 100%;
  border-radius: 8px;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 26px 8px;
  border: 1px dashed rgba(255, 255, 255, 0.22);
  border-radius: 8px;
  color: #8b91a7;
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: 11px;
}

.working .pill {
  color: #4ade80;
}

.node[data-task='running'] .pill {
  animation: pulse 1.4s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.45;
  }
}

.failed {
  display: grid;
  gap: 8px;
  justify-items: start;
  padding: 10px;
  border: 1px solid rgba(255, 143, 143, 0.35);
  border-radius: 8px;
}

.failed .error {
  margin: 0;
  font-size: 12px;
  color: #ff8f8f;
  word-break: break-all;
}

.retry {
  border: none;
  border-radius: 8px;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.1);
  color: inherit;
}

.retry:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.video-gen {
  display: grid;
  gap: 6px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed rgba(255, 255, 255, 0.16);
}

.video-gen select,
.video-gen textarea {
  width: 100%;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  padding: 5px 6px;
  color: inherit;
  font-size: 12px;
  box-sizing: border-box;
}

.video-gen textarea {
  resize: vertical;
}

.video-gen-row {
  display: flex;
  gap: 6px;
}

.video-gen-row select {
  flex: 0 0 auto;
}

.video-gen-btn {
  flex: 1;
  border: none;
  border-radius: 6px;
  padding: 6px 8px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: rgba(250, 204, 21, 0.16);
  color: #facc15;
}

.video-gen-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
