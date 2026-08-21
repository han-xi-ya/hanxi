# HubKit 项目架构设计

> 版本：v0.1 · 2026-08-20 · 配套 [PRD.md](PRD.md) v0.3
> 技术基线：Go ≥1.26 + Wails 3 + Vue 3/TS + Windows 优先（便携版、多实例 frpc、内建扩展模块）

---

## 0. 设计目标与原则

1. **工具箱模块化**：frpc 联调与局域网扫描/释放端口/公网 IP 都是统一模块（同一注册表、同一启停语义、同一导航注入）；frpc 是旗舰模块而不是特殊代码。内置模块可启停；外部模块（独立安装/拆卸）为 P1+ 形态，接口已预留。
2. **`_windows.go` 是真实平台**：macOS/Linux 只保留接口与编译占位，首期不验收。
3. **领域干净**：`domain` 不依赖任何平台包；`platform` 不依赖任何业务包；`app` 是唯一装配所有模块的地方（Composition Root）。
4. **特权收敛**：扩展（乃至前端）都不能绕过宿主直接操作系统资源；安全红线只在宿主 `Host` 实现。
5. **一切长任务携带 `context.Context`**，全链路可取消。
6. **Wails 是薄壳**：核心业务不依赖 Wails 类型，未来可替换 CLI/TUI；Wails 只出现在 `app` 层与绑定层。

---

## 1. 总体分层与依赖规则

```text
┌───────────────────────────────────────────────────┐
│  frontend/  Vue3 + TS（仅展示与交互）               │
│  路由注入 · stores · bindings(生成) · 组件            │
└──────────────────────┬────────────────────────────┘
                       │ wails3 类型化绑定（生成代码）
┌──────────────────────▼────────────────────────────┐
│  internal/app  Composition Root（唯一装配点）        │
│  - 生命周期 / 服务容器 / 扩展注册表 / SafetyHost     │
│  - wails3 Services 注册（核心服务 + 启用中的扩展）    │
└──────┬───────────────┬────────────────┬────────────┘
       │               │                │
┌──────▼──────┐ ┌──────▼──────┐ ┌──────▼────────────┐
│ 核心 Services│ │内部/extapi  │ │  内部/Host         │
│ frpc 版本     │ │Extension    │ │ 特权操作统一入口    │
│ settings     │ │接口与注册表  │ │ 复核·保护·提权      │
│ log      ... │ │             │ │                  │
└──────┬──────┘ └──────┬──────┘ └──────┬────────────┘
       │               │               │
┌──────▼───────────────▼───────────────▼────────────┐
│  internal/domain（纯领域，无平台、无 Wails 依赖）     │
│  FrpcProject/Proxy/Version/Instance/Settings/Err   │
└──────────────────────┬────────────────────────────┘
                       │ 接口（在 platform 包声明）
┌──────────────────────▼────────────────────────────┐
│  internal/platform（接口 + build-tag 实现）          │
│  windows/（首个实现） darwin/ linux/（占位）         │
│  网卡/ICMP/邻居表 · 端口表 · 进程 · JobObject ·      │
│  DPAPI(预留) · UAC helper · 路径                    │
└───────────────────────────────────────────────────┘
```

### 依赖规则（违反即架构错误）

- `domain` → 只依赖标准库。
- `platform` → 只依赖标准库 + `golang.org/x/sys/*`；不得引用任何业务包。
- 业务包/模块 → 只依赖 `domain` + `host`（+`extapi`）；**`platform` 仅对 `host` 与 `app`（装配）可见**，
  业务层不得直接触碰；特权写操作（杀进程/提权/写系统资源）唯一入口是 `host`，
  低风险只读枚举（网卡/端口表/ICMP）也经 `host` 的只读 API 暴露，规则见 §12。
- `app` → 依赖一切并装配；禁止反向依赖。
- 模块（`modules/*`）→ 只依赖 `extapi` + `host` + `domain`；模块之间互不依赖。
- 前端 → 只能调用 wails 绑定方法，不得直接碰文件/进程。

---

## 2. 目录结构（最终）

```text
hubkit/
├─ cmd/hubkit/
│  ├─ main.go              # wails3 app 入口（仅装配 + 启动）
│  └─ killhelper_main.go   # 同一二进制的 UAC helper 模式（-mode=killhelper；M3）
├─ embedassets.go          # 根目录包：//go:embed all:frontend/dist
│                          # （Go embed 禁止 ".."，只能用根级包承载）
├─ internal/
│  ├─ app/                 # Composition Root：生命周期、服务容器、扩展注册、wails 绑定
│  │  ├─ services.go       #   wails3 Services 装配
│  │  ├─ registry.go       #   扩展注册表（加载/开关/路由与前端注入）
│  │  └─ shutdown.go       #   优雅退出编排
│  ├─ host/                # 特权操作统一入口（扩展只能经由这里）
│  │  ├─ host.go           #   Host 接口：KillVerified / Escalate / ScanPrimitives / HashVerify
│  │  └─ kill.go           #   终止前复核流程（PID+启动时间+路径）
│  ├─ extapi/              # 扩展契约（面向未来子进程插件的语法）
│  │  ├─ extension.go      #   Extension 接口 + ExtensionInfo/NavEntry/Permission
│  │  └─ registry.go       #   注册表（Generic 实现，app 使用）
│  ├─ domain/              # 纯领域模型与校验（见 §3）
│  │  ├─ project.go  proxy.go  version.go  instance.go
│  │  ├─ settings.go  extension.go  apperr.go  validate.go
│  ├─ modules/
│  │  ├─ frpc/             # 模块（旗舰）：联调核心（见 §5/§6）
│  │  │  ├─ manager.go     #   FrpcManager：map[projectID]*Instance，多实例
│  │  │  ├─ instance.go    #   Instance：状态机 + exec + JobObject + 退出
│  │  │  ├─ docgen.go      #   domain.Model → frpc TOML（纯函数，golden 测试）
│  │  │  ├─ logstream.go   #   stdout/stderr 行推流（events + 文件轮转）
│  │  │  └─ versions/      #   frp 版本管理（frpc 模块内部件）
│  │  │     ├─ provider.go #   GitHub Releases 客户端（内置清单 + 在线刷新）
│  │  │     ├─ matcher.go  #   平台/架构 → 资产匹配
│  │  │     ├─ downloader.go # 进度、限流、超时
│  │  │     ├─ verifier.go #   SHA256 强制校验
│  │  │     └─ installer.go#   解压 tar.gz/zip → data/versions/<ver>/
│  ├─ modules/
│  │  ├─ frpc/             # 模块（旗舰）：联调核心（见 §5/§6）
│  │  ├─ lan/              # 模块：局域网扫描（见 §8.1）
│  │  ├─ portkill/         # 模块：释放端口（见 §8.2）
│  │  └─ publicip/         # 模块：公网 IP（见 §8.3）
│  ├─ settings/            # 配置加载/保存/迁移；路径解析（便携/非便携）
│  ├─ security/            # SecretStore 接口 + 明文实现（MVP）+ Redactor 脱敏
│  ├─ logging/             # slog + 轮转 + 脱敏 writer
│  ├─ netcore/             # 通用网络工具：CIDR、私有判断、worker pool、限流
│  └─ platform/
│     ├─ platform.go       #   接口定义（见 §4）
│     ├─ platform_windows.go
│     ├─ platform_darwin.go  # 占位：接口存在，实现 stubs
│     ├─ platform_linux.go    # 占位
│     └─ windows/          #  Windows 具体实现（被上面 build-tag 文件引用）
│        ├─ adapters.go  icmp.go  neighbors.go  ports.go
│        ├─ process.go   jobobject.go  dpapi.go(预留)  uac.go
├─ frontend/
│  ├─ src/
│  │  ├─ main.ts  App.vue
│  │  ├─ api/            # bindings.ts（wails3 生成）+ client.ts
│  │  ├─ router/         # 核心路由 + 扩展路由注入
│  │  ├─ stores/         # frpc / settings / extensions / portkill
│  │  ├─ components/     # LogViewer / ConfirmDialog / CopyButton / Progress
│  │  ├─ views/          # 核心页：Home、FrpcProjects、Versions、Settings、About
│  │  └─ ext/            # 无——扩展视图随开关注入，仅内部(见 §10 说明)
├─ build/                # 图标、manifest(asInvoker)、WebView2 检测资源
├─ tests/                # 集成与平台测试
├─ docs/                 # PRD.md / ARCHITECTURE.md（产品与技术文档）
├─ Taskfile.yml          # wails3 标准任务（dev/build:win/portable/test/generate）
└─ go.mod
```

