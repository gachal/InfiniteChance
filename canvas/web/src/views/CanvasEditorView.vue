<script setup lang="ts">
// 画布编辑器:vue-flow 三类节点、自由拖拽连线、整图防抖自动保存
// 与版本冲突处理(09 号票)。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Background } from '@vue-flow/background'
import { VueFlow, useVueFlow, type Connection, type Edge } from '@vue-flow/core'

import { ApiError, type CanvasDetail } from '@infinitechance/api'

import { useAuth } from '../auth'
import {
  initialData,
  isCanvasNodeType,
  NODE_TYPE_LABEL,
  type CanvasNodeData,
  type CanvasNodeType,
} from '../graph'
import { useAutosave } from '../composables/useAutosave'
import ImageNode from '../components/nodes/ImageNode.vue'
import PromptNode from '../components/nodes/PromptNode.vue'
import VideoNode from '../components/nodes/VideoNode.vue'

const route = useRoute()
const router = useRouter()
const { client, clearSession } = useAuth()

const canvasId = Number(route.params.id)

// useVueFlow 在组件挂载前调用:编辑器拥有这个 flow 实例,
// toObject/addNodes 等操作与 <VueFlow> 渲染共享同一份状态。
const { addEdges, addNodes, fitView, onConnect, onEdgesChange, onNodesChange, setEdges, setNodes, toObject, updateNodeData } =
  useVueFlow()

const canvasName = ref('')
const loadError = ref('')
const loading = ref(true)
const missing = ref(false)

const renaming = ref(false)
const renameDraft = ref('')

const autosave = useAutosave({
  save: (expectedVersion) => client.saveCanvasGraph(canvasId, snapshot(), expectedVersion),
})

const saveLabel = computed(() => {
  switch (autosave.state.value) {
    case 'dirty':
      return '有未保存的更改'
    case 'saving':
      return '保存中…'
    case 'saved':
      return '已保存'
    case 'error':
      return '保存失败,将自动重试'
    case 'conflict':
      return '版本冲突'
    default:
      return '所有更改已保存'
  }
})

// 持久化文档:只保留语义字段,vue-flow 的内部装饰不落库。
function snapshot() {
  const doc = toObject()
  return {
    nodes: doc.nodes.map((n) => ({
      id: n.id,
      type: n.type,
      position: { x: n.position.x, y: n.position.y },
      data: n.data as CanvasNodeData,
    })),
    edges: doc.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      sourceHandle: e.sourceHandle ?? null,
      targetHandle: e.targetHandle ?? null,
    })),
  }
}

// 尺寸/选中态变更不改变持久化文档,不触发保存。
onNodesChange((changes) => {
  if (changes.some((c) => c.type === 'add' || c.type === 'remove' || c.type === 'position')) {
    autosave.markDirty()
  }
})
onEdgesChange((changes) => {
  if (changes.some((c) => c.type === 'add' || c.type === 'remove')) {
    autosave.markDirty()
  }
})
onConnect((params: Connection) => {
  addEdges([{ ...params }])
})

// 节点内容编辑(提示词文本)不产生变更事件:由节点上抛,这里落到
// flow 状态并标记脏。
function onTextChange(nodeId: string, text: string): void {
  updateNodeData(nodeId, { text })
  autosave.markDirty()
}

let nodeSeq = 0

function addNode(type: CanvasNodeType): void {
  nodeSeq += 1
  const step = (toObject().nodes.length % 8) * 48
  addNodes([
    {
      id: `${type}-${Date.now()}-${nodeSeq}`,
      type,
      position: { x: 140 + step, y: 120 + step },
      data: initialData(type),
    },
  ])
  // addNodes 会产生 'add' 变更事件,那里已 markDirty;这里无需重复。
}

async function loadCanvas(): Promise<void> {
  loading.value = true
  loadError.value = ''
  missing.value = false
  try {
    const detail = await client.getCanvas(canvasId)
    await applyServerGraph(detail)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      await router.replace({ name: 'login' })
      return
    }
    if (e instanceof ApiError && e.status === 404) {
      missing.value = true
    } else {
      loadError.value = e instanceof ApiError ? e.message : '无法连接画布服务,请确认后端已启动'
    }
  } finally {
    loading.value = false
  }
}

