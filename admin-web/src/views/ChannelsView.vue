<script setup lang="ts">
// 渠道管理:厂商渠道的增删改查 + 一键连通测试。
// 厂商密钥只在创建/编辑时提交;列表仅显示 has_key 与尾号提示。
import { computed, onMounted, reactive, ref } from 'vue'

import { ApiError, type Channel, type ChannelTestResult } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const auth = useAuth()

const channels = ref<Channel[]>([])
const loading = ref(false)
const error = ref('')

const showForm = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')

const testingId = ref<number | null>(null)
const testResults = reactive<Record<number, ChannelTestResult>>({})

interface ChannelForm {
  name: string
  type: string
  baseUrl: string
  apiKey: string
  secretId: string
  secretKey: string
  subAppId: string
  region: string
  mappings: { from: string; to: string }[]
  capChat: boolean
  capImages: boolean
  capVideos: boolean
  priority: number
  weight: number
  enabled: boolean
}

function blankForm(): ChannelForm {
  return {
    name: '',
    type: 'openai',
    baseUrl: '',
    apiKey: '',
    secretId: '',
    secretKey: '',
    subAppId: '',
    region: '',
    mappings: [{ from: '', to: '' }],
    capChat: true,
    capImages: false,
    capVideos: false,
    priority: 0,
    weight: 1,
    enabled: true,
  }
}

const isVod = computed(() => form.type === 'tencent-vod')

const form = reactive<ChannelForm>(blankForm())

// 编辑 tencent-vod 渠道时展示已存敏感键的尾号提示(值本身只写不读)。
const editingHints = ref('')

const formTitle = computed(() => (editingId.value === null ? '新建渠道' : '编辑渠道'))

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    channels.value = await auth.client.listChannels()
  } catch (e) {
    error.value = authErrorMessage(e)
  } finally {
    loading.value = false
  }
}

onMounted(() => void refresh())

function openCreate(): void {
  editingId.value = null
  Object.assign(form, blankForm())
  editingHints.value = ''
  formError.value = ''
  showForm.value = true
}

function openEdit(ch: Channel): void {
  editingId.value = ch.id
  const mappings = Object.entries(ch.model_map).map(([from, to]) => ({ from, to }))
  Object.assign(form, {
    name: ch.name,
    type: ch.type,
    baseUrl: ch.base_url,
    apiKey: '', // 留空 = 保留已存密钥
    secretId: '', // 敏感键:留空 = 保留已存值(hint 见表单提示)
    secretKey: '',
    subAppId: ch.config?.sub_app_id ?? '',
    region: ch.config?.region ?? '',
    mappings: mappings.length > 0 ? mappings : [{ from: '', to: '' }],
    capChat: ch.capabilities.includes('chat'),
    capImages: ch.capabilities.includes('images'),
    capVideos: ch.capabilities.includes('videos'),
    priority: ch.priority,
    weight: ch.weight,
    enabled: ch.enabled,
  } satisfies ChannelForm)
  editingHints.value = Object.entries(ch.config_hints ?? {})
    .map(([k, hint]) => `${k} ${hint}`)
    .join('、')
  formError.value = ''
  showForm.value = true
}

function closeForm(): void {
  showForm.value = false
  editingId.value = null
  editingHints.value = ''
  formError.value = ''
}

function addMapping(): void {
  form.mappings.push({ from: '', to: '' })
}

function removeMapping(index: number): void {
  form.mappings.splice(index, 1)
}

function buildInput() {
  const modelMap: Record<string, string> = {}
  for (const { from, to } of form.mappings) {
    if (from.trim() !== '' && to.trim() !== '') {
      modelMap[from.trim()] = to.trim()
    }
  }
  const capabilities: string[] = []
  if (form.capChat) capabilities.push('chat')
  if (form.capImages) capabilities.push('images')
  if (form.capVideos) capabilities.push('videos')
  const input: Parameters<typeof auth.client.createChannel>[0] = {
    name: form.name.trim(),
    type: form.type,
    base_url: form.baseUrl.trim(),
    api_key: isVod.value ? '' : form.apiKey.trim(),
    capabilities,
    model_map: modelMap,
    priority: form.priority,
    weight: form.weight,
    enabled: form.enabled,
  }
  if (isVod.value) {
    // 敏感键留空 = 后端保留已存值;非敏感键(sub_app_id/region)原值回传。
    input.config = {
      secret_id: form.secretId.trim(),
      secret_key: form.secretKey.trim(),
      sub_app_id: form.subAppId.trim(),
      region: form.region.trim(),
    }
  }
  return input
}

