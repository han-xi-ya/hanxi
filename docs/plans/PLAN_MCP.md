# PLAN_MCP —— MCP / Skill AI 接入可行性分析与开发计划

> 日期：2026-09-17 · 状态：方案评审稿（未动任何代码）
> 需求：给 hanxi 增加 `hanxi mcp` headless 子命令（stdio MCP server），首批只暴露只读工具
> （everything 全盘搜索 / hanxi-ocr 识图 / envcheck 环境体检 / memo 检索），含一键安装到
> Claude Code / Codex / Cursor 的受确认向导；同一入口兼做 CLI（`--list` / `--call`）供 Skill 消费。

## 1. 结论先行

1. **可行，无架构性障碍**。入口分流有现成先例（`-mode killhelper` 在 `app.New` 之前短路，
   `cmd/hanxi/main.go:33-36`）；四个目标服务的构造链全部不依赖窗口/托盘/全局钩子，且事件出口
   都带 `application.Get()` nil 守卫，headless 不会炸（见 §2）。
2. **选型**：`github.com/mark3labs/mcp-go`。本机 module cache 已有 v0.31.0 / v0.41.1 完整包
   （离线即可起步），上游最新 v1.1.0（2026-09-15）仅有 .mod 缓存——**v1.x 的 API 需联网确认后再定升级**
   （v1.0 起 jsonschema 依赖有换，疑似 schema 侧破坏性改动）。API 三件套已实测存在：
   `server.NewMCPServer`（server.go:335）→ `AddTool`（server.go:528）→ `ServeStdio`（server/stdio.go:710），
   注解支持 `WithReadOnlyHintAnnotation`（mcp/tools.go:832-836）。依赖树纯 Go（uuid/jsonschema/cast），与 Wails 无冲突。
3. **必须先纠正两个需求设定里的事实偏差**：
   - memo **不存在"逐库"概念**——单文件 `<DataDir>/memo.json`（`internal/modules/memo/module.go:1-2`，
     全包 grep 无库列表/库目录）。「逐库授权」退化为「整库授权 + 敏感遮罩项脱敏」，需用户确认（§8-2）。
   - everything 的内嵌搜索**不是无前提只读**：ES.exe 经窗口消息 IPC 直连**正在运行的 Everything 实例**
     （`internal/modules/everything/search/es.go:3-4`）；且 es.exe 缺失时会**静默联网下载**官方 zip
     （es.go:55-147），服务层 `Search` 无实例时还会**懒拉起 Everything.exe**（service.go:313-318）。
     工具契约要么如实接受这些副作用，要么首版只做"有实例才搜、无实例报错指引"（§8-3）。
4. 唯一需要真机验证的工程风险：生产构建 `-H windowsgui`（`build/windows/Taskfile.yml:65`）下无控制台，
   MCP 客户端经管道 CreateProcess 拉起时 stdin/stdout 句柄仍继承可用，但**从 cmd 手敲测试看不到输出**——
   验收必须包含"管道实测"项（用 dev 构建或 go run 直测 + 一次 release 管道冒烟）。
5. 改用户 AI 客户端配置属项目红线：**默认不装、preview→确认→备份→原子写→回读校验→失败回滚、
   冲突永不静默覆盖**。仓内已有完整先例可套：wsl `.wslconfig` 的"闸门→写前备份 .hanxi.bak→原子替换→复验"
   （`internal/modules/wsl/hostconf.go:130-176`）+ jsonstore 原子写核（`internal/jsonstore/jsonstore.go:52-87`）。

## 2. 现状与证据（关键问题逐答）

### 2.1 入口分流与 headless 装配

- `cmd/hanxi/main.go` 唯一 GUI 入口：`flag.Parse` 后先分流 killhelper（main.go:33-36），正常路径才走
  `RegisterEvents`（:44）+ `app.New`（:46）。`hanxi mcp` 在 `flag.Parse` 之前加
  `os.Args[1] == "mcp"` 短路即可（flag 包遇首个位置参数即停，不冲突）；位置参数形态优于 `-mode=mcp`，
  因为客户端配置里 `args:["mcp"]` 语义自然，且 `--list/--call` 双身份顺理成章（先例：`cmd/envprobe` 独立 headless main）。
- **单实例锁不会冲突**：Wails `SingleInstance` 在 `application.New` 内才建（`internal/app/app.go:364-373`），
  MCP 分支根本不进 `application.New`，与运行中的主程序并存。
