<script setup lang="ts">
// 生成对话框(21 号票图片模式;24 号票视频模式):画布生成的统一对话框,
// 单实例悬浮层(范式见 ADR 0002)。组件只做渲染与事件:草稿(提示词/
// 模型/尺寸/时长/参考条)由编辑器按模式独立持有(切换不清空),发送语义
// = 提交画布任务。视频模式 = 文生视频/图生视频/首尾帧/多参考的组合形态,
// 参考组合自然成模式,无显式模式 tab;选中视频节点时模式锁定视频(视频
// 产物不能当生图参考)。
// 90 号票重排:无边框语言(填充代描边,89 号票 hairline 分组退役)+ 三段
// 结构(顶部动作行 → 大留白输入区 → 底部 meta 行),参数收纳为「参数摘要
// 按钮 + 弹出面板」(模式段/比例/分辨率/背景/风格/时长),成本预估收成
// meta 行 ¥ 胶囊;展开 toggle 与面板开合的状态由编辑器持有(placement
// 估高与 Esc 浮层退栈同步消费)。
import { computed, nextTick, ref, watch } from 'vue'

import type { ModelPriceSummary } from '@infinitechance/api'

import {
  IMAGE_RATIO_PRESETS,
  MAX_COMPOSER_REFS,
  RATIO_PRESETS,
  RESOLUTION_PRESETS,
  durationRangeFor,
  resolutionPresetsFor,
  rolesForKind,
  VIDEO_ROLE_CAPS,
  VIDEO_ROLE_LABEL,
  videoPlaceholder,
  type ComposerRef,
  type VideoComposerRef,
  type VideoRefRole,
} from '../composer'
import { autoDurationDefault, estimateCost, fmtCNY, paramSummary } from '../composerPanel'
import { styleSegment } from '../ipStyle'
import {
  buildElementLine,
  CHARACTER_ELEMENT_HINT_LIMIT,
  countCharacterElements,
  ELEMENT_TYPE_LABELS,
  hasResolvedElement,
} from '../element'
import {
  hasFrameReferenceConflict,
  mentionInsertText,
  presetNeedsConfirm,
  presetPromptFor,
} from '../composerElements'
import { GAMEPLAY_PRESETS, IMAGE_GAMEPLAY_PRESETS } from '../presets'
import {
  composerRefMentions,
  insertMentionToken,
  mentionTriggerIndex,
  videoRefBadge,
  videoRefMentions,
  type MentionCandidate,
} from '../refmention'
import { useThumbFallback } from '../thumbnail'

const props = defineProps<{
  /** 当前模式:图片生成或视频生成(编辑器持有,切换不清空两套草稿)。 */
  mode: 'image' | 'video'
  /** 模式锁定(选中视频节点):面板模式段的图片生成禁选。 */
  modeLocked: boolean
  /** 可用的按次计价生图模型(编辑器从 /image-models 拉取)。 */
  imageModels: string[]
  /** 可用的按秒计价视频模型(编辑器从 /video-models 拉取)。 */
  videoModels: string[]
  /** 当前模式的模型价格摘要(38 号票):key = 模型名,人类人民币单位;
   * 旧目录无 prices 时为空对象,价格展示与预估随之静默退位。 */
  prices: Record<string, ModelPriceSummary>
  /** 目录不可用原因(38 号票评审:目录拉取失败或未配价时,对话框保留
   * 但整体禁用并说明原因,不再静默蒸发);空 = 正常可用。 */
  disabledReason: string
  /** 图片模式的生效参考图列表。 */
  refs: ComposerRef[]
  /** 视频模式的全模态参考条(角色在 chip 上切换)。 */
  videoRefs: VideoComposerRef[]
  /** 元素显示名(71 号票):与 refs / videoRefs 各自同序同长,编辑器按 IP
   * 目录实时解析后下传(组件不直连 API);null = 无名参考图(含实体已删
   * 退化的元素 chip)。缺省 = 全部无名(行为与 71 号票前一致)。 */
  refNames?: readonly (string | null)[]
  videoRefNames?: readonly (string | null)[]
  /** 当前模式的草稿:提示词/模型/比例/分辨率(视频分辨率为档位串)。 */
  prompt: string
  model: string
  ratio: string
  resolution: string
  /** 图片模式草稿:透明背景开关(37 号票,所有生图模型显示;视频模式
   * 不出现 —— 视频透明显式 out of scope)。 */
  transparent: boolean
  /** 画布所属 IP 的画面风格(69 号票);空串 = 未定,「带 IP 风格」开关
   * 不显示。 */
  ipStyle: string
  /** 当前模式草稿:「带 IP 风格」开关(默认关;图片/视频两模式各存一份,
   * 37 号票透明背景开关范式),开 = 发送时把风格段拼在提示词前部。 */
  withStyle: boolean
  /** 设定图五类型(70 号票):入口一「生成设定图」预填在途时非空 ——
   * 图片模式顶部出现类型选择行(默认三视图),切换由编辑器重拼提示词;
   * 缺省/空 = 普通生成,不出本行。 */
  sheetTypes?: readonly string[]
  /** 设定图类型草稿(预填时默认三视图)。 */
  sheetType?: string
  /** 视频模式草稿:时长档('' = 自动,不传 seconds)。 */
  duration: string
  /** 提交在途(编辑器级状态,防连点)。 */
  generating: boolean
  /** 参考上传在途。 */
  uploading: boolean
  /** 展开态(90 号票 Q8):输入区大留白两档的档位,编辑器持有并喂给
   * placement 估高;组件只渲染 toggle 与样式档。 */
  expanded: boolean
  /** 参数面板开合(90 号票 Q5a):状态提升到编辑器 —— Esc 关闭归编辑器
   * onGlobalKeydown 浮层退栈管,组件渲染面板、摘要按钮与垫挡层。 */
  panelOpen: boolean
}>()

const emit = defineEmits<{
  'update:mode': [value: 'image' | 'video']
  'update:prompt': [value: string]
  'update:model': [value: string]
  'update:ratio': [value: string]
  'update:resolution': [value: string]
  'update:transparent': [value: boolean]
  'update:withStyle': [value: boolean]
  'update:sheetType': [value: string]
  'update:duration': [value: string]
  'update:expanded': [value: boolean]
  'update:panelOpen': [value: boolean]
  'remove-ref': [index: number]
  'set-ref-role': [index: number, role: VideoRefRole]
  /** 参考条 chip 点击预览:图片看大图、视频进灯箱播放(编辑器级灯箱单
   * 实例承接);预览对象无源节点语义,灯箱截帧入口随之收起。 */
  'preview-ref': [payload: { kind: 'image' | 'video'; url: string }]
  upload: [file: File]
  /** 素材库挑选(47 号票):打开/关闭素材选择面板,落条由编辑器接线。 */
  'pick-assets': []
  /** 目录禁用态里的「重试」:编辑器重新拉取模型目录。 */
  'retry-catalogs': []
  send: []
}>()

const fileInput = ref<HTMLInputElement | null>(null)

// chip 缩略显示(49 号票):条目 url 是发送载荷原样保留,仅 <img> 的显示
// 地址按内容寻址派生缩略,onerror 回落原图。
const thumbs = useThumbFallback()

