<script setup lang="ts">
// 预演台全屏编辑层(40 号票):预演台的排练工作台,编辑能力自画布卡片
// 整体搬迁而来(39 号票的交互语义原样保留,不新增不削减)。编辑器级
// 单实例浮层(Teleport 到 body,灯箱/生成对话框同族范式,ADR 0002),
// 全屏遮挡时画布不卸载 —— 关闭即回原位,场景改动仍走同一条 scene-change
// → updateNodeData → autosave 落盘链。布局 = 顶栏(返回/机位/渲染/落
// 画布)+ 左面板(对象/机位列表与添加)+ 全屏 3D 视口 + 右面板检查器;
// 选中态(selectedId)是层内临时 UI 态,不入 data;机位的「选中即当前」:
// 点选机位 gizmo 同时把它设为渲染机位。渲染帧大图(灯箱)入口在本层。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { PrevizCamera, PrevizNodeData, PrevizObject, PrevizObjectKind } from '../graph'
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
} from '../previz'
import PrevizStage from './nodes/PrevizStage.vue'

const props = defineProps<{
  data: PrevizNodeData
  /** 渲染帧上传在途(编辑器级状态),本节点渲染按钮随之禁用。 */
  rendering: boolean
  /** 任一预演台的渲染帧上传在途(编辑器级全局),渲染按钮禁用。 */
  renderBusy: boolean
}>()

const emit = defineEmits<{
  /** 场景数据变更(对象/机位/活跃机位),编辑器 updateNodeData 落盘。 */
  'scene-change': [patch: { objects: PrevizObject[]; cameras: PrevizCamera[]; active_camera_id?: string }]
  /** 渲染按钮产出的静帧 PNG,编辑器上传素材库并把 latest 写回节点。 */
  render: [blob: Blob]
  /** 落画布:把最新渲染帧落为新图片节点并连线(编辑器处理)。 */
  drop: []
  /** 查看渲染帧大图:编辑器级灯箱打开(33 号票组件,入口收进本层)。 */
  preview: []
  /** 关闭编辑层,返回画布(原位不丢)。 */
  close: []
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

// ---- 检查器滑杆(旋转/缩放/FOV):入口夹紧后走同一条 scene-change 出口 ----

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

// ---- 打开时收焦点、Esc 关闭并归还 ----

const rootEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

// Esc 走元素级 keydown(焦点在层内子树才触发),不用 window 监听:
// 层上可再开编辑器级灯箱(查看帧),灯箱自己也在 window 上听 Esc ——
// 若两层同听,按一次 Esc 会把灯箱和编辑层一起关掉;灯箱打开时焦点在
// 灯箱根上,事件不会落进本层子树,关闭后焦点归还原按钮,Esc 恢复关层。
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
  }
}

