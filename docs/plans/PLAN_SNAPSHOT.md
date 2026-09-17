# 数据自动版本快照：可行性分析与开发计划

> **版本**：v1.0（2026-09-17 定稿调研，本文档为开发计划）
> **决策依据**：`docs/MOOTOOL_ANALYSIS.md:25-29,55` 借鉴点 #3（P1，"memo 文件库化 + 配置目录快照，git 不存在降级普通备份"）；
> 参考实现 MooTool `vaultGitService.ts` / `vaultGitCheckpointScheduler.ts` / `backupService.ts`（外部 git + 5s tick 调度 + 白名单时间戳导出）。
> 红线约定：**永不自动 push（纯本地历史）**；快照全程静默，不打扰用户。

---

## 1. 结论先行

| 问题 | 结论 |
|---|---|
| 可行性 | **高**。数据面是 24 个平铺 JSON（KB 级）+ 统一原子写公共核 `internal/jsonstore`，git 天然擅长；外部 git 探测/调用/降级在仓内全有先例可抄 |
| 触发源 | **全部留在 Go 侧**：主窗 `OnWindowEvent(common:WindowLostFocus)` 现成先例 + 文件 mtime 巡检兜底 + `OnShutdown` 退出前补一次；**前端零改动** |
| 安全边界 | 密文（`dpapi:` 前缀）入库无增量风险；**明文雷区靠白名单排除**：`runtime/`（frpc 明文 TOML 残留）、`logs/`、`versions/`、`installers/`、原子写/取证中间产物——详见 §2.3 风险表 |
| memo 迁移 | service API 与前端 MemoView **可做到零改动**（内存全量缓存 + List/GetStats 语义不变）；迁移走"判据幂等 + 改名即提交点"，有 ocr 双写先例背书 |
| 量级 | 第一步（config 快照链路）约 **5 人日**，第二步（memo 文件库化）约 **4 人日**；合计 ~9 人日 + 真机联调缓冲 1 日 |
| 与统一历史包的关系 | 建议**历史包先行、快照随后**（§5 给理由），但两者代码零耦合，紧急可并行 |

概念撞名预警：仓内 "Snapshot" 一词已被"托管实例运行态事件载荷"占用（`internal/app/app.go:131-185` 二十余处 `instance.Snapshot`），新功能统一用**"检查点 / checkpoint"**命名（包名 `internal/snapshot` 保留用户口径，对外文案用"历史版本"）。

## 2. 现状与证据

### 2.1 数据目录布局（快照作用域的事实底座）

- 解析：`internal/settings/paths.go:23-26`（`hanxidata` 常量，旧名 `data`）、`:61-86`（exe 同目录探测，同级 `hanxidata/` 存在即生效含空目录 `:93-96`；旧 `data/` 需特征认定 `:106-114`）、兜底 `%APPDATA%\Hanxi`（`:75-85`）。**无环境变量/命令行覆盖**。
- 布局：`:117-127` **configDir == dataDir**（config.json 直接落数据根 `:147`），另有 `logs/`、`versions/`、`runtime/` 三子层。
- 实测 `bin/hanxidata/`：`config.json` + **24 个模块 JSON 平铺根目录**（memo、projects(frpc)、ccswitch、ocr 等，注入方式统一 `newXxxStore(paths.DataDir())`）+ 子目录 `everything/`（含 ES.exe 二进制）、`installers/`、`logs/`、`runtime/`、`versions/`（托管工具安装树，数百 MB）。
- quickmenu 无独立文件：条目即 config.json 的 `trayMenu/quickMenuTwoTier` 字段（`internal/modules/quickmenu/service.go:60` 注 settings.Store）。
- ⚠️ 仓库根 `.gitignore:14` 只忽略旧名 `data/`，**未忽略 `hanxidata/`**（在仓库根跑 exe 即产生海量 untracked）——顺带修的独立小项。

### 2.2 落盘机制（决定了"mtime 巡检"可行且免埋点）

