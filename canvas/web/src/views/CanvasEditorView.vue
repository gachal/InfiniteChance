<script setup lang="ts">
// 画布编辑器:vue-flow 四类节点(Agent/图片/视频,17 号票起加分析;
// 29 号票提示词节点升级为 Agent 并就地迁移)、节点旁 + 按钮的两击连线
// (30 号票)、整图防抖自动保存与版本冲突处理(09 号票);文生图任务
// 编排的客户端侧(10 号票):生成动作 → 结果节点先落库再提交 → 轮询
// 任务 → 产物写回节点。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Background } from '@vue-flow/background'
import { VueFlow, useVueFlow, type Connection, type Edge } from '@vue-flow/core'

import {
  ApiError,
  type AssetRecord,
  type CanvasDetail,
  type CanvasTask,
  type PromptTemplateOption,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import {
  type AgentChatMessage,
  type AgentNodeData,
  appendAgentTurn,
  initialData,
  NODE_TYPE_LABEL,
  normalizeNodeType,
  type CanvasNodeData,
  type CanvasNodeType,
  type MediaNodeData,
} from '../graph'
import {
  appendComposerRef,
  appendVideoRef,
  composerImageUrls,
  composerVideoRefs,
  composeSize,
  durationRangeFor,
  isRelayableRef,
  mediaSyncPatch,
  parseDurationInput,
  resultNodeData,
  rolesForKind,
  taskUrlFor,
  VIDEO_ROLE_CAPS,
  withVideoRefRole,
  type ComposerRef,
  type VideoComposerRef,
  type VideoRefKind,
  type VideoRefRole,
} from '../composer'
import { useAutosave } from '../composables/useAutosave'
import { useCanvasTasks } from '../composables/useCanvasTasks'
import { useConnection } from '../composables/useConnection'
import { canInsertRecord } from '../records'
import AssetPanel from '../components/AssetPanel.vue'
import GenerationComposer from '../components/GenerationComposer.vue'
import GenerationRecordsPanel from '../components/GenerationRecordsPanel.vue'
import AgentNode from '../components/nodes/AgentNode.vue'
import AnalysisNode from '../components/nodes/AnalysisNode.vue'
import ImageNode from '../components/nodes/ImageNode.vue'
import VideoNode from '../components/nodes/VideoNode.vue'

const route = useRoute()
const router = useRouter()
const { client, clearSession } = useAuth()

const canvasId = Number(route.params.id)

// useVueFlow 在组件挂载前调用:编辑器拥有这个 flow 实例,
// toObject/addNodes 等操作与 <VueFlow> 渲染共享同一份状态。
const {
  addEdges,
  addNodes,
  addSelectedNodes,
  edges: flowEdges,
  findNode,
  fitView,
  getSelectedNodes,
  nodes: flowNodes,
  onConnect,
  onEdgesChange,
  onNodeClick,
  onNodesChange,
  onPaneClick,
  removeSelectedNodes,
  setEdges,
  setNodes,
  toObject,
  updateNodeData,
  viewport,
} = useVueFlow()

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

/** 程序化连线与两击连线(30 号票)共用的落边形状:id 形制
 * `e-{source}-{target}`,Handle 锚点缺省(null)= 节点左右缘中点。 */
function connectNodes(source: string, target: string): void {
  addEdges([
    {
      id: `e-${source}-${target}`,
      source,
      target,
      sourceHandle: null,
      targetHandle: null,
    },
  ])
}

// ---- 生成任务(10 号票文生图,12 号票图生视频)----

// 生图/图生视频模型目录:拉取失败视为暂无可用模型,生成入口随之隐藏。
const imageModels = ref<string[]>([])
const videoModels = ref<string[]>([])

/** 产物落位与提示词回填:成功任务的补丁(对账逻辑在 composer.ts 的
 * mediaSyncPatch)写进绑定节点;节点是否存在以图为准 —— 结果节点在提交
 * 任务前已先落库,这里不负责物化,也不复活被删除的节点;产物本体在素
 * 材库里,重开可查。27 号票:节点缺 prompt/model 而任务行有时一并补写,
 * 旧产物重开画布轮询到任务行后自动长出提示词。 */
function syncTaskToCanvas(task: CanvasTask): void {
  const node = findNode(task.node_id)
  const data = node?.data as MediaNodeData | undefined
  if (!node || !data) {
    return
  }
  const patch = mediaSyncPatch(task, data)
  if (patch) {
    updateNodeData(task.node_id, patch)
    autosave.markDirty()
  }
}

/** 生成记录面板的数据源(28 号票):useCanvasTasks 每张任务表的同步快照
 * (新到旧,与节点轮询同拍)—— 面板开着时新任务完成,行内状态随之翻成
 * 终态;提交/重试/取消的单条快照即时合并,不等下一轮拉取。 */
const taskRecords = ref<CanvasTask[]>([])

const taskSync = useCanvasTasks({
  fetchTasks: () => client.listCanvasTasks(canvasId),
  retryTask: (taskId) => client.retryCanvasTask(canvasId, taskId),
  cancelTask: (taskId) => client.cancelCanvasTask(canvasId, taskId),
  onTask: syncTaskToCanvas,
  onTasks: (tasks) => {
    taskRecords.value = tasks
  },
})

const generating = ref(false)
const generateError = ref('')
const retryingNode = ref('')
const cancelingNode = ref('')

/** 图片任务的统一提交路径(10 号票纪律;21 号票起对话框发送走这里):结果节点与连线先入图并立即落库(autosave flush 跳过防抖),再
 * 提交任务 —— 浏览器随后关闭,任务与节点都在服务端/图里,重开不丢。
 * 21 号票修订:source 是空占位图片节点(无产物、无任务绑定)时直接作
 * 为结果节点填入 —— 工具栏添加的图片节点即从零生成的锚点,不再新建;
 * 有产物或任务历史的节点,结果落其右侧新节点并连线(自动选中,参考条
 * 切到新产物,迭代链连续)。imageUrls 非空 = 图生图(服务端按有无参考
 * 分流 generations/edits)。27 号票:两条落点都把 prompt/model 写进结果
 * 节点 —— 失败任务同样保留,重试时可见当时生成的是什么。 */
async function submitImageTask(payload: {
  prompt: string
  model: string
  size?: string
  imageUrls?: string[]
  sourceNodeId: string
}): Promise<void> {
  if (generating.value) {
    return
  }
  const source = findNode(payload.sourceNodeId)
  if (!source) {
    return
  }
  const sourceData = source.data as MediaNodeData | undefined
  const fillsSource =
    source.type === 'image' &&
    (sourceData?.url ?? '') === '' &&
    !taskSync.byNode.get(source.id)
  generating.value = true
  generateError.value = ''
  try {
    let targetNodeId = source.id
    if (!fillsSource) {
      nodeSeq += 1
      targetNodeId = `image-${Date.now()}-${nodeSeq}`
      // 新结果节点成为唯一选中(addNodes 的入参类型不带选中位,落图后
      // 用 store 的选区动作补上)。
      removeSelectedNodes(getSelectedNodes.value)
      addNodes([
        {
          id: targetNodeId,
          type: 'image',
          position: { x: source.position.x + 320, y: source.position.y },
          data: resultNodeData(payload.prompt, payload.model),
        },
      ])
      const added = findNode(targetNodeId)
      if (added) {
        addSelectedNodes([added])
      }
      connectNodes(source.id, targetNodeId)
    } else {
      updateNodeData(source.id, { prompt: payload.prompt, model: payload.model })
    }
    // 填入路径同样要 flush:锚点节点多半还在防抖窗口里没落库,任务必须
    // 等图持久化后再提交;已全部落库时多一次 markDirty 只是无变化的版本
    // 推进,无实际代价(flush 在 idle 态返回 false,不能省掉 markDirty)。
    autosave.markDirty()
    const saved = await autosave.flush()
    if (!saved) {
      generateError.value = '画布尚未保存成功,生成任务未提交;请先解决保存问题'
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: targetNodeId,
      kind: 'image',
      prompt: payload.prompt,
      model: payload.model,
      ...(payload.size ? { size: payload.size } : {}),
      ...(payload.imageUrls && payload.imageUrls.length > 0 ? { image_urls: payload.imageUrls } : {}),
    })
    taskSync.track(task)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '生成任务提交失败,请稍后再试'
  } finally {
    generating.value = false
  }
}

