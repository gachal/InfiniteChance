<script setup lang="ts">
// 媒体预览灯箱(33 号票):图片/视频节点点击媒体区打开的全屏预览浮层,
// 编辑器级单实例(Teleport 到 body,生成对话框同族范式,ADR 0002)。
// 纯展示档:媒体等比适配居中、不放大超过原尺寸(max 约束只缩不放),
// 不做缩放平移;关闭 = Esc / 点击遮罩 / × 三路等价。视频自动播放有声 +
// 完整 controls —— 打开灯箱本身是用户手势,浏览器放行有声自动播放。
import { onBeforeUnmount, onMounted } from 'vue'

const props = defineProps<{
  kind: 'image' | 'video'
  url: string
}>()

const emit = defineEmits<{ close: [] }>()

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div
      class="lightbox"
      @click="emit('close')"
    >
      <div class="actions">
        <a
          class="action"
          :href="props.url"
          download
          title="下载"
          @click.stop
        >⬇</a>
        <button
          class="action"
          type="button"
          title="关闭预览"
          @click.stop="emit('close')"
        >
          ✕
        </button>
      </div>
      <img
        v-if="props.kind === 'image'"
        :src="props.url"
        alt="图片预览"
        @click.stop
      >
      <video
        v-else
        :src="props.url"
        controls
        autoplay
        playsinline
        @click.stop
      />
    </div>
  </Teleport>
</template>

<style scoped>
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  padding: 32px;
  background: rgba(6, 9, 18, 0.88);
  cursor: zoom-out;
}

.lightbox img,
.lightbox video {
  display: block;
  max-width: 100%;
  max-height: 100%;
  border-radius: 12px;
  background: #0d1220;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.55);
  cursor: default;
}

/* 操作组:右上角,下载 + 关闭;点按钮不冒泡到遮罩。 */
.actions {
  position: absolute;
  top: 16px;
  right: 16px;
  display: flex;
  gap: 8px;
}

.action {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 10px;
  background: rgba(13, 18, 32, 0.85);
  color: #dfe3ee;
  font-size: 15px;
  text-decoration: none;
  cursor: pointer;
}

.action:hover {
  background: rgba(30, 38, 60, 0.9);
}
</style>
