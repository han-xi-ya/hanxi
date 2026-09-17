# 剪贴板联动惯例 · 可行性分析 / 方案 / 计划（PLAN_CLIPBOARD）

> 状态：方案定稿待排期 · 范围：规范①+ 组件②+ 全局热键识图③ · 约束：不改现有契约、不做全局剪贴板监听
> 关联：docs/MOOTOOL_ANALYSIS.md（剪贴板条目）、.claude/skills/hanxi-workbench-ui/、正在开发的 ocr 悬浮卡（SnipCardView.vue / snipcard.go）

## 一、结论先行

1. **可行，且三块收益不对等**。①②（规范 + 组件收口）是低成本、高确定性的"补齐"，主要价值在一致性与可维护性；③（全局热键识图）是**最大单点收益**——截图后一键出字、直接衔接 snipaste 工作流，且后端原语**已几乎全部就位**，只缺一条热键注册与"读现成剪贴板图"入口。
2. **③ 不需要手写键盘钩子**。Wails v3 beta.10 自带全局快捷键管理器（`application.Get().GlobalShortcut.Register(...)`），Windows 侧走 `RegisterHotKey`，与 quickmenu 的 `WH_MOUSE_LL` 是两套互不相干的机制，**天然共存、无低级钩子超时链风险**。这把 ③ 的工程量与风险砍掉一大半。
3. **不做全局剪贴板监听**（隐私取舍，与 MooTool 一致，docs/MOOTOOL_ANALYSIS.md:95）。所有读取动作一律"用户主动触发"：点按钮粘贴 / 按热键读一次。轮询仅允许出现在**单次有始有终的操作内**（如 snip.WaitFor 等截屏写回，最长 45s 即退），**不得作为常驻监听**。
4. **粘贴侧完全空白，复制侧底层已收但入口有漏**。全仓 41 个文件引用 `useClipboard`，视图层已无一处直接 `navigator.clipboard`，但它**只有 `copy`、没有 `paste`**——"点击按钮读剪贴板"的能力为 0；复制侧虽底层统一，却仍有 **4 种调用形态 + 3 帮失败文案**，且 PortKill/EnvCheck/托管族日志等**输出区根本没做复制**。①② 重心＝补粘贴入口 + 收口复制话术/形态 + 填输出复制缺口，而非重新发明复制。
5. **建议排期**：② → ① → ③。组件先立（②），规范随组件定稿写（①），热键独立推进（③）。③ 依赖 ocr 悬浮卡收尾，但**不必串行等待**——可先落"文本热键"占位或等卡就绪。

---

## 二、现状与证据（文件:行）

### 2.1 复制侧：视图层已不碰 `navigator.clipboard`，但调用形态四路、失败话术三帮
- 唯一底层实现：`useClipboard.ts:11`——① 安全上下文优先 `writeText`（:14），② 回退隐藏 textarea + `execCommand('copy')`（:22-30）。**视图层已无一处直接 `navigator.clipboard`**（grep 仅命中 useClipboard 与 spec），底层算收干净。但"调用形态"仍有 **4 种**：
  1. `useClipboard` + `await` + 三元 toast（主流，PublicIpView.vue:42、WifiView.vue:87、MemoView.vue:220、OcrView.vue:332 等）；
  2. `useClipboard` + 内联 `void copy().then()`（FrpcProjectsView.vue:210/:386、FrpcProjectEditor.vue:423）；
  3. **后端 Go 代理写剪贴板**，绕开前端——SnipCardView.vue:51 → `OcrAPI.SnipCopyText` → snipcard.go:128 `snip.WriteText`（③ 悬浮卡走的正是这条路）；
  4. **组合式二次包装 / 点击即复制无按钮**——useFileShareServer.ts:182、useEverythingSearch.ts:129（点单元格复制，根本没有钮）。
- **失败话术三帮**：`'复制失败: execCommand 不可用'`（FlClashView.vue:283、GuoheViewView.vue:261——已过时的内部实现细节泄漏）／`'复制失败: 剪贴板不可用'`（BCU/Keyviz/Rufus 等 10+ 处）／裸 `'复制失败'`。根因：`copy()` 只返回 `boolean`，无错误通道，各调用方自写 toast。
- **样式/命名四皮**：同一"复制文本"有 `.link-button` 裸文字、`.btn-secondary btn-small` 带 📋、`.btn-copy-chip`、⧉ 纯字形、点击即复制等，图标 📋/⧉/无 混用，命名「复制 / 复制全文 / 复制 IP / 复制路径 / 复制直链」不统一。
- **注意**：复制**输出**并非全仓标配——PortKillView（见 2.2）整页零剪贴板代码。

