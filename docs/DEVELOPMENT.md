# Hanxi 开发与构建指南

> **更新日期**：2026-10-21（与 `Taskfile.yml`/`build/Taskfile.yml`/`.github/workflows`/`internal/settings/paths.go` 逐项对账；版本钉子与任务编排为当次核读，标注「未复核」的条目未经真机复跑）
> **取数纪律**：模块数/托管数等会漂移的数字一律回 `docs/FACTS.md`，本文不载计数。

本文档介绍 Hanxi 的本地环境准备、开发调试模式（HMR）、静态检查、单测执行以及生产编译的完整流程。

---

## 1. 前置环境要求

Hanxi 采用 **Go + Wails 3 + Vue 3 (TypeScript/Vite)** 架构，仅面向 Windows x64 平台。

| 工具 / 依赖 | 最低版本 | 说明 |
|---|---|---|
| **操作系统** | Windows 10 (22H2+) / Windows 11 x64 | 纯 Windows 原生调用 |
| **Go** | ≥ 1.26 | 核心后端与平台 API 调用 |
| **Node.js** | ≥ 20.19 或 ≥ 22.12 | 前端 Vite 构建与 TypeScript 编译 |
| **Wails 3 CLI** | `v3.0.0-beta.10` | 负责 bindings 生成与 dev 热重载编排 |
| **Taskfile CLI** | `v3.53.1` | 统一任务编排执行器 |

### 安装全局 CLI 工具

在终端运行以下命令安装 `wails3` 和 `task` CLI：

```powershell
# 1. 安装 Wails v3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.10

# 2. 安装 go-task
go install github.com/go-task/task/v3/cmd/task@v3.53.1
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

> **锁文件纪律**：CI 只认 `frontend/package-lock.json`（npm 口径，见 `.github/workflows/ci.yml`）；用 pnpm 干活产生的 `frontend/pnpm-lock.yaml` **不得入库**——留两份锁等于两套依赖真相。

> **本机已知坑（机主环境，2026-10-12 记录在案；本轮未真机复跑，标注未复核）**：机主机器上 `npm run` 的脚本 shim 损坏，直接 `npm run <script>` 会失败，而依赖安装（`npm install`）不受影响。绕行两条路：
> 1. **经 task 入口**一律加变量 `PACKAGE_MANAGER=pnpm`（Taskfile 的 `frontend:run:pnpm` 通道实测可用，如 `task build PACKAGE_MANAGER=pnpm`）；`task check`/`task dev`/`task package` 走同一 `common:frontend:run` 分发，预期同样适用（未逐一复跑）。
> 2. **绕开 task 直调**时用 node 直接执行包入口而非 `.bin` shim，例：前端单测 `cd frontend && node node_modules/vitest/vitest.mjs run`（`docs/FACTS.md` §一即此命令），前端构建 `node node_modules/vite/bin/vite.js build`。

---

## 3. 开发模式运行（热重载 Dev）

开发模式会同时启动 Vite Dev Server 与 Wails 3 原生窗口，支持前端 HMR（热更新）与 Go 后端实时调用：

```powershell
task dev
```

> 本机 `npm run` 坏 shim 时请 `task dev PACKAGE_MANAGER=pnpm`——dev 编排同样经 `common:frontend:run` 拉起 Vite（`build/config.yml` 的 `dev_mode.executes`）；该绕行形态在 build 通道已实测，dev 通道未逐一复跑，标注未复核。

运行该命令后：
1. 会在后台启动 Vite（默认端口 `9245`）；
2. 自动根据 Go 代码结构生成 TypeScript 绑定文件到 `frontend/bindings/`；
3. 弹出 Hanxi 开发窗口（尺寸 1200×780，主题跟随已保存的设置）；
4. 修改前端 `frontend/src/` 代码后界面即时热更新；
5. 在界面上可按 `F12` 或右键打开 WebView2 开发者工具进行调试。

---

## 4. 静态检查与单元测试

在提交代码前，必须保证静态检查与单元测试全部通过：

### 4.1 全量质量门禁（gofmt + go vet + Go 单测 + 前端 typecheck/build/Vitest + 漂移检查）

```powershell
task check
```

> `task check` 依次执行（2026-10-21 与 `Taskfile.yml` 对账）：`gofmt` 格式检查、`go vet ./...`、`go test -count=1 ./...`（已含 `scripts/docsfacts` 文档事实对账闸门）、前端 `typecheck` 与 `build`、前端 Vitest 单测、`verify:tidy`（go.mod/go.sum 漂移）、`verify:bindings`（bindings 漂移）与 `verify:docs`（活文档计数/链接/表格结构），与 CI 门禁等价。本机 `npm run` 坏 shim 时，`task check` 各前端步骤需带 `PACKAGE_MANAGER=pnpm`（见 §2 本机已知坑）。

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

## 5. 生产构建与打包

### 5.1 生成绿色单二进制

执行编译任务：

```powershell
task build
```

**构建流水线会自动执行**：
1. 安装前端依赖（npm 锁 `package-lock.json`，`run:once` 去重；`PACKAGE_MANAGER` 变量可切换 npm/yarn/pnpm/bun 通道，机主 `npm run` 坏 shim 时用 `task build PACKAGE_MANAGER=pnpm`，见 §2）；
2. 前端构建脚本（`vue-tsc` 类型检查 + Vite 打包）产出前端资源到 `frontend/dist/`；
3. `wails3 generate syso` 生成 Windows 图标/清单/版本资源（`icon.ico` 已固化于 `build/windows/`，不重新生成）；
4. `go build -tags production -trimpath -ldflags="-w -s -H windowsgui"` 原生编译，前端资源经 `//go:embed`（`embedassets.go`）嵌入二进制；
5. 最终产物生成在：**`bin/hanxi.exe`**（单文件绿色，双击即活）。