---

## 3. 领域模型（internal/domain）

### 3.1 关键类型（与 PRD §7 对齐）

```go
type Proxy struct {
    Name          string
    Type          string   // tcp|udp|http|https|stcp|xtcp
    LocalIP       string
    LocalPort     string   // "8080" | "8000-8005" | "80,81"
    RemotePort    string
    CustomDomains []string
    Subdomain     string
    Locations     []string
    Transport     Transport
    BasicAuth     bool; HTTPUser, HTTPPassword string
    SecretKey     string   // stcp/xtcp
    ServerName    string; BindAddr string; BindPort uint16 // visitors
    Status        bool
}

type FrpcProject struct {
    ID        string
    Name      string
    Server    FrpcServer       // Addr/Port/User/AuthMethod/Token/TLS
    Proxies   []Proxy
    LogLevel  string
    Notes     string
    CreatedAt, UpdatedAt time.Time
}

type InstalledVersion struct {
    ID, Name   string
    GOOS, GOARCH string
    LocalDir   string
    SHA256     string
    InstalledAt time.Time
}

type Settings struct {
    Language, Theme string
    ScanConcurrency int; ScanTimeoutMs int
    PublicIPProviders []string
    DownloadDir      string
    LogRetainDays    int
    Extensions       map[string]bool  // id → enabled
    // 不含任何密文；token 等走 security.SecretStore
}

type AppError struct { Code, Message, Detail, Action string; Retryable bool; Cause error }
```

### 3.2 校验规则（domain/validate.go，纯函数）

- Proxy：名称唯一性（项目内）、端口合法、tcp/udp 必须有 localPort/remotePort，http/https 必须有 subdomain 或 customDomains…
- FrpcProject：serverAddr 非空、port 1–65535、代理至少 1 条（允许 0 在新建态）。
- 校验失败返回 `domain.ErrValidation`，由 UI 层映射到字段。

---

## 4. 平台抽象（internal/platform）

### 4.1 接口集合（platform.go）

```go
// 网卡与路由
type NetworkAPI interface {
    Adapters() ([]Adapter, error)        // 含名称/描述/MAC/IPv4(6)/mask/Gateway/IsPhysical/IsLoopback/IsUp
    DefaultAdapter() (*Adapter, error)
    NeighborTable() ([]Neighbor, error)  // IP → MAC 邻居表
    Ping(ctx, ip string, timeout time.Duration) (rtt time.Duration, ok bool, err error) // IcmpSendEcho2
}

// 端口 → PID
type PortAPI interface {
    TCPTable(family Family) ([]TCPRow, error)   // LocalIP/LocalPort/State/Remote/PID
    UDPTable(family Family) ([]UDPRow, error)
}

// 进程
type ProcessAPI interface {
    Query(pid uint32) (ProcInfo, error)   // Name/Path/StartedAt/Owner
    KillVerified(ctx, token VerifyToken, force bool) error   // 复核后 TerminateProcess
    KillTreeVerified(ctx, token VerifyToken, force bool) error // Job/process tree
    IsProtected(pid uint32, info ProcInfo) bool
}

// 密钥存储
type SecretStore interface {
    Save(ctx, scope string, payload []byte) error
    Load(ctx, scope string) ([]byte, error)
    Rotate(ctx, scope string) error
}

// Job Object（Windows）
type JobAPI interface {
    Create() (Job, error)
    Assign(job Job, pid uint32) error
    Terminate(job Job) error
}
```

### 4.2 实现分布（build-tag）

- `platform_windows.go` 组装 Windows 实现（`windows.*`），并提供 `New(…) (Platform, error)`。
- `platform_darwin.go` / `platform_linux.go`：返回 `ErrNotSupported` 或最小实现（如 ping 用 `os/exec` 调系统命令）——首期不验收。
- 字节序：TCP/UDP 行中端口为 little-endian，`windows` 包内转换，接口层即 uint16。

---

## 5. frpc 核心设计（modules/frpc）

### 5.1 配置生成 pipeline（纯函数，可单测）

```text
domain.FrpcProject ──validate──▶ docgen.Render(project, runtime) ──▶ toml 字节
          ▲                                                    │
          │                                                    ▼
    golden 测试                                            ──▶ 写入实例临时目录（受限 ACL，MVP：data/runtime/<id>/.toml）
```

要点：

