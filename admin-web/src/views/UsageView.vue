<script setup lang="ts">
// 用量审计(15 号票,antd 重写 36 号票):请求级日志列表与按天/模型/渠道
// 汇总。列表与汇总共用同一套过滤(时间/key/渠道/模型/状态/来源),后端用
// 同一 WHERE 落查询,两边的请求数与扣费天然对账一致。失败行带上游错误摘要,
// 画布来源标记单列区分;分页靠过滤后总数 total 驱动。
import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Button,
  DatePicker,
  Input,
  RadioButton,
  RadioGroup,
  Select,
  Table,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import {
  ApiError,
  type ApiKeyRecord,
  type Channel,
  type UsageBucket,
  type UsageLogRecord,
} from '@infinitechance/api';

import { useAuth } from '../auth';

const { client } = useAuth();

// 视图模式:请求明细,或三种汇总维度;过滤条对四种模式一体生效。
type Mode = 'logs' | 'day' | 'model' | 'channel';
const modes: { key: Mode; label: string }[] = [
  { key: 'logs', label: '请求明细' },
  { key: 'day', label: '按天汇总' },
  { key: 'model', label: '按模型汇总' },
  { key: 'channel', label: '按渠道汇总' },
];
const mode = ref<Mode>('logs');

const range = ref<[string, string] | undefined>(undefined);
// 下拉选中值是 option 的字符串 value,转数字发生在组装 API 参数时。
const keyFilter = ref('');
const channelFilter = ref('');
const modelInput = ref('');
const statusFilter = ref<'' | 'success' | 'upstream_error'>('');
const sourceFilter = ref<'' | 'canvas' | 'direct'>('');

const keys = ref<ApiKeyRecord[]>([]);
const channels = ref<Channel[]>([]);

const logs = ref<UsageLogRecord[]>([]);
const total = ref(0);
const offset = ref(0);
const buckets = ref<UsageBucket[]>([]);
const loading = ref(false);
const error = ref('');

const pageSize = 50;

const logColumns = [
  { key: 'created_at', title: '时间', width: 170 },
  { key: 'key_id', title: 'Key', width: 130 },
  { dataIndex: 'channel_name', key: 'channel_name', title: '渠道', width: 130 },
  { key: 'model', title: '模型' },
  { key: 'quantity', title: '用量' },
  { key: 'duration', title: '耗时', width: 80 },
  { key: 'status', title: '状态', width: 150 },
  { key: 'charge', title: '扣费', width: 110 },
  { key: 'source', title: '来源', width: 90 },
];

const bucketColumns = [
  { key: 'label', title: '维度' },
  { dataIndex: 'requests', key: 'requests', title: '请求数', width: 100 },
  { key: 'errors', title: '失败', width: 80 },
  { key: 'charge', title: '扣费合计', width: 130 },
];

// 过滤条件 → API 参数;RangePicker(valueFormat 字符串)按本地时区解释,
// 转成 RFC3339 上送。
function auditParams() {
  return {
    from: range.value?.[0] ? new Date(range.value[0].replace(' ', 'T')).toISOString() : undefined,
    to: range.value?.[1] ? new Date(range.value[1].replace(' ', 'T')).toISOString() : undefined,
    key_id: keyFilter.value === '' ? undefined : Number(keyFilter.value),
    channel_id: channelFilter.value === '' ? undefined : Number(channelFilter.value),
    model: modelInput.value.trim() || undefined,
    status: statusFilter.value || undefined,
    source: sourceFilter.value || undefined,
  };
}