onMounted(() => {
  opener = (document.activeElement as HTMLElement | null) ?? null
  rootEl.value?.focus()
})
onBeforeUnmount(() => {
  opener?.focus?.()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="rootEl"
      class="layer"
      tabindex="-1"
      @keydown.esc="onKeydown"
    >
      <header class="bar">
        <button
          class="back"
          type="button"
          title="关闭排练,返回画布(Esc)"
          @click="emit('close')"
        >
          ← 返回画布
        </button>
        <span class="title">预演台排练</span>

        <label class="camera-pick">
          <span>机位</span>
          <select
            :value="activeCameraId"
            title="当前机位(渲染出口);点选场景里的机位同样切换"
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
        </label>
        <button
          type="button"
          class="ghost"
          title="添加机位"
          @click="addCamera"
        >
          +机位
        </button>

        <span class="spacer" />

        <button
          v-if="data.latest"
          type="button"
          class="ghost"
          title="查看最新渲染帧大图"
          @click="emit('preview')"
        >
          查看帧
        </button>
        <button
          type="button"
          class="primary"
          :disabled="rendering || renderBusy"
          :title="renderBusy && !rendering ? '另一帧正在上传,稍候再点' : '按当前机位渲染静帧 PNG(本地 WebGL,零计费)'"
          @click="onRender"
        >
          {{ rendering ? '出帧中…' : '渲染' }}
        </button>
        <button
          type="button"
          class="ghost"
          :disabled="!data.latest"
          title="把最新渲染帧落为新图片节点并连线,可喂生成对话框参考条/首帧"
          @click="emit('drop')"
        >
          落画布
        </button>
      </header>

      <div class="body">
        <aside class="panel">
          <section class="group">
            <div class="group-title">
              对象
            </div>
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
            <ul class="entities">
              <li
                v-for="o in data.objects"
                :key="o.id"
                :class="{ active: selectedId === o.id }"
                @click="onSelect(o.id)"
              >
                <i
                  class="dot"
                  :class="o.kind"
                />
                {{ o.name }}
              </li>
            </ul>
          </section>

          <section class="group">
            <div class="group-title">
              机位
            </div>
            <ul class="entities">
              <li
                v-for="c in data.cameras"
                :key="c.id"
                :class="{ active: selectedId === c.id }"
                @click="onSelect(c.id)"
              >
                <i class="dot camera" />
                {{ c.name }}
                <em
                  v-if="c.id === activeCameraId"
                  class="badge"
                >当前</em>
              </li>
            </ul>
            <small class="hint">点选机位即设为渲染机位;拖黄色标记调注视点</small>
          </section>
        </aside>

        <div class="viewport">
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
        </div>

        <aside class="panel">
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
          <div
            v-else
            class="inspector empty"
          >
            <small class="hint">
              点选场景中的对象、机位或注视点进行编辑;左键拖拽转视角、右键平移、滚轮缩放。
            </small>
          </div>
        </aside>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.layer {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  flex-direction: column;
  background: #10162a;
  color: #cdd7ee;
  font-size: 13px;
}

.layer:focus {
  outline: none;
}

/* ---- 顶栏 ---- */

.bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(20, 26, 43, 0.95);
}

.bar .title {
  font-weight: 600;
  color: #f472b6;
}

.bar .spacer {
  flex: 1;
}

.bar button,
.camera-pick select {
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.05);
  color: #cdd7ee;
  white-space: nowrap;
}

.bar button:hover {
  border-color: rgba(244, 114, 182, 0.6);
}

.bar .back:hover {
  border-color: rgba(122, 162, 247, 0.7);
}

.bar .ghost:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.bar .primary {
  background: rgba(244, 114, 182, 0.16);
  border-color: rgba(244, 114, 182, 0.55);
  color: #f472b6;
  font-weight: 600;
}

.bar .primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.camera-pick {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #8b91a7;
}

.camera-pick select {
  padding: 6px;
}

/* ---- 三栏主体 ---- */

.body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.panel {
  width: 230px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 12px;
  overflow-y: auto;
  background: rgba(20, 26, 43, 0.75);
}

.panel:first-child {
  border-right: 1px solid rgba(255, 255, 255, 0.08);
}

.panel:last-child {
  border-left: 1px solid rgba(255, 255, 255, 0.08);
}

.group {
  display: grid;
  gap: 8px;
}

.group-title {
  font-size: 12px;
  font-weight: 600;
  color: #8b91a7;
}

.add-row {
  display: flex;
  gap: 6px;
}

.add-row button {
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

.add-row button:hover {
  border-color: rgba(244, 114, 182, 0.6);
}

.entities {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 2px;
}

.entities li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 8px;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.entities li:hover {
  background: rgba(255, 255, 255, 0.05);
}

.entities li.active {
  background: rgba(122, 162, 247, 0.16);
  color: #fff;
}

.entities .dot {
  flex-shrink: 0;
  width: 10px;
  height: 10px;
  border-radius: 3px;
  background: #5b6b8c;
}

.entities .dot.cylinder {
  background: #5c8a72;
}

.entities .dot.board {
  background: #8c7a5b;
}

.entities .dot.humanoid {
  background: #d8dce6;
}

.entities .dot.camera {
  border-radius: 50%;
  background: #f472b6;
}

.entities .badge {
  flex-shrink: 0;
  font-style: normal;
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(244, 114, 182, 0.2);
  color: #f472b6;
}

/* ---- 视口 ---- */

.viewport {
  flex: 1;
  position: relative;
  min-width: 0;
  min-height: 0;
}

/* ---- 检查器 ---- */

.inspector {
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 8px;
  display: grid;
  gap: 6px;
}

.inspector.empty {
  border-style: dashed;
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
  line-height: 1.6;
}
</style>
