# 36 管理页 antd 重写 + 模型价格页

Type: grilling
Status: 已实现(2026-10-03;八页 antd 重写 + 价格页补缺,vue-tsc/eslint/make test 全绿,dev 点验八页并经 /v1 证实计价门按 UI 写入价格预扣与退款)

## Question

35 号票落地后,八个业务页是自绘样式嵌 vben 布局的过渡态;且 `/admin/prices` 有 API 无 UI,模型配价一直走 curl(管理台已知缺口)。收尾怎么定?

## Answer

1. **八页按 antd 重写**:仪表盘/渠道/API Key/用量审计/存储设置/技能/素材管理改用 Ant Design Vue(Table/Form/Modal/Select 等)重写,交互行为对齐现状——渠道一键连通测试、Key 创建/吊销/配额充值(微美元整数不变量不碰)、用量筛选、存储驱动切换、技能 CRUD、素材管理;过渡态自绘 CSS 清除。
2. **新增模型价格页(补缺口)**:列表 + 配价编辑,覆盖双轨计价(token 轨/次轨)字段;「未配价一律 `model_not_priced` 拒绝」的服务端语义不变,本票只补 UI。
3. **旧壳退役删除**:LoginView、AuthLayout、AdminShell、style.css / admin-ui.css / auth-form.css 等过渡遗留清干净,防止「看起来还在用」。
4. **共享包不动**:`@infinitechance/api` 原样;`packages/ui` 的 HealthCard 保留服务 canvas/web,admin-web 侧仪表盘展示改用 antd 呈现。
5. **验证点**:`make test` / `make lint` / `make build`;dev 实际点验全页 + 价格页实际写入后经 `/v1` 发请求验证计价生效(用例对 docs/api.md curl 段)。
