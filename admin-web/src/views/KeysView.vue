<script setup lang="ts">
// API Key 管理:创建(完整值仅显示一次)、吊销、过期、手工充值与额度流水。
import { computed, onMounted, reactive, ref } from 'vue'

import {
  ApiError,
  type ApiKeyRecord,
  type CreatedApiKey,
  type QuotaEntry,
} from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const auth = useAuth()

const keys = ref<ApiKeyRecord[]>([])
const loading = ref(false)
const error = ref('')

// ---- 创建表单与一次性展示 ----
const showForm = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({ name: '', expiresAt: '', initialQuota: '' })
const createdKey = ref<CreatedApiKey | null>(null)
const copied = ref(false)

// ---- 充值与流水 ----
const topupTarget = ref<ApiKeyRecord | null>(null)
const topupAmount = ref('')
const toppingUp = ref(false)
const topupError = ref('')
const logTarget = ref<ApiKeyRecord | null>(null)
const logEntries = ref<QuotaEntry[]>([])
const logError = ref('')

const statusLabel: Record<string, string> = {
  active: '有效',
  revoked: '已吊销',
  expired: '已过期',
}

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    keys.value = await auth.client.listKeys()
  } catch (e) {
    error.value = authErrorMessage(e)
  } finally {
    loading.value = false
  }
}

onMounted(() => void refresh())

// ---- 创建 ----

function openCreate(): void {
  createdKey.value = null
  Object.assign(form, { name: '', expiresAt: '', initialQuota: '' })
  formError.value = ''
  showForm.value = true
}

function closeCreate(): void {
  showForm.value = false
  createdKey.value = null
}

async function submit(): Promise<void> {
  if (saving.value) {
    return
  }
  saving.value = true
  formError.value = ''
  try {
    const input: { name: string; expires_at?: string; initial_quota_usd?: number } = {
      name: form.name.trim(),
    }
    if (form.expiresAt !== '') {
      input.expires_at = new Date(form.expiresAt).toISOString()
    }
    if (form.initialQuota.trim() !== '') {
      input.initial_quota_usd = Number(form.initialQuota)
    }
    createdKey.value = await auth.client.createKey(input)
    await refresh()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e)
  } finally {
    saving.value = false
  }
}

async function copyCreated(): Promise<void> {
  if (createdKey.value === null) {
    return
  }
  try {
    await navigator.clipboard.writeText(createdKey.value.key)
    copied.value = true
  } catch {
    // 剪贴板不可用(非安全上下文等):保持原文可见,让用户手动复制。
  }
}

// ---- 吊销 ----

