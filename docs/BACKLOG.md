# Hanxi 待实现功能清单（执行账本）

> **定位**：待实现功能/修项的**唯一跟踪账本**。动工、完成时更新本表状态；里程碑级归总仍看 `docs/DEVPLAN.md`，功能细节与裁定看对应方案文档。
> **来源**：MooTool 借鉴分析（`docs/MOOTOOL_ANALYSIS.md`）→ 四份可研报告（`docs/plans/PLAN_*.md`，开放问题已于 2026-09-17 全部拍板，裁定见各报告末尾"决策回写"，实现时不得偏离）。
> **状态图例**：⚪ 未动工 ｜ 🔵 进行中 ｜ 🟢 已完成（完成后挪到"已清账"，里程碑归 DEVPLAN §1）。

---

## 1. 已排期批次（MooTool 借鉴四件套）

**执行顺序**（依赖链，勿并行抢跑）：

```
F1 统一历史 → F2-①② 剪贴板组件+规范 → F3-a 配置快照
→ F4-a MCP MVP → F3-b memo 文件库化 → F4-b MCP 完善 → F2-③ 热键识图
```

> F2-③ 有硬前置：SnipCardView 悬浮卡合入后再接线（此前是移动靶）。
> **F5（2026-09-17 用户指定为下一个排期任务）与 F1-F4 无硬依赖，可即刻插入执行。**
> **F6 建议排头先行**（2026-09-17 用户指定）：路径策略未收口前，F1/F3 的落盘面都要跟着"家在哪"走。

### F1 · 统一历史记录公共包 —— 🟢 已完成（合入 fd830ab；PLAN 七提交全落；落点随 F6 复核）

- **做什么**：`internal/history`（单文件 `state/history.json`，按 funcType 分桶、每桶 200 条头插裁剪、`newRecord` 统一过 `logging.Redact`）+ 通用 `HistoryPanel.vue`；首批接 ocr / portkill / envcheck。
- **量级**：7~9 人日，6 个原子提交；先打通 ocr 全链路作样板再铺开。
- **裁定要点**：动作类全记、查询类仅 ocr/portkill（envcheck Overview 不记）；OCR 全文入库 + 设置页开关（默认开）；停用不清桶；清空仅作用当前桶；复制输入/输出分离。
- **方案**：`docs/plans/PLAN_HISTORY.md`

### F2 · 剪贴板联动惯例 —— 🟢 已完成（合入 6a38422；三段全落，悬浮卡依赖当时已解除；热键抢键实测待人工）

- **做什么**：① 规范条款入 `FRONTEND.md` + workbench-ui 技能；② `UiClipboardField` / `useClipboard` 扩展（粘贴侧全仓从零起步）；③ `Ctrl+Alt+T` 全局热键识剪贴板图（Wails `GlobalShortcut` 免手写钩子，复用 snip 包原语 + 悬浮卡；取色并入同一注册器）。
- **量级**：①② 3.5 人日；③ 5.5 人日。
- **裁定要点**：**不做全局剪贴板监听**；短输入（端口/IP）有意不加粘贴钮；③ 结果只出悬浮卡、主窗不留痕；纯 F 键不开放。
- **方案**：`docs/plans/PLAN_CLIPBOARD.md`

### F3 · 数据自动版本快照 —— 🟢 已完成（合入 7a6cc78；memo 文件库化随行；三红线守住；首拍实测待人工）

- **做什么**：`internal/snapshot` 平台底座——失焦/隐藏 + mtime 巡检 + 退出补拍三源触发；外部 git（`.snapshots/repo.git` 分离仓库，Microsoft Store 假存根判定），git 缺失降级影子拷贝 30 份；白名单**排除 `runtime/`**；第二步 memo 文件库化（前端零改动）。
- **量级**：F3-a 配置快照 5 人日 → F3-b memo 4 人日，另真机闸门 1 日。
- **裁定要点**：**永不自动 push**；全程静默；wechat 明文 token 接受入历史；遮罩便签照进；空闲 300s / 间隔 5min；含手动「立即快照」按钮。命名：对外文案用"历史版本"（"Snapshot" 撞 instance.Snapshot）。
- **方案**：`docs/plans/PLAN_SNAPSHOT.md`

