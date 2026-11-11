# 03 角色库与消费链全景调查

Label: wayfinder:research
Status: closed(2026-11-11,研究子代理交付,纪要落 `research/character-library-survey` 分支)
Assignee: 本图会话(子代理已交付)

## Question

A 域(Lore 实体库)的自我宣称是「角色库已验证模式的横向复制」——复制前先把原版摸清。产出三份事实:

1. **表结构**:characters/变体(51 号票)、character_section 物化契约(52 号票)、设定图五类型锚定(70 号票)的双方言表结构与列语义。
2. **消费链全景**:52 号票消费面(聊天注入/cut 角色)、70 号票 cut 角色注入、71 号票元素参考的注入路径与拼装点(前端/服务端各在哪)。
3. **可复用性评估**:Lore 实体(地点/组织/物种/物品)复用这套模式(表结构/物化/锚定/注入)哪些成立、哪些不成立,列差异点清单,不拍板(拍板是 04 号票的事)。

产物:`research/character-library-survey.md`(落 `research/character-library-survey` 分支),04 号票 grilling 时必读。

## Resolution(2026-11-11)

纪要:`.scratch/ip-capabilities/research/character-library-survey.md` @ `research/character-library-survey` 分支(commit `5c2ea48`,基于 main,已推 origin)。阅读方式:`git show research/character-library-survey:.scratch/ip-capabilities/research/character-library-survey.md`。

给 04 号票的三条硬事实:

1. **横向复制已有既成先例,Lore 不是第一次**:65 号票 scenes/props 与 characters 三表同构但砍掉变体层与 voice_id(`internal/scene/store_mysql.go:25-46`)——列结构、目录树 API、封面投影降一维、级联删除、「素材不可用」占位全套平移成功。Lore 存储面最小复制面 = 照 scene 包改 label 白名单与主资产集合。
2. **注入链三层截断口径互不通用**:聊天注入不截 detail(实际上限 = 固化段 50 万 runes 整单拒绝,`internal/promptgen/handler.go:47`)、生图拼装 2000 runes 硬截(`internal/canvastask/iptask.go:56`)、风格字段 500 字(`internal/canvas/ip.go:26`);52 号票物化契约(段渲染 + 固化段随 Agent 节点 data 持久、服务端逐字复用)只有 `characterSection` 渲染函数是角色特定的——Lore 换芯即可复用。
3. **「已验证」只覆盖存储面与生图锚,消费面实体版整体缺位**:引用节点(CharacterNode)、连线矩阵源端(`connection.ts:51` `character: ['image','video']`)、cut 出场标注 `character_ids`、Agent 聊天 chip 四件入口是角色独有;scene/prop 只有目录 + AssetPicker、无注入链(65 号票 Out of Scope)。「横向复制」对消费面只部分成立——印证本图 05 号票(Lore 消费面)必须独立成票,不能搭 04 的车。