const isVideo = computed(() => props.mode === 'video')
const activeModels = computed(() => (isVideo.value ? props.videoModels : props.imageModels))

/** 比例列表按模式拆分:图片模式用扩展档(多 3:2/2:3),视频沿用
 * RATIO_PRESETS;两模式 sticky 各存一份,不存在跨模式孤儿值。 */
const ratioPresets = computed(() => (isVideo.value ? RATIO_PRESETS : IMAGE_RATIO_PRESETS))

/** 视频分辨率档位随模型收敛(45 号票):MiniMax H3 官方档 768P/2K
 * (H3-Max 另有 480P),Seedance 及未知模型走默认档;档位串以各厂商官方
 * 枚举为准直传上游。 */
const videoResolutionPresets = computed(() => resolutionPresetsFor(props.model))

/** 分辨率列表按模式拆分:图片走全站档,视频按模型收敛(45 号票);
 * 面板分辨率段与参数摘要共用这一份。 */
const activeResolutionPresets = computed(() =>
  isVideo.value ? videoResolutionPresets.value : RESOLUTION_PRESETS,
)

/** IP 已定画面风格才出「带 IP 风格」开关(69 号票:未配风格不显示)。 */
const styleAvailable = computed(() => styleSegment(props.ipStyle) !== '')

/** 时长区间随所选模型收敛(官方校准:seedance 2.0 = 4–15,2.5 = 4–30;
 * minimax-h3 = 4–15、h3-max = 5–15;未知模型保守 4–15)。留空 = 自动
 * (不传 seconds)。 */
const durationRange = computed(() => durationRangeFor(props.model))

// 模型切换后,已选分辨率档可能不属于新模型(如 seedance 720p → H3 只有
// 768P/2K):回落到「自动分辨率」而非静默持无效档,提交必被上游 400。
watch(
  () => props.model,
  () => {
    if (isVideo.value && !videoResolutionPresets.value.some((p) => p.value === props.resolution)) {
      emit('update:resolution', '')
    }
  },
)

/** 失焦时把输入夹紧回区间(整数):留空保持自动,越界/小数就地归位。 */
function onDurationBlur(e: Event): void {
  const raw = (e.target as HTMLInputElement).value.trim()
  if (raw === '') {
    return
  }
  const n = Number(raw)
  if (Number.isInteger(n) && n >= durationRange.value.min && n <= durationRange.value.max) {
    return
  }
  const clamped = Math.min(Math.max(Math.round(n) || durationRange.value.min, durationRange.value.min), durationRange.value.max)
  emit('update:duration', String(clamped))
}

const canSend = computed(
  () =>
    props.prompt.trim().length > 0 &&
    props.model !== '' &&
    !props.generating &&
    props.disabledReason === '',
)

const refsFull = computed(() => props.refs.length >= MAX_COMPOSER_REFS)

// ---- 成本可见性(38 号票):下拉带价、发送前预估(90 号票起拆成 meta 行
// ¥ 胶囊的短句 + title 原句,数字与文案的实现在 composerPanel.ts 纯函数)----

/** 模型下拉 option 的展示名:带价时缀上人类可读单价,选中即见成本。 */
function modelOptionLabel(m: string): string {
  const p = props.prices[m]
  if (!p) {
    return m
  }
  if (p.unit === 'call' && p.cny_per_call != null) {
    return `${m} · ${fmtCNY(p.cny_per_call)}/张`
  }
  if (p.unit === 'second' && p.cny_per_call != null) {
    return `${m} · ${fmtCNY(p.cny_per_call)}/秒`
  }
  if (p.unit === 'token' && p.output_cny_per_mtokens != null) {
    return `${m} · 约 ${fmtCNY(p.output_cny_per_mtokens)}/百万 tok`
  }
  return m
}

/** 发送前预估(仅展示,向上取整对齐服务端预扣口径;旧目录无 prices 或
 * 未知轨道时 null 退位 —— 90 号票起 meta 行 ¥ 胶囊整颗隐藏,不显示 ¥0)。
 * 轨道口径与 38 号票逐字一致:second 轨按 分辨率系数 × 秒数;时长自动时
 * 只报单价;视频 token 轨按 秒折算率 × 秒数 × 输出单价 × 倍率;call 轨按
 * 尺寸系数,不乘参考图数(参考加价各厂商不同,不虚报)。text = 「本次
 * 预估 …」原句(胶囊 title),amount = 胶囊短句。 */
const estimateInfo = computed(() =>
  estimateCost(props.prices[props.model], {
    ratio: props.ratio,
    resolution: props.resolution,
    duration: props.duration,
  }),
)

// ---- @ 引用浮层(59 号票)----

// 触发与保活同一条判定:光标前一字符是 @ 且参考条有可 @ 条目(口径随
// 模式:图片模式全条目、视频模式仅参考图角色);继续输入、移动光标离开
// @ 位即收,不做文本过滤。
const promptEl = ref<HTMLTextAreaElement | null>(null)
const mentionOpen = ref(false)
const mentionHighlight = ref(0)
const mentionTriggerAt = ref(-1)

// 71 号票:元素 chip 的条目带显示名与类型(names 与条目同序),选中插纯名字。
const mentionCandidates = computed(() =>
  isVideo.value
    ? videoRefMentions(props.videoRefs, props.videoRefNames)
    : composerRefMentions(props.refs, props.refNames),
)

watch(mentionCandidates, (list) => {
  if (mentionOpen.value && list.length === 0) {
    closeMention()
  }
  if (mentionHighlight.value >= list.length) {
    mentionHighlight.value = Math.max(list.length - 1, 0)
  }
})

function syncMention(): void {
  const el = promptEl.value
  if (!el || props.disabledReason !== '') {
    closeMention()
    return
  }
  const trigger = mentionTriggerIndex(el.value, el.selectionStart ?? 0)
  mentionTriggerAt.value = trigger
  if (trigger >= 0 && mentionCandidates.value.length > 0) {
    if (!mentionOpen.value) {
      mentionHighlight.value = 0
    }
    mentionOpen.value = true
  } else {
    mentionOpen.value = false
  }
}

function closeMention(): void {
  mentionOpen.value = false
  mentionTriggerAt.value = -1
}

function onPromptInput(e: Event): void {
  emit('update:prompt', (e.target as HTMLTextAreaElement).value)
  syncMention()
}

// 键盘抬起的光标同步:方向键/点击移走光标即收;Enter/Tab/Esc 的 keyup
// 不重判 —— 选中/Esc 刚收起浮层而光标仍在 @ 位,重判会立刻弹回。
function onMentionKeyup(e: KeyboardEvent): void {
  if (e.key === 'Enter' || e.key === 'Tab' || e.key === 'Escape') {
    return
  }
  syncMention()
}

function insertMention(c: MentionCandidate): void {
  const el = promptEl.value
  if (!el || mentionTriggerAt.value < 0) {
    return
  }
  const { text, cursor } = insertMentionToken(el.value, mentionTriggerAt.value, mentionInsertText(c))
  emit('update:prompt', text)
  closeMention()
  void nextTick(() => {
    el.focus()
    el.setSelectionRange(cursor, cursor)
  })
}