/** 以服务器返回的整图为权威覆盖本地(初次加载与冲突重载共用)。 */
async function applyServerGraph(detail: CanvasDetail): Promise<void> {
  canvasName.value = detail.name
  setNodes(normalizeNodes(detail))
  setEdges(detail.graph.edges as unknown as Edge[])
  await nextTick()
  // 水合触发的同步变更事件先落地,再认版本,避免把加载当成编辑。
  autosave.setVersion(detail.version)
  void fitView({ padding: 0.2, maxZoom: 1.2, duration: 120 })
}

function normalizeNodes(detail: CanvasDetail) {
  return detail.graph.nodes.map((raw) => {
    const node = raw as {
      id: string
      type?: string
      position: { x: number; y: number }
      data?: CanvasNodeData
    }
    const type = isCanvasNodeType(node.type) ? node.type : 'prompt'
    return {
      id: node.id,
      type,
      position: { x: node.position.x, y: node.position.y },
      data: node.data ?? initialData(type),
    }
  })
}

// ---- 版本冲突的两个出口 ----

const resolvingConflict = ref(false)

/** 放弃本地修改,回到服务器版本。 */
async function reloadServerVersion(): Promise<void> {
  if (resolvingConflict.value) {
    return
  }
  resolvingConflict.value = true
  try {
    const detail = await client.getCanvas(canvasId)
    await applyServerGraph(detail)
  } catch {
    // 重载失败保持 conflict 态,横幅仍在,可再次尝试。
  } finally {
    resolvingConflict.value = false
  }
}

/** 以本地内容覆盖服务器:取服务器当前版本号重发一帧。 */
async function overwriteServer(): Promise<void> {
  if (resolvingConflict.value) {
    return
  }
  resolvingConflict.value = true
  try {
    const detail = await client.getCanvas(canvasId)
    autosave.setVersion(detail.version)
    autosave.markDirty()
  } catch {
    // 同上:保持 conflict 态。
  } finally {
    resolvingConflict.value = false
  }
}

// ---- 画布改名(编辑器内)----

async function commitRename(): Promise<void> {
  const name = renameDraft.value.trim()
  renaming.value = false
  if (!name || name === canvasName.value) {
    return
  }
  try {
    const renamed = await client.renameCanvas(canvasId, name)
    canvasName.value = renamed.name
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '重命名失败,请稍后再试'
  }
}

// 未保存更改离开页面前提醒(冲突/失败态同样有未落库内容)。
function beforeUnload(e: BeforeUnloadEvent): void {
  const state = autosave.state.value
  if (state === 'dirty' || state === 'saving' || state === 'error' || state === 'conflict') {
    e.preventDefault()
    e.returnValue = ''
  }
}

onMounted(() => {
  void loadCanvas()
  window.addEventListener('beforeunload', beforeUnload)
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
})

function backToList(): void {
  void router.push({ name: 'canvases' })
}
</script>

