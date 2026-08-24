# HubKit

> 🚀 **极轻量现代桌面开发者网络与内网穿透工具箱**（Go + Wails v3 + Vue 3）
> 面向全栈与后端开发者的 Windows 原生工具箱：以 **frpc 多实例穿透** 为核心，集成了 **微信机器人助手、端口占用释放、Nmap 服务指纹扫描、局域网设备发现、公网 IP/Ping/Traceroute 诊断、系统级快捷直达、运行日志查看** 等高频能力，内置 **Windows DPAPI 凭据加密、JobObject 进程隔离、系统托盘常驻与开机自启**，装进同一个便携单二进制。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-%E2%89%A51.24-00ADD8?logo=go)](https://go.dev/)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0--beta-DF1C24?logo=wails)](https://v3.wails.io/)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vue.js)](https://vuejs.org/)

---

## 🌟 核心特性

### 1. ⚡ frpc 多实例与沙箱进程管理（旗舰核心）
- **多实例独立并发**：每个项目对应独立 `frpc.exe` 进程，支持同时联调多个内网穿透服务端。
- **Windows JobObject 隔离**：利用操作系统内核级作业对象（JobObject），主程序退出或崩溃时所有子进程一并被内核强制清理，**彻底告别孤儿进程与端口残留占用**。
- **连接状态实时嗅探**：实时分析 `frpc` 控制台日志特征词，毫秒级感知服务端连接状态（已连接 / 认证失败 / 重连中 / 异常），告别“进程存活但穿透未通”的假绿状态。
- **Windows DPAPI 硬件级凭据加密**：服务端连接 Token 采用 Windows 原生 `CryptProtectData` (DPAPI) 绑定当前用户凭据安全加密落盘，杜绝明文凭证泄露；停止实例时自动擦除运行时生成的临时 TOML 配置文件。
- **配置一键分享与导入**：
  - 支持将服务端连接与代理规则序列化生成 `frp://<base64>` 协议链接，一键复制分享。
  - 支持多端一键导入 `frp://` 链接或原始 `frpc.toml` 文件内容。
- **代理规则批量端口区间导入**：支持 `8080-8085`、`8080,8081,8082` 等离散/连续端口范围批量映射生成规则。
- **TOML 实时双向预览**：规范生成 frp v0.53+ 标准 TOML 格式，支持自定义域名、TCP/UDP、HTTP/HTTPS、STCP/XTCP。
- **官方版本管理与自动重试**：内置 GitHub Releases 官方源与国内镜像加速下载，支持 SHA256 完整性校验、指数退避自动重试与本地 `frpc.exe` 一键导入。
- **实时日志与脱敏流**：单实例独立日志抽屉，自动脱敏 Token/Secret，ANSI 彩色终端日志过滤。

### 2. 🤖 微信机器人助手 (WeChat ClawBot)
- **扫码快捷登录**：内置微信机器人协议，支持前端一键拉取二维码并轮询扫码确认状态。
- **AES 安全加密通信**：支持配置 Token 与 EncodingAESKey 进行高强度消息加解密。
- **长轮询与事件监听**：支持长轮询实时监听微信事件，接收群聊/私聊指令。
- **多模态消息推送**：支持文本、图文卡片、Markdown 与多格式文件推送。

### 3. 🔎 端口扫描与服务指纹识别 (PortScan)
- **高并发端口探测**：支持单端口、连续区间（`8000-8080`）、离散列表与常用预设（Web开发/数据库/TOP 100/系统保留端口）。
- **Nmap 深度指纹识别**：集成纯 Go Nmap 探针引擎（`gonmap`），自动识别协议类型、组件版本（Nginx/OpenSSH/MySQL/Redis 等）与 Web 状态。
- **快捷联动操作**：Web 端口浏览器一键直达，支持全量开放端口一键复制与结果流式实时渲染。

### 4. 🎯 端口占用精准排查与一键释放 (PortKill)
- 快速查询指定本地端口（TCP/UDP）占用情况。
- 深度提取占用进程名称、PID、启动时间、可执行文件完整路径及命令行参数。
- 采用 Windows 原生 `ShellExecuteEx(runas)` 快速 UAC 提权，支持三级提权与强制安全释放。

### 5. 📡 局域网活跃设备与服务发现 (LAN Scanner)
- 局域网 CIDR 自动探测与并发活跃 IP 扫描。
- MAC 地址与网络设备硬件厂商（OUI 数据库）精准匹配识别。
- 支持 mDNS / SSDP 服务发现。

### 6. 🔍 网络全能诊断与公网探测
- **出口公网 IP & IPv6**：聚合国内外高可用查询源，双栈自动回退，精准识别家宽、专线与公网 IPv6。
- **全网卡拓扑透视**：物理网卡、虚拟网卡（Hyper-V/WSL/VPN）、网关、DNS、临时 IPv6 地址一览。
- **ICMP Ping 稳定性探测**：内置纯 Go 探测引擎，支持发包计数、实时丢包率、最小/最大/平均 RTT 统计。
- **Traceroute 路由追踪**：系统级路由跳跃追踪（Windows `tracert`），内置 `CREATE_NO_WINDOW` 静默无黑窗口执行。

### 7. 🧰 桌面系统体验与通用设置
- **系统托盘与最小化常驻**：支持关闭主窗口时自动最小化到 Windows 系统托盘，后台静默守护穿透进程；托盘支持双击唤醒、菜单直达与彻底退出。
- **Windows 开机自启动**：设置页一键开关开机自启（管理注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`），支持后台静默拉起。
- **内置运行日志查看器**：提供应用全局运行日志文件列表检索、多行日志实时分页查看与历史日志一键清理。
- **系统快捷直达 (Quick Launch)**：一键打开系统 `hosts` 文件、环境变量配置、网络适配器（`ncpa.cpl`）等。
- **按需懒加载插件架构**：所有工具模块支持在设置页按需启停，未启用模块 0 内存与 0 协程常驻。

---

## 🛠️ 技术架构与优势

| 维度 | HubKit (本项目) | 传统 Electron 类工具 | 传统 GUI 类工具 |
|---|---|---|---|
| **技术底座** | **Go + Wails v3 + Vue 3** | Electron + Node.js | Go + IMGUI / Fyne |
| **内存占用** | **~20 MB ~ 25 MB**（极低资源消耗） | 150MB ~ 300MB+ | 30MB ~ 50MB |
| **凭据安全** | **Windows DPAPI 硬件级加密** | 明文 JSON/TOML 落盘 | 明文存储 |
| **进程治理** | **Windows JobObject 绑定**（零孤儿进程） | Node 子进程（易残留） | 简单 Process Kill |
| **连接感知** | **日志特征词嗅探（毫秒级细粒度状态）** | 仅根据进程存活判定 | 仅根据进程存活判定 |
| **多实例支持** | **原生支持多项目独立并行运行** | 仅单配置单运行 | 仅单配置运行 |
| **配置分享** | **`frp://` 链接一键分享 + 批量端口导入** | 部分支持 | 不支持 |
| **系统集成** | **系统托盘常驻 + 开机自启 + UAC 提权** | 占用大/启动慢 | 功能单一 |
| **开发者套件** | **微信机器人 + 端口扫描 + 局域网 + Ping** | 仅单一穿透功能 | 仅单一穿透功能 |

---

## 🚀 快速开始

### 前置要求

- **操作系统**：Windows 10 (22H2+) / Windows 11 x64
- **Go** ≥ 1.24
- **Node.js** ≥ 20.x
- **Wails 3 CLI** & **Task**：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
go install github.com/go-task/task/v3/cmd/task@latest
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
task build     # 产出便携版单二进制 bin/hubkit.exe
```

---

## 📁 目录结构

```text
hubkit/
├─ cmd/
│  └─ hubkit/                    # 应用入口（Main 装配与 UAC 提权模式分流）
├─ internal/
│  ├─ app/                       # Composition Root (Wails 窗口、托盘、生命周期与服务注入)
│  ├─ domain/                    # 纯领域模型 (Project, ServerConfig, ProxyRule, Snapshot 等)
│  ├─ extapi/                    # 模块插件化抽象（OnInit / OnDestroy / 懒加载注册中心）
│  ├─ modules/                   # 独立工具模块
│  │  ├─ frpc/                   # 旗舰模块：多实例引擎 / 版本下载 / TOML生成 / 状态嗅探 / DPAPI存储
│  │  ├─ wechat/                 # 微信机器人：扫码登录 / AES加密通信 / 消息推送
│  │  ├─ portscan/               # 端口扫描：高并发探测 / Gonmap Nmap 服务指纹识别
│  │  ├─ portkill/               # 端口占用排查 / 进程指纹比对 / UAC 提权释放
│  │  ├─ lan/                    # 局域网扫描：活跃 IP / MAC / OUI 厂商匹配 / mDNS
│  │  └─ publicip/               # 公网 IP / 网卡拓扑 / ICMP Ping / Traceroute
│  ├─ platform/                  # 平台底层（Windows JobObject / DPAPI / 注册表自启 / IP Helper）
│  ├─ logging/                   # slog 结构化日志与凭据自动脱敏
│  └─ settings/                  # 便携化路径解析与通用设置持久化
├─ frontend/                     # Vue 3 + TypeScript + TailwindCSS 前端
│  ├─ src/
│  │  ├─ views/                  # 业务视图（Frpc项目、版本、微信机器人、端口扫描、端口释放、LAN、网络诊断、日志、设置等）
│  │  ├─ components/             # 复用组件（配置编辑/批量端口/分享模态框/状态指示器等）
│  │  └─ App.vue / main.ts       # 侧边栏导航与主应用布局
│  └─ bindings/                  # Wails v3 自动生成的 Go-JS API 绑定
├─ docs/                         # 项目文档（PRD / 架构设计 / 开发计划 / 开发指南 / 插件机制）
└─ Taskfile.yml                  # 自动化开发与构建任务
```

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。
