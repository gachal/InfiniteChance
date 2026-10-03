<script setup lang="ts">
// 技能:Agent 节点经 `/` 调用的提示词底稿管理(11 号票建;29 号票由
// 「提示词模板」更名演进),antd 重写(36 号票)。template 须包含 {topic}
// 占位符,会话首条指令以主题替换;description/target 是技能卡的副标题与
// 目标徽章(target 纯展示 + 浮层筛选,后端零行为);画布侧每次即时读库,
// 这里的增删改立即生效。
import type { FormInstance } from 'ant-design-vue';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Form,
  FormItem,
  Input,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Textarea,
  Tooltip,
  message,
} from 'ant-design-vue';

import {
  ApiError,
  type PromptTemplate,
  type SkillTarget,
} from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const templates = ref<PromptTemplate[]>([]);
const loading = ref(false);
const error = ref('');

const open = ref(false);
const editingId = ref<null | number>(null);
const saving = ref(false);
const formError = ref('');

interface SkillForm {
  description: string;
  enabled: boolean;
  name: string;
  target: SkillTarget;
  template: string;
}

function blankForm(): SkillForm {
  return { description: '', enabled: true, name: '', target: 'any', template: '' };
}

const formRef = ref<FormInstance>();
const formState = reactive<SkillForm>(blankForm());

const formTitle = computed(() => (editingId.value === null ? '新建技能' : '编辑技能'));

const TARGET_OPTIONS: { label: string; value: SkillTarget }[] = [
  { value: 'any', label: '通用(图/视频皆可)' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
];

const TARGET_LABEL: Record<SkillTarget, string> = {
  any: '通用',
  image: '图',
  video: '视频',
};

const TARGET_COLOR: Record<SkillTarget, string> = {
  any: 'processing',
  image: 'success',
  video: 'warning',
};

const columns = [
  { dataIndex: 'name', key: 'name', title: '名称' },
  { dataIndex: 'description', key: 'description', title: '描述' },
  { key: 'template', title: '指令内容' },
  { key: 'updated_at', title: '更新时间', width: 170 },
  { key: 'actions', title: '操作', width: 170 },
];

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    templates.value = await auth.client.listPromptTemplates();
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
  formError.value = '';
  open.value = true;
}

function openEdit(t: PromptTemplate): void {
  editingId.value = t.id;
  Object.assign(formState, {
    description: t.description,
    enabled: t.enabled,
    name: t.name,
    target: t.target,
    template: t.template,
  } satisfies SkillForm);
  formError.value = '';
  open.value = true;
}

function closeForm(): void {
  open.value = false;
  editingId.value = null;
  formError.value = '';
  formRef.value?.clearValidate();
}

function buildInput() {
  return {
    description: formState.description.trim(),
    enabled: formState.enabled,
    name: formState.name.trim(),
    target: formState.target,
    template: formState.template.trim(),
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
    if (editingId.value === null) {
      await auth.client.createPromptTemplate(buildInput());
    } else {
      await auth.client.updatePromptTemplate(editingId.value, buildInput());
    }
    closeForm();
    await refresh();
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

async function toggleEnabled(t: PromptTemplate): Promise<void> {
  try {
    await auth.client.updatePromptTemplate(t.id, {
      description: t.description,
      enabled: !t.enabled,
      name: t.name,
      target: t.target,
      template: t.template,
    });
    await refresh();
  } catch (e) {
    message.error(authErrorMessage(e));
  }
}

function remove(t: PromptTemplate): void {
  Modal.confirm({
    title: `确定删除技能「${t.name}」?`,
    content: '该操作不可恢复。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await auth.client.deletePromptTemplate(t.id);
        await refresh();
      } catch (e) {
        message.error(authErrorMessage(e));
      }
    },
  });
}

function templateSummary(t: PromptTemplate): string {
  const oneLine = t.template.replaceAll(/\s+/g, ' ').trim();
  return oneLine.length > 120 ? `${oneLine.slice(0, 120)}…` : oneLine;
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false });
}
</script>

<template>
  <Page
    title="技能"
    description="技能是画布 Agent 节点经 / 调用的提示词底稿:名称与描述出现在技能卡上,生成时模板内容中的 {topic} 占位符替换为用户输入的主题。"
  >
    <div class="flex flex-col gap-4">
      <div class="flex justify-end">
        <Button
          type="primary"
          @click="openCreate"
        >
          新建技能
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
        :data-source="templates"
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
              :color="TARGET_COLOR[record.target as SkillTarget] ?? 'default'"
            >
              {{ TARGET_LABEL[record.target as SkillTarget] ?? record.target }}
            </Tag>
            <Tag :color="record.enabled ? 'success' : 'default'">
              {{ record.enabled ? '已启用' : '已停用' }}
            </Tag>
          </template>

          <template v-else-if="column.key === 'description'">
            <span :class="record.description ? '' : 'text-neutral-400'">
              {{ record.description || '—' }}
            </span>
          </template>

          <template v-else-if="column.key === 'template'">
            <Tooltip
              placement="topLeft"
              :title="record.template"
            >
              <span class="block max-w-[360px] truncate">{{ templateSummary(record as PromptTemplate) }}</span>
            </Tooltip>
          </template>

          <template v-else-if="column.key === 'updated_at'">
            {{ formatTime(record.updated_at) }}
          </template>

          <template v-else-if="column.key === 'actions'">
            <Space :size="0">
              <Button
                type="link"
                size="small"
                @click="openEdit(record as PromptTemplate)"
              >
                编辑
              </Button>
              <Button
                type="link"
                size="small"
                @click="toggleEnabled(record as PromptTemplate)"
              >
                {{ record.enabled ? '停用' : '启用' }}
              </Button>
              <Button
                type="link"
                size="small"
                danger
                @click="remove(record as PromptTemplate)"
              >
                删除
              </Button>
            </Space>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            还没有技能。点击「新建技能」给画布的 Agent 节点写提示词底稿。
          </div>
        </template>
      </Table>
    </div>

    <Modal
      v-model:open="open"
      :title="formTitle"
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
            label="名称"
            name="name"
            :rules="[{ max: 128, message: '最多 128 个字符' }, { required: true, message: '请输入名称' }]"
          >
            <Input
              v-model:value="formState.name"
              placeholder="例如 英文生图提示词"
              :maxlength="128"
            />
          </FormItem>
          <FormItem
            label="描述(技能卡副标题,可空)"
            name="description"
            :rules="[{ max: 200, message: '最多 200 个字符' }]"
          >
            <Input
              v-model:value="formState.description"
              placeholder="例如 按主题写一段英文文生图提示词"
              :maxlength="200"
            />
          </FormItem>
          <FormItem label="目标(纯展示,供画布技能浮层筛选)">
            <Select
              v-model:value="formState.target"
              :options="TARGET_OPTIONS"
            />
          </FormItem>
          <FormItem>
            <Space align="center">
              <Switch
                v-model:checked="formState.enabled"
                checked-children="启用"
                un-checked-children="停用"
              />
              <span>启用(停用后画布侧不再出现)</span>
            </Space>
          </FormItem>
        </div>

        <FormItem
          label="指令内容(必须包含 {topic} 占位符,生成时替换为主题)"
          name="template"
          :rules="[{ max: 8000, message: '最多 8000 个字符' }, { required: true, message: '请输入指令内容' }]"
        >
          <Textarea
            v-model:value="formState.template"
            :rows="8"
            :maxlength="8000"
            placeholder="例如:你是提示词工程师。请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。"
          />
        </FormItem>

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
