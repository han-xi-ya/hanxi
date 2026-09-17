# 统一历史记录公共包——可行性分析与开发计划

> 日期：2026-09-16 ｜ 依据：`docs/MOOTOOL_ANALYSIS.md` §2（P0）、`docs/FEATURES_OVERVIEW.md` §5.1（公共包下沉债务）
> 范围：`internal/history` 公共包 + 通用 Vue 面板 + 首批接入 ocr / portkill / envcheck。只增不改业务语义。

## 0. 结论先行

**可行，且成本明显低于收益。** 全仓所需的每一块地基都已有先例：原子落盘有 `internal/jsonstore`（25 个 store 已收口，`jsonstore.go:55`），"有 service 无 view 的公共能力"有 notify 直挂 Wails services 先例（`internal/app/app.go:336-338`），跨模块注入有 memo↔fileshare 装配根接线先例（`app.go:283`），裁剪有 notify 环形上限头插法（`internal/notify/hub.go:57-61`），脱敏有导出的 `logging.Redact`（`internal/logging/logger.go:25`），前端子 Tab 有 `MainTabNav`（25 个 view 复用），绑定漂移检查只需 `wails3 generate bindings` 后随源码提交。**无需引入数据库、无需改 registry 契约、无需新增路由**。
预估 7~9 人日，分 6 个可独立提交的 commit；建议先打通 ocr 全链路作为样板，再铺开另两个模块。

## 1. 现状与证据（五个关键问题的实地结论）

### 1.1 存储层

- 数据目录解析唯一入口 `settings.GetPaths().DataDir()`（`internal/settings/paths.go:143`）；便携模式即 exe 同级 `hanxidata/`（`paths.go:23-26,92-103`）。现状布局：顶层扁平 `config.json` + 每模块一个 `<id>.json`（memo/ocr/bcu… 实测 bin/hanxidata），另有 `logs/`、`versions/`、`runtime/` 子目录；**按模块建子目录有先例**（frpc：`<dataDir>/frpc/projects.json`，`internal/modules/frpc/store.go:21,31`）。
- 两种成熟落盘模式可选：settings 的"克隆→改→失败回滚内存"（`internal/settings/store.go:216-229`，`saveLocked` tmp+rename `:407-429`）与 jsonstore 公共核（`Load` 三分语义 `(ok,err)`：不存在/ErrEmpty/ErrCorrupt，`jsonstore.go:22-26,35`；`Save` = MarshalIndent→`tmp.<pid>`→**fsync**→rename，`:55-87`，比 settings 私有版多一步刷盘）。memo 为单 JSON 文件全量覆写的最简样板（`internal/modules/memo/store.go:58-62`，每次变更立即 `Save`，无 debounce——全仓一致，`internal/modules/recordly/store.go:69-77` 注释直书"立即落盘"）。
- 项目自身持久化**清一色 JSON，无任何内嵌数据库**（grep sqlite/bbolt/badger 仅命中外部托管工具注释）。损坏处置有降级先例：config.json 损坏→改名隔离 `corrupt-<ts>` + 默认值启动（`internal/app/app.go:224-238`）；严格防误覆盖先例：frpc `loadErr` 驻留禁止空库覆写磁盘（`internal/modules/frpc/store.go:26,46-48`）。
- **MooTool 对照**：Java/Electron 两侧都是 sqlite 单表按 `func_type` 列分桶，每桶 200 条、insert 后 `delete not in top-200` 裁剪，无去重（`TFuncHistory.java:5-17`、`FuncHistoryUtil.java:9,36-38`、`next/electron/main/historyRepository.ts:13-27,59-72`）。hanxi 体量小两个数量级，不需要 sqlite。

### 1.2 服务层与注册机制