> 注意：`task build` 不修改源码树。`go mod tidy` 与 `wails3 generate bindings` 属独立维护动作，用 `task generate` 执行；二者的一致性由 `task verify:tidy` / `task verify:bindings` 漂移检查守护。`bin/hanxi.exe` 被运行中实例占用时 Windows 覆盖不了正在执行的 exe——先彻底退出旧实例（含托盘），或临时用 `APP_NAME=hanxi-new task build` 产出别名包。

### 5.2 验证数据根（F6 定稿口径，2026-10-21 按 `internal/settings/paths.go` 重写）

v0.3.0 起**不再区分「便携/标准」双模式**，数据根解析链只有两级：

1. **exe 同级 `hanxi.bind` 绑定指针**——首行非空内容即数据目录绝对路径，"绑定哪个用哪个"；
2. **exe 同级 `hanxidata/`**——存在即用，**无则自动创建**（裸 exe 双击即活，无需预建目录）。

验证方式：直接双击 `bin/hanxi.exe`，随后检查 `bin/hanxidata/` 是否出现 `config.json`（数据根**特征签名**，不是模式开关）、`state/`、`logs/`、`versions/`、`runtime/`、`installers/`、`modules/` 等分区——数据永远落在 exe 可达之处，全程**无 `%APPDATA%` 静默兜底**；同级不可写且绑定目标也不可用时 fail loud 并附处置指引（移动应用文件夹或手工补建 `hanxi.bind`），绝不把数据悄悄挪进用户目录。

> 历史兼容：旧版本把各模块状态 JSON 平铺在数据根，新版启动时自动收拢进 `hanxidata/state/`（`config.json` 留根不动）。若曾回滚旧版运行，旧版会再把文件写回根目录，此时升级后根/`state/` 双份并存并告警留痕——升级/回滚前先彻底退出另一版本（含托盘）即可避免。