/** 键盘发送(38 号票评审):Enter 发送、Shift+Enter 换行,Cmd/Ctrl+Enter
 * 同样发送;输入法组词中不触发(isComposing,31 号票同款纪律)。
 * 59 号票:@ 浮层开着时键盘归浮层 —— ↑↓ 移高亮、Enter/Tab 选中插入、
 * Esc 仅关浮层,不波及对话框与发送。 */
function onPromptKeydown(e: KeyboardEvent): void {
  if (e.isComposing) {
    return
  }
  if (mentionOpen.value) {
    const len = mentionCandidates.value.length
    if (e.key === 'ArrowDown' && len > 0) {
      e.preventDefault()
      mentionHighlight.value = (mentionHighlight.value + 1) % len
      return
    }
    if (e.key === 'ArrowUp' && len > 0) {
      e.preventDefault()
      mentionHighlight.value = (mentionHighlight.value - 1 + len) % len
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      const picked = mentionCandidates.value[mentionHighlight.value] ?? mentionCandidates.value[0]
      if (picked) {
        insertMention(picked)
      }
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      closeMention()
      return
    }
    return
  }
  if (e.key !== 'Enter') {
    return
  }
  if (e.shiftKey && !(e.metaKey || e.ctrlKey)) {
    return // Shift+Enter = 换行,交给默认行为。
  }
  e.preventDefault()
  if (canSend.value) {
    emit('send')
  }
}

/** @ 引用按钮(90 号票 Q7):在光标处插入 @ 并唤起既有浮层 —— 纯入口
 * 增强,零新触发判定;触发与键盘语义全在既有 syncMention/onPromptKeydown。
 * 插入经 emit 走草稿(:value 绑定),nextTick 后光标落到 @ 之后再同步。 */
const insertAtMention = (): void => {
  const el = promptEl.value
  if (!el || props.disabledReason !== '') {
    return
  }
  el.focus()
  const pos = el.selectionStart ?? el.value.length
  emit('update:prompt', `${el.value.slice(0, pos)}@${el.value.slice(pos)}`)
  void nextTick(() => {
    el.setSelectionRange(pos + 1, pos + 1)
    syncMention()
  })
}

/** @ 引用按钮的可用面:目录禁态或参考条无可引用条目(视频模式非参考图
 * 角色不进 @ 口径)时禁用,防插进一个永远弹不出浮层的孤儿 @。 */
const mentionBtnDisabled = computed(
  () => props.disabledReason !== '' || mentionCandidates.value.length === 0,
)

const mentionBtnTitle = computed(() => {
  if (props.disabledReason !== '') {
    return '模型目录不可用'
  }
  return mentionCandidates.value.length === 0
    ? '参考条暂无可引用条目'
    : '在光标处插入 @ 引用参考'
})

/** 视频参考条还有空位:任一角色未满即可再收(appendVideoRef 会按角色
 * 最终裁决,这里只决定加号按钮的可用态与提示)。 */
const videoRefsVacant = computed(() =>
  (Object.keys(VIDEO_ROLE_CAPS) as VideoRefRole[]).some(
    (role) => props.videoRefs.filter((r) => r.role === role).length < VIDEO_ROLE_CAPS[role],
  ),
)

/** 上传/素材库两个入口的共用可用面(90 号票把 89 号票前的 ref-add 与
 * ref-library 收编为动作行图标钮,禁用表达式逐字随迁):图片模式吃满条
 * 禁用 + 目录禁态;视频模式只看角色空位。 */
const addRefDisabled = computed(() =>
  isVideo.value
    ? props.uploading || !videoRefsVacant.value
    : props.uploading || refsFull.value || props.disabledReason !== '',
)

const placeholder = computed(() =>
  isVideo.value ? videoPlaceholder(props.videoRefs) : '描述画面,或对选中图片说明要改什么…',
)

const uploadAccept = computed(() => (isVideo.value ? 'image/*,video/*,audio/*' : 'image/*'))

/** 参考视频在场时提示 Seedance 的编辑型契约(实测 25 号票渠道):此类任务
 * 输出比例与时长跟随输入视频(输入视频本身需 4–30 秒),强制指定比例/时长
 * 会被上游在生成阶段拒绝 —— 提交时 2xx、跑起来才 400,失败落节点很难归
 * 因,故在参考条上就地提醒留空「自动」。 */
const hasVideoRef = computed(() => props.videoRefs.some((r) => r.kind === 'video'))

/** 首帧/尾帧与参考互斥提示(71 号票 A3):三家视频渠道把「首帧/首尾帧」与
 * 「全模态参考」当互斥场景,混用必拒;只提示不拦发送(上游裁决原则不变)。 */
const frameReferenceConflict = computed(() => isVideo.value && hasFrameReferenceConflict(props.videoRefs))

/** 绑定行预览(71 号票 B5):发送时会插进提示词的那一行(编号与 chip 角标
 * 同口径),输入框下方只读展示、不改写输入框;无元素为空串,不显示。 */
const elementLine = computed(() =>
  isVideo.value
    ? buildElementLine(props.videoRefs, props.videoRefNames ?? [], 'video')
    : buildElementLine(props.refs, props.refNames ?? [], 'image'),
)

/** 出场角色超过阈值的提示(71 号票 B7 引入;85 号票 C 泛化:文案去厂商
 * 语境,图片模式同样出提示行 —— 多角色同框稳定性下降不限于视频)。只提示
 * 不拦发送,两模式一致;≤4 不出现。退化无名的角色 chip 不进计数(已不是
 * 有名元素,71 号票 B2)。 */
const tooManyCharacters = computed(() =>
  isVideo.value
    ? countCharacterElements(props.videoRefs, props.videoRefNames ?? []) >
      CHARACTER_ELEMENT_HINT_LIMIT
    : countCharacterElements(props.refs, props.refNames ?? []) > CHARACTER_ELEMENT_HINT_LIMIT,
)

/** 当前模式的玩法预设清单(85 号票 B1:图片模式也出「玩法」下拉,渲染
 * IMAGE_GAMEPLAY_PRESETS;视频模式维持 GAMEPLAY_PRESETS,85 号票 B2 已
 * 垫后追加「三人同框」)。 */
const gameplayPresets = computed(() => (isVideo.value ? GAMEPLAY_PRESETS : IMAGE_GAMEPLAY_PRESETS))

/** 「玩法」下拉(71 号票 C;85 号票 B1 起图片模式同款):选中即按参考条
 * 元素 chip 序填名写进提示词 —— 提示词空直接写入、非空先确认替换;只写
 * 提示词,不动时长/比例/分辨率/模型/风格,不记「当前预设」状态(下拉写完
 * 复位到占位项)。 */
function onPresetChange(e: Event): void {
  const select = e.target as HTMLSelectElement
  const preset = gameplayPresets.value.find((p) => p.key === select.value)
  select.value = ''
  if (!preset) {
    return
  }
  const [refs, names] =
    isVideo.value ? [props.videoRefs, props.videoRefNames ?? []] : [props.refs, props.refNames ?? []]
  const next = presetPromptFor(preset, refs, names)
  if (presetNeedsConfirm(props.prompt, next) && !window.confirm(`用「${preset.label}」模板替换当前提示词?`)) {
    return
  }
  emit('update:prompt', next)
}

