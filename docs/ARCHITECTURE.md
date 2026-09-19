# Hanxi 技术架构设计规范

> **产品定位**：开源工具工作台
> **产品版本**：v0.3.0
> **更新日期**：2026-09-19
> **技术基线**：Go ≥1.26 + Wails v3 + Vue 3 + TypeScript + Vite  
> **设计模式**：单体分层架构 + 单体内建按需懒加载 (On-demand Lifecycle Architecture) + 外部工具托管集成 (Managed Integration) + 共享托管内核 (packages/go) + 统一状态契约与调用门 (ADR-0001/0002/0003)

---

## 1. 总体架构分层

Hanxi 严格遵循整洁架构原则，分层自上而下单向依赖，禁止反向或跨层违规调用：

```text
┌────────────────────────────────────────────────────────┐
│  frontend/  Vue 3 + TypeScript + Vite                  │
│  - 视图组件、双栏导航外壳与状态投影缓存（不存第二份真相）│
│  - 调用 Wails v3 自动生成的类型化 Bindings JS API     │
└───────────────────────────┬────────────────────────────┘
                            │ Wails v3 IPC / Events
┌───────────────────────────▼────────────────────────────┐
│  internal/app/  Composition Root (应用唯一装配点)       │
│  - 生命周期管理、系统托盘 (Systray)、关闭拦截、优雅退出│
│  - AppService (设置/目录与状态投影/操作观察面/关于)    │
│  - 41 模块注册与调用门注入、receipt 补建、journal 恢复 │
└──────┬────────────────────┬────────────────────┬───────┘
       │                    │                    │
┌──────▼───────────┐ ┌──────▼───────────┐ ┌──────▼───────┐
│ internal/modules │ │ internal/extapi  │ │ internal/    │
│ 41 个业务模块    │ │ 模块契约：生命周 │ │ settings     │
│ 自建: frpc 网络  │ │ 期 + 四维状态目  │ │ 便携路径解析 │
│ 诊断 环境检测    │ │ 录/安装凭据/调用 │ │ 与配置持久化 │
│ 快传 随手记等    │ │ 门/更新感知契约  │ │ ReceiptStore │
│ 托管: 25 款桌面  ├──────────────────┤ │ 安装凭据存储 │
│ 工具 version+    │ │ internal/product │ ├──────────────┤
│ instance 子包    │ │ 品牌身份常量     │ │ internal/    │
│ （资产与进程主流 │ ├──────────────────┤ │ notify 通知  │
│ 程委托下方共享内 │ │ internal/        │ │ 中心         │
│ 核，bespoke 名单 │ │ updatewatch 更   │ ├──────────────┤
│ 见 §2.3）        │ │ 新感知调度       │ │ internal/    │
└──────┬───────────┘ ├──────────────────┤ │ history 统一 │
       │             │ internal/ops     │ │ 历史         │
       │             │ journal 事务接线 │ └──────────────┘
┌──────▼─────────────▼──────────────────▼────────────────┐
│  packages/go/  共享托管内核（零框架依赖，ADR-0002 冻结）│
│  - artifact: 下载→官方 SHA-256 必检→安全解包→版本树落位│
│  - supervisor: JobObject 进程治理引擎（Probe/Engine）   │
│  - operation: journal 账本 + 崩溃恢复 + 在途操作观察面  │
└───────────────────────────┬────────────────────────────┘
                            │
┌───────────────────────────▼────────────────────────────┐
│  internal/domain (纯领域模型，零外部与平台依赖)        │
│  - Project, ProxyRule, Snapshot, ConnState, AppError   │
└───────────────────────────┬────────────────────────────┘
                            │
┌───────────────────────────▼────────────────────────────┐
│  internal/platform (Windows 原生底层原语)               │
│  - windows: JobObject 进程树保护 · DPAPI 凭据硬件加密  │
│    注册表开机自启 · IP Helper 端口/网卡表 · 进程指纹   │
│    桌面快捷方式 · Appx 包操作                          │
│  - apppackage: MSIX/AppxBundle 当前用户安装管理        │
│  - versioncmp: 版本号比较 · versioninfo: PE 版本核对   │
└────────────────────────────────────────────────────────┘
```

---

## 2. 核心设计与子系统

