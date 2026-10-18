<script setup lang="ts">
// 存储设置(19 号票 OSS;23 号票追加 COS 与直连生图转存开关),antd 重写
// (36 号票):local 本地卷为缺省,oss/cos 各走原生 SDK。密钥只写不读
// (渠道密钥同款:仅 has_* 与尾 4 位提示,留空保存 = 保留已存密钥);保存
// 即时生效,画布侧按请求读表。relay_persist 开启要求所选云驱动已配
// public_base_url。48 号票追加素材哈希回填:存量素材补算内容哈希并合并
// 重复对象,手动触发、幂等可重跑、进度轮询(2s;关闭页面不影响任务)。
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Form,
  FormItem,
  Input,
  InputPassword,
  Radio,
  RadioGroup,
  Space,
  Spin,
  Switch,
} from 'ant-design-vue';

import { ApiError, type HashBackfillProgress, type StorageSettings } from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const loading = ref(false);
const error = ref('');
const saving = ref(false);
const savedAt = ref('');
const formError = ref('');

const driver = ref<'cos' | 'local' | 'oss'>('local');
const relayPersist = ref(false);

const ossForm = reactive({
  endpoint: '',
  bucket: '',
  publicBaseUrl: '',
  accessKey: '',
  secretKey: '',
});
const cosForm = reactive({
  endpoint: '',
  bucket: '',
  publicBaseUrl: '',
  secretId: '',
  secretKey: '',
});

const ossHints = reactive({ accessKey: '', secret: '' });
const cosHints = reactive({ secretId: '', secretKey: '' });

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    const settings = await auth.client.getStorageSettings();
    applySettings(settings);
  } catch (e) {
    error.value = authErrorMessage(e);
  } finally {
    loading.value = false;
  }
}

function applySettings(settings: StorageSettings): void {
  driver.value = settings.driver;
  relayPersist.value = settings.relay_persist;
  ossForm.endpoint = settings.oss.endpoint;
  ossForm.bucket = settings.oss.bucket;
  ossForm.publicBaseUrl = settings.oss.public_base_url;
  ossForm.accessKey = ''; // 密钥只写不读:表单永远从空起步
  ossForm.secretKey = '';
  ossHints.accessKey = settings.oss.access_key_hint ?? '';
  ossHints.secret = settings.oss.secret_hint ?? '';
  cosForm.endpoint = settings.cos.endpoint;
  cosForm.bucket = settings.cos.bucket;
  cosForm.publicBaseUrl = settings.cos.public_base_url;
  cosForm.secretId = '';
  cosForm.secretKey = '';
  cosHints.secretId = settings.cos.secret_id_hint ?? '';
  cosHints.secretKey = settings.cos.secret_key_hint ?? '';
  savedAt.value = settings.updated_at;
}

onMounted(() => void refresh());

// 切到本地卷时转存必然失效:视觉上直接取消勾选,提交也按关发送 ——
// 开关在 local 下是禁用的,不联动就出现「开着但禁用」的死状态。
watch(driver, (d) => {
  if (d === 'local') {
    relayPersist.value = false;
  }
});

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  if (relayPersist.value && driver.value === 'local') {
    // watch 已联动取消;真到这里说明竞态,直接按关发送而非卡死表单。
    relayPersist.value = false;
  }
  saving.value = true;
  formError.value = '';
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
    });
    applySettings(settings);
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

function formatSavedAt(): string {
  if (!savedAt.value) {
    return '从未保存(使用缺省本地卷)';
  }
  try {
    return new Date(savedAt.value).toLocaleString('zh-CN', { hour12: false });
  } catch {
    return savedAt.value;
  }
}

// ---- 48 号票:素材哈希回填与去重 ----

const backfill = reactive<HashBackfillProgress>({
  running: false,
  scanned_rows: 0,
  hashed_rows: 0,
  repointed_rows: 0,
  deleted_objects: 0,
});
const backfillBusy = ref(false);
const backfillError = ref('');
// 有过任何动作才显示计数行,避免零值噪音。
const backfillActive = computed(
  () => backfill.scanned_rows > 0 || backfill.hashed_rows > 0 || backfill.repointed_rows > 0 || backfill.deleted_objects > 0,
);
let backfillTimer: ReturnType<typeof setInterval> | null = null;

function stopBackfillPolling(): void {
  if (backfillTimer !== null) {
    clearInterval(backfillTimer);
    backfillTimer = null;
  }
}

function applyBackfillProgress(p: HashBackfillProgress): void {
  Object.assign(backfill, p);
  if (!p.running) {
    stopBackfillPolling();
  }
}

async function pollBackfill(): Promise<void> {
  try {
    applyBackfillProgress(await auth.client.getHashBackfillProgress());
  } catch (e) {
    backfillError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
    stopBackfillPolling();
  }
}

async function startBackfill(): Promise<void> {
  if (backfillBusy.value || backfill.running) {
    return;
  }
  backfillBusy.value = true;
  backfillError.value = '';
  try {
    applyBackfillProgress(await auth.client.startHashBackfill());
    if (backfill.running) {
      backfillTimer = setInterval(() => void pollBackfill(), 2000);
    }
  } catch (e) {
    backfillError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    backfillBusy.value = false;
  }
}

onUnmounted(() => stopBackfillPolling());
</script>

