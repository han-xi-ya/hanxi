# N33 设置「历史版本」分区交互重做设计方案（待机主拍板件，只定案不动码）

> 排期出处 [PLAN_REMAINING_WORK](PLAN_REMAINING_WORK.md) N33（机主裁决 2026-09-23：
> "反正不友好,排期"——三步讲解仍看不懂，问题在概念模型不在文案）。
> 本文以 2026-09-25 实读 `internal/snapshot/` 全部服务面 + `SnapshotSection.vue`
> 及其特征测试为底座，先把三条病灶逐条对照代码验真/修正，再给目标信息架构、
> 新增 RPC 契约、摘要生成边界、降级态文案与分批实施表。
> 边界声明：本批不读取、不改动 `hanxi-workbench-ui` skill（有并行任务）；
> 皮一律沿用分区现有工作台原子类（`.card/.tbl/.chip/.setting-row/modal-card`）。

---

## 0. 现状审计与病灶对照（代码实证）

### 0.1 现有 RPC 面（`internal/snapshot/service.go`，绑定名 `CheckpointService`）

| RPC | 语义 | 实读结论 |
|---|---|---|
| GetStatus | 状态行（mode/gitAvailable/份数/偏好/目录） | 两模式统一引擎接口 |
| ListRevisions | 全家福版本列表 | **上限 `maxListRevisions = 50`**（git log `--max-count` / 备份目录截断） |
| RevisionDetail(id) | 该版文件清单 + 变化类型 | git 有真实 M/A/D/R（`show --name-status`）；**备份模式恒 M**（全量拷贝无变化语义） |
| PreviewFile(id,path) | 单文件全文预览 ≤512KB 截断 | 无 old/new 对比，即排期说的"内容盲看" |
| RestoreFile(id,path) | 单文件回滚 | memo/ 走热恢复（`SetMemoRestorer` 注入 `MemoService.RestoreFile`，内存换装 + `memo:changed`）；config/state 走 `StagePendingRestore`，**下次启动生效**（`app.go:271` ApplyPendingRestores） |
| CheckpointNow / OpenHistoryDir / SetPreferences | 手动快照 / 开目录 / 偏好 | 不变 |

路径与版本参数闸门：`normalizeWhitelistPath`（白名单 + 穿越双闸）+ `checkRevisionID`
（git hex 7–40 或备份时间戳形态）——强度高于 `DataRootSubUsage` 的单段名校验
（`internal/app/storage_usage.go:98`），新 RPC 沿用同一纪律。

### 0.2 排期三条病灶逐条对账

| # | 排期诊断 | 验真结果 | 修正 |
|---|---|---|---|
| ① | "版本=全家福"是快照为轴心智，用户要文件为轴 | **属实**。唯一浏览链 = 版本表 → 预览弹窗 → 文件行（`SnapshotSection.vue:130-148`），从文件出发查历史完全无路 | 无修正，这是本方案主靶 |
| ② | 变更摘要是文件路径串（memo/xxx.md） | **半属实，方向要改**：`summarizeFiles` 已 `baseNames` 剥掉路径（git.go:349-371），实际是**文件名串**（"config.json, memo_1758….md 等 N 个文件"，最多列 5 个）；备份模式摘要更是只有"N 个文件" | 病灶比诊断更深一层：不是"路径太技术"，是**字符串本身无语义**——便签文件名 `memo_<unixnano>.md` 不指向任何一篇可辨认的便签，config.json 出现在串里等于没说。修复靠 §4 文件名字映射 + §5 自然语言摘要，不靠"显示全路径"或"藏路径" |
| ③ | git 与备份两套 UI 落差大，降级只剩一个按钮像坏了 | **属实但根因修正**：`backupEngine` **已完整实现** engine 接口的 `revisions()/revisionFiles()/file()`（backup.go:162-242），ListRevisions/RevisionDetail/PreviewFile/RestoreFile 四支 RPC 在备份模式下**后端全部可用**；是前端按当年 Q3 裁决（PLAN_SNAPSHOT §7"恢复 UI 仅打开目录"）主动隐藏——`refresh()` 里 `mode==='git'` 才拉列表（SnapshotSection.vue:66-70），测试还钉死了"降级不拉列表"（spec:69-77）。 | 改造 = **解封前端 + 补变化检测**，不是给备份模式新造浏览面，成本远低于诊断字面预期。真实缺项只有两处：备份模式 revisionFiles 恒 M（无 A/D 徽标，读时相邻 manifest 差集可补，见 §6），以及"打开目录"是唯一动作的呈现 |

