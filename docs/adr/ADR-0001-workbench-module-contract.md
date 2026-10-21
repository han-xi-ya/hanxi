# ADR-0001:工作台 × 官方模块化共同契约冻结(Wave 0)

- 状态:已接受(2026-10-21 状态核查确认仍生效,未被取代;后续部分实况见 §4)
- 日期:2026-09-18
- 关联:`docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md` §5(Wave 0)、`PLAN_UI_THEME_REFINEMENT.md`、`PLAN_OFFICIAL_MODULE_DISTRIBUTION.md`、`ADR-0002`(Wave 4 内核稳定门,本 ADR 的后置波次)、`ADR-0003`(Wave 5 声明化/签名路线裁剪,影响 §1.4 登记风险清偿路径与 §2 后置项走向)
- 权威源:`internal/extapi/catalog.go`、`internal/extapi/receipt.go`、`scripts/fixture/module_catalog.json`、`frontend/src/constants/status.ts`

## 1. 决议

### 1.1 契约版本
- `extapi.ModuleContractSchema = 1` 随 ModuleState/Operation 投影下发;前端与契约测试以 schema 判定兼容性。
- 破坏性变更(枚举值语义、字段删除、状态机优先级)必须递增 schema 并在本目录另立 ADR,禁止静默改语义。字段新增属于兼容演进,不升 schema。
- **修订(2026-09-19,冻结收尾)**:`ModuleCatalogItem.delivery` 更名 `deliveryKind`——原与 `ModuleState.delivery`(生命周期维度)同名同 JSON 键、语义正交,消费端易拿错。契约从未对外发布、仓内消费者已全部同步,属发布前收口而非破坏性变更,schema 保持 1;两键自此**永久异名,禁止回撞**。

### 1.2 三模型职责(单一真相)
| 模型 | 权威源 | 消费者 |
|---|---|---|
| `ModuleCatalogItem`(静态身份/交付形态/入口/能力/owner) | 装配根 Catalog 表(生成物 `internal/app/catalog.go`,基线冻结于 fixture) | 模块中心、导航、搜索、托盘、热键、MCP、可见性判断 |
| `ModuleState`(四维:Delivery/Policy/Runtime/Health + primaryAction/reason/summary) | `extapi.Registry.ListStates()`(receipt/配置/wrapper 标志/覆盖项合并投影) | 首页摘要、模块中心卡片、入口门控 |
| `Operation`(长操作与租约载荷) | Wave 1-3:Registry 租约与内存事务;Wave 4+:journal 持久化 | 模块中心详情、首页最近任务、代表页、诊断 |

前端只缓存投影副本(`useModuleCatalog` composable 级别),不持久化、不推导第二份业务状态;卡片禁止自行堆叠 if/else 推断主操作——`primaryAction` 与 `reason` 由 `StateInput.Project()` 统一状态机计算。

### 1.3 状态机优先级(冻结)
`installing → updating → removing → repairing → orphaned → corrupt → revoked → incompatible → unverified → blocked → pending-consent → absent(install) → disabled(enable) → crashed/failed(retry) → open`
命中即停。摘要派生口径按模块分发专项 §6.5,测试穷举全笛卡尔积(2205 组合)守护无未定义输出。

