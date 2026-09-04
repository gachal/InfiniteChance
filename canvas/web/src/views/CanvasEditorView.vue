<script setup lang="ts">
// 画布编辑器:vue-flow 三类节点、自由拖拽连线、整图防抖自动保存
// 与版本冲突处理(09 号票);文生图任务编排的客户端侧(10 号票):
// 生成动作 → 结果节点先落库再提交 → 轮询任务 → 产物写回节点。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Background } from '@vue-flow/background'
import { VueFlow, useVueFlow, type Connection, type Edge } from '@vue-flow/core'

import {
  ApiError,
  type CanvasDetail,
  type CanvasTask,
  type PromptTemplateOption,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import {
  initialData,
  isCanvasNodeType,
  NODE_TYPE_LABEL,
  type CanvasNodeData,
  type CanvasNodeType,
  type MediaNodeData,
  type PromptNodeData,
} from '../graph'
import { useAutosave } from '../composables/useAutosave'
import { useCanvasTasks } from '../composables/useCanvasTasks'
import ImageNode from '../components/nodes/ImageNode.vue'
import PromptNode from '../components/nodes/PromptNode.vue'
import VideoNode from '../components/nodes/VideoNode.vue'

const route = useRoute()
const router = useRouter()
const { client, clearSession } = useAuth()

const canvasId = Number(route.params.id)

// useVueFlow 在组件挂载前调用:编辑器拥有这个 flow 实例,
// toObject/addNodes 等操作与 <VueFlow> 渲染共享同一份状态。
const { addEdges, addNodes, findNode, fitView, onConnect, onEdgesChange, onNodesChange, setEdges, setNodes, toObject, updateNodeData } =
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

// ---- 生成任务(10 号票文生图,12 号票图生视频)----

// 生图/图生视频模型目录:拉取失败视为暂无可用模型,生成入口随之隐藏。
const imageModels = ref<string[]>([])
const videoModels = ref<string[]>([])

/** 任务产物地址:图片任务落 image_url,视频任务落 video_url;
 * data: URI 不进图(几 MB 的内联图片会把整图 JSON 顶破上限):
 * 有素材引用时走内容寻址,由服务端解出字节。 */
function taskUrlFor(task: CanvasTask): string {
  const url = task.kind === 'video' ? task.video_url : task.image_url
  if (url.startsWith('data:') && task.asset_id > 0) {
    return `/api/assets/${task.asset_id}/content`
  }
  return url
}

/** 产物落位:成功任务的地址写进绑定节点;有变化才落一次盘。节点是否存在
 * 以图为准 —— 结果节点在提交任务前已先落库,这里不负责物化,也不复活
 * 被删除的节点;产物本体在素材库里,重开可查。 */
function syncTaskToCanvas(task: CanvasTask): void {
  if (task.status !== 'succeeded') {
    return
  }
  const url = taskUrlFor(task)
  if (url === '') {
    return
  }
  const node = findNode(task.node_id)
  const data = node?.data as MediaNodeData | undefined
  if (node && data && data.url !== url) {
    updateNodeData(task.node_id, { url })
    autosave.markDirty()
  }
}

const taskSync = useCanvasTasks({
  fetchTasks: () => client.listCanvasTasks(canvasId),
  retryTask: (taskId) => client.retryCanvasTask(canvasId, taskId),
  cancelTask: (taskId) => client.cancelCanvasTask(canvasId, taskId),
  onTask: syncTaskToCanvas,
})

const generating = ref(false)
const videoGenerating = ref(false)
const generateError = ref('')
const retryingNode = ref('')
const cancelingNode = ref('')

/** 生成动作:结果节点(图片)与提示词连线先入图并落库,再提交任务 ——
 * 浏览器随后关闭,任务与节点也都在服务端/图里,重开不丢。 */
async function onGenerate(promptNodeId: string, payload: { model: string }): Promise<void> {
  if (generating.value) {
    return
  }
  const promptNode = findNode(promptNodeId)
  const data = promptNode?.data as PromptNodeData | undefined
  const text = data?.text.trim() ?? ''
  if (!promptNode || text === '' || payload.model === '') {
    return
  }
  generating.value = true
  generateError.value = ''
  try {
    nodeSeq += 1
    const nodeId = `image-${Date.now()}-${nodeSeq}`
    addNodes([
      {
        id: nodeId,
        type: 'image',
        position: { x: promptNode.position.x + 260, y: promptNode.position.y },
        data: initialData('image'),
      },
    ])
    addEdges([
      {
        id: `e-${promptNodeId}-${nodeId}`,
        source: promptNodeId,
        target: nodeId,
        sourceHandle: null,
        targetHandle: null,
      },
    ])
    autosave.markDirty()
    const saved = await autosave.flush()
    if (!saved) {
      generateError.value = '画布尚未保存成功,生成任务未提交;请先解决保存问题'
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: nodeId,
      kind: 'image',
      prompt: text,
      model: payload.model,
    })
    taskSync.track(task)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '生成任务提交失败,请稍后再试'
  } finally {
    generating.value = false
  }
}

