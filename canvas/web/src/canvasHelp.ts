/**
 * 画布帮助浮层(68 号票)的零依赖内容模块:帮助 = 章节数组 → 块数组,
 * 全部文案住在这里,渲染组件(CanvasHelpModal.vue)只 import 类型与常量、
 * 不含任何帮助文案字面量 —— 改文案/加章只动本文件(票·验证点的结构性
 * 要求:流程未定型,后续修改零组件 diff)。
 *
 * 块五型:heading 小节标题 / p 段落 / list 要点列表 / keys 键位表 /
 * note 边界注记。每章末固定「边界与后续」小节(canvasHelp.test.ts 按
 * 结构规范守住)。文案用词严格按 CONTEXT.md(剧本节点/絵コンテ/画面槽/
 * 生成对话框/直通/角色对表);画面槽按 FrameSlot 现在的四入口口径
 * (素材库挑选/本机上传/落图节点/直通生成,93a94b2)。实现现状注记
 * 逐条对账出处见 68 号票与 /tmp/ic-68-research/help-modal-notes.md 第 6 节。
 */

/** 一条键位:keys 是按键写法,label 是它的作用。 */
export interface HelpKeyEntry {
  keys: string
  label: string
}

/** 帮助块五型(票·定案 3):渲染组件按 kind 分支,不感知具体文案。 */
export type HelpBlock =
  | { kind: 'heading'; text: string }
  | { kind: 'p'; text: string }
  | { kind: 'list'; items: string[] }
  | { kind: 'keys'; entries: HelpKeyEntry[] }
  | { kind: 'note'; text: string }

/** 一章:帮助浮层里的一个可滚动小节,末尾固定「边界与后续」。 */
export interface HelpChapter {
  id: string
  title: string
  blocks: HelpBlock[]
}

/** 章末固定小节的标题(内容文件内部复用;测试按它守住收尾纪律)。 */
export const BOUNDARY_HEADING = '边界与后续'

/** 「画布级键位」小节的标题(章尾公共段第一块,测试按它守组装纪律)。 */
export const KEYS_HEADING = '画布级键位'

/** 边界注记共用一行开头的口径:帮助是现状备忘,不是承诺清单。 */
const BOUNDARY_PREAMBLE = '流程未定型,以下为当前实现边界(防预期差):'

/** 68 号票定案 5 的四条边界,两章共用(原文措辞出处见笔记 6c 节)。 */
const BOUNDARY_NOTES: string[] = [
  `${BOUNDARY_PREAMBLE}出场角色 → 生成注入(生图自动带角色设定图参考;场景/道具引用同)为二期;`,
  'cut 批量生图/生视频(「全部生图/全部生视频」)为三期;台词 → TTS(吃角色 voice_id)为三期;自动出角色设定图为三期;',
  '真渠道 tools 透传实测留部署后点验(httptest 全链与网关透传复核已做)。',
]

/** 键位速查(与 CanvasEditorView/工具栏提示行同一口径,38 号票纪律)。 */
const GLOBAL_KEYS: HelpKeyEntry[] = [
  { keys: '?', label: '打开/关闭本帮助' },
  { keys: 'Esc', label: '逐层退出(栈顶先收:灯箱 → 帮助 → 面板 → 编辑层)' },
  { keys: 'Enter', label: '生成对话框内直接发送' },
  { keys: 'Shift+Enter', label: '对话框内换行' },
  { keys: 'F', label: '画布回全景' },
  { keys: '⌘Z', label: '撤销(Shift+Z 或 ⌘Y 重做)' },
]

/** 章尾公共段之一:画布级键位小节(纯函数,加章零复制,68 号票评审)。 */
export function keysSection(): HelpBlock[] {
  return [
    { kind: 'heading', text: KEYS_HEADING },
    { kind: 'keys', entries: GLOBAL_KEYS },
  ]
}

/** 章尾公共段之二:「边界与后续」注记小节(定案 5 四条,纯函数,加章零复制)。 */
export function boundarySection(): HelpBlock[] {
  return [
    { kind: 'heading', text: BOUNDARY_HEADING },
    ...BOUNDARY_NOTES.map((text): HelpBlock => ({ kind: 'note', text })),
  ]
}

