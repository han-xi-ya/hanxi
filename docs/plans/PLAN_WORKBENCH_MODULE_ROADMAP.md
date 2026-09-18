# Hanxi 工作台界面与官方模块化联合总排期

> 状态：执行路线草案；用于协调工作台界面、主题体系与官方模块按需分发，不替代两份专项设计。
>
> 专项文档：[界面与主题优化计划](./PLAN_UI_THEME_REFINEMENT.md) · [官方模块分发计划](./PLAN_OFFICIAL_MODULE_DISTRIBUTION.md)
>
> 设计参考：[工作台演进设计图与可交互稿](../design/workbench-evolution/)（[首页稿](../design/workbench-evolution/workbench-home.html)、[Jade 浅色图](../design/workbench-evolution/workbench-home-jade-light.png)、[Teal 深色图](../design/workbench-evolution/workbench-home-teal-dark.png)、[窄屏图](../design/workbench-evolution/workbench-home-jade-narrow.png)）

## 1. 文档定位

本文件回答“两个专项按什么顺序联合落地、在哪里汇合、何时可发布”。颜色、Token、响应式规格、模块事务协议、签名与 sidecar 安全边界等细节仍以两份专项文档为准，本路线不重复抄录。

联合排期的原因：

1. **两条线会修改同一批入口。** `App.vue`、Shell、导航、首页、模块状态组件与全局操作反馈，既是视觉改造面，也是模块安装态、启停态和调用门的产品出口。
2. **状态模型决定界面能否真实。** 首页和模块中心若先用前端临时布尔值落地，后续接入 Delivery、Policy、Runtime、Health/Currency 时必然重写卡片、导航、搜索和操作反馈。
3. **视觉基线决定模块中心能否扩展。** 若模块中心先独立造卡片、徽标和弹窗，主题专项随后收口公共组件，会产生第二轮全页面替换。
4. **共享托管内核需要可观察界面。** 下载、校验、安装、更新、回滚、修复、卸载均是长操作，必须由统一 `Operation` 在模块中心、首页和代表页面中一致呈现。
5. **发布风险必须分层。** 逻辑安装态与 UI 首个里程碑可先发布；共享托管内核、声明式迁移和物理拆包逐级提高风险，不应绑成一次大爆炸发布。

### 1.1 避免返工原则

- **先契约、后页面：** Wave 0 先冻结状态投影、主操作和可见性语义，再开始模块中心与新首页。
- **一个后端真相：** 前端不维护第二份模块安装、启用、运行或健康状态；所有入口消费同一 Catalog 投影。
- **Shell 一次改骨架：** 主题、导航、模块中心一级入口和首页职责在同一 Shell 基线上确定，后续只增内容，不再次改导航骨架。
- **公共组件先于批量迁移：** 状态徽标、模块卡片、操作进度、空态、错误态、确认对话框先收口，再迁移代表页和批量页面。
- **双样本后抽象：** 共享托管内核至少用两种托管形态验证；禁止为赶进度把某个模块的特殊流程直接命名为“通用内核”。
- **逻辑卸载不冒充物理瘦身：** Wave 1–3 仍可把代码留在宿主中，界面必须准确说明；只有 Wave 6 验证 clean host 不再携带相应资产后，才宣称释放宿主体积。
- **旧路径有期限地共存：** 每次迁移设置回滚窗口和删除门槛，但不得无限期保留两套主路径。

## 2. 联合共同模型

共同模型是两份专项的交界面。命名可在实现阶段按 Go/TypeScript 风格调整，但职责不得拆成多套互相推导的前端状态。

### 2.1 `ModuleCatalogItem`：静态身份与能力目录

职责：描述“模块是谁、如何交付、有哪些入口和能力、需要什么兼容与权限”，不承载瞬时进度。

```ts
interface ModuleCatalogItem {
  id: string
  name: string
  category: string
  delivery: 'builtin-logical' | 'managed-declarative' | 'official-sidecar'
  capabilities: string[]
  entrypoints: Array<'navigation' | 'search' | 'tray' | 'hotkey' | 'mcp' | 'window'>
  compatibility: { hostRange: string; platform: string[] }
  permissions: string[]
}
```

它由后端 Catalog/Registry 投影到前端，是模块中心、首页快捷入口、导航、搜索、托盘和 MCP 可见性判断的共同输入。Catalog 元数据与运行状态分离，避免每次进度变化都重建静态目录。

### 2.2 `ModuleState`：四维事实投影

职责：描述模块在当前设备上的事实，不把“已安装”“已启用”“正在运行”“可安全使用”压成一个布尔值。

```ts
interface ModuleState {
  moduleId: string
  delivery: 'absent' | 'installing' | 'installed' | 'updating' | 'removing' | 'repairing' | 'orphaned'
  policy: 'enabled' | 'disabled' | 'blocked' | 'pending-consent' | 'mandatory'
  runtime: 'inactive' | 'activating' | 'active' | 'busy' | 'stopping' | 'crashed' | 'failed'
  health: 'current' | 'update-available' | 'pinned' | 'incompatible' | 'corrupt' | 'revoked' | 'unverified' | 'degraded' | 'offline-stale'
  primaryAction: string
  reason?: string
}
```