// ---- 生成对话框(21 号票图片模式;24 号票视频模式)----

const wrapEl = ref<HTMLDivElement | null>(null)

// 模式:选中视频节点时锁定视频生成,选中图片节点默认图片模式、可切视频
// (切过去时节点产物自动进参考条、默认角色「首帧」)。两套草稿按模式独立
// 存放,互不清空(ADR 0002)。
const composerMode = ref<'image' | 'video'>('image')

// 图片模式草稿:提示词/模型/比例/分辨率不随选中切换清空;比例 × 分辨率
// 在发送时经 composeSize 组合成 size 串(双自动 = 不传)。
const composerPrompt = ref('')
const composerModel = ref('')
const composerRatio = ref('')
const composerResolution = ref('')
// 本会话上传的参考图(素材引用);选中节点产物作为 chips 由下方归并。
const uploadedRefs = ref<ComposerRef[]>([])
// 选中节点的产物 chip 可被用户移除: detachment 只针对当前选中,选中
// 一变(切到别的图)即复位。
const detachedFromSelection = ref(false)
const refUploading = ref(false)

// 视频模式草稿(24 号票):提示词/模型/时长/分辨率/比例/参考条独立一套。
const videoPrompt = ref('')
const videoModel = ref('')
const videoDuration = ref('')
const videoRatio = ref('')
const videoResolution = ref('')
const uploadedVideoRefs = ref<VideoComposerRef[]>([])
const detachedVideoFromSelection = ref(false)

/** 对话框的上下文 = 最近选中的图片或视频节点(多选时取数组末位);空 =
 * 未选中(对话框不出现,Agent/分析节点不唤起 —— 维持现状)。 */
const composerNode = computed(() => {
  const selected = getSelectedNodes.value.filter(
    (n) => n.type === 'image' || n.type === 'video',
  )
  return selected.length > 0 ? selected[selected.length - 1] : null
})

/** 视频节点锁定视频模式;图片节点默认图片模式。 */
const composerModeLocked = computed(() => composerNode.value?.type === 'video')

watch(
  () => composerNode.value?.id ?? '',
  () => {
    detachedFromSelection.value = false
    detachedVideoFromSelection.value = false
    composerMode.value = composerModeLocked.value ? 'video' : 'image'
  },
)

