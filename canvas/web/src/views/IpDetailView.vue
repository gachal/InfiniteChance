<script setup lang="ts">
// IP 详情页(50 号票;53 号票 A 段画布回归通用):第二层 —— 头部信息
// (封面/名称/简介/画布数/最近更新)+ 编辑/删除;「新建画布」对话框 =
// 仅名称输入(画布不再带媒介类型,POST 只带 ip_id,后端恒落 general),
// 提交后直进编辑器;画布区平铺展示(接口本就按 updated_at 新到旧),
// 行内操作:打开/重命名/移动到其他 IP/删除(移动只改归属,历史素材不回填
// —— 票面口径,这里只发起请求);素材区复用素材面板(scope=ip,上传带
// 该 IP)。角色区(51 号票)插在画布区与素材区之间:角色卡墙(首图投影
// cover_asset_id + 名称/别名/变体与图计数)+ 新建角色对话框 + 详情抽屉
// (CharacterDrawer 自管数据,卡墙经 changed 事件同步)。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  ApiError,
  type AssetRecord,
  type CanvasSummary,
  type CharacterRecord,
  type IPRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'
import { characterImageContentURL, characterImageThumbURL } from '../characters'
import AssetPanel from '../components/AssetPanel.vue'
import AssetPicker from '../components/AssetPicker.vue'
import CharacterDrawer from '../components/CharacterDrawer.vue'

const route = useRoute()
const router = useRouter()
const { client, clearSession } = useAuth()

const ipId = computed(() => Number(route.params.id))

const ip = ref<IPRecord | null>(null)
const canvases = ref<CanvasSummary[]>([])
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')

// 两步确认删除(IP 与画布各自独立确认)。
const confirmingDeleteIP = ref(false)
const confirmingCanvasId = ref<number | null>(null)

// 画布行内状态。
const renamingId = ref<number | null>(null)
const renameDraft = ref('')
const moveTargetId = ref<number | null>(null)
const movingId = ref<number | null>(null)

// 新建画布对话框:仅名称(53 号票 A 段,画布回归通用)。
const createOpen = ref(false)
const createName = ref('')
const creating = ref(false)

// IP 编辑对话框(名称 + 简介 + 封面;封面交互对齐 IP 列表页编辑态:
// 素材库选记 id 随保存 PATCH,本机上传即时生效)。
const editOpen = ref(false)
const editName = ref('')
const editDescription = ref('')
const editCoverId = ref(0)
const savingIP = ref(false)

// 封面挑选与上传(列表页同款三入口:素材库选/本机上传/清除)。
const coverPickerOpen = ref(false)
const coverFileInput = ref<HTMLInputElement | null>(null)
const uploadingCover = ref(false)

/** 对话框封面预览地址(缩略 302,49 号票)。 */
const editCoverURL = computed(() =>
  editCoverId.value > 0 ? `/api/assets/${editCoverId.value}/thumb` : '',
)

/** 图片缩略走 thumbnail_url(49 号票)。 */
const thumbs = useThumbFallback()

async function refresh(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const [ipRow, canvasRows] = await Promise.all([client.getIP(ipId.value), client.listCanvases({ ip_id: ipId.value })])
    ip.value = ipRow
    canvases.value = canvasRows
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      await router.replace({ name: 'login' })
      return
    }
    loadError.value = e instanceof ApiError ? e.message : '无法加载 IP 详情,请稍后再试'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (!Number.isInteger(ipId.value) || ipId.value < 1) {
    loadError.value = 'IP id 不合法'
    loading.value = false
    return
  }
  void refresh()
  void loadOtherIPs()
  void loadCharacters()
})

function openCreate(): void {
  createName.value = ''
  actionError.value = ''
  createOpen.value = true
}

async function submitCreate(): Promise<void> {
  const name = createName.value.trim()
  if (!name || creating.value) {
    return
  }
  creating.value = true
  actionError.value = ''
  try {
    // 53 号票 A 段:POST 不带 type,后端恒落缺省 general;存量画布的
    // 旧类型值成为不可见的库内历史事实。
    const created = await client.createCanvas({ name, ip_id: ipId.value })
    createOpen.value = false
    await router.push({ name: 'canvas-editor', params: { id: created.id } })
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '创建失败,请稍后再试'
  } finally {
    creating.value = false
  }
}