### 1.4 可见性与调用门
- `ModuleState.Visible()` = `installed ∧ (enabled ∨ mandatory)`,全部入口(导航/搜索/托盘/热键/MCP/窗口/后台/RPC)消费同一判定。
- Wave 3 `Acquire(moduleID)` 检查顺序冻结为:**blocked → receipt 已安装 → 懒激活(并发初始化一次) → enabled/非 stopping → operation lease 入账**(安全裁决优先于安装呈现,与 §1.3 状态机同序);release 幂等,停用与退出 drain 以 lease 归零为门。
- Core 守卫:mandatory 模块拒绝 SetEnabled(false) 与卸载事务,投影恒 present mandatory+可用,禁止"停用成功但状态仍显 mandatory"的分叉。
- Wave 1 允许"先观测后强制"过渡:入口未接线前,投影与日志先行;Wave 3 收口时禁止保留任何已知旁路。
- **有界 drain**(Wave 3 落地):模块普遍缺操作取消通道(专项认可"先拒绝新操作+有界 drain,逐模块补取消"),`SetEnabled(false)` drain 预算 30s、`ShutdownAll` 60s,超时强制收口并 `slog.Error` 显式告警,绝不静默;进程侧残留由 JobObject 兜底。
- **接线通道**:模块实现 `extapi.GateAware`(Register 时锁外注入 `Gate`),service 内嵌 `LeaseHolder`,业务 RPC 方法一行 `holder.Enter()` 接门。**装配布线 setter**(SetWailsApp/SetHistory/SetMemoHook/SetMainWindow 等)本 Wave 不接门——Go 直调时序在注入前,接门会破坏装配;其"前端可经绑定调用"属既有面残留风险,Wave 5 声明化时随接口私有化清偿(登记于本 ADR,不静默)。
- 被 Module/钩子/goroutine 直调的导出方法(如 Shutdown)拆 unexported 内部版供 Go 直调,导出版接门,防止停用自锁。
- MCP 无头 `registryGate` 升级为取租约式 `Check(moduleID) (release, err)`,无头在途调用纳入 drain 门。

### 1.5 逻辑安装态与迁移
- Receipt 落点:`<数据根>/modules/receipts/<id>.json`(kind=builtin-logical,原子写,损坏按未安装处理并告警)。
- 首次升级迁移:为全部 41 个注册模块幂等补建 installed receipt;`Enabled=true → installed+enabled`,`Enabled=false → installed+disabled`(不丢数据/入口)。迁移写 versioned marker、幂等、失败保留旧配置。
- 逻辑卸载 = drain + OnDestroy + 移除 receipt;UI 必须同步呈现"不释放 hanxi.exe 体积"口径(文案由 delivery kind 强制)。

### 1.6 入口清点结论(Wave 0 DoD:无"未知旁路")
- RPC(全 41 模块 service 方法)为唯一真正执行旁路面;托盘命令执行链、MCP(gateMiddleware fail-closed)、启动预激活、命令面板均已尊重 Enabled,无需返工,仅需并入统一 Acquire。
- 已登记缺陷(本轮修复):①停用模块托盘条目不消失(可见性);②停用 ocr 后剪贴板识图热键不注销(OS 键位占用);③`inFlight` lease 仅覆盖托盘命令——RPC 接线时同点补全。
- 各模块入口/能力矩阵以 `scripts/fixture/module_catalog.json` 为准,契约测试锚定源码证据(TrayCommands/NewWithOptions/mcpModules/preactivated)。

### 1.7 视觉与 Shell 基线决议
- 三档语义容器冻结:`--container-standard:1000px`、`--container-workbench:1200px`、`--container-wide:1440px`;`PageContainer.vue` 为唯一入口,页面容器管宽度、Shell 管视口边距,禁止双重 padding。
- 焦点与禁用态收口到 `--focus-ring`/`--surface-disabled` 语义 token,禁止页面私有补丁。
- 双栏 Shell 三层语义与 `hanxi.railExpanded`/`hanxi.navPanelCollapsed` 偏好保持不变;抽屉补齐焦点进入/归还与 Esc 行为。
- 模块中心为一级核心入口,rail 顶部位(首页之下、分类之上),任何模块状态下可进入;核心页豁免清单同步(`App.vue` 回落逻辑)。
- Jade 浅色新装默认推迟到 Wave 5 门禁通过后评估,本轮只动结构 token 不动默认值;老用户已保存主题/色板/偏好零迁移。

### 1.8 首页/模块中心边界(按专项 §7.3)
首页=工作台(运行摘要、待处理、最近任务、常用入口,不展示完整目录);模块中心=完整 Catalog 与管理入口(安装/启用/停用/卸载/诊断)。迁移期首页保留"管理模块"跳转,不留第二套目录。