- **go:embed 前端**：`embedassets.go:9-10` 的 dist 字节随 exe 常驻，但只有 `app.New` 的
  `AssetFileServerFS`（main.go:46-47）会消费；headless 分支不触发 WebView 与资产服务，也不调
  `RegisterEvents`（ocr/everything 等的事件在 MCP 进程里未注册，但它们的 emit 全部 nil 守卫：
  `ocr/service.go:191-194`、`everything/service.go:74-86` 区段、`notify/hub.go:69-73`、`memo/service.go:283-287`）。
- headless 装配链复用件：`settings.InitPaths`（便携 `hanxidata/` 探测，`settings/paths.go:45-103`，同 exe 同数据根，
  MCP 与 GUI 看到同一份数据）、`settings.NewStore` 只读（`settings/store.go:103`；**MCP 永不 Update**）、
  `windows.New()`（`platform/windows/platform.go:24-32`，纯构造无 IO）、`extapi.Registry` 只注册 4 个目标模块，
  借用其 **enabled 门禁 + EnsureActive 懒初始化**（`registry.go:100-121`）与 `ShutdownAll`（registry.go:317）收尾——
  GUI 里停用模块 = MCP 同步不可用，语义一致。**纪律：MCP 进程任何代码不得 print 到 stdout**（stdio 协议通道），
  以单测断言守卫。

### 2.2 四个工具的真实依赖（均已实读源码确认）

| 工具 | headless 初始化 | 调用面 | 依赖与副作用 |
|---|---|---|---|
| envcheck | ⭐ 最低，`NewEnvCheckService(nil)` 即可（service.go:43；plat 仅作 OpenURL 且 nil 守卫） | `DetectAll()`（service.go:58，并发、单工具 5s 超时，纯本机 `LookPath`+版本命令） | OnInit 空。npmtool 探测器经 init 注册会进 DetectAll 结果（只读，无害）。在线版本对比（Get*Overview）出网，首版不暴露 |
| everything | ⭐⭐ `NewEverythingService(plat)`（只用 plat.Job，service.go:54-67） | `(*EverythingService).Search`（service.go:307-336）：确保实例→确保 es.exe→`search.Search(esExe,q,limit)`（search/es.go:156，10s 超时、上限 300） | 见 §1-3：懒拉起实例 + 组件缺失联网下载。结果元数据本地 stat 补齐（es.go:252+），`-export-tsv` 临时文件读后即删（es.go:168-175，防中文 GBK 乱码） |
| ocr | ⭐⭐⭐ `NewOcrService(plat)`（store=数据根 ocr.json，client 已显式 `Proxy:nil` 防系统代理污染，service.go:87-102） | `RecognizeImage(path)`（service.go:402-461）：绝对路径校验→`POST http://127.0.0.1:<port>/api/ocr`（默认端口 53120，store.go:13-14），35s 超时 | **不自动拉起**：离线只返回"请先启动服务"业务错误（service.go:431）；拉起需 `StartService`（:304，JobObject+端口预检）。组件发现三级：登记路径→exe 同级 `../hanxi-ocr/`→PATH（models.go:153-169），登记失效**报错不回退**。与主程序共用同一上游：端口占用+`/api/status` 契约判别为 external，无双起冲突 |
| memo | ⭐ 只读可完全绕开 Module：直接 `jsonstore.Load(<DataDir>/memo.json, &items)` | `Store.Load`（store.go:38）；损坏严格报错不静默清空（store.go:50，jsonstore ErrCorrupt） | 注意 `NewStore` 对缺省会**创建文件**（store.go:27-32 区段）→ MCP 用裸 Load 保零落盘。敏感遮罩 `IsMasked`（models.go:12）是用户标记的 API Key/Token 级条目，**输出必须脱敏**（§4.3） |

### 2.3 并发访问 hanxidata（双进程同开）

- 所有状态文件（memo.json / ocr.json / everything.json / config.json）写盘统一走
  **tmp + rename 原子替换**（jsonstore.go:52-87 公共核；settings 旧样板 store.go:406-429 同形态），
  rename 语义保证读侧永远看到完整旧版或完整新版，**不会读到半截**。
- 但**无跨进程锁**（进程内 mutex 而已）：两进程并发写 = 后写者整盘覆盖（lost-update）。
- 取舍结论：**MCP 子进程独立打开、严格只读**（本方案全部工具均只读成立）。不采用"经主程序回环 HTTP"
  通道：主程序可能未运行（MCP 场景常如此），引入固定端口+发现协议+生命周期耦合的复杂度远超收益；
  且即便未来引入，须遵守回环 `Transport{Proxy:nil}` 纪律（仓内已成规：ocr/service.go:92 注释、
  `instance/probe_port.go` 同款）。GUI 侧偏好写盘后 MCP 的内存快照会陈旧——可接受（每次会话短命，
  必要时 per-call 重读文件，见 §4.2 access 同款手法）。