const uploadTitle = computed(() => {
  if (isVideo.value) {
    return videoRefsVacant.value ? '上传本机图片/视频/音频作为参考' : '各角色参考已满员'
  }
  return refsFull.value ? `最多 ${MAX_COMPOSER_REFS} 张参考图` : '上传本机图片作为参考图'
})

/** 素材库入口(47 号票)与上传按钮同一可用面:参考条满员时禁选。 */
const pickAssetsTitle = computed(() => {
  if (isVideo.value) {
    return videoRefsVacant.value ? '从素材库挑选图片/视频/音频作为参考' : '各角色参考已满员'
  }
  return refsFull.value ? `最多 ${MAX_COMPOSER_REFS} 张参考图` : '从素材库挑选图片作为参考图'
})

/** 参考上传:kind 由编辑器按文件 MIME/扩展名判断(图片模式下恒 image),
 * 服务端按魔数最终裁决。 */
function onFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (!file) {
    return
  }
  emit('upload', file)
}

// ---- 参数摘要与面板(90 号票 Q5a/Q10a)----

/** 参数摘要按钮文案:按当前值实时拼(如图片「自适应 / 1K」、视频
 * 「5s / 16:9 / 1080p」;透明附「透明」、IP 风格附「风格」),拼装规则
 * 在 composerPanel.paramSummary 纯函数。 */
const paramSummaryText = computed(() =>
  paramSummary(
    props.mode,
    {
      ratio: props.ratio,
      resolution: props.resolution,
      duration: props.duration,
      transparent: props.transparent,
      withStyle: props.withStyle,
    },
    { ratio: ratioPresets.value, resolution: activeResolutionPresets.value },
  ),
)

/** 背景段(90 号票:仅图片模式,仅「自动 / 透明」两档 —— 「不透明」不是
 * 契约值,37 号票 background 只收 transparent)。 */
const backgroundChips = [
  { label: '自动', transparent: false },
  { label: '透明', transparent: true },
] as const

/** 时长自动档(90 号票 Q11):duration 空串 = 自动(不传 seconds,保
 * 24 号票「留空 = 不传 = 厂商缺省」语义),滑杆区随之隐藏;取消自动 =
 * 滑杆出现默认 5s(autoDurationDefault 按模型区间夹紧)。 */
const durationAuto = computed(() => props.duration === '')

const onAutoChipToggle = (): void => {
  emit('update:duration', durationAuto.value ? autoDurationDefault(durationRange.value) : '')
}

/** 比例形状预览(90 号票):按 w:h 把预览块长边收进 22px,短边兜底 6px,
 * 「自适应」档不参算(模板里另渲染虚线空档)。 */
function ratioShapeStyle(value: string): Record<string, string> {
  const [w, h] = value.split(':').map((n) => Number(n))
  if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) {
    return {}
  }
  const scale = 22 / Math.max(w, h)
  return {
    width: `${Math.max(Math.round(w * scale), 6)}px`,
    height: `${Math.max(Math.round(h * scale), 6)}px`,
  }
}
</script>