### 0.3 实读新增病灶（排期未列，重做时必须一并收）

- **A. 删除的便签当前找不回（功能洞，非观感题）**：预览弹窗对 `status==='D'` 行不出
  恢复按钮（SnapshotSection.vue:303），且 `PreviewFile` 对该版本中已删文件必失败
  （git show id:path 落空）。而"删错的便签能找回"恰是 PLAN_SNAPSHOT Q1 拍板快照
  要服务的头号场景。修法不需要新 RPC：文件为轴的时间线上，"删除"事件行给
  「恢复被删内容」，目标取该文件中**最后一个存在的版本**（`FileHistory` 列表里就有），
  走现有 `RestoreFile`；`MemoService.RestoreFile` 对不在内存的 ID 会重建装载
  （memo/service.go:429 起，恢复链已支持复活，实施时补一条测试钉死）。
- **B. 恢复动作埋两级深**：版本行 →「预览」弹窗 → 文件行 →「恢复」。文件为轴
  重排后恢复应长在文件时间线行上（排期方向正确），弹窗链只保留给"全部版本"辅助视角。
- **C. 热/重启生效差异要在 UI 层显形**：便签即时 vs config/state 重启生效是后端
  既有语义（好设计，不动），现在只活在确认框文案与 toast 里；重做后应成为文件
  行上的常驻微标（"即时生效"/"重启生效"），而不是点了才知道。
- **D. 50 版截断无感知**：列表静默截断到 50，用户不知道"更早的看不看得到"。
  新时间线 RPC 同受 cap，需在超过时如实给"仅展示最近 50 版"字样。

---

## 1. 目标信息架构：文件为轴双栏

```
┌── 历史版本 ──────────────────────────────────────────────── [全部版本] chip(模式) ┐
│ 偏好卡（现状四行不动：开关/空闲/间隔/存储位置+立即快照）                          │
├────────────────────┬──────────────────────────────────────────────────────────┤
│ 受保文件清单        │  所选文件的时间线（新→旧，≤50）                            │
│  便签 (n)          │  ┌ 09-25 14:30 · 修改了内容（+3 −1 行） [展开对比] [恢复]  ┐ │
│   《购物清单》      │  ┌ 09-24 09:00 · 新建了这篇便签          [展开对比]        ┐ │
│   《XX 排障笔记》   │  └ …                                                      ┘ │
│  工作台设置 (1)     │   [查看该文件在磁盘上的当前内容]                            │
│  模块状态 (n)      │                                                              │
│   快捷菜单 · 文字识别 …                                                          │
└────────────────────┴──────────────────────────────────────────────────────────┘
```

- **左栏 = 受保文件清单**（新 RPC `ListFiles`）：按三组折叠——便签（显示标题）、
  工作台设置（config.json）、模块状态（state/ 各文件显示模块中文名）；每行尾给
  "最后修改时间 · 版本数"。默认展开"便签"组（机主主场景），其余折叠计数。
- **右栏 = 该文件时间线**（新 RPC `FileHistory`）：每行一个"该文件有变化的版本"，
  行上直接长三个动作：**展开对比（diff）/ 恢复此版本 /（删除事件行）恢复被删内容**。
- **顶部「全部版本」= 保留快照为轴视角**为次级入口：点开即现有版本表 + 预览弹窗
  （RPC 全保留不删），服务"看看某天整体改了什么"与跨文件对照；拍板点 P1。
- **两种模式共用同一 IA 与同一组 RPC**（§0.2-③），备份模式仅在变化徽标处降级
  （§6），不再有"另一副面孔"。
- 窄屏（<640px）左右栏纵向堆叠：清单变横向 chip 滚动条，时间线满宽。

## 2. 文件名字映射数据通路（实读可行性结论）