### 2.4 安全链路：输出进云端模型上下文

MCP 工具返回值会原样进入远端大模型上下文——**等价于"用户授权下向云端披露"**。据此定红线：

- 永不暴露：portkill/frpc 等提权/写操作（MOOTOOL_ANALYSIS.md:17 既定纪律）、`EverythingService.OpenTarget`
  （以系统关联程序打开任意文件 = 执行链）、任何写盘/删除/进程拉起类方法、OCR 对话框方法
  （`PickImageDialog`/`BrowseServiceExeDialog` 依赖 `application.Get()` 且 headless 会 nil 崩，service.go:466/563——本就不该暴露）。
- 逐工具暴露面：搜索（全盘路径披露）、识图（任意可读图片内容 + 提示注入面）、体检（软件版本指纹）、
  便签（个人数据；IsMasked 条目正文默认 `[已脱敏]`，标题保留可定位）。
- 尺寸与预算：单结果 ≤300 条（searchResultLimit=300 沿用）、文本载荷上限 1MB（超限截断+`truncated` 标志，
  学 MooTool「预算耗尽 ≠ 无结果」语义）；工具 description 明示授权要求与只读性。

### 2.5 一键安装面（目标文件与编辑方式）

| 客户端 | 文件 | 格式 | 编辑策略（Go 重设计） |
|---|---|---|---|
| Claude Code | `~/.claude.json`（支持 CLAUDE_CONFIG_DIR） | JSON | `map[string]json.RawMessage` 顶层键保留，写 `mcpServers.hanxi`（`{command: hanxi.exe 绝对路径, args:["mcp"]}`）；缩进跟随探测；解析失败=拒绝写入 |
| Codex | `~/.codex/config.toml`（CODEX_HOME） | TOML | **末尾追加托管区块**（`# >>> hanxi mcp >>>` 成对哨兵），写后用仓内 BurntSushi/toml 复验语义；卸载按哨兵精确匹配整块删除，出现 0 或 ≥2 次拒绝（防被重排后误删）；绝不重排用户文件 |
| Cursor | `~/.cursor/mcp.json` | **JSONC**（可含注释） | 首版 fail-closed：检测到注释/解析失败即拒绝自动写、给出手动配置片段与预览；保注释外科手术编辑（等价 jsonc-parser applyEdits）列为二期，需联网评估 Go JSONC 库 |

流程复用 wsl/hostconf.go 三步防线（备份→原子写→读回复验）+ MooTool 语义四件：
preview（含 before/after diff，只回显将写入内容、不上传既有配置）→ 用户确认 → `assertUnchanged`
（预览后被第三方改过则整体拒绝）→ 备份命名 `<file>.hanxi-bak-<时间戳>`（仓内先例 `.hanxi.bak`、
`.corrupt-<ts>`，app.go:226-237）→ 失败逆序回滚（仅当文件仍等于我们写的）。
**所有权 receipt**：`<DataDir>/mcp/install.json` 记录上次写入指纹；状态四态 not-installed / installed /
needs-repair / conflict，**同名条目被用户改过时安装与卸载都拒绝**，绝不静默覆盖。安装前自检：以管道
spawn 自家 `hanxi mcp` 跑 `listTools` + 一次 envcheck 真调，先验证后写配置。

### 2.6 工程面（构建/绑定/目录）

- 构建零改动：Taskfile 只打 `./cmd/hanxi`（build/windows/Taskfile.yml:53），子命令同一 exe，
  NSIS/便携包/快捷方式不受影响（快捷方式无参数 = 进 GUI，正确默认）。
- 绑定生成器扫 `-i ./cmd/hanxi ./internal/...`（build/Taskfile.yml:183）但**只为注册成 Wails service 的类型
  产 TS 绑定**（实证：internal 下 jsonstore/launcher/ringbuf 等无 service 包均无 bindings 产物）→
  `internal/mcp/` 不注册 service 即零绑定漂移，`task verify:bindings` 门禁天然安全。
