# MooTool 借鉴分析

> 分析对象：[rememberber/MooTool](https://github.com/rememberber/MooTool)（2026-09 浅克隆 `master` 逐源核实，关键机制均给出仓库内文件路径，非 README 转抄）。
> 对照基线：本项目 `docs/FEATURES_OVERVIEW.md`（2026-09-16 dev 分支盘点，38 模块 + 9 平台能力）。
> 结论先行：**MooTool 与 hanxi 是同一赛道的两种路线——它走"全自研微工具集"，hanxi 走"原生模块 + 托管外部工具工作台"。值得学的优先级是：机制 ≫ 交互惯例 ≫ 具体功能。**最值钱的三件事：MCP/Skill AI 接入层、跨模块统一历史记录、数据库 Git 静默 checkpoint。功能层面唯一明显短板是 hanxi「开发者工具」分区没有任何自研件，MooTool 的 micro-tool 纯 Go 单文件即可低成本补齐。

---

## 1. MooTool 是什么

- **主版本**：Java 21 + Swing（无 Webview），20+ 个工具 Tab，作者以"下班和周末时间"持续维护多年，HelloGitHub 推荐过。
- **7 条产品线并行**（同仓库各子目录，独立版本/构建/更新通道）：
  - `src/`——Java Swing 原始版（v1.8.6，稳定）；
  - `next/`——Electron + React + TS（v1.2.0，**当前主力，AI 接入在此实现**）；
  - `next-tauri/`（Tauri 2 + Rust，0.1.0-rc，已实现 25 工具）、`next-flutter/`、`next-compose/`、`next-fx/`、`next-macos-native/`。
- **功能盘**：随手记（富文本 + Markdown 预览 + 附件 + Git 同步）、JSON/编解码/加解密（含国密）/时间戳/Cron/正则/UA 解析/Protobuf wire 解码/二维码/调色板取色/图片助手（压缩/水印/Base64/OCR）/HTTP 客户端/Host 管理/PDF 拆合/文本对比/网络诊断/系统信息(ODPI 式)/环境变量/配置文件转换 + 快捷替换面板（27 种批量行操作）。
- 与 hanxi 的形态相似度：单分发物、托盘常驻、便携数据目录、主题体系、自绘 UI——**同一设计语境的竞品参照系**。多语言重写线（7 条）对 hanxi 无意义，不列入借鉴。

---

## 2. 借鉴清单（按价值排序）

### 2.1 ★★★★★ MCP / Skill：把 hanxi 变成"AI 可调用的本地工具中枢"

**MooTool 的做法**（`next/electron/mcp/`，文档 `next/doc/mootool-ai-integration.md`）：

- 独立 stdio MCP 入口 `mcp/index.ts`（官方 `@modelcontextprotocol/sdk`），**不启动 UI 与数据库**，直接 import 前端纯函数（`jsonTools/encodeTools/timeTools/diffTools`）复用同一实现；`tools.ts` 用 zod 声明 schema，`annotations` 标 readOnly。
- 进程来源：借 Electron 的 `ELECTRON_RUN_AS_NODE=1` 用应用自带 Node 跑 MCP，用户免装运行时。
- **一键安装到 AI 客户端**：Codex 写 `config.toml` 的 `[mcp_servers.mootool]` 托管区块（文本编辑保留原格式）、Claude Code 写 `~/.claude.json`、Cursor 写 `~/.cursor/mcp.json`（jsonc 保注释）。写前备份 `.mootool-backup-<id>`、临时文件原子替换、失败回滚、发现用户手改冲突拒绝覆盖、配置预览 10 分钟过期（`aiIntegrationService.ts`）。
- **文档库只读授权**（`mcp/vaultAccess.ts`）：默认关闭、两个库分别授权、access-file 记实际路径且**每次调用重读校验**（realpath 一致/非符号链接/库被挪走即撤销），遍历有深度 24 / 4000 条目 / 10MB 硬限，respect `.gitignore`，禁绝对路径与穿越。
- 无 MCP 的 Agent 走独立 Skill：装 `SKILL.md` + 按安装路径生成的 `runtime.md`，同一入口兼做 CLI（`--list` 出 schema、`--call TOOL < args.json` 走 stdin）。

**对 hanxi 的判断**：这是**全清单里唯一的战略级项**。MooTool 暴露的是 JSON 格式化、时间戳这类 AI 客户端自己就会的弱工具，价值有限；hanxi 若做 MCP，暴露的是 AI 自己**拿不到**的东西：

| hanxi 能力 | MCP 工具化后 | 备注 |
|---|---|---|
| everything（内嵌 ES.exe） | `file_search` | 本地毫秒级全盘检索，Claude Code 当前只有慢 glob |
| ocr / hanxi-ocr | `ocr_image` | 私发组件仅本机调用，不进任何公开面 |
| memo | `memo_search / memo_append` | 照抄 MooTool 的默认关闭 + 每次重读授权模型 |
| envcheck | `env_report` | "本机 java/go/node 什么版本、有无可升级"一条问完 |
| launcher.Dispatcher | `launch_entry` | 轮盘/托盘条目 AI 直达 |
| fileshare | `share_file` | 回环 HTTP 注意 [[hanxi-loopback-no-proxy]] |
| lan / publicip / portscan | `net_probe` 只读子集 | portkill 提权查杀**刻意不暴露**——红线操作不留 AI 通道 |

**Go 天然优势**：Electron 需要 `RUN_AS_NODE` hack，hanxi 单 exe 加一个 `hanxi mcp` headless 子命令即可（不装配 WebView/托盘/钩子，直接 new 需要的 service）。实现用 mark3labs/mcp-go 或 stdio 手写 JSON-RPC 皆可。
**风险与纪律**：①"一键写客户端配置"触碰 `~/.claude.json` 这类文件，必须完整继承 MooTool 的备份/原子替换/冲突拒绝/预览语义，且按 hanxi 红线规范默认关闭、显式确认；②暴露面每个工具单独标 readOnly/destructive；③MCP 进程与主进程并发访问 hanxidata 需要文件锁或走主进程回环通道，不能双写。
**成本**：中等（新模块 + CLI 模式分流），但 hanxi 无历史包袱，一次做对。

### 2.2 ★★★★ 统一「历史记录」公共包——正好对冲自认的最大债务

**MooTool 的做法**：Java 侧三层抽象——`domain/TFuncHistory.java`（单表 funcType/summary/input/output/extra/createTime）+ `util/FuncHistoryUtil`（静态 save，按 funcType 分组、每组 200 条自动裁剪、summary 缺省取输入前 40 字）+ `ui/component/FuncHistoryPanel`（搜索/应用回填/复制/删除/清空通用面板），挂载靠 `FuncHistorySupport.attachTab(...)` 一行接入。Electron 侧重写后**换了形态保留契约**：`historyRepository.ts`（node:sqlite，表结构与 200 条裁剪逐字镜像 Java）+ `HistoryDialog.tsx`，是否支持由 registry 的 `supportsHistory` 标志声明。

**hanxi 现状与缺口**：`FEATURES_OVERVIEW.md` §5.1 自认"托管模板未下沉公共包"是最大债务（versionCompare 拷了 14 份）。历史记录是**新领域，正好从第一天就按公共包设计，避免重蹈逐模块复制的覆辙**。38 个模块里大量产生可复用的输入/输出对，今天全部即抛：

- ocr 识别结果、portkill 查杀记录、publicip 诊断输出、wifi 密码查询、lan 扫描结果、envcheck 体检报告、fileshare 投递日志（部分已有模块内账本，但格式互不相同、无前端统一消费面）。

**落地建议**：`internal/history/`（store：单 JSON 或 bboltdb/sqlite 均可，按 funcType 分桶 + N 条裁剪 + 敏感条目脱敏钩子——复用现成 `logging/RedactHandler` 的字段口径）+ 前端 `components/HistoryPanel.vue` 通用组件（沿用设计系统 token）。事件总线已有（`ext:changed` 模式），save 走法与之一致。**先给 2-3 个模块接入验证抽象，再推全量**——这是对"复制起步、过阈值再收口"路线的一次主动纠偏。
**成本**：低-中。

### 2.3 ★★★★ 开发者微工具：补齐 hanxi 唯一没有自研件的分区

**对照**：hanxi「开发者工具」分区现有 ccswitch/vscode/paseo/wsl，**全是托管，没有一个原生自研的开发者高频小工具**。MooTool 证明这类工具是日活黏性来源（20 个 Tab 里 15 个属于此类）。hanxi 纯 Go 后端 + 现成 Vue 设计系统，多数是一个 service + 一个 view 的量级：

**建议引入（按 hanxi 场景契合度排序）**：

| 工具 | MooTool 实现要点 | hanxi 契合理由 | 成本 |
|---|---|---|---|
| 快捷替换面板 | 27 种批量行操作（驼峰↔下划线、去重计数、换行↔逗号、按拼音排序…），**支持仅处理选中行** | 与「开发者工具箱」同壳落地；纯字符串处理，Go 标准库为主 | 低 |
| JSON 全家桶 | 格式化/压缩/Key 排序/重复 key 检测/JsonPath 可视化/JSON↔XML/转义 | 每个工具箱的流量入口 | 低 |
| 编解码 + 加解密 | URL/Base32/64、AES/DES/RSA/MD5/SHA 族、国密 SM2/3/4、随机串/密码 | Go crypto 全家桶现成；国密用 emmansun/gmsm 补齐（面向国内用户差异化） | 低 |
| 时间戳 + 大屏时钟 | 双向转换、时区快捷、挂墙大屏模式 | 大屏时钟可复用现成透明窗基建（quickmenu 已有真透明 WebView 窗） | 低 |
| 二维码 | 生成（尺寸/纠错/logo 叠加）+ 解析 + **从剪贴板识别** | 与 snipaste/ocr 工作流天然衔接；Go 用 skip2/go-qrcode | 低 |
| 正则测试 | 实时匹配高亮 + 常用正则收藏 | 直接复用 ocr 结果区做取词 | 低 |
| Cron | 生成/解析/校验/**转自然语言**/最近 10 次运行时间 | Linux 5 段 + Quartz 7 段双口径 | 低（gorhill/cronexpr） |
| 调色板 | 屏幕取色器、格式转换、颜色收藏、混合计算 | hanxi 已有 GDI/全局钩子/透明窗三件套，**取色器对我们是顺手的**；且轮盘/标注场景常配 | 中 |
| 文本对比 | 并排 + 同步滚动 + 统一差异 + 复制差异 | diff 前端有成熟方案；与 memo 联动 | 中 |
| HTTP 客户端 | 全方法 + **cURL 导入** + 请求管理 + 历史 | hanxi 网络分区全是"扫描/诊断"，没有"请求"；cURL 导入是正确取舍（不做 Postman 化） | 中 |
| Protobuf | JSON↔二进制互转、**无 .proto 的 wire format 解码** | 抓包/逆向场景与 portscan 指纹气质一致，差异化强 | 中 |
| Host 管理 | 本机 hosts 编辑/导入导出/语法高亮 | hanxi 提权基建（killhelper 链路、.lnk、UAC）是现成的——**别的工具改 hosts 要另起提权进程，我们不用** | 中 |
| UA 解析 | 浏览器/引擎/OS/设备/Bot 识别 + 预设 UA 库 | 与 HTTP 客户端同壳 | 低 |

**刻意不做**（避免分区膨胀）：Java 代码解释执行（依赖 JDK，envcheck 场景已有 IDE）、PDF 拆合/图片水印（已有托管 piclite/snipaste/果核看图，重复建设违背"托管优先"路线）、系统信息（litemonitor 托管更专业）、翻译+单词本（依赖外部免费端点，Google/Bing 接口随时变卦，与 hanxi"外部依赖单点"债项冲突，除非走用户自配 API key）。

**组织形态建议**：不要 15 个平级模块——按 MooTool 自己的分组思路收成 1-2 个复合模块（如 `devkit` 开发者工具箱，内部子 Tab：JSON/编解码/加解密/时间/Cron/正则/文本；`netkit` 并入网络分区），配合 2.2 的历史公共包统一挂历史。导航分组六分区不动。

### 2.4 ★★★★ 数据 vault 化 + Git 静默 checkpoint（本地版本历史）

**MooTool 的做法**：

- 随手记/JSON 文档都是**目录文件库（vault）**而非单文件库：多格式文件 + 子目录树 + YAML frontmatter 元数据 + `attachments/` 相对路径引用（`QuickNoteAttachmentUtil.java` / `quickNoteVaultRepository.ts`）。全文搜索直接遍历（限 5000 条目/深度 24）。
- **Git 同步不引入 JGit**，ProcessBuilder/execFile 调外部 git（`vaultGitService.ts`：porcelain=v1 -z 解析、per-repo 操作队列互斥、**陈旧 index.lock 清理**、凭据走 GIT_ASKPASS 文件、自动生成 .gitignore）。
- 最巧思的一笔——**失焦/空闲自动 checkpoint**（`vaultGitCheckpointScheduler.ts`）：5s tick，仅当"无未保存改动 + 用户空闲 N ms"或"窗口失焦 N ms"才静默 commit，同一活动只提交一次；另有定时静默 pull；冲突时扫描 `ls-files --unmerged` 在编辑器里**高亮冲突行**、保留现场让用户解（`QuickNoteConflictHighlightUtil.java`）。
- 导出备份（`backupService.ts`）：按类别时间戳目录整体拷贝，且校验目标不能是源子集。

**对 hanxi 的判断**：

1. hanxi 便携目录 `hanxidata/` 本来就是纯文件（config.json、memo JSON）——**Git 化的前提天然成立，MooTool 验证了"调外部 git"而非内嵌 git 库是正确取舍**（免依赖膨胀，与 hanxi"单 exe"约束兼容：git 在不在无所谓，不存在就降级提示）。
2. 最高性价比落点是 **memo**：单 JSON → 目录 vault（每便签一文件 + frontmatter），空闲/失焦静默 commit 给用户"手滑删了能回滚"的能力。hanxi 的 config.json 已有原子落盘+候选提交回滚，但只有"最后一次"，同一套 checkpoint 调度器可让**配置也获得可回看的版本历史**。
3. `hanxi` 已有 DPAPI/强一致落盘文化，冲突高亮这类多端同步 UI 可后置——单人单机场景主要吃"自动本地快照"的收益。
**风险**：调外部 git 属新增运行时依赖路径，需按托管模块同等标准写降级链（git 缺失 → 仅静默 shadow-copy 快照）。
**成本**：中（checkpoint 调度器 + vault 存储层）；memo 迁移需数据一次性转换，旧格式保留回退。

### 2.5 ★★★ 懒初始化壳层：MooTool 已验证，hanxi 有现成钩子可挂

**MooTool 的做法**（`JAVA_STARTUP_RESPONSIVENESS_PLAN.md`，非纸上谈兵，已落地 `…/tool/ui/startup/`）：诊断结论是"启动时 EDT 做 IO/DB、25 个页面全部 eager new"；方案是 `StartupCoordinator` 阶段状态机 + `LazyToolManager`（`ConcurrentHashMap<String, ToolSlot>`，首次选中才后台初始化，槽位先显示"正在加载…"占位，完成后 swap），新旧工具两种适配器，外加 EDT 延迟监控与埋点。原则一句话："EDT 只负责 Swing"。

**对 hanxi 的判断**：前端已经赢在起跑线（Vue 全异步组件 + KeepAlive(10)，天然等价于懒加载 UI）；**真正对应的问题是后端**——`FEATURE_OVERVIEW` 点了"flclash 5s 轮询常驻""nanazip 查询冷启 PowerShell ~1.8s"这类**首启后台成本**。hanxi 已有 `EnsureModuleActive` 路由门禁，最自然的延伸是：**模块的 watcher/轮询 goroutine 不随 app 启动拉起，首次进入路由（或托盘直达）才 Start，离开 N 分钟后 Stop**——把"MooTool 的 ToolSlot"翻译成"service 生命周期门控"。同时抄它一个纪律：任何模块 `Start()` 里禁止同步做网络 IO/外部进程调用（全部 go 后台 + 前端状态条如实展示"初始化中"，设计系统已有操作条组件）。
**成本**：低（约定 + app.go 装配处统一 gate），但要写进 ARCHITECTURE 防新模块回退。

### 2.6 ★★★ 剪贴板联动：学惯例，不学监听

MooTool 全部工具**手动触发取剪贴板**（粘贴按钮/右键），两端源码确认**无全局监听、无轮询**——二维码识别、图片 OCR、UA 粘贴、时间戳粘贴都是"从剪贴板"一键。这是隐私与体验的平衡点，hanxi 直接继承为交互规范：

- 每个有输入区的工具 view 标配「从剪贴板粘贴」按钮；有输出的标配「复制结果」+ 历史面板复制输入/输出分离（MooTool 历史面板就是这么设计的）；
- ocr 已有原生拖图，补**全局热键：识别剪贴板图片**（截图 → 不落地直接热键出文本，衔接 snipaste）与**二维码：剪贴板图即识别**；
- 与 snipaste/quickmenu 的既有热键体系合并规划，避免各模块自注册钩子（quickmenu 的 WH_KEYBOARD_LL/MOUSE_LL 是全局唯一钩子持有者，新需求走它的扩展位）。
**成本**：低，收益是"顺手感"，这是工具箱类产品的口碑来源。

### 2.7 ★★★ 自更新：一份多产品 asset 清单

**MooTool 的做法**：`update-manifest.json` 按产品线分节（`products.java` / `products.next-electron`），每节 `releases[]`，每个 asset 带 platform/architecture/packageType/priority/**sha512**/size；`updateService.ts` 只读本产品线节、按平台选 asset，下载校验后 mac 提示 DMG 拖拽安装；主进程每小时轮询。老 Java 版则退化为"只检查 + 给直链清单 `download_links.json` 让用户手动下载"。

**对 hanxi 的判断**：hanxi 对**托管工具**的下载校验已是全组标杆（ccswitch 四层 sha256），但**hanxi 本体没有自更新通道**（盘点文档只字未提，发布靠重新跑安装包）。MooTool 的清单结构可几乎照搬为 hanxi 本体升级格式：NSIS 静默升级 / MSIX 走 `winget`（平台层已有 MSIX 脚本）双 packageType，`priority` 字段处理"优先便携提示"。检查节奏建议"启动时一次 + 托盘手动"，不必学每小时轮询（单用户桌面工具没必要）。GitHub Releases 在国内可达性差，manifest URL 走可配置的镜像源——hanxi 已有托管工具多渠道先例（guoheview 私有 JSON）。
**成本**：低-中。安全口径必须硬：manifest 本身放 HTTPS + 发布私钥签名（MooTool 没做签名，hanxi 别抄这个短板）。

### 2.8 ★★ 细节与交互拾遗

| MooTool 细节 | 实现 | hanxi 借鉴 |
|---|---|---|
| 显示防休眠引用计数 | `displaySleepService.ts`：多请求方 set 聚合，任一开启即 prevent-display-sleep | fileshare 长传输、recordly 录屏、lan 大网段扫描时自动阻止睡眠，结束即释放——Windows 侧 SetThreadExecutionState 封装进 `platform/windows/` 顺手 |
| 托盘直挂功能动作 | `trayMenu.ts` 把 Host profile 切换/截图/取色直接放托盘 | hanxi 托盘+launcher.Dispatcher 已同构，等取色/二维码落地后把"取色""识码"挂进托盘二级组（正在做的 TrayItemGroup 正好承接） |
| registry 元数据含 keywords + supportsHistory 标志 | `next/src/app/toolRegistry.ts`，迁移状态机 placeholder→complete | `constants/navigation.ts` 已有单一来源，补 keywords 数组喂 Ctrl+K 搜索与历史标志位；多产品迁移状态机不需要 |
| 强调色 + 跟随系统 + Tab 仅图标模式 | 主题体系 | hanxi 设计系统本来就是 CSS 变量 token，**强调色一键切换（primary 色相 token 化）是低成本高感知项**；跟随系统 DWM 深色已有 |
| 大屏时钟/留言牌（"马上回来"） | time/messageBoard Tab | 桌面增强分区缺这类"挂墙"件；留言牌与透明窗基建同构，纯锦上添花，P3 |
| 旧版内嵌新版推荐 | `NextEditionRecommendationPanel` | 对 hanxi 唯一可用场景：douzy 内测版升级提示条的位置，已有人味 |

---

## 3. 明确不借鉴（避免误伤）

1. **7 条产品线重写**——是个人项目的技术探索与招聘名片，不是产品需要；hanxi 锁死 Go+Wails 单栈。
2. **翻译+单词本**——免费端点无 SLA，与 hanxi"外部依赖单点"债项正面冲突；真要翻译，托管一个开源自建（或并入 MCP 后让 AI 客户端自己干）。
3. **全局剪贴板监听**——MooTool 特意没做，我们也不做，理由同上（2.6）。
4. **Tesseract 图片 OCR**——hanxi 已有私发 hanxi-ocr（微信 4.0 引擎，中文效果与体积双优），见 [[hanxi-ocr-private-component]]，绝不倒退。
5. **Java 代码解释执行 / PDF 拆合 / 图片水印**——分别缺场景、撞托管件、撞 piclite。
6. **把托管模块改造成 Webview 内嵌上游 UI**——MooTool 没有托管概念，其全部经验只适用于自研微工具；hanxi 托管路线（JobObject + 版本管理）不因本次分析动摇。

---

## 4. 建议路线图

| 优先级 | 事项 | 承接 | 规模 |
|---|---|---|---|
| P0 | `internal/history` 公共包 + 通用面板，先接 ocr/portkill/envcheck | 2.2；同时是公共包下沉路线的样板工程 | 小 |
| P0 | `hanxi mcp` headless 子命令，首批只暴露只读工具（everything/ocr/envcheck/memo 检索），授权默认关闭 | 2.1 | 中 |
| P1 | `devkit` 复合模块第一批：JSON / 编解码加解密 / 时间戳 / 正则 / 快捷替换 / Cron（全纯 Go，全挂历史面板） | 2.3 + 2.2 | 中 |
| P1 | 模块服务生命周期门控（watcher 首次进入才起）+ `Start()` 禁同步 IO 写进架构规范 | 2.5 | 小 |
| P2 | devkit 第二批：调色板取色 / 文本对比 / 二维码；HTTP 客户端独立模块 | 2.3 | 中 |
| P2 | memo vault 化 + 空闲/失焦 git checkpoint 调度器 | 2.4 | 中 |
| P2 | hanxi 本体自更新（签名的 update-manifest + NSIS/MSIX 双通道） | 2.7 | 中 |
| P3 | Host 管理、UA 解析、Protobuf、留言牌、强调色、防休眠引用计数 | 2.3/2.8 | 按需 |

> 每步注意项目既有纪律：原子化提交、module.go 包注释即 ADR 的决策考古文化、`task check` 八项质量门、绑定漂移同步。

---

## 附：本次分析的 MooTool 关键源码坐标

- 历史抽象：`src/main/java/com/luoboduner/moo/tool/domain/TFuncHistory.java`、`util/FuncHistoryUtil.java`、`util/FuncHistorySupport.java`、`ui/component/FuncHistoryPanel.java`；Electron 镜像 `next/electron/main/historyRepository.ts`
- 工具注册：`src/.../ui/FuncTabCatalog.java`（分组+关键词）、`next/src/app/toolRegistry.ts`
- MCP：`next/electron/mcp/{index,tools,vaultAccess,vaultTools}.ts`、`next/electron/main/aiIntegration{Service,Config}.ts`、`next/doc/mootool-ai-integration.md`
- Git vault：`next/electron/main/{vaultGitService,vaultGitCheckpointScheduler,backupService}.ts`、`src/.../util/QuickNoteGitUtil.java`
- 懒加载：`JAVA_STARTUP_RESPONSIVENESS_PLAN.md`、`src/.../ui/startup/{StartupCoordinator,LazyToolManager}.java`
- 更新：`update-manifest.json`、`next/electron/main/{updateService,updateDownloader,updateManager}.ts`、`src/.../util/UpgradeUtil.java`
- 杂项：`next/electron/main/displaySleepService.ts`、`trayMenu.ts`