### 2.1 单体内建按需懒加载与模块状态契约 (`internal/extapi` + `internal/app`)

为了在保持单个二进制文件的同时实现极致的内存与 CPU 节省，系统定义了标准的模块生命周期契约（以 `internal/extapi/module.go` 实际定义为准）：

```go
// Module 是单体内建模块的统一契约。
type Module interface {
    Info() ModuleInfo    // 元信息（ID 全局唯一，含 Name/Version/Description/Author/Level）
    Nav() []NavEntry     // 左侧导航条目（SectionCore/SectionExt 分区 + Order 排序）
    Services() []Service // 已包装的 Wails 服务

    // --- 按需运行资源生命周期钩子 ---
    OnInit(ctx context.Context) error // Registry 首次激活时分配运行资源
    OnDestroy() error                 // 停用/退出时释放协程、句柄与缓存
    IsInitialized() bool              // 模块自身报告的运行资源状态
}
```

可选契约族（实现即接入，保持向后兼容）：`TrayCommandsProvider`（托盘命令）、`GateAware`（调用门注入，`gate.go`）、`UpdateChecker`（更新感知，`update.go`）。

- **模块级别现状**：`LevelBuiltin` 为编译进主程序的内建模块；`LevelExternal` 仅是未来外部子进程扩展的枚举预留。当前 41 个模块全部为 `LevelBuiltin`，没有动态安装或加载第三方 Hanxi 插件的运行时；模块中心的"安装/卸载"是 **逻辑安装态**（见下），不涉及第三方插件的物理装卸。
- **权限现状**：`Permission` 类型及 `kill-process` / `lan-scan` / `network` 常量仍保留供未来外部扩展能力握手复用，但 `Permissions()` 已从 `Module` 接口移除，当前内建模块没有统一的 Permission Gateway。模块启停、各业务服务的安全校验，以及四个只读 MCP 工具的 `access.json` 授权是不同层次，不能表述为通用插件权限隔离。
- **状态契约（Wave 0 冻结，`catalog.go` + `receipt.go`）**：`ModuleContractSchema = 1` 随投影下发，破坏性变更必须递增 schema 并在 `docs/adr/` 立案（ADR-0001）。
  - `ModuleCatalogItem`：静态身份与能力目录（交付形态字段为 **`deliveryKind`**——2026-09-19 修订自 `delivery`，与 `ModuleState.delivery` 生命周期维度语义正交、永久异名，ADR-0001 修订记录；含 `Entrypoint` 八值入口、`capabilities`、`owner`），由 `scripts/cataloggen` 从源码证据与 composition 契约推导生成物表 `internal/app/catalog.go`（41 项），基线冻结于 `scripts/fixture/module_catalog.json`，漂移由契约测试守护；
  - `ModuleState`：四维事实投影——Delivery(7 值) / Policy(5 值) / Runtime(7 值) / Health(9 值)，`StateInput.Project()` 按冻结优先级计算 `primaryAction` / `reason` / `summary`（八个 SummaryKey 族），页面禁止自行堆叠 if/else 推断；纯展示附加字段 `remoteVersion`（health=update-available 时的上游新版本号，由更新感知链写入、其余健康值下清空 omitempty，**不参与状态机裁决**）；`Visible()` = installed ∧ (enabled ∨ mandatory)；`CanDeliveryTransition` / `CanPolicyTransition` / `CanRuntimeTransition` 提供转换合法性门（非法转换为 0 的 DoD 守卫）；
  - `Operation`：统一表达安装、启停、更新、回滚、修复、卸载与业务调用租约（kind 8 值、queued/running/succeeded/failed/cancelled 状态机、`TxnID` 关联 journal 事务、结构化 `OperationError{code,message,recoverable}`）；
  - `receipt.go` 定义 `ReceiptStorage` 抽象与 `Receipt` 落盘 schema（字段变更须递增 Schema 并立案 ADR）。
  - **权威源是 `Registry.ListStates()`**（receipt / 启用位 / wrapper 标志 / 覆盖项合并投影，按 ID 稳定排序）；前端 `useModuleCatalog` 只缓存投影副本，不持久化第二份真相。
