import { defineOverridesPreferences } from '@vben/preferences';

/**
 * 项目配置:只覆盖与默认值不同的部分。
 * vben 默认值已符合本仓形态:frontend 菜单模式、zh-CN、登录过期跳登录页、
 * 不启用 refresh token(后端 /auth 无刷新端点)。
 * 更改配置后请清空浏览器缓存,否则可能读到旧偏好。
 */
export const overridesPreferences = defineOverridesPreferences({
  app: {
    name: import.meta.env.VITE_APP_TITLE,
  },
  // 品牌 logo:替换 vben 默认的 unpkg 远程图;登录页左上角、登录后导航栏等所有展示位
  // 共用此配置(public/logo.png,256px 见方足够 42px 展示位与高分屏)。
  logo: {
    source: `${import.meta.env.BASE_URL}logo.png`,
    sourceDark: `${import.meta.env.BASE_URL}logo.png`,
  },
  // 上游版权条是 vben 官网备案信息,自用管理台不展示(36 号票换皮时再定制)。
  copyright: {
    companySiteLink: '',
    companyName: '',
    date: '',
    enable: false,
    icpLink: '',
  },
  // 单管理员中文单语:隐藏语言切换按钮(35 号票)。
  widget: {
    languageToggle: false,
  },
});