- `Render` 不写文件、不碰网络；版本差异（frp 0.5x 老配置 vs 新 TOML）收敛为 `docgen.VersionVariant` 参数，默认最新。
- 敏感字段（token）渲染后由 `logging.Redactor` 保证不进入日志/崩溃输出。

### 5.2 多实例管理器与状态机

```go
type State int // Stopped Starting Running Stopping Failed Exited

type Instance struct {
    ProjectID, ProfileName string
    PID        uint32
    State      State
    StartedAt, StoppedAt time.Time
    ExitCode   *int
    Cancel     context.CancelFunc
    cmd        *exec.Cmd
    job        platform.Job      // Windows: JobObject；其他平台 nil
    logs       *ringBuffer
    mutex      sync.Mutex
}

type FrpcManager struct {
    mutex sync.Mutex
    instances map[string]*Instance  // key: projectID
    // 状态/日志/退出 事件经 app 转发到前端
}
```

- 启动序：校验项目 → 版本存在 + SHA256 不变 → `docgen.Render` → `exec.Command`（参数数组，不经 shell）→ `job.Assign` → 管道接 `logstream` → 状态 Running。
- 停止序：状态 Stopping → 复核 PID → 温和退出（`GenerateConsoleCtrlEvent`/SIGTERM，显 3s）→ 未退则 `KillVerified`/Job 终止 → 清理临时 TOML → Stopped/Exited。
- 禁止：同一 projectID 重复启动（返回 `FRPC_ALREADY_RUNNING`）；不同项目共享 PID/Job/日志。
- 前台事件：`frpc:{id}:state`、`frpc:{id}:log`、`frpc:{id}:exit`。

### 5.3 日志流

- stdout/stderr 读入 line scanner → 每行经 `Redactor.Redact(line)` → 推给（a）内存环形缓冲（UI 快照）（b）`logging` 文件轮转（默认 7 天）（c）wails event。
- UI 上「清空视图」= 清环形缓冲，不影响磁盘文件。

---

## 6. 版本管理设计（frpc 模块内部件 modules/frpc/versions）

```text
启动 ─▶ 内置清单(frp-releases.json + sha256 表) ── 可“刷新” ──▶ GitHub API（超时/重试）
        │                                                    │
        ▼                                                    ▼
  本地登记 versions.json                    新版本清单（内存合并）
        │
        ◀──下载(进度事件 dl:{job}:progress)──▶ verifier.SHA256 ──▶ installer(解压)
                                                                    │
                                                data/versions/<version>/frpc(.exe)
```

- `Provider` 接口：`List() / Latest() / Asset(ver, goos, goarch)`；内置 JSON 为默认实现，GitHub 为在线实现。
- 下载器：`http.Client`（连接/首字节/总超时）、限速（可选）、断点续传（P1）。
- **verifier 是硬门**：清单中无 SHA256 的版本不得启动；校验失败 → `VERSION_INTEGRITY_FAILED`，删除残留。
- 目录：便携模式 `data/versions/<ver>/`；非便携 `os.UserCacheDir()/HubKit/versions/<ver>/`。

---

## 7. 模块框架（internal/extapi + host）

> 工具箱统一模块模型：注册表平等对待 frpc（旗舰）与各工具模块；
> 内置模块（LevelBuiltin）只可启停，外部模块（LevelExternal，P1+）可独立安装/拆卸。

### 7.1 Extension 接口

```go
type Extension interface {
    Info() ExtensionInfo        // ID/Name/Version/Description/Author
    Nav() []NavEntry            // Route+Icon+Title（扩展区）
    Services() []any            // wails3 Services（绑定到前端）
    Permissions() []Permission  // 声明能力（扫描/杀进程/公网）
    Version() int               // 契约版本（未来子进程插件握手用）
}
```

### 7.2 注册与生命周期（app/registry.go）

1. `registry.Register(lan.Ext, portkill.Ext, publicip.Ext)` —— 编译期内建。
2. 启动：读取 settings.Extensions 开关 → 启用的扩展：收集 `Nav()`（注入前端路由）、`Services()`（注册 wails）、`Permissions()`（核对是否超出宿主白名单）。
3. 关闭：设置页切换 → 事件 `ext:changed` → 前端卸载路由/页面；Go 侧解绑服务（wails 3 支持动态注册为准；否则重启提示）。

**启停语义（定案）**：停用立即生效——导航隐藏；已注册服务的方法入口检查 enabled，禁用态返回 `MODULE_DISABLED` 拒绝执行；
重启后不再注册该模块服务。启用状态持久化在 M1（settings）接入。
4. 前端拿到扩展导航的方式：绑定方法 `GetEnabledNavs()` 返回 `[]NavEntry`；路由动态 `addRoute`。

### 7.3 宿主 API（internal/host）——特权唯一入口

```go
type Host struct {
    p   platform.Platform
    log *logging.Logger
}

// 扩展只能用这些方法；红线全部在这里强制执行
func (h *Host) KillPortOwner(ctx, port uint16, family protocol) error
func (h *Host) KillVerified(ctx, tok VerifyToken, force bool) error
func (h *Host) VerifySha256(ctx, path, want string) error
func (h *Host) Scan(ctx, range string, opts ScanOpts) (<-chan HostResult, <-chan progress, cancel func())
```

扩展（portkill）流程：`platform.PortAPI` 查 PID → 取 `VerifyToken{PID,StartedAt,Path}` → `host.KillVerified`：
- 宿主重新 `Query(pid)` 比较三项；不一致 → `PORT_PROCESS_CHANGED`，拒绝；
- 匹配系统保护规则（PID 0/4、自身、受保护）→ 拒绝；
- Access Denied → 生成 `killhelper` 请求（nonce + 目标三元组 + 有效期）→ `ShellExecuteEx(runas)` 同二进制的 helper 模式 → helper 重查复核后 `TerminateProcess` → 回写结构化结果。

---

## 8. 扩展模块内设计

### 8.1 局域网扫描（extensions/lan）

```go
// scanner.Scanner：纯并发调度（netcore.WorkerPool），platform 只提供原语
type Opts struct { CIDR string; Concurrency int; Timeout time.Duration; Cancel context.Context }
```
- 私有网段 + 地址上限 4096 检查在 `netcore`（单测覆盖）；
- 探测组合：邻居表预加载 → ICMP（默认 64 并发/800ms）→ ARP/邻居补齐；
- 结果以批事件推送（每 50 个或 200ms 合并），UI 表格增量更新；
- 复制 IP 纯前端（绑定产物），不涉及特权。