| 对象 | 通路 | 可行性与出处 |
|---|---|---|
| `memo/<id>.md` → 便签标题 | frontmatter 现成 `title:` 行（`filestore.go:35` EncodeMemo / `:90` Decode 认该 key），**盘上与历史 blob 里都自带**，零新账本。快照侧新增 `SetMemoTitleResolver(func(relPath) (title string, ok bool))` 装配根注入（与 `SetMemoRestorer` 完全同款 DI 先例，app.go:606），避免 snapshot→memo 包引用 | ✅ 可行。已删便签的标题：blob 还在 git 里，diff RPC 返回的旧文本人话解析即可，无需落任何新账 |
| `config.json` → "工作台设置" | 静态单行 | ✅ 可行 |
| `state/<name>.json` → 模块中文名 | 各模块 `module.go` 的 `ModuleInfo.ID/Name`（如 memo=「极客随手记」、frpc=「frpc 联调」、quickmenu=「快捷菜单」）——装配根经 `registry.List()` 可得，注入快照侧或前端映射表。**例外清单（实扫 state/ 文件名 33 个 json 标签所得，不可纯约定推导）**：`projects.json`→frpc（frpc store 落盘名不是模块 ID）；`wsl-portproxy/wsl-install-pref/wsl-usbipd.json` 三文件→一个 WSL2 模块；`history.json`→统一历史（公共件非模块）；`memo.json`→旧库回落脚本（正常不存在，出现时兜底显示"便签旧库"） | ✅ 可行，代价 = 一张 ~5 行的例外表。其余 27 个 `<id>.json` 按约定命中 |
| `config.json` 内部 key → 中文 | **无现成通路**：全部设置项中文名以模板文案散在各设置分区 .vue 里，无机器可读注册表 | ❌ 不可行（零改动前提下）。需要新账本件：一份**前端 top-level key → 中文名映射表**（AppSettings 顶层实读约 17 个键：theme/accent/language/autoStart/minimizeToTray/logRetainDays/modules/lanRemarks/trayMenu/quickMenuTwoTier/historyOcrFullText/wechat/wechatAccounts/webAppEntries/snapshot 等）。嵌套数组（trayMenu 条目、webAppEntries 等）**不做逐项名**，摘要计数化（§5）；此表放前端（贴近文案源、随设置分区演进顺手改），Go 端不复制 |

## 3. 新增 RPC 面（增量、不破坏既有契约）

engine 接口只加一支方法，两引擎各自实现；服务层三张新 RPC 全部经现有
`engineReady + checkRevisionID + normalizeWhitelistPath` 三闸：

```go
// engine 新增（models.go）：
// fileHistory 返回该文件有变化的版本（新→旧，含变化类型），上限 limit≤50。
fileHistory(ctx context.Context, relPath string, limit int) ([]FileRevision, error)

type TrackedFile struct {
    Path       string `json:"path"`       // 白名单相对路径（RPC 往返用）
    Display    string `json:"display"`    // 中文名（§2 映射，映射不到回落文件名）
    Group      string `json:"group"`      // "memo" | "config" | "state"
    Revisions  int    `json:"revisions"`  // 该文件的历史条数（受 50 版观察窗约束，如实标注口径）
    LastChange string `json:"lastChange"` // RFC3339
    Alive      bool   `json:"alive"`      // 磁盘上当前是否还在（false=已删除，仍可见历史）
}
type FileRevision struct {
    RevisionID string `json:"revisionId"`
    Time       string `json:"time"`
    Status     string `json:"status"`  // A/M/D/R（备份模式恒 M，§6）
    Summary    string `json:"summary"` // 自然语言摘要，可空→前端回落 status 词表
}
type FileDiff struct {
    Path, Status              string
    Old, New                  string  // ≤512KB 各截断
    OldTruncated, NewTruncated bool
    Summary                   string  // 该版人话摘要
}
```

