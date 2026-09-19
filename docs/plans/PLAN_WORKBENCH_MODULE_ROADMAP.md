# Hanxi 工作台界面与官方模块化联合总排期

> 状态：执行路线；2026-09-18 依据 [项目缘起](../MOTIVATION.md) 完成受众校准（v2）。校准只动优先级与启动条件，Wave 0–3 已冻结契约与在建内容不回头。
>
> **2026-09-19 执行进度与范围裁剪（[ADR-0003](../adr/ADR-0003-scope-cuts-for-personal-nas-first-tool.md)，以此为准）**：Wave 0–4 全部落地并提交；Wave 5 的托管模块去重复已完成（版本面 20 家委托 artifact、instance 面 18 家委托 supervisor，quicklook/bili23 仅版本面迁入、其 instance 与 frpc/rustdesk/subnetdesk 按 ADR-0002 §3/ADR-0003 §4 留 bespoke），其**签名 Catalog / 声明式 manifest / key 轮换相关 DoD 整体 N/A**；Wave 6 物理拆包与多产物发布降级为**条件触发项**（宿主体积或真实发布通道出现时重开），不再按波次排期。
>
> 专项文档：[界面与主题优化计划](./PLAN_UI_THEME_REFINEMENT.md) · [官方模块分发计划](./PLAN_OFFICIAL_MODULE_DISTRIBUTION.md) · [为什么做 Hanxi](../MOTIVATION.md)
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

### 1.2 受众前提与校准口径（2026-09-18 新增）

本路线的受众前提已由 [MOTIVATION.md](../MOTIVATION.md) 明确：**第一且最重要的用户是作者本人，且是"多台机器上的同一个本人"**——通过飞牛同步程序目录与数据目录，实现"软件装在 NAS 里、随身可用"。开源与外部用户是顺带可能，不是投入前提。

由此确立全路线统一的取舍口径：

1. **直接服务"随身/同步/新电脑上手"的投入升权**：数据根可携带、机器绑定项分类与首启重锚、托管资产跨机复用、逻辑安装态准确——这些决定"换电脑爽不爽"。
2. **持续纳管新工具的成本升权**：作者会不断从 GitHub 挑软件托管进来，声明式 manifest（不含签名链）能把"集成一个工具"从写代码降到写配置，个人价值实打实。
3. **为陌生用户建的供应链治理降权为条件项**：签名目录、撤回、密钥轮换、SBOM/provenance、多产物发布与渠道演练，在出现真实外部分发（开源正式发布/第二个用户）之前不启动；相关 DoD 与日历从承诺改为按需。
4. **验收新增一个原始场景**：任何 Wave 的 DoD 都不排斥回答"同步到第二台机器后首次启动会发生什么"；该场景在 §16 成为固定验收项。

## 2. 联合共同模型

共同模型是两份专项的交界面。命名可在实现阶段按 Go/TypeScript 风格调整，但职责不得拆成多套互相推导的前端状态。

> **权威源提示（校准新增）：** 本节 TS 伪代码是概念示意，契约冻结事实以 [ADR-0001](../adr/ADR-0001-workbench-module-contract.md) 与 `internal/extapi/` 为准（实现已强于本节：`primaryAction` 已枚举化、入口集已含 RPC/后台任务、目录项含 `owner`）。消费方会话一律读 ADR，不照抄本节。

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

校准后（v2）的里程碑总表。实际执行形态为**单人 + 多 AI 会话**，因此"单人"列为操作口径，多人列仅作容量参考：

| 联合范围 | 对应 Wave | 2–3 人 P50 | 2–3 人 P80 | 单人 P50 | 单人 P80 | 可交付结果 |
|---|---|---:|---:|---:|---:|---|
| 第一里程碑 | Wave 0–3 | 6–8 周 | 9–12 周 | **10–14 周** | **15–20 周** | 新 Shell/主题、模块中心 MVP、新首页 MVP、统一调用门与公共组件，可发布 |
| 随身可用 | Wave 0–4S（内核＋同步亲和包） | 11–15 周 | 16–22 周 | **16–20 周** | **23–29 周** | Artifact/Process/Operation 内核＋"同步到第二台机器即可用"验收通过 |
| 纳管成本收口 | Wave 0–5a（声明式，不含签名链） | 14–20 周 | 21–28 周 | **20–26 周** | **29–37 周** | 新 GitHub 工具按受控 manifest 纳管，批量页面收敛；外部下载完整性靠哈希核对即可 |
| 外部分发（条件启动） | Wave 5b/6（签名链、物理拆包、多产物） | — | — | — | — | **不计入承诺日历**。启动触发：出现真实外部用户/正式发布决定，或作者明确授权。启动时按当时仓库状态重估 |

