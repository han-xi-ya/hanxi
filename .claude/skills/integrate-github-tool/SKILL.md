---
name: integrate-github-tool
description: 把 GitHub 上的桌面/CLI 工具集成进 Hanxi（托管模式：版本管理+JobObject 启停+前端控制台）。用户提供 GitHub 地址说"集成到 hanxi"时使用。已用本模式落地 markeron、everything、ccswitch、vscode（双形态托管）等模块。
---

# 集成 GitHub 工具进 Hanxi（托管模式）

用户给出 GitHub 地址要求"把 XX 集成到 hanxi"时，按本 skill 执行。已落地先例：**markeron**（GitHub releases、无官方哈希）、**everything**（官网下载、ini 改写藏托盘）、**ccswitch**（GitHub digest 官方 sha256、tauri 单实例互斥体）——新模块优先对照 ccswitch 模板（最新最完整）。**vscode** 是"双形态托管"先例（官方 CDN 单源、GitHub 无二进制、哈希仅最新版、Inno 安装版 + zip 便携版两台 Engine，见 TROUBLESHOOTING #31）。另有反模板先例 **rustdesk/subnetdesk**（rust-portable 自解压单 exe：外层秒退不能当生命周期锚点、提取目录父 PID 闭包归属、无优雅退出无唤窗契约，见 TROUBLESHOOTING #24）、**bcu**（#24 家族第二变体：根目录小体积 exe 是官方 bootstrapper，~20ms 接力自退，真身在 `win-x64\`——ResolveExe 直指内层，见 manager.go innerExeRel）与 **litemonitor/guoheview/recordly**（进程名探测 + EnumWindows 唤窗家族）、**airsnip**（**无主窗口 + 无 CLI + 上游唤窗是死代码**：托盘常驻只有一个隐藏消息窗，上游"第二实例 PostMessage(WM_APP+2)"在 wndproc 里无人处理——照信使先例写必然静默失灵；唯一可编程入口是它自己托盘菜单的内部 ID，故用 WM_COMMAND 投递，且**送达判据取"对应顶层窗真出现"而非 PostMessage 返回值**（UIPI 丢弃时返回值照样成功），见 TROUBLESHOOTING #143/#144）——开工前先按上游实例模型选对模板族，别硬套互斥体。

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