| RPC | 实现要点 | 备份模式实现要点 |
|---|---|---|
| `ListFiles() ([]TrackedFile, error)` | `enumerateWhitelist` 现状 ∪ 最近 50 版 `git log --name-only` 聚合（含已删文件）；标题经 resolver | manifest ∪ 相邻差集里消失过的路径（Alive=false） |
| `FileHistory(path, limit) ([]FileRevision, error)` | `git log --follow -z --max-count=N --name-status -- <path>`（--follow 吃 R 改名，旧名一路追溯） | 遍历时间戳目录（旧→新）做相邻 manifest 指纹差集，收集该路径的 A/M/D 事件 |
| `DiffFile(id, path) (FileDiff, error)` | New=`git show id:path`（D 则空）；Old=`git show id~1:path`（A 则空；R 用旧名）。首版无父 → Old 空即可 | 该目录文件 + 前一份含该路径的目录文件（全量拷贝天然随处可取） |
| `GetStatus` 微扩 | **零字段变更**：backup 模式下 RevisionCount 本来就如实返回，纯前端解封 `mode==='git'` 门（连带改 spec:69-77 的钉） | 同左 |

**不新增**：恢复侧任何 RPC（删除文件恢复 = 前端从 `FileHistory` 取"最后存在版本"的
ID 调现有 `RestoreFile`）；`RevisionDetail/PreviewFile` 原样保留服务"全部版本"视图。
绑定再生成走 `frontend/bindings/hanxi/internal/snapshot/`（wails 工具链，随批 `task check`）。

## 4. 每版 diff 呈现

- 后端只回 old/new **原样文本**（复用 512KB 截断纪律），**行 diff 在前端算**：
  新纯函数件 `frontend/src/utils/textdiff.ts`（LCS 统一式行对比，产 hunk 列表：
  等/加/删/改 + 连续未变段折叠为"… N 行未变…"），零第三方依赖 + vitest 穷举
  （空 old、空 new、截断尾行、CRLF 原样）。放前端算的理由：git 模式后端已有
  show 双读，diff 属观感计算，不进 15s RPC 超时预算，也不给双引擎各写一份。
- 渲染：GitHub 式 unified 单栏（删红/加绿底取工作台状态色降透明度，等宽字体，
  沿用现 `.preview-body` 皮扩展），**替换**现在的"内容"盲看按钮（拍板点 P2 双栏案）。
- 大文件保护：行数 >2000 时只渲染前 N 个 hunk + "展开完整对比"，防 LCS 卡顿。
- config.json/state JSON 的 diff 行级语义不透明（一行 = 一个字段还好，压缩重写会
  整篇花）：摘要行兜底（"工作台设置：+3 −1 行"），并保留点开看全文对照的退路。

## 5. 自然语言摘要的生成边界

**后端已有、可直接拼（零新账本）**：
- 变更类型 A/M/D/R（git porcelain/name-status 现成）；
- 便签标题（frontmatter，盘上与 blob 双源，§2）；
- 备份模式相邻 manifest 差集（读时算，份数 ≤30 成本可忽略）；
- 版本时间、revision 数、diff 行计数（DiffFile 现算）。

**需要新账本字段/新件**：
- **git 模式 commit 摘要升级为整句人话**（"删除了便签《购物清单》；修改了 快捷菜单、
  文字识别；工作台设置 +3 −1 行"）：写在 commit message 里——message 即账本，
  **无 schema 变更**；代价是**只有新提交**享受，旧版本前端按现格式
  （`parseGitLog` TrimPrefix "checkpoint: "）回落渲染文件名串，UI 如实不装新。
  实施细节一个：现 `commit(files []string)` 丢失状态位，需 engine 内部把
  `changes()` 的 XY 状态带到摘要生成（gitEngine 可在 commit 事务内重取 status
  缓存，接口不加参数，避免双引擎签名 churn）。
- **备份模式摘要读时计算，不引入 manifest v2**：manifest.json 现为
  `map[string]string` 且 `validManifest` 严格校验（backup.go:360），加字段要动
  校验链与存量兼容——收益不值。读侧用 §3 的事件聚合现算"N 新增 · M 修改 · K 删除"
  级别摘要（备份模式无 A/D 细节事件时恒"全量备份 N 个文件"如实）。
- **便签摘要只到标题不到正文**；标题级信息本就是盘上明文（PLAN_SNAPSHOT Q1 已裁
  masked 条目照常入库），摘要显示标题 = 与 blob 明文同强度，**不新增泄露面**，
  一句写死免得实施时重新纠结。