function openIPDetail(c: CanvasSummary): void {
  void router.push({ name: 'canvas-editor', params: { id: c.id } })
}

function startRename(c: CanvasSummary): void {
  renamingId.value = c.id
  renameDraft.value = c.name
  confirmingCanvasId.value = null
}

async function commitRename(): Promise<void> {
  const id = renamingId.value
  const name = renameDraft.value.trim()
  if (id === null || !name) {
    renamingId.value = null
    return
  }
  actionError.value = ''
  try {
    const renamed = await client.renameCanvas(id, name)
    const index = canvases.value.findIndex((c) => c.id === id)
    if (index >= 0) {
      canvases.value.splice(index, 1, renamed)
    }
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '重命名失败,请稍后再试'
  } finally {
    renamingId.value = null
  }
}

/** 移动到其他 IP:下拉选定即提交;历史素材留在原 IP(服务端语义)。 */
async function move(c: CanvasSummary, targetIP: number): Promise<void> {
  if (targetIP === c.ip_id || movingId.value !== null) {
    moveTargetId.value = null
    return
  }
  movingId.value = c.id
  actionError.value = ''
  try {
    const moved = await client.patchCanvas(c.id, { ip_id: targetIP })
    canvases.value = canvases.value.filter((x) => x.id !== c.id)
    if (moved.ip_id !== ipId.value) {
      // 移走了:留在列表外,但给个可见反馈。
      actionError.value = `「${moved.name}」已移动到「${moved.ip_name || '其他 IP'}」。`
    }
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '移动失败,请稍后再试'
  } finally {
    movingId.value = null
    moveTargetId.value = null
  }
}

async function removeCanvas(id: number): Promise<void> {
  actionError.value = ''
  try {
    await client.deleteCanvas(id)
    canvases.value = canvases.value.filter((c) => c.id !== id)
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试'
  } finally {
    confirmingCanvasId.value = null
  }
}

function openEditIP(): void {
  if (!ip.value) {
    return
  }
  editName.value = ip.value.name
  editDescription.value = ip.value.description
  editCoverId.value = ip.value.cover_asset_id
  coverPickerOpen.value = false
  actionError.value = ''
  editOpen.value = true
}

function closeEditIP(): void {
  editOpen.value = false
  coverPickerOpen.value = false
}

/** 素材库选中封面:记 id,随保存一并 PATCH(列表页同语义)。 */
function onCoverPicked(a: AssetRecord): void {
  editCoverId.value = a.id
  coverPickerOpen.value = false
}

/** 本机上传封面:详情页只有编辑态(IP 必已存在),直接带该 IP 上传并
 * 即时 PATCH(列表页编辑态同语义),头部封面随之刷新。 */
function onCoverFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) {
    void uploadCoverIP(file)
  }
}

async function uploadCoverIP(file: File): Promise<void> {
  if (!ip.value) {
    return
  }
  uploadingCover.value = true
  actionError.value = ''
  try {
    const a = await client.uploadAsset(file, 'image', ip.value.id)
    ip.value = await client.patchIP(ip.value.id, { cover_asset_id: a.id })
    editCoverId.value = a.id
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '封面上传失败,请稍后再试'
  } finally {
    uploadingCover.value = false
  }
}

function clearCover(): void {
  editCoverId.value = 0
}

async function saveIP(): Promise<void> {
  if (!ip.value || savingIP.value) {
    return
  }
  const name = editName.value.trim()
  if (!name) {
    return
  }
  savingIP.value = true
  actionError.value = ''
  try {
    ip.value = await client.patchIP(ip.value.id, {
      name,
      description: editDescription.value.trim(),
      cover_asset_id: editCoverId.value,
    })
    closeEditIP()
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '保存失败,请稍后再试'
  } finally {
    savingIP.value = false
  }
}