### F4 · MCP / Skill AI 接入 —— 🟢 已达成可用（F4a 021ce5e + F4b aa48a73 + R6 6096a4f 授权写入口；剩 C9 真机红队与 C6 CLI 双身份）

- **已实现（2026-09-17 wave3）**：**F4a** 无头 server——`hanxi mcp` stdio 四件只读工具全上（C1-C5；ocr/memo 未等二期提前随批落，独立开关默认全关，无额外暴露，PROGRESS R4 裁决保留），合入 `021ce5e`；**F4b** 安装向导——`internal/mcpwizard` 三客户端编辑引擎 + 「AI 接入」第 8 分区（C7/C8：preview→确认令牌→备份→原子写→复验→回滚全链，JSONC fail-closed），合入 `aa48a73`。S2 文案随批收口；windowsgui 管道冒烟已过（坑 #63）。逐项对账见 PLAN_MCP 各节"实际落地"注记。
- **剩余**：① **C9 真机红队**——Claude Code/Codex/Cursor 三客户端真机各接一次 + 人工安全审查清单，不可跳过；② R2 安装前自检接线（随批修）；③ C6 CLI 双身份（`--list`/`--call`）未落地未排期；④ ~~access.json 无编程写入口~~（已随 R6 销案：mcpwizard 写引擎 + 「AI 接入」四开关，真读方对拍矩阵锁口径；见 PLAN_MCP §6 注记）。
- **做什么**：`hanxi mcp` headless 子命令（mcp-go v0.41.1 锁版本，不装配 WebView/托盘/钩子）；MVP 只放 envcheck + everything；GUI 一键安装向导（preview→确认→备份→原子写→复验→回滚）；memo 检索二期（整库总开关 + 遮罩条目不下发）。
- **量级**：MVP 3~4 人日 → 完善共 9~12 人日。
- **裁定要点**：portkill/frpc 等**提权与写操作永不暴露**；everything 严格只读（无实例报错给指引，不代启动不触发下载）；MCP 输出会进云端模型上下文——逐工具过红线；CLI `--yes` 不开放；配置含注释即 fail-closed 给手动片段。
- **方案**：`docs/plans/PLAN_MCP.md`

### F5 · 软件版本检测（softver，微信为首个目标）—— 🟢 已完成（合入 d79f235；本机双口径实测一致；winget 备用通道按裁定未做；3.x 样本待他机）

- **要什么**（用户 2026-09-17 原话拆解）：① 本机装的微信是什么版本；② 官方最新发布的是什么版本（方便去下载升级）；③ 微信安装目录及其占用大小；④ 微信**数据目录**及其大小。定位是"envcheck 面向日常软件"——envcheck 管 git/go/node 开发工具链，本模块管微信这类装机软件，版本落后引导下载。
- **本机版本怎么拿（复用现成轮子）**：注册表 Uninstall 键（HKLM 64/32 + HKCU）出 `DisplayName/DisplayVersion/InstallLocation`——bcu 的 `instance/prober.go` 与 envcheck `detect/registry.go` 已有同谱探测经验；注册表版本是安装器口径，须再用 `platform/versioninfo`（PE FileVersion，含踩坑 #10 的 VerQueryValueW 两个实测坑）对微信主程序 exe 做第二口径校准——本机×官方双口径正是 envcheck 的成熟模型。
- **官方版本通道（✅ 2026-09-17 已实连验证，原"真风险"解除）**：官方更新页 `https://weixin.qq.com/updates?platform=windows` 为 SSR，内嵌全平台更新史与最新直链。实测锚点：正则 `WeChatWin_([0-9.\d]+)\.exe` 一发命中**版本号+下载直链**双结果（验证日最新 `4.1.15`，直链 `dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe`，universal 版，另有 `WeChatSetup_x86.exe`；`?version=` 参数仅前端"是否已最新"提示用，可不传）。注意口径：页内 `8.0.x` 系列是移动端，勿混。风险降级如实记录：这是 HTML 抓取非结构化 API，页面改版可能失配——解析失败必须优雅降级为"拉起浏览器打开该页+显示本机版本"，不猜不编；winget 降为备用对照通道。副产品红利：直链可让工具提供"复制下载地址/浏览器直接下载"，比"去官网找"更进一步。
- **目录与大小**：安装目录取注册表 `InstallLocation`，缺失回落默认安装路径探测；数据目录要认**两代**——3.x 的 `Documents\WeChat Files` 与 4.0（Weixin，注册表键名亦不同）的 `xwechat_files`，且用户可自定义盘符路径（注册表/配置记录，真机验证）。数据目录常达几十 GB：du 扫描必须**异步 + 可取消 + 进度事件 + 结果缓存**，范式抄 wsl 磁盘体检（VHDX 双口径）与 mangodisk，不仿 envcheck（它没有重 IO）。
- **与 bcu 的边界**：bcu 是"卸载器"（全量列表、动手删），本模块是"版本跟踪 + 升级引导 + 空间勘察"（白名单跟踪对象，微信首个，结构留扩展位）——不做全量软件列表，避免重复建设。
- **量级**：MVP（微信单目标四件套）**3~4 人日**——通道已验证，0.5 日闸门取消；剩余不确定性收敛到"两代微信目录/注册表键真机验证"一项。扩展第二个软件时再决定是否升格为通用模块（暂不写方案文档，动工前补 `PLAN_SOFTVER.md`）。

