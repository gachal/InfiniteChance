<script setup lang="ts">
// 桌宠设置(92 号票):settings KV `pet` 行的管理表单(照 StorageView 范式:
// 读取回填 + 保存 + 「保存后即时生效」)。桌宠是画布助手(91 号票)的化身壳:
// 名称/问候语/建议 chips/缺省对话模型 + 待机与互动两张动图形象。
//
// 形象上传走网关 admin 面 /admin/settings/pet/image(multipart,FormData 是
// admin-web 第一个上传 UI)—— 别误用创作面的 uploadAsset,那会落 assets
// 创作素材库,票面明令桌宠形象不进素材库。预览分两态:本次会话内刚上传的
// 用本地 objectURL(键未保存时读端点尚不认它),已保存的键走画布侧读端点
// /canvas-api/pet/object(故意不挂鉴权,键只认配置登记值)。
import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Alert,
  Button,
  Form,
  FormItem,
  Input,
  Space,
  Spin,
  Textarea,
} from 'ant-design-vue';

import { ApiError, type PetSettings } from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const auth = useAuth();

const loading = ref(false);
const error = ref('');
const saving = ref(false);
const savedAt = ref('');
const formError = ref('');

const name = ref('');
const greeting = ref('');
const chips = ref<string[]>([]);
const model = ref('');
const idleImageKey = ref('');
const interactiveImageKey = ref('');

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

function applySettings(settings: PetSettings): void {
  name.value = settings.name;
  greeting.value = settings.greeting;
  chips.value = [...settings.chips];
  model.value = settings.model;
  idleImageKey.value = settings.idle_image;
  interactiveImageKey.value = settings.interactive_image;
  savedAt.value = settings.updated_at;
}

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    applySettings(await auth.client.getPetSettings());
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

// ---- 保存(全字段 PUT:本页是完整表单,显式空串/空数组 = 清空)----

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  formError.value = '';
  try {
    applySettings(
      await auth.client.updatePetSettings({
        name: name.value.trim(),
        greeting: greeting.value.trim(),
        chips: chips.value.map((c) => c.trim()).filter((c) => c !== ''),
        model: model.value.trim(),
        idle_image: idleImageKey.value,
        interactive_image: interactiveImageKey.value,
      }),
    );
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : authErrorMessage(e);
  } finally {
    saving.value = false;
  }
}

function formatSavedAt(): string {
  if (!savedAt.value) {
    return '从未保存(画布侧走 CSS 缺省形象与内置文案)';
  }
  try {
    return new Date(savedAt.value).toLocaleString('zh-CN', { hour12: false });
  } catch {
    return savedAt.value;
  }
}
</script>

<template>
  <Page title="桌宠设置">
    <Alert
      v-if="error"
      type="error"
      show-icon
      :message="error"
    />
    <Spin
      v-else-if="loading"
      class="mt-8 block!"
      tip="正在加载桌宠设置…"
    />

    <Form
      v-else
      layout="vertical"
      class="max-w-[720px]"
    >
      <div class="mb-4 flex items-center justify-between">
        <h3 class="m-0 text-base font-medium">
          画布桌宠(画布助手在画布上的化身壳)
        </h3>
        <span class="text-neutral-500">{{ formatSavedAt() }}</span>
      </div>

      <FormItem label="名称(悬浮提示与助手面板标题)">
        <Input
          v-model:value="name"
          placeholder="绿毛球"
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
        <span class="text-neutral-500">保存后即时生效,无需重启服务;形象对画布侧经内容读端点可见</span>
      </Space>
    </Form>
  </Page>
</template>

<style scoped>
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