- 目录落位：新建 `internal/mcp/`（无命名冲突，全仓 mcp 字样只在文档/注释）。包注释即 ADR（项目文化，
  如 extapi/module.go:1-13、ocr/module.go:1-8）：写清"只暴露只读工具、stdout 即协议、与主程序并存的
  只读承诺"三条决策。子包：`mcp/tools_*.go`（各工具注册，瘦包装 service）、`mcp/access.go`（授权文件）、
  `mcp/install/`（三客户端编辑引擎，纯函数+临时目录全测）——`install` 独立包便于 GUI 向导与 CLI 共用。
- `go vet ./internal/...` / `task check` 全绿为每提交门禁（Taskfile.yml:69-84）。

## 3. 方案设计（汇总）

```
hanxi mcp                 # stdio MCP server（供 Claude Code/Codex/Cursor）
hanxi mcp --list          # 打印全部工具 JSON schema（Skill 免 MCP 消费）
hanxi mcp --call NAME < args.json   # stdin 读参、stdout 出 MCP result、isError→非零码
hanxi mcp install --client claude|codex|cursor [--preview] [--yes]   # 红线：默认仅 preview，--yes 才写且仍走备份/复验
hanxi mcp auth            # 交互式授权备忘（主要入口仍是 GUI 设置分区；CLI 写 access 文件需二次确认）
```

装配：`mcp.Run` = InitPaths → settings 只读 → slog 落文件（stderr 兜底）→ windows.New →
Registry 注册 {envcheck, everything, ocr, memo} 四模块（enabled 沿用 config.json）→ mcp-go server
按 **access.json ∩ 模块启用** 组工具表 → ServeStdio；会话结束 ShutdownAll（一切子实例随 JobObject 收）。
未授权工具**仍在列表中但调用报错并指引开关**（deny-by-default + 可发现性，MooTool 语义）；
access.json **每次调用重读**（撤销即时生效，≤16KB、损坏 fail-closed、路径锚定 DataDir）。

## 4. 任务分解（commit 粒度；每提交含测试、过 `task check`）

| # | 提交 | 内容 | 验收 |
|---|---|---|---|
| C1 | `chore(deps): 引入 mark3labs/mcp-go v0.41.1` | go.mod/go.sum 变更 | verify:tidy |
| C2 | `feat(mcp): headless stdio 分流与最小 server 骨架` | main.go 分支、internal/mcp 装配（含 ADR 包注释）、access 骨架、envcheck 只读工具、stdout 污染守卫单测、mcptest 进程内往返 | release 构建管道冒烟（实测 windowsgui 假设，§1-4） |
| C3 | `feat(mcp): everything 全盘搜索工具` | 工具注册、limit/truncated、错误指引文案；无实例行为按 §8-3 拍板结果 | 中文路径/长路径/300 截断用例 |
| C4 | `feat(mcp): ocr 识图工具与私发组件转发` | RecognizeImage 包装 + 服务离线指引；绝对路径/尺寸上限校验 | 无组件环境返回指引而非崩溃 |
| C5 | `feat(mcp): memo 只读检索与敏感遮罩脱敏` | 裸 jsonstore.Load（零落盘）、关键词过滤、IsMasked 正文替换 | 遮罩条目泄露回归测试 |
| C6 | `feat(mcp): CLI 双身份 --list/--call` | 复用同一工具注册表；BOM/编码防护（PowerShell stdin 会带 UTF-8 BOM——MooTool 实测坑，TextDecoder fatal 等价 Go 处理） | 脚本级 e2e：spawn 自身跑通 |
| C7 | `feat(mcp-install): 三客户端配置编辑引擎（纯逻辑）` | JSON/TOML 托管块/JSONC 判定、receipt 四态、preview/backup/原子写/复验/回滚 | 临时 HOME 全矩阵单测（冲突/重排/坏文件/符号链接/幂等零 diff） |
| C8 | `feat(settings): 「AI 接入」设置分区` | 工具授权开关 + 安装向导 preview/确认 UI + 绑定生成 | 前端 spec；红线：无确认不落盘 |
| C9 | `test(mcp): 安全审查与端到端验收` | Claude Code 真机三客户端各接一次；红队自查清单（路径穿越/OpenTarget 缺席/遮罩/输出尺寸）；沉淀 TROUBLESHOOTING | 人工安全审查步骤，不可跳过 |
| C10 | `docs: PLAN_MCP 状态回写与 MOOTOOL_ANALYSIS 收口` | 文档 | — |

依赖关系：C2→C3..C6 串行于骨架，C7 独立可并行，C8 依赖 C7。C1-C5 即"首版 2 工具 MVP"。

## 5. 排期

