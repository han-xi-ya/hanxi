# 剪贴板内置 · 收口接线勘查清单（A7 勘查线）

> **文档定位**：把 [决策档案⑤总闸清单](2026-09-26-clipboard-decision.md) 的十条收口项逐一勘查到**行级锚点**，每项给「现格式摘录（文件:行号）→ 目标增量（伪 diff）→ 先例出处 → 风险提示」，让收口变成照单执行。
> **勘查口径**：本文全部行号取自 **2026-09-26 19:2x 实扫的工作区（dev,含在途未提交改动）**。在途面不小（见 §0.2），**执行收口前必须按文末 §9 的"重锚命令"重扫一遍，别信本文快照**（piik 档案纪律）。
> **本线零写入**：除本文件外未改一行代码、未做 git 写操作。
> 契约唯一事实源：[2026-09-26-clipboard-contract.md](2026-09-26-clipboard-contract.md)（含 §12 v1.1/v1.2）。

---

## 0. 总览

### 0.1 收口项与归属一览（编号对齐决策档案⑤）

| # | 收口项 | 触及文件 | 需拍板 |
|---|---|---|---|
| W1 | app.go 服务装配（构造/注册/生命周期/事件/热键交接） | `internal/app/app.go` | — |
| W2 | composition fixture + cataloggen + 四处计数锁 | fixture、`cataloggen/main.go`、`catalog_contract_test.go`、`contract-enum.spec.ts` | 顺序（与 piik 批）|
| W3 | MCP 工具接线 + registryGate 剪贴板特例（**无头表禁令**） | `internal/mcp/{server,mcp,tools_clipboard}.go` | — |
| W4 | access.json 第十键三处同批 + AI 面板行 | `server.go`、`mcpwizard/{access_write,service}.go`、`guards_test.go`、`AiSection.vue(+spec)` | — |
| W5 | 托盘三件（TrayCommands + 擦除确认闸） | `internal/modules/clipboard/module.go`（A2 文件，收口期主控执笔）| **是：确认闸形态** |
| W6 | 前端路由/导航/图标/浮层挂载/transport 桥 | `navigation.ts(+spec)`、`main.ts`、`icons.ts`、新增 `adapters/clipboardBridge.ts` | **是：图标方案** |
| W7 | bindings 再生 | `frontend/bindings/**` | 顺序（与 piik 批）|
| W8 | DataDir 实位与同步排除（契约 v1.1.3 欠账） | 勘查结论见 §8，**落点在仓库外（飞牛侧）** | **是：排除机制** |
| W9 | 全量闸门自检 | 命令表见 §9 | — |
| W10 | TROUBLESHOOTING 沉淀（v1.2.7 挂账） | `docs/TROUBLESHOOTING.md`（现被他线占用）| 缓写并报 |

### 0.2 现场观察（只记录，不评判不修补）

- **piik/他线收口正在进行时**（实扫 mtime）：`app.go` 18:14 已含 piik 九行装配、`composition_contract.json` 18:14 已含 piik（counts 48/91）、`navigation.ts` 19:14 已含 piik 三表、`navigation.spec.ts` 路由锁已改 61。即：**本文锚点是"piik 落地后"的地基**，clipboard 全部数字按 48→49、61→62 递进。
- 当前红区（收口 clipboard 前属他人欠账，勿混账）：`go test ./internal/app/` 4 红（catalog.go 17:53 未随 piik 重生成、fixture 事件对拍）；`vue-tsc` 2 错（piik.ts 绑定未生成、WebAppView.spec 旧欠）；`contract-enum.spec.ts` 48 项锁红（同上）。`internal/mcp`、`internal/mcpwizard` 绿；clipboard 前端 spec 92/92 绿；`internal/modules/clipboard` 有红（A2 在途写作，service.go mtime 19:14）。
- clipboard 六线资产**已基本落盘可勘查**：`internal/modules/clipboard/` 15 文件、`frontend/src/{types,adapters}/clipboard.ts`、`views/Clipboard{View,Overlay}.vue`、`components/clipboard/`、`internal/mcp/tools_clipboard{,_test}.go`。

---

## 1. W1 — app.go 装配链路（memo 同谱复制形）

memo 完整链路实测六个插入点（行号=现工作区）：

