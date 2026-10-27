<script setup lang="ts">
// 图片节点:文生图/图生图任务的展示面(10/21 号票)。任务在途时显示排队/
// 生成中,失败显示原因与原地重试,产物落位后直接呈现图片。图生视频表单
// 已收编进生成对话框的视频模式(24 号票,对话框是其超集);「分析」入口
// (17 号票)保留 —— 经网关多模态聊天理解产物、落新分析节点。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask } from '@infinitechance/api'

import type { MediaNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import { useThumbFallback } from '../../thumbnail'
import NodePorts from './NodePorts.vue'
import MediaPrompt from './MediaPrompt.vue'

const props = defineProps<{
  id: string
  type: string
  data: MediaNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 无任务)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** 可用的 token 轨聊天模型(编辑器从 /prompt-models 拉取)。 */
  chatModels: string[]
  /** 画布分析的在途标记(编辑器级状态,任一分析在途即禁用)。 */
  analyzing: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  retry: []
  analyze: [payload: { model: string }]
  /** 点击媒体区打开灯箱预览(33 号票);选中行为不受影响(不拦冒泡)。 */
  preview: []
  /** 左右 + 按钮点击(30 号票两击连线):side 决定本节点作 source 还是 target。 */
  'connect-start': [side: ConnectSide]
}>()

const status = computed(() => props.task?.status)

const analyzeModel = ref('')
watch(
  () => props.chatModels,
  (list) => {
    if (!list.includes(analyzeModel.value)) {
      analyzeModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

// 分析入口的地址判断:厂商 http(s) 地址原样透传;素材内容寻址引用
// (/api/assets/{id}/content,14 号票起节点普遍持有)由服务端解出素材行
// 的厂商地址。data: URI 进不了网关的媒体契约,入口不出现。
const hasReference = computed(() => {
  const url = props.data.url ?? ''
  return url.startsWith('http') || url.startsWith('/api/assets/')
})

// 产物加载失败(素材被删除、对象被清理):显示占位而非破图 —— 删除素材
// 不应让引用它的画布报错(14 号票)。url 变化(重新生成/插入新素材)时
// 复位再试。显示 src 走缩略(49 号票):缩略失败先回落原图,原图也失败
// 才落「素材不可用」;下载与灯箱仍用 data.url 原图。
const thumbs = useThumbFallback()
const loadFailed = ref(false)
watch(
  () => props.data.url,
  () => {
    loadFailed.value = false
  },
)

/** 媒体区的显示地址:缩略优先,回落簿记录的行保持原图。 */
const displayURL = computed(() => thumbs.displayURL(props.data.url ?? ''))

function onImageError(): void {
  if (!thumbs.onThumbError(props.data.url ?? '')) {
    loadFailed.value = true
  }
}

const canAnalyze = computed(
  () => hasReference.value && analyzeModel.value !== '' && !props.analyzing,
)

function submitAnalyze(): void {
  if (!canAnalyze.value) {
    return
  }
  emit('analyze', { model: analyzeModel.value })
}
</script>

<template>
  <div
    class="node media image"
    :class="connectState ? `connect-${connectState}` : undefined"
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
    <div
      v-else-if="data.url && !loadFailed"
      class="media-frame"
      title="点击查看大图"
      @click="emit('preview')"
    >
      <img
        :src="displayURL"
        alt="图片节点产物"
        @error="onImageError"
      >
      <a
        class="download"
        :href="data.url"
        download
        title="下载图片"
        aria-label="下载图片"
        @click.stop
      >⬇</a>
    </div>
    <div
      v-else-if="data.url && loadFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>图片占位</span>
      <small>生成结果将出现在这里</small>
    </div>

    <!-- 27 号票:产物/占位/失败态下方随身携带的提示词区,缺省不渲染。 -->
    <MediaPrompt
      :prompt="data.prompt"
      :model="data.model"
    />

    <div
      v-if="hasReference && chatModels.length > 0"
      class="analyze"
    >
      <div class="analyze-row">
        <select
          v-model="analyzeModel"
          title="分析用的聊天模型"
        >
          <option
            v-for="m in chatModels"
            :key="m"
            :value="m"
          >
            {{ m }}
          </option>
        </select>
        <button
          class="analyze-btn"
          type="button"
          :disabled="!canAnalyze"
          title="分析图片产出画面理解,落为分析节点"
          @click="submitAnalyze"
        >
          {{ analyzing ? '分析中…' : '分析' }}
        </button>
      </div>
    </div>
    <!-- 30 号票:左右常驻 + 按钮,两击连线的唯一入口;Handle 已视觉
         隐藏,仅作边端点锚点。 -->
    <NodePorts
      :sides="['left', 'right']"
      @start="emit('connect-start', $event)"
    />

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
/* 21 号票的媒体卡片:更宽、大图圆角;图生视频表单已收编进生成对话框的
 * 视频模式(24 号票),分析入口留在卡片底部。 */
.node {
  width: 280px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--success-rgb), 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
}

.node header {
  font-weight: 600;
  color: var(--success);
  margin-bottom: 8px;
}

.node[data-task='queued'] {
  border-style: dashed;
}

.node[data-task='running'] {
  border-color: rgba(var(--success-rgb), 0.85);
}

.node[data-task='failed'] {
  border-color: rgba(var(--danger-rgb), 0.6);
}

/* vue-flow 把选中态标在节点包装层(本组件根的祖先),作用域样式以
   前置 :global 够到它 —— 编译为 .vue-flow__node.selected .media-frame[data-v]。 */
:global(.vue-flow__node.selected) .media-frame {
  outline: 2px solid rgba(var(--accent-rgb), 0.8);
  outline-offset: 2px;
}

.media-frame {
  position: relative;
  cursor: zoom-in;
}

.media-frame img {
  display: block;
  width: 100%;
  border-radius: var(--r-lg);
}

/* 悬浮下载:媒体卡片右上角,hover 才出现(触屏常显)。 */
.media-frame .download {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-md);
  background: rgba(var(--surface-panel-rgb), 0.85);
  color: var(--text-bright);
  font-size: var(--fs-body);
  text-decoration: none;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.media-frame:hover .download,
.media-frame .download:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .media-frame .download {
    opacity: 1;
  }
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 26px 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: var(--fs-micro);
}

.placeholder.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
  color: var(--danger);
}

.working .pill {
  color: var(--success);
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
  border: 1px solid rgba(var(--danger-rgb), 0.35);
  border-radius: var(--r-md);
}

.failed .error {
  margin: 0;
  font-size: var(--fs-label);
  color: var(--danger);
  word-break: break-all;
}

.retry {
  border: none;
  border-radius: var(--r-md);
  padding: 6px 14px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  background: var(--wash-fill);
  color: inherit;
}

.retry:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 分析动作(17 号票):模型选择 + 触发按钮,描边用分析节点的淡紫。 */
.analyze {
  margin-top: 8px;
}

.analyze-row {
  display: flex;
  gap: 6px;
}

.analyze-row select {
  flex: 1;
  min-width: 0;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px;
  color: inherit;
  font-size: var(--fs-label);
}

.analyze-btn {
  flex-shrink: 0;
  background: transparent;
  border: 1px solid rgba(var(--indigo-soft-rgb), 0.55);
  border-radius: var(--r-md);
  padding: 6px 12px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  color: var(--indigo-soft);
}

.analyze-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