<template>
  <Page title="存储设置">
    <Alert
      v-if="error"
      type="error"
      show-icon
      :message="error"
    />
    <Spin
      v-else-if="loading"
      class="mt-8 block!"
      tip="正在加载存储设置…"
    />

    <Form
      v-else
      layout="vertical"
      class="max-w-[720px]"
    >
      <div class="mb-4 flex items-center justify-between">
        <h3 class="m-0 text-base font-medium">
          对象存储驱动
        </h3>
        <span class="text-neutral-500">{{ formatSavedAt() }}</span>
      </div>

      <FormItem label="驱动">
        <RadioGroup
          v-model:value="driver"
          class="flex flex-col gap-2"
        >
          <Radio value="local">
            本地卷(缺省)—— 生成产物与上传素材落在服务器素材目录
          </Radio>
          <Radio value="oss">
            阿里云 OSS —— 新对象写入云端,历史本地对象读取自动回退
          </Radio>
          <Radio value="cos">
            腾讯云 COS —— 新对象写入云端,历史本地对象读取自动回退
          </Radio>
        </RadioGroup>
      </FormItem>

      <template v-if="driver === 'oss'">
        <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <FormItem
            label="Endpoint"
            required
          >
            <Input
              v-model:value="ossForm.endpoint"
              placeholder="oss-cn-hangzhou.aliyuncs.com"
            />
          </FormItem>
          <FormItem
            label="Bucket"
            required
          >
            <Input
              v-model:value="ossForm.bucket"
              placeholder="my-bucket"
            />
          </FormItem>
          <FormItem
            class="md:col-span-2"
            label="公网访问基地址(bucket 公共读;留空 = 不启用自有公网地址)"
          >
            <Input
              v-model:value="ossForm.publicBaseUrl"
              placeholder="https://my-bucket.oss-cn-hangzhou.aliyuncs.com"
            />
          </FormItem>
          <FormItem :label="`AccessKey ID${ossHints.accessKey ? `(已存 ${ossHints.accessKey},留空保持)` : ''}`">
            <InputPassword
              v-model:value="ossForm.accessKey"
              autocomplete="off"
              placeholder="LTAI…"
            />
          </FormItem>
          <FormItem :label="`AccessKey Secret${ossHints.secret ? `(已存 ${ossHints.secret},留空保持)` : ''}`">
            <InputPassword
              v-model:value="ossForm.secretKey"
              autocomplete="off"
              placeholder="留空 = 保留已存密钥"
            />
          </FormItem>
        </div>
      </template>

      <template v-if="driver === 'cos'">
        <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <FormItem
            label="Endpoint(地域域名)"
            required
          >
            <Input
              v-model:value="cosForm.endpoint"
              placeholder="https://cos.ap-guangzhou.myqcloud.com"
            />
          </FormItem>
          <FormItem
            label="Bucket(带 APPID 后缀的完整桶名)"
            required
          >
            <Input
              v-model:value="cosForm.bucket"
              placeholder="my-bucket-1250000000"
            />
          </FormItem>
          <FormItem
            class="md:col-span-2"
            label="公网访问基地址(bucket 公共读;留空 = 不启用自有公网地址)"
          >
            <Input
              v-model:value="cosForm.publicBaseUrl"
              placeholder="https://my-bucket-1250000000.cos.ap-guangzhou.myqcloud.com"
            />
          </FormItem>
          <FormItem :label="`SecretId${cosHints.secretId ? `(已存 ${cosHints.secretId},留空保持)` : ''}`">
            <InputPassword
              v-model:value="cosForm.secretId"
              autocomplete="off"
              placeholder="AKID…"
            />
          </FormItem>
          <FormItem :label="`SecretKey${cosHints.secretKey ? `(已存 ${cosHints.secretKey},留空保持)` : ''}`">
            <InputPassword
              v-model:value="cosForm.secretKey"
              autocomplete="off"
              placeholder="留空 = 保留已存密钥"
            />
          </FormItem>
        </div>
      </template>

      <FormItem>
        <Switch
          v-model:checked="relayPersist"
          :disabled="driver === 'local'"
        />
        <span class="ml-2 inline-block max-w-[560px] align-middle text-neutral-500">
          直连生图产物转存 —— /v1/images 的产物落进所选云桶,响应 URL 改写为永久地址并记录素材行(需公网基地址)
        </span>
      </FormItem>

      <Alert
        v-if="formError"
        class="mb-4"
        type="error"
        show-icon
        :message="formError"
      />

      <Space align="center">
        <Button
          type="primary"
          :loading="saving"
          @click="submit"
        >
          保存
        </Button>
        <span class="text-neutral-500">保存后即时生效,无需重启服务</span>
      </Space>

      <div class="mt-8 border-t border-neutral-200 pt-6">
        <h3 class="m-0 mb-2 text-base font-medium">
          素材哈希回填与去重
        </h3>
        <p class="mb-4 max-w-[640px] text-neutral-500">
          对存量素材补算内容哈希(SHA-256 指纹)并合并重复对象:相同字节全库只留一份,
          其余行改指同一对象后删除冗余字节。新上传已自动去重,存量合并在升级后手动触发一次即可。
          回填会整库读一遍对象字节(云存储计读流量);幂等可重跑,任务在跑时再点不重复起任务,无独立取消。
        </p>
        <Space
          align="center"
          wrap
        >
          <Button
            :loading="backfillBusy"
            :disabled="backfill.running"
            @click="startBackfill"
          >
            {{ backfill.running ? '回填进行中…' : '开始回填' }}
          </Button>
          <span
            v-if="backfill.running"
            class="text-neutral-500"
          >正在后台推进,关闭页面不影响任务</span>
        </Space>
        <Alert
          v-if="backfillError"
          class="mt-4"
          type="error"
          show-icon
          :message="backfillError"
        />
        <p
          v-if="backfillActive"
          class="mb-0 mt-4 text-neutral-600"
        >
          本次回填:已扫 {{ backfill.scanned_rows }} 行 · 已算哈希 {{ backfill.hashed_rows }} 行 ·
          已改指 {{ backfill.repointed_rows }} 行 · 已删冗余对象 {{ backfill.deleted_objects }} 个
        </p>
      </div>
    </Form>
  </Page>
</template>
