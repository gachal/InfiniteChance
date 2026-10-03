<script setup lang="ts">
// 仪表盘:两个服务的健康状态,antd 呈现(36 号票;HealthCard 留服务 canvas/web)。
// 轮询节奏对齐原 HealthCard(10s);/healthz 依赖不可达时后端以 503 带
// 完整报告作答,ApiClient.health() 对 503 正常解析,只有网络错误才算不可达。
import { onMounted, onUnmounted, reactive } from 'vue';

import { Page } from '@vben/common-ui';

import { Alert, Badge, Card, Tag } from 'ant-design-vue';

import type { HealthReport } from '@infinitechance/api';

import { authErrorMessage, useAuth } from '../auth';

const REFRESH_MS = 10_000;

interface HealthPanel {
  error: string;
  errorLabel: string;
  report: null | HealthReport;
}

const panels = reactive<Record<'canvas' | 'gateway', HealthPanel>>({
  canvas: { error: '', errorLabel: '无法连接画布服务', report: null },
  gateway: { error: '', errorLabel: '无法连接网关服务', report: null },
});

const { canvasClient, client } = useAuth();

async function poll(kind: 'canvas' | 'gateway'): Promise<void> {
  const fetcher = kind === 'canvas' ? canvasClient : client;
  try {
    panels[kind].report = await fetcher.health();
    panels[kind].error = '';
  } catch (e) {
    panels[kind].error = authErrorMessage(e);
  }
}

let timer: undefined | number;
onMounted(() => {
  void poll('gateway');
  void poll('canvas');
  timer = window.setInterval(() => {
    void poll('gateway');
    void poll('canvas');
  }, REFRESH_MS);
});
onUnmounted(() => window.clearInterval(timer));
</script>

<template>
  <Page title="仪表盘">
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <Card
        v-for="(panel, kind) in panels"
        :key="kind"
        :title="kind === 'gateway' ? '网关服务健康' : '画布服务健康'"
      >
        <template #extra>
          <Badge
            v-if="panel.report"
            :status="panel.report.status === 'ok' ? 'success' : 'warning'"
            :text="panel.report.status === 'ok' ? '正常' : '降级'"
          />
          <Badge
            v-else-if="panel.error"
            status="error"
            text="不可达"
          />
        </template>

        <Alert
          v-if="panel.error"
          type="error"
          show-icon
          :message="`${panel.errorLabel}:${panel.error}`"
        />

        <template v-if="panel.report">
          <div
            v-for="(check, name) in panel.report.checks"
            :key="name"
            class="mb-2 flex flex-wrap items-center gap-2 last:mb-0"
          >
            <Tag
              :color="check.status === 'up' ? 'success' : 'error'"
              class="mr-0"
            >
              {{ check.status === 'up' ? '已连接' : '未连接' }}
            </Tag>
            <span class="font-medium">{{ name }}</span>
            <code
              v-if="check.error"
              class="text-xs break-all text-neutral-500"
            >
              {{ check.error }}
            </code>
          </div>
        </template>
        <p
          v-else-if="!panel.error"
          class="m-0 text-neutral-500"
        >
          正在获取服务状态…
        </p>
      </Card>
    </div>
  </Page>
</template>
