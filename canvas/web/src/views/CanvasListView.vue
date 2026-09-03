<script setup lang="ts">
// 画布列表:创建、重命名、删除、进入编辑器(09 号票验收:增删改名,数据持久)。
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError, type CanvasSummary } from '@infinitechance/api'

import { useAuth } from '../auth'

const { client, username, clearSession } = useAuth()
const router = useRouter()

const canvases = ref<CanvasSummary[]>([])
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')

const newName = ref('')
const creating = ref(false)

// 行内重命名状态:正在改名的画布 id 与草稿名。
const renamingId = ref<number | null>(null)
const renameDraft = ref('')

// 待确认删除的画布 id(两步确认,防误删)。
const confirmingDeleteId = ref<number | null>(null)

async function refresh(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    canvases.value = await client.listCanvases()
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

async function create(): Promise<void> {
  const name = newName.value.trim()
  if (!name || creating.value) {
    return
  }
  creating.value = true
  actionError.value = ''
  try {
    const created = await client.createCanvas(name)
    newName.value = ''
    await router.push({ name: 'canvas-editor', params: { id: created.id } })
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '创建失败,请稍后再试'
  } finally {
    creating.value = false
  }
}

function startRename(c: CanvasSummary): void {
  renamingId.value = c.id
  renameDraft.value = c.name
  confirmingDeleteId.value = null
}

async function commitRename(): Promise<void> {
  const id = renamingId.value
  const name = renameDraft.value.trim()
  if (id === null || !name) {
    renamingId.value = null
    return
  }
  actionError.value = ''
  try {
    const renamed = await client.renameCanvas(id, name)
    const index = canvases.value.findIndex((c) => c.id === id)
    if (index >= 0) {
      // 服务器按 updated_at 排序;改名会把它顶到最前。
      canvases.value.splice(index, 1)
      const insertAt = canvases.value.findIndex((c) => c.updated_at <= renamed.updated_at)
      if (insertAt < 0) {
        canvases.value.unshift(renamed)
      } else {
        canvases.value.splice(insertAt, 0, renamed)
      }
    }
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '重命名失败,请稍后再试'
  } finally {
    renamingId.value = null
  }
}

async function remove(id: number): Promise<void> {
  actionError.value = ''
  try {
    await client.deleteCanvas(id)
    canvases.value = canvases.value.filter((c) => c.id !== id)
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试'
  } finally {
    confirmingDeleteId.value = null
  }
}

function open(c: CanvasSummary): void {
  void router.push({ name: 'canvas-editor', params: { id: c.id } })
}

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
        <h1>无限画布</h1>
        <p class="subtitle">
          提示词 · 图片 · 视频
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

    <form
      class="create"
      @submit.prevent="create"
    >
      <input
        v-model="newName"
        type="text"
        placeholder="新画布名称…"
        maxlength="128"
      >
      <button
        type="submit"
        :disabled="creating || !newName.trim()"
      >
        {{ creating ? '创建中…' : '新建画布' }}
      </button>
    </form>

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
      v-else-if="canvases.length === 0"
      class="feedback"
    >
      还没有画布,从上面新建一张开始创作。
    </p>

    <ul class="list">
      <li
        v-for="c in canvases"
        :key="c.id"
      >
        <template v-if="renamingId === c.id">
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
              type="submit"
              class="primary"
            >
              保存
            </button>
            <button
              type="button"
              class="ghost"
              @click="renamingId = null"
            >
              取消
            </button>
          </form>
        </template>
        <template v-else>
          <button
            class="open"
            type="button"
            @click="open(c)"
          >
            <span class="name">{{ c.name }}</span>
            <span class="meta">更新于 {{ formatTime(c.updated_at) }}</span>
          </button>
          <span class="row-actions">
            <button
              type="button"
              class="ghost"
              @click="startRename(c)"
            >
              重命名
            </button>
            <template v-if="confirmingDeleteId === c.id">
              <button
                type="button"
                class="danger"
                @click="remove(c.id)"
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
              @click="confirmingDeleteId = c.id"
            >
              删除
            </button>
          </span>
        </template>
      </li>
    </ul>
  </main>
</template>

<style scoped>
.shell {
  max-width: 720px;
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
  color: #8b91a7;
  font-size: 14px;
}

.create {
  display: flex;
  gap: 10px;
  margin: 28px 0 12px;
}

.create input {
  flex: 1;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 10px 12px;
  color: inherit;
  font-size: 15px;
}

.create input:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.create button,
.list button {
  border: none;
  border-radius: 10px;
  padding: 10px 16px;
  font-size: 14px;
  cursor: pointer;
}

.create button {
  background: #4c6ef5;
  color: #fff;
  font-weight: 600;
}

.create button:hover:not(:disabled) {
  background: #4263eb;
}

.create button:disabled {
  opacity: 0.6;
  cursor: default;
}

.list {
  list-style: none;
  margin: 16px 0 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  padding: 10px 12px;
}

.open {
  flex: 1;
  display: grid;
  gap: 2px;
  text-align: left;
  background: transparent;
  border: none;
  color: inherit;
  padding: 6px 8px;
  border-radius: 8px;
  cursor: pointer;
}

.open:hover {
  background: rgba(255, 255, 255, 0.06);
}

.open .name {
  font-size: 16px;
  font-weight: 600;
}

.open .meta {
  color: #8b91a7;
  font-size: 12px;
}

.row-actions {
  display: flex;
  gap: 6px;
}

.primary {
  background: #4c6ef5;
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

.danger {
  background: #e03131;
  color: #fff;
  font-weight: 600;
}

.rename-form {
  flex: 1;
  display: flex;
  gap: 8px;
}

.rename-form input {
  flex: 1;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 8px 12px;
  color: inherit;
  font-size: 15px;
}

.feedback {
  margin: 16px 0 0;
  color: #8b91a7;
  font-size: 14px;
}

.error {
  color: #ff8f8f;
}
</style>