- Registry/生命周期管理器是权威源。
- `primaryAction` 与禁止原因由统一状态机计算，卡片不得各自堆叠条件分支。
- 可见性规则同样从 Catalog + State 推导；停用或未安装后，RPC、导航、托盘、热键、MCP、独立窗口和后台任务不得旁路执行。
- 状态表达必须同时具备文字、图标语义和无障碍标签，不能只靠颜色。

### 2.3 `Operation`：长操作与调用租约

职责：统一表达安装、启动、停止、更新、回滚、修复、卸载及业务调用的生命周期；同时为 `Acquire(moduleID)` 提供可取消、可排空、可审计的租约。

```ts
interface Operation {
  id: string
  moduleId: string
  kind: 'install' | 'activate' | 'stop' | 'update' | 'rollback' | 'repair' | 'remove' | 'invoke'
  phase: string
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  progress?: number
  cancellable: boolean
  startedAt: string
  error?: { code: string; message: string; recoverable: boolean }
}
```

同一 Operation 应能被模块中心详情、首页“最近任务”、代表模块页面和诊断视图观察；前端重载后能从后端恢复，而非依赖组件内存。持久化安装事务还需与 journal、receipt、staging 和原子 active pointer 对齐，细节见模块分发专项。

## 3. 排期口径与联合总表

### 3.1 估算口径

- 人日包含设计澄清、实现、单测/契约测试、联调、可访问性、故障注入、发布演练和必要文档，不只计算编码。
- P50 表示依赖清楚、无重大平台事故的中位情形；P80 包含 Windows 文件锁、旧配置迁移、托管差异和发布演练返工。
- “2–3 人日历”按 A/B/C 三类职责可并行但关键路径仍串行估算，不是简单用人日除以人数。
- 表内日历为累计区间；团队少于 2 人、成员兼职或同时维护线上版本时应重新排期。

### 3.2 联合里程碑总表

| 联合范围 | 对应 Wave | 2–3 人团队 P50 | 2–3 人团队 P80 | 可交付结果 |
|---|---|---:|---:|---|
| 第一里程碑 | Wave 0–3 | **6–8 周** | **9–12 周** | 新 Shell/主题、模块中心 MVP、新首页 MVP、统一调用门与公共组件，可发布 |
| 加共享托管内核 | Wave 0–4 | **10–14 周** | **15–20 周** | Artifact/Process/Operation 内核及代表页面验证 |
| 加声明式迁移 | Wave 0–5 | **15–22 周** | **22–30 周** | 签名 Catalog、声明式托管与批量页面收敛 |
| 加物理拆包和多产物发布 | Wave 0–6 | **20–28 周** | **28–38 周** | OCR/FileShare sidecar、独立构建签名发布与回滚 |
| 单人完整执行 | Wave 0–6 | **7–9 个月** | **10–13 个月** | 同上；必须顺序化，不能照搬多人并行日历 |

**这不是把界面专项的 40–64 人日与模块专项的 108–172 人日直接相加。** 两份专项存在 Shell、首页、状态组件、Operation 展示、代表页迁移、测试与发布演练等重叠工作；联合排期会合并这些任务，同时新增跨线契约、集成缓冲和阶段发布成本。因此应以 Wave 的资源负载和里程碑日历管理，不以 `40–64 + 108–172` 得出承诺日期。各 Wave 人日用于容量规划，也不应机械求和后再除以人数。

## 4. 人员分工

| 角色 | 主责 | 次责与交接 |
|---|---|---|
| **A：后端状态与生命周期** | Catalog/Registry、`ModuleState`、统一 `Acquire` 调用门、drain/cancel、ArtifactManager、ProcessSupervisor、Operation、sidecar 协议 | 向 B 提供稳定投影；向 C 提供契约夹具、故障注入点和发布恢复工具 |
| **B：Shell、主题、首页与组件** | Token/主题、Shell、导航、新首页、模块卡片、状态徽标、Operation 进度、响应式和可访问性 | 与 A 共定状态呈现；向 C 提供模块中心可复用组件和视觉回归基线 |
| **C：模块中心、接线、测试与发布** | 模块中心信息架构、前后端接线、代表模块迁移、契约/E2E/回归、签名 Catalog、CI/Release、灰度与回滚演练 | 对 A/B 的接口做集成验收；维护发布清单与跨 Wave 风险台账 |

两人团队建议：A 独立负责后端主线；B 负责视觉与首页，C 职责由两人按“接线测试”和“发布迁移”拆分。任何情况下，发布签名和回滚演练不能成为“大家顺手做”的无 owner 工作。

## 5. Wave 0：共同契约与视觉基线