| 链路环节 | memo 现状（文件:行） | clipboard 目标增量（伪 diff） |
|---|---|---|
| ① import | `internal/app/app.go:67` `"hanxi/internal/modules/memo"` | 在 :26（ccswitch 组）与 :27（dbx 组）之间按字母序插 `"hanxi/internal/modules/clipboard"` |
| ② 事件注册 | `:179-182` `RegisterEvent[Void]("memo:changed")`、`("memo:quicksheet:opening")` | :182 后插四行，**类型见下方"事件类型闸"专注**：`RegisterEvent[clipboard.Entry]("clipboard:updated")` / `[clipboard.RemovedPayload]("clipboard:removed")` / `[clipboard.PausedPayload]("clipboard:paused")` / `[Void]("clipboard:overlay:opening")` |
| ③ 构造 | `:463-466` `memoModule, err := memo.New(paths)`（失败→nil→后续自然跳过） | :466 后同形：`clipboardModule, err := clipboard.New(paths)`；err 只 `slog.Error` 不致命（A2 `module.go:28` New 签名已对位 memo） |
| ④ 注册表入表 | `:525-527` `if memoModule != nil { modulesToRegister = append(...) }` | :527 后同形 append `clipboardModule`。**服务面零额外接线**：`module.go:78-82 Services()` 已经 `application.NewService(m.svc)`，经 `:587 registry.AllServices()` 自动挂上；安装凭据 `:538-544 EnsureSeen` 自动补建 |
| ⑤ Wails App 注入 | `:633-641` `mMod.GetService().SetWailsApp(a)`（application.New 之后） | :641 后同谱块：`cMod.GetService().SetWailsApp(a)`（A2 `service.go:125` 已有 SetWailsApp，rpc_gate_matrix 豁免表 `internal/app/rpc_gate_matrix_test.go:26` 已含 "SetWailsApp"，无需扩表） |
| ⑥ 热键交接+预激活 | `:716-725` `mMod.SetHotkeyRegistry(hk)` → `registry.EnsureActive("memo")`（hk 建于 :681） | :725 后新块同形：`cMod.SetHotkeyRegistry(hk)` → `EnsureActive("clipboard")`。槽名 `clipboard/overlay`、默认 Ctrl+Alt+V 均由 A2 `hotkey.go:20-23` 自绑（memo 谱，**不进** `internal/app/hotkeys.go`——那文件只管 ocr snip 装配根代绑型） |

**先例出处**：memo 块逐行即模板；msgboard 同款热键交接 `:707-714`。

**风险与硬点**：
1. **事件类型闸（本次勘查新发现，契约未钉）**：Wails beta.10 `validateCustomEvent` 对已注册事件做**精确类型匹配**（wails 源 `pkg/application/events.go:314-340`），不匹配即静默丢弃——`app.go:151-152` 注释的教训反方向适用。clipboard 的 `clipboard:removed/:paused` 载荷结构体现在是**未导出的**（`clipboard/service.go:71-76` `removedPayload`/`pausedPayload`），`RegisterEvent[T]` 无法引用它。收口二选一：**（推荐）收口期把两类型导出**（`RemovedPayload`/`PausedPayload`，A2 文件同包改名，emit 处 :249/:277/:444 三行同步）；或注册 `RegisterEvent[any]`（interface 分支放行任何载荷，绑定生成 TS 类型为宽松档——不推荐，失去类型账目）。
2. **注册失败降级形**：clipboard 与 memo 同属"构造可失败"模块，注册表 `:529 panic` 只针对 Register 本身；构造失败→nil→不入表，此时 fixture/catalog/navigation 各表**仍含 clipboard 项**（fixture 锁的是源码静态推导，不是运行态），无连带问题，照 memo 先例即可。
3. 浮层窗不需要装配根持有引用（A2 `overlay.go` 自建自毁，TTL 2min 已实现于 `overlay.go:27`），与 quickmenu `SetMainWindow` 那类需求无关。
4. **不进** `snapSvc.SetMemoRestorer` 型联动（快照白名单不含 clipboard，本轮无此要求）。

---

## 2. W2 — composition fixture + cataloggen + 计数锁（四处同批）

### 2.1 fixture：`scripts/fixture/composition_contract.json`（现值 modules=48, events=91）

```diff
   "modules": [ ... {"id":"ccswitch",...},
+    {"id": "clipboard", "route": "/ext/clipboard", "group": "efficiency"},
   {"id":"dbx",...}, ... ]
   "events": [ ...
+    "clipboard:overlay:opening", "clipboard:paused", "clipboard:removed", "clipboard:updated",
   ... ]
   "wiring": { "preactivatedModules": [ "wechat","quickmenu","msgboard","memo",
+    "clipboard",
     "wsl" ],
     "orderedCalls": [ ...
+    {"before": "cMod.SetHotkeyRegistry(hk)", "after": "registry.EnsureActive(\"clipboard\")"}
   ] }
   "counts": { "modules": 49, "events": 95 }
```

route/group 值与 A2 `clipboard/module.go:64-74` Nav 实声明逐字一致（`/ext/clipboard`、GroupEfficiency）；Order=37 与 webapp 的 37 同组并列（`webapp/module.go:53`），排序器按 ID 定序，无冲突，不强改。

### 2.2 `internal/app/composition_contract_test.go:154-158` 的"变量入表"特例（勘查新发现）

测试从 `modulesToRegister := []extapi.Module{...}` 块内正则提取 `xxx.New(`，块外构造、变量入表的模块靠**硬编码名单**（:155 `"ocr","portkill","fileshare","quickmenu","msgboard"` + :156 memo 的 strings.Contains 特判）。clipboard 走 memo 同款"条件 append"，因此必须补：

```diff
 	if strings.Contains(source, "modulesToRegister = append(modulesToRegister, memoModule)") {
 		constructors = append(constructors, "memo")
 	}
+	if strings.Contains(source, "modulesToRegister = append(modulesToRegister, clipboardModule)") {
+		constructors = append(constructors, "clipboard")
+	}
```