- 公共核 `internal/jsonstore/jsonstore.go:55-87`：25 个模块 store 共用的 **tmp(`<path>.tmp.<pid>`) + fsync(`:73`) + rename 原子替换**；读侧严格区分 缺文件/0字节(`ErrEmpty`)/坏 JSON(`ErrCorrupt`)（`:35-50`）。
- settings 自留一套同构原子写（`internal/settings/store.go:406-429`，**无 fsync**，与 jsonstore 的差异点）；**候选提交回滚** = `Update(fn)`（`:213-229`：fn 只改副本、落盘成功才换装内存、失败回滚——"保证内存与磁盘不分叉"）；**损坏隔离** = 装配根 rename 到 `config.json.corrupt-<时间戳>` 后降级出厂默认（`internal/app/app.go:219-238`）。
- 推论：所有用户数据写入都表现为"根目录少数 JSON 文件的 rename 式 mtime 跳变"，巡检 `os.Stat` 白名单即可判定脏，**不需要在 25 个 store 里埋快照钩子**。

### 2.3 敏感面审计（入库安全性的直接答案）

| 数据 | 位置与证据 | 密/明 | 入库判定 |
|---|---|---|---|
| frpc server token | `internal/modules/frpc/store.go:17,81-91`：`dpapi:<base64>` 内联于 `projects.json`；加密失败**拒存明文**（`:85-91`） | 密文 | **可入库**。DPAPI 绑定当前用户+机器（`internal/platform/windows/dpapi.go:21-51`），历史里的密文与盘上密文同强度，无增量泄露 |
| frpc `proxies[].secretKey`、`server.proxyUrl` | `internal/domain/frpc.go:23,46`——**saveLocked 只加密 Server.Token**，这两处不加密 | 明文 | 盘上既已明文，入库不降标准，但"删了仍驻史"；列入开放问题 Q2 |
| wechat botToken/contextToken | `internal/settings/store.go:14-26` **明文**入 config.json，且监听器持续回写（`internal/modules/wechat/listener.go:202`） | 明文 | 同上 Q2；注意 `.corrupt-*` 取证副本连带明文驻盘（`app.go:226-227`） |
| frpc 运行时明文 TOML（含 `[auth] token`） | 写 `hanxidata/runtime/frpc/frpc-<id>.toml`（`frpc/service.go:45,334-336`）；仅手动 Stop（`:358-369`）/删项目（`:268`）擦除；**正常退出走 StopAll 不删**（`frpc/module.go:59-61`→`service.go:390-394`→`instance/manager.go:78-83` 全链无 Remove）、崩溃同样残留、**无启动孤儿扫描** | 明文 | **必须排除 `runtime/`**（白名单天然覆盖）。另建议独立 fix：OnDestroy 擦除 + OnInit 孤儿清理，见 §4 任务 S0 |
| OCR 粘贴图 | `runtime/ocr/`（`ocr/service.go:52,504`） | 用户截图可能含敏感 | 排除 |
| 日志 | `logs/app-*.log`（`logging/logger.go:132-133`）；RedactHandler 正则脱敏有边界（`:32-66`） | 基本干净 | 排除（无版本价值） |
| 工具二进制树 | `versions/`、`installers/`、`everything/es/`、`ocr-engines/` | — | 排除（非用户数据，GB 级） |
| 中间产物 | `*.json.tmp.<pid>`（两处原子写）、`*.corrupt-*` | — | 排除 |

**白名单策略**（正向列举，新模块 JSON 落根目录即自动入保）：`/config.json`、`/*.json`（再排除 `*.tmp.*`、`*.corrupt-*`、`memo.json.migrated` 视情况）、第二步追加 `/memo/`。ignore 规则写入 git-dir 的 `info/exclude`（`--git-dir` 隔离后**数据根不落任何 git 可见文件**）。

### 2.4 触发源证据（失焦信号怎么拿）