### 目标

冻结两条线共同消费的语义与视觉边界，使后续页面不依赖临时状态，不在批量迁移中反复改组件。

### 任务

- 清点官方模块、入口、现有 Enabled 配置、运行资源、权限与 owner。
- 定义 `ModuleCatalogItem`、四维 `ModuleState`、`Operation`、主操作和可见性投影。
- 定义首页与模块中心职责：首页是工作台摘要，模块中心是完整 Catalog 与管理入口。
- 固定双栏 Shell、五色板/明暗双轴主题、三档内容容器和响应式/可访问性基线。
- 建立状态/组件示例矩阵：正常、缺失、禁用、安装中、更新、崩溃、损坏、撤回、离线陈旧。
- 制定旧配置迁移、埋点/日志、契约版本和回滚原则。

### 依赖与分工

- **依赖：** 无；是所有后续 Wave 的门槛。
- **后端 A：** 模块与入口清单、状态枚举、Registry 投影草案、契约夹具。
- **前端 B：** Token/主题基线、Shell 信息架构、组件状态样本和设计图核对。
- **集成 C：** 契约测试骨架、验收矩阵、风险台账与里程碑发布清单。
- **可并行：** 模块清点与视觉基线可并行；状态命名和页面交互必须联合评审后冻结。

### 关键文件

- `internal/extapi/module.go`
- `internal/extapi/registry.go`
- `internal/app/app.go`
- `internal/app/composition_contract_test.go`
- `scripts/fixture/composition_contract.json`
- `frontend/src/App.vue`
- `frontend/src/constants/navigation.ts`
- `frontend/src/constants/status.ts`
- `frontend/src/styles/tokens.css`
- `frontend/src/styles/base.css`
- `frontend/src/styles/components.css`
- `docs/design/workbench-evolution/`

### 估算

- **P50/P80：8/13 人日**
- **2–3 人日历：P50 1–1.5 周；P80 2–3 周**

### DoD

- 模块、入口和 owner 清单可由测试夹具验证。
- 三个共同模型、状态转换、主操作及可见性规则有版本化契约。
- 首页/模块中心边界、主题继承和老用户偏好策略有明确决议。
- 设计基线覆盖 390px、200% 缩放、键盘与焦点，不依赖颜色区分状态。
- 后续 Wave 不需要另造第二套状态枚举即可开工。

### 风险

- 状态枚举过度设计，尚未用真实模块验证。
- 设计基线和现有 CSS Token 差异引发全仓回归。
- 模块清点遗漏托盘、热键、MCP 或后台任务等隐蔽入口。

## 6. Wave 1：主题/Shell 与安装状态底座

### 目标

在不改变模块物理交付方式的前提下，落地稳定 Shell/主题和逻辑安装状态底座，让所有入口开始消费同一真相。

### 任务

- 实现五色板与明暗主题、偏好迁移、系统主题跟随和防闪烁初始化。
- 收敛 Shell、导航组、活动态、窄屏行为与模块中心一级入口占位。
- 建立 Catalog/Registry 查询、逻辑 receipt、Enabled 迁移和四维状态投影。
- 将导航、搜索、托盘、热键、MCP 等入口接到统一可见性判断，先观测再强制。
- 建立前端状态 store/composable，但只缓存投影，不成为权威源。
- 增加主题、状态迁移和入口矩阵测试。

### 依赖与分工

- **依赖：** Wave 0 契约冻结。
- **后端 A：** Catalog 查询、逻辑安装态、旧 Enabled 迁移、可见性投影。
- **前端 B：** 主题与 Shell 落地、导航入口、主题偏好兼容。
- **集成 C：** Wails 接线、入口矩阵测试、老用户升级回归。
- **可并行：** 主题/Shell 与后端状态底座可并行；导航接线等待投影接口稳定。

### 关键文件

- `frontend/src/App.vue`
- `frontend/src/components/shell/`
- `frontend/src/constants/navigation.ts`
- `frontend/src/styles/tokens.css`
- `frontend/src/styles/base.css`
- `frontend/src/styles/components.css`
- `internal/extapi/registry.go`
- `internal/app/service.go`
- `internal/settings/paths.go`

### 估算

- **P50/P80：18/29 人日**
- **2–3 人日历：累计 P50 2–3 周；累计 P80 4–5 周**

### DoD

- 老用户主题和 Enabled 偏好无损迁移，Jade 等默认值不覆盖显式选择。
- Shell 在明暗主题、390px 和 200% 缩放下无关键内容阻断，键盘焦点可见。
- Installed、Enabled、Runtime、Health/Currency 可准确区分并由后端投影。
- 所有已知入口至少进入统一可见性观测；旁路命中有日志和测试。
- UI 明确说明逻辑卸载不会释放宿主程序空间。

### 风险

- 全局 Token 改动导致大量旧页对比度或布局回归。
- 双状态迁移期间 Enabled 与 receipt 不一致。
- Shell 改动和模块中心入口占位互相阻塞。