<template>
  <section
    class="composer"
    :class="{ disabled: disabledReason !== '', expanded }"
    aria-label="生成对话框"
  >
    <!-- 目录不可用禁态(38 号票评审):对话框保留、说明原因、可重试,
         不再静默蒸发。 -->
    <div
      v-if="disabledReason"
      class="catalog-error"
      role="alert"
    >
      <span>{{ disabledReason }}</span>
      <button
        type="button"
        @click="emit('retry-catalogs')"
      >
        重试
      </button>
    </div>

    <!-- 顶部动作行(90 号票):引用类入口聚左 —— @ 引用(光标处插 @ 唤起
         既有浮层)/ + 上传 / 库(47 号 AssetPicker 收编为图标钮);右侧
         展开 toggle(Q8,输入区大留白两档,placement 估高同步消费)。 -->
    <div class="composer-actions">
      <button
        class="action-btn"
        type="button"
        aria-label="插入 @ 引用参考"
        :title="mentionBtnTitle"
        :disabled="mentionBtnDisabled"
        @click="insertAtMention"
      >
        @
      </button>
      <button
        class="action-btn"
        type="button"
        aria-label="上传参考"
        :title="uploadTitle"
        :disabled="addRefDisabled"
        @click="fileInput?.click()"
      >
        {{ uploading ? '…' : '+' }}
      </button>
      <button
        class="action-btn"
        type="button"
        aria-label="从素材库挑选参考"
        :title="pickAssetsTitle"
        :disabled="addRefDisabled"
        @click="emit('pick-assets')"
      >
        库
      </button>
      <input
        ref="fileInput"
        type="file"
        :accept="uploadAccept"
        class="ref-file"
        @change="onFileChange"
      >
      <button
        class="expand-toggle"
        type="button"
        :aria-pressed="expanded"
        :title="expanded ? '收起输入区' : '展开输入区'"
        @click="emit('update:expanded', !expanded)"
      >
        {{ expanded ? '收起' : '展开' }}
      </button>
    </div>

    <!-- 图片模式:参考图缩略条(90 号票起仅 chip,+ / 库 上移动作行) -->
    <div
      v-if="!isVideo"
      class="ref-strip"
    >
      <div
        v-for="(r, i) in refs"
        :key="r.url"
        class="ref-chip"
      >
        <span class="ref-badge">{{ i + 1 }}</span>
        <img
          :src="thumbs.displayURL(r.url)"
          alt="参考图"
          title="点击预览大图"
          @click="emit('preview-ref', { kind: 'image', url: r.url })"
          @error="thumbs.onThumbError(r.url)"
        >
        <!-- 元素名称角标(71 号票 B4):类型小标 + 实时显示名;实体已删退化
             无名时不出(编号角标照旧)。 -->
        <span
          v-if="r.element && refNames?.[i]"
          class="ref-element"
          :title="`元素:${refNames?.[i]}(${ELEMENT_TYPE_LABELS[r.element.type]})`"
        >
          <span class="ref-element-type">{{ ELEMENT_TYPE_LABELS[r.element.type] }}</span>
          <span class="ref-element-name">{{ refNames?.[i] }}</span>
        </span>
        <button
          class="ref-remove"
          type="button"
          title="移除参考图(发送即文生图,连线仍保留)"
          aria-label="移除参考图"
          @click="emit('remove-ref', i)"
        >
          ×
        </button>
      </div>
    </div>

    <!-- 视频模式:全模态参考条,角色标在 chip 上(图缩略,视频/音频图标
         占位,不做波形与抽帧);图片 chip 的角色可切换。 -->
    <div
      v-else
      class="ref-strip"
    >
      <div
        v-for="(r, i) in videoRefs"
        :key="r.url"
        class="vref-chip"
        :data-kind="r.kind"
      >
        <span
          v-if="r.role === 'reference_image'"
          class="ref-badge"
        >{{ videoRefBadge(videoRefs, i) }}</span>
        <img
          v-if="r.kind === 'image'"
          :src="thumbs.displayURL(r.url)"
          alt="参考图"
          title="点击预览大图"
          @click="emit('preview-ref', { kind: 'image', url: r.url })"
          @error="thumbs.onThumbError(r.url)"
        >
        <button
          v-else-if="r.kind === 'video'"
          class="vref-icon"
          type="button"
          title="点击播放"
          aria-label="播放参考视频"
          @click="emit('preview-ref', { kind: 'video', url: r.url })"
        >
          ▶
        </button>
        <span
          v-else
          class="vref-icon"
        >♪</span>
        <span
          v-if="r.element && videoRefNames?.[i]"
          class="ref-element"
          :title="`元素:${videoRefNames?.[i]}(${ELEMENT_TYPE_LABELS[r.element.type]})`"
        >
          <span class="ref-element-type">{{ ELEMENT_TYPE_LABELS[r.element.type] }}</span>
          <span class="ref-element-name">{{ videoRefNames?.[i] }}</span>
        </span>
        <!-- 可解析元素 chip 恒为参考图(71 号票 B3):角色切换禁用;实体已删
             退化无名的 chip 不再算元素,恢复 44 号票普通角色规则(71 号票 B2)。 -->
        <select
          v-if="r.kind === 'image'"
          class="vref-role"
          :value="r.role"
          :disabled="hasResolvedElement(r, videoRefNames?.[i])"
          :title="hasResolvedElement(r, videoRefNames?.[i]) ? '元素固定为参考图' : `角色(${VIDEO_ROLE_LABEL[r.role]})`"
          @change="emit('set-ref-role', i, ($event.target as HTMLSelectElement).value as VideoRefRole)"
        >
          <option
            v-for="role in rolesForKind('image')"
            :key="role"
            :value="role"
          >
            {{ VIDEO_ROLE_LABEL[role] }}
          </option>
        </select>
        <span
          v-else
          class="vref-role fixed"
        >{{ VIDEO_ROLE_LABEL[r.role] }}</span>
        <button
          class="ref-remove"
          type="button"
          title="移除该参考"
          @click="emit('remove-ref', i)"
        >
          ×
        </button>
      </div>
    </div>

    <!-- 设定图类型选择(70 号票入口一):预填在途时出现,五类型固定序、
         默认三视图;切换上抛编辑器重拼提示词(模板 + 四件套 + 变体 +
         风格段),label 随发送冻结进锚定预约。 -->
    <div
      v-if="!isVideo && (sheetTypes?.length ?? 0) > 0"
      class="sheet-types"
      role="tablist"
      aria-label="设定图类型"
    >
      <button
        v-for="t in sheetTypes"
        :key="t"
        type="button"
        role="tab"
        :class="{ on: t === sheetType }"
        :aria-selected="t === sheetType"
        @click="emit('update:sheetType', t)"
      >
        {{ t }}
      </button>
    </div>

    <!-- 大留白输入区(90 号票):占位符顶左,常规 120 / 展开 240 两档,
         resize 手柄移除(与无边框语言冲突);@ 引用浮层贴输入框上沿。 -->
    <div class="prompt-wrap">
      <textarea
        ref="promptEl"
        :value="prompt"
        :placeholder="placeholder"
        :disabled="disabledReason !== ''"
        aria-label="生成提示词"
        @input="onPromptInput"
        @keydown="onPromptKeydown"
        @keyup="onMentionKeyup"
        @click="syncMention"
        @blur="closeMention"
      />
      <!-- @ 引用浮层(59 号票):贴输入框上沿,列表 = 缩略 + token(视频
           模式加角色标签);↑↓/Enter/Tab/Esc 见 onPromptKeydown。 -->
      <div
        v-if="mentionOpen"
        class="mention-overlay"
        aria-label="参考引用选择"
      >
        <ul class="mention-list">
          <li
            v-for="(c, i) in mentionCandidates"
            :key="`${c.token}-${c.url}`"
            :class="{ on: i === mentionHighlight }"
            @mousedown.prevent="insertMention(c)"
          >
            <img
              :src="thumbs.displayURL(c.url)"
              alt=""
              @error="thumbs.onThumbError(c.url)"
            >
            <!-- 71 号票 B6:元素条目显示名称 + 类型(选中插纯名字),无名图
                 维持 @图片N。 -->
            <span class="mention-token">{{ c.elementName ?? c.token }}</span>
            <span
              v-if="c.elementType"
              class="mention-type"
            >{{ ELEMENT_TYPE_LABELS[c.elementType] }}</span>
            <span
              v-if="c.roleLabel"
              class="mention-role"
            >{{ c.roleLabel }}</span>
          </li>
        </ul>
      </div>
    </div>

    <!-- 输入区与 meta 行之间的提示/预览带(90 号票中间件安置,顺序不变):
         契约提示(vref-hint 族)→ 绑定行预览 + 玩法下拉。 -->
    <!-- 编辑型契约提示(Seedance 参考视频实测):比例/时长跟随输入视频,
         强制指定会被上游在生成阶段拒绝;提示用户留空「自动」。 -->
    <p
      v-if="isVideo && hasVideoRef"
      class="vref-hint"
    >
      含参考视频:任务可能被识别为「视频编辑」,输出比例与时长将跟随输入视频(需
      4–30 秒)——比例与时长请留空「自动」,避免生成阶段被上游拒绝。
    </p>
    <!-- 首帧/参考互斥提示(71 号票 A3):同款安静小字,不拦发送。 -->
    <p
      v-if="frameReferenceConflict"
      class="vref-hint"
    >
      首帧/尾帧与参考图/视频/音频不能混用(Seedance、MiniMax H3 会拒);要首帧效果可只传参考图,并在提示词里写明哪张作首帧
    </p>
    <!-- 出场角色数提示(71 号票 B7;85 号票 C 泛化文案并扩图片模式):
         角色元素超阈值时出,不拦发送。 -->
    <p
      v-if="tooManyCharacters"
      class="vref-hint"
    >
      出场角色超过 {{ CHARACTER_ELEMENT_HINT_LIMIT }} 个,多角色同框稳定性会下降
    </p>

    <!-- 提示词框下沿一行:绑定行预览(71 号票 B5:发送时插在风格段之后的
         那一行,只读、不改写输入框;无元素不显示)+「玩法」下拉(71 号票
         C 视频;85 号票 B1 起图片模式同款,选中写提示词,非空先确认替换)。 -->
    <div
      v-if="elementLine || gameplayPresets.length > 0"
      class="prompt-meta"
    >
      <p
        v-if="elementLine"
        class="element-line"
        :title="elementLine"
      >
        将附加:{{ elementLine }}
      </p>
      <select
        class="preset"
        :disabled="disabledReason !== ''"
        title="玩法预设:按参考条元素填名写进提示词(只写提示词,不动时长/比例/分辨率/模型)"
        aria-label="玩法预设"
        @change="onPresetChange"
      >
        <option value="">
          玩法…
        </option>
        <option
          v-for="p in gameplayPresets"
          :key="p.key"
          :value="p.key"
        >
          {{ p.label }}
        </option>
      </select>
    </div>

    <!-- 底部 meta 行(90 号票):模型带价下拉 + 参数摘要按钮 + ¥ 成本胶囊
         (38 号票人民币口径,无法预估整颗隐藏不显示 ¥0)+ 圆形发送键,
         单行收纳;独立预估行与参数 select 六件套就此退役。 -->
    <div class="composer-meta">
      <select
        :value="model"
        class="model"
        :disabled="disabledReason !== ''"
        :title="isVideo ? '视频模型' : '生图模型'"
        @change="emit('update:model', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="m in activeModels"
          :key="m"
          :value="m"
        >
          {{ modelOptionLabel(m) }}
        </option>
      </select>
      <button
        class="param-summary"
        type="button"
        :disabled="disabledReason !== ''"
        :aria-expanded="panelOpen"
        title="生成参数(比例/分辨率/时长/背景/风格),点击开合参数面板"
        @click="emit('update:panelOpen', !panelOpen)"
      >
        {{ paramSummaryText }}
      </button>
      <span
        v-if="estimateInfo"
        class="cost-pill"
        :title="estimateInfo?.text ?? ''"
      >{{ estimateInfo.amount }}</span>
      <button
        class="send"
        type="button"
        :disabled="!canSend"
        :title="canSend ? '生成(Enter 发送,Shift+Enter 换行)' : '先写提示词并选模型'"
        aria-label="生成"
        @click="emit('send')"
      >
        {{ generating ? '…' : '↑' }}
      </button>
    </div>

    <!-- 参数面板(90 号票 Q5a/Q10a):composer 内浮层,垫挡层点击即收,
         Esc 走编辑器 onGlobalKeydown 浮层退栈。段序 = 模式段(顶部,视频
         节点锁定禁切照旧)→ 比例网格(带形状预览)→ 分辨率通栏分段 →
         背景段(仅图片)→ 带 IP 风格开关行(未配风格不显示照旧)→ 时长段
         (仅视频)。不引入截图有但无领域事实的参数(格式/质量/音频/联网
         搜索等,见票面不引入清单)。 -->
    <div
      v-if="panelOpen"
      class="panel-layer"
    >
      <div
        class="panel-backdrop"
        @click="emit('update:panelOpen', false)"
      />
      <div
        class="param-panel"
        role="dialog"
        aria-label="生成参数面板"
      >
        <div
          class="segment"
          role="group"
          aria-label="生成模式"
        >
          <button
            type="button"
            :class="{ on: mode === 'image' }"
            :disabled="modeLocked || disabledReason !== ''"
            title="选中视频节点时锁定视频生成(视频产物不能当生图参考)"
            @click="emit('update:mode', 'image')"
          >
            图片生成
          </button>
          <button
            type="button"
            :class="{ on: mode === 'video' }"
            :disabled="disabledReason !== ''"
            title="生成模式"
            @click="emit('update:mode', 'video')"
          >
            视频生成
          </button>
        </div>

        <!-- 比例(档位照旧:图片含 3:2/2:3,视频共享 RATIO_PRESETS);空档
             显示「自适应」。图片模式左列竖块、视频模式网格 tile,形状预览
             按 w:h 换算。 -->
        <div
          class="ratio-picker"
          :class="{ 'as-grid': isVideo }"
          role="group"
          aria-label="画面比例"
        >
          <button
            v-for="p in ratioPresets"
            :key="p.value"
            type="button"
            class="ratio-tile"
            :class="{ on: ratio === p.value }"
            :disabled="disabledReason !== ''"
            :title="p.value === '' ? '自适应(由模型缺省裁决)' : `画面比例 ${p.label}`"
            @click="emit('update:ratio', p.value)"
          >
            <span
              v-if="p.value === ''"
              class="ratio-shape auto"
            />
            <span
              v-else
              class="ratio-shape"
              :style="ratioShapeStyle(p.value)"
            />
            <span class="ratio-label">{{ p.value === '' ? '自适应' : p.label }}</span>
          </button>
        </div>

        <!-- 分辨率通栏分段(按模型分档照旧;档位串直传上游)。 -->
        <div
          class="segment"
          role="group"
          aria-label="分辨率档位"
        >
          <button
            v-for="p in activeResolutionPresets"
            :key="p.value"
            type="button"
            :class="{ on: resolution === p.value }"
            :disabled="disabledReason !== ''"
            :title="p.value === '' ? '由模型缺省裁决' : `分辨率档位 ${p.label}(档位串直传上游)`"
            @click="emit('update:resolution', p.value)"
          >
            {{ p.label }}
          </button>
        </div>

        <!-- 背景段(仅图片,仅「自动 / 透明」两档;「不透明」不是契约值)。 -->
        <div
          v-if="!isVideo"
          class="segment"
          role="group"
          aria-label="背景"
        >
          <button
            v-for="c in backgroundChips"
            :key="c.label"
            type="button"
            :class="{ on: transparent === c.transparent }"
            :disabled="disabledReason !== ''"
            :title="c.transparent ? '透明背景(产物为带 alpha 通道的 PNG;上游不支持时错误原样透出)' : '背景由模型缺省裁决'"
            @click="emit('update:transparent', c.transparent)"
          >
            {{ c.label }}
          </button>
        </div>

        <!-- 带 IP 风格开关行(69 号票:未配风格不显示照旧)。 -->
        <label
          v-if="styleAvailable"
          class="style-row"
          :title="`发送时在提示词前拼「${styleSegment(ipStyle)}」(在 IP 编辑里可改风格)`"
        >
          <input
            type="checkbox"
            :checked="withStyle"
            :disabled="disabledReason !== ''"
            @change="emit('update:withStyle', ($event.target as HTMLInputElement).checked)"
          >
          带 IP 风格
        </label>

        <!-- 时长段(仅视频):「自动」chip 选中 = 不传 seconds(滑杆区隐藏,
             保 24 号票「留空 = 不传 = 厂商缺省」);取消自动 = 滑杆出现默认
             5s。滑杆区间跟随 durationRangeFor、step 1,数字输入失焦夹紧照旧。 -->
        <div
          v-if="isVideo"
          class="duration-section"
          role="group"
          aria-label="时长"
        >
          <div class="segment">
            <button
              type="button"
              :class="{ on: durationAuto }"
              :disabled="disabledReason !== ''"
              :title="durationAuto ? '时长由模型裁决;点击取消自动,滑杆默认 5 秒' : '回到自动:不传 seconds,时长由模型缺省裁决'"
              @click="onAutoChipToggle"
            >
              自动
            </button>
          </div>
          <div
            v-if="!durationAuto"
            class="duration-row"
          >
            <input
              type="range"
              class="duration-slider"
              :min="durationRange.min"
              :max="durationRange.max"
              step="1"
              :value="Number(duration)"
              :aria-label="`时长滑杆(${durationRange.min}–${durationRange.max} 秒)`"
              :disabled="disabledReason !== ''"
              @input="emit('update:duration', ($event.target as HTMLInputElement).value)"
            >
            <input
              type="number"
              class="duration-number"
              :value="duration"
              :min="durationRange.min"
              :max="durationRange.max"
              step="1"
              aria-label="时长(秒)"
              :disabled="disabledReason !== ''"
              :title="`时长(秒):本模型支持 ${durationRange.min}–${durationRange.max} 秒`"
              @input="emit('update:duration', ($event.target as HTMLInputElement).value)"
              @blur="onDurationBlur"
            >
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 90 号票无边框协议:填充代描边 —— 更实的面板填充靠色差与画布断开,外
   描边退役(89 号票底部 hairline 分组一并收编),投影弱化为单层极淡
   (悬浮层在密集线稿上仍需一点断开,不全去);内部控件同语言:wash 填充
   代描边,无内部分割线。 */
