<script setup lang="ts">
// 素材管理(14 号票,antd 重写 36 号票):画布管理区的素材库页 —— 按类型/
// 来源画布过滤、预览、下载、删除。素材挂在 canvas/server(对象文件在画布
// 服务的存储卷上),这里的列表与删除走 canvasClient;删除素材后引用它的
// 画布节点会显示占位而非报错。
import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Button,
  Modal,
  RadioButton,
  RadioGroup,
  Select,
  Table,
  Tooltip,
  message,
} from 'ant-design-vue';

import {
  ApiError,
  type AssetRecord,
  type CanvasSummary,
} from '@infinitechance/api';

import { useAuth } from '../auth';

const { canvasClient } = useAuth();

const assets = ref<AssetRecord[]>([]);
const canvases = ref<CanvasSummary[]>([]);
const kindFilter = ref<'' | 'audio' | 'image' | 'video'>('');
const canvasFilter = ref<number | ''>('');
const loading = ref(false);
const error = ref('');
const deletingId = ref<null | number>(null);

// 预览:点缩略图放大,视频/音频可播放;关闭即销毁(stop 播放)。
const preview = ref<AssetRecord | null>(null);

const pageSize = 30;

const columns = [
  { key: 'thumb', title: '预览', width: 130 },
  { key: 'kind', title: '类型', width: 80 },
  { key: 'canvas', title: '来源画布', width: 150 },
  { dataIndex: 'model', key: 'model', title: '模型', width: 150 },
  { key: 'size', title: '大小', width: 90 },
  { key: 'prompt', title: '提示词' },
  { key: 'created_at', title: '生成时间', width: 170 },
  { key: 'actions', title: '操作', width: 120 },
];

const canvasName = computed(() => {
  const map = new Map(canvases.value.map((c) => [c.id, c.name]));
  return (id: number) => map.get(id) ?? '';
});

function listParams(offset: number) {
  return {
    canvas_id: canvasFilter.value === '' ? undefined : canvasFilter.value,
    kind: kindFilter.value === '' ? undefined : kindFilter.value,
    limit: pageSize,
    offset,
  };
}

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    assets.value = await canvasClient.listAssets(listParams(0));
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载素材,请稍后再试';
  } finally {
    loading.value = false;
  }
}

async function loadMore(): Promise<void> {
  if (loading.value) {
    return;
  }
  loading.value = true;
  error.value = '';
  try {
    const more = await canvasClient.listAssets(listParams(assets.value.length));
    assets.value.push(...more);
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载更多素材,请稍后再试';
  } finally {
    loading.value = false;
  }
}

const hasMore = () => assets.value.length >= pageSize && assets.value.length % pageSize === 0;

function setKind(k: '' | 'audio' | 'image' | 'video'): void {
  kindFilter.value = k;
  void refresh();
}

function setCanvasId(v: null | string): void {
  canvasFilter.value = v === null || v === '' ? '' : Number(v);
  void refresh();
}

function kindLabel(k: 'audio' | 'image' | 'video'): string {
  return k === 'image' ? '图片' : k === 'video' ? '视频' : '音频';
}

// 管理页的媒体地址走 canvas-api 代理;下载用同一内容路由的 attachment 形态。
function contentURL(a: AssetRecord): string {
  return `/canvas-api/assets/${a.id}/content`;
}

function downloadURL(a: AssetRecord): string {
  return `${contentURL(a)}?download=1`;
}

function remove(a: AssetRecord): void {
  Modal.confirm({
    title: `确定删除这条${kindLabel(a.kind)}素材?`,
    content: '对象文件会被一并清除,引用它的画布节点将显示占位。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      deletingId.value = a.id;
      error.value = '';
      try {
        await canvasClient.deleteAsset(a.id);
        assets.value = assets.value.filter((x) => x.id !== a.id);
        if (preview.value?.id === a.id) {
          preview.value = null;
        }
      } catch (e) {
        error.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试';
        message.error(error.value);
      } finally {
        deletingId.value = null;
      }
    },
  });
}

function formatSize(bytes: number): string {
  if (bytes <= 0) {
    return '—';
  }
  if (bytes >= 1 << 20) {
    return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  }
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false });
}

onMounted(() => {
  void refresh();
  void canvasClient
    .listCanvases()
    .then((list) => {
      canvases.value = list;
    })
    .catch(() => {
      /* 来源画布下拉拉不到就保持空,不影响列表与过滤 */
    });
});
</script>

