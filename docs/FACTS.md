# Hanxi 事实基准（Facts Baseline）

> **文档定位**：所有"会随代码演进而失真"的计数，**唯一权威出处**。任何文档、注释、对外文案要写这些数字，都以本表为准；本表由 `scripts/docsfacts` 从代码现推，不由人手抄。
>
> **为什么要有它**：2026-10-12 全项目文档对账发现，同一事实在不同文档里存着 3~4 套值（模块数 37/38/41/45、托管款数 25/28、MCP「四件/六件/八键」、前端门禁 94/101/112/653/1261），且架构文档里存在**描述了并不存在的东西**（"集成纯 Go Nmap 探针引擎 gonmap"，仓内无此引擎）。根因是数字散落在十余个文档里各处手写、无任何机器校验。本表 + `go test ./scripts/docsfacts` 把它变成结构性不可能。
>
> **纪律**：改了代码导致事实变化 → 跑 `go run ./scripts/docsfacts -emit` 取现推事实（该命令**刻意只打印到 stdout、不写盘**），人工合并进 `scripts/fixture/doc_facts.json` 的 facts 段，并同批改文档数字与本文。**禁止**为了让测试变绿而反向修改本文数值；本文只陈述代码现推结果。

---

## 一、核心计数（代码现推，非人记）

| 事实 | 现值 | 权威来源 | 复核命令（仓库根执行） |
|---|:---:|---|---|
| 业务模块总数 | **54** | `internal/app/catalog.go` 生成物 | `grep -c 'ID:' internal/app/catalog.go` |
| 自建侧模块数 | **22** | `moduleTotal − hostedVersions`（叙述列与能力口径在此互换对齐，见 §二） | `go run ./scripts/docsfacts -emit` 的 `selfBuiltModules` |
| 注册构成 | **52 常驻 + 2 条件**（`memo` / `clipboard`） | `internal/app/app.go` `modulesToRegister` | 读 `app.go` 装配段 |
| 托管款数 | **32** | catalog `hosted-versions` 能力 = 模块存在 `version/` 子包 | `for d in internal/modules/*/; do [ -d "$d/version" ] && basename $d; done \| wc -l` |
| 进程托管数 | **31** | catalog `managed-process` 能力 = 存在 `instance/` 子包 | 同上换 `instance` |
| 版本面内核委托 | **27** | `import hanxi/packages/go/artifact` 的模块数 | `grep -rl 'hanxi/packages/go/artifact' internal/modules/ \| sed 's\|internal/modules/\|\|\;s\|/.*\|\|\' \| sort -u \| wc -l` |
| 实例面内核委托 | **24** | `import hanxi/packages/go/supervisor` 的模块数 | 同上换 `supervisor` |
| MCP 工具数 | **12** | `internal/mcp/` 的 `mcp.NewTool(` 调用 | `grep -rho 'mcp\.NewTool(' internal/mcp/*.go \| wc -l` |
| MCP 授权键数 | **10** | `internal/mcp/server.go` `knownModuleIDs` | 读该 map（`envcheck/everything/ocr/memo/sysinfo/logs/portscan/lan/portkill/clipboard`） |
| 上游真图标数 | **28** | `internal/app/appicons/*.png`（台账 `sources.go`，`sources_test.go` 逐枚解码锁死） | `ls internal/app/appicons/*.png \| wc -l` |
| Go 测试文件数 | **532**（`internal/`）/ **562**（全仓） | `git ls-files`（只数已跟踪文件；2026-10-31 现取·二连（一键美化回归 +1；前值 531/561 同日早间、532/561 为终值——当日三改，逐笔账看 git log --diff-filter=A）。；2026-10-27 01:1x 由后端审查批次在补测批次入库后按右列命令现取，兑现"入库后必再变"待办）。上一版 **471/495**（并行会话自取）→ 本次 **+31 / +32**：来自本会话 2026-10-27 凌晨入库的 `edd52ec`（实例家族 68 用例）、`d60c6b5`（托管族 38 用例）、`65ed5c9`（wifi/quickmenu/papertodo 用例）与 `a7ff440`（wifi 假测试改写，净增 0）；逐笔归属要细拆看 `git log --diff-filter=A -- '*_test.go'`，本行不再自行加总他人账。⚠ 本行为**两条会话共同维护**（并行会话亦在更新本行与模块数行），冲突时以"现跑右列命令"为准，不许各自凭记忆推算。上一版 **502/527**（2026-10-27 实跑）→ 本次 **+30/+34**：2026-10-28~30 前后端审查批次与本线 AI 提取/常驻内核/聊天模块陆续入库（逐笔归属看 `git log --diff-filter=A -- '*_test.go'`，本行不自行加总他人账）；旧注"工作区仍有未跟踪测试文件 4 个"随该批入库销账。⚠ 本行不在闸门 9 个 facts 键内，绿≠表体绿（见 §四） | internal：`git ls-files 'internal/**/*_test.go' \| wc -l`；全仓：`git ls-files '*_test.go' \| wc -l` |
| 前端测试规模 | **205 spec / 2648 用例**（2026-10-31 实跑·终值二，`Test Files 205 passed (205)` / `Tests 2648 passed (2648)`，无失败无跳过；前值 2638 系一键美化 spec 族入库前，`git ls-files 'frontend/**/*.spec.ts'` 同为 205。增量系 terminal 目录用例与 jsonfmt 简化重写（27→15）+ aichat 桥用例（34→39）；上一版 **204/2616**（2026-10-30 实跑）。增量系 `aichat` 前端批次（AiChatView.spec 34 例）入库；上一版 **203/2581**（2026-10-28 实跑），上一版 **170/2136**（2026-10-21 实跑），增量出自 2026-10-26 之后陆续入库的审查批次用例与本会话 `jsonfmt` AI 提取一族用例，逐份账看 vitest 明细） | `vitest run` 实跑；旧记 168/2120 **落笔时即已滞后**——`3bc27ec` 那笔只把数往上推了 1 份 spec（heukms 适配层），同批仓内已存在的 `f40385b` 两份 portscan 面板 spec（`ProxyCheckPanel`/`SSHCheckPanel`）未入账，故与实测差 2 spec / 16 例；该笔提交信息自报实测恰是 170 spec / 2136 例，与它写进本表的值自相矛盾。注：16 例是**总差**，逐份归因不可靠（两份面板 spec 的 `it(` 直数只有 15，余 1 例出自参数化展开），要逐份账得看 vitest 明细 | `cd frontend && node node_modules/vitest/vitest.mjs run`（spec 文件数另可用 `git ls-files 'frontend/**/*.spec.ts' \| wc -l` 交叉核对，2026-10-31 实测同为 205） |
| 应用版本 | **0.3.0** | `internal/product/identity.go` `var Version`（**唯一运行时权威**） | `grep -n 'var Version' internal/product/identity.go` |

