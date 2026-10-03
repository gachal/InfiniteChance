import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

/**
 * 画布管理组(35 号票):素材与技能挂在 canvas/server 的管理面。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:shapes',
      title: $t('page.canvas.title'),
    },
    name: 'Canvas',
    path: '/canvas-admin',
    children: [
      {
        name: 'Skills',
        path: '/skills',
        component: () => import('#/views/SkillsView.vue'),
        meta: {
          icon: 'lucide:wand-sparkles',
          title: $t('page.canvas.skills'),
        },
      },
      {
        name: 'Assets',
        path: '/assets',
        component: () => import('#/views/AssetsView.vue'),
        meta: {
          icon: 'lucide:images',
          title: $t('page.canvas.assets'),
        },
      },
    ],
  },
];

export default routes;
