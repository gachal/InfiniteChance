<script setup lang="ts">
// 角色引用节点(52 号票):钉在画布上的设定参照卡 —— 渲染本 IP 角色目录
// 里 data.character_id 指向的角色(只存 id 不存快照,角色改名/换封面/加
// 变体后随目录刷新即变),卡片含封面首图、name/alias、变体 tabs 与逐图
// 「落图」动作(素材插入语义:编辑器把该设定图插为独立图片节点落视口中
// 心,不连线 —— 设定图进生成链的桥)。角色已删/跨 IP 落「角色不可用」
// 占位,可在卡上换绑其他角色或删节点。
//
// 连线矩阵零改动:本组件不渲染 + 把手(Handle/NodePorts 一概没有),
// character 不作任何连线的源与目标(参照卡语义)。变体选中态是组件内
// UI 态,不入节点 data。
import { computed, ref } from 'vue'

import type { CharacterImageRecord, CharacterRecord, VoiceRecord } from '@infinitechance/api'

import type { CharacterNodeData } from '../../graph'
import {
  characterImageContentURL,
  characterImageThumbURL,
  isViewLabel,
  orderedCharacterImages,
} from '../../characters'
import { useThumbFallback, assetContentURL } from '../../thumbnail'
import { voiceStatusBadge } from '../../voice'

const props = defineProps<{
  id: string
  type: string
  data: CharacterNodeData
  /** 本 IP 的角色目录(编辑器按画布 ip_id 拉取并持有刷新)。 */
  characters: CharacterRecord[]
  /** 音色台账目录(57 号票,编辑器拉取):绑定音色的徽章与试听从这解析。 */
  voices?: VoiceRecord[]
}>()

const emit = defineEmits<{
  /** 绑定/换绑角色(data.character_id 落库由编辑器应用)。 */
  bind: [characterId: number]
  /** 把一张设定图落为独立图片节点(素材插入,不连线)。 */
  'land-image': [image: CharacterImageRecord]
}>()

// 目录解析:绑定 id 找不到(已删/跨 IP)与未绑定(0)是两种占位形态。
const character = computed(
  () => props.characters.find((c) => c.id === props.data.character_id) ?? null,
)
const unbound = computed(() => props.data.character_id === 0)
const unavailable = computed(() => !unbound.value && character.value === null)

// 变体 tabs:默认选第一个变体;目录刷新后原选中被删(换装合并等)回落
// 首个 —— 只影响展示,不入持久化。
const activeVariantId = ref(0)
const activeVariant = computed(() => {
  const variants = character.value?.variants ?? []
  return variants.find((v) => v.id === activeVariantId.value) ?? variants[0] ?? null
})

// 变体画廊展示序:视图类置前(51 号票主资产口径,与详情抽屉同规则)。
const galleryImages = computed(() => orderedCharacterImages(activeVariant.value?.images ?? []))

// 封面与缩略的显示回落:thumb 302 失败回原图,原图也失败落占位
// (49 号票同款两阶段回落)。
const thumbs = useThumbFallback()
const brokenCover = ref(false)
const brokenImageIds = ref(new Set<number>())

// 绑定音色(57 号票,角色级):台账行解析 + 状态徽章 + 试听(内容寻址;
// 素材行被删 <audio> @error 落「暂无试听」,台账行不动)。
const boundVoice = computed(() => {
  const id = character.value?.voice_id ?? 0
  if (!id) {
    return null
  }
  return props.voices?.find((v) => v.id === id) ?? null
})
const voiceBadge = computed(() =>
  boundVoice.value ? voiceStatusBadge(boundVoice.value.status) : null,
)
const brokenVoicePreview = ref(false)

const coverURL = computed(() => {
  const coverId = character.value?.cover_asset_id ?? 0
  if (coverId === 0) {
    return ''
  }
  return thumbs.displayURL(
    characterImageContentURL(coverId),
    characterImageThumbURL(coverId),
  )
})

function onCoverError(): void {
  brokenCover.value = true
}

function gallerySrc(assetId: number): string {
  return thumbs.displayURL(characterImageContentURL(assetId), characterImageThumbURL(assetId))
}

function onGalleryError(assetId: number): void {
  if (!thumbs.onThumbError(characterImageContentURL(assetId))) {
    brokenImageIds.value = new Set([...brokenImageIds.value, assetId])
  }
}