1. **便携资产**：releases 里有没有 `*-Portable.zip`/`*_x64_portable.zip`；连续 N 个版本是否稳定发。**GitHub releases 可能根本不挂二进制**（大厂常把分发留在自有 CDN——VS Code 的 release 只有源码归档，`.../releases/expanded_assets/<tag>` HTML 实证）——先确认资产真实存在，再谈布局；zip 内部布局直接下载验证，大 zip 用 **HTTP Range 拉尾部 + 离线解析中央目录**（VS Code 332MB 包只取了 512KB 就拿到全部条目）；下载域名（release-assets.githubusercontent.com）可能 DNS 失败——重试，或读 `.github/workflows/release.yml` 的打包步骤推断布局。**警惕 zip 无根目录变体**：exe 躺在包根 + 运行时目录名随 commit 漂移（布局自检须按"存在 */resources/app/product.json"这类不变式，勿写死路径）
2. **官方哈希**：资产 `digest: "sha256:<hex>"` 是否存在（2024 起 GitHub 全量有）→ 决定四层完整性的第一层；无 digest 则降级 markeron 三层（字节数+CRC32+布局自检）。**哈希可能仅最新版可得**（VS Code 官方 feed 恒返最新版 manifest，历史版本无哈希可查）——降级路径与 UI 标注（meta.verifiedHash + 徽章）要一次做全
3. **单实例/唤窗契约**：tauri 应用 → 源码 `tauri-plugin-single-instance` 依赖是否启用 semver feature、回调是否无条件 show+focus；mutex 名 = `{identifier}-sim`、窗口类 `{identifier}-sic`。**信使路线必须读到接收端**：只看"第二实例 PostMessage 了什么"不够，要把它投的消息号在窗口过程 match 分支里逐条搜一遍——上游发送端存在而处理分支缺失的死代码真实存在（airsnip 的 WM_APP+2 即如是，`src/app/wndproc.rs` 全文无该分支），照信使写出来的唤窗会 100% 静默失灵且全程无报错。electron 应用（VS Code/markeron 家族）→ `requestSingleInstanceLock` 按 **user-data 目录**分实例组：多形态工具（便携 data\ vs %APPDATA%）信使天然各唤各的，安装版信使与用户日常实例同组=外部感知语义自洽；无 CLI 则"打开窗口"=无参二次拉起。CLI 应用 → 有无 `-startup`/`-quit`（everything）或纯信使语义（无 CLI 则"打开窗口"=无参拉起）
4. **探测方式**：优先命名互斥体（`OpenMutex(SYNCHRONIZE)` 最小权限）；找不到互斥体再看顶层窗口/进程名。**互斥体可能有安装形态门控**（VS Code 的 `"vscode"` 仅 Inno 安装版创建，便携版持锁探测永远查无此人）——多形态工具按形态分治探测通道（安装版 mutex / 便携版进程镜像路径前缀匹配托管目录）
5. **数据落盘**：配置跟 exe 走（everything portable）还是固定用户目录（ccswitch `~/.cc-switch`）→ 决定 ImportLocal 搬什么（整套 vs 单 exe）
6. **关窗/退出语义**：`on_window_event(CloseRequested)` 是 exit 还是 hide 驻托盘 → 决定 Quit 走 WM_CLOSE/信号 + 宽限 + 强杀兜底的结构
7. **托盘**：无条件创建还是可配置。用户问"能藏托盘吗"：上游无开关就如实说"要 fork"；**托盘有功能价值时（如供应商切换菜单）明确建议不藏**——别默认隐藏
8. **提权 manifest**：`grep -r requestedExecutionLevel`（.NET 的 `app.manifest`、Go/rsrc 清单、electron-builder 的 `requestedExecutionLevel`、C 项目的 `*.manifest`）。命中 `requireAdministrator` 给三重契约（TROUBLESHOOTING #17 实证，BCU 集成漏查导致真机裸 errno 740 复发）：① 未提权 Hanxi `CreateProcess` 被 **ERROR_ELEVATION_REQUIRED(740)** 直拒——Go exec 不像 ShellExecute `runas` 会代弹 UAC，Start **与信使唤窗等全部 exec 路径**都要 `elevateHint` 特判文案指向"以管理员身份重启 Hanxi"，且**文案必须含"管理员"关键词**（前端 `ElevateRestart` 一键提权重启组件据此匹配显隐，跨层契约由三模块 `TestElevateHint` 钉死；一键通道本身已产品化，见 TROUBLESHOOTING #17 与 `platform/windows/elevate_windows.go`）；② UIPI：中完整性 Hanxi 对 elevated 实例的 WM_CLOSE/SetForegroundWindow 被静默拦截，托管仅在 Hanxi 同级或更高权限时可靠；③ 前端 stopped 引导行如实预告提权要求，别让用户首撞才看懂

## 决策点：集成范围

侦查后若存在"内嵌核心功能"的分叉（如 everything 的内嵌搜索），用 AskUserQuestion 给两选项并**把纯托管设为 Recommended**（上游界面已完整，内嵌重做性价比低；内嵌还会把上游的配置数据纳入 Hanxi 直接改写，风险高）。用户拍板后进入模板实施；决策记录写进模块 package 注释。

## 阶段 1-2：Go 子包（照 litemonitor/airsnip 现行内核委托形态；ccswitch 是同族更早的样本）

**现行形态（2026-11-03 按树校正，勿再照旧文案写）**：下载→校验→安全解包→版本树落位的主流程已上收 `packages/go/artifact`（`Fetch`/`UnpackZipContext`/`Tree.StageDir+Commit+Remove`），进程治理（spawn→JobObject→就绪→wait 四分类→Stop 宽限）已上收 `packages/go/supervisor`；模块侧只留领域知识（选包判据、镜像 URL 模板、目录形状、布局锚点、导入语义、Win32 通道）。CI runner 是 `windows-latest`，全家族**不写 `_other.go` 桩**（litemonitor/airsnip 均如此），`go vet ./...` 与单测按 Windows 口径写。