- **逻辑安装态（Wave 1，ADR-0001 §1.5）**：`settings.ReceiptStore` 落 `<数据根>/modules/receipts/<id>.json`（tmp+rename 原子写、损坏按未安装处理并告警、同 ID 异 kind 漂移拒写）；`Registry.Install` = 登记凭据 + 默认启用（幂等、保留首次 InstalledAt），`Registry.Uninstall` = 先 SetEnabled(false) 完成 drain/析构收口、再移除凭据，用户数据默认保留。**builtin-logical 的卸载只移除逻辑凭据与入口，不释放宿主（hanxi.exe）体积——UI 文案与文档必须如实表达**。装配根在每次启动 `Register` 成功后调用 `ReceiptStore.EnsureSeen`（`known-modules.json` 已见名单账本）：**首次运行**对全部注册模块一次性补建凭据（老用户 Enabled=true→installed+enabled、false→installed+disabled，入口与数据零丢失）；**名单内模块永不补建**——用户卸载过的模块重启不复活，卸载是永久的；此后版本升级新增的模块（名单外）才自动安装（失败仅告警不阻断启动，缺凭据模块按未安装呈现、可在模块中心一键找回）。
- **生命周期状态现状**：Registry 以 `ModuleWrapper` 的 `initialized/initializing/stopping/inFlight/failed` 标志经 `runtimeStateLocked()` 折算 Runtime 维度投影（enabled=false→inactive；init 失败滞留 failed 直到重试/收口）；`IsInitialized()` 仍是接口成员，但 Registry 当前不读取它。
- **懒激活链路**：前端进入模块路由 → `AppService.EnsureModuleActive(id)` → `Registry.EnsureActive`（**前置 receipt 门**，未安装拒绝激活）→ 首次触发 `OnInit()`。启动预激活例外（常驻/唤出语义所需）：`wechat`（入站消息监听）、`quickmenu` / `msgboard`（全局弹窗入口）、`wsl`（USB 自动共享账本重放）。
- **统一调用门（Wave 3）**：`Registry.Acquire(moduleID)` 是全部入口共用的唯一裁决实现，检查顺序冻结为 **blocked → receipt 已安装 → 懒激活（并发初始化一次）→ enabled/非 stopping → operation lease 入账**（安全裁决优先于安装呈现，与 `Project()` 状态机同序）；release 幂等（`defer` 即可），停用与退出的有界 drain 以租约归零为门——`SetEnabled(false)` 预算 30s、`ShutdownAll` 预算 60s，超时强制收口并 `slog.Error` 显式告警绝不静默（在途调用由模块内部锁兜底，JobObject 保证进程侧无孤儿）。模块侧注入模式（`gate.go`）：`Module` 实现可选契约 `GateAware`（`Register` 时在 Registry 锁外注入 `Gate`），service 内嵌 `LeaseHolder`，业务方法入口一行 `holder.Enter()` 接门——带 error 的方法拒则回传、纯 void 方法拒即早退、单值无 error 的方法必须先扩为 `(T, error)`，禁止"拒则回空值"的静默消化。**41 模块 RPC 全接门由 AST 穷举门守护**：`internal/app/rpc_gate_matrix_test.go` 逐模块校验 GateAware 实现、New 构造持有 LeaseHolder、持户结构体每个导出方法（= Wails 绑定面）调用 `Enter()`；豁免表只收"装配布线 setter"（`SetWailsApp`/`SetHistory`/`SetMemoHook`/`SetMainWindow`/`SetHotkeyRegistry`/`SetSnipHotkeyBinding`——Go 直调时序先于 gate 注入）。
- **入口按投影收口（Wave 3，与 `Visible()` 同一裁决）**：导航 `Registry.GetEnabledNavs` 仅输出 installed∧enabled 模块的条目；托盘命令候选 `ListTrayCommands` 同口径过滤、执行 `RunTrayCommand` 经 `Acquire` 取租约，菜单中引用停用/卸载模块的条目随启停热重建（`AppService.SetModuleInstalled`/`SetModuleEnabled` → `broadcastModuleChange`：广播无载荷事件 `ext:changed` + `SetTrayRebuilder` 注入的托盘重建回调）；全局热键期望绑定态 = 模块启用 × 用户开关（`snipHotkeyGate` 挂 `Registry.OnLifecycle`，停用即注销 OS 键位不留按键黑洞；回调复用 `RunTrayCommand` 命令派发链，同过调用门）；MCP 无头模式 `registryGate.Check` 为取租约式（`headlessReceipts` 与 GUI 同账，无头在途调用纳入 drain 门；memo 因构造带迁移写盘不进无头 registry，特例直读 config enabled 位 + receipt 保持同语义拒绝）；独立窗口与后台预激活走 `EnsureActive`（自带 receipt 门）。**未安装或停用模块不得从任一入口旁路执行。**
- **更新感知（Wave 4+，健康维度真实来源）**：`extapi.UpdateChecker` 可选契约——`CheckUpdate(ctx) (local, remote, hasUpdate, err)` 必须廉价（只读本地安装账目 + 带 10 分钟 TTL 缓存的远程列表）、可并发重入、不得触发懒激活或写盘；判定失败必须保持原健康值（"无可比事实"≠"有更新"，不谎报）。`internal/updatewatch.Scheduler` 由装配根从注册模块类型断言收集实现并调度：启动后一次性延迟 60s 首检（**无常驻 ticker**，让路启动风暴、避免 GitHub 匿名限流配额成本）+ `AppService.RefreshUpdates` 手动一轮（`updates_service.go`）；单轮并发 ≤3、全局预算 5 分钟、单模块预算 60 秒、同轮 in-flight 合并（single-flight）；裁决写 `registry.SetHealth(moduleID, health, remoteVersion)`（update-available 时随记上游新版本号、current 清空），结果缓存 `state/updates.json`（`checkedAt` + `available: moduleID→remoteVersion`，仅存"有更新"一侧）且启动时 `Restore` 回灌供前端首帧即时点亮；每轮收口广播 `updates:checked` 事件提示前端重拉投影。前端零新 RPC——状态与 `remoteVersion` 均随 `ListModuleStates` 既有投影携带。
- **动态回收**：设置页停用模块时先阻止新命令并有界 drain，再调用 `OnDestroy()` 清空对象，随后在 goroutine 中触发 `runtime.GC()` 与 `debug.FreeOSMemory()` 将内存彻底归还操作系统；启用状态经 `Registry` 持久化（Store）。
- **退出编排**：`OnShutdown → Registry.ShutdownAll()`（60s 有界 drain 后逐模块析构），JobObject 受管工具连带退出；脱管工具（Snipaste 等）保留原生托盘不受波及。
- **宿主服务**：`internal/notify` 提供全局通知中心（Hub 分级通知 + `notify:received` 事件 + 已读管理）；`internal/product/identity.go` 集中品牌常量（名称/标识符/可执行名/数据目录/版本）。宿主级无载荷广播三件套：`ext:changed`（安装/启停可见性收口）、`operation:changed`（journal 事务登记与全部收口点，前端重拉操作观察面）、`updates:checked`（更新感知轮次收口）。
- **统一历史（`internal/history`）**：公共包型服务（与 notify 同谱：直挂 services、无模块身份、无 Nav 无路由）。单文件 `state/history.json`，格式 `map[funcType][]Record`（桶头插新→旧，每桶 200 条裁剪）；`Save` 入口统一补 ID/时间、过 `logging.Redact`（与日志出口同口径）并按 4000 rune 截断，损坏取 frpc 严格档（loadErr 驻留禁写、绝不空库覆写）。模块侧经装配根 `SetHistory` 注入在业务动作点自动留档（首批：ocr 识别 defer 单点、portkill 查询与两路查杀、npmtool 装升卸终态；Q2 口径：动作全记、查询仅 ocr/portkill、envcheck 版本查询不记），`HistoryService` 只暴露读删清、无前端写入面。前端通用件 `components/tool/HistoryPanel.vue` 自取数（funcType prop + 350ms 防抖搜索），宿主按形态嵌 Tab（envcheck）或 Teleport 弹窗（ocr/portkill），双击行上抛整条 Record 直接回填（Q6 行内数据直用，不做应用前强一致重查）。OCR 全文入历史由 config.json `historyOcrFullText` 档位裁决（默认开；关=只记路径与摘要），读写走 `HistoryService.GetOcrFullText/SetOcrFullText`。