async function removeIP(): Promise<void> {
  if (!ip.value) {
    return
  }
  actionError.value = ''
  try {
    await client.deleteIP(ip.value.id)
    await router.replace({ name: 'ips' })
  } catch (e) {
    if (e instanceof ApiError && e.code === 'ip_not_empty') {
      actionError.value = '该 IP 名下还有画布或素材,先移走再删除。'
    } else {
      actionError.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试'
    }
  } finally {
    confirmingDeleteIP.value = false
  }
}

/** 其他 IP 名单(移动画布的下拉;不含画布当前所属)。 */
const otherIPs = ref<IPRecord[]>([])

// ---- 角色区(51 号票):卡墙 + 新建对话框 + 详情抽屉 ----

const characters = ref<CharacterRecord[]>([])
const charactersLoading = ref(false)
const charactersError = ref('')

// 新建角色:名称必填 + 别名/简介/详细设定。
const createCharacterOpen = ref(false)
const characterName = ref('')
const characterAlias = ref('')
const characterDescription = ref('')
const characterDetail = ref('')
const creatingCharacter = ref(false)

// 详情抽屉:按角色 id 换 key 重挂(CharacterDrawer 自取数据)。
const drawerCharacterId = ref<number | null>(null)

/** 角色卡的图计数(全部变体的设定图总数)。 */
const imageCount = (c: CharacterRecord): number =>
  c.variants.reduce((sum, v) => sum + v.images.length, 0)

async function loadCharacters(): Promise<void> {
  charactersLoading.value = true
  charactersError.value = ''
  try {
    characters.value = await client.listIPCharacters(ipId.value)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      await router.replace({ name: 'login' })
      return
    }
    charactersError.value = e instanceof ApiError ? e.message : '无法加载角色,请稍后再试'
  } finally {
    charactersLoading.value = false
  }
}

function openCreateCharacter(): void {
  characterName.value = ''
  characterAlias.value = ''
  characterDescription.value = ''
  characterDetail.value = ''
  actionError.value = ''
  createCharacterOpen.value = true
}

async function submitCreateCharacter(): Promise<void> {
  const name = characterName.value.trim()
  if (!name || creatingCharacter.value) {
    return
  }
  creatingCharacter.value = true
  actionError.value = ''
  try {
    const created = await client.createCharacter(ipId.value, {
      name,
      alias: characterAlias.value.trim(),
      description: characterDescription.value.trim(),
      detail: characterDetail.value,
    })
    createCharacterOpen.value = false
    await loadCharacters()
    drawerCharacterId.value = created.id
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '创建角色失败,请稍后再试'
  } finally {
    creatingCharacter.value = false
  }
}

/** 角色封面缩略的两阶段回落(缩略失败回原图,原图再失败落占位)。 */
function onCharacterCoverError(c: CharacterRecord): void {
  thumbs.onThumbError(characterImageContentURL(c.cover_asset_id))
}

function characterCoverThumb(c: CharacterRecord): string {
  return characterImageThumbURL(c.cover_asset_id)
}

function characterCoverContent(c: CharacterRecord): string {
  return characterImageContentURL(c.cover_asset_id)
}

async function loadOtherIPs(): Promise<void> {
  try {
    otherIPs.value = await client.listIPs()
  } catch {
    /* 拉不到就隐藏移动入口的下拉数据,不影响其他操作 */
  }
}

/** 抽屉内的任何变更(CharacterDrawer 自取后回抛的最新角色)同步进卡墙:
 * 排序键 updated_at 在服务端随动作推进,直接按 id 替换。 */
