<script setup lang="ts">
// 渠道管理:厂商渠道的增删改查 + 一键连通测试,antd 重写(36 号票)。
// 厂商密钥只在创建/编辑时提交;列表仅显示 has_key 与尾号提示(密钥只写不读)。
import type { FormInstance } from 'ant-design-vue';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Checkbox,
  CheckboxGroup,
  Form,
  FormItem,
  Input,
  InputNumber,
  InputPassword,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  message,
} from 'ant-design-vue';

import {
  ApiError,
  type Channel,
  type ChannelTestResult,
} from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const channels = ref<Channel[]>([]);
const loading = ref(false);
const error = ref('');

const open = ref(false);
const editingId = ref<null | number>(null);
const saving = ref(false);
const formError = ref('');

interface ChannelForm {
  apiKey: string;
  baseUrl: string;
  capabilities: string[];
  enabled: boolean;
  mappings: { from: string; to: string }[];
  name: string;
  priority: number;
  region: string;
  secretId: string;
  secretKey: string;
  subAppId: string;
  type: string;
  weight: number;
}

function blankForm(): ChannelForm {
  return {
    apiKey: '',
    baseUrl: '',
    // 不勾任何项后端按仅聊天处理,表单缺省勾上 chat。
    capabilities: ['chat'],
    enabled: true,
    mappings: [{ from: '', to: '' }],
    name: '',
    priority: 0,
    region: '',
    secretId: '',
    secretKey: '',
    subAppId: '',
    type: 'openai',
    weight: 1,
  };
}

const isVod = computed(() => formState.type === 'tencent-vod');

// volcengine-ark(25 号票):单 Bearer key 存「厂商密钥」字段,BaseURL 必填
// 含版本路径;MVP 仅实现 videos 能力(表单上提示,不强制改勾选)。
const isArk = computed(() => formState.type === 'volcengine-ark');

const formRef = ref<FormInstance>();
const formState = reactive<ChannelForm>(blankForm());

// 编辑 tencent-vod 渠道时展示已存敏感键的尾号提示(值本身只写不读)。
const editingHints = ref('');

const formTitle = computed(() => (editingId.value === null ? '新建渠道' : '编辑渠道'));

const typeOptions = [
  { label: 'OpenAI 兼容', value: 'openai' },
  { label: '腾讯云 VOD AIGC', value: 'tencent-vod' },
  { label: '火山方舟 Ark(Seedance)', value: 'volcengine-ark' },
];