### 2.2 frpc 多实例引擎与进程沙箱 (`internal/modules/frpc`)

1. **Windows JobObject 绑定**：每个启动的 `frpc.exe` 进程均关联到作业对象，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。即使 Hanxi 异常退出，操作系统内核也会强制清理所有子进程。
2. **连接状态嗅探**：`Instance` 在读取标准输出/错误流时实时分析特征词（如 `login to server success`, `authorization failed`, `connect to server error`），实时更新 `ConnState` 并通过 Wails Event 推送到前端。
3. **DPAPI 凭据加密与临时文件治理**：
   - 存储持久化时，Token 经 DPAPI 加密为密文落盘；
   - 停止项目实例时，立即清理 `runtime/frpc/frpc-<id>.toml` 临时配置文件。
4. **版本子包**：官方 GitHub Releases（`fatedier/frp`）+ 国内镜像加速下载、SHA256 校验、指数退避重试、本地 `frpc.exe` 导入，附 TOML 文档生成器（docgen）。
5. **内核豁免**：frpc 的多实例 map、连接嗅探与 DPAPI 脱敏日志使差异大于共性，version/instance 全程 bespoke，不套 `packages/go` 内核（ADR-0002 §3：豁免即终点，不得反向扩内核迁它）。

### 2.3 外部工具托管架构（Managed Integration）

