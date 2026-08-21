# HubKit 开发计划与执行路线图

> **版本**：v1.0 · **基线**：PRD v0.4 / ARCHITECTURE v0.1 · **目标平台**：Windows 10 22H2 / 11 x64

---

## 1. 计划总览与里程碑节奏

```text
[M0 框架与平台层] ──► [M1 基础服务与持久化] ──► [M2 局域网与公网模块]
                             │
                             ▼
[M5 交付与便携打包] ◄── [M4 frpc旗舰模块] ◄── [M3 端口查杀与提权]
```

| 里程碑 | 名称 | 核心目标 | 产出物 | 依赖项 |
|---|---|---|---|---|
| **M0** | 骨架与平台基石 | 验证 Wails3 + Vue3 管道，完成 Windows 底层 API 适配层封装 | `platform/windows` 原语、骨架跑通 | 无 |
| **M1** | 基础服务与持久化 | 便携路径解析、配置与状态落盘、统一日志、优雅退出编排 | `settings/`、`logging/`、`host/` 框架 | M0 |
| **M2** | 局域网扫描与公网 IP | 完成两个轻量级网络工具模块开发与前端交互 | `modules/lan`、`modules/publicip` | M1 |
| **M3** | 端口查杀与安全特权 | 端口占有表、进程指纹复核、UAC 提权 Helper 模式 | `modules/portkill`、`cmd/hubkit/killhelper_main.go` | M1 |
| **M4** | frpc 旗舰多实例系统 | 版本下载管理、TOML 生成、JobObject 实例生命周期、日志流 | `modules/frpc` 完整业务闭环 | M1, M3 |
| **M5** | 便携发布与全量验证 | 最终单二进制编译、集成自测、便携模式回归验证 | `bin/hubkit.exe`、便携包分发 | M2, M3, M4 |

---

## 2. 里程碑详细分解与任务清单

### M0: 骨架与 Windows 平台抽象层 (已完成 80% / 补全平台底层)

#### 目标
完成统一模块注册框架（已完成），建立零依赖平台接口 `internal/platform` 与 Windows 专用实现 `internal/platform/windows`。

#### 任务清单
- [x] **M0.1 骨架搭建**：Wails 3 + TS Bindings + Vue 3 浅色主题 + 模块契约 `extapi.Module`。
- [x] **M0.2 模块导航动态注入**：实现前端根据 `AppService.GetNavs()` 动态呈现侧边栏。
- [ ] **M0.3 平台抽象定义**（`internal/platform/platform.go`）：
  - 定义 `NetworkAdapter`（网卡信息、ICMP Echo、ARP 邻居表读取）。
  - 定义 `PortInspector`（TCP/UDP 连接表、PID 映射）。
  - 定义 `ProcessController`（进程指纹获取、令牌校验、JobObject 绑定）。
