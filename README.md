# HubKit

> 🚀 **极轻量现代桌面开发者网络与内网穿透工具箱**（Go + Wails v3 + Vue 3）
> 面向全栈与后端开发者的 Windows 原生工具箱：**frpc 多实例穿透**为旗舰核心，内置**端口释放、局域网扫描、公网 IP/Ping/Traceroute 诊断、系统快捷直达**等实用功能，装进同一个便携单二进制。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-%E2%89%A51.24-00ADD8?logo=go)](https://go.dev/)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0--beta-DF1C24?logo=wails)](https://v3.wails.io/)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vue.js)](https://vuejs.org/)

---

## 🌟 核心特性

### 1. ⚡ frpc 多实例与沙箱进程管理（旗舰核心）
- **多实例独立并发**：每个项目对应独立 `frpc.exe` 进程，支持同时联调多个内网穿透服务端。
- **Windows JobObject 隔离**：利用操作系统内核级作业对象（JobObject），主程序退出或崩溃时所有子进程一并被内核强制清理，**彻底告别孤儿进程与端口残留占用**。
- **配置一键分享与导入**：
  - 支持将服务端连接与代理规则序列化生成 `frp://<base64>` 协议链接，一键复制分享。
  - 支持多端一键导入 `frp://` 链接或原始 `frpc.toml` 文件内容。
- **代理规则批量端口区间导入**：支持 `8080-8085`、`8080,8081,8082` 等离散/连续端口范围批量映射生成规则。
- **TOML 实时双向预览**：规范生成 frp v0.53+ 标准 TOML 格式，支持自定义域名、TCP/UDP、HTTP/HTTPS、STCP/XTCP。
- **官方版本管理与下载**：内置 GitHub Releases 官方源与国内镜像加速下载，支持 SHA256 完整性校验与本地 `frpc.exe` 一键导入。
- **实时日志与脱敏流**：单实例独立日志抽屉，自动脱敏 Token/Secret，ANSI 彩色终端日志过滤。

### 2. 🔍 网络全能诊断与公网探测
- **出口公网 IP & IPv6**：聚合国内外高可用查询源，双栈自动回退，精准识别家宽、专线与公网 IPv6。
- **全网卡拓扑透视**：物理网卡、虚拟网卡（Hyper-V/WSL/VPN）、网关、DNS、临时 IPv6 地址一览。
- **ICMP Ping 稳定性探测**：内置纯 Go 探测引擎，支持发包计数、实时丢包率、最小/最大/平均 RTT 统计。
- **Traceroute 路由追踪**：系统级路由跳跃追踪（Windows `tracert` / Linux `traceroute`），内置 `CREATE_NO_WINDOW` 静默无黑窗口执行。

### 3. 🎯 端口占用精准排查与一键释放 (PortKill)
- 快速查询指定本地端口（TCP/UDP）占用情况。
- 深度提取占用进程名称、PID、启动时间、可执行文件完整路径及命令行参数。
- 支持三级提权与强制安全释放。

### 4. 📡 局域网活跃设备与服务发现 (LAN Scanner)
- 局域网 CIDR 自动探测与并发活跃 IP 扫描。
- MAC 地址与网络设备硬件厂商（OUI 数据库）精准匹配识别。
- 支持 mDNS / SSDP 服务发现。

### 5. 🧰 开发者快捷直达 (System Quick Launch)
- 快捷一键打开系统 `hosts` 文件（记事本即开即改）。
- 一键直达 `AppData` 目录、系统环境变量设置、网络适配器配置（`ncpa.cpl`）、设备管理器等。

---

## 🛠️ 技术架构与优势

| 维度 | HubKit (本项目) | 传统 Electron 类工具 | 传统 GUI 类工具 |
|---|---|---|---|
| **技术底座** | **Go + Wails v3 + Vue 3** | Electron + Node.js | Go + IMGUI / Fyne |
| **内存占用** | **~25 MB**（极低资源消耗） | 150MB ~ 300MB+ | 30MB ~ 50MB |
| **进程治理** | **Windows JobObject 绑定**（零孤儿进程） | Node 子进程（易残留） | 简单 Process Kill |
| **多实例支持** | **原生支持多项目独立并行运行** | 仅单配置单运行 | 仅单配置运行 |
| **配置分享** | **`frp://` 链接一键分享 + 批量端口导入** | 部分支持 | 不支持 |
| **开发者套件** | **内置 Ping/路由追踪/端口释放/局域网发现** | 仅单一穿透功能 | 仅单一穿透功能 |

---

## 🚀 快速开始

### 前置要求

- **Go** ≥ 1.24
- **Node.js** ≥ 20
- **Wails 3 CLI** & **Task**：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
go install github.com/go-task/task/v3/cmd/task@latest
```

### 安装依赖

```bash
cd frontend && npm install && cd ..
```

### 本地开发运行 (HMR 实时热重载)

```bash
task dev
```

### 编译打包产物

```bash
task build     # 产出便携版单二进制 bin/hubkit.exe
```

---

## 📁 目录结构

```text
hubkit/
├─ cmd/
│  └─ hubkit/                    # 应用入口（Main 装配）
├─ internal/
│  ├─ app/                       # Composition Root (Wails 窗口与服务注入)
│  ├─ domain/                    # 纯领域模型 (Project, ServerConfig, ProxyRule 等)
│  ├─ modules/                   # 独立工具模块
│  │  ├─ frpc/                   # 旗舰模块：多实例引擎 / 版本下载 / TOML生成 / 校验
│  │  ├─ publicip/               # 公网 IP / 网卡信息 / ICMP Ping / Traceroute
│  │  ├─ portkill/               # 端口占用侦测与进程释放
│  │  └─ lan/                    # 局域网主机扫描与设备识别
│  ├─ platform/                  # 平台底层实现（Windows JobObject / 网卡 / 进程）
│  ├─ logging/                   # slog 结构化日志
│  └─ settings/                  # 便携化路径与配置存储
├─ frontend/                     # Vue 3 + TypeScript 前端
│  ├─ src/
│  │  ├─ views/                  # 业务视图（Frpc项目、版本、公网IP/Ping/路由追踪、端口释放、LAN扫描、设置等）
│  │  ├─ components/             # 复用组件（FrpcProjectEditor 配置编辑/批量端口/分享等）
│  │  └─ App.vue / main.ts       # 侧边栏与主应用布局
│  └─ bindings/                  # Wails v3 自动生成的 Go-JS API 绑定
└─ Taskfile.yml                  # 自动化开发与构建任务
```

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。
