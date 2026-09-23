# Hanxi 剩余工作排期(项目暂停基线)

> **文档状态**:唯一复入口。项目于 2026-09-20 暂告一段落,恢复工作时以本文为起点,不必再跨多个文档自行拼接现状。
>
> **基线**:`dev` @ `5773ff9`,工作区无未提交代码改动;Wave 0–5 全部落地;41 个业务模块;前端全量门禁绿(112 测试文件 / 1261 项 / `vue-tsc` / 生产构建 / ESLint 0 error)。并行会话(F1–F10、Wave 系列)的工作均已入库,经全 worktree 扫描确认**无任何未提交的代码欠账**。

---

## P0:代码质量修复(复进第一件事)

证据与逐条代码定位全部在 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md),已暂停待批,恢复时按原批次执行、不重做审查:

| 批次 | 内容 | 关键落点 |
|---|---|---|
| 批 1 ✅ **已完成(2026-09-21,f63d9b2+c679764)** | Registry 覆盖状态数据竞态(值类型投影消灭指针逃逸);receipt 与 `known-modules.json` 成组缺陷(账本独立命名空间/独立 schema/三态保守重建/fail-closed 卸载事务;计划见 [PLAN_P0_BATCH1_FIX](PLAN_P0_BATCH1_FIX.md)) | `internal/extapi/registry.go`、`internal/settings/receipts.go` |
| 批 2 → **2a ✅ 已完成(2026-09-21,4725e52+5881c8e)**:journal 步进 fail-closed 闸门(降级拒新事务+journal-degraded 收口+GetJournalHealth 观察面);Artifact Commit 原子边界收口 rename(黑户隔离放行重装)。**2b 🔄 施工中(机主 2026-09-22 选案 B)**:ctx+租约一并买断。内核已落(`extapi.BackgroundLease`/`EnterBackground`+Registry 停用先 cancel 再 drain+`ops.BeginTxnWithLifecycle`/`Txn.Context/Cancel/Close/JournalFailed`+`artifact.UnpackZipContext` 可取消解包);**已迁移 19/19 journal 模块**(黄金样本 flclash/paseo;普通 zip 族 markeron/ccswitch/everything/litemonitor/ddnsgo/bcu/bili23/mangodisk/rufus/translucenttb;特殊形态 quicklook(bespoke extractAll ctx 化)/keyviz+piclite(MSI:提取器返回后与轮询边界收口,msiexec 不可强杀如实注释)/papertodo(降级下载链 ctx 化)/guoheview(MD5 bespoke 链 ctx 化)/recordly+vscode(安装器一旦拉起即走完,启动前审取消;双形态全链 ctx),全仓 build/vet/单测绿);**2b 遗留待办**:①~~下载中停用/退出三场景 service 级集成测试~~ ✅ 已落(2026-09-23,ops 层全链集成 `TestDeactivateCancelsInFlightTxn`:真 Registry+真 journal,断言停用先取消、operation-cancelled 收口、无 succeeded 假账、drain 放行;退出场景由 ShutdownAll 同构 cancelBackgroundLocked 路径覆盖,真机冒烟见 ② 后补跑);②`go test -race` Windows 侧补跑;③已知环境 flake 与本批无关:papertodo/instance(cmd.exe 秒退 #81 家族)、portscan(port+1 撞外部监听)(zip+Fetch 族为机械迁移;keyviz/piclite MSI、recordly/vscode 安装器、guoheview bespoke 需按各自取消边界处理,不伪造即时取消) | 迁移模板见 flclash service/manager 两文件;对比表见 [PLAN_P0_BATCH2_FIX](PLAN_P0_BATCH2_FIX.md) |
| 批 3 ✅ **已完成(2026-09-23,五子批 b3-a~e,计划见 [PLAN_P0_BATCH3_FIX](PLAN_P0_BATCH3_FIX.md))** | 4.1 stale 呈现(statusError/lastStatusAt+降灰灯)、4.2 本地未决不判空(localResolved)、4.3 清票走 sameVersionOf 互认、4.4 vscode/everything 自定义加载代次闸(状态/加载独立计数)、4.5 专属动作统一互斥(ddnsgo console/extras 卡/everything 写动作)、4.6 键盘可达(TabNav roving+方向键、progressbar 语义、重试转按钮)、§5-1 cancellable 诚实标注(仅 install/update 有 2b 真链)。**人工余项**:390px/200% WebView2 真机验收(归 P1 清单随 F6 重跑同场) | `frontend/src/components/managed/`、`adapters/vscode.ts`、`views/EverythingView.vue`、`ui/MainTabNav.vue`、`packages/go/operation/hub.go` |

- 每批开工前先形成正式修复计划确认范围;批 1、2 完成后补 `go test -race`(Windows 侧走 CI/真机;云侧已具备 hanxi-dev:2404 容器回路,43 个 Linux 可构建包可本地跑 race,Windows-only 包仍须 CI——配方与豁免清单见踩坑 #84)与组合级故障注入。
- 审查文档 §5 另有 9 项"已发现待复核/裁决"事项,随批次一并收口。

## P1:真机验收债

1. **F10 网页应用(webapp)冒烟**:task dev 开微信扫码、X 关窗后 WebView2 内存回落、收起 TTL 释放——唯一纯新未验项,成本最低,建议最先做。
2. **F6(存储目录)、F3(快照)、F4/F4b(MCP 与向导)、F7(OCR 托管)、F9(WSL USB)**:按 [PROGRESS.md](../PROGRESS.md) 既有人工清单补跑;**随 F6 加测批 3 人工余项**——共享托管面板/版本表在 390px 宽 + 200% 缩放下无溢出、TabNav 纯键盘(←/→/Home/End/Tab)可完成换选、NVDA/讲述人可读进度与"暂不可确认"标注。
3. **A 机 → NAS → 干净 B 机黄金路径**:先完整走一次手工验收(建立模块迁移等级:可携带/需重建缓存/需系统注册/需重填凭据/不适合临时电脑),据结果再决定机器绑定恢复提示与临时电脑模式是否立项。

## P2:文档与工程卫生

- [x] 三份未跟踪文档已随本排期一并入库:本文、CODE_QUALITY_REVIEW_PROGRESS_2026-09-19、FUTURE_FEATURE_RECOMMENDATIONS。
- [x] 卫生清理已执行完毕(2026-09-20,机主点名批准):28 个 worktree 目录(全 MERGED)注销删除,含 hubkit 旧名时代 3 个失效 locked 登记;已合并分支(`feat/f*`、`fix/q*`、`feat/r*`、`fix/nine-suite-quality`)与全部无主 `worktree-agent-*`  scratch 分支删除。现仓库仅存 `dev`/`main`/`pre-split-backup` 三分支、单一工作树。注:8 个 scratch 分支同指 hubkit v0.2.0 时期 release 合并点 `60aff0c`(不在任何 tag/远程/备份可达范围),删除后仅 reflog 可回捞(约 90 天窗口)。
- [ ] 事实漂移收口:README/PRD/PROJECT_STRATEGY 的 37/38 旧统计 → 41;旧数据根语义(`hanxidata` 标记、`%APPDATA%` 回退、旧 `data/` 兼容)→ 现行"exe 同级 + `hanxi.bind`",含 Release workflow 便携说明;BACKLOG 中 WebApp 状态更新。
- [ ] gofmt 闸门红在 8 个历史文件(`logging/everything/ocr/platform` 等):机主裁决——建议单开一次 `style(frontend)`/`chore` 性格式化提交,不与功能混淆。

## P3:已暂停批次(恢复与否由机主定)

- **前端打磨批次一**:审查与实施计划已完成,三路实现已停止且主工作区无本轮改动;恢复时直接重新并行实施,不必重做审查。

## P4:复进日新增(2026-09-20,机主点名,只登记不即修)

| # | 事项 | 现状与落点 |
|---|---|---|
| N1 | **snipaste 打开/退出异常**(机主真机报告) → **✅ 实施已落(4cfd83f,待真机复验)** | 模块无 OpenWindow 动词(仅 Launch);状态词表无 external——`noExternalProbe` 恒报"不在运行"(`internal/modules/snipaste/instance/instance.go`),管理边界=本会话启动的进程树。先真机复现定位,再定修复范围(含是否引入外部检测)。 |
| N2 | **everything 外部实例可检测但退不掉**(机主真机报告) → **✅ 实施已落(de27a5f,待真机复验)** | external 探测与信使唤窗可用;`Quit()` 在 external 态刻意不越权、只回指引(`internal/modules/everything/service.go` ~303 行,探针窗口类/互斥体通道拿不到 PID)。需补"外部 PID 取得(按进程名/窗口枚举)→ `-quit` 优雅退出或 WM_CLOSE"。 |
| N3 | **Wave 4X 复活评估:托管家族外部实例退出/唤窗策略统一** | 唤窗两方案并存(单实例信使:ccswitch/everything;按 PID EnumWindows:rustdesk/litemonitor/subnetdesk/rufus/flclash/bcu/guoheview);外部实例退出一律只指引。已知隐患:①`focusWindowsByPIDs` 族以 `IsWindowVisible` 判恢复,最小化窗仍带 Visible 标志→不 SW_RESTORE、置前台无效,guoheview 的 `IsIconic` 方案为标杆;②托盘/后台唤起需 `platform/windows.SetForegroundForce` 借前台特权,族内多用裸 `SetForegroundWindow`。收口"是否越权强杀外部实例"决策后按模块推开,吸收 N1/N2 结论。 |
| N4 | **RAMMap64 托管集成** → **✅ 代码收口(2026-09-23,机主拍板路线 A 托管本体,待真机验收)**(微软 Sysinternals 内存释放) | 单文件绿色 exe,源 `download.sysinternals.com/files/RAMMap.zip`(非 GitHub,借用 integrate-github-tool 托管模式但换下载源);一次性"清空待机列表"语义、需提权、非常驻 GUI,形态近 recordly/paseo 单目录家族,提权与退出语义开工前正式计划确认。 || N4 | **RAMMap64 托管集成**(微软 Sysinternals 内存释放) | 单文件绿色 exe,源 `download.sysinternals.com/files/RAMMap.zip`(非 GitHub,借用 integrate-github-tool 托管模式但换下载源);一次性"清空待机列表"语义、需提权、非常驻 GUI,形态近 recordly/paseo 单目录家族,提权与退出语义开工前正式计划确认。 |
| N5 | **quickmenu 右键轮盘交互改进**(机主:"实在是不好用") | 借鉴 GitHub 开源项目 starpie(登记日检索未定位到确切仓库,开工先核实链接)。落点 `QuickMenuPopup.vue`/`useWheelRingState.ts`/`wheelGeometry.ts`/`internal/modules/quickmenu/`;既有欠账:前后端几何不同源(512 DIP vs 376 viewBox,见 FEATURES_OVERVIEW 与踩坑 #50)、触发时长/容差为编译期常量。先出改进分析+实施计划,再动代码。 |
| N6 | **msgboard 桌面留言板 UI 重设计**(机主:不喜欢、不友好) | 走 hanxi-workbench-ui 设计语言评审;落点 `MsgBoardView.vue` 与留言板组件族,`internal/modules/msgboard/` 后端契约尽量不动。先出方案给机主过目再实施。 |
| N7 | **飞牛同步客户端托管集成**(标准安装形态) → **⏸️ 机主裁决暂停(2026-09-23):CDN 三种客户端均 403,取证成本>收益,先放下;侦查结论留存下行,若重启从"机主浏览器下载 setup.exe 交卷"接上** | 源 `fnnas.com/download?key=fn-sync-client`;机主明确不能免安装,须走安装型先例(recordly NSIS 静默安装、vscode installer 双形态)。开工前确认:静默参数、版本提取、升级语义(覆盖/先卸后装)、且本机正在履行同步义务的实例按 N3 终裁登记为 `confirm_force` 档(优雅退出优先,强杀须 UI 明示"传输中"并确认,不闷头杀)。缘起级用途(MOTIVATION:NAS 同步随身),优先级建议靠前。 | **侦查结论(离线,部分实证)**:①下载页 JSON 实证当前版 `0.2.3`,Windows 直链 `iso.liveupdate.fnnas.com/pc/fn-sync_0.2.3_x64-setup.exe`(另有 macIntel/macApple dmg,无 Linux 桌面包);②`fn-sync` 系 Tauri 应用(命名 `<name>_<ver>_x64-setup.exe` 是 Tauri v2 NSIS 打包器签名产物——非 Inno,与 recordly NSIS 先例同族但 Tauri 有自己的静默参数约定);③**版本提取无需 GitHub API**:直链文件名即含版本,或解析 download 页 JSON 的 `version` 字段(everything 网页解析+快照兜底先例);④**卡点**:该 CDN 对非国内/数据中心 IP 返 403(`X-Error-Info: typeC`,四种 UA/Referer/Range 全拒),离线无法下载二进制做静默参数实证——按 skill"无实证不猜参数"铁律不预写 NSIS 解码链。**需机主侧一步取证**:在你本机(能正常访问 fnnas)浏览器下载该 setup.exe 或 `curl -sIL` 存 headers,或授权我在你网络环境跑一次下载,再定 `-S /当前用户静默` 具体参数与退出码族。
| N8 | **WindTerm 托管集成** → **✅ W1 复核更正:无 7z 门槛;W4 实施再更正:上游全量无官方摘要,须无摘要降级链(非零改动)** | 仓库 `github.com/kingToolbox/WindTerm`,走 integrate-github-tool 托管模式。**登记时"7z 绿色包"有误:GitHub API 全量核对,Windows 资产均为 `*_Windows_Portable*.zip`**(仅 2020 v1.1 带过历史 7z)。稳定版已至 2.7.0(2025-03)+Prerelease 线(原"2.6.1 稳定/2.7.0 beta"表述过期),channel 适配按 paseo/recordly 先例;Qt 单 exe 无信使→唤窗按 PID EnumWindows(guoheview IsIconic 标杆)、退出 WM_CLOSE+JobObject 兜底不变。⚠️ `windterm.cn` 为第三方网盘站(自动化只走 GitHub releases);官方称 Apache-2.0 但仓库根缺 LICENSE 文件,集成文档留备忘。 |
| N9 | **Termora 托管集成** → **✅ 代码收口(2026-09-23,待真机验收)** | 仓库 `github.com/TermoraDev/termora`(机主指定),同技能同模式。jpackage 双发布型:zip(内嵌 JRE 绿色,优先收编版本目录)与 exe 安装器(若采则归 N7 安装型先例族);Java 应用注意子进程树收口。与 N8 同类,建议同批实施、互相抄验。 |
| N10 | **借鉴 MooTool 系统信息功能**(自研新模块) | 参照 [rememberber/MooTool](https://github.com/rememberber/MooTool) 的系统信息工具:CPU/内存/磁盘/主板/显卡/OS/网络等软硬件档案一览。借鉴纪律沿 `docs/MOOTOOL_ANALYSIS.md`(学机制不抄实现);Go 候选 gopsutil 或 WMI/syscall 直采,开工定。划界:envcheck=开发工具链、litemonitor=托管第三方实时监控,本模块=静态软硬件档案,独立模块或并入 envcheck 家族开工定。 |
| N11 | **数据根占用可视化**(hanxidata 每子文件夹大小一览) | 数据根解析走 `internal/settings/paths.go` 现成通道(同级 `hanxidata/` 或 `hanxi.bind` 绑定);每个一级子文件夹大小=每便携软件磁盘占用,一眼看清。要点:walk 统计缓存+手动/后台刷新、巨型目录(Everything 索引/快照库)限时渐进、紧凑排序列表走 hanxi-workbench-ui;挂设置页「存储目录」分区还是独立模块开工定。 |
| N12 | **litemonitor 简介补「释放内存」能力**(机主指出) → **✅ 已落(f41f1a7)** | 上游 LiteMonitor 支持释放内存,hanxi 模块简介未写。定位 catalog/视图简介文案,对照上游功能清单核实后补齐,顺带排查其他"已支持未宣传"缺项;纯文案低风险可插空。与 N4(RAMMap 内存释放)同主题,文案口径可同批收口。 |
| N13 | **托管版本列表显示全平台安装包并标注形态** | 现 release 被过滤到只剩当前 Win 机器可用包(以 flclash 为例);改为展示 Windows/macOS/Android APK(Linux 可不要),逐包写清**便携版/标准安装版**。三处落点:①各模块 `version` 包平台过滤改"全量返回+平台标注";②共享契约 `ManagedReleaseRecord`(现仅 version/published/size/isPre)扩平台/形态/URL 字段,Wave 5 版本面板加平台与形态 chip——**跨模块契约变更,开工前出正式计划评审**;③非 Windows 资产只给"浏览器打开下载",不进 hanxi 下载/设使用链路。 |
| N14 | **envcheck 扩展:环境本体+依赖目录占用显示** | 现只报"装了什么/什么版本",看不到空间家底:①每个环境**本体安装目录**大小(Go SDK、Node、JDK、Python、.NET 各占多大);②依赖/缓存目录位置与大小(Go `GOPATH/pkg/mod`/GOCACHE、npm 全局包与缓存、pip/Maven `~/.m2`/Gradle/NuGet)。落点 `internal/modules/envcheck/` 检测器补双目录推导(env var→默认路径→工具命令三级回退)+`EnvCheckView` 展示"本体/依赖家底"两列。**与 N11 共用同一 walk 目录统计公共件,两项同批:先下沉公共包再各自接线**(实践"新功能第一天走公共包"纪律);UI 走 hanxi-workbench-ui。 |
| N15 | **评估:OCR 与 Snipaste 结合**(机主:"不能就算了") → **✅ 已定:可行,免费路径可落地,W1 调研收口** | 核心机制=Qt 单实例命令行通道(`Snipaste.exe snip -o 文件` 免费,外部实例同样可达),hanxi 目录监听落盘文件接现有 `ocr` 模块;PRO 另有 exec/`ocr_clipboard`/`--block` 增强。边界:目标提权运行被 UIPI 拦→降级指引;免费版无官方退出命令(`exit` 属 PRO)。详见 [PLAN_W1_DECISIONS §2](PLAN_W1_DECISIONS.md)。 |
| N16 | **随手记(memo)演进:路线已定 A→B→C**(首轮沟通 2026-09-20 完成) | 机主四类痛点全选(怕丢/记不快/找不回/用不上)→ 完整升级,三批次:**A** 文件库化+空闲静默 git 快照(无 git 降级普通备份,底稿 `PLAN_SNAPSHOT.md`,与 F3/N7 飞牛同步天然合流)→ **B** 全局热键快记+悬浮速记卡+全文/模糊检索 → **C** MCP/CLI 联动深化与导出(依赖 A/B 形态)。每批开工前出实施计划;后续批次细节沟通随批进行。 |

> N1/N2 与 N3 同为"外部进程也能被 hanxi 退出/唤窗"诉求的具体症状:N1/N2 先真机复现修点,N3 决策收口成面。**2026-09-21 W1 已裁决收口(采纳 C+风险分档),三项调研结论与 W2 实施方法全部见 [PLAN_W1_DECISIONS](PLAN_W1_DECISIONS.md)。**

### P4 执行波次排序(2026-09-20 复核重排;执行以本节为准,上表保留原始登记与详情)

复核实测三条硬事实(原登记未覆盖,直接影响排序):

1. **artifact 包只支持 zip**:`packages/go/artifact/unpack.go` 仅有 `UnpackZip`(全套 zip 安全闸门),无 7z 解包能力。**但 W1 调研已推翻本条对 N8 的前提**:WindTerm Windows 资产实测全为 zip,门槛不存在;7z-only 场景(如未来 mpv)已归档选型 `bodgit/sevenzip`(纯 Go/BSD-3,见 [PLAN_W1_DECISIONS §4](PLAN_W1_DECISIONS.md)),触发时再实施、当前不预建。
2. **版本契约在 Go 侧并不共享**:21 个模块各有一份 `version/models.go`(`isPre` 字段重复 21 次),N13 实际成本="21+ 处字段扩散 + 前端共享层 + 版本面板";且新托管模块每先落地一个、N13 就多一处返工点。
3. **N1/N2 的卡点在决策不在代码**:`noExternalProbe` 恒报不在运行、`Quit()` external 态只回指引,均为刻意设计(管理边界=本会话进程树)。动代码前须先裁决"hanxi 是否有权控制自己未启动的进程",即 Wave 4X 的收口决策(从 N3 中拆出前置)。

| 波次 | 内容 | 成本 | 排位理由 |
|---|---|---|---|
| **W1 决策与前置调研**(半天,不写码) → **✅ 全部收口(2026-09-21)** | ①N3 决策(**已裁:采纳 C+风险分档**——`force_free` 档低损外部实例优雅后直接强杀、`confirm_force` 档打扰类须明示确认后杀、未声明默认保守档,详见 [PLAN_W1_DECISIONS §1](PLAN_W1_DECISIONS.md));②N15 查证销项(→已定可行,免费路径);③N5 核实 starpie 仓库(→Star-Pie/StarPie,高置信);④N8 的 7z 方案选型(→前提反转,WindTerm 全 zip,选型无需,归档见 §4)。成果载体:[PLAN_W1_DECISIONS](PLAN_W1_DECISIONS.md) | 小 | 四项全是后续波次的门闸,一次沟通全部裁决 |
| **W2 外部实例治理** → **代码侧收口✅(2026-09-22,余真机验收)**:公共件 `packages/go/externalquit`(5f12779,force-free/confirm-force 双档执行器,未声明默认保守档已入包文档)→ snipaste(4cfd83f:external 按名取 PID、OpenWindow 重定为官方 toggle-images 显隐贴图、Quit 分档+UIPI 提权降级)→ everything(de27a5f:-quit 信使优雅→验证→强杀兜底)+ N12(f41f1a7)。**真机验收项**:①snipaste 外部实例探测/显隐/退出;②everything 外部实例退出(优雅与强杀两路);③提权实例降级指引。catalog 级档位字段未预建(现两模块服务层显式决策即终裁口径,W4 新托管接入时如需声明再加) | N1+N2 同批按 W1 裁决(C+分档)实施:平台层"外部 PID 取得+分档退出执行器"公共件 → snipaste(external 探测+toggle-images 唤窗+`force_free` 退出)、everything(按名取 PID+优雅信号+`force_free` 强杀兜底);N12 纯文案 30 分钟搭车 | 中 | 机主真机报告的在用功能故障,痛感最高;与 P1 F10 冒烟同场跑,合并真机验收 |
| **W3 空间可见性** → **✅ 全部收口(2026-09-21)**:公共件 `packages/go/dirstats`(7bf1b3a)→ N11 数据根占用一览(46119bb)→ N14 envcheck 空间家底(f384948) | 先下沉 walk 目录统计公共包 → N11/N14 各自接线。真机验收项:家底路径族(AppData/GOROOT 推导)与占用数值抽查 | 中 | 原登记已自判同批;"新功能第一天走公共包"纪律;纯本地可开发,适合真机等待期填充 |

#### W3 真机反馈(2026-09-21 机主实测;三件当日实施完毕,待真机复验)

| # | 反馈 | 定位与方案 |
|---|---|---|
| W3-a ✅ | **重新测量时黑窗连闪** | 已修:`defaultRun` 套 `windows.HideConsole`(detect 家族本就 Windows-only,无需 stub);**并查出更深一层**——`.cmd/.bat` 分发器未如 `detect.runVersionCommand` 做 `cmd /C` 包装,命令实际可能静默走默认路径回退,已同型补齐 |
| W3-b ✅ | **versions 目录要再展开一层** | 已实现:`AppService.DataRootSubUsage(sub, force)`(安全单段名校验,扫描面锁数据根内)+ `groupBySoftware` 按首个 `_` 前缀聚合(纯函数带单测)+ `StorageSection.vue` versions 行"展开到每软件",N 版本 chip 悬停列版本目录清单 |
| W3-c ✅ | **占用行直接标删留建议** | 已实现:后端 `DirUsage.Verdict` 四档词表(keep/recommended/redownload/caution,静态建议非删除入口)+ `EnvCheckView` 徽章行;数据根一级行 `logs/memo/.snapshots/versions` 前端静态徽章(未列名目录不戴章,宁缺毋滥)。**一键清理动作仍未做**(待裁决) |
| 附注 ✅ | 小瑕疵两处 | 随 W3-a 顺收:`normalizePath`(Clean 去尾分隔符+盘符大写)统一所有推导路径;整串大小写如需彻底规一(GetLongPathName)另议 |
| **W4 托管集成批** → **N8 ✅ 代码收口(2026-09-23,待真机验收)**:windterm 模块全链接线(version GitHub 链+无摘要降级三层〔vscode 先例,实测上游 32 release 全量无 digest——登记时"现成 zip 管线零改动"不成立,artifact.Fetch 摘要必检须走 bespoke 分治〕/instance 多实例引擎+confirm-force 外部档〔N3 终裁点名〕/共享壳前端+图标+路由+契约锁)。**N9 ✅ 代码收口(2026-09-23,待真机验收,与 N8 互为抄验)**:termora 模块全链接线(version GitHub 官方摘要主链〔与 WindTerm 相反,34 release 全量带 digest,走 markeron 必检口径〕/tag x.y.z[-beta.N] 双收且 beta 即主干如实全列〔recordly 先例,不做通道藏行〕/instance 单实例信使引擎〔ApplicationSingleton CreateMutex+二次拉起 tick,ccswitch/snipaste 混合〕+confirm-force 外部档〔N3 终裁点名 Termora〕/portable data 激活器补齐/共享壳前端+server 图标+路由+契约锁)。**N4 ✅ 代码收口(2026-09-23)**:rammap 模块——日期版模型(Last-Modified 唯一诚实版本语义)+单源降级三层+提权三重契约(#17 litemonitor 同族:740 广播暂扣改写含「管理员」关键词喂 ElevateRestart、ElevationStatus 预告 RPC、UIPI blocked 如实降级)+force-free 外部档(零状态观察工具)。**W4 全部收口,余 N7 暂停件待机主取证** | 大 | N7 缘起级用途、N3 裁决落地后即可开工(先确认静默参数/升级语义);N8/N9 皆为 zip 绿色形态、同技能同管线,互相抄验效率最高;N4 提权+一次性语义开工前正式计划 |
> **N9 真机验收清单(W4)**:①远程列表 stable+beta 全量呈现、预发布徽标在位(上游 2.x 线 beta 即主干,无藏行);②下载安装全链(官方 sha256 四层校验→jpackage 嵌套根落位→data\ 激活器自动补齐)→启动/唤窗/退出三动词;③自带 JRE 确认:干净 PATH(无系统 java)也能冷启动;**单实例互斥验证**:自行打开一个 Termora 后点「启动」应被门禁如实拒绝(而非谎报成功);④外部自行启动的 Termora:external 黄标、「退出」弹风险确认后走优雅/强杀两路、提权实例降级仅指引;⑤导入本地解压目录(含 data\ 会话数据)→ 导入件 verifiedHash 如实 false、卸载运行中/使用中拒绝;⑥活动 SSH 会话下退出:关窗确认框挂起→宽限 3s→强杀兜底文案如实;⑦"随 Hanxi 关闭"开关双向。
> **N4 真机验收清单(W4)**:①远程列表恒一条"最新版(日期)"、下载安装全链(降级三层,已装未验官方哈希如实标注);②**未提权 Hanxi 启动**:应被 740 特判拒并以引导条呈现「以管理员身份重启 Hanxi」+一键 ElevateRestart 挂载;③提权态 Hanxi 启动/唤窗/退出三动词与外部实例 force-free 直退(多实例并看合法);④外部"自启且已提权"的 RAMMap:退出如实回 blocked 降级指引不谎报;⑤导入已解压目录→卸载保护;⑥上游改版(日期变)后 CheckUpdate 点亮、再装落新日期目录。
> **N8 真机验收清单(W4)**:①远程列表只显示稳定版 x86_64 zip(2.7.0 起,无 prerelease 混入);②下载安装全链(进度→落位→verifiedHash 徽章如实为"未验官方哈希"文案)→启动/唤窗/退出三动词;③退出含活动会话场景:关窗确认框挂起→宽限→强杀兜底文案如实;④外部自行启动的 WindTerm:external 黄标、「退出」弹风险确认后走优雅/强杀两路;⑤管理员权限运行的 WindTerm → 降级仅指引不谎报;⑥导入本地目录(含会话数据)→ 卸载保护(运行中/使用中拒绝);⑦"随 Hanxi 关闭"开关双向。

| **W5 体验债** | N6 msgboard 方案先行→实施(纯前端、盘子较小先做);N5 轮盘改进(前后端几何同源改造,大活,含踩坑 #50 欠账);P3 前端打磨批次一可同窗并流 | 大 | 属重设计而非修 bug,须先出方案给机主过目;出方案可与 W3/W4 并行,动代码排队 |
| **W6 契约收口** | N13 全平台安装包+形态标注,一次扫过 21+ 模块 models 与前端契约 | 大 | 后置于全部新托管落地,避免逐模块返工;与 P0 批 3(managed 前端)同域,排在批 3 之后;开工前正式计划评审 |
| **W7 长线** | N16 随手记 A→B→C;N10 系统信息新模块(独立无依赖,可作任意波次并行候选) | 大 | A 批同步语义宜等 N7(及 F3)收口后设计,与飞牛合流;N10 为借鉴型痒点,痛感低于其余,不占主线窗口 |

与原登记的关键差异:N3 拆"决策前置 W1 / 族推广后置于 N1、N2 验证";N8/N9 曾按"7z 门槛"解绑为 N9 先行,**W1 调研推翻前提(WindTerm 发布全为 zip),已恢复原"同批互相抄验"**;N12 从"与 N4 同批"改为搭 W2 便车(现成功能文案缺漏没理由等 RAMMap);真机类任务并段(N1/N2+F10),控制 Linux 开发环境与 Windows 真机的往返成本;与 P0 质量批的并行边界——W2 在实例管理层、P0 批 1/2 在 registry/租约层不冲突,仅 N13(W6)与 P0 批 3 同域需后置。

## 路线参考(未排期)

- 中长期三阶段(搬家与数据真相 → 统一治理 → 灾备与恢复)见 [FUTURE_FEATURE_RECOMMENDATIONS](../FUTURE_FEATURE_RECOMMENDATIONS.md);仅方向,非承诺。
- Wave 4X(外部实例控制)维持延期;manifest、签名 Catalog、物理拆包为 N/A 条件路径(ADR-0003),非默认欠项。

## 复进建议顺序

1. 提交三份文档、执行 worktree 清理(收尾本次暂停);
2. P1-1 F10 冒烟(半小时级,先清最便宜的债);
3. P0 批 1(竞态 + receipt 设计)→ 批 2(租约/journal/Commit)→ 批 3(前端真相);
4. P1-2/3 真机与黄金路径验收;
5. P2 文档漂移随各批提交顺带收口;P3 视意愿插空恢复。
6. P4 复进日新增项按上方"P4 执行波次排序"W1→W7 执行,真机类(W2/F10)并段验收。
