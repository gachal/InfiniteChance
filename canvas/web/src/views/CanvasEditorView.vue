<script setup lang="ts">
// 画布编辑器:vue-flow 六类节点(Agent/图片/视频/分析;29 号票提示词
// 节点升级为 Agent 并就地迁移;39 号票追加预演台;52 号票追加角色引用
// 节点)、节点旁 + 按钮的两击连线(30 号票)、整图防抖自动保存与版本冲突
// 处理(09 号票);文生图任务编排的客户端侧(10 号票):生成动作 → 结果
// 节点先落库再提交 → 轮询任务 → 产物写回节点。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { VueFlow, useVueFlow, type Connection, type Edge } from '@vue-flow/core'

import {
  ApiError,
  type AssetRecord,
  type CanvasDetail,
  type CanvasTask,
  type CharacterRecord,
  type ModelPriceSummary,
  type PromptTemplateOption,
  type VoiceRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import {
  type AgentChatMedia,
  type AgentChatMessage,
  type AgentNodeData,
  agentCharacterInjection,
  appendAgentTurn,
  type CharacterNodeData,
  initialData,
  NODE_TYPE_LABEL,
  normalizeNodeType,
  type AudioNodeData,
  type CanvasNodeData,
  type CanvasNodeType,
  type LevelNodeData,
  type MediaNodeData,
  type NameNodeData,
  type NamePage,
  type PrevizNodeData,
  type ScriptNodeData,
  type StoryboardCut,
  type StoryboardNodeData,
  type VoiceNodeData,
} from '../graph'
import {
  type PendingAgentMedia,
  type AgentMediaKind,
  hasAgentMediaSlot,
  nextAgentMediaId,
} from '../agentMedia'
import {
  type AgentToolStep,
  applyAgentToolResult,
  scriptDraftFromPayload,
  setAgentToolApplier,
  storyboardDraftFromPayload,
  upsertAgentToolStep,
} from '../agentTools'
import { canAddLevelAsset, toggleLevelAsset } from '../level'
import {
  appendComposerRef,
  appendVideoRef,
  audioSyncPatch,
  composerImageUrls,
  composerRefFromAsset,
  composerVideoRefs,
  composeSize,
  defaultVideoRefRole,
  durationRangeFor,
  isReferenceRole,
  isRelayableRef,
  mediaSyncPatch,
  parseDurationInput,
  resultNodeData,
  taskUrlFor,
  videoRefFromAsset,
  VIDEO_REFS_TOTAL_CAP,
  VIDEO_ROLE_CAPS,
  withVideoRefRole,
  type ComposerRef,
  type VideoComposerRef,
  type VideoRefKind,
  type VideoRefRole,
} from '../composer'
import { characterImageContentURL } from '../characters'
import { buildCutVideoPrompt, frameContentURL } from '../storyboard'
import { useAutosave } from '../composables/useAutosave'
import { useCanvasTasks } from '../composables/useCanvasTasks'
import { useConnection } from '../composables/useConnection'
import { canInsertRecord } from '../records'
import { newNodeAnchor } from '../placement'
import { useComposerPlacement } from '../composables/useComposerPlacement'
import { useEditorToast } from '../composables/useEditorToast'
import CanvasTopbar from '../components/editor/CanvasTopbar.vue'
import EditorAlerts from '../components/editor/EditorAlerts.vue'
import CanvasToolbar from '../components/editor/CanvasToolbar.vue'
import EditorToast from '../components/editor/EditorToast.vue'
import AssetPanel from '../components/AssetPanel.vue'
import AgentHistoryPanel from '../components/AgentHistoryPanel.vue'
import AssetPicker from '../components/AssetPicker.vue'
import GenerationComposer from '../components/GenerationComposer.vue'
import GenerationRecordsPanel from '../components/GenerationRecordsPanel.vue'
import MediaLightbox from '../components/MediaLightbox.vue'
import PrevizEditorLayer from '../components/PrevizEditorLayer.vue'
import StoryboardEditorLayer from '../components/StoryboardEditorLayer.vue'
import NameEditorLayer from '../components/NameEditorLayer.vue'
import ScriptEditorLayer from '../components/ScriptEditorLayer.vue'
import AgentNode from '../components/nodes/AgentNode.vue'
import AnalysisNode from '../components/nodes/AnalysisNode.vue'
import CharacterNode from '../components/nodes/CharacterNode.vue'
import ImageNode from '../components/nodes/ImageNode.vue'
import LevelCard from '../components/nodes/LevelCard.vue'
import NameNode from '../components/nodes/NameNode.vue'
import PrevizNode from '../components/nodes/PrevizNode.vue'
import ScriptNode from '../components/nodes/ScriptNode.vue'
import StoryboardNode from '../components/nodes/StoryboardNode.vue'
import VideoNode from '../components/nodes/VideoNode.vue'
import AudioNode from '../components/nodes/AudioNode.vue'
import VoiceNode from '../components/nodes/VoiceNode.vue'

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
  screenToFlowCoordinate,
  setEdges,
  setNodes,
  toObject,
  updateNodeData,
  viewport,
  vueFlowRef,
} = useVueFlow()

const canvasName = ref('')
// IP 归属(50 号票):面包屑「IP 名 / 画布名」的数据源;ipId 同时是编辑器
// 内全部上传的固定归属(画布内上传传画布 IP,不可改)。uploadIPId 是全部
// uploadAsset 调用的归属参数单一来源(0 = 无 IP 维度,按全局上传)。
// 53 号票 A 段:画布回归通用,类型值不再读写展示(canvases.type 列冻结保留)。
const canvasIPID = ref(0)
const uploadIPId = computed(() => canvasIPID.value || undefined)
const canvasIPName = ref('')
const loadError = ref('')
const loading = ref(true)
const missing = ref(false)

