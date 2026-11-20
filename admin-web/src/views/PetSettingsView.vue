<script setup lang="ts">
// 桌宠管理(94 号票):多桌宠两栏列表 + 表单 —— 左列表(绿毛球恒首项带
// 「默认」徽标、active 带「当前」标记、自定义项带删除),右表单(名称/
// 问候/chips/模型/两图上传预览 + 「设为当前」)。绿毛球形象锁定为前端
// 内置 CSS 形象(93 号票),表单无图槽。
//
// 保存链走 94 号票 REST 五端点(GET/POST/DELETE /admin/pets、PUT
// /admin/pets/:id、PUT /admin/pets/active)。形象上传仍走 92 号票既有
// 端点 /admin/settings/pet/image(multipart)—— 别误用创作面的
// uploadAsset,那会落 assets 创作素材库。预览分两态:本次会话内刚上传的
// 用本地 objectURL(键未保存时读端点尚不认它),已保存的键走画布侧读端点
// /canvas-api/pet/object(故意不挂鉴权,键只认全部桌宠登记键并集)。
import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Form,
  FormItem,
  Input,
  message,
  Modal,
  Space,
  Spin,
  Tag,
  Textarea,
} from 'ant-design-vue';

import {
  ApiError,
  type PetEntry,
  type PetsDocument,
} from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const loading = ref(false);
const error = ref('');
const saving = ref(false);
const switchingActive = ref(false);
const formError = ref('');

const doc = ref<PetsDocument | null>(null);
const selectedId = ref('');

const name = ref('');
const greeting = ref('');
const chips = ref<string[]>([]);
const model = ref('');
const idleImageKey = ref('');
const interactiveImageKey = ref('');

/** 当前选中的条目(列表数据驱动,表单是它的编辑态)。 */
const selected = computed<PetEntry | null>(
  () => doc.value?.pets.find((p) => p.id === selectedId.value) ?? null,
);
const isDefaultSelected = computed(() => selected.value?.id === 'default');
const isActiveSelected = computed(
  () => doc.value?.active_id === selectedId.value && selectedId.value !== '',
);
const maxReached = computed(() => (doc.value?.pets.length ?? 0) >= 21);

/** 本次会话内上传的对象键 → 本地 objectURL(保存前读端点不认新键)。 */
const localPreviews = ref(new Map<string, string>());
/** 上传中的通道:互不阻塞,按钮各自转圈。 */
const uploading = ref({ idle: false, interactive: false });

const idlePreviewSrc = computed(() =>
  idleImageKey.value
    ? (localPreviews.value.get(idleImageKey.value) ??
      `/canvas-api/pet/object?key=${encodeURIComponent(idleImageKey.value)}`)
    : '',
);
const interactivePreviewSrc = computed(() =>
  interactiveImageKey.value
    ? (localPreviews.value.get(interactiveImageKey.value) ??
      `/canvas-api/pet/object?key=${encodeURIComponent(interactiveImageKey.value)}`)
    : '',
);

/** 列表展示名:绿毛球兜底专名,自定义空名兜底「未命名」。 */
function entryName(entry: PetEntry): string {
  if (entry.id === 'default') {
    return entry.name || '绿毛球';
  }
  return entry.name || '未命名桌宠';
}

function fillForm(entry: PetEntry): void {
  name.value = entry.name;
  greeting.value = entry.greeting;
  chips.value = [...entry.chips];
  model.value = entry.model;
  idleImageKey.value = entry.idle_image;
  interactiveImageKey.value = entry.interactive_image;
  formError.value = '';
}

function select(id: string): void {
  const entry = doc.value?.pets.find((p) => p.id === id);
  if (!entry) {
    return;
  }
  selectedId.value = id;
  fillForm(entry);
}

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    const next = await auth.client.getPets();
    doc.value = next;
    // 选中项还在则保持(保存后回读),丢了(被删/首次加载)落 active。
    const kept = next.pets.some((p) => p.id === selectedId.value);
    if (!kept) {
      const active = next.pets.find((p) => p.id === next.active_id);
      select(active ? active.id : (next.pets[0]?.id ?? ''));
    } else {
      // 回读以规约后的形状刷新表单(chips 收敛等)。
      const entry = next.pets.find((p) => p.id === selectedId.value);
      if (entry) {
        fillForm(entry);
      }
    }
  } catch (e) {
    error.value = authErrorMessage(e);
  } finally {
    loading.value = false;
  }
}

onMounted(() => void refresh());

// ---- 建议 chips:动态数组编辑(antd 无现成列表编辑器,Input 列表 + 增删)----

function addChip(): void {
  chips.value.push('');
}

function removeChip(index: number): void {
  chips.value.splice(index, 1);
}

function updateChip(index: number, value: string): void {
  chips.value[index] = value;
}

// ---- 形象上传(GIF/WebP/PNG;服务端魔数嗅探是权威,accept 只做前端拦一道)----

function pickImage(channel: 'idle' | 'interactive'): void {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = 'image/gif,image/webp,image/png,.gif,.webp,.png';
  input.onchange = () => {
    const file = input.files?.[0];
    input.remove();
    if (file) {
      void uploadImage(channel, file);
    }
  };
  input.click();
}