## 2. 未决与后置
- Runtime 的 `crashed` 上报通道(受管进程异常退出→Registry)在 Wave 4 ProcessSupervisor 接通;`pending-consent`、`blocked`(撤回类)随 Wave 5 签名目录启用。
- Operation 的 journal 持久化与刷新恢复在 Wave 4;Wave 1-3 为内存事务(逻辑安装/卸载/启停可短到同步完成)。
- 状态词汇表与 `TOOL_STATE_META` 的合并在 Wave 3 组件收口时执行。

## 3. 回滚
契约测试全绿为合入门禁;回滚 = revert 契约文件 + fixture + 投影消费方,注册表旧路径(SetEnabled/RunTrayCommand 语义)保持兼容,无数据不可逆变更(receipt 缺失自动回落"未安装",Enabled 语义未变)。

## 4. 后续实况(2026-10-21 增补,只登记事实,不改历史决议正文)

- **Wave 0–4 已落地**:roadmap 2026-09-19 执行进度行(`docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md` 第 5 行)确认 Wave 0–4 已入库。§2 后置项对应实况:Operation journal 持久化已接装配根(`internal/app/app.go:361-378` `operation.OpenStore(paths.ModulesJournalsDir())`,账本不可信时托管写事务保持拒绝);`crashed` 枚举与状态机位已冻结在案(`internal/extapi/catalog.go:97-98,301,379-381` `RuntimeCrashed`)。`pending-consent`/`blocked` 原计划"随 Wave 5 签名目录启用",签名目录已按 ADR-0003 裁为 N/A 条件路径,此二态的启用前提自此悬空(登记,不覆写原文)。
- **注册模块数漂移(41→50)**:§1.5/§1.6 的"41 个注册模块"为冻结时点基数(基线佐证:`8b1eb3c` 时点 fixture 计数 = 41);现 fixture 与 `internal/app/catalog.go` 均为 50,最近一笔新增为 `3bc27ec`(2026-10-21,heukms 托管模块,含契约与文档计数)。此为数据层增长,`ModuleContractSchema` 保持 1,与 §1.1"新增属兼容演进"口径一致;跨文档取数以 `docs/FACTS.md`"业务模块总数 50"为准。
- **§1.4 登记风险的清偿路径被移除**:原文登记"装配布线 setter(SetWailsApp/SetHistory/SetMemoHook/SetMainWindow 等)……Wave 5 声明化时随接口私有化清偿"。Wave 5 声明化已被 ADR-0003 裁掉(roadmap 第 41、489 行:声明式 manifest 降为 N/A 条件路径),装配 setter 至今仍为 Go 直调且未接门(`internal/app/app.go:492,576,656,730` 等)。**该"不静默"登记的风险自此没有承载波次,处置需另行裁决**(存疑项,不在本增补中私自改判)。
- **模块名/描述属数据层演进,不动 schema**:`f40385b`(2026-10-20)portscan 显示名"端口扫描与指纹"→"网络测试"(源头 `internal/modules/portscan/module.go` Info().Name/Nav().Title 经 cataloggen 流入产物;fixture 779-781 行 `id` 仍为 `portscan`、`name` 为"网络测试",入口、绑定名不变),`ee7fd0d`(2026-10-12)删描述中不存在的 Nmap 引擎宣称。与 §1.2 `ModuleCatalogItem` 管静态身份的分层一致——id 稳定、name/description 演改属兼容变更。
- **§1.3 穷举守护仍有效**:`internal/extapi/catalog_test.go:259,517-518` 锚定 7×5×7×9=2205 全笛卡尔积穷举,无未定义输出。
- **§1.7 视觉基线抽查有效**:`PageContainer.vue` 存在(现位于 `frontend/src/components/ui/PageContainer.vue`);`hanxi.railExpanded` 偏好键在用(`frontend/src/components/shell/AppNavRail.vue:98`);Jade 浅色仍在 `frontend/src/composables/useTheme.ts`/`frontend/src/styles/tokens.css` 且未反转为默认,与"推迟到 Wave 5 门禁后评估"的裁量现状一致(其前提 Wave 5 同上已裁剪)。