const autosave = useAutosave({
  save: (expectedVersion) => client.saveCanvasGraph(canvasId, snapshot(), expectedVersion),
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

// ---- 撤销/重做与轻提示(38 号票评审 P1)----

// 撤销栈 = 整图快照(snapshot() 的输出形状)的环形队列:每个编辑边界
// (markChanged 的调用点)防抖 600ms 后入栈一帧 —— 拖动节点与流式文本
// 只在停手时占一帧;上限 50 帧。undo/redo 以 setNodes/setEdges 整图回放,
// 回放期间的变更事件照常 markDirty(落库语义不变)但不再入栈。
const undoStack: ReturnType<typeof snapshot>[] = []
const redoStack: ReturnType<typeof snapshot>[] = []
let historyArmed = false
let historyTimer: ReturnType<typeof setTimeout> | undefined
let applyingSnapshot = false

function scheduleHistoryCapture(): void {
  if (applyingSnapshot || !historyArmed) {
    return
  }
  if (historyTimer !== undefined) {
    clearTimeout(historyTimer)
  }
  historyTimer = setTimeout(() => {
    historyTimer = undefined
    pushHistory()
  }, 600)
}

function pushHistory(): void {
  if (applyingSnapshot) {
    return
  }
  const snap = snapshot()
  const top = undoStack[undoStack.length - 1]
  if (top && JSON.stringify(top) === JSON.stringify(snap)) {
    return
  }
  undoStack.push(snap)
  if (undoStack.length > 50) {
    undoStack.shift()
  }
  redoStack.length = 0
}

/** 整图加载/重载后重置基线:栈里只留当前帧,redo 清空。 */
function resetHistory(): void {
  undoStack.length = 0
  redoStack.length = 0
  historyArmed = true
  undoStack.push(snapshot())
}

function applyGraphSnapshot(s: ReturnType<typeof snapshot>): void {
  applyingSnapshot = true
  setNodes(s.nodes.map((n) => ({ ...n })))
  setEdges(s.edges.map((e) => ({ ...e })) as unknown as Edge[])
  void nextTick().finally(() => {
    applyingSnapshot = false
  })
  autosave.markDirty()
}

function undoGraph(): void {
  if (undoStack.length < 2) {
    return
  }
  redoStack.push(undoStack.pop()!)
  applyGraphSnapshot(undoStack[undoStack.length - 1])
}

function redoGraph(): void {
  const s = redoStack.pop()
  if (!s) {
    return
  }
  undoStack.push(s)
  applyGraphSnapshot(s)
}

/** 编辑边界的统一入口:标脏 + 排一次撤销快照。 */
function markChanged(): void {
  autosave.markDirty()
  scheduleHistoryCapture()
}

// 轻提示(38 号票评审):删除可撤销提示、非法连线反馈共用,4 秒自动消退。
const { notice, show: showNotice } = useEditorToast()

// ---- 生成任务(10 号票文生图,12 号票图生视频)----

// 生图/图生视频模型目录(38 号票起随行带价格摘要;拉取失败置失败位,
// 对话框进入禁态说明原因,不再静默蒸发 —— 评审 P1)。
const imageModels = ref<string[]>([])
const videoModels = ref<string[]>([])
const imagePrices = ref<Record<string, ModelPriceSummary>>({})
const videoPrices = ref<Record<string, ModelPriceSummary>>({})
const imageCatalogFailed = ref(false)
const videoCatalogFailed = ref(false)
const modelCatalogsLoaded = ref(false)
// 音频目录(57 号票):字符轨合成模型(/audio-models,网关代理),音色
// 创建表单的模型下拉与台词预估共用。enrollment(定制按次价)不在字符轨
// 目录里,从 image-models 的按次轨投影取(voice-enrollment 配了价就在)。
// 配乐目录(60 号票):按次配乐模型(/music-models),音频节点创作态表单
// 下拉与预估共用。音效目录(61 号票):按秒音效模型(/sfx-models),
// source='sfx' 创作态表单下拉与预估共用。
const audioModels = ref<string[]>([])
const audioPrices = ref<Record<string, ModelPriceSummary>>({})
const musicModels = ref<string[]>([])
const musicPrices = ref<Record<string, ModelPriceSummary>>({})
const sfxModels = ref<string[]>([])
const sfxPrices = ref<Record<string, ModelPriceSummary>>({})
const enrollmentPrice = computed(() => imagePrices.value['voice-enrollment'])

/** 拉取生图/视频模型目录:成功写名单与价格摘要,失败置失败位(对话框
 * 禁态 + 重试入口指向这里)。 */
function refreshModelCatalogs(): void {
  imageCatalogFailed.value = false
  videoCatalogFailed.value = false
  void client
    .listImageModels()
    .then((catalog) => {
      imageModels.value = catalog.models
      imagePrices.value = catalog.prices
    })
    .catch(() => {
      imageCatalogFailed.value = true
    })
    .finally(() => {
      modelCatalogsLoaded.value = true
    })
  void client
    .listVideoModels()
    .then((catalog) => {
      videoModels.value = catalog.models
      videoPrices.value = catalog.prices
    })
    .catch(() => {
      videoCatalogFailed.value = true
    })
  void client
    .listAudioModels()
    .then((catalog) => {
      audioModels.value = catalog.models
      audioPrices.value = catalog.prices
    })
    .catch(() => {
      /* 目录失败静默保持旧值:创建表单下拉可能为空,重试随窗口聚焦 */
    })
  void client
    .listMusicModels()
    .then((catalog) => {
      musicModels.value = catalog.models
      musicPrices.value = catalog.prices
    })
    .catch(() => {
      /* 目录失败静默保持旧值:创作态下拉可能为空,重试随窗口聚焦 */
    })
  void client
    .listSfxModels()
    .then((catalog) => {
      sfxModels.value = catalog.models
      sfxPrices.value = catalog.prices
    })
    .catch(() => {
      /* 目录失败静默保持旧值:创作态下拉可能为空,重试随窗口聚焦 */
    })
}

/** 产物落位与提示词回填:成功任务的补丁(对账逻辑在 composer.ts 的
 * mediaSyncPatch / audioSyncPatch)写进绑定节点;节点是否存在以图为准
 * —— 结果节点在提交任务前已先落库,这里不负责物化,也不复活被删除的
 * 节点;产物本体在素材库里,重开可查。27 号票:节点缺 prompt/model 而
 * 任务行有时一并补写,旧产物重开画布轮询到任务行后自动长出提示词。
 * 音频节点走 audioSyncPatch 专属形状(57 号票评审 F2:chars 回填,
 * asset_id 是唯一产物引用,不落 url)。 */
function syncTaskToCanvas(task: CanvasTask): void {
  const node = findNode(task.node_id)
  if (!node) {
    return
  }
  if (node.type === 'audio') {
    const audio = node.data as AudioNodeData
    const patch = audioSyncPatch(task, audio)
    if (patch) {
      updateNodeData(task.node_id, patch)
      markChanged()
    }
    return
  }
  const data = node.data as MediaNodeData | undefined
  if (!data) {
    return
  }
  const patch = mediaSyncPatch(task, data)
  if (patch) {
    updateNodeData(task.node_id, patch)
    markChanged()
  }
  if (task.kind === 'voice' && task.status === 'succeeded') {
    // 音色部署完成:台账行已由 worker 落库,目录刷新点亮节点卡面。
    void refreshVoices()
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
  background?: string
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
    markChanged()
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
      ...(payload.background ? { background: payload.background } : {}),
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
const topbarRef = ref<InstanceType<typeof CanvasTopbar> | null>(null)

// 模式:选中视频节点时锁定视频生成,选中图片节点默认图片模式、可切视频
// (切过去时节点产物自动进参考条、默认角色「首帧」)。两套草稿按模式独立
// 存放,互不清空(ADR 0002)。
const composerMode = ref<'image' | 'video'>('image')

// 图片模式草稿:提示词/模型/比例/分辨率不随选中切换清空;比例 × 分辨率
// 在发送时经 composeSize 组合成 size 串(双自动 = 不传);透明背景开关
// (37 号票)同款独立草稿,默认关、发送时折成 background 参数。
const composerPrompt = ref('')
const composerModel = ref('')
const composerRatio = ref('')
const composerResolution = ref('')
const composerTransparent = ref(false)
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
 * composer 纯函数(去重、data URI 不收、上限 9)。 */
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

/** 对话框只在选中图片/视频节点时存在(21 号票修订:打开画布即常驻底部
 * 中央的形态已按用户反馈移除);工具栏添加节点并选中它,对话框随之出现
 * —— 空视频占位节点即纯文生视频的锚点。目录拉取失败或未配价时对话框
 * 保留但禁用并说明原因(38 号票评审,替换旧的「无模型即隐藏」)。 */
const composerVisible = computed(() => composerNode.value !== null)

// 对话框消失(取消选中)时素材挑选面板随行关闭 —— 它是对话框的伴生面。
watch(composerVisible, (visible) => {
  if (!visible) {
    composerPickerOpen.value = false
  }
})

/** 对话框禁用原因:空 = 正常可用。目录未加载完(首屏竞态)不算失败。 */
const composerDisabledReason = computed(() => {
  if (composerMode.value === 'video') {
    if (videoCatalogFailed.value) {
      return '视频模型目录拉取失败(网关或画布服务不可达),生成暂不可用'
    }
    if (modelCatalogsLoaded.value && videoModels.value.length === 0) {
      return '没有已配价的视频模型:请在管理台「模型价格」配置按秒或视频 token 轨价格'
    }
    return ''
  }
  if (imageCatalogFailed.value) {
    return '生图模型目录拉取失败(网关或画布服务不可达),生成暂不可用'
  }
  if (modelCatalogsLoaded.value && imageModels.value.length === 0) {
    return '没有已配价的生图模型:请在管理台「模型价格」配置按次价格'
  }
  return ''
})

/** 悬浮定位:贴选中节点正下方(视口变换 + 节点尺寸换算成画布区坐标,
 * 拖动/缩放/图片加载都跟随),并 clamp 在画布区内。视频模式控件更多,
 * clamp 的估算高度按模式取档(评审:旧值一律 240,视频模式会贴边不准)。
 * 58 号票阶段一抽到 useComposerPlacement。 */
const composerStyle = useComposerPlacement({
  node: composerNode,
  mode: composerMode,
  wrapEl,
  viewport,
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
    const a = await client.uploadAsset(file, 'image', uploadIPId.value)
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

/** 视频参考条满员预检(42 号票守卫,上传与素材库挑选共用):该 kind 按
 * 默认角色能否再收一条,不能收时返回用户文案,能收返回 null。 */
function videoRefCapBlocked(refs: VideoComposerRef[], kind: VideoRefKind): string | null {
  const role = defaultVideoRefRole(refs, kind)
  const roleFull = refs.filter((r) => r.role === role).length >= VIDEO_ROLE_CAPS[role]
  const totalFull = refs.filter((r) => isReferenceRole(r.role)).length >= VIDEO_REFS_TOTAL_CAP
  if (!roleFull && !totalFull) {
    return null
  }
  return totalFull && !roleFull
    ? '参考条已满员(参考图/参考视频/参考音频合计最多 9 条)'
    : kind === 'image'
      ? '图片参考已满员(首帧/尾帧各 1,参考图最多 9)'
      : `${kind === 'video' ? '视频' : '音频'}参考已满员`
}

/** 参考上传(视频模式):图片/视频/音频入库后按默认角色进条(defaultVideoRefRole:
 * 图片在首帧空缺时默认首帧,否则参考图;视频/音频角色固定)。上传前先按
 * 「默认角色能否收下」预检(42 号票:含参考条目合计 ≤9 的总量守卫),该
 * 条目必被拒时直接报错不传字节,免得上传成素材却在参考条上拒收(留下
 * 孤儿素材行)。 */
async function onComposerUploadVideo(file: File): Promise<void> {
  if (refUploading.value) {
    return
  }
  const kind = videoUploadKindOf(file)
  const refs = videoComposerRefs.value
  const blocked = videoRefCapBlocked(refs, kind)
  if (blocked) {
    generateError.value = blocked
    return
  }
  refUploading.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, kind, uploadIPId.value)
    uploadedVideoRefs.value = appendVideoRef(uploadedVideoRefs.value, {
      url: a.content_url,
      kind,
      role: defaultVideoRefRole(refs, kind),
      asset_id: a.id,
    })
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    refUploading.value = false
  }
}

// ---- 素材库挑选(47 号票):对话框两模式从素材库直接落参考条 ----

const composerPickerOpen = ref(false)

/** kind 白名单随模式:图片模式仅图片;视频模式图/视频/音频三类(顶部
 * 过滤档,「全部」= 三类)。 */
const composerPickerKinds = computed<Array<'image' | 'video' | 'audio'>>(() =>
  composerMode.value === 'video' ? ['image', 'video', 'audio'] : ['image'],
)

const composerPickerHint = computed(() =>
  composerMode.value === 'video'
    ? '点选按默认角色挂为参考,再点取消;图片条目的角色可在条上切换。'
    : '点选挂为参考图,再点取消。',
)

/** 已选高亮 = 当前模式参考条上的素材 id(选中节点产物 + 上传/挑选条目)。 */
const composerPickerSelectedIds = computed<number[]>(() => {
  const ids = composerMode.value === 'video' ? videoComposerRefs.value : composerRefs.value
  return ids.map((r) => r.asset_id).filter((id): id is number => id != null)
})

function toggleComposerPicker(): void {
  composerPickerOpen.value = !composerPickerOpen.value
  if (composerPickerOpen.value) {
    // 面板同占画布右缘,互斥打开(32 号票纪律)。
    assetPanelOpen.value = false
    recordsPanelOpen.value = false
    historyNodeId.value = ''
    pickerNodeId.value = ''
    levelPickerNodeId.value = ''
  }
}

/** 从参考条摘掉一条素材挑选:选中节点产物 chip 按 asset_id 拆下
 * (detached 语义与移除按钮同款),上传/挑选条目按 asset_id 移除。 */
function detachComposerAsset(a: AssetRecord): void {
  if (composerMode.value === 'video') {
    const node = composerNode.value
    const data = node?.data as MediaNodeData | undefined
    const hasSelectionChip =
      !detachedVideoFromSelection.value && !!(node && data?.url && isRelayableRef(data.url))
    if (hasSelectionChip && data?.asset_id === a.id) {
      detachedVideoFromSelection.value = true
      return
    }
    uploadedVideoRefs.value = uploadedVideoRefs.value.filter((r) => r.asset_id !== a.id)
    return
  }
  const node = composerNode.value
  const data = node?.data as MediaNodeData | undefined
  const hasSelectionChip =
    !detachedFromSelection.value && !!(node && data?.url && isRelayableRef(data.url))
  if (hasSelectionChip && data?.asset_id === a.id) {
    detachedFromSelection.value = true
    return
  }
  uploadedRefs.value = uploadedRefs.value.filter((r) => r.asset_id !== a.id)
}

/** 素材库点选(对话框):已在条上 = 摘下;不在 = 按 44 号票默认角色规则
 * 落条(多选连选逐条应用:空白条多选图时第一张落首帧、其余落参考图),
 * 上限守卫与上传同款,超限整条拒收。 */
function onComposerPickToggle(a: AssetRecord): void {
  const selectedIds = composerPickerSelectedIds.value
  if (selectedIds.includes(a.id)) {
    detachComposerAsset(a)
    return
  }
  if (composerMode.value === 'video') {
    const refs = videoComposerRefs.value
    const blocked = videoRefCapBlocked(refs, a.kind as VideoRefKind)
    if (blocked) {
      generateError.value = blocked
      return
    }
    uploadedVideoRefs.value = appendVideoRef(uploadedVideoRefs.value, videoRefFromAsset(a, refs))
    return
  }
  uploadedRefs.value = appendComposerRef(uploadedRefs.value, composerRefFromAsset(a))
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
    background: composerTransparent.value ? 'transparent' : undefined,
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
    markChanged()
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
    promptModels.value = models.models
  } catch {
    /* 目录拉不到就保持现状,不打扰画布编辑 */
  } finally {
    refreshingCatalogs = false
  }
}

// ---- 本 IP 角色目录(52 号票)----

// 角色引用节点与 Agent 角色 chip 共用的目录源:按画布 ip_id 拉全树
// (51 号票目录 API)。节点只存 character_id 不存快照,渲染数据全从这
// 份目录解析 —— 目录刷新(重开画布/窗口聚焦)后角色改名、换封面、加
// 变体,节点即变,这就是「不存快照」的实证面。加载失败保持空目录:
// 节点落「角色不可用」占位(可换绑/删节点),不拦画布其他编辑。
const ipCharacters = ref<CharacterRecord[]>([])

async function refreshIPCharacters(): Promise<void> {
  if (canvasIPID.value <= 0) {
    return
  }
  try {
    ipCharacters.value = await client.listIPCharacters(canvasIPID.value)
  } catch {
    /* 拉不到就保持现状,下次聚焦再试 */
  }
}

// ---- 音色台账目录(57 号票,全局作用域)----

// 音色节点卡面与角色绑定 picker 共用的目录源:节点只存 ledger_id 不存
// 快照(角色节点同哲学),状态/试听随目录刷新。kind=voice 任务 succeeded
// 时由任务同步回调刷新(部署完成 → 台账行落库 → 节点亮起)。
const voicesCatalog = ref<VoiceRecord[]>([])

async function refreshVoices(): Promise<void> {
  try {
    voicesCatalog.value = await client.listVoices()
  } catch {
    /* 拉不到就保持现状,下次聚焦再试 */
  }
}

/** 角色节点绑定/换绑:data 只写 character_id(52 号票「不存快照」)。 */
function onCharacterBind(nodeId: string, characterId: number): void {
  updateNodeData(nodeId, { character_id: characterId } satisfies Partial<CharacterNodeData>)
  markChanged()
}

/** 落图节点 = 素材插入语义(52 号票):选中设定图按 asset_id + 内容寻址
 * 路径插为独立图片节点落视口中心,不复制字节、不连线 —— 设定图进生成
 * 链的桥(落图后经对话框参考条/首帧消费)。 */
function onCharacterLandImage(image: { asset_id: number }): void {
  insertAssetRef('image', image.asset_id, characterImageContentURL(image.asset_id))
  showNotice('已把设定图落为图片节点(视口中心)')
}

// ---- 音色闭环(57 号票)----

/** 音色表单事实落盘:updateNodeData 浅合并 + markChanged(表单逐键
 * 上抛,自动保存免费获得 —— 与絵コンテ板名同链)。 */
function onVoiceFormChange(nodeId: string, patch: Partial<VoiceNodeData>): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

// 音色节点创建表单的提交路径:create task 绑本节点(表单即在节点上,
// 无结果节点要先落库的问题 —— 数据已随整图 JSON 持久化);照生图纪律
// 先 markChanged + flush 再提交,防抖窗口里的表单编辑必须先落图。
const voiceSubmitting = ref(false)

async function submitVoiceTask(nodeId: string): Promise<void> {
  const node = findNode(nodeId)
  const data = node?.data as VoiceNodeData | undefined
  if (!node || !data || voiceSubmitting.value) {
    return
  }
  voiceSubmitting.value = true
  try {
    markChanged()
    const saved = await autosave.flush()
    if (!saved) {
      showNotice('画布尚未保存成功,任务未提交;请先解决保存问题')
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: nodeId,
      kind: 'voice',
      prompt: data.preview_text ?? '',
      model: data.target_model ?? '',
      voice: {
        mode: data.mode,
        name: data.name,
        target_model: data.target_model ?? '',
        preview_text: data.preview_text ?? '',
        ...(data.mode === 'design' && data.voice_prompt ? { voice_prompt: data.voice_prompt } : {}),
        ...(data.mode === 'clone' && data.sample_asset_id ? { sample_asset_id: data.sample_asset_id } : {}),
      },
    })
    taskSync.track(task)
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '音色创建任务提交失败,请稍后再试')
  } finally {
    voiceSubmitting.value = false
  }
}

// 配乐/歌曲节点创作态的提交路径(60 号票;62 号票增补歌曲):kind=music
// 任务绑本节点(表单就在节点上,数据已随整图 JSON 持久化);照音色先例先
// markChanged + flush 再提交,防抖窗口里的表单编辑必须先落图。人声歌曲
// (instrumental=false)提交前先把 source:'song' 与歌词落节点 —— 'song'
// 兼作创作意图标记,audioSyncPatch 对账时不覆盖;产物由 audioSyncPatch 按
// 配乐/歌曲对(account)回填(prompt/model/lyrics/source),节点从表单态
// 翻产物态。
const musicSubmitting = ref(false)

/** 配乐/歌曲/音效创作表单的事实变更(逐键上抛 → updateNodeData 浅合并
 * 落盘,自动保存免费获得;与音色表单同一纪律)。 */
function onMusicFormChange(nodeId: string, patch: Partial<AudioNodeData>): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

async function submitMusicTask(nodeId: string): Promise<void> {
  const node = findNode(nodeId)
  const data = node?.data as AudioNodeData | undefined
  if (!node || !data || musicSubmitting.value) {
    return
  }
  musicSubmitting.value = true
  try {
    // 62 号票:source:'song' = 人声歌曲意图(instrumental=false),先落节
    // 点再 flush;纯空白歌词按未携带处理(模型自动写词)。
    const instrumental = data.source !== 'song'
    if (!instrumental) {
      updateNodeData(nodeId, { source: 'song', lyrics: data.lyrics ?? '' } satisfies Partial<AudioNodeData>)
    }
    markChanged()
    const saved = await autosave.flush()
    if (!saved) {
      showNotice('画布尚未保存成功,任务未提交;请先解决保存问题')
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: nodeId,
      kind: 'music',
      prompt: data.prompt ?? '',
      model: data.model ?? '',
      instrumental,
      ...(!instrumental && (data.lyrics ?? '').trim() !== '' ? { lyrics: data.lyrics ?? '' } : {}),
    })
    taskSync.track(task)
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '配乐任务提交失败,请稍后再试')
  } finally {
    musicSubmitting.value = false
  }
}