## 二、口径边界（写文档必须先知道的六件易混事）

1. **两把尺，勿互证**：托管叙述列（README/PRD 的表）与 catalog `hosted-versions` 计数**恰好同为 32，但集合不同**——前者含 `eartrumpet`、`frpc` 归自建列；后者含 `frpc`、不含 `eartrumpet`（它 `version/`、`instance/` 皆无）。任何一处改动都可能让两个 32 不再相等，**不要把它们当同一个数**来引用。（2026-10-12 两把尺同为 31；`heukms` 入库后同为 32——巧合延续，仍不可互证。）
2. **图标数 ≠ 托管款数**：28 枚是"图标资产入库"口径（含 `frpc`/`ddnsgo` 两件 48px 源天花板件，与 `eartrumpet`——它在托管叙述列却既无 `version/` 亦无 `instance/`）。32 托管款与 28 枚的差集已逐名核对（2026-10-21）：`rammap`（Sysinternals 条款禁再分发二进制资产）、`vscode`（同属红线件）、`recordly`（AGPL 附加条款 + 安装器未签名）三件走「红线图标运行期本机提取」通道不落仓库（见 `docs/THIRD_PARTY_NOTICES.md` 该节与 `internal/app/runtimeicon_service.go`）；`heukms` 无开源许可证可过审、主程序名带 ™，**连运行期提取通道也不进**，界面永久回落 `generic.png`；`piik` 未入台账（headless 服务型，PRD §2.2 骨架例外④）。故 32 − 5 + 1 = 28 成立，台账键集与内嵌 PNG 由 `sources_test.go` 双向锁死。
3. **版本号的三处硬编码**：权威是 `internal/product/identity.go`；`build/config.yml`、`build/windows/info.json`、`build/windows/msix/app_manifest.xml` 需人工同步；`frontend/package.json` 的 `0.0.0` 是 npm 占位，**不是**版本源。git tag 目前只到 `v0.2.0`——这是发布节奏事实，**不是**文档错误，任何文档笔都不得把 0.3.0 改掉。
4. **`internal/modules/` 目录数 ≠ 模块总数**：目录 **55** 个、catalog 条目 **54** 条，差的那个仍是 `modpath`——它是公共样板件（只解析托管工具用户数据目录），无 `module.go` 注册、无 `version/`、无 `instance/`，因此既不入 `moduleTotal` 也不入托管/进程两把尺（2026-10-22 实测：`ls internal/modules \| wc -l` 得 52，`grep -c 'ID:' internal/app/catalog.go` 得 51；本次新增的第 52 个目录 `sshkeys` 有 `module.go`、已入条目，差额不变）。**别拿目录数当模块数**，那会 +1；`DeriveFacts` 的 `moduleTotal` 取 catalog 的 `ID:` 行，`hostedVersions`/`managedProcess` 取目录子包存在性，两把尺混用即本表前言点名的病根。
5. **显示名 ≠ 模块 ID**：`portscan` 的显示名自 `f40385b`（2026-10-20）起为「网络测试」（该页当时并标签页化为端口扫描 / 代理连通性 / SSH 三页；2026-10-22 SSH 主动检查 RPC 搬出为新模块 `sshkeys`，本页现余端口扫描 / 代理连通性两页签），但模块 ID `portscan`、目录 `internal/modules/portscan/`、MCP 授权键 `knownModuleIDs["portscan"]`、前端 `components/portscan/` 全部**不随之改名**。写「端口扫描与指纹」是旧显示名（2026-10-21 记账时：`HEAD` 上 `catalog.go`/`module_catalog.json` 两份生成物仍是该旧名，`cataloggen` 再生挂在并行会话工作区未入库；权威源 `module.go` 已随 `f40385b` 入库——此句是带日期快照，两份产物入库后即过期，按 §四 那条"状态头抓不住"由人工销）；引用 ID 或路径时 `portscan` 依旧正确。同理，`netx` 是包名不是模块 ID。
6. **`packages/go/netx` 是第三个公共内核件，且不在闸门锁内**：`469e3e1`（2026-10-20）把显式代理拨号内核自 `portscan` 上收为 `hanxi/packages/go/netx`，现被 `internal/modules/` 下 **39** 个模块引用（2026-10-21 现取：`grep -rl 'hanxi/packages/go/netx' internal/modules/ --include=*.go \| sed 's\|internal/modules/\|\|\;s\|/.*\|\|\' \| sort -u \| wc -l`）。§一 的「版本面/实例面内核委托」两行只管 `artifact`/`supervisor`，`netx` **没有**对应的 `facts` 键——给它加锁须改 `scripts/docsfacts/main.go` 的 `DeriveFacts`（属取数逻辑变更，须与基线同批走），本表暂不新增数值行，引用此数前必重跑上面那条命令。

