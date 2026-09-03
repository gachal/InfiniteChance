<script setup lang="ts">
// 提示词模板:画布「生成提示词」动作的底稿管理(11 号票)。
// 模板内容须包含 {topic} 占位符,生成时替换为用户输入的主题;
// 画布侧每次即时读库,这里的增删改立即生效。
import { computed, onMounted, reactive, ref } from 'vue'

import { ApiError, type PromptTemplate } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const auth = useAuth()

const templates = ref<PromptTemplate[]>([])
const loading = ref(false)
const error = ref('')

const showForm = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')

interface TemplateForm {
  name: string
  template: string
  enabled: boolean
}

function blankForm(): TemplateForm {
  return { name: '', template: '', enabled: true }
}

const form = reactive<TemplateForm>(blankForm())

const formTitle = computed(() => (editingId.value === null ? '新建模板' : '编辑模板'))

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    templates.value = await auth.client.listPromptTemplates()
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
  formError.value = ''
  showForm.value = true
}

function openEdit(t: PromptTemplate): void {
  editingId.value = t.id
  Object.assign(form, { name: t.name, template: t.template, enabled: t.enabled } satisfies TemplateForm)
  formError.value = ''
  showForm.value = true
}

function closeForm(): void {
  showForm.value = false
  editingId.value = null
  formError.value = ''
}

function buildInput() {
  return {
    name: form.name.trim(),
    template: form.template.trim(),
    enabled: form.enabled,
  }
}

async function submit(): Promise<void> {
  if (saving.value) {
    return
  }
  saving.value = true
  formError.value = ''
  try {
    if (editingId.value === null) {
      await auth.client.createPromptTemplate(buildInput())
    } else {
      await auth.client.updatePromptTemplate(editingId.value, buildInput())
    }
    closeForm()
    await refresh()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e)
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(t: PromptTemplate): Promise<void> {
  error.value = ''
  try {
    await auth.client.updatePromptTemplate(t.id, {
      name: t.name,
      template: t.template,
      enabled: !t.enabled,
    })
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

async function remove(t: PromptTemplate): Promise<void> {
  if (!window.confirm(`确定删除模板「${t.name}」?该操作不可恢复。`)) {
    return
  }
  error.value = ''
  try {
    await auth.client.deletePromptTemplate(t.id)
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

function templateSummary(t: PromptTemplate): string {
  const oneLine = t.template.replaceAll(/\s+/g, ' ').trim()
  return oneLine.length > 120 ? `${oneLine.slice(0, 120)}…` : oneLine
}
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>提示词模板</h2>
      <button
        type="button"
        class="primary"
        @click="openCreate"
      >
        新建模板
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
            maxlength="128"
            placeholder="例如 文生图-中文"
          >
        </label>
        <label class="check">
          <input
            v-model="form.enabled"
            type="checkbox"
          >
          <span>启用(停用后画布侧不再出现)</span>
        </label>

        <label class="wide">
          <span>模板内容(必须包含 {topic} 占位符,生成时替换为主题)</span>
          <textarea
            v-model="form.template"
            rows="8"
            required
            maxlength="8000"
            placeholder="例如:你是提示词工程师。请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。"
          />
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
      正在加载模板…
    </p>
    <p
      v-else-if="templates.length === 0"
      class="muted"
    >
      还没有模板。点击「新建模板」给画布的提示词生成写底稿。
    </p>

    <section
      v-for="t in templates"
      :key="t.id"
      class="card"
    >
      <div class="card-head">
        <h3>
          {{ t.name }}
          <span
            class="badge"
            :class="t.enabled ? 'ok' : 'off'"
          >{{ t.enabled ? '已启用' : '已停用' }}</span>
        </h3>
        <div class="row-actions">
          <button
            type="button"
            class="ghost"
            @click="openEdit(t)"
          >
            编辑
          </button>
          <button
            type="button"
            class="ghost"
            @click="toggleEnabled(t)"
          >
            {{ t.enabled ? '停用' : '启用' }}
          </button>
          <button
            type="button"
            class="danger"
            @click="remove(t)"
          >
            删除
          </button>
        </div>
      </div>

      <dl class="fields">
        <div class="wide">
          <dt>模板内容</dt>
          <dd>
            <code class="template-body">{{ templateSummary(t) }}</code>
          </dd>
        </div>
        <div>
          <dt>更新时间</dt>
          <dd>{{ new Date(t.updated_at).toLocaleString() }}</dd>
        </div>
      </dl>
    </section>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* 模板视图私有样式:模板内容预览。 */
.template-body {
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