## 7. Wave 2：模块中心 MVP 与新首页 MVP

### 目标

用真实共同模型交付两个核心产品面：完整管理模块的模块中心，以及面向日常任务的新首页。

### 任务

- 模块中心实现分类、筛选、搜索、状态摘要、主操作、详情与诊断入口。
- 新首页实现常用功能、已安装/已启用摘要、最近任务、异常提醒和继续操作。
- 建立 `ModuleCard`、状态徽标、空态、错误态、骨架屏和 Operation 基础展示。
- 接通安装/启用/停用的逻辑操作，处理刷新恢复、并发点击和长文案。
- 将首页与完整 Catalog 严格分离，避免首页再次演化为模块列表。
- 完成 41 项与 100 项规模数据、明暗主题、窄屏及无障碍测试。

### 依赖与分工

- **依赖：** Wave 1 的 Catalog/State 查询、Shell 和主题可用。
- **后端 A：** 查询/操作 API、主操作推导、错误码和刷新恢复。
- **前端 B：** 新首页、公共卡片与状态组件、响应式和无障碍。
- **集成 C：** 模块中心页面、Wails 接线、规模/状态矩阵 E2E。
- **可并行：** 首页和模块中心并行，共用组件先以联合 story 固化；操作接线可在只读列表之后增量完成。

### 关键文件

- `frontend/src/views/HomeView.vue`
- `frontend/src/App.vue`
- `frontend/src/components/ui/`
- `frontend/src/components/shell/`
- `frontend/src/constants/status.ts`
- `internal/app/service.go`
- 计划新增：`frontend/src/views/ModuleCenterView.vue`
- 计划新增：`frontend/src/components/modules/`

### 估算

- **P50/P80：22/34 人日**
- **2–3 人日历：累计 P50 4–5 周；累计 P80 6–8 周**

### DoD

- 模块中心始终可进入，首页仍是工作台而不是完整 Catalog。
- 卡片主操作由统一投影决定，刷新后状态和进行中 Operation 可恢复。
- 缺失、禁用、运行、崩溃、损坏、撤回和离线状态均有明确文本及操作边界。
- 41/100 项规模下搜索、筛选和滚动可用；390px、200%、键盘和屏幕阅读器主路径通过。
- 主题切换不改变业务状态，不产生浅色/深色两套行为。

### 风险

- 后端 API 延迟迫使前端伪造状态。
- 卡片承载过多诊断信息，信息层级失控。
- 首页范围膨胀，挤占模块中心和公共组件工期。

## 8. Wave 3：统一调用门与公共组件收口

> **首个可发布里程碑。** Wave 0–3 完成后，可在仍以逻辑安装为主的前提下发布真实、统一、可回滚的工作台与模块管理体验。

### 目标

关闭所有调用旁路，完成生命周期排空与公共组件收口，使“界面显示不可用”和“后端确实不可执行”一致。

### 任务

- 所有 RPC、导航、搜索、托盘、热键、MCP、独立窗口和后台任务统一经过 `Acquire(moduleID)`。
- 实现 Operation lease、并发策略、有界 drain/cancel、停用与宿主退出清理。
- 统一 Operation 进度、恢复条、错误提示、危险确认和全局 `usePrompt/useConfirm`。
- 将首页、模块中心和首批代表页接入统一组件；清除重复状态判断和局部弹窗。
- 建立旁路穷举、竞态、强杀、资源残留、焦点与生命周期回归测试。
- 执行第一里程碑 beta/stable 发布与回滚演练。

### 依赖与分工

- **依赖：** Wave 2 页面可消费真实 Catalog/State；入口清单完整。
- **后端 A：** `Acquire`、租约、drain/cancel、资源清理和统一错误。
- **前端 B：** Operation/状态/确认组件收口，首页和代表页替换。
- **集成 C：** 全入口接线、旁路与 E2E 测试、首个里程碑发布。
- **可并行：** 不同入口接线可并行；生命周期核心实现与压力测试串行；组件替换可按页面分组并行。

### 关键文件

- `internal/extapi/registry.go`
- `internal/app/app.go`
- `internal/app/service.go`
- `internal/app/composition_contract_test.go`
- `internal/platform/windows/job.go`
- `internal/platform/windows/process.go`
- `frontend/src/components/ui/`
- `frontend/src/components/modules/`
- `frontend/src/views/HomeView.vue`
- 计划新增：`packages/go/operation/`

### 估算

- **P50/P80：24/38 人日**
- **2–3 人日历：累计 P50 6–8 周；累计 P80 9–12 周**

### DoD

- 未安装、停用或 blocked 模块无法从任何已知入口旁路执行。
- 停用和退出可有界 drain/cancel，无死锁；代表模块资源和受管孤儿进程为 0。
- Operation 在首页、模块中心和代表页语义一致，失败可诊断、可重试或明确回滚。
- 公共组件替换完成，模块卡片和页面不再复制业务状态机。
- 前端构建、Go 测试、契约测试、关键 E2E、可访问性和回滚演练通过。