function onDrawerChanged(fresh: CharacterRecord): void {
  const index = characters.value.findIndex((c) => c.id === fresh.id)
  if (index >= 0) {
    characters.value.splice(index, 1, fresh)
  } else {
    characters.value.unshift(fresh)
  }
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function back(): void {
  void router.push({ name: 'ips' })
}
</script>

<template>
  <main class="shell">
    <header class="masthead">
      <button
        class="ghost"
        type="button"
        @click="back"
      >
        ← IP 列表
      </button>
      <button
        class="ghost"
        type="button"
        @click="openCreate()"
      >
        + 新建画布
      </button>
    </header>

    <p
      v-if="loadError && !ip"
      class="feedback error"
      role="alert"
    >
      {{ loadError }}
      <button
        class="ghost"
        type="button"
        @click="refresh"
      >
        重试
      </button>
    </p>

    <template v-else-if="ip">
      <section class="ip-head">
        <span class="cover">
          <img
            v-if="ip.cover_asset_id > 0"
            :src="thumbs.displayURL(`/api/assets/${ip.cover_asset_id}/content`, `/api/assets/${ip.cover_asset_id}/thumb`)"
            class="cover-img"
            alt="IP 封面"
            @error="thumbs.onThumbError(`/api/assets/${ip.cover_asset_id}/content`)"
          >
          <span
            v-else
            class="cover-fallback"
          >{{ ip.name.slice(0, 1) }}</span>
        </span>
        <div class="head-info">
          <h1>{{ ip.name }}</h1>
          <p class="description">
            {{ ip.description || '(还没有一句话简介)' }}
          </p>
          <p class="meta">
            {{ ip.canvas_count }} 张画布 · 更新于 {{ formatTime(ip.latest_at) }}
          </p>
        </div>
        <span class="head-actions">
          <button
            type="button"
            class="ghost"
            @click="openEditIP"
          >
            编辑 IP
          </button>
          <template v-if="confirmingDeleteIP">
            <button
              type="button"
              class="danger"
              @click="removeIP"
            >
              确认删除
            </button>
            <button
              type="button"
              class="ghost"
              @click="confirmingDeleteIP = false"
            >
              取消
            </button>
          </template>
          <button
            v-else
            type="button"
            class="ghost"
            @click="confirmingDeleteIP = true"
          >
            删除 IP
          </button>
        </span>
      </section>

      <p
        v-if="actionError"
        class="feedback error"
        role="alert"
      >
        {{ actionError }}
      </p>
      <p
        v-else-if="loadError"
        class="feedback error"
        role="alert"
      >
        {{ loadError }}
      </p>

      <!-- 画布区(53 号票 A 段平铺):接口本就按 updated_at 新到旧,直接
           单列表展示,不再按类型分组。 -->
      <section class="canvas-section">
        <h2>
          画布
          <span class="count">{{ canvases.length }}</span>
        </h2>
        <p
          v-if="canvases.length === 0"
          class="feedback"
        >
          还没有画布 —— 新建一张,从第一格分镜开始。
        </p>
        <ul
          v-else
          class="list"
        >
          <li
            v-for="c in canvases"
            :key="c.id"
          >
            <template v-if="renamingId === c.id">
              <form
                class="rename-form"
                @submit.prevent="commitRename"
              >
                <input
                  v-model="renameDraft"
                  type="text"
                  maxlength="128"
                  autofocus
                >
                <button
                  type="submit"
                  class="primary"
                >
                  保存
                </button>
                <button
                  type="button"
                  class="ghost"
                  @click="renamingId = null"
                >
                  取消
                </button>
              </form>
            </template>
            <template v-else>
              <button
                class="open"
                type="button"
                @click="openIPDetail(c)"
              >
                <span class="name">{{ c.name }}</span>
                <span class="meta">更新于 {{ formatTime(c.updated_at) }}</span>
              </button>
              <span class="row-actions">
                <button
                  type="button"
                  class="ghost"
                  @click="startRename(c)"
                >
                  重命名
                </button>
                <select
                  class="move-select"
                  :class="{ placeholder: moveTargetId !== c.id }"
                  :value="''"
                  :disabled="movingId === c.id"
                  aria-label="移动到其他 IP"
                  @change="move(c, Number(($event.target as HTMLSelectElement).value))"
                >
                  <option
                    value=""
                    disabled
                  >
                    移动到其他 IP…
                  </option>
                  <option
                    v-for="o in otherIPs.filter((x) => x.id !== ipId)"
                    :key="o.id"
                    :value="o.id"
                  >
                    {{ o.name }}
                  </option>
                </select>
                <template v-if="confirmingCanvasId === c.id">
                  <button
                    type="button"
                    class="danger"
                    @click="removeCanvas(c.id)"
                  >
                    确认删除
                  </button>
                  <button
                    type="button"
                    class="ghost"
                    @click="confirmingCanvasId = null"
                  >
                    取消
                  </button>
                </template>
                <button
                  v-else
                  type="button"
                  class="ghost"
                  @click="confirmingCanvasId = c.id"
                >
                  删除
                </button>
              </span>
            </template>
          </li>
        </ul>
      </section>

      <!-- 角色区(51 号票):IP 级角色实体,插在画布区与素材区之间。
           卡墙 = 首图投影 cover_asset_id + 名称/别名/变体与图计数。 -->
      <section class="character-section">
        <div class="section-head">
          <h2>角色</h2>
          <span class="section-count">{{ characters.length }}</span>
          <button
            type="button"
            class="ghost"
            @click="openCreateCharacter"
          >
            + 新建角色
          </button>
        </div>
        <p
          v-if="charactersError"
          class="feedback error"
          role="alert"
        >
          {{ charactersError }}
          <button
            class="ghost"
            type="button"
            @click="loadCharacters"
          >
            重试
          </button>
        </p>
        <p
          v-else-if="charactersLoading"
          class="feedback"
        >
          加载角色…
        </p>
        <p
          v-else-if="characters.length === 0"
          class="feedback"
        >
          还没有角色 —— 新建一个,从三视图开始立人设。
        </p>
        <ul
          v-else
          class="character-wall"
        >
          <li
            v-for="c in characters"
            :key="c.id"
          >
            <button
              type="button"
              class="character-card"
              @click="drawerCharacterId = c.id"
            >
              <span class="character-cover">
                <img
                  v-if="c.cover_asset_id > 0"
                  :src="thumbs.displayURL(characterCoverContent(c), characterCoverThumb(c))"
                  alt="角色首图"
                  loading="lazy"
                  @error="onCharacterCoverError(c)"
                >
                <span
                  v-else
                  class="cover-fallback small"
                >{{ c.name.slice(0, 1) }}</span>
              </span>
              <span class="character-name">
                {{ c.name }}
                <em
                  v-if="c.alias"
                  class="character-alias"
                >{{ c.alias }}</em>
              </span>
              <span class="character-meta">{{ c.variants.length }} 造型 · {{ imageCount(c) }} 图</span>
            </button>
          </li>
        </ul>
      </section>

      <!-- 素材区:复用素材面板,scope=ip(本 IP + 全局,可切全部),
           上传固定带该 IP;纯浏览形态不出插入按钮。 -->
      <section class="asset-section">
        <h2>素材</h2>
        <div class="asset-holder">
          <AssetPanel
            :ip-id="ipId"
            :upload-ip-id="ipId"
            :selectable="false"
          />
        </div>
      </section>
    </template>
    <p
      v-else-if="loading"
      class="feedback"
    >
      加载中…
    </p>

    <!-- 新建画布对话框(53 号票 A 段):仅名称。 -->
    <div
      v-if="createOpen"
      class="dialog-mask"
      @click.self="createOpen = false"
    >
      <div class="dialog">
        <h3>新建画布</h3>
        <form
          class="create-form"
          @submit.prevent="submitCreate"
        >
          <input
            v-model="createName"
            type="text"
            maxlength="128"
            placeholder="新画布名称…"
            autofocus
          >
          <p
            v-if="actionError"
            class="feedback error"
            role="alert"
          >
            {{ actionError }}
          </p>
          <div class="form-actions">
            <button
              type="submit"
              class="primary"
              :disabled="creating || !createName.trim()"
            >
              {{ creating ? '创建中…' : '创建并进入' }}
            </button>
            <button
              type="button"
              class="ghost"
              @click="createOpen = false"
            >
              取消
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- 编辑 IP 对话框:名称 + 简介 + 封面(交互与 IP 列表页编辑态同款)。 -->
    <div
      v-if="editOpen"
      class="dialog-mask"
      @click.self="closeEditIP"
    >
      <div class="dialog">
        <h3>编辑 IP 企划</h3>
        <form
          class="create-form"
          @submit.prevent="saveIP"
        >
          <label class="field">
            名称
            <input
              v-model="editName"
              type="text"
              maxlength="255"
              autofocus
            >
          </label>
          <label class="field">
            简介(一句话 logline)
            <input
              v-model="editDescription"
              type="text"
              maxlength="1024"
            >
          </label>
          <div class="cover-field">
            <span class="cover-label">封面</span>
            <span class="cover-preview">
              <img
                v-if="editCoverURL"
                :src="editCoverURL"
                alt="封面预览"
                @error="($event.target as HTMLImageElement).style.display = 'none'"
              >
              <span
                v-else
                class="placeholder"
              >{{ editName.trim().slice(0, 1) || '?' }}</span>
            </span>
            <span class="cover-actions">
              <button
                type="button"
                class="ghost"
                @click="coverPickerOpen = !coverPickerOpen"
              >
                从素材库选
              </button>
              <button
                type="button"
                class="ghost"
                :disabled="uploadingCover"
                @click="coverFileInput?.click()"
              >
                {{ uploadingCover ? '上传中…' : '本机上传' }}
              </button>
              <button
                v-if="editCoverId > 0"
                type="button"
                class="ghost"
                @click="clearCover"
              >
                清除
              </button>
            </span>
            <input
              ref="coverFileInput"
              type="file"
              accept="image/*"
              class="sr-input"
              @change="onCoverFileChange"
            >
          </div>
          <p
            v-if="actionError"
            class="feedback error"
            role="alert"
          >
            {{ actionError }}
          </p>
          <div class="form-actions">
            <button
              type="submit"
              class="primary"
              :disabled="savingIP || !editName.trim()"
            >
              {{ savingIP ? '保存中…' : '保存' }}
            </button>
            <button
              type="button"
              class="ghost"
              @click="closeEditIP"
            >
              取消
            </button>
          </div>
        </form>
        <!-- 封面挑选:本 IP + 全局(scope=ip),单选图片;面板盖住对话框右缘。 -->
        <AssetPicker
          v-if="coverPickerOpen"
          title="选择封面素材"
          :kind-whitelist="['image']"
          hint="点选设为封面;也可本机上传(即时生效)。"
          :selected-asset-ids="editCoverId > 0 ? [editCoverId] : []"
          :ip-id="ipId"
          @toggle="onCoverPicked"
          @close="coverPickerOpen = false"
        />
      </div>
    </div>
    <!-- 新建角色对话框(51 号票):名称必填 + 别名/简介/详细设定;
         建角色自动带「默认」变体,成功后直接打开详情抽屉。 -->
    <div
      v-if="createCharacterOpen"
      class="dialog-mask"
      @click.self="createCharacterOpen = false"
    >
      <div class="dialog">
        <h3>新建角色</h3>
        <form
          class="create-form"
          @submit.prevent="submitCreateCharacter"
        >
          <label class="field">
            名称
            <input
              v-model="characterName"
              type="text"
              maxlength="255"
              placeholder="角色名(同 IP 内可重名)"
              autofocus
            >
          </label>
          <label class="field">
            别名
            <input
              v-model="characterAlias"
              type="text"
              maxlength="255"
              placeholder="可选"
            >
          </label>
          <label class="field">
            简介(一句话)
            <input
              v-model="characterDescription"
              type="text"
              maxlength="1024"
              placeholder="可选"
            >
          </label>
          <label class="field">
            详细设定(纯文本)
            <textarea
              v-model="characterDetail"
              rows="4"
              maxlength="20000"
              placeholder="可选:性格、背景、发型发色…"
            />
          </label>
          <p
            v-if="actionError"
            class="feedback error"
            role="alert"
          >
            {{ actionError }}
          </p>
          <div class="form-actions">
            <button
              type="submit"
              class="primary"
              :disabled="creatingCharacter || !characterName.trim()"
            >
              {{ creatingCharacter ? '创建中…' : '创建' }}
            </button>
            <button
              type="button"
              class="ghost"
              @click="createCharacterOpen = false"
            >
              取消
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- 角色详情抽屉(51 号票):字段编辑 + 变体条 + 设定图画廊;按角色
         id 换 key 重挂,组件自管数据,changed 事件同步卡墙。 -->
    <CharacterDrawer
      v-if="drawerCharacterId !== null"
      :key="drawerCharacterId"
      :character-id="drawerCharacterId"
      :ip-id="ipId"
      @changed="onDrawerChanged"
      @close="drawerCharacterId = null"
    />
  </main>
</template>

<style scoped>
/* 详情页改版(58 号票阶段三):1080 壳、IP 头升格海报位(28px 展示档
   属于 IP 身份时刻)、区块节奏 token 化。 */
.shell {
  max-width: 1080px;
  margin: 0 auto;
  padding: 40px 32px 64px;
}

.masthead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-lg);
  margin-bottom: 20px;
}

