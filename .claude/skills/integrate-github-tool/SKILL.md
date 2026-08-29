---
name: integrate-github-tool
description: 把 GitHub 上的桌面/CLI 工具集成进 Hanxi（托管模式：版本管理+JobObject 启停+前端控制台）。用户提供 GitHub 地址说"集成到 hanxi"时使用。已用本模式落地 markeron、everything、ccswitch 三个模块。
---

# 集成 GitHub 工具进 Hanxi（托管模式）

用户给出 GitHub 地址要求"把 XX 集成到 hanxi"时，按本 skill 执行。已落地先例：**markeron**（GitHub releases、无官方哈希）、**everything**（官网下载、ini 改写藏托盘）、**ccswitch**（GitHub digest 官方 sha256、tauri 单实例互斥体）——新模块优先对照 ccswitch 模板（最新最完整）。

## 阶段 0：上游侦查（先查后问，全部要实证）

用 `curl` 直连 GitHub API（git bash 无 gh；`python` 可能不在 PATH，用 node 或 grep 解析 JSON）：

```bash
curl -s "https://api.github.com/repos/<owner>/<repo>"                        # license/default_branch
curl -s "https://api.github.com/repos/<owner>/<repo>/releases?per_page=60"    # 资产列表、digest、大小
curl -s "https://api.github.com/repos/<owner>/<repo>/readme" -H "Accept: application/vnd.github.raw+json"
curl -s "https://api.github.com/repos/<owner>/<repo>/git/trees/<branch>?recursive=1"
curl -s "https://api.github.com/repos/<owner>/<repo>/contents/<file>" -H "Accept: application/vnd.github.raw+json"
```

**必查清单（每项要有源码/API 实证，严禁猜）**：

1. **便携资产**：releases 里有没有 `*-Portable.zip`/`*_x64_portable.zip`；连续 N 个版本是否稳定发。zip 内部布局直接下载验证；下载域名（release-assets.githubusercontent.com）可能 DNS 失败——重试，或读 `.github/workflows/release.yml` 的打包步骤推断布局
2. **官方哈希**：资产 `digest: "sha256:<hex>"` 是否存在（2024 起 GitHub 全量有）→ 决定四层完整性的第一层；无 digest 则降级 markeron 三层（字节数+CRC32+布局自检）
3. **单实例/唤窗契约**：tauri 应用 → 源码 `tauri-plugin-single-instance` 依赖是否启用 semver feature、回调是否无条件 show+focus；mutex 名 = `{identifier}-sim`、窗口类 `{identifier}-sic`。CLI 应用 → 有无 `-startup`/`-quit`（everything）或纯信使语义（无 CLI 则"打开窗口"=无参拉起）
4. **探测方式**：优先命名互斥体（`OpenMutex(SYNCHRONIZE)` 最小权限）；找不到互斥体再看顶层窗口/进程名
5. **数据落盘**：配置跟 exe 走（everything portable）还是固定用户目录（ccswitch `~/.cc-switch`）→ 决定 ImportLocal 搬什么（整套 vs 单 exe）
6. **关窗/退出语义**：`on_window_event(CloseRequested)` 是 exit 还是 hide 驻托盘 → 决定 Quit 走 WM_CLOSE/信号 + 宽限 + 强杀兜底的结构
7. **托盘**：无条件创建还是可配置。用户问"能藏托盘吗"：上游无开关就如实说"要 fork"；**托盘有功能价值时（如供应商切换菜单）明确建议不藏**——别默认隐藏

## 决策点：集成范围

侦查后若存在"内嵌核心功能"的分叉（如 everything 的内嵌搜索），用 AskUserQuestion 给两选项并**把纯托管设为 Recommended**（上游界面已完整，内嵌重做性价比低；内嵌还会把上游的配置数据纳入 Hanxi 直接改写，风险高）。用户拍板后进入模板实施；决策记录写进模块 package 注释。

## 阶段 1-2：Go 子包（照 ccswitch 模板文件清单）

```
internal/modules/<mod>/
├── version/
│   ├── models.go        # <X>Release / <X>VersionInfo / DownloadProgress
│   ├── remote.go        # GitHub API 列表：plainSemverTag 过滤 + findPortableAsset（后缀排除 arm64/msi/sig）+ 10min cache
│   ├── downloader.go    # assetMirrors（4 个镜像回退）+ downloadTo 重试 + fileSHA256/verifySHA256
│   ├── manager.go       # ListInstalled/Download（四层完整性）/ImportLocal/Remove/ResolveExe + extractAll（ZipSlip+CRC 读满+布局自检）
│   └── version_test.go
└── instance/
    ├── prober.go        # 探测接口：IsRunning / WaitForReady / （窗口开）IsXxxWindowOpen
    ├── probe_windows.go / probe_other.go
    ├── instance.go      # Engine：Start/OpenWindow(信使)/Quit(优雅+宽限+强杀兜底)/Stop/RefreshExternal/wait 四分类
    ├── close_windows.go / close_other.go   # WM_CLOSE 信使（PostMessage/FindWindowW），可有可无
    └── instance_test.go # fakeProbe/fakeJobAPI 注入 + cmd.exe 冒烟进程 + waitState 轮询
```

