# 前端质量审查报告（2026-10-26）

> 方式：6 个代码区审查 agent（A–F，互斥文件所有权）+ 2 个样式审计 agent（G 缺陷 / H 打磨，先审后刀、串行执行）。全部改动**未提交**，存于工作区待分批入库。本报告为唯一账本；agent 期审计清单原件在临时目录，要点已转录于此。

## 一、结论与门禁实测

**无并行串行终验四连，全绿：**

| 门禁 | 结果 |
|---|---|
| `vitest run`（全量） | **178 文件 / 2368 测试全过**（起点约 2288，净增 80） |
| `vue-tsc --noEmit`（全项目） | **exit 0，零错误** |
| `eslint . --max-warnings=11` | **9 警告，0 错误**（预算内；FileShareSettings 8 处 vue/no-mutating-props 与 MarkerOnView 1 处 v-for-key 系存量，本轮曾引入的 PortScanView 未用 import 已由编排者清除） |
| `vite build --mode production` | **成功**（✓ built in 1.22s） |

**改动面**：存量文件 121 个（+1255/−1125），新增 9 个——源码 5（`adapters/managedCommon.ts`、`components/memo/MemoEditorPane.vue`、`MemoGlyph.vue`、`MemoSheetSettings.vue`、`views/settings/SettingsModal.vue`）+ spec 4（`clipboardBridge`、`managedCommon`、`markeron`、`useNotification`）。

**总账**：修复约 58 项（每项带 file:line 证据与回归验证）；复核后否决误报约 33 项（防"为凑数改码"）；真实但因铁律（禁改用户可见行为/区外文件）或需实机测量而**呈裁/记录**约 30 项。

## 二、分区战果

### A 区 · 托管控制台家族（修复 10）
- **竞态与泄露**：FrpcProjectsView 日志抽屉跨项目迟到回包整表覆盖（`logsGeneration` 代次闸）；FrpcVersionsTab 下载双击窗口（resolve 占位票封闸，对齐 store 标准面）；loadManagedVersions 同步抛出穿透（loading 永挂 + unhandled rejection，`attempt()` 归一）；PaperTodoView `variant.get` 缺 catch。
- **行为缺陷**：RAMMapView A 路吞回执（取消/提权 message 静默失踪，经 `runSlotVerb` 收编）；SnipasteView 恒假门禁矛盾式（`'quitting'` 进放行数组又被尾项反判）；SnipasteView 首挂载双拉（补 activatedOnce 守卫，对齐 TTB/EarTrumpet 先例）。
- **合并**：HeuKms/TTB `#versions-body` 整替抄骨退役（壳新增 `#release-actions` 槽转发）；`needsElevationRestart` 判据单源化新增共享谓词；PiikView 15 行手抄编排收编。
- 值得记录：`#version-row-extra` **不**转发的裁决——泛型退化丢 `verifiedHash` 方言字段，PaseoView 维持整替是对的。

### B 区 · 适配器层（修复 5 项 + 26 件迁移）
- **骨架收编**：26 个适配器逐字同形块（订阅接线 18–24 份、回执模板 21–22 份）迁入新建 `managedCommon.ts`；带方言转换的块一律不硬塞；导出签名/回执词面/事件名逐字节等值。
- **不变式测试**：bcu 票据键对齐（事件键形与反查键形跨两条独立代码路径必须同形，防"行不亮/票据永挂"复发）；markeron `annotateToggleView` 六态矩阵从零测试补齐；`managedCommon` 契约 7 案（断渲染词与丢弃语义，非抄实现）。
- BCU/Rufus 换用 A 区共享谓词；终跑 vue-tsc 全项目零错误。