async function uploadImage(channel: 'idle' | 'interactive', file: File): Promise<void> {
  if (uploading.value[channel]) {
    return;
  }
  uploading.value[channel] = true;
  formError.value = '';
  try {
    const image = await auth.client.uploadPetImage(file);
    // 保存前预览走本地 objectURL(URL.revokeObjectURL 就不做了:预览图
    // 生命周期与页面一致,量级两张,泄漏可忽略)。
    localPreviews.value.set(image.key, URL.createObjectURL(file));
    if (channel === 'idle') {
      idleImageKey.value = image.key;
    } else {
      interactiveImageKey.value = image.key;
    }
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    uploading.value[channel] = false;
  }
}

function clearImage(channel: 'idle' | 'interactive'): void {
  if (channel === 'idle') {
    idleImageKey.value = '';
  } else {
    interactiveImageKey.value = '';
  }
}

// ---- 保存 / 新建 / 设为当前 / 删除 ----

async function submit(): Promise<void> {
  if (saving.value || !selected.value) {
    return;
  }
  saving.value = true;
  formError.value = '';
  try {
    await auth.client.updatePet(selected.value.id, {
      name: name.value.trim(),
      greeting: greeting.value.trim(),
      chips: chips.value.map((c) => c.trim()).filter((c) => c !== ''),
      model: model.value.trim(),
      ...(selected.value.id === 'default'
        ? {}
        : { idle_image: idleImageKey.value, interactive_image: interactiveImageKey.value }),
    });
    message.success('已保存,画布侧即时生效');
    await refresh();
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

async function addPet(): Promise<void> {
  if (maxReached.value) {
    return;
  }
  formError.value = '';
  try {
    const created = await auth.client.createPet({ name: '新桌宠' });
    await refresh();
    select(created.id);
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  }
}

async function setActive(): Promise<void> {
  if (switchingActive.value || !selected.value) {
    return;
  }
  switchingActive.value = true;
  formError.value = '';
  try {
    doc.value = await auth.client.setPetActive(selected.value.id);
    message.success(`当前桌宠已切换为「${entryName(selected.value)}」`);
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    switchingActive.value = false;
  }
}

function confirmRemove(entry: PetEntry): void {
  Modal.confirm({
    title: `删除桌宠「${entryName(entry)}」?`,
    content: '其登记的形象对象会一并从存储中回收;若它是当前桌宠,当前桌宠回落绿毛球。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await auth.client.deletePet(entry.id);
        message.success('已删除');
        if (selectedId.value === entry.id) {
          selectedId.value = '';
        }
        await refresh();
      } catch (e) {
        formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
      }
    },
  });
}

function formatUpdatedAt(): string {
  const stamp = doc.value?.updated_at;
  if (!stamp) {
    return '从未保存';
  }
  try {
    return new Date(stamp).toLocaleString('zh-CN', { hour12: false });
  } catch {
    return stamp;
  }
}
</script>