const columns = [
  { dataIndex: 'name', key: 'name', title: '名称' },
  { dataIndex: 'type', key: 'type', title: '类型', width: 130 },
  { dataIndex: 'base_url', key: 'base_url', title: 'BaseURL' },
  { key: 'secret', title: '密钥', width: 150 },
  { key: 'priority', title: '优先级/权重', width: 100 },
  { key: 'model_map', title: '模型映射' },
  { key: 'test', title: '连通', width: 90 },
  { key: 'actions', title: '操作', width: 210 },
];

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    channels.value = await auth.client.listChannels();
  } catch (e) {
    error.value = authErrorMessage(e);
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

function openCreate(): void {
  editingId.value = null;
  Object.assign(formState, blankForm());
  editingHints.value = '';
  formError.value = '';
  open.value = true;
}

function openEdit(ch: Channel): void {
  editingId.value = ch.id;
  const mappings = Object.entries(ch.model_map).map(([from, to]) => ({ from, to }));
  Object.assign(formState, {
    apiKey: '', // 留空 = 保留已存密钥
    baseUrl: ch.base_url,
    capabilities: [...ch.capabilities],
    enabled: ch.enabled,
    mappings: mappings.length > 0 ? mappings : [{ from: '', to: '' }],
    name: ch.name,
    priority: ch.priority,
    region: ch.config?.region ?? '',
    secretId: '', // 敏感键:留空 = 保留已存值(hint 见表单提示)
    secretKey: '',
    subAppId: ch.config?.sub_app_id ?? '',
    type: ch.type,
    weight: ch.weight,
  } satisfies ChannelForm);
  editingHints.value = Object.entries(ch.config_hints ?? {})
    .map(([k, hint]) => `${k} ${hint}`)
    .join('、');
  formError.value = '';
  open.value = true;
}

function closeForm(): void {
  open.value = false;
  editingId.value = null;
  editingHints.value = '';
  formError.value = '';
  formRef.value?.clearValidate();
}

function buildInput(): Parameters<typeof auth.client.createChannel>[0] {
  const modelMap: Record<string, string> = {};
  for (const mapping of formState.mappings) {
    if (mapping.from.trim() !== '' && mapping.to.trim() !== '') {
      modelMap[mapping.from.trim()] = mapping.to.trim();
    }
  }
  const input: Parameters<typeof auth.client.createChannel>[0] = {
    name: formState.name.trim(),
    type: formState.type,
    base_url: formState.baseUrl.trim(),
    api_key: isVod.value ? '' : formState.apiKey.trim(),
    capabilities: formState.capabilities,
    model_map: modelMap,
    priority: formState.priority,
    weight: formState.weight,
    enabled: formState.enabled,
  };
  if (isVod.value) {
    // 敏感键留空 = 后端保留已存值;非敏感键(sub_app_id/region)原值回传。
    input.config = {
      secret_id: formState.secretId.trim(),
      secret_key: formState.secretKey.trim(),
      sub_app_id: formState.subAppId.trim(),
      region: formState.region.trim(),
    };
  }
  return input;
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
    if (editingId.value === null) {
      await auth.client.createChannel(buildInput());
    } else {
      await auth.client.updateChannel(editingId.value, buildInput());
    }
    closeForm();
    await refresh();
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

async function toggleEnabled(ch: Channel): Promise<void> {
  try {
    await auth.client.updateChannel(ch.id, {
      name: ch.name,
      type: ch.type,
      base_url: ch.base_url,
      api_key: '', // 保留已存密钥
      config: ch.config, // 敏感键回显为空 = 保留已存值
      capabilities: ch.capabilities,
      model_map: ch.model_map,
      priority: ch.priority,
      weight: ch.weight,
      enabled: !ch.enabled,
    });
    await refresh();
  } catch (e) {
    message.error(authErrorMessage(e));
  }
}

function remove(ch: Channel): void {
  Modal.confirm({
    title: `确定删除渠道「${ch.name}」?`,
    content: '该操作不可恢复。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await auth.client.deleteChannel(ch.id);
        delete testResults[ch.id];
        await refresh();
      } catch (e) {
        message.error(authErrorMessage(e));
      }
    },
  });
}

// ---- 一键连通测试(结果落在行内标签,悬停看明细) ----

const testResults = reactive<Record<number, ChannelTestResult>>({});
const testingId = ref<null | number>(null);

async function test(ch: Channel): Promise<void> {
  if (testingId.value !== null) {
    return;
  }
  testingId.value = ch.id;
  try {
    testResults[ch.id] = await auth.client.testChannel(ch.id);
  } catch (e) {
    message.error(authErrorMessage(e));
  } finally {
    testingId.value = null;
  }
}

function modelMapSummary(ch: Channel): string {
  const entries = Object.entries(ch.model_map);
  if (entries.length === 0) {
    return '(无映射)';
  }
  const shown = entries.slice(0, 3).map(([from, to]) => (from === to ? from : `${from} → ${to}`));
  const rest = entries.length - shown.length;
  return shown.join('、') + (rest > 0 ? ` 等 ${entries.length} 条` : '');
}

function secretSummary(ch: Channel): string {
  if (ch.type === 'tencent-vod') {
    return ch.config_hints?.secret_id
      ? `已设置 ${Object.values(ch.config_hints).join(' / ')}`
      : '未设置';
  }
  return ch.has_key ? `已设置 ${ch.key_hint ?? ''}` : '未设置';
}
</script>