（:154 注释"四个被装配根持有引用"的数字措辞早已失真，顺手不修不碍事。）

### 2.3 cataloggen 两张 allowlist + 重跑

- `scripts/cataloggen/main.go:117-121` `hotkeyAllowlist` 加 `"clipboard": true`（证据注释补一行：槽 `clipboard/overlay`，`clipboard/hotkey.go:20` 自绑）；
- `:125-131` `mcpAllowlist` 加 `"clipboard": true`（证据：`internal/mcp/server.go` toolDefs 剪贴板行——见 §3）。
- 其余入口全自动：TrayCommands()（若 W5 落地）→ EntryTray；`Window.NewWithOptions`（`overlay.go:105` 已实证）→ EntryWindow/dedicated-window；preactivated→background-listener。
- 重跑：`go run ./scripts/cataloggen`（生成 `internal/app/catalog.go` + `scripts/fixture/module_catalog.json`，字节幂等）。

### 2.4 两处计数锁

- `internal/app/catalog_contract_test.go:57`：`contract.Counts.Modules != 48` → `!= 49`（错误文案"（期望 48）"同步）。
- `frontend/src/constants/__tests__/contract-enum.spec.ts:116`：`toHaveLength(48)` → `toHaveLength(49)`（注释"48 项"同步）。

**风险**：这四处 + W1 的 fixture 事件名是**一套原子账**，漏任何一处 `go test ./internal/app` / vitest 直接红；且 cataloggen 重跑会连 piik 一起入账——**必须等 piik 线把它的 catalog 债清完（当前 internal/app 四红即其未清账目）或明确同批**。

---

## 3. W3 — MCP 面无头接线（`internal/mcp/`）

A3 交付形已勘查清楚（`tools_clipboard.go` 19:02 版）：hook 通道 + 双接法注释齐备。收口取**接法①（表内，推荐，A3 注释 :236 原话）**：

### 3.1 `server.go` 三处

```diff :107-117 knownModuleIDs
 	"lan":             true,
 	portkillAccessKey: true,
+	clipboardAccessKey: true,
 }
```

```diff :128-140 toolDefs（只读面在前，插在 logs 行后、portscan 前）
 	{Name: toolLogs, ModuleID: accessKeyLogs, Build: buildLogsTool},
+	{Name: toolClipboard, ModuleID: clipboardAccessKey, Build: buildClipboardSearchTool},
 	{Name: toolPortScan, ModuleID: "portscan", Build: buildPortScanTool},
```

`toolClipboard` 常量已在 `tools_clipboard.go:44` 包级定义，server.go 直用。`gateMiddleware` 名称表由 toolDefs 自动派生（:157-160），零额外。

第三处：**运行时可见文案** `:77` instructions 串——`"access.json，九键，默认全关"` → `十键`（这是发给 MCP 客户端的协议文本，属功能面非注释面，必改）。`server.go:19-32`、`mcp.go:15` 的"九键"历史注释顺手改十键。

### 3.2 `clipboardAccessKey` 改 `clipboard.ID` 引用（portkill 先例）

`tools_clipboard.go:50` 现为 `clipboardAccessKey = "clipboard"` 本地字面量。portkill 先例=`tools_portkill.go:39` `const portkillAccessKey = portkill.ID`（含 import `hanxi/internal/modules/portkill`）。收口同款：

```diff
 import ( ...
+	"hanxi/internal/modules/clipboard"
 ...
-	clipboardAccessKey = "clipboard"
+	clipboardAccessKey = clipboard.ID
```

顺带修 :48 注释的过期指位（"现值即 clipboard/service.go:57"——**实扫 ID 在 `clipboard/module.go:17`**，A2 重构后注释未跟）。`tools_clipboard_test.go:643` 的值锁（=="clipboard"）改后仍绿，不用动。

### 3.3 `mcp.go` registryGate 剪贴板特例（契约 v1.2.4 硬约束，安全级）

clipboard 的 `NewStore` 构造即 `MkdirAll`+空库回写（实证 `clipboard/store.go:114,131`），**禁入 `mcpModules`（:227-232 零改动）与任何 Acquire 懒激活**。照 memo 特例（:88-96）逐字复制：

```diff :88 之后
+	// clipboard 模块构造带建目录/空库回写副作用，与无头"零落盘"承诺冲突（同 memo 谱，
+	// 契约 §12.2 v1.2.4）：不进 registry、不走 Acquire，直读 config.json enabled + receipt。
+	if moduleID == clipboard.ID {
+		if !g.store.IsModuleEnabled(moduleID, true) {
+			return noop, fmt.Errorf("「剪贴板历史」模块已在 hanxi 中停用，请先在设置中启用该模块")
+		}
+		if g.receipts != nil && !g.receipts.IsInstalled(moduleID) {
+			return noop, fmt.Errorf("「剪贴板历史」模块尚未安装，请先在 hanxi 模块中心安装该模块")
+		}
+		return noop, nil
+	}
```

（import 加 `"hanxi/internal/modules/clipboard"`；包注释决策 3 :29-30"因此 memo 模块不进无头 registry"句顺带补 clipboard。）

### 3.4 后端装配：`Run()` 一行

