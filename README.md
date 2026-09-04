# Hanxi

> **开源工具工作台**（Go + Wails v3 + Vue 3）
> 集中安装、管理与运行常用开源软件。Hanxi 以两条主线组织能力：**自建功能模块**（frpc 内网穿透、网络诊断、端口查杀、开发环境检测、局域网快传、随手记等）与**第三方桌面工具托管**（Snipaste、Everything、QuickLook、Keyviz、LiteMonitor、NanaZip、EarTrumpet、果核看图、ddns-go 等 17 款），统一提供版本管理、完整性校验、JobObject 进程托管、系统托盘与本地数据管理能力。
>
> **v0.3.0 品牌断代**：产品标识、进程名和标准数据目录已切换为 Hanxi，不读取旧版数据、自启项或单实例标识。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-%E2%89%A51.24-00ADD8?logo=go)](https://go.dev/)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0--beta-DF1C24?logo=wails)](https://v3.wails.io/)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vue.js)](https://vuejs.org/)

---

## 🌟 已集成工具

### 一、自建功能模块

#### 1. ⚡ frpc 多实例与沙箱进程管理
- **多实例独立并发**：每个项目对应独立 `frpc.exe` 进程，支持同时联调多个内网穿透服务端。
- **全协议与精细化配置**：
  - 支持 **TCP / UDP / HTTP / HTTPS / STCP / XTCP** 全类型穿透；
  - **STCP/XTCP 访客端 (Visitor)** 完整支持：通过 `[[visitors]]` 架构轻松打通安全点对点与 P2P 隧道；
  - **底层传输协议自由切换**：支持 `TCP (默认)`、`KCP (抗高丢包)`、`QUIC`、`WebSocket`、`WSS`；
  - **高级企业级特性**：支持上游代理 (`proxyURL`)、HTTP `hostHeaderRewrite` 虚拟主机重写、`proxyProtocolVersion` (v1/v2 真实 IP 透传) 及单规则带宽限速。
- **可视化表单 ⇋ TOML 源码双向实时同步**：支持在可视化表单与原始 TOML 之间无损一键切换编辑与语法校验。
- **穿透项目与版本管理统一工作台**：单入口 Tab 自由切换穿透项目与版本管理，官方 GitHub Releases / 国内镜像加速一键下载、SHA256 完整性校验、指数退避重试与本地 `frpc.exe` 快速导入。
- **Windows JobObject 隔离**：利用操作系统内核级作业对象（JobObject），主程序退出或崩溃时所有子进程一并被内核强制清理，**彻底告别孤儿进程与端口残留占用**。
- **连接状态实时嗅探**：实时分析 `frpc` 控制台日志特征词，毫秒级感知服务端连接状态（已连接 / 认证失败 / 重连中 / 异常），告别“进程存活但穿透未通”的假绿状态。
- **Windows DPAPI 硬件级凭据加密**：服务端连接 Token 采用 Windows 原生 `CryptProtectData` (DPAPI) 绑定当前用户凭据安全加密落盘，杜绝明文凭证泄露；停止实例时自动擦除运行时生成的临时 TOML 配置文件。
- **配置一键分享与导入**：支持 `frp://<base64>` 协议链接一键复制分享，支持多端一键导入 `frp://` 链接或原始 `frpc.toml` 文件内容。
- **代理规则批量端口区间导入**：支持 `8080-8085`、`8080,8081,8082` 等离散/连续端口范围批量映射生成规则。
- **实时日志与脱敏流**：单实例独立日志抽屉，自动脱敏 Token/Secret，ANSI 彩色终端日志过滤。

#### 2. 🤖 微信机器人助手 (WeChat ClawBot)
- **扫码快捷登录**：内置微信机器人协议，支持前端一键拉取二维码并轮询扫码确认状态，支持多账号与会话保持。
- **AES 安全加密通信**：支持配置 Token 与 EncodingAESKey 进行高强度消息加解密。
- **长轮询与事件监听**：支持长轮询实时监听微信事件，接收群聊/私聊指令，入站文件自动落盘。
- **多模态消息推送**：支持文本、图文卡片、Markdown 与多格式文件推送。

#### 3. 🔎 端口扫描与服务指纹识别 (PortScan)
- **高并发端口探测**：支持单端口、连续区间（`8000-8080`）、离散列表与常用预设（Web开发/数据库/TOP 100/系统保留端口）。
- **Nmap 深度指纹识别**：集成纯 Go Nmap 探针引擎（`gonmap`），自动识别协议类型、组件版本（Nginx/OpenSSH/MySQL/Redis 等）与 Web 状态。
- **快捷联动操作**：Web 端口浏览器一键直达，支持全量开放端口一键复制与结果流式实时渲染。

#### 4. 🎯 端口占用精准排查与一键释放 (PortKill)
- 快速查询指定本地端口（TCP/UDP）占用情况与全量监听端口表。
- 深度提取占用进程名称、PID、启动时间、可执行文件完整路径及命令行参数。
- 采用 Windows 原生 `ShellExecuteEx(runas)` 快速 UAC 提权，支持三级提权与强制安全释放。

#### 5. 📡 局域网活跃设备与服务发现 (LAN Scanner)
- 局域网 CIDR 自动探测与并发活跃 IP 扫描。
- MAC 地址与网络设备硬件厂商（OUI 数据库）精准匹配识别。
- 支持设备备注标注与 IP 一键复制，扫描进度实时推送、可随时取消。

#### 6. 🔍 网络全能诊断与公网探测
- **出口公网 IP & IPv6**：聚合国内外高可用查询源，双栈自动回退，精准识别家宽、专线与公网 IPv6。
- **全网卡拓扑透视**：物理网卡、虚拟网卡（Hyper-V/WSL/VPN）、网关、DNS、临时 IPv6 地址一览。
- **ICMP Ping 稳定性探测**：内置纯 Go 探测引擎，支持发包计数、实时丢包率、最小/最大/平均 RTT 统计。
- **Traceroute 路由追踪**：系统级路由跳跃追踪（Windows `tracert`），内置 `CREATE_NO_WINDOW` 静默无黑窗口执行。
- **WiFi 密码查看**：一键导出本机已保存 Wi-Fi 配置与明文密码（`netsh wlan`）。

#### 7. 🧪 开发环境检测与官网版本通道 (EnvCheck)
- **本机工具链一键盘点**：自动检测 Git、Go、Node.js、Java、Python、.NET 等开发工具的安装状态、版本与安装路径，支持在资源管理器中直接定位。
- **官网最新版本的查询**：实时拉取各工具官方发布通道的最新版本，按本机版本线置顶展示，并给出 winget 等包管理器升级命令提示。
- **.NET 深度检测**：并排列出全部已安装的 .NET 运行时/SDK，对照微软官方支持线（LTS/STS/EOL）标记生命周期状态。

#### 8. 📤 局域网文件快传 (FileShare)
- **零客户端分享站**：在局域网起一个文件/文本快传服务，手机扫码即可互传，无需安装任何 App。
- **端点自动枚举**：自动列出本机各网卡可用监听端点，收件箱目录统一管理收到的文件。
- **文本投递联动随手记**：收到的文本/剪贴板内容可自动落入「极客随手记」，形成收集闭环。

#### 9. 📝 极客随手记 (Memo)
- 本地持久化备忘与代码片段：快速新建、置顶、全文检索与一键复制。
- 敏感内容脱敏开关：开启后列表页遮蔽敏感片段，兼顾分享与隐私。
- 数据统计与实时事件同步（与快传等模块联动刷新）。

### 二、第三方桌面工具托管（托管模式）

Hanxi 将 16 款常用开源/免费桌面工具纳入统一管理。除 frpc 等特例外，托管模块共享同一套标准骨架：

- **版本管理**：上游 Releases / 官网清单侦查 → 多层完整性校验（GitHub digest / SHA256SUMS / 官方哈希清单 / 字节数 + PE 版本核对）→ 下载、本地导入与版本删除；
- **进程托管**：Windows JobObject 绑定的启停引擎、进程枚举/互斥体探测运行态、Win32 唤窗 / 官方单实例命令通道唤起窗口、"跟随 Hanxi 退出"开关、桌面快捷方式创建；
- **合规红线**：**全部按需用户侧下载，Hanxi 仓库与安装包不捆绑、不再分发任何第三方二进制**（详见 [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)）。

| 模块 | 上游项目 | 许可证 | 托管要点 |
|---|---|---|---|
| **Snipaste 截图贴图** | 官方站点 www.snipaste.com（非 GitHub 分发） | 闭源免费软件 | **脱管模式**：保留原生托盘与快捷键，Hanxi 退出不强杀；官网 sha-1 清单校验下载 |
| **Everything 文件检索** | voidtools（官网分发） | 专有免费软件（ES.exe 为 MIT） | 托管之外**内嵌秒级搜索控制台**：经官方 ES.exe CLI 直查结果、后台索引托管、打开/定位文件 |
| **QuickLook 空格预览** | QL-Win/QuickLook | GPL-3.0 | 便携 zip 安装、命名管道 Quit/Reload 优雅退出、低级键盘钩子强杀零残渣 |
| **Keyviz 按键可视化** | mulaRahul/keyviz | GPL-3.0 | MSI 托管安装与提取、互斥体探测、常驻可视化工具强杀式退出 |
| **LiteMonitor 硬件监控** | Diorser/LiteMonitor | 上游未声明 | 处理 `requireAdministrator` 清单直拒与 UIPI 边界、Win32 直操作唤窗、首启配置种子关更 |
| **CCSwitch 供应商切换** | farion1231/cc-switch | MIT | Claude Code / Codex 供应商切换器纯托管，Tauri 单实例协议唤窗 |
| **MarkerOn 屏幕标注** | ifer47/markeron | MIT | 单实例二次拉起实现标注开关（开启/停止批注） |
| **FlClash 代理客户端** | chen08209/FlClash | GPL-3.0 | Clash 系跨平台客户端托管，第二实例不唤窗时改用 EnumWindows 直接置前台 |
| **NanaZip 归档压缩** | M2Team/NanaZip | 多元许可（MIT + 7-Zip/LGPL/unRAR 等） | **MSIX 安装管理型**：官方 stable MSIXBundle 交由 Windows 为当前用户安装/卸载 |
| **EarTrumpet 音量控制** | File-New-Project/EarTrumpet | MIT（含 Excluded Entities） | **官方直装渠道纳管**：AppInstaller 清单 + winget SHA-256 交叉校验、AUMID 激活启动 |
| **BCU 批量卸载** | BCUninstaller/Bulk-Crap-Uninstaller | Apache-2.0 | Bulk Crap Uninstaller 托管，闲置自动退出 + .NET 运行时依赖检测 |
| **MangoDisk 磁盘清理** | harry0703/MangoDisk | GPL-3.0 | 原版 GUI 纯托管，磁盘扫描/清理/卸载功能均由上游提供 |
| **Recordly 开源录屏** | webadderallorg/Recordly | AGPL-3.0（附加条款） | NSIS 静默安装进托管目录、双发布通道切换、注入开关禁用上游自动更新 |
| **PaperTodo 桌面便签** | snownico0722/PaperTodo | PolyForm Noncommercial 1.0.0 | self-contained / no-runtime 双变体切换，唤窗/收拢/退出走官方命令通道 |
| **PicLite 图片压缩** | amiaoapp/PicLite | GPL-3.0 | 上游仅提供 MSI：采用 `msiexec /a` 管理提取免管理员提权安装 |
| **果核看图** | 果核 ghxi.com（官方自建接口分发，非 GitHub） | 闭源免费软件（Certum 签名） | **多实例上游**：无单实例锁→进程名探测 + 按自有 PID 唤窗/关窗；官方发布接口仅 MD5 + 当前版本，便携 zip 顶层包装目录收割 |
| **ddns-go 动态域名** | jeessy2/ddns-go | MIT | 纯 CLI + Web 面板形态：`DDNS_GO_DAEMON=1` 注入绕开上游服务劫持分支、TCP 端口就绪判定、Quit 前配置写静默期防截断、面板走独立子 Webview 窗口 |

### 三、桌面系统体验与通用设置

- **系统托盘与最小化常驻**：支持关闭主窗口时自动最小化到 Windows 系统托盘，后台静默守护托管进程；托盘**单击**切换主窗口显隐、菜单直达与彻底退出。
- **Windows 开机自启动**：设置页一键开关开机自启（管理注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`），支持后台静默拉起。
- **全局通知中心**：模块下载/实例状态/扫描进度等事件汇聚为分级通知（信息/成功/警告/错误），抽屉式查看与已读管理。
- **内置运行日志查看器**：提供应用全局运行日志文件列表检索、多行日志实时分页查看与历史日志一键清理（凭据自动脱敏）。
- **系统快捷直达 (Quick Launch)**：一键打开系统 `hosts` 文件、环境变量配置、网络适配器（`ncpa.cpl`）等。
- **按需懒加载模块架构**：全部 25 个功能模块支持在设置页按需启停，未启用模块 0 内存与 0 协程常驻；模块导航由后端注册表动态驱动前端渲染。

---

## 🛠️ 技术架构与优势

| 维度 | Hanxi (本项目) | 传统 Electron 类工具 | 传统 GUI 类工具 |
|---|---|---|---|
| **技术底座** | **Go + Wails v3 + Vue 3** | Electron + Node.js | Go + IMGUI / Fyne |
| **内存占用** | **~20 MB ~ 25 MB**（极低资源消耗） | 150MB ~ 300MB+ | 30MB ~ 50MB |
| **凭据安全** | **Windows DPAPI 硬件级加密** | 明文 JSON/TOML 落盘 | 明文存储 |
| **进程治理** | **Windows JobObject 绑定**（零孤儿进程） | Node 子进程（易残留） | 简单 Process Kill |
| **连接感知** | **日志特征词嗅探（毫秒级细粒度状态）** | 仅根据进程存活判定 | 仅根据进程存活判定 |
| **多实例支持** | **原生支持多项目独立并行运行** | 仅单配置单运行 | 仅单配置运行 |
| **配置分享** | **`frp://` 链接一键分享 + 批量端口导入** | 部分支持 | 不支持 |
| **第三方生态** | **15 款桌面工具统一托管**（版本管理+完整性校验+进程监管） | 不支持 | 不支持 |
| **系统集成** | **系统托盘常驻 + 开机自启 + UAC 提权** | 占用大/启动慢 | 功能单一 |
| **开发者套件** | **环境检测 + 端口扫描 + 局域网快传 + 随手记** | 仅单一功能 | 仅单一功能 |

---

## 🚀 快速开始

### 前置要求

- **操作系统**：Windows 10 (22H2+) / Windows 11 x64
- **Go** ≥ 1.26
- **Node.js** ≥ 20.19（或 ≥ 22.12）
- **Wails 3 CLI** & **Task**：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.10
go install github.com/go-task/task/v3/cmd/task@v3.53.1
```

### 安装依赖

```powershell
cd frontend && npm install && cd ..
go mod tidy
```

### 本地开发运行 (HMR 实时热重载)

```powershell
task dev
```

### 编译打包产物

```powershell
task build     # 产出便携版单二进制 bin/hanxi.exe
```

---

## 📁 目录结构

```text
hanxi/
├─ cmd/
│  └─ hanxi/                    # 应用入口（Main 装配与 UAC 提权模式分流）
├─ internal/
│  ├─ app/                      # Composition Root (Wails 窗口、托盘、生命周期与服务注入、25 模块统一注册)
│  ├─ product/                  # 品牌身份常量（名称/标识/版本/数据目录单一真相源）
│  ├─ domain/                   # 纯领域模型 (Project, ServerConfig, ProxyRule, Snapshot 等)
│  ├─ extapi/                   # 模块插件化抽象（Module 契约 / 懒加载注册中心 / 导航与启用状态）
│  ├─ notify/                   # 全局通知中心（分级通知 Hub 与前端事件推送）
│  ├─ modules/                  # 25 个功能模块
│  │  ├─ 自建能力               # frpc / wechat / portscan / portkill / lan / publicip
│  │  │                         # / wifi / envcheck / fileshare / memo
│  │  └─ 工具托管               # snipaste / everything / quicklook / keyviz / litemonitor
│  │                            # / ccswitch / markeron / flclash / nanazip / eartrumpet
│  │                            # / bcu / mangodisk / recordly / papertodo / piclite / guoheview
│  │                            # / ddnsgo
│  │                            # （托管模块标准形态：version/ 版本管理子包 + instance/ 实例引擎子包）
│  ├─ platform/                 # 平台底层（Windows JobObject / DPAPI / 注册表自启 / IP Helper / Appx 包管理 / Shell 提权）
│  ├─ logging/                  # slog 结构化日志与凭据自动脱敏
│  └─ settings/                 # 便携化路径解析与通用设置持久化
├─ frontend/                    # Vue 3 + TypeScript + TailwindCSS 前端
│  ├─ src/
│  │  ├─ views/                 # 各模块业务视图（31 个）与统一工作台布局
│  │  ├─ components/            # 复用组件（配置编辑/批量端口/分享模态框/状态指示器等）
│  │  └─ App.vue / main.ts      # 后端导航注册表驱动的侧边栏动态渲染
│  └─ bindings/                 # Wails v3 自动生成的 Go-JS API 绑定
├─ docs/                         # 项目文档（PRD / 架构设计 / 开发计划 / 开发指南 / 插件机制 / 第三方告知 / 踩坑记录）
└─ Taskfile.yml                  # 自动化开发与构建任务
```

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。托管的第三方工具版权归各自上游项目所有，许可证与合规说明见 [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)。