```
internal/modules/<mod>/
├── models.go            # ControlOutcome / QuitOutcome（多形态可加组合 Status）
├── module.go            # ID/Info/Nav/SetGate/Services/OnInit/OnDestroy/IsInitialized/TrayCommands/CheckUpdate
├── service.go           # 版本八件套 + GetStatus + 控制编排 + 外部感知 watcher（每 RPC 首行过 holder.Enter()）
├── store.go             # activeVersion + followOnExit（jsonstore 原子写/损坏容忍）
├── version/
│   ├── models.go        # <X>Release / <X>VersionInfo / DownloadProgress（词表 downloading/extract/done/error）
│   ├── remote.go        # GitHub API 列表：plainSemverTag 过滤 + findPortableAsset（后缀排除 arm64/msi/sig/裸exe/latest.json）+ 10min cache + hostfeed.Notes 展示矩阵
│   ├── downloader.go    # 只剩 assetMirrors（4 地址：直连 + 三镜像；回退纪律已在内核 Fetch）
│   ├── manager.go       # NewManager/OpenTree/ListRemote/ListInstalled/DownloadContext/Remove/ResolveExe/ImportLocal；四层完整性=官方摘要+字节上限+内核解包闸+布局自检
│   ├── check_update.go  # extapi.UpdateChecker shim
│   └── *_test.go        # 失败注入用回环 httptest 真驱动内核链（无外网）
└── instance/
    ├── prober.go        # 包注释=领域事实档案；探测接口 FindPIDs/IsRunning/WaitForReady（+ 模块特有窗口位）
    ├── probe_windows.go # //go:build windows；互斥体或进程快照枚举
    ├── close_windows.go # WM_CLOSE / elevateHint（740→含「管理员」文案）/ 私有消息通道与窗对账
    ├── config.go        # 启动前接管上游自更新/托盘等行为（纯函数 + 落盘两段，便于单测）
    ├── instance.go      # Engine：组合内核 + 状态词表/快照映射 + holdSpawnFail 暂扣 + 接缝字段（postCmd/waitWin/focusWin…）
    ├── instance_test.go # fakeProbe/fakeJobAPI/fake 通道注入 + cmd.exe/ping.exe 冒烟 + waitState 轮询
    └── realhost_windows_test.go # env 门控默认 skip 的真机夹具（见阶段 5）
```

**Engine 必须保持的硬约定**（内核化后=模块 wrapper 的纪律，照 litemonitor/airsnip 现行件抄，勿照旧文案自写）：
- Start：JobObject 绑定 + `opts.Detached`（"不随 Hanxi 关闭"开关，全托管统一**默认 Detached** → 内核 `Spec.DetachFromJob` 做 `SetAllowKillOnClose(false)`，Hanxi 退出/崩溃不连带）；工作目录锁 exe 目录由内核默认保证
- 信使进程（OpenWindow）Start+Release **不 Wait、不进 Job**——代码注释里写明"勿好心改成 Wait"（markeron 先例，改了就拖慢冷启动）；**先按阶段 0 第 3 条实证信使是否真被接收**，死代码唤窗上游（airsnip）不能照抄本条
- wait() 四分类已进内核且顺序不可换：stopping → 外部接管 → exit0 → failed；模块侧只做**词表与快照形状映射**（内核 `stopping` 折并入 `running`、"已手动停止"折回空文案、异常退出文案改写成"〈工具名〉 异常退出（退出码 N）+ 依赖指引"），措辞与内核文案正则耦合处要在注释里点名"文案变更即退化透传，不崩"
- 退出宽限期用**包级变量**（`closeGracePeriod`/`windowReadyWait`/`settingsFocusWait`），单测压缩、生产恢复
- 提权件（requireAdministrator）：`holdSpawnFail` 暂扣 spawn 失败广播 → Start 返回后 `elevateHint` 改写再补播（litemonitor 最完整）；RPC 错误通道另经导出的 `ElevateHint` 同一判据（heukms 双通道形态）
- **接管类"启动前干预"紧邻 spawn 写在 wrapper 里**（内核不做 PreStart），失败不阻断启动=回到上游默认行为；这类操作用例**必须** `t.Setenv("APPDATA"/"HOME", t.TempDir())`，否则写进机主真实目录（#144）