### F6 · 数据目录策略改造：同级强制、退出用户目录、支持绑定 —— 🟢 已完成（合入 8ba24bd；裸 exe 实弹全过；便携模式概念已退役）

- **四条裁定（2026-09-17 用户拍板）**：① 单 exe 跑起来就在 **exe 同级自动创建 `hanxidata/`** 并落数据；② **不再使用 `%APPDATA%\Hanxi`**（用户目录不进）；③ 旧 `data/` 便携兼容**正式废弃**（v0.3.0 品牌断代遗留的最后一条兼容，说断就断）；④ 支持**数据目录绑定**——有绑定声明就认绑定，没有就落同级（"绑定哪个用哪个"）。
- **改造后解析顺序**：绑定指针（exe 同级小文件，载体格式定稿时定）→ 同级 `hanxidata/`（无则自动创建）→ 都不可用则**显式报错并引导去设置「存储」绑定**，禁止静默回退用户目录。同谱先例：hanxi-ocr 组件的"设置路径 > 同级目录 > PATH"三级纯函数解析。
- **模式消亡宣告（写死，防将来复活）**：F6 交付后，**"绿色免安装 / Portable"不再是一种模式、不再是一个特性、不再有任何单独排期项**——它是唯一存在方式，如同"文件管理器支持读硬盘"，不再需要说。便携/标准双模式判定整体退役：`Mode` 收缩为内部实现细节（同级根/绑定根），存储分区 UI 撤下模式概念、只展示"当前数据根 + 更改位置"；旧标准模式仅剩的价值（同级不可写时的去处）由**显式绑定**承接，且永远"用户说了算、无静默路径"。
- **完成判据（DoD，六条全绿才算 F6 交付）**：① 新电脑裸 exe 双击即活，自动建同级 `hanxidata/` 并落默认配置；② 全代码路径无 `%APPDATA%\Hanxi` 兜底（用户目录只进不出也不进）；③ 旧 `data/` 零识别；④ 绑定指针优先级正确（有绑定认绑定，格式/载体见 PLAN_PATHS.md）；⑤ 同级不可写场景 fail loud + 引导绑定，MSIX 按产品裁决处理不静默；⑥ DEVPLAN §2.3"绿色免安装 Portable 模式"验收项改写为"数据根策略：同级默认 + 绑定"（届时同步撤销本节 ⚪ 状态）。
- **影响面判断（架构 cushion）**：`BaseDir/DataDir/StateDir/RuntimeDir/ConfigFile` **访问器面一字不动**，38 个模块的 `newXxxStore(paths.XxxDir())` 全数无感——改动真实收口在 `internal/settings/paths.go` 的 `resolvePaths`/`detectPortableBaseDir`（删 `:99-114` 旧 data 认定与 `:76-86` AppData 兜底）+ `Mode` 枚举语义收缩（只剩便携与绑定）+ 指针读写 + 测试。
- **两个硬问题（PLAN 阶段必答，不回避）**：
  - **同级不可写**：exe 放 Program Files/只读介质时"自动创建"必失败，新规矩下没有 AppData 兜底 → 必须 fail loud 弹引导（跳设置存储分区）；**MSIX 打包形态同级根本不可写**——MSIX 包就此放弃还是保留为例外形态，**留给用户产品裁决**。
  - **存量迁移**：首启若发现 AppData 旧家有效数据且同级是新家 → 做**一次性静默搬迁**（move 语义、搬迁成功前不删旧家），避免老会话用户升级即"数据蒸发"；旧 `data/` 按③彻底不认不搬（最多加一条首启检测提示）。搬迁细节定稿时定。