.composer {
  position: absolute;
  z-index: var(--z-canvas-float);
  width: 560px;
  display: grid;
  gap: var(--sp-sm);
  padding: var(--sp-md);
  background: rgba(var(--surface-panel-rgb), 0.98);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-control);
  box-sizing: border-box;
  animation: composer-in var(--dur-slow) var(--ease);
}

@keyframes composer-in {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

/* ---- 顶部动作行 ---- */
.composer-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

/* 动作钮 / 分段 chips / 比例 tile 共用同一按钮基形:无边框 + 次级文字 +
   wash 交互(hover 提亮、选中实心 accent、禁用降透明),公共声明收编为
   一条;差异项(底色深浅、圆角、内距、布局)各选择器自理。 */
.action-btn,
.segment button,
.ratio-tile {
  border: none;
  color: var(--text-secondary);
  font-size: var(--fs-label);
  cursor: pointer;
  transition:
    background-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.action-btn {
  min-width: 28px;
  height: 28px;
  padding: 0 8px;
  border-radius: var(--r-md);
  background: transparent;
}

.action-btn:hover:not(:disabled) {
  background: var(--wash-fill);
  color: var(--text-primary);
}

.action-btn:disabled,
.segment button:disabled,
.ratio-tile:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* 展开 toggle:居右,实底填充与引用类入口拉开一档。 */
.expand-toggle {
  margin-left: auto;
  height: 28px;
  padding: 0 10px;
  border: none;
  border-radius: var(--r-md);
  background: var(--wash-soft);
  color: var(--text-secondary);
  font-size: var(--fs-label);
  cursor: pointer;
  transition:
    background-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.expand-toggle:hover {
  background: var(--wash-active);
  color: var(--text-primary);
}

/* ---- 参考条(90 号票仅换肤:+ / 库 上移,条内只留 chip)---- */
.ref-strip {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  min-height: 48px;
  flex-wrap: wrap;
}

.ref-strip:empty {
  min-height: 0;
}

.ref-chip {
  position: relative;
  width: 48px;
  flex-shrink: 0;
  display: grid;
  gap: 2px;
}

.ref-chip img {
  width: 48px;
  height: 48px;
  object-fit: cover;
  border-radius: var(--r-lg);
  display: block;
  cursor: zoom-in;
}

/* 元素名称角标(71 号票 B4):类型小标 + 显示名,窄 chip 内省略号截断,
   全称见 title;无名 chip 不渲染本行,尺寸与 71 号票前一致。 */
.ref-element {
  display: flex;
  align-items: center;
  gap: 2px;
  min-width: 0;
  font-size: var(--fs-micro);
  line-height: 1.3;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
}

.ref-element-type {
  flex-shrink: 0;
  padding: 0 3px;
  border-radius: var(--r-sm);
  background: rgba(var(--teal-rgb), 0.16);
  color: var(--teal);
}

.ref-element-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 视频参考 chip:图片缩略或图标占位,角色标记贴在底部。 */
.vref-chip {
  position: relative;
  width: 64px;
  flex-shrink: 0;
  display: grid;
  gap: 2px;
  justify-items: stretch;
}

.vref-chip img {
  width: 64px;
  height: 48px;
  object-fit: cover;
  border-radius: var(--r-lg);
  display: block;
  cursor: zoom-in;
}

.vref-icon {
  width: 64px;
  height: 48px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-lg);
  background: var(--wash-soft);
  color: var(--text-secondary);
  font-size: 18px;
}

/* 参考视频 chip 的播放入口 button 化(键盘可达),视觉与音频图标占位一致。 */
button.vref-icon {
  padding: 0;
  font: inherit;
  font-size: 18px;
  cursor: pointer;
}

button.vref-icon:hover {
  background: var(--wash-active);
}

.vref-role {
  width: 100%;
  background: var(--wash-soft);
  border: none;
  border-radius: var(--r-sm);
  color: inherit;
  font-size: var(--fs-micro);
  padding: 1px 2px;
}

.vref-role.fixed {
  text-align: center;
  color: var(--text-muted);
  font-size: var(--fs-micro);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 元素 chip 的角色下拉禁用态(71 号票:元素固定为参考图)。 */
.vref-role:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.ref-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 50%;
  background: rgba(var(--surface-raised-rgb), 0.95);
  color: var(--text-bright);
  font-size: var(--fs-label);
  line-height: 1;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
  transition: background-color var(--dur-fast) var(--ease);
}

.ref-remove:hover {
  background: rgba(var(--danger-rgb), 0.9);
}

.ref-file {
  display: none;
}

/* chip 编号角标(59 号票):@ 浮层与 chip 编号互相印证;视频模式仅
   参考图角色有号。贴左上角,与右上角的移除钮对称。 */
.ref-badge {
  position: absolute;
  top: -6px;
  left: -6px;
  z-index: 1;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: var(--r-pill);
  background: var(--accent);
  color: var(--accent-ink);
  font-size: var(--fs-micro);
  font-weight: 600;
  line-height: 16px;
  text-align: center;
  pointer-events: none;
}

/* ---- 设定图类型 chips(70 号票,无边框换肤:填充代描边)---- */
.sheet-types {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.sheet-types button {
  border: none;
  background: var(--wash-soft);
  color: var(--text-muted);
  border-radius: var(--r-pill);
  padding: 2px 10px;
  font-size: var(--fs-micro);
  cursor: pointer;
}

.sheet-types button.on {
  color: var(--teal);
  background: rgba(var(--teal-rgb), 0.16);
}

/* ---- 大留白输入区(90 号票)---- */
.prompt-wrap {
  position: relative;
}

textarea {
  width: 100%;
  min-height: 120px;
  background: var(--wash-subtle);
  border: none;
  border-radius: var(--r-lg);
  padding: 8px 10px;
  color: inherit;
  font: inherit;
  line-height: 1.6;
  box-sizing: border-box;
  transition: background-color var(--dur-fast) var(--ease);
}

.composer.expanded textarea {
  min-height: 240px;
}

textarea::placeholder {
  color: var(--text-faint);
}

/* @ 引用浮层:贴输入框上沿左对齐,列表常显全部可 @ 条目(不做过滤)。
   浮层是悬浮件,保留对话框影 + accent 细描边与面板断开(不在壳体无边框
   协议内)。 */
.mention-overlay {
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(100% + 6px);
  z-index: 2;
  padding: 4px;
  background: rgba(var(--surface-panel-rgb), 0.98);
  border: 1px solid rgba(var(--accent-rgb), 0.45);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-dialog);
}

.mention-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 220px;
  overflow-y: auto;
  display: grid;
  gap: 2px;
}

.mention-list li {
  display: flex;
  align-items: center;
  gap: 8px;
  border-radius: var(--r-md);
  padding: 4px 8px;
  cursor: pointer;
}

.mention-list li.on {
  background: rgba(var(--accent-rgb), 0.16);
}

.mention-list img {
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: var(--r-sm);
  display: block;
}

.mention-token {
  font-size: var(--fs-label);
  font-weight: 600;
  color: var(--text-primary);
}

.mention-role {
  margin-left: auto;
  font-size: var(--fs-micro);
  color: var(--text-muted);
}

/* 元素条目的类型小标(71 号票 B6,与 chip 名称角标同色)。 */
.mention-type {
  font-size: var(--fs-micro);
  color: var(--teal);
}

/* ---- 输入区与 meta 行之间的提示/预览带 ---- */
/* 绑定行预览(71 号票 B5):发送时附加的那一行,只读小字、单行省略;
   「玩法」下拉(71 号票 C)贴右。 */
.prompt-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.element-line {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: var(--fs-label);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.prompt-meta .preset {
  margin-left: auto;
  flex-shrink: 0;
  background: var(--wash-soft);
  border: none;
  border-radius: var(--r-md);
  padding: 4px 8px;
  color: inherit;
  font-size: var(--fs-label);
  cursor: pointer;
  transition: background-color var(--dur-fast) var(--ease);
}

.prompt-meta .preset:hover:not(:disabled) {
  background: var(--wash-active);
}

.prompt-meta .preset:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 契约提示行:安静的单行小字,琥珀警示色(与目录禁态同族)。 */
.vref-hint {
  margin: 0;
  font-size: var(--fs-label);
  color: var(--amber);
  line-height: 1.5;
}

/* ---- 目录不可用禁态(38 号票评审,语义色描边不属无边框协议)---- */
.composer.disabled .ref-strip {
  opacity: 0.5;
}

.catalog-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid rgba(var(--amber-rgb), 0.4);
  border-radius: var(--r-lg);
  background: rgba(var(--amber-rgb), 0.08);
  font-size: var(--fs-label);
  color: var(--amber);
}

.catalog-error button {
  border: 1px solid rgba(var(--amber-rgb), 0.5);
  border-radius: var(--r-md);
  background: transparent;
  color: var(--amber);
  padding: 4px 10px;
  font-size: var(--fs-label);
  cursor: pointer;
  white-space: nowrap;
}

.catalog-error button:hover {
  background: rgba(var(--amber-rgb), 0.12);
}

/* ---- 底部 meta 行(90 号票)---- */
.composer-meta {
  display: flex;
  align-items: center;
  gap: var(--sp-sm);
}

.composer-meta .model {
  flex-shrink: 0;
  max-width: 172px;
  background: var(--wash-soft);
  border: none;
  border-radius: var(--r-md);
  padding: 6px 8px;
  color: inherit;
  font-size: var(--fs-label);
  cursor: pointer;
  transition: background-color var(--dur-fast) var(--ease);
}

.composer-meta .model:hover:not(:disabled) {
  background: var(--wash-active);
}

.composer-meta .model:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 参数摘要按钮:占 meta 行剩余宽度,文案按当前值实时拼。 */
.param-summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
  background: var(--wash-soft);
  border: none;
  border-radius: var(--r-md);
  padding: 6px 10px;
  color: var(--text-secondary);
  font-size: var(--fs-label);
  cursor: pointer;
  transition: background-color var(--dur-fast) var(--ease);
}