<template>
  <div class="editor">
    <header class="topbar">
      <button
        class="ghost"
        type="button"
        @click="backToList"
      >
        ← 画布列表
      </button>

      <template v-if="renaming">
        <form
          class="rename-form"
          @submit.prevent="commitRename"
        >
          <input
            v-model="renameDraft"
            type="text"
            maxlength="128"
            autofocus
          >
          <button
            class="primary"
            type="submit"
          >
            保存
          </button>
          <button
            class="ghost"
            type="button"
            @click="renaming = false"
          >
            取消
          </button>
        </form>
      </template>
      <template v-else>
        <button
          class="title"
          type="button"
          title="点击重命名"
          @click="renaming = true; renameDraft = canvasName"
        >
          {{ canvasName || '未命名画布' }}
        </button>
      </template>

      <span
        class="save-state"
        :data-state="autosave.state.value"
      >
        {{ saveLabel }}
      </span>
    </header>

    <div
      v-if="autosave.state.value === 'conflict'"
      class="conflict-banner"
      role="alert"
    >
      <p>
        画布已在其他窗口被修改,本地更改尚未保存。
        可加载服务器版本(放弃本地更改),或以当前内容覆盖服务器。
      </p>
      <span class="conflict-actions">
        <button
          class="primary"
          type="button"
          :disabled="resolvingConflict"
          @click="reloadServerVersion"
        >
          加载服务器版本
        </button>
        <button
          class="danger"
          type="button"
          :disabled="resolvingConflict"
          @click="overwriteServer"
        >
          以我的版本覆盖
        </button>
      </span>
    </div>

    <div
      v-if="loadError"
      class="load-error"
      role="alert"
    >
      <p>{{ loadError }}</p>
      <button
        class="ghost"
        type="button"
        @click="loadCanvas"
      >
        重试
      </button>
    </div>

    <div
      v-if="missing"
      class="load-error"
    >
      <p>画布不存在或已被删除。</p>
      <button
        class="ghost"
        type="button"
        @click="backToList"
      >
        返回列表
      </button>
    </div>

    <div class="canvas-wrap">
      <div
        v-if="loading"
        class="canvas-loading"
      >
        加载画布…
      </div>
      <VueFlow
        class="flow"
        :min-zoom="0.2"
        :max-zoom="2"
      >
        <Background :gap="24" />
        <template #node-prompt="nodeProps">
          <PromptNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            @text-change="onTextChange(nodeProps.id, $event)"
          />
        </template>
        <template #node-image="nodeProps">
          <ImageNode v-bind="nodeProps" />
        </template>
        <template #node-video="nodeProps">
          <VideoNode v-bind="nodeProps" />
        </template>
      </VueFlow>
    </div>

    <footer class="toolbar">
      <span class="hint">拖拽节点排布,拖动端口连线。</span>
      <span class="add-group">
        <button
          v-for="t in (['prompt', 'image', 'video'] as const)"
          :key="t"
          :class="`add-${t}`"
          type="button"
          @click="addNode(t)"
        >
          + {{ NODE_TYPE_LABEL[t] }}
        </button>
      </span>
    </footer>
  </div>
</template>

<style scoped>
.editor {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

.topbar {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(13, 21, 36, 0.9);
}

.topbar button {
  border: none;
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 14px;
  cursor: pointer;
}

.title {
  background: transparent;
  color: inherit;
  font-size: 16px;
  font-weight: 600;
}

.title:hover {
  background: rgba(255, 255, 255, 0.06);
}

.rename-form {
  display: flex;
  gap: 8px;
  flex: 1;
}

.rename-form input {
  flex: 1;
  max-width: 320px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 8px 10px;
  color: inherit;
  font-size: 14px;
}

.save-state {
  margin-left: auto;
  color: #8b91a7;
  font-size: 13px;
}

.save-state[data-state='saved'] {
  color: #4ade80;
}

.save-state[data-state='error'],
.save-state[data-state='conflict'] {
  color: #ff8f8f;
}

.conflict-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.16);
  border-bottom: 1px solid rgba(224, 49, 49, 0.45);
}

.conflict-banner p {
  margin: 0;
  font-size: 14px;
}

.conflict-actions {
  display: flex;
  gap: 8px;
  white-space: nowrap;
}

.conflict-actions button,
.load-error button {
  border: none;
  border-radius: 8px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.load-error {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.12);
  border-bottom: 1px solid rgba(224, 49, 49, 0.35);
}

.load-error p {
  margin: 0;
  font-size: 14px;
}

.canvas-wrap {
  position: relative;
  flex: 1;
  min-height: 0;
}

.canvas-loading {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: #8b91a7;
  z-index: 5;
}

.flow {
  width: 100%;
  height: 100%;
}

.primary {
  background: #4c6ef5;
  color: #fff;
  font-weight: 600;
}

.danger {
  background: #e03131;
  color: #fff;
  font-weight: 600;
}

.ghost {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: #aab1c5;
}

.ghost:hover {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(13, 21, 36, 0.9);
}

.hint {
  color: #8b91a7;
  font-size: 13px;
}

.add-group {
  display: flex;
  gap: 8px;
}

.add-group button {
  border: none;
  border-radius: 10px;
  padding: 9px 16px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}

.add-prompt {
  background: rgba(122, 162, 247, 0.2);
  color: #7aa2f7;
}

.add-image {
  background: rgba(74, 222, 128, 0.16);
  color: #4ade80;
}

.add-video {
  background: rgba(250, 204, 21, 0.14);
  color: #facc15;
}
</style>
