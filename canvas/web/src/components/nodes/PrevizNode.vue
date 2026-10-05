<script setup lang="ts">
// 预演台节点卡片(39 号票):三维静帧排练的工作台 —— PrevizStage 视口
// (点选/平移)+ 对象增补(盒/柱/板/人形)+ 机位管理(下拉切换、FOV、
// 注视点)与渲染/落画布。场景数据(objects/cameras/active_camera_id)随
// 节点 data 走整图 JSON 自动保存;最新渲染帧(latest)是素材库 uploads/
// 档引用,媒体区展示、点击灯箱、「落画布」复制为图片节点并连线 —— 喂
// 生成链一律经图片节点,对话框不认本节点。选中态(selectedId)是卡片
// 内的临时 UI 态,不入 data;机位的「选中即当前」:点选机位 gizmo 同时
// 把它设为渲染机位,与下拉联动。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { PrevizCamera, PrevizNodeData, PrevizObject, PrevizObjectKind } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import {
  PREVIZ_FOV_RANGE,
  PREVIZ_SCALE_RANGE,
  PREVIZ_KIND_LABEL,
  appendCamera,
  appendObject,
  clampFov,
  clampScale,
  normalizeRotation,
  removeCamera,
} from '../../previz'
import NodePorts from './NodePorts.vue'
import PrevizStage from './PrevizStage.vue'

const props = defineProps<{
  id: string
  type: string
  data: PrevizNodeData
  /** 渲染帧上传在途(编辑器级状态),本节点渲染按钮随之禁用。 */
  rendering: boolean
  /** 任一预演台的渲染帧上传在途(编辑器级全局),所有渲染按钮禁用 ——
   * 上传链一次一条,后台静默吞掉并发点击不如明示不可点。 */
  renderBusy: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  /** 场景数据变更(对象/机位/活跃机位),编辑器 updateNodeData 落盘。 */
  'scene-change': [patch: { objects: PrevizObject[]; cameras: PrevizCamera[]; active_camera_id?: string }]
  /** 渲染按钮产出的静帧 PNG,编辑器上传素材库并把 latest 写回节点。 */
  render: [blob: Blob]
  /** 落画布:把最新渲染帧落为新图片节点并连线(编辑器处理)。 */
  drop: []
  /** 点击媒体区打开灯箱(33 号票留口的挂入口)。 */
  preview: []
  /** 右 + 连下游(39 号票:预演台不作任何连线的目标,无左 +)。 */
  'connect-start': [side: ConnectSide]
}>()

const activeCameraId = computed(() => props.data.active_camera_id ?? props.data.cameras[0]?.id ?? '')

const selectedId = ref<string | undefined>(undefined)

const selectedObject = computed(() => props.data.objects.find((o) => o.id === selectedId.value) ?? null)
const selectedCamera = computed(() => props.data.cameras.find((c) => c.id === selectedId.value) ?? null)
/** 选中机位的注视点:选中态形如 `{camId}:target` 时命中。 */
const selectedTarget = computed(() => {
  const id = selectedId.value
  if (!id?.endsWith(':target')) {
    return null
  }
  return props.data.cameras.find((c) => c.id === id.slice(0, -':target'.length)) ?? null
})

// 撤销/重做会把场景整包换掉,选中态若指向已消失的实体则就地清空 ——
// 检查器与 gizmo 附着都吃这个态,悬空会让面板停在僵尸信息上。
watch([() => props.data.objects, () => props.data.cameras], () => {
  const id = selectedId.value
  if (!id) {
    return
  }
  const ok = id.endsWith(':target')
    ? props.data.cameras.some((c) => c.id === id.slice(0, -':target'.length))
    : props.data.objects.some((o) => o.id === id) || props.data.cameras.some((c) => c.id === id)
  if (!ok) {
    selectedId.value = undefined
  }
})

const stageRef = ref<InstanceType<typeof PrevizStage> | null>(null)