> 校准说明：原表"Wave 0–6 完整执行 7–9 个月（单人）"的终点从承诺改为条件项；原"加声明式迁移"行隐含的签名 Catalog 前置被拆出（见 §10 的 5a/5b 分割）。**随身可用（0–4S）是本项目的价值顶点**——此后每一分投入都需要用"下一个用户"来辩护。

**这不是把界面专项的 40–64 人日与模块专项的 108–172 人日直接相加。** 两份专项存在 Shell、首页、状态组件、Operation 展示、代表页迁移、测试与发布演练等重叠工作；联合排期会合并这些任务，同时新增跨线契约、集成缓冲和阶段发布成本。因此应以 Wave 的资源负载和里程碑日历管理，不以 `40–64 + 108–172` 得出承诺日期。各 Wave 人日用于容量规划，也不应机械求和后再除以人数。

## 4. 人员分工

| 角色 | 主责 | 次责与交接 |
|---|---|---|
| **A：后端状态与生命周期** | Catalog/Registry、`ModuleState`、统一 `Acquire` 调用门、drain/cancel、ArtifactManager、ProcessSupervisor、Operation、sidecar 协议 | 向 B 提供稳定投影；向 C 提供契约夹具、故障注入点和发布恢复工具 |
| **B：Shell、主题、首页与组件** | Token/主题、Shell、导航、新首页、模块卡片、状态徽标、Operation 进度、响应式和可访问性 | 与 A 共定状态呈现；向 C 提供模块中心可复用组件和视觉回归基线 |
| **C：模块中心、接线、测试与发布** | 模块中心信息架构、前后端接线、代表模块迁移、契约/E2E/回归、签名 Catalog、CI/Release、灰度与回滚演练 | 对 A/B 的接口做集成验收；维护发布清单与跨 Wave 风险台账 |

两人团队建议：A 独立负责后端主线；B 负责视觉与首页，C 职责由两人按“接线测试”和“发布迁移”拆分。任何情况下，发布签名和回滚演练不能成为“大家顺手做”的无 owner 工作。

> **实际形态重述（校准）：** 本项目为**单人 + 多 AI 会话**执行——A/B/C 是会话职责切面而非三个人，"联合评审"这道保险不存在，由契约测试 + fixture + ADR 承担同等职能（现状已如此，如 cataloggen 漂移守护、2205 组合穷举）。因此唯一不可让渡给任何会话的人类职责是：**冻结点的最终拍板，以及（若 5b 未来启动）签名私钥与发布授权**。

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

### 校准备忘（2026-09-18 登记;状态注记 2026-09-19）

1. **`delivery` 一词两义**：Catalog 的 `delivery` 是交付形态（`builtin-logical`/…），`ModuleState` 的 `delivery` 是安装生命周期（`absent`/`installing`/…），实现已继承该碰撞。模块中心卡片同时消费两个模型的 `delivery` 字段，语义正交、JSON key 相同。建议冻结前把状态维改名（如 `installation`），交付形态保留 `delivery`；ADR-0001 §1.3 措辞同步。改名成本现在≈一个类型重命名，Wave 2 卡片/徽标铺开后再改就是全量替换。**状态(09-19):✅ 已解决**——方向与本备忘相反:状态侧 `delivery` 保留,catalog 侧改名 `deliveryKind`(commit 47969da,fixture/receipt kind 对齐、爆炸半径更小;ADR-0001 已落修订注,schema 维持 1)。碰撞消解,此议翻篇,后续会话勿再提 installation。
2. **Operation 双职责边界**：`install/update` 类是持久化长事务（journal 可恢复、一次一个），`invoke` 类是高频短租约（进程死即失效）。Wave 4 做 journal 前必须在 ADR 补冻结：**持久化范围是否排除 `kind: 'invoke'`**；未定义前 Wave 1–3 内存事务不受影响，不得提前把 invoke 写入恢复面。**状态(09-19):✅ 已冻结**——invoke/activate/stop 排除在 journal 恢复面之外,由 `packages/go/operation` Hub 映射与故障注入测试钉死("租约终态误触 journal"即红)。
3. **本文 §2 TS 伪代码回填**：实现（枚举化 primaryAction、RPC/background 入口、owner 字段）已领先文档。§2 已加权威源提示；是否回填伪代码由契约 owner 顺手决定，不阻塞任何 Wave。

