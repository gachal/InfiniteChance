<script setup lang="ts">
// 画布助手面板(91 号票):右侧固定面板照三面板范式(--w-panel 族宽、
// --z-side-panel、右缘互斥纪律 —— 开本面板关素材/记录/挑选等)。会话切换
// 下拉 + 新建/删除、「先询问我/完全授权」授权档切换(PATCH 即时生效)、
// 模型下拉(GET /prompt-models 目录;新会话初值 = 桌宠配置的「缺省对话
// 模型」,92 号票已接线,不在目录回落首项)、工具过程时间线(.tool-timeline
// 族引用 style.css 共享段,与 AgentNode 同源)、
// 「先询问我」的变更清单确认卡(整批批准/拒绝,不做逐项勾选)。
//
// 协议(91 号票 SSE + 回执):草案工具 tool_call 到达时,完全授权档直接
// 经 host 应用并发回执;先询问我档出确认卡等用户裁决。建议 chips/问候语
// 是 92 号票桌宠的职责 —— 新会话首屏空态即本票的接缝。
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import {
  ApiError,
  type AgentToolCallEvent,
  type AgentToolResultEvent,
  type AssistantMessage,
  type AssistantReceiptInput,
  type AssistantSession,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import { agentToolLabel, styleFromPayload, upsertAgentToolStep, type AgentToolStep } from '../agentTools'
import { petChipPage, petDefaultModel, PET_CHIP_PAGE_SIZE } from '../canvasPet'
import {
  appendAssistantTurn,
  describeDraft,
  isAssistantDraftTool,
  parseCreateNodesDraft,
  parseDeleteNodesDraft,
  parseEdgePairsDraft,
  parseTriggerTaskDraft,
  parseUpdateNodesDraft,
  type AssistantHost,
  type AssistantMode,
} from '../assistantPanel'

const props = defineProps<{
  canvasId: number
  /** 聊天模型目录(GET /prompt-models,父视图已加载)。 */
  promptModels: string[]
  /** 视图提供的图应用回调(ADR 0006 前端唯一写者:落图走 useVueFlow +
   * autosave,任务提交走既有 createCanvasTask 同链路)。 */
  host: AssistantHost
  /** ---- 92 号票桌宠接缝(配置自 settings pet 行,热生效)----
   * greeting:新会话首屏文案;chips:开场快捷指令(「换一批」轮换,点击
   * = 预填输入框不直接发送);defaultModel:新会话模型下拉初值(不在目录
   * 回落首项,目录为空回空串)。 */
  greeting?: string
  chips?: string[]
  defaultModel?: string
}>()

const emit = defineEmits<{
  close: []
  /** set_style 回执(复用工具,后端已直写):上抛刷新编辑器本地风格缓存。 */
  'style-changed': [style: string]
}>()

const { client } = useAuth()

const sessions = ref<AssistantSession[]>([])
const activeId = ref<number | null>(null)
const messages = ref<AssistantMessage[]>([])
const mode = ref<AssistantMode>('ask')
const model = ref('')
const loadingSessions = ref(false)
const loadingThread = ref(false)
const error = ref('')
const input = ref('')
const streaming = ref(false)
const streamText = ref('')
const toolSteps = ref<AgentToolStep[]>([])
/** 「先询问我」档的在途草案(确认卡数据);完全授权档不驻留。 */
const pending = ref<{ callId: string; name: string; args: unknown; headline: string; lines: string[] } | null>(null)

const threadEl = ref<HTMLDivElement | null>(null)

const activeModel = computed(() => model.value || props.promptModels[0] || '')

// ---- 92 号票桌宠接缝:首屏问候语 + 建议 chips(「换一批」轮换,点击
// = 预填输入框不直接发送,照玩法预设「写进提示词框再人工改」范式)。----

/** 当前批次的 chips(循环左移轮换;chips 不超过一批时恒回原批)。 */
const chipPage = ref(0)
const visibleChips = computed(() => petChipPage(props.chips ?? [], chipPage.value))
const chipsShuffleable = computed(() => (props.chips?.length ?? 0) > PET_CHIP_PAGE_SIZE)

/** 预填助手输入框(不直接发送):已有草稿时另起一行追加,保留在打内容。 */
function prefillChip(chip: string): void {
  input.value = input.value.trim() === '' ? chip : `${input.value.replace(/\s+$/, '')}\n${chip}`
}

/** 新会话模型初值:桌宠配置的缺省模型优先,不在目录回落首项(目录为空
 * 回空串,模型下拉自身有「无可用模型」禁态)。 */
function applyDefaultModel(): void {
  model.value = petDefaultModel(props.defaultModel ?? '', props.promptModels)
}

function fail(e: unknown, fallback: string): void {
  error.value = e instanceof ApiError ? e.message : fallback
}

onMounted(() => {
  applyDefaultModel()
  void refreshSessions()
})

// 模型目录异步到达时兜底(编辑器加载顺序不保证目录先于面板);仍按
// 「缺省模型优先、回落首项」口径,不让目录时序吞掉桌宠配置的初值。
watch(
  () => props.promptModels,
  (models) => {
    if (model.value === '' && models.length > 0) {
      applyDefaultModel()
    }
  },
)

// 会话流推进时贴底(长对话滚出视口是聊天面板的常态痛点)。
watch([messages, streamText, toolSteps], () => {
  void nextTick(() => {
    threadEl.value?.scrollTo({ top: threadEl.value.scrollHeight })
  })
})

async function refreshSessions(): Promise<void> {
  loadingSessions.value = true
  try {
    const body = await client.listAssistantSessions(props.canvasId)
    sessions.value = body.sessions
    if (activeId.value !== null && !body.sessions.some((s) => s.id === activeId.value)) {
      activeId.value = null
      messages.value = []
    }
  } catch (e) {
    fail(e, '无法加载会话列表,请稍后再试')
  } finally {
    loadingSessions.value = false
  }
}

async function openSession(id: number): Promise<void> {
  if (streaming.value) {
    return // 流在途不切会话(服务端一会话一在途流)
  }
  error.value = ''
  pending.value = null
  activeId.value = id
  loadingThread.value = true
  try {
    const body = await client.getAssistantSession(props.canvasId, id)
    messages.value = body.session.messages
    mode.value = body.session.mode === 'full' ? 'full' : 'ask'
  } catch (e) {
    fail(e, '无法加载会话,请稍后再试')
  } finally {
    loadingThread.value = false
  }
}

async function createSession(): Promise<void> {
  if (streaming.value) {
    return
  }
  error.value = ''
  try {
    const body = await client.createAssistantSession(props.canvasId, {})
    // 新会话的模型下拉回到缺省初值(桌宠配置;92 号票接缝)。
    applyDefaultModel()
    await refreshSessions()
    await openSession(body.session.id)
  } catch (e) {
    fail(e, '无法新建会话,请稍后再试')
  }
}

async function deleteSession(): Promise<void> {
  if (activeId.value === null || streaming.value) {
    return
  }
  error.value = ''
  const id = activeId.value
  activeId.value = null
  messages.value = []
  try {
    await client.deleteAssistantSession(props.canvasId, id)
    await refreshSessions()
  } catch (e) {
    fail(e, '删除会话失败,请稍后再试')
    await openSession(id)
  }
}

/** 切授权档:本地立即生效(下一轮草案走新档),会话侧 PATCH 持久。 */
async function switchMode(event: Event): Promise<void> {
  const next = (event.target as HTMLSelectElement).value as AssistantMode
  mode.value = next
  if (activeId.value === null) {
    return
  }
  try {
    await client.updateAssistantSession(props.canvasId, activeId.value, { mode: next })
  } catch (e) {
    fail(e, '切换授权档失败,请稍后再试')
  }
}

// ---- 回执协议(91 号票)----

function onToolCall(ev: AgentToolCallEvent): void {
  toolSteps.value = upsertAgentToolStep(toolSteps.value, ev)
  if (!isAssistantDraftTool(ev.name)) {
    return // 读/库表类工具服务端同步执行,无草案
  }
  try {
    const card = describeDraft(ev.name, ev.args)
    if (mode.value === 'full') {
      // 完全授权:直接应用 + 回执(任务直提 = 真花钱,票面授权语义)。
      void applyAndReceipt(ev.id, ev.name, ev.args)
    } else {
      pending.value = { callId: ev.id, name: ev.name, args: ev.args, headline: card.headline, lines: card.lines }
    }
  } catch (e) {
    // 草案形状坏:直接回执 failed(不出坏卡),模型看到原因自行纠正。
    void sendReceipt({
      call_id: ev.id,
      status: 'failed',
      message: e instanceof Error ? e.message : '草案无法解析',
    })
  }
}

function onToolResult(ev: AgentToolResultEvent): void {
  const failed = typeof ev.summary === 'string' && (ev.summary.startsWith('应用失败') || ev.summary.startsWith('执行失败'))
  toolSteps.value = upsertAgentToolStep(toolSteps.value, ev, failed)
  // set_style(复用工具,后端已直写 IP 表):刷新编辑器本地风格缓存。
  if (ev.name === 'set_style' && ev.payload !== undefined && ev.payload !== null) {
    try {
      emit('style-changed', styleFromPayload(ev.payload).style)
    } catch {
      // 形状坏忽略:风格缓存下次读表自愈。
    }
  }
}

/** 应用一次草案并回执(完全授权档与确认卡批准共用)。 */
async function applyAndReceipt(callId: string, name: string, args: unknown): Promise<void> {
  try {
    const out = await applyDraft(name, args)
    await sendReceipt({
      call_id: callId,
      status: out.status,
      message: out.message,
      ...(out.status === 'applied' && out.result !== undefined ? { result: out.result } : {}),
    })
  } catch (e) {
    await sendReceipt({
      call_id: callId,
      status: 'failed',
      message: e instanceof Error ? e.message : '应用失败,请稍后再试',
    })
  }
}

async function applyDraft(
  name: string,
  args: unknown,
): Promise<{ status: AssistantReceiptInput['status']; message: string; result?: unknown }> {
  switch (name) {
    case 'create_nodes': {
      const out = props.host.applyCreateNodes(parseCreateNodesDraft(args))
      return fold(out.errors, out.nodes.length, `已新建 ${out.nodes.length} 个节点`, { nodes: out.nodes })
    }
    case 'update_nodes': {
      const out = props.host.applyUpdateNodes(parseUpdateNodesDraft(args))
      return fold(out.errors, out.updated, `已修改 ${out.updated} 个节点`, { updated: out.updated })
    }
    case 'delete_nodes': {
      const ids = parseDeleteNodesDraft(args)
      const out = props.host.applyDeleteNodes(ids)
      return fold(out.errors, out.deleted, `已删除 ${out.deleted} 个节点`, { deleted: out.deleted })
    }
    case 'connect_nodes': {
      const pairs = parseEdgePairsDraft(args)
      const out = props.host.applyConnect(pairs)
      return fold(out.errors, out.applied, `已连线 ${out.applied} 条`, { connected: out.applied })
    }
    case 'disconnect_nodes': {
      const pairs = parseEdgePairsDraft(args)
      const out = props.host.applyDisconnect(pairs)
      return fold(out.errors, out.applied, `已断开 ${out.applied} 条连线`, { disconnected: out.applied })
    }
    case 'trigger_task': {
      const draft = parseTriggerTaskDraft(args)
      const out = await props.host.submitTask(draft)
      return { status: 'applied', message: `已提交生成任务 ${out.task_id}`, result: { task_id: out.task_id } }
    }
    default:
      return { status: 'failed', message: `工具 ${name} 不是草案工具` }
  }
}

/** 折叠「全成 / 全败 / 部分成」三态:部分失败如实回执(已应用 + 失败原因),
 * 全败按 failed 让模型换方案。 */
function fold(
  errors: string[],
  okCount: number,
  okMessage: string,
  result: unknown,
): { status: AssistantReceiptInput['status']; message: string; result?: unknown } {
  if (errors.length === 0) {
    return { status: 'applied', message: okMessage, result }
  }
  if (okCount <= 0) {
    return { status: 'failed', message: errors.join(';') }
  }
  return { status: 'applied', message: `${okMessage};部分失败:${errors.join(';')}`, result }
}

async function sendReceipt(input: AssistantReceiptInput): Promise<void> {
  if (activeId.value === null) {
    return
  }
  try {
    await client.sendAssistantReceipt(props.canvasId, activeId.value, input)
  } catch (e) {
    // 409 no_pending_call = 流已超时/中断:图改动已落图,不回滚也不重投,
    // 时间线的 error 态即本事实(ADR 0006 应用不可撤的既有口径)。
    fail(e, '回执发送失败,请稍后再试')
  }
}

/** 确认卡:整批批准应用(不做逐项勾选)。 */
async function approvePending(): Promise<void> {
  const current = pending.value
  if (current === null) {
    return
  }
  pending.value = null
  await applyAndReceipt(current.callId, current.name, current.args)
}

async function rejectPending(): Promise<void> {
  const current = pending.value
  if (current === null) {
    return
  }
  pending.value = null
  // 拒绝回执如实告知 LLM(「用户拒绝了这批变更」),由它决定后续话术。
  await sendReceipt({ call_id: current.callId, status: 'rejected' })
}

// ---- 会话流 ----

async function send(): Promise<void> {
  const text = input.value.trim()
  if (text === '' || streaming.value || activeId.value === null) {
    return
  }
  if (activeModel.value === '') {
    error.value = '没有可用的聊天模型,请先在管理端配置 token 计价的模型'
    return
  }
  error.value = ''
  streaming.value = true
  streamText.value = ''
  toolSteps.value = []
  input.value = ''
  const before = messages.value
  messages.value = [...before, { role: 'user', content: text }]
  try {
    const result = await client.assistantStream(
      props.canvasId,
      { session_id: activeId.value, message: text, model: activeModel.value },
      {
        onDelta: (d) => {
          streamText.value += d
        },
        onToolCall: onToolCall,
        onToolResult: onToolResult,
      },
    )
    // done:乐观 user 轮 + 最终回答全文 + 工具摘要行(持久由服务端完成,
    // 本地渲染同形)。
    messages.value = appendAssistantTurn(before, text, { content: result.text, summary: result.summary })
    await refreshSessions() // 标题可能已生成
  } catch (e) {
    fail(e, '助手会话失败,请稍后再试')
    // 流中断(断线/回执超时)服务端不持久本轮:撤回乐观轮,输入框保留
    // 原文供重试整轮重放。
    messages.value = before
    input.value = text
  } finally {
    streaming.value = false
    streamText.value = ''
    pending.value = null
  }
}

</script>

<template>
  <aside
    class="assistant-panel"
    aria-label="画布助手"
  >
    <header class="panel-head">
      <h3>画布助手</h3>
      <div class="head-actions">
        <button
          class="close"
          type="button"
          @click="emit('close')"
        >
          关闭
        </button>
      </div>
    </header>

    <div class="session-row">
      <select
        class="session-select"
        :value="activeId ?? ''"
        :disabled="streaming || sessions.length === 0"
        @change="openSession(Number(($event.target as HTMLSelectElement).value))"
      >
        <option
          v-if="sessions.length === 0"
          value=""
        >
          暂无会话
        </option>
        <option
          v-for="s in sessions"
          :key="s.id"
          :value="s.id"
        >
          {{ s.title || `会话 ${s.id}` }}
        </option>
      </select>
      <button
        class="ghost"
        type="button"
        :disabled="streaming"
        title="新建会话"
        @click="createSession"
      >
        新建
      </button>
      <button
        class="ghost"
        type="button"
        :disabled="streaming || activeId === null"
        title="删除当前会话"
        @click="deleteSession"
      >
        删除
      </button>
    </div>

    <div class="controls-row">
      <label class="control">
        <span>授权</span>
        <select
          class="mode-select"
          :value="mode"
          @change="switchMode"
        >
          <option value="ask">先询问我</option>
          <option value="full">完全授权</option>
        </select>
      </label>
      <label class="control">
        <span>模型</span>
        <select
          v-model="model"
          class="model-select"
          :disabled="promptModels.length === 0"
        >
          <option
            v-if="promptModels.length === 0"
            value=""
          >无可用模型</option>
          <option
            v-for="m in promptModels"
            :key="m"
            :value="m"
          >{{ m }}</option>
        </select>
      </label>
    </div>
    <p
      v-if="promptModels.length === 0"
      class="catalog-missing"
    >
      聊天模型目录为空或未加载:请确认管理端已配置 token 计价的聊天模型后刷新。
    </p>

    <p
      v-if="error"
      class="panel-error"
    >
      {{ error }}
    </p>

    <div
      ref="threadEl"
      class="thread"
    >
      <!-- 92 号票接缝:新会话首屏 = 桌宠问候语 + 建议 chips(点击预填
           输入框不发送;「换一批」轮换)。无配置回内置兜底文案。 -->
      <div
        v-if="messages.length === 0 && !streaming && !loadingThread"
        class="panel-empty"
        data-testid="assistant-empty-state"
      >
        <p class="greeting">
          {{ props.greeting?.trim() || '新的画布助手会话。让它读取画布结构、批量建节点、改剧本,或在完全授权档下直接提交生成任务。' }}
        </p>
        <div
          v-if="visibleChips.length > 0"
          class="chip-row"
        >
          <button
            v-for="chip in visibleChips"
            :key="chip"
            class="chip"
            type="button"
            :disabled="activeId === null || streaming"
            :title="'填入输入框(不直接发送)'"
            @click="prefillChip(chip)"
          >
            {{ chip }}
          </button>
          <button
            v-if="chipsShuffleable"
            class="chip shuffle"
            type="button"
            title="换一批建议指令"
            @click="chipPage += 1"
          >
            换一批
          </button>
        </div>
      </div>
      <div
        v-for="(m, i) in messages"
        :key="i"
        class="turn"
        :data-role="m.role"
      >
        <p class="bubble">
          {{ m.content }}
        </p>
        <p
          v-if="m.summary"
          class="turn-summary"
        >
          {{ m.summary }}
        </p>
      </div>
      <div
        v-if="streaming"
        class="turn"
        data-role="assistant"
      >
        <div
          v-if="toolSteps.length > 0"
          class="tool-timeline"
        >
          <div
            v-for="step in toolSteps"
            :key="step.id"
            class="tool-step"
            :data-state="step.state"
          >
            <span class="tool-step-dot" />
            <span class="tool-step-name">{{ agentToolLabel(step.name) }}</span>
            <span class="tool-step-hint">{{ step.summary ?? (step.state === 'running' && pending?.callId === step.id ? '等待你确认…' : '') }}</span>
          </div>
        </div>
        <p
          v-if="streamText"
          class="bubble"
        >
          {{ streamText }}
        </p>
        <p
          v-else
          class="bubble streaming"
        >
          …
        </p>
      </div>
    </div>

    <div
      v-if="pending"
      class="change-card"
      data-testid="assistant-change-card"
    >
      <p class="card-headline">
        {{ pending.headline }}
      </p>
      <ul
        v-if="pending.lines.length > 0"
        class="card-lines"
      >
        <li
          v-for="(line, i) in pending.lines"
          :key="i"
        >
          {{ line }}
        </li>
      </ul>
      <div class="card-actions">
        <button
          class="primary"
          type="button"
          @click="approvePending"
        >
          批准应用
        </button>
        <button
          class="ghost"
          type="button"
          @click="rejectPending"
        >
          拒绝
        </button>
      </div>
    </div>

    <div class="composer">
      <textarea
        v-model="input"
        rows="2"
        :placeholder="activeId === null ? '先新建或选择一个会话' : (streaming ? '助手回复中…' : (mode === 'full' ? '完全授权:写操作将直接应用' : '先询问我:写操作会先出确认卡'))"
        :disabled="streaming || activeId === null"
        @keydown.enter.exact.prevent="send"
      />
      <button
        class="primary send"
        type="button"
        :disabled="streaming || activeId === null || input.trim() === ''"
        @click="send"
      >
        发送
      </button>
    </div>
  </aside>
</template>

<style scoped>
/* 三面板范式逐字同构(AssetPanel.vue:378-393):右缘 380 宽、--z-side-panel
   档、panel-in 入场。 */
.assistant-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: var(--w-panel);
  display: flex;
  flex-direction: column;
  gap: var(--sp-md);
  padding: var(--sp-lg);
  background: rgba(var(--surface-panel-rgb), 0.96);
  border-left: 1px solid var(--line-control);
  box-shadow: var(--shadow-slide);
  z-index: var(--z-side-panel);
  overflow: hidden;
  animation: panel-in var(--dur-slow) var(--ease);
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.panel-head h3 {
  margin: 0;
  font-size: var(--fs-title);
}

.head-actions {
  display: flex;
  gap: var(--sp-sm);
}

.panel-head button,
.session-row button,
.card-actions button,
.composer .send {
  border: none;
  border-radius: var(--r-md);
  padding: 7px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
}

.panel-head button:disabled,
.session-row button:disabled,
.composer .send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.close {
  background: transparent;
  color: var(--text-secondary);
}

.close:hover {
  background: var(--wash-soft);
  color: var(--text-primary);
}

.session-row {
  display: flex;
  gap: var(--sp-sm);
  align-items: center;
}

.session-select {
  flex: 1;
  min-width: 0;
}

select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 7px 10px;
  color: inherit;
  font-size: var(--fs-label);
}

