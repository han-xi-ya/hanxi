# Hanxi 文档地图（Document Map）

> **文档定位**：全仓入库 `.md` 的**唯一索引**——每个文件是什么、多大、何时末改、初判处于什么状态，以及链接体检结果与版图层面问题清单。本文件只做地图与体检，**不裁决任何文档的去留**；归并与归档动作一律交各所有权线与机主裁决。
> **测量口径**：文件清单取 `git ls-files '*.md'`（**2026-10-26 19:4x 现取 68 份**：含本批入库的 `docs/reviews/2026-10-26-backend-code-review.md` 与前一轮入库的 `docs/FRONTEND_QUALITY_REVIEW_2026-10-26.md`，以及 `docs/archive/KEY-NUGGETS.md`）；末改日期取该文件最近一次提交的作者日期；行数取**当前工作区** `wc -l`，**含并行会话在途未提交编辑**（BACKLOG/PROGRESS 等 2026-10-21 刷新行即此口径，提交后数值可能再变）。基线为 `dev` @ `2c3fa57`（2026-10-21 12:25）；2026-10-26 仅刷新本行份数、`docs/TROUBLESHOOTING.md` 行数并补两行审查报告，其余行未复核、可能仍滞后。**注意：当前有多路并行会话正在分片改写各文档，本表是快照，行号与日期可能随时漂移。**
> **状态图例**：`活` = 活文档（描述/约束现状，需随代码演进）；`快照` = 历史/时点存档（自述或明显不再更新，旧值是证据不是错误）；`疑过期` = 状态栏或正文与仓内他方记录存在矛盾/过期迹象，**待所有权线核对**。状态栏全部为**初判，以各战线归档判定为准**。

---

## 1. 全量清单（按目录树）

### 仓库根（2）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `README.md` | 对外产品门户：定位、自建模块+32 款托管名单、构建入口 | 259 | 2026-10-21 | 活 |
| `CLAUDE.md` | 项目规范：Git 提交/原子化/代码质量/安全守则（与 `.claude/CLAUDE.md` **逐字节相同**，见 §3.1） | 49 | 2026-09-14 | 活（双份并存） |