- **连带账**：DEVPLAN §2.3"绿色免安装 Portable 模式（检测 `hanxidata/`，兼容旧包真数据根 `data/`）"验收项届时改写；`.gitignore` 与打包脚本已按 hanxidata 口径，无需动。
- **量级**：2~4 人日（paths 改造+测试 1~1.5、绑定指针 0.5~1、迁移+存储分区 UI 1；MSIX 裁决与迁移细节动工前补 `PLAN_PATHS.md`）。

### F7 · hanxi-ocr 托管化改造（双引擎 zip 分发体系）—— 🟢 已完成（主仓 aee1119 + 后厨 hanxi-ocr-dev 三提交；真双包已产出；48MB 停产与否留用户）

- **目标形态（2026-09-17 用户设计）**：终结"两个成品文件夹摆旁边"的现状——后厨产出**两个 zip（微信引擎版 / PP-OCR 引擎版）**；安装=把 zip **拖进 hanxi**，自动识别引擎、解压进 `versions/`，zip 原件留 `installers/`——从此 hanxi-ocr 与 20 多个托管工具同族同规（引用式→托管式，用户挪走原件即断线的老毛病一并根治）。
- **目录与键位**：`versions/hanxi-ocr/<engine>-<version>/`（如 `wechat-4.1.15.9`、`paddle-0.4.0-alpha`，两引擎共用一个托管 ID，切引擎=切子目录）；现存第四种落点 `ocr-engines/` 收编废弃；`installers/` 留包与托管惯例对齐。
- **后厨侧改造（hanxi-ocr-dev，1~2 人日）**：① build 脚本升级为一次产出**三件套：zip + manifest.json（逐文件 sha256）+ 聚合摘要**——微信版现在裸奔无身份证，必须先补齐；② 打包闸门断言：paddle 包逐文件比 manifest、**exe 体积超 ~10MB 即拒发**（防"烤进腾讯件"暗雷——微信单文件版 48MB 就是 go:embed 嵌引擎的先例）；③ 仓库推 Gitee **必须私有仓**（内含 wcocr.dll 专有件与逆向胶水，见 [[hanxi-ocr-private-component]] 纪律），`publicdist/` 只放可公开面。
- **hanxi 主仓侧改造（3~4 人日）**：① store/`GetEnginePath` 语义从"路径引用"改"版本树解析"（复用托管模板 `version/manager` 与 `resolveActiveVersion` 自愈回退）；② 拖放通道扩收 `.zip`（现认 exe/图片，commit c252d8e 地基在）→ 校验链走 flclash 四层校验范式 + manifest engine 键自动判引擎；③ 引擎页 UI：列版本/安装/卸载/切换 + 保留"外部实例"探测（StateExternal 判定现成）与手动指路径兜底；④ 双引擎切换语义不变，只换取径。
- **分发策略（写死）**：**paddle zip 可进公开 Release 当引流品**（Apache-2.0 模型 LICENSE/NOTICE 已随件 ✓、ORT MIT ✓、VC 运行时 app-local 随应用 ✓），公开前三道闸：验货（无腾讯件夹带）、署名（README 声明"引擎 PP-OCRv6，与微信无涉"）、背书（挂 hanxi 名即负责任）；**wechat zip 永远私发**，与 hanxi 本体"公开安装包+私发组件"分界同构。
- **待拍板**：单文件版 48MB 形态保留还是废（托管化后与 zip 拖放抢戏）；paddle 版是否顺带公开源码（拆仓）——不动 F7 主线，定稿 PLAN 时裁决。
- **量级**：后厨 1~2 + 主仓 3~4 人日；动工前补 `PLAN_OCR_HOSTING.md`。与 F1-F6 无硬依赖，建议排在 F6 之后（数据根策略先收口，versions/installers 才安稳）。