**Engine 必须保持的硬约定**（三个模块一字不差，照抄勿改）：
- Start：JobObject `KILL_ON_JOB_CLOSE` 绑定；`cmd.Dir = filepath.Dir(exe)`；wait() 里 job.Close+cmd=nil
- 信使进程（OpenWindow）Start+Release **不 Wait、不进 Job**——代码注释里写明"勿好心改成 Wait"（markeron 先例，改了就拖慢冷启动）
- wait() 分类顺序不可换：stopping → 外部接管（`!stopped && probe.IsRunning()`）→ exit0 → failed（错误文案指向 WebView2/依赖）
- 退出宽限期用**包级变量**（`closeGracePeriod`），单测压缩、生产恢复

## 阶段 3：模块骨架

- `store.go`（activeVersion 原子写/损坏容忍）、`models.go`（ControlOutcome/QuitOutcome）、`module.go`（Nav 照 ccswitch：Icon/Order/SectionExt）、`service.go`（版本八件套 + GetStatus + OpenWindow/Quit + openXXX 控制编排 + 外部感知 watcher）
- `internal/app/app.go`：两处事件 `RegisterEvent[<version>.DownloadProgress]("<mod>:version-download")`、`RegisterEvent[<instance>.Snapshot]("<mod>:instance-state")`（**值类型，Emit 也传值**——指针/值不匹配会被 Wails 静默丢弃，TROUBLESHOOTING #9）+ `modulesToRegister` 追加
- 需要"打开安装目录"就加模块自有 `OpenDir(dir)` RPC（explorer.exe 目录语义），**绝不复用 AppService.OpenPath 传文件路径**（markeron 事故：explorer.exe 收 exe 会执行它）
- PE 版本读取复用 `internal/platform/versioninfo.FileVersion`（共享包，勿再复制）

## 阶段 4：前端

- `<X>View.vue` 照 `CCSwitchView.vue`/`EverythingView.vue` 双 Tab：控制台（状态灯+版本/PID/时长+按钮组+条件提示条）+ 版本管理（已装卡片+远程表+导入本地）
- 三处挂载：`App.vue`（import + CORE_VIEWS + ROUTE_MODULE_MAP）、`HomeView.vue` MODULE_META
- **bindings 必须 `task common:generate:bindings`**（裸 `wails3 generate bindings` 会清空输出目录）
- CSS：所有自定义类名带模块前缀（`cc-`/`ev-`）——App.vue 全局有 `.status-dot`（7px）会压扁表格圆点（markeron 垂直字体事故）
- 验证：`npm run build`（vue-tsc 零报错）

## 阶段 5：真机联调（用户参与，清单化验收）

下载真实版本 → 启动/唤窗/退出 → 状态灯六态 → 外部实例感知（用户自行启动）→ 导入本地 → JobObject 连带退出。**离线难验证项**（编码/落盘位置/真实退出行为）逐条列给用户勾验。

## 阶段 6：收尾

1. `gofmt -w` + `go test ./...` + `task check` + `npm run build` 全绿
2. 新契约/新坑 → `docs/TROUBLESHOOTING.md` 新条目（问题现象/排查过程/正确做法/避坑建议四段式）
3. 汇报变更摘要等用户"合理提交"指令；原子提交拆分模板：共享包提取独立 refactor commit → version 子包 → instance 子包 → 骨架装配 → 前端+bindings → docs（每步独立可编译）

## 既有模块对照速查

| 特征 | 参照模块 | 关键文件 |
|---|---|---|
| 多形态发布资产（自包含+框架依赖变体+环境推荐） | bcu | version/remote.go pickAsset / dotnet_windows.go 环境探测 |
| GitHub releases 无官方哈希 | markeron | version/downloader.go 三层校验 |
| 官网下载+官方 manifest 哈希 | everything | version/remote.go 网页解析+快照兜底 |
| GitHub digest 官方 sha256+tauri 单实例 | **ccswitch（主模板）** | remote.go/instance/** |
| 内嵌 CLI 搜索/额外组件 | everything | search/es.go + EnsureTool |
| ini 改写（托盘隐藏等） | everything | instance/config.go ensureHiddenTray |
| 空闲自动退出 | everything/ccswitch | service.go idleCheck/touch/shouldIdleQuit |

## 红线提醒

- 上游闭源/无哈希/资产不稳定的项目，先如实告诉用户风险再开工
- 用户没要求就别 fork 上游；藏托盘/加功能等 fork 类需求明确报价后等拍板
- 全程中文汇报、结论先行；每个阶段验证通过才进下一阶段