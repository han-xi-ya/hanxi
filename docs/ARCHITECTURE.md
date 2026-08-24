# HubKit 技术架构设计规范

> **版本**：v1.0  
> **更新日期**：2026-08-24  
> **技术基线**：Go ≥1.24 + Wails v3 + Vue 3 + TypeScript + TailwindCSS  
> **设计模式**：单体分层架构 + 单体内建按需懒加载 (On-demand Lifecycle Architecture)

---

## 1. 总体架构分层

HubKit 严格遵循整洁架构原则，分层自上而下单向依赖，禁止反向或跨层违规调用：

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
└──────┬────────────────────┬────────────────────┬───────┘
       │                    │                    │
┌──────▼───────────┐ ┌──────▼───────────┐ ┌──────▼───────┐
│ internal/modules │ │ internal/extapi  │ │ internal/    │
│ 独立业务模块集合 │ │ 模块生命周期契约 │ │ settings     │
│ (frpc, wechat,   │ │ 与按需懒加载注册 │ │ 便携路径解析 │
│  portscan, lan,  │ │ (OnInit/OnDestroy│ │ 与配置持久化 │
│  portkill...)    │ │  EnsureActive)   │ │              │
└──────┬───────────┘ └──────┬───────────┘ └──────┬───────┘
       │                    │                    │
┌──────▼────────────────────▼────────────────────▼───────┐
│  internal/domain (纯领域模型，零外部与平台依赖)        │
│  - Project, ProxyRule, Snapshot, ConnState, AppError   │
└───────────────────────────┬────────────────────────────┘
                            │
┌───────────────────────────▼────────────────────────────┐
│  internal/platform/windows (Windows 原生底层原语)       │
│  - JobObject 进程树保护 · DPAPI 凭据硬件加密           │
│  - 注册表开机自启 · IP Helper 端口表 · ShellExecute 提权│
└────────────────────────────────────────────────────────┘
```

---

## 2. 核心设计与子系统

### 2.1 单体内建按需懒加载架构 (`internal/extapi`)

为了在保持单个二进制文件的同时实现极致的内存与 CPU 节省，系统定义了标准的模块生命周期接口：

```go
type Module interface {
    ID() string
    Title() string
    Description() string
    Route() string
    Icon() string
    Level() ModuleLevel

    // 懒加载生命周期
    OnInit(ctx context.Context) error
    OnDestroy() error
    IsInitialized() bool
}
```

- **按需分配**：用户在未打开该功能或在设置页禁用该模块时，不分配任何内存缓冲区或常驻协程。
- **动态回收**：当在设置页停用某模块时，调用 `OnDestroy()` 清空对象并触发 `runtime.GC()` 与 `debug.FreeOSMemory()` 将内存彻底归还操作系统。

### 2.2 frpc 多实例引擎与进程沙箱 (`internal/modules/frpc`)

1. **Windows JobObject 绑定**：每个启动的 `frpc.exe` 进程均调用 `internal/platform/windows.AssignProcessToJob()` 关联到作业对象，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。即使 HubKit 异常退出，操作系统内核也会强制清理所有子进程。
2. **连接状态嗅探**：`Instance` 在读取标准输出/错误流时实时分析特征词（如 `login to server success`, `authorization failed`, `connect to server error`），实时更新 `ConnState` 并通过 Wails Event 推送到前端。
3. **DPAPI 凭据加密与临时文件治理**：
   - 存储持久化时，调用 `platform/windows.DPAPIEncrypt()` 将 Token 转换为 DPAPI 密文落盘；
   - 停止项目实例时，立即调用 `os.Remove()` 清理 `runtime/frpc/frpc-<id>.toml` 临时配置文件。

### 2.3 平台底层实现 (`internal/platform/windows`)

- **`autostart.go`**：管理注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\HubKit` 实现开机静默启动。
- **`dpapi.go`**：封装 Windows `CryptProtectData` 与 `CryptUnprotectData`。
- **`job_windows.go`**：封装 Windows JobObject 作业对象原语。
- **`process_windows.go`**：基于 `OpenProcess` 与 `QueryFullProcessImageNameW` 获取进程路径与启动时间，用于防误杀校验。
- **`net_windows.go` & `port_windows.go`**：封装 `iphlpapi.dll` 获取网卡、ARP 表、TCP/UDP 扩展连接表。

---

## 3. 持久化与便携化设计 (`internal/settings`)

- **路径判定**：
  - 启动时若在可执行文件同级目录检测到 `data/` 目录，则判定为 **Portable 便携模式**，所有数据、日志、版本文件均存放在 `data/` 下；
  - 否则进入 **Standard 标准模式**，数据存储在 `%APPDATA%/HubKit/`。
- **并发安全原子写**：配置保存采用“写入临时文件 + `os.Rename`”策略，杜绝因程序异常断电造成 JSON 文件损坏。
