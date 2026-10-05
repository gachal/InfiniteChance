<script setup lang="ts">
// 模型价格(36 号票补 UI):/admin/prices 的列表 + 配价编辑,覆盖双轨计价
// 字段(token 轨单价×倍率、次/秒轨单价×尺寸系数;视频 token 模型另有秒
// 折算表,25 号票)。服务端语义不变 —— 未配价模型一律 model_not_priced
// 拒绝,这里只是把 curl 配价换成界面;金额按人类单位上送,微人民币换算在
// 服务端 handler。数据走本应用的 requestClient(共享包按票定案不动)。
import type { FormInstance } from 'ant-design-vue';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Divider,
  Form,
  FormItem,
  Input,
  InputNumber,
  Modal,
  RadioButton,
  RadioGroup,
  Space,
  Table,
  Tag,
  Tooltip,
  message,
} from 'ant-design-vue';

import {
  deleteModelPrice,
  listModelPrices,
  type ModelPrice,
  type PriceUnit,
  upsertModelPrice,
} from '../api/prices';

const prices = ref<ModelPrice[]>([]);
const loading = ref(false);
const error = ref('');

const open = ref(false);
// 编辑时锁公开模型名:PUT 按 public_model 整行覆盖,改名会另起新行。
const editingModel = ref<null | string>(null);
const saving = ref(false);
const formError = ref('');

interface PriceForm {
  defaultTokensPerSecond: number | undefined;
  inputCny: number | undefined;
  outputCny: number | undefined;
  publicModel: string;
  ratio: number;
  sizeFactors: { factor: number | undefined; size: string }[];
  unit: PriceUnit;
  cnyPerCall: number | undefined;
  videoRates: { rate: number | undefined; size: string }[];
}

function blankForm(): PriceForm {
  return {
    defaultTokensPerSecond: undefined,
    inputCny: undefined,
    outputCny: undefined,
    publicModel: '',
    ratio: 1,
    sizeFactors: [],
    unit: 'token',
    cnyPerCall: undefined,
    videoRates: [],
  };
}

const formRef = ref<FormInstance>();
const formState = reactive<PriceForm>(blankForm());

const columns = [
  { dataIndex: 'public_model', key: 'public_model', title: '公开模型' },
  { key: 'unit', title: '轨道', width: 100 },
  { key: 'price', title: '单价' },
  { key: 'table', title: '尺寸表' },
  { key: 'updated_at', title: '更新时间', width: 170 },
  { key: 'actions', title: '操作', width: 110 },
];

const UNIT_LABEL: Record<PriceUnit, string> = {
  call: '按次',
  second: '按秒',
  token: '按 token',
};

const UNIT_COLOR: Record<PriceUnit, string> = {
  call: 'success',
  second: 'warning',
  token: 'processing',
};

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    prices.value = await listModelPrices();
  } catch (e) {
    error.value = e instanceof Error ? e.message : '无法加载模型价格,请稍后再试';
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

function openCreate(): void {
  editingModel.value = null;
  Object.assign(formState, blankForm());
  formError.value = '';
  open.value = true;
}

function openEdit(p: ModelPrice): void {
  editingModel.value = p.public_model;
  Object.assign(formState, {
    defaultTokensPerSecond: p.default_tokens_per_second || undefined,
    inputCny: p.input_cny_per_mtokens,
    outputCny: p.output_cny_per_mtokens,
    publicModel: p.public_model,
    ratio: p.ratio || 1,
    sizeFactors: Object.entries(p.size_factors ?? {}).map(([size, factor]) => ({ factor, size })),
    unit: p.unit,
    cnyPerCall: p.cny_per_call,
    videoRates: Object.entries(p.size_tokens_per_second ?? {}).map(([size, rate]) => ({ rate, size })),
  } satisfies PriceForm);
  formError.value = '';
  open.value = true;
}

function closeForm(): void {
  open.value = false;
  editingModel.value = null;
  formError.value = '';
  formRef.value?.clearValidate();
}

function buildInput() {
  const publicModel = formState.publicModel.trim();
  if (formState.unit === 'token') {
    const rates: Record<string, number> = {};
    for (const { rate, size } of formState.videoRates) {
      if (size.trim() !== '' && rate !== undefined) {
        rates[size.trim()] = rate;
      }
    }
    return {
      public_model: publicModel,
      unit: formState.unit,
      input_cny_per_mtokens: formState.inputCny ?? 0,
      output_cny_per_mtokens: formState.outputCny ?? 0,
      ratio: formState.ratio,
      size_tokens_per_second: rates,
      default_tokens_per_second: formState.defaultTokensPerSecond ?? 0,
    };
  }
  const factors: Record<string, number> = {};
  for (const { factor, size } of formState.sizeFactors) {
    if (size.trim() !== '' && factor !== undefined) {
      factors[size.trim()] = factor;
    }
  }
  return {
    public_model: publicModel,
    unit: formState.unit,
    cny_per_call: formState.cnyPerCall ?? 0,
    size_factors: factors,
  };
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  saving.value = true;
  formError.value = '';
  try {
    await upsertModelPrice(buildInput());
    closeForm();
    await refresh();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : '保存失败,请稍后再试';
  } finally {
    saving.value = false;
  }
}

function remove(p: ModelPrice): void {
  Modal.confirm({
    title: `确定删除模型「${p.public_model}」的价格?`,
    content: '删除后该模型的请求会因未配价被拒(model_not_priced)。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await deleteModelPrice(p.public_model);
        await refresh();
      } catch (e) {
        message.error(e instanceof Error ? e.message : '删除失败,请稍后再试');
      }
    },
  });
}

