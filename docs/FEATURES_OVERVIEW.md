# Hanxi 功能全景盘点

> 盘点日期：2026-09-16（基于 `dev` 分支工作区现状，含未提交改动）
> 用途：供审查的功能清单与真实状态评估。完成度口径：**成熟**（功能闭环+测试+决策注释）/ **基本可用**（闭环但有缺口）/ **半成品** / **占位**。
> 结论先行：**38 个业务模块 + 9 项平台能力，无一是占位空壳**；26 个成熟、11 个基本可用、1 个刻意止步（douzy）。主要债务不在功能，而在**托管模板未下沉公共包**与**少数"文案超前于实现"**。

## 1. 项目概况

- **定位**：Windows 桌面工具工作台——自研 Go 原生模块 + 统一模板托管外部工具（版本管理 / JobObject 启停 / 托盘直达），Vue 3 + TS 自研设计系统，无第三方 UI 框架 / 无 vue-router / 无 Pinia。
- **技术栈**：Go 1.26 + Wails v3 beta.10；Windows JobObject / DPAPI / 原生提权 / 托盘。
- **形态**：单 exe（前端 go:embed 全内嵌）+ 便携数据目录 `hanxidata/`；NSIS/MSIX 双安装包，已弃用 UPX。
- **导航结构**：六个一级分组（网络与传输 / 系统管理 / 桌面增强 / 效率办公 / 媒体影音 / 开发者工具），路由单一来源 `constants/navigation.ts`，全部异步组件 + KeepAlive(10)。

## 2. 核心平台能力（非业务模块）

| # | 能力 | 位置 | 状态 | 要点 |
|---|------|------|------|------|
| 1 | 应用骨架与模块注册 | `internal/app/`、`internal/extapi/` | 成熟 | Composition Root 装配 38 模块；`EnsureModuleActive` 路由门禁；`ext:changed` 广播；停用即 OnDestroy+GC；单实例锁 + 提权重启 hash 回航 |
| 2 | 条目分发器 | `internal/launcher/` | 成熟 | 托盘与轮盘**共用**的 exe/command/route/group 四类派发；自研 argv 拆分防注入；exe 条目刻意不进 JobObject |
| 3 | 设置体系 | `internal/settings/` + `views/settings/` | 基本可用→成熟（拆分未提交） | config.json 纯 JSON（DPAPI 仅 frpc 凭据用）；损坏自动隔离降级启动；原子落盘+候选提交回滚；便携/标准双目录模式。**刚把单页 SettingsView 拆成六分区**（常规/主题/托盘/存储/系统/工作台），旧页已删、新目录未跟踪 |
| 4 | 托盘与通知 | `tray.go`、`internal/notify/` | 成熟 | 托盘动态装配+热重建+group 原生子菜单（未提交）；通知 Hub 双通道（前端 Toast + Windows 原生 Toast，后者走 PowerShell WinRT，偏 hack 但可用） |
| 5 | Windows 平台层 | `internal/platform/windows/` | 成熟 | 提权/JobObject/DPAPI/自启/DWM 深色/强抢前台/.lnk 创建/GDI 椭圆裁剪(region.go)/MSIX 包管理脚本/killhelper 三重复核查杀 |
| 6 | 日志体系 | `logging/`、`ringbuf/`、`LogsView` | 成熟 | slog 按天落盘+保留期清理；**RedactHandler 层强制脱敏**（token/password 无法绕过）；环形缓冲供子进程控制台；前端轮询+过滤+竞态丢弃 |
| 7 | 双栏外壳 | `App.vue`、`AppNavRail/Sidebar` | 成熟 | 图标轨道(六分组+角标+运行绿点) + 二级面板 + Ctrl+K 命令面板，同一门禁链；三份旧路由表已收编单一来源 |
| 8 | 错误与产品契约 | `domain/`、`product/` | 成熟 | 统一 AppError（Code/Action/Retryable，前端按 Code 渲染）；产品身份常量 |
| 9 | 子进程插件体系 | `extapi.LevelExternal` + `PLUGINS_ARCHITECTURE.md` | **仅契约预留** | manifest + JSON-RPC 只有接口占位，未实现；设计文档已标注"历史留档，以 ARCHITECTURE.md 为准" |

