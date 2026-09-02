<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'

import { ApiClient, type HealthReport } from '@infinitechance/api'

const REFRESH_MS = 10_000

const props = withDefaults(
  defineProps<{
    title: string
    /** ApiClient base path; the app's dev proxy maps it to a backend. */
    base?: string
    /** Prefix of the unreachable line, e.g. 无法连接网关. */
    errorLabel?: string
  }>(),
  { base: '/api', errorLabel: '无法连接服务' },
)

const client = new ApiClient({ base: props.base })
const report = ref<HealthReport | null>(null)
const error = ref('')

async function refresh(): Promise<void> {
  try {
    report.value = await client.health()
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

let timer: number | undefined
onMounted(() => {
  void refresh()
  timer = window.setInterval(() => void refresh(), REFRESH_MS)
})
onUnmounted(() => window.clearInterval(timer))
</script>

<template>
  <section
    class="card"
    aria-labelledby="health-title"
  >
    <div class="card-head">
      <h2 id="health-title">
        {{ title }}
      </h2>
      <span
        v-if="report"
        class="badge"
        :class="report.status"
      >
        {{ report.status === 'ok' ? '正常' : '降级' }}
      </span>
      <span
        v-else-if="error"
        class="badge degraded"
      >不可达</span>
    </div>

    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ errorLabel }}:{{ error }}
    </p>

    <ul
      v-if="report"
      class="deps"
    >
      <li
        v-for="(check, name) in report.checks"
        :key="name"
        class="dep"
      >
        <span
          class="dot"
          :class="check.status"
          aria-hidden="true"
        />
        <span class="dep-name">{{ name }}</span>
        <span class="dep-status">{{ check.status === 'up' ? '已连接' : '未连接' }}</span>
        <code
          v-if="check.error"
          class="dep-error"
        >{{ check.error }}</code>
      </li>
    </ul>
    <p
      v-else-if="!error"
      class="muted"
    >
      正在获取服务状态…
    </p>
  </section>
</template>

<style scoped>
.card {
  margin-top: 32px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  padding: 20px 22px;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.card-head h2 {
  margin: 0;
  font-size: 17px;
  font-weight: 600;
}

.badge {
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 999px;
  font-weight: 600;
}

.badge.ok {
  background: rgba(52, 199, 123, 0.15);
  color: #4cd787;
}

.badge.degraded {
  background: rgba(255, 159, 10, 0.15);
  color: #ffb340;
}

.deps {
  list-style: none;
  margin: 18px 0 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.dep {
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 10px;
  padding: 10px 14px;
  font-size: 14px;
}

.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex: none;
}

.dot.up {
  background: #4cd787;
  box-shadow: 0 0 8px rgba(76, 215, 135, 0.7);
}

.dot.down {
  background: #ff6b6b;
  box-shadow: 0 0 8px rgba(255, 107, 107, 0.7);
}

.dep-name {
  font-weight: 600;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: 1px;
}

.dep-status {
  color: #8b91a7;
}

.dep-error {
  margin-left: auto;
  color: #ff8f8f;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 50%;
}

.error {
  color: #ff8f8f;
  font-size: 14px;
}

.muted {
  color: #8b91a7;
  font-size: 14px;
}
</style>