### 8.2 释放端口（extensions/portkill）

- `List(port?)` 调 `platform.PortAPI` 全表（缓存 ≤1s 或手动刷新），前端本地过滤；MVP 也用全表过滤，避免高频 IPC。
- UI：列表 + 端口直达搜索 + 详情（进程名/路径/启动时间/Owner）。
- 操作：`host.KillVerified`（含二次确认对话框，前端做；复核走宿主）。

### 8.3 公网 IP（extensions/publicip）

- `Provider` 接口 + 内置 ≥2 个 HTTPS 源 + 超时（3s/8s）+ `net/netip` 严格解析 + 响应体上限。
- 结果事件 `pubip:updated`；IPv6 独立降级。

---

## 9. 存储与配置（internal/settings）

### 9.1 路径解析

```go
func Resolve() (Paths, error) {
    // 便携：exe 同目录存在 .portable 或 data/ → 全部置于 exe/data/
    // 非便携：config=os.UserConfigDir()/HubKit, cache=os.UserCacheDir()/HubKit, log=…/log
}
```

### 9.2 文件布局（便携模式示例）

```text
HubKit.exe
data/
├─ config.json        # Settings（原子写：temp+rename，带备份 .bak）
├─ projects.json      # FrpcProject 列表（Secrets 分离）
├─ secrets.json       # 仅经 SecretStore（MVP 明文=base64 混淆 + 注释警示；预留 DPAPI）
├─ extensions.json    # 开关（或并入 config.json；推荐并入）
├─ versions.json      # 已安装版本登记
├─ versions/          # frp 二进制目录（sha256 登记）
├─ runtime/           # 每个实例的临时 TOML（UUID 子目录，退出即清理）
└─ logs/              # 应用日志 + frpc 日志（轮转）
```

- 所有写操作：临时文件 → `fsync` → 原子 rename；损坏时回退 `.bak` 并报 `SYS_CONFIG_CORRUPT`。

---

## 10. 前端架构（frontend/）

```text
frontend/src/
├─ main.ts / App.vue        # 挂载 + 主题 + i18n(zh/en)
├─ api/bindings.ts          # wails3 生成的类型化绑定（勿手改）
├─ api/client.ts            # 绑定封装、事件订阅封装（cdk 类型安全事件）
├─ router/index.ts          # 核心路由 + injectExtensionRoutes(navs)
├─ stores/                  # pinia：frpc(实例列表/状态) settings extensions portkill
├─ components/              # LogViewer(虚拟滚+搜索) ConfirmDialog CopyButton ProgressBar
├─ views/
│  ├─ HomeView.vue          # 最近实例 + 扩展摘要卡（按 enabled 控制显示）
│  ├─ FrpcProjectsView.vue  # 项目 CRUD / 代理编辑 / 启停 / 日志
│  ├─ VersionsView.vue      # 版本下载/删除
│  ├─ SettingsView.vue      # 主体设置 + 扩展管理（开关）
│  └─ AboutView.vue
└─ ext/                     # 扩展视图（lan/portkill/publicip 的页面组件；编译期内建，
                            #   由 routes 注入，未启用时不注册路由）
```

- 交互约定：所有长操作调用返回 `JobID`，前端订阅 `dl:{job}:progress` / 直接事件；取消 = 调用对应 `Cancel(jobID)`。
- 危险按钮（杀进程/删版本/删项目）统一走 `ConfirmDialog` 组件。
- 日志组件：环形缓冲 + 行级渲染 + 搜索/暂停/复制；大小上限（默认 5000 行）。

---

## 11. 错误与事件模型

### 11.1 AppError

`domain.AppError{Code, Message(zh), Detail, Action, Retryable, Cause}`；所有绑定方法返回 `(T, *AppError)`；前端根据 `Code` 渲染（内联/对话框），`Detail` 可复制。

错误码清单收敛自 PRD §12：`LAN_*` `PORT_*` `PORT_*` `FRPC_*` `VERSION_*` `PUBLIC_IP_*` `SYS_*` `EXT_*`。

### 11.2 事件表（wails events）

| 渠道 | 事件 | 载荷 |
|---|---|---|
| frpc | `frpc:{id}:state` / `log` / `exit` | State / 行 / ExitCode |
| 下载 | `dl:{job}:progress` / `dl:{job}:done` / `dl:{job}:fail` | 百分比+速率 / path / AppError |
| 扫描 | `ext:lan:progress` / `ext:lan:batch` / `ext:lan:done` | 计数 / []HostResult / 汇总 |
| 公网IP | `pubip:updated` | IPv4/IPv6+来源+时间 |
| 扩展 | `ext:changed` | []NavEntry |

---

## 12. 安全红线落点（谁强制什么）

| 红线 | 强制者 | 位置 |
|---|---|---|
| 下载校验 SHA256 | 版本模块（宿主） | versions/verifier.go |
| 终止前复核 PID+启动时间+路径 | 宿主 | host/kill.go |
| 系统进程保护（PID0/4/自身/受保护） | 宿主 | host/kill.go |
| UAC 最小提权（一次性 helper） | 宿主 | host/kill.go + cmd/killhelper |
| shell 禁用（exec 参数数组） | frpc 模块 | frpc/instance.go |
| 日志/事件脱敏（token） | logging.Redactor | logging/ + security/ |
| 绑定参数二次校验 | 各 Service 入口 | app/services.go 包装层 |
| 无遥测（无对外上报） | 架构约束 | — |

### 12.1 UAC helper 时序（Windows）

```text
portkill 扩展 ─▶ host.KillVerified
                    ├─ 普通权限可杀 → TerminateProcess → 复查
                    └─ Access Denied → 写请求文件(带 nonce/目标三元组/有效期, 用户 ACL)
                                        └▶ ShellExecuteEx(runas) hubkit.exe -mode=killhelper
                                                              │ 重读并复核三元组 →
                                                              │ 保护规则复查 → Terminate
                                                              │ 回写 result.json(ACL) → 退出
```

---

## 13. 生命周期与优雅退出

启动序：`settings.Load`（含扩展开关）→ `logging.Init` → `platform.New` → `versions` 登记 → `frpc.FrpcManager` → 扩展注册 → wails `app.Run`。

关闭序：窗口关闭 → 对每个 Running 实例：按设置询问「停止并退出 / 保留后台（Windows 下 JobObject 分离或停机不留）」→ 取消 ctx → 停止下载器 → 清理 runtime 临时目录 → flush 日志 → 退出。