// ---- 展示辅助 ----

function formatCNY(cny: number): string {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(cny);
}

/** 单价列:token 轨展示输入/输出单价与倍率;次/秒轨展示单价。 */
function priceSummary(p: ModelPrice): string {
  if (p.unit === 'token') {
    const parts = [
      `${formatCNY(p.input_cny_per_mtokens)} / ${formatCNY(p.output_cny_per_mtokens)} 每 1M tok`,
    ];
    if (p.ratio !== 1) {
      parts.push(`×${p.ratio}`);
    }
    return parts.join(' · ');
  }
  return `${formatCNY(p.cny_per_call)} / ${p.unit === 'call' ? '张' : '秒'}`;
}

/** 尺寸表列:次/秒轨的尺寸系数或视频 token 轨的秒折算率,悬停看全表。 */
function tableSummary(p: ModelPrice): string {
  const entries =
    p.unit === 'token'
      ? Object.entries(p.size_tokens_per_second ?? {}).map(([k, v]) => `${k} ${v} tok/s`)
      : Object.entries(p.size_factors ?? {}).map(([k, v]) => `${k} ×${v}`);
  if (p.unit === 'token' && (p.default_tokens_per_second ?? 0) > 0) {
    entries.push(`缺省 ${p.default_tokens_per_second} tok/s`);
  }
  if (entries.length === 0) {
    return '—';
  }
  const shown = entries.slice(0, 3);
  const rest = entries.length - shown.length;
  return shown.join('、') + (rest > 0 ? ` 等 ${entries.length} 条` : '');
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false });
}

const modalTitle = computed(() =>
  editingModel.value === null ? '新建模型价格' : `编辑价格 · ${editingModel.value}`,
);
</script>