### 2.2 粘贴侧：完全空白（连一枚"粘贴按钮"都不存在）
- 全仓 `navigator.clipboard.read/readText` **零使用**，`粘贴` 二字在视图中**只以 placeholder/提示文字出现，从未做成按钮**。
- 唯二读取剪贴板处均为 `@paste` 键盘事件且**只取图片、纯文本直接 return 不处理**：OcrView.vue:295（dropzone Ctrl+V）、WechatBotChatInput.vue:35（聊天附件）。即"点击按钮读取剪贴板文本"的能力= 0。
- **输入区有、粘贴钮无，且有明显"从别处复制来"场景的**（① 的高价值挂点）：MemoView.vue:407 textarea（placeholder 竟明写「在此粘贴文本、命令行、cURL、JWT」却无钮）、FrpcProjectsView.vue:587 导入框（label 直接叫「粘贴 frp:// 分享链接」，只有"解析并进入编辑":598）、PortScanView.vue:241 起 SOCKS5 代理 URL 框、PublicIp Ping/Trace 目标（PingPanel.vue:44）、LanScanner CIDR、WSL 编辑器 confText。
- **双缺口**：PortKillView 整页**零剪贴板代码**（imports 无 useClipboard）——端口 input（:158）无粘贴、占用/LISTEN 大表（:189/:233，含 exe 路径 :213/:261）无复制。收口优先级最高。
- 规范①的"标配"这一侧完全未成立；EnvCheck 报告本体（版本/路径 :346）也只在子组件有"复制升级命令"，属输出侧半缺。

### 2.3 后端剪贴板原语：已具备且健壮（③ 的关键前提）
- `internal/modules/ocr/snip/snip_windows.go`：
  - `GrabImage()`（:180）——优先取 Win11 私有 `PNG` 格式（零转码，:39 `pngFormatID`），回退 `CF_DIB`（:191）；
  - `DecodeDIBToPNG()`（codec.go:15）——覆盖 1/4/8/24/32 位与 top-down 行序，异常格式明确报"不支持"；
  - `SnapshotText()`（:100）读 CF_UNICODETEXT、`WriteText()`（:134）写回、`Empty()`；`openClipboard()`（:63）带 6×50ms 重试，规避覆盖层持锁竞态。
- `snip.WaitFor()`（snip.go:34）——轮询等图状态机，已单测。
- **接口化可打桩**：`Snipper`/`Clip` 接口（snip.go:12-30），服务层测试无需真实剪贴板。
- 悬浮结果卡已成形：`snipcard.go:27 showSnipCard`（frameless + Acrylic + 置顶 + 不进任务栏 + 游标定位钳位），`SnipCopyText()`（:123）Go 代理写剪贴板、绕开 webview 安全上下文；前端 `SnipCardView.vue` 支持手动选字 + 复制全文 + 拖拽 + Esc 收起。

### 2.4 全局快捷键能力：Wails 自带，hanxi 零使用（绿地）
- `application.Get().GlobalShortcut`（wails application.go:432），`Register(accel, callback)`（global_shortcut_manager.go:100）。
- Windows 实现（global_shortcut_windows.go:68）用 **`RegisterHotKey`** 挂到主线程隐藏窗，`InvokeSync` 保证注册在主 UI 线程执行（manager.go:137）；回调经 `WM_HOTKEY` 在应用现有消息泵派发。
- 支持 `Ctrl/Alt/Shift/Win/Super` 组合（MOD_NOREPEAT 防连触发，:49）；键名映射含字母/数字/F1-F24/标点（:106）。
- **冲突可捕获**：`RegisterHotKey` 失败即返回 "already registered (possibly by another application)"（:69-73）——抢键场景有明确错误，可提示用户改键。
- 与 quickmenu 共存证据：mousetrap 是 `SetWindowsHookEx(WH_MOUSE_LL)` 独立锁线程 + 自带消息泵（mousetrap_windows.go:178/181/363），**不碰键盘、不注册热键**；热键走 RegisterHotKey 另一条 OS 通路，二者零交集。