### 风险

- 隐蔽入口遗漏，产生“UI 停用但仍可执行”的安全与信任问题。
- drain/cancel 竞态导致死锁、数据损坏或退出残留。
- 公共组件收口夹带全量页面重构，错过首个发布窗口。

## 9. Wave 4：共享托管内核与代表页面

### 目标

在不急于物理拆包的前提下，以不同托管形态的代表模块验证 ArtifactManager、ProcessSupervisor 与 Operation 共享内核，并让代表页面成为后续迁移模板。

### 任务

- 建立 ArtifactManager：下载、校验、staging、receipt、active pointer、更新与回滚。
- 建立 ProcessSupervisor：JobObject、受管/外部/脱管实例识别、启动停止和退出清理。
- 将持久化 Operation 与 journal 恢复、故障注入、诊断信息接通。
- 选择至少两个差异样本验证内核，优先 MarkerOn 与 Rufus；页面接入统一状态和组件。
- 验证 Windows 文件锁、断网、磁盘满、校验失败、强杀和旧版本恢复。
- 建立共享内核 API 稳定门和逃生钩子审查规则。

### 依赖与分工

- **依赖：** Wave 3 的统一调用门和 Operation；代表模块 owner 可投入。
- **后端 A：** Artifact、Supervisor、journal/receipt、故障恢复。
- **前端 B：** 代表页面、诊断/恢复展示和托管状态组件。
- **集成 C：** MarkerOn/Rufus 迁移、故障注入、安装更新回滚矩阵。
- **可并行：** ArtifactManager 与 ProcessSupervisor 可先并行；第一样本整合后再接第二样本，禁止两个团队各自复制一套内核。

### 关键文件

- `internal/modules/markeron/`
- `internal/modules/rufus/`
- `internal/modules/ocr/hosted.go`
- `internal/modules/litemonitor/version/manager.go`
- `internal/platform/windows/job.go`
- `internal/platform/windows/process.go`
- 计划新增：`packages/go/artifact/`
- 计划新增：`packages/go/supervisor/`
- 计划新增：`packages/go/operation/`

### 估算

- **P50/P80：30/47 人日**
- **2–3 人日历：累计 P50 10–14 周；累计 P80 15–20 周**

### DoD

- 两个差异样本不再复制下载、校验、版本切换和进程治理主流程。
- 任一更新阶段失败后旧版本仍可用；journal 强杀恢复和故障注入通过。
- 外部、受管和脱管实例可区分，宿主退出后受管孤儿进程为 0。
- 代表页面只消费共同状态与 Operation，诊断信息足以定位失败阶段。
- 共享内核无任意命令/脚本钩子；新增例外必须经过架构评审。

### 风险

- 单一样本驱动的假抽象，在第二样本出现大量分支。
- Windows 文件锁与杀进程时序造成半安装或半卸载。
- JobObject 被误当成安全沙箱，掩盖权限边界不足。

## 10. Wave 5：声明式托管与批量页面收敛

### 目标

把已验证的托管策略变成受控声明，按策略族迁移普通托管模块，同时统一其页面结构和发布治理。

### 任务

- 定义受控 manifest、官方 Catalog schema、签名、撤回和兼容校验。
- manifest 仅允许宿主内建的枚举策略，禁止任意脚本、命令钩子与远程 UI。
- 依次迁移 MarkerOn、Rufus、QuickLook，再按 portable zip、单 exe、MSI/MSIX、窗口/互斥体/命令通道等策略族扩展。
- 将批量页面收敛到状态头、主操作、版本、Operation、日志/诊断和危险区模板。
- 建立每个策略族的安装、更新、回滚、启动、停用、卸载契约测试。
- 按小批 beta 灰度，短期保留旧路径并设置明确移除门槛。

### 依赖与分工

- **依赖：** Wave 4 双样本稳定，共享内核 API 达到冻结门槛。
- **后端 A：** schema、签名验证、策略解释器和兼容/撤回逻辑。
- **前端 B：** 声明式模块详情模板与批量代表页收敛。
- **集成 C：** Catalog 生成签名、策略族迁移、灰度和契约矩阵。
- **可并行：** schema/签名链与页面模板可并行；同一策略族先做一个黄金样本，再并行迁移其余模块。

### 关键文件

- `internal/modules/quicklook/`
- `internal/modules/markeron/`
- `internal/modules/rufus/`
- 计划新增：`packages/go/modulecatalog/`
- 计划新增：`schemas/managed-module.schema.json`
- 计划新增：`schemas/official-catalog.schema.json`
- 计划新增：`schemas/transaction-journal.schema.json`
- `frontend/src/components/modules/`

### 估算

- **P50/P80：38/60 人日**
- **2–3 人日历：累计 P50 15–22 周；累计 P80 22–30 周**