构建/发布：Taskfile 体系 + 一键脚本（环境自检→build→组装便携包），`task check` 八项质量门（含 Wails 绑定漂移检查）——成熟。

## 3. 功能总表（38 模块）

### 3.1 网络与传输（10）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| frpc | frp 内网穿透 | 托管 frpc + 原生控制面 | 成熟 |
| fileshare | 局域网文件快传 | 原生 Go HTTP 服务 | 成熟 |
| lan | 局域网扫描 | 原生（ICMP+ARP） | 基本可用 |
| portscan | 端口扫描 | 原生 TCP+指纹 | 基本可用 |
| publicip | IP 查看与诊断 | 原生 | 基本可用 |
| wifi | WiFi 密码 | 原生壳 + netsh | 基本可用 |
| flclash | FlClash 代理 | 全托管 | 成熟 |
| ddnsgo | ddns-go | 托管 + 内嵌 Web 控制台 | 成熟 |
| subnetdesk | 局域网远控 | 全托管（RustDesk LAN fork） | 成熟 |
| rustdesk | RustDesk 远控 | 全托管 | 成熟 |

- **frpc**：多项目多实例、TOML 双向生成/解析（兼容 v1.x/v0.x）、连接态解析转通知、Token DPAPI 加密、运行时明文配置停止即擦除。风险：崩溃残留明文配置；projectMu 锁条目只增不删（量小无碍）。
- **fileshare**：内嵌静态前端扫码互传；`os.Root` 沙箱、Range 续传、口令门禁（HMAC+Bearer 恒时比较）、审计事件；全组测试最齐。风险：默认端口 80 + 免密默认可用是产品语义，需用户自知。
- **lan**：CIDR/短范围解析、64 并发、ARP 补 MAC、备注持久化。风险：**注释宣称 mDNS/主机名识别未实现**（Hostname 恒空）；无测试。
- **portscan**：并发 2000+限速、Banner/HTTP 指纹、SOCKS5/HTTP 代理扫描、按任务取消。风险：**Info 宣称"集成 Nmap"完全未做**；代理模式 TCP 探测退化为直连（注释自认）。
- **publicip**：公网 v4/v6 多源容错、TTL 缓存、ping/tracert。风险：tracert 文本解析脆弱；与 portscan 各养一份出网 IP 探测源清单。
- **wifi**：netsh 列 SSID+明文密码。风险：解析依赖中英文输出，其他语言系统返空；吞错误导致前端分不清"无网络"与"失败"。
- **flclash**：便携 zip 四层校验、外部实例感知+唤窗、托盘/快捷方式。风险：包注释"闲置自动退出"已被决策禁用（文档漂移）；5s 轮询常驻。
- **ddnsgo**：托管组唯一带**内嵌 WebView 子窗直挂上游 Web UI** 的模块；配置恒用上游路径无缝接管。风险：`versionCompare` 私有拷贝；退出路径 consoleWin 不显式 Close。
- **subnetdesk / rustdesk**：刻意孪生的双形态（便携全托管 + MSI 安装版注册表探测），代码约 95% 相同；托盘无窗时 SpawnWindow 派生新窗。风险见 §5 重复建设。

### 3.2 系统管理（5）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| portkill | 释放端口 | 原生（含提权 helper） | 成熟但零测试 |
| bcu | BC 卸载器 | 全托管 BCU | 成熟 |
| litemonitor | 硬件监控 | 全托管 LiteMonitor | 成熟 |
| rufus | 启动盘制作 | 全托管 Rufus | 成熟 |
| envcheck | 开发环境检测 | 原生 9 子包 | 成熟（全组最厚） |