/** 失败任务的原地重试:同一任务回队,节点绑定不变。 */
async function onRetry(nodeId: string): Promise<void> {
  const task = taskSync.byNode.get(nodeId)
  if (!task || retryingNode.value !== '') {
    return
  }
  retryingNode.value = nodeId
  try {
    await taskSync.retry(task.id)
  } catch (e) {
    if (!taskSync.isRetryConflict(e)) {
      generateError.value = e instanceof ApiError ? e.message : '重试失败,请稍后再试'
    }
  } finally {
    retryingNode.value = ''
  }
}

/** 进行中视频任务的原地取消(12 号票):服务端同步取消网关任务并退预扣。 */
async function onCancelVideo(nodeId: string): Promise<void> {
  const task = taskSync.byNode.get(nodeId)
  if (!task || cancelingNode.value !== '') {
    return
  }
  cancelingNode.value = nodeId
  try {
    await taskSync.cancel(task.id)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '取消失败,请稍后再试'
  } finally {
    cancelingNode.value = ''
  }
}

/** 图生视频动作(12 号票):以图片节点的产物为参考图,结果视频节点与
 * 连线先入图并落库,再提交任务 —— 与文生图同一纪律。 */
async function onGenerateVideo(
  imageNodeId: string,
  payload: { model: string; prompt: string; seconds: number },
): Promise<void> {
  if (videoGenerating.value) {
    return
  }
  const imageNode = findNode(imageNodeId)
  const data = imageNode?.data as MediaNodeData | undefined
  const refUrl = data?.url ?? ''
  if (!imageNode || !refUrl.startsWith('http') || payload.model === '' || payload.prompt === '') {
    return
  }
  videoGenerating.value = true
  generateError.value = ''
  try {
    nodeSeq += 1
    const nodeId = `video-${Date.now()}-${nodeSeq}`
    addNodes([
      {
        id: nodeId,
        type: 'video',
        position: { x: imageNode.position.x + 260, y: imageNode.position.y },
        data: initialData('video'),
      },
    ])
    addEdges([
      {
        id: `e-${imageNodeId}-${nodeId}`,
        source: imageNodeId,
        target: nodeId,
        sourceHandle: null,
        targetHandle: null,
      },
    ])
    autosave.markDirty()
    const saved = await autosave.flush()
    if (!saved) {
      generateError.value = '画布尚未保存成功,生成任务未提交;请先解决保存问题'
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: nodeId,
      kind: 'video',
      prompt: payload.prompt,
      model: payload.model,
      seconds: payload.seconds,
      image_url: refUrl,
    })
    taskSync.track(task)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '生成任务提交失败,请稍后再试'
  } finally {
    videoGenerating.value = false
  }
}

// ---- 提示词生成(11 号票)与视频反推(13 号票)----

// 模板与聊天模型目录。服务端按请求读表,管理端的增删改即刻生效;
// 这里负责前端目录的新鲜度:窗口重新聚焦时刷新(管理端常在另一窗口
// 操作),模板失效导致生成失败时也立即刷新,让失效选项当场消失。
const promptTemplates = ref<PromptTemplateOption[]>([])
const promptModels = ref<string[]>([])
const promptGenerating = ref(false)
const videoReverseGenerating = ref(false)

let refreshingCatalogs = false
async function refreshCatalogs(): Promise<void> {
  if (refreshingCatalogs) {
    return
  }
  refreshingCatalogs = true
  try {
    const [templates, models] = await Promise.all([
      client.listPromptTemplateCatalog(),
      client.listPromptModels(),
    ])
    promptTemplates.value = templates
    promptModels.value = models
  } catch {
    /* 目录拉不到就保持现状,不打扰画布编辑 */
  } finally {
    refreshingCatalogs = false
  }
}

/** 生成提示词:主题按模板经网关聊天生成,同步返回文本。结果落位 ——
 * 当前节点为空 → 直接写回本节点;已有内容 → 落为新提示词节点并连线
 * (派生关系可见)。文本落图后由自动保存收尾,无需先 flush。 */
async function onGeneratePrompt(
  nodeId: string,
  payload: { template_id: number; topic: string; model: string },
): Promise<void> {
  if (promptGenerating.value) {
    return
  }
  const node = findNode(nodeId)
  if (!node || payload.topic === '' || payload.model === '') {
    return
  }
  promptGenerating.value = true
  generateError.value = ''
  try {
    const result = await client.generatePrompt(canvasId, {
      node_id: nodeId,
      template_id: payload.template_id,
      topic: payload.topic,
      model: payload.model,
    })
    const data = node.data as PromptNodeData | undefined
    if (data && data.text.trim() === '') {
      updateNodeData(nodeId, { text: result.text })
      autosave.markDirty()
      return
    }
    landPromptNode(nodeId, result.text)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '提示词生成失败,请稍后再试'
    // 模板刚被删除/停用时本地目录已过期:立刻刷新,失效选项当场消失。
    if (e instanceof ApiError && (e.status === 404 || e.code === 'template_disabled')) {
      void refreshCatalogs()
    }
  } finally {
    promptGenerating.value = false
  }
}