25 款第三方桌面工具按同一标准骨架纳管，每模块三件套（例外：`douzy` 上游尚处内测期、无便携形态，刻意止步于 `version/` 版本管理 + 安装包下载，不做进程托管、无 `instance/` 子包；`nanazip` 为 MSIX Bundle 形态，同样止步 `version/` 验签拆包，无 instance 面）：

```text
internal/modules/<tool>/
├─ module.go      # extapi.Module 实现：元信息 / 导航 / 服务注册 / 生命周期
├─ version/       # 版本管理子包：上游侦查 → 完整性校验 → 下载 → 安装布局适配
│   └─ remote.go  #   repoOwner/repoName 常量、镜像链、指数退避、SHA256/digest 校验
└─ instance/      # 实例引擎子包：受管进程启停、运行态探测、唤窗、退出治理
```

**版本管理子包（`version/`）**

- **共享内核委托（Wave 4/5S）**：下载→校验→安全解包→版本树落位主流程已收口 `packages/go/artifact`（详见 §2.4）——版本面委托共 **20 个模块**，其中 18 个连 instance 进程治理一并委托 supervisor（见下），`quicklook`/`bili23` 仅版本段委托、instance 留 bespoke（ADR-0003 §4）；18 家之中 `snipaste`（官网仅 SHA-1）/`guoheview`（官方仅 MD5）/`vscode`（历史版无官方摘要）为**薄适配器**——"下载+官方哈希校验"段留模块 bespoke（弱摘要/缺摘要上游不放宽 `Fetch` 的 SHA-256 唯一信任根，ADR-0002 §5 内核边界裁定；vscode 降级理由登记于迁移分类账"信任根缺位第 3 家"），解包落位仍走 `UnpackZip`/`Tree` 闸门；`recordly` 为 NSIS 覆盖式单目录、无 Tree 可登记（版本面偏离 zip 模板）；
- **完整性四层兜底链**（按上游能力择优组合，为各家取证来源）：GitHub API 资产 `digest` → 官方 `SHA256SUMS.txt` 双源比对 → 官方站哈希清单（如 Snipaste sha-1、voidtools sha256、果核看图官方接口 MD5）→ 字节数 + MZ/PE `versioninfo` 版本核对 + sha256 下载指纹存档；委托 `artifact.Fetch` 的模块以官方 SHA-256 必检为硬线（缺摘要一律拒装，镜像只是搬运工）；
- **安装布局适配**（免提权优先）：便携 zip 直解 / 顶层包装目录收割（zip 内 exe 深一层，如果核看图 `GuoheViewPortable/`）/ `msiexec /a` 管理提取（MSI 无 zip 形态，如 PicLite、Keyviz）/ NSIS `/S /D=` 静默安装（Recordly）/ 当前用户 MSIX（`platform/apppackage`，NanaZip）/ AppInstaller 官方直装清单交叉校验（EarTrumpet）/ 单文件 exe 下载即安装（含 rust-portable packer 内层自解压形态，Rufus、RustDesk/SubnetDesk）/ MSI 安装版双形态纳管（Hanxi 取包校验+发起上游向导+注册表探测安装位，RustDesk/SubnetDesk 与 VS Code User Installer）；
- 统一支持本地导入、版本删除与 `*:version-download` 进度事件。