- **portkill**：四表端口定位 + PID/路径/启动时间三重复核后查杀，`-mode=killhelper` 提权链路完整。风险：**安全敏感模块却是唯一零测试**；notify 路由 `/portkill` 与 Nav `/ext/portkill` 前缀不一致（疑似 bug）；提权取消判定依赖英文系统输出。
- **bcu**：portable/fdd 双变体、.NET 运行时探测、单实例协议唤窗。风险：shouldIdleQuit 恒 false 死代码。
- **litemonitor**：托管模板 + 独有 GetRuntimeStatus 条件提示条；Win32 直操作唤窗（上游无信使）。
- **rufus**：单 exe 三重校验、预置 ini 关上游更新检查。风险：要求 Hanxi 本身提权；托管组唯一无桌面快捷方式（理由未写明）。
- **envcheck**：git/go/node/java/python/dotnet 本机×官方版本对比、npm 全局 CLI 白名单装升卸。唯一 0.5.0 版本号、20+ 测试含官方接口冒烟。风险：`probe_tmp/` 目录留在模块树。

### 3.3 桌面增强（9）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| quickmenu | 快捷轮盘 | **原生 Go**（全局钩子+透明窗） | 基本可用偏成熟 |
| markeron | 屏幕标注 | 全托管 MarkerOn | 成熟 |
| nanazip | 归档解压 | MSIX 包管理托管 | 成熟 |
| eartrumpet | 音量控制 | MSIX 直装渠道托管 | 成熟 |
| mangodisk | 磁盘清理 | 全托管（托管模板标准件） | 成熟 |
| translucenttb | 任务栏透明 | 全托管 | 成熟 |
| keyviz | 按键可视化 | 全托管（MSI 提取） | 成熟 |
| quicklook | 空格秒预览 | 全托管 | 成熟 |
| guoheview | 果核看图 | 托管（非 GitHub 私有接口） | 基本可用偏成熟 |

- **quickmenu（重点，工作区有改动）**：Quicker 式验证品——右键长按 450ms 弹圆形轮盘。三件套：WH_MOUSE_LL 钩子（吞按下/短按回放/超位移让位拖拽/任务栏旁路）+ 单例真透明 WebView 弹窗（SVG 扇区 + GDI 椭圆裁剪点击穿透 + DPI 兜底）+ 与托盘共用 `launcher.Dispatcher`。本次未提交改动：`TrayItemGroup` 二级分组 + `QuickMenuTwoTier` 开关 + 设置页 `TraySection.vue`，把轮盘/托盘原生子菜单/设置编辑器三方打通。**提交前风险：后端几何已升 512 DIP 并注释"与前端 wheelGeometry.ts 同源"，但该文件不存在**，前端仍 376 viewBox，裁剪圈/收起半径/视觉盘缘三者不重合。触发时长/容差仍编译期常量。
- **markeron**：独有 ToggleAnnotate 三态编排（WM_COPYDATA 信使转发标注开关）。
- **nanazip**：不走 JobObject 模板，MSIX 走系统部署栈（PowerShell 常驻脚本 + 钉死证书指纹），校验链全组最重。风险：每次查询冷启 PowerShell ~1.8s（已缓解）。
- **eartrumpet**：官方直装渠道检测 + winget 清单交叉比对；Exit 按 PID 指纹复核强杀（上游无优雅通道，决策记录最详细）。风险：上游短时效证书偶发 CERT_E_EXPIRED。
- **mangodisk**：模板标准件；OpenWindow 四态编排。风险：service_test 薄壳；TryLock vs Lock 与 markeron 漂移。
- **translucenttb**：独有 ResetState（信使触发上游重放配置修复任务栏）。已知坑：卸载连 settings.json 一起删（配置在版本目录内）。
- **keyviz**：上游无便携包 → `msiexec /a` 提取。Quit 强杀（有落盘论证）。
- **quicklook**：九模块唯一有**运行期控制通道**（命名管道 Quit/Reload）。缺 store/service 测试。
- **guoheview**：唯一非 GitHub 渠道（果核私有 JSON 接口）+ 多实例语义。**单点风险：接口变更即版本管理全瘫**；闭源仅 MD5 信任锚。