### F8 · 桌面留言板（msgboard，参考 MooTool messageBoard）—— 🟢 已完成（合入 00f1dc9；KeepAwake 聚合器入平台层；全屏/防休眠实测待人工）

- **要什么**：离开工位时一键在屏幕上挂出"马上回来 / 请勿动我电脑 / 会议中"式告示牌——MooTool 全 registry 里唯一标记 complete 的小工具，验证了这类"挂墙信息"是真需求，也是 hanxi 桌面增强分区缺的那块"人不在、话在"。
- **复用家底（几乎全是现成件）**：真透明全屏 WebView 窗（quickmenu 弹窗同谱，含 DWM 深色与 DPI 钳位）、收起即销毁范式（踩坑 #53 的"注销 hook→Close→可重建"闭环直接照搬，不养隐藏窗烧 WebView2 内存）、轮盘/托盘条目走 `launcher.Dispatcher` 现成派发、展示文案存 settings.Store。
- **硬需求一条（留言板的命根子）**：挂牌期间**必须阻止显示器睡眠**——屏一熄牌子就白挂。这正好把候选池里的"防休眠引用计数"（`platform/windows` 封 SetThreadExecutionState 聚合器）收进本卡一并交付，MooTool `displaySleepService.ts` 为蓝本；摘牌即释放，别学它写死常开。
- **功能面（MVP 克制版）**：全屏覆盖（背景半透深色 + 大字文案）+ 预设几条（马上回来/会议中/下班了）+ 自定义文字与字号 + 热键/轮盘/托盘三种唤起 + 副屏跟随主屏选择。**不做**：倒计时自动摘、从日历联动状态、密码锁屏（隐私向的都不碰，和 F6 无涉）。
- **量级**：2~3 人日（透明窗+文案 UI 1~1.5、防休眠引用计数 0.5、三通道接线+测试 0.5~1）。无硬依赖，可与 F1-F4 任意穿插；排在有闲空做"小而爽"时最划算。

### F9 · WSL USB 设备共享管理（参考 wsl-dashboard，usbipd-win 集成）—— 🟢 已完成（合入 91f5ce3；上游实况纠偏见卡内注记；真机七步闸门待人工）

- **要什么**：usbipd-win 深度集成三件套——① 图形化列出可共享 USB 设备；② 绑定（bind）/附加（attach --wsl）/解绑全部点点点，不碰命令行；③ **USB 设备开机自动共享给 Linux**（重连场景：开机/拔插后自动重新 attach 到指定发行版）。
- **技术路径（CLI 封装，与 wsl-dashboard 同谱）**：~~包 `usbipd list --json` 出设备表~~ **⚠️ 2026-09-18 实现期纠偏**：上游 usbipd-win 的 `list` **无 `--json`**，JSON 面只有 `usbipd state`（VID/PID/状态为 JsonIgnore 计算属性，须从 InstanceId 提取+可空性推导，与上游 list 自身同口径）；attach 语法为 `--wsl <DISTRO>`（无 `-d`）。实现按上游实况落地并兼容两代字段形态，见 `internal/modules/wsl/usbipd`。`bind` 需管理员——走 wsl 模块现成的**白名单固定参数提权通道**（M10 基建）。**前置检测**照托管惯例：usbipd-win 未装时探测 → 引导安装（实现取保守：发布页+winget 命令复制，未接托管链）。
- **开机自动共享的设计选择（关键决策，倾向已定）**：**不自建 Windows 计划任务，抄自家 NAT 端口转发账本范式**——"hanxi 记录 desired-attach 账本 + 启动/解锁时重放 attach"，与外部工具创建的规则互不触碰（端口转发账本同款边界纪律）。代价：仅 hanxi 在跑时生效；收益：账本可视化、可勾选停用、无系统残留。**若用户要求"hanxi 不在场也要自动共享"，再补计划任务注册选项**（写进账本 UI 的进阶开关，默认关）。
- **落点**：`internal/modules/wsl` 加 `usb.go` + `usbipd` 子包（CLI 胶水与状态机），前端 WSLView 加"USB 共享"页签（MainTabNav 现成）；**真机闸门**：需一台有可直通 USB 设备（串口/存储类）的环境实测 bind/attach/重连，wsl 模块"离线桩结论不可信"的老纪律（FEATURES_OVERVIEW §5.3）在此条目上加倍适用。
- **量级**：3~4 人日 + 真机联调缓冲 1 日。无硬依赖；建议排在 wsl 模块下次动刀时同车，不单独起浪。
- **边界**：不做 GUI 级驱动安装（Windows PnP 的事）、不做远程主机的 usbip（本机 --wsl 直通 only）。