`:167-188` Deps 组表段附近加：

```diff
 	deps := Deps{ ... }
+	SetClipboardSource(newClipboardDiskReader()) // A3 hook 通道（tools_clipboard.go:218）
```

`newClipboardDiskReader`（:144）走 `settings.GetPaths().DataDir()` + `windows.DPAPIDecrypt`，零落盘直读——与 memo `newMemoDiskReader()` 注于 :180 的形完全同谱。**注意**：`SetClipboardSource` 必须在 `NewMCPServer(deps)`（:217）之前执行无硬性要求（handler 每次调用重读 hook，:225-229），放段内任意处皆可。

### 3.5 测试面同批（机械）

- `guards_test.go` `TestToolNamesStable` want 表（:82-95）加 `"hanxi_clipboard_search": clipboardAccessKey,`；
- `server_test.go:84` `newFakeGate(...)` 参数表加 `clipboardAccessKey`；
- `access_test.go:27,36,40` 键集全清单加 `"clipboard"`；
- `mcpwizard/access_readmatch_test.go`（矩阵自适配 len(accessToolKeys)，大概率零改，跑一遍验收）。

---

## 4. W4 — mcpwizard 扩表 + AI 接入面板（第十键全链）

契约 v1.2.5 警告：**knownModuleIDs / accessToolKeys / guards_test 三处漏一处 = 含 clipboard 键的整档 access.json 被 fail-closed 拒读，连坐封死全部工具**（读方 `access.go:117` 未知键即整档拒）。三处已在 §3 钉两处，第三处 + 面板如下：

| 处 | 文件:行 | 增量 |
|---|---|---|
| 写方键表 | `internal/mcpwizard/access_write.go:45` | `accessToolKeys` 追加 `"clipboard"`。**位序建议插在 `"lan"` 后、`"portkill"` 前**（读方 map 序无关，写侧"键序=工具面展示序"注释 :41——只读键聚在破坏键前，与 toolDefs 插位一致；换序只影响下次整档回写的字节序，无兼容问题） |
| 呈现 struct | `internal/mcpwizard/service.go:94-105` `AccessTools` | 加 `Clipboard bool \`json:"clipboard"\``；`accessInfo()` 装配段 `:524` 附近加 `Clipboard: st.tools["clipboard"],` |
| 面板行 | `frontend/src/views/settings/AiSection.vue:65-110` | `accessTools` computed 数组加第 9 行（portkill 刻意不呈现是特例）：`{ key:'clipboard', name:'剪贴板检索', tool:'hanxi_clipboard_search', on:!!t?.clipboard, desc:'AI 可搜索你复制过的历史文本（正文与摘要先经打码）；命中密钥启发式的条目与图片内容不经本通道', risk:{text:'含个人剪贴板 · 建议关', chip:'chip-danger'} }`；:65 注释"八行"→九行 |
| 面板锁 | `frontend/src/views/settings/__tests__/AiSection.spec.ts:250` | `expect(rows).toHaveLength(8)` → `9` |
| 名称白名单 | `internal/mcp/guards_test.go:82-95` | （§3.5 已列，三处同批口径在此汇总：**server.go:107-117 ↔ access_write.go:45 ↔ guards_test.go:93 后新增行**） |

`SetToolAccess` 写链自适配（`knownAccessTool` 遍历 accessToolKeys，`access_write.go:184,216-218`），`access_write_test.go:71,130` 用 `len(accessToolKeys)` 无硬码——绿。

---

## 5. W5 — 托盘三件（唤出浮层 / 暂停记录 / 一键擦除）

### 5.1 托盘机制实扫纠偏（决策档案⑤.5 措辞需要落地形修正）

实扫：`trayMenuBuilder.build()`（`internal/app/tray.go:52-113`）**没有硬编码业务条目**——除固定"显示/设置/退出"外，全部条目来自机主在「设置→托盘菜单」勾选的 `settings.TrayMenuItem` 账本；候选目录由 `AppService.ListTrayMenuOptions`（`elevate_tray.go:90-102`）自动聚合 `registry.ListTrayCommands()` + SectionExt 导航 route。所以"托盘三件"的收口动作**不是改 tray.go/builder**，而是：

1. **route 候选**（"打开剪贴板页"）随 Nav 注册自动出现，零代码。
2. **命令候选**：给 `clipboard.Module` 补 `extapi.TrayCommandsProvider`（A2 文件，收口主控执笔），先例 `msgboard/module.go:85-93` 逐字同形：

```go
// TrayCommands 实现 extapi.TrayCommandsProvider：唤出浮层 / 暂停-恢复记录 / 一键擦除（危险，见下）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "overlay", Label: "唤出剪贴板浮层", Run: func(context.Context) error { return m.svc.toggleOverlay() }},
		{ID: "toggle-pause", Label: "暂停/恢复记录", Run: func(context.Context) error { /* 读 paused 翻位，走 svc 内部无门路径或 SetPaused */ }},
		{ID: "wipe", Label: "一键擦除剪贴板历史…", Run: func(context.Context) error { /* 确认闸后 ClearAll */ }},
	}
}
```