### `docs/`（23）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/ARCHITECTURE.md` | 现行架构事实权威规范（模块契约、托管、数据根） | 186 | 2026-10-21 | 活 |
| `docs/BACKLOG.md` | 候选想法池 + 已入库清账表 + "明确不做"裁决留档（2026-10-21「合并+更新」轮瘦身，原 §1 十四张卡片并账为一张清账表；剩余工作/复进唯一入口在 PLAN_REMAINING_WORK，计数只认 FACTS） | 98 | 2026-09-25 | 活（候选池账本） |
| `docs/BUG_AUDIT.md` | 2026-09-04 全仓静态 Bug 审查报告（后续修复进度在文外） | 694 | 2026-09-16 | **已删除**（2026-10-21 机主授权；孤本见 [`docs/archive/KEY-NUGGETS.md` §一](archive/KEY-NUGGETS.md)） |
| `docs/CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md` | 收敛后代码质量审查账（批 1/2/3 已闭环，复核门禁全绿） | 219 | 2026-09-26 | **已删除**（2026-10-21 机主授权；§5 裁决孤本见 [KEY-NUGGETS §三](archive/KEY-NUGGETS.md)；PRW 与 P0 三档依据行已改指该存底） |
| `docs/DEVELOPMENT.md` | 环境准备/HMR/静态检查/单测/生产编译流程指南 | 165 | 2026-09-17 | 活（存疑，见 §4） |
| `docs/DEVPLAN.md` | M0–M11 里程碑史账（自述"执行现状"名不副实，进度看 PLAN_REMAINING_WORK） | 141 | 2026-10-12 | 活（史账角色） |
| `docs/FACTS.md` | 计数类事实唯一权威，由 `scripts/docsfacts` 从代码现推（人勿手抄数） | 60 | 2026-10-21 | 活 |
| `docs/FEATURES_OVERVIEW.md` | 2026-09-16 功能全景盘点（38 模块口径为时点值） | 168 | 2026-09-16 | 快照 |
| `docs/FRONTEND.md` | 前端架构、设计语言约定与重构蓝图唯一权威（视觉冲突让位 design-system.md） | 311 | 2026-10-21 | 活 |
| `docs/FUTURE_FEATURE_RECOMMENDATIONS.md` | 09-19 后续功能候选方向评估（已带时点快照帽） | 584 | 2026-10-12 | 快照 |
| `docs/MOOTOOL_ANALYSIS.md` | MooTool 借鉴分析简版（四份可研报告的输入源，详版在 git 历史 77044c0） | 108 | 2026-09-17 | 快照 |
| `docs/MOTIVATION.md` | 项目缘起自述（"为什么做"，功能取舍的原始依据） | 52 | 2026-09-19 | 活 |
| `docs/OCR_DUAL_ENGINE_PLAN.md` | OCR 双引擎（微信私发+PP-OCRv6 公开）执行计划 | 181 | 2026-09-17 | 疑过期（复选框未勾/M0"待验"，与 hanxi-ocr 组件已实际发货的现状疑似脱节，待 OCR 线对账） |
| `docs/PLUGINIZATION_AND_PRODUCT_EVOLUTION.md` | 插件化时机与产品演进专题基线（带时点快照帽，数字回 FACTS） | 416 | 2026-10-12 | 活（判断基线） |
| `docs/PLUGINS_ARCHITECTURE.md` | 六模块时代的单体懒加载改造方案（首段显式声明"历史决策留档不再更新"） | 204 | 2026-10-12 | 快照 |
| `docs/PRD.md` | 需求与产品设计规范 v1.3（"做成什么样"） | 165 | 2026-10-21 | 活 |
| `docs/PROGRESS.md` | F1–F9 九件套历史过程流水账（10-12 归档注记 + 10-21 指针补记：计数查 FACTS、真机开单让位验收工单、待裁三件归 PRW 拍板族） | 126 | 2026-10-12 | 快照 |
| `docs/FRONTEND_QUALITY_REVIEW_2026-10-26.md` | 2026-10-26 前端质量审查报告与错题本（**属并行会话产出**，本表仅登记索引） | 160 | 2026-10-26 | 快照 |
| `docs/PROJECT_REVIEW_2026-09-08.md` | 2026-09-08 项目分析与优化路线（基线 c91a96f，三路审阅汇总） | 586 | 2026-09-08 | **已删除**（2026-10-21 机主授权；孤本见 [KEY-NUGGETS §二](archive/KEY-NUGGETS.md)） |
| `docs/PROJECT_STRATEGY.md` | 战略基线（2026-09–2027-09，做什么/不做什么，季度复审） | 775 | 2026-10-12 | 活 |
| `docs/THIRD_PARTY_NOTICES.md` | 第三方托管许可证告知登记（32 款口径由 cataloggen 锁定，EarTrumpet 特例） | 390 | 2026-10-21 | 活 |
| `docs/TROUBLESHOOTING.md` | 踩坑记录与排查指南（CLAUDE.md 指定的持续沉淀地；目录/编号问题见 §3.6） | 1581 | 2026-10-26 | 活 |
| `docs/WSL_REDESIGN_PROGRESS_2026-09-08.md` | WSL 模块改造过程存档（09-14 订正为落地实况版） | 44 | 2026-09-15 | **已删除**（2026-10-21 机主授权；孤本见 [KEY-NUGGETS §四](archive/KEY-NUGGETS.md)） |

### `docs/reviews/`（1）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/reviews/2026-10-26-backend-code-review.md` | 2026-10-26 后端 Go 8 路分区审查汇总（P0 静默失效两条、门禁三轮红逐轮定性、死代码分档、待裁 16 条、子 agent 不实交付的实测驳回） | 354 | 2026-10-26 | 快照 |

### `docs/adr/`（3）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/adr/ADR-0001-workbench-module-contract.md` | Wave 0 工作台×官方模块化共同契约冻结 | 64 | 2026-09-19 | 活（决策记录） |
| `docs/adr/ADR-0002-shared-kernel-api-gate.md` | Wave 4 共享托管内核 API 稳定门与逃生钩子审查规则 | 49 | 2026-09-19 | 活（决策记录） |
| `docs/adr/ADR-0003-scope-cuts-for-personal-nas-first-tool.md` | 自用+NAS 同步定位下的范围裁剪（签名/拆包/加密降级为条件路径） | 29 | 2026-09-19 | 活（决策记录） |

