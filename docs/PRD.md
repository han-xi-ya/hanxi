# 跨平台开发网络工具箱 —— 需求与技术方案（PRD v0.3）

> 暂定产品名：HubKit  
> 文档版本：v0.4  
> 文档日期：2026-08-20  
> 主目标平台：**仅 Windows** 10 22H2、Windows 11 x64（arm64 尽力）；macOS/Linux 不做实现与发布，仅在架构上保留接口纯度  
> 发布形态：**便携版（默认交付，绿色单目录）**；安装器为可选 P2  
> 开发语言：Go（GUI + 核心 + 平台适配均用 Go）

---

## 1. 定位与竞争结论

本产品参考开源项目 [frpc-desktop](https://github.com/luckjiawei/frpc-desktop)（MIT，Electron + Vue + SQLite）的产品形态，用 Go + Wails 3 重构。

**产品身份：Windows 开发者工具箱**——所有能力都是**统一模块**：frpc 联调是旗舰模块（项目、多实例、版本管理、日志、托盘），局域网扫描、端口查杀、公网 IP 是工具模块；每个模块可在设置页独立启停，模块之间完全平等，未来可支持外部模块（独立安装/拆卸）。

### 1.1 参考的价值点（来自对 frpc-desktop 的分析）

- **frp 版本矩阵管理**：版本列表、按平台/架构下载、解压、SHA256 校验。
- **TOML 生成**：把 UI 配置翻译为 frpc `toml`，支持 tcp/udp/http/https/stcp/xtcp 等代理类型与批量端口。
- **进程生命周期**：启动/停止/重启 frpc，日志读取与推送，断线提示与自动恢复。
- **导入导出**：配置的导入（含 frpc.toml 识别）与导出。
- **平台适配**：Windows 用 `taskkill /T /F` 结束进程树，macOS 用特权 launcher、Linux 用 `pkill`；处理后台静默启动与开机自启。
- **多语言**：EN / 简体中文 i18n。

### 1.2 不推荐直接复用的部分

- **明文保存凭据**：原版把 `auth.token`、frpc `webServer.user/password` 直接写进 `userData/config/*.toml` 并落盘，无加密。我们首期先实现**明文 + 抽象接口**（动机：跨平台统一、开发快），但**外网/生产环境必须换 DPAPI 等平台加密**，并作为验收安全红线。
- **未做进程身份复核**：原版停止/结束进程前只校验 PID 存活，不校验 PID/进程启动时间/可执行路径是否变化，存在误杀风险。我们终止进程前必须复核。
- **部分误导性混淆**：MD5 命名目录只是混淆，不能防杀软，也不能替代哈希校验。

### 1.3 相对原版的差异化价值

| 能力 | frpc-desktop | HubKit |
|---|---|---|
| frpc 管理（核心） | 有（成熟） | **有**：重构版，多实例 |
| 局域网扫描（扩展） | 无 | **有**：IPv4 `/24` 组合探测 + 复制 IP |
| 端口查杀（扩展） | 无 | **有**：首页端口直达 + IP Helper 查 PID + 复核后中止 |
| 扩展机制 | 无 | **内建扩展模块**：统一接口 + 设置开关，未来可拆子进程插件 |
| 单二进制跨平台 | Electron（大） | **Go 单二进制**（Wails3 + Vue） |
| 技术栈 | TS/Node/Electron | Go + Wails3/Vue |

### 1.4 首期明确不做

- 不做全端口扫描 / 隐蔽扫描 / 口令爆破 / 漏洞扫描；
- 不做默认公网网段扫描；
- 不做远程结束其他机器进程；
- 不部署 frps 服务端；
- 不做 OpenVPN / 代理服务器 / 流量劫持；
- 不做抓包 / HTTPS 解密；
- 不做 1–65535 全量端口高速扫描；
- 不做第三方插件热插拔 / 插件市场；
- 不在 macOS/Linux 正式发布安装包（首期）。

---

## 2. 术语澄清（重要）

- **frpc（Client）**：我们管理的是 *frpc*。原版名“frpc-desktop”，实际上它**内置 frp 版本下载也包含 frps**（Release 资产里既有 `frpc_*` 也有 `frps_*`）。我们首期**仍然只下载/管理 frpc**，不包含 frps。
- **frps（Server）**：用户需要自己已有 frps 服务端。我们**不提供** frps 下载与部署；连接地址、token 由用户填写。

---

## 3. 核心用户工作流

### 3.0 核心：frpc 工作流

#### 工作流 C：frpc 线上联调（核心）

1. 新建开发项目，填 frps 地址/端口/token，保存本地探察时使用**明文（首期）+ 接口抽象**。
2. 添加代理（tcp/udp/http/https 等）。
3. 点击“开始联调”：生成 TOML → `frpc -c config.toml` 启动 → 实时日志 → 可复制远程 URL。
4. 多项目并行运行，每个实例独立日志与状态。
5. 停止时先温和再强制，避免孤儿进程；链接断开可提示并可自动重连。

#### 工作流 D：版本管理（核心）

1. “frp 版本管理”页：从 GitHub Releases 拉取版本列表。
2. 按当前平台/架构匹配资产（win32_x64 → windows/amd64 等）。
3. 下载 → 解压 → SHA256 校验 → 登记到本地版本库。
4. 可删除本地版本；启动项目时校验所依赖版本存在。

### 3.1 扩展工作流（内置扩展模块，设置页可开关）

#### 工作流 A：局域网找机器（内置扩展）

1. 打开“局域网扫描”。
2. 自动选择默认路由网卡，默认扫描其 `/24` 私有网段。
3. 组合 ICMP + ARP/邻居表 + Neighbor 探测，进度可取消。
4. 结果以 IPv4 为主列，显示在线依据/延迟/MAC/主机名（尽力）。
5. 双击行或按钮复制单个 IP；支持批量复制。

#### 工作流 B：端口查杀（内置扩展）

1. 首页输入端口（如 `8080`），Enter。
2. 显示协议/监听地址/PID/进程名/路径/启动时间。
3. 点击“释放端口”，**复核 PID+启动时间+路径**后二次确认。
4. 权限不足触发 UAC（Windows）；成功后复查端口。

#### 工作流 E：公网 IP（内置扩展）

1. 首页展示公网 IPv4/IPv6 摘要（异步，provider fallback，严格解析）。
2. 可刷新、复制；IPv6 不可用独立降级提示。

---

## 4. 功能需求

### 4.0 全局 UI / 导航

左导航分两区：
- **核心**：frpc 项目、版本管理、设置、关于；
- **扩展**：局域网扫描、释放端口、公网 IP（仅扩展启用时显示，具体见 6.6 扩展框架）。

---

### 4.1 Dashboard

- D1: 显示本机网卡、IPv4(6)、默认网关、公网 IPv4/IPv6（异步刷新，fallback）。
- D2: 端口直达输入框（Enter 即跳转端口结果）。
- D3: 最近使用的 frpc 项目一键启动/停止。

---

### 4.2 局域网扫描（内置扩展）

- L1: 网卡枚举与默认路由选择；只允许私有网段；默认 `/24`；单次上限 4096 地址。
- L2: 主机发现：ICMP + ARP/邻居表 + Neighbor；满足任一即上线；**不依赖单一 Ping**。
- L3: 并发 1–256（默认 64）；单目标超时 800ms；支持取消（1s 内停止派发）。
- L4: 结果：IP 主列 + 在线依据 + 延迟 + MAC + Hostname(尽力) + 错误。
- L5: 复制单个/批量 IP（换行或逗号）。
- L6 (P1): 结果导出 CSV/JSON。

---

### 4.3 frpc 项目管理（重构自原版）

- F1: 项目模型：name、frps(addr/port/user/auth token/TLS)、localIP/localPort、proxy 列表、log、备注。
- F2: 代理类型：tcp、udp、http、https（http/https 同时支持 subdomain、customDomains 与 locations；stcp/xtcp/visitors 与批量端口为 P1）。
- F3: 配置生成：完整 frpc TOML（`serverAddr`/`serverPort`/`auth`/`log`/`proxies`/`webServer`），不使用 shell 拼接。
- F4: 导入导出：导入 frpc.toml（识别并重建项目）、导出项目配置。
- F5: 启动前校验：字段、占位、本地端口监听检查（存在性警告）、frpc 版本存在。
- F6: 启动/停止/重启（先温和后强制，进程树）；Job Object（Windows）防孤儿；主应用退出时询问。
- F6A: **多实例（P0）**：同一时间可同时运行多个 hubkit 项目，每个实例独立 PID、日志文件、Job Object（Windows）、状态机与退出结果；启动时校验同项目不重复启动、本地端口不冲突；首页可对最近项目逐个启停。
- F7: 实时日志（行流推送）、搜索/复制/清空视图；文件日志轮转（大小+天）；脱敏。
- F8: 状态：Stopped/Starting/Running/Stopping/Failed/Exited；区分“进程运行/已连服务端/业务接口可用”。
- F9 (P1): 开机自启、自动重连/守护（指数退避，仅对用户运行中的实例）。
- F10 (P2): 自动更新与代码签名校验（沿用哈希）。

---

### 4.4 frp 版本管理（重构自原版）

- V1: 来源：GitHub Releases 直连（github.com/fatedier/frp），内置版本/资产/发布时间表；可刷新；实时检测更新。
- V2: 平台匹配：列表按 `(GOOS, GOARCH)` → frp 资产名映射（如 win64 → `windows_amd64`）；支持 `(windows, amd64/arm64)` 优先。
- V3: 下载与安装：进度显示、断点续传（P1）、并发控制；解压 tar.gz/zip；文件校验。
- V4: 安全：下载期间**必须校验 SHA256**（内置已知版本清单；动态释放版本若无哈希不能启动）；用随机名/加密目录避免误配；安装后记录版本目录+hash。
- V5: 删除本地版本：确认后删除目录与记录。

---

### 4.5 释放端口（内置扩展）

- P1: 枚举：Windows IP Helper（TCP/UDP、v4/v6、Owner PID）；macOS `lsof`/`netstat`；Linux `/proc/net/tcp`；**首期只保证 Windows 主实现**。
- P2: 端口直达：输入端口 Enter 查询，优先展示 Listen 记录。
- P3: 进程信息：PID、进程名、路径（尽力）、启动时间；权限不足显示占位。
- P4: 结束前复核：PID+启动时间+路径+端口变化检测，否则取消并提示。
- P5: 风险确认框：进程名/PID/路径/端口 + “未保存数据可能丢失”；不提供一键全杀。
- P6: 权限：Windows 普通权限优先，Access Denied → UAC 提权助手（一次调用一个动作，最小编面）；系统进程（PID 0/4、当前应用自身、保护进程）禁止结束。
- P7: 结果：成功/权限不足/目标变化/系统保护/进程拒绝/已退出/未知错误；结束后复查端口。
- P8 (P1): 结束进程树（Windows）。

---

### 4.6 公网 IP（内置扩展）

- IP1: IPv4/IPv6 分别查询；provider 列表 ≥2，超时 3s、总流程 ≤8s；`net/netip` 严格解析；响应体上限。
- IP2: 显示来源/耗时/更新时间；支持复制。
- IP3: 隐私声明：查询第三方服务会暴露出口 IP；默认无遥测。

---

### 4.7 模块管理（工具箱模块框架）

- X1: 模块清单：id、名称、版本、描述、作者、**级别（builtin/external）**、可否卸载、启用状态；**frpc 与工具模块完全平等**，统一在此管理。
- X2: 设置页展示模块开关；关闭后对应导航项隐藏、后端服务不注册；状态持久化。
- X3: 启停（enable/disable）MVP 全量支持；**独立安装/拆卸（install/remove）仅对 external 级别**（未来子进程模块，P1+），内置模块仅可启停。
- X4: 模块通过宿主 API 执行特权操作（进程复核、系统进程保护、哈希校验由宿主核心强制执行），模块自身不直接触碰 Windows 安全红线。

### 4.8 设置 / 日志 / 诊断

- S1: 本地设置：主题、语言（en/zh）、扫描并发/超时、公网源、frpc 下载目录、日志策略；敏感字段单独处理。
- S2: 存储：
  - **便携模式（默认）**：检测 exe 同目录 `data/`（或 `.portable` 标记文件），配置、日志、`frp-versions`、下载缓存全部保存在 `data/` 下，整个目录可整体复制/搬移；不写注册表、不要求安装器。
  - **非便携模式**：`os.UserConfigDir()/HubKit`（配置）、`os.UserCacheDir()/HubKit`（日志缓存；可用 `--portable=off` 强制切换）。
  - 所有配置写入采用临时文件 + 原子替换。
- S3: 应用日志：结构化（时间/级别/模块/事件/错误），不记录 token/完整认证头/明文配置。
- S4 (P1): 诊断包导出（脱敏）。

---

## 5. 非功能需求

### 5.1 性能

- 冷启动 ≤ 3s（Win10/11）；空闲内存 ≤ 200MB（WebView2 桌面 shell 实测约 150–250MB，作为参考目标）。
- LAN 扫描 `/24` 默认 30s 内；取消 ≤1s。
- 端口直达 ≤ 500ms。
- 公网 IP ≤ 5s。
- UI 主线程不阻塞网络/系统调用。

### 5.2 稳定性

- 长任务全部可取消/超时。
- frpc 生命周期：无孤儿进程；外部二进制存在校验。
- 配置损坏：保留备份并进入恢复。
- 单模块异常不拖垮整个应用。

### 5.3 安全（红线）

1. **首期凭据策略=明文+抽象**：token/secret 以明文保存在配置文件中（与锁定的平台文件一致，且**仅在用户主动告知、自建内网 frp 使用**）。**外网/生产环境必须切换为平台加密（Windows DPAPI / macOS Keychain / Linux SecretService）** —— PRD 明确标注为 rollout 阻断项。
2. **下载即校验**：任何下载的 frp 二进制在运行前必须通过 SHA256 校验（内置清单或发布时预置 hash）；校验失败拒绝运行。
3. **进程复核**：终止进程前必须复核 PID + 启动时间 + 路径。
4. **系统进程保护**：PID 0/4、自身、保护进程禁杀；不得“一键全杀”。
5. **最小权限**：GUI 默认普通权限；UAC 只用于单次操作。
6. **无遥测**：不自动上传扫描/诊断/配置。
7. **无 shell 拼接**：所有启动用 `exec.Command` 参数数组。

### 5.4 兼容性

- P0：Windows 10 22H2 / 11 x64（GUI 验收），依赖 **WebView2 Runtime**（Win11 通常内置；Win10 可能需安装）。
- P0.5：Windows arm64 尽力交叉编译，不作为验收。
- **不支持**：macOS / Linux（不做平台实现与发布；接口纯度保留，未来有需求再开）。
- WebView2 处理：启动时检测 Evergreen Runtime，缺失时引导安装（便携版不自动写系统）；P2 可选捆绑 Fixed-Version Runtime。

---

## 6. 技术方案

### 6.1 技术栈

- **语言**：Go（≥1.22）。
- **GUI**：**Wails 3**（当前为 v3 迭代版本，M0 以 `wails3 version` 实测确认；若 v3 存在阻塞问题，回退 Wails v2 稳定版，核心层不受影响）+ Vue 3 + TypeScript + Vite。
- **前端**：Vue 3 + TypeScript + Vite + Pinia + Vue Router；通过 Wails 生成的类型化绑定调用 Go 服务。
- **平台适配**：`golang.org/x/sys/{windows,unix}` + 各平台系统调用；代码中 `internal/platform/` 分平台实现（`_windows.go` / `_darwin.go` / `_linux.go`）。
- **下载/解压**：标准库 + `archive/tar` + `compress/gzip`；zip。
- **序列化**：`encoding/json`、TOML（`arionum/mustangtoml` 或等）。
- **日志**：`log/slog` + 自写轮转。
- **测试**：`testing` + `httptest`。

### 6.2 架构

```text
┌──────────────────────────────────────────────┐
│              Wails3 + Vue UI                 │
│ Home / LAN / FRPC / Ports / Versions / ...  │
└───────────────────┬──────────────────────────┘
                    │ wails3 service bindings + events
┌───────────────────▼──────────────────────────┐
│              Application Layer               │
│ LanService / FrpcService / PortService /     │
│ VersionService / PublicIpService / Settings  │
│ (wails3 Services 暴露给前端)                  │
└───────────────────┬──────────────────────────┘
                    │ interfaces
┌───────────────────▼──────────────────────────┐
│                Core / Domain                 │
│ Models / Validation / StateMachine /         │
│ WorkerPool / Cancellation / Redaction        │
└───────────────────┬──────────────────────────┘
                    │ adapters
┌───────────────────▼──────────────────────────┐
│            Platform Adapters                 │
│ windows/ darwin/ linux/                      │
│ iphelper / icmp / proc / dpapi / job         │
└──────────────────────────────────────────────┘
```

- 前端只做展示与交互；所有系统能力在 Go 侧，通过 Wails 类型化绑定暴露。
- Wails 仅在 `application layer` 暴露 Service；平台能力通过 interface 抽象，`_windows.go` 等实现。
- 所有长任务接受 `context.Context`，耗时任务用 Wails 3 Task/事件机制推送进度。

### 6.3 目录结构

```text
hubkit/
├─ cmd/hubkit/main.go   # wails3 app 装配
├─ internal/
│  ├─ app/            # 应用生命周期与装配（wails3 Services 注册、模块注册表）
│  ├─ domain/         # 模型与校验
│  ├─ extapi/         # 模块（Extension）接口：能力声明、命令路由、事件通道（统一模块框架）
│  ├─ modules/
│  │  ├─ frpc/        # 模块（旗舰）：frpc 项目、配置、进程（多实例）
│  │  ├─ lan/         # 模块：局域网扫描
│  │  ├─ portkill/    # 模块：释放端口
│  │  └─ publicip/    # 模块：公网 IP
│  ├─ versions/       # frpc 模块内部件：版本下载/安装
│  ├─ settings/
│  ├─ security/       # secrets 抽象+明文实现（预留 DPAPI）
│  ├─ logging/
│  └─ platform/
│     ├─ windows/  # _windows.go
│     ├─ darwin/   # _darwin.go
│     └─ linux/    # _linux.go
├─ frontend/           # Vue3 + TS + Vite（Wails 嵌入编译产物；核心路由 + 扩展路由）
├─ assets/  build/  tests/
├─ Taskfile.yml        # wails3 标准任务入口
├─ go.mod
└─ README.md
```

### 6.4 平台适配要点

| 能力 | Windows | macOS | Linux |
|---|---|---|---|
| 进程枚举/结束 | IP Helper / TerminateProcess / taskkill / JobObject | sysctl / kill | /proc / kill |
| 端口→PID | GetExtendedTcpTable/UdpTable | lsof/netstat | /proc/net |
| ICMP | IP Helper | ping 后台或 sock | raw socket |
| 凭据加密 | DPAPI (预留) | Keychain (预留) | SecretService (预留) |
| 特权提权 | UAC (runas) | osascript/sudo(参考原版) | pkexec/sudo |

首期只对 Windows 做发布与验收；macOS/Linux 仅保证交叉编译 + 冒烟。

### 6.5 frp 版本/下载实现（对齐原版，但安全更强）

- 内置 JSON（`frp-releases.json` + `sha256`），运行时可通过 GitHub API 刷新。
- 下载：`http.Client` + 进度回调；校验 SHA256；解压 tar.gz（过滤 `frpc*`）或 zip。
- 双签名/防篡改：先校验发布清单中的 hash，再落盘/启动。
- 目录：`os.UserConfigDir()/frp-versions/<version>/...`；避免用易猜目录。

---

### 6.6 扩展框架（MVP 内建，面向子进程插件设计）

扩展接口（`internal/extapi`）最小集：

```go
type Extension interface {
    Info() ExtensionInfo        // id / name / version / description / author
    Nav() []NavEntry            // 注册到左侧导航（route + icon + 所属区：扩展区）
    Services() []any            // 注册为 wails3 Services（类型化绑定）
    Permissions() []Permission  // 声明的平台能力（进程结束 / 网络扫描 / 公网访问 / 文件）
    VersionHandshake() string   // 协议版本；未来子进程插件用于握手
}
```

- 注册：启动时扩展注册表加载所有内建扩展；设置页开关持久化到 settings；关闭的扩展：导航隐藏、服务不注册。
- 特权收敛：扩展只能调用宿主 API（`host.ProcessKill`、`host.Scan` 等），宿主强制执行安全红线（进程复核、系统进程保护、哈希校验、UAC 提权）。
- 未来拆子进程插件的映射（不在 MVP 实现，但接口对齐）：
  - `Info()+Permissions()` → 插件 manifest；
  - `Services()` 方法 → JSON-RPC over stdio 的命令路由；
  - `Nav()` → UI 页面由宿主按 manifest 提供的基础 Webview 页面。
- 明确不做：第三方插件市场、签名校验体系、热加载。

## 7. 数据模型（节选）

```go
type FrpcProject struct {
    ID          string
    Name        string
    Server      FrpcServer         // addr/port/user/auth/tls
    Proxies     []Proxy
    LocalIP     string
    LocalPort   uint16
    LogLevel    string
    Notes       string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Proxy struct {
    Name       string
    Type       string   // tcp|udp|http|https|stcp|xtcp
    LocalIP    string
    LocalPort  string   // 支持 "8080" / "8000-8005" / "80,81"
    RemotePort string
    CustomDomains []string
    Subdomain  string
    Locations  []string
    Transport  Transport
    BasicAuth  bool
    HTTPUser   string
    HTTPPassword string
    SecretKey  string   // stcp/xtcp
    ServerName string   // visitors
    BindAddr   string
    BindPort   uint16
    Status     bool
    // ...
}

type InstalledVersion struct {
    ID        string
    Name      string   // v0.68.1
    GOOS      string
    GOARCH    string
    LocalDir  string
    SHA256    string
    InstalledAt time.Time
}
```

---

## 8. 错误处理

统一 `AppError{ Code, Message(zh), Detail, Retryable, Action, Cause(redacted) }`。
错误码示例：`LAN_INVALID_CIDR`、`LAN_RANGE_TOO_LARGE`、`FRPC_BINARY_MISSING`、`FRPC_VERSION_UNSUPPORTED`、`FRPC_CONFIG_INVALID`、`FRPC_START_FAILED`、`PORT_PROCESS_CHANGED`、`PORT_ACCESS_DENIED`、`PORT_PROTECTED_PROCESS`、`VERSION_MISSING_HASH`、`VERSION_INTEGRITY_FAILED`、`PUBLIC_IP_TIMEOUT`…

---

## 9. 测试策略

- 单测：CIDR/私有判断、worker pool 取消、provider fallback、TOML 生成/脱敏、状态机、hash 校验、进程复核逻辑、保护规则。
- 集成：httptest 公网 provider；fake frpc 子进程（正常/崩溃/卡住）；临时监听端口 + PID 映射；hash 校验失败拒绝启动；Job Object 清理。
- 手工矩阵（Win 重点）：Win10/11；普通/管理员；有线/WiFi/VPN/多网卡；IPv4-only/dual-stack；ICMP blocked；无外网/DNS 失败；frpc 正常/认证失败/服务端不可达/版本不兼容；系统进程/普通进程/已退出进程。

---

## 10. 里程碑（单人，核心为 Go 开发者）

- **M0 验证（2–4 天）**：安装 wails3 CLI（`go install`）+ `wails3 init` 脚手架 + WebView2 检测 + Go↔Vue 绑定冒烟（若 v3 阻塞则回退 Wails v2）；Windows ICMP/邻居表/端口→PID；frpc 启停 + JobObject；版本下载+解压+SHA256；安全抽象实现明文版。
- **M1 骨架（3–5 天）**：目录分层、slog 日志、设置持久化、Wails Services 注册与前端导航（Vue Router）、**扩展框架（extapi 接口 + 注册表 + 设置开关 + 导航注入）**、事件机制、错误模型、取消。
- **M2 LAN + 公网 IP 扩展（4–7 天）**：网卡/路由/CIDR、组合发现、进度取消、结果表+复制；公网 provider fallback；均以扩展模块实现。
- **M3 端口扩展（4–7 天）**：IP Helper 查询、进程信息、复核中止、UAC helper（Windows）、保护规则；以扩展模块实现。
- **M4 frpc（6–10 天）**：项目模型、TOML 生成、导入导出、启停/日志/状态机（含**多实例并行**）、版本管理（GitHub 直连+SHA256）。
- **M5 收尾（4–7 天）**：Win 测试矩阵、**便携版发布（默认交付形态，Windows x64）**、文档/README；安装器作为 P2 不进入首期。

**合计约 4–6 周**（不含 macOS/Linux 发布）。

---

## 11. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 未签名二进制 + 自动下载 + 明文凭据 | 信任与安全 | 下载哈希校验强制；凭据默认明文但 UI 明示 + 文档红线；外网前必须 DPAPI |
| GitHub 被墙/限流 | 版本下载失败 | 内置清单先验、失败提示；P1 引入镜像扩展点（接口已预留） |
| Fyne 中文/高DPI | 体验 | M0 验证；字体回退、缩放 |
| 跨平台发布负担 | 范围蔓延 | 首期只发 Windows；其他平台仅编译冒烟 |
| macOS 特权 launcher 安全 | 高危 | 参考原版但改为黑名单/最小命令；或首期 Windows 优先暂缓 |
| 进程树误杀 | 数据丢失 | 复核 PID+start time+path；系统进程保护 |
| frpc 版本碎片化 | 启动失败 | 版本-兼容矩阵 + 官方校验 |

---

## 12. 红线验收（必过）

1. 下载的 frpc 二进制在运行前必须通过 SHA256（内置清单）；不通过绝运行。
2. 停止/结束进程前必须复核 PID、启动时间、可执行路径；变化则取消。
3. PID 0/4、HubKit 自身、受保护进程禁止结束。
4. 默认无遥测、不自动上传任何配置/扫描/诊断数据。
5. 配置加载失败不启动；不做 shell 拼接。
6. **首期明文凭据仅限内网自建 frp 使用；发布说明与 UI 明确标注；外网使用必须切换 DPAPI/Keychain/SecretService**（作为 rollout 阻断）。
7. **绑定安全**：renderer 传入的绑定参数在主进程再次校验（路径、端口、CIDR、PID）；启用 contextIsolation，不启用 nodeIntegration。

---

## 13. 待确认项与默认决策

已确认：

- ✅ **frpc 多实例并行（P0）**：同一时间可运行多个项目，实例间完全隔离；
- ✅ **便携版（P0）**：默认交付形态为绿色单目录便携版；
- ✅ **产品定位与扩展机制**：frpc 为核心，局域网扫描 / 释放端口 / 公网 IP 为**内建扩展模块**（Go 内编译、设置页开关；接口按子进程插件最小集设计，MVP 不实现热加载）。

默认决策（不阻塞开发，如有异议可改）：

1. 产品正式命名/图标 —— 暂用 HubKit。
2. HTTP/HTTPS 联调 —— 同时支持 subdomain 与 customDomains（P0 全部实现）。
3. Windows arm64 —— 默认尽力交叉编译，不作为首期验收。
4. 导出 CSV/JSON —— 默认 P1。
5. 版本清单 —— 默认“内置离线清单 + GitHub 直连刷新”双模式。
6. macOS 权限方案 —— 首期暂缓（Windows 优先），预留接口。

---

*文档结束。*