<template>
  <Page title="桌宠管理">
    <Alert
      v-if="error"
      type="error"
      show-icon
      :message="error"
    />
    <Spin
      v-else-if="loading"
      class="mt-8 block!"
      tip="正在加载桌宠…"
    />

    <div
      v-else
      class="flex flex-col gap-4 lg:flex-row"
    >
      <!-- 左栏:桌宠列表(绿毛球恒首项) -->
      <div class="w-full flex-shrink-0 lg:w-[300px]">
        <div class="mb-2 flex items-center justify-between">
          <h3 class="m-0 text-base font-medium">
            桌宠列表
          </h3>
          <span class="text-xs text-neutral-500">{{ doc?.pets.length ?? 0 }}/21</span>
        </div>
        <div class="flex flex-col gap-2">
          <div
            v-for="entry in doc?.pets ?? []"
            :key="entry.id"
            class="pet-list-item flex cursor-pointer items-center justify-between gap-2 rounded-md border px-3 py-2"
            :class="{ 'pet-list-item-active': entry.id === selectedId }"
            @click="select(entry.id)"
          >
            <div class="flex min-w-0 items-center gap-2">
              <span class="truncate">{{ entryName(entry) }}</span>
              <Tag
                v-if="entry.id === 'default'"
                class="flex-shrink-0"
                color="default"
              >
                默认
              </Tag>
              <Tag
                v-if="entry.id === doc?.active_id"
                class="flex-shrink-0"
                color="green"
              >
                当前
              </Tag>
            </div>
            <Button
              v-if="entry.id !== 'default'"
              danger
              size="small"
              type="text"
              @click.stop="confirmRemove(entry)"
            >
              删除
            </Button>
          </div>
        </div>
        <Button
          class="mt-3 w-full"
          :disabled="maxReached"
          type="dashed"
          @click="addPet"
        >
          + 添加桌宠
        </Button>
        <p class="mt-1 text-xs text-neutral-500">
          上限绿毛球 + 20 只自定义;删除自定义桌宠会一并回收其形象对象
        </p>
      </div>

      <!-- 右栏:选中项编辑表单 -->
      <Form
        v-if="selected"
        layout="vertical"
        class="min-w-0 flex-1"
      >
        <div class="mb-4 flex items-center justify-between">
          <h3 class="m-0 text-base font-medium">
            编辑「{{ entryName(selected) }}」
          </h3>
          <span class="text-neutral-500">{{ formatUpdatedAt() }}</span>
        </div>

        <FormItem label="名称(悬浮提示与助手面板标题)">
          <Input
            v-model:value="name"
            :placeholder="isDefaultSelected ? '绿毛球' : '皮皮'"
            :maxlength="64"
          />
        </FormItem>

        <FormItem label="问候语(助手面板新会话首屏文案)">
          <Textarea
            v-model:value="greeting"
            placeholder="画布上有什么想改的?"
            :rows="3"
            :maxlength="500"
          />
        </FormItem>

        <FormItem label="建议 chips(面板首屏快捷指令,「换一批」轮换;点击预填输入框不直接发送)">
          <div class="flex flex-col gap-2">
            <div
              v-for="(_, index) in chips"
              :key="index"
              class="flex items-center gap-2"
            >
              <Input
                :value="chips[index]"
                placeholder="帮我建一组分镜"
                :maxlength="100"
                @update:value="(v: string) => updateChip(index, v)"
              />
              <Button
                danger
                type="text"
                @click="removeChip(index)"
              >
                删除
              </Button>
            </div>
            <Button
              class="self-start"
              type="dashed"
              @click="addChip"
            >
              + 加一条建议指令
            </Button>
          </div>
        </FormItem>

        <FormItem label="缺省对话模型(新会话模型下拉初值;目录以画布侧实时为准,不在目录自动回落首项)">
          <Input
            v-model:value="model"
            placeholder="留空 = 用目录首项"
            :maxlength="128"
          />
        </FormItem>

        <template v-if="!isDefaultSelected">
          <div class="grid grid-cols-1 gap-x-4 md:grid-cols-2">
            <FormItem label="待机动图(GIF / WebP / PNG(APNG))">
              <div class="flex items-start gap-3">
                <div class="pet-preview">
                  <img
                    v-if="idlePreviewSrc"
                    :src="idlePreviewSrc"
                    alt="待机形象预览"
                  >
                  <span
                    v-else
                    class="text-neutral-400"
                  >未配置</span>
                </div>
                <div class="flex flex-col gap-2">
                  <Button
                    :loading="uploading.idle"
                    @click="pickImage('idle')"
                  >
                    {{ idleImageKey ? '更换形象' : '上传形象' }}
                  </Button>
                  <Button
                    v-if="idleImageKey"
                    danger
                    type="text"
                    @click="clearImage('idle')"
                  >
                    移除(保存后生效)
                  </Button>
                </div>
              </div>
            </FormItem>

            <FormItem label="互动动图(可选;点击桌宠瞬间短播)">
              <div class="flex items-start gap-3">
                <div class="pet-preview">
                  <img
                    v-if="interactivePreviewSrc"
                    :src="interactivePreviewSrc"
                    alt="互动形象预览"
                  >
                  <span
                    v-else
                    class="text-neutral-400"
                  >未配置</span>
                </div>
                <div class="flex flex-col gap-2">
                  <Button
                    :loading="uploading.interactive"
                    @click="pickImage('interactive')"
                  >
                    {{ interactiveImageKey ? '更换形象' : '上传形象' }}
                  </Button>
                  <Button
                    v-if="interactiveImageKey"
                    danger
                    type="text"
                    @click="clearImage('interactive')"
                  >
                    移除(保存后生效)
                  </Button>
                </div>
              </div>
            </FormItem>
          </div>
        </template>
        <Alert
          v-else
          class="mb-4"
          type="info"
          show-icon
          message="绿毛球形象锁定为内置 CSS 形象,不可上传;文案照常可编辑,留空走画布侧内置兜底"
        />

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
          <Button
            v-if="!isActiveSelected"
            :loading="switchingActive"
            @click="setActive"
          >
            设为当前
          </Button>
          <Tag
            v-else
            color="green"
          >
            当前桌宠
          </Tag>
        </Space>
        <p class="mt-2 mb-0 text-neutral-500">
          保存与切换即时生效,无需重启服务;画布侧每只桌宠各带一份文案配置,切桌宠 = 换皮 + 换人设
        </p>
      </Form>
    </div>
  </Page>
</template>

<style scoped>
/* 列表项:选中项高亮主色边框。 */
.pet-list-item {
  border-color: hsl(var(--border));
  background: hsl(var(--background));
  transition: border-color 0.2s;
}

.pet-list-item:hover {
  border-color: hsl(var(--primary) / 0.5);
}

.pet-list-item-active,
.pet-list-item-active:hover {
  border-color: hsl(var(--primary));
}

/* 形象预览框:限定框内等比缩放(GIF/WebP/APNG 动图在 <img> 里自带动帧)。 */
.pet-preview {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 96px;
  height: 96px;
  flex-shrink: 0;
  border: 1px dashed hsl(var(--border));
  border-radius: 8px;
  overflow: hidden;
  background: hsl(var(--background));
}

.pet-preview img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}
</style>
