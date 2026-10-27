<script setup lang="ts">
// IP 卡片墙(50 号票):进入应用的第一层 —— 每个卡片是「IP 企划」容器
// (封面[复用素材缩略 302]+ 名称 + 一句话简介 + 画布数 + 最近更新),空
// 封面回落「渐变 + 首字」占位。新建/编辑对话框收名称 + 简介 + 封面(素材
// 库选或本机上传,上传带该 ip_id);删除名下有画布/素材时后端 409
// ip_not_empty,原样提示不级联。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError, type AssetRecord, type IPRecord } from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'
import AssetPicker from '../components/AssetPicker.vue'

const { client, username, clearSession } = useAuth()
const router = useRouter()

const ips = ref<IPRecord[]>([])
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')

// 对话框状态:mode 区分新建/编辑;editingId 是编辑目标的 IP id。
const dialogOpen = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const editingId = ref<number | null>(null)
const formName = ref('')
const formDescription = ref('')
const formCoverId = ref<number>(0)
const saving = ref(false)

// 封面挑选:打开 AssetPicker(单选图片);pendingCoverFile 是待上传的本机
// 文件(新建 IP 时先建 IP 再上传,上传带新 ip_id)。
const coverPickerOpen = ref(false)
const coverFileInput = ref<HTMLInputElement | null>(null)
const uploadingCover = ref(false)

// 两步确认删除的 IP id。
const confirmingDeleteId = ref<number | null>(null)

// 图片缩略走 thumbnail_url(49 号票):封面 onerror 回落 content。
const thumbs = useThumbFallback()

async function refresh(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    ips.value = await client.listIPs()
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      await router.replace({ name: 'login' })
      return
    }
    loadError.value = e instanceof ApiError ? e.message : '无法连接画布服务,请确认后端已启动'
  } finally {
    loading.value = false
  }
}

onMounted(refresh)

function openCreate(): void {
  dialogMode.value = 'create'
  editingId.value = null
  formName.value = ''
  formDescription.value = ''
  formCoverId.value = 0
  actionError.value = ''
  dialogOpen.value = true
}

function openEdit(ip: IPRecord): void {
  dialogMode.value = 'edit'
  editingId.value = ip.id
  formName.value = ip.name
  formDescription.value = ip.description
  formCoverId.value = ip.cover_asset_id
  actionError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  dialogOpen.value = false
  coverPickerOpen.value = false
}

/** 素材库选中封面:记 id(随保存一并 PATCH/创建)。 */
function onCoverPicked(a: AssetRecord): void {
  formCoverId.value = a.id
  coverPickerOpen.value = false
}

/** 本机上传封面:编辑态直接带该 IP 上传并落 cover;新建态先记文件,
 * 保存流程里建完 IP 再上传(上传必须带 ip_id)。 */
function onCoverFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) {
    return
  }
  if (dialogMode.value === 'edit' && editingId.value !== null) {
    void uploadCover(editingId.value, file)
  } else {
    pendingCoverFile.value = file
  }
}

const pendingCoverFile = ref<File | null>(null)

async function uploadCover(ipId: number, file: File): Promise<void> {
  uploadingCover.value = true
  actionError.value = ''
  try {
    const a = await client.uploadAsset(file, 'image', ipId)
    formCoverId.value = a.id
    await client.patchIP(ipId, { cover_asset_id: a.id })
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '封面上传失败,请稍后再试'
  } finally {
    uploadingCover.value = false
  }
}

function clearCover(): void {
  formCoverId.value = 0
  pendingCoverFile.value = null
}

async function save(): Promise<void> {
  const name = formName.value.trim()
  if (!name || saving.value) {
    return
  }
  saving.value = true
  actionError.value = ''
  try {
    if (dialogMode.value === 'create') {
      const created = await client.createIP({
        name,
        description: formDescription.value.trim() || undefined,
      })
      if (pendingCoverFile.value) {
        await uploadCover(created.id, pendingCoverFile.value)
      } else if (formCoverId.value > 0) {
        await client.patchIP(created.id, { cover_asset_id: formCoverId.value })
      }
    } else if (editingId.value !== null) {
      await client.patchIP(editingId.value, {
        name,
        description: formDescription.value.trim(),
        cover_asset_id: formCoverId.value,
      })
    }
    pendingCoverFile.value = null
    dialogOpen.value = false
    await refresh()
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '保存失败,请稍后再试'
  } finally {
    saving.value = false
  }
}

async function remove(id: number): Promise<void> {
  actionError.value = ''
  try {
    await client.deleteIP(id)
    ips.value = ips.value.filter((ip) => ip.id !== id)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'ip_not_empty') {
      actionError.value = '该 IP 名下还有画布或素材,先移走再删除。'
    } else {
      actionError.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试'
    }
  } finally {
    confirmingDeleteId.value = null
  }
}

function open(ip: IPRecord): void {
  void router.push({ name: 'ip-detail', params: { id: ip.id } })
}

const coverURL = computed(() =>
  formCoverId.value > 0 ? `/api/assets/${formCoverId.value}/thumb` : '',
)

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

