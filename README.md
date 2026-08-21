# HubKit

> **frpc 内网穿透开发工具箱** —— Windows 开发者的三个高频动作，一个窗口搞定。

HubKit 是面向 Go/前后端开发者的 Windows 工具箱：**frpc 线上联调**是旗舰模块（多实例、版本管理、TOML 生成、实时日志），**局域网扫描、释放端口、公网 IP** 是内置工具模块。所有模块一律平等，可在设置页独立启停——装进同一个便携单二进制。

> 状态：**开发中（M0 框架骨架已打通）** · 架构文档见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

---

## 功能特性

| 模块 | 能力 | 状态 |
|---|---|---|
| **frpc 联调**（旗舰） | 项目化配置、TOML 生成、**多实例并行**、启停/日志/状态机、导入导出 | 规划（M4） |
| **与版本管理** | GitHub Releases 下载、SHA256 硬校验、多版本并存、每项目锁版本 | 规划（M4） |
| **局域网扫描** | 组合 ICMP + ARP/邻居表、进度/取消、复制 IP | 规划（M2） |
| **释放端口** | 端口直达、PID/进程/路径/启动时间、**复核后终止**、UAC 最小提权 | 规划（M3） |
| **公网 IP** | IPv4/IPv6 查询、provider fallback、严格解析 | 规划（M2） |
| 模块管理 | 所有模块统一启停，导航/服务随开关注入或隐藏 | ✅ 已实现 |
| 便携版 | 绿色单目录 `data/`，随拷随走，不写注册表 | 规划（M5） |

## 技术栈

- **后端**：Go（≥1.26），`golang.org/x/sys` 直接调 Windows API
- **GUI**：Wails 3（v3.0.0-beta.10）+ Vue 3 + TypeScript + Vite
- **架构**：领域层 `domain`（零依赖）→ 平台层 `platform`（仅 Windows）→ 装配层 `app`（Composition Root），详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## 快速开始

前置要求：

- Go ≥ 1.26
- Node ≥ 22（前端构建）
- `wails3` 与 `task` CLI：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
go install github.com/go-task/task/v3/cmd/task@latest
```

安装前端依赖：

```sh
cd frontend && npm install && cd ..
```

开发模式（HMR，弹窗即用）：

```sh
task dev
```

静态检查与构建：

```sh
task check     # go vet + go build
task build     # 产出 bin/hubkit.exe（Windows 便携版）
```

## 目录结构

> 标注：`✅` = 已落地；`(规划 Mx)` = 文档已规划、代码未建。build/ 下的 darwin/linux/ios/android 为 wails3 脚手架产物，仅保留不用于发布。

```text
hubkit/
├─ cmd/
│  ├─ devnetkit/                 # ✅ 入口（main.go，仅装配 + 运行）
│  └─ killhelper_main.go         # (规划 M3) 同一二进制的 UAC helper 模式（-mode=killhelper）
├─ embedassets.go                # ✅ 根级包：//go:embed all:frontend/dist
├─ internal/
│  ├─ app/                       # ✅ Composition Root
│  │  ├─ app.go                  #   ✅ wails 装配：模块注册 + 服务 + 窗口
│  │  ├─ service.go              #   ✅ AppService：应用信息/导航/模块启停
│  │  └─ shutdown.go             #   (规划 M1) 优雅退出编排
│  ├─ extapi/                    # ✅ 模块契约（Module = Extension 统一定义）
│  │  ├─ extension.go            #   ✅ 接口/Info/Nav/Permission/Level/协议号
│  │  └─ registry.go             #   ✅ 注册表：注册/启停/导航聚合
│  ├─ domain/                    # ✅ 纯领域模型（零平台依赖）
│  │  ├─ apperr.go               #   ✅ AppError 统一错误模型
│  │  └─ project.go proxy.go version.go instance.go validate.go  # (规划 M4)
│  ├─ modules/                   # ✅ 工具箱模块（全部平等，统一注册）
│  │  ├─ frpc/extension.go       #   ✅ 旗舰模块（导航承接；业务 M4）
│  │  ├─ lan/extension.go        #   ✅ 局域网扫描（功能 M2）
│  │  ├─ portkill/extension.go   #   ✅ 释放端口（功能 M3）
│  │  └─ publicip/extension.go   #   ✅ 公网 IP（功能 M2）
│  ├─ host/                      # (规划 M1/M3) 特权统一入口：KillVerified/提权/哈希
│  ├─ frpc/（并入 modules/frpc）    # (规划 M4) 业务在 modules/frpc 下（manager/instance/docgen/logstream）
│  └─（frp 版本管理 = frpc 模块内部件，规划 M4）
│  ├─ settings/                  # (规划 M1) 配置/路径解析（便携 + 非便携）
│  ├─ security/                  # (规划 M1) SecretStore 抽象 + 明文实现（预留 DPAPI）
│  ├─ logging/                   # (规划 M1) slog + 轮转 + 脱敏
│  ├─ netcore/                   # (规划 M2) CIDR/私有判断/worker pool
│  └─ platform/                  # (规划 M0) Windows 平台接口与实现
│     ├─ platform.go             #   接口定义（Network/Port/Process/Secret/Job）
│     └─ windows/                #   网卡/ICMP/邻居表/端口表/进程/JobObject/DPAPI/UAC
├─ frontend/
│  ├─ src/
│  │  ├─ main.ts / App.vue       #   ✅ 入口 + 侧边栏壳（浅色主题、模块导航注入）
│  │  ├─ views/                  #   ✅ Home/FrpcProjects/Versions/Settings(模块管理)/About
│  │  ├─ components/             #   (规划 M2) 日志/确认框/复制按钮
│  │  ├─ stores/                 #   (规划 M1) pinia 状态
│  │  └─ api/                    #   bindings（生成） + client 封装
│  ├─ bindings/                  # ✅ wails3 生成产物（勿手改）
│  ├─ index.html                 # ✅
│  └─ vite.config.ts             # ✅
├─ build/                        # ✅ wails3 构建链（windows 为实际发布目标）
├─ docs/                         # ✅ PRD.md · ARCHITECTURE.md
├─ tests/                        # (规划 M5) 集成与平台测试
├─ Taskfile.yml                  # ✅ dev / check / build / package
└─ go.mod                        # ✅ module hubkit
```

## 设计要点（与文档一致）

- **工具箱模块化**：frpc 与其他工具完全平等——同一注册表、同一启停语义、同一导航注入（见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §7）。
- **安全红线**：下载二进制 SHA256 硬校验；终止进程前复核 PID+启动时间+路径；系统进程保护；UAC 最小提权；日志脱敏；默认无遥测。
- **仅 Windows**：Windows 10 22H2 / 11 x64（arm64 尽力）；macOS/Linux 不做实现与发布（决策见 ARCHITECTURE 附录 D）。
- **frpc 来源**：管理官方 `frpc.exe`（多版本）；不内置 frps，服务端由用户自备。

## 文档

- [docs/PRD.md](docs/PRD.md) —— 产品需求与技术方案（v0.4）
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) —— 架构设计（分层、模块框架、平台隔离、附录 A–D）
- [docs/DEVPLAN.md](docs/DEVPLAN.md) —— 开发计划与里程碑路线图（M0–M5）

## 参考项目

产品形态参考 [frpc-desktop](https://github.com/luckjiawei/frpc-desktop)（Electron/Vue）与 [FrpGUI](https://gitee.com/kuriyama-mirai/frp-gui)（Go/Shirei）；三向对比见 ARCHITECTURE 附录 C——HubKit 的差异化在「开发者工作流 + 多实例 + 安全设计」。

## License

MIT