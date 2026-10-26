<script setup lang="ts">
// 媒体预览灯箱(33 号票):图片/视频节点点击媒体区打开的全屏预览浮层,
// 编辑器级单实例(Teleport 到 body,生成对话框同族范式,ADR 0002)。
// 纯展示档:媒体等比适配居中、不放大超过原尺寸(max 约束只缩不放),
// 不做缩放平移;关闭 = Esc / 点击遮罩 / × 三路等价。视频自动播放有声 +
// 完整 controls —— 打开灯箱本身是用户手势,浏览器放行有声自动播放。
// 视频灯箱带截帧(41 号票):📷 按钮 / S 键取当前帧 JPEG 上抛,入素材与
// 落画布在编辑器侧处理;图片灯箱不出现截帧入口。无源节点的预览(生成对话
// 框参考条 chip,canCapture=false)同样收起截帧入口 —— 截帧落画布依赖
// 源节点连线,悬空预览不给这个入口。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{
  kind: 'image' | 'video'
  url: string
  /** 截帧入口开关:缺省开启;参考条 chip 等无源节点预览传 false 收起。 */
  canCapture?: boolean
}>()

const emit = defineEmits<{
  close: []
  /** 截帧成功(41 号票):当前帧 JPEG blob + 时间码秒,上传与落画布由编辑器处理。 */
  capture: [blob: Blob, seconds: number]
  /** 截帧失败:面向用户的报错文案,编辑器 toast 展示(浮层自身不持提示面)。 */
  'capture-error': [message: string]
}>()

// 38 号票评审:键盘可达性 —— 打开时把焦点移进浮层(屏幕阅读器/Tab
// 顺序不再落在被遮住的画布上),关闭时归还给打开它的元素。
const rootEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

const videoEl = ref<HTMLVideoElement | null>(null)

/** 截帧(41 号票):当前播放帧经离屏 canvas 导出 JPEG(0.92、原生分辨率),
 * 不暂停可连截;每帧独立成事件,落素材/落画布全在编辑器侧。跨源字节
 * (302 回退厂商 CDN 的历史行)污染画布时 toBlob 同步抛 SecurityError,
 * 显式上抛报错不静默。 */
function captureFrame(): void {
  if (props.kind !== 'video' || props.canCapture === false) {
    return
  }
  const video = videoEl.value
  if (!video || video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
    emit('capture-error', '视频还没有可截取的画面,稍候再试')
    return
  }
  const canvas = document.createElement('canvas')
  canvas.width = video.videoWidth
  canvas.height = video.videoHeight
  if (!canvas.width || !canvas.height) {
    emit('capture-error', '视频还没有可截取的画面,稍候再试')
    return
  }
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    emit('capture-error', '当前浏览器不支持截帧')
    return
  }
  ctx.drawImage(video, 0, 0)
  try {
    canvas.toBlob(
      (blob) => {
        if (!blob) {
          emit('capture-error', '截帧失败,请稍后再试')
          return
        }
        emit('capture', blob, video.currentTime)
      },
      'image/jpeg',
      0.92,
    )
  } catch {
    emit('capture-error', '该视频来自外部临时地址,无法截帧')
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
    return
  }
  // S 截帧(41 号票):仅在视频灯箱生效;带修饰键不触发,不劫持 ⌘S 存页。
  if (
    (event.key === 's' || event.key === 'S') &&
    !event.ctrlKey && !event.metaKey && !event.altKey
  ) {
    event.preventDefault()
    captureFrame()
  }
}

onMounted(() => {
  opener = (document.activeElement as HTMLElement | null) ?? null
  rootEl.value?.focus()
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  opener?.focus?.()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="rootEl"
      class="lightbox"
      tabindex="-1"
      @click="emit('close')"
    >
      <div class="actions">
        <button
          v-if="props.kind === 'video' && props.canCapture !== false"
          class="action"
          type="button"
          title="截帧落画布(S)"
          aria-label="截帧落画布"
          @click.stop="captureFrame"
        >
          📷
        </button>
        <a
          class="action"
          :href="props.url"
          download
          title="下载"
          aria-label="下载"
          @click.stop
        >⬇</a>
        <button
          class="action"
          type="button"
          title="关闭预览"
          aria-label="关闭预览"
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
        ref="videoEl"
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
  z-index: var(--z-lightbox);
  display: grid;
  place-items: center;
  padding: 32px;
  background: rgba(6, 9, 18, 0.88);
  cursor: zoom-out;
}

.lightbox img,
.lightbox video {
  display: block;
  /* 媒体边界用视口单位定界,不用百分比:grid 单 auto 行的格域高度依赖
   * 内容撑出,竖图场景 max-height: 100% 解析不到定值被当作无约束,大图
   * 以原尺寸溢出视口被裁(点击预览竖图显示不全的根因)。padding 32px,
   * 两方向各扣 64。 */
  width: auto;
  height: auto;
  max-width: calc(100vw - 64px);
  max-height: calc(100vh - 64px);
  border-radius: var(--r-xl);
  background: #0d1220;
  box-shadow: var(--shadow-overlay-strong);
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
  border: 1px solid var(--line-16);
  border-radius: var(--r-lg);
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
