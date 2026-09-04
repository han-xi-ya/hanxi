# Hanxi 技术架构设计规范

> **产品定位**：开源工具工作台
> **产品版本**：v0.3.0
> **更新日期**：2026-09-03
> **技术基线**：Go ≥1.24 + Wails v3 + Vue 3 + TypeScript + TailwindCSS  
> **设计模式**：单体分层架构 + 单体内建按需懒加载 (On-demand Lifecycle Architecture) + 外部工具托管集成 (Managed Integration)

---

## 1. 总体架构分层

Hanxi 严格遵循整洁架构原则，分层自上而下单向依赖，禁止反向或跨层违规调用：

```text
┌────────────────────────────────────────────────────────┐
│  frontend/  Vue 3 + TypeScript + TailwindCSS           │
│  - 视图组件、状态管理、路由与侧边栏动态渲染           │
│  - 调用 Wails v3 自动生成的类型化 Bindings JS API     │
└───────────────────────────┬────────────────────────────┘
                            │ Wails v3 IPC / Events
┌───────────────────────────▼────────────────────────────┐
│  internal/app/  Composition Root (应用唯一装配点)       │
│  - 生命周期管理、系统托盘 (Systray)、关闭拦截、优雅退出│
│  - AppService (通用设置/日志/导航/关于信息)            │
│  - 26 个模块统一注册与 Wails 服务注入                  │
└──────┬────────────────────┬────────────────────┬───────┘
       │                    │                    │
┌──────▼───────────┐ ┌──────▼───────────┐ ┌──────▼───────┐
│ internal/modules │ │ internal/extapi  │ │ internal/    │
│ 25 个业务模块    │ │ 模块生命周期契约 │ │ settings     │
│ 自建: frpc 网络  │ │ 与按需懒加载注册 │ │ 便携路径解析 │
│ 诊断 环境检测    │ │ (Info/Nav/       │ │ 与配置持久化 │
│ 快传 随手记等    │ │  Services/       │ ├──────────────┤
│ 托管: 15 款桌面  │ │  OnInit/         │ │ internal/    │
│ 工具 version+    │ │  OnDestroy)      │ │ notify       │
│ instance 子包    │ ├──────────────────┤ │ 全局通知中心 │
└──────┬───────────┘ │ internal/product │ └──────────────┘
       │             │ 品牌身份常量     │
┌──────▼─────────────▼───────────────────────────────────┐
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

### 2.1 单体内建按需懒加载架构 (`internal/extapi` + `internal/app`)

为了在保持单个二进制文件的同时实现极致的内存与 CPU 节省，系统定义了标准的模块生命周期契约（以 `internal/extapi/module.go` 实际定义为准）：

```go
// Module 是扩展契约（支持单体零开销懒加载生命周期）。
type Module interface {
    Info() ModuleInfo          // 元信息（ID 全局唯一，含 Name/Version/Description/Author/Level）
    Nav() []NavEntry           // 左侧导航条目（SectionCore/SectionExt 分区 + Order 排序）
    Services() []Service       // 已包装的 Wails 服务（extapi.NewService 泛型包装）
    Permissions() []Permission // 能力白名单声明（kill-process / lan-scan / network）
    Protocol() int             // 契约版本，为未来子进程插件握手预留

    // --- 懒加载生命周期钩子 ---
    OnInit(ctx context.Context) error // 首次激活时分配资源（进入路由或调用 API 触发）
    OnDestroy() error                 // 停用/退出时释放协程、句柄与缓存
    IsInitialized() bool              // 运行时资源是否已分配
}
```

- **两级模块级别**：`LevelBuiltin` 编译进主程序、仅可启停；`LevelExternal` 预留给未来独立子进程插件（manifest + JSON-RPC over stdio），当前全部模块为内建级。
- **懒激活链路**：前端进入模块路由 → `AppService.EnsureModuleActive(id)` → `Registry.EnsureActive` → 首次触发 `OnInit()`。例外：`wechat` 启动时即预激活（常驻监听语义所需）。
- **动态回收**：设置页停用模块时调用 `OnDestroy()` 清空对象并触发 `runtime.GC()` 与 `debug.FreeOSMemory()` 将内存彻底归还操作系统；启用状态经 `Registry` 持久化（Store）。
- **退出编排**：`OnShutdown → Registry.ShutdownAll()`，JobObject 受管工具连带退出；脱管工具（Snipaste 等）保留原生托盘不受波及。
- **宿主服务**：`internal/notify` 提供全局通知中心（Hub 分级通知 + `notify:received` 事件 + 已读管理）；`internal/product/identity.go` 集中品牌常量（名称/标识符/可执行名/数据目录/版本）。

### 2.2 frpc 多实例引擎与进程沙箱 (`internal/modules/frpc`)

1. **Windows JobObject 绑定**：每个启动的 `frpc.exe` 进程均关联到作业对象，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。即使 Hanxi 异常退出，操作系统内核也会强制清理所有子进程。
2. **连接状态嗅探**：`Instance` 在读取标准输出/错误流时实时分析特征词（如 `login to server success`, `authorization failed`, `connect to server error`），实时更新 `ConnState` 并通过 Wails Event 推送到前端。
3. **DPAPI 凭据加密与临时文件治理**：
   - 存储持久化时，Token 经 DPAPI 加密为密文落盘；
   - 停止项目实例时，立即清理 `runtime/frpc/frpc-<id>.toml` 临时配置文件。
4. **版本子包**：官方 GitHub Releases（`fatedier/frp`）+ 国内镜像加速下载、SHA256 校验、指数退避重试、本地 `frpc.exe` 导入，附 TOML 文档生成器（docgen）。

### 2.3 外部工具托管架构（Managed Integration）

15 款第三方桌面工具按同一标准骨架纳管，每模块三件套：

```text
internal/modules/<tool>/
├─ module.go      # extapi.Module 实现：元信息 / 导航 / 服务注册 / 生命周期
├─ version/       # 版本管理子包：上游侦查 → 完整性校验 → 下载 → 安装布局适配
│   └─ remote.go  #   repoOwner/repoName 常量、镜像链、指数退避、SHA256/digest 校验
└─ instance/      # 实例引擎子包：受管进程启停、运行态探测、唤窗、退出治理
```

**版本管理子包（`version/`）**

- **完整性四层兜底链**（按上游能力择优组合）：GitHub API 资产 `digest` → 官方 `SHA256SUMS.txt` 双源比对 → 官方站哈希清单（如 Snipaste sha-1、voidtools sha256、果核看图官方接口 MD5）→ 字节数 + MZ/PE `versioninfo` 版本核对 + sha256 下载指纹存档；
- **安装布局适配**（免提权优先）：便携 zip 直解 / 顶层包装目录收割（zip 内 exe 深一层，如果核看图 `GuoheViewPortable/`）/ `msiexec /a` 管理提取（MSI 无 zip 形态，如 PicLite、Keyviz）/ NSIS `/S /D=` 静默安装（Recordly）/ 当前用户 MSIX（`platform/apppackage`，NanaZip）/ AppInstaller 官方直装清单交叉校验（EarTrumpet）；
- 统一支持本地导入、版本删除与 `*:version-download` 进度事件。

**实例引擎子包（`instance/`）**

- **运行态探测**：进程枚举、命名互斥体探测（Keyviz/PicLite/FlClash 等）、.NET 单实例通道（BCU/PaperTodo）、商店/DSD 注册态（EarTrumpet）；**多实例上游无锁可探**（果核看图二次拉起即新开窗）→ 进程名快照 + EnumWindows 按自有 PID 过滤；
- **唤窗通道**：官方命令信使（show/hide/exit）、`EnumWindows` 置前台、AUMID 激活、Win32 `ShowWindow/SetForegroundWindow` 直操作，全部先做进程指纹复核（PID+启动时间+路径）防误杀；
- **进程模型**：默认 JobObject 强绑定；闭源工具脱管（Snipaste）；MSIX/Appx 型不绑 JobObject（NanaZip/EarTrumpet）；
- **退出治理**：命名管道优雅退出（QuickLook Quit/Reload）→ 命令通道 → 指纹复核强杀（上游事务性写入保证安全）三级策略，支持"跟随 Hanxi 退出"开关（`SetFollowOnExit`）与桌面快捷方式（`platform/windows/shortcut.go`）。

**事件契约**：托管模块统一推送 `*:version-download`（下载进度）与 `*:instance-state`（实例状态）两类事件，汇入 `notify` 通知中心。

### 2.4 平台底层实现 (`internal/platform`)

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
  - 启动时若在可执行文件同级目录检测到 `data/` 目录，则判定为 **Portable 便携模式**，所有数据、日志、版本文件均存放在 `data/` 下；
  - 否则进入 **Standard 标准模式**，数据存储在 `%APPDATA%/Hanxi/`。
- **托管目录约定**：frpc 与各托管工具的二进制、版本文件、运行时配置统一落在对应模块的托管子目录（`versions/`、`runtime/`）内；上游工具自身数据（如 PaperTodo `data.json`）原地保留于托管目录，卸载 Hanxi 不删除用户数据。
- **并发安全原子写**：配置保存采用“写入临时文件 + `os.Rename`”策略，杜绝因程序异常断电造成 JSON 文件损坏。
- **单实例锁**：以 `io.hanxi.desktop` 标识抢占，防止双开导致的托盘与端口冲突。
