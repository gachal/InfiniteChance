<script setup lang="ts">
// IP 详情页(50 号票):第二层 —— 头部信息(封面/名称/简介/画布数/最近
// 更新)+ 编辑/删除;「新建画布」对话框 = 四张类型卡单选 + 名称输入,提交
// POST 带 ip_id + type 直进编辑器;画布区按类型分组展示(空类型不显示),
// 行内操作:打开/重命名/移动到其他 IP/删除(移动只改归属,历史素材不回填
// —— 票面口径,这里只发起请求);素材区复用素材面板(scope=ip,上传带
// 该 IP)。角色区(51 号票)插在画布区与素材区之间:角色卡墙(首图投影
// cover_asset_id + 名称/别名/变体与图计数)+ 新建角色对话框 + 详情抽屉
// (CharacterDrawer 自管数据,卡墙经 changed 事件同步)。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  ApiError,
  type CanvasSummary,
  type CharacterRecord,
  type IPRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'
import { characterImageContentURL, characterImageThumbURL } from '../characters'
import { CANVAS_TYPES, canvasTypeLabel } from '../canvasTypes'
import AssetPanel from '../components/AssetPanel.vue'
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

// 新建画布对话框:类型卡单选 + 名称。
const createOpen = ref(false)
const createType = ref('general')
const createName = ref('')
const creating = ref(false)

// IP 编辑对话框(名称 + 简介)。
const editOpen = ref(false)
const editName = ref('')
const editDescription = ref('')
const savingIP = ref(false)

// 图片缩略走 thumbnail_url(49 号票)。
const thumbs = useThumbFallback()

/** 类型注册表序 × 该类型的画布;空类型不显示。 */
const grouped = computed(() =>
  CANVAS_TYPES.map((t) => ({
    meta: t,
    items: canvases.value.filter((c) => c.type === t.id),
  })).filter((g) => g.items.length > 0),
)

/** 注册表之外的未知类型画布(type 不在四类先行里,后端不校验枚举):
 * 兜底一组照常展示,展示面永远可用。 */
const extraGroup = computed(() => {
  const known = new Set<string>(CANVAS_TYPES.map((t) => t.id as string))
  const items = canvases.value.filter((c) => !known.has(c.type))
  return items.length > 0 ? items : []
})

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

function openCreate(type: string): void {
  createType.value = type
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
    const created = await client.createCanvas({ name, ip_id: ipId.value, type: createType.value })
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
  actionError.value = ''
  editOpen.value = true
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
    ip.value = await client.patchIP(ip.value.id, { name, description: editDescription.value.trim() })
    editOpen.value = false
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
        @click="openCreate('general')"
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

      <!-- 新建画布:类型卡单选 + 名称(四类先行,注册表驱动)。 -->
      <section class="create-row">
        <button
          v-for="t in CANVAS_TYPES"
          :key="t.id"
          type="button"
          class="type-card"
          @click="openCreate(t.id)"
        >
          <span class="type-icon">{{ t.icon }}</span>
          <span class="type-label">新建{{ t.label }}画布</span>
          <span class="type-desc">{{ t.description }}</span>
        </button>
      </section>

      <section
        v-for="g in grouped"
        :key="g.meta.id"
        class="type-group"
      >
        <h2>
          <span class="type-icon">{{ g.meta.icon }}</span>
          {{ g.meta.label }}画布
          <span class="count">{{ g.items.length }}</span>
        </h2>
        <ul class="list">
          <li
            v-for="c in g.items"
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
                <span class="meta">{{ canvasTypeLabel(c.type) }} · 更新于 {{ formatTime(c.updated_at) }}</span>
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

      <!-- 注册表之外的类型(历史数据/手写串):兜底照常展示。 -->
      <section
        v-if="extraGroup.length > 0"
        class="type-group"
      >
        <h2>其他类型</h2>
        <ul class="list">
          <li
            v-for="c in extraGroup"
            :key="c.id"
          >
            <button
              class="open"
              type="button"
              @click="openIPDetail(c)"
            >
              <span class="name">{{ c.name }}</span>
              <span class="meta">{{ c.type }} · 更新于 {{ formatTime(c.updated_at) }}</span>
            </button>
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

    <!-- 新建画布对话框:类型卡单选 + 名称。 -->
    <div
      v-if="createOpen"
      class="dialog-mask"
      @click.self="createOpen = false"
    >
      <div class="dialog">
        <h3>新建画布</h3>
        <div class="type-pick">
          <button
            v-for="t in CANVAS_TYPES"
            :key="t.id"
            type="button"
            class="type-card"
            :class="{ active: createType === t.id }"
            @click="createType = t.id"
          >
            <span class="type-icon">{{ t.icon }}</span>
            <span class="type-label">{{ t.label }}</span>
            <span class="type-desc">{{ t.description }}</span>
          </button>
        </div>
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

    <!-- 编辑 IP 对话框:名称 + 简介(封面在 IP 列表页编辑)。 -->
    <div
      v-if="editOpen"
      class="dialog-mask"
      @click.self="editOpen = false"
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
              @click="editOpen = false"
            >
              取消
            </button>
          </div>
        </form>
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
.shell {
  max-width: 960px;
  margin: 0 auto;
  padding: 40px 24px 64px;
}

.masthead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}

