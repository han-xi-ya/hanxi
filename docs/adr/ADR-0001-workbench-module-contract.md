# ADR-0001:工作台 × 官方模块化共同契约冻结(Wave 0)

- 状态:已接受
- 日期:2026-09-18
- 关联:`docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md` §5(Wave 0)、`PLAN_UI_THEME_REFINEMENT.md`、`PLAN_OFFICIAL_MODULE_DISTRIBUTION.md`
- 权威源:`internal/extapi/catalog.go`、`internal/extapi/receipt.go`、`scripts/fixture/module_catalog.json`、`frontend/src/constants/status.ts`

## 1. 决议

### 1.1 契约版本
- `extapi.ModuleContractSchema = 1` 随 ModuleState/Operation 投影下发;前端与契约测试以 schema 判定兼容性。
- 破坏性变更(枚举值语义、字段删除、状态机优先级)必须递增 schema 并在本目录另立 ADR,禁止静默改语义。字段新增属于兼容演进,不升 schema。

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
