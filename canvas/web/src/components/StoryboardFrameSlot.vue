<script setup lang="ts">
// 画面槽(53 号票,絵コンテ cut 的画面位;54 号票ネーム复用同一组件):
// 三个入口 —— 素材库挑选(AssetPicker 由调用方挂,本组件只发 pick)、
// 本机上传(经 /api/assets/upload,归属由编辑器统一挂画布 IP)、落图节点
// (cut → 图片节点,发 drop 由编辑器落)。无专用「设为分镜画面」动作:
// 挑选即设置(再点取消),「草图 → 生成 → 回填」闭环经素材库最近使用排序
// 完成。画面持素材引用,素材行被删时按既有契约落「素材不可用」占位
// (两阶段回落:缩略 → 原图 → 占位)。
import { computed, ref, watch } from 'vue'

import { useThumbFallback } from '../thumbnail'
import { frameContentURL } from '../storyboard'

const props = defineProps<{
  /** 画面素材引用;0 = 空画面。 */
  assetId: number
  /** 上传在途(编辑器级状态),上传按钮随之禁用。 */
  uploading?: boolean
}>()

const emit = defineEmits<{
  /** 打开素材库挑选(面板由调用方挂,选点经 toggle 回流)。 */
  pick: []
  /** 本机上传的图片文件,编辑器入库后回填本槽。 */
  upload: [file: File]
  /** 落图节点:把本画面落为图片节点并连线(编辑器处理)。 */
  drop: []
}>()

const hasPicture = computed(() => props.assetId > 0)

// 两阶段回落簿(49 号票契约):缩略失败回原图,原图也失败落「素材不可用」。
const thumbs = useThumbFallback()
const loadFailed = ref(false)
watch(
  () => props.assetId,
  () => {
    loadFailed.value = false
  },
)

const displayURL = computed(() => thumbs.displayURL(frameContentURL(props.assetId)))

function onImageError(): void {
  if (!thumbs.onThumbError(frameContentURL(props.assetId))) {
    loadFailed.value = true
  }
}

const canDrop = computed(() => hasPicture.value && !loadFailed.value)

const fileInput = ref<HTMLInputElement | null>(null)

function onFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (file) {
    emit('upload', file)
  }
}
</script>

<template>
  <div class="frame-slot">
    <div
      v-if="hasPicture && !loadFailed"
      class="frame-media"
    >
      <img
        :src="displayURL"
        alt="分镜画面"
        @error="onImageError"
      >
    </div>
    <div
      v-else-if="hasPicture && loadFailed"
      class="frame-box missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除,可重新挑选或上传</small>
    </div>
    <div
      v-else
      class="frame-box empty"
    >
      <span>空画面</span>
      <small>素材库挑选,或本机上传</small>
    </div>

    <div class="frame-actions">
      <button
        type="button"
        title="从素材库挑选画面:点选即设为本槽画面,再点取消"
        @click="emit('pick')"
      >
        素材库
      </button>
      <button
        type="button"
        :disabled="uploading"
        :title="uploading ? '上传中…' : '上传本机图片作为本槽画面(入素材库,挂画布 IP)'"
        @click="fileInput?.click()"
      >
        {{ uploading ? '上传中…' : '上传' }}
      </button>
      <button
        type="button"
        :disabled="!canDrop"
        title="把本画面落为图片节点并连线,可喂生成对话框参考条/首帧"
        @click="emit('drop')"
      >
        落图
      </button>
      <input
        ref="fileInput"
        type="file"
        accept="image/*"
        class="frame-file"
        @change="onFileChange"
      >
    </div>
  </div>
</template>

<style scoped>
.frame-slot {
  display: grid;
  gap: 6px;
}

.frame-media img {
  display: block;
  width: 100%;
  max-height: 150px;
  object-fit: contain;
  border-radius: var(--r-md);
  background: var(--wash-soft);
}

.frame-box {
  display: grid;
  gap: 4px;
  place-items: center;
  min-height: 84px;
  padding: 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
  text-align: center;
}

.frame-box span {
  font-weight: 600;
  font-size: var(--fs-label);
}

.frame-box small {
  font-size: var(--fs-micro);
}

.frame-box.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
  color: var(--danger);
}

.frame-actions {
  display: flex;
  gap: 6px;
}

.frame-actions button {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 4px 0;
  font-size: var(--fs-micro);
  cursor: pointer;
  background: var(--wash-soft);
  color: var(--text-bright);
  white-space: nowrap;
}

.frame-actions button:hover:not(:disabled) {
  border-color: rgba(var(--orange-rgb), 0.6);
}

.frame-actions button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.frame-file {
  display: none;
}
</style>