- **config.json 逐项中文**受 §2 最后一行边界约束：v1 只到 top-level 键名映射
  （新前端表），嵌套数组一律计数化。做到逐项需要"设置项注册表"级别的新工程，
  超出本排期定位，不做。

## 6. 备份模式（本机备份）呈现与文案

- **解封浏览**：`refresh()` 去掉 `mode==='git'` 门（两模式同拉同渲）；
  spec 对应用例反转到"备份模式同样拉列表"。
- 变化徽标：备份模式无逐文件 A/D（恒 M），时间线行给中性徽标「快照」，
  不硬造"修改/新增"假信号——诚实优于对称。
- 状态 chip 文案（大白话 + 技术细节折叠，沿 N22 文案纪律）：
  - 琥珀 chip：`本机备份模式 · 每次留整份拷贝（无逐文件对比）`；
  - 分区卡内一行：`你的历史都在下面这个文件夹里，每份按日期命名，打开就能手工找回文件。
    [打开备份文件夹]`——把排期要求的"打开文件夹找"从"降级像坏了"翻正为**有面子的
    第二通道**，版本列表是主通道；
  - "为什么没有逐文件对比？未检测到可用的 Git"收进 details/tooltip，不占正文。
- 滚动 30 份上限如实写进摘要区（"最多保留最近 30 份"），防"历史无限"错觉。

## 7. 分批实施表

| 批 | 内容 | 文件清单 | 验收 | 风险与对策 |
|---|---|---|---|---|
| **批 A 后端读面** | engine 加 `fileHistory`；新 RPC `ListFiles/FileHistory/DiffFile`；`TrackedFile/FileRevision/FileDiff` 模型；memo 标题 resolver 注入；绑定再生 | `internal/snapshot/{models,git,backup,service}.go`、`internal/app/app.go`（DI 一行）、`frontend/bindings/hanxi/internal/snapshot/*`（再生） | `go build/vet/test` 绿；双引擎 fixture 表驱动（git 真仓 + 假备份目录树）；路径闸门三 RPC 穿越用例钉死（`../`、绝对路径、非白名单全拒）；`--follow` 改名追溯用例 | git log 参数拼错=静默空列表 → 每 RPC 断言"非零结果路径"用例；ListFiles 全史聚合 O(50 版×文件数) 在 15s ctx 内，加计数上限 |
| **批 B 前端双栏重做** | SnapshotSection 重排（清单+时间线+展开对比）；`textdiff.ts`；「全部版本」次级视图收纳现有表+弹窗；恢复按钮上长文件行；热/重启生效常驻徽标；spec 重写（偏好四行用例保留、模式门反转、恢复链改走时间线行） | `frontend/src/views/settings/SnapshotSection.vue`、`frontend/src/utils/textdiff.ts`（新）、`__tests__/SnapshotSection.spec.ts`、textdiff.spec.ts | `vue-tsc` + vitest + 生产构建 + ESLint 绿；640px 纵向堆叠无溢出（组件级断言）；D 行「恢复被删内容」链在备份/git 两模式 fixture 下均可发起 RestoreFile 且带正确"存在版本"ID | 单文件体积膨胀（现 404 行 → 预计 700+）：拆 `SnapshotFileList.vue`/`SnapshotTimeline.vue` 两子件同目录，皮仍走全局原子类 |
| **批 C 摘要与映射账本** | commit 摘要整句化（含状态带参）；前端 state 文件名例外表 + config top-level 中文名表；备份模式读时摘要；chip/文案按 §6 | `internal/snapshot/git.go`（summarize）、`internal/snapshot/backup.go`（manifest 差集读时算）、前端两张新表 + 用例 | 新提交摘要为人话、旧提交回落渲染有测试钉；例外表覆盖 §2 全清单（projects/wsl×3/history/memo.json）；旧测试 fixture 摘要（"config.json, memo/a.md"）渲染不崩 | 只有新 commit 是新摘要——文档与 UI 不承诺历史换脸；validManifest 链**零触碰**（备份读时算不动写侧） |
| **批 D 真机验收** | §8 清单跑 + 观感裁决 | 无码（或观感微调小批） | 见 §8 | — |

每批独立编译绿、可各自成原子提交（`feat(snapshot)` / `refactor(frontend)` /
`feat(snapshot,frontend)` 中文规范）；批 B 依赖批 A 绑定，批 C 与批 B 可并行但
同文件冲突面（SnapshotSection.vue vs git.go）正交。

