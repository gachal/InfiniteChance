<script setup lang="ts">
// 音频节点(57 号票,第十一种节点类型):台词合成产物与素材面板/
// AssetPicker 音频行的插入载体。卡面 = 内联 <audio controls> + 台词摘录
// (27 号票产物-元数据配对);素材行被删落「素材不可用」占位(照
// ImageNode 两阶段回落 —— <audio> @error 即落占位,音频没有缩略阶段)。
// 只渲染左把手(voice → audio 合成来源边的目标端),不进生成对话框、
// 无灯箱入口。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask } from '@infinitechance/api'

import type { AudioNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import { assetContentURL } from '../../thumbnail'
import NodePorts from './NodePorts.vue'
import MediaPrompt from './MediaPrompt.vue'

const props = defineProps<{
  id: string
  type: string
  data: AudioNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 面板插入行)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  retry: []
  /** 左 + 接上游(voice → audio 合成来源)。 */
  'connect-start': [side: ConnectSide]
}>()

const status = computed(() => props.task?.status)

/** 台词摘录(卡面一行,完整台词进 MediaPrompt 配对区)。 */
const excerpt = computed(() => {
  const text = props.data.text ?? ''
  return text.length > 42 ? `${text.slice(0, 42)}…` : text
})

// 内容寻址地址按素材 id 拼装;asset_id 变化(重新生成/换插)即复位重试。
const src = computed(() => (props.data.asset_id > 0 ? assetContentURL(props.data.asset_id) : ''))
const loadFailed = ref(false)
watch(
  () => props.data.asset_id,
  () => {
    loadFailed.value = false
  },
)

function onAudioError(): void {
  loadFailed.value = true
}
</script>

<template>
  <div
    class="node audio"
    :class="connectState ? `connect-${connectState}` : undefined"
    :data-task="status ?? 'none'"
  >
    <header>音频</header>
    <div
      v-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '合成中…' }}</span>
      <small>任务在服务端进行,关闭页面不丢失</small>
    </div>
    <div
      v-else-if="status === 'failed'"
      class="failed"
      role="alert"
    >
      <p class="error">
        {{ task?.error || '合成失败' }}
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
    <template v-else-if="src && !loadFailed">
      <audio
        controls
        preload="metadata"
        :src="src"
        @error="onAudioError"
      />
      <p
        v-if="excerpt"
        class="excerpt"
        :title="data.text"
      >
        {{ excerpt }}
      </p>
      <MediaPrompt
        v-if="data.model"
        :prompt="data.text ?? ''"
        :model="data.model"
      />
    </template>
    <div
      v-else-if="src && loadFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>音频占位</span>
      <small>台词合成结果将出现在这里</small>
    </div>

    <!-- 57 号票:audio 只作为连线目标,只渲染左 +。 -->
    <NodePorts
      :sides="['left']"
      @start="emit('connect-start', $event)"
    />
    <Handle
      type="target"
      :position="Position.Left"
    />
  </div>
</template>

<style scoped>
.node {
  width: 300px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(167, 139, 250, 0.45);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

header {
  font-size: 12px;
  font-weight: 600;
  color: #a78bfa;
  margin-bottom: 8px;
}

audio {
  width: 100%;
  height: 36px;
  display: block;
}

.excerpt {
  margin: 8px 0 0;
  color: #8b91a7;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 22px 8px;
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

.placeholder.missing {
  border-color: rgba(255, 143, 143, 0.4);
  color: #ff8f8f;
}

.pill {
  border-radius: 999px;
  background: rgba(122, 162, 247, 0.18);
  color: #7aa2f7;
  padding: 3px 10px;
  font-size: 12px;
}

.failed {
  display: grid;
  gap: 6px;
}

.error {
  margin: 0;
  color: #ff8f8f;
  font-size: 12px;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.retry {
  justify-self: start;
  border: none;
  border-radius: 8px;
  background: rgba(122, 162, 247, 0.2);
  color: #7aa2f7;
  padding: 6px 12px;
  cursor: pointer;
  font-size: 12px;
}

.retry:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>