watch(
  () => imageModels.value,
  (list) => {
    if (!list.includes(composerModel.value)) {
      composerModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

watch(
  () => videoModels.value,
  (list) => {
    if (!list.includes(videoModel.value)) {
      videoModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

/** 生效参考图 = 选中节点产物(未拆下时)+ 本会话上传,收编规则走
 * composer 纯函数(去重、data URI 不收、上限 4)。 */
const composerRefs = computed<ComposerRef[]>(() => {
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  let list: ComposerRef[] = []
  if (!detachedFromSelection.value && node && data?.url && isRelayableRef(data.url)) {
    list = appendComposerRef(list, { url: data.url, asset_id: data.asset_id })
  }
  for (const up of uploadedRefs.value) {
    list = appendComposerRef(list, up)
  }
  return list
})

/** 视频参考条(24 号票):选中节点产物按种类落默认角色 —— 图片产物 →
 * 首帧(12 号票图生视频语义平移)、视频产物 → 参考视频;上传条目按上传
 * 时定下的角色进条。收编规则走 appendVideoRef(角色上限、去重)。 */
const videoComposerRefs = computed<VideoComposerRef[]>(() => {
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  let list: VideoComposerRef[] = []
  if (!detachedVideoFromSelection.value && node && data?.url && isRelayableRef(data.url)) {
    const kind: VideoRefKind = node.type === 'video' ? 'video' : 'image'
    const role: VideoRefRole = node.type === 'video' ? 'reference_video' : 'first_frame'
    list = appendVideoRef(list, { url: data.url, kind, role, asset_id: data.asset_id })
  }
  for (const up of uploadedVideoRefs.value) {
    list = appendVideoRef(list, up)
  }
  return list
})

/** 对话框只在选中图片/视频节点、且当前模式有可用模型时存在(21 号票修订:
 * 打开画布即常驻底部中央的形态已按用户反馈移除);工具栏添加节点并选中
 * 它,对话框随之出现 —— 空视频占位节点即纯文生视频的锚点。 */
const composerVisible = computed(() => {
  if (!composerNode.value) {
    return false
  }
  return composerMode.value === 'video' ? videoModels.value.length > 0 : imageModels.value.length > 0
})

/** 悬浮定位:贴选中节点正下方(视口变换 + 节点尺寸换算成画布区坐标,
 * 拖动/缩放/图片加载都跟随),并 clamp 在画布区内。 */
const composerStyle = computed(() => {
  const node = composerNode.value
  if (!node) {
    // v-if 已挡住渲染,这里只是类型兜底。
    return { display: 'none' }
  }
  const vp = viewport.value
  const wrapW = wrapEl.value?.clientWidth ?? 0
  const wrapH = wrapEl.value?.clientHeight ?? 0
  const halfW = 280
  const estH = 240
  const dims = node.dimensions
  const left = vp.x + (node.position.x + (dims?.width ?? 200) / 2) * vp.zoom
  const top = vp.y + (node.position.y + (dims?.height ?? 160)) * vp.zoom + 12
  const clampedLeft = Math.min(Math.max(left, halfW + 8), Math.max(wrapW - halfW - 8, halfW + 8))
  const clampedTop = Math.min(Math.max(top, 8), Math.max(wrapH - estH, 8))
  return { left: `${clampedLeft}px`, top: `${clampedTop}px`, transform: 'translateX(-50%)' }
})

/** 参考条移除(图片模式):第 0 位且来自选中节点 = 拆下产物 chip(发送
 * 退化为文生图,连线仍建立);其余按上传列表移除。 */
function onRemoveRef(index: number): void {
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  const hasSelectionChip =
    !detachedFromSelection.value && !!(node && data?.url && isRelayableRef(data.url))
  if (hasSelectionChip && index === 0) {
    detachedFromSelection.value = true
    return
  }
  const uploadIndex = hasSelectionChip ? index - 1 : index
  uploadedRefs.value = uploadedRefs.value.filter((_, i) => i !== uploadIndex)
}

/** 参考条移除(视频模式):同款纪律,拆下的是当前选中节点的产物 chip。 */
function onRemoveVideoRef(index: number): void {
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  const hasSelectionChip =
    !detachedVideoFromSelection.value && !!(node && data?.url && isRelayableRef(data.url))
  if (hasSelectionChip && index === 0) {
    detachedVideoFromSelection.value = true
    return
  }
  const uploadIndex = hasSelectionChip ? index - 1 : index
  uploadedVideoRefs.value = uploadedVideoRefs.value.filter((_, i) => i !== uploadIndex)
}

/** 视频参考条上切换角色(24 号票):图片 chip 可在首帧/尾帧/参考图间切。
 * 选中节点的产物 chip 是计算值,改角色 = 物化进上传列表(原位拆下,新位
 * 按 appendVideoRef 的上限纪律收编);同一 URL 已在上传列表里时(先上传
 * 过、又选中了同款产物)物化会被去重挡下,回退为改既有上传条目的角色,
 * 让切换总是生效。上传 chip 用 withVideoRefRole 原地换角色。 */
function onSetVideoRefRole(index: number, role: VideoRefRole): void {
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  const hasSelectionChip =
    !detachedVideoFromSelection.value && !!(node && data?.url && isRelayableRef(data.url))
  if (hasSelectionChip && index === 0) {
    const url = data!.url!
    const next = appendVideoRef(uploadedVideoRefs.value, {
      url,
      kind: 'image',
      role,
      asset_id: data!.asset_id,
    })
    if (next !== uploadedVideoRefs.value) {
      uploadedVideoRefs.value = next
      detachedVideoFromSelection.value = true
      return
    }
    const existing = uploadedVideoRefs.value.findIndex((r) => r.url === url)
    if (existing >= 0) {
      uploadedVideoRefs.value = withVideoRefRole(uploadedVideoRefs.value, existing, role)
      detachedVideoFromSelection.value = true
    }
    return
  }
  const uploadIndex = hasSelectionChip ? index - 1 : index
  uploadedVideoRefs.value = withVideoRefRole(uploadedVideoRefs.value, uploadIndex, role)
}

/** 参考图上传(图片模式):复用 18 号票素材入库,内容寻址引用进列表(与
 * 素材面板插入同语义),顺带获得转存与跨画布复用。 */
async function onComposerUpload(file: File): Promise<void> {
  if (refUploading.value) {
    return
  }
  if (composerMode.value === 'video') {
    await onComposerUploadVideo(file)
    return
  }
  refUploading.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, 'image')
    uploadedRefs.value = appendComposerRef(uploadedRefs.value, { url: a.content_url, asset_id: a.id })
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    refUploading.value = false
  }
}

/** 视频参考的种类按浏览器 MIME 判断,缺失时按扩展名兜底;服务端按魔数
 * 嗅探最终裁决,这里的判断只为选对 kind 字段与立即反馈。 */
function videoUploadKindOf(file: File): VideoRefKind {
  if (file.type.startsWith('video/')) {
    return 'video'
  }
  if (file.type.startsWith('audio/')) {
    return 'audio'
  }
  if (file.type.startsWith('image/')) {
    return 'image'
  }
  if (/\.(mp4|m4v|webm|mov|avi)$/i.test(file.name)) {
    return 'video'
  }
  if (/\.(mp3|wav|m4a|ogg|flac)$/i.test(file.name)) {
    return 'audio'
  }
  return 'image'
}

/** 参考上传(视频模式):图片/视频/音频入库后按默认角色进条 —— 图片在
 * 首帧空缺时默认首帧(与选中图片节点产物同款默认),否则参考图;视频/
 * 音频角色固定。上传前先按种类预检空位:该种类已无任何可扮角色可收时
 * 直接报错不传字节,免得上传成素材却在参考条上拒收(留下孤儿素材行)。 */
async function onComposerUploadVideo(file: File): Promise<void> {
  if (refUploading.value) {
    return
  }
  const kind = videoUploadKindOf(file)
  const roles = rolesForKind(kind)
  const vacant = roles.some((role) =>
    videoComposerRefs.value.filter((r) => r.role === role).length < VIDEO_ROLE_CAPS[role],
  )
  if (!vacant) {
    generateError.value =
      kind === 'image' ? '图片参考已满员(首帧/尾帧各 1,参考图最多 4)' : `${kind === 'video' ? '视频' : '音频'}参考已满员`
    return
  }
  refUploading.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, kind)
    const role: VideoRefRole =
      kind === 'video'
        ? 'reference_video'
        : kind === 'audio'
          ? 'reference_audio'
          : videoComposerRefs.value.some((r) => r.role === 'first_frame')
            ? 'reference_image'
            : 'first_frame'
    uploadedVideoRefs.value = appendVideoRef(uploadedVideoRefs.value, {
      url: a.content_url,
      kind,
      role,
      asset_id: a.id,
    })
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    refUploading.value = false
  }
}

/** 草稿更新的模式分发:video 分支写视频草稿,image 分支写图片草稿
 * (两套草稿独立,切换不清空)。 */
function onDraftUpdate(
  videoSet: (v: string) => void,
  imageSet: (v: string) => void,
  value: string,
): void {
  if (composerMode.value === 'video') {
    videoSet(value)
  } else {
    imageSet(value)
  }
}

/** 对话框发送:按模式分流。图片模式 —— 选中图片节点产物即参考图(可拆
 * 下),有参考走图生图、无参考走文生图(空占位锚点直接填入),比例×分
 * 辨率组合成 size。视频模式 —— 参考组合交给 submitVideoTask(空参考 =
 * 文生视频),时长「自动」不传 seconds,分辨率档位串直接作 size,比例
 * 走 ratio。发送成功草稿全保留(27 号票,推翻 21/24 号票「成功后清空
 * 提示词」定案):改一版直接重发即迭代链;提示词已随提交落到结果节点,
 * 「清空防丢」的前提不复存在。 */
async function onComposerSend(): Promise<void> {
  const node = composerNode.value
  if (!node || generating.value) {
    return
  }
  if (composerMode.value === 'video') {
    const prompt = videoPrompt.value.trim()
    if (prompt === '' || videoModel.value === '') {
      return
    }
    const vRange = durationRangeFor(videoModel.value)
    const parsed = parseDurationInput(videoDuration.value, vRange)
    if (parsed === null) {
      generateError.value = `时长需为 ${vRange.min} 到 ${vRange.max} 之间的整数,留空表示自动`
      return
    }
    const refs = composerVideoRefs(videoComposerRefs.value)
    await submitVideoTask({
      prompt,
      model: videoModel.value,
      seconds: parsed === '' ? undefined : parsed,
      size: videoResolution.value === '' ? undefined : videoResolution.value,
      ratio: videoRatio.value === '' ? undefined : videoRatio.value,
      videoRefs: refs.length > 0 ? refs : undefined,
      sourceNodeId: node.id,
    })
    return
  }
  const prompt = composerPrompt.value.trim()
  if (prompt === '' || composerModel.value === '') {
    return
  }
  const urls = composerImageUrls(composerRefs.value)
  await submitImageTask({
    prompt,
    model: composerModel.value,
    size: composeSize(composerRatio.value, composerResolution.value) || undefined,
    imageUrls: urls.length > 0 ? urls : undefined,
    sourceNodeId: node.id,
  })
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

/** 视频任务的统一提交路径(24 号票对话框视频模式;发送落点与图片模式
 * 同款纪律):结果视频节点与连线先入图并立即落库(autosave flush 跳过
 * 防抖),再提交任务。空视频占位锚点(工具栏「+视频」,无产物、无任务
 * 绑定)发送直接填入本节点 —— 纯文生视频由此落脚;有产物或任务绑定的
 * 视频节点发送落右侧新视频节点并连线(自动选中,参考条随之切换);图片
 * 节点上切视频模式发送恒落新视频节点(图片锚点装不下视频产物,连线表达
 * 迭代来源)。videoRefs 非空 = 结构化参考(空 = 文生视频),服务端解引
 * 用;seconds 缺省 = 自动(厂商缺省);分辨率档位串直接作 size。27 号
 * 票:两条落点都把 prompt/model 写进结果视频节点。 */
async function submitVideoTask(payload: {
  prompt: string
  model: string
  size?: string
  ratio?: string
  seconds?: number
  videoRefs?: { url: string; kind: VideoRefKind; role: VideoRefRole }[]
  sourceNodeId: string
}): Promise<void> {
  if (generating.value) {
    return
  }
  const source = findNode(payload.sourceNodeId)
  if (!source) {
    return
  }
  const sourceData = source.data as MediaNodeData | undefined
  const fillsSource =
    source.type === 'video' && (sourceData?.url ?? '') === '' && !taskSync.byNode.get(source.id)
  generating.value = true
  generateError.value = ''
  try {
    let targetNodeId = source.id
    if (!fillsSource) {
      nodeSeq += 1
      targetNodeId = `video-${Date.now()}-${nodeSeq}`
      removeSelectedNodes(getSelectedNodes.value)
      addNodes([
        {
          id: targetNodeId,
          type: 'video',
          position: { x: source.position.x + 320, y: source.position.y },
          data: resultNodeData(payload.prompt, payload.model),
        },
      ])
      const added = findNode(targetNodeId)
      if (added) {
        addSelectedNodes([added])
      }
      connectNodes(source.id, targetNodeId)
    } else {
      updateNodeData(source.id, { prompt: payload.prompt, model: payload.model })
    }
    autosave.markDirty()
    const saved = await autosave.flush()
    if (!saved) {
      generateError.value = '画布尚未保存成功,生成任务未提交;请先解决保存问题'
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: targetNodeId,
      kind: 'video',
      prompt: payload.prompt,
      model: payload.model,
      ...(payload.size ? { size: payload.size } : {}),
      ...(payload.ratio ? { ratio: payload.ratio } : {}),
      ...(payload.seconds !== undefined ? { seconds: payload.seconds } : {}),
      ...(payload.videoRefs && payload.videoRefs.length > 0
        ? { video_refs: payload.videoRefs }
        : {}),
    })
    taskSync.track(task)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '生成任务提交失败,请稍后再试'
  } finally {
    generating.value = false
  }
}

// ---- Agent 会话(29 号票)与视频反推(13 号票)----

// 技能与聊天模型目录。服务端按请求读表,管理端的增删改即刻生效;
// 这里负责前端目录的新鲜度:窗口重新聚焦时刷新(管理端常在另一窗口
// 操作),技能失效导致生成失败时也立即刷新,让失效 chip 当场收回。
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

/** Agent 会话的一轮(29 号票):输入作本轮主题/修改意见,服务端把技能
 * 渲染文本作首条指令、拼历史与本轮输入经网关聊天生成。结果写回本节点
 * 文本区并追加进对话历史(20 轮上限截断最旧),随后沿连线自动投递。
 * 技能刚被删/停用时立刻刷新目录,悬空 chip 当场收回。 */
async function onGeneratePrompt(
  nodeId: string,
  payload: { template_id?: number; topic: string; model: string; history: AgentChatMessage[] },
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
      ...(payload.template_id != null ? { template_id: payload.template_id } : {}),
      topic: payload.topic,
      model: payload.model,
      ...(payload.history.length > 0 ? { history: payload.history } : {}),
    })
    const messages = appendAgentTurn(payload.history, payload.topic, result.text)
    updateNodeData(nodeId, { text: result.text, messages })
    autosave.markDirty()
    deliverPromptFrom(nodeId)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '提示词生成失败,请稍后再试'
    // 技能刚被删除/停用时本地目录已过期:立刻刷新,悬空 chip 当场收回。
    if (e instanceof ApiError && (e.status === 404 || e.code === 'template_disabled')) {
      void refreshCatalogs()
    }
  } finally {
    promptGenerating.value = false
  }
}