.ghost,
.danger,
.primary {
  border: none;
  border-radius: var(--r-md);
  padding: 8px 14px;
  font-size: var(--fs-body);
  cursor: pointer;
  transition:
    background-color var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.ghost {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
}

.ghost:hover:not(:disabled) {
  border-color: var(--line-action);
  background: var(--wash-soft);
  color: var(--text-primary);
}

/* 危险操作 = 失败绯文字而非红底(DESIGN.md)。 */
.danger {
  background: transparent;
  border: 1px solid rgba(var(--danger-rgb), 0.5);
  color: var(--danger);
  font-weight: 600;
}

.danger:hover:not(:disabled) {
  background: rgba(var(--danger-rgb), 0.12);
}

.primary {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 700;
}

.primary:hover:not(:disabled) {
  background: var(--accent-soft);
}

.primary:disabled {
  opacity: 0.4;
  cursor: default;
}

/* IP 头:全站唯一的海报位之一,玻璃顶缘受光 + 展示档标题。 */
.ip-head {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-xl);
  padding: 20px;
  box-shadow: var(--glass-highlight);
}

.cover {
  display: block;
  width: 168px;
  aspect-ratio: 16 / 9;
  border-radius: var(--r-lg);
  overflow: hidden;
  flex-shrink: 0;
  background: var(--wash-soft);
}