### `docs/design/`（6）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/design/themes/README-v1.md` | 主题配色提案 v1（自述已废，留对照与取值考古） | 74 | 2026-10-12 | **已删除**（2026-10-21 机主授权；有效方法论已由 `themes/README.md` §2 承接；同批删除连带 `preview-v1.html`、`gen-palettes-v1.mjs`，`preview.html` 内两处指向已改口径） |
| `docs/design/themes/README.md` | 主题配色提案 v2·mica 通透系落地说明（双轴五板已接入 tokens.css） | 49 | 2026-09-17 | 活 |
| `docs/design/themes/tokens/iris-report.md` | iris 色板 OKLCH 深化数值报告 | 131 | 2026-09-17 | 快照（取值存档） |
| `docs/design/themes/tokens/jade-report.md` | jade 色板深化数值报告（v2.1） | 162 | 2026-09-17 | 快照（取值存档） |
| `docs/design/themes/tokens/onyx-report.md` | onyx 色板审查数值报告（v2.1） | 113 | 2026-09-17 | 快照（取值存档） |
| `docs/design/workbench-evolution/README.md` | 未来首页静态设计稿资产说明（未接入业务的评审件） | 61 | 2026-09-18 | 疑过期（排期归属未裁：仍是要做的未来稿还是已冷处理，见 §4） |

### `docs/archive/`（1，T8 轮 2026-10-21 新建，尚未入库）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/archive/KEY-NUGGETS.md` | 归档线孤本摘入档（T8：只收"别处没有"的仍有效结论/实测数字/机主裁决，来源文件只盖戳不删移） | 86 | 2026-10-21 | 活（归档线摘入档；§六为 hanxi-ocr 私发孤本数字，**含私发敏感节，勿公开引用**） |