<template>
  <Page title="素材管理">
    <div class="flex flex-col gap-4">
      <div class="flex flex-wrap items-center gap-2">
        <RadioGroup
          :value="kindFilter"
          button-style="solid"
          @update:value="setKind($event as '' | 'audio' | 'image' | 'video')"
        >
          <RadioButton value="">
            全部类型
          </RadioButton>
          <RadioButton value="image">
            图片
          </RadioButton>
          <RadioButton value="video">
            视频
          </RadioButton>
          <RadioButton value="audio">
            音频
          </RadioButton>
        </RadioGroup>
        <Select
          class="w-48"
          allow-clear
          placeholder="全部画布"
          aria-label="按来源画布过滤"
          :value="canvasFilter === '' ? undefined : String(canvasFilter)"
          :options="canvases.map((c) => ({ label: c.name, value: String(c.id) }))"
          @change="setCanvasId($event as null | string)"
        />
        <Button
          :loading="loading"
          @click="refresh"
        >
          刷新
        </Button>
      </div>

      <p
        v-if="error"
        class="mb-0 text-red-500"
        role="alert"
      >
        {{ error }}
      </p>

      <Table
        :columns="columns"
        :data-source="assets"
        :loading="loading && assets.length === 0"
        :pagination="false"
        row-key="id"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'thumb'">
            <!-- 点缩略图开预览;视频缩略位取首帧画面,音频给 ♪ 占位。 -->
            <img
              v-if="record.kind === 'image'"
              class="h-16 w-24 cursor-zoom-in rounded object-cover"
              :src="contentURL(record as AssetRecord)"
              :alt="`素材 ${record.id}`"
              loading="lazy"
              @click="preview = record as AssetRecord"
            >
            <video
              v-else-if="record.kind === 'video'"
              class="h-16 w-24 cursor-zoom-in rounded bg-black/30 object-cover"
              :src="contentURL(record as AssetRecord)"
              muted
              preload="metadata"
              @click="preview = record as AssetRecord"
            />
            <button
              v-else
              type="button"
              class="grid h-16 w-24 cursor-zoom-in place-items-center rounded bg-black/10 text-xl text-neutral-500"
              aria-label="预览音频"
              @click="preview = record as AssetRecord"
            >
              ♪
            </button>
          </template>

          <template v-else-if="column.key === 'kind'">
            {{ kindLabel(record.kind) }}
          </template>

          <template v-else-if="column.key === 'canvas'">
            {{ canvasName(record.canvas_id) || `画布 #${record.canvas_id}` }}
          </template>

          <template v-else-if="column.key === 'model'">
            {{ record.model || '—' }}
          </template>

          <template v-else-if="column.key === 'size'">
            {{ formatSize(record.size_bytes) }}
          </template>

          <template v-else-if="column.key === 'prompt'">
            <Tooltip
              v-if="record.prompt"
              placement="topLeft"
              :title="record.prompt"
            >
              <span class="block max-w-[280px] truncate">{{ record.prompt }}</span>
            </Tooltip>
            <span
              v-else
              class="text-neutral-400"
            >—</span>
          </template>

          <template v-else-if="column.key === 'created_at'">
            {{ formatTime(record.created_at) }}
          </template>

          <template v-else-if="column.key === 'actions'">
            <div class="flex">
              <Button
                type="link"
                size="small"
                :href="downloadURL(record as AssetRecord)"
                :download="`asset-${record.id}-${record.kind}`"
              >
                下载
              </Button>
              <Button
                type="link"
                size="small"
                danger
                :loading="deletingId === record.id"
                @click="remove(record as AssetRecord)"
              >
                删除
              </Button>
            </div>
          </template>
        </template>

        <template #emptyText>
          <div class="py-6 text-neutral-500">
            没有符合条件的素材。画布上生成的图片和视频会自动进入素材库。
          </div>
        </template>
      </Table>

      <Button
        v-if="hasMore()"
        class="self-center"
        :loading="loading"
        @click="loadMore"
      >
        加载更多
      </Button>
    </div>

    <!-- 预览:关闭即销毁,停掉视频/音频播放。 -->
    <Modal
      :open="preview !== null"
      :title="preview ? `${kindLabel(preview.kind)} #${preview.id} · ${canvasName(preview.canvas_id) || `画布 #${preview.canvas_id}`}` : ''"
      width="880px"
      :footer="null"
      destroy-on-close
      @cancel="preview = null"
    >
      <template v-if="preview">
        <div class="flex justify-center">
          <img
            v-if="preview.kind === 'image'"
            class="max-h-[70vh] max-w-full rounded"
            :src="contentURL(preview as AssetRecord)"
            :alt="`素材 ${preview.id}`"
          >
          <video
            v-else-if="preview.kind === 'video'"
            class="max-h-[70vh] max-w-full rounded"
            :src="contentURL(preview as AssetRecord)"
            controls
            autoplay
          />
          <audio
            v-else
            class="w-full max-w-[480px]"
            :src="contentURL(preview as AssetRecord)"
            controls
            autoplay
          />
        </div>
        <div class="mt-3 flex items-center justify-center gap-2">
          <Button
            :href="downloadURL(preview as AssetRecord)"
            :download="`asset-${preview.id}-${preview.kind}`"
          >
            下载
          </Button>
          <Button
            danger
            :loading="deletingId === preview.id"
            @click="remove(preview as AssetRecord)"
          >
            删除
          </Button>
          <Button @click="preview = null">
            关闭
          </Button>
        </div>
      </template>
    </Modal>
  </Page>
</template>