function onSelect(id: string | null): void {
  selectedId.value = id ?? undefined
  // 点选机位 gizmo = 查看谁就渲染谁,与下拉语义合流。
  if (id && !id.endsWith(':target') && props.data.cameras.some((c) => c.id === id)) {
    emitScene({ active_camera_id: id })
  }
}

/** 场景数据统一出口:整包上抛(数组换新,纯函数纪律),由编辑器落盘。 */
function emitScene(
  patch: Partial<{ objects: PrevizObject[]; cameras: PrevizCamera[]; active_camera_id?: string }> = {},
): void {
  emit('scene-change', {
    objects: patch.objects ?? props.data.objects,
    cameras: patch.cameras ?? props.data.cameras,
    active_camera_id: patch.active_camera_id ?? activeCameraId.value,
  })
}

function onStageObjectChange(object: PrevizObject): void {
  emitScene({ objects: props.data.objects.map((o) => (o.id === object.id ? object : o)) })
}

function onStageCameraChange(camera: PrevizCamera): void {
  emitScene({ cameras: props.data.cameras.map((c) => (c.id === camera.id ? camera : c)) })
}

function addObject(kind: PrevizObjectKind): void {
  const { object, objects } = appendObject(props.data.objects, kind)
  emitScene({ objects })
  selectedId.value = object.id
}

function addCamera(): void {
  const { camera, cameras } = appendCamera(props.data.cameras)
  emitScene({ cameras, active_camera_id: camera.id })
  selectedId.value = camera.id
}

function onCameraPicked(id: string): void {
  emitScene({ active_camera_id: id })
  selectedId.value = id
}

/** 删除选中实体:对象直接删;机位走 removeCamera 的「至少保留一个」
 * 守卫(最后一个机位是渲染出口,不可删,按钮禁用)。 */
function removeSelected(): void {
  if (selectedObject.value) {
    emitScene({ objects: props.data.objects.filter((o) => o.id !== selectedObject.value!.id) })
    selectedId.value = undefined
    return
  }
  if (selectedCamera.value) {
    const next = removeCamera(props.data.cameras, selectedCamera.value.id, activeCameraId.value)
    if (!next) {
      return
    }
    emitScene({ cameras: next.cameras, active_camera_id: next.activeCameraId })
    selectedId.value = undefined
  }
}

const canRemoveSelected = computed(
  () => selectedObject.value !== null || (selectedCamera.value !== null && props.data.cameras.length > 1),
)

// ---- 卡片滑杆(旋转/缩放/FOV):入口夹紧后走同一条 scene-change 出口 ----

function setObjectRotation(degrees: number): void {
  const obj = selectedObject.value
  if (!obj) {
    return
  }
  emitScene({
    objects: props.data.objects.map((o) => (o.id === obj.id ? { ...o, rotation: normalizeRotation(degrees) } : o)),
  })
}

function setObjectScale(scale: number): void {
  const obj = selectedObject.value
  if (!obj) {
    return
  }
  emitScene({
    objects: props.data.objects.map((o) => (o.id === obj.id ? { ...o, scale: clampScale(scale) } : o)),
  })
}

function setCameraFov(fov: number): void {
  const cam = selectedCamera.value
  if (!cam) {
    return
  }
  emitScene({
    cameras: props.data.cameras.map((c) => (c.id === cam.id ? { ...c, fov: clampFov(fov) } : c)),
  })
}

// ---- 渲染与落画布 ----

async function onRender(): Promise<void> {
  if (props.rendering) {
    return
  }
  const blob = await stageRef.value?.captureFrame()
  if (blob) {
    emit('render', blob)
  }
}

// ---- 渲染帧加载失败占位(素材被删等同款纪律,14 号票)----

const latestFailed = ref(false)
watch(
  () => props.data.latest?.url,
  () => {
    latestFailed.value = false
  },
)
</script>