async function refresh(nextOffset = 0): Promise<void> {
  loading.value = true;
  error.value = '';
  offset.value = nextOffset;
  try {
    if (mode.value === 'logs') {
      const page = await client.listUsageLogs({ ...auditParams(), limit: pageSize, offset: nextOffset });
      logs.value = page.logs;
      total.value = page.total;
    } else {
      buckets.value = await client.usageSummary({ ...auditParams(), by: mode.value });
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载用量数据,请稍后再试';
  } finally {
    loading.value = false;
  }
}

function setMode(next: Mode): void {
  if (mode.value === next) {
    return;
  }
  mode.value = next;
  void refresh(0);
}

function search(): void {
  void refresh(0);
}

function handlePage(page: number): void {
  void refresh((page - 1) * pageSize);
}

const pagination = computed(() => ({
  current: Math.floor(offset.value / pageSize) + 1,
  pageSize,
  showSizeChanger: false,
  total: total.value,
  onChange: handlePage,
}));

const keyName = computed(() => {
  const map = new Map(keys.value.map((k) => [k.id, k.name]));
  return (id: number) => map.get(id) ?? `#${id}`;
});

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false });
}

function formatDuration(ms: number): string {
  if (ms <= 0) {
    return '—';
  }
  if (ms < 1000) {
    return `${ms} ms`;
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(1)} s`;
  }
  return `${(ms / 60_000).toFixed(1)} min`;
}

function formatUSD(usd: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(usd);
}

// 计费单位与数量:token 轨读列,按次/按秒轨读快照里的请求事实(张/秒)。
function formatQuantity(l: UsageLogRecord): string {
  if (l.unit === 'token') {
    return `${l.prompt_tokens} + ${l.completion_tokens} tok`;
  }
  const n = l.request?.n;
  const amount = n === undefined || n === null ? '—' : String(n);
  if (l.unit === 'call') {
    return `${amount} 张${l.request?.size ? ` · ${l.request.size}` : ''}`;
  }
  return `${amount} 秒${l.request?.size ? ` · ${l.request.size}` : ''}`;
}

// 来源标记:画布侧解析出画布 id(完整标记悬停可见),空 = 直连流量;
// 其它自报标记原样显示。
function sourceLabel(l: UsageLogRecord): string {
  if (!l.source) {
    return '直连';
  }
  const m = /canvas=(\d+)/.exec(l.source);
  return m ? `画布 ${m[1]}` : l.source;
}

function bucketLabel(b: UsageBucket): string {
  if ('day' in b) {
    return b.day;
  }
  if ('model' in b) {
    return b.model;
  }
  return b.channel_name || `渠道 #${b.channel_id}`;
}

onMounted(() => {
  void refresh();
  void client
    .listKeys()
    .then((list) => {
      keys.value = list;
    })
    .catch(() => {
      /* key 下拉拉不到就不影响主列表 */
    });
  void client
    .listChannels()
    .then((list) => {
      channels.value = list;
    })
    .catch(() => {
      /* 渠道下拉拉不到就不影响主列表 */
    });
});
</script>