### C 区 · 剪贴板与留言板（修复 7）
- ClipboardView 全局 Esc 失活幽灵监听（KeepAlive 四斩挂摘，同 BoardFullscreen 先例）；`clipboard:removed` 无守卫解构 TypeError；useClipboard 降级路 textarea 泄露（try/finally 对称）；浮层 opening 双发 List IPC；队列位次 O(行×队)→Map O(1)；`raw` 深响应→shallowRef（原位变异纪律已注释立规）。
- **存量测试雷实跑踩中**：相对时间断言用真实挂钟，每天 00:00–02:59 必红——已 `setSystemTime` 锚死。
- 新建 `clipboardBridge.spec`（wire null→契约形归一化 8 案），补上"视图永不见 null"投影层零测试真空。

### D 区 · 备忘·设置·模块中心（修复 6）
- MemoView 保存事务重入（连点双笔 Create）；SnapshotSection 迟到响应竞态（旧 FileHistory/DiffFile 覆盖新面板，曾可"按 A 版本配 B 路径发起恢复"）；textdiff `foldContext` 参数护栏（`threshold<2·keep` 锚点重叠）；隐藏态 Markdown 每击键重渲染。
- **单体拆分**：MemoView 1157→908，抽出 MemoEditorPane/MemoSheetSettings/MemoGlyph——金标准 39 例断言一字未改全绿后才落刀。
- **合并**：Ai/Snapshot 两份逐字同构 `.modal-*` 皮肤并为 `SettingsModal.vue`。

### E 区 · 壳层·设计系统·utils（修复 12）
- 行为类：useNotification 同 id 叠卡 + 计时器交叉误杀（旧卡计时器误清新卡、旧卡永驻）；QuickMenuPopup Esc 一键双杀违背分级契约（defaultPrevented 让行）；aria-activedescendant 帽带分支永不可达；sector focus 漏清帽带位；WheelPreview 未喂逐枚源档触"永不放大"红线；UiPromptDialog IDREF 含空格断链；导航栏两折叠钮 aria-expanded 恰好写反。
- 注释/文档失真 5 处订正；NotificationToast/Drawer ✕ 钮无障碍名补齐。
- 方法论标杆：三项关键修复做了"临时回退→测试红→恢复→绿"**非空虚验证**。

### F 区 · 网络与系统工具（修复 7）
- 全为竞态/定时器族：PortScan 出网 IP 旧代理结果倒灌 + 终止重开旧结算污染（双 seq 代次闸）；PortKill 连点乱序（旧进程行挂新端口标题下，**杀错 PID 的诱导面**）；LanScanner 停止即落旗撞后端单飞闸；SubnetDesk/useEverythingSearch 卸载后写回定时器未登记；FileShareView 双份 `.fileshare-page` 合一。
- 4 个新用例按"改前必红"设计；否决理由落后端代码证据（Softver 13 emit 点全传值类型，null 不可达）。

### G 线 · 样式缺陷执行波（执行 11 条 + C10 九处）
- **C1 双 toast 同角重叠**：global-toast 右上→右下 snackbar 位（执行期推翻审计期方案：统一容器侵入 E 新契约大于收益；顶部居中在 <1160px 与通知栈仍有交叠带且 transform 打架）。
- C2 flyout z1001 穿透对话框→900；C3 `.content-area` scrollbar-gutter 稳定；C5 焦点环裁切三处内收；C6 rail-dot green-glow 回流件按 N37② 归一；**Q1 Everything 列宽错位一行修**（删 `.result-path` display:block——CSS 2.1 匿名表对象使首行列宽脱靶，拖窄列即五列全错位；标注**规格推演级，非浏览器实证**）。
- C10 全库 24 处 `outline:none` 逐处判：9 处删还全局环，其余**有替代呈现的不硬动**（EverythingConsoleBar 自带影环、QuickMemoSheet 自绘下划线等，判定留档）。