/** 反推/派生的落图纪律(11/13 号票共用):生成的提示词恒落为新提示词
 * 节点,与来源节点连线(派生关系可见),图由自动保存收尾。 */
function landPromptNode(sourceNodeId: string, text: string): void {
  const node = findNode(sourceNodeId)
  if (!node) {
    return
  }
  nodeSeq += 1
  const newId = `prompt-${Date.now()}-${nodeSeq}`
  addNodes([
    {
      id: newId,
      type: 'prompt',
      position: { x: node.position.x + 260, y: node.position.y },
      data: { text },
    },
  ])
  addEdges([
    {
      id: `e-${sourceNodeId}-${newId}`,
      source: sourceNodeId,
      target: newId,
      sourceHandle: null,
      targetHandle: null,
    },
  ])
}

/** 视频反推提示词(13 号票):以视频节点持有的地址为输入,经网关多模态
 * 聊天同步分析;结果恒落为新提示词节点并与视频节点连线(派生关系可见,
 * 随后可直接从该节点发起生图/生视频形成闭环)。 */
async function onReversePrompt(
  videoNodeId: string,
  payload: { model: string },
): Promise<void> {
  if (videoReverseGenerating.value) {
    return
  }
  const node = findNode(videoNodeId)
  const data = node?.data as MediaNodeData | undefined
  if (!node || !data?.url || payload.model === '') {
    return
  }
  videoReverseGenerating.value = true
  generateError.value = ''
  try {
    const result = await client.reversePrompt(canvasId, {
      node_id: videoNodeId,
      video_url: data.url,
      model: payload.model,
    })
    landPromptNode(videoNodeId, result.text)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '视频反推失败,请稍后再试'
  } finally {
    videoReverseGenerating.value = false
  }
}

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
  // 任务同步必须等整图加载落地之后再开始:抢先回来的一次同步会被
  // applyServerGraph 的 setNodes 整个覆盖掉。
  void loadCanvas().finally(() => {
    void taskSync.start()
  })
  void client
    .listImageModels()
    .then((models) => {
      imageModels.value = models
    })
    .catch(() => {
      /* 目录拉不到就隐藏生成入口,不打扰画布编辑 */
    })
  void client
    .listVideoModels()
    .then((models) => {
      videoModels.value = models
    })
    .catch(() => {
      /* 同上:视频模型目录拉不到就不显示图生视频入口 */
    })
  void refreshCatalogs()
  window.addEventListener('focus', refreshCatalogs)
  window.addEventListener('beforeunload', beforeUnload)
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('focus', refreshCatalogs)
  taskSync.stop()
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

      <span
        v-if="taskSync.activeCount.value > 0"
        class="task-state"
      >
        生成中 {{ taskSync.activeCount.value }} 个任务…
      </span>
    </header>

    <div
      v-if="generateError"
      class="generate-error"
      role="alert"
    >
      <p>{{ generateError }}</p>
      <button
        class="ghost"
        type="button"
        @click="generateError = ''"
      >
        知道了
      </button>
    </div>

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
            :models="imageModels"
            :generating="generating"
            :templates="promptTemplates"
            :chat-models="promptModels"
            :prompt-generating="promptGenerating"
            @text-change="onTextChange(nodeProps.id, $event)"
            @generate="onGenerate(nodeProps.id, $event)"
            @generate-prompt="onGeneratePrompt(nodeProps.id, $event)"
          />
        </template>
        <template #node-image="nodeProps">
          <ImageNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :task="taskSync.byNode.get(nodeProps.id) ?? null"
            :retrying="retryingNode === nodeProps.id"
            :video-models="videoModels"
            :video-generating="videoGenerating"
            @retry="onRetry(nodeProps.id)"
            @generate-video="onGenerateVideo(nodeProps.id, $event)"
          />
        </template>
        <template #node-video="nodeProps">
          <VideoNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :task="taskSync.byNode.get(nodeProps.id) ?? null"
            :retrying="retryingNode === nodeProps.id"
            :canceling="cancelingNode === nodeProps.id"
            :chat-models="promptModels"
            :reverse-generating="videoReverseGenerating"
            @retry="onRetry(nodeProps.id)"
            @cancel="onCancelVideo(nodeProps.id)"
            @reverse-prompt="onReversePrompt(nodeProps.id, $event)"
          />
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

.task-state {
  color: #4ade80;
  font-size: 13px;
  white-space: nowrap;
}

.generate-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.12);
  border-bottom: 1px solid rgba(224, 49, 49, 0.35);
}

.generate-error p {
  margin: 0;
  font-size: 14px;
}

.generate-error button {
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 8px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
  white-space: nowrap;
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