// 换绑:select 的 change 即 emit;0(占位项)不上抛。
function onBindChange(e: Event): void {
  const value = Number((e.target as HTMLSelectElement).value)
  if (value > 0) {
    emit('bind', value)
  }
}
</script>

<template>
  <div class="node character">
    <header>角色</header>

    <!-- 未绑定:选本 IP 角色即绑(data 只落 id)。 -->
    <div
      v-if="unbound"
      class="placeholder"
    >
      <p>选择本 IP 的角色绑定为参照卡。</p>
      <select
        class="bind-select"
        :value="''"
        aria-label="选择角色"
        @change="onBindChange"
      >
        <option
          value=""
          disabled
        >
          — 选择角色 —
        </option>
        <option
          v-for="c in characters"
          :key="c.id"
          :value="c.id"
        >
          {{ c.name }}
        </option>
      </select>
      <p
        v-if="characters.length === 0"
        class="hint"
      >
        本 IP 还没有角色,先到 IP 详情页创建。
      </p>
    </div>

    <!-- 已删/跨 IP:「角色不可用」占位,可换绑恢复。 -->
    <div
      v-else-if="unavailable"
      class="placeholder unavailable"
    >
      <p>角色不可用(已被删除或不属于本 IP)。</p>
      <select
        class="bind-select"
        :value="''"
        aria-label="换绑角色"
        @change="onBindChange"
      >
        <option
          value=""
          disabled
        >
          — 换绑其他角色 —
        </option>
        <option
          v-for="c in characters"
          :key="c.id"
          :value="c.id"
        >
          {{ c.name }}
        </option>
      </select>
    </div>

    <!-- 参照卡:封面首图 + name/alias + 变体 tabs + 逐图「落图」。 -->
    <template v-else-if="character">
      <div class="cover">
        <img
          v-if="coverURL && !brokenCover"
          :src="coverURL"
          alt="角色封面"
          loading="lazy"
          @error="onCoverError"
        >
        <span
          v-else
          class="cover-fallback"
        >{{ character.name.slice(0, 1) }}</span>
      </div>
      <p class="name">
        {{ character.name }}
        <span
          v-if="character.alias"
          class="alias"
        >{{ character.alias }}</span>
      </p>

      <!-- 绑定音色(57 号票):徽章 + 试听;目录里找不到(已删)时只显示
           解除提示,换绑在角色抽屉。 -->
      <div
        v-if="character.voice_id > 0"
        class="voice-line"
      >
        <template v-if="boundVoice">
          <span
            class="voice-badge"
            :class="voiceBadge?.tone"
          >{{ voiceBadge?.label }}</span>
          <span class="voice-name">{{ boundVoice.name }}</span>
          <audio
            v-if="boundVoice.preview_asset_id > 0 && !brokenVoicePreview"
            class="voice-preview"
            controls
            preload="none"
            :src="assetContentURL(boundVoice.preview_asset_id)"
            @error="brokenVoicePreview = true"
          />
        </template>
        <span
          v-else
          class="voice-badge rejected"
          title="绑定的音色已不存在"
        >音色已删</span>
      </div>

      <div
        v-if="character.variants.length > 1"
        class="variant-tabs"
        role="tablist"
      >
        <button
          v-for="v in character.variants"
          :key="v.id"
          type="button"
          role="tab"
          :class="{ on: v.id === activeVariant?.id }"
          :aria-selected="v.id === activeVariant?.id"
          @click="activeVariantId = v.id"
        >
          {{ v.name }}
        </button>
      </div>

      <div class="gallery">
        <p
          v-if="galleryImages.length === 0"
          class="hint"
        >
          「{{ activeVariant?.name ?? '默认' }}」还没有设定图。
        </p>
        <div
          v-for="im in galleryImages"
          v-else
          :key="im.id"
          class="gallery-item"
        >
          <span
            v-if="brokenImageIds.has(im.asset_id)"
            class="img-fallback"
          >
            素材不可用
          </span>
          <img
            v-else
            :src="gallerySrc(im.asset_id)"
            :alt="im.label || '设定图'"
            loading="lazy"
            @error="onGalleryError(im.asset_id)"
          >
          <span class="item-meta">
            <span
              class="label-badge"
              :class="{ view: isViewLabel(im.label) }"
            >{{ im.label || '未标签' }}</span>
            <button
              type="button"
              class="land"
              title="落图节点:把这张设定图插为独立图片节点(视口中心,不连线)"
              @click="emit('land-image', im)"
            >
              落图
            </button>
          </span>
        </div>
      </div>

      <select
        class="bind-select rebind"
        :value="String(character.id)"
        aria-label="换绑其他角色"
        title="换绑其他角色"
        @change="onBindChange"
      >
        <option
          v-for="c in characters"
          :key="c.id"
          :value="c.id"
        >
          换绑:{{ c.name }}
        </option>
      </select>
    </template>
  </div>