// 音效节点创作态的提交路径(61 号票):kind=sfx 任务绑本节点,秒数与描述
// 已随表单变更落节点(source:'sfx' 是工具栏「音效」入口的创作意图标记,
// audioSyncPatch 对账时不覆盖);照配乐先例先 markChanged + flush 再提交,
// 防抖窗口里的表单编辑必须先落图;产物由 audioSyncPatch 按 sfx 对账
// (prompt/model/seconds 回填),节点从表单态翻产物态。
const sfxSubmitting = ref(false)

async function submitSfxTask(nodeId: string): Promise<void> {
  const node = findNode(nodeId)
  const data = node?.data as AudioNodeData | undefined
  if (!node || !data || sfxSubmitting.value) {
    return
  }
  sfxSubmitting.value = true
  try {
    markChanged()
    const saved = await autosave.flush()
    if (!saved) {
      showNotice('画布尚未保存成功,任务未提交;请先解决保存问题')
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: nodeId,
      kind: 'sfx',
      prompt: data.prompt ?? '',
      model: data.model ?? '',
      seconds: data.seconds ?? 0,
    })
    taskSync.track(task)
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '音效任务提交失败,请稍后再试')
  } finally {
    sfxSubmitting.value = false
  }
}

/** 音频节点创作态提交的分发(61 号票):source='sfx' 走音效链,其余(配
 * 乐/歌曲)走配乐链 —— 节点侧表单已按 source 分叉,这里同款分流。 */
function submitAudioTask(nodeId: string): void {
  const data = findNode(nodeId)?.data as AudioNodeData | undefined
  if (data?.source === 'sfx') {
    void submitSfxTask(nodeId)
  } else {
    void submitMusicTask(nodeId)
  }
}

// 克隆样本:本机上传 kind=audio 入素材库(服务端魔数裁决),成功回写
// sample_asset_id;失败轻提示,节点侧表单保持可再选。
const voiceUploading = ref(false)

async function onVoiceSampleFile(nodeId: string, file: File): Promise<void> {
  if (voiceUploading.value) {
    return
  }
  voiceUploading.value = true
  try {
    const a = await client.uploadAsset(file, 'audio', uploadIPId.value)
    updateNodeData(nodeId, { sample_asset_id: a.id } satisfies Partial<VoiceNodeData>)
    markChanged()
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '样本上传失败,请稍后再试')
  } finally {
    voiceUploading.value = false
  }
}

// 克隆样本的素材库挑选面板(与右缘面板互斥纪律一致,开时关其他面板)。
const voicePickerNodeId = ref('')
const voicePickerNode = computed(() =>
  voicePickerNodeId.value ? findNode(voicePickerNodeId.value) ?? null : null,
)
const voicePickerSelectedIds = computed<number[]>(() => {
  const data = voicePickerNode.value?.data as VoiceNodeData | undefined
  const id = data?.sample_asset_id ?? 0
  return id > 0 ? [id] : []
})

function openVoicePicker(nodeId: string): void {
  voicePickerNodeId.value = nodeId
  assetPanelOpen.value = false
  recordsPanelOpen.value = false
  composerPickerOpen.value = false
  historyNodeId.value = ''
  pickerNodeId.value = ''
  levelPickerNodeId.value = ''
}

function closeVoicePicker(): void {
  voicePickerNodeId.value = ''
}

function onVoicePickToggle(nodeId: string, a: AssetRecord): void {
  if (a.kind !== 'audio') {
    return
  }
  const data = findNode(nodeId)?.data as VoiceNodeData | undefined
  if (!data) {
    return
  }
  const next = data.sample_asset_id === a.id ? 0 : a.id
  updateNodeData(nodeId, { sample_asset_id: next } satisfies Partial<VoiceNodeData>)
  markChanged()
}

// 台词合成(消费闭环):先落右侧音频节点 + voice→audio 边并入图(生图
// 纪律),flush 后提交 kind=tts 任务绑音频节点 —— 浏览器随后关闭,节点、
// 连线与任务都在。
const speechSubmitting = ref(false)

async function onVoiceSpeech(nodeId: string, payload: { text: string; instruction: string }): Promise<void> {
  const node = findNode(nodeId)
  const data = node?.data as VoiceNodeData | undefined
  if (!node || !data || !data.ledger_id || speechSubmitting.value) {
    return
  }
  speechSubmitting.value = true
  try {
    nodeSeq += 1
    const audioId = `audio-${Date.now()}-${nodeSeq}`
    removeSelectedNodes(getSelectedNodes.value)
    addNodes([
      {
        id: audioId,
        type: 'audio',
        position: { x: node.position.x + 380, y: node.position.y },
        data: { asset_id: 0, source: 'tts', text: payload.text } satisfies AudioNodeData,
      },
    ])
    const added = findNode(audioId)
    if (added) {
      addSelectedNodes([added])
    }
    connectNodes(nodeId, audioId)
    markChanged()
    const saved = await autosave.flush()
    if (!saved) {
      showNotice('画布尚未保存成功,任务未提交;请先解决保存问题')
      return
    }
    const task = await client.createCanvasTask(canvasId, {
      node_id: audioId,
      kind: 'tts',
      prompt: payload.text,
      model: data.target_model ?? '',
      tts: {
        voice_ledger_id: data.ledger_id,
        text: payload.text,
        ...(payload.instruction ? { instruction: payload.instruction } : {}),
      },
    })
    taskSync.track(task)
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '台词合成任务提交失败,请稍后再试')
  } finally {
    speechSubmitting.value = false
  }
}

// 删除音色:二次确认后先经网关 delete_voice 成功再删台账行(服务端序);
// 成功后节点回表单态(台账引用清空,可再建),目录刷新。节点本身仅
// 表单事实,不动整图其他部分。
const deletingVoice = ref(false)

async function onDeleteVoice(nodeId: string): Promise<void> {
  const data = findNode(nodeId)?.data as VoiceNodeData | undefined
  const ledgerId = data?.ledger_id ?? 0
  if (!ledgerId || deletingVoice.value) {
    return
  }
  if (!window.confirm(`删除音色「${data?.name || ledgerId}」?上游与台账将一并删除,不可恢复。`)) {
    return
  }
  deletingVoice.value = true
  try {
    await client.deleteVoice(ledgerId)
    updateNodeData(nodeId, { ledger_id: undefined, status: undefined } satisfies Partial<VoiceNodeData>)
    markChanged()
    void refreshVoices()
    showNotice('音色已删除')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'voice_in_use') {
      showNotice('音色仍被角色绑定,请先在角色详情中解除绑定')
    } else {
      showNotice(e instanceof ApiError ? e.message : '删除音色失败,请稍后再试')
    }
  } finally {
    deletingVoice.value = false
  }
}

/** Agent 会话的一轮(29 号票;31 号票起走流式;32 号票带媒体;63 号票起
 * 走 Agent 工具循环端点):输入作本轮主题/修改意见,media 为本轮附件引用,
 * 服务端把技能渲染文本作首条指令、拼历史与本轮输入,带工具定义经网关
 * 流式循环生成。delta 实时写节点文本区(不标脏,autosave 防抖不被高频
 * 打扰);tool_call/tool_result 落过程时间线(UI 态),写图类 tool_result
 * 由前端经 applyAgentToolResult 落图(ADR 0006 单一写者),应用失败显式
 * 报错不静默;收尾一次性落盘 —— 成功追加对话历史(20 轮上限截断最旧,
 * 媒体随轮进历史;assistant 轮 = 最终回答全文 + 工具摘要行,循环中间帧
 * 不入持久历史)、持久固化设定段(52 号票历史固化)、清空待发附件并沿
 * 连线自动投递,失败保留已流出文本与待发附件(不回滚、不追加历史不投递)。
 * 技能刚被删/停用时立刻刷新目录,悬空 chip 当场收回。 */