### H 线 · 样式打磨波（A 组全落 + 让位 3 条）
- **A1 焦点环单源 20/20**（删 11 处死兜底 + 9 处 primary→ring，全库残留 0）；A2 tabular-nums 上收四档原子 + 视图散补丁删净；A3 时长归 `--motion-*` 阶梯 17 处；A4 `transition:all` 普查扩面 13 处；A5 灯族 pulse 1s→1.8s 统一 9 行（重锚后比审计册多计 2 处）；A6 `.form-chip` 三份逐字副本上收（含 Everything 同文件双定义合一；margin-left 排位差两处按实测留 scoped）；A7 省略号补 title 三处（Everything 分项**作废**——重读发现截断文本本已裹在带 title 的 copy-cell 内，审计期漏判）。
- 改后实测：105 文件 1365 测试绿 + vue-tsc exit 0。

## 三、用户可感知的行为变化（如实清单）

1. **LanScanner"停止扫描"**：按钮落回从瞬时变为 ≤1.2s（等后端 worker 排空——原瞬时落旗才是 bug，会造成"已有扫描任务正在进行"红字 + 旧结果倒灌）。
2. **全局 toast 换角**：右上 → 右下；窄窗 max-width 钳制。通知抽屉在场时的压制关系不变（解需 z 戏法，归案 β）。
3. 快捷菜单**分级 Esc** 行为归正（此前焦点在盘内按一次 Esc 会先缩环再关窗）。
4. aria 修正：折叠钮展开态播报对正、帽带高亮读数可达、UiPromptDialog 标题引用修复。
5. 视觉微差（**未真机目检**，jsdom 不含布局）：环色 alpha 化（A1）、灯频变缓（A5）、等宽数字（A2/C7）、rail-dot 去 halo（C6）、toast 位（C1）。
6. RAMMap A 路 / Piik 打开：回执 message 从静默改为上屏；Frpc 下载双击封闸窗口内显示"正在创建下载任务…"行（既有 resolve 阶段词表，非新形制）。
7. **波次三（机主两项裁决）**：RAMMap/MangoDisk 装完提示统一为"版本 X 已安装"（原"RAMMap X 已安装"/裸版本号）；全站文本输入框点选时的焦点边**由绿变蓝**（绿色系成功状态色被副本渗漏误用，全库仅此一处）。

## 四、待机主裁决清单（agent 按纪律未动）