</template>

<style scoped>
.node {
  width: 280px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--teal-rgb), 0.45);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: 13px;
  display: grid;
  gap: 8px;
}

.node header {
  font-weight: 600;
  color: var(--teal);
  margin-bottom: 0;
}

.placeholder {
  display: grid;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
}

.placeholder p {
  margin: 0;
}

.placeholder.unavailable {
  border: 1px dashed var(--line-18);
  border-radius: var(--r-md);
  padding: 10px;
}

.bind-select {
  width: 100%;
  background: var(--wash-05);
  border: 1px solid var(--line-12);
  border-radius: var(--r-md);
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.hint {
  margin: 0;
  font-size: 11px;
  color: var(--text-faint);
}

.cover {
  position: relative;
  height: 140px;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--wash-05);
}

.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-fallback {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  font-size: 40px;
  font-weight: 700;
  color: rgba(var(--teal-rgb), 0.55);
  background: linear-gradient(135deg, rgba(var(--teal-rgb), 0.12), rgba(var(--accent-rgb), 0.1));
}

.name {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
}

.voice-line {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px 8px;
  align-items: center;
  margin: 6px 0 0;
  padding: 6px 8px;
  border: 1px dashed rgba(var(--teal-light-rgb), 0.35);
  border-radius: var(--r-md);
}

.voice-badge {
  border-radius: var(--r-pill);
  padding: 1px 8px;
  font-size: 11px;
  background: rgba(var(--teal-light-rgb), 0.12);
  color: var(--teal-light);
  justify-self: start;
}

.voice-badge.ok {
  background: rgba(var(--success-rgb), 0.16);
  color: var(--success);
}

.voice-badge.deploying {
  background: rgba(var(--amber-rgb), 0.16);
  color: var(--amber);
}

.voice-badge.rejected {
  background: rgba(var(--red-signal-rgb), 0.16);
  color: var(--red-signal);
}

.voice-name {
  font-size: 12px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.voice-preview {
  grid-column: 1 / -1;
  width: 100%;
  height: 30px;
}

.alias {
  font-size: 12px;
  font-weight: 400;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.variant-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.variant-tabs button {
  border: 1px solid var(--line-12);
  background: transparent;
  color: var(--text-muted);
  border-radius: var(--r-pill);
  padding: 2px 10px;
  font-size: 11px;
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.variant-tabs button.on {
  border-color: rgba(var(--teal-rgb), 0.7);
  color: var(--teal);
  background: rgba(var(--teal-rgb), 0.1);
}

.gallery {
  display: grid;
  gap: 6px;
  max-height: 220px;
  overflow-y: auto;
}

.gallery-item {
  display: grid;
  grid-template-columns: 64px 1fr;
  align-items: center;
  gap: 8px;
}

.gallery-item img {
  width: 64px;
  height: 64px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-05);
  display: block;
}

.img-fallback {
  width: 64px;
  height: 64px;
  display: grid;
  place-items: center;
  font-size: 10px;
  color: var(--text-muted);
  border: 1px dashed var(--line-16);
  border-radius: var(--r-md);
  text-align: center;
}

.item-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  min-width: 0;
}

.label-badge {
  border: 1px solid var(--line-20);
  color: #cbd3ea;
  font-size: 10px;
  border-radius: var(--r-pill);
  padding: 1px 7px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.label-badge.view {
  border-color: rgba(var(--teal-rgb), 0.8);
  color: var(--teal-light);
}

.land {
  flex-shrink: 0;
  border: 1px solid rgba(var(--teal-rgb), 0.55);
  background: transparent;
  color: var(--teal);
  border-radius: 7px;
  padding: 3px 8px;
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}

.land:hover {
  background: rgba(var(--teal-rgb), 0.12);
}

.rebind {
  font-size: 11px;
  color: var(--text-muted);
}
</style>