- **Go 侧原生失焦**：Wails beta.10 Windows 后端 `WM_KILLFOCUS → emit(WindowKillFocus)`（模块缓存 `pkg/application/webview_window_windows.go:1668-1672`），映射 `common:WindowLostFocus`（`pkg/events/defaults.go:23`）；**仓内现成用法**：quickmenu 弹窗 `popup.OnWindowEvent(events.Common.WindowLostFocus, ...)`（`internal/modules/quickmenu/service.go:117-119`）。主窗同款可挂（先例 `app.go:411,449` 两处窗口钩子）。
- **盲区**：主窗关到托盘是 `Hide()` 不销毁（`app.go:448-458,466-474`），隐藏后不再产生失焦事件——驻托盘期间"失焦"永远不再命中。旁证感知手段：`notify/hub.go:76-79` 用 `IsVisible()/IsMinimised()` 判后台。**对策**：挂 `common:WindowHide/WindowMinimise` 等价视为一次失焦事件 + mtime 空闲巡检兜底。
- 前端 `@wailsio/runtime` 也暴露 `WindowLostFocus`（`frontend/node_modules/@wailsio/runtime/types/event_types.d.ts:262`），但走前端再回传后端纯属绕路（仓内无先例），**不采用**。
- ticker 生命周期范式：stop-channel + `OnInit/OnDestroy`（`internal/modules/ccswitch/service.go:85-119`，其 `idleCheck :131-146` 就是"空闲自动动作"模板；契约 `internal/extapi/module.go:115-118`，回收 `registry.go:317-329`）。
- 退出钩子：`Options.OnShutdown → registry.ShutdownAll()`（`app.go:354-359`，注释明言"阻塞至返回"）——**退出前最后一次同步快照挂这里**。
- 节流语义（对照 MooTool `vaultGitCheckpointScheduler.ts:49-76`）：其"同一活动只一次"在 hanxi 场景**更简单**——`git status --porcelain` 干净即无变更，"无变更不 commit"天然去重，只需再加 `lastCommitAt` 最小间隔（默认 5 分钟）防编辑期连环保存灌碎历史。

### 2.5 外部 git 调用规范（仓内先例齐备）

- 探测：envcheck 标准链 `LookPath → --version → 正则`（`internal/modules/envcheck/detect/detect.go:17-70`），5s ctx + `windows.HideConsole` + `CombinedOutput`（`detect/runner.go:14-42`）；**git 有 Microsoft Store 假存根专属判定**（`detect.go:44-48`，存根执行即报错退出 → 自动落入"不可用"分支）。快照侧自带同款 20 行探测即可，不 import envcheck（避免模块互引）。
- 一次性短命令：`exec.CommandContext + HideConsole`（`internal/modules/wsl/readiness/wslcli.go:17-32`）；`.cmd/.bat` 需 `cmd /C` 包装（`runner.go:22-27`），git.exe 是原生 exe 不涉及。
- 串行队列：per-repo 单飞闸照抄 `wsl/distro.go:269-306` `tryBeginDistroOp`（"快速失败不制造排队假象"）；快照只此一个仓库，一个 mutex + TryLock 忙即跳过即可（tick 语义下无排队需求）。
- 陈旧 `index.lock`：借鉴 MooTool——重试一次→锁文件 mtime>5min 且句柄无占用→rename 隔离→再试→删隔离件（`vaultGitService.ts:352-396`）。Go 侧句柄探测可简化为"仅按 mtime+大小稳定判据"。
- 静默失败边界：异常一律 `slog.Error` + 首次失败 `notify.Warning("snapshot", …)` 去重一次（notify 四件套 `internal/notify/notify.go:12-53`）；git 缺失**不是错误**（探测得结构化状态，参照 `detect.go:30 StatusMissing` 哲学），直接走降级链。
- 必带防护参数：`GIT_OPTIONAL_LOCKS=0`、`GIT_TERMINAL_PROMPT=0`（MooTool `vaultGitService.ts:398-405`）；`-c user.name=Hanxi -c user.email=snapshot@hanxi.local` 仓库内 `git config` 一次性设 local，**绝不碰用户全局配置**；另设 `core.autocrlf=false`、`commit.gpgsign=false`（用户全局开了 gpg 签名时静默提交必炸）；提权运行场景加 `-c safe.directory=<dataDir>` 防 dubious-ownership 拒命。
- 仓库位置：`hanxidata/.snapshots/repo.git`（`--git-dir` 分离、非 bare + `--work-tree=hanxidata`）。理由：① 数据面零污染（不写 .gitignore 到用户目录）；② 清史/搬移 = 删一个隐藏目录，降级影子拷贝同放 `.snapshots/` 下语义统一；③ 用户若未来自行在数据目录 git init 不冲突。代价：每条命令固定两前缀参数，封装进一个 `gitCmd(args...)` 构造函数即可。

### 2.6 memo 现状（迁移影响面清单）