### 3.4 效率办公（6）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| memo | 随手记 | 纯原生 | 成熟 |
| everything | 文件搜索 | 托管 + 内嵌 ES.exe 搜索 | 成熟 |
| snipaste | 截图贴图 | 托管（闭源官方渠道） | 成熟 |
| papertodo | 桌面便签 | 全托管 | 成熟 |
| wechat | 微信机器人 | 纯原生网关客户端 | 成熟 |
| ocr | 文字识别 | 托管私发组件 hanxi-ocr | 基本可用偏成熟（有未提交改动） |

- **memo**：JSON 单文件、标签云、敏感遮罩、跨模块 `GetService` 直调（fileshare 投递箱联动）。测试仅 1 例但逻辑简单。
- **everything**：官网便携包四级校验 + `-startup/-quit` 契约 + 空闲 3 分钟自动退出；内嵌搜索走官方 ES.exe（MIT，窗口消息 IPC）——托管组唯一**在 Hanxi 窗口内出真实搜索结果**的。风险：ES 版本常量需手动跟升。
- **snipaste**：官网 SHA-1 清单沿用上游口径；刻意不随 Hanxi 退出（OnDestroy 不收尾）。
- **papertodo**：双变体（self-contained/no-runtime + .NET 探测）；上游无哈希→降级完整性链（有 TROUBLESHOOTING 沉淀）；PolyForm Noncommercial 只托管不分发。
- **wechat**：iLink 网关多账号机器人，AES 自实现、凭据持久化、多模态密文收发。风险：**协议非公开且版本号硬编码，微信侧变更即失联**。前端 2176 行孤本已拆 composable+7 组件。
- **ocr**：消费方托管特例——hanxi-ocr 为**私发组件**（微信 4.0 OCR 引擎封装，含腾讯专有件，永不进公开包），隔离靠"设置路径 > 同级目录 > PATH"三级纯函数解析，无版本管理只探活转发。工作区未提交的 +232 行是**组件导入通道**（拖 `.exe` 校验接管 + 原生拖图免 dataURL + 统一事件回执）。引用式导入，用户挪走 exe 即断。

### 3.5 媒体影音（4）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| recordly | 录屏 | 托管（NSIS 静默实现免安装） | 成熟（测试最厚） |
| piclite | 图片压缩 | 托管（msiexec /a 提取） | 成熟 |
| bili23 | B 站下载 | 全托管 | 基本可用偏成熟 |
| douzy | 全能下载器 | **仅版本+下载托管** | 基本可用（刻意止步） |

- **recordly**：托管组唯一保留**空闲 5 分钟自动退出**的 + stable/beta 双通道。
- **piclite**：上游无优雅退出 → Quit 即 JobObject 强杀（配置即时落盘论证）；缺 store_test。
- **bili23**：Quit 三态如实上报 + 显式 ForceStop（托管组唯一不静默强杀兜底的）。风险：**测试最薄弱**（无 service/store 测试）且命名风格（`Service`/`Status`）偏离模板约定。
- **douzy**：内测期+闭源壳+无便携包 → 只做"列版本→下载→校验→拉起安装向导"，不做进程托管。**这是有完整决策记录的产品边界，不是半成品**。

### 3.6 开发者工具（4）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| ccswitch | 模型切换 | 全托管 CC Switch | 成熟（digest 硬校验标杆） |
| vscode | 编辑器 | 双形态托管（便携+User Installer） | 成熟 |
| paseo | Agent 编排器 | 全托管（数据共享用户目录） | 成熟 |
| wsl | WSL2 工作台 | **纯原生**，全仓最重工程 | 成熟（离线层面） |

- **ccswitch**：托管模板标杆实例，四层 sha256 全组最严。切换操作在上游窗口完成。
- **vscode**：仅微软官方 CDN；双引擎各一套启停。两个已知约束（安装版应用内更新致记录漂移、与用户日常实例同组）经拍板接受并前端如实预告。
- **paseo**：双通道版本；数据刻意与自装实例共享；用户在界面内升级会装出平行副本（仅提示条预告）。
- **wsl**：十项流式体检、发行版六操作+克隆/导入导出、wsl.conf/.wslconfig 图形编辑（语法闸门+写前备份）、VHDX 三级瘦身、NAT 端口转发账本。120 用例全仓第一。**核心风险（docs 自述）：克隆/瘦身/导入/netsh 应用均未经真机端到端验证**，本机无 CGO 跑不了 -race，结论全部来自离线桩。