- 模块契约在 `internal/extapi/module.go`：`Module` 接口 `Info/Nav/Services/OnInit/OnDestroy/IsInitialized`（`:106-121`）。**Registry 没有 GetService(id)**（`registry.go` 全量方法：Register/EnsureActive/List/AllServices/SetEnabled…），跨模块直调的惯例是**装配根类型断言 + 模块上的导出访问器**：`memo.Module.GetService()`（`memo/module.go:31-34`）→ `app.go:283` `fsMod.Service().SetMemoHook(mMod.GetService().QuickCreate)`。
- **"公共包型 service"有现成模式，不必注册为业务模块**：AppService 与 notify 就是"有 Wails service、无模块身份、无路由"，直接进 services 列表（`app.go:335-340`，`registry.AllServices()` 之外手工 append）。38 个模块目前全部有 Route，尚无"空 Nav 模块"先例——没必要做第一个。
- 绑定生成读的是 `application.Options.Services` 全列表，**与是否模块无关**：`wails3 generate bindings`（`build/Taskfile.yml:183`）；`task check` 的 `verify:bindings` = 重新生成后 `git diff --exit-code -- frontend/bindings`（`Taskfile.yml:50-54`），生成物必须入库（`frontend/bindings` 在 git 跟踪内，禁手改，`docs/FRONTEND.md:13`）。
- MCP：当前无基础设施，仅两处"本期不接"注释（`bili23/module.go:10`、`papertodo/module.go:18`）。history 服务进入 services 列表后，将来 `hanxi mcp` 可按装配根直引用复用同一实例。

### 1.3 前端层

- view 内子标签页已有标准件 `MainTabNav`（`src/components/ui/MainTabNav.vue:7-14`，v-model + tablist 无障碍），typical 用法 `EnvCheckView.vue:31,309-311,332-335`（`activeMainTab` ref + `v-show` panel）。设置页六分区**不是 vue-router**，是 `ROUTES` 表（`src/constants/navigation.ts:41-97`）+ `App.vue` 的 `activeRoute` ref 状态机（`App.vue:32,57-73,165-172`）——history 面板不碰这套，只在宿主 view 内部挂 Tab/弹窗。
- 弹窗契约：`open: boolean` + `confirm/cancel` + Teleport + 焦点陷阱（`src/components/ConfirmDialog.vue:4-17`）；Promise 式封装 `useConfirm()`（`src/composables/useConfirm.ts:38-42`，单例挂 App.vue 顶层）。嵌入式面板两种形态先例齐备：纯 props 上抛（`components/envcheck/OfficialVersionsPanel.vue:4-15`）与自持数据（`components/FrpcVersionsTab.vue:11`）。
- 设计系统：token 单一来源 `src/styles/tokens.css`（`--surface-panel`、`--color-text-muted`、`--state-danger`、`--radius-control`、`--motion-fast` 等），原子类在 `src/styles/components.css`（`.btn/.card/.tbl/.control-bar`），组件样式一律 var() 引用（范例 `MainTabNav.vue:35-38`）。
- 绑定调用无封装层，flat-service 直调：`import * as OcrAPI from '../../bindings/hanxi/internal/modules/ocr/ocrservice'`（`OcrView.vue:8`）。搜索框+列表的最佳参照 `src/composables/useEverythingSearch.ts:22-33`（350ms 防抖、searchSeq 丢弃过期响应）。
- 测试：Vitest 5 + @vue/test-utils + happy-dom；`vi.mock` 打桩 bindings 约定见 `src/views/__tests__/PortKillView.spec.ts:13-22`、`docs/FRONTEND.md:158,175`。

### 1.4 首批三模块接入点（save 插哪、回填到哪）

| 模块 | 后端记录点 | 事件流？ | 前端回填目标 | 敏感面 |
|---|---|---|---|---|
| ocr | `RecognizeImage` 成功组 `out` 前（`internal/modules/ocr/service.go:456` 附近），建议**命名返回值+defer 单点**统一记成败；`SnipAndRecognize` 内部复用 RecognizeImage（`snip.go:65`），来源区分记入 extra，勿二处插 | 全同步返回，事件仅状态广播 | `image = ref<ImageRef|null>`（`OcrView.vue:36`）或经 `InspectImage(path)`（先例 `OcrView.vue:308`） | **识别文本内容不可控**（聊天记录/密码）；绝不存 `PreviewURL` dataURL |
| portkill | `KillProcess`（`portkill/service.go:162`，:177 三分支前）、`KillProcessElevated`（`:202`，6+ return 需命名返回值+defer） | 全同步 | `inputPort = ref<number|''>`（`PortKillView.vue:14`）再触发查询 | 端口/PID/进程路径，无密钥面 |
| envcheck | 查询类 `DetectAll` 等 return 前（`envcheck/service.go:58`）；动作类 npm 装升卸**终态在 `npmtool/manager.go:135-149`**（runOperation 两分支，同步回执不含结果），事件 `envcheck:npm-tool-operation` | npm 操作异步终态 | 无自由文本框；回填 = 定位到对应工具/版本 Tab 动作 | 不读环境变量值；**别存 npm 原始日志行**（可能含私有源凭据） |