<template>
  <div
    class="node previz"
    :class="connectState ? `connect-${connectState}` : undefined"
  >
    <header>预演台</header>

    <PrevizStage
      ref="stageRef"
      :objects="data.objects"
      :cameras="data.cameras"
      :active-camera-id="activeCameraId"
      :selected-id="selectedId"
      @select="onSelect"
      @object-change="onStageObjectChange"
      @camera-change="onStageCameraChange"
    />

    <div class="add-row">
      <button
        v-for="kind in (['box', 'cylinder', 'board', 'humanoid'] as const)"
        :key="kind"
        type="button"
        :title="`添加${PREVIZ_KIND_LABEL[kind]}`"
        @click="addObject(kind)"
      >
        + {{ PREVIZ_KIND_LABEL[kind] }}
      </button>
    </div>

    <div class="camera-row">
      <select
        :value="activeCameraId"
        title="当前机位(渲染出口)"
        @change="onCameraPicked(($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="c in data.cameras"
          :key="c.id"
          :value="c.id"
        >
          {{ c.name }}
        </option>
      </select>
      <button
        type="button"
        title="添加机位"
        @click="addCamera"
      >
        +机位
      </button>
      <button
        class="render"
        type="button"
        :disabled="rendering || renderBusy"
        :title="renderBusy && !rendering ? '另一帧正在上传,稍候再点' : '按当前机位渲染静帧 PNG(本地 WebGL,零计费)'"
        @click="onRender"
      >
        {{ rendering ? '出帧中…' : '渲染' }}
      </button>
    </div>

    <div
      v-if="selectedObject || selectedCamera || selectedTarget"
      class="inspector"
    >
      <div class="inspector-head">
        <span class="name">
          {{ selectedObject?.name ?? selectedCamera?.name ?? `${selectedTarget?.name}注视点` }}
        </span>
        <button
          v-if="!selectedTarget"
          type="button"
          class="remove"
          :disabled="!canRemoveSelected"
          title="删除选中实体(最后一个机位不可删)"
          @click="removeSelected"
        >
          删除
        </button>
      </div>
      <template v-if="selectedObject">
        <label class="slider">
          <span>旋转</span>
          <input
            type="range"
            min="-180"
            max="180"
            step="5"
            :value="selectedObject.rotation"
            @change="setObjectRotation(Number(($event.target as HTMLInputElement).value))"
          >
          <em>{{ Math.round(selectedObject.rotation) }}°</em>
        </label>
        <label class="slider">
          <span>缩放</span>
          <input
            type="range"
            :min="PREVIZ_SCALE_RANGE.min"
            :max="PREVIZ_SCALE_RANGE.max"
            step="0.1"
            :value="selectedObject.scale"
            @change="setObjectScale(Number(($event.target as HTMLInputElement).value))"
          >
          <em>{{ selectedObject.scale.toFixed(1) }}×</em>
        </label>
      </template>
      <label
        v-else-if="selectedCamera"
        class="slider"
      >
        <span>视角</span>
        <input
          type="range"
          :min="PREVIZ_FOV_RANGE.min"
          :max="PREVIZ_FOV_RANGE.max"
          step="1"
          :value="selectedCamera.fov"
          @change="setCameraFov(Number(($event.target as HTMLInputElement).value))"
        >
        <em>{{ Math.round(selectedCamera.fov) }}°</em>
      </label>
      <small
        v-else-if="selectedTarget"
        class="hint"
      >
        拖动黄色标记调整该机位的注视点
      </small>
    </div>

    <!-- 最新渲染帧(uploads/ 档素材引用):点击灯箱,悬浮下载。 -->
    <div
      v-if="data.latest && !latestFailed"
      class="media-frame"
      title="点击查看大图"
      @click="emit('preview')"
    >
      <img
        :src="data.latest.url"
        alt="预演台最新渲染帧"
        @error="latestFailed = true"
      >
      <a
        class="download"
        :href="data.latest.url"
        download
        title="下载渲染帧"
        aria-label="下载渲染帧"
        @click.stop
      >⬇</a>
    </div>
    <div
      v-else-if="data.latest && latestFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>尚无渲染帧</span>
      <small>摆好场景与机位后点「渲染」</small>
    </div>

    <button
      class="drop"
      type="button"
      :disabled="!data.latest?.url || latestFailed"
      title="把最新渲染帧落为新图片节点并连线,可喂生成对话框参考条/首帧"
      @click="emit('drop')"
    >
      落画布
    </button>

    <!-- 39 号票:预演台不作任何连线的目标,只渲染右 + 连下游。 -->
    <NodePorts
      :sides="['right']"
      @start="emit('connect-start', $event)"
    />

    <Handle
      type="source"
      :position="Position.Right"
    />
  </div>