### F10 · 网页应用窗口（webapp）—— 🔵 进行中

- **要什么**：通用"网页应用"内置模块——网址条目管理（名称/URL/图标/几何记忆），点击以独立 WebviewWindow 打开外部站点；预置条目"微信文件传输助手 `https://filehelper.weixin.qq.com/`"；轮盘/托盘按条目动态成项。
- **裁定要点（2026-09-17 用户拍板）**：**X 关闭 = 真销毁**（cookie 存于共享 WebView2 user data folder，销毁不丢登录，依据踩坑 #56 的退出判据实证）；"收起" = Hide + 5min TTL 到期真销毁，TTL 内热复用秒显；**不做协议逆向**——filehelper 走 A 方案协议通道明确不做（归候选池），本模块以 B 方案"打开网页版"实现；URL 闸门仅放行 http/https；每条目至多一窗。
- **落点**：`internal/settings/store.go`（WebAppEntry 五处联动）+ `internal/modules/webapp/{module,service,models}.go` + 前端 `WebAppView.vue`/`useWebApp.ts` + `navigation.ts` 三表；Nav Order 37（efficiency 组）。
- **量级**：原估 1.5~2 人日；依赖链 7 个原子提交：配置面→骨架→窗体核心→TrayCommands→bindings→前端→文档（前 2 已入库 `67e19e4`、`05dc232`）。
- **关联**：踩坑 #56（关闭即真销毁的判据实证，本次沉淀）；旧版对比结论归档在会话方案——B 方案胜出的原因：零协议风控、泛化任意网址。

### 随批修项

| # | 项 | 状态 |
|---|---|:---:|
| S0 | frpc 运行时明文 TOML 停用/退出即擦 + 启动孤儿收编 | 🟢 2026-09-17（`f4c527b`，踩坑 #55） |
| S1 | `.gitignore` 忽略 `hanxidata/` | 🟢 随数据根治理线并行修复 |
| S2 | everything 的 MCP 文案如实引导（ES 依赖运行中实例，勿承诺代拉起） | 🟢 2026-09-17（`feat/f4-mcp` C3：`hanxi_file_search` description 与前置错误双处如实，测试含文案诚实性断言） |
| R1 | msgboard 热键薄封装收编 `internal/hotkey` 通用注册器（wave2 接缝债） | 🟢 2026-09-17（`d96aac6` 合入 + bindings 回补 `bb7905a`） |
| R2 | 安装向导"安装前自检"接线——F4b 合入时 `hanxi mcp` 不存在刻意留白，F4a 合并后补 spawn `hanxi mcp` 跑 listTools + envcheck 真调（见 PLAN §2.5） | 🔵 进行中（分支 `feat/r2-selfcheck`；F4 合入后的真机前闸门） |
| R3 | go.mod `spf13/cast` replace 复核与撤除（#62 离线绕行产物，网络/代理可达后跑 `go mod tidy`） | 🟢 2026-09-17（`8476ff3` dropreplace + 有网 tidy 收敛，`f09fc19` 补记 #62 复核结论） |
| R4 | PLAN_MCP 回写实际落地形态（ocr/memo 提前落地注记，C10 归收尾批） | 🟢 本批（R4/C10：PLAN_MCP 已加"实际落地"注记，四件套文档同批） |

---

## 2. 未排期候选池（借鉴分析 P2/P3，随四件套推进后再裁决）

### devkit 开发者工具箱（`MOOTOOL_ANALYSIS.md` §2.3，全部自研纯 Go、共用 F1 历史）

- 第一批（P1 待排）：JSON 全家桶 / 编解码 / 加解密含国密 / 时间戳 / 正则 / Cron / **快捷替换面板（27 种行操作）** / 二维码。
- 第二批（P2）：屏幕取色（钩子+GDI 基建现成）/ 文本对比 / HTTP 客户端（cURL 导入，不做 Postman 化）/ UA 分析 / Protobuf wire 解码。
- 第三批（P3）：Hosts 管理（hanxi 提权基建是差异化优势）。