## 阶段 3：模块骨架

- `store.go`（activeVersion 原子写/损坏容忍 + `followOnExit *bool` 默认 false——"随 Hanxi 一起关闭"开关，service 启动传 `Detached: !store.GetFollowOnExit()`、Shutdown 里 `GetFollowOnExit()` 为 true 才 Stop）、`models.go`（ControlOutcome/QuitOutcome；多形态可加组合 Status 结构一次拉全）、`module.go`（Nav 照 ccswitch：Icon/Order/SectionExt，Order 先 grep 全部模块取空位）、`service.go`（版本八件套 + GetStatus + OpenWindow/Quit + openXXX 控制编排 + 外部感知 watcher；多形态 = 每形态一台 Engine + 探测通道分治，如 vscode 便携/安装双引擎）
- **安装器通道的确认闸**：安装器会强关运行实例时（Inno `CloseApplications=force` / NSIS oneClick 卸载重装），RPC 必须带 `confirm bool` 参数：命中运行实例且未确认 → 返回 `ControlOutcome{Action:"confirm-required"}` 前端弹全局 confirm 后带 `confirm=true` 重入——无此参数用户确认后会被同一道闸再拦成死锁（vscode DownloadVersion 实证）
- `internal/app/app.go`：两处事件 `RegisterEvent[<version>.DownloadProgress]("<mod>:version-download")`、`RegisterEvent[<instance>.Snapshot]("<mod>:instance-state")`（**值类型，Emit 也传值**——指针/值不匹配会被 Wails 静默丢弃，TROUBLESHOOTING #9）+ `modulesToRegister` 追加
- **托盘候选必须显式接线**（2026-10-12 补，Termora/WindTerm 缺席事故，TROUBLESHOOTING #107）：`module.go` 加 `TrayCommands() []extapi.TrayCommand`，照 `ccswitch/module.go:79-88` 七行模板，`Run` **复用页面「启动」按钮同一个 `OpenWindow()` 入口**（勿新造 RPC、勿在闭包里另起一套控制编排）。这是托盘候选的唯一供给源——`registry.ListTrayCommands()` 逐模块查 `Enabled ∧ IsInstalled ∧ 类型断言 TrayCommandsProvider`，**没实现就是结构性缺席，全链路零报错**（契约是"可选"的，Go 不会因缺实现编译失败；catalog 只是源码镜像、不参与运行期供选），症状仅仅是"设置里少一项"，肉眼与构建都发现不了。加完必跑 `go run ./scripts/cataloggen` 再生（`tray-commands` 能力与 `EntryTray` 入口由源码证据反向推导，`TestCatalogEntrypointsGrounded` 要求实现↔catalog 互为充要，漏再生必红）。刻意不供的（headless 服务型、仅版本+下载型、无主窗的 MSIX 型、多实例无从指代单命令型）必须在包注释与 catalog 描述里留**显式负裁决**，否则下一个人当遗漏再补一遍。
- 需要"打开安装目录"就加模块自有 `OpenDir(dir)` RPC（explorer.exe 目录语义），**绝不复用 AppService.OpenPath 传文件路径**（markeron 事故：explorer.exe 收 exe 会执行它）
- PE 版本读取复用 `internal/platform/versioninfo.FileVersion`（共享包，勿再复制）

## 阶段 4：前端

