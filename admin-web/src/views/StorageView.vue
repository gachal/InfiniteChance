<script setup lang="ts">
// 存储设置(19 号票 OSS;23 号票追加 COS 与直连生图转存开关):
// local 本地卷为缺省,oss/cos 各走原生 SDK。密钥只写不读(渠道密钥同款:
// 仅 has_* 与尾 4 位提示,留空保存 = 保留已存密钥);保存即时生效,画布
// 侧按请求读表。relay_persist 开启要求所选云驱动已配 public_base_url。
import { onMounted, reactive, ref, watch } from 'vue'

import { ApiError, type StorageSettings } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const auth = useAuth()

const loading = ref(false)
const error = ref('')
const saving = ref(false)
const savedAt = ref('')
const formError = ref('')

const driver = ref<'local' | 'oss' | 'cos'>('local')
const relayPersist = ref(false)

const ossForm = reactive({
  endpoint: '',
  bucket: '',
  publicBaseUrl: '',
  accessKey: '',
  secretKey: '',
})
const cosForm = reactive({
  endpoint: '',
  bucket: '',
  publicBaseUrl: '',
  secretId: '',
  secretKey: '',
})

const ossHints = reactive({ accessKey: '', secret: '' })
const cosHints = reactive({ secretId: '', secretKey: '' })

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
  driver.value = settings.driver
  relayPersist.value = settings.relay_persist
  ossForm.endpoint = settings.oss.endpoint
  ossForm.bucket = settings.oss.bucket
  ossForm.publicBaseUrl = settings.oss.public_base_url
  ossForm.accessKey = '' // 密钥只写不读:表单永远从空起步
  ossForm.secretKey = ''
  ossHints.accessKey = settings.oss.access_key_hint ?? ''
  ossHints.secret = settings.oss.secret_hint ?? ''
  cosForm.endpoint = settings.cos.endpoint
  cosForm.bucket = settings.cos.bucket
  cosForm.publicBaseUrl = settings.cos.public_base_url
  cosForm.secretId = ''
  cosForm.secretKey = ''
  cosHints.secretId = settings.cos.secret_id_hint ?? ''
  cosHints.secretKey = settings.cos.secret_key_hint ?? ''
  savedAt.value = settings.updated_at
}

onMounted(() => void refresh())

// 切到本地卷时转存必然失效:视觉上直接取消勾选,提交也按关发送 ——
// 勾选框在 local 下是禁用的,不联动就出现「勾着但禁用」的死状态。
watch(driver, (d) => {
  if (d === 'local') {
    relayPersist.value = false
  }
})