### DoD

- 首批模块由签名声明驱动，篡改 Catalog 或 manifest 必须拒绝。
- 任意脚本、任意命令入口和远程 UI 为 0；未知策略默认拒绝。
- 每个策略族至少一个黄金样本通过完整生命周期与故障恢复测试。
- 批量页面使用共同模型和模板，业务差异通过受控插槽表达，不复制基础状态机。
- 每批迁移可独立回滚，旧路径删除条件和日期明确。

### 风险

- manifest 膨胀成脚本语言，安全边界和可测试性失控。
- 批量迁移同时触发多模块回归，定位困难。
- 页面模板追求“一页适配全部”而产生不可维护条件森林。
- 签名撤回误伤正常版本或离线用户无法恢复。

## 11. Wave 6：OCR/FileShare 物理拆包与多产物发布

### 目标

以 OCR 为黄金样本、FileShare 为网络边界样本，完成官方 sidecar 物理拆包；建立 host、声明包、sidecar 与 Catalog 的独立构建、签名、发布、撤回和兼容治理。

### 任务

- 定义最小 sidecar protocol、能力协商、Host API 权限、host × sidecar 兼容矩阵。
- OCR 先拆：迁移私有引擎资产、安装/升级/回滚、崩溃隔离和用户数据保留。
- OCR 稳定后拆 FileShare：明确监听地址、鉴权、网络权限、防火墙、数据目录与卸载语义。
- clean host 移除已拆模块重型后端和资源，验证包体与运行依赖真实分离。
- 建立 `.hxmanaged`、`.hxmodule`、Catalog、host 的多产物 CI/Release、SBOM、provenance 与签名链。
- 演练断网、错误撤回、密钥轮换、旧 host/新 sidecar、更新失败和宿主回滚。
- OCR/FileShare 经至少两个稳定发布周期且无 P0/P1 事故后，才评估扩大拆包；WSL 另立 ADR，不进入本 Wave 默认范围。

### 依赖与分工

- **依赖：** Wave 5 的签名 Catalog、共享内核和声明式发布链稳定。
- **后端 A：** sidecar protocol、Host API、OCR/FileShare 进程与数据生命周期。
- **前端 B：** sidecar 权限、下载、恢复、网络风险和诊断界面。
- **集成 C：** 多产物构建签名、兼容矩阵、灰度、撤回和发布演练。
- **可并行：** 多产物 CI 骨架与 OCR 协议可并行；FileShare 实施必须等待 OCR 黄金路径稳定，安全评审可提前。

### 关键文件

- `internal/modules/ocr/`
- `internal/modules/fileshare/`
- `embedassets.go`
- `Taskfile.yml`
- `build/Taskfile.yml`
- `build/windows/Taskfile.yml`
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- 计划新增：`packages/go/sidecarprotocol/`
- 计划新增：`schemas/sidecar-manifest.schema.json`

### 估算

- **P50/P80：48/75 人日**
- **2–3 人日历：累计 P50 20–28 周；累计 P80 28–38 周**

### DoD

- clean host 不再携带已拆 OCR/FileShare 的重型后端和资源，包体差异可验证。
- sidecar 可独立安装、启用、更新和回滚；失败不拖垮宿主，退出后孤儿进程为 0。
- 卸载真实移除 sidecar 程序与资源，默认保留用户数据，不误删外部安装软件。
- 未授权 Host API 默认拒绝；FileShare 网络暴露、鉴权和防火墙行为通过安全测试。
- host、`.hxmanaged`、`.hxmodule` 与 Catalog 可独立构建、签名和追溯；撤回、密钥轮换、离线与回滚完成演练。
- 私有 OCR 资产不进入公开仓库、公开构建缓存或公开 Release。

### 风险

- sidecar 协议过早固化，跨版本兼容成本失控。
- OCR 私有资产误入公开产物或日志。
- FileShare 网络边界、鉴权或目录权限出现高危缺陷。
- 数据 schema 不可逆，阻断 host 或 sidecar 回滚。
- 多产物签名、撤回和密钥轮换流程人为失误。

## 12. 关键路径与并行关系

### 12.1 关键路径

```text
共同模型与入口清单
  → Registry/Catalog 状态投影
  → 模块中心与首页接真实状态
  → 统一 Acquire + drain/cancel
  → 双样本共享托管内核
  → 受控声明与签名 Catalog
  → OCR sidecar 黄金样本
  → FileShare 网络样本
  → 多产物 stable 发布
```

任一节点未达到 DoD，不得用更多页面或更多模块迁移掩盖基础问题。

### 12.2 可并行关系

