import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

/**
 * 网关管理组(35 号票 frontend 菜单模式写死):对应原 admin-web 平铺路由,
 * 子路由用绝对路径保持 URL 不变,父级无组件 = 菜单分组不占 router-view 层。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:server',
      order: -1,
      title: $t('page.gateway.title'),
    },
    name: 'Gateway',
    path: '/gateway',
    children: [
      {
        name: 'Dashboard',
        path: '/dashboard',
        component: () => import('#/views/DashboardView.vue'),
        meta: {
          affixTab: true,
          icon: 'lucide:layout-dashboard',
          title: $t('page.dashboard.title'),
        },
      },
      {
        name: 'Channels',
        path: '/channels',
        component: () => import('#/views/ChannelsView.vue'),
        meta: {
          icon: 'lucide:arrow-left-right',
          title: $t('page.gateway.channels'),
        },
      },
      {
        name: 'Keys',
        path: '/keys',
        component: () => import('#/views/KeysView.vue'),
        meta: {
          icon: 'lucide:key-round',
          title: $t('page.gateway.keys'),
        },
      },
      {
        name: 'Prices',
        path: '/prices',
        component: () => import('#/views/PricesView.vue'),
        meta: {
          icon: 'lucide:tags',
          title: $t('page.gateway.prices'),
        },
      },
      {
        name: 'Usage',
        path: '/usage',
        component: () => import('#/views/UsageView.vue'),
        meta: {
          icon: 'lucide:scroll-text',
          title: $t('page.gateway.usage'),
        },
      },
      {
        name: 'Storage',
        path: '/storage',
        component: () => import('#/views/StorageView.vue'),
        meta: {
          icon: 'lucide:database',
          title: $t('page.gateway.storage'),
        },
      },
    ],
  },
];

export default routes;