### 2.5 派发接缝与规范落点
- 命令派发：`registry.RunTrayCommand(ctx, key)`（registry.go:281）——要求模块启用、`EnsureActive` 懒初始化后执行 `ocr/snip`（module.go:58 TrayCommands，宿主后台 goroutine 调用，内部 `snipMu.TryLock` 防重入，snip.go:29）。③ 复用此路，无需新接线层。
- 服务就绪：`ensureOnlineForSnip()`（snip.go:89）已处理"未运行→自动拉起、starting→轮询、external→不越权"。
- 设计规范落点：`.claude/skills/hanxi-workbench-ui/references/`（design-system.md / component-patterns.md / quality-checklist.md，SKILL.md 声明其为"normal design specification"）；工程规范落点 `docs/FRONTEND.md`（composable 优先、无第三方 UI 框架原则）。

---

## 三、方案设计

### 3.1 规范①：设计条款草案（写入 skill references，另摘一条进 FRONTEND.md）

> **剪贴板交互条款（草案原文）**
> 1. **有输入区的视图标配"从剪贴板粘贴"**：凡含多行文本框、命令输入、批量内容粘贴场景的视图，其输入区旁须有一枚"粘贴"次级按钮，点击即把剪贴板文本填入输入框（不自动覆盖已填内容时给确认或追加）。单行结构化短输入（如端口号）**按需**，不为凑数强加。
> 2. **有输出区的视图标配"复制结果"**：识别/查询/生成的结果卡片右上角须有"复制全文"，明细行可选"复制本行"。复制一律经 `useClipboard`，成功/失败回执用统一话术。
> 3. **历史/结果面板中"复制输入"与"复制输出"分离**：不得用一枚"复制"混淆二者（借鉴 MooTool 双按钮取舍）。
> 4. **不做全局剪贴板监听**：应用不常驻读取、不轮询、不镜像系统剪贴板历史。任何剪贴板读取只在用户显式动作（点按钮 / 按热键 / Ctrl+V 事件）时发生一次。热键识图属"一次触发读一次"，符合本条。

### 3.2 组件②：前端可复用件

**A. 扩展 `useClipboard.ts`（改，向后兼容）**
```ts
export interface UseClipboardReturn {
  copy: (text: string) => Promise<boolean>
  paste: () => Promise<string | null>   // 新增：navigator.clipboard.readText 主路，失败 null
  copyWithToast: (text: string, okTip?: string) => Promise<boolean> // 统一回执话术
}
```
`paste()` 主路 `navigator.clipboard.readText()`（需安全上下文 + WebView2 权限）；不可用时返回 `null`，由调用方提示"请手动 Ctrl+V"。**不引入 `document.execCommand('paste')`**（多数内核已禁）。

**B. 新增 `components/ui/UiClipboardField.vue`（薄封装，仿 UiButton.vue）**
- 输入侧：`<textarea>` + 右上"从剪贴板粘贴"按钮（`paste()` 回填）+ "清空"。
- 输出侧：只读结果区 + "复制全文"（`copyWithToast`），可选 `lines` 渲染逐行"复制本行"。
- props：`modelValue / readonly / showPaste / showCopy / placeholder`；emits：`update:modelValue`。让规范"默认就会用上"而非靠自觉。

**C. 存量收口清单（②落地时逐个评估，勿一刀切）**
- **粘贴入口应补（高价值，输入本就为"从别处复制来"而设）**：MemoView.vue:407、FrpcProjectsView.vue:587（frp:// 链接）、PortScan 代理 URL、PublicIp Ping/Trace 目标、LanScanner CIDR、WSL 编辑器。
- **复制入口应补（真缺口，非风格问题）**：PortKill 进程/exe 路径表（:189/:233，全页零复制）、EnvCheck 环境报告版本与路径（:346）、WifiView 二维码弹窗（:164）、Frpc 日志抽屉（:604，对照 LogsView 有）、托管工具族内嵌日志输出区（现仅"复制仓库地址"）。
- **回执/形态统一**：4 种调用形态与 3 帮失败文案收进 `copyWithToast` 单一话术；`useEverythingSearch` 点击即复制、`SnipCardView` 后端写分别登记为**有意例外**（前者是表格交互范式、后者绕 webview 限制，不强行改）。
- **结构化短输入不强加粘贴钮**：端口/IP 单行框加"粘贴"价值低，登记"有意不加"，避免为凑规范硬塞（违背"工具式克制"）。**注意**：PortKill 的缺口在**输出侧复制**，与"输入侧可不加粘贴"并存，勿混为一谈。

