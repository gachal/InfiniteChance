<script setup lang="ts">
// API Key 管理:创建(完整值仅显示一次)、吊销、过期、手工充值与额度流水,
// antd 重写(36 号票)。微美元不变量在服务端,前端只过人类 USD 数字。
import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  DatePicker,
  Drawer,
  Form,
  FormItem,
  Input,
  InputNumber,
  Modal,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'ant-design-vue';

import {
  ApiError,
  type ApiKeyRecord,
  type CreatedApiKey,
  type QuotaEntry,
} from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const keys = ref<ApiKeyRecord[]>([]);
const loading = ref(false);
const error = ref('');

// ---- 创建表单与一次性展示 ----
const createOpen = ref(false);
const saving = ref(false);
const formError = ref('');
const formState = reactive({ expiresAt: '', initialQuota: undefined as number | undefined, name: '' });
const createdKey = ref<CreatedApiKey | null>(null);

// ---- 充值与流水 ----
const topupTarget = ref<ApiKeyRecord | null>(null);
const topupAmount = ref<number | undefined>(undefined);
const toppingUp = ref(false);
const topupError = ref('');
const logTarget = ref<ApiKeyRecord | null>(null);
const logEntries = ref<QuotaEntry[]>([]);
const logLoading = ref(false);
const logError = ref('');

const statusLabel: Record<string, string> = {
  active: '有效',
  revoked: '已吊销',
  expired: '已过期',
};

const statusColor: Record<string, string> = {
  active: 'success',
  expired: 'warning',
  revoked: 'error',
};

const columns = [
  { key: 'name', title: '名称' },
  { dataIndex: 'prefix', key: 'prefix', title: 'Key', width: 130 },
  { dataIndex: 'quota_usd', key: 'quota_usd', title: '余额', width: 130 },
  { key: 'expires_at', title: '过期时间', width: 180 },
  { key: 'created_at', title: '创建时间', width: 180 },
  { key: 'actions', title: '操作', width: 200 },
];