.param-summary:hover:not(:disabled) {
  background: var(--wash-active);
  color: var(--text-primary);
}

.param-summary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* ¥ 成本胶囊(38 号票人民币口径):预估灰蓝一族,安静可见不惊扰。 */
.cost-pill {
  flex-shrink: 0;
  padding: 4px 10px;
  border-radius: var(--r-pill);
  background: rgba(var(--estimate-ink-rgb), 0.14);
  color: var(--estimate-ink);
  font-size: var(--fs-label);
  font-weight: 600;
  white-space: nowrap;
  cursor: help;
}

.send {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: var(--r-pill);
  background: var(--accent);
  color: var(--accent-ink);
  font-size: 16px;
  font-weight: 700;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
  transition:
    background-color var(--dur-fast) var(--ease),
    box-shadow var(--dur-fast) var(--ease),
    transform var(--dur-fast) var(--ease);
}

/* skill 微动效数字:hover 上步 1px、按下回沉 0.985;
   只在真指针环境生效,触屏不表演(89 号票微动效保留)。 */
@media (hover: hover) and (pointer: fine) {
  .send:hover:not(:disabled) {
    background: var(--accent-soft);
    box-shadow: 0 0 12px rgba(var(--accent-rgb), 0.35);
    transform: translateY(-1px);
  }
}