- 模型：`internal/modules/memo/models.go:6-16`——`ID string`（`memo_<unixnano>`，`service.go:157`）、Title、Content、Tags（内嵌数组，`#` 前缀清洗 `:291-308`）、IsPinned、**IsMasked（持久布尔，非临时显示态）**、ColorTag、双时间戳。
- 存储：`hanxidata/memo.json` **裸数组无版本字段**（`store.go:20-66`），启动一次性 Load 进内存全量缓存（`service.go:32-35`，注意此处 Load 错误被吞——BUG-032 点名位置 `docs/BUG_AUDIT.md:392-400`）；**整体重写点共 5 处**：Create/Update/TogglePin/ToggleMask/Delete（`service.go:169,209,231,251,274`）。
- 前端：`frontend/src/views/MemoView.vue`（750 行）——每次变更后**全量重拉** List+GetStats（`:58-88,176,186,196,213`），350ms 防抖 + loadSeq 竞态守卫（BUG-049 修复 `:53-63`）；遮罩渲染 `.masked` 显 `•••`（`:351-354,628-632`）；订阅 `memo:changed`（`:226`）。实测数据量：11 条 / 3.7KB——**全量内存缓存模式迁移后照旧成立**。
- 耦合：fileshare 投递经 `QuickCreate` hook 写入（`app.go:280-285`）；除此之外全仓无 memo.json 第二读者（调研 grep 结论）。
- 迁移先例：ocr 的"load 时判据幂等 + 旧字段双写镜像 + 盘上断言测试"（`internal/modules/ocr/store.go:92-96,121-122`；`store_test.go:72-135`）；frpc 事务性回退测试（`store_transaction_test.go:11-36`）。**仓内无 schema 版本号字段先例**，全部走判据幂等——本迁移遵循同一文化。

## 3. 方案设计

### 3.1 服务形态

`internal/snapshot`（非 extapi 模块——它是平台底座不是"工具"；先例：settings 也走 AppService 暴露面）：

```
hanxidata/
├── .snapshots/
│   ├── repo.git/          # git-dir（探测到 git 时）
│   └── backup/            # 降级影子拷贝（git 缺失时）
│       └── 20260917-1430/ # 时间戳白名单拷贝，滚动保留
├── config.json / *.json   ← 白名单快照对象
└── memo/<id>.md           ← 第二步入库
```

- 装配：`app.New` 构造 `snapshot.New(paths, settingsStore)`，挂 `application.NewService` 出绑定面；配置字段（enabled/idleSeconds/intervalMinutes）经 settings.Store 新增 `SnapshotConfig` 持久化（缺字段回落出厂即向前兼容，`settings/store.go:127-130` 机制）。
- 生命周期：`OnInit` 起 5s tick（ccswitch 范式），`OnShutdown` 链上补最后一次**同步** commit（有超时闸门如 3s，卡死不拖退出）。

### 3.2 触发器（三源一闸）

1. **失焦/隐藏**：主窗 `OnWindowEvent(common:WindowLostFocus/WindowHide/WindowMinimise)` → 置 `deactivatedAt`；恢复焦点清零。
2. **空闲兜底**：tick 内对白名单文件 `Stat` mtime，距最近活动 ≥ idleSeconds（默认 300s）视为空闲——覆盖"驻托盘后台仍有写入"（如 wechat 回写 contextToken）与失焦盲区。
3. **退出**：OnShutdown 一次。
闸门：`running` 单飞 + 距上次 commit ≥ intervalMinutes（默认 5）+ `git status --porcelain -z` 空即跳过（= MooTool "同一活动只一次"的等价实现）。tick 只读 Stat/git，永不阻塞 UI。

### 3.3 提交与降级链

- init（首次）：探测 git → `--git-dir … --work-tree … init -q` + local config（身份/autocrlf/gpgsign）+ 写 `info/exclude`；init 失败 → 静默转降级链。
- commit：`add -A -- <白名单>` → 无变更 skip → `commit -q -m "checkpoint: <人话摘要>"`（摘要 = 变更文件名列表，供恢复 UI 直接展示）。
- 降级：影子拷贝按同一白名单逐个 copy 进 `backup/<ts>/`（白名单式拷贝天然规避 MooTool backupService 的"目标不得为源子集"坑——`.snapshots` 不在白名单），保留最近 30 份滚动删。
- 两种模式对外统一 `History()/Restore()` 接口（降级模式 History=列时间戳目录，Restore=单文件回拷）。

### 3.4 memo 文件库化

