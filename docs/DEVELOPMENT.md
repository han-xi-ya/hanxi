# Hanxi 开发与构建指南

本文档介绍 Hanxi 的本地环境准备、开发调试模式（HMR）、静态检查、单测执行以及生产编译（便携版）的完整流程。

---

## 1. 前置环境要求

Hanxi 采用 **Go + Wails 3 + Vue 3 (TypeScript/Vite)** 架构，仅面向 Windows x64 平台。

| 工具 / 依赖 | 最低版本 | 说明 |
|---|---|---|
| **操作系统** | Windows 10 (22H2+) / Windows 11 x64 | 纯 Windows 原生调用 |
| **Go** | ≥ 1.22（推荐最新 1.26） | 核心后端与平台 API 调用 |
| **Node.js** | ≥ 20.x（推荐 22.x LTS） | 前端 Vite 构建与 TypeScript 编译 |
| **Wails 3 CLI** | `v3.0.0-beta.10`+ | 负责 bindings 生成与 dev 热重载编排 |
| **Taskfile CLI** | `v3.x` | 统一任务编排执行器 |

### 安装全局 CLI 工具

在终端运行以下命令安装 `wails3` 和 `task` CLI：

```powershell
# 1. 安装 Wails v3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@latest

# 2. 安装 go-task
go install github.com/go-task/task/v3/cmd/task@latest
```

> **提示**：请确保 `%GOPATH%/bin`（通常为 `C:\Users\<你的用户名>\go\bin`）已加入系统环境变量 `PATH`。

---

## 2. 依赖安装

第一次拉取仓库或前端依赖发生变更时，进入前端目录安装 npm 包：

```powershell
# 进入项目根目录
cd hanxi

# 安装前端依赖
cd frontend
npm install
cd ..

# 整理 Go 依赖
go mod tidy
```

---

## 3. 开发模式运行（热重载 Dev）

开发模式会同时启动 Vite Dev Server 与 Wails 3 原生窗口，支持前端 HMR（热更新）与 Go 后端实时调用：

```powershell
task dev
```

运行该命令后：
1. 会在后台启动 Vite（默认端口 `9245`）；
2. 自动根据 Go 代码结构生成 TypeScript 绑定文件到 `frontend/bindings/`；
3. 弹出 Hanxi 开发窗口（浅色主题，尺寸 1200×780）；
4. 修改前端 `frontend/src/` 代码后界面即时热更新；
5. 在界面上可按 `F12` 或右键打开 WebView2 开发者工具进行调试。

---

## 4. 静态检查与单元测试

在提交代码前，必须保证静态检查与单元测试全部通过：

### 4.1 全量静态检查（Go vet + Go build）

```powershell
task check
```

### 4.2 运行平台与业务单元测试

```powershell
# 运行全部 internal 测试
go test -v ./internal/...

# 单独测试 Windows 平台原语（网卡/端口/进程指纹/JobObject/Ping）
go test -v ./internal/platform/windows/

# 单独测试配置持久化
go test -v ./internal/settings/

# 单独测试日志脱敏
go test -v ./internal/logging/
```

---

## 5. 生产构建与打包（便携版）

### 5.1 生成便携式单二进制

执行编译任务：

```powershell
task build
```

**构建流水线会自动执行**：
1. `go mod tidy` 依赖对齐；
2. `wails3 generate bindings` 生成最新的类型绑定；
3. `npm run build` 打包前端资源到 `frontend/dist/`；
4. Go 原生编译，将前端资源嵌入二进制（通过 `//go:embed`）；
5. 最终产物生成在：**`bin/hanxi.exe`**。

### 5.2 验证便携模式

Hanxi 支持零安装、不写注册表的绿色便携模式：

1. 在 `bin/` 目录下创建一个名为 `data` 的空文件夹：
   ```powershell
   mkdir bin\data
   ```
2. 双击运行 `bin/hanxi.exe`；
3. 程序启动后会自动侦测并切换至 **Portable 便携模式**；
4. 所有的配置文件（`config.json`）、运行时 TOML、日志（`logs/`）、frpc 各版本文件（`versions/`）将全部落入 `bin/data/` 目录中，随时拷走即用。

---

## 6. 常用命令速查表

| 动作 | 命令 | 适用场景 |
|---|---|---|
| **启动开发模式** | `task dev` | 日常开发、联调、前端热更新 |
| **静态语法检查** | `task check` | 快速排查类型与编译报错 |
| **运行单元测试** | `go test -v ./internal/...` | 验证底层平台原语与核心逻辑 |
| **打包 Windows 便携版** | `task build` | 产出 `bin/hanxi.exe` 绿色单文件 |
| **单前端编译** | `cd frontend && npm run build` | 验证前端 Vue 3 / TS 编译状态 |
| **手动生成绑定** | `task generate:bindings` | 仅重新生成 Go 到 TS 的 Bindings |

---

## 7. 常见问题排查 (FAQ)

### Q1: 运行 `task dev` 提示找不到 `wails3` 或 `task`
- **原因**：Go 的 bin 目录未加入环境变量。
- **解决**：将 `C:\Users\<你的用户名>\go\bin` 添加到 Windows 系统的 `PATH` 环境变量中，并重启终端。

### Q2: 为什么构建不需要安装 GCC / CGO？
- **说明**：Hanxi 全部采用 Windows API（通过 `golang.org/x/sys/windows` 和 `syscall` 动态链接 Windows 系统自带的 `iphlpapi.dll`、`kernel32.dll`），`CGO_ENABLED=0` 即可纯 Go 原生编译，无需配置 MinGW / C++ 编译环境。

### Q3: 端口查杀为什么不需要以管理员身份启动主程序？
- **说明**：Hanxi 遵循最小特权原则。日常查询端口与查杀普通开发者进程（如 node、go run、vite 等）只需普通权限；当遇到系统服务等受限进程时，主程序会自动触发 Windows 标准 UAC 提权确认，无需常驻管理员身份。