崩溃兜底：Windows JobObject 保证 frpc 随宿主退出终止；杀进程请求文件 5 分钟自动过期。

---

## 14. 构建与发布（Taskfile.yml）

| 任务 | 作用 |
|---|---|
| `dev` | wails3 dev（HMR） |
| `generate` | 重新生成前端 bindings |
| `test` | go test ./... + 前端 vitest |
| `build:win` | 产物 + manifest(asInvoker) + 图标 + ldflags 版本戳 |
| `build:portable` | 组装 `data/` 骨架 + zip 压缩包（绿色版） |
| `doctor` | WebView2 运行时检测/指引 |

- 版本信息：`-ldflags -X main.Version=...`，注册到 About 与日志头部。
- 便携包内含：exe、默认 `data/.gitkeep`+`config.json` 模板、README。
- WebView2 缺失：启动时提示 + 打开官方引导页；首期不捆绑 runtime。

---

## 15. 测试策略

| 层 | 方式 |
|---|---|
| domain / netcore / docgen / redactor / verifier / matcher | 纯 Go 单测 + golden 文件 |
| frpc 状态机 | fake frpc（测试脚本进程）：正常/崩溃/卡住/慢退出 |
| 进程复核 | 启动测试进程 → 改时间/复用 PID 模拟变化 → 拒绝终止 |
| portkill 集成 | 临时监听端口 → 映射 PID → 复核 → 杀 → 复查释放 |
| 扩展框架 | 假扩展注册/启用/禁用 → 路由与服务随开关变化 |
| 平台（Windows CI） | IP Helper 表、ICMP、JobObject、UAC helper（手动/受限机器） |

---

## 16. 里程碑映射

- M0：platform 接口 + Windows 首实现（ICMP/端口表/JobObj/SecretStore-明文）→ 冒烟。
- M1：app 骨架 + extapi/registry + settings + 日志 + wails 绑定 + 前端导航。
- M2：扩展[lan] + [publicip]。
- M3：扩展[portkill] + host.KillVerified + UAC helper + 保护规则。
- M4：frpc 核心（docgen/manager/instance/logstream）+ versions。
- M5：收尾（测试矩阵、便携打包、文档）。

---

## 17. 明确不做（先声明，防蔓延）

- 不做真插件热加载 / 插件市场 / 插件签名体系；
- 不做 frps 服务端；不做 Web 管理端；
- 不做 1–65535 全量扫描 / SYN / 隐蔽探测；
- 不做远程进程管理；不做开机自启（P1 再议）；
- 不变更第三方独立工具职责（抓包、代理软件等）。
---

## 附录 A：frpc 单实例 vs 多实例 全方位对比

> 本文档结论：采用**混合模型**——「一个项目内支持多代理（单实例能力）」+「项目间多进程多实例（并行运行多个项目）」。

### A.0 先厘清两个层次

| 层次 | 含义 | 例子 |
|---|---|---|
| 层次一：单进程内多代理 | 一个 `frpc.exe` + 一份 TOML 内多条 `[[proxies]]` | 一个项目同时穿透 TCP 8080 + HTTP 域名 |
| 层次二：多进程多实例 | N 个 `frpc.exe`，各自独立配置/服务端/生命周期 | 项目 A 连测试 frps + 项目 B 连公网 frps 并行 |

单实例 = 只占层次一；多实例 = 层次二（每个实例内部仍可占层次一）。

### A.1 二十个维度逐项对比

| # | 维度 | 单实例（1 进程/1 配置） | 多实例（N 进程/N 配置） |
|---|---|---|---|
| 1 | 进程与连接 | 1 个进程、1 条到 frps 长连接 | N 个进程、N 条连接（可与不同 frps） |
| 2 | 服务端（frps） | 同一时间只能连 1 个 | 每个项目各连各的，可同时连多个 |
| 3 | 配置模型 | 一份 TOML；改一条代理需整体 reload | 每项目独立 TOML；独立生成/校验/失败定位 |
| 4 | 认证/凭据 | 一套 token | 每实例独立 token，SecretStore 按 `projectID` 分作用域 |
| 5 | frp 版本 | 应用级单一版本，升级影响全体 | **每项目可锁版本**，交叉并存（0.5x + 0.6x 共跑） |
| 6 | 生命周期 | 1 个状态机，整体启停 | N 个状态机（`FrpcManager.map[id]*Instance`）；同 ID 禁止重复启动 |
| 7 | 故障隔离 | 单进程崩溃→全部隧道中断；单代理异常可能拖累整体 | **崩溃只挂自己**，退出码独立，其他实例照常 |
| 8 | 启停域 | 全局一个开关 | 单项目启停；一次操作不影响他人 |
| 9 | 内存占用 | ~5–15MB（含连接） | 每实例 +5–15MB（N 个翻 N 倍，10 实例≈100–150MB） |
| 10 | 连接/带宽 | tcpMux 复用一条连接，开销最低 | 每实例独立连接，连接数与带宽随 N 叠加 |
| 11 | 吞吐 | 单连接复用的合并调度，同 frps 下最好 | 独立连接互不阻塞；跨 frps 时多实例是唯一可行方案 |
| 12 | 日志可观测性 | 一份日志多代理混杂，定位靠 grep | 每实例独立日志 + `frpc:{id}:log` 事件，定位即点即得 |
| 13 | 端口冲突 | 冲突仅限同配置内部（自己保证唯一） | 需**启动前检查**：同 frps 的 remotePort 全局唯一、项目本地端口不重复 |
| 14 | 孤儿进程防护 | 1 个 JobObject | 每实例独立 JobObject；宿主退出统一收尸 |
| 15 | 安全面 | 单进程单一权限上下文，被利用=全开 | 每实例独立会话/日志，攻击面被分割；但多 PID 追踪面变大 |
| 16 | UI/UX | 一个开关，心智最简单 | 项目卡片矩阵各自启停（循环渲染，前端成本低） |
| 17 | 心智模型 | 弱「项目」概念 | 强「项目」概念：一个项目 = 一个可独立控制的实例 |
| 18 | 测试/调试 | 单进程单日志，矩阵小 | 需 fake 多进程矩阵；但实例间测试隔离更干净 |
| 19 | 维护/运维 | 近乎零 | 需处理：同 ID 防重、remotePort 冲突提示、版本存在性、崩溃上报 |
| 20 | 演进能力 | 加多环境要改进程内结构 | 天然支持多环境/多团队并行；可扩展「环境模板一键切换」 |

