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
  `${BOUNDARY_PREAMBLE}出场角色 → 生成注入已落地(cut 有画面时画面作首帧、不注入设定图);cut 的场景/道具出场字段与注入、「先出同框关键帧 → 图生视频」一键编排另票;`,
  'cut 批量生图/生视频(「全部生图/全部生视频」)为三期;台词 → TTS(吃角色 voice_id)为三期;自动出角色设定图为三期;',
  '真渠道 tools 透传实测留部署后点验(httptest 全链与网关透传复核已做)。',
]

/** 键位速查(与 CanvasEditorView/左下提示行同一口径,38 号票纪律)。 */
const GLOBAL_KEYS: HelpKeyEntry[] = [
  { keys: '?', label: '打开/关闭本帮助' },
  { keys: 'Esc', label: '逐层退出(栈顶先收:灯箱 → 右键菜单 → 帮助 → 面板 → 编辑层)' },
  { keys: 'Enter', label: '生成对话框内直接发送' },
  { keys: 'Shift+Enter', label: '对话框内换行' },
  { keys: 'F', label: '画布回全景' },
  { keys: '⌘Z', label: '撤销(Shift+Z 或 ⌘Y 重做)' },
  { keys: '⌘G', label: '框选节点成组(Ctrl+G 同)' },
  { keys: '⌘Shift+G', label: '解组选中的分组(Ctrl+Shift+G 同)' },
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
        { kind: 'heading', text: '右键菜单:添加与入口' },
        {
          kind: 'list',
          items: [
            '右键画布空白处弹出分区菜单,悬停分区名立即展开二级条目(点击分区名本身无动作):创作(Agent / 图片 / 视频 / 预演台 / 角色 / 音色 / 配乐 / 音效)、规划(分镜 / 分镜稿 / 关卡 / 剧本)、组织(分组)、面板(素材库 / 生成记录 / 上传 / 帮助)。',
            '右键位置即新节点落点:节点落在右键点上(连加自动错开防重叠),成为唯一选中,落点中心在已有分组框内即自动入组;素材库/生成记录插入与上传仍落视口中心。',
            '节点与连线上右键不弹任何菜单(仅抑制浏览器默认菜单);节点内的输入框/文本域中右键保留原生菜单(复制粘贴)。',
            '点菜单外任意处、Esc(后开先关)、画布平移或缩放,菜单即关;移出菜单与展开条目的整体范围,条目即收起;菜单与展开条目贴近窗口边缘时自动向内翻转(贴底向上弹、条目贴右缘翻左侧),不会溢出视口。',
          ],
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
        { kind: 'heading', text: '元素:资产库实体出场' },
        {
          kind: 'list',
          items: [
            '参考条「库」里从角色/场景/道具视图挑的图 = 元素 chip:带类型小标与名称,一元素一图 —— 再挑同一实体的另一张即替换,同一张再点取消;素材视图或本机上传的散图仍是无名参考图。',
            '视频模式里元素恒为参考图(角色下拉禁用);同一角色多个造型同时出场时名称带「·造型名」;名称按 IP 资产库实时取,实体或造型被删即退化为无名参考图。',
            '发送时在提示词前部(风格段之后)自动附一行「出场元素:小蓝@图片1(角色)、天台@图片2(场景)」,输入框下方预览、不改写输入框;@ 浮层里选元素插纯名字。',
            '视频模式两条只提示不拦发送的提醒:角色元素超过 4 个(稳定性下降);首帧/尾帧与参考图/视频/音频同在(Seedance、MiniMax H3 会拒)。',
          ],
        },
        { kind: 'heading', text: '玩法预设(视频模式)' },
        {
          kind: 'p',
          text: '提示词框下方「玩法」下拉:单角色登场 / 单角色造型展示 / 双角色同框互动 / 双角色对决。选中即按参考条元素顺序填名(第 1、2 个角色与第 1 个场景;缺角色留「〔角色1〕」占位,缺场景省略那半句)写进提示词,提示词非空先确认替换;只写提示词,不动时长/比例/分辨率/模型。',
        },
        { kind: 'heading', text: '角色引用卡:生成登场视频' },
        {
          kind: 'p',
          text: '角色卡「生成登场视频」(与「生成设定图」并列,对当前造型发起)= 落空视频节点 + 角色→视频连线 + 生成对话框预填:参考条换成该角色元素(造型最新视图类 > 任意设定图 > 封面),提示词为「单角色登场」玩法填名、IP 风格段在最前;该造型还没有图时提示先生成设定图。人工确认后才发送。',
        },
        { kind: 'heading', text: '音频节点(配乐 / 音效)' },
        {
          kind: 'p',
          text: '右键菜单「创作」分区的「配乐」「音效」都落音频节点(音效带 source: sfx 意图标记);创作在节点内的表单里人工填写并提交,不进生成对话框。',
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
        { kind: 'heading', text: '画布分组(72 号票)' },
        {
          kind: 'list',
          items: [
            '框选若干节点按 ⌘G / Ctrl+G 成组:组框自动包住所选节点,已在别的组里的一律换到新组;右键菜单「组织 → 分组」进入画框模式,拖一个矩形落一个空组(Esc 取消)。',
            '归属看成员不看框:节点拖入框内松手即加入、包围盒中心拖出即脱离、跨组拖动即换组;拖动中目标组框实时高亮,松手后框一次性扩张或回落。',
            '点组框空白或标题栏拖动 = 整组移动;直通生成、Agent 产物、素材插入、右键菜单添加的落点中心在组框内即自动入组。',
            '标题栏双击改名;选中组浮出色板与「解组」;四角手柄只改组的用户尺寸,框实际尺寸 = max(用户尺寸, 成员包围盒 + 内边距),成员移出/删除后回落,空组保留。',
            '组不参与框选与多选、不作连线端点、不吸纳组、不嵌套、不影响生成与计费;删组 = 解组,成员原地保留(Backspace 或「解组」钮);成组/解组 ⌘Z 一步回滚。',
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
          text: 'cut 卡「生成视频」= 落空视频节点 + 絵コンテ→视频新边 + 生成对话框预填:cut 有画面 = 画面作首帧,出场角色设定图不注入(首帧与参考不能混用);无画面 = 出场角色设定图以元素注入参考条;prompt 拼画面/台词/运镜/景别,时长预填 cut 秒数;人工确认后才发送。秒数超出所选模型区间时原样预填不夹紧,发送时由时长解析拦下人工改 —— 不静默改分镜意图。',
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
