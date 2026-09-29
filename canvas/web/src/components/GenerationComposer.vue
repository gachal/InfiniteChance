<script setup lang="ts">
// 生成对话框(21 号票):画布图片生成的统一入口,单实例悬浮层 —— 选中
// 图片节点时由编辑器贴其正下方,未选中时停靠画布底部中央。组件只做
// 渲染与事件:草稿(提示词/模型/尺寸)与参考图列表由编辑器持有(选中
// 切换不清空),发送语义 = 提交画布任务。底栏的模式下拉本票仅「图片
// 生成」一项,为后续视频/分析模式预留结构(ADR 0002)。
import { computed, ref } from 'vue'

import { MAX_COMPOSER_REFS, SIZE_PRESETS, type ComposerRef } from '../composer'

const props = defineProps<{
  /** 可用的按次计价生图模型(编辑器从 /image-models 拉取)。 */
  models: string[]
  /** 生效中的参考图列表(编辑器归并选中节点与上传条目)。 */
  refs: ComposerRef[]
  /** 全局持久草稿:提示词/模型/尺寸,不随选中切换清空。 */
  prompt: string
  model: string
  size: string
  /** 提交在途(编辑器级状态,防连点)。 */
  generating: boolean
  /** 参考图上传在途。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:model': [value: string]
  'update:size': [value: string]
  'remove-ref': [index: number]
  upload: [file: File]
  send: []
}>()

const fileInput = ref<HTMLInputElement | null>(null)

const canSend = computed(
  () => props.prompt.trim().length > 0 && props.model !== '' && !props.generating,
)

const refsFull = computed(() => props.refs.length >= MAX_COMPOSER_REFS)

/** 参考图上传的 kind 恒 image(18 号票入口),服务端按魔数最终裁决。 */
function onFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (!file) {
    return
  }
  emit('upload', file)
}
</script>

<template>
  <section
    class="composer"
    aria-label="生成对话框"
  >
    <div class="ref-strip">
      <div
        v-for="(r, i) in refs"
        :key="r.url"
        class="ref-chip"
      >
        <img
          :src="r.url"
          alt="参考图"
        >
        <button
          class="ref-remove"
          type="button"
          title="移除参考图(发送即文生图,连线仍保留)"
          @click="emit('remove-ref', i)"
        >
          ×
        </button>
      </div>
      <button
        class="ref-add"
        type="button"
        :disabled="uploading || refsFull"
        :title="refsFull ? `最多 ${MAX_COMPOSER_REFS} 张参考图` : '上传本机图片作为参考图'"
        @click="fileInput?.click()"
      >
        {{ uploading ? '…' : '+' }}
      </button>
      <input
        ref="fileInput"
        type="file"
        accept="image/*"
        class="ref-file"
        @change="onFileChange"
      >
    </div>

    <textarea
      :value="prompt"
      placeholder="描述画面,或对选中图片说明要改什么…"
      rows="3"
      @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
    />

    <div class="controls">
      <select
        :value="size"
        class="size"
        title="尺寸预设(默认 = 不传,由模型缺省裁决)"
        @change="emit('update:size', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="p in SIZE_PRESETS"
          :key="p.value"
          :value="p.value"
        >
          {{ p.label }}
        </option>
      </select>
      <select
        :value="model"
        class="model"
        title="生图模型"
        @change="emit('update:model', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="m in models"
          :key="m"
          :value="m"
        >
          {{ m }}
        </option>
      </select>
      <span
        class="mode"
        title="本票仅图片生成;视频/分析模式留待后续票"
      >图片生成</span>
      <button
        class="send"
        type="button"
        :disabled="!canSend"
        :title="canSend ? '生成(提交画布任务)' : '先写提示词并选模型'"
        @click="emit('send')"
      >
        {{ generating ? '…' : '↑' }}
      </button>
    </div>
  </section>
</template>

<style scoped>
/* 即梦式深色面板:参考条 + 输入区 + 参数栏,悬浮在画布上。 */
.composer {
  position: absolute;
  z-index: 10;
  width: min(560px, calc(100% - 32px));
  display: grid;
  gap: 8px;
  padding: 12px;
  background: rgba(13, 18, 32, 0.96);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 16px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.45);
  box-sizing: border-box;
}

.ref-strip {
  display: flex;
  gap: 8px;
  align-items: center;
  min-height: 48px;
}

.ref-chip {
  position: relative;
  width: 48px;
  height: 48px;
  flex-shrink: 0;
}

.ref-chip img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  display: block;
}

.ref-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 50%;
  background: rgba(60, 64, 80, 0.95);
  color: #dfe3ee;
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
}

.ref-add {
  width: 48px;
  height: 48px;
  flex-shrink: 0;
  border: 1px dashed rgba(255, 255, 255, 0.3);
  border-radius: 10px;
  background: transparent;
  color: #aab1c5;
  font-size: 20px;
  cursor: pointer;
}

.ref-add:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.ref-file {
  display: none;
}

textarea {
  width: 100%;
  resize: vertical;
  min-height: 64px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 8px 10px;
  color: inherit;
  font: inherit;
  line-height: 1.5;
  box-sizing: border-box;
}

textarea:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.controls {
  display: flex;
  align-items: center;
  gap: 8px;
}

.controls select {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
  max-width: 150px;
}

.controls .size {
  flex-shrink: 0;
}

.controls .model {
  flex: 1;
  min-width: 0;
}

.mode {
  flex-shrink: 0;
  color: #8b91a7;
  font-size: 12px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 10px;
}

.send {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 50%;
  background: #7aa2f7;
  color: #10152a;
  font-size: 16px;
  font-weight: 700;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
}

.send:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