（命令体走同包不导出方法绕 RPC 门是合法形：`registry.RunTrayCommand`（extapi/registry.go:506）已先 Acquire 持租约，msgboard `Toggle()` 里再 `holder.Enter()` 的嵌套计数租约实证可行——`gate.go:128-134` + `registry.go` Acquire 的 inFlight 计数制。）

3. cataloggen 扫到 `TrayCommands() []extapi.TrayCommand` 正则（`main.go:141`）→ EntryTray 入账自动（W2 前落地即可）。

### 5.2 "暂停记录"状态镜像

托盘无状态灯机制（菜单只在配置保存时 Rebuild，`tray.go:44-49`）；两页镜像靠 `clipboard:paused` 事件（前端视图已订阅，契约 §5），命令标签用"暂停/恢复记录"对开形——**先例即 msgboard "挂出/撤下留言牌"（module.go:88）**，仓库内无动态改标签先例，不为 clipboard 新造。

### 5.3 一键擦除确认闸（需拍板 ⚑1）

实扫全仓：**Go 侧零原生确认先例**（`a.Dialog` 仅用于 `PickExeFile` 文件框，`elevate_tray.go:223-243`）；危险确认先例全在前端 `useConfirm`（`composables/useConfirm` + `ConfirmDialog.vue`，`ClipboardView.vue:21,39,50` 页面级删除/全清闸已用它钉死）。托盘点击时主窗可能隐藏，前端闸够不着。三个可选形态：

- **甲（推荐）**：命令 Run 内原生 `a.Dialog.Question().SetTitle("一键擦除剪贴板历史").SetMessage("全部条目与图片将不可恢复地删除，确定？").AddButton("Yes").OnClick(擦除).AddButton("No")`（beta.10 Windows 实现=MB_YESNO 系统模态阻塞 MessageBox，wails `dialogs_windows.go:34-87`；回调按返回值映射的英文 "Yes"/"No" 匹配按钮 Label——**Label 必须写字面 "Yes"/"No" 才会触发**，中文标签收不到回调）。风险：①新形态零先例，须真机冒烟（主窗隐藏时弹窗归属、托盘 goroutine InvokeSync 阻塞面——`tray.go:117` dispatch 已走独立 goroutine，不死锁）；②MessageBox 按钮文案是系统"是/否"，不可自定义。
- 乙：双击节奏（首点弹 notify 通知"10 秒内再点=全清"，二点执行）——无先例、状态机自造，不推荐。
- 丙：托盘条目降级为 route 跳页面（复用前端 useConfirm 闸），牺牲"不开窗即擦"诉求——违背决策档案②风险 2"退路"本意，不推荐但列全。

---

## 6. W6 — 前端：navigation / main.ts 分流 / 图标 / transport 桥 / bindings

### 6.1 `frontend/src/constants/navigation.ts`（三表 + 一锁）

```diff
 // ROUTES（现 61 键；:49 '/ext/memo' 行后插）
+  '/ext/clipboard': { component: defineAsyncComponent(() => import('@/views/ClipboardView.vue')), moduleId: 'clipboard' },
 // MODULE_GROUP（现 48 键；memo:'efficiency' 行旁）
+  clipboard: 'efficiency',
 // MODULE_PRESENTATION（现 48 键；memo 行旁）
+  clipboard: { icon: 'i:clipboard', route: '/ext/clipboard' },
```

锁：`navigation.spec.ts:23-24` "登记了全部 61 条路由" → **62**（piik 已把 60 改 61 的现场实证：该数由他线随行就市，收口时以实跑数为准）；:26 spot-check 数组可顺手加 `'/ext/clipboard'`。MODULE_PRESENTATION 图标解析锁（spec:115 `i:` 名必须 ∈ ICON_NAMES）——**图标不登记则此 spec 红**，强制闭环。

### 6.2 `icons.ts` 新增矢量（需拍板 ⚑2）

A1 实扫 71 键无 clipboard 候选属实（本线复核 `constants/icons.ts:13-98` 恰 71 键）。**硬约束：A2 后端 Nav 已声明 `Icon: "i:clipboard"`（`clipboard/module.go:68`）且 sidebar 直接渲染该名**——方案 a 是与后端零改动对齐的正解：

- **甲（推荐）**：新增 `clipboard` 键（24×24 纯 path stroke，遵池内风格：背板+夹头+两行文本线）：
  ```ts
  clipboard: ['M9 3h6v3H9z', 'M17 4.5h1.5a2 2 0 0 1 2 2V20a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V6.5a2 2 0 0 1 2-2H7', 'M8 12h8', 'M8 16h5'],
  ```
  （夹头小方块 + 大背板两段 path 起于 y=4.5 让位夹头，与 sticky-note/file-text 形距足够。）
- 乙：复用 `i:inbox`——零新键，但与设置分区「托盘菜单」同图标（navigation.ts:139），且需回改后端 Nav 一行；辨识度弱。
- 丙：复用 `i:layers`/`i:archive`——A1 已点名形近易混，出局，列出为反面记录。

### 6.3 `main.ts` 浮层分流（`#memosheet` 先例到行）