| # | 事项 | 出处 |
|---|---|---|
| 1 | ~~回执词形~~ **波次三已落**（机主选 A）：rammap/mangodisk 两处安装回执改走 `downloadOutcome` 单源，toast 现统一"版本 X 已安装"。**甄别后保留两处方言**：vscode"已将**便携版** X 设为使用版本"系双形态（装版/便携并存）的 setActive 回执、动作类别不同且"便携版"三字有信息量；translucenttb"打包版 X 已安装（便携版未受影响）"同理 | B |
| 2 | ~~6px 半径档~~ **波次四已落**（机主选 A）：新增 `--radius-chip: 6px` 档（命名依 55 处消费面三簇普查：行内紧凑控件/行内输入展示框/同档小贴面块，4→6→8→12→16→999 单调）；全库 55 处 `border-radius: 6px` 全部换 var 等值别名化，裸写清零、零观感变化 | H-B1 |
| 3 | ~~绿边~~ **波次三已落**（机主选 A）：原子层 `.form-text-input:focus` 的 `--state-positive`（成功状态色挪用，grep 证实全库仅此一处、确系副本渗漏）改回 `--color-primary`，与全库主流"焦点换蓝 border"形制归一（键盘另有全局 focus-visible 环，波次二已交还）。**仍开放**："换 border vs 上 ring"两派并存的最终收口（EverythingConsoleBar 等少数派带 ring）属形制裁决 | H-B3 |
| 4 | PageHeader 标题↔副标题 16px vs About/Home 自绘头 4–6px，24 视图头部密度分组不一致 | H-B4 |
| 5 | ~~toast 时长超纪律~~ **波次二已落**：0.25s→var(--motion-slow)，卡 hover 0.2s→base 且 all 改显式属性；观感微差随目检清单一并过 | H-B6 |
| 6 | ModuleCard 卡头 chip 硬占挤没模块名（形制优先级取舍） | G-C8 |
| 7 | ~~α+β~~ **全部落地**：α 见波次二；β 波次四（机主批准"叠放去运气化"）——四处视图级弹层降挂 --z-dialog-under(950)：ModuleCenter 抽屉（Teleport+可同屏确认框实锤 confirmUninstall）、WslUsb pick-backdrop、WifiView 二维码（如实注：今日无同屏场景，属语义归位防未来赌序）、components.css custom-modal 皮肤（消费面三方全查，KeyOpsModal 本地 950 覆盖恒赢、行为零变）。**1000 档现只剩 App 单例两处**，登记册语义=现实。遗留备案：MCV 抽屉 Esc 系 document 级无 escapeYield，现无真实触发路径，日后抽屉内加确认框须补互锁 | G-C11 |
| 8 | ~~空错误口径~~ **波次四已落**（机主选 A）：errors.ts 三形态（裸字符串/Error.message/对象 message）空白一律兜"未知错误"，非空白原样透传不裁剪；errors.spec +3 例断外部可见文案，全库无依赖空串的旧断言（grep 核实） | E |
| 9 | ~~遮罩点数~~ **全部落地**：波次二 48 单源（ClipDetail/MemoCard）；波次四（机主选 A）阅读页统一吃 MASK_DOTS——**勘误**：阅读页实况为 62 点位字面量而非本报告先前所记 60（W1 逐字计数，D 区旧报口径按 62 更正）。三处遮罩现全库唯一出处 constants/masking.ts | D+C |
| 10 | emoji 图标阶段3 清扫（UiClipboardField"📋"、MAIN_TABS 前缀等）宜一次性批次 | E |
| 11 | SnipasteView 失活期 Wails updated 事件仍重拉——"setup 期即订阅"是契约 §5 明文口径，改否属契约裁决 | C |
| 12 | Q2 clip/memo 索引列 dvh 魔数、Q4 页切 transform 陷 fixed、Q5 浮层队 N 宽差、Q6 toast 收合跳动、Q7 Win11 滚动条槽宽——**需真窗实测** | G |

## 五、跨区合并建议台账（下一批候选，本轮未动）

1. ~~useWindowKeyListener~~ **波次二已落**：BoardFullscreen/ClipboardView 两处四斩迁移公共件 + 3 例 KeepAlive 契约 spec；ClipToolbar 系"菜单开合条件期挂摘"不同形制，刻意不收编（composable 头注已写明边界）。
2. 相对时间四家分叉（fmtClipAgo / fmtClipAgoCompact / memoMetrics.fmtAgo / 视图零星）→ utils/format 进位核心 + 回落档形参。
3. ~~敏感遮罩常量~~ **波次二已落**：`constants/masking.ts` 单源 48 点；60 档分叉见呈裁 #9。
4. QR 码 SVG **波次二已落** `utils/qr.ts` makeQrSvg（公共形制合一；两档墨色 #1f2328/#1e293b 与宽度按"归一属视觉裁决"原样保留随调用方，呈裁连带）。
5. 字节口径三套（fmtSize 封顶 MB / formatBytes 千进制 / GB 升档内联×2）→ utils/format 增 GB 封顶档。
6. 下载票据驻留 800ms 摘票编排三处三份 + done 票 setTimeout 同形 → 收进 managed 共享壳"波 2"（勿再各自补洞）。
7. 日志面板家族（Frpc 抽屉 / DdnsGo dd-log / LogsView）ANSI 剥离+warn 着色+40px 自动滚阈值逐字三份 → `useLogTails` 或 ManagedLogPanel。
8. 设置区 `.switch`/`.input-inline`/`.input-number`/`.card-head` 三份逐字 → 升格 components.css 全局原子。
9. frpc 两件 modal 尺寸漂移（480/460）→ UiModal 收编波（先定标准宽）；SettingsModal 可作参照系。
10. ManagedConsoleShell 泛型携带面板 V（免整替）——涉 20+ 视图与壳 spec 基线，破坏性风险级，单独评审。
11. wire null→契约形归一化 spec 模板（clipboardBridge.spec 形制）推广至 bcu/piik 等投影家族。
12. snipaste `versions.remove` 无确认闸——现网经门面不可达，属未来裸删陷阱，建议进 adapter.ts 契约注记。
13. **TROUBLESHOOTING 沉淀**（该文件当前带未提交在途改动，hunk 级追加）：取消在飞扫描的两种收口形制（早落旗+seq 挡结算 vs 晚落旗单路径收口）、KeepAlive 失活期全局监听四斩、shallowRef 后禁止原位变异、测试时间锚定（凌晨必红案）。