## 6. Wave 1：主题/Shell 与安装状态底座

### 目标

在不改变模块物理交付方式的前提下，落地稳定 Shell/主题和逻辑安装状态底座，让所有入口开始消费同一真相。

### 任务

- 实现五色板与明暗主题、偏好迁移、系统主题跟随和防闪烁初始化。
- 收敛 Shell、导航组、活动态、窄屏行为与模块中心一级入口占位。
- 建立 Catalog/Registry 查询、逻辑 receipt、Enabled 迁移和四维状态投影。
- 将导航、搜索、托盘、热键、MCP 等入口接到统一可见性判断，先观测再强制。
- 建立前端状态 store/composable，但只缓存投影，不成为权威源。
- **（校准新增·同步亲和基线）** 清点数据根内"机器绑定项 vs 可携带项"：DPAPI 凭据、绝对路径、索引缓存、receipt 各归其类；定义"同步到新机首启"行为——可携带项直接可用，绑定项给出显式重锚/重录提示，禁止静默半可用。
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
- （校准新增）把数据根整包拷到第二台 Windows 机器/第二个用户首次启动：无崩溃、无误判"已安装"，机器绑定项均有显式提示与恢复路径；此场景进 §16 固定回归。

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
- **（校准新增·4S 同步亲和）** 托管资产跨机复用：A 机下载的便携/托管文件同步到 B 机后校验复用、免重下；校验失败自动回退重下载。journal/receipt/active pointer 的恢复逻辑显式处理"目录来自另一台机器"的初始状态。
- **（校准新增·前置裁决）** 落实 Wave 0 备忘 2：journal 持久化范围与 `kind: 'invoke'` 的排除决定写入 ADR。

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
- （校准新增）**"同步可用"黄金路径通过**：把完整数据根同步到一台干净第二机器，首启后已安装模块即可用、托管工具免重下、绑定项按提示重录；全程无静默损坏。这是 0–4S 的终点验收，未过不得宣称"软件在 NAS 上了"。

### 风险

- 单一样本驱动的假抽象，在第二样本出现大量分支。
- Windows 文件锁与杀进程时序造成半安装或半卸载。
- JobObject 被误当成安全沙箱，掩盖权限边界不足。

## 10. Wave 5：声明式托管与批量页面收敛（校准拆分为 5a/5b）

### 目标

把已验证的托管策略变成受控声明，按策略族迁移普通托管模块，同时统一其页面结构和发布治理。

### 任务

- 定义受控 manifest、官方 Catalog schema 与兼容校验（5a）；签名与撤回链属 5b 条件项，见本节末"校准拆分"。
- manifest 仅允许宿主内建的枚举策略，禁止任意脚本、命令钩子与远程 UI。
- 依次迁移 MarkerOn、Rufus、QuickLook，再按 portable zip、单 exe、MSI/MSIX、窗口/互斥体/命令通道等策略族扩展。
- 将批量页面收敛到状态头、主操作、版本、Operation、日志/诊断和危险区模板。
- 建立每个策略族的安装、更新、回滚、启动、停用、卸载契约测试。
- 每批迁移短期保留旧路径并设置明确移除门槛（"灰度到陌生用户渠道"属 5b）。

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

- 首批模块由受控声明驱动，篡改 manifest 必须拒绝（"由**签名**声明驱动"属 5b）。
- 任意脚本、任意命令入口和远程 UI 为 0；未知策略默认拒绝。
- 每个策略族至少一个黄金样本通过完整生命周期与故障恢复测试。
- 批量页面使用共同模型和模板，业务差异通过受控插槽表达，不复制基础状态机。
- 每批迁移可独立回滚，旧路径删除条件和日期明确。