### A.2 开发成本差异（对比基线 = 单实例多代理）

| 新增能力 | 成本 | 说明 |
|---|---|---|
| `FrpcManager` 多实例注册表与防重 | +0.5d | map + mutex，单一职责 |
| 每实例 JobObject/日志/事件独立 | +0.5–1d | 复用同一 Instance 类型，仅按 id 分实例 |
| remotePort/本地端口前置冲突检测 | +0.5d | 启动前查一次表即可 |
| UI 项目卡片循环 + 各自日志视图 | +0.5d | 一个组件参数化 |
| 多实例集成测试（fake frpc 矩阵） | +0.5–1d | 3–4 个 fake 场景脚本 |
| **合计** | **约 2.5–3.5 天（一次性）** | 换取的能力：多 frps 并行、多版本并存、故障隔离 |

### A.3 什么情况下该考虑退回纯单实例

- 目标用户永远只联调 1 个 frps、1–2 条隧道；
- 极端低资源设备（内存 < 1GB）；
- 产品定位是「给单场景穿透用的最小工具」。

以上任意一条成立才值得退；本产品定位（开发者多项目联调）三条都不成立，维持多实例。

### A.4 我们的最终组合决策

```text
项目（图纸卡片）
├─ 项目内：多条 proxy（层次一：单实例能力，覆盖"一个 frps 透多个端口/域名"）
└─ 项目间：多进程并行（层次二：多实例，覆盖"多个 frps / 多版本 / 多环境同时联调"）

管理器约束：
  1. projectID 唯一 → 同一项目不可双开
  2. 启动前检查 → 同 frps remotePort 唯一、项目本地端口不重复
  3. 每实例独立 JobObject + 独立日志 + 独立事件通道
  4. UI：项目卡片矩阵，每卡片独立启停/日志/状态
```

---

## 附录 B：HubKit（Go/Wails3） vs frpc-desktop（TypeScript/Electron） 全方位对比

> 两个产品功能高度重叠（frpc 客户端 + 可视化配置 + 版本管理 + 内置扩展差异），代表两种技术路线。以下按「落地事实 + 诚实估测」给出（内存/启动/包体为典型量级，实际以 M0 实测为准）。

### B.1 技术栈对比

| # | 维度 | frpc-desktop（Electron+TS） | HubKit（Go+Wails3+Vue） |
|---|---|---|---|
| 1 | 语言 | TS/Node/HTML/CSS 全栈 | 后端 Go + 前端 TS/Vue（两个语言） |
| 2 | 渲染内核 | 内置完整 Chromium | Windows:系统 WebView2（Chromium 内核）；macOS WKWebView；Linux WebKitGTK |
| 3 | 运行时 | Node + Electron，随应用分发 | Go 单二进制；WebView 运行时由系统提供 |
| 4 | 持久化 | NeDB → SQLite（better-sqlite3 原生模块） | 纯 JSON/TOML 文件（MVP 无数据库） |
| 5 | 构建链 | Node+Vite+electron-builder（每平台打包） | Go build + wails3 嵌入前端产物（交叉编译容易） |
| 6 | 依赖体积 | node_modules 数百 MB；安装包典型 70–110MB | Go 依赖全部编译进二进制；安装包典型 15–25MB（不含 WebView2） |

### B.2 运行时表现对比

| # | 维度 | Electron | Wails3 |
|---|---|---|---|
| 7 | 空闲内存 | 常驻 200–500MB | 常驻约 80–200MB（WebView2 进程开销） |
| 8 | 冷启动 | 约 2–5s（加载 Chromium） | 约 0.5–2s（系统 WebView 更快） |
| 9 | 包体 | 大一码（"浏览器"随装随走） | 小（系统已有运行时） |
| 10 | CPU/多开 | 每窗口一个 Chromium 进程组 | WebView2 进程组按需创建 |

### B.3 开发与工程对比

| # | 维度 | Electron | Wails3 |
|---|---|---|---|
| 11 | 后端系统能力 | Node child_process + 原生模块（tree-kill、better-sqlite3 等） | Go 标准库 + x/sys；无需原生模块重编译 |
| 12 | 类型安全 | TS 可带类型，但 Electron 主/渲染 IPC 常手写 any | wails3 自动生成类型化绑定（Go↔TS 双向） |
| 13 | 跨平台一致性 | 三平台同一 Chromium，行为最一致（含 WebGL/自动机） | 各平台系统 WebView 有差异（Linux WebKitGTK 最弱、问题最多） |
| 14 | 生态/成熟度 | 电子应用生态极成熟，问题答案多 | Wails 生态较小；v3 在功能推进中，文档略薄 |
| 15 | 前端 UI 库 | Element Plus 等 Vue 组件库可用 | 同一批 Vue 组件库可用（渲染能力接近） |
| 16 | 调试体验 | DevTools 内置满血（断点/网络/内存） | WebView2 DevTools 可用；Go 侧 dlv 调试 |
| 17 | 安全默认值 | nodeIntegration/contextIsolation 需显式收紧（原版正是风险点） | 默认无 nodeIntegration、隔离良好；绑定参数建议二次校验 |
| 18 | 更新策略 | Chromium 随之更新，版本升级是持续负担 | WebView2 由 Windows Update 管理，应用本体为小更新 |
| 19 | 发布/签名 | 需要 per-platform 打包；便携版是"绿色目录+运行时缺失警告" | 交叉编译即得单二进制；便携版打包极简 |
| 20 | 测试 | 主进程测试需 Electron runtime；renderer 需浏览器 | Go 单测天然；前端 vitest；无需模拟 Electron 环境 |
| 21 | 维护成本（长期） | Node/Electron 大版本升级周期 | Go 工具链 + Wails 依赖升级轻量 |
| 22 | 开源许可 | MIT（原版 MIT） | MIT（Wails3 MIT） |

### B.4 产品对比（同功能位面）

| # | 维度 | frpc-desktop | HubKit |
|---|---|---|---|
| 23 | 功能完整度 | 高：完整代理类型/批量端口/导入导出/多语言（已发布 1.x） | 规划中（我们刚定完 PRD/架构） |
| 24 | 差异化功能 | 无局域网扫描/端口查杀 | 具备扩展三件套（差异化来源） |
| 25 | 技术栈契合度（对你） | 后端生态偏离 Go | 后端 Go 完全契合你的主栈 |
| 26 | 可维护性（对你的长期价值） | TS 代码库维护需要持续投入 JS 生态 | Go 代码库，单一 test 链、单一二进制 |