async function onGeneratePrompt(
  nodeId: string,
  payload: {
    template_id?: number
    topic: string
    model: string
    media: AgentChatMedia[]
    history: AgentChatMessage[]
    character_ids: number[]
    character_section?: string
  },
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
  agentActiveNodeId.value = nodeId
  agentToolSteps.value = []
  agentApplySeq = 0
  // 流中途失败时已到手的增量(text)用于保留部分文本;成功路径以 done
  // 事件的累计全文为准。
  let text = ''
  try {
    const full = await client.agentStream(
      canvasId,
      {
        node_id: nodeId,
        ...(payload.template_id != null ? { template_id: payload.template_id } : {}),
        topic: payload.topic,
        model: payload.model,
        ...(payload.media.length > 0 ? { media: payload.media } : {}),
        ...(payload.history.length > 0 ? { history: payload.history } : {}),
        // 角色注入(52 号票历史固化):有固化段回传固化段,否则首轮带
        // character_ids 取最新数据渲染。
        ...agentCharacterInjection(payload.character_ids, payload.character_section),
      },
      {
        onDelta: (delta) => {
          text += delta
          updateNodeData(nodeId, { text })
        },
        onToolCall: (ev) => {
          agentToolSteps.value = upsertAgentToolStep(agentToolSteps.value, ev)
        },
        onToolResult: (ev) => {
          agentToolSteps.value = upsertAgentToolStep(agentToolSteps.value, ev)
          // 写图类结果由前端落图(ADR 0006):未注册应用器而有 payload 的
          // 结果显式报错,不静默丢弃。
          const applyError = applyAgentToolResult(ev.name, ev.payload)
          if (applyError) {
            generateError.value = applyError
          }
        },
      },
    )
    // 持久纪律(63 号票):assistant 轮 = 最终回答全文 + 工具调用摘要行;
    // 摘要行只进历史(会话面板可见做了什么),文本区保持纯回答 —— 投递
    // 到下游的是干净产物。
    const assistant = full.summary ? `${full.text}\n\n${full.summary}` : full.text
    const messages = appendAgentTurn(payload.history, payload.topic, assistant, payload.media)
    updateNodeData(nodeId, {
      text: full.text,
      messages,
      character_section: full.character_section || undefined,
    })
    // 发送成功即清空进历史:待发附件的引用已随本轮进 messages。
    clearAgentAttachments(nodeId)
    markChanged()
    deliverPromptFrom(nodeId)
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '提示词生成失败,请稍后再试'
    // 技能刚被删除/停用时本地目录已过期:立刻刷新,悬空 chip 当场收回。
    if (e instanceof ApiError && (e.status === 404 || e.code === 'template_disabled')) {
      void refreshCatalogs()
    }
    // 流中途失败:已流出文本保留在节点并落盘(重开画布可见当时进展);
    // 待发附件一并保留,修正后可直接重发。
    if (text !== '') {
      markChanged()
    }
  } finally {
    promptGenerating.value = false
  }
}

// Agent 会话在途的工具过程(63 号票,UI 态不入节点 data):只绑定发起
// 发送的那个节点,时间线在会话结束(含失败)后保留到下次发送。
const agentActiveNodeId = ref('')
const agentToolSteps = ref<AgentToolStep[]>([])

// ===== Agent 工具落图应用器(66 号票,ADR 0006 单一写者) =====

/** 同一轮会话里多个产物的级联偏移(发送开始归零):沿对角线错开,防叠卡。 */
let agentApplySeq = 0

/** Agent 产物的落位锚:优先当前选中节点右侧(产物与工作对象相邻,与
 * dropFrameNode 的落图习惯一致),无选中落视口中心(与工具栏 addNode
 * 同规);级联偏移按会话内已落数叠加。 */
function agentProducedPosition(): { x: number; y: number } {
  const selected = getSelectedNodes.value
  const base =
    selected.length > 0
      ? { x: selected[selected.length - 1].position.x + 380, y: selected[selected.length - 1].position.y }
      : viewportCenterPosition()
  return { x: base.x + agentApplySeq * 48, y: base.y + agentApplySeq * 48 }
}

/** Agent 产物节点落图(addNode 的工具版):整节点草案原子落进本地图状态,
 * 新节点即选中(与 addNode 同);addNodes 触发 'add' 变更事件已 markDirty,
 * autosave 链接管落库。不连边 —— script 不作任何连线端点(64 号票),
 * 絵コンテ落点也没有来源语义。 */
function addAgentProducedNode(type: CanvasNodeType, data: CanvasNodeData): void {
  nodeSeq += 1
  agentApplySeq += 1
  const id = `${type}-${Date.now()}-${nodeSeq}`
  addNodes([{ id, type, position: agentProducedPosition(), data }])
  removeSelectedNodes(getSelectedNodes.value)
  const added = findNode(id)
  if (added) {
    addSelectedNodes([added])
  }
}

/** save_outline / save_script:script 节点草案(64 号票载体,payload =
 * title+text);校验不过抛错,由 applyAgentToolResult 显式上报。 */
function applyAgentScriptDraft(payload: unknown): void {
  const draft = scriptDraftFromPayload(payload)
  const data: ScriptNodeData = { title: draft.title, text: draft.text }
  addAgentProducedNode('script', data)
}

/** save_storyboard:絵コンテ节点草案(cuts 为 53 号票结构 + 67 号票
 * shot/character_ids,画面 asset_id 恒空 → 卡面「未配画面」占位)。 */
function applyAgentStoryboardDraft(payload: unknown): void {
  const draft = storyboardDraftFromPayload(payload)
  const data: StoryboardNodeData = { title: draft.title, cuts: draft.cuts }
  addAgentProducedNode('storyboard', data)
}

// 库表/只读类工具(create_*/list_assets/read_source):后端已直写或只读,
// 前端无可落图之物 —— 注册空应用器。这令 63 号票「有 payload 却无应用器
// = 显式报错」的契约只对真正的版本偏差(客户端缺工具)触发,而不是误伤
// 带回执 payload 的库表类结果。
const AGENT_NOOP_APPLY_TOOLS = ['create_character', 'create_scene', 'create_prop', 'list_assets', 'read_source']
for (const name of AGENT_NOOP_APPLY_TOOLS) {
  setAgentToolApplier(name, () => {})
}
setAgentToolApplier('save_outline', applyAgentScriptDraft)
setAgentToolApplier('save_script', applyAgentScriptDraft)
setAgentToolApplier('save_storyboard', applyAgentStoryboardDraft)
onBeforeUnmount(() => {
  // 编辑器实例卸载即注销应用器:闭包持着旧图状态,残留会写进重挂后的
  // 新编辑器(63 号票 setAgentToolApplier(name, null) 的注销语义)。
  for (const name of [...AGENT_NOOP_APPLY_TOOLS, 'save_outline', 'save_script', 'save_storyboard']) {
    setAgentToolApplier(name, null)
  }
})

/** 技能变更(选中/移除/悬空收回)= 开新会话:写 skill_id 并清空对话
 * 历史与未发送附件,连带清角色 chip 与固化段(52 号票:换技能清历史
 * 重算,注入的角色设定随会话一起作废 —— 固化段属会话状态,同作废,
 * 新会话首轮按最新角色数据重新渲染);文本草稿保留,已投递文本不受影响。 */
function onSkillChange(nodeId: string, skillId: number | null): void {
  updateNodeData(nodeId, {
    ...(skillId != null ? { skill_id: skillId } : { skill_id: undefined }),
    messages: [],
    character_ids: undefined,
    character_section: undefined,
  })
  clearAgentAttachments(nodeId)
  markChanged()
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
    markChanged()
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

// ---- Agent 媒体附件与会话历史面板(32 号票)----

// 待发附件按节点 id 分桶(编辑器持有的 UI 态,不入节点 data/整图 JSON):
// 选中/挑选即上传,发送成功/换技能/新会话清空对应桶。
const agentAttachments = ref<Record<string, PendingAgentMedia[]>>({})

function agentAttachmentsOf(nodeId: string): PendingAgentMedia[] {
  return agentAttachments.value[nodeId] ?? []
}

function setAgentAttachments(nodeId: string, list: PendingAgentMedia[]): void {
  agentAttachments.value = { ...agentAttachments.value, [nodeId]: list }
}

function clearAgentAttachments(nodeId: string): void {
  if (!agentAttachments.value[nodeId]?.length) {
    return
  }
  const next = { ...agentAttachments.value }
  delete next[nodeId]
  agentAttachments.value = next
}

const agentUploading = ref(false)

const AGENT_CAP_MESSAGES: Record<AgentMediaKind, string> = {
  image: '单条消息最多 4 张图片,请先移除或开新会话',
  video: '单条消息最多 1 个视频,请先移除或开新会话',
}

/** Agent 节点的本机附件上传(32 号票,选中即上传):逐个文件入素材库,
 * 上传中/失败的 chip 留在输入区可移除;超限的文件直接报错不传字节(与
 * 服务端「单条 ≤4 图 + ≤1 视频」同款前置纪律)。 */
async function onAgentAttachFiles(nodeId: string, files: File[]): Promise<void> {
  if (agentUploading.value) {
    return
  }
  agentUploading.value = true
  try {
    for (const file of files) {
      const kind = uploadKindOf(file)
      const list = agentAttachmentsOf(nodeId)
      if (!hasAgentMediaSlot(list, kind)) {
        generateError.value = AGENT_CAP_MESSAGES[kind]
        continue
      }
      const pending: PendingAgentMedia = { id: nextAgentMediaId(), kind, state: 'uploading' }
      setAgentAttachments(nodeId, [...agentAttachmentsOf(nodeId), pending])
      try {
        const a = await client.uploadAsset(file, kind, uploadIPId.value)
        setAgentAttachments(
          nodeId,
          agentAttachmentsOf(nodeId).map((m) =>
            m.id === pending.id ? { ...m, state: 'ready', ref: a.content_url, assetId: a.id } : m,
          ),
        )
      } catch (e) {
        setAgentAttachments(
          nodeId,
          agentAttachmentsOf(nodeId).map((m) =>
            m.id === pending.id
              ? { ...m, state: 'failed', error: e instanceof ApiError ? e.message : '上传失败,请稍后再试' }
              : m,
          ),
        )
      }
    }
  } finally {
    agentUploading.value = false
  }
}

/** 素材库点选(32 号票):选中即挂待发附件(ready,内容寻址引用,与上
 * 传两路引用形状一致),再点取消;超限拒收与上传入口同一套纪律。 */
function onAgentPickToggle(nodeId: string, a: AssetRecord): void {
  if (a.kind !== 'image' && a.kind !== 'video') {
    return
  }
  const list = agentAttachmentsOf(nodeId)
  const existing = list.find((m) => m.assetId === a.id)
  if (existing) {
    setAgentAttachments(nodeId, list.filter((m) => m.id !== existing.id))
    return
  }
  if (!hasAgentMediaSlot(list, a.kind)) {
    generateError.value = AGENT_CAP_MESSAGES[a.kind]
    return
  }
  setAgentAttachments(nodeId, [
    ...list,
    { id: nextAgentMediaId(), kind: a.kind, state: 'ready', ref: a.content_url, assetId: a.id },
  ])
}

function onAgentDetach(nodeId: string, id: number): void {
  setAgentAttachments(nodeId, agentAttachmentsOf(nodeId).filter((m) => m.id !== id))
}

/** 新会话(32 号票显式入口):清空对话历史、未发送附件、角色 chip 与
 * 固化段(52 号票:新会话不背旧会话的注入配置,固化段同作废),技能
 * 不变 —— 此前唯一清历史途径是换技能,太隐蔽。 */
function onAgentNewSession(nodeId: string): void {
  updateNodeData(nodeId, { messages: [], character_ids: undefined, character_section: undefined })
  clearAgentAttachments(nodeId)
  markChanged()
}

/** Agent 角色 chip 变更(52 号票):选择序落节点 data,换技能/新会话由
 * 上面两个入口连带清空。chip 变更即注入配置变更,固化段一并作废 ——
 * 下一轮按当前选择重新取最新角色数据渲染(已发送轮次的文本不受影响,
 * 历史固化只保护已发生的对话)。 */
function onAgentCharactersChange(nodeId: string, ids: number[]): void {
  updateNodeData(nodeId, {
    character_ids: ids.length > 0 ? ids : undefined,
    character_section: undefined,
  })
  markChanged()
}

// 会话历史 / 素材选择面板的节点绑定:点哪个 Agent 节点开谁的;与素材库、
// 生成记录面板同占画布右缘,四方互斥。
const historyNodeId = ref('')
const pickerNodeId = ref('')

const historyNode = computed(() => (historyNodeId.value ? findNode(historyNodeId.value) ?? null : null))
const pickerNode = computed(() => (pickerNodeId.value ? findNode(pickerNodeId.value) ?? null : null))

const historyMessages = computed<AgentChatMessage[]>(() => {
  const data = historyNode.value?.data as AgentNodeData | undefined
  return data?.messages ?? []
})

const pickerSelectedIds = computed<number[]>(() =>
  agentAttachmentsOf(pickerNodeId.value)
    .map((m) => m.assetId)
    .filter((id): id is number => id != null),
)

function openAgentHistory(nodeId: string): void {
  if (historyNodeId.value === nodeId) {
    historyNodeId.value = ''
    return
  }
  historyNodeId.value = nodeId
  pickerNodeId.value = ''
  assetPanelOpen.value = false
  recordsPanelOpen.value = false
  composerPickerOpen.value = false
  levelPickerNodeId.value = ''
}

function openAgentPicker(nodeId: string): void {
  if (pickerNodeId.value === nodeId) {
    pickerNodeId.value = ''
    return
  }
  pickerNodeId.value = nodeId
  historyNodeId.value = ''
  assetPanelOpen.value = false
  recordsPanelOpen.value = false
  composerPickerOpen.value = false
  levelPickerNodeId.value = ''
}

/** 清空会话(历史面板顶部按钮):与新会话同一语义,清后节点可重新开始。 */
function clearAgentHistory(nodeId: string): void {
  onAgentNewSession(nodeId)
}

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
    markChanged()
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
  markChanged()
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

// ---- 预演台(39 号票)----

// 渲染帧上传在途的节点 id('' = 无):渲染本身是本地 WebGL 零计费,耗时
// 在上传素材库这一步,节点按钮随此标记禁用。
const previzRendering = ref('')

// 40 号票:全屏预演台编辑层绑定的节点('' = 关闭)。编辑层不卸载画布,
// 场景改动仍走 onPrevizSceneChange 同一条落盘链;节点被删时 computed
// 就地关层(与 Agent 历史面板同款兜底)。
const previzEditorNodeId = ref('')
const previzEditorNode = computed(() => {
  const node = previzEditorNodeId.value ? findNode(previzEditorNodeId.value) : undefined
  return node ? { id: node.id, data: node.data as PrevizNodeData } : null
})

/** 预演台场景数据落盘:updateNodeData 浅合并(objects/cameras/
 * active_camera_id 整包替换),自动保存免费获得(27 号票「数据随身」)。 */
function onPrevizSceneChange(
  nodeId: string,
  patch: { objects: PrevizNodeData['objects']; cameras: PrevizNodeData['cameras']; active_camera_id?: string },
): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

/** 渲染帧入库(39 号票 Q6):WebGL 画布 toBlob 出的 PNG 经
 * /api/assets/upload 进素材库(服务端魔数裁决,键落 uploads/ 档,与用户
 * 上传同义),成功后把素材引用写回节点 latest —— 媒体区/灯箱/落画布都
 * 吃这个引用;后端零改动。 */
async function onPrevizRender(nodeId: string, blob: Blob): Promise<void> {
  if (previzRendering.value !== '') {
    return
  }
  previzRendering.value = nodeId
  generateError.value = ''
  try {
    const file = new File([blob], 'previz.png', { type: 'image/png' })
    const a = await client.uploadAsset(file, 'image', uploadIPId.value)
    updateNodeData(nodeId, { latest: { url: a.content_url, asset_id: a.id } })
    markChanged()
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '渲染帧上传失败,请稍后再试'
  } finally {
    previzRendering.value = ''
  }
}

/** 落画布(39 号票 Q7):最新渲染帧落为新图片节点并连线 —— 复用产物落位
 * +连线语义,可多次,每次渲染的帧各自成节点;图片节点持有 uploads/ 档
 * 素材引用,喂生成链(对话框参考条/首帧)全链照旧。 */
function onPrevizDrop(nodeId: string): void {
  const node = findNode(nodeId)
  const data = node?.data as PrevizNodeData | undefined
  if (!node || !data?.latest?.url) {
    return
  }
  nodeSeq += 1
  const newId = `image-${Date.now()}-${nodeSeq}`
  removeSelectedNodes(getSelectedNodes.value)
  addNodes([
    {
      id: newId,
      type: 'image',
      position: { x: node.position.x + 380, y: node.position.y },
      data: { url: data.latest.url, asset_id: data.latest.asset_id, note: '' } satisfies MediaNodeData,
    },
  ])
  const added = findNode(newId)
  if (added) {
    addSelectedNodes([added])
  }
  connectNodes(nodeId, newId)
  // addNodes/连线各自触发变更事件,那边已 markDirty,这里无需重复。
}

// ---- 絵コンテ(53 号票)----

// 53 号票:全屏分镜编辑层绑定的节点('' = 关闭)。与预演台编辑层同族:
// 不卸载画布,cut 改动走 onStoryboardSceneChange 同一条落盘链;节点被删
// 时 computed 就地关层。
const storyboardEditorNodeId = ref('')
const storyboardEditorNode = computed(() => {
  const node = storyboardEditorNodeId.value ? findNode(storyboardEditorNodeId.value) : undefined
  return node ? { id: node.id, data: node.data as StoryboardNodeData } : null
})

// 画面槽本机上传在途(编辑器级全局,任一在途即禁用各槽上传按钮)。
const storyboardUploading = ref(false)

/** cut/板名数据落盘:updateNodeData 浅合并(cuts 整包替换,数组换新由
 * 编辑层纯函数保证),自动保存免费获得(整图 JSON 随节点 data 先例)。 */
function onStoryboardSceneChange(
  nodeId: string,
  patch: { title: string; cuts: StoryboardCut[] },
): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

/** 板名编辑(板卡输入框逐键上抛):与编辑层同一条落盘链。 */
function onStoryboardTitleChange(nodeId: string, title: string): void {
  updateNodeData(nodeId, { title })
  markChanged()
}

/** 画面槽本机上传的共享骨架(絵コンテ/ネーム同构,评审合并):在途守卫 →
 * /api/assets/upload 入素材库(挂画布 IP,键落 uploads/ 档)→ apply 以节点
 * data 现值为准生成补丁回填(编辑层在途改动不丢)→ 失败统一报错;节点已
 * 被删(编辑层开着时删节点的窄窗)静默丢弃,素材行删除后槽落「素材不可用」
 * 占位(既有契约)。关卡卡不走这里:满员预检与回填拒收是另一套守卫语义,
 * 硬套要改行为,维持独立实现。 */
async function uploadFrameAsset(
  inFlight: Ref<boolean>,
  nodeId: string,
  file: File,
  apply: (data: unknown, assetId: number) => Record<string, unknown>,
): Promise<void> {
  if (inFlight.value) {
    return
  }
  inFlight.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, 'image', uploadIPId.value)
    const data = findNode(nodeId)?.data
    if (data) {
      updateNodeData(nodeId, apply(data, a.id))
      markChanged()
    }
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    inFlight.value = false
  }
}