.cover-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-fallback {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  font-size: 40px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.82);
  background: linear-gradient(135deg, var(--ip-indigo) 0%, var(--ip-violet) 55%, var(--ip-wine) 100%);
}

.head-info {
  flex: 1;
  min-width: 0;
}

.head-info h1 {
  margin: 0;
  font-size: 28px;
  letter-spacing: 0.5px;
}

.description {
  margin: 6px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.meta {
  margin: 8px 0 0;
  color: var(--text-faint);
  font-size: var(--fs-label);
}

.head-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.canvas-section {
  margin-top: 24px;
}

.canvas-section h2 {
  margin: 0 0 10px;
  font-size: var(--fs-title);
  display: flex;
  align-items: center;
  gap: var(--sp-sm);
}

.canvas-section .count {
  color: var(--text-faint);
  font-size: var(--fs-body);
  font-weight: 400;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--sp-sm);
}

.list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-md);
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-xl);
  padding: 8px 12px;
  transition: border-color var(--dur-fast) var(--ease);
}

.list li:hover {
  border-color: var(--line-action);
}

.open {
  flex: 1;
  display: grid;
  gap: 2px;
  text-align: left;
  background: transparent;
  border: none;
  color: inherit;
  padding: 6px 8px;
  border-radius: var(--r-md);
  cursor: pointer;
}

