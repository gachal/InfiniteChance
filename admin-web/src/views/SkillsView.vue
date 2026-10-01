<script setup lang="ts">
// 技能:Agent 节点经 `/` 调用的提示词底稿管理(11 号票建;29 号票由
// 「提示词模板」更名演进)。template 须包含 {topic} 占位符,会话首条指令
// 以主题替换;description/target 是技能卡的副标题与目标徽章(target 纯
// 展示 + 浮层筛选,后端零行为);画布侧每次即时读库,这里的增删改立即生效。
import { computed, onMounted, reactive, ref } from 'vue'

import { ApiError, type PromptTemplate, type SkillTarget } from '@infinitechance/api'

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

interface SkillForm {
  name: string
  description: string
  template: string
  target: SkillTarget
  enabled: boolean
}

function blankForm(): SkillForm {
  return { name: '', description: '', template: '', target: 'any', enabled: true }
}

const form = reactive<SkillForm>(blankForm())

const formTitle = computed(() => (editingId.value === null ? '新建技能' : '编辑技能'))

const TARGET_OPTIONS: { value: SkillTarget; label: string }[] = [
  { value: 'any', label: '通用(图/视频皆可)' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
]

const TARGET_LABEL: Record<SkillTarget, string> = {
  any: '通用',
  image: '图',
  video: '视频',
}

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
  Object.assign(form, {
    name: t.name,
    description: t.description,
    template: t.template,
    target: t.target,
    enabled: t.enabled,
  } satisfies SkillForm)
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
    description: form.description.trim(),
    template: form.template.trim(),
    target: form.target,
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
      description: t.description,
      template: t.template,
      target: t.target,
      enabled: !t.enabled,
    })
    await refresh()
  } catch (e) {
    error.value = authErrorMessage(e)
  }
}

async function remove(t: PromptTemplate): Promise<void> {
  if (!window.confirm(`确定删除技能「${t.name}」?该操作不可恢复。`)) {
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
      <h2>技能</h2>
      <button
        type="button"
        class="primary"
        @click="openCreate"
      >
        新建技能
      </button>
    </div>

    <p class="muted">
      技能是画布 Agent 节点经 <code>/</code> 调用的提示词底稿:名称与描述出现在技能卡上,
      生成时模板内容中的 <code>{topic}</code> 占位符替换为用户输入的主题。
    </p>

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
            placeholder="例如 英文生图提示词"
          >
        </label>
        <label>
          <span>描述(技能卡副标题,可空)</span>
          <input
            v-model="form.description"
            type="text"
            maxlength="200"
            placeholder="例如 按主题写一段英文文生图提示词"
          >
        </label>
        <label>
          <span>目标(纯展示,供画布技能浮层筛选)</span>
          <select v-model="form.target">
            <option
              v-for="o in TARGET_OPTIONS"
              :key="o.value"
              :value="o.value"
            >
              {{ o.label }}
            </option>
          </select>
        </label>
        <label class="check">
          <input
            v-model="form.enabled"
            type="checkbox"
          >
          <span>启用(停用后画布侧不再出现)</span>
        </label>

        <label class="wide">
          <span>指令内容(必须包含 {topic} 占位符,生成时替换为主题)</span>
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
      正在加载技能…
    </p>
    <p
      v-else-if="templates.length === 0"
      class="muted"
    >
      还没有技能。点击「新建技能」给画布的 Agent 节点写提示词底稿。
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
            class="badge target"
            :data-target="t.target"
          >{{ TARGET_LABEL[t.target] ?? t.target }}</span>
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
        <div
          v-if="t.description"
          class="wide"
        >
          <dt>描述</dt>
          <dd>{{ t.description }}</dd>
        </div>
        <div class="wide">
          <dt>指令内容</dt>
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
/* 技能视图私有样式:指令内容预览与目标徽章。 */
.template-body {
  white-space: pre-wrap;
  word-break: break-word;
}

.badge.target {
  background: rgba(122, 162, 247, 0.16);
  color: #7aa2f7;
}

.badge.target[data-target='video'] {
  background: rgba(250, 204, 21, 0.14);
  color: #facc15;
}

.badge.target[data-target='image'] {
  background: rgba(74, 222, 128, 0.14);
  color: #4ade80;
}
</style>