/** 技能变更(选中/移除/悬空收回)= 开新会话:写 skill_id 并清空对话
 * 历史(换技能清历史重算);文本草稿保留,已投递文本不受影响。 */
function onSkillChange(nodeId: string, skillId: number | null): void {
  updateNodeData(nodeId, {
    ...(skillId != null ? { skill_id: skillId } : { skill_id: undefined }),
    messages: [],
  })
  autosave.markDirty()
}

/** 投递(29 号票推模式):把 Agent 节点当前文本写进所有下游连线上的媒体
 * 节点 prompt 字段(产物 url/asset_id 不动,仅覆盖 prompt;下游含分析
 * 节点时只投媒体节点);无下游媒体节点时结果只留本节点。生成成功自动
 * 投递一次,手改后可点「投递」重推。 */
function deliverPromptFrom(agentNodeId: string): number {
  const node = findNode(agentNodeId)
  const data = node?.data as AgentNodeData | undefined
  const text = data?.text.trim() ?? ''
  if (!node || text === '') {
    return 0
  }
  let delivered = 0
  for (const edge of toObject().edges) {
    if (edge.source !== agentNodeId) {
      continue
    }
    const target = findNode(edge.target)
    if (target && (target.type === 'image' || target.type === 'video')) {
      updateNodeData(target.id, { prompt: text })
      delivered += 1
    }
  }
  if (delivered > 0) {
    autosave.markDirty()
  }
  return delivered
}

