<script setup lang="ts">
// 存储设置(19 号票):对象存储驱动的动态配置 —— local 本地卷为缺省,
// oss 走阿里云原生 SDK。AK/SK 只写不读(渠道密钥同款:仅 has_* 与尾 4
// 位提示,留空保存 = 保留已存密钥);保存即时生效,画布侧按请求读表。
import { onMounted, reactive, ref } from 'vue'

import { ApiError, type StorageSettings } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const auth = useAuth()

const loading = ref(false)
const error = ref('')
const saving = ref(false)
const savedAt = ref('')
const formError = ref('')

interface StorageForm {
  driver: 'local' | 'oss'
  endpoint: string
  bucket: string
  publicBaseUrl: string
  accessKey: string
  secretKey: string
}

const form = reactive<StorageForm>({
  driver: 'local',
  endpoint: '',
  bucket: '',
  publicBaseUrl: '',
  accessKey: '',
  secretKey: '',
})

const hints = reactive({ accessKey: '', secret: '' })

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const settings = await auth.client.getStorageSettings()
    applySettings(settings)
  } catch (e) {
    error.value = authErrorMessage(e)
  } finally {
    loading.value = false
  }
}

function applySettings(settings: StorageSettings): void {
  form.driver = settings.driver
  form.endpoint = settings.oss.endpoint
  form.bucket = settings.oss.bucket
  form.publicBaseUrl = settings.oss.public_base_url
  form.accessKey = '' // 密钥只写不读:表单永远从空起步
  form.secretKey = ''
  hints.accessKey = settings.oss.access_key_hint ?? ''
  hints.secret = settings.oss.secret_hint ?? ''
  savedAt.value = settings.updated_at
}

onMounted(() => void refresh())

async function submit(): Promise<void> {
  if (saving.value) {
    return
  }
  saving.value = true
  formError.value = ''
  try {
    const settings = await auth.client.updateStorageSettings({
      driver: form.driver,
      oss: {
        endpoint: form.endpoint.trim(),
        bucket: form.bucket.trim(),
        public_base_url: form.publicBaseUrl.trim(),
        access_key: form.accessKey.trim(),
        secret_key: form.secretKey.trim(),
      },
    })
    applySettings(settings)
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e)
  } finally {
    saving.value = false
  }
}

function formatSavedAt(): string {
  if (!savedAt.value) {
    return '从未保存(使用缺省本地卷)'
  }
  try {
    return new Date(savedAt.value).toLocaleString()
  } catch {
    return savedAt.value
  }
}
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>存储设置</h2>
    </div>

    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-else-if="loading"
      class="muted"
    >
      正在加载存储设置…
    </p>

    <section
      v-if="!error && !loading"
      class="card"
    >
      <div class="card-head">
        <h3>对象存储驱动</h3>
        <span class="muted">{{ formatSavedAt() }}</span>
      </div>

      <form
        class="grid"
        @submit.prevent="submit"
      >
        <fieldset class="wide">
          <legend>驱动</legend>
          <label class="radio">
            <input
              v-model="form.driver"
              type="radio"
              value="local"
            >
            <span>本地卷(缺省)—— 生成产物与上传素材落在服务器素材目录</span>
          </label>
          <label class="radio">
            <input
              v-model="form.driver"
              type="radio"
              value="oss"
            >
            <span>阿里云 OSS —— 新对象写入云端,历史本地对象读取自动回退</span>
          </label>
        </fieldset>

        <template v-if="form.driver === 'oss'">
          <label>
            <span>Endpoint</span>
            <input
              v-model="form.endpoint"
              type="text"
              required
              placeholder="oss-cn-hangzhou.aliyuncs.com"
            >
          </label>
          <label>
            <span>Bucket</span>
            <input
              v-model="form.bucket"
              type="text"
              required
              placeholder="my-bucket"
            >
          </label>
          <label class="wide">
            <span>公网访问基地址(bucket 公共读;留空 = 不启用自有公网地址)</span>
            <input
              v-model="form.publicBaseUrl"
              type="text"
              placeholder="https://my-bucket.oss-cn-hangzhou.aliyuncs.com"
            >
          </label>
          <label>
            <span>AccessKey ID{{ hints.accessKey ? `(已存 ${hints.accessKey},留空保持)` : '' }}</span>
            <input
              v-model="form.accessKey"
              type="password"
              autocomplete="off"
              placeholder="LTAI…"
            >
          </label>
          <label>
            <span>AccessKey Secret{{ hints.secret ? `(已存 ${hints.secret},留空保持)` : '' }}</span>
            <input
              v-model="form.secretKey"
              type="password"
              autocomplete="off"
              placeholder="留空 = 保留已存密钥"
            >
          </label>
        </template>

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
          <span class="muted">保存后即时生效,无需重启服务</span>
        </div>
      </form>
    </section>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* 存储视图私有样式:驱动单选与提示行。 */
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

label.radio {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 14px;
}
</style>