.open:hover {
  background: var(--wash-soft);
}

.open .name {
  font-size: var(--fs-title);
  font-weight: 600;
}

.open .meta {
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.row-actions {
  display: flex;
  gap: 6px;
  align-items: center;
}

.row-actions button {
  padding: 5px 10px;
  font-size: var(--fs-label);
}

.move-select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  color: var(--text-secondary);
  padding: 5px 8px;
  font-size: var(--fs-label);
}

.move-select.placeholder {
  color: var(--text-muted);
}

.rename-form {
  flex: 1;
  display: flex;
  gap: 8px;
}

.rename-form input {
  flex: 1;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-lg);
  padding: 8px 12px;
  color: inherit;
  font-size: var(--fs-title);
}

.feedback {
  margin: 14px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.error {
  color: var(--danger);
}

/* 素材区:面板原是编辑器右缘浮层,这里改为文档流内的固定高度区。 */
.asset-section {
  margin-top: 26px;
}

/* 角色区(51 号票):卡墙插在画布区与素材区之间。 */
.character-section {
  margin-top: 26px;
}

.section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.section-head h2 {
  margin: 0;
  font-size: var(--fs-title);
}

.section-count {
  color: var(--text-faint);
  font-size: var(--fs-body);
}

.section-head button {
  margin-left: auto;
  padding: 5px 10px;
  font-size: var(--fs-label);
}

.character-wall {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: var(--sp-md);
}

.character-card {
  display: grid;
  gap: 6px;
  text-align: left;
  width: 100%;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-xl);
  padding: 10px;
  color: inherit;
  cursor: pointer;
  transition:
    transform var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease);
}