function onDeliver(agentNodeId: string): void {
  deliverPromptFrom(agentNodeId)
}

/** Agent 节点的「投递」按钮可用性 = 下游连线上存在媒体节点(按连线源
 * 汇总,供节点禁用态)。 */
const downstreamHasMedia = computed(() => {
  const isMedia = new Set<string>()
  const typeOf = (id: string) => flowNodes.value.find((n) => n.id === id)?.type
  for (const edge of flowEdges.value) {
    const t = typeOf(edge.target)
    if (t === 'image' || t === 'video') {
      isMedia.add(edge.source)
    }
  }
  return isMedia
})

/** 反推/派生的落图纪律(11/13 号票共用,29 号票落点改 Agent):生成的
 * 提示词恒落为新 Agent 节点,与来源节点连线(派生关系可见),图由自动
 * 保存收尾;新会话无历史,文本可手编后投递下游媒体节点。 */
function landAgentNode(sourceNodeId: string, text: string): void {
  const node = findNode(sourceNodeId)
  if (!node) {
    return
  }
  nodeSeq += 1
  const newId = `agent-${Date.now()}-${nodeSeq}`
  addNodes([
    {
      id: newId,
      type: 'agent',
      position: { x: node.position.x + 260, y: node.position.y },
      data: { text } satisfies AgentNodeData,
    },
  ])
  connectNodes(sourceNodeId, newId)
}

/** 视频反推提示词(13 号票):以视频节点持有的地址为输入,经网关多模态
 * 聊天同步分析;结果恒落为新 Agent 节点并与视频节点连线(29 号票,原为
 * 「新提示词节点」;连线不变),可手编后投递下游媒体节点形成闭环。 */
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
    landAgentNode(videoNodeId, result.text)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '视频反推失败,请稍后再试'
  } finally {
    videoReverseGenerating.value = false
  }
}