</template>

<style scoped>
.node {
  width: 340px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(244, 114, 182, 0.4);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #f472b6;
  margin-bottom: 8px;
}

:global(.vue-flow__node.selected) .media-frame {
  outline: 2px solid rgba(122, 162, 247, 0.8);
  outline-offset: 2px;
}

.add-row,
.camera-row {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.add-row button,
.camera-row button {
  flex: 1;
  min-width: 0;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  padding: 5px 0;
  font-size: 12px;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.05);
  color: #cdd7ee;
  white-space: nowrap;
}

.add-row button:hover,
.camera-row button:hover {
  border-color: rgba(244, 114, 182, 0.6);
}

.camera-row select {
  flex: 1.4;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 5px;
  color: inherit;
  font-size: 12px;
}

.camera-row .render {
  flex: 1;
  background: rgba(244, 114, 182, 0.16);
  border-color: rgba(244, 114, 182, 0.55);
  color: #f472b6;
  font-weight: 600;
}

.camera-row .render:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 选中实体的小检查器:名称 + 滑杆(+ 删除);注视点只读提示。 */
.inspector {
  margin-top: 8px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px 8px;
  display: grid;
  gap: 4px;
}

.inspector-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.inspector-head .name {
  font-weight: 600;
  font-size: 12px;
  color: #cdd7ee;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.inspector-head .remove {
  flex-shrink: 0;
  border: 1px solid rgba(255, 143, 143, 0.4);
  border-radius: 8px;
  padding: 2px 10px;
  font-size: 11px;
  cursor: pointer;
  background: transparent;
  color: #ff8f8f;
}

.inspector-head .remove:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.slider {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: #8b91a7;
}

.slider span {
  flex-shrink: 0;
  width: 28px;
}

.slider input[type='range'] {
  flex: 1;
  min-width: 0;
  accent-color: #f472b6;
}

.slider em {
  flex-shrink: 0;
  width: 38px;
  text-align: right;
  font-style: normal;
  color: #cdd7ee;
}

.hint {
  font-size: 11px;
  color: #8b91a7;
}

.media-frame {
  position: relative;
  margin-top: 8px;
  cursor: zoom-in;
}

.media-frame img {
  display: block;
  width: 100%;
  border-radius: 10px;
}

.media-frame .download {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: 8px;
  background: rgba(13, 18, 32, 0.85);
  color: #dfe3ee;
  font-size: 14px;
  text-decoration: none;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.media-frame:hover .download,
.media-frame .download:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .media-frame .download {
    opacity: 1;
  }
}

.placeholder {
  margin-top: 8px;
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 18px 8px;
  border: 1px dashed rgba(255, 255, 255, 0.22);
  border-radius: 8px;
  color: #8b91a7;
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: 11px;
}

.placeholder.missing {
  border-color: rgba(255, 143, 143, 0.4);
  color: #ff8f8f;
}

.drop {
  width: 100%;
  margin-top: 8px;
  border: 1px solid rgba(122, 162, 247, 0.55);
  border-radius: 8px;
  padding: 7px 0;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: rgba(122, 162, 247, 0.12);
  color: #7aa2f7;
}

.drop:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