- Wave 0：视觉基线可与模块/入口清点并行，契约冻结点汇合。
- Wave 1：主题/Shell 与 Registry/Catalog 底座并行，导航接线后置。
- Wave 2：首页与模块中心并行，共享组件由 B 统一 owner。
- Wave 3：入口接线按导航、托盘、热键、MCP、窗口分组并行；生命周期核心不可分叉实现。
- Wave 4：Artifact 与 Supervisor 可并行，第二代表样本须复用第一样本形成的内核。
- Wave 5：策略族之间可并行，但每族先串行完成黄金样本。
- Wave 6：OCR 与多产物流水线骨架可并行；FileShare 只在 OCR 稳定后进入实现。

## 13. 发布节奏

1. **Wave 0–1 内部版本：** 契约和 Shell/状态底座仅进 dev，不向用户承诺“按需瘦身”。
2. **Wave 2 可用性预览：** 模块中心与新首页进入 dev/beta，重点收集信息架构、长状态和入口一致性问题。
3. **Wave 3 第一正式里程碑：** 统一调用门、资源清理、公共组件和回滚演练通过后进入 stable。
4. **Wave 4 平台预览：** 代表模块先 dev，再 beta；共享内核不因单个成功样本直接全量推广。
5. **Wave 5 小批迁移：** 每个策略族独立灰度，观察一个发布周期后移除旧路径。
6. **Wave 6 分阶段发布：** OCR dev → beta → stable；至少一个稳定周期后才开始 FileShare beta；两者各自满足两个稳定周期后再评估扩大拆包。

渠道纪律：

- `dev` 使用测试根签名，可高频验证。
- `beta` 面向候选用户，保留快速撤回和旧路径回滚能力。
- `stable` 使用正式签名，必须完成恢复、撤回、密钥与离线演练。
- 声明包不得引用当前 stable host 未知策略；sidecar 必须通过 host × sidecar 兼容矩阵。
- 新敏感权限、许可证变化和不可逆迁移不得静默更新。

## 14. 风险矩阵与停止条件

| 风险 | 概率/影响 | 预警指标 | 控制措施 | 停止条件 |
|---|---|---|---|---|
| 调用门遗漏 | 中/高 | 停用后仍可从任一入口执行 | 入口清单、统一 `Acquire`、旁路契约测试 | 发现任一可执行旁路，停止 stable 发布 |
| drain/cancel 死锁或残留 | 中/高 | 停用超时、退出残留进程/端口 | 有界超时、JobObject、竞态与强杀测试 | 代表模块出现不可恢复死锁或孤儿进程，停止扩大接线 |
| Token/Shell 全仓回归 | 中/中 | 对比度、焦点、窄屏阻断激增 | 视觉基线、代表页矩阵、分批迁移 | 主路径在 390px/200%/键盘下不可用，停止主题推广 |
| 前端第二份状态 | 中/高 | 刷新后 UI 与后端不一致 | 单一投影、契约类型、禁止局部推导 | 出现用户可见状态冲突，停止新增页面 |
| 共享内核假抽象 | 中/高 | 逃生钩子或模块特判持续增加 | 双样本、枚举策略、架构评审 | 第二样本需大量复制/任意钩子，停止批量迁移并回退设计 |
| journal/文件锁恢复失败 | 中/高 | orphan receipt、active pointer、半卸载 | rename-first、故障注入、启动恢复 | 更新失败后旧版不可用或数据不一致，停止 beta |
| manifest 脚本化 | 中/高 | 新需求要求任意命令/脚本 | 受控枚举、未知策略拒绝 | 必须开放任意执行才能迁移时，停止声明式扩展并立 ADR |
| 批量迁移回归 | 中/中 | 多模块同步失败、回滚困难 | 按策略族和小批灰度、旧路径限期保留 | 单批出现 P0/P1 或无法独立回滚，停止下一批 |
| sidecar 协议失控 | 中/高 | 频繁破坏兼容、Host API 扩张 | 最小协议、能力协商、兼容矩阵 | 无法支持前后各一稳定版本，停止 FileShare 拆包 |
| FileShare 网络风险 | 中/极高 | 未鉴权监听、越权目录访问 | 默认本机、显式授权、安全测试 | 任一未授权远程访问或目录越权，立即停止发布 |
| OCR 私有资产泄露 | 低/极高 | 公开仓库/缓存/Release 命中资产 | 私有构建源、产物扫描、发布门禁 | 任一公开泄露迹象，立即停止流水线并执行凭据/产物处置 |
| 签名/撤回误操作 | 低/极高 | 正常版本被拒、恶意版本未撤回 | 双人复核、演练、审计和分环境根 | 无法安全区分 dev/beta/stable 或完成撤回，停止多产物 stable |
| 数据不可逆 | 中/高 | 回滚后旧版本无法读数据 | schema 版本化、前向兼容、默认保留 | 迁移无可验证回滚路径，停止升级发布 |

全局停止条件：

- 契约仍在高频破坏性变化，却开始批量页面或模块迁移。
- P0/P1 数据、安全、供应链或网络事故未完成根因和防复发验证。
- 测试只能验证 happy path，故障注入和回滚长期跳过。
- 为赶日历需要取消签名、权限、数据保留或调用门约束。
- 连续两个 Wave 的 P80 被突破，应停止追加范围并重估余下路线。