// ---- 画布分析(17 号票)----

// 在途分析绑定到分析节点 id:来源节点上的按钮只看「有没有分析在跑」,
// 分析节点自己则按 id 命中「分析中…」占位。一次只跑一个分析(聊天动作
// 无取消语义,连点只会多花钱)。
const analyzingNode = ref('')

/** 执行一次分析:以来源节点的产物地址发起同步调用,文本写入分析节点。
 * 失败时分析节点保留(文本为空),由横幅报错 —— 空节点可原地重新分析
 * 或删除(选中 + Delete)。 */
async function runAnalysis(
  sourceNodeId: string,
  analysisNodeId: string,
  payload: { model: string },
): Promise<void> {
  const source = findNode(sourceNodeId)
  const media = source?.data as MediaNodeData | undefined
  if (!source || !media?.url || payload.model === '') {
    return
  }
  if (source.type !== 'video' && source.type !== 'image') {
    return
  }
  analyzingNode.value = analysisNodeId
  generateError.value = ''
  try {
    const result = await client.analyzeMedia(canvasId, {
      node_id: analysisNodeId,
      media_url: media.url,
      media_kind: source.type,
      model: payload.model,
    })
    updateNodeData(analysisNodeId, { text: result.text, model: payload.model })
    autosave.markDirty()
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '分析失败,请稍后再试'
  } finally {
    analyzingNode.value = ''
  }
}

/** 视频/图片节点上的「分析」动作:分析节点与连线先入图并立即落盘
 * (flush 跳过防抖),再发起同步调用 —— 与生成任务「结果节点先落库再
 * 提交」同一纪律,分析比反推慢,用户需要看到占位。 */
async function onAnalyzeAction(
  sourceNodeId: string,
  payload: { model: string },
): Promise<void> {
  if (analyzingNode.value !== '') {
    return
  }
  const source = findNode(sourceNodeId)
  if (!source || (source.type !== 'video' && source.type !== 'image')) {
    return
  }
  nodeSeq += 1
  const analysisId = `analysis-${Date.now()}-${nodeSeq}`
  addNodes([
    {
      id: analysisId,
      type: 'analysis',
      position: { x: source.position.x + 260, y: source.position.y },
      data: initialData('analysis'),
    },
  ])
  connectNodes(sourceNodeId, analysisId)
  autosave.markDirty()
  const saved = await autosave.flush()
  if (!saved) {
    generateError.value = '画布尚未保存成功,分析未发起;请先解决保存问题'
    return
  }
  await runAnalysis(sourceNodeId, analysisId, payload)
}

/** 空分析节点的原地重新分析:经连线找回来源媒体节点,不新建节点。 */
async function onReanalyze(
  analysisNodeId: string,
  payload: { model: string },
): Promise<void> {
  if (analyzingNode.value !== '') {
    return
  }
  const sourceId = toObject().edges.find((e) => e.target === analysisNodeId)?.source
  if (!sourceId) {
    return
  }
  await runAnalysis(sourceId, analysisNodeId, payload)
}

// ---- 两击连线(30 号票)----

// 连线唯一入口 = 节点旁常驻 + 按钮:点击进入连接态,预连线跟随鼠标,点
// 目标节点完成(方向按 + 在左/右自动定),Esc/点空白/点非法节点取消。
// 合法矩阵见 connection.ts;历史已存边不清洗不校验,程序化连线(反推落
// 节点、对话框发送、分析动作)不经此路径照常直连。
const connection = useConnection({
  nodes: () => flowNodes.value,
  onCommit: ({ source, target }) => {
    // 同向边已存在时不重复建(id 形制与程序化连线一致,重复会串 id);
    // 意图已表达,连接态照常退出。
    const exists = flowEdges.value.some((e) => e.source === source && e.target === target)
    if (!exists) {
      connectNodes(source, target)
    }
  },
})
const connecting = computed(() => connection.active.value)

/** 预连线起点:+ 所在节点在「连下游」时锚右缘中点、「接上游」时锚左缘
 * 中点(画布区屏幕坐标,随视口缩放平移跟随 —— 与对话框悬浮同一换算)。 */
const previewAnchor = computed(() => {
  const p = connection.pending.value
  if (!p) {
    return null
  }
  const node = findNode(p.nodeId)
  if (!node) {
    return null
  }
  const vp = viewport.value
  const w = node.dimensions?.width ?? 200
  const h = node.dimensions?.height ?? 160
  return {
    x: vp.x + (node.position.x + (p.side === 'right' ? w : 0)) * vp.zoom,
    y: vp.y + (node.position.y + h / 2) * vp.zoom,
  }
})

// 连接态下点击节点只表达连线意图:暂关选区(elements-selectable),点击
// 经 onNodeClick 进状态机;点空白经 onPaneClick 取消(vue-flow 自带的
// 清空选区与其不冲突)。框选/拖动节点手势保持原样,不与连接态抢语义。
onNodeClick(({ node }) => {
  if (connection.pending.value) {
    connection.clickNode(node.id)
  }
})
onPaneClick(() => {
  connection.cancel()
})

/** 预连线跟随:画布区屏幕坐标(与预连线 overlay 同一坐标系)。 */
function onCanvasMouseMove(e: MouseEvent): void {
  if (!connection.pending.value) {
    return
  }
  const rect = wrapEl.value?.getBoundingClientRect()
  if (!rect) {
    return
  }
  connection.moveTo({ x: e.clientX - rect.left, y: e.clientY - rect.top })
}

/** Esc 取消连接态(窗口级监听,连接态外按下无副作用)。 */
function onGlobalKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    connection.cancel()
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

// 节点内容编辑(Agent 文本草稿)不产生变更事件:由节点上抛,这里落到
// flow 状态并标记脏。
function onTextChange(nodeId: string, text: string): void {
  updateNodeData(nodeId, { text })
  autosave.markDirty()
}

let nodeSeq = 0