select:focus {
  border-color: rgba(var(--accent-rgb), 0.85);
  outline: none;
}

.ghost {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
}

.ghost:hover:not(:disabled) {
  border-color: var(--line-action);
  background: var(--wash-soft);
  color: var(--text-primary);
}

.controls-row {
  display: flex;
  gap: var(--sp-md);
}

.control {
  display: flex;
  align-items: center;
  gap: var(--sp-sm);
  font-size: var(--fs-label);
  color: var(--text-muted);
  min-width: 0;
}

.control select {
  min-width: 0;
  flex: 1;
}

.catalog-missing {
  margin: 0;
  font-size: var(--fs-micro);
  color: var(--text-muted);
}

.panel-error {
  margin: 0;
  padding: 8px 10px;
  border-radius: var(--r-md);
  /* --danger-rgb 实存于 style.css :root(失败绯 #ff8f8f);此前带的
     220,80,80 是编造的死 fallback,与 token 色不符(评审修复去掉)。 */
  background: rgba(var(--danger-rgb), 0.12);
  color: var(--danger);
  font-size: var(--fs-label);
  overflow-wrap: anywhere;
}

.thread {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--sp-md);
  padding-right: 2px;
}

.panel-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
  line-height: 1.6;
}

/* 92 号票接缝:问候语与建议 chips。chip 点击只预填输入框(票面定案),
   视觉上与生成对话框的玩法预设 chip 同族(描边小按钮)。 */
