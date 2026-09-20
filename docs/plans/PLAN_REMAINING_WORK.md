# Hanxi 剩余工作排期(项目暂停基线)

> **文档状态**:唯一复入口。项目于 2026-09-20 暂告一段落,恢复工作时以本文为起点,不必再跨多个文档自行拼接现状。
>
> **基线**:`dev` @ `5773ff9`,工作区无未提交代码改动;Wave 0–5 全部落地;41 个业务模块;前端全量门禁绿(112 测试文件 / 1261 项 / `vue-tsc` / 生产构建 / ESLint 0 error)。并行会话(F1–F10、Wave 系列)的工作均已入库,经全 worktree 扫描确认**无任何未提交的代码欠账**。

---

## P0:代码质量修复(复进第一件事)

证据与逐条代码定位全部在 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md),已暂停待批,恢复时按原批次执行、不重做审查:

| 批次 | 内容 | 关键落点 |
|---|---|---|
| 批 1 | Registry 覆盖状态数据竞态;receipt 与 `known-modules.json` 成组设计缺陷 | `internal/extapi/registry.go`、`internal/settings/receipts.go` |
| 批 2 | 异步托管下载提前释放 Acquire 租约;journal 步进失败仍执行副作用(fail-open);Artifact Commit rename 后/meta 前崩溃窗口 | `internal/modules/*/service.go`、`internal/ops/ops.go`、`packages/go/artifact/` |
| 批 3 | 前端旧快照冒充实时状态、本地/远程版本加载竞态、`already-installed` 清票未走版本互认、旧响应覆盖(generation)、多份 busy 真相、无障碍缺口 | `frontend/src/components/managed/`、`frontend/src/composables/loadManagedVersions.ts` |