function addNode(type: CanvasNodeType): void {
  nodeSeq += 1
  const step = (toObject().nodes.length % 8) * 48
  const id = `${type}-${Date.now()}-${nodeSeq}`
  addNodes([
    {
      id,
      type,
      position: { x: 140 + step, y: 120 + step },
      data: initialData(type),
    },
  ])
  // 新节点成为唯一选中:空图片/视频占位节点即从零生成的锚点(24 号票),
  // 添加后对话框随即贴上(模式按节点类型自动定),不再要求用户多点一下。
  // Agent/分析节点选中也无害 —— 对话框只认图片/视频节点。
  removeSelectedNodes(getSelectedNodes.value)
  const added = findNode(id)
  if (added) {
    addSelectedNodes([added])
  }
  // addNodes 会产生 'add' 变更事件,那里已 markDirty;这里无需重复。
}

// ---- 素材库面板(14 号票)与生成记录面板(28 号票)----

const assetPanelOpen = ref(false)
const recordsPanelOpen = ref(false)

// 两个面板同占画布右缘,互斥打开。
function toggleAssetPanel(): void {
  assetPanelOpen.value = !assetPanelOpen.value
  if (assetPanelOpen.value) {
    recordsPanelOpen.value = false
  }
}

function toggleRecordsPanel(): void {
  recordsPanelOpen.value = !recordsPanelOpen.value
  if (recordsPanelOpen.value) {
    assetPanelOpen.value = false
  }
}

/** 素材引用落画布的公共落点(素材面板插入、生成记录插入共用,28 号票):
 * 节点持有素材的内容寻址引用(asset_id + content_url),不复制字节,跨画布
 * 复用同一素材。 */
function insertAssetRef(kind: 'image' | 'video', assetId: number, contentUrl: string): void {
  nodeSeq += 1
  const type: CanvasNodeType = kind === 'video' ? 'video' : 'image'
  const step = (toObject().nodes.length % 8) * 48
  addNodes([
    {
      id: `${type}-${Date.now()}-${nodeSeq}`,
      type,
      position: { x: 140 + step, y: 120 + step },
      data: { url: contentUrl, asset_id: assetId, note: '' } satisfies MediaNodeData,
    },
  ])
}

/** 素材插入:从素材库把历史产物放进当前画布。音频素材没有对应节点类型,
 * 不落画布(参考音频经对话框上传入口进条)。 */
function insertAsset(a: AssetRecord): void {
  if (a.kind === 'audio') {
    return
  }
  insertAssetRef(a.kind === 'video' ? 'video' : 'image', a.id, a.content_url)
}

// ---- 生成记录面板的动作(28 号票)----

/** 点记录行:画布平移定位并选中绑定节点;节点已被删(node_id 悬空)时静默
 * 降级为无操作,不报错。 */
function locateRecordTask(task: CanvasTask): void {
  const node = findNode(task.node_id)
  if (!node) {
    return
  }
  removeSelectedNodes(getSelectedNodes.value)
  addSelectedNodes([node])
  void fitView({ nodes: [task.node_id], padding: 1, maxZoom: 1.2, duration: 300 })
}

/** 记录行「插入画布」:成功节点已删或想再放一份时,以素材内容寻址引用落
 * 新节点 —— 与素材面板插入同一语义(守卫复用 canInsertRecord 的判定,
 * taskUrlFor 在 asset_id > 0 时恒为内容寻址路径)。 */
function insertRecordTask(task: CanvasTask): void {
  if (!canInsertRecord(task)) {
    return
  }
  insertAssetRef(task.kind === 'video' ? 'video' : 'image', task.asset_id, taskUrlFor(task))
}

// ---- 本机素材上传(18 号票)----

const uploading = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)

/** 上传种类按浏览器 MIME 判断,缺失时按扩展名兜底;服务端按魔数嗅探最
 * 终裁决,这里的判断只为选对 kind 字段与立即反馈。 */
function uploadKindOf(file: File): 'image' | 'video' {
  if (file.type.startsWith('video/')) {
    return 'video'
  }
  if (file.type.startsWith('image/')) {
    return 'image'
  }
  return /\.(mp4|m4v|webm|mov|avi)$/i.test(file.name) ? 'video' : 'image'
}

/** 工具栏「上传」:本机文件进素材库,成功后落媒体节点 —— 与素材面板插
 * 入同一语义(asset_id + 内容寻址),节点先上图,自动保存收尾。 */