### 3.3 全局热键识图链路③

**架构判断**：热键服务**独立**（挂 app 装配根 `internal/app/app.go`），不塞进 ocr 模块、也不并进 quickmenu。
- 理由：热键属**跨模块的全局输入能力**（未来"取色/识码"可复用同一注册器），而 quickmenu 的语义是"鼠标右键轮盘"，混入会让一个模块同时持有鼠标钩子 + 键盘热键两套异质监听；注册时机须在 `app.Run()` 前（manager 会把 Register 排入 pending、flushPending 绑定）。

**新增后端命令**（ocr 模块内，最小改动）：
```
recognizeClipboardImage(): 不弹覆盖层，直接 snip.GrabImage()
  ├─ found=false → 提示"剪贴板中无图片"（区分"复制了文件/文字"歧义）
  └─ found=true  → 落临时件 → RecognizeImage → showSnipCard（复用 SnipCardView）
```
作为新 TrayCommand（如 Key `ocr/snip-clipboard`），与现有 `ocr/snip` 并列暴露给设置页/热键。
- **歧义处理**：GrabImage 只看 CF_DIB/PNG 图像格式；资源管理器"复制文件"给出的是文件名/CF_HDROP 而非位图，`found=false` 即明确告知"剪贴板不是图片"，不误识别。
- **热键回调**：`Register(cfg.SnipHotkey, func(){ go registry.RunTrayCommand(ctx, "ocr/snip-clipboard") })`——须 `go` 异步（回调在 UI 线程，识图耗时不能阻塞消息泵）。

**新增设置持久化**（ocr store，仿 :64-66 指针可选字段）：`SnipHotkeyEnabled *bool`、`SnipHotkey *string`（默认建议 `Ctrl+Alt+T`，避让 snipaste 的 `F1`/`Ctrl+Shift+A` 与 keyviz 常用组合）；设置页给"键位录制输入框 + 注册冲突即时报错 + 恢复默认"。

**与 snip 覆盖层方案的边界**：现有 `SnipAndRecognize`（snip.go:28）= "唤起系统截屏覆盖层 → 等框选 → 识别"（会覆写用户剪贴板，取消时还原）；③ 新命令 = "剪贴板里已有图 → 直接识别"（不截屏、不弹覆盖层，更贴 snipaste "先截图再识别" 工作流）。二者互补，共用识别转发与悬浮卡。

---

## 四、任务分解（commit 粒度）

### ② 组件落地（先做，为①提供实体）
- `feat(frontend): useClipboard 增 paste/copyWithToast 并补单测`
- `feat(ui): 新增 UiClipboardField 复用件（粘贴/复制统一入口）`
- `refactor(views): 存量复制回执/形态收口至 copyWithToast（统一话术，登记 Everything 点击复制、SnipCard 后端写为有意例外）`
- `feat(portkill,envcheck,frpc): 补齐输出区复制（进程/exe 路径表、环境报告、日志抽屉）`
- `feat(memo,frpc,portscan,publicip): 高价值输入区接入"从剪贴板粘贴"`

### ① 规范条款（组件定型后写）
- `docs(design): 剪贴板交互条款写入 workbench-ui 规范 + FRONTEND.md 摘条`

### ③ 全局热键识图（最重，单独拆；依赖悬浮卡收尾）
- `feat(ocr): 新增剪贴板识图命令（不弹覆盖层，直取图像+悬浮卡）` + 单测（Snipper 打桩）
- `feat(settings): OCR 热键开关与键位配置持久化`
- `feat(app): 全局快捷键注册器 + 冲突报错 + 派发至 snip-clipboard 命令`
- `test(ocr): 剪贴板无图/取图成功/服务冷启动路径`
- `docs: 更新 OCR 使用说明（热键工作流）`