/** 落图节点的共享骨架(53/54 号票同构,评审合并):槽画面落为右侧新图片
 * 节点(asset 引用 + 内容寻址,prompt 空)并连线(来源边);多次落图沿
 * 出边数纵向级联偏移防叠卡;无画面的槽在入口已禁用,这里再守一道。 */
function dropFrameNode(nodeId: string, assetIdOf: (data: unknown) => number): void {
  const node = findNode(nodeId)
  const assetId = node ? assetIdOf(node.data) : 0
  if (!node || assetId <= 0) {
    return
  }
  nodeSeq += 1
  const newId = `image-${Date.now()}-${nodeSeq}`
  const lane = flowEdges.value.filter((e) => e.source === nodeId).length
  removeSelectedNodes(getSelectedNodes.value)
  addNodes([
    {
      id: newId,
      type: 'image',
      position: { x: node.position.x + 380, y: node.position.y + lane * 120 },
      data: { url: frameContentURL(assetId), asset_id: assetId, note: '' } satisfies MediaNodeData,
    },
  ])
  const added = findNode(newId)
  if (added) {
    addSelectedNodes([added])
  }
  connectNodes(nodeId, newId)
  // addNodes/连线各自触发变更事件,那边已 markDirty,这里无需重复。
}

/** 画面槽本机上传:经 uploadFrameAsset 入库,成功后按素材 id 回填本 cut
 * —— 与预演台渲染帧同语义。 */
function onStoryboardUpload(nodeId: string, cutIndex: number, file: File): Promise<void> {
  return uploadFrameAsset(storyboardUploading, nodeId, file, (raw, assetId) => {
    const data = raw as StoryboardNodeData
    return { cuts: data.cuts.map((c, i) => (i === cutIndex ? { ...c, asset_id: assetId } : c)) }
  })
}

/** 落图节点(53 号票,预演台落画布同构):cut 画面落为图片节点并连线
 * (storyboard → 图片来源边);机制在上方 dropFrameNode。 */
function onStoryboardDrop(nodeId: string, cutIndex: number): void {
  dropFrameNode(nodeId, (raw) => (raw as StoryboardNodeData).cuts[cutIndex]?.asset_id ?? 0)
}

/** 编辑层 upload 事件的落点(双参数事件经命名 handler 中转,避开模板
 * 闭包里的可空收窄):节点已被删(编辑层开着时删节点的窄窗)就静默丢弃。 */
function onStoryboardLayerUpload(cutIndex: number, file: File): void {
  const editor = storyboardEditorNode.value
  if (editor) {
    void onStoryboardUpload(editor.id, cutIndex, file)
  }
}

/** cut→视频直通(67 号票,53 号票遗留收编):落空视频节点 + 絵コンテ→
 * 视频来源边(dropFrameNode 同款骨架:沿出边数级联偏移防叠卡),随后
 * 预填视频草稿并关闭编辑层让位对话框 —— cut 画面(非空)自动进首帧、
 * 提示词按 画面描述/景别/运镜/台词 拼装(storyboard.buildCutVideoPrompt)、时长
 * 预填 cut 秒数(未定为 0/空 = 留「自动」)。人工确认后才发送(花钱操作
 * 人工确认,总案 Q5):这里只落节点与预填,不发任务;无画面 cut 也可发起
 * (纯文生视频,预填不动会话已上传的参考条,对话框里可见可删)。 */
function onStoryboardGenerateVideo(nodeId: string, cutIndex: number): void {
  const node = findNode(nodeId)
  const cut = (node?.data as StoryboardNodeData | undefined)?.cuts[cutIndex]
  if (!node || !cut) {
    return
  }
  nodeSeq += 1
  const newId = `video-${Date.now()}-${nodeSeq}`
  const lane = flowEdges.value.filter((e) => e.source === nodeId).length
  removeSelectedNodes(getSelectedNodes.value)
  addNodes([
    {
      id: newId,
      type: 'video',
      position: { x: node.position.x + 380, y: node.position.y + lane * 120 },
      data: { url: '', note: '' } satisfies MediaNodeData,
    },
  ])
  const added = findNode(newId)
  if (added) {
    addSelectedNodes([added])
  }
  connectNodes(nodeId, newId)
  // 预填 = 写视频草稿(composer 是「选中驱动 + 草稿 refs」模型,新节点无
  // 产物、选中 chip 不产生):有画面把 cut 画面物化进参考条作首帧 —— 整条
  // 替换,本动作的预填语义就是「这个 cut 的画面即首帧」,不带其他会话
  // 残留;无画面清空参考条,纯文生视频。选中切换的 watch 会把拆下标记与
  // 模式(锁视频)一并复位,这里只写草稿。
  videoPrompt.value = buildCutVideoPrompt(cut)
  videoDuration.value = cut.seconds ? String(cut.seconds) : ''
  uploadedVideoRefs.value =
    cut.asset_id > 0
      ? [
          {
            url: frameContentURL(cut.asset_id),
            kind: 'image' as const,
            role: 'first_frame' as const,
            asset_id: cut.asset_id,
          },
        ]
      : []
  // 收编辑层:生成对话框贴新视频节点正下方,全屏编辑层开着会挡住它。
  storyboardEditorNodeId.value = ''
}

// ---- ネーム(54 号票)----

// 54 号票:全屏ネーム编辑层绑定的节点('' = 关闭)。与絵コンテ编辑层
// 同族:不卸载画布,页/格改动走 onNameSceneChange 同一条落盘链;节点被
// 删时 computed 就地关层。
const nameEditorNodeId = ref('')
const nameEditorNode = computed(() => {
  const node = nameEditorNodeId.value ? findNode(nameEditorNodeId.value) : undefined
  return node ? { id: node.id, data: node.data as NameNodeData } : null
})

// 画面槽本机上传在途(编辑器级全局,任一在途即禁用各槽上传按钮)。
const nameUploading = ref(false)

/** 页/格/板名数据落盘:updateNodeData 浅合并(pages 整包替换,数组换新由
 * 编辑层纯函数保证),自动保存免费获得(整图 JSON 随节点 data 先例)。 */
function onNameSceneChange(
  nodeId: string,
  patch: { title: string; pages: NamePage[] },
): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