## 六、风险与未验证项

- **观感类改动无真机目检**（H 线全部 + G 线 C1/C6 + D 线弹窗壳/SFC 拆分）：测试绿仅证无回归断裂；建议开一次目检窗口过：toast 双栈、快捷菜单高 DPR 预览、设置弹窗、memo 编辑器、Everything 结果表列宽（Q1 修复实证）。
- Q1 为**规格推演级**修复，happy-dom 无布局引擎，浏览器实证仍缺。
- shallowRef（ClipboardOverlay.raw）与"只整列重赋"纪律绑定，后人原位变异会静默不渲染（注释已立规）。
- C 区时间锚定测试用 `setSystemTime(正午)` 构造（断言文案逐字未动），反感可单独回滚此例。
- E 区 focusSector 清帽带若被视为特性回滚，仅一函数可拆。
- 并行期间各区曾实测到的 vue-tsc 瞬态错（MemoView 22 / PaperTodoView 1）均为在途中间态，终局已清零，与迁移无关（留档防再疑）。
- 部分存量文件 CRLF，git 提示后续会归一为 LF——提交时注意行尾噪音混入 diff。

## 七、入库建议（待机主令，不自动提交）

按 Angular 规范拆批，示意 10 批：
1. `fix(managed): 日志抽屉代次闸与下载双击封闸等竞态修复`（A 竞态类）
2. `refactor(adapters): 26 适配器同形骨架收编 managedCommon 并补契约测试`（B）
3. `fix(clipboard): Esc 幽灵监听、降级泄露与浮层竞态/性能修复`（C）
4. `refactor(memo): MemoView 拆分呈现件并修复保存重入与快照竞态`（D）
5. `fix(shell): 通知去重交叉误杀、分级 Esc 与无障碍族修复`（E）
6. `fix(tools): 扫描族代次守卫与驻留定时器收编`（F）
7. `fix(ui): 双 toast 锚位、层序降档与焦点可见性普查`（G）
8. `style(frontend): 焦点环/tabular/motion 令牌纪律收口`（H）
9. `test(frontend): clipboardBridge/managedCommon/markeron/useNotification 契约册`
10. 或按战线合并 9 进各 fix 批——分批时逐文件归属再定，勿大杂烧。

提交前逐批 `git status` 复核，勿带入 pnpm-lock 与会话外在途改动（internal/、docs/ 等属其他战线）。

## 八、波次二补记（机主授权"能做的做了"后落地，同日）

按"方向唯一确定、零裁量空间"的准绳，把呈裁/台账里不需要品味的部分直接落了：