## 4. 工作区未提交改动（进行中的工作）

当前 dev 分支有一批相互关联的未提交变更（`git diff` internal 侧 +560/-116），主题是**「托盘菜单二级分组 + 轮盘联动 + 设置页分区化」**：

1. `settings/store.go`：`TrayItemGroup` 嵌套 Children + 深拷贝；
2. `app/tray.go`：托盘原生子菜单渲染；
3. `quickmenu`：`QuickMenuTwoTier` 开关 + 子盘换层 + 512 DIP 几何；`platform/windows/region.go` 配套；
4. `ocr`：组件导入通道（拖 exe 接管 + 原生拖放 + app.go 接线）；
5. 前端：`SettingsView.vue` 删除 → `views/settings/` 六分区（新目录未跟踪）；navigation/AppSidebar/icons/OcrView/QuickMenuView 及绑定同步。

**审查要点**：① quickmenu 前后端几何单一事实源（`wheelGeometry.ts`）尚未建立，提交前必须补齐；② 这批改动横跨 settings/quickmenu/ocr 三个主题，按项目原子化提交规范宜拆 3 个 commit。

## 5. 横向技术债与风险（按建议处理优先级排序）

1. **托管模板未下沉公共包（最大债务）**：38 个模块里 20+ 个是同构托管形态，各自维护 version/instance/store 三件套（合计估 1.5 万行级），下载锁、`resolveActiveVersion` 自愈回退、5s watcher 近逐字复制。具体失同步证据：`versionCompare` 在 **14 个模块 service.go 本地拷贝**，而共享包 `internal/platform/versioncmp` 已存在且 bcu 已在用；`remote.go` 下载器六份同构；store 新增字段需手工同步有事故前科（WechatAccounts 漏拷注释在案）。**建议：至少先把 versionCompare、下载器、外部实例 watcher 三块收口，不必合并整模块。**
2. **文案超前于实现（3 处声明与代码不符）**：portscan「集成 Nmap」完全未做；lan「mDNS/主机名识别」未做；flclash 包注释「闲置自动退出」已被固化禁用。建议二选一：实现或改文案，对外 Info 文案尤其要紧。
3. **测试覆盖不均**：安全/复杂敏感区最薄——**portkill（提权查杀）零测试**、bili23/quicklook 缺 service/store 测试、lan/wifi 无测试；subnetdesk/rustdesk 孪生体修一处漏一处。**wsl 是真机未验证**（不可测环境所迫，但属于已声明的风险）。
4. **小 bug 与气味**：portkill 通知路由前缀不一致（疑似跳空）；wifi/ListProfiles 吞错误；envcheck `probe_tmp/` 已提交；bcu shouldIdleQuit 恒 false；ddnsgo consoleWin 退出不显式 Close；公网 IP 探测源清单两处重复维护（publicip/portscan）。
5. **外部依赖单点**：guoheview 私有 JSON 接口、wechat 非公开网关协议、eartrumpet 短时效证书、piclite/keyviz 的 `msiexec /a` 提取路线——这些是托管闭源/非标准渠道工具的固有代价，代码内均有决策注释，属"已知可控"而非疏漏。
6. **真未落地的功能**（非缺陷，是边界）：extapi LevelExternal 子进程插件（契约预留）、Language 设置项（模型有字段无 UI）、quickmenu 触发参数外化（验证期刻意固化）。

## 6. 总体评价

- **完成度**：38 模块无一占位；`module.go` 包注释即 ADR 的"决策考古"文化是全仓最大资产（为什么不走模板、为什么只能这样校验，全部可考）。
- **一致性好，但有阈值已到**：托管模板从"复制起步避免过早抽象"演进到今天 20+ 实例，公共包下沉的收益已明确大于成本（versionCompare 14 份拷贝是临界信号）。
- **最需盯的三件事**：wsl 动盘操作的真机验证、portkill 的测试补齐、quickmenu 几何漂移在提交前收口。