### B.5 结论与各自的适用位面

**选 Electron（原版路线）当：**
- 需要三平台渲染 100% 一致（含 WebGL、自动化测试脚本）；
- 前端重、需要最大生态兜底；
- 团队本就在 TS 全栈；
- 项目追求"拿来即用"而非深耕系统层。

**选 Go+Wails3（本路线）当：**
- 你是 Go 开发者，希望后端逻辑/二进制/测试一步到位；
- 便携版 + 单二进制 + 小包体是交付刚需；
- 系统级能力（IP Helper、进程复核、ICMP、JobObject）是核心价值——Go 直接调系统 API，比 Node 的 child_process 壳更容易做干净；
- 能接受"依赖系统 WebView2"这一运行时前提（Windows 10/11 绝大多数已具备）。

**HubKit 的最终判断**：功能位面与原版对齐（甚至 edge 上更好），技术位面选择"轻内核 + 单二进制 + Go 系统层"这条路；前端复杂度集中在 Vue，两个项目在这一层是可比的，真正的差异收益在后端与交付形态。

---

## 附录 C：同类项目三家对比（frpc-desktop / FrpGUI / HubKit）

> FrpGUI（Mirai，MIT，2026-08-13 立项，v1.0，~1 万行 Go）：纯 Go + Shirei（IMGUI 即时模式 GUI，无 CGO、零运行时依赖），单一原生二进制 ~10MB，frpc 由用户自行放置同目录。
> 注意：FrpGUI 极新（立项 7 天），其定位值得参考，但成熟度不宜作基准。

### C.1 技术与形态

| 维度 | frpc-desktop | FrpGUI | HubKit |
|---|---|---|---|
| 语言/框架 | TS + Electron + Vue | Go + Shirei（IMGUI） | Go + Wails3 + Vue |
| 运行时依赖 | 内置 Chromium（~100MB 包） | 无（真零依赖） | 系统 WebView2（Win10/11 大多已具备） |
| 包体/资源 | 70–110MB / 200–500MB 内存 | ~10MB / 最小（原生直渲） | 15–25MB / 80–200MB |
| GUI 生态风险 | 极低（成熟） | **高（Shirei v0.6.6，单人维护、社区极小）** | 中（Wails3 beta，社区发展中） |
| 代码量/测试 | 较大，无自动化测试脚本 | ~10k 行含丰富单测 | 规划中 |

### C.2 功能位面对比

| 能力 | frpc-desktop | FrpGUI | HubKit（规划） |
|---|---|---|---|
| frp 可视化配置 | ✅ 全代理类型 | ✅ 服务端/代理/工具/日志标签页 | ✅ 项目化 |
| 多版本下载管理 | ✅ GitHub+镜像+SHA256 校验 | ❌ 用户自备 frpc | ✅ GitHub 直连+硬校验 |
| 多实例并行 | ❌ 单实例 | ❌ 单实例（FrpcManager 单 cmd） | ✅ 多实例（P0） |
| 配置导入导出 | ✅（TOML 识别+双向） | ✅ 导入/导出 toml | ✅ |
| 多语言 | ✅ en/zh | ✅ 16 种（文件即生效） | ⏸ en/zh |
| 主题 | 固定 | ✅ toml 主题文件热切换 | ⏸ 计划单主题+暗色 |
| 托盘/自启 | ✅ 托盘 | ✅ 托盘+自启（registry/launchagents/.desktop） | ✅ 托盘（P0）自启（P1） |
| 完整性校验 | ✅ 下载即校验 | ⚠️ frpc.sha256 侧车文件，仅告警不阻断 | ✅ 硬门（无哈希不发启动） |
| 连接检测/诊断包 | 部分 | ✅ netcheck + 诊断 zip（配置脱敏） | ⏸ S4 诊断包（P1） |
| 单实例锁/崩溃保护 | 部分 | ✅ 互斥体 + crash guard | ⏸ 计划 |

### C.3 安全位面对比（诚实排序）

| 项 | frpc-desktop | FrpGUI | HubKit（规划） |
|---|---|---|---|
| token 存储 | 明文（无加密） | 明文（有 clear_creds 清理入口） | 明文 MVP + DPAPI 接口预留（外网前阻断项） |
| 进程结束 | taskkill /T /F，无复核 | TerminateProcess 直杀，无复核 | 终止前复核 PID+启动时间+路径 |
| 提权 | 无专门设计 | 无 | UAC helper（一次性最小提权） |
| 下载校验 | 有 | 有（信息性） | 强制 |

### C.4 差异化

| 能力 | frpc-desktop | FrpGUI | HubKit |
|---|---|---|---|
| 局域网扫描/复制 IP | ❌ | ❌ | ✅ 内置扩展 |
| 释放端口 | ❌ | ❌ | ✅ 内置扩展 |
| 公网 IP | ❌ | ❌ | ✅ 内置扩展 |
| 扩展机制 | ❌ | ❌ | ✅ 内建扩展模块+接口 |

### C.5 值得借鉴清单（已归入我们的设计）