1. **useWindowKeyListener 公共件**（`composables/useWindowKeyListener.ts` + 3 例 KeepAlive 契约 spec）——BoardFullscreen 与 ClipboardView 的两处四斩手写体迁移；ClipToolbar 条件开合形制刻意排除（头注写明边界）。
2. **遮罩单源**（`constants/masking.ts`）——ClipDetail/MemoCard 同值双写收编；60 vs 48 分叉留呈裁 #9。
3. **QR 公共件**（`utils/qr.ts`）——Wifi/fileshare 两处调用合一；两档墨色与宽度原样保留（归一属视觉裁决）。
4. **toast 时长回成文纪律**——四处 0.25s/0.2s → var(--motion-slow)/var(--motion-base)（≤180ms 红线，无裁量空间）；NotificationToast 两处 transition:all 同步改显式属性。
5. **z-index 登记册 α 案 CSS 侧**——tokens.css 新增 `--z-*` 九档（flyout 900 / dialog-under 950 / dialog 1000 / snackbar 9999 / drawer 10001/10002 / board 99998 / palette 100000 / toast 999999），15 处跨文件单例层等值别名化（零行为；JS 侧 DIALOG_Z 本就存在，互锁关系写进 token 注释；AppSidebar 组件内部两处 1000 系局部叠放不入册）。
6. **TROUBLESHOOTING.md 追加 #116–118**——在飞取消两种收口形制、KeepAlive 失活幽灵监听、时间锚定+shallowRef 纪律（纯追加，未触碰该文件在途的他人 hunk）。

**波次二终验（串行）**：vitest **179 文件 / 2371 测试全过**（磁盘 spec 文件数与收集数一致）；vue-tsc exit 0；eslint 9/11 预算内；vite build 成功。全量账目更新：129 存量文件 +1293/−1184，新增 13（含本报告与 TROUBLESHOOTING 外的 9 个源/spec）。

> **编排者自纠记录**：波次二首读全量结果曾误报"168 文件/2186"——那是 grep 掐到 vitest 实时进度行而非最终汇总（还净差一个 spec 却未起疑）。复跑取终行 + `find *.spec.ts` 对账后确认无文件丢失。与本项目"局部样本下结论"前科同族，如实留痕。

**仍然剩下的（只有你能定）**：呈裁清单除 #5/#7(α)/#9(部分) 外原样在 §四：回执词形标准、6px 半径档、输入焦点形制与绿边、头部密度、C8 chip 挤名、β 案、getErrorMessage 口径、遮罩 60 vs 48、emoji 批次、契约 §5 订阅口径、QR 墨色归一；跨区合并台账 §五剩余 9 条（useLogTails、字节口径、票据波 2、设置原子上收、UiModal 标准宽、壳泛型等）；以及 §三/§六 的**真机目检清单**——jsdom 没有布局引擎，样式类修复的最后一验只能靠开一次 app。

## 九、波次三 & 四（机主逐项裁决落地，同日）

**波次三**（见 §三-7、§四-1/-3 行内注记）：安装回执统一"版本 X 已安装"（rammap/mangodisk 收编 downloadOutcome；vscode"便携版"与 translucenttb"打包版"经语义甄别**保留**——不同动作类别、有区分信息量，非漂移）；`.form-text-input:focus` 成功绿渗漏改回 --color-primary 主流形制。

**波次四**（机主对七题菜单的批答：1A 2A 3A 4A 5-7 按推荐=5 跳/6 记着/7 跳；三路 agent 并行 + 编排者补尾 11 处）：
1. 遮罩三处全单源 48（阅读页实况 62 勘误入档，见 §四-9）。
2. `--radius-chip: 6px` 官方档 + 全库 55 处裸写清零（§四-2）。
3. β 案视图级弹层降 950，App 确认框恒在上（§四-7；注：机主菜单里的表述"命令面板提到对话框之上"系编排者转述失准——面板本就在 100000 档，实际落的是更对症的 β：视图层降档去运气）。
4. getErrorMessage 三形态空白兜底"未知错误"（§四-8）。
5. **未做**（按推荐）：QR 墨色归一、契约 §5 订阅口径维持；emoji 清扫列入下批。

**波次四终验（串行）**：vitest 179 文件 / **2373 测试全过**；vue-tsc exit 0；eslint 9/11；build ✓。

**至此呈裁清单收敛为**：头部密度(B4)、C8 chip 挤名、输入焦点"换 border vs 上 ring"两派最终收口、波 2 迁壳类合并族（§五剩余）、以及真机目检清单——其余全部落地。