/** 板名编辑(板卡输入框逐键上抛):与编辑层同一条落盘链。 */
function onNameTitleChange(nodeId: string, title: string): void {
  updateNodeData(nodeId, { title })
  markChanged()
}

/** 画面槽本机上传:经 uploadFrameAsset 入库,成功后按素材 id 回填本格
 * —— 与絵コンテ上传同语义。 */
function onNameUpload(
  nodeId: string,
  pageIndex: number,
  panelIndex: number,
  file: File,
): Promise<void> {
  return uploadFrameAsset(nameUploading, nodeId, file, (raw, assetId) => {
    const data = raw as NameNodeData
    return {
      pages: data.pages.map((p, i) =>
        i === pageIndex
          ? {
              ...p,
              panels: p.panels.map((n, j) => (j === panelIndex ? { ...n, asset_id: assetId } : n)),
            }
          : p,
      ),
    }
  })
}

/** 落图节点(54 号票,絵コンテ落图同构):格画面落为图片节点并连线
 * (name → 图片来源边,storyboard 同族);机制在上方 dropFrameNode。 */
function onNameDrop(nodeId: string, pageIndex: number, panelIndex: number): void {
  dropFrameNode(
    nodeId,
    (raw) => (raw as NameNodeData).pages[pageIndex]?.panels[panelIndex]?.asset_id ?? 0,
  )
}

/** 编辑层 upload/drop 事件的落点(多参数事件经命名 handler 中转,避开
 * 模板闭包里的可空收窄):节点已被删(编辑层开着时删节点的窄窗)就静默
 * 丢弃。 */
function onNameLayerUpload(pageIndex: number, panelIndex: number, file: File): void {
  const editor = nameEditorNode.value
  if (editor) {
    void onNameUpload(editor.id, pageIndex, panelIndex, file)
  }
}

function onNameLayerDrop(pageIndex: number, panelIndex: number): void {
  const editor = nameEditorNode.value
  if (editor) {
    onNameDrop(editor.id, pageIndex, panelIndex)
  }
}

// ---- 剧本(64 号票)----

// 全屏剧本编辑层绑定的节点('' = 关闭)。与絵コンテ/ネーム编辑层同族:
// 不卸载画布,标题/正文整包走 onScriptSceneChange 同一条落盘链;节点被删
// 时 computed 就地关层。
const scriptEditorNodeId = ref('')
const scriptEditorNode = computed(() => {
  const node = scriptEditorNodeId.value ? findNode(scriptEditorNodeId.value) : undefined
  return node ? { id: node.id, data: node.data as ScriptNodeData } : null
})

/** 剧本标题/正文数据落盘:updateNodeData 浅合并(编辑层整包上抛),自动
 * 保存免费获得(整图 JSON 随节点 data 先例;30 万字大文本 autosave 全量
 * 提交是 64 号票已接受的代价,不做优化)。 */
function onScriptSceneChange(nodeId: string, patch: { title: string; text: string }): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

// ---- 关卡卡(55 号票)----

// 55 号票:关卡卡**卡内直接编辑不开编辑层**(53 号票定案),字段补丁由
// 卡面逐键/逐动作上抛,这里走同一条 updateNodeData + markChanged 落盘链
// (整图 JSON 随节点 data,自动保存免费获得)。
function onLevelChange(nodeId: string, patch: Partial<LevelNodeData>): void {
  updateNodeData(nodeId, patch)
  markChanged()
}

// 画面槽本机上传在途(编辑器级全局,任一在途即禁用各卡上传入口)。
const levelUploading = ref(false)

// 画面槽素材挑选面板绑定的节点 id('' = 关闭):与素材库、生成记录、
// 对话框挑选、Agent 选择等右缘面板互斥(32 号票纪律)。
const levelPickerNodeId = ref('')
const levelPickerNode = computed(() =>
  levelPickerNodeId.value ? findNode(levelPickerNodeId.value) ?? null : null,
)

const levelPickerSelectedIds = computed<number[]>(
  () => (levelPickerNode.value?.data as LevelNodeData | undefined)?.asset_ids ?? [],
)

function openLevelPicker(nodeId: string): void {
  if (levelPickerNodeId.value === nodeId) {
    levelPickerNodeId.value = ''
    return
  }
  levelPickerNodeId.value = nodeId
  assetPanelOpen.value = false
  recordsPanelOpen.value = false
  composerPickerOpen.value = false
  historyNodeId.value = ''
  pickerNodeId.value = ''
}

/** 画面槽挑选点选:已在槽中 = 摘下;不在 = 追加,满员整条拒收并提示
 * (9 上限守卫,与参考图口径一致)。 */
function onLevelPickToggle(nodeId: string, a: AssetRecord): void {
  if (a.kind !== 'image') {
    return
  }
  const data = findNode(nodeId)?.data as LevelNodeData | undefined
  if (!data) {
    return
  }
  const next = toggleLevelAsset(data.asset_ids, a.id)
  if (next === data.asset_ids && !data.asset_ids.includes(a.id)) {
    generateError.value = '画面槽已满(最多 9 张),请先移除一张'
    return
  }
  updateNodeData(nodeId, { asset_ids: next })
  markChanged()
}

/** 画面槽本机上传:上传前按当前槽位预检(满员直接报错不传字节,免得
 * 上传成素材却在槽上拒收 —— 42 号票前置纪律);入库挂画布 IP(键落
 * uploads/ 档),成功后按素材 id 回填画面槽,回填以节点 data 现值为准
 * (上传在途的卡内改动不丢),素材行删除后槽落「素材不可用」占位。 */