- `<X>View.vue` 照 `CCSwitchView.vue`/`EverythingView.vue` 双 Tab：控制台（状态灯+版本/PID/时长+按钮组+条件提示条）+ 版本管理（已装卡片+远程表+导入本地）；多形态 = 控制台两行 control-bar + 远程表形态切换（vscode 先例）
- 前端挂载单一来源：`frontend/src/constants/navigation.ts` 的 `ROUTES`（route→组件+moduleId）与 `MODULE_PRESENTATION`（首页图标）两张表各加一行即可（App.vue 手写三挂载已收编退役，勿再照旧文案找）；**`src/constants/__tests__/navigation.spec.ts` 有路由数量回归锁**（"登记了全部 N 条路由"/门禁集合长度），新增模块必须同步改数
- **bindings 必须 `task common:generate:bindings`**（裸 `wails3 generate bindings` 会清空输出目录）；后端改了 RPC 签名（哪怕加一个参数）也要重生成再跑前端构建
- CSS：所有自定义类名带模块前缀（`cc-`/`ev-`）——App.vue 全局有 `.status-dot`（7px）会压扁表格圆点（markeron 垂直字体事故）
- 验证：`npm run build`（vue-tsc 零报错）+ `task frontend:test`（导航锁在这一步兜底）

## 阶段 5：真机联调（用户参与，清单化验收）

下载真实版本 → 启动/唤窗/退出 → 状态灯六态 → 外部实例感知（用户自行启动）→ 导入本地 → 联动开关验收（默认不随 Hanxi 关闭：退 Hanxi 工具存活；开启"随 Hanxi 一起关闭"并重启工具后：连带退出）。**离线难验证项**（编码/落盘位置/真实退出行为）逐条列给用户勾验。