### 风险

- manifest 膨胀成脚本语言，安全边界和可测试性失控。
- 批量迁移同时触发多模块回归，定位困难。
- 页面模板追求“一页适配全部”而产生不可维护条件森林。
- 签名撤回误伤正常版本或离线用户无法恢复。（5b 风险，5a 不触发）

### 校准拆分（2026-09-18）

- **5a 声明式纳管（按原计划启动）**：受控 manifest、枚举策略、兼容校验、策略族迁移、批量页面收敛。动机变化使其价值**上升**：作者会持续从 GitHub 挑工具纳管，5a 把"集成一个工具"从写代码降到写配置——服务的是自己，不是假想市场。完整性用哈希核对兜底即可（现状已如此）。
- **5b 签名与撤回治理（条件启动）**：签名 Catalog、撤回链、密钥管理、灰度渠道、双人复核。**触发条件**：出现真实外部用户、决定正式发布开源版本、或作者明确授权，三者其一。5b 启动前，本地/内网同步场景不因缺签名而阻塞任何 5a 能力。
- 本节人日估算（38/60）含 5a+5b，拆出 5b 后 5a 约占三分之二；总表已按此重排。

## 11. Wave 6：OCR/FileShare 物理拆包与多产物发布

> **校准状态（2026-09-18）：整体降级为条件 Wave，与 5b 同一触发条件。** 理由：① 同步随身场景下，包体瘦身的收益从"每个用户下载"变成"一次同步多占点盘"，权重骤降；② 私有 OCR 资产不进公开仓库这条底线由现有发布流程与产物扫描守住，不依赖拆包；③ 拆包的真正残余价值（崩溃隔离、独立更新、Host API 权限面）对一个单机自用者收益薄，而 sidecar 协议 + 兼容矩阵 + 多产物 CI 是全路线最重的治理面。**若长期无外部分发，本 Wave 可以永久不做，且不算路线失败。** 本节内容作为已评审的预案保留，启动时按当时仓库状态重估。

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

## 11A. Wave 4X：外部实例控制与退出兜底（2026-09-19 新增，机主确认制启动）

> **状态（2026-09-19）：延期。** 机主裁决:工作台/模块化收尾(页面收敛批次)全部入库前不启动,届时以新任务书重新对齐排期;等待期内**两侧零落码**(含 ADR 修订、词表扩维、supervisor 通道)。本节任务书内容保持有效,作为下次启用的底稿。

### 目标

托管目录内"外部运行"的实例对机主**可唤回窗口、可请求退出(含强杀)**;同步收口 §44"退出漏杀"旧案。本批全部新增逻辑服从一条家规:**hanxi 可以管不了,不可以管了还说谎。**

### 安全前提(随 ADR 登记,是全批授权的地基)

本机对托管软件**单装、单实例**;外部实例出身只有桌面快捷方式与 hanxi 两种,身份无歧义。若未来引入多用户、双实例或外部分发,本前提失效,以下所有放权须重评。

### 任务

- **外部唤窗**:解除"外部仅观测"政策闸门,FocusWindow/信使链路 pid 集扩至外部实例(复用现成实现,基本只拆不建)。
- **外部退出**:普通确认框(不做 busy 检测、不做长按/敲字确认、不按模块脾气分档——机主自判)→ WM_CLOSE → 宽限 → 强杀。**三铁律**:
  1. **圈地**:强杀范围锁死为 exe 路径位于托管 `versions/` 目录内的进程;
  2. **验身**:动手前复核 PID + 进程创建时刻 + 完整路径三件套,任一不符不动手;目标完整性级别高于本进程时不尝试,如实提示"管理员实例,管不动";
  3. **验尸**:动作收尾必须探针复查,结果如实上墙——"已退出"或"没退干净,残留 N 个"(附二次清理);**禁止静默报成功**。