- [ ] **M0.4 Windows 平台实现**（`internal/platform/windows/`）：
  - `net_windows.go`：封装 `iphlpapi.dll`（`GetAdaptersAddresses`, `IcmpSendEcho2`, `GetIpNetTable2`）。
  - `port_windows.go`：封装 `GetExtendedTcpTable`, `GetExtendedUdpTable`。
  - `process_windows.go`：封装 `OpenProcess`, `QueryFullProcessImageNameW`, `GetProcessTimes`。
  - `job_windows.go`：封装 Windows Job Object（`CreateJobObject`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`），用于 frpc 子进程防孤儿兜底。

#### 验收标准
- `go test ./internal/platform/...` 在 Windows 上通过，能正确读取本机网卡、端口列表与进程信息。

---

### M1: 基础服务、持久化与生命周期 (预计耗时: 1-2 天)

#### 目标
实现绿色便携模式路径解析、统一配置与持久化存储、日志系统与优雅退出。

#### 任务清单
- [ ] **M1.1 便携路径解析**（`internal/settings/paths.go`）：
  - 启动时检测可执行文件同级目录是否存在 `data/` 目录；若存在进入 Portable 模式，否则进入 Standard 模式（`%APPDATA%/HubKit`）。
  - 导出统一目录接口：`ConfigDir()`、`VersionsDir()`、`LogsDir()`、`TempDir()`。
- [ ] **M1.2 配置存储引擎**（`internal/settings/store.go`）：
  - 实现基于 JSON 的轻量级原子写（写临时文件 + `os.Rename`）配置存储。
  - 结构定义：通用设置（开机启动、语言、主题）、模块启用状态持久化、frpc 全局配置。
- [ ] **M1.3 统一日志脱敏系统**（`internal/logging/logger.go`）：
  - 基于 Go 官方 `log/slog`，支持滚动落盘到 `data/logs/app.log`。
  - 实现正则过滤处理器：针对 `token = "..."`、`sk = "..."`、`Authorization` 自动脱敏打码。
- [ ] **M1.4 退出编排与清理**（`internal/app/shutdown.go`）：
  - 监听 Wails 退出事件与 Windows 系统关闭信号（`WM_QUERYENDSESSION`）。
  - 编排退出：停止所有运行中的 frpc 实例 → 释放网络监听与临时文件 → 刷新日志落盘。

#### 验收标准
- 切换模块启用状态后重启程序，设置正确保持；
- 可执行文件同级创建 `data/` 目录后，所有配置与日志均落入 `data/`，系统无痕。

---

### M2: 局域网扫描与公网 IP 模块 (预计耗时: 1-2 天)

#### 目标
完成 `modules/lan` 和 `modules/publicip` 两个高频排障工具的完整闭环。

#### 任务清单
- [ ] **M2.1 局域网扫描后端服务**（`internal/modules/lan/`）：
  - `service.go`：实现 `Scan(cidr string) error`，`GetInterfaces() []InterfaceInfo`，`Cancel()`。
  - `scanner.go`：
    - CIDR 范围校验，默认限制 `/24`（最多 254 IP 并发）；
    - 结合 ICMP Echo（探测存活）与 Windows 邻居表（`GetIpNetTable2`）解析 MAC/厂商前缀；
    - 进度通过 Wails Event 实时推送到前端：`events.Emit("lan:progress", current, total)`。
- [ ] **M2.2 局域网扫描前端界面**（`frontend/src/views/LanScannerView.vue`）：
  - 网卡/子网下拉选择器、一键扫描/取消按钮、进度条展示；
  - 活跃设备列表表格（IP、MAC、设备名称、延迟、厂商）；
  - 单击快速复制 IP 功能，提供 Toast 提示反馈。
- [ ] **M2.3 公网 IP 模块实现**（`internal/modules/publicip/`）：
  - `service.go`：并发查询 IPv4 / IPv6，提供多源 Provider 回退机制（`ipify`, `icanhazip`, `ident.me`）。
  - 严格 IP 格式正则校验与超时熔断控制（2 秒超时）。
- [ ] **M2.4 公网 IP 前端界面**（`frontend/src/views/PublicIpView.vue`）：
  - 异步加载卡片，展示当前公网 IPv4、IPv6、归属运营商与地理位置（可选）；
  - 刷新按钮与一键复制按钮。

#### 验收标准
- 局域网扫描在 3 秒内完成 `/24` 网段扫描并高亮本机与网关；
- 公网 IP 模块在断网或单 Provider 挂掉时能正确降级并提示。

---

### M3: 端口查杀与提权安全系统 (预计耗时: 2-3 天)

#### 目标
实现端口快速诊断、安全进程指纹复核、以及通过同二进制 Helper 模式实现的最小 UAC 提权终止。

#### 任务清单
- [ ] **M3.1 端口占用查询**（`internal/modules/portkill/`）：
  - `service.go`：提供 `QueryPort(port int) (*PortOccupant, error)` 与 `ListPorts() []PortOccupant`。
  - 读取 Windows TCP/UDP 占用表，关联 PID、进程名、完整 Exe 路径、启动时间（`StartTime`）。
- [ ] **M3.2 安全复核令牌机制**（`internal/host/kill.go`）：
  - 构造 `VerifyToken{PID, StartTime, ExePath}`；
  - 实施**系统红线拦截**：禁止查杀 PID 0（System Idle）、PID 4（System）、HubKit 自身进程。
  - 终止前原子比对 `VerifyToken`，防止 PID 快速复用误杀正常进程。
- [ ] **M3.3 UAC 提权 Helper 模式**（`cmd/hubkit/killhelper_main.go`）：
  - 主程序以普通权限运行，当 `TerminateProcess` 返回 `Access Denied` 时，触发提权流程；
  - 采用同二进制自调用：`ShellExecuteEx("runas", "hubkit.exe -mode=killhelper -payload=...")`；
  - Payload 包含随机一次性 Nonce、目标 PID、期望指纹，采用标准错误码退出反馈结果。
- [ ] **M3.4 端口查杀前端界面**（`frontend/src/views/PortKillView.vue`）：
  - 端口即时搜索框（1-65535），支持常用端口（80/443/3000/8080/8000/3306/6379）快捷标签；
  - 进程信息详情卡片（PID、路径、启动时间、内存占用）；
  - 查杀二次确认弹窗（展示风险标识：系统/常规进程），查杀结果 Toast 反馈。

#### 验收标准
- 占用端口查询耗时 < 50ms；
- 普通进程一键杀死；管理员权限进程查杀触发标准 Windows UAC 提窗并在确认后成功释放。

---

### M4: frpc 旗舰模块（多实例/版本/TOML/日志流） (预计耗时: 3-5 天)

#### 目标
完成 HubKit 的核心旗舰功能：支持多版本 frp 下载管理、多项目配置、TOML 生成、多实例并行与实时日志。

#### 任务清单
- [ ] **M4.1 frp 版本管理引擎**（`internal/modules/frpc/version/`）：
  - `downloader.go`：获取 GitHub Releases 列表（内置镜像源加速回退）。
  - 下载官方 Windows zip 包，**强制执行 SHA256 哈希硬校验**。
  - 解压提取 `frpc.exe`，按版本隔离存储于 `data/versions/frp_vX.Y.Z/frpc.exe`。
- [ ] **M4.2 纯领域模型与 TOML 生成**（`internal/domain/` 与 `internal/modules/frpc/docgen/`）：
  - 领域结构：`Project`、`ServerConfig`、`ProxyRule`（TCP, UDP, HTTP, HTTPS, STCP 等）。
  - TOML 序列化生成器：支持最新 frp v0.52+ 的标准 TOML 格式生成与语法校验。
- [ ] **M4.3 多实例运行与 JobObject 监管**（`internal/modules/frpc/instance/`）：
  - `Instance` 状态机实现：`Stopped` -> `Starting` -> `Running` -> `Failed`。
  - 每个项目独立启动专属 `frpc.exe` 子进程，绑定到宿主 Job Object；
  - 捕获 stdout/stderr，写入环形内存缓冲区（RingBuffer，保留最新 1000 行）。
- [ ] **M4.4 实时日志流推送**（`internal/modules/frpc/logstream/`）：
  - 基于 Wails 3 Event 将实例日志推送到前端：`events.Emit("frpc:log:{projectId}", line)`。
  - 敏感信息（token/key）实时脱敏处理。
- [ ] **M4.5 frpc 前端完整工作流**：
  - `views/FrpcProjectsView.vue`：项目卡片列表，批量/单独启停，状态小圆点，快捷复制穿透地址；
  - `views/FrpcEditProjectView.vue`：服务端配置、代理规则表格（新增/编辑/删除）、生成 TOML 预览；
  - `views/FrpcVersionsView.vue`：版本下载列表、进度条、哈希校验状态展示；
  - `components/LogDrawer.vue`：底部日志抽屉组件，支持自动滚动、清屏、按关键字过滤与复制。

#### 验收标准
- 支持同时启动 3 个以上不同配置的 frpc 实例，互不干扰；
- 关闭 HubKit 主程序时，所有拉起的 `frpc.exe` 100% 自动退出无残留孤儿进程。

---

### M5: 便携打包、集成验证与发布 (预计耗时: 1-2 天)

#### 目标
完成单二进制便携打包构建、全功能冒烟与极端异常场景测试。

#### 任务清单
- [ ] **M5.1 Windows 发布构建流水线**（`Taskfile.yml` & `build/windows/`）：
  - 配置 `rsrc` 生成 Windows PE 资源（应用图标 `icon.ico`、版本号、公司名信息）。
  - 产出发布二进制：`-ldflags "-s -w -H=windowsgui"` 去除调试符号与隐藏控制台黑框。
- [ ] **M5.2 极端场景健壮性自测**：
  - **网络异常**：拔掉网线时 frpc 自动重连与状态显示测试；
  - **崩溃残留**：任务管理器强制结束 `hubkit.exe`，检验 JobObject 是否带走所有 `frpc.exe`；
  - **便携移动**：将 `hubkit` 文件夹移动到全新 Windows 10/11 电脑（未装 Go/Node/C++ 运行库）运行验证；
  - **UAC 取消**：查杀管理员进程时，用户在 UAC 弹窗点击“否”，验证主程序错误处理是否优雅。
- [ ] **M5.3 文档与交付件归档**：
  - 更新发布 Release 说明与便携版使用指南。

---

## 3. 代码库实施路径与分阶段文件增量矩阵

| 里程碑 | 新增/修改的主要代码路径 | 关键职责 |
|---|---|---|
| **M0** | `internal/platform/platform.go`<br>`internal/platform/windows/*.go` | 平台原语接口定义、Windows API (`iphlpapi.dll`, JobObject) 封装 |
| **M1** | `internal/settings/*.go`<br>`internal/logging/*.go`<br>`internal/app/shutdown.go` | 便携路径解析、JSON 原子写配置、脱敏日志、安全退出编排 |
| **M2** | `internal/modules/lan/*.go`<br>`internal/modules/publicip/*.go`<br>`frontend/src/views/LanScannerView.vue`<br>`frontend/src/views/PublicIpView.vue` | 局域网 ICMP/ARP 扫描器、公网 IP 查询、前端视图与复制交互 |
| **M3** | `internal/modules/portkill/*.go`<br>`internal/host/*.go`<br>`cmd/hubkit/killhelper_main.go`<br>`frontend/src/views/PortKillView.vue` | 端口与进程关联、安全校验令牌、UAC 最小提权 Helper 模式 |
| **M4** | `internal/modules/frpc/**/*.go`<br>`internal/domain/*.go`<br>`frontend/src/views/Frpc*.vue`<br>`frontend/src/components/LogDrawer.vue` | frp 下载校验引擎、TOML 生成、多实例进程池、实时日志推流 |
| **M5** | `Taskfile.yml`<br>`build/windows/` | Windows PE 资源嵌入、无控制台窗口优化发布编译 |

---

## 4. 关键风险、红线与应对预案

| 风险点 | 严重级 | 潜在影响 | 预防与解决策略 |
|---|---|---|---|
| **PID 快速复用导致误杀** | **高** | 端口查杀误关无关重要程序 | 实施 `VerifyToken{PID, StartTime, ExePath}` 三要素比对，不匹配拒绝终止。 |
| **主进程退出后 frpc 孤儿残留** | **高** | 端口持续被占、后台跑流量 | 强绑定 Windows Job Object (`LIMIT_KILL_ON_JOB_CLOSE`)，内核级连带销毁。 |
| **GitHub 下载 frpc 失败/限流** | **中** | 用户无法直接使用联调功能 | 内置 GitHub FastGit/CDN 备用下载镜像，支持用户手动拖入本地 `frpc.exe`。 |
| **非管理员权限无法杀管理进程** | **中** | 提示权限不足无法释放端口 | 实现同二进制 `-mode=killhelper` 最小 UAC 提权，仅在必要时弹窗授权。 |
| **日志泄露敏感 Token/Key** | **高** | 团队协作或排障截图造成安全事故 | 建立统一 Logging/Logstream 正则脱敏管道，敏感字段打码为 `******`。 |

---

## 5. 下一步执行指引

开发计划文档已确立。后续可按里程碑顺序推进：
1. **优先进入 M0 补全 / M1 基础服务**：完成 `internal/platform/windows` 底层原语与 `internal/settings` 便携存储；
2. **随后进入 M2 / M3 功能模块落地**：完成局域网扫描、公网 IP 与端口查杀功能；
3. **重点突破 M4 frpc 旗舰多实例系统**。