DI 注入方式与 `SetMemoHook` 先例完全同型：装配根构造共享 history store，`NewXxx(plat)` 加参或 `SetHistory(h)` setter（`app.go:288-326` 注册段）。

### 1.5 脱敏口径

`logging.RedactHandler` 只在 slog 出口生效（`internal/logging/logger.go:48-66`），history 落库不经过它；但其底层 `Redact(text string) string` 是**导出、无状态、预编译正则**函数（`:25`，口径：`(token|secret|password|passwd|sk|auth|authorization)[:=]值` 与 `bearer <token>`）。history 在 `Record` 构造入口对四字段各调一次即可复用同一口径——这是唯一正确做法，散在各模块各自脱敏必漏。

## 2. 方案设计

### 2.1 数据模型与存储（`internal/history/`）

```go
// models.go
type Record struct {
    ID       int64  `json:"id"`       // 全局单调递增（时间戳ms*1000+seq，重启续接取桶内max）
    FuncType string `json:"funcType"` // "ocr" / "portkill" / "envcheck"，常量集中定义
    Summary  string `json:"summary"`  // 一行摘要，空则取 input 前 40 字（对齐 MooTool 口径）
    Input    string `json:"input"`    // 截断上限 4000 rune，超限追加"…[truncated]"
    Output   string `json:"output"`   // 同上
    Extra    string `json:"extra"`    // 元信息，如 "snip|dialog"、"npm-install"
    CreatedAt string `json:"createdAt"` // RFC3339
}
```

- **落盘：单文件 `<DataDir>/history.json`，格式 `map[funcType][]Record`（桶头插，新→旧）**。理由：与 hanxidata 顶层扁平 JSON 现状一致、整体拷贝搬迁语义不变（frpc 注释论证）；桶数首批 3、devkit 后 ≲15，每桶 ≤200 且 input/output 有截断，单文件体量有硬上界（ worst case 个位数 MB）；每桶一文件要多文件生命周期与目录扫描，收益为零。
- **写并发**：`Store{mu sync.Mutex; buckets map…; path}`——内存权威副本，所有公开方法整体持锁，改完即 `jsonstore.Save` 全量原子覆写（200 条体量下与现有"立即写"惯例同成本；全仓无 debounce 先例，不做第一个特例）。写失败返回 error 并 `slog.Error`，内存不回滚（历史是尽力而为的副作用，不能让记历史失败阻塞/回滚业务操作——与 memo TogglePin 仅记日志同档，`memo/service.go:217-221`）。
- **裁剪**：头插后 `if len(b) > maxPerBucket { b = b[:maxPerBucket] }`，照抄 `notify/hub.go:57-61`；上限 `const maxPerBucket = 200` 编译期常量（对齐需求"约 200"）。
- **损坏策略**：取 frpc 严格档——`jsonstore.Load` 返回 ErrCorrupt 时置 `loadErr` 驻留，**禁一切 Save 防空库覆写存量**，同时返回错误让面板可见提示；不做隔离改名（那是启动关键路径 config 的待遇，历史损坏降级为空即可，不救不可恢复字节）。
- **脱敏**：`newRecord()` 构造器内对 summary/input/output/extra 统一过 `logging.Redact`，模块侧零责任。

### 2.2 服务与接口签名

公共包内两个角色（对齐 notify：包内即有 store 又有 service）：

```go
// store.go —— Go 侧被各模块直调（进程内，不经过 Wails）
func NewStore(dataDir string) *Store
func (s *Store) Save(r Record) error            // 补 ID/时间/脱敏/截断/裁剪
func (s *Store) List(funcType, keyword string) ([]Record, error) // keyword 空=全桶；四字段大小写不敏感 Contains
func (s *Store) Delete(id int64) error
func (s *Store) Clear(funcType string) error

// service.go —— Wails 绑定壳，薄转发（Save 不暴露给前端，历史由后端动作自动产生）
type HistoryService struct{ store *history.Store }
func NewHistoryService(store *history.Store) *HistoryService
func (h *HistoryService) List(funcType, keyword string) ([]history.Record, error)
func (h *HistoryService) Delete(id int64) error
func (h *HistoryService) Clear(funcType string) error
```