**实例引擎子包（`instance/`）**

- **共享内核委托（Wave 4/5S）**：进程治理主流程（spawn → JobObject 绑定 → 就绪探测/分类入账 → 退出分类收口）收口 `packages/go/supervisor`，**18 个模块**把探针族/优雅退出通道/状态词表映射以 `Probe`/`QuitHook`/`Callbacks` 注入委托；**留 bespoke 全量名单**（豁免即终点，防假抽象第二样本）：`frpc`（多实例 map + 连接嗅探 + DPAPI 脱敏日志）、`rustdesk`/`subnetdesk`（packer 外层早退的进程树监督，击穿"自有句柄=生命周期"公理）、`quicklook`（Quit+Reload 双令命令通道，单家需求不扩内核）、`bili23`（退出不强杀 + 三态如实上报，与固定 killSequence 冲突）、`ocr`（私有 hanxi-ocr 资产契约与双探针，均见 ADR-0002 §3 / ADR-0003 §4）；
- **运行态探测**：进程枚举、命名互斥体探测（Keyviz/PicLite/FlClash 等；RustDesk 系按安装形态分治——便携版进程镜像路径探针、安装版命名互斥体）、.NET 单实例通道（BCU/PaperTodo）、商店/DSD 注册态（EarTrumpet）；**多实例上游无锁可探**（果核看图二次拉起即新开窗）→ 进程名快照 + EnumWindows 按自有 PID 过滤；委托内核的模块以上述探针实现 `supervisor.Probe.Inspect` 单一事实源；
- **唤窗通道**：官方命令信使（show/hide/exit）、`EnumWindows` 置前台、AUMID 激活、Win32 `ShowWindow/SetForegroundWindow` 直操作，全部先做进程指纹复核（PID+启动时间+路径）防误杀；
- **进程模型**：JobObject 全程托管绑定（默认解除 kill-on-close，工具不随 Hanxi 退出；开启"跟随 Hanxi 退出"才启用内核连带强杀）；闭源工具脱管（Snipaste）；MSIX/Appx 型不绑 JobObject（NanaZip/EarTrumpet）；
- **退出治理**：命名管道优雅退出（QuickLook Quit/Reload）→ 命令通道 → 指纹复核强杀（上游事务性写入保证安全）三级策略；委托内核的模块经 `SetQuitHook` 注入优雅通道、末级强杀由 Engine 承担（Quit 多态归因决策留模块 instance 层，不为塞内核造第二返回值，ADR-0002 §5）；**例外分治**——上游关窗行为用户可配且强杀有中断在途任务代价的下载器型（Bili23 Downloader）不做静默强杀兜底，Quit 三态（exited/hidden/windowUp）如实上报，强杀仅留给用户显式点击；外部自行启动的实例 supervisor 只甄别、只指引（`ErrExternal`），绝不由引擎强杀；支持"跟随 Hanxi 退出"开关（`SetFollowOnExit`，**默认关闭**：开启 Detached 解除 Job 退出联动，Hanxi 退出/崩溃均不影响工具）与桌面快捷方式（`platform/windows/shortcut.go`）。

**启动恢复与残骸清理（journal 背书法）**：装配根在一切模块构造前执行 `operation.Recover`（`internal/app/app.go` versionTrees 登记 18 棵托管版本树）——补偿只动**有账本背书**事务在各树内的 `.tmp-<txnID>` staging 与 `.removing-<txnID>` 隔离目录，收口后 `Complete(compensated)` 落账、由观察面以 `resumable` 呈现待用户处置；未登记模块的事务不猜测、不自动执行；无背书的事务前缀残骸只上报不动盘（删除决策永远保留给有账本背书的收口路径）；`artifact.CleanStaleParts` 另对 `%TEMP%\hanxi-*`、`installers/`、`versions/` 三根各扫一轮超龄 `.part-<hex>` 下载残件（形状+超龄+普通文件三闸俱备才删）。

**事件契约**：托管模块统一推送 `*:version-download`（下载进度）与 `*:instance-state`（实例状态）两类事件，汇入 `notify` 通知中心；托管安装事务另经统一 Operation 观察面（`operation:changed` 广播 + `AppService.ListOperations` / `DismissResumable` RPC），供模块中心恢复条/在途条与首页最近任务消费。

