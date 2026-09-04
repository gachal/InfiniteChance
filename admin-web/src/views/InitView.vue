<script setup lang="ts">
// 初始化引导(16 号票):串起首个管理员账号与首个厂商渠道。
// 步骤 1 创建唯一管理员(POST /auth/init,成功即登录);步骤 2 录入第一家
// 厂商渠道(可跳过,留待渠道管理页配置)——完成后引导不再出现。
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AuthLayout from '../components/AuthLayout.vue'

const MIN_PASSWORD_LENGTH = 8

const auth = useAuth()
const router = useRouter()

const step = ref<1 | 2>(1)

// —— 步骤 1:管理员账号 ——
const username = ref('admin')
const password = ref('')
const confirmPassword = ref('')
const adminError = ref('')
const creatingAdmin = ref(false)

async function submitAdmin(): Promise<void> {
  if (creatingAdmin.value) {
    return
  }
  const name = username.value.trim()
  if (name.length === 0) {
    adminError.value = '请填写用户名'
    return
  }
  if (password.value.length < MIN_PASSWORD_LENGTH) {
    adminError.value = `密码至少 ${MIN_PASSWORD_LENGTH} 个字符`
    return
  }
  if (password.value !== confirmPassword.value) {
    adminError.value = '两次输入的密码不一致'
    return
  }

  adminError.value = ''
  creatingAdmin.value = true
  try {
    await auth.initAdmin(name, password.value)
    step.value = 2
  } catch (e) {
    if (e instanceof ApiError && e.code === 'already_initialized') {
      // 初始化已完成:引导不再出现,转去登录。
      await router.replace({ name: 'login' })
      return
    }
    adminError.value = authErrorMessage(e)
  } finally {
    creatingAdmin.value = false
  }
}

// —— 步骤 2:首个渠道(字段与渠道管理页同形)——
const form = reactive({
  name: '',
  baseUrl: '',
  apiKey: '',
  mappings: [{ from: '', to: '' }],
})
const channelError = ref('')
const savingChannel = ref(false)

function addMapping(): void {
  form.mappings.push({ from: '', to: '' })
}

function removeMapping(index: number): void {
  form.mappings.splice(index, 1)
}

async function finish(): Promise<void> {
  await router.replace('/')
}

async function submitChannel(): Promise<void> {
  if (savingChannel.value) {
    return
  }
  if (form.name.trim() === '') {
    channelError.value = '请填写渠道名称'
    return
  }
  if (form.baseUrl.trim() === '') {
    channelError.value = '请填写 BaseURL'
    return
  }
  if (form.apiKey.trim() === '') {
    channelError.value = '请填写厂商密钥'
    return
  }

  channelError.value = ''
  savingChannel.value = true
  try {
    const modelMap: Record<string, string> = {}
    for (const { from, to } of form.mappings) {
      if (from.trim() !== '' && to.trim() !== '') {
        modelMap[from.trim()] = to.trim()
      }
    }
    await auth.client.createChannel({
      name: form.name.trim(),
      type: 'openai',
      base_url: form.baseUrl.trim(),
      api_key: form.apiKey.trim(),
      model_map: modelMap,
      priority: 0,
      weight: 1,
      enabled: true,
    })
    await finish()
  } catch (e) {
    channelError.value = authErrorMessage(e)
  } finally {
    savingChannel.value = false
  }
}
</script>

<template>
  <AuthLayout>
    <form
      v-if="step === 1"
      class="auth-form"
      @submit.prevent="submitAdmin"
    >
      <p class="hint">
        首次使用(第 1 步,共 2 步):创建唯一的管理员账号,随后录入首个厂商渠道。
      </p>

      <label>
        <span>用户名</span>
        <input
          v-model="username"
          type="text"
          name="username"
          autocomplete="username"
          required
        >
      </label>
      <label>
        <span>密码(至少 8 个字符)</span>
        <input
          v-model="password"
          type="password"
          name="new-password"
          autocomplete="new-password"
          required
        >
      </label>
      <label>
        <span>确认密码</span>
        <input
          v-model="confirmPassword"
          type="password"
          name="confirm-password"
          autocomplete="new-password"
          required
        >
      </label>

      <p
        v-if="adminError"
        class="error"
        role="alert"
      >
        {{ adminError }}
      </p>

      <button
        type="submit"
        :disabled="creatingAdmin"
      >
        {{ creatingAdmin ? '创建中…' : '创建管理员,下一步' }}
      </button>
    </form>

    <form
      v-else
      class="auth-form"
      @submit.prevent="submitChannel"
    >
      <p class="hint">
        第 2 步,共 2 步:录入第一家厂商渠道(OpenAI 兼容,含 DeepSeek/GLM/Kimi 等)。
        模型映射可留空,之后可随时在「渠道管理」补充或修改。
      </p>

      <label>
        <span>渠道名称</span>
        <input
          v-model="form.name"
          type="text"
          required
          placeholder="例如 deepseek-main"
        >
      </label>
      <label>
        <span>BaseURL(含版本路径)</span>
        <input
          v-model="form.baseUrl"
          type="text"
          required
          placeholder="https://api.openai.com/v1"
        >
      </label>
      <label>
        <span>厂商密钥</span>
        <input
          v-model="form.apiKey"
          type="password"
          required
          autocomplete="off"
          placeholder="sk-…"
        >
      </label>

      <fieldset class="mappings">
        <legend>模型映射(公开模型名 → 上游模型名,可留空)</legend>
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

      <p
        v-if="channelError"
        class="error"
        role="alert"
      >
        {{ channelError }}
      </p>

      <div class="actions">
        <button
          type="submit"
          class="primary"
          :disabled="savingChannel"
        >
          {{ savingChannel ? '保存中…' : '创建渠道并完成' }}
        </button>
        <button
          type="button"
          class="ghost"
          :disabled="savingChannel"
          @click="finish"
        >
          跳过,稍后配置
        </button>
      </div>
    </form>
  </AuthLayout>
</template>

<style scoped src="../components/auth-form.css"></style>

<style scoped>
.hint {
  margin: 0 0 4px;
  color: #8b91a7;
  font-size: 13px;
  line-height: 1.6;
}

.mappings {
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 10px;
  display: grid;
  gap: 8px;
  padding: 12px 14px 14px;
}

.mappings legend {
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

.actions {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 10px;
}

/* auth-form.css 把所有按钮统一染成主色;引导里的次级按钮(跳过/映射增删)
 * 退为描边样式。 */
button.ghost {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 10px;
  color: #aab1c5;
  padding: 10px 14px;
  font-size: 13px;
}

button.ghost:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
}
</style>