---

## 五、排期（人日估算与依赖）

| 项 | 内容 | 人日 | 依赖 |
|----|------|------|------|
| ② | useClipboard 扩展 + UiClipboardField + 单测 | 1.5 | 无 |
| ② | 存量收口：回执统一 + 输出复制缺口补齐（PortKill/EnvCheck/Frpc 等）+ 高价值粘贴挂点 | 1.5 | 上一提交 |
| ① | 规范条款落文档 | 0.5 | ② 定型 |
| ③ | 剪贴板识图命令 + 单测 | 1 | 悬浮卡/recognize 稳定 |
| ③ | 热键设置持久化 + 键位录入 UI | 1 | — |
| ③ | 全局注册器 + 冲突处理 + 派发接线 | 1.5 | app 启动时序确认 |
| ③ | 联调（多 DPI/多屏/真机剪贴板矩阵）+ 文档 | 1 | 前三项 |

- **顺序**：②（0.5 周内）→ ①（随 ②）→ ③（约 5.5 人日）。
- **③ 是否等悬浮卡收尾**：**命令③a 应等 SnipCardView 稳定**（它直接复用 `showSnipCard`）；但设置持久化、注册器骨架可并行先搭。当前 SnipCardView/snipcard.go 尚在开发（git 未提交），③ 建议排在悬浮卡合入之后，避免在移动靶上接线。

---

## 六、风险与规避

1. **热键抢键**（snipaste / keyviz / 输入法）：RegisterHotKey 失败有明确错误串（§2.4）——捕获后不静默，设置页红字提示"该组合已被占用，请改键"，绝不在启动期 panic 或阻塞。
2. **回调阻塞消息泵**：RegisterHotKey 回调运行在主 UI 线程，识图（拉起引擎 + HTTP 转发）可达秒级——**必须 `go` 异步派发**（同 quickmenu Launch 进 goroutine 的先例），否则整个应用卡死。
3. **防重入**：复用 `snipMu.TryLock`（snip.go:29），热键连按返回"正在进行中"。
4. **键盘钩子超时链**：本方案**不用** WH_KEYBOARD_LL，此风险直接规避；若将来确需底层键盘监听，须照 mousetrap 规范（回调零阻塞零日志、锁线程、PostThreadMessage 转交，mousetrap_windows.go:361 注释）。
5. **粘贴权限**：WebView2 下 `clipboard.readText()` 需安全上下文与用户手势触发——只在"点粘贴按钮"时调用；不可用则优雅降级为提示手动 Ctrl+V，不报错刷屏。
6. **剪贴板持锁竞态**：GrabImage 依赖 `openClipboard` 重试（snip_windows.go:63）；极端下仍失败→按"无图"提示，可重按。

---

## 七、开放问题（需用户拍板）

1. **热键默认值**：建议 `Ctrl+Alt+T`，但需确认不与用户既有 snipaste（默认 `F1`）/ keyviz / 其他工具撞；是否允许"无修饰键纯 F 键"（更易撞，默认关）。
2. **存量收口范围**：② 的"复制回执统一 + 粘贴按钮"做到哪一层——只给**多行文本输入**视图（Memo 等）加粘贴、结构化短输入（端口/IP）**有意不加**？还是要求全 view 一律配齐？倾向前者（克制），请确认。
3. **③ 交付形态**：热键识图结果**只出悬浮卡**，还是同时在 OCR 主视图留一条历史/最近结果？（悬浮卡当前"不挂失焦自动隐藏"以便手动选字，主窗留痕是否必要）
4. **是否顺带做"取色/识码热键"**：既然 ③ 要建全局注册器，是否把 MOOTOOL_ANALYSIS 提到的"取色"一并纳入注册器抽象（影响 ③ 接线复杂度与排期）。

**决策回写（2026-09-17，用户拍板）**：1 = 默认 `Ctrl+Alt+T`，纯 F 键选项不开放；2 = 存量收口走克制路线（多行输入配粘贴钮，端口/IP 类短输入有意不加）；3 = 热键识图结果只出悬浮卡，主窗不留痕（历史包上线后经 ocr 桶自然承接）；4 = 取色并入同一全局注册器抽象（约 +0.5 人日，计入 ③ 任务分解）。