- **注册**：`app.go` services 列表 append `application.NewService(historySvc)`（notify 同位置，`app.go:335-340`），**不进 modulesToRegister、不进 ROUTES 表、无 Nav**。
- 装配根构造 `historyStore := history.NewStore(paths.DataDir())`，经构造参数或 setter 注入 ocr/portkill/envcheck 三 service（`SetHistoryHook` 先例 `app.go:283`）。
- 模块 funcType 常量由各模块自己的 models.go 定义字符串（如 `"ocr"`），history 包不 import 任何模块（防环）。

### 2.3 前端组件契约

`src/components/tool/HistoryPanel.vue`（`components/` 已有 `tool/` 域目录）：

```
props:  { funcType: string; maxHeight?: string }
emits:  apply: [rec: HistoryRecord]      // 双击行或"应用"按钮 → 宿主写回自己的输入区
内部:    自持数据（仿 FrpcVersionsTab）：import HistoryAPI from bindings，
        keyword 搜索 350ms 防抖 + seq 过期丢弃（仿 useEverythingSearch.ts:22-33）
UI:     搜索框 + 列表（summary/时间/extra 行，.tbl 原子类）+ 详情区（input/output 只读展示）
        按钮组：应用 / 复制输入 / 复制输出 / 删除本条 / 清空本桶（清空走 useConfirm() 二次确认）
样式:    全部 var(--token) + components.css 原子类，禁新造颜色
```

嵌入形态按宿主 view 现状就地取材：envcheck 已有 MainTabNav → `activeMainTab` 加 `'history'` 值；ocr/portkill 单页 → 页脚"历史记录"按钮开 Teleport 弹窗（复用 ConfirmDialog 的 open/Esc/焦点契约）。**不做跨桶"全局历史页"**（MooTool 也没有，且会诱导往 navigation 里塞无模块路由）。绑定生成后 `frontend/bindings/hanxi/internal/history/historyservice.js` + `models.js` 随 commit 提交，过 `verify:bindings`。

## 3. 任务分解（commit 粒度，均可独立编译+过 task check）

| # | Commit | 内容 | 测试 |
|---|---|---|---|
| 1 | `feat(history): 新增统一历史记录公共包` | models/store：分桶、头插裁剪、截断、Redact、严格损坏策略；包注释写明选型论证（jsonstore vs 每桶一文件、无 debounce） | `store_test.go`：裁剪边界（200/201）、脱敏命中、损坏 loadErr 禁写、并发 Save（`-race` 口径见 wsl 教训，Windows 无 CGO 则普通 test） |
| 2 | `feat(history): 接入 Wails 服务装配与依赖注入` | app.go services 直挂 + 构造 historyStore + 三模块注入缝（构造参/setter）；`wails3 generate bindings` | service 薄壳转发测试 |
| 3 | `feat(frontend): 通用历史记录面板组件` | HistoryPanel.vue + 复制/删除/清空/搜索/应用事件；vitest 组件级（vi.mock bindings） | `HistoryPanel.spec.ts` |
| 4 | `feat(ocr): 识别结果接入统一历史` | RecognizeImage 命名返回值+defer 单点 save（snip 来源标 extra）；OcrView 加 Tab/弹窗 + 回填 image | Go 侧 save 断言（桩 store）+ OcrView.spec 增例 |
| 5 | `feat(portkill): 查杀操作接入统一历史` | KillProcess/KillProcessElevated defer save；PortKillView 弹窗嵌入 + 回填 inputPort | 顺带补该模块 service_test 欠账（FEATURES §5.3 点名的零测试区，新增 defer 路径必须有测试兜底） |
| 6 | `feat(envcheck): 体检与 npm 操作接入统一历史` | DetectAll 等查询 return 前 save；npmtool.runOperation 两终态接包级 hook（只记摘要不存日志行）；EnvCheckView 加 history Tab | npmtool hook 测试 + EnvCheckView 增例 |
| 7 | `docs: 历史公共包架构决策与 FEATURES 同步` | ARCHITECTURE.md 公共包清行 + TROUBLESHOOTING（若有踩坑） | — |

顺序即依赖：1→2→3 可并行 2/3，4/5/6 依赖 1-3 合流；4 最先打通全链路当样板（含绑定同步走通一遍流程）。

## 4. 排期

**总量 7~9 人日**（单人）。里程碑：

- **M1（约 4 人日）**：任务 1-4 合入，ocr 端到端可用 = 公共包 + 面板 + 首模块全链路样板跑通，含绑定漂移门。
- **M2（约 2-3 人日）**：任务 5-6，portkill（含测试补账）、envcheck 接入。
- **M3（约 1 人日）**：任务 7 文档收口 + `task check` 全绿 + 真机回归（便携/标准双目录各开一次，确认 history.json 落点）。