- **阶段一（MVP，2 工具：envcheck + everything）**：C1+C2+C3 ≈ **3–4 人日**
  （含 windowsgui 管道实测与 schema 打磨；手动往客户端配置贴 JSON 即可用，不带向导）。
- **阶段二（完善版）**：C4+C5+C6+C7+C8+C9+C10 ≈ **6–8 人日**。
- 合计 **9–12 人日**。建议顺序：MVP 先跑通真实使用（自己拿 Claude Code 搜文件/识图两周），
  授权语义与安装向导按实测反馈再定稿——红线功能的确认 UI（C8）不抢跑。
- **与其他三功能（MOOTOOL_ANALYSIS.md:100-108 顺序）的依赖建议**：
  统一历史记录（建议 1）与 MCP 弱耦合——工具调用审计行后续一行接 history 即可，无需互相等待；
  devkit（建议 3）应排在 MCP 之后——纯文本小工具是零风险 MCP 工具增量（MooTool 7 件文本工具即现成清单）；
  memo 文件库化+快照（建议 4）与 MCP 的 memo 工具读写路径相冲——**MCP 先行**（当前单文件读取封装薄），
  库化落地时仅改 memo 工具内部取数，schema 不变。另：当前工作区仍有未提交的 ocr/settings 改动
  （轮盘截屏识别 + 设置分区），C2/C8 涉及同文件，**先收编工作区再开工**。

## 6. 授权模型细节（防"文案超前于实现"）

`<DataDir>/mcp/access.json`：`{"version":1,"tools":{"envcheck":true,"everything":false,"ocr":false,"memo":false}}`。
默认全 false（envcheck 也默认关，开关语义统一）；MCP 工具表 = access ∩ registry.IsEnabled。
GUI 设置分区与 `hanxi mcp auth` 是仅有的两个写入口。不暴露任何配置文件的写能力（安装向导产物
不算 MCP 工具面，属本地 CLI/GUI 行为）。

## 7. 踩坑预登记（写入包注释/后续 TROUBLESHOOTING）

① MCP 进程禁止一切 stdout 杂写；② ES.exe 依赖运行实例与按需下载两条副作用必须如实进工具文案；
③ OCR 登记路径失效即报错、不做静默回退（与 GUI 行为一致）；④ `-H windowsgui` 下手工 cmd 测试无回显；
⑤ PowerShell 管道 stdin BOM；⑥ 客户端配置写坏的备份/回滚/复验三件套缺一不可。

## 8. 开放问题（需用户拍板，未拍板前相应任务不启动）

1. **MVP 工具组合确认**：推荐 envcheck+everything（零私发件依赖、最有差异化）；ocr/memo 放二期。
2. **memo 授权粒度**：需求设定"逐库授权"不成立（单库单文件，§1-3）——接受"整库开关 + IsMasked 条目脱敏"，
   还是等 memo 文件库化重构后再暴露？
3. **everything 工具副作用档位**：A=直接复用 `service.Search` 全编排（含懒拉起 Everything.exe——实例绑在
   MCP 进程 JobObject 上、客户端会话结束即被回收，且首用可能触发联网下载 es.exe）；B=严格只读（无实例/缺
   组件即报错指引）。**推荐 B 起步**，A 作为 access 开关里的可选项二期评估。
4. **Cursor JSONC 注释保留**：一期 fail-closed（有注释即拒绝、给手动片段）是否可接受，还是要求一期就上
   保注释外科手术（需联网选型 Go JSONC 库）？
5. **安装面默认态**：写配置入口是否只留 GUI 向导，CLI `--yes` 是否开放？（两者都改用户文件，红线同级）
6. **mcp-go 版本策略**：一期锁 v0.41.1（离线可构建已证）；v1.1.0 升级是否值得等 v1 生态稳定？
7. **工具名与描述语言**：`hanxi_file_search` 式命名 + 中文 description（模型可理解）还是中英双注？

**决策回写（2026-09-17，用户拍板）**：1 = MVP 组合定为 envcheck + everything，ocr/memo 放二期；2 = 便签采用"整库总开关 + IsMasked 条目不下发"，不等文件库化重构；3 = 方案 B 严格只读，无实例/缺组件报错给指引，不代启动不触发下载；4 = 一期 fail-closed（配置含注释即拒绝自动改、给手动片段）；5 = 写配置仅保留 GUI 向导一条路，CLI `--yes` 不开放；6 = mcp-go 锁 v0.41.1，v1 生态稳定后再评估；7 = 英文工具名 + 中文 description。