.ghost,
.danger,
.primary {
  border: none;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.ghost {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: #aab1c5;
}

.ghost:hover:not(:disabled) {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

.danger {
  background: #e03131;
  color: #fff;
  font-weight: 600;
}

.primary {
  background: #3b5bdb;
  color: #fff;
  font-weight: 600;
}

.primary:disabled {
  opacity: 0.6;
  cursor: default;
}

.ip-head {
  display: flex;
  gap: 18px;
  align-items: flex-start;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  padding: 16px;
}

.cover {
  display: block;
  width: 168px;
  aspect-ratio: 16 / 9;
  border-radius: 10px;
  overflow: hidden;
  flex-shrink: 0;
  background: rgba(255, 255, 255, 0.05);
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
  background: linear-gradient(135deg, #243b8f 0%, #57348a 55%, #8a2f5c 100%);
}

.head-info {
  flex: 1;
  min-width: 0;
}

.head-info h1 {
  margin: 0;
  font-size: 24px;
}

.description {
  margin: 6px 0 0;
  color: #8b91a7;
  font-size: 14px;
}

.meta {
  margin: 8px 0 0;
  color: #6f7690;
  font-size: 12px;
}

.head-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.create-row {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
  margin: 20px 0 8px;
}

.type-card {
  display: grid;
  gap: 4px;
  text-align: left;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  padding: 12px;
  color: inherit;
  cursor: pointer;
}

.type-card:hover {
  border-color: rgba(122, 162, 247, 0.5);
}

.type-card.active {
  border-color: rgba(122, 162, 247, 0.9);
  background: rgba(76, 110, 245, 0.18);
}

.type-icon {
  font-size: 20px;
}

.type-label {
  font-size: 14px;
  font-weight: 600;
}

.type-desc {
  color: #8b91a7;
  font-size: 12px;
}

.type-group {
  margin-top: 22px;
}

.type-group h2 {
  margin: 0 0 10px;
  font-size: 16px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.type-group .count {
  color: #6f7690;
  font-size: 13px;
  font-weight: 400;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 8px;
}

.list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  padding: 8px 12px;
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
  border-radius: 8px;
  cursor: pointer;
}

.open:hover {
  background: rgba(255, 255, 255, 0.06);
}

.open .name {
  font-size: 15px;
  font-weight: 600;
}

.open .meta {
  color: #8b91a7;
  font-size: 12px;
}

.row-actions {
  display: flex;
  gap: 6px;
  align-items: center;
}

.row-actions button {
  padding: 5px 10px;
  font-size: 12px;
}

.move-select {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  color: #aab1c5;
  padding: 5px 8px;
  font-size: 12px;
}

.move-select.placeholder {
  color: #8b91a7;
}

.rename-form {
  flex: 1;
  display: flex;
  gap: 8px;
}

.rename-form input {
  flex: 1;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 8px 12px;
  color: inherit;
  font-size: 15px;
}

.feedback {
  margin: 14px 0 0;
  color: #8b91a7;
  font-size: 14px;
}

.error {
  color: #ff8f8f;
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
  font-size: 16px;
}

.section-count {
  color: #6f7690;
  font-size: 13px;
}

.section-head button {
  margin-left: auto;
  padding: 5px 10px;
  font-size: 12px;
}

.character-wall {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 10px;
}

.character-card {
  display: grid;
  gap: 6px;
  text-align: left;
  width: 100%;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  padding: 10px;
  color: inherit;
  cursor: pointer;
}

.character-card:hover {
  border-color: rgba(122, 162, 247, 0.5);
}

.character-cover {
  display: block;
  width: 100%;
  aspect-ratio: 1 / 1;
  border-radius: 10px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.05);
}

.character-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-fallback.small {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  font-size: 32px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.82);
  background: linear-gradient(135deg, #243b8f 0%, #57348a 55%, #8a2f5c 100%);
}

.character-name {
  font-size: 14px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.character-alias {
  font-style: normal;
  font-weight: 400;
  color: #8b91a7;
  margin-left: 6px;
  font-size: 12px;
}

.character-meta {
  color: #6f7690;
  font-size: 12px;
}

/* 新建角色对话框的文本域与详情抽屉共用 pre-wrap 纯文本口径。 */
.create-form textarea {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 10px 12px;
  color: inherit;
  font-size: 14px;
  resize: vertical;
  white-space: pre-wrap;
}

.asset-section h2 {
  margin: 0 0 10px;
  font-size: 16px;
}

.asset-holder {
  position: relative;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 14px;
  overflow: hidden;
  height: 420px;
}

.asset-holder :deep(.asset-panel) {
  position: absolute;
  border-left: none;
  background: rgba(13, 21, 36, 0.6);
}

.dialog-mask {
  position: fixed;
  inset: 0;
  background: rgba(4, 8, 18, 0.7);
  display: grid;
  place-items: center;
  z-index: 20;
}

.dialog {
  width: min(520px, calc(100vw - 48px));
  background: #101a2e;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 14px;
  padding: 20px;
}

.dialog h3 {
  margin: 0 0 14px;
  font-size: 18px;
}

.type-pick {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
  margin-bottom: 14px;
}

.create-form {
  display: grid;
  gap: 10px;
}

.create-form input {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 10px 12px;
  color: inherit;
  font-size: 15px;
}

.field {
  display: grid;
  gap: 6px;
  font-size: 13px;
  color: #aab1c5;
}

.form-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
</style>