async function submit(): Promise<void> {
  if (saving.value) {
    return
  }
  if (relayPersist.value && driver.value === 'local') {
    // watch 已联动取消勾选;真到这里说明竞态,直接按关发送而非卡死表单。
    relayPersist.value = false
  }
  saving.value = true
  formError.value = ''
  try {
    const settings = await auth.client.updateStorageSettings({
      driver: driver.value,
      relay_persist: relayPersist.value,
      // 只发所选驱动的块:另一家连接在服务端原样保留(不带块 = 不动)。
      ...(driver.value === 'oss'
        ? {
            oss: {
              endpoint: ossForm.endpoint.trim(),
              bucket: ossForm.bucket.trim(),
              public_base_url: ossForm.publicBaseUrl.trim(),
              access_key: ossForm.accessKey.trim(),
              secret_key: ossForm.secretKey.trim(),
            },
          }
        : {}),
      ...(driver.value === 'cos'
        ? {
            cos: {
              endpoint: cosForm.endpoint.trim(),
              bucket: cosForm.bucket.trim(),
              public_base_url: cosForm.publicBaseUrl.trim(),
              secret_id: cosForm.secretId.trim(),
              secret_key: cosForm.secretKey.trim(),
            },
          }
        : {}),
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
              v-model="driver"
              type="radio"
              value="local"
            >
            <span>本地卷(缺省)—— 生成产物与上传素材落在服务器素材目录</span>
          </label>
          <label class="radio">
            <input
              v-model="driver"
              type="radio"
              value="oss"
            >
            <span>阿里云 OSS —— 新对象写入云端,历史本地对象读取自动回退</span>
          </label>
          <label class="radio">
            <input
              v-model="driver"
              type="radio"
              value="cos"
            >
            <span>腾讯云 COS —— 新对象写入云端,历史本地对象读取自动回退</span>
          </label>
        </fieldset>

        <template v-if="driver === 'oss'">
          <label>
            <span>Endpoint</span>
            <input
              v-model="ossForm.endpoint"
              type="text"
              required
              placeholder="oss-cn-hangzhou.aliyuncs.com"
            >
          </label>
          <label>
            <span>Bucket</span>
            <input
              v-model="ossForm.bucket"
              type="text"
              required
              placeholder="my-bucket"
            >
          </label>
          <label class="wide">
            <span>公网访问基地址(bucket 公共读;留空 = 不启用自有公网地址)</span>
            <input
              v-model="ossForm.publicBaseUrl"
              type="text"
              placeholder="https://my-bucket.oss-cn-hangzhou.aliyuncs.com"
            >
          </label>
          <label>
            <span>AccessKey ID{{ ossHints.accessKey ? `(已存 ${ossHints.accessKey},留空保持)` : '' }}</span>
            <input
              v-model="ossForm.accessKey"
              type="password"
              autocomplete="off"
              placeholder="LTAI…"
            >
          </label>
          <label>
            <span>AccessKey Secret{{ ossHints.secret ? `(已存 ${ossHints.secret},留空保持)` : '' }}</span>
            <input
              v-model="ossForm.secretKey"
              type="password"
              autocomplete="off"
              placeholder="留空 = 保留已存密钥"
            >
          </label>
        </template>

        <template v-if="driver === 'cos'">
          <label>
            <span>Endpoint(地域域名)</span>
            <input
              v-model="cosForm.endpoint"
              type="text"
              required
              placeholder="https://cos.ap-guangzhou.myqcloud.com"
            >
          </label>
          <label>
            <span>Bucket(带 APPID 后缀的完整桶名)</span>
            <input
              v-model="cosForm.bucket"
              type="text"
              required
              placeholder="my-bucket-1250000000"
            >
          </label>
          <label class="wide">
            <span>公网访问基地址(bucket 公共读;留空 = 不启用自有公网地址)</span>
            <input
              v-model="cosForm.publicBaseUrl"
              type="text"
              placeholder="https://my-bucket-1250000000.cos.ap-guangzhou.myqcloud.com"
            >
          </label>
          <label>
            <span>SecretId{{ cosHints.secretId ? `(已存 ${cosHints.secretId},留空保持)` : '' }}</span>
            <input
              v-model="cosForm.secretId"
              type="password"
              autocomplete="off"
              placeholder="AKID…"
            >
          </label>
          <label>
            <span>SecretKey{{ cosHints.secretKey ? `(已存 ${cosHints.secretKey},留空保持)` : '' }}</span>
            <input
              v-model="cosForm.secretKey"
              type="password"
              autocomplete="off"
              placeholder="留空 = 保留已存密钥"
            >
          </label>
        </template>

        <label class="wide checkbox">
          <input
            v-model="relayPersist"
            type="checkbox"
            :disabled="driver === 'local'"
          >
          <span>直连生图产物转存 —— /v1/images 的产物落进所选云桶,响应 URL 改写为永久地址并记录素材行(需公网基地址)</span>
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
          <span class="muted">保存后即时生效,无需重启服务</span>
        </div>
      </form>
    </section>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
/* 存储视图私有样式:驱动单选、转存勾选与提示行。 */
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

label.radio,
label.checkbox {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 14px;
}

label.checkbox span {
  color: #aab1c5;
}

.actions {
  display: flex;
  align-items: center;
  gap: 14px;
}
</style>