## 三、已订正的不诚实项（留档，防回写）

- **portscan 服务指纹（2026-10-12 已连根订正，`ee7fd0d`）**：`internal/modules/portscan/module.go` 的 `Description` 曾写「集成 Nmap 深度服务指纹识别」、包注释曾写「可选 Nmap 深度指纹」，而仓内**无** Nmap/gonmap 引擎、`go.mod` 无相关依赖——真实实现是 `scanner.go` 的 `lightweightProbe`（主动握手 + 被动 Banner 抓取覆盖 SSH/FTP/SMTP/MySQL/Redis，另作 HTTP 头部识别）。该句经 `cataloggen` 流入 `internal/app/catalog.go`、`scripts/fixture/module_catalog.json` 与前端 Info 面板，**光改 README/PRD/DEVPLAN 挡不住对外失真**，故本轮先正文档、随即补正代码源头并再生两份产物。
- 现口径（2026-10-22 复核源头，从 `module.go` 现读）：`internal/modules/portscan/module.go` 的 `Description` 现为「高并发多端口扫描与自研轻量 Banner / HTTP 服务指纹（不含 Nmap），另含代理连通性测试」——`f40385b` 曾把口径扩到代理与 SSH 两块新能力；2026-10-22 SSH 主动检查（`CheckSSH` 版本横幅探测 / `CheckSSHAuth` 一次性密码认证）整体搬出为新模块 `sshkeys`，本行 2026-10-21 所引「……另含代理连通性与 SSH 连接测试」自此成为旧引文（再往前它照 2026-10-12 措辞写的是「自研轻量 Banner 与 HTTP 服务指纹探测（不含 Nmap）」）；**「不含 Nmap」这条诚实边界始终未动**。**若有人照旧文档把它改回"集成 Nmap"，包注释里的诚实边界段与 `docs/FEATURES_OVERVIEW.md` 的原始点名即是反证**。
- **docsfacts 再生命令（2026-10-19 订正）**：本文纪律段曾写「跑 `go run ./scripts/docsfacts -regen` 重写 `doc_facts.json`」，而 **`-regen` 标志不存在**——`scripts/docsfacts/main.go:321` 只定义了 `-emit`（全仓 `*.go` 内 `regen` 零命中），且 `-emit` **刻意只打印到 stdout、不写盘**（包注释 `main.go:18` 与失败文案 `:354` 都要求人工合并基线）。上条那句"不再有任何在册欠账"的宣告并不完备：同属"描述了并不存在的东西"，这一项就一直躺在**讲防失真的那份文档自己的纪律段**里。闸门也抓不住它——断链检查只查带扩展名的相对目标，命令行不是链接（能力边界见 §四）。
- **本表自记数值滞后（2026-10-21 订正，T5 对账批次）**：上一条已经点出"受锁的 9 项绿了不等于全表绿"，本轮又被证实一次——`heukms`（`3bc27ec`）与网络测试页改名（`f40385b`）入库后，§一 的「Go 测试文件数」与「前端测试规模」两行、§二.2 图标差集、§三 那句现口径引文全部滞后，而闸门**全绿**（这几处不是 `facts` 键，双向锁管不到；`386/403` 那行还留着一句"`heukms` 新增 2 份，入库后自当 +2"的待办，而 `heukms` 实际入库 3 份、全仓同期净增 9 份（旧注只预告了 2 份，另有 7 份从未入账），那行"入库后自当 +2"因此永远绿不了也永远不红——它根本不是闸门认识的量）。更糟的一例：`3bc27ec` 的提交信息自报实测「前端 170 spec / 2136 例」，同笔写进本表的却是 168/2120——**同一笔提交里账本与自述打架，闸门无声**。本轮已按实取改为 393/412 与 170 spec/2136 用例，并补 §二.4（目录数 ≠ 条目数）、§二.5（显示名 ≠ 模块 ID）、§二.6（`netx` 引用面未入闸）三条口径。结论按实取成立，但**上一行那句"本表不再有任何在册欠账"式的完备宣告，本轮不再重写**——它就是被这类宣告掩盖住的病型。

