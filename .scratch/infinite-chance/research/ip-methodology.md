# 业界 IP 打造方法论研究纪要(2026-10-24)

50 号票(IP 企划分组层)与 51-55 号票规划的方法论依据,由 grilling 会话的网络研究产出。
问题:为一款 AI 无限画布工具重塑为「ACG IP 打造平台」,业界顶级 IP 工厂如何组织创作、顶层信息架构长什么样、一致性机制怎么做。

---

## 一、顶级 IP 工厂 IP 开发期的共同核心工件

### 1.1 共同工件清单(跨工厂收敛出的 7 类)

| 工件 | 定义 | 典型字段/结构 | 在流程中的位置 |
| --- | --- | --- | --- |
| **故事圣经 Story Bible / 剧集圣经 Show Bible** | 捕捉作品全部核心叙事元素的参考文档,同时是**销售文档**(pitch)与**连续性工具**(writers' room) | logline/premise → 概要 synopsis → 世界与设定 → 基调 tone → **叙事引擎**(每周冲突的生成公式)→ 角色传记与关系 → 季纲/试播集概要 → 分集点子库 | 企划期产出;拍板后成为写作团队的第一参照 |
| **世界观设定集 Lore Bible** | 跨媒介 franchises 的**正典主文档**:规则、时间线、角色、地理 | 正典事件表、世界规则(魔法/科技)、地图、势力、禁例 | 跨媒介扩张前先建;由专门岗位维护(见下 Lucasfilm Story Group) |
| **角色圣经 Character Bible / 角色表** | 单角色的完整档案。文字侧字段:姓名/别名、故事角色、外貌、性格与声线、**backstory**、want(外部目标) vs need(内部需求)、成长弧线、人物关系;**动画侧追加:三视图模型表、表情集、姿态集、色板、服装变体、设计备注** | 见 [Penpoint 模板](https://penpoint.app/character-bible)、[The Prompt Home(AI 角色圣经)](https://theprompthome.com/ai-character-bible-guide) | 企划书/设定集阶段定型,全周期演进 |
| **美术风格指南 Art Bible / Style Guide** | 「如果项目是生命体,Art Bible 是它的 DNA」——规定一切视觉决策的唯一真相源 | 美术方向陈述、概念草图与视觉参考、色彩规范、角色/场景设计规则、正误对照(do/don't) | **前置**:由美术总监在量产资产开始前建立,是活文档随制作演进 |
| **分集/章节大纲 Outline** | 从圣经展开的节拍级结构 | 幕/章/场,每章 beats,喂给后续分镜/分集 | 圣经→大纲→(分镜/正片)的中间层 |
| **时间线 Timeline** | 正典事件的时序权威 | 事件、日期、跨作品归属 | 连续性仲裁工具(MCU 时间线即内部资产) |
| **角色关系图** | 人物间关系的可视化 | 关系类型、强度、阵营 | 写作与设定双用 |

### 1.2 各工厂的独特点(与工件的关系)

- **迪士尼/皮克斯**:组织工件是 **Braintrust**(同伴评审机制,非文档)——定期拉人只挑故事问题不给处方;创意权威见 [Medium: Lessons from Pixar — The Braintrust](https://medium.com/great-business-stories/lessons-from-pixar-1-the-braintrust-e306843a5153)、[SoHawk 解读](https://shohawk.com/the-pixar-braintrust)。视觉侧以 concept art / color scripts(色彩脚本)前置定调。
- **漫威 MCU / 卢卡斯影业**:Wired 名文确认「**Marvel 原型化了流程,Lucasfilm 在把它工业化**」——Lucasfilm 设 **Story Group**(Pablo Hidalgo、Leland Chee 等专职正典管理员)用内部数据库统一管理全媒介正典;Marvel 内部持有非公开的 story bible 与官方事件时间线。见 [Wired: You Won't Live to See the Final Star Wars Movie](https://www.wired.com/2015/11/building-the-star-wars-universe)、学术侧证 [Curtin University 2024](https://curate.curtin.edu.au)。**启示:IP 工厂的差异化能力不是某部作品,而是「正典维护系统」。**
- **任天堂**: approval-first。1993 年《Official Nintendo Style Guide》细到规定 Yoshi 的 Pantone 色、宣称「Mario 不是人类」([r/modelsheetarchive 存档](https://www.reddit.com/r/modelsheetarchive/comments/12n9eau/nintendo_character_style_manual_1993_part_1));马力欧电影由宫本茂任制作人全程审设计([Miyamoto 解释为何选 Illumination](https://www.reddit.com/r/nintendo/comments/7x06cu/miyamoto_explains_why_they_went_with_illumination))。1993 年烂片后停发影视授权近 30 年——**风格失控一次,代价是 30 年**。
- **宝可梦公司**:向每个被授权方下发**成套官方 style guide**(图形、配色、比例、禁例),授权商品逐件过风格审。见设计师公开的 [Pokémon Style Guide 案例](https://georgenasenior.com/Pokemon-Style-Guide)、[Behance 项目](https://www.behance.net/gallery/22750305/Pokemon-Style-Guide)、官方 [Media Usage Guidelines](https://pokemon.gamespress.com/Media-Usage-Guidelines)。
- **集英社(漫画编辑部)**:流程是「**企画书 ↔ 担当编辑 → 编集部会议(1–3 月一次,全部门合议)→ GO 后做角色设计/大纲 → ネーム(分镜稿)反复修改 → 编集长 OK → 连载决定**」,连载后靠**读者问卷**决定去留。担当编辑是流程 hub。见 [note: 漫画企划书作法](https://note.com/can7t_eat_me/n/n0fa9d8e0ee49)、[新人漫画家咨询室](http://manga-gai.net/mangakasoudan/sodansitu/2007_05_29.html)、[JUMP+ 编辑谈新人课题](https://rookie.shonenjump.com/info/entry/201809_blog)、[集英社招聘官网](https://www.shueisha.co.jp/2026saiyo/sp-01)。
- **吉卜力**:宫本路线——不写完整剧本,先画 **image board(イメージボード,概念画)** 定氛围与美术方向,**分镜(絵コンテ)本身就是剧本+layout+美术指导三合一**,宫崎骏一人画完全部分镜保证连续性([Story Field Notes](https://storyfieldnotes.substack.com/p/how-hayao-miyazaki-builds-a-story)、[前高管 Steve Alpert 谈 5 段式开发](https://www.readtrung.com/p/hayao-miyazaki-9-creative-lessons))。

**结论:六家共同的最小工件集 = 世界观圣经 + 角色圣经(含视觉设定)+ 美术风格指南 + 大纲 + 时间线 + 关系图;差异只在「谁维护、怎么仲裁」。**

---

## 二、主流世界观/叙事管理工具的顶层信息架构

| 工具 | 第一层根节点 | 第二层 | 实体类型 |
| --- | --- | --- | --- |
| **World Anvil** | **World(世界)**——一切内容的容器 | **Categories(分类树)** → **Articles(类型化条目)** | 条目模板 24+ 种:Character、Building、Geography(8 种地理条目)、Settlement、Organization、Natural Law、Language、Military Conflict、Species、Item、Condition、Rank、Tradition 等,每种模板有专属字段提示;另有**专项实体:Timeline、Interactive Map、Family Tree、外交关系图、Secrets(分权限条目)、图片库**;Manuscript(小说写作)与 RPG Campaign Manager 也挂在 world 下。分类树两种主流组织法:按主题(Character/Place/Faction)或按地理(每洲一个子类)。来源:[官方入门教程](https://www.worldanvil.com/learn/beginner-tutorials/get-started-articles)、[官方分类组织指南](https://blog.worldanvil.com/worldanvil/tutorials/pro-tips/how-to-organize-your-articles-categories-on-world-anvil)、[分类树教程](https://www.worldanvil.com/learn/categories/category-tree)、[Codex: Articles & Categories Manager](https://www.worldanvil.com/w/WorldAnvilCodex/a/articles-categories-manager) |
| **Campfire Writing** | **Series(系列)→ Projects(作品)** | **模块制:每种实体一个模块** | 17 个模块:Manuscript(手稿)、Characters(含档案面板)、Relationships(关系网)、Plot、Storylines、Arcs、Timeline、Locations、Maps、Species、Magic、Languages、Items、Cultures、Religions、Calendar 等;**Arcmobs**=跨项目复用的实体组。按模块单独购买。来源:[官方 Sci-Fi 页](https://campfirewriting.com/write/sci-fi)、[SelfPublishing 17 模块评](https://selfpublishing.com/campfire-writing-review)、[Reedsy 评](https://reedsy.com/blog/guide/book-writing-software/campfire-write-review)、[Storyflow 对比](https://storyflow.so/campfire-alternative) |
| **Plottr** | **Series(系列圣经)** → Books(书) | **Timeline 是 hub**,其余为并列库 | 时间线组织 Acts/Chapters/Scenes + plotlines + scene card;Characters、Places、Notes、Tags 四个并列库,均可自定义 category;全部资产集中在系列级「Series Bible」。来源:[Plottr Overview 官方文档](https://docs.plottr.com/article/8-plottr-overview)、[Timeline Overview](https://docs.plottr.com/article/54-timeline-overview)、[官方 Features](https://plottr.com/features) |
| **Obsidian(社区范式)** | **Vault = 一个 IP 宇宙**(无原生根概念,靠约定) | 顶层文件夹 = 实体类型 | 惯例结构:`Characters/`、`Locations/`、`Factions/`、`Lore/`、`Timeline/`、`Sessions/` + `Templates/`;每个实体一张 note,**YAML frontmatter 定义结构化字段**(name/type/role/description),wikilink 互联 + Dataview 聚合成关系表/时间线;World Builder 插件把这套范式产品化。来源:[Vaultorial 模板库](https://vaultorial.com/worldbuilding-vault)、[GitHub 模板](https://github.com/juliandeleonguerrero/obsidian-worldbuilding-template)、[World Builder 插件](https://community.obsidian.md/plugins/world-builder)、[TJ's Vault 实践](https://vault.tjtrewin.com/how-i-use-obsidian) |

**共性结论(可直接抄的 IA)**:`IP/系列(根容器) → ① 类型化实体库(角色/地点/组织/物种/条目) ② 时间线 ③ 地图/空间 ④ 关系图 ⑤ 大纲/剧本产物 ⑥ 资产(图片/设定图)库`——五个工具全部收敛到这个形状,且**时间线与地图永远是"视图"而非"文件夹"**。

---

## 三、漫画/动画工业化管线的阶段与交付物

### 3.1 动画(日本 TV anime 三段式)

**Pre-production(企划·设计)** → **Production(作画)** → **Post-production(合成·声优)**:

| 阶段 | 交付物 |
| --- | --- |
| 企画 Planning | 企划书、系列构成、预算与委员会 |
| 脚本 Scenario | 台本(第 N 话剧本) |
| **设定 Settei** | **角色设定(模型表)、道具/机械设定、美术设定(背景板)、色彩设计(色指定表)**——全片一致性的锚 |
| **絵コンテ Storyboard** | 全话分镜:每 cut 的画面、时长、台词、运镜、效果注记(由「演出」画) |
| レイアウト Layout | 每 cut 的构图定稿:人物配置、背景、镜头角度——原画/背景/3D 的共同上游 |
| 原画 Genga | 关键帧(动作/演技的骨架帧) |
| 動画 Douga | 中割与清稿线画 |
| 背景 Art | 美术背景 |
| **仕上 Paint(上色)** | 按**色指定表**填色、阴影高光 |
| 撮影 Photography | 合成(滤镜、光效、镜头运动) |
| 編集 → アフレコ → ダビング | 剪辑、配音(常与上色并行)、混音 |

来源:[Sakugaart 动画管线](https://www.sakugaart.com/ja/anime-pipeline-production)、[Otsu Labs: Anime Pipeline in a Real Studio](https://www.otsulabs.com/blog/anime-pipeline-in-a-real-studio)、[SHIROBAKO 官方用语集](http://shirobako-anime.com/words.html)、[东映动画官方制作工程](https://corp.toei-anim.co.jp/ja/company/animation_production.html)、[东放学园](https://www.tohogakuen.ac.jp/enta/anime)。

### 3.2 漫画

`企画书 → 编集部会议 → プロット/キャラデザ → ネーム(分镜稿,决定性审查物) → 下書き(铅笔稿) → ペン入れ(描线+贝塔涂黑) → トーン(网点) → 仕上げ(集中线/手写字/完稿)`。ネーム 是编辑与作家反复拉锯的核心工件——「コマ割り+棒人形+台词」定义整部作品的可读性。来源:[小学馆まんが家養成講座](https://shincomi.shogakukan.co.jp/training/002.html)、[常磐庄.net 流程指南](https://www.tokiwa-so.net/news/11661)、[职业漫画家实际工序](https://note.com/tukuyomi_works/n/nfd04f5e3a52c)。

### 3.3 游戏侧

- **GDD(Game Design Document)**:pre-production 产出、随制作演进的活文档,捕获愿景、机制、系统、内容清单,是团队与外包的沟通蓝图。来源:[GitBook: How to write a GDD](https://www.gitbook.com/blog/how-to-write-a-game-design-document)、[Wikipedia: GDD](https://en.wikipedia.org/wiki/Game_design_document)。
- **Art Bible**:视觉 DNA,量产资产前的闸门;与 GDD 平行,由美术总监持有。来源:[Makko.ai: How to Write a Game Art Bible](https://blog.makko.ai/art-bible-game-development)、[Ciszek 3D Production Pipeline 论文](https://jyx.jyu.fi/bitstreams/0b4c8061-b946-4a21-a8fe-74a65f8f8939/download)。

---

## 四、跨媒介一致性管理

1. **角色一致性的物理载体 = 设定(settei)系统**:
   - **设定画/模型表(model sheet)**:正面/侧面/背面(+3/4)的**三视图 turnaround**、表情集、姿态集、服装变体——所有作画者「on-model」的依据([Sakuga Wiki: Settei](https://sakuga.fandom.com/wiki/Settei)、[Art Rocket/Clip Studio: Model Sheets](https://www.clipstudio.net/how-to-draw/archives/164740)、[JoJo 官方模型表存档实例](https://jojowiki.com/Model_Sheet_Archive/Part_4)、[The AK Studio: Character Turnaround](https://www.theakstudio.com/blog/creating-a-character-turnaround))。术语对照:Turnaround=转身图、Color Sample=色指定([动画术语对照表](https://iamqqq2002.pixnet.net/blog/posts/8173949732))。
   - **色指定/色彩设计**:每角色的**官方色板**(发色、肤色、瞳色、线色)+ **场景变体**(昼/夜/夕/逆光各一张指定表),色指定师逐 cut 检查套用([Style.fm 色彩设计专栏](http://www.style.fm/as/05_column/tsujita/tsujita01.shtml)、[Clip Studio: 色指定表做法](https://tips.clip-studio.com/ja-jp/articles/5854)、[玄光社: 色彩设计基础](https://genkosha.pictures/movie/19100328493))。
2. **One source of truth 的三种实现**:
   - **文档型**:授权 style guide 是被授权方的唯一真相(Pokémon 模式,[案例](https://georgenasenior.com/Pokemon-Style-Guide));
   - **组织型**:Lucasfilm Story Group——专职正典管理员 + 内部正典数据库,仲裁所有媒介的设定冲突([Wired](https://www.wired.com/2015/11/building-the-star-wars-universe));
   - **系统型**:DAM/品牌门户——「单一真相源」存资产+规范+使用权限,所有下游从同一处取最新资产([Brandkit: DAM for Franchising](https://brandkit.com/brandkit-dam-for-franchising)、[Aprimo: Brand Portals](https://www.aprimo.com/blog/how-companies-use-brand-portals-to-distribute-digital-assets)、[Brault: 2026 buyer's guide](https://brault.app/blog/brand-management/brand-asset-management-for-franchises))。
3. **IP 资产台账**:业界用 DAM 承担——资产文件 + 风格指南 + 许可范围 + 版本,按对象键/标签检索, franchise 场景下每个被授权方只读拿最新版。可对应本仓库 `internal/asset` + 对象存储三分前缀的既有形态。

---

## 五、2024–2026 AI 辅助 IP/影视创作工具的顶层产品结构

| 工具 | 第一层 | 下挂结构 | 角色一致性做法 |
| --- | --- | --- | --- |
| **LTX Studio** | **Project(=一部片)** | Script → **Scenes → Shots → Frames**;**Cast/Elements 库**(角色/物体/地点作为可复用元素,脚本上传后自动抽取);每 shot 一个 **Storyboard**;Edit 时间线;导出 shot list + storyboard PDF + 成片;**Lorebook** 存放地点/美学/角色图像跨生成保持一致。来源:[LTX 官方博客](https://ltx.io/blog/top-ltx-studio-features)、[官网](https://website.ltx.studio) | 每个元素锚定**参考图(单 shot 最多 5 张角色参考)**+ 元素库跨 shot 复用;LTX-2 强化多 shot 一致性 |
| **Sudowrite** | **Project(=一部小说);Series(系列)可跨书共享实体** | **Story Bible 是一等公民**,固定 7 区:Braindump(自由倾倒)→ Synopsis → Genre → Style → **Characters → Worldbuilding** → Outline → Chapters;草稿时实时显示 AI 正在引用哪些圣经条目。来源:[官方文档 What is Story Bible](https://docs.sudowrite.com/using-sudowrite/1ow1qkGqof9rtcyGnrWUBS/what-is-story-bible/jmWepHcQdJetNrE991fjJC)、[Story Bible Template](https://sudowrite.com/blog/story-bible-template)、[Series Support](https://feedback.sudowrite.com/help/articles/1251175-series-support)、[Bible Detection 更新](https://feedback.sudowrite.com/changelog/mag-story-bible-detection-and-visibility-update) | **文本知识注入**(圣经条目进上下文),无图像锚定 |
| **NovelAI** | Story(故事)为主体;**Lorebook** 为设定库 | Lorebook entry = **激活键(activation keys)+ 正文 + 注入设置**(搜索范围、优先级、插入位置、激活概率、强制激活);Memory 与 Author's Note 是常驻上下文字段。来源:[官方 Lorebook 文档](https://docs.novelai.net/en/text/lorebook)、[Lorebook API](https://docs.novelai.net/en/scripting/lorebook-api)、[官方博客](https://blog.novelai.net/lorebook-generation-custom-context-menu-more-da7f49817ef3) | 同上,按键触发注入,是「Lore Bible 的 API 化」 |
| **MidReal** | 交互式图文小说,一次一个故事 | **持久记忆系统**保持角色/场景/剧情长程一致(核心卖点) | 纯记忆注入。**注意:产品已停运**([回顾文](https://skywork.ai/skypage/en/MidReal-The-Rise-and-Fall-of-a-Next-Gen-AI-Story-Generator/1972919059107344384)、[AnyGen 存档](https://www.anygen.io/showcase/midreal-ai/index.html)、[Product Hunt](https://www.producthunt.com/products/midreal))——单故事交互形态未能撑起产品 |
| **Midjourney**(一致性原语层) | 无项目概念,纯生成原语 | --cref(旧)→ **--oref Omni Reference**(把参考图中的角色/物体放进新图,--ow 调权重)+ --sref 锁风格;新 **Edit Model 支持最多 4 张参考图**。来源:[官方 Omni Reference 文档](https://docs.midjourney.com/hc/en-us/articles/36285124473997-Omni-Reference)、[Edit Model 文档](https://docs.midjourney.com/hc/en-us/articles/48495453462797-Edit-Model) | **参考图锚定**,层级最底层 |
| **Scenario**(游戏资产向) | 资产库/项目 | **为每个角色或风格单独训练模型**(10–20 张参考图 fine-tune 出 Character Model),之后所有生成自动落在该角色/风格上。来源:[官网](https://www.scenario.com)、[官方: Train a Consistent Character Model](https://help.scenario.com/articles/4280773511-train-a-consistent-character-model)、[Lovart 2026 工具综述](https://www.lovart.ai/blog/6-best-ai-character-consistency-tools-2026) | **逐角色微调模型**,一致性最强、成本最高 |

**AI 工具层的共性结论**:① 全部以「项目/IP」为第一层;② 第二层收敛为**实体库(角色/地点/风格)+ 生成产物(场景/镜头/章节)两个大类**;③ 角色一致性三条技术路线:**参考图锚定**(LTX/Midjourney,便宜、弱约束)、**逐实体微调**(Scenario,强约束、重成本)、**文本设定注入**(Sudowrite/NovelAI,只管叙事不管形象)——业界还没有任何一家把「设定集(三视图+色指定)」作为中间件产品化,这正是传统动画工业验证过的第三条路(设定锚)的空档。

---

## 附:对「无限画布 × IP 打造」定位的直接映射

传统 IP 工厂的共同工件(圣经/设定集/风格指南/大纲/时间线/关系图)已经被 World Anvil、Campfire、Plottr 收敛为「**世界容器 → 类型化实体库 + 时间线/地图视图**」的标准 IA;AI 工具(LTX/Sudowrite/Scenario)在这之上补了生成层,但**「设定图(三视图/色指定/表情集)作为角色一致性的锚」这一动画工业验证了 60 年的机制,在现有 AI 工具里全部缺席**。画布作为自由空间恰好是承载设定集、关系图、时间线、分镜板这类「非表单工件」的自然形态——顶层做 IP 容器与实体库,中间层做设定/圣经工件,底层用参考图锚定 + 风格锚做生成一致性,是与两条现有产品线都错位的定位空间。