<template>
  <Page title="用量审计">
    <div class="flex flex-col gap-4">
      <div>
        <RadioGroup
          :value="mode"
          button-style="solid"
          @update:value="setMode($event as Mode)"
        >
          <RadioButton
            v-for="m in modes"
            :key="m.key"
            :value="m.key"
          >
            {{ m.label }}
          </RadioButton>
        </RadioGroup>
      </div>

      <!-- 过滤条:明细与汇总共用同一套条件 -->
      <div class="flex flex-wrap items-center gap-2">
        <DatePicker.RangePicker
          v-model:value="range"
          show-time
          value-format="YYYY-MM-DD HH:mm:ss"
          :placeholder="['从', '到']"
        />
        <Select
          v-model:value="keyFilter"
          class="w-36"
          aria-label="按 key 过滤"
          :options="[{ label: '全部 key', value: '' }, ...keys.map((k) => ({ label: k.name, value: String(k.id) }))]"
        />
        <Select
          v-model:value="channelFilter"
          class="w-36"
          aria-label="按渠道过滤"
          :options="[{ label: '全部渠道', value: '' }, ...channels.map((c) => ({ label: c.name, value: String(c.id) }))]"
        />
        <Input
          v-model:value="modelInput"
          class="w-44"
          placeholder="公开模型(精确)"
          aria-label="按模型过滤"
          allow-clear
          @press-enter="search"
        />
        <Select
          v-model:value="statusFilter"
          class="w-28"
          aria-label="按状态过滤"
          :options="[
            { label: '全部状态', value: '' },
            { label: '成功', value: 'success' },
            { label: '失败', value: 'upstream_error' },
          ]"
        />
        <Select
          v-model:value="sourceFilter"
          class="w-28"
          aria-label="按来源过滤"
          :options="[
            { label: '全部来源', value: '' },
            { label: '画布', value: 'canvas' },
            { label: '直连', value: 'direct' },
          ]"
        />
        <Button
          type="primary"
          :loading="loading"
          @click="search"
        >
          查询
        </Button>
      </div>

      <p
        v-if="error"
        class="mb-0 text-red-500"
        role="alert"
      >
        {{ error }}
      </p>

      <!-- 请求明细:时间/key/渠道/模型/用量/耗时/状态/扣费/来源 -->
      <Table
        v-if="mode === 'logs'"
        :columns="logColumns"
        :data-source="logs"
        :loading="loading"
        :pagination="pagination"
        row-key="id"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'created_at'">
            {{ formatTime(record.created_at) }}
          </template>

          <template v-else-if="column.key === 'key_id'">
            {{ keyName(record.key_id) }}
          </template>

          <template v-else-if="column.key === 'model'">
            {{ record.public_model }}
            <Tooltip
              v-if="record.upstream_model !== record.public_model"
              :title="`上游模型 ${record.upstream_model}`"
            >
              <span class="cursor-help text-neutral-400">↗</span>
            </Tooltip>
          </template>

          <template v-else-if="column.key === 'quantity'">
            {{ formatQuantity(record as UsageLogRecord) }}
          </template>

          <template v-else-if="column.key === 'duration'">
            {{ formatDuration(record.duration_ms) }}
          </template>

          <template v-else-if="column.key === 'status'">
            <Tag
              class="mr-0"
              :color="record.status === 'success' ? 'success' : 'error'"
            >
              {{ record.status === 'success' ? '成功' : '失败' }}
            </Tag>
            <Tooltip
              v-if="record.upstream_error"
              :title="record.upstream_error"
            >
              <div class="mt-1 max-w-[240px] truncate text-xs text-red-400">
                {{ record.upstream_error }}
              </div>
            </Tooltip>
          </template>

          <template v-else-if="column.key === 'charge'">
            {{ formatUSD(record.charge_usd) }}
          </template>

          <template v-else-if="column.key === 'source'">
            <Tooltip :title="record.source || '直连流量'">
              <Tag
                class="mr-0 cursor-default"
                :color="record.source ? 'processing' : 'default'"
              >
                {{ sourceLabel(record as UsageLogRecord) }}
              </Tag>
            </Tooltip>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            没有符合条件的用量记录。
          </div>
        </template>
      </Table>

      <!-- 汇总:与明细同一套过滤,请求数/失败/扣费与明细对账一致 -->
      <template v-else>
        <Table
          :columns="bucketColumns"
          :data-source="buckets"
          :loading="loading"
          :pagination="false"
          :row-key="(_record: UsageBucket, index?: number) => index ?? 0"
          size="middle"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'label'">
              {{ bucketLabel(record as UsageBucket) }}
            </template>
            <template v-else-if="column.key === 'errors'">
              <span :class="record.errors > 0 ? 'text-red-500' : ''">{{ record.errors }}</span>
            </template>
            <template v-else-if="column.key === 'charge'">
              {{ formatUSD(record.charge_usd) }}
            </template>
          </template>
          <template #emptyText>
            <div class="py-6 text-neutral-500">
              没有符合条件的用量记录。
            </div>
          </template>
        </Table>
        <p class="mb-0 text-xs text-neutral-500">
          汇总与请求明细使用同一套过滤条件,数字逐条对账一致。按天汇总的自然日以数据库时区(UTC)为准。
        </p>
      </template>
    </div>
  </Page>
</template>