.send:active:not(:disabled) {
  box-shadow: none;
  transform: scale(0.985);
}

.send:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* ---- 参数面板(90 号票):composer 内浮层,垫挡层点击即收 ---- */
.panel-layer {
  position: absolute;
  inset: 0;
  z-index: 3;
}

.panel-backdrop {
  position: absolute;
  inset: 0;
}

.param-panel {
  position: absolute;
  left: var(--sp-md);
  right: var(--sp-md);
  bottom: 48px;
  display: grid;
  gap: var(--sp-md);
  padding: var(--sp-md);
  max-height: 420px;
  overflow-y: auto;
  background: rgba(var(--surface-raised-rgb), 0.98);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-dialog);
  animation: composer-in var(--dur-fast) var(--ease);
}

/* 分段 chips 族:选中实心(工作灯蓝);hover 提亮与比例 tile 共条。 */
.segment {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.segment button {
  border-radius: var(--r-pill);
  padding: 5px 12px;
  background: var(--wash-soft);
}

.segment button:hover:not(:disabled),
.ratio-tile:hover:not(:disabled) {
  background: var(--wash-active);
  color: var(--text-primary);
}

.segment button.on,
.segment button.on:hover:not(:disabled),
.ratio-tile.on,
.ratio-tile.on:hover:not(:disabled) {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 600;
}

/* 比例两形态(90 号票):图片左列竖块、视频网格 tile,形状预览描边随
   currentColor(选中实心底上自动转深)。 */
.ratio-picker {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ratio-picker.as-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
}

.ratio-tile {
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: var(--r-md);
  background: var(--wash-soft);
  padding: 6px 10px;
}

.as-grid .ratio-tile {
  flex-direction: column;
  justify-content: center;
  gap: 6px;
  padding: 8px 4px;
}

.ratio-shape {
  flex-shrink: 0;
  display: inline-block;
  border: 1.5px solid currentColor;
  border-radius: 2px;
}

.ratio-shape.auto {
  width: 18px;
  height: 18px;
  border-style: dashed;
}

.ratio-label {
  min-width: 0;
}

/* 带 IP 风格开关行(69 号票语义,行式复选)。 */
.style-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--fs-label);
  color: var(--text-secondary);
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
}

.style-row input {
  accent-color: var(--accent);
  margin: 0;
}

/* 时长段(仅视频):自动 chip + 滑杆 + 数字输入。 */
.duration-section {
  display: grid;
  gap: var(--sp-sm);
}

.duration-row {
  display: flex;
  align-items: center;
  gap: var(--sp-sm);
}

.duration-slider {
  flex: 1;
  min-width: 0;
  accent-color: var(--accent);
}

.duration-number {
  width: 64px;
  flex-shrink: 0;
  background: var(--wash-soft);
  border: none;
  border-radius: var(--r-md);
  padding: 6px 8px;
  color: inherit;
  font-size: var(--fs-label);
  box-sizing: border-box;
}
</style>