与另外两个 MooTool 借鉴项的关系（对齐 `MOOTOOL_ANALYSIS.md` 建议顺序 1→2→4）：
- **MCP（顺序 2）**：history 先行是正序——`hanxi mcp` 未来的调用审计/上下文直接复用同一 store 与 funcType 词汇表，服务实例经装配根直引用取，无需先给 registry 扩 GetService。反向依赖为零，排期互不阻塞。
- **数据快照（顺序 4）**：与本包无耦合（对象是 memo/config 目录，机制是 git）；唯一共享约束是 hanxidata 布局——两者都只新增顶层文件/子目录，不冲突，可并行。
- **devkit（顺序 3）**：面板与 funcType 约定是 devkit"全部顺手接历史"的前提，本包是它的硬依赖，排在其前合理。

## 5. 风险与对策

1. **历史膨胀**：桶上限 200 编译期常量 + input/output 4000 rune 截断（ocr 全文、npm 长输出是最大两块）+ 绝不存 dataURL/日志行 → 单文件有硬上界。**extraData 刻意不做成 map**，纯字符串字段防被塞任意大对象。
2. **脱敏遗漏**：口径收口在 `newRecord` 构造器一处（模块侧无脱敏责任）；但 `Redact` 正则只认 `token/password=xxx` 形态，**OCR 识别出的自然语言密码、环境变量值裸文本拦不住**——见开放问题 Q1。
3. **单文件损坏**：jsonstore tmp+fsync+rename 使半写损坏概率极低；真损坏则严格模式停写（loadErr 驻留），历史只读不可新增，绝不空库覆写。代价：丢不了旧数据，换来"需手动删文件才能恢复写"。可接受（历史非关键数据，config 级隔离改名不需要）。
4. **写入热点**：三模块共用一个 mutex，单用户桌面频率（每分钟个位数次）远低于全量写成本；若 devkit 接入后出现卡顿，预案是"改桶文件"平滑迁移（版本字段已在顶层预留 wrapper 的机会——注意：一旦选单文件，未来分文件的迁移成本已在选型时比较过，仍占优）。
5. **portkill 测试欠账**：借接入补 defer 路径测试，不扩面（FEATURES 点名的零测试模块，本包只保证自己新增代码有测试）。

## 6. 开放问题（需拍板）

- **Q1 OCR 文本入库隐私档位**：a) 全文截断入库（MooTool 口径，默认）；b) 只存摘要+图片路径，不存识别文本；c) 设置页开关（默认 a）。倾向 c 的开关 + 默认 a——识别文本是回填复用的价值主体，但截图含聊天记录的真实风险存在。
- **Q2 记录面宽度**：查询类（QueryPort、envcheck 各 Overview）是否入历史，还是只记动作类（kill/npm 装升卸/识别）？只记动作类桶更干净，记查询类"上次的扫描结果可回看"更实用。倾向：动作全记，查询仅记 ocr/portkill 查询（轻量），envcheck Overview 不记（版本信息时效性强，历史无复用价值）。
- **Q3 portkill 提权失败的拒绝记录**：`KillProcessElevated` 用户拒绝 UAC 等失败路径记不记？倾向记（标 `extra="denied"`，便于回看"当时为什么没杀掉"），失败信息即 KillResult.ErrorMessage，无敏感面。
- **Q4 模块停用与卸载**：history 是公共包不随模块停用清桶（关掉 ocr 模块，ocr 桶保留）——确认此语义。
- **Q5 清空作用域**：面板"清空"只清当前 funcType 桶（对齐 MooTool `deleteAllByFuncType`），不提供跨桶全清入口——确认。
- **Q6 ID 语义**：双击/应用按 ID 定位（MooTool 按 id 重查回填）；hanxi 面板行内已有整条 Record，可直接用不回查——实现细节默认用行内数据，不单独拍板，若要求"应用前强一致重查"则加 `Get(id)` 方法。

**决策回写（2026-09-17，用户拍板）**：Q1 = 方案 c，设置页开关、默认全文入库；Q2 = 采纳建议（动作类全记，查询类仅 ocr/portkill，envcheck Overview 不记）；Q3 = 记，标 `extra="denied"`；Q4 = 桶随包保留、停用不清；Q5 = 清空仅作用当前 funcType 桶；Q6 = 行内数据直用，不加强一致重查。