### `docs/plans/`（27）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `docs/plans/2026-09-26-clipboard-contract.md` | 剪贴板内置多 agent 并行开发冻结契约（当时唯一事实源） | 325 | 2026-09-27 | 快照（契约已随收口完成） |
| `docs/plans/2026-09-26-clipboard-decision.md` | 剪贴板内置决策档案（A1 档案线，裁决+剧本，星数等带日期口径） | 136 | 2026-09-27 | 快照 |
| `docs/plans/2026-09-26-clipboard-wiring-checklist.md` | 剪贴板收口接线行级勘查清单（自述"行号快照勿信"，执行前须重锚） | 399 | 2026-09-27 | 快照 |
| `docs/plans/PLAN_CLIPBOARD.md` | 剪贴板联动惯例可研（10-12 终态帽：核心约束已被 N45 推翻，按史实留档） | 170 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_HISTORY.md` | 统一历史记录公共包计划（`internal/history` 已存在，首批接入落地） | 157 | 2026-09-17 | 快照 |
| `docs/plans/PLAN_MCP.md` | MCP/Skill AI 接入计划（主线已落地；C6 CLI 双身份从未落地待裁废弃、C9 真机红队未跑） | 269 | 2026-09-24 | 快照（两尾账未销） |
| `docs/plans/PLAN_N27_ICON.md` | 托管软件真实图标侦查收口+分批方案（批 A–B 全数落地，文内自述"建议归档"） | 78 | 2026-09-24 | 快照 |
| `docs/plans/PLAN_N33_SNAPSHOT_UX.md` | 设置「历史版本」分区交互重做方案（批 A–D 已落地；真机双模式验收 D1–D10 与 AtPath 改名仍挂账） | 243 | 2026-10-12 | 快照（实施说明可复用） |
| `docs/plans/PLAN_N37_ONCOLOR.md` | N37 按钮配色返工定案执行收口件（六路并流落地，§5 为拍板台账） | 259 | 2026-10-13 | 快照 |
| `docs/plans/PLAN_N40_WHEEL_SKIN.md` | N40③ 轮盘皮肤提案拍板件（"全 B"已落地，余两枚 drop-shadow 待看） | 171 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_N4_RAMMAP.md` | RAMMap 集成拍板件（路线 A 已入库；真机验收六条未跑；路线 B 复活为 N23 已落地） | 98 | 2026-09-23 | 快照 |
| `docs/plans/PLAN_N5_QUICKMENU.md` | 快捷轮盘改进收口件（终态帽：多数批次已入库，余 N5b 两笔+判重病灶） | 104 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_N6_MSGBOARD_UI.md` | 留言板 UI 重设计收口件（牌桌演进全入库，余真机观感验收+三条结构观察） | 38 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_OFFICIAL_MODULE_DISTRIBUTION.md` | 官方模块按需装卸与 Monorepo 多产物改造执行计划（签名/manifest/多产物章节经 ADR-0003 降为 N/A 条件路径储备） | 1760 | 2026-09-19 | 活（条件重开段） |
| `docs/plans/PLAN_P0_BATCH1_FIX.md` | P0 批 1 修复计划（Registry 竞态等两项，随 CODE_QUALITY 批 1 收口） | 43 | 2026-09-21 | 快照 |
| `docs/plans/PLAN_P0_BATCH2_FIX.md` | P0 批 2 修复计划（标题"2b 待机主选机制"与 CODE_QUALITY"批 2 已收口入库"矛盾） | 32 | 2026-09-21 | 疑过期（状态冲突，见 §4） |
| `docs/plans/PLAN_P0_BATCH3_FIX.md` | P0 批 3 前端真相收口计划（先复核后修，随批 3 收口） | 58 | 2026-09-23 | 快照 |
| `docs/plans/PLAN_PATHS.md` | F6 数据目录策略定稿（已实施入库；Portable/Standard 字样均为改造前语义） | 76 | 2026-10-12 | 快照（裁定档案） |
| `docs/plans/PLAN_PIIK_HOSTING.md` | piik 托管决策档案与验收剧本（条目 13 许可证义务已随总闸与告知文档落账） | 137 | 2026-09-26 | 快照 |
| `docs/plans/PLAN_REMAINING_WORK.md` | **唯一复入口**：剩余工作排期（暂停基线+在途 N/W 账） | 287 | 2026-10-21 | 活 |
| `docs/plans/PLAN_SNAPSHOT.md` | 数据自动版本快照计划（10-12 终态帽：已落地，UX 重做由 N33 接管） | 190 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_UI_THEME_REFINEMENT.md` | 界面与主题优化改造提案（终态帽：随 N37/N39 落地，留作案值考古，不再当排期输入） | 986 | 2026-10-12 | 快照 |
| `docs/plans/PLAN_W1_DECISIONS.md` | W1 决策与前置调研（N3 策略裁决 C+分档终稿+三项调研证据留档，W2 工作分解输入） | 91 | 2026-09-21 | 活（W2 执行输入） |
| `docs/plans/PLAN_W6_HOST_CONTRACT.md` | N13 全平台发布物标注底账与契约方案（开工前评审件；底账"28 款"为时点值，现值 32） | 142 | 2026-09-23 | 疑过期（待开工但底账数字已漂移，动工前必须按 FACTS 重锚） |
| `docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md` | 工作台界面×官方模块化联合总排期（Wave 0–4 落地，5 去重复完成，6 条件路径） | 720 | 2026-09-19 | 活（总排期） |
| `docs/plans/REAL_DEVICE_ACCEPTANCE_2026-09-25.md` | 真机补看与验收清单（锚定 09-25 构建 `bin/hanxi-new.exe`，距 HEAD 已 26 天+后续多批入库） | 76 | 2026-09-25 | 疑过期（构建物陈旧，需重验或归档裁决） |
| `docs/plans/WAVE5_MIGRATION_INVENTORY.md` | Wave 5 托管模块迁移分类账（只读盘点，supervisor 接线决策输入，自称"规划输入非实现事实"） | 65 | 2026-09-19 | 快照（时点盘点） |

### `frontend/`（1）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `frontend/README.md` | 前端工程薄层说明（技术栈声明，权威指回 docs/FRONTEND.md 与 design-system.md） | 63 | 2026-09-14 | 活 |

### `.claude/`（6）

| 文件 | 一句话职责 | 行数 | 末改 | 初判状态 |
|---|---|--:|---|---|
| `.claude/CLAUDE.md` | 同根 `CLAUDE.md`（逐字节相同的副本，见 §3.1） | 49 | 2026-09-14 | 活（双份并存） |
| `.claude/skills/hanxi-workbench-ui/SKILL.md` | 工作台 UI 设计技能入口（触发条件+三份 references 索引） | 92 | 2026-08-29 | 活 |
| `.claude/skills/hanxi-workbench-ui/references/component-patterns.md` | 工作台组件模式库（英文） | 231 | 2026-09-17 | 活 |
| `.claude/skills/hanxi-workbench-ui/references/design-system.md` | 视觉设计语言/色板 token 权威（FRONTEND.md 自认视觉冲突时让位于此） | 187 | 2026-09-25 | 活 |
| `.claude/skills/hanxi-workbench-ui/references/quality-checklist.md` | UI 完工前质量清单（英文） | 99 | 2026-08-29 | 活 |
| `.claude/skills/integrate-github-tool/SKILL.md` | GitHub 工具托管集成 SOP（版本/启停/契约 checklist，事件注册示例含 Go 代码） | 112 | 2026-10-13 | 活 |

---

## 2. 链接体检结果（只报告，未修复）

扫描范围：全部 67 份入库 `.md`；方法：正则提取 `](...)` 全部 170 处，逐类核验（文件存在性按链接所在目录相对解析；锚点按 GitHub slug 规则近似推算）。

**结论：相对文件链接 0 断链。** 明细：

| 类别 | 数量 | 结果 |
|---|--:|---|
| 相对文件链接（`./`、`../`、同目录） | 84 | 全部指向存在的路径，0 断链；未发现"以仓库根为基准"的错写风格（如 `docs/plans/` 内写 `](docs/x.md)`） |
| 外部链接（http/https） | 10 | 未核验可达性（离线环境，且非本任务口径） |
| 纯锚点链接（`](#…)` 同文件跳转） | 68 | **3 处疑断**（见下表） |
| 代码文本伪链接（`RegisterEvent[T]("事件名")` 形态命中 `](...)` 语法） | 8 | 误报已剔除（TROUBLESHOOTING:216、clipboard-wiring-checklist:42、integrate-github-tool SKILL:65），**非链接，勿修** |

### 2.1 断/疑断锚点清单（全在 `docs/TROUBLESHOOTING.md` 目录区，交 TROUBLESHOOTING 所有权线）

| 出处 | 目标锚 | 病因 |
|---|---|---|
| `docs/TROUBLESHOOTING.md:20` | `#6-构建upx-压缩-go-程序导致运行时内存暴涨` | 目录条目 6 号 vs 正文该坑现为 `### 7. 构建：UPX…`（L190），编号漂移致悬空 |
| `docs/TROUBLESHOOTING.md:21` | `#7-wifi-密码netsh-解析三连坑gbk-编码--行锚--提权输出通道` | 正文实为 `### 8. WiFi 密码：netsh 解析三连坑（GBK 编码 / $ 行锚 / 错误归因权限）`（L199）——编号漂移+尾词措辞已变，双重不符 |
| `docs/TROUBLESHOOTING.md:82` | `#51-…-varshadow-panel-…` | 正文 `### 51. …var(--shadow-panel)…`（L916）按 GitHub 规则 slug 应为 `var--shadow-panel`（双连字符），目录写成单连字符 |

> 口径说明：锚点核验为 GitHub slug 规则的**近似推算**，前两处是编号漂移属硬伤（任何渲染器都落空），第三处双连字符规则在不同渲染器有极小差异，故标"疑"。同文件正文编号体系的整体病灶见 §3.6。

---

## 3. 版图层面问题与归并建议（供机主裁决，本路不动手）

### 3.1 CLAUDE.md 双份逐字节相同
`CLAUDE.md` 与 `.claude/CLAUDE.md` 经 `diff` 确认内容完全一致（各 49 行，同 2026-09-14 末改）。两份独立编辑迟早漂移。**建议**：定一处为源（按 harness 加载优先级说明），另一处生成或改为一行指路；具体机制涉及 Claude Code 加载语义，需机主先确认双份是否都必需。

### 3.2 `docs/` 根混放活文档与历史快照，无归档区
22 份中至少 8 份是明确历史件（BUG_AUDIT、CODE_QUALITY_PROGRESS_2026-09-19、WSL_REDESIGN_PROGRESS_2026-09-08、PROJECT_REVIEW_2026-09-08、FEATURES_OVERVIEW、FUTURE_FEATURE_RECOMMENDATIONS、MOOTOOL_ANALYSIS、PLUGINS_ARCHITECTURE），其中 3 份还把日期烧进了文件名。它们与 FACTS/ARCHITECTURE/PRD 等活文档平级混放，进门者难辨现行与存档。**处置进展（2026-10-21 机主授权删除）**：前四份——BUG_AUDIT、CODE_QUALITY_PROGRESS_2026-09-19、WSL_REDESIGN_PROGRESS_2026-09-08、PROJECT_REVIEW_2026-09-08——**已删除**，孤本结论存底于 [`docs/archive/KEY-NUGGETS.md`](archive/KEY-NUGGETS.md)，外部引用行（PRW P0 段、P0 三档依据行）已同步改指存底；同批另删 `themes/README-v1.md` + `preview-v1.html` + `gen-palettes-v1.mjs`（v1 全族）与 `frontend/public/Inter-Medium.ttf` + `frontend/Inter Font License.txt`（零消费方死重，T4 实测）。剩余 FEATURES_OVERVIEW / FUTURE / MOOTOOL / PLUGINS_ARCHITECTURE 四份判为仍有效或活文档，未删。**仍待裁**：`docs/archive/` 已开设但历史件大部队迁移未执行（plans 甲类 15 份建议"移不是删"）；同步修正各文件互引链接（本地图已列全引主）。PLUGINS_ARCHITECTURE 与 ARCHITECTURE 文件名一字之差、性质迥异（现行 vs 留档），迁移时可顺带改名消歧。

### 3.3 `docs/plans/` 命名三风格并存
同一目录混着三种命名法：`PLAN_*` 大写蛇形（22 份）、`2026-09-26-*` 日期前缀小写（3 份）、`REAL_DEVICE_ACCEPTANCE_2026-09-25` / `WAVE5_*` 日期后缀（2 份）。且 `PLAN_*` 内部语义也分三层：**总排期**（REMAINING_WORK、WORKBENCH_ROADMAP、OFFICIAL_DISTRIBUTION）、**单议题方案**（N 系 10 份、P0 批 3 份、W1/W6、PATHS/SNAPSHOT/MCP/CLIPBOARD/HISTORY/PIIK）、**契约+档案+勘查件**（clipboard 三部曲）。多数 N 系/W 系单议题件已落地成"收口档案"。**建议**：确立"plans=未完结，完结即移 archive（或加终态帽留地）"的纪律与命名规范（一种风格），归并动作交主会话分发。

### 3.4 进度账四件套主从已由 T7 裁定但未在文件名层显形
2026-10-21「合并+更新」轮（T7）终态裁定：**剩余工作/复进唯一入口 = `docs/plans/PLAN_REMAINING_WORK.md`；BACKLOG = 候选想法池 + 已清账 + 不做裁决留档；PROGRESS = 历史过程流水；DEVPLAN = M0–M11 里程碑史账 + 阶段规划**。这套主从目前靠各文件头注维持；DEVPLAN 标题仍叫"执行现状"（自述名不副实）。**建议**：DEVPLAN 可考虑改名 `MILESTONES.md` 或在文件头给最强提示，避免新会话误读为现行进度。

### 3.5 设计权威链跨三个目录，缺 docs/design 顶层入口
现行链条：`docs/FRONTEND.md`（总权威）→ 视觉冲突让位 `.claude/skills/hanxi-workbench-ui/references/design-system.md` → 色板落地说明 `docs/design/themes/README.md` → 数值考古 `themes/*-report.md`。链路本身各方已互认（无矛盾），但 `docs/design/` 无顶层 README，themes/ 下已废的 v1 与现行的 v2 并排；`workbench-evolution/` 归属排期未裁。**建议**：v1 及 *-report 随 §3.2 归档区一并处理；workbench-evolution 交 N 系排期线裁决"继续待采纳还是归档"。

### 3.6 TROUBLESHOOTING.md 编号体系双轨、目录滞后
1394 行的持续追加型账本里，`### N.` 正文标题共 124 个、最大编号 111，而数字目录条目仅 57 条；且有 12 个编号各被两个不同标题使用（`0, 8–17, 58`）——早期条目用一套编号，后期条目又起一套，两轨并行导致 §2.1 的目录悬空只是表象，**根因是编号分配无单一账**。新条目持续追加会加重漂移。**建议**：TROUBLESHOOTING 所有权线做一次编号重排或"编号即文件名式全局唯一"治理，并决定目录是逐条全覆盖还是仅索引大类。

### 3.7 归并建议地图（主题组 → 现状 → 建议）

| 主题组 | 现存文件 | 建议去向（待裁） |
|---|---|---|
| 项目规范 | `CLAUDE.md` + `.claude/CLAUDE.md` | 单源化（§3.1） |
| 现行权威五件套 | README / ARCHITECTURE / PRD / FRONTEND / FACTS | 留 `docs/` 根，不动 |
| 进度/排期 | PLAN_REMAINING_WORK（复入口）、BACKLOG、DEVPLAN、PROGRESS | 职责不动；DEVPLAN 标题或改名消歧（§3.4） |
| 战略/动机/评估 | PROJECT_STRATEGY、MOTIVATION、PLUGINIZATION…、FUTURE_FEATURE…、MOOTOOL… | 战略与动机留根；两份带帽评估+借鉴分析入 archive |
| 审计/质量史账 | ~~BUG_AUDIT、CODE_QUALITY_PROGRESS、PROJECT_REVIEW、WSL_REDESIGN~~（四份已于 2026-10-21 删除，孤本入 KEY-NUGGETS）、FEATURES_OVERVIEW、PLUGINS_ARCHITECTURE、ADR 关联旧方案 | 前四份已结案删除；余下入 archive（ADR 三份留 `docs/adr/` 不动） |
| OCR 域 | OCR_DUAL_ENGINE_PLAN（+私发组件在仓外文档） | 疑过期待 OCR 线对账后归档或续活（§4） |
| 单议题收口档案 | plans 下 N 系×10、P0 批×3、PATHS、SNAPSHOT、PIIK、CLIPBOARD、HISTORY、MCP、clipboard 三部曲×3 | 完结者移 archive 或统一挂终态帽留地（§3.3） |
| 未完工执行面 | PLAN_OFFICIAL…、PLAN_WORKBENCH…、PLAN_W1…、PLAN_W6…、WAVE5…（待用部分） | 留 plans；W6/WAVE5 动工前按 FACTS 重锚 |
| 设计资产 | docs/design/**、design-system 三 reference | 维持权威链；v1/报告随 archive（§3.5） |

---

## 4. 存疑项（无法只读核验定论，需所有权线/机主确认）

1. **`docs/DEVELOPMENT.md`（活文档中末改最早，09-17）**：构建/运行命令描述是否仍与现行环境一致（本机存在 npm run shim 异常、`task build` 可用等环境事实），需一次真机对账。
2. **`docs/OCR_DUAL_ENGINE_PLAN.md`**：正文复选框与风险表仍是"待验"口径，而 OCR 私发组件实际已在发货迭代——计划执行状态未回写。
3. ~~**`docs/plans/PLAN_P0_BATCH2_FIX.md`** 标题写"2b 待机主选机制"与 `CODE_QUALITY_REVIEW_PROGRESS`"批 1/2/3 全部收口入库"矛盾~~ **已结案（2026-10-21 T8 取证）**：CODE_QUALITY 侧为实——2b 的 19/19 journal 租约买断由 `667006f`+`338dac5` 收口、登记 `f3f63fe`（均 2026-09-23），PRW 批 2 行亦载"2b ✅"实况；相撞处仅该档旧标题一句，T6 已为其盖"已完成"状态戳。原 `CODE_QUALITY_REVIEW_PROGRESS` 档已删，孤本见 [KEY-NUGGETS §三](archive/KEY-NUGGETS.md)。
4. **`docs/plans/PLAN_W6_HOST_CONTRACT.md`**：底账 28 款 vs FACTS 现值 32 款；"开工前评审件"是否仍排期内，或应按 ADR-0003 降档。
5. **`docs/plans/REAL_DEVICE_ACCEPTANCE_2026-09-25.md`**：验收锚定 09-25 构建物，其后已有多批入库（含托管新件）；重验或归档需裁决。
6. **`docs/design/workbench-evolution/`**：首页演进稿仍在引用生产 token，但其设计意图是否有排期承接（N 系无对应条目）未明。
7. **锚点规则口径**：§2.1 第三处按 GitHub slug 判"疑断"，若团队主要用 VSCode 预览，渲染器差异可能使结论不同。
8. **并行在途**：本表生成时多路会话正在改写文档，任何行号/日期/状态以各文件最新提交为准；本文件建议由主会话在各战线收口后统一刷新一次。