async function onUploadChange(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (!file || uploading.value) {
    return
  }
  uploading.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, uploadKindOf(file))
    insertAsset(a)
  } catch (err) {
    generateError.value = err instanceof ApiError ? err.message : '上传失败,请稍后再试'
  } finally {
    uploading.value = false
  }
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
  // 读旧图的就地迁移(29 号票):存在 prompt 等旧类型值时立即触发一次
  // 保存,让 agent 新值落库,而不是等下一次用户编辑才持久化。
  const hasLegacyType = detail.graph.nodes.some(
    (raw) => normalizeNodeType((raw as { type?: string }).type) !== (raw as { type?: string }).type,
  )
  if (hasLegacyType) {
    autosave.markDirty()
  }
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
    // 读图归一(29 号票):类型值 prompt 就地映射为 agent,自动保存后
    // 落库为新值;历史连线不清洗。
    const type = normalizeNodeType(node.type)
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
  window.addEventListener('keydown', onGlobalKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('focus', refreshCatalogs)
  window.removeEventListener('keydown', onGlobalKeydown)
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

    <div
      ref="wrapEl"
      class="canvas-wrap"
      @mousemove="onCanvasMouseMove"
    >
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
        :nodes-connectable="false"
        :elements-selectable="!connecting"
      >
        <Background :gap="24" />
        <template #node-agent="nodeProps">
          <AgentNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :skills="promptTemplates"
            :chat-models="promptModels"
            :prompt-generating="promptGenerating"
            :has-downstream="downstreamHasMedia.has(nodeProps.id)"
            :connect-state="connection.stateOf(nodeProps.id)"
            @text-change="onTextChange(nodeProps.id, $event)"
            @send="onGeneratePrompt(nodeProps.id, $event)"
            @skill-change="onSkillChange(nodeProps.id, $event)"
            @deliver="onDeliver(nodeProps.id)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
        <template #node-image="nodeProps">
          <ImageNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :task="taskSync.byNode.get(nodeProps.id) ?? null"
            :retrying="retryingNode === nodeProps.id"
            :chat-models="promptModels"
            :analyzing="analyzingNode !== ''"
            :connect-state="connection.stateOf(nodeProps.id)"
            @retry="onRetry(nodeProps.id)"
            @analyze="onAnalyzeAction(nodeProps.id, $event)"
            @connect-start="connection.start(nodeProps.id, $event)"
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
            :analyzing="analyzingNode !== ''"
            :connect-state="connection.stateOf(nodeProps.id)"
            @retry="onRetry(nodeProps.id)"
            @cancel="onCancelVideo(nodeProps.id)"
            @reverse-prompt="onReversePrompt(nodeProps.id, $event)"
            @analyze="onAnalyzeAction(nodeProps.id, $event)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
        <template #node-analysis="nodeProps">
          <AnalysisNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :chat-models="promptModels"
            :analyzing="analyzingNode === nodeProps.id"
            :connect-state="connection.stateOf(nodeProps.id)"
            @analyze="onReanalyze(nodeProps.id, $event)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
      </VueFlow>
      <!-- 30 号票:连接态预连线(画布区屏幕坐标;pointer-events none,
           不挡任何底层交互)。 -->
      <svg
        v-if="connecting && previewAnchor && connection.pointer.value"
        class="connect-preview"
      >
        <line
          :x1="previewAnchor.x"
          :y1="previewAnchor.y"
          :x2="connection.pointer.value.x"
          :y2="connection.pointer.value.y"
        />
        <circle
          :cx="connection.pointer.value.x"
          :cy="connection.pointer.value.y"
          r="5"
        />
      </svg>
      <GenerationComposer
        v-if="composerVisible"
        :style="composerStyle"
        :mode="composerMode"
        :mode-locked="composerModeLocked"
        :image-models="imageModels"
        :video-models="videoModels"
        :refs="composerRefs"
        :video-refs="videoComposerRefs"
        :prompt="composerMode === 'video' ? videoPrompt : composerPrompt"
        :model="composerMode === 'video' ? videoModel : composerModel"
        :ratio="composerMode === 'video' ? videoRatio : composerRatio"
        :resolution="composerMode === 'video' ? videoResolution : composerResolution"
        :duration="videoDuration"
        :generating="generating"
        :uploading="refUploading"
        @update:mode="composerMode = $event"
        @update:prompt="onDraftUpdate(($v) => (videoPrompt = $v), ($v) => (composerPrompt = $v), $event)"
        @update:model="onDraftUpdate(($v) => (videoModel = $v), ($v) => (composerModel = $v), $event)"
        @update:ratio="onDraftUpdate(($v) => (videoRatio = $v), ($v) => (composerRatio = $v), $event)"
        @update:resolution="onDraftUpdate(($v) => (videoResolution = $v), ($v) => (composerResolution = $v), $event)"
        @update:duration="videoDuration = $event"
        @remove-ref="composerMode === 'video' ? onRemoveVideoRef($event) : onRemoveRef($event)"
        @set-ref-role="onSetVideoRefRole"
        @upload="onComposerUpload"
        @send="onComposerSend"
      />
      <AssetPanel
        v-if="assetPanelOpen"
        @insert="insertAsset"
      />
      <GenerationRecordsPanel
        v-if="recordsPanelOpen"
        :tasks="taskRecords"
        @locate="locateRecordTask"
        @insert="insertRecordTask"
      />
    </div>

    <footer class="toolbar">
      <span class="hint">拖拽节点排布,点节点旁 + 号连线(左接上游、右连下游)。</span>
      <span class="add-group">
        <button
          v-for="t in (['agent', 'image', 'video'] as const)"
          :key="t"
          :class="`add-${t}`"
          type="button"
          @click="addNode(t)"
        >
          + {{ NODE_TYPE_LABEL[t] }}
        </button>
        <button
          class="add-asset"
          type="button"
          :title="assetPanelOpen ? '关闭素材库面板' : '打开素材库:浏览历史素材并插入画布'"
          @click="toggleAssetPanel"
        >
          {{ assetPanelOpen ? '收起素材库' : '素材库' }}
        </button>
        <button
          class="add-records"
          type="button"
          :title="recordsPanelOpen ? '关闭生成记录面板' : '打开生成记录:本画布的任务流水(最近 200 条)'"
          @click="toggleRecordsPanel"
        >
          {{ recordsPanelOpen ? '收起记录' : '生成记录' }}
        </button>
        <button
          class="add-upload"
          type="button"
          :disabled="uploading"
          :title="uploading ? '上传中…' : '上传本机图片/视频进素材库,并作为节点插入画布'"
          @click="uploadInput?.click()"
        >
          {{ uploading ? '上传中…' : '上传' }}
        </button>
        <input
          ref="uploadInput"
          class="upload-input"
          type="file"
          accept="image/*,video/*"
          @change="onUploadChange"
        >
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

/* 30 号票:连接态预连线,盖在画布上但不接收任何指针事件。 */
.connect-preview {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 4;
}

.connect-preview line {
  stroke: #7aa2f7;
  stroke-width: 2;
  stroke-dasharray: 6 4;
}

.connect-preview circle {
  fill: rgba(122, 162, 247, 0.9);
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

.add-agent {
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

.add-asset {
  background: rgba(165, 180, 252, 0.16);
  color: #a5b4fc;
}

.add-records {
  background: rgba(251, 146, 60, 0.14);
  color: #fdba74;
}

.add-upload {
  background: rgba(94, 234, 212, 0.14);
  color: #5eead4;
}

.add-upload:disabled {
  cursor: default;
  opacity: 0.6;
}

.upload-input {
  display: none;
}
</style>