const logColumns = [
  { key: 'created_at', title: '时间', width: 170 },
  { key: 'delta_usd', title: '变动' },
  { dataIndex: 'balance_usd', key: 'balance_usd', title: '变动后余额' },
  { key: 'reason', title: '原因' },
];

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    keys.value = await auth.client.listKeys();
  } catch (e) {
    error.value = authErrorMessage(e);
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

// ---- 创建 ----

function openCreate(): void {
  createdKey.value = null;
  Object.assign(formState, { expiresAt: '', initialQuota: undefined, name: '' });
  formError.value = '';
  createOpen.value = true;
}

function closeCreate(): void {
  createOpen.value = false;
  createdKey.value = null;
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  if (formState.name.trim() === '') {
    formError.value = '请输入名称';
    return;
  }
  saving.value = true;
  formError.value = '';
  try {
    const input: { expires_at?: string; initial_quota_usd?: number; name: string } = {
      name: formState.name.trim(),
    };
    if (formState.expiresAt !== '') {
      input.expires_at = new Date(formState.expiresAt.replace(' ', 'T')).toISOString();
    }
    if (formState.initialQuota !== undefined) {
      input.initial_quota_usd = formState.initialQuota;
    }
    createdKey.value = await auth.client.createKey(input);
    await refresh();
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

// ---- 吊销 ----

function revoke(key: ApiKeyRecord): void {
  Modal.confirm({
    title: `确定吊销 key「${key.name}」(${key.prefix}…)?`,
    content: '使用它的请求将立即被拒绝。',
    okText: '吊销',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await auth.client.revokeKey(key.id);
        await refresh();
      } catch (e) {
        message.error(authErrorMessage(e));
      }
    },
  });
}

// ---- 充值 ----

function openTopUp(key: ApiKeyRecord): void {
  topupTarget.value = key;
  topupAmount.value = undefined;
  topupError.value = '';
}

async function submitTopUp(): Promise<void> {
  const target = topupTarget.value;
  if (target === null || toppingUp.value) {
    return;
  }
  const amount = topupAmount.value;
  if (amount === undefined || !Number.isFinite(amount) || amount <= 0) {
    topupError.value = '请输入大于 0 的金额';
    return;
  }
  toppingUp.value = true;
  topupError.value = '';
  try {
    const updated = await auth.client.topUpKey(target.id, amount);
    topupTarget.value = null;
    // 充值后余额即时可见:列表原地替换,不打断当前视图。
    keys.value = keys.value.map((k) => (k.id === updated.id ? updated : k));
  } catch (e) {
    topupError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    toppingUp.value = false;
  }
}

// ---- 额度流水 ----

const reasonLabel: Record<string, string> = {
  initial: '初始额度',
  manual_topup: '手工充值',
};

async function openLog(key: ApiKeyRecord): Promise<void> {
  logTarget.value = key;
  logEntries.value = [];
  logError.value = '';
  logLoading.value = true;
  try {
    logEntries.value = await auth.client.keyQuotaLog(key.id);
  } catch (e) {
    logError.value = authErrorMessage(e);
  } finally {
    logLoading.value = false;
  }
}

// ---- 展示辅助 ----

function formatUSD(usd: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(usd);
}

function formatTime(iso: null | string): string {
  if (iso === null) {
    return '—';
  }
  return new Date(iso).toLocaleString('zh-CN', { hour12: false });
}

const formHint = computed(() =>
  formState.expiresAt === '' ? '永不过期' : `过期时间:${formState.expiresAt}`,
);
</script>

<template>
  <Page title="API Key 管理">
    <div class="flex flex-col gap-4">
      <div class="flex justify-end">
        <Button
          type="primary"
          @click="openCreate"
        >
          新建 Key
        </Button>
      </div>

      <Alert
        v-if="error"
        type="error"
        show-icon
        :message="error"
      />

      <Table
        :columns="columns"
        :data-source="keys"
        :loading="loading"
        :pagination="false"
        row-key="id"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <span class="font-medium">{{ record.name }}</span>
            <Tag
              class="ml-2"
              :color="statusColor[record.status] ?? 'default'"
            >
              {{ statusLabel[record.status] ?? record.status }}
            </Tag>
          </template>

          <template v-else-if="column.key === 'prefix'">
            <code>{{ record.prefix }}…</code>
          </template>

          <template v-else-if="column.key === 'quota_usd'">
            <span class="font-medium">{{ formatUSD(record.quota_usd) }}</span>
          </template>

          <template v-else-if="column.key === 'expires_at'">
            {{ formatTime(record.expires_at) }}
          </template>

          <template v-else-if="column.key === 'created_at'">
            {{ formatTime(record.created_at) }}
          </template>

          <template v-else-if="column.key === 'actions'">
            <Space :size="0">
              <Button
                type="link"
                size="small"
                @click="openTopUp(record as ApiKeyRecord)"
              >
                充值
              </Button>
              <Button
                type="link"
                size="small"
                @click="openLog(record as ApiKeyRecord)"
              >
                额度记录
              </Button>
              <Button
                type="link"
                size="small"
                danger
                :disabled="record.status === 'revoked'"
                @click="revoke(record as ApiKeyRecord)"
              >
                吊销
              </Button>
            </Space>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            还没有 key。点击「新建 Key」发放第一把。
          </div>
        </template>
      </Table>
    </div>

    <!-- 创建:表单态与一次性完整值展示态共用一个 Modal -->
    <Modal
      v-model:open="createOpen"
      :title="createdKey ? 'Key 已创建,请立即保存' : '新建 Key'"
      :mask-closable="false"
      :closable="!createdKey"
      :keyboard="!createdKey"
      @cancel="closeCreate"
    >
      <template v-if="createdKey">
        <Alert
          type="warning"
          show-icon
          message="完整 key 只显示这一次,关闭后无法再次查看,请复制保存。"
        />
        <div class="mt-3 flex items-center gap-2">
          <Typography.Text
            class="break-all"
            copyable
            :content="createdKey.key"
          />
        </div>
      </template>

      <Form
        v-else
        layout="vertical"
        class="mt-2"
      >
        <FormItem
          label="名称"
          required
        >
          <Input
            v-model:value="formState.name"
            placeholder="例如 canvas-service"
          />
        </FormItem>
        <FormItem label="过期时间(留空 = 永不过期)">
          <DatePicker
            v-model:value="formState.expiresAt"
            class="w-full"
            show-time
            value-format="YYYY-MM-DD HH:mm:ss"
            placeholder="留空 = 永不过期"
          />
        </FormItem>
        <FormItem label="初始额度(USD)">
          <InputNumber
            v-model:value="formState.initialQuota"
            class="w-full"
            :min="0"
            :step="0.01"
            placeholder="0.00"
          />
        </FormItem>
        <p class="mb-0 text-neutral-500">
          {{ formHint }}
        </p>
        <Alert
          v-if="formError"
          class="mt-3"
          type="error"
          show-icon
          :message="formError"
        />
      </Form>

      <template #footer>
        <template v-if="createdKey">
          <Button
            type="primary"
            @click="closeCreate"
          >
            我已保存
          </Button>
        </template>
        <template v-else>
          <Button @click="closeCreate">
            取消
          </Button>
          <Button
            type="primary"
            :loading="saving"
            @click="submit"
          >
            创建
          </Button>
        </template>
      </template>
    </Modal>

    <!-- 充值 -->
    <Modal
      :open="topupTarget !== null"
      :title="topupTarget ? `给「${topupTarget.name}」(${topupTarget.prefix}…)充值` : ''"
      :confirm-loading="toppingUp"
      :mask-closable="false"
      ok-text="确认充值"
      cancel-text="取消"
      @ok="submitTopUp"
      @cancel="topupTarget = null"
    >
      <p class="mb-1">
        充值金额(USD)
      </p>
      <InputNumber
        v-model:value="topupAmount"
        class="w-full!"
        :min="0.01"
        :step="0.01"
        autofocus
      />
      <Alert
        v-if="topupError"
        class="mt-3"
        type="error"
        show-icon
        :message="topupError"
      />
    </Modal>

    <!-- 额度流水 -->
    <Drawer
      :open="logTarget !== null"
      :title="logTarget ? `额度记录 · ${logTarget.name}(${logTarget.prefix}…)` : ''"
      width="560"
      @close="logTarget = null"
    >
      <Alert
        v-if="logError"
        type="error"
        show-icon
        :message="logError"
      />
      <Table
        v-else
        :columns="logColumns"
        :data-source="logEntries"
        :loading="logLoading"
        :pagination="false"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'created_at'">
            {{ formatTime(record.created_at) }}
          </template>
          <template v-else-if="column.key === 'delta_usd'">
            <span :class="record.delta_usd >= 0 ? 'text-green-600' : 'text-red-500'">
              {{ record.delta_usd >= 0 ? '+' : '' }}{{ formatUSD(record.delta_usd) }}
            </span>
          </template>
          <template v-else-if="column.key === 'balance_usd'">
            {{ formatUSD(record.balance_usd) }}
          </template>
          <template v-else-if="column.key === 'reason'">
            {{ reasonLabel[record.reason] ?? record.reason }}
          </template>
        </template>
        <template #emptyText>
          <div class="py-6 text-neutral-500">
            暂无记录
          </div>
        </template>
      </Table>
    </Drawer>
  </Page>
</template>