### 2.4 共享托管内核 (`packages/go/`)

Wave 4 从 25 份 version 下载链、24 份 instance 进程治理主流程中收口出三个零框架依赖的共享包（升版规则、稳定门判定与逃生钩子白名单由 `docs/adr/ADR-0002` 冻结：zip 族 + 单 exe 族两差异样本全程走内核、失败注入矩阵通过、≥3 家需求才入内核）：

- **`artifact`（交付链）**：`Fetch` 受控下载——官方 SHA-256 必检（缺失即拒，"托管下载不允许无校验安装"）、Content-Length 与实收双核、流式大小断言、落盘后全量重读复核；HTTPS-only（http 仅限回环且强制绕过代理）、重定向不跨 scheme、错误/进度不回显带 query 的 URL。`UnpackZip` 安全解包闸门——ZipSlip（反斜杠归一 + 组件级清洗 + Clean 前后双查）、炸弹预算 `Limits`（条目数/单文件/总解压量/压缩比）、`allowFiles` 清单白名单逐文件摘要复核、大小写占位冲突检测、反斜杠结尾目录条目（不带 ModeDir 位）修复；`Tree` 版本树——`OpenTree(root, entryName)` 的 `<entryName>_<version>/` 布局、`StageDir` 独占 `.tmp-<txnID>` 中转（不覆盖、不执行任何内容）、`Commit` 原子落位（同摘要幂等复用 / 异摘要**防同版本内容漂移拒覆** / 无账本目录拒覆盖 / rename 优先、meta.json 写失败搬回回退）、`Remove` 先 rename 隔离到 `.removing-*` 再删（文件被占用时留下可恢复状态）、`Versions/Resolve/CleanupAbandoned/InstallZip`；`CleanStaleParts` 超龄 `.part-*` 残件受控清理；版本令牌白名单（`ValidateVersionToken`）防目录名注入。
- **`supervisor`（进程治理）**：`Probe.Inspect` 单一事实源（外部性 + `ProcInfo`，取代各蓝本 IsRunning/PortOpen/WaitForReady 碎片接口）；`Spec` 受控字段（Exe/Args 原样 argv 无 shell、`DetachFromJob`、`ReadyTimeout`、`HideWindow`（Windows 经 CREATE_NO_WINDOW 注入、其他平台 no-op）、`Env` 键值白名单注入）；`Engine` 生命周期（Start/Stop/Snapshot，状态锁与操作锁分离、`cmd.Wait` 单所有者、上一代未收口拒新启）；`SetQuitHook` 优雅退出通道（管道命令/WM_CLOSE/HTTP shutdown）；`ErrExternal`（外部实例只甄别指引）/`ErrBusy`；`Callbacks.OnState/OnLog`（OnLog 逐行透传 stdout/stderr，脱敏/缓冲策略留模块层）；PID 复用防护（VerifyToken 身份 + KillVerified 复核）；停止三段：QuitHook grace → Job Terminate → 指纹复核强杀。
- **`operation`（事务账本与观察面）**：`Journal` 落盘 schema v1 固定 **15 字段**（schema/transactionId/operation/deliveryKind/moduleId/fromVersion/toVersion/phase/state/createdAt/updatedAt/activeBefore/dataPolicy/steps/error），绝不记录 Secret、用户文件内容或敏感完整命令行；`Store` 记账面 Begin/Advance/Complete/Get/Pending/AbandonedDirs——**journal 先落盘并 fsync 再执行副作用**，更新走 tmp+rename，每个 moduleId 同时只允许一笔未收口写事务（`ErrTxnActive`）；`Recover` 背书法崩溃恢复（Report 四桶：resumed/rolledBack/orphaned/quarantined，损坏文件改名隔离取证），恢复收口前不开放业务调用；`Hub` 在途操作观察面输出 `extapi.Operation`（journal 词汇 uninstall↔remove 映射收敛在 hub.go），未收口事务回灌为 `failed + error.code="resumable" + recoverable=true`（合成记录 ID `"resumed-"+txnID`，收口通道一律消费 `TxnID` 裸字段），`ForgetResumable` 支持宿主代收口摘除。`internal/ops` 为装配根与模块间接线层：`SetKernel` 注入（未注入时 BeginTxn 全方法安全退化为 no-op，无账模式不阻断业务）、`operation:changed` 事件广播、残骸清理工具面（`CleanTxnResidue`/`ListUnbackedTxnDirs`/`CleanStaleDownloadParts`）。
- **范围裁剪（ADR-0003）**：官方 Catalog 签名链/密钥基建/撤回演练不实施——供应链完整性的实际承担者为 HTTPS + 上游官方摘要必检（`artifact.Fetch` 全线强制）；Wave 6 物理拆包缓行无排期；DPAPI 凭据保持本机绑定（搬家语义由数据根整体同步覆盖）。