async function submit(): Promise<void> {
  if (saving.value) {
    return
  }
  saving.value = true
  formError.value = ''
  try {
    if (editingId.value === null) {
      await auth.client.createChannel(buildInput())
    } else {
      await auth.client.updateChannel(editingId.value, buildInput())
    }
    closeForm()
    await refresh()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e)
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(ch: Channel): Promise<void> {
  error.value = ''
  try {
    await auth.client.updateChannel(ch.id, {
      name: ch.name,
      type: ch.type,
      base_url: ch.base_url,
      api_key: '', // 保留已存密钥
      config: ch.config, // 敏感键回显为空 = 保留已存值
      capabilities: ch.capabilities,
      model_map: ch.model_map,
      priority: ch.priority,
      weight: ch.weight,
      enabled: !ch.enabled,
    })
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

async function remove(ch: Channel): Promise<void> {
  if (!window.confirm(`确定删除渠道「${ch.name}」?该操作不可恢复。`)) {
    return
  }
  error.value = ''
  try {
    await auth.client.deleteChannel(ch.id)
    delete testResults[ch.id]
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

async function test(ch: Channel): Promise<void> {
  if (testingId.value !== null) {
    return
  }
  testingId.value = ch.id
  error.value = ''
  try {
    testResults[ch.id] = await auth.client.testChannel(ch.id)
  } catch (e) {
    error.value = authErrorMessage(e)
  } finally {
    testingId.value = null
  }
}

function modelMapSummary(ch: Channel): string {
  const entries = Object.entries(ch.model_map)
  if (entries.length === 0) {
    return '(无映射)'
  }
  const shown = entries.slice(0, 3).map(([from, to]) => (from === to ? from : `${from} → ${to}`))
  const rest = entries.length - shown.length
  return shown.join('、') + (rest > 0 ? ` 等 ${entries.length} 条` : '')
}
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>渠道管理</h2>
      <button
        type="button"
        class="primary"
        @click="openCreate"
      >
        新建渠道
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
      <div class="card-head">
        <h3>{{ formTitle }}</h3>
        <button
          type="button"
          class="ghost"
          @click="closeForm"
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
            placeholder="例如 openai-main"
          >
        </label>
        <label>
          <span>厂商类型</span>
          <select v-model="form.type">
            <option value="openai">OpenAI 兼容</option>
            <option value="tencent-vod">腾讯云 VOD AIGC</option>
          </select>
        </label>
        <label class="wide">
          <span>BaseURL{{ isVod ? '(可空,缺省 vod.tencentcloudapi.com)' : '(含版本路径)' }}</span>
          <input
            v-model="form.baseUrl"
            type="text"
            :required="!isVod"
            :placeholder="isVod ? '留空 = https://vod.tencentcloudapi.com' : 'https://api.openai.com/v1'"
          >
        </label>
        <label
          v-if="!isVod"
          class="wide"
        >
          <span>厂商密钥{{ editingId === null ? '' : '(留空保持现有密钥)' }}</span>
          <input
            v-model="form.apiKey"
            type="password"
            :required="editingId === null"
            autocomplete="off"
            placeholder="sk-…"
          >
        </label>
        <template v-else>
          <label>
            <span>SecretId{{ editingId === null ? '' : '(留空保持现有)' }}</span>
            <input
              v-model="form.secretId"
              type="password"
              :required="editingId === null"
              autocomplete="off"
              placeholder="AKID…"
            >
          </label>
          <label>
            <span>SecretKey{{ editingId === null ? '' : '(留空保持现有)' }}</span>
            <input
              v-model="form.secretKey"
              type="password"
              :required="editingId === null"
              autocomplete="off"
              placeholder="腾讯云 API 密钥"
            >
          </label>
          <label>
            <span>SubAppId(点播子应用,可空)</span>
            <input
              v-model="form.subAppId"
              type="text"
              placeholder="1500000000"
            >
          </label>
          <label>
            <span>地域(可空)</span>
            <input
              v-model="form.region"
              type="text"
              placeholder="ap-guangzhou"
            >
          </label>
          <p
            v-if="editingId !== null && editingHints"
            class="muted wide"
          >
            已存凭据:{{ editingHints }}
          </p>
        </template>

        <fieldset class="wide">
          <legend>模型映射(公开模型名 → 上游模型名)</legend>
          <div
            v-for="(mapping, index) in form.mappings"
            :key="index"
            class="mapping-row"
          >
            <input
              v-model="mapping.from"
              type="text"
              placeholder="gpt-4o"
              aria-label="公开模型名"
            >
            <span class="arrow">→</span>
            <input
              v-model="mapping.to"
              type="text"
              placeholder="gpt-4o-2024-11-20"
              aria-label="上游模型名"
            >
            <button
              type="button"
              class="ghost"
              :disabled="form.mappings.length === 1"
              @click="removeMapping(index)"
            >
              删除
            </button>
          </div>
          <button
            type="button"
            class="ghost"
            @click="addMapping"
          >
            + 添加映射
          </button>
        </fieldset>

        <fieldset class="wide">
          <legend>能力(该渠道可转发的请求;不勾任何项按仅聊天处理)</legend>
          <div class="cap-row">
            <label class="check">
              <input
                v-model="form.capChat"
                type="checkbox"
              >
              <span>聊天(chat)</span>
            </label>
            <label class="check">
              <input
                v-model="form.capImages"
                type="checkbox"
              >
              <span>生图(images)</span>
            </label>
            <label class="check">
              <input
                v-model="form.capVideos"
                type="checkbox"
              >
              <span>生视频(videos)</span>
            </label>
          </div>
        </fieldset>

        <label>
          <span>优先级(越大越优先)</span>
          <input
            v-model.number="form.priority"
            type="number"
            min="0"
          >
        </label>
        <label>
          <span>权重</span>
          <input
            v-model.number="form.weight"
            type="number"
            min="0"
          >
        </label>
        <label class="check">
          <input
            v-model="form.enabled"
            type="checkbox"
          >
          <span>启用该渠道</span>
        </label>

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
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>
    </section>

    <p
      v-if="loading"
      class="muted"
    >
      正在加载渠道…
    </p>
    <p
      v-else-if="channels.length === 0"
      class="muted"
    >
      还没有渠道。点击「新建渠道」录入第一家厂商。
    </p>

    <section
      v-for="ch in channels"
      :key="ch.id"
      class="card"
    >
      <div class="card-head">
        <h3>
          {{ ch.name }}
          <span
            class="badge"
            :class="ch.enabled ? 'ok' : 'off'"
          >{{ ch.enabled ? '已启用' : '已停用' }}</span>
        </h3>
        <div class="row-actions">
          <button
            type="button"
            class="primary"
            :disabled="testingId === ch.id"
            @click="test(ch)"
          >
            {{ testingId === ch.id ? '测试中…' : '连通测试' }}
          </button>
          <button
            type="button"
            class="ghost"
            @click="openEdit(ch)"
          >
            编辑
          </button>
          <button
            type="button"
            class="ghost"
            @click="toggleEnabled(ch)"
          >
            {{ ch.enabled ? '停用' : '启用' }}
          </button>
          <button
            type="button"
            class="danger"
            @click="remove(ch)"
          >
            删除
          </button>
        </div>
      </div>

      <dl class="fields">
        <div>
          <dt>类型</dt>
          <dd>{{ ch.type }}</dd>
        </div>
        <div class="wide">
          <dt>BaseURL</dt>
          <dd><code>{{ ch.base_url || (ch.type === 'tencent-vod' ? 'https://vod.tencentcloudapi.com(缺省)' : '-') }}</code></dd>
        </div>
        <div>
          <dt>{{ ch.type === 'tencent-vod' ? '凭据' : '密钥' }}</dt>
          <dd>
            {{
              ch.type === 'tencent-vod'
                ? (ch.config_hints?.secret_id ? `已设置 ${Object.values(ch.config_hints).join(' / ')}` : '未设置')
                : ch.has_key ? `已设置 ${ch.key_hint ?? ''}` : '未设置'
            }}
          </dd>
        </div>
        <div>
          <dt>优先级 / 权重</dt>
          <dd>{{ ch.priority }} / {{ ch.weight }}</dd>
        </div>
        <div class="wide">
          <dt>模型映射</dt>
          <dd>{{ modelMapSummary(ch) }}</dd>
        </div>
      </dl>

      <p
        v-if="testResults[ch.id]"
        class="test-result"
        :class="testResults[ch.id]!.ok ? 'ok' : 'fail'"
        role="status"
      >
        <template v-if="testResults[ch.id]!.ok">
          连通成功({{ testResults[ch.id]!.detail }} · {{ testResults[ch.id]!.latency_ms }}ms)
        </template>
        <template v-else>
          连通失败:{{ testResults[ch.id]!.error }}
        </template>
      </p>
    </section>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* 渠道视图私有样式:模型映射行、启用勾选与连通测试结果。 */
.grid fieldset {
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 10px;
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
}

.grid fieldset legend {
  font-size: 13px;
  color: #8b91a7;
  padding: 0 4px;
}

.mapping-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr auto;
  gap: 8px;
  align-items: center;
}

.mapping-row input {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 8px 10px;
  color: inherit;
  font-size: 13px;
}

.arrow {
  color: #8b91a7;
}

.cap-row {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
}

label.check {
  display: flex;
  align-items: center;
  gap: 8px;
  align-self: end;
  padding-bottom: 10px;
}

label.check span {
  color: inherit;
  font-size: 14px;
}

.test-result {
  margin: 14px 0 0;
  padding: 10px 14px;
  border-radius: 10px;
  font-size: 13px;
}

.test-result.ok {
  background: rgba(52, 199, 123, 0.12);
  color: #4cd787;
}

.test-result.fail {
  background: rgba(255, 107, 107, 0.1);
  color: #ff8f8f;
}
</style>