## 8. 真机验收清单

1. 便签主场景全链：改一篇 → 等自动快照 → 时间线该便签行"修改了内容"→ 展开 diff
   红绿行正确 → 恢复即时生效（随手记页免重启看到旧文）；
2. 删一篇便签 → 时间线出现「删除」事件行 → 「恢复被删内容」找回原文件与标题；
3. 改一处设置（如主题）→ "工作台设置"行 diff 呈现 + chip 名正确；config 恢复弹
   "重启生效"、重启后 `ApplyPendingRestores` 落地、pending 包被清；
4. 模块状态文件（快捷菜单条目改动）→ 中文名命中 §2 例外表全部四类（frpc 项目、
   wsl 三件、history、正常 `<id>.json`）；
5. **拔 git 场景**（PATH 遮蔽/存根机）：重启 hanxi → 备份模式 chip 文案正确 →
   版本列表/时间线/diff（快照徽标）/恢复全部可用 → 「打开备份文件夹」落
   `.snapshots/backup/`；装回 git → 下次启动自动回 git 模式，旧备份目录不混入列表；
6. 超 50 版与 >512KB 截断提示如实；390px 窄屏纵向堆叠无溢出；
7. 「立即快照」在双栏新布局下仍一钮可达；连续失败通知去重链未回归。

## 9. 机主拍板点（9 项）

| # | 问题 | 选项与建议 |
|---|---|---|
| P1 | 「全部版本」（快照轴）视图去留 | A 收为顶部次级入口（建议，旧用户"看某天整体"仍有路）；B 彻底删掉只留文件轴（更纯粹，但版本级恢复跨文件时没入口） |
| P2 | diff 版式 | A 统一单栏（建议，同 GitHub）；B 新旧双栏并排（对照直观但半宽挤，长行 JSON 难看） |
| P3 | 左栏清单范围 | A 全量三组含 30 个模块状态文件（建议，折叠收纳）；B 只放便签+工作台设置，模块状态藏进"更多"（更清爽，但 frpc/quickmenu 配置回滚恰是刚需） |
| P4 | **推翻 Q3 旧拍板**：备份模式解封完整浏览+恢复（当年裁"仅打开目录"） | 建议解封（后端能力本就全量在，解封只是撤前端门）；若你仍想保持降级简单，UI 落差病灶就不可能除 |
| P5 | config.json 中文名粒度 | A top-level 键表 + 嵌套计数化（建议，本方案默认）；B 追加"设置项注册表"新工程逐项到字段级（另立项，不进 N33） |
| P6 | 「恢复此版本」按钮位置 | A 时间线每行直挂（建议，配 danger 确认框防误点，恢复链现成）；B 展开 diff 后再给（多一步但每点必读 diff） |
| P7 | 旧版本的旧式摘要（文件名串） | A 如实回落显示（建议）；B 旧行整行显示"该版本留有人工摘要前的旧记录"（掩耳，不建议） |
| P8 | 快照模式 chip 里"Git"一词 | A 大白话为主、Git 收 tooltip（建议）；B 直书"未检测到可用的 Git"（现文案，对技术用户更准确） |
| P9 | 跨文件"整版恢复"（一键把某全家福的所有文件拉回） | 本方案**不做**（恢复语义全在文件行）；若你要，另出 RPC 与确认流，估 +0.5 天，默认否 |

## 10. 排期诊断外的关键修正汇总（防实施时按旧账修）

1. 摘要是**文件名串**不是路径串（baseNames 已剥路径）——问题在无语义，不在长度；
2. 备份模式**后端全能力已在**，落差是前端门 + Q3 旧裁决，拆门即愈；
3. **删除文件不可恢复**是功能洞（D 行无恢复钮 + PreviewFile 必失败），必须随批收，
   修法零新 RPC（FileHistory 定位存在版本 + 现有 RestoreFile）；
4. 恢复"重启生效/即时生效"双语义是后端既定事实，UI 从点了才说改为行上常驻标；
5. 版本观察窗 50（git log cap）与备份 30 份滚动是两个独立上限，文案都要如实露出。