- 格式：`memo/<id>.md`，最小手写 frontmatter（**不引 YAML 库**，`---` 包裹 `key: value` 行，tags 逗号分隔，时间 RFC3339；解析失败该条报错不落盘——延续严格型读策略）+ 正文原样。
- 读：启动全量入内存（照旧）；写：Create/Update 只写单条文件、Delete 删单文件——**整体重写点从 5 个变 0**，git diff 从此精确到"改了哪一条"。
- 迁移（判据幂等，无版本号）：`memo/ 不存在(或空) && memo.json 存在` → 逐条写 `memo/migrating-<pid>/` → fsync 全量 → `rename memo.json → memo.json.migrated`（**改名即提交点**，旧文件保留可人工回退）→ rename 暂存目录为 `memo/`；任一步失败清理暂存、memo.json 未动即原样回退。迁移后 `memo.json.migrated` 先不入库（exclude），一个版本周期后转正。
- service API / bindings / MemoView 全部零改动（§2.6 前提）；唯一建议顺手修：`service.go:32-35` Load 错误吞没改为隔离副本+报错（对齐 BUG-032 结论）。

### 3.5 恢复 UI（从简）

设置页第七分区「数据快照」（`constants/navigation.ts:81-87,100-114` 清单加一条，AppSidebar 自动渲染）：状态行（git 可用/模式/上次快照/份数）→ `.tbl` 历史列表（`styles/components.css:138-159`，形照 `FrpcVersionsTab.vue:25-41` 列表+行尾动作）→ 行内「预览」弹 `modal-card`（`FrpcProjectEditor.vue:771-810` 形，列该 commit 变更文件+≤512KB 文本预览，截断规则照 MooTool `vaultGitService.ts:40,151-162`）→「恢复」走 `useConfirm`（tone=danger + details 列版本/文件/时间，`composables/useConfirm.ts:8-15`）。恢复语义分两路：
- **memo 单条**：后端 `RestoreMemo(id, rev)` 写回文件 + 内存换装 + emit `memo:changed`，热生效无感。
- **config.json / 其他模块 JSON**：直接写盘会与运行中内存态分叉（违背 `settings.Update` 不变式），故写盘后 `notify` 提示「重启生效」——v1 不做热回滚。
降级模式 UI 退化为「打开备份目录」一个按钮。

## 4. 任务分解（commit 粒度）

**第 0 步（前置独立 fix，可先行合入）**
- S0 `fix(frpc): 退出/崩溃后运行时明文 TOML 残留清理`——OnDestroy 链补 `os.Remove(cfgPath)` + OnInit 扫 `runtime/frpc/` 孤儿删除（补 §2.3 已核实的双缺口）。

**第一步：config 快照（里程碑 M1）**
- T1 `feat(snapshot): 服务骨架、git 探测与白名单巡检`（含 settings 配置字段；单测：探测降级、mtime 脏判定）
- T2 `feat(snapshot): git 检查点管线`（gitCmd 封装、init、锁恢复、身份/参数钉死、串行闸；单测用本机 git 真跑 + fake-git 注入桩）
- T3 `feat(snapshot): 失焦/隐藏/空闲三源触发与退出补拍`（主窗钩子、5s tick、interval 闸；OnShutdown 挂链）
- T4 `feat(snapshot): 影子拷贝降级链与滚动清理`
- T5 `feat(settings): 第七分区「数据快照」——历史浏览、预览与恢复`（含 AppService 绑定方法；vitest 照 `SettingsSections.spec.ts` 打桩范式 + `navigation.spec.ts:13` 计数 48→49）
- T6 `chore(repo): .gitignore 忽略 hanxidata/`

**第二步：memo 文件库（里程碑 M2）**
- M1 `feat(memo): 每条一文件 frontmatter 存储与幂等迁移`（migrating 目录 + 改名提交点；**必含回退测试**：半途中止后 memo.json 字节不动、重入跳过）
- M2 `refactor(memo): 保存点改单条写，整体重写点清零`（含 QuickCreate/fileshare 回归、BUG-032 吞错修复）
- M3 `feat(snapshot): memo 目录入白名单 + RestoreMemo 热恢复`
- M4 `test(frontend)`：MemoView 期望零改动，仅补回归断言（若 bindings 因新方法再生成则同步 `task check`）

## 5. 排期与依赖

| 阶段 | 内容 | 人日 |
|---|---|---|
| M0 闸门 | 真机 spike：①主窗 WindowLostFocus 钩子在 beta.10 实测可达；②本机 git + Store 存根两环境探测；③`--git-dir` 分离模式 add/commit/log/show 全链路。半天不通过则改道（数据根直接 `.git`），**止损点** | 1 |
| M1 | T0-T6（第一步全量） | 5 |
| M2 | M1-M4（第二步全量） | 4 |
| 合计 | 含真机联调与 TROUBLESHOOTING 沉淀 | **~10** |