**先把真机夹具自己跑一轮再交给人工**（airsnip 起的新规矩）：控制面走 Win32 消息/窗口探测的模块，源码推断全对也可能运行时失效（UIPI、时序、消息过滤）。做法 = 一个 **env 门控、默认 skip** 的 `instance/realhost_windows_test.go`，用真 exe 跑"启动 → 窗/进程外部事实断言 → 退出"，跑法与前提（须提权会话）写进文件头，人工验收项则进 `docs/plans/PLAN_REMAINING_WORK.md` P1（heukms/airsnip 先例）。夹具首轮就抓到过"进程在场≠窗已建"的瞬时判定竞态（#143），这条价值已实证。
**两条相关红线**：① 用例会写真实用户目录的一律先 `t.Setenv("APPDATA"/"HOME", t.TempDir())`（#144：启动前接管类逻辑经 `Engine.Start` 就会写真实 %APPDATA%）；② 消息投递类"送达"必须对账外部事实（窗/文件/端口），不得拿 `PostMessage` 返回值当证据。

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
| 官方 CDN 单源（GitHub 无二进制）+ 哈希仅最新版 + 双形态两台 Engine + Inno 确认闸 | vscode | version/remote.go fetchRemoteList / registry_windows.go / instance/probe_windows.go 双探针 / service.go DownloadVersion confirm（TROUBLESHOOTING #31） |
| 内嵌 CLI 搜索/额外组件 | everything | search/es.go + EnsureTool |
| ini 改写（托盘隐藏等） | everything | instance/config.go ensureHiddenTray |
| 空闲自动退出 | everything/ccswitch | service.go idleCheck/touch/shouldIdleQuit |
| rust-portable 自解压单 exe（外层秒退，锚点=提取目录父 PID 闭包进程树） | rustdesk / subnetdesk | instance/instance.go supervise + probe_windows.go 路径判别（TROUBLESHOOTING #24） |
| 根目录官方启动器接力自退（真身在子目录，启动入口直指内层） | bcu | version/manager.go innerExeRel/ResolveExe（TROUBLESHOOTING #24 第二变体） |
| 进程名探测 + EnumWindows 按 PID 唤窗（无互斥体可用） | litemonitor / recordly / guoheview | instance/probe_windows.go + close_windows.go |
| 单文件 exe 无 zip 下载（digest+字节数+MZ 魔数三重校验） | rustdesk / subnetdesk | version/manager.go Download/placeFile |
| 被控/服务型常驻——禁用空闲退出、退出即断会话的诚实文案 | rustdesk / subnetdesk | service.go 包注释 + OpenWindow/Quit 编排 |
| requireAdministrator 清单 740 三重预告（elevateHint 特判 + 前端一键提权重启 ElevateRestart 组件） | rufus / litemonitor / bcu | instance/close_windows.go + close_other.go 桩；通道在 platform/windows/elevate_windows.go（TROUBLESHOOTING #17） |
| 外部安装器（NSIS/Inno/MSI）静默安装与退出码分族翻译 | recordly（NSIS）/ vscode（Inno）/ rustdesk（MSI） | version/nsis_windows.go / inno_windows.go decodeInstallerExit / manager.go installExitError |
| **该不该挂托盘/轮盘候选**（2026-10-13 覆盖面收口定案，别再靠记忆判断） | 常驻可唤窗型挂 `launch`（ccswitch/dbx/gonavi 模板）；高频开关型挂 `toggle`（markeron「开启/停止屏幕标注」、msgboard「挂出/撤下留言牌」）；**5 款有终裁不接**：piik（headless 服务型）、douzy（仅版本+下载不接管进程）、nanazip（MSIX 无主窗）、frpc（多实例无单命令语义）、rammap（一次性观察工具） | 各 `module.go` 的 `TrayCommands()`；2026-11-03 复核现 **32 入口 = 27 托管 + 5 非托管**（clipboard/eartrumpet/msgboard/ocr/webapp），33 款托管 27 供候选 + 6 负裁（piik/douzy/nanazip/frpc/rammap + **heukms**——它有窗但按 2026-10-21 负裁决不接），逐名账见 TROUBLESHOOTING #107。一个模块供几条命令都可以（`TrayCommand.ID` 只在本模块内唯一，宿主键形如 `airsnip/shot`），但 catalog 的 `tray-commands` 能力是**模块级布尔**——供两条也只按 1 个入口入账。**不接的必须在包注释留显式负裁决**，否则会被下一个人当遗漏补一遍 |
| **无主窗口 / 无 CLI 的上游，控制面怎么接** | airsnip（先读接收端证伪信使，再走上游内部菜单 ID；截图/唤窗以"对应顶层窗出现"为送达判据；退出走等价 WM_CLOSE；贴图无独立窗才维持尽力投递） | `instance/commands_windows.go`（ID/类名/`waitForWindowByClass`/`WindowPresentByClass`）+ `instance.go` 的接缝字段 + `realhost_windows_test.go`（TROUBLESHOOTING #143） |
| **上游 exe 无 VERSIONINFO / 配置在 %APPDATA% 全局单文件** | airsnip（已装版本只认目录令牌、导入版本"名字嗅探→内容哈希"两级；启动前就地改写单键关上游自更新，非首启播种） | `version/manager.go` 头注与 `versionFromImportName`、`instance/config.go` 的 `disableAutoUpdate` 纯函数（#143/#144） |
| **新增模块要同步的数字闸门（一处不改就红）** | 五处：`scripts/fixture/composition_contract.json`（modules+events+counts 两个数）、`internal/app/catalog_contract_test.go` 与 `catalog_service_test.go` 的目录数硬闸、`frontend/src/constants/__tests__/navigation.spec.ts` 的 ROUTES 长度、`frontend/src/constants/__tests__/contract-enum.spec.ts` 的 fixture 项数；再跑 `go run ./scripts/cataloggen` + 人工合并 `scripts/fixture/doc_facts.json` 的 facts 段（`-emit` 只打印不写盘）并按历次先例**收窗** forbidden 正则的旧值区间，最后 `go test ./scripts/docsfacts` | 见 `docs/FACTS.md` §一/§四 与本轮 airsnip 入库批 |

## 红线提醒

- 上游闭源/无哈希/资产不稳定的项目，先如实告诉用户风险再开工
- 用户没要求就别 fork 上游；藏托盘/加功能等 fork 类需求明确报价后等拍板
- 全程中文汇报、结论先行；每个阶段验证通过才进下一阶段