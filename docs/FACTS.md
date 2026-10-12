# Hanxi 事实基准（Facts Baseline）

> **文档定位**：所有"会随代码演进而失真"的计数，**唯一权威出处**。任何文档、注释、对外文案要写这些数字，都以本表为准；本表由 `scripts/docsfacts` 从代码现推，不由人手抄。
>
> **为什么要有它**：2026-10-12 全项目文档对账发现，同一事实在不同文档里存着 3~4 套值（模块数 37/38/41/45、托管款数 25/28、MCP「四件/六件/八键」、前端门禁 94/101/112/653/1261），且架构文档里存在**描述了并不存在的东西**（"集成纯 Go Nmap 探针引擎 gonmap"，仓内无此引擎）。根因是数字散落在十余个文档里各处手写、无任何机器校验。本表 + `go test ./scripts/docsfacts` 把它变成结构性不可能。
>
> **纪律**：改了代码导致事实变化 → 跑 `go run ./scripts/docsfacts -regen` 重写 `scripts/fixture/doc_facts.json`，并同批改文档数字与本文。**禁止**为了让测试变绿而反向修改本文数值；本文只陈述代码现推结果。

---

## 一、核心计数（代码现推，非人记）

| 事实 | 现值 | 权威来源 | 复核命令（仓库根执行） |
|---|:---:|---|---|
| 业务模块总数 | **49** | `internal/app/catalog.go` 生成物 | `grep -c 'ID:' internal/app/catalog.go` |
| 自建侧模块数 | **18** | `moduleTotal − hostedVersions`（叙述列与能力口径在此互换对齐，见 §二） | `go run ./scripts/docsfacts -emit` 的 `selfBuiltModules` |
| 注册构成 | **47 常驻 + 2 条件**（`memo` / `clipboard`） | `internal/app/app.go` `modulesToRegister` | 读 `app.go` 装配段 |
| 托管款数 | **31** | catalog `hosted-versions` 能力 = 模块存在 `version/` 子包 | `for d in internal/modules/*/; do [ -d "$d/version" ] && basename $d; done \| wc -l` |
| 进程托管数 | **30** | catalog `managed-process` 能力 = 存在 `instance/` 子包 | 同上换 `instance` |
| 版本面内核委托 | **26** | `import hanxi/packages/go/artifact` 的模块数 | `grep -rl 'hanxi/packages/go/artifact' internal/modules/ \| sed 's\|internal/modules/\|\|\;s\|/.*\|\|\' \| sort -u \| wc -l` |
| 实例面内核委托 | **23** | `import hanxi/packages/go/supervisor` 的模块数 | 同上换 `supervisor` |
| MCP 工具数 | **12** | `internal/mcp/` 的 `mcp.NewTool(` 调用 | `grep -rho 'mcp\.NewTool(' internal/mcp/*.go \| wc -l` |
| MCP 授权键数 | **10** | `internal/mcp/server.go` `knownModuleIDs` | 读该 map（`envcheck/everything/ocr/memo/sysinfo/logs/portscan/lan/portkill/clipboard`） |
| 上游真图标数 | **28** | `internal/app/appicons/*.png`（台账 `sources.go`，`sources_test.go` 逐枚解码锁死） | `ls internal/app/appicons/*.png \| wc -l` |
| Go 测试文件数 | **386**（`internal/`）/ 403（全仓） | `git ls-files` | `git ls-files 'internal/**/*_test.go' \| wc -l` |
| 前端测试规模 | **167 spec / 2111 用例** | `vitest run` 实跑 | `cd frontend && node node_modules/vitest/vitest.mjs run` |
| 应用版本 | **0.3.0** | `internal/product/identity.go` `var Version`（**唯一运行时权威**） | `grep -n 'var Version' internal/product/identity.go` |

## 二、口径边界（写文档必须先知道的三件易混事）

1. **两把尺，勿互证**：托管叙述列（README/PRD 的表）与 catalog `hosted-versions` 计数**恰好同为 31，但集合不同**——前者含 `eartrumpet`、`frpc` 归自建列；后者含 `frpc`、不含 `eartrumpet`（它 `version/`、`instance/` 皆无）。任何一处改动都可能让两个 31 不再相等，**不要把它们当同一个数**来引用。
2. **图标数 ≠ 托管款数**：28 枚是"图标资产入库"口径（含 `frpc`/`ddnsgo` 等 48px 封顶件，不含 `recordly`/`vscode`/`rammap` 等许可受限不入库件）。
3. **版本号的三处硬编码**：权威是 `internal/product/identity.go`；`build/config.yml`、`build/windows/info.json`、`build/windows/msix/app_manifest.xml` 需人工同步；`frontend/package.json` 的 `0.0.0` 是 npm 占位，**不是**版本源。git tag 目前只到 `v0.2.0`——这是发布节奏事实，**不是**文档错误，任何文档笔都不得把 0.3.0 改掉。

## 三、已知不诚实项（挂账，未随文档笔混改）

- **portscan 服务指纹**：`internal/modules/portscan/module.go:37` 的 `Description` 仍写「集成 Nmap 深度服务指纹识别」，经 `cataloggen` 流入 `internal/app/catalog.go:369`、`scripts/fixture/module_catalog.json:753` 与前端 Info 面板。仓内**无** Nmap/gonmap 引擎、`go.mod` 无相关依赖，实为自研 banner 级探测。文档侧（README/PRD/DEVPLAN）已于 2026-10-12 勘误并明写"代码侧另行批次"；**这条是本表唯一"代码与事实不符"的在册欠账**，订正它需要同改 module.go + `go run ./scripts/cataloggen` 再生，属代码改动，不混入文档提交。

## 四、闸门能力边界（如实说明，不夸大）

`go test ./scripts/docsfacts`（已并入 `go test ./...`，CI 零新步骤）能抓：

- ✅ 上述计数在**活文档**里被写成旧值（禁词表命中即红，附现值与出处）；
- ✅ 活文档相对链接指向不存在的 `.md`（断链）；
- ✅ Markdown 表格行列数与表头不一致（畸形表格）；
- ✅ 代码事实变了但 `scripts/fixture/doc_facts.json` 未再生（现推 ↔ 冻结基线双向锁）。

抓不住（仍靠人）：

- ❌ **状态头语义**——"施工中 / 在途 / 待拍板"这类自然语言是否与实况相符（无锚可校，2026-10-12 那五处过期状态头是人工销的）；
- ❌ **带日期的历史文档内旧值**（`FEATURES_OVERVIEW` 的 38、`PROGRESS` 的 94/939 等属史实，被 `forbidden.allowIfContains` 与豁免表有意放行）；
- ❌ 前端用例数（`167/2111` 随每次加测变动，不入禁词表，靠文档"当时值/现值"措辞规范）。