async function onLevelUpload(nodeId: string, file: File): Promise<void> {
  if (levelUploading.value) {
    return
  }
  const data = findNode(nodeId)?.data as LevelNodeData | undefined
  if (!data || !canAddLevelAsset(data.asset_ids)) {
    generateError.value = '画面槽已满(最多 9 张),请先移除一张'
    return
  }
  levelUploading.value = true
  generateError.value = ''
  try {
    const a = await client.uploadAsset(file, 'image', uploadIPId.value)
    const fresh = findNode(nodeId)?.data as LevelNodeData | undefined
    if (fresh) {
      const next = toggleLevelAsset(fresh.asset_ids, a.id)
      if (next !== fresh.asset_ids) {
        updateNodeData(nodeId, { asset_ids: next })
        markChanged()
      } else {
        generateError.value = '画面槽已满(最多 9 张),请先移除一张'
      }
    }
  } catch (e) {
    generateError.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    levelUploading.value = false
  }
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
  // 非法目标点击的可见反馈(38 号票评审:替换静默取消)。
  onReject: ({ sourceType, targetType }) => {
    const label = (t?: string) => NODE_TYPE_LABEL[normalizeNodeType(t)] ?? t ?? '未知节点'
    showNotice(
      `「${label(sourceType)}」→「${label(targetType)}」不可连线(合法:Agent→媒体、视频→Agent、媒体→分析、媒体→媒体、预演台→图片、分镜→图片、分镜稿→图片)`,
    )
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

/** 窗口级快捷键(38 号票评审扩展):Esc 逐层退出(灯箱开着时让位给它
 * 自己的 Esc,33 号票);F 适配视图;Cmd/Ctrl+Z 撤销、Shift+Z 或 Y 重做。
 * 输入控件内(输入框/文本域/下拉/可编辑区)让位给浏览器原生行为 ——
 * 文本编辑的撤销归输入框自己。 */
function isTypingTarget(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null
  if (!el) {
    return false
  }
  return (
    el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable
  )
}

function onGlobalKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    if (lightbox.value) {
      return // 灯箱自己处理 Esc(33 号票),这里不叠加动作。
    }
    if (assetPanelOpen.value) {
      assetPanelOpen.value = false
      return
    }
    if (recordsPanelOpen.value) {
      recordsPanelOpen.value = false
      return
    }
    if (composerPickerOpen.value) {
      composerPickerOpen.value = false
      return
    }
    if (pickerNodeId.value) {
      pickerNodeId.value = ''
      return
    }
    if (levelPickerNodeId.value) {
      levelPickerNodeId.value = ''
      return
    }
    if (historyNodeId.value) {
      historyNodeId.value = ''
      return
    }
    if (topbarRef.value?.cancelRenaming()) {
      return
    }
    if (connection.pending.value) {
      connection.cancel()
      return
    }
    const selected = getSelectedNodes.value
    if (selected.length > 0) {
      removeSelectedNodes(selected)
    }
    return
  }
  const mod = e.metaKey || e.ctrlKey
  if (mod && (e.key === 'z' || e.key === 'Z')) {
    if (isTypingTarget(e)) {
      return
    }
    e.preventDefault()
    if (e.shiftKey) {
      redoGraph()
    } else {
      undoGraph()
    }
    return
  }
  if (mod && (e.key === 'y' || e.key === 'Y')) {
    if (isTypingTarget(e)) {
      return
    }
    e.preventDefault()
    redoGraph()
    return
  }
  if (isTypingTarget(e) || mod || e.altKey) {
    return
  }
  if (e.key === 'f' || e.key === 'F') {
    void fitView({ padding: 0.2, maxZoom: 1.2, duration: 250 })
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

// 尺寸/选中态变更不改变持久化文档,不触发保存。删除给出可撤销提示
// (38 号票评审:误删零恢复的焦虑由撤销栈兜底,提示把这条路告诉用户)。
onNodesChange((changes) => {
  if (changes.some((c) => c.type === 'add' || c.type === 'remove' || c.type === 'position')) {
    if (changes.some((c) => c.type === 'remove')) {
      showNotice('已删除节点,⌘Z / Ctrl+Z 可撤销')
    }
    markChanged()
  }
})
onEdgesChange((changes) => {
  if (changes.some((c) => c.type === 'add' || c.type === 'remove')) {
    markChanged()
  }
})
onConnect((params: Connection) => {
  addEdges([{ ...params }])
})

// 节点内容编辑(Agent 文本草稿)不产生变更事件:由节点上抛,这里落到
// flow 状态并标记脏。
function onTextChange(nodeId: string, text: string): void {
  updateNodeData(nodeId, { text })
  markChanged()
}

let nodeSeq = 0

/** 无锚点落位(33 号票):新节点落当前视口中心。锚点按容器 client 矩形
 * 计算后经 screenToFlowCoordinate 换算成图坐标 —— 画布平移缩放到哪里,
 * 新节点都出现在眼前的同一位置;级联偏移在 newNodeAnchor 里叠加。
 * 容器矩形取不到(理论只在未挂载时)退回旧图坐标基点。 */
function viewportCenterPosition(): { x: number; y: number } {
  const count = toObject().nodes.length
  const rect = vueFlowRef.value?.getBoundingClientRect()
  if (!rect) {
    return { x: 140 + (count % 8) * 48, y: 120 + (count % 8) * 48 }
  }
  return screenToFlowCoordinate(newNodeAnchor(rect, count))
}

function addNode(type: CanvasNodeType, dataPatch?: Partial<AudioNodeData>): void {
  nodeSeq += 1
  const id = `${type}-${Date.now()}-${nodeSeq}`
  addNodes([
    {
      id,
      type,
      position: viewportCenterPosition(),
      // 61 号票:入口可携带初始创作意图(音效入口带 source:'sfx'),
      // 浅合并进 initialData —— 其余入口不传,行为不变。
      data: { ...initialData(type), ...dataPatch } as CanvasNodeData,
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

/** 小地图节点配色:与节点描边色同源(Agent 蓝/图片绿/视频黄/分析紫/
 * 预演台粉/角色青/絵コンテ橙/ネーム天蓝/关卡红/剧本金),38 号票评审补
 * 的导航面沿用同一语义色系。 */
function miniMapNodeColor(node: { type?: string | null }): string {
  switch (normalizeNodeType(node.type ?? undefined)) {
    case 'agent':
      return '#7aa2f7'
    case 'image':
      return '#4ade80'
    case 'video':
      return '#facc15'
    case 'previz':
      return '#f472b6'
    case 'character':
      return '#2dd4bf'
    case 'storyboard':
      return '#fb923c'
    case 'name':
      return '#38bdf8'
    case 'level':
      return '#f87171'
    case 'voice':
      return '#5eead4'
    case 'audio':
      return '#a78bfa'
    case 'script':
      return '#d3b571'
    default:
      return '#a78bfa'
  }
}

// ---- 媒体预览灯箱(33 号票)----

/** 当前灯箱内容(null = 关闭);编辑器级单实例,媒体节点点击媒体区置位。
 * 触发侧组件已保证只在产物成功落位时上抛,这里只兜一层空 url。nodeId =
 * 触发预览的源节点(41 号票截帧落画布要用;图片灯箱无截帧入口,字段空置)。 */
const lightbox = ref<{ kind: 'image' | 'video'; url: string; nodeId: string } | null>(null)

function openPreview(kind: 'image' | 'video', url: string, nodeId: string): void {
  if (url) {
    lightbox.value = { kind, url, nodeId }
  }
}

/** 灯箱截帧落画布(41 号票):当前帧 JPEG 经 /api/assets/upload 入素材库
 * (服务端魔数裁决,键落 uploads/ 档,与用户上传/预演台渲染帧同义)→ 源
 * 视频节点右侧落图片节点 + 媒体→媒体来源边(帧提取复用迭代来源边,不新增
 * 边类型);连截按源节点已有出边数纵向级联偏移防叠卡,每帧独立上传独立
 * 成节点;addNodes/连线各自触发变更事件,那边已 markDirty。 */
async function onLightboxCapture(blob: Blob, seconds: number): Promise<void> {
  const sourceId = lightbox.value?.nodeId ?? ''
  const source = sourceId ? findNode(sourceId) : undefined
  if (!source) {
    showNotice('源节点已不存在,截帧未落画布')
    return
  }
  try {
    const file = new File([blob], `frame-${seconds.toFixed(1)}s.jpg`, { type: 'image/jpeg' })
    const a = await client.uploadAsset(file, 'image', uploadIPId.value)
    nodeSeq += 1
    const newId = `image-${Date.now()}-${nodeSeq}`
    const lane = flowEdges.value.filter((e) => e.source === sourceId).length
    removeSelectedNodes(getSelectedNodes.value)
    addNodes([
      {
        id: newId,
        type: 'image',
        position: { x: source.position.x + 380, y: source.position.y + lane * 120 },
        data: { url: a.content_url, asset_id: a.id, note: '' } satisfies MediaNodeData,
      },
    ])
    const added = findNode(newId)
    if (added) {
      addSelectedNodes([added])
    }
    // 上传在途时源节点可能被删(关灯箱后删除的窄窗):帧照落(上传已花),
    // 连线只在源仍存在时落 —— connectNodes 不校验存在性,悬空边会随整图
    // JSON 持久化,加载侧 setEdges 原样灌入也不清洗。
    const sourceAlive = !!findNode(sourceId)
    if (sourceAlive) {
      connectNodes(sourceId, newId)
    }
    showNotice(
      sourceAlive
        ? `已截帧 ${seconds.toFixed(1)}s 并落为图片节点`
        : `已截帧 ${seconds.toFixed(1)}s 落为图片节点(源节点已被删除,未连线)`,
    )
  } catch (e) {
    showNotice(e instanceof ApiError ? e.message : '截帧上传失败,请稍后再试')
  }
}

// ---- 素材库面板(14 号票)与生成记录面板(28 号票)----

const assetPanelOpen = ref(false)
const recordsPanelOpen = ref(false)

// 面板同占画布右缘,互斥打开(32 号票起含 Agent 的历史/素材选择面板与
// 47 号票的对话框素材挑选面板)。
function toggleAssetPanel(): void {
  assetPanelOpen.value = !assetPanelOpen.value
  if (assetPanelOpen.value) {
    recordsPanelOpen.value = false
    composerPickerOpen.value = false
    historyNodeId.value = ''
    pickerNodeId.value = ''
    levelPickerNodeId.value = ''
  }
}

function toggleRecordsPanel(): void {
  recordsPanelOpen.value = !recordsPanelOpen.value
  if (recordsPanelOpen.value) {
    assetPanelOpen.value = false
    composerPickerOpen.value = false
    historyNodeId.value = ''
    pickerNodeId.value = ''
    levelPickerNodeId.value = ''
  }
}

/** 素材引用落画布的公共落点(素材面板插入、生成记录插入共用,28 号票):
 * 节点持有素材的内容寻址引用(asset_id + content_url),不复制字节,跨画布
 * 复用同一素材。 */
function insertAssetRef(kind: 'image' | 'video', assetId: number, contentUrl: string): void {
  nodeSeq += 1
  const type: CanvasNodeType = kind === 'video' ? 'video' : 'image'
  addNodes([
    {
      id: `${type}-${Date.now()}-${nodeSeq}`,
      type,
      position: viewportCenterPosition(),
      data: { url: contentUrl, asset_id: assetId, note: '' } satisfies MediaNodeData,
    },
  ])
}

/** 音频素材的插入落点(57 号票):音频节点持 asset_id,内容寻址路径由
 * 展示层按 id 拼装;24 号票「音频不落画布」随音频节点翻案。60 号票起
 * 落 source='insert'(来源判别:素材插入)。 */
function insertAudioAsset(assetId: number): void {
  nodeSeq += 1
  addNodes([
    {
      id: `audio-${Date.now()}-${nodeSeq}`,
      type: 'audio',
      position: viewportCenterPosition(),
      data: { asset_id: assetId, source: 'insert' } satisfies AudioNodeData,
    },
  ])
}

/** 素材插入:从素材库把历史产物放进当前画布。音频落音频节点(57 号票
 * 翻案),图片/视频落媒体节点。 */
function insertAsset(a: AssetRecord): void {
  if (a.kind === 'audio') {
    insertAudioAsset(a.id)
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
  if (task.kind === 'tts') {
    // 台词合成产物落音频节点(57 号票;素材行是产物本体,asset_id 引用)。
    insertAudioAsset(task.asset_id)
    return
  }
  insertAssetRef(task.kind === 'video' ? 'video' : 'image', task.asset_id, taskUrlFor(task))
}

// ---- 本机素材上传(18 号票)----

const uploading = ref(false)

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
    const a = await client.uploadAsset(file, uploadKindOf(file), uploadIPId.value)
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
  canvasIPID.value = detail.ip_id
  canvasIPName.value = detail.ip_name
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
    markChanged()
  }
  // 撤销基线 = 服务器图落位后的这一帧(含上面的就地迁移),redo 清空。
  resetHistory()
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
    markChanged()
  } catch {
    // 同上:保持 conflict 态。
  } finally {
    resolvingConflict.value = false
  }
}

// ---- 画布改名(编辑器内)----

async function commitRename(draft: string): Promise<void> {
  const name = draft.trim()
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
  // applyServerGraph 的 setNodes 整个覆盖掉。角色目录同理 —— ip_id 要
  // 等画布详情回来才有。
  void loadCanvas().finally(() => {
    void taskSync.start()
    void refreshIPCharacters()
    void refreshVoices()
  })
  refreshModelCatalogs()
  void refreshCatalogs()
  window.addEventListener('focus', refreshCatalogs)
  window.addEventListener('focus', refreshIPCharacters)
  window.addEventListener('focus', refreshVoices)
  window.addEventListener('beforeunload', beforeUnload)
  window.addEventListener('keydown', onGlobalKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('focus', refreshCatalogs)
  window.removeEventListener('focus', refreshIPCharacters)
  window.removeEventListener('focus', refreshVoices)
  window.removeEventListener('keydown', onGlobalKeydown)
  if (historyTimer !== undefined) {
    clearTimeout(historyTimer)
  }
  taskSync.stop()
})

/** 工具栏添加按钮的两组(53 号票,ADR 0005 组织下沉):「创作」= 媒介
 * 生产节点(analysis 仍仅由动作创建,不经面板);「规划」= 规划工件
 * 节点(絵コンテ + ネーム + 关卡,54/55 号票)。纯展示分组,无语义
 * 过滤 —— 全节点全画布可用(53 号票 A 段)。 */
function backToList(): void {
  // 面包屑的 IP 段是主出口;ip_id=0(防御态)兜底回 IP 层。
  if (canvasIPID.value > 0) {
    void router.push({ name: 'ip-detail', params: { id: canvasIPID.value } })
    return
  }
  void router.push({ name: 'ips' })
}
</script>

<template>
  <div class="editor">
    <CanvasTopbar
      ref="topbarRef"
      :ip-id="canvasIPID"
      :ip-name="canvasIPName"
      :canvas-name="canvasName"
      :save-state="autosave.state.value"
      :task-active-count="taskSync.activeCount.value"
      @back="backToList"
      @rename="commitRename"
    />

    <EditorAlerts
      :generate-error="generateError"
      :load-error="loadError"
      :missing="missing"
      :autosave-state="autosave.state.value"
      :resolving-conflict="resolvingConflict"
      @dismiss-generate="generateError = ''"
      @reload-server="reloadServerVersion"
      @overwrite-server="overwriteServer"
      @retry="loadCanvas"
      @back="backToList"
    />

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
        <!-- 缩放/回正控件与小地图(38 号票评审:画布变大后回全景不再只能
             靠刷新或记录面板定位)。深色主题在全局 style.css 覆写。 -->
        <Controls position="bottom-right" />
        <MiniMap
          position="bottom-left"
          pannable
          zoomable
          :node-color="miniMapNodeColor"
        />
        <template #node-agent="nodeProps">
          <AgentNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :skills="promptTemplates"
            :chat-models="promptModels"
            :characters="ipCharacters"
            :prompt-generating="promptGenerating"
            :has-downstream="downstreamHasMedia.has(nodeProps.id)"
            :attachments="agentAttachments[nodeProps.id] ?? []"
            :tool-activity="agentActiveNodeId === nodeProps.id ? agentToolSteps : undefined"
            :history-open="historyNodeId === nodeProps.id"
            :picker-open="pickerNodeId === nodeProps.id"
            :connect-state="connection.stateOf(nodeProps.id)"
            @text-change="onTextChange(nodeProps.id, $event)"
            @send="onGeneratePrompt(nodeProps.id, $event)"
            @skill-change="onSkillChange(nodeProps.id, $event)"
            @characters-change="onAgentCharactersChange(nodeProps.id, $event)"
            @deliver="onDeliver(nodeProps.id)"
            @attach-files="onAgentAttachFiles(nodeProps.id, $event)"
            @detach-attachment="onAgentDetach(nodeProps.id, $event)"
            @open-picker="openAgentPicker(nodeProps.id)"
            @open-history="openAgentHistory(nodeProps.id)"
            @new-session="onAgentNewSession(nodeProps.id)"
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
            @preview="openPreview('image', nodeProps.data.url ?? '', nodeProps.id)"
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
            @preview="openPreview('video', nodeProps.data.url ?? '', nodeProps.id)"
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
        <!-- 40 号票:预演台帧卡,整卡点击进全屏编辑层;渲染上传与落画布在编辑器侧。 -->
        <template #node-previz="nodeProps">
          <PrevizNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :connect-state="connection.stateOf(nodeProps.id)"
            @edit="previzEditorNodeId = nodeProps.id"
            @drop="onPrevizDrop(nodeProps.id)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
        <!-- 52 号票:角色引用节点 —— 参照卡,不渲染把手(不作任何连线的
             源与目标);绑定/换绑与落图动作经事件回编辑器。 -->
        <template #node-character="nodeProps">
          <CharacterNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :characters="ipCharacters"
            :voices="voicesCatalog"
            @bind="onCharacterBind(nodeProps.id, $event)"
            @land-image="onCharacterLandImage($event)"
          />
        </template>
        <!-- 53 号票:絵コンテ板卡,整卡点击进全屏编辑层;板名卡上直编,
             落图与画面三入口在编辑层。 -->
        <template #node-storyboard="nodeProps">
          <StoryboardNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :connect-state="connection.stateOf(nodeProps.id)"
            @edit="storyboardEditorNodeId = nodeProps.id"
            @title-change="onStoryboardTitleChange(nodeProps.id, $event)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
        <!-- 54 号票:ネーム板卡,整卡点击进全屏编辑层;板名卡上直编,
             落图与画面三入口在编辑层。 -->
        <template #node-name="nodeProps">
          <NameNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :connect-state="connection.stateOf(nodeProps.id)"
            @edit="nameEditorNodeId = nodeProps.id"
            @title-change="onNameTitleChange(nodeProps.id, $event)"
            @connect-start="connection.start(nodeProps.id, $event)"
          />
        </template>
        <!-- 55 号票:关卡卡,卡内直接编辑不开编辑层(53 号票定案);
             字段补丁/上传/素材挑选面板经事件回编辑器;不渲染把手,
             不参与连线。 -->
        <template #node-level="nodeProps">
          <LevelCard
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :uploading="levelUploading"
            @change="onLevelChange(nodeProps.id, $event)"
            @upload="onLevelUpload(nodeProps.id, $event)"
            @open-picker="openLevelPicker(nodeProps.id)"
          />
        </template>
        <!-- 64 号票:剧本卡,卡面只折叠摘要(标题/摘要行/字数),整卡
             点击进全屏编辑层;不渲染把手,不作任何连线的源与目标。 -->
        <template #node-script="nodeProps">
          <ScriptNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            @edit="scriptEditorNodeId = nodeProps.id"
          />
        </template>
        <!-- 57 号票:音色节点(创建表单内联卡上,只渲染右把手连音频)与
             音频节点(台词合成产物/面板插入的载体,只渲染左把手)。 -->
        <template #node-voice="nodeProps">
          <VoiceNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :voices="voicesCatalog"
            :audio-models="audioModels"
            :audio-prices="audioPrices"
            :enrollment-price="enrollmentPrice"
            :task="taskSync.byNode.get(nodeProps.id) ?? null"
            :retrying="retryingNode === nodeProps.id"
            :deleting="deletingVoice"
            :connect-state="connection.stateOf(nodeProps.id)"
            @form-change="onVoiceFormChange(nodeProps.id, $event)"
            @submit="submitVoiceTask(nodeProps.id)"
            @retry="onRetry(nodeProps.id)"
            @connect-start="connection.start(nodeProps.id, $event)"
            @sample-file="onVoiceSampleFile(nodeProps.id, $event)"
            @pick-sample="openVoicePicker(nodeProps.id)"
            @speech="onVoiceSpeech(nodeProps.id, $event)"
            @delete-voice="onDeleteVoice(nodeProps.id)"
          />
        </template>
        <template #node-audio="nodeProps">
          <AudioNode
            :id="nodeProps.id"
            :type="nodeProps.type"
            :data="nodeProps.data"
            :task="taskSync.byNode.get(nodeProps.id) ?? null"
            :retrying="retryingNode === nodeProps.id"
            :music-models="musicModels"
            :music-prices="musicPrices"
            :music-submitting="musicSubmitting"
            :sfx-models="sfxModels"
            :sfx-prices="sfxPrices"
            :sfx-submitting="sfxSubmitting"
            :connect-state="connection.stateOf(nodeProps.id)"
            @retry="onRetry(nodeProps.id)"
            @form-change="onMusicFormChange(nodeProps.id, $event)"
            @submit="submitAudioTask(nodeProps.id)"
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
        :prices="composerMode === 'video' ? videoPrices : imagePrices"
        :disabled-reason="composerDisabledReason"
        :refs="composerRefs"
        :video-refs="videoComposerRefs"
        :prompt="composerMode === 'video' ? videoPrompt : composerPrompt"
        :model="composerMode === 'video' ? videoModel : composerModel"
        :ratio="composerMode === 'video' ? videoRatio : composerRatio"
        :resolution="composerMode === 'video' ? videoResolution : composerResolution"
        :transparent="composerTransparent"
        :duration="videoDuration"
        :generating="generating"
        :uploading="refUploading"
        @update:mode="composerMode = $event"
        @update:prompt="onDraftUpdate(($v) => (videoPrompt = $v), ($v) => (composerPrompt = $v), $event)"
        @update:model="onDraftUpdate(($v) => (videoModel = $v), ($v) => (composerModel = $v), $event)"
        @update:ratio="onDraftUpdate(($v) => (videoRatio = $v), ($v) => (composerRatio = $v), $event)"
        @update:resolution="onDraftUpdate(($v) => (videoResolution = $v), ($v) => (composerResolution = $v), $event)"
        @update:transparent="composerTransparent = $event"
        @update:duration="videoDuration = $event"
        @remove-ref="composerMode === 'video' ? onRemoveVideoRef($event) : onRemoveRef($event)"
        @set-ref-role="onSetVideoRefRole"
        @preview-ref="openPreview($event.kind, $event.url, '')"
        @upload="onComposerUpload"
        @pick-assets="toggleComposerPicker"
        @retry-catalogs="refreshModelCatalogs"
        @send="onComposerSend"
      />
      <AssetPanel
        v-if="assetPanelOpen"
        :ip-id="canvasIPID || undefined"
        :upload-ip-id="canvasIPID || undefined"
        @insert="insertAsset"
      />
      <GenerationRecordsPanel
        v-if="recordsPanelOpen"
        :tasks="taskRecords"
        @locate="locateRecordTask"
        @insert="insertRecordTask"
      />
      <!-- 32 号票:Agent 会话历史面板与素材选择面板,绑定单个 Agent 节点
           (按节点 id 换 key 重挂),节点被删时随 computed 自动消失。 -->
      <AgentHistoryPanel
        v-if="historyNode"
        :key="`history-${historyNode.id}`"
        :messages="historyMessages"
        @clear="clearAgentHistory(historyNode.id)"
        @close="historyNodeId = ''"
      />
      <!-- 47 号票:对话框素材挑选面板(与 composer 伴生,key 带模式 --
           模式切换重挂,白名单与过滤档随行重置);32 号票 Agent 素材选择
           面板泛化为同一组件。两面板互斥(右缘面板纪律)。 -->
      <AssetPicker
        v-if="composerPickerOpen"
        :key="`composer-picker-${composerMode}`"
        title="挑选参考素材"
        :kind-whitelist="composerPickerKinds"
        :hint="composerPickerHint"
        :selected-asset-ids="composerPickerSelectedIds"
        :ip-id="canvasIPID || undefined"
        @toggle="onComposerPickToggle"
        @close="composerPickerOpen = false"
      />
      <AssetPicker
        v-else-if="pickerNode"
        :key="`picker-${pickerNode.id}`"
        title="选择参考素材"
        :kind-whitelist="['image', 'video']"
        hint="点选挂为会话附件,再点取消;单条消息 ≤4 图 + ≤1 视频。"
        :selected-asset-ids="pickerSelectedIds"
        :ip-id="canvasIPID || undefined"
        @toggle="onAgentPickToggle(pickerNode.id, $event)"
        @close="pickerNodeId = ''"
      />
      <!-- 55 号票:关卡卡画面槽的素材挑选面板(scope=ip 默认),节点被删
           时随 computed 自动消失。 -->
      <AssetPicker
        v-else-if="levelPickerNode"
        :key="`level-picker-${levelPickerNode.id}`"
        title="挑选画面参考"
        :kind-whitelist="['image']"
        hint="点选挂为关卡画面参考,再点取消;画面槽最多 9 张。"
        :selected-asset-ids="levelPickerSelectedIds"
        :ip-id="canvasIPID || undefined"
        @toggle="onLevelPickToggle(levelPickerNode.id, $event)"
        @close="levelPickerNodeId = ''"
      />
      <!-- 57 号票:音色克隆样本的挑选面板(白名单仅音频),点选即绑样本,
           再点取消;≤10MB 与音频种类的前置守卫在节点表单侧。 -->
      <AssetPicker
        v-else-if="voicePickerNode"
        :key="`voice-picker-${voicePickerNode.id}`"
        title="挑选声音样本"
        :kind-whitelist="['audio']"
        hint="点选挂为克隆样本,再点取消;建议 10–20 秒清晰人声。"
        :selected-asset-ids="voicePickerSelectedIds"
        :ip-id="canvasIPID || undefined"
        @toggle="onVoicePickToggle(voicePickerNode.id, $event)"
        @close="closeVoicePicker"
      />
      <!-- 40 号票:预演台全屏编辑层(Teleport 到 body),场景改动走同一条
           scene-change 落盘链;渲染帧大图经编辑器级灯箱打开(灯箱入口收进
           编辑层,33 号票对图片/视频节点不变)。 -->
      <PrevizEditorLayer
        v-if="previzEditorNode"
        :key="`previz-${previzEditorNode.id}`"
        :data="previzEditorNode.data"
        :rendering="previzRendering === previzEditorNode.id"
        :render-busy="previzRendering !== ''"
        @scene-change="onPrevizSceneChange(previzEditorNode.id, $event)"
        @render="onPrevizRender(previzEditorNode.id, $event)"
        @drop="onPrevizDrop(previzEditorNode.id)"
        @preview="openPreview('image', previzEditorNode.data.latest?.url ?? '', previzEditorNode.id)"
        @close="previzEditorNodeId = ''"
      />
      <!-- 53 号票:絵コンテ全屏编辑层(Teleport 到 body),cut/板名整包
           走同一条 scene-change 落盘链;画面三入口(素材库/上传/落图)在
           层内,素材挑选面板挂层右缘;67 号票增出场角色标注(本 IP 角色
           目录)与「生成视频」直通(落节点+来源边+对话框预填)。 -->
      <StoryboardEditorLayer
        v-if="storyboardEditorNode"
        :key="`storyboard-${storyboardEditorNode.id}`"
        :data="storyboardEditorNode.data"
        :ip-id="canvasIPID || undefined"
        :uploading="storyboardUploading"
        :characters="ipCharacters"
        @scene-change="onStoryboardSceneChange(storyboardEditorNode.id, $event)"
        @upload="onStoryboardLayerUpload"
        @drop="onStoryboardDrop(storyboardEditorNode.id, $event)"
        @generate-video="onStoryboardGenerateVideo(storyboardEditorNode.id, $event)"
        @close="storyboardEditorNodeId = ''"
      />
      <!-- 54 号票:ネーム全屏编辑层(Teleport 到 body),页/格/板名整包
           走同一条 scene-change 落盘链;画面三入口(素材库/上传/落图)在
           层内,素材挑选面板挂层右缘。 -->
      <NameEditorLayer
        v-if="nameEditorNode"
        :key="`name-${nameEditorNode.id}`"
        :data="nameEditorNode.data"
        :ip-id="canvasIPID || undefined"
        :uploading="nameUploading"
        @scene-change="onNameSceneChange(nameEditorNode.id, $event)"
        @upload="onNameLayerUpload"
        @drop="onNameLayerDrop"
        @close="nameEditorNodeId = ''"
      />
      <!-- 64 号票:剧本全屏编辑层(Teleport 到 body),标题/正文整包走
           同一条 scene-change 落盘链;超 30 万字上限在层内提示分段粘贴。 -->
      <ScriptEditorLayer
        v-if="scriptEditorNode"
        :key="`script-${scriptEditorNode.id}`"
        :data="scriptEditorNode.data"
        @scene-change="onScriptSceneChange(scriptEditorNode.id, $event)"
        @close="scriptEditorNodeId = ''"
      />
      <!-- 33 号票:媒体预览灯箱,编辑器级单实例(Teleport 到 body);41 号票
           截帧事件在此入素材 + 落画布,失败文案走轻提示。参考条 chip 的预览
           (nodeId 空)无源节点,截帧入口收起。 -->
      <MediaLightbox
        v-if="lightbox"
        :kind="lightbox.kind"
        :url="lightbox.url"
        :can-capture="!!lightbox.nodeId"
        @close="lightbox = null"
        @capture="onLightboxCapture"
        @capture-error="showNotice($event)"
      />
      <!-- 38 号票评审:轻提示(删除可撤销 / 非法连线反馈),自动消退。 -->
      <EditorToast :text="notice" />
    </div>

    <CanvasToolbar
      :asset-panel-open="assetPanelOpen"
      :records-panel-open="recordsPanelOpen"
      :uploading="uploading"
      @add="addNode"
      @toggle-asset="toggleAssetPanel"
      @toggle-records="toggleRecordsPanel"
      @upload-change="onUploadChange"
    />
  </div>
</template>

<style scoped>
.editor {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

/* 顶栏/横幅/工具栏/轻提示样式已随组件拆分迁出(58 号票阶段一):
   CanvasTopbar / EditorAlerts / CanvasToolbar / EditorToast。 */

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
  color: var(--text-muted);
  z-index: var(--z-canvas-loading);
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
  z-index: var(--z-connect-preview);
}

.connect-preview line {
  stroke: var(--accent);
  stroke-width: 2;
  stroke-dasharray: 6 4;
}

.connect-preview circle {
  fill: rgba(var(--accent-rgb), 0.9);
}
</style>