async function logout(): Promise<void> {
  clearSession()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <main class="shell">
    <header class="masthead">
      <div>
        <h1>IP 企划</h1>
        <p class="subtitle">
          打造 ACG IP:企划 → 画布 → 素材
          <template v-if="username">
            · {{ username }}
          </template>
        </p>
      </div>
      <button
        class="ghost"
        type="button"
        @click="logout"
      >
        退出登录
      </button>
    </header>

    <div class="toolbar">
      <button
        class="primary"
        type="button"
        @click="openCreate"
      >
        + 新建 IP
      </button>
    </div>

    <p
      v-if="actionError"
      class="feedback error"
      role="alert"
    >
      {{ actionError }}
    </p>

    <p
      v-if="loading"
      class="feedback"
    >
      加载中…
    </p>
    <p
      v-else-if="loadError"
      class="feedback error"
      role="alert"
    >
      {{ loadError }}
      <button
        class="ghost"
        type="button"
        @click="refresh"
      >
        重试
      </button>
    </p>
    <p
      v-else-if="ips.length === 0"
      class="feedback"
    >
      还没有 IP 企划,从上面新建一个开始创作。
    </p>

    <ul class="ip-grid">
      <li
        v-for="ip in ips"
        :key="ip.id"
        class="ip-card"
      >
        <button
          class="card-body"
          type="button"
          @click="open(ip)"
        >
          <span class="cover">
            <img
              v-if="ip.cover_asset_id > 0"
              :src="thumbs.displayURL(`/api/assets/${ip.cover_asset_id}/content`, `/api/assets/${ip.cover_asset_id}/thumb`)"
              class="cover-img"
              loading="lazy"
              alt="IP 封面"
              @error="thumbs.onThumbError(`/api/assets/${ip.cover_asset_id}/content`)"
            >
            <span
              v-else
              class="cover-fallback"
            >{{ ip.name.slice(0, 1) }}</span>
          </span>
          <span class="name">{{ ip.name }}</span>
          <span
            class="description"
            :title="ip.description"
          >{{ ip.description || '(还没有一句话简介)' }}</span>
          <span class="meta">{{ ip.canvas_count }} 张画布 · 更新于 {{ formatTime(ip.latest_at) }}</span>
        </button>
        <span class="row-actions">
          <button
            type="button"
            class="ghost"
            @click="openEdit(ip)"
          >
            编辑
          </button>
          <template v-if="confirmingDeleteId === ip.id">
            <button
              type="button"
              class="danger"
              @click="remove(ip.id)"
            >
              确认删除
            </button>
            <button
              type="button"
              class="ghost"
              @click="confirmingDeleteId = null"
            >
              取消
            </button>
          </template>
          <button
            v-else
            type="button"
            class="ghost"
            @click="confirmingDeleteId = ip.id"
          >
            删除
          </button>
        </span>
      </li>
    </ul>

    <!-- 新建/编辑 IP 对话框:相对定位容器,封面挑选面板锚定其右缘。 -->
    <div
      v-if="dialogOpen"
      class="dialog-mask"
      @click.self="closeDialog"
    >
      <div class="dialog">
        <div class="dialog-inner">
          <h3>{{ dialogMode === 'create' ? '新建 IP 企划' : '编辑 IP 企划' }}</h3>
          <form
            class="ip-form"
            @submit.prevent="save"
          >
            <label>
              名称
              <input
                v-model="formName"
                type="text"
                maxlength="255"
                placeholder="IP 名称…"
                autofocus
              >
            </label>
            <label>
              简介(一句话 logline)
              <input
                v-model="formDescription"
                type="text"
                maxlength="1024"
                placeholder="一句话介绍这个企划…"
              >
            </label>
            <div class="cover-field">
              <span class="cover-label">封面</span>
              <span class="cover-preview">
                <img
                  v-if="coverURL && !pendingCoverFile"
                  :src="coverURL"
                  alt="封面预览"
                  @error="($event.target as HTMLImageElement).style.display = 'none'"
                >
                <span
                  v-else-if="pendingCoverFile"
                  class="pending"
                >已选文件:{{ pendingCoverFile.name }}</span>
                <span
                  v-else
                  class="placeholder"
                >{{ formName.trim().slice(0, 1) || '?' }}</span>
              </span>
              <span class="cover-actions">
                <button
                  type="button"
                  class="ghost"
                  @click="coverPickerOpen = !coverPickerOpen"
                >
                  从素材库选
                </button>
                <button
                  type="button"
                  class="ghost"
                  :disabled="uploadingCover"
                  @click="coverFileInput?.click()"
                >
                  {{ uploadingCover ? '上传中…' : '本机上传' }}
                </button>
                <button
                  v-if="formCoverId > 0 || pendingCoverFile"
                  type="button"
                  class="ghost"
                  @click="clearCover"
                >
                  清除
                </button>
              </span>
              <input
                ref="coverFileInput"
                type="file"
                accept="image/*"
                class="sr-input"
                @change="onCoverFileChange"
              >
            </div>
            <p
              v-if="actionError"
              class="feedback error"
              role="alert"
            >
              {{ actionError }}
            </p>
            <div class="form-actions">
              <button
                type="submit"
                class="primary"
                :disabled="saving || !formName.trim()"
              >
                {{ saving ? '保存中…' : '保存' }}
              </button>
              <button
                type="button"
                class="ghost"
                @click="closeDialog"
              >
                取消
              </button>
            </div>
          </form>
        </div>
        <!-- 封面挑选:本 IP + 全局(scope=ip),单选图片;面板锚定对话框右缘。 -->
        <AssetPicker
          v-if="coverPickerOpen"
          title="选择封面素材"
          :kind-whitelist="['image']"
          hint="点选设为封面;也可本机上传(随保存自动带该 IP)。"
          :selected-asset-ids="formCoverId > 0 ? [formCoverId] : []"
          :ip-id="editingId ?? undefined"
          @toggle="onCoverPicked"
          @close="coverPickerOpen = false"
        />
      </div>
    </div>
  </main>
</template>

<style scoped>
.shell {
  max-width: 960px;
  margin: 0 auto;
  padding: 48px 24px;
}

.masthead {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.masthead h1 {
  margin: 0;
  font-size: 28px;
  letter-spacing: 0.5px;
}

.subtitle {
  margin: 6px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.toolbar {
  display: flex;
  gap: 10px;
  margin: 28px 0 12px;
}

.toolbar button,
.ghost,
.danger {
  border: none;
  border-radius: var(--r-lg);
  padding: 10px 16px;
  font-size: var(--fs-body);
  cursor: pointer;
}

.primary {
  background: #3b5bdb;
  color: #fff;
  font-weight: 600;
}

.primary:hover:not(:disabled) {
  background: var(--accent-soft);
}

.primary:disabled {
  opacity: 0.6;
  cursor: default;
}

.ghost {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
}

.ghost:hover:not(:disabled) {
  border-color: var(--line-action);
  color: inherit;
}

.danger {
  background: #e03131;
  color: #fff;
  font-weight: 600;
}

.feedback {
  margin: 16px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.error {
  color: var(--danger);
}

.ip-grid {
  list-style: none;
  margin: 16px 0 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 14px;
}

.ip-card {
  display: grid;
  gap: 8px;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-xl);
  padding: 12px;
}

.card-body {
  display: grid;
  gap: 6px;
  text-align: left;
  background: transparent;
  border: none;
  color: inherit;
  padding: 4px;
  border-radius: var(--r-md);
  cursor: pointer;
}

.card-body:hover {
  background: var(--wash-soft);
}

.cover {
  display: block;
  aspect-ratio: 16 / 9;
  border-radius: var(--r-lg);
  overflow: hidden;
  background: var(--wash-soft);
}

.cover-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-fallback {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  font-size: 42px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.82);
  background: linear-gradient(135deg, var(--ip-indigo) 0%, var(--ip-violet) 55%, var(--ip-wine) 100%);
}

.name {
  font-size: 17px;
  font-weight: 600;
}

.description {
  color: var(--text-muted);
  font-size: var(--fs-body);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.meta {
  color: var(--text-faint);
  font-size: var(--fs-label);
}

.row-actions {
  display: flex;
  gap: 6px;
  padding: 0 4px 4px;
}

.row-actions button {
  padding: 6px 12px;
  font-size: var(--fs-label);
}

.dialog-mask {
  position: fixed;
  inset: 0;
  background: rgba(var(--bg-deep-rgb), 0.7);
  display: grid;
  place-items: center;
  z-index: var(--z-dialog-mask);
}

.dialog {
  position: relative;
  width: min(440px, calc(100vw - 48px));
  background: var(--bg-navy);
  border: 1px solid var(--line-control);
  border-radius: 14px;
  overflow: hidden;
}

.dialog-inner {
  padding: 20px;
}

.dialog h3 {
  margin: 0 0 14px;
  font-size: 18px;
}

.ip-form {
  display: grid;
  gap: 12px;
}

.ip-form label {
  display: grid;
  gap: 6px;
  font-size: var(--fs-body);
  color: var(--text-secondary);
}

.ip-form input {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-lg);
  padding: 9px 12px;
  color: inherit;
  font-size: var(--fs-title);
}

.ip-form input:focus {
  outline: 2px solid rgba(var(--accent-rgb), 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.cover-field {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cover-label {
  font-size: var(--fs-body);
  color: var(--text-secondary);
}

.cover-preview {
  display: grid;
  place-items: center;
  width: 88px;
  height: 50px;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--wash-soft);
  font-size: 18px;
  font-weight: 700;
}

.cover-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-preview .placeholder {
  background: linear-gradient(135deg, var(--ip-indigo) 0%, var(--ip-violet) 55%, var(--ip-wine) 100%);
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
}

.cover-preview .pending {
  font-size: var(--fs-micro);
  color: var(--text-muted);
  padding: 4px;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
  white-space: nowrap;
}

.cover-actions {
  display: flex;
  gap: 6px;
}

.cover-actions button {
  padding: 6px 10px;
  font-size: var(--fs-label);
}

.sr-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.form-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
</style>
