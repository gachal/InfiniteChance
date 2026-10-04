import { initPreferences, updatePreferences } from '@vben/preferences';
import { unmountGlobalLoading } from '@vben/utils';

import { overridesPreferences } from './preferences';

/**
 * 应用初始化完成之后再进行页面加载渲染
 */
async function initApplication() {
  // name用于指定项目唯一标识
  // 用于区分不同项目的偏好设置以及存储数据的key前缀以及其他一些需要隔离的数据
  const env = import.meta.env.PROD ? 'prod' : 'dev';
  const appVersion = import.meta.env.VITE_APP_VERSION;
  const namespace = `${import.meta.env.VITE_APP_NAMESPACE}-${appVersion}-${env}`;

  // app偏好设置初始化
  await initPreferences({
    namespace,
    overrides: overridesPreferences,
  });

  // vben 偏好缓存整体优先于代码覆写:老缓存里快照过 vben 默认的 unpkg logo,
  // 会盖掉 logo 覆写(症状:浅色模式左上角回退旧图,暗色模式因 sourceDark 是
  // 新字段不受污染而正常)。初始化后重放一次 logo 覆写,既修正本次会话也把
  // 缓存治愈;logo 随代码发行,不视作用户可改项。
  if (overridesPreferences.logo) {
    updatePreferences({ logo: overridesPreferences.logo });
  }

  // 启动应用并挂载
  // vue应用主要逻辑及视图
  const { bootstrap } = await import('./bootstrap');
  await bootstrap(namespace);

  // 移除并销毁loading
  unmountGlobalLoading();
}

initApplication();