## 15. 原子提交与分支纪律

- 每个提交只承担一个可描述、可回滚的职责，使用中文 Conventional Commit，例如：
  - `feat(module): 新增模块四维状态投影`
  - `refactor(frontend): 收口模块状态与操作反馈组件`
  - `test(module): 补充停用调用门旁路矩阵`
- 契约、后端实现、前端接线和批量迁移可拆成连续原子提交，但每个提交必须保持前后端可构建；必要时使用兼容适配层，不提交半套破坏性接口。
- 不把主题全仓格式化、模块业务迁移和发布配置混入同一提交。
- 每个 Wave 使用短生命周期分支；共享契约先合并，页面和模块分支从已冻结契约同步，避免多分支各自改枚举。
- 代表模块一次只迁移一个；策略族批量迁移也按模块拆分提交，便于 `git revert`。
- 禁止提交构建产物、私有 OCR 资产、临时日志、测试下载缓存和本机 receipt。
- 未通过对应 Wave DoD 不合入 stable 发布分支；禁止以关闭测试、跳过签名或保留无限期 feature flag 换取合并。
- 涉及 CI/Release、签名、密钥或公开发布的变更按项目安全规则单独评审和明确授权。

## 16. 验收、测试与回滚

### 16.1 分层验收

- **契约层：** Catalog/State/Operation schema、状态转换、主操作、可见性和兼容性夹具。
- **后端层：** Registry、调用门、并发租约、drain/cancel、journal 恢复、Artifact、Supervisor、sidecar Host API。
- **前端层：** 明暗主题、五色板、组件状态矩阵、长文案、空/错/加载态、390px、200%、键盘、焦点和屏幕阅读器。
- **集成层：** RPC、导航、搜索、托盘、热键、MCP、窗口、后台任务全入口；刷新、重启、断网和并发操作。
- **平台层：** Windows 文件锁、JobObject、外部/受管/脱管进程、磁盘满、强杀、休眠/恢复和退出残留。
- **发布层：** clean install、旧版升级、签名验证、SBOM/provenance、灰度、撤回、离线、密钥轮换和最终资产 smoke。

### 16.2 每 Wave 最低测试门

1. Go 单测、竞态敏感路径测试和 composition contract 通过。
2. 前端类型检查、单测、构建与关键 E2E 通过。
3. 共同模型 fixture 在 Go 与 TypeScript 两端一致。
4. 新增状态至少覆盖成功、失败、取消、刷新恢复和无权限。
5. UI 变更完成明暗主题、窄屏、200% 与键盘验收。
6. 托管变更完成断网、校验失败、文件锁、强杀与旧版恢复。
7. 发布候选在干净 Windows 环境和真实升级路径各 smoke 一次。

### 16.3 回滚策略

- **Wave 1–3：** 保留旧配置读取和兼容写入窗口；新 Shell/首页可通过版本回滚恢复，但不得让后端状态降级为前端布尔值。逻辑 receipt 迁移必须幂等。
- **Wave 4：** 更新采用 staging + 校验 + 原子 active pointer；失败自动回到旧资产。journal 启动恢复必须处理进程中断。
- **Wave 5：** 每批模块保留有期限的旧托管路径；Catalog 可撤回有问题版本，但不得删除设备上的最后可用版本。
- **Wave 6：** host 与 sidecar 分别可回滚；数据 schema 保持前向兼容或提供经过演练的逆迁移。卸载默认保留用户数据，模块程序、上游软件、缓存和数据分别确认。
- **发布事故：** 先冻结渠道和撤回 Catalog，再恢复最后已知良好产物；签名或私有资产事故按安全事件处理，不以普通版本回滚代替。

## 17. 决策门与完成定义

- **进入 Wave 1：** 共同模型、入口清单和视觉基线完成评审。
- **进入 Wave 2：** Catalog/State 能提供真实只读投影，Shell 与主题主路径稳定。
- **进入 Wave 3：** 模块中心和首页不依赖伪造状态，公共组件已形成可复用基线。
- **发布第一里程碑：** 所有调用旁路关闭、生命周期有界、全量门禁通过。
- **进入 Wave 4：** Operation 和错误恢复语义稳定，代表模块 owner 与测试资源到位。
- **进入 Wave 5：** 两个差异样本证明共享内核，更新失败可恢复旧版。
- **进入 Wave 6：** 签名 Catalog、撤回和策略族迁移经稳定版本验证。
- **路线完成：** OCR/FileShare 物理拆包、多产物发布、回滚与安全演练全部达到 Wave 6 DoD；后续模块扩展作为新的路线评审，不自动纳入本排期。

本路线的首要成功标准不是“页面换完”或“模块拆得越多越好”，而是：用户在任一入口看到的状态、能执行的动作、后台真实生命周期和发布可恢复性始终一致。