### 2.5 平台底层实现 (`internal/platform`)

- **`windows/autostart.go`**：管理注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\Hanxi` 实现开机静默启动。
- **`windows/dpapi.go`**：封装 Windows `CryptProtectData` 与 `CryptUnprotectData`。
- **`windows/job.go`**：封装 Windows JobObject 作业对象原语（进程树兜底清理）。
- **`windows/process.go`**：基于 `OpenProcess` 与 `QueryFullProcessImageNameW` 获取进程路径与启动时间，用于防误杀校验。
- **`windows/net.go` & `windows/port.go`**：封装 `iphlpapi.dll` 获取网卡、ARP 表、TCP/UDP 扩展连接表。
- **`windows/shortcut.go`**：Shell Link 桌面快捷方式创建。
- **`windows/apppackage*.go` & `apppackage/`**：当前用户 Appx/MSIX/AppxBundle 安装、注册检测与卸载（PowerShell `Add-/Remove-AppxPackage` 通道，含依赖透传与未安装类型化错误）。
- **`versioncmp/`**：语义化版本号比较（版本通道按本机版本线排序等场景）。

---

## 3. 持久化与便携化设计 (`internal/settings`)

> Hanxi v0.3.0 是品牌断代版本：源码命名空间、应用标识、进程名、自启项、单实例 ID 和标准数据目录均使用 Hanxi，不探测或迁移旧产品数据。

- **路径判定**：
  - 启动时若在可执行文件同级目录检测到 `hanxidata/` 目录（存在即生效，含空目录），则判定为 **Portable 便携模式**，所有数据、日志、版本文件均存放在 `hanxidata/` 下；为兼容旧便携包，同级泛化名 `data/` 仅当其中已含 Hanxi 数据根特征（`config.json` 或 `versions/`）时仍被识别，空 `data/` 不再触发便携模式；
  - 否则进入 **Standard 标准模式**，数据存储在 `%APPDATA%/Hanxi/`。
- **数据根布局与状态收纳**：数据根只保留应用级锚点——`config.json`（便携数据根标记，不可下迁）、`state/`（各模块状态 JSON，如 `markeron.json`、`memo.json`，含公共统一历史 `history.json` 与更新感知缓存 `updates.json`）、`logs/`、`versions/`、`runtime/`、`installers/`（托管安装版 MSI 缓存与安装包落地区，消费方懒建）、`modules/`（模块工作台数据树：`receipts/` 逻辑安装凭据随布局预建，`journals/` 操作事务账本由消费方懒建）与个别组件二进制锚（`everything/es`、`ocr-engines`）。历史版本把模块状态平铺在根目录，v0.3.x 起由 `settings.MigrateRootStateFiles` 在启动时（模块装配前）自动收拢进 `state/`：`config.json` 除外；目标同名不覆盖、告警留痕（升级/回滚前请先彻底退出另一版本，避免双份文件）。
- **托管目录约定**：frpc 与各托管工具的二进制、版本文件、运行时配置统一落在对应模块的托管子目录（`versions/`、`runtime/`）内；上游工具自身数据（如 PaperTodo `data.json`）原地保留于托管目录，卸载 Hanxi 不删除用户数据。
- **并发安全原子写**：配置保存采用"写入临时文件 + `os.Rename`"策略，杜绝因程序异常断电造成 JSON 文件损坏；ReceiptStore 与 operation journal 同纪律（落盘并 fsync 先行、tmp+rename 更新）。
- **单实例锁**：以 `io.hanxi.desktop` 标识抢占，防止双开导致的托盘与端口冲突。