依赖判断：**统一历史包先行**。理由：① `MOOTOOL_ANALYSIS.md:100-108` 路线图既定序（历史包=第 1 位"量级小"，快照=第 4 位）；② 历史包是"新功能第一天走公共包"的还债样板（该文档 `:21-23` 原话），快照作为后来者应直接采用其沉淀的面板范式，而非先建一套第七分区再被历史包推翻重来；③ 两者数据面无耦合（历史包管工具结果，快照管文件版本），先行不阻塞快照设计定稿。**若用户 memo 丢失之痛更急，两步序对调亦无冲突**——唯一代价是 T5 的列表面板将来向历史包公共件看齐微调。

## 6. 风险与边界登记

1. **驻托盘后台写入**（wechat 每收消息回写 config.json）会让"空闲"定义复杂化：mitigate 于 interval 闸 + "无变更不 commit"，历史会碎但容量数学无压力（JSON 百字节级 diff，千次 commit 仅 MB 级，git 不 prune；影子模式才滚动删）。
2. git 仓库自身损坏（断电瞬间 commit）：git 有 fsck 自愈习惯，快照服务 commit 连续失败 N 次→自动重 init 仓库（旧历史尽力 rename 保留为 `.snapshots/repo.git.broken-<ts>`），不再静默烧 tick。
3. 用户全局 git 配置千奇百怪（hooks core.hooksPath、模板 core.hooksPath 提交钩子）：命令统一带 `-c core.hooksPath=`（置空）钉死，防用户全局 hooks 拦快照。
4. beta.10 事件系统"未注册类型载荷静默丢弃"（`app.go:120-122` 注释在案）：快照若新增 `snapshot:committed` 类事件回前端刷列表，**必须 Void 注册**。
5. 提权实例与普通实例同开两进程（理论单实例锁已防，`app.go` 提权回航链路在）：若未来破锁，两进程共写 git 必互踩——依赖现有单实例锁，登记不重做。

## 7. 待拍板决策（进入 M1 前确认）

| # | 问题 | 选项与建议 |
|---|---|---|
| Q1 | **敏感便签（isMasked）是否进快照**：git 历史 = 明文驻留，UI 遮罩是视觉约定非加密边界（今天它本来就是盘上明文） | A 照常入库（建议：与现状同标准，"删错的便签能找回"恰是快照主目的）；B 白名单排除 `masked` 条目（代价：开关切换引发文件增删噪音，且恢复 UI 少一块） |
| Q2 | **config.json 内 wechat 明文 token / projects.json 明文 secretKey 入历史**：入库不降标准但拉长明文生命周期 | A 接受并推进（建议）；B 先另立小项把它们 DPAPI 化（frpc token 先例现成），快照随后 |
| Q3 | 降级影子拷贝保留份数与是否需要恢复 UI（MVP 建议 30 份 + 仅"打开目录"） | 拍数量即可 |
| Q4 | 空闲阈值/最小提交间隔默认值（提案 300s / 5min，进设置分区可调） | 拍默认值 |
| Q5 | 要不要「立即快照」手动按钮（成本 ~0.2 人日） | 建议加（首拍验证链路全靠它） |
| Q6 | `memo.json.migrated` 保留多久后转正入库（建议两个版本周期后删旧链） | 随口确认 |

> 决策回写机制同 `OCR_DUAL_ENGINE_PLAN.md` 体例：拍板后在本节追加"决策回写（日期）"行，不另开文档。

**决策回写（2026-09-17，用户拍板）**：

- Q1 = A：遮罩便签照常入快照（遮罩是视觉约定非加密边界，防手滑正是快照主目的）。
- Q2 = A：wechat 明文 token / secretKey 接受随快照入历史，快照推进不等待；DPAPI 化不单独立项阻塞本功能。
- Q3 = 降级影子拷贝 30 份、恢复 UI 仅"打开目录"。
- Q4 = 默认值按提案 300s / 5min，设置分区可调。
- Q5 = 加「立即快照」手动按钮（首拍验证链路依赖它）。
- Q6 = `memo.json.migrated` 两个版本周期后删旧链。
- **另：§4 任务 S0（frpc 运行时明文 TOML 退出/崩溃均不擦除 + 无孤儿扫描）由"另建议"转正为独立 fix 排期**，用户明确批准修——先于快照工程走，建议单开 `fix(frpc)` 提交与 TROUBLESHOOTING 沉淀。