async function revoke(key: ApiKeyRecord): Promise<void> {
  if (!window.confirm(`确定吊销 key「${key.name}」(${key.prefix}…)?使用它的请求将立即被拒绝。`)) {
    return
  }
  error.value = ''
  try {
    await auth.client.revokeKey(key.id)
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

// ---- 充值 ----

function openTopUp(key: ApiKeyRecord): void {
  topupTarget.value = key
  topupAmount.value = ''
  topupError.value = ''
}

async function submitTopUp(): Promise<void> {
  const target = topupTarget.value
  if (target === null || toppingUp.value) {
    return
  }
  const amount = Number(topupAmount.value)
  if (!Number.isFinite(amount) || amount <= 0) {
    topupError.value = '请输入大于 0 的金额'
    return
  }
  toppingUp.value = true
  topupError.value = ''
  try {
    const updated = await auth.client.topUpKey(target.id, amount)
    topupTarget.value = null
    // 充值后余额即时可见:列表原地替换,不打断当前视图。
    keys.value = keys.value.map((k) => (k.id === updated.id ? updated : k))
  } catch (e) {
    topupError.value = e instanceof ApiError ? e.message : authErrorMessage(e)
  } finally {
    toppingUp.value = false
  }
}

// ---- 额度流水 ----

const reasonLabel: Record<string, string> = {
  initial: '初始额度',
  manual_topup: '手工充值',
}

async function openLog(key: ApiKeyRecord): Promise<void> {
  logTarget.value = key
  logEntries.value = []
  logError.value = ''
  try {
    logEntries.value = await auth.client.keyQuotaLog(key.id)
  } catch (e) {
    logError.value = authErrorMessage(e)
  }
}

// ---- 展示辅助 ----

function formatUSD(usd: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(usd)
}

function formatTime(iso: string | null): string {
  if (iso === null) {
    return '—'
  }
  return new Date(iso).toLocaleString()
}

const formHint = computed(() =>
  form.expiresAt === '' ? '永不过期' : `过期时间:${new Date(form.expiresAt).toLocaleString()}`,
)
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>API Key 管理</h2>
      <button
        type="button"
        class="primary"
        @click="openCreate"
      >
        新建 Key
      </button>
    </div>

    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>

    <section
      v-if="showForm"
      class="card"
    >
      <!-- 创建结果:完整值仅此一次展示 -->
      <template v-if="createdKey">
        <div class="card-head">
          <h3>Key 已创建,请立即保存</h3>
          <button
            type="button"
            class="ghost"
            @click="closeCreate"
          >
            我已保存
          </button>
        </div>
        <p class="warn">
          完整 key 只显示这一次,关闭后无法再次查看,请复制保存。
        </p>
        <div class="reveal">
          <code>{{ createdKey.key }}</code>
          <button
            type="button"
            class="ghost"
            @click="copyCreated"
          >
            {{ copied ? '已复制' : '复制' }}
          </button>
        </div>
      </template>

      <!-- 创建表单 -->
      <template v-else>
        <div class="card-head">
          <h3>新建 Key</h3>
          <button
            type="button"
            class="ghost"
            @click="showForm = false"
          >
            取消
          </button>
        </div>
        <form
          class="grid"
          @submit.prevent="submit"
        >
          <label>
            <span>名称</span>
            <input
              v-model="form.name"
              type="text"
              required
              placeholder="例如 canvas-service"
            >
          </label>
          <label>
            <span>过期时间(留空 = 永不过期)</span>
            <input
              v-model="form.expiresAt"
              type="datetime-local"
            >
          </label>
          <label>
            <span>初始额度(USD)</span>
            <input
              v-model="form.initialQuota"
              type="number"
              min="0"
              step="0.01"
              placeholder="0.00"
            >
          </label>
          <p class="hint">
            {{ formHint }}
          </p>

          <p
            v-if="formError"
            class="error wide"
            role="alert"
          >
            {{ formError }}
          </p>
          <div class="wide actions">
            <button
              type="submit"
              class="primary"
              :disabled="saving"
            >
              {{ saving ? '创建中…' : '创建' }}
            </button>
          </div>
        </form>
      </template>
    </section>

    <!-- 充值面板 -->
    <section
      v-if="topupTarget"
      class="card"
    >
      <div class="card-head">
        <h3>给「{{ topupTarget.name }}」({{ topupTarget.prefix }}…)充值</h3>
        <button
          type="button"
          class="ghost"
          @click="topupTarget = null"
        >
          取消
        </button>
      </div>
      <form
        class="grid"
        @submit.prevent="submitTopUp"
      >
        <label>
          <span>充值金额(USD)</span>
          <input
            v-model="topupAmount"
            type="number"
            min="0.01"
            step="0.01"
            autofocus
          >
        </label>
        <p
          v-if="topupError"
          class="error"
          role="alert"
        >
          {{ topupError }}
        </p>
        <div class="wide actions">
          <button
            type="submit"
            class="primary"
            :disabled="toppingUp"
          >
            {{ toppingUp ? '充值中…' : '确认充值' }}
          </button>
        </div>
      </form>
    </section>

    <p
      v-if="loading"
      class="muted"
    >
      正在加载 key…
    </p>
    <p
      v-else-if="keys.length === 0"
      class="muted"
    >
      还没有 key。点击「新建 Key」发放第一把。
    </p>

    <section
      v-for="key in keys"
      :key="key.id"
      class="card"
    >
      <div class="card-head">
        <h3>
          {{ key.name }}
          <span
            class="badge"
            :class="key.status"
          >{{ statusLabel[key.status] ?? key.status }}</span>
        </h3>
        <div class="row-actions">
          <button
            type="button"
            class="primary"
            @click="openTopUp(key)"
          >
            充值
          </button>
          <button
            type="button"
            class="ghost"
            @click="openLog(key)"
          >
            额度记录
          </button>
          <button
            type="button"
            class="danger"
            :disabled="key.status === 'revoked'"
            @click="revoke(key)"
          >
            吊销
          </button>
        </div>
      </div>

      <dl class="fields">
        <div>
          <dt>Key</dt>
          <dd><code>{{ key.prefix }}…</code></dd>
        </div>
        <div>
          <dt>余额</dt>
          <dd class="balance">
            {{ formatUSD(key.quota_usd) }}
          </dd>
        </div>
        <div>
          <dt>过期时间</dt>
          <dd>{{ formatTime(key.expires_at) }}</dd>
        </div>
        <div>
          <dt>创建时间</dt>
          <dd>{{ formatTime(key.created_at) }}</dd>
        </div>
      </dl>

      <div
        v-if="logTarget && logTarget.id === key.id"
        class="ledger"
      >
        <div class="ledger-head">
          <h4>额度记录</h4>
          <button
            type="button"
            class="ghost"
            @click="logTarget = null"
          >
            收起
          </button>
        </div>
        <p
          v-if="logError"
          class="error"
          role="alert"
        >
          {{ logError }}
        </p>
        <p
          v-else-if="logEntries.length === 0"
          class="muted"
        >
          暂无记录
        </p>
        <table v-else>
          <thead>
            <tr>
              <th>时间</th>
              <th>变动</th>
              <th>变动后余额</th>
              <th>原因</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="entry in logEntries"
              :key="entry.id"
            >
              <td>{{ formatTime(entry.created_at) }}</td>
              <td :class="entry.delta_usd >= 0 ? 'delta-plus' : 'delta-minus'">
                {{ entry.delta_usd >= 0 ? '+' : '' }}{{ formatUSD(entry.delta_usd) }}
              </td>
              <td>{{ formatUSD(entry.balance_usd) }}</td>
              <td>{{ reasonLabel[entry.reason] ?? entry.reason }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* Key 视图私有样式:一次性完整值展示、充值提示与额度流水表。 */
.hint {
  align-self: end;
  margin: 0;
  padding-bottom: 10px;
  color: #8b91a7;
  font-size: 13px;
}

.warn {
  color: #ffb340;
  font-size: 14px;
  margin: 12px 0 0;
}

.reveal {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 159, 10, 0.35);
  border-radius: 10px;
  padding: 12px 14px;
}

.reveal code {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.balance {
  font-weight: 600;
}

.ledger {
  margin-top: 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding-top: 12px;
}

.ledger-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ledger-head h4 {
  margin: 0;
  font-size: 14px;
}

.ledger .muted {
  margin: 10px 0 0;
}

.ledger table {
  width: 100%;
  margin-top: 10px;
  border-collapse: collapse;
  font-size: 13px;
}

.ledger th,
.ledger td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.ledger th {
  color: #8b91a7;
  font-weight: 500;
  font-size: 12px;
}

.delta-plus {
  color: #4cd787;
}

.delta-minus {
  color: #ff8f8f;
}
</style>