- 每批开工前先形成正式修复计划确认范围;批 1、2 完成后补 `go test -race`(Windows 侧走 CI/真机;云侧已具备 hanxi-dev:2404 容器回路,43 个 Linux 可构建包可本地跑 race,Windows-only 包仍须 CI——配方与豁免清单见踩坑 #84)与组合级故障注入。
- 审查文档 §5 另有 9 项"已发现待复核/裁决"事项,随批次一并收口。

## P1:真机验收债

1. **F10 网页应用(webapp)冒烟**:task dev 开微信扫码、X 关窗后 WebView2 内存回落、收起 TTL 释放——唯一纯新未验项,成本最低,建议最先做。
2. **F6(存储目录)、F3(快照)、F4/F4b(MCP 与向导)、F7(OCR 托管)、F9(WSL USB)**:按 [PROGRESS.md](../PROGRESS.md) 既有人工清单补跑。
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
| N1 | **snipaste 打开/退出异常**(机主真机报告) | 模块无 OpenWindow 动词(仅 Launch);状态词表无 external——`noExternalProbe` 恒报"不在运行"(`internal/modules/snipaste/instance/instance.go`),管理边界=本会话启动的进程树。先真机复现定位,再定修复范围(含是否引入外部检测)。 |
| N2 | **everything 外部实例可检测但退不掉**(机主真机报告) | external 探测与信使唤窗可用;`Quit()` 在 external 态刻意不越权、只回指引(`internal/modules/everything/service.go` ~303 行,探针窗口类/互斥体通道拿不到 PID)。需补"外部 PID 取得(按进程名/窗口枚举)→ `-quit` 优雅退出或 WM_CLOSE"。 |
| N3 | **Wave 4X 复活评估:托管家族外部实例退出/唤窗策略统一** | 唤窗两方案并存(单实例信使:ccswitch/everything;按 PID EnumWindows:rustdesk/litemonitor/subnetdesk/rufus/flclash/bcu/guoheview);外部实例退出一律只指引。已知隐患:①`focusWindowsByPIDs` 族以 `IsWindowVisible` 判恢复,最小化窗仍带 Visible 标志→不 SW_RESTORE、置前台无效,guoheview 的 `IsIconic` 方案为标杆;②托盘/后台唤起需 `platform/windows.SetForegroundForce` 借前台特权,族内多用裸 `SetForegroundWindow`。收口"是否越权强杀外部实例"决策后按模块推开,吸收 N1/N2 结论。 |
| N4 | **RAMMap64 托管集成**(微软 Sysinternals 内存释放) | 单文件绿色 exe,源 `download.sysinternals.com/files/RAMMap.zip`(非 GitHub,借用 integrate-github-tool 托管模式但换下载源);一次性"清空待机列表"语义、需提权、非常驻 GUI,形态近 recordly/paseo 单目录家族,提权与退出语义开工前正式计划确认。 |
| N5 | **quickmenu 右键轮盘交互改进**(机主:"实在是不好用") | 借鉴 GitHub 开源项目 starpie(登记日检索未定位到确切仓库,开工先核实链接)。落点 `QuickMenuPopup.vue`/`useWheelRingState.ts`/`wheelGeometry.ts`/`internal/modules/quickmenu/`;既有欠账:前后端几何不同源(512 DIP vs 376 viewBox,见 FEATURES_OVERVIEW 与踩坑 #50)、触发时长/容差为编译期常量。先出改进分析+实施计划,再动代码。 |
| N6 | **msgboard 桌面留言板 UI 重设计**(机主:不喜欢、不友好) | 走 hanxi-workbench-ui 设计语言评审;落点 `MsgBoardView.vue` 与留言板组件族,`internal/modules/msgboard/` 后端契约尽量不动。先出方案给机主过目再实施。 |
| N7 | **飞牛同步客户端托管集成**(标准安装形态) | 源 `fnnas.com/download?key=fn-sync-client`;机主明确不能免安装,须走安装型先例(recordly NSIS 静默安装、vscode installer 双形态)。开工前确认:静默参数、版本提取、升级语义(覆盖/先卸后装)、且本机同步正履行义务的实例勿被强杀(与 N3 外部实例策略联动)。缘起级用途(MOTIVATION:NAS 同步随身),优先级建议靠前。 |
| N8 | **WindTerm 托管集成** | 仓库 `github.com/kingToolbox/WindTerm`,走 integrate-github-tool 托管模式。release 为 7z 绿色包(需确认 artifact 包 7z 解包支持);Qt 单 exe 无信使协议→唤窗按 PID EnumWindows(guoheview IsIconic 为标杆),退出 WM_CLOSE+JobObject 兜底;2.6.1 稳定/2.7.0 beta 双通道参考 paseo/recordly channel 适配位。 |
| N9 | **Termora 托管集成** | 仓库 `github.com/TermoraDev/termora`(机主指定),同技能同模式。jpackage 双发布型:zip(内嵌 JRE 绿色,优先收编版本目录)与 exe 安装器(若采则归 N7 安装型先例族);Java 应用注意子进程树收口。与 N8 同类,建议同批实施、互相抄验。 |
| N10 | **借鉴 MooTool 系统信息功能**(自研新模块) | 参照 [rememberber/MooTool](https://github.com/rememberber/MooTool) 的系统信息工具:CPU/内存/磁盘/主板/显卡/OS/网络等软硬件档案一览。借鉴纪律沿 `docs/MOOTOOL_ANALYSIS.md`(学机制不抄实现);Go 候选 gopsutil 或 WMI/syscall 直采,开工定。划界:envcheck=开发工具链、litemonitor=托管第三方实时监控,本模块=静态软硬件档案,独立模块或并入 envcheck 家族开工定。 |
| N11 | **数据根占用可视化**(hanxidata 每子文件夹大小一览) | 数据根解析走 `internal/settings/paths.go` 现成通道(同级 `hanxidata/` 或 `hanxi.bind` 绑定);每个一级子文件夹大小=每便携软件磁盘占用,一眼看清。要点:walk 统计缓存+手动/后台刷新、巨型目录(Everything 索引/快照库)限时渐进、紧凑排序列表走 hanxi-workbench-ui;挂设置页「存储目录」分区还是独立模块开工定。 |
| N12 | **litemonitor 简介补「释放内存」能力**(机主指出) | 上游 LiteMonitor 支持释放内存,hanxi 模块简介未写。定位 catalog/视图简介文案,对照上游功能清单核实后补齐,顺带排查其他"已支持未宣传"缺项;纯文案低风险可插空。与 N4(RAMMap 内存释放)同主题,文案口径可同批收口。 |
| N13 | **托管版本列表显示全平台安装包并标注形态** | 现 release 被过滤到只剩当前 Win 机器可用包(以 flclash 为例);改为展示 Windows/macOS/Android APK(Linux 可不要),逐包写清**便携版/标准安装版**。三处落点:①各模块 `version` 包平台过滤改"全量返回+平台标注";②共享契约 `ManagedReleaseRecord`(现仅 version/published/size/isPre)扩平台/形态/URL 字段,Wave 5 版本面板加平台与形态 chip——**跨模块契约变更,开工前出正式计划评审**;③非 Windows 资产只给"浏览器打开下载",不进 hanxi 下载/设使用链路。 |
| N14 | **envcheck 扩展:环境本体+依赖目录占用显示** | 现只报"装了什么/什么版本",看不到空间家底:①每个环境**本体安装目录**大小(Go SDK、Node、JDK、Python、.NET 各占多大);②依赖/缓存目录位置与大小(Go `GOPATH/pkg/mod`/GOCACHE、npm 全局包与缓存、pip/Maven `~/.m2`/Gradle/NuGet)。落点 `internal/modules/envcheck/` 检测器补双目录推导(env var→默认路径→工具命令三级回退)+`EnvCheckView` 展示"本体/依赖家底"两列。**与 N11 共用同一 walk 目录统计公共件,两项同批:先下沉公共包再各自接线**(实践"新功能第一天走公共包"纪律);UI 走 hanxi-workbench-ui。 |
| N15 | **评估:OCR 与 Snipaste 结合**(机主:"不能就算了") | 纯可行性评估,先查证再定立项。候选结合点:Snipaste 截图产物(剪贴板/保存目录)触发 hanxi 识图、钉图取词、轮盘/托盘一键"截图+识别"(现 `ocr/snip` 已有自截链路可比照)。关键未知=Snipaste 对外触发接口面(命令行/热键编程接口/文件监听),以官方文档+实测为准;不可行则销项留档。 |
| N16 | **随手记(memo)演进:路线已定 A→B→C**(首轮沟通 2026-09-20 完成) | 机主四类痛点全选(怕丢/记不快/找不回/用不上)→ 完整升级,三批次:**A** 文件库化+空闲静默 git 快照(无 git 降级普通备份,底稿 `PLAN_SNAPSHOT.md`,与 F3/N7 飞牛同步天然合流)→ **B** 全局热键快记+悬浮速记卡+全文/模糊检索 → **C** MCP/CLI 联动深化与导出(依赖 A/B 形态)。每批开工前出实施计划;后续批次细节沟通随批进行。 |

> N1/N2 与 N3 同为"外部进程也能被 hanxi 退出/唤窗"诉求的具体症状:N1/N2 先真机复现修点,N3 决策收口成面。

### P4 执行波次排序(2026-09-20 复核重排;执行以本节为准,上表保留原始登记与详情)

复核实测三条硬事实(原登记未覆盖,直接影响排序):

1. **artifact 包只支持 zip**:`packages/go/artifact/unpack.go` 仅有 `UnpackZip`(全套 zip 安全闸门),无 7z 解包能力——N8(WindTerm release 为 7z 包)存在真实技术选型门槛,不是"确认一下"的成本。
2. **版本契约在 Go 侧并不共享**:21 个模块各有一份 `version/models.go`(`isPre` 字段重复 21 次),N13 实际成本="21+ 处字段扩散 + 前端共享层 + 版本面板";且新托管模块每先落地一个、N13 就多一处返工点。
3. **N1/N2 的卡点在决策不在代码**:`noExternalProbe` 恒报不在运行、`Quit()` external 态只回指引,均为刻意设计(管理边界=本会话进程树)。动代码前须先裁决"hanxi 是否有权控制自己未启动的进程",即 Wave 4X 的收口决策(从 N3 中拆出前置)。

| 波次 | 内容 | 成本 | 排位理由 |
|---|---|---|---|
| **W1 决策与前置调研**(半天,不写码) | ①N3 决策拆半:外部实例退出/唤窗权限边界一页纸裁决;②N15 查证销项;③N5 核实 starpie 仓库(登记日未定位);④N8 的 7z 方案选型 | 小 | 四项全是后续波次的门闸,一次沟通全部裁决 |
| **W2 外部实例治理**(真机) | N1+N2 同批按 W1 裁决实施(snipaste 补 external 探测+OpenWindow;everything 补外部 PID 取得+优雅退出);N12 纯文案 30 分钟搭车 | 中 | 机主真机报告的在用功能故障,痛感最高;与 P1 F10 冒烟同场跑,合并真机验收 |
| **W3 空间可见性** | 先下沉 walk 目录统计公共包 → N11/N14 各自接线 | 中 | 原登记已自判同批;"新功能第一天走公共包"纪律;纯本地可开发,适合真机等待期填充 |
| **W4 托管集成批** | N7 飞牛同步 → N9 Termora → N4 RAMMap;**N8 WindTerm 押后至 W1 的 7z 选型裁决**(成本高则长期挂起,不拖累本批) | 大 | N7 缘起级用途、W1 清障后可开工(先确认静默参数/升级语义);N9 先行规避 7z 门槛并树绿色型样板,N8 复活时抄验;N4 提权+一次性语义开工前正式计划 |
| **W5 体验债** | N6 msgboard 方案先行→实施(纯前端、盘子较小先做);N5 轮盘改进(前后端几何同源改造,大活,含踩坑 #50 欠账);P3 前端打磨批次一可同窗并流 | 大 | 属重设计而非修 bug,须先出方案给机主过目;出方案可与 W3/W4 并行,动代码排队 |
| **W6 契约收口** | N13 全平台安装包+形态标注,一次扫过 21+ 模块 models 与前端契约 | 大 | 后置于全部新托管落地,避免逐模块返工;与 P0 批 3(managed 前端)同域,排在批 3 之后;开工前正式计划评审 |
| **W7 长线** | N16 随手记 A→B→C;N10 系统信息新模块(独立无依赖,可作任意波次并行候选) | 大 | A 批同步语义宜等 N7(及 F3)收口后设计,与飞牛合流;N10 为借鉴型痒点,痛感低于其余,不占主线窗口 |

与原登记的关键差异:N3 拆"决策前置 W1 / 族推广后置于 N1、N2 验证";N8/N9 由"同批互相抄验"解绑为 N9 先行;N12 从"与 N4 同批"改为搭 W2 便车(现成功能文案缺漏没理由等 RAMMap);真机类任务并段(N1/N2+F10),控制 Linux 开发环境与 Windows 真机的往返成本;与 P0 质量批的并行边界——W2 在实例管理层、P0 批 1/2 在 registry/租约层不冲突,仅 N13(W6)与 P0 批 3 同域需后置。

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