```diff :6 后
+import ClipboardOverlay from './views/ClipboardOverlay.vue'
 :18-21 注释加一行 "#clipboardoverlay → 剪贴板浮层（Ctrl+Alt+V 唤出，卡体由视图自绘）"
 :29-31 三元链尾插一支（App 回落前）：
-        ? QuickMemoSheet
-        : App
+        ? QuickMemoSheet
+          : hash.startsWith('#clipboardoverlay')
+            ? ClipboardOverlay
+            : App
```

后端 URL 实证 `/#clipboardoverlay`（`clipboard/overlay.go:25`，窗名 `clipboard-overlay`）。`popup-shell` 打标自动（:32-36 `rootComponent !== App` 即加类），真透明窗不吃白底纪律 #50 沿用。**分流顺序无碰撞**：`#msgboard`/`#clipboardoverlay` 前缀互不包含。

### 6.4 transport 桥（契约 §7 "前端零改动"的兑现点）

- 视图层实扫纯净：`ClipboardView.vue`/`ClipboardOverlay.vue` 零 `bindings/` import，全走 `@/adapters/clipboard`（`adapters/clipboard.ts:37 setClipboardTransport`）。
- 新建 **`frontend/src/adapters/clipboardBridge.ts`**（收口期新增文件，不违 §9——kernel 两文件本体不动）：`import * as Clip from '../../bindings/hanxi/internal/modules/clipboard'` + `setClipboardTransport({ List:(q,k,l)=>Clip.ClipboardService.List(q,k,l), Get:…, Set:…, CreateText:…, TogglePin:…, Delete:…, ClearAll:…, SetPaused:…, GetStatus:… })`；`main.ts` 顶层 `import './adapters/clipboardBridge'` 副作用挂载——**必须在 :22 hash 分流前生效**（浮层窗与主窗同一 main.ts 入口，一处注入全员覆盖）。
- 生成物名形（据 memo/portkill 绑定实扫推断，命名规则=结构体名逐字小写）：目录 `bindings/hanxi/internal/modules/clipboard/`，服务文件 `ClipboardService → clipboardservice.js`（同谱实证：MemoService→memoservice.js、MsgBoardService→msgboardservice.js），index.js 导出命名空间 `ClipboardService`，模型在 `models.js`（`Entry/Status` 类；`blobData []byte` → **base64 string**，`runtimeiconservice.js:20` 注释实证，与 `types/clipboard.ts:44 blobData?: string` 逐字兼容；`Set()` 返回 void→Promise<void>，`List` 返回 `Entry[]`）。**方法名逐字保留**（List/Get/Set/…即冻结面 §4），桥为薄透传，无字段映射层。
- 事件订阅零新增：视图直接用 `useWailsEvent + CLIP_EV`（契约 §7 钉死），事件 TS 类型由 RegisterEvents（W1②）进绑定。

### 6.5 bindings 再生（本机可实跑命令，实扫验证）

```bash
# PATH 实证已含：C:\Users\Administrator\go\bin\{task.exe,wails3.exe}（Git Bash 直调正常，非 npm shim）
task common:generate:bindings          # = wails3 generate bindings -f '' -clean=true -i ./cmd/hanxi ./internal/...
# 等价裸调用（勿手敲少了 -i/-clean 组合；"禁裸 wails3"=禁不带 build/Taskfile.yml:185 全参数的直调，会清空/错扫 frontend/bindings）
```

- 验证位：`build/Taskfile.yml:170-185`（generate:bindings 任务本体）。
- **时机关账（契约 v1.2.5 / 决策⑤.7）**：`-clean=true` 全量重生成会把在途欠账（wheel/webapp/piik/portkill）一次并跑——piik.ts 现正因绑定缺失红 vue-tsc（§0.2 实证），**下一次 bindings 跑即是 piik+clipboard 合并闸**，跑完 `git diff -- frontend/bindings` 审面会很大，事先清工作区、同批验收。`task verify:bindings`（Taskfile.yml:50-54）用后即验 git diff 净。
- 生成后自检：`node node_modules/typescript/bin/tsc --noEmit` 不够，走 §9 的 vue-tsc 全量。

---

## 7. W7/W2 交叠备忘 — rpc_gate_matrix / receipts / nav 门禁

- `rpc_gate_matrix_test.go:33-47` 以 fixture modules 驱动扫 `internal/modules/<id>`：clipboard 包实证**已过闸**（service 九法全带 `holder.Enter()`（service.go:136…283 共 9 处）、New 内 `extapi.NewLeaseHolder(ID)`（:88 经 NewClipboardService）、Module 实现 SetGate）；W5 新增 TrayCommands 不触该门。
- receipts/EnsureSeen、GetNavs、模块中心启停：全自动（app.go:538-544、registry 通用链路），零接线。
- `App.vue` 的 EnsureModuleActive 门禁由 ROUTES.moduleId 驱动（navigation.ts 注释），W6.1 一行即活。

---

## 8. W8 — DataDir 实位与同步排除（契约 v1.1.3 欠账，结论钉死）

### 8.1 解析链（代码实证 `internal/settings/paths.go`）