export const CANVAS_HELP_DOC = {
  title: '画布帮助',
  chapters: [
    {
      id: 'nodes',
      title: '① 节点与运行总览',
      blocks: [
        {
          kind: 'p',
          text: '创作类节点自带各自的运行方式;规划类节点是被 Agent 工具读写或人工编辑的载体。每类节点「怎么往下走」如下。',
        },
        { kind: 'heading', text: '图片 / 视频节点:生成对话框' },
        {
          kind: 'p',
          text: '选中图片或视频节点时,生成对话框贴节点正下方出现(未选中不出现;Agent/分析节点不唤起)。要画什么写进对话框,人工确认后才发送。',
        },
        {
          kind: 'keys',
          entries: [
            { keys: 'Enter', label: '对话框内直接发送' },
            { keys: 'Shift+Enter', label: '对话框内换行' },
          ],
        },
        { kind: 'heading', text: '音频节点(配乐 / 音效)' },
        {
          kind: 'p',
          text: '「+ 配乐」「+ 音效」都落音频节点(音效带 source: sfx 意图标记);创作在节点内的表单里人工填写并提交,不进生成对话框。',
        },
        { kind: 'heading', text: 'Agent 节点:对话驱动工具循环' },
        {
          kind: 'list',
          items: [
            '输入即主题/修改意见,回车提交;继续输入即修改意见,换技能开新会话。',
            'Agent 边对话边调用工具(创建角色、读取资产库、保存剧本…),过程以 chip 显示,如「正在创建角色…」。',
            '花钱的操作(生图、生视频)永不自动进循环 —— 一律由生成对话框人工发送。',
            '循环跑在画布服务端,浏览器断线即中断,重新发送可续。',
          ],
        },
        { kind: 'heading', text: '剧本节点:被动载体,不连线' },
        {
          kind: 'p',
          text: '剧本节点不作任何连线的源与目标(不渲染把手),内容经 Agent 工具读写;点击整卡进全屏编辑层编辑。一个类型三种用途:分集大纲 / 每集剧本 / 原文(长篇小说,上限 30 万字,经 read_source 分段消费)。',
        },
        { kind: 'heading', text: '絵コンテ:cut 编辑层' },
        {
          kind: 'p',
          text: '絵コンテ节点点击整卡进 cut 编辑层。每条 cut = 序号 + 画面槽 + 时长秒(可空)+ 台词/音效 + 景别/运镜 chip + 出场角色标注 + 备注;底部「+ cut」添加条目;Esc 与「返回」双路等价关闭。',
        },
        { kind: 'heading', text: '画面槽:四个配画面入口' },
        {
          kind: 'list',
          items: [
            '素材库挑选:从本 IP 素材里挑一张;挑选即设置,再点取消(无专用「设为分镜画面」动作)。',
            '本机上传:图片入素材库(挂画布 IP)并填进本槽。',
            '落图节点:把本画面落成画布上的图片节点并连线。',
            '直通生成:落空图片节点并打开生成对话框预填(画面描述/景别),人工确认发送;任务成功后产物素材自动回填本槽 —— 生成即配画。',
          ],
        },
        ...keysSection(),
        ...boundarySection(),
      ],
    },
    {
      id: 'pipeline',
      title: '② 剧本流水线全链',
      blocks: [
        {
          kind: 'p',
          text: '集组织:短剧 = 一个 IP;大纲一张画布,每集一张画布(EP01…);跨集批量与剧集目录另票。全链由 Agent 节点的对话驱动,下列工具是它唯一的写入口。',
        },
        { kind: 'heading', text: '第 1 步 角色对表(list_assets)' },
        {
          kind: 'p',
          text: '动笔之前 Agent 必须先「读取资产库」对本 IP 库存对表:命中复用已有角色/场景/道具、禁止重建;拿不准(笔名/外号/译名差)问用户;未命中才新建 —— 角色对表纪律,强制。一致性红利经 cut 的 character_ids 引用兑现。',
        },
        { kind: 'heading', text: '第 2 步 先选后展' },
        {
          kind: 'p',
          text: 'Agent 先给 2-3 个差异化的故事方案(各一句话)供选择,选定后再展开;这一步在对话内完成,不建专门界面。',
        },
        { kind: 'heading', text: '第 3 步 大纲与逐集剧本(save_outline / save_script)' },
        {
          kind: 'p',
          text: '「保存大纲」落大纲画布的剧本节点;「保存剧本」落每集画布的剧本节点(一个类型三种用途:分集大纲 / 每集剧本 / 原文)。',
        },
        { kind: 'heading', text: '第 4 步 分镜落图(save_storyboard)' },
        {
          kind: 'p',
          text: '「保存分镜板」落絵コンテ节点;cuts 的画面不用 Agent 填,空画面落「未配画面」占位,由你后续配置。',
        },
        { kind: 'heading', text: '第 5 步 cut 配画面' },
        {
          kind: 'p',
          text: '用画面槽的四个入口:素材库挑选 / 本机上传 / 落图节点 / 直通生成(详见「① 节点与运行总览」);直通生成的产物成功后自动回填来源 cut。',
        },
        { kind: 'heading', text: '第 6 步 cut → 视频(生成视频)' },
        {
          kind: 'p',
          text: 'cut 卡「生成视频」= 落空视频节点 + 絵コンテ→视频新边 + 生成对话框预填:画面素材自动作首帧,prompt 拼画面/台词/运镜/景别,时长预填 cut 秒数;人工确认后才发送。秒数超出所选模型区间时原样预填不夹紧,发送时由时长解析拦下人工改 —— 不静默改分镜意图。',
        },
        { kind: 'heading', text: 'Agent 工具清单(前端标签)' },
        {
          kind: 'list',
          items: [
            'create_character 创建角色 / create_scene 创建场景 / create_prop 创建道具',
            'list_assets 读取资产库 / read_source 读取原文',
            'save_outline 保存大纲 / save_script 保存剧本 / save_storyboard 保存分镜板',
          ],
        },
        ...keysSection(),
        ...boundarySection(),
      ],
    },
  ],
} satisfies { title: string; chapters: HelpChapter[] }

/** 「?」(Shift+/)开关帮助的键判定:带 ctrl/meta/alt 不触发
 * (38 号票:修饰键组合不劫持,MediaLightbox 截帧 S 同款纪律)。
 * 行为是开关式 toggle:帮助未开且灯箱/全屏编辑层不在场才开,开着即收
 * (开路径守卫与栈序见 CanvasEditorView.onGlobalKeydown,68 号票评审)。 */
export function isHelpToggleKey(
  e: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey' | 'altKey'>,
): boolean {
  return e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey
}