- **§44 收口**:自家实例退出路径补"进程树扫描补杀 + 残留 Operation 告警";**必须尊重各模块随退/留守策略,留守的绝不杀**。
- **契约仪式**:ADR 修订 ADR-0001"外部仅观测"语义(记录前提与三铁律);primaryAction/状态词表 Go/TS 两端扩维、穷举测试长大,禁止卡片层 if 绕过;故障注入四例进测试门(路径出圈→不动 / 身份不符→不动 / 权限够不着→如实提示 / 杀后残留→UI 显式报)。

### 明确不做(防复活条款)

收养(adopt)、启动对账收尸(账本方案)、按模块退出脾气分档——均经论证砍除;后续任何会话不得以"完善"为名加回。`delivery` 命名碰撞已由 catalog 侧 `deliveryKind` 消解(commit 47969da),**禁止再提 installation 改法**。

### 依赖与协调

与前端收敛批 0(components/managed 契约面)共享词表,排期以后端契约会话批次为准对齐;**启动门槛 = 机主亲口确认**(行为政策变更,任何会话转述不生效)。

### 估算与 DoD

P50 **5–8 人日**(大头是闸门拆除与链路复用;新增仅在验身三件套、复查上墙)。DoD:故障注入四例全绿;真机"启动托管 → 退出 hanxi(随退开/关两态)→ 重启 → 对残留执行唤窗/退出"全流程 UI 与进程实况一致;§44 复现清单转常规回归。

## 12. 关键路径与并行关系

### 12.1 关键路径

```text
共同模型与入口清单
  → Registry/Catalog 状态投影
  → 模块中心与首页接真实状态
  → 统一 Acquire + drain/cancel
  → 双样本共享托管内核 ＋ 同步亲和收口   ← 到此为"随身可用"，价值顶点
  → 受控 manifest（不含签名链）
  —— 以下为条件路径，仅当真实外部分发启动 ——
  → 签名 Catalog 与撤回治理
  → OCR sidecar 黄金样本 → FileShare 网络样本
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

> **校准调整：** 自用场景下"发布"的成本只是更新一个 exe，渠道纪律的主要作用是给未来开源留底子，不必因此扣留已完成的体验。**Wave 1 的纯视觉部分（主题/五色板/Shell 骨架）不依赖状态底座，可在 Wave 1 结束先自用转正一次**（全仓 Token 回归在有真实日常使用的机器上早暴露，好过压到第一里程碑一起炸）；"不向用户承诺按需瘦身"的约束不变。

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
- 在不存在的外部用户面前，为假想外部用户建供应链治理（签名、撤回、SBOM、密钥轮换、渠道演练）——这是 5b/6 的启动条件缺失，不是进度。
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
- **平台层：** Windows 文件锁、JobObject、外部/受管/脱管进程、磁盘满、强杀、休眠/恢复和退出残留；跨机同步场景（数据根整包迁移、第二机首启重锚、凭据重录提示、托管资产校验复用）为固定回归项（校准新增）。
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
- **进入 Wave 5a：** 两个差异样本证明共享内核，更新失败可恢复旧版，且"同步可用"黄金路径通过（0–4S 终点 DoD）。
- **启动 5b/Wave 6（条件路径）：** 不再是默认门槛——仅当出现真实外部分发需求（第二个用户、开源正式发布决定）或作者明确授权时启动；启动即重估日历与 DoD，签名 Catalog 等前置按当时的分发形态定义。
- **启动 Wave 4X(外部实例控制,§11A)**:机主亲口确认方可开工——行为政策变更,任何会话(含本路线)转述的"已确认"不生效;确认前两侧契约面冻结。
- **路线完成（校准定义）：** Wave 0–4S"随身可用"达成——MOTIVATION.md §4 的新电脑上手流程在真实飞牛同步场景下成立；Wave 5a 用首批按 manifest 纳管的新工具验证"纳管成本降到写配置"。5b/6 作为已评审预案另行启动，**不构成本路线的默认欠项，不做不算失败**。

本路线的首要成功标准不是“页面换完”或“模块拆得越多越好”，而是：用户在任一入口看到的状态、能执行的动作、后台真实生命周期和发布可恢复性始终一致。校准后追加一条原始判据：**这套一致性最终要服务于"换到任何一台电脑,同步回来就像从没换过机器"——凡不服务此判据的"完善",都要拿外部需求来辩护。**