- `DataDir() == baseDir`（:260 直接同值），解析链两级（:116-133）：**exe 同级 `hanxi.bind` 首行绝对路径 → 否则 exe 同级 `hanxidata/` 自动创建**；无 %APPDATA% 兜底（F6 裁定，:22-27）。
- **本机实存**：运行实例即 `bin/hanxi.exe`，数据根 = `E:\System\桌面\工具\hanxi\bin\hanxidata\`（实扫含 config.json/memo/mcp/state/versions 真数据；无 hanxi.bind）——**在 hanxi 仓库/便携目录树内**。
- clipboard 落位（`clipboard/service.go:88`）= `<DataDir>/clipboard/{index.json,blobs/}` → 实体 = `bin/hanxidata/clipboard/`。

### 8.2 是否随同步扩散

- `scripts/build_and_compress.bat:62-69`：发布包只装 `hanxi.exe + README + 空 data/ 旗标目录`，**数据不进包**——发布链无漏。
- 但机主模型是"飞牛同步整目录（程序+数据随身）"（docs/MOTIVATION.md §3、N7 行）：**部署目录=同步目录，bin/hanxidata/clipboard 必随飞牛上 NAS**。同步排除**实际必要**，钉死如下。

### 8.3 唯一可行落点与风险如实

- hanxi 仓内**无任何同步忽略机制**（契约 v1.1.3 已实扫 `.syncignore` 不存在；本线复核 scripts/ 仅 cataloggen/build 脚本）；N7 飞牛客户端托管已终裁放弃，hanxi 无从代发忽略规则。
- **结论**：落点只能在本机之外——**飞牛同步客户端侧的忽略/排除规则**，规则对象 `hanxidata/clipboard/`（若机主把数据根 hanxi.bind 到同步树外则天然免疫，但那改变"数据随身"模型）。
- **未实证项如实申报**：fn-sync 客户端是否支持目录级忽略**从未实证**（N7 侦查卡点：CDN 对非家宽 IP 403，二进制未下载）。机主一步取证：打开本机飞牛同步任务的设置页看"忽略规则/文件过滤"，或实测把 `bin/hanxidata/clipboard` 加入排除后观察一轮同步。
- **若飞牛不支持忽略的升级案**（择一，均改契约需主控改判）：①clipboard 落点改 `hanxi.bind` 式第二指针到同步树外（模块内自造例外，破坏"数据随身"语义）；②整数据根搬家出同步树（影响全模块）；③接受"上 NAS 的是密文 text + 明文 preview/metadata"的扩散并复核风险——**不推荐**，index.json 的 preview/sourceApp/files 全明文（store.go 只加密 Text），正是决策档案②风险 1 的"头号泄露面"本体。
- hanxi 侧顺手可做的缓解（可选）：存储分区页/模块页对剪贴板目录挂"本机专属数据，勿同步"提示（纯文案，M2）。

---

## 9. W9 — 全量自检命令表（本机实跑形态，npm run 一律绕行）

> 本机 Git Bash 下 `npm run *` 的 shim 坏（'"node"' not recognized，既有踩坑），全部走 node 直调 / Go 二进制；以下除标注外本线已实验可用。

```bash
cd "E:/System/桌面/工具/hanxi"
# —— Go ——
gofmt -l cmd internal embedassets.go            # 期望空（task check 同口径,Taskfile.yml:73）
go build ./...                                   # ✅ 19:17 实跑绿（现含在途 clipboard）
go vet ./...                                     # ✅ 分包抽跑绿（app/mcp/clipboard）
go test -count=1 ./internal/app/ ./internal/mcp/ ./internal/mcpwizard/ ./internal/modules/clipboard/
#   ⚠ 现红三堆均他线/在途欠账（§0.2），收口 clipboard 时以"新增红=我的账"为准绳；
#     六线全落+piik 清账后应全绿，再上 `go test -count=1 ./...`
go run ./scripts/cataloggen                      # W2.3（改完 allowlist/fixture 后跑）
# —— 绑定 ——
task common:generate:bindings                    # W6.5（时机=合并闸）
# —— 前端（先 cd frontend）——
node node_modules/vitest/vitest.mjs run                          # 全量（clipboard spec 92/92 ✅ 19:20 实绿）
node node_modules/vue-tsc/bin/vue-tsc.js --noEmit                # 现 2 红=piik绑定+WebAppView 旧账（§0.2）
node node_modules/eslint/bin/eslint.js . --max-warnings=11       # 与 package.json:15 同口径
node node_modules/vite/bin/vite.js build                         # 生产构建闸（= npm run build 的 vite 段）
```

**收口一键脚本建议**（顺序即依赖序，绑 git 审面之前）：
`fixture+cataloggen+app.go+mcp+mcpwizard+module.go(TrayCommands) 编辑 → gofmt && go build && go vet && go test(五包) → cataloggen 重跑后复跑 app 测 → task common:generate:bindings → 前端四板斧(vitest 全量/vue-tsc/eslint/build) → git diff 审面`。

**重锚命令**（执行收口前对本文所有行号）：`grep -n "memoModule\|EnsureActive(\"memo\")\|newTrayMenuBuilder" internal/app/app.go`；`grep -n "knownModuleIDs\|toolDefs = \|九键" internal/mcp/server.go`；`grep -n "accessToolKeys = " internal/mcpwizard/access_write.go`；`grep -n "toHaveLength" frontend/src/constants/__tests__/*.spec.ts frontend/src/views/settings/__tests__/AiSection.spec.ts`；`git status --porcelain internal/app/ internal/mcp*/ frontend/src/constants/ scripts/fixture/`。

---

## 10. W10 — TROUBLESHOOTING 挂账（契约 v1.2.7）

`docs/TROUBLESHOOTING.md` 现为脏文件（M，他线在途）——按契约口径**缓写并报**：clipboard 收口合入时不夹带该文档改动，待其净仓后补一条四段式："构造即写盘模块进无头 registry 的零落盘陷阱"（实证材料：`clipboard/store.go:114,131` 建目录+空库回写、`mcp.go:85-96` memo 特例谱、v1.2.4 禁令）。

---

## 11. 需机主/主控拍板才能继续的清单（汇总）

1. **⚑1 一键擦除确认闸形态**（§5.3）：推荐甲案=原生 MB_YESNO（新形态、按钮文案系统固定"是/否"、需真机冒烟）；如否，退丙案=托盘条目改跳页面复用前端闸（放弃不开窗即擦）。
2. **⚑2 图标方案**（§6.2）：推荐甲案=icons.ts 新增 `clipboard` 矢量（d 串已给，可再校形）；乙案复用 inbox 须回改后端 Nav。
3. **⚑3 同步排除落点**（§8.3）：飞牛侧忽略规则是否可行需机主一步取证；不可行时三升级案择一（均动契约）。
4. **⚑4 收口时序**：piik 线收口正在进行（app.go/fixture/navigation.ts 在途、catalog/bindings 未再生、前端锁已改数）——clipboard 收口**必须等 piik 落定合入后**再按本文重锚执行，bindings/cataloggen 两闸各跑一次，禁止两线并改共享文件。

---

## 12. 执行台账(主控,2026-09-27 02:3x 收线时点)

**拍板状态**:⚑1=丙案形(跳回视图复用前端富文案闸,由此擦除**不做成命令**,route 候选天然即入口,零新事件);⚑2=甲案(e0 已落 icons.ts clipboard 键);⚑3=**挂起**——机主裁决"先不考虑同步"(契约 §12 v1.5.3,风险知情记录不销账);⚑4=串行闸已半开(hanxi-e0 装配落定但仍在途,cataloggen/bindings 双闸未获静默窗口)。

| 项 | 状态 | 备注 |
|---|---|---|
| W1/W2/W6.1-6.3 | ✅ hanxi-e0 施工(02:0x),勿重复 | 六点位/RegisterEvent×4/fixture 49 数/三表建档 |
| W3 | ✅ 主控 02:2x | server.go toolDefs+knownModuleIDs+十键文案;tools_clipboard.go clipboardAccessKey=clipboard.ID;mcp.go registryGate clipboard 特例+SetClipboardSource 装配;guards/server/access_test 同批。`go test ./internal/mcp/` ok |
| W4 | ✅ 主控 02:2x | access_write 键表+**accessToolsDoc/accessDocOf 写侧模板补键**(ResetAccess 硬码表,漏则整档九键——首跑红已修);service.go AccessTools+装配;AiSection.vue 第九行(位序随写侧=行尾,保 spec 6/7 锚位);spec 17/17 |
| W5 命令面 | ✅ 代码落 module.go | TrayCommands 两件(overlay/toggle-pause),包测绿;⚠ 台账未入 fixture:**cataloggen 重跑等 e0 静默** |
| W6.4 bridge / W7 bindings | ⏸ 等静默窗口 | `task common:generate:bindings` 总闸+clipboardBridge 一行注入 |
| W8 | ⏸ 挂起 | 同 ⚑3 |
| W9 全量验证 | ⏳ 收口尾执行 | go build/vet/test ./... + vitest 全量 + vue-tsc |
| W10 TROUBLESHOOTING | ⏳ 缓写并报 | 两笔(无头零落盘陷阱;预算起步价只拦残段);docs 面在 e0 手中,静默后落 |

**静默后队列(一句话驱动)**:cataloggen 重跑(EntryTray 入账,计数不动)→ bindings 总闸再生 → clipboardBridge.ts+main.ts 注入(注入面与 e0 的 W6.3 分流对表)→ W9 全量 → W10 → 原子提交清单呈机主。

**终局(09-27 02:5x)**:静默窗到达(e0 idle + `19e8eae` 落定),队列全跑——cataloggen 重跑(tray=26 入账,fixture +54 行)→ `internal/app` 转绿 → bindings 总闸再跑(AccessTools.clipboard 入线形,再生仅动 5 文件零串线)→ clipboardBridge 落位 + main.ts 注入 → W9:vue-tsc 0 错、vitest 163 文件/1991 测全绿、Go 全量经 cmd 原生壳复核(instance 族与 wsl 单发均定性环境/flake,非回归,已沉淀 TROUBLESHOOTING #99-101)→ W10 三笔落账。**W1-W10 全部关闭,余提交一步待机主点头。**