<template>
  <Page title="渠道管理">
    <div class="flex flex-col gap-4">
      <div class="flex justify-end">
        <Button
          type="primary"
          @click="openCreate"
        >
          新建渠道
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
        :data-source="channels"
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
              :color="record.enabled ? 'success' : 'default'"
            >
              {{ record.enabled ? '已启用' : '已停用' }}
            </Tag>
          </template>

          <template v-else-if="column.key === 'base_url'">
            <Tooltip :title="record.base_url">
              <code class="block max-w-[240px] truncate text-xs">
                {{ record.base_url || (record.type === 'tencent-vod' ? 'https://vod.tencentcloudapi.com(缺省)' : '-') }}
              </code>
            </Tooltip>
          </template>

          <template v-else-if="column.key === 'secret'">
            {{ secretSummary(record as Channel) }}
          </template>

          <template v-else-if="column.key === 'priority'">
            {{ record.priority }} / {{ record.weight }}
          </template>

          <template v-else-if="column.key === 'model_map'">
            <Tooltip :title="modelMapSummary(record as Channel)">
              <span class="block max-w-[220px] truncate">{{ modelMapSummary(record as Channel) }}</span>
            </Tooltip>
          </template>

          <template v-else-if="column.key === 'test'">
            <Tooltip
              v-if="testResults[record.id]"
              :title="testResults[record.id]!.ok ? testResults[record.id]!.detail : testResults[record.id]!.error"
            >
              <Tag
                class="mr-0 cursor-default"
                :color="testResults[record.id]!.ok ? 'success' : 'error'"
              >
                {{ testResults[record.id]!.ok ? `${testResults[record.id]!.latency_ms}ms` : '失败' }}
              </Tag>
            </Tooltip>
            <span
              v-else
              class="text-neutral-400"
            >—</span>
          </template>

          <template v-else-if="column.key === 'actions'">
            <Space :size="0">
              <Button
                type="link"
                size="small"
                :disabled="testingId === record.id"
                @click="test(record as Channel)"
              >
                {{ testingId === record.id ? '测试中…' : '连通测试' }}
              </Button>
              <Button
                type="link"
                size="small"
                @click="openEdit(record as Channel)"
              >
                编辑
              </Button>
              <Button
                type="link"
                size="small"
                @click="toggleEnabled(record as Channel)"
              >
                {{ record.enabled ? '停用' : '启用' }}
              </Button>
              <Button
                type="link"
                size="small"
                danger
                @click="remove(record as Channel)"
              >
                删除
              </Button>
            </Space>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            还没有渠道。点击「新建渠道」录入第一家厂商。
          </div>
        </template>
      </Table>
    </div>

    <Modal
      v-model:open="open"
      :title="formTitle"
      :confirm-loading="saving"
      width="680px"
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
            label="名称"
            name="name"
            :rules="[{ required: true, message: '请输入渠道名称' }]"
          >
            <Input
              v-model:value="formState.name"
              placeholder="例如 openai-main"
            />
          </FormItem>
          <FormItem
            label="厂商类型"
            name="type"
            :rules="[{ required: true, message: '请选择厂商类型' }]"
          >
            <Select
              v-model:value="formState.type"
              :options="typeOptions"
            />
          </FormItem>
          <FormItem
            class="md:col-span-2"
            :label="`BaseURL${isVod ? '(可空,缺省 vod.tencentcloudapi.com)' : '(含版本路径)'}`"
            name="baseUrl"
            :rules="[{ required: !isVod, message: '请输入 BaseURL' }]"
          >
            <Input
              v-model:value="formState.baseUrl"
              :placeholder="isVod ? '留空 = https://vod.tencentcloudapi.com' : (isArk ? 'https://ark.cn-beijing.volcengine.com/api/v3' : 'https://api.openai.com/v1')"
            />
          </FormItem>

          <!-- openai / ark:单 Bearer 密钥;vod:SecretId/SecretKey + 子应用/地域 -->
          <FormItem
            v-if="!isVod"
            class="md:col-span-2"
            :label="`厂商密钥${editingId === null ? '' : '(留空保持现有密钥)'}`"
            name="apiKey"
            :rules="[{ required: editingId === null, message: '请输入厂商密钥' }]"
          >
            <InputPassword
              v-model:value="formState.apiKey"
              autocomplete="off"
              placeholder="sk-…"
            />
          </FormItem>
          <template v-else>
            <FormItem
              :label="`SecretId${editingId === null ? '' : '(留空保持现有)'}`"
              name="secretId"
              :rules="[{ required: editingId === null, message: '请输入 SecretId' }]"
            >
              <InputPassword
                v-model:value="formState.secretId"
                autocomplete="off"
                placeholder="AKID…"
              />
            </FormItem>
            <FormItem
              :label="`SecretKey${editingId === null ? '' : '(留空保持现有)'}`"
              name="secretKey"
              :rules="[{ required: editingId === null, message: '请输入 SecretKey' }]"
            >
              <InputPassword
                v-model:value="formState.secretKey"
                autocomplete="off"
                placeholder="腾讯云 API 密钥"
              />
            </FormItem>
            <FormItem label="SubAppId(点播子应用,可空)">
              <Input
                v-model:value="formState.subAppId"
                placeholder="1500000000"
              />
            </FormItem>
            <FormItem label="地域(可空)">
              <Input
                v-model:value="formState.region"
                placeholder="ap-guangzhou"
              />
            </FormItem>
            <p
              v-if="editingId !== null && editingHints"
              class="mb-2 md:col-span-2 text-neutral-500"
            >
              已存凭据:{{ editingHints }}
            </p>
          </template>
        </div>

        <FormItem label="模型映射(公开模型名 → 上游模型名)">
          <div class="flex flex-col gap-2">
            <div
              v-for="(mapping, index) in formState.mappings"
              :key="index"
              class="grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2"
            >
              <Input
                v-model:value="mapping.from"
                placeholder="gpt-4o"
                aria-label="公开模型名"
              />
              <span class="text-neutral-500">→</span>
              <Input
                v-model:value="mapping.to"
                placeholder="gpt-4o-2024-11-20"
                aria-label="上游模型名"
              />
              <Button
                :disabled="formState.mappings.length === 1"
                @click="formState.mappings.splice(index, 1)"
              >
                删除
              </Button>
            </div>
            <Button
              class="self-start"
              @click="formState.mappings.push({ from: '', to: '' })"
            >
              + 添加映射
            </Button>
          </div>
        </FormItem>

        <FormItem name="capabilities">
          <template #label>
            能力(该渠道可转发的请求;不勾任何项按仅聊天处理)
          </template>
          <p
            v-if="isArk"
            class="mb-2 mt-0 text-neutral-500"
          >
            volcengine-ark 当前仅实现生视频(videos);勾选其他能力不会被转发。
          </p>
          <CheckboxGroup
            v-model:value="formState.capabilities"
            class="flex flex-wrap gap-5"
          >
            <Checkbox value="chat">
              聊天(chat)
            </Checkbox>
            <Checkbox value="images">
              生图(images)
            </Checkbox>
            <Checkbox value="videos">
              生视频(videos)
            </Checkbox>
          </CheckboxGroup>
        </FormItem>

        <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <FormItem label="优先级(越大越优先)">
            <InputNumber
              v-model:value="formState.priority"
              class="w-full"
              :min="0"
            />
          </FormItem>
          <FormItem label="权重">
            <InputNumber
              v-model:value="formState.weight"
              class="w-full"
              :min="0"
            />
          </FormItem>
          <FormItem>
            <Switch
              v-model:checked="formState.enabled"
              checked-children="启用"
              un-checked-children="停用"
            />
            <span class="ml-2">启用该渠道</span>
          </FormItem>
        </div>

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