> 历史兼容（口径已反转，2026-10-21 订正）：v0.3.0 及以前便携包使用的泛化名 `data/` 兼容**已正式废弃**——现行代码零识别、零迁移，仅在检测到旧数据特征时经 `warnRetiredLegacyDataDir` 告警指路。旧文中「`data/` 含 `config.json` 或 `versions/` 即认定为便携根」「空 `data/` 不再触发便携模式」等表述均属于已退役的双模式世界观，勿再照旧文理解（权威口径见 `docs/ARCHITECTURE.md` §3）。

---

## 6. 常用命令速查表

| 动作 | 命令 | 适用场景 |
|---|---|---|
| **启动开发模式** | `task dev` | 日常开发、联调、前端热更新 |
| **全量质量门禁** | `task check` | 提交前跑齐 Go/前端全部检查与漂移校验 |
| **运行单元测试** | `go test -v ./internal/...` | 验证底层平台原语与核心逻辑 |
| **打包 Windows 绿色版** | `task build`（机主加 `PACKAGE_MANAGER=pnpm`） | 产出 `bin/hanxi.exe` 绿色单文件 |
| **单前端编译** | `cd frontend && npm run build`（CI 口径；本机坏 shim 时 `node node_modules/vite/bin/vite.js build`） | 验证前端 Vue 3 / TS 编译状态 |
| **前端单测** | `cd frontend && node node_modules/vitest/vitest.mjs run` | 绕开 npm shim 的直调形态（FACTS 同口径） |
| **文档事实对账** | `task verify:docs` | 活文档计数/断链/畸形表格闸门（已并入 `go test ./...` 与 `task check`） |
| **手动生成绑定** | `task common:generate:bindings` | 仅重新生成 Go 到 TS 的 Bindings |
| **依赖对齐 + 绑定再生** | `task generate` | `go mod tidy` 与 bindings 一并刷新 |
| **安装包打包（NSIS/MSIX）** | `task package`（可加 `FORMAT=msix`、`INSTALL_SCOPE=user`） | 产出安装器（需 NSIS `makensis` 在 PATH） |

---

## 7. 常见问题排查 (FAQ)

### Q1: 运行 `task dev` 提示找不到 `wails3` 或 `task`
- **原因**：Go 的 bin 目录未加入环境变量。
- **解决**：将 `C:\Users\<你的用户名>\go\bin` 添加到 Windows 系统的 `PATH` 环境变量中，并重启终端。

### Q2: 为什么构建不需要安装 GCC / CGO？
- **说明**：Hanxi 全部采用 Windows API（通过 `golang.org/x/sys/windows` 和 `syscall` 动态链接 Windows 系统自带的 `iphlpapi.dll`、`kernel32.dll`），`CGO_ENABLED=0` 即可纯 Go 原生编译，无需配置 MinGW / C++ 编译环境。

### Q3: 端口查杀为什么不需要以管理员身份启动主程序？
- **说明**：Hanxi 遵循最小特权原则。日常查询端口与查杀普通开发者进程（如 node、go run、vite 等）只需普通权限；当遇到系统服务等受限进程时，主程序会自动触发 Windows 标准 UAC 提权确认，无需常驻管理员身份。

### Q4: `npm run build`/`npm run test` 报找不到可执行文件（shim 层失败）
- **原因**：机主环境已知坑（2026-10-12 记录；本轮未复跑确认现状）——`npm run` 生成的 `.bin` shim 损坏，`npm install` 本身不受影响。
- **解决**：task 入口带 `PACKAGE_MANAGER=pnpm`；或绕开 task 用 node 直调包入口（`node node_modules/vitest/vitest.mjs run`、`node node_modules/vite/bin/vite.js build`）。详见 §2 本机已知坑。

### Q5: Git Bash 里 `wails3`/`go test` 报 "go not found" 或 cmd.exe 假不可用
- **说明**：POSIX→Windows 子进程的 PATH 交接/截断类环境坑（非工具损坏），已有多条实录：`docs/TROUBLESHOOTING.md` #40、#59、#79、#82。判据与绕行（行内 Windows 形态 PATH、走 cmd/PowerShell 会话、排除 `*/instance` 族另交 CI）以该四处条目为准。