.character-card:hover {
  transform: translateY(-2px);
  border-color: rgba(var(--accent-rgb), 0.5);
}

.character-cover {
  display: block;
  width: 100%;
  aspect-ratio: 1 / 1;
  border-radius: var(--r-lg);
  overflow: hidden;
  background: var(--wash-soft);
}

.character-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform var(--dur-slow) var(--ease);
}

.character-card:hover .character-cover img {
  transform: scale(1.03);
}

.cover-fallback.small {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  font-size: 32px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.82);
  background: linear-gradient(135deg, var(--ip-indigo) 0%, var(--ip-violet) 55%, var(--ip-wine) 100%);
}

.character-name {
  font-size: var(--fs-body);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.character-alias {
  font-style: normal;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 6px;
  font-size: var(--fs-label);
}

.character-meta {
  color: var(--text-faint);
  font-size: var(--fs-label);
}

/* 新建角色对话框的文本域与详情抽屉共用 pre-wrap 纯文本口径。 */
.create-form textarea {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-lg);
  padding: 10px 12px;
  color: inherit;
  font-size: var(--fs-body);
  resize: vertical;
  white-space: pre-wrap;
}

.asset-section h2 {
  margin: 0 0 10px;
  font-size: 16px;
}

.asset-holder {
  position: relative;
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  overflow: hidden;
  height: 420px;
}

.asset-holder :deep(.asset-panel) {
  position: absolute;
  border-left: none;
  background: rgba(var(--surface-panel-rgb), 0.6);
}

.dialog-mask {
  position: fixed;
  inset: 0;
  background: rgba(var(--bg-deep-rgb), 0.7);
  display: grid;
  place-items: center;
  z-index: var(--z-dialog-mask);
  animation: mask-in var(--dur) var(--ease);
}

.dialog {
  position: relative;
  overflow: hidden;
  width: 520px;
  background: var(--bg-navy);
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  padding: 20px;
  box-shadow: var(--shadow-dialog-strong);
  animation: dialog-in var(--dur-slow) var(--ease);
}

.dialog h3 {
  margin: 0 0 14px;
  font-size: var(--fs-title);
}

.create-form {
  display: grid;
  gap: 10px;
}

.create-form input {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 10px 12px;
  color: inherit;
  font-size: var(--fs-title);
  transition:
    border-color var(--dur-fast) var(--ease),
    box-shadow var(--dur-fast) var(--ease);
}

.create-form input:focus {
  outline: none;
  border-color: rgba(var(--accent-rgb), 0.85);
  box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.12);
}

.field {
  display: grid;
  gap: 6px;
  font-size: var(--fs-body);
  color: var(--text-secondary);
}

/* 封面字段(IP 列表页编辑对话框同款):预览 + 三入口 + 隐藏 file input。 */
.cover-field {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cover-label {
  font-size: var(--fs-body);
  color: var(--text-secondary);
}

.cover-preview {
  display: grid;
  place-items: center;
  width: 88px;
  height: 50px;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--wash-soft);
  font-size: 18px;
  font-weight: 700;
}

.cover-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-preview .placeholder {
  background: linear-gradient(135deg, var(--ip-indigo) 0%, var(--ip-violet) 55%, var(--ip-wine) 100%);
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
}

.cover-actions {
  display: flex;
  gap: 6px;
}

.cover-actions button {
  padding: 6px 10px;
  font-size: var(--fs-label);
}

.sr-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.form-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
</style>