### 平台能力

- **本体自更新**（P2）：签名的 update-manifest JSON + NSIS/MSIX 双 packageType + 启动检查托盘提示；MooTool 无签名这点不抄。
- **轮询自适应降频**（P3）：页面离开时 flclash 等 5s 轮询降为 60s（"懒启动"核源后的残余项，见分析 A 表注）。
- ~~**防休眠引用计数**~~：已收编进 F8（随留言板交付同一聚合器）；fileshare/recordly 等后续复用。
- **主题强调色一键切换**（P3）：primary 色相 token 化为设置项（DWM 深色已有）。
- **大屏时钟**（P3）：复用透明窗基建，锦上添花（留言牌已升格为 F8 排期）。

### WSL 能力补强（wsl-dashboard 借鉴裁决，USB 已升 F9，其余归池）

| 参考项 | 裁决 | 理由/落点 |
|---|---|---|
| 实例控制族（启停/终止/状态/磁盘/默认发行版/移除清 Appx） | **基本已有**，两个小残口入池 | 六操作+取证抽屉（VHDX 双口径/df/IPv4）已覆盖；缺：**设默认发行版**（一行 `wsl -s`，薄）、**发行版随 hanxi 启动自拉起 + 退出自选关停**（选项式，别默认动用户实例）；Appx 残留清理待核（wsl unregister 后 Store 发行版注册项是否残留，PLAN 时真机看） |
| 物理迁移（VHDX 挪盘） | **值得做**，入池（P2） | 与三级瘦身同族刚需；克隆快路径（拷 VHDX + `--import --vhd`）已把最硬的实现趟通，迁移≈克隆+删旧+记账，增量小 |
| 导出 tar/.tar.gz、克隆 | **已有** | M11 交付面，格式选择与多工件登记都收编过 |
| 快速集成（终端/VS Code/资源管理器一键 + 工作目录） | **值得做**，入池（P3） | 薄而高频：右键实例→"打开终端/VS Code/资源管理器"三项 + 自定义工作目录，launcher/dispatcher 与 vscode 托管件现成接线 |
| 发行版安装（Store/GitHub/RootFS 助手） | **不做** | 上游 `wsl --install/-l` 与 wsl-dashboard 各自的下载面，hanxi 十项体检已管"装不上"的根因诊断；自建 RootFS 下载助手是重复上游且引入新出网面 |
| 网络管理：防火墙规则/HTTP 代理 | **半个已有 + 两小口入池** | 端口转发账本已覆盖 NAT 转发与 IP 漂移；补：**转发账本连带建/删防火墙规则**（P2，netsh 同批提权顺手）、**`.wslconfig` 编辑器的 proxy 段模板/校验**（现编辑器已覆盖文件面，补 proxy 键语法闸门即成）；"转发开机自动激活"随 F9 账本重放范式一并考虑 |
| 任务计划（cron→Windows 触发器） | **不做** | hanxi 不是计划任务管理器，语义越界；cron 表达式工具价值在 devkit 候选池（编辑/解读），不在系统调度 |
| 磁盘挂载（物理盘/VHD、裸挂载、持久化） | **值得做**，入池（P2） | `wsl --mount` 面 hanxi 完全空白，与系统管理分区气质正合；持久化重挂可复用 F9 账本范式 |
| 配置选项/系统集成族（安装目录/日志级别/深色/更新频率/托盘/静默） | **已有** | hanxi 设置体系全量覆盖，无借鉴 |

### 明确不做的裁决也记这里（防反复讨论）

不学：七条产品线重写、自带 Tesseract、翻译+单词本、全局剪贴板监听、PDF 拆合/水印（撞托管件）、闲置模块自动停用（误伤常驻型）、`--yes` 式免确认安装。详见 `MOOTOOL_ANALYSIS.md` "明确不学"。

---

## 3. 更新纪律

- 动工即把该节状态改 🔵 并在提交信息引用条目号（如 `feat(history): ...`，F1）；完成改 🟢、里程碑归 DEVPLAN §1、细节沉淀归 TROUBLESHOOTING。
- 候选池条目**升入排期时**整卡挪进 §1（带量级与裁定），不留在池内双写。
- 各 `PLAN_*.md` 是方案唯一事实源，本文件不复制其任务分解，只记状态与依赖。