## 四、闸门能力边界（如实说明，不夸大）

`go test ./scripts/docsfacts`（已并入 `go test ./...`，CI 零新步骤）能抓：

- ✅ 上述计数在**活文档**里被写成旧值（禁词表命中即红，附现值与出处）；
- ✅ 活文档里指向不存在文件的相对链接（断链）——2026-10-12 起从仅 `.md` **放宽为任意带扩展名的相对目标**（`gen-palettes.mjs` 那类脚本/图片链接同受检；无扩展名的锚与目录一律跳过，宁可漏报也不误报，否则人一多就开始无视它）；
- ✅ Markdown 表格行列数与表头不一致（畸形表格）；
- ✅ 代码事实变了但 `scripts/fixture/doc_facts.json` 未再生（现推 ↔ 冻结基线双向锁）。

抓不住（仍靠人）：

- ❌ **状态头语义**——"施工中 / 在途 / 待拍板"这类自然语言是否与实况相符（无锚可校，2026-10-12 那五处过期状态头是人工销的）；
- ❌ **带帽行的"同行连坐豁免"**——`allowIfContains` 按**整行**判免责，于是"同一行既记史又拿旧值当现状"时闸门哑火：2026-10-21 实测 `docs/DEVPLAN.md:55`（写作「49 模块统一注册：47 常驻…」）与 `docs/PROJECT_STRATEGY.md:6`（写作「当前代码基线：**49 个业务模块**…**31 款**」）都因行内另有「原记」「前记」「未跟进」这类帽而被放行，**放宽正则也抓不住它们**——要真抓得改句式把史述与现值分句分行，属文档面工程而非规则表面能（两文件的现值订正归各文档 owner，本表只登记病型）；
- ❌ **带日期的历史文档内旧值**（`FEATURES_OVERVIEW` 的 38、`PROGRESS` 的 94/939 等属史实，被 `forbidden.allowIfContains` 与豁免表有意放行）；
- ❌ 前端用例数（`170/2136` 随每次加测变动，不入禁词表，靠文档"当时值/现值"措辞规范）；
- ❌ **§一 表内未入 `facts` 键的行**（`Go 测试文件数`、`前端测试规模`、`注册构成` 常驻/条件拆分、`应用版本`）与 §二/§三 的口径叙述句——闸门只锁 `doc_facts.json` 的 9 个键，其余数值全靠人工重取；2026-10-21 那批滞后（`386/403`、`168/2120`）正是死在这一格。**凡有模块入库、成批加测或改名，除跑 `-emit` 外须手工重跑 §一 该行的复核命令**，绿≠表体绿。