<template>
  <Page title="模型价格">
    <div class="flex flex-col gap-4">
      <div class="flex justify-end">
        <Button
          type="primary"
          @click="openCreate"
        >
          新建价格
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
        :data-source="prices"
        :loading="loading"
        :pagination="false"
        row-key="public_model"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'public_model'">
            <code>{{ record.public_model }}</code>
          </template>

          <template v-else-if="column.key === 'unit'">
            <Tag :color="UNIT_COLOR[record.unit as PriceUnit] ?? 'default'">
              {{ UNIT_LABEL[record.unit as PriceUnit] ?? record.unit }}
            </Tag>
          </template>

          <template v-else-if="column.key === 'price'">
            {{ priceSummary(record as ModelPrice) }}
          </template>

          <template v-else-if="column.key === 'table'">
            <Tooltip
              v-if="tableSummary(record as ModelPrice) !== '—'"
              :title="tableSummary(record as ModelPrice)"
            >
              <span class="block max-w-[280px] truncate">{{ tableSummary(record as ModelPrice) }}</span>
            </Tooltip>
            <span
              v-else
              class="text-neutral-400"
            >—</span>
          </template>

          <template v-else-if="column.key === 'updated_at'">
            {{ formatTime(record.updated_at) }}
          </template>

          <template v-else-if="column.key === 'actions'">
            <Space :size="0">
              <Button
                type="link"
                size="small"
                @click="openEdit(record as ModelPrice)"
              >
                编辑
              </Button>
              <Button
                type="link"
                size="small"
                danger
                @click="remove(record as ModelPrice)"
              >
                删除
              </Button>
            </Space>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            还没有配价。未配价的模型请求会被网关以 model_not_priced 拒绝。
          </div>
        </template>
      </Table>
    </div>

    <Modal
      v-model:open="open"
      :title="modalTitle"
      :confirm-loading="saving"
      width="640px"
      :mask-closable="false"
      ok-text="保存"
      cancel-text="取消"
      @ok="submit"
      @cancel="closeForm"
    >
      <Form
        ref="formRef"
        :model="formState"
        layout="vertical"
        class="mt-2"
      >
        <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <FormItem
            label="公开模型名"
            name="publicModel"
            :rules="[{ max: 200, message: '最多 200 个字符' }, { required: true, message: '请输入公开模型名' }]"
          >
            <Input
              v-model:value="formState.publicModel"
              :disabled="editingModel !== null"
              placeholder="例如 gpt-4o(渠道映射左边的名字)"
            />
          </FormItem>
          <FormItem
            label="计价轨道"
            name="unit"
            :rules="[{ required: true, message: '请选择计价轨道' }]"
          >
            <RadioGroup
              v-model:value="formState.unit"
              button-style="solid"
            >
              <RadioButton value="token">
                按 token
              </RadioButton>
              <RadioButton value="call">
                按次
              </RadioButton>
              <RadioButton value="second">
                按秒
              </RadioButton>
            </RadioGroup>
          </FormItem>
        </div>

        <!-- token 轨:输入/输出单价(元 每百万 token)与倍率 -->
        <template v-if="formState.unit === 'token'">
          <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
            <FormItem
              label="输入单价(元 / 1M tokens)"
              name="inputCny"
              :rules="[{ required: true, message: '请输入输入单价' }]"
            >
              <InputNumber
                v-model:value="formState.inputCny"
                class="w-full"
                :min="0"
                :max="10000"
                :step="0.01"
              />
            </FormItem>
            <FormItem
              label="输出单价(元 / 1M tokens)"
              name="outputCny"
              :rules="[{ required: true, message: '请输入输出单价' }]"
            >
              <InputNumber
                v-model:value="formState.outputCny"
                class="w-full"
                :min="0"
                :max="10000"
                :step="0.01"
              />
            </FormItem>
            <FormItem label="倍率(×1.0 = 按上游成本原价)">
              <InputNumber
                v-model:value="formState.ratio"
                class="w-full"
                :min="0"
                :max="1000"
                :step="0.1"
              />
            </FormItem>
          </div>

          <Divider class="my-2!">
            <span class="text-sm text-neutral-500">视频秒折算表(可选;视频 token 模型的提交预扣估算)</span>
          </Divider>
          <div class="flex flex-col gap-2">
            <div
              v-for="(row, index) in formState.videoRates"
              :key="index"
              class="grid grid-cols-[1fr_1fr_auto] items-center gap-2"
            >
              <Input
                v-model:value="row.size"
                placeholder="档位,如 720p"
                aria-label="折算档位"
              />
              <InputNumber
                v-model:value="row.rate"
                class="w-full"
                :min="0"
                :max="100000"
                placeholder="每秒 token 数"
                aria-label="每秒 token 数"
              />
              <Button
                danger
                @click="formState.videoRates.splice(index, 1)"
              >
                删除
              </Button>
            </div>
            <div class="grid grid-cols-[1fr_1fr_auto] items-center gap-2">
              <span class="text-neutral-500">缺省折算率(档位未配时兜底)</span>
              <InputNumber
                v-model:value="formState.defaultTokensPerSecond"
                class="w-full"
                :min="0"
                :max="100000"
              />
              <span />
            </div>
            <Button
              class="self-start"
              @click="formState.videoRates.push({ rate: undefined, size: '' })"
            >
              + 添加档位
            </Button>
          </div>
        </template>

        <!-- 次/秒轨:单价(元 每张/每秒)与尺寸系数(未配档恒 ×1.0) -->
        <template v-else>
          <FormItem
            :label="`单价(元 / ${formState.unit === 'call' ? '张' : '秒'})`"
            name="cnyPerCall"
            :rules="[{ required: true, message: '请输入单价' }]"
          >
            <InputNumber
              v-model:value="formState.cnyPerCall"
              class="w-full"
              :min="0"
              :max="1000"
              :step="0.001"
            />
          </FormItem>

          <FormItem>
            <template #label>
              尺寸系数(可选;未配置的尺寸恒 ×1.0)
            </template>
            <div class="flex flex-col gap-2">
              <div
                v-for="(row, index) in formState.sizeFactors"
                :key="index"
                class="grid grid-cols-[1fr_1fr_auto] items-center gap-2"
              >
                <Input
                  v-model:value="row.size"
                  placeholder="尺寸,如 1024x1024 或 720p"
                  aria-label="尺寸"
                />
                <InputNumber
                  v-model:value="row.factor"
                  class="w-full"
                  :min="0"
                  :max="1000"
                  :step="0.1"
                  placeholder="系数"
                  aria-label="系数"
                />
                <Button
                  danger
                  @click="formState.sizeFactors.splice(index, 1)"
                >
                  删除
                </Button>
              </div>
              <Button
                class="self-start"
                @click="formState.sizeFactors.push({ factor: undefined, size: '' })"
              >
                + 添加尺寸
              </Button>
            </div>
          </FormItem>
        </template>

        <Alert
          v-if="formError"
          type="error"
          show-icon
          :message="formError"
        />
      </Form>
    </Modal>
  </Page>
</template>