1. **frpc.sha256 侧车文件**：用户自备 frpc 场景下保留「信息性校验 → 硬门」两级策略（对齐友情提示而非阻断，官方库场景仍为硬门）。
2. **文件式语言包**：lang/*.toml 放文件即生效，热切换——替代或补充生成式 i18n，便携友好（P1）。
3. **主题 toml 文件扩展**：主题即文件、删文件即没主题、保底浅/深两套（理念直接采用）。
4. **单实例互斥锁（Windows 命名 Mutex）+ crash guard + 诊断包导出（含配置脱敏）**：参考其实现。
5. **配置自动保存 + 备份**：设置变更即写盘 + .bak（我们原子写方案兼容）。
6. **连接性/延迟检测**：可并入我们的诊断/首页能力（P1）。

### C.6 结论

- FrpGUI 验证了「纯 Go 单二进制最小 frpc 客户端」的可行性，但它同时舍弃了版本管理、多实例、进程复核等对开发者联调更重要的能力——与我们定位不冲突，反而是接近我们"基线的下方边界"。
- Shirei IMGUI 对复杂表格/日志/扩展注入（DOM 优势区）不友好，Wails3 决策不因它改变。
- 我们的差异化（局域网扫描/释放端口/多实例/扩展框架）在三个项目中仍保持唯一。

---

## 附录 C.7：三家优缺点一览

### frpc-desktop（Electron + TS）— 重功能全家桶

**优点**

- 功能完整度最高：全代理类型（tcp/udp/http/https/stcp/xtcp/visitors）、批量端口、多用户插件、多语言、导入识别 frpc.toml。
- 版本管理成熟：GitHub Releases + 镜像站 + SHA256 校验 + 便携版。
- 用户与时间验证：2023-11 起，v1.2.6，下载 1 万+，Issue/问答多。
- 三平台渲染一致（内置 Chromium）。
- 近期做了 NeDB→SQLite 结构化迁移，工程态度在补强。

**缺点**

- 技术栈重：包体 70–110MB、空闲内存 200–500MB、Node 构建链、Electron 大版本是长期升级负担。
- 安全弱：`nodeIntegration: true / contextIsolation: false`；token 与 webServer 密码明文落盘；`taskkill /T /F` 硬杀不复核进程身份。
- 工程细节欠佳：v1.2.5→v1.2.6 纯发版无代码变更；默认配置对象在多文件重复；日志含公众号/捐赠引导；README 称"无自动化测试脚本"。
- 无开发者工具差异化：无局域网/端口/多实例等开发场景能力。

---

### FrpGUI（Go + Shirei IMGUI）— 极致轻量最小客户端

**优点**

- 真零依赖单二进制 ~10MB，原生直渲，资源占用三者最低；双击即跑。
- 工程细节三者最好：~1 万行含大量单测、配置自动保存、诊断包导出（配置脱敏）、崩溃保护、单实例互斥锁、连接/延迟检测。
- 便携友好：settings.json 同目录；语言包/主题全部文件化（放文件即生效）。
- 供应链可观测：`frpc.sha256` 侧车文件做完整性校验（信息性告警）。
- 文档规范：中英双语 README、SECURITY.md、THIRD-PARTY-LICENSES.md、SBOM 工具齐备。

**缺点**

- 立项仅 7 天（v1.0）：无真实用户、无社区、无问题沉淀。
- Shirei 框架 v0.6.6 单人维护、生态趋近于零；IMGUI 对复杂表格/日志检索/扩展注入不友好，长期维护风险三者最高。
- 功能面最小：无版本管理/多实例/扩展机制；frpc 需用户自备。
- 安全弱：token 明文（只有清理入口）；Windows 直杀 `TerminateProcess`，无复核、无提权设计。
- 自动保存对 frpc 配置是双刃剑：误改即落盘、无版本回滚。

---

### HubKit（Go + Wails3 + Vue）— 开发者联调工作流（进行中）

**优点**

- 差异化唯一：局域网扫描 + 释放端口 + 公网 IP 扩展，面向开发者三项高频操作。
- 多实例 + 每项目锁 frp 版本 + 独立日志/生命周期——开发者刚需，另两家没有。
- 安全设计三者领先（规划）：终止前复核 PID+启动时间+路径、UAC helper 最小提权、SHA256 硬门、绑定参数二次校验、日志脱敏。
- Go 系统层直接对接 Windows API（IP Helper/JobObject/DPAPI），后端单测与交叉编译友好。
- 便携单二进制 15–25MB + WebView2 现代 UI；PRD/架构文档先行，范围受控。

**缺点**

- 最大缺点：还没有实现（框架骨架刚打通，0 个业务功能），上线时间最晚。
- Wails3 beta（v3.0.0-beta.10）：社区小、文档薄、API 未冻结（已验证需修 Taskfile/绑定路径）。
- WebView2 外部运行时依赖：Win11 基本内置，部分 Win10 需另装。
- 双栈复杂度：Go 后端 + Vue 前端两个语言、两条构建链。
- 功能范围小于 frpc-desktop：stcp/xtcp、批量端口、多语言均排后。

---

### 一句话总结

> frpc-desktop 赢在「全而成熟」，FrpGUI 赢在「轻而严谨」，HubKit 赢在「面向开发者差异化的安全设计」——三者缺的时间验证、重量、功能面各不相同。
> 对使用者：要现成可用的 frp 客户端，直接选 frpc-desktop；要极致轻量，拿 FrpGUI；**要"找 IP + 杀端口 + 多项目联调"的工作流，才是 HubKit 存在的理由**（也正因此我们不必在功能面与 frpc-desktop 全面对齐）。

---

## 附录 D：平台决策记录（2026-08-20 定案：仅 Windows）

> 决策：HubKit 定位为**仅 Windows 开发者工具箱**（Windows 10 22H2 / 11 x64 首发；
> arm64 尽力交叉编译；macOS/Linux 不实现、不发布、不测试）。
> 本附录替代早期"Windows 优先、跨平台预留"的草案，作为架构与文档的最终依据。

### D.1 决策理由

1. **产品定位**：开发者工具箱（frpc 联调 + 局域网扫描 + 释放端口），目标用户在 Windows 生态；
   macOS/Linux 需求占比低，收益不足以覆盖维护面。
2. **跨平台的真实成本**（不止初始实现）：
   - 每功能 × 3 平台 = 3 套实现 × 3 套测试矩阵；
   - WKWebView / WebKitGTK 行为差异排障；
   - Apple 签名 + 公证（$99/年 + notarytool 流程）；
   - 每次 Windows API 变更都要同步评估 mac/linux 等价物 —「注意力税」对单人项目最贵。
3. **保留选项**：分层已保证核心/业务/前端 100% 与平台解耦；若未来需要 macOS，
   只需补齐 `internal/platform/darwin` 实现与发布链，性价比届时单独评估。

### D.2 取消的能力与对应处理

| 草案内容 | 定案处理 |
|---|---|
| darwin/linux 平台实现 | 不做；`platform` 仅实现 windows（`_windows.go`），其余 OS 返回 `ErrNotSupported` 占位 |
| `check:cross` 跨平台编译门 | 移除；改为 `task check`（vet + windows 显式包构建） |
| macOS 签名/公证流程 | 不开展 |
| Keychain / WebKitGTK 适配 | 不开展（凭据仅 DPAPI） |
| 前端平台适配代码 | 前端不做平台分支（仅展示 GOOS） |

### D.3 仍然保留的架构约束（成本≈0，价值留到将来）

1. `internal/platform` 接口纯度：业务只依赖接口，不泄漏 Windows 概念；
2. `VerifyToken{PID, StartedAt, Path}` 复核模型本身是跨平台可表示的，不用改；
3. 路径层用 `os.UserConfigDir/UserCacheDir`（Windows 上即 %AppData%/%LocalAppData%），便携模式不受影响；
4. 错误统一 `domain.AppError`，不暴露平台错误码。