.panel-empty .greeting {
  margin: 0;
  white-space: pre-wrap;
}

.chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: var(--sp-sm);
}

.chip {
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  background: var(--wash-soft);
  color: var(--text-secondary);
  font-size: var(--fs-label);
  padding: 4px 10px;
  cursor: pointer;
  transition:
    border-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.chip:hover:not(:disabled) {
  border-color: var(--line-action);
  color: var(--text-primary);
}

.chip:disabled {
  opacity: 0.55;
  cursor: default;
}

.chip.shuffle {
  border-style: dashed;
  color: var(--text-muted);
}

.turn {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.turn[data-role='user'] {
  align-items: flex-end;
}

.turn[data-role='assistant'] {
  align-items: flex-start;
}

.bubble {
  margin: 0;
  max-width: 92%;
  padding: 8px 12px;
  border-radius: var(--r-lg);
  background: var(--wash-soft);
  font-size: var(--fs-body);
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.turn[data-role='user'] .bubble {
  background: rgba(var(--accent-rgb), 0.14);
}

.bubble.streaming {
  color: var(--text-muted);
}

.turn-summary {
  margin: 0;
  max-width: 92%;
  font-size: var(--fs-micro);
  color: var(--text-muted);
  overflow-wrap: anywhere;
}

/* 工具过程时间线(.tool-timeline 族)已收编 style.css 全局共享段
   (91/92 号票评审:与 AgentNode 的逐字拷贝归一,89 号票收编先例),
   本组件只引用共享类;running 脉冲的常动守卫随共享段。 */

/* 「先询问我」变更清单确认卡:整批批准/拒绝,不做逐项勾选。 */
.change-card {
  border: 1px solid rgba(var(--accent-rgb), 0.5);
  border-radius: var(--r-lg);
  padding: var(--sp-md);
  display: flex;
  flex-direction: column;
  gap: var(--sp-sm);
  background: var(--wash-soft);
}

.card-headline {
  margin: 0;
  font-size: var(--fs-body);
  /* 确认卡标题是卡面小标题不是主按钮,600 档(DESIGN.md 字档
     400/600/700;700 是主按钮/品牌展示的档位,评审修复归档)。 */
  font-weight: 600;
  color: var(--text-primary);
}

.card-lines {
  margin: 0;
  padding-left: 18px;
  display: grid;
  gap: 2px;
  font-size: var(--fs-label);
  color: var(--text-muted);
  max-height: 140px;
  overflow-y: auto;
}

.card-actions {
  display: flex;
  gap: var(--sp-sm);
}

.primary {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 700;
}

.primary:hover:not(:disabled) {
  background: var(--accent-soft);
}

.composer {
  display: flex;
  gap: var(--sp-sm);
  align-items: flex-end;
}

.composer textarea {
  flex: 1;
  min-width: 0;
  resize: none;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 8px 10px;
  color: inherit;
  font-size: var(--fs-body);
  font-family: inherit;
  line-height: 1.5;
  transition: border-color var(--dur-fast) var(--ease);
}

.composer textarea:focus {
  border-color: rgba(var(--accent-rgb), 0.85);
  outline: none;
}

.composer textarea:disabled {
  opacity: 0.6;
}
</style>
