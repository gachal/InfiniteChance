<script setup lang="ts">
// 用量审计(15 号票):网关管理区的请求级日志列表与按天/模型/渠道汇总。
// 列表与汇总共用同一套过滤(时间/key/渠道/模型/状态/来源),后端用同一
// WHERE 落查询,两边的请求数与扣费天然对账一致。失败行带上游错误摘要,
// 画布来源标记单列区分;分页靠过滤后总数 total 驱动。
import { computed, onMounted, ref } from 'vue'

import {
  ApiError,
  type ApiKeyRecord,
  type Channel,
  type UsageBucket,
  type UsageLogRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const { client } = useAuth()

// 视图模式:请求明细,或三种汇总维度;过滤条对四种模式一体生效。
type Mode = 'logs' | 'day' | 'model' | 'channel'
const modes: { key: Mode; label: string }[] = [
  { key: 'logs', label: '请求明细' },
  { key: 'day', label: '按天汇总' },
  { key: 'model', label: '按模型汇总' },
  { key: 'channel', label: '按渠道汇总' },
]
const mode = ref<Mode>('logs')

const fromInput = ref('')
const toInput = ref('')
// 下拉选中值是 option 的字符串 value,转数字发生在组装 API 参数时。
const keyFilter = ref('')
const channelFilter = ref('')
const modelInput = ref('')
const statusFilter = ref<'' | 'success' | 'upstream_error'>('')
const sourceFilter = ref<'' | 'canvas' | 'direct'>('')

const keys = ref<ApiKeyRecord[]>([])
const channels = ref<Channel[]>([])

const logs = ref<UsageLogRecord[]>([])
const total = ref(0)
const offset = ref(0)
const buckets = ref<UsageBucket[]>([])
const loading = ref(false)
const error = ref('')

const pageSize = 50

const hasPrevious = computed(() => offset.value > 0)
const hasNext = computed(() => offset.value + pageSize < total.value)

// 过滤条件 → API 参数;datetime-local 按本地时区解释,转成 RFC3339 上送。
function auditParams() {
  return {
    from: fromInput.value ? new Date(fromInput.value).toISOString() : undefined,
    to: toInput.value ? new Date(toInput.value).toISOString() : undefined,
    key_id: keyFilter.value === '' ? undefined : Number(keyFilter.value),
    channel_id: channelFilter.value === '' ? undefined : Number(channelFilter.value),
    model: modelInput.value.trim() || undefined,
    status: statusFilter.value || undefined,
    source: sourceFilter.value || undefined,
  }
}

async function refresh(nextOffset = 0): Promise<void> {
  loading.value = true
  error.value = ''
  offset.value = nextOffset
  try {
    if (mode.value === 'logs') {
      const page = await client.listUsageLogs({ ...auditParams(), limit: pageSize, offset: nextOffset })
      logs.value = page.logs
      total.value = page.total
    } else {
      buckets.value = await client.usageSummary({ ...auditParams(), by: mode.value })
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载用量数据,请稍后再试'
  } finally {
    loading.value = false
  }
}

function setMode(next: Mode): void {
  if (mode.value === next) {
    return
  }
  mode.value = next
  void refresh(0)
}

function search(): void {
  void refresh(0)
}

const keyName = computed(() => {
  const map = new Map(keys.value.map((k) => [k.id, k.name]))
  return (id: number) => map.get(id) ?? `#${id}`
})

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(ms: number): string {
  if (ms <= 0) {
    return '—'
  }
  if (ms < 1000) {
    return `${ms} ms`
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(1)} s`
  }
  return `${(ms / 60_000).toFixed(1)} min`
}

function formatUSD(usd: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(usd)
}

// 计费单位与数量:token 轨读列,按次/按秒轨读快照里的请求事实(张/秒)。
function formatQuantity(l: UsageLogRecord): string {
  if (l.unit === 'token') {
    return `${l.prompt_tokens} + ${l.completion_tokens} tok`
  }
  const n = l.request?.n
  const amount = n === undefined || n === null ? '—' : String(n)
  if (l.unit === 'call') {
    return `${amount} 张${l.request?.size ? ` · ${l.request.size}` : ''}`
  }
  return `${amount} 秒${l.request?.size ? ` · ${l.request.size}` : ''}`
}

function unitLabel(unit: string): string {
  return unit === 'token' ? 'token' : unit === 'call' ? '按次' : '按秒'
}

// 来源标记:画布侧解析出画布 id(完整标记悬停可见),空 = 直连流量;
// 其它自报标记原样显示。
function sourceLabel(l: UsageLogRecord): string {
  if (!l.source) {
    return '直连'
  }
  const m = /canvas=(\d+)/.exec(l.source)
  return m ? `画布 ${m[1]}` : l.source
}

function bucketLabel(b: UsageBucket): string {
  if ('day' in b) {
    return b.day
  }
  if ('model' in b) {
    return b.model
  }
  return b.channel_name || `渠道 #${b.channel_id}`
}

onMounted(() => {
  void refresh()
  void client
    .listKeys()
    .then((list) => {
      keys.value = list
    })
    .catch(() => {
      /* key 下拉拉不到就不影响主列表 */
    })
  void client
    .listChannels()
    .then((list) => {
      channels.value = list
    })
    .catch(() => {
      /* 渠道下拉拉不到就不影响主列表 */
    })
})
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>用量审计</h2>
      <div class="mode-filter">
        <button
          v-for="m in modes"
          :key="m.key"
          type="button"
          :class="{ active: mode === m.key }"
          @click="setMode(m.key)"
        >
          {{ m.label }}
        </button>
      </div>
    </div>

    <form
      class="filters"
      @submit.prevent="search"
    >
      <label>
        从
        <input
          v-model="fromInput"
          type="datetime-local"
        >
      </label>
      <label>
        到
        <input
          v-model="toInput"
          type="datetime-local"
        >
      </label>
      <select
        v-model="keyFilter"
        aria-label="按 key 过滤"
      >
        <option value="">
          全部 key
        </option>
        <option
          v-for="k in keys"
          :key="k.id"
          :value="String(k.id)"
        >
          {{ k.name }}
        </option>
      </select>
      <select
        v-model="channelFilter"
        aria-label="按渠道过滤"
      >
        <option value="">
          全部渠道
        </option>
        <option
          v-for="ch in channels"
          :key="ch.id"
          :value="String(ch.id)"
        >
          {{ ch.name }}
        </option>
      </select>
      <input
        v-model="modelInput"
        type="text"
        placeholder="公开模型(精确)"
        aria-label="按模型过滤"
        class="model-input"
      >
      <select
        v-model="statusFilter"
        aria-label="按状态过滤"
      >
        <option value="">
          全部状态
        </option>
        <option value="success">
          成功
        </option>
        <option value="upstream_error">
          失败
        </option>
      </select>
      <select
        v-model="sourceFilter"
        aria-label="按来源过滤"
      >
        <option value="">
          全部来源
        </option>
        <option value="canvas">
          画布
        </option>
        <option value="direct">
          直连
        </option>
      </select>
      <button
        type="submit"
        class="ghost"
        :disabled="loading"
      >
        {{ loading ? '查询中…' : '查询' }}
      </button>
    </form>

    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- 请求明细:时间/key/渠道/模型/单位与数量/耗时/状态/扣费/来源 -->
    <template v-if="mode === 'logs'">
      <p
        v-if="loading && logs.length === 0"
        class="muted"
      >
        正在加载用量日志…
      </p>
      <p
        v-else-if="logs.length === 0"
        class="muted"
      >
        没有符合条件的用量记录。
      </p>
      <div
        v-else
        class="ledger"
      >
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>Key</th>
              <th>渠道</th>
              <th>模型</th>
              <th>用量</th>
              <th>耗时</th>
              <th>状态</th>
              <th>扣费</th>
              <th>来源</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="l in logs"
              :key="l.id"
            >
              <td>{{ formatTime(l.created_at) }}</td>
              <td>{{ keyName(l.key_id) }}</td>
              <td>{{ l.channel_name }}</td>
              <td>
                {{ l.public_model }}
                <span
                  v-if="l.upstream_model !== l.public_model"
                  class="upstream"
                  :title="`上游模型 ${l.upstream_model}`"
                >↗</span>
              </td>
              <td :title="`${unitLabel(l.unit)} · ${formatQuantity(l)}`">
                {{ formatQuantity(l) }}
              </td>
              <td>{{ formatDuration(l.duration_ms) }}</td>
              <td>
                <span
                  class="badge"
                  :class="l.status === 'success' ? 'ok' : 'revoked'"
                >{{ l.status === 'success' ? '成功' : '失败' }}</span>
                <div
                  v-if="l.upstream_error"
                  class="error-summary"
                  :title="l.upstream_error"
                >
                  {{ l.upstream_error }}
                </div>
              </td>
              <td>{{ formatUSD(l.charge_usd) }}</td>
              <td>
                <span
                  class="badge"
                  :class="l.source ? 'active' : 'off'"
                  :title="l.source || '直连流量'"
                >{{ sourceLabel(l) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="pagination">
          <span class="muted">共 {{ total }} 条 · 第 {{ Math.floor(offset / pageSize) + 1 }} 页</span>
          <button
            type="button"
            class="ghost"
            :disabled="loading || !hasPrevious"
            @click="refresh(offset - pageSize)"
          >
            上一页
          </button>
          <button
            type="button"
            class="ghost"
            :disabled="loading || !hasNext"
            @click="refresh(offset + pageSize)"
          >
            下一页
          </button>
        </div>
      </div>
    </template>

    <!-- 汇总:与明细同一套过滤,请求数/失败/扣费与明细对账一致 -->
    <template v-else>
      <p
        v-if="loading && buckets.length === 0"
        class="muted"
      >
        正在加载汇总…
      </p>
      <p
        v-else-if="buckets.length === 0"
        class="muted"
      >
        没有符合条件的用量记录。
      </p>
      <div
        v-else
        class="ledger"
      >
        <table>
          <thead>
            <tr>
              <th>{{ mode === 'day' ? '日期' : mode === 'model' ? '模型' : '渠道' }}</th>
              <th>请求数</th>
              <th>失败</th>
              <th>扣费合计</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(b, i) in buckets"
              :key="i"
            >
              <td>{{ bucketLabel(b) }}</td>
              <td>{{ b.requests }}</td>
              <td :class="b.errors > 0 ? 'fail-count' : ''">
                {{ b.errors }}
              </td>
              <td>{{ formatUSD(b.charge_usd) }}</td>
            </tr>
          </tbody>
        </table>
        <p class="muted hint">
          汇总与请求明细使用同一套过滤条件,数字逐条对账一致。按天汇总的自然日以数据库时区(UTC)为准。
        </p>
      </div>
    </template>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* 用量审计私有样式:过滤条、模式切换、日志表与分页。 */
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.filters label {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #8b91a7;
  font-size: 13px;
}

.filters input,
.filters select {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 7px 10px;
  color: inherit;
  font-size: 13px;
}

.model-input {
  width: 170px;
}

.mode-filter {
  display: flex;
  gap: 4px;
}

.mode-filter button {
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: transparent;
  color: #aab1c5;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}

.mode-filter button.active {
  background: rgba(76, 110, 245, 0.25);
  color: inherit;
  border-color: rgba(76, 110, 245, 0.6);
}

.ledger table {
  width: 100%;
  margin-top: 6px;
  border-collapse: collapse;
  font-size: 13px;
}

.ledger th,
.ledger td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  vertical-align: top;
}

.ledger th {
  color: #8b91a7;
  font-weight: 500;
  font-size: 12px;
  white-space: nowrap;
}

.upstream {
  color: #5b627a;
  cursor: help;
}

.error-summary {
  margin-top: 4px;
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #ff8f8f;
  font-size: 12px;
}

.fail-count {
  color: #ff8f8f;
}

.pagination {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
}

.pagination .muted {
  margin-right: auto;
}

.hint {
  margin-top: 10px;
  font-size: 12px;
}
</style>
