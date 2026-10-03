<script lang="ts" setup>
// 首管理员初始化两步向导(16 号票语义,35 号票改为 vben 风格独立页):
// 步骤 1 创建唯一管理员(POST /auth/init,成功即登录);步骤 2 录入第一家厂商
// 渠道(可跳过)。仅在 /auth/status.initialized === false 时由路由守卫放行。
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import { useAccessStore } from '@vben/stores';

import { ApiError, type ChannelInput } from '@infinitechance/api';

import {
  Button,
  Form as AForm,
  FormItem,
  Input,
  InputPassword,
  Step,
  Steps,
} from 'ant-design-vue';

import { initAdminApi } from '#/api';
import { authErrorMessage, useAuth } from '#/auth';
import { useAuthStore } from '#/store';

defineOptions({ name: 'InitWizard' });

const MIN_PASSWORD_LENGTH = 8;

const accessStore = useAccessStore();
const authStore = useAuthStore();
const { client } = useAuth();
const router = useRouter();

const step = ref<1 | 2>(1);

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
    const session = await initAdminApi({
      password: password.value,
      username: name,
    })
    accessStore.setAccessToken(session.token)
    await authStore.fetchUserInfo()
    // 后端 initialized 已翻真:失效守卫的会话缓存,完成后不再弹回向导。
    authStore.invalidateAuthStatus()
    step.value = 2
  } catch (e) {
    if (e instanceof ApiError && e.code === 'already_initialized') {
      // 初始化已完成:引导不再出现,转去登录。
      await router.replace('/auth/login')
      return
    }
    adminError.value = authErrorMessage(e)
  } finally {
    creatingAdmin.value = false
  }
}

// —— 步骤 2:首个渠道(字段与渠道管理页同形)——
const form = reactive({
  apiKey: '',
  baseUrl: '',
  mappings: [{ from: '', to: '' }],
  name: '',
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
    const input: ChannelInput = {
      api_key: form.apiKey.trim(),
      base_url: form.baseUrl.trim(),
      enabled: true,
      model_map: modelMap,
      name: form.name.trim(),
      priority: 0,
      type: 'openai',
      weight: 1,
    }
    await client.createChannel(input)
    await finish()
  } catch (e) {
    channelError.value = authErrorMessage(e);
  } finally {
    savingChannel.value = false
  }
}
</script>

<template>
  <div>
    <h3 class="mb-2 text-lg font-semibold">
      初始化 InfiniteChance 管理后台
    </h3>
    <Steps
      :current="step - 1"
      class="mb-6"
      size="small"
    >
      <Step description="创建唯一管理员账号">
        管理员
      </Step>
      <Step description="OpenAI 兼容,可跳过">
        首个渠道
      </Step>
    </Steps>

    <AForm
      v-if="step === 1"
      layout="vertical"
      @submit.prevent="submitAdmin"
    >
      <FormItem label="用户名">
        <Input
          v-model:value="username"
          autocomplete="username"
          name="username"
        />
      </FormItem>
      <FormItem label="密码(至少 8 个字符)">
        <InputPassword
          v-model:value="password"
          autocomplete="new-password"
          name="new-password"
        />
      </FormItem>
      <FormItem label="确认密码">
        <InputPassword
          v-model:value="confirmPassword"
          autocomplete="new-password"
          name="confirm-password"
        />
      </FormItem>

      <p
        v-if="adminError"
        class="mb-3 text-sm text-destructive"
        role="alert"
      >
        {{ adminError }}
      </p>

      <Button
        block
        :loading="creatingAdmin"
        type="primary"
        @click="submitAdmin"
      >
        创建管理员,下一步
      </Button>
    </AForm>

    <AForm
      v-else
      layout="vertical"
      @submit.prevent="submitChannel"
    >
      <FormItem label="渠道名称">
        <Input
          v-model:value="form.name"
          placeholder="例如 deepseek-main"
        />
      </FormItem>
      <FormItem label="BaseURL(含版本路径)">
        <Input
          v-model:value="form.baseUrl"
          placeholder="https://api.openai.com/v1"
        />
      </FormItem>
      <FormItem label="厂商密钥">
        <InputPassword
          v-model:value="form.apiKey"
          autocomplete="off"
          placeholder="sk-…"
        />
      </FormItem>

      <FormItem label="模型映射(公开模型名 → 上游模型名,可留空)">
        <div class="flex flex-col gap-2">
          <div
            v-for="(mapping, index) in form.mappings"
            :key="index"
            class="flex items-center gap-2"
          >
            <Input
              v-model:value="mapping.from"
              placeholder="gpt-4o"
            />
            <span class="text-muted-foreground">→</span>
            <Input
              v-model:value="mapping.to"
              placeholder="gpt-4o-2024-11-20"
            />
            <Button
              :disabled="form.mappings.length === 1"
              danger
              type="text"
              @click="removeMapping(index)"
            >
              删除
            </Button>
          </div>
          <Button
            class="self-start"
            type="dashed"
            @click="addMapping"
          >
            + 添加映射
          </Button>
        </div>
      </FormItem>

      <p
        v-if="channelError"
        class="mb-3 text-sm text-destructive"
        role="alert"
      >
        {{ channelError }}
      </p>

      <div class="flex flex-col gap-2">
        <Button
          block
          :loading="savingChannel"
          type="primary"
          @click="submitChannel"
        >
          创建渠道并完成
        </Button>
        <Button
          block
          :disabled="savingChannel"
          @click="finish"
        >
          跳过,稍后配置
        </Button>
      </div>
    </AForm>
  </div>
</template>
