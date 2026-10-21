# Hanxi 功能全景盘点

> 首盘点：2026-09-16（基于当日 `dev` 分支工作区现状，含未提交改动）；复核更新：2026-10-21（基于 dev HEAD `2c3fa57`，只对齐已失真口径并登记新增模块，首盘点评级不重评）
> 用途：供审查的功能清单与真实状态评估。完成度口径：**成熟**（功能闭环+测试+决策注释）/ **基本可用**（闭环但有缺口）/ **半成品** / **占位**。
> 结论先行：**50 个业务模块 + 10 项平台能力，无一是占位空壳**（首盘点时为 38 个，此后新增 12 个本轮只登记不深审，见表首说明）。首盘点 38 个中 26 成熟、11 基本可用、1 刻意止步（douzy）。主要债务已换代：**托管模板公共包下沉大体已偿**（versioncmp/artifact/supervisor/netx 等内核收口在途落地，残账见 §5.1 订正），"文案超前于实现"三处**两处已连根订正、仅剩 lan 一处在册**（§5.2）。

## 1. 项目概况

- **定位**：Windows 桌面工具工作台——自研 Go 原生模块 + 统一模板托管外部工具（版本管理 / JobObject 启停 / 托盘直达），Vue 3 + TS 自研设计系统，无第三方 UI 框架 / 无 vue-router / 无 Pinia。
- **技术栈**：Go 1.26 + Wails v3 beta.10；Windows JobObject / DPAPI / 原生提权 / 托盘。
- **形态**：单 exe（前端 go:embed 全内嵌）+ 便携数据目录 `hanxidata/`；NSIS/MSIX 双安装包，已弃用 UPX。
- **导航结构**：六个一级分组（网络与传输 / 系统管理 / 桌面增强 / 效率办公 / 媒体影音 / 开发者工具），路由单一来源 `constants/navigation.ts`，全部异步组件 + KeepAlive(10)。

## 2. 核心平台能力（非业务模块）

| # | 能力 | 位置 | 状态 | 要点 |
|---|------|------|------|------|
| 1 | 应用骨架与模块注册 | `internal/app/`、`internal/extapi/` | 成熟 | Composition Root 装配 50 模块（48 常驻 + 2 条件：memo/clipboard，口径见 docs/FACTS.md）；`EnsureModuleActive` 路由门禁；`ext:changed` 广播；停用即 OnDestroy+GC；单实例锁 + 提权重启 hash 回航 |
| 2 | 条目分发器 | `internal/launcher/` | 成熟 | 托盘与轮盘**共用**的 exe/command/route/group 四类派发；自研 argv 拆分防注入；exe 条目刻意不进 JobObject |
| 3 | 设置体系 | `internal/settings/` + `views/settings/` | 成熟（拆分已入库） | config.json 纯 JSON（DPAPI 仅 frpc 凭据用）；损坏自动隔离降级启动；原子落盘+候选提交回滚；数据根两级解析链（`hanxi.bind` 绑定指针 → exe 同级 `hanxidata/`，"便携/标准"双模式判定已随 F6 整体退役）。SettingsView 六分区拆分已入库（`2dcb5bf`），并已增至八分区（常规偏好/外观主题/托盘菜单/系统管理/存储目录/数据与存储/工作台入口/AI 接入） |
| 4 | 托盘与通知 | `tray.go`、`internal/notify/` | 成熟 | 托盘动态装配+热重建+group 原生子菜单（已入库 `5032682`）；通知 Hub 双通道（前端 Toast + Windows 原生 Toast，后者走 PowerShell WinRT，偏 hack 但可用） |
| 5 | Windows 平台层 | `internal/platform/windows/` | 成熟 | 提权/JobObject/DPAPI/自启/DWM 深色/强抢前台/.lnk 创建/GDI 椭圆裁剪(region.go)/MSIX 包管理脚本/killhelper 三重复核查杀 |
| 6 | 日志体系 | `logging/`、`ringbuf/`、`LogsView` | 成熟 | slog 按天落盘+保留期清理；**RedactHandler 层强制脱敏**（token/password 无法绕过）；环形缓冲供子进程控制台；前端轮询+过滤+竞态丢弃 |
| 7 | 双栏外壳 | `App.vue`、`AppNavRail/Sidebar` | 成熟 | 图标轨道(六分组+角标+运行绿点) + 二级面板 + Ctrl+K 命令面板，同一门禁链；三份旧路由表已收编单一来源 |
| 8 | 错误与产品契约 | `domain/`、`product/` | 成熟 | 统一 AppError（Code/Action/Retryable，前端按 Code 渲染）；产品身份常量 |
| 9 | 子进程插件体系 | `extapi.LevelExternal` + `PLUGINS_ARCHITECTURE.md` | **仅契约预留** | manifest + JSON-RPC 只有接口占位，未实现；设计文档已标注"历史留档，以 ARCHITECTURE.md 为准" |
| 10 | MCP 服务面（AI 接入） | `internal/mcp/`、设置"AI 接入"分区、`hanxi mcp` 无头模式 | 复核登记（未深审） | 首盘点后显性成面：工具数与授权键数以 docs/FACTS.md 现值为准（由 docsfacts 闸门锁死，本档不复抄）；设置页提供客户端接入向导 |

构建/发布：Taskfile 体系 + 一键脚本（环境自检→build→组装便携包），`task check` 八项质量门（含 Wails 绑定漂移检查）——成熟。

## 3. 功能总表（50 模块 = 首盘点深审 38 + 复核登记 12）

**复核登记说明**：12 个新增模块（piik、sysinfo、softver、heukms、rammap、msgboard、clipboard、webapp、windterm、termora、dbx、gonavi）本轮只按 `constants/navigation.ts` 的分组与 docs/PRD.md §2 能力矩阵登记名目/形态，**未逐一深审**，状态标"复核登记（未深审）"不等于占位或达标，评级待后轮审查。

### 3.1 网络与传输（11，复核 +piik）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| frpc | frp 内网穿透 | 托管 frpc + 原生控制面 | 成熟 |
| fileshare | 局域网文件快传 | 原生 Go HTTP 服务 | 成熟 |
| lan | 局域网扫描 | 原生（ICMP+ARP） | 基本可用 |
| portscan | 网络测试（首盘点名"端口扫描"） | 原生 TCP+指纹+代理/SSH 检查 | 基本可用 |
| piik | Piik 屏幕分享 | headless 服务型托管 | 复核登记（未深审） |
| publicip | IP 查看与诊断 | 原生 | 基本可用 |
| wifi | WiFi 密码 | 原生壳 + netsh | 基本可用 |
| flclash | FlClash 代理 | 全托管 | 成熟 |
| ddnsgo | ddns-go | 托管 + 内嵌 Web 控制台 | 成熟 |
| subnetdesk | 局域网远控 | 全托管（RustDesk LAN fork） | 成熟 |
| rustdesk | RustDesk 远控 | 全托管 | 成熟 |

- **frpc**：多项目多实例、TOML 双向生成/解析（兼容 v1.x/v0.x）、连接态解析转通知、Token DPAPI 加密、运行时明文配置停止即擦除。风险：崩溃残留明文配置；projectMu 锁条目只增不删（量小无碍）。
- **fileshare**：内嵌静态前端扫码互传；`os.Root` 沙箱、Range 续传、口令门禁（HMAC+Bearer 恒时比较）、审计事件；全组测试最齐。风险：默认端口 80 + 免密默认可用是产品语义，需用户自知。
- **lan**：CIDR/短范围解析、64 并发、ARP 补 MAC、备注持久化。风险：**注释宣称 mDNS/主机名识别未实现**（Hostname 恒空）；无测试。
- **portscan**：并发 2000+限速、Banner/HTTP 指纹、SOCKS5/HTTP 代理扫描、按任务取消。复核：页面已改名**「网络测试」**并标签页化（端口扫描/代理连通性/SSH，`f40385b`）——代理判定口径"能经代理拿到公网出口 IP 才算通"，五个出口源并发各查、全结果并排、结论取多数一致并报 n/m 源一致；SSH 分两个动作——版本横幅交换（自写协议首回合、零第三方依赖）与密码试登录（x/crypto/ssh 完整握手，副作用单独上界面、密码不落盘不进结果）。拨号与出口核验内核已上收 `packages/go/netx`（`469e3e1`），并新增默认 skip 的真机验证夹具（`2c3fa57`）。首盘点风险之一"Nmap 超前宣称"已连根订正（见 §5.2）；仍在册：代理模式下 TCP 探测仍退化为直连（CONNECT 隧道只作用于 http.Client 一侧，netx 注释自认原样搬运）。
- **publicip**：公网 v4/v6 多源容错、TTL 缓存、ping/tracert。风险：tracert 文本解析脆弱；与 portscan 各养一份出网 IP 探测源清单。
- **wifi**：netsh 列 SSID+明文密码。风险：解析依赖中英文输出，其他语言系统返空；吞错误导致前端分不清"无网络"与"失败"。
- **flclash**：便携 zip 四层校验、外部实例感知+唤窗、托盘/快捷方式。复核：包注释"闲置自动退出"的文档漂移**已修**——包注释按实况改写（主流程委托 `packages/go/{artifact,supervisor}` 共享内核），`shouldIdleQuit` 保留恒 false + 决策注释；5s 轮询常驻。
- **ddnsgo**：托管组唯一带**内嵌 WebView 子窗直挂上游 Web UI** 的模块；配置恒用上游路径无缝接管。风险：`versionCompare` 私有拷贝；退出路径 consoleWin 不显式 Close。
- **subnetdesk / rustdesk**：刻意孪生的双形态（便携全托管 + MSI 安装版注册表探测），代码约 95% 相同；托盘无窗时 SpawnWindow 派生新窗。风险见 §5 重复建设。

### 3.2 系统管理（9，复核 +sysinfo/softver/heukms/rammap）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| portkill | 释放端口 | 原生（含提权 helper） | 成熟（测试仍薄，见 §5.3） |
| bcu | BC 卸载器 | 全托管 BCU | 成熟 |
| litemonitor | 硬件监控 | 全托管 LiteMonitor | 成熟 |
| heukms | HEU KMS 激活 | 全托管（RAR5 解包器特例） | 复核登记（未深审） |
| rufus | 启动盘制作 | 全托管 Rufus | 成熟 |
| envcheck | 开发环境检测 | 原生 9 子包 | 成熟（全组最厚） |
| softver | 软件版本 | 原生（注册表×PE 双口径+更新雷达） | 复核登记（未深审） |
| sysinfo | 系统信息 | 原生（只读采集） | 复核登记（未深审） |
| rammap | RAMMap 内存观察 | 全托管（Sysinternals 直链） | 复核登记（未深审） |

- **portkill**：四表端口定位 + PID/路径/启动时间三重复核后查杀，`-mode=killhelper` 提权链路完整。复核：首盘点两笔账已销——notify 路由前缀不一致已修（通知现同走 `/ext/portkill`）；"安全敏感模块唯一零测试"已破零（现有 1 份测试文件，仍偏薄）。残余风险：提权取消判定依赖英文系统输出。
- **bcu**：portable/fdd 双变体、.NET 运行时探测、单实例协议唤窗。风险：shouldIdleQuit 恒 false 死代码。
- **litemonitor**：托管模板 + 独有 GetRuntimeStatus 条件提示条；Win32 直操作唤窗（上游无信使）。
- **rufus**：单 exe 三重校验、预置 ini 关上游更新检查。风险：要求 Hanxi 本身提权；托管组唯一无桌面快捷方式（理由未写明）。
- **envcheck**：git/go/node/java/python/dotnet 本机×官方版本对比、npm 全局 CLI 白名单装升卸。唯一 0.5.0 版本号、20+ 测试含官方接口冒烟。风险：`probe_tmp/` 目录留在模块树。

### 3.3 桌面增强（10，复核 +msgboard）

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
| msgboard | 桌面留言板 | 原生（真透明置顶窗 + KeepAwake 聚合器） | 复核登记（未深审） |

- **quickmenu**：Quicker 式验证品——右键长按 450ms 弹圆形轮盘。三件套：WH_MOUSE_LL 钩子（吞按下/短按回放/超位移让位拖拽/任务栏旁路）+ 单例真透明 WebView 弹窗（SVG 扇区 + GDI 椭圆裁剪点击穿透 + DPI 兜底）+ 与托盘共用 `launcher.Dispatcher`。复核：首盘点"工作区有改动"那批已全部入库——托盘二级分组 `TrayItemGroup`/轮盘双层开关随 `5032682` 落地，轮盘升级为扇区级联外扩双环并切独立账本（`ade2338`）；当时点名的**"后端 512 DIP 与前端 wheelGeometry.ts 同源文件不存在"风险已收口**——`frontend/src/components/quickmenu/wheelGeometry.ts` 已入库且带跨语言契约锁 spec（`55118d9`）。残余：触发时长/容差仍编译期常量（factory 450ms、区间 200~1500ms，外化见 §5.6）。
- **markeron**：独有 ToggleAnnotate 三态编排（WM_COPYDATA 信使转发标注开关）。
- **nanazip**：不走 JobObject 模板，MSIX 走系统部署栈（PowerShell 常驻脚本 + 钉死证书指纹），校验链全组最重。风险：每次查询冷启 PowerShell ~1.8s（已缓解）。
- **eartrumpet**：官方直装渠道检测 + winget 清单交叉比对；Exit 按 PID 指纹复核强杀（上游无优雅通道，决策记录最详细）。风险：上游短时效证书偶发 CERT_E_EXPIRED。
- **mangodisk**：模板标准件；OpenWindow 四态编排。风险：service_test 薄壳；TryLock vs Lock 与 markeron 漂移。
- **translucenttb**：独有 ResetState（信使触发上游重放配置修复任务栏）。已知坑：卸载连 settings.json 一起删（配置在版本目录内）。
- **keyviz**：上游无便携包 → `msiexec /a` 提取。Quit 强杀（有落盘论证）。
- **quicklook**：桌面增强组唯一有**运行期控制通道**（命名管道 Quit/Reload）。复核：service/version/instance 测试已补，store 测试仍缺。
- **guoheview**：唯一非 GitHub 渠道（果核私有 JSON 接口）+ 多实例语义。**单点风险：接口变更即版本管理全瘫**；闭源仅 MD5 信任锚。

### 3.4 效率办公（8，复核 +clipboard/webapp）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| memo | 随手记 | 纯原生 | 成熟 |
| clipboard | 剪贴板历史 | 原生（持续捕获 + MCP 只读检索） | 复核登记（未深审） |
| webapp | 网页应用 | 原生（WebView2 多窗） | 复核登记（未深审） |
| everything | 文件搜索 | 托管 + 内嵌 ES.exe 搜索 | 成熟 |
| snipaste | 截图贴图 | 托管（闭源官方渠道） | 成熟 |
| papertodo | 桌面便签 | 全托管 | 成熟 |
| wechat | 微信机器人 | 纯原生网关客户端 | 成熟 |
| ocr | 文字识别 | 托管私发组件 hanxi-ocr | 基本可用偏成熟 |

- **memo**：JSON 单文件、标签云、敏感遮罩、跨模块 `GetService` 直调（fileshare 投递箱联动）。测试仅 1 例但逻辑简单。
- **everything**：官网便携包四级校验 + `-startup/-quit` 契约 + 空闲 3 分钟自动退出；内嵌搜索走官方 ES.exe（MIT，窗口消息 IPC）——托管组唯一**在 Hanxi 窗口内出真实搜索结果**的。风险：ES 版本常量需手动跟升。
- **snipaste**：官网 SHA-1 清单沿用上游口径；刻意不随 Hanxi 退出（OnDestroy 不收尾）。
- **papertodo**：双变体（self-contained/no-runtime + .NET 探测）；上游无哈希→降级完整性链（有 TROUBLESHOOTING 沉淀）；PolyForm Noncommercial 只托管不分发。
- **wechat**：iLink 网关多账号机器人，AES 自实现、凭据持久化、多模态密文收发。风险：**协议非公开且版本号硬编码，微信侧变更即失联**。前端 2176 行孤本已拆 composable+7 组件。
- **ocr**：消费方托管特例——hanxi-ocr 为**私发组件**（微信 4.0 OCR 引擎封装，含外部闭源件、无再分发许可，永不进公开包），隔离靠"设置路径 > 同级目录 > PATH"三级纯函数解析，无版本管理只探活转发。首盘点时"工作区未提交的 +232 行"**组件导入通道**已入库（`c252d8e`：拖 `.exe` 校验接管 + 原生拖图免 dataURL + 统一事件回执）。引用式导入，用户挪走 exe 即断。

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

### 3.6 开发者工具（8，复核 +windterm/termora/dbx/gonavi）

| 模块 | 中文名 | 形态 | 状态 |
|------|--------|------|------|
| ccswitch | 模型切换 | 全托管 CC Switch | 成熟（digest 硬校验标杆） |
| vscode | 编辑器 | 双形态托管（便携+User Installer） | 成熟 |
| paseo | Agent 编排器 | 全托管（数据共享用户目录） | 成熟 |
| windterm | WindTerm 终端 | 全托管（官方便携 zip） | 复核登记（未深审） |
| termora | Termora 终端 | 全托管（jpackage 自带 JRE） | 复核登记（未深审） |
| dbx | DBX 数据库 | 全托管（DBX_DATA_DIR 数据改道） | 复核登记（未深审） |
| gonavi | GoNavi 数据库 | 全托管（数据不改道，与 DBX 方案相反） | 复核登记（未深审） |
| wsl | WSL2 工作台 | **纯原生**，全仓最重工程 | 成熟（离线层面） |

- **ccswitch**：托管模板标杆实例，四层 sha256 全组最严。切换操作在上游窗口完成。
- **vscode**：仅微软官方 CDN；双引擎各一套启停。两个已知约束（安装版应用内更新致记录漂移、与用户日常实例同组）经拍板接受并前端如实预告。
- **paseo**：双通道版本；数据刻意与自装实例共享；用户在界面内升级会装出平行副本（仅提示条预告）。
- **wsl**：十项流式体检、发行版六操作+克隆/导入导出、wsl.conf/.wslconfig 图形编辑（语法闸门+写前备份）、VHDX 三级瘦身、NAT 端口转发账本。120 用例（时点值，首盘点时全仓第一；全仓测试现规模见 docs/FACTS.md）。**核心风险（docs 自述）：克隆/瘦身/导入/netsh 应用均未经真机端到端验证**，本机无 CGO 跑不了 -race，结论全部来自离线桩。

## 4. 工作区未提交改动（进行中的工作）【复核已了结：2026-10-21】

> **对账帽**：本节为 2026-09-16 时点快照，所列批次此后已按主题拆分原子提交全部入库——设置六分区拆分 `2dcb5bf`（后增至八分区）、托盘二级分组与轮盘双层开关 `5032682`、轮盘几何同源 `wheelGeometry.ts` + 跨语言契约锁 `55118d9`、OCR 组件导入通道 `c252d8e`；审查要点①（几何单一事实源）②（拆 3 个 commit）均已满足。原文照录如下，仅作史账：

当时 dev 分支有一批相互关联的未提交变更（`git diff` internal 侧 +560/-116），主题是**「托盘菜单二级分组 + 轮盘联动 + 设置页分区化」**：

1. `settings/store.go`：`TrayItemGroup` 嵌套 Children + 深拷贝；
2. `app/tray.go`：托盘原生子菜单渲染；
3. `quickmenu`：`QuickMenuTwoTier` 开关 + 子盘换层 + 512 DIP 几何；`platform/windows/region.go` 配套；
4. `ocr`：组件导入通道（拖 exe 接管 + 原生拖放 + app.go 接线）；
5. 前端：`SettingsView.vue` 删除 → `views/settings/` 六分区（新目录未跟踪）；navigation/AppSidebar/icons/OcrView/QuickMenuView 及绑定同步。

**审查要点**：① quickmenu 前后端几何单一事实源（`wheelGeometry.ts`）尚未建立，提交前必须补齐；② 这批改动横跨 settings/quickmenu/ocr 三个主题，按项目原子化提交规范宜拆 3 个 commit。

## 5. 横向技术债与风险（按建议处理优先级排序）

1. **托管模板公共包下沉（首盘点判"最大债务"，复核：大体已偿，残账在列）**：首盘点点名三块，现两块已收口——版本比较逻辑下沉 `internal/platform/versioncmp`（`1599d4a` 起，`23db497`/`e56fa73` 批量推广，18 个模块仅剩薄壳转发）；外部实例 watcher 有 `packages/go/externalquit`；此外 `packages/go/{artifact,supervisor}` 双内核委托达版本面 27、实例面 24（现值以 docs/FACTS.md 为准），上游列表、出网拨号（netx）、目录测量（dirstats）、用户数据目录样板（modpath）也已上收。仍在账：每模块 `remote.go`（Release 列表/下载适配层）38 份各自维护；下载锁与 `resolveActiveVersion` 自愈回退仍 25 个模块各养一份；store 新增字段手工同步的事故前科（WechatAccounts 漏拷）未变。**建议：残账收口优先级降一档，按模块增补顺带迁，不专门开仓。**
2. **文案超前于实现（首盘点 3 处，复核：2 处已连根订正、1 处在册）**：①portscan「集成 Nmap」→ 已随 `ee7fd0d` 连根订正（包注释 + 模块 Description + cataloggen 再生 + 前端 Info 同步，现口径"不含 Nmap"），docs/FACTS.md §三留档并由 docsfacts 禁词闸门反回写；②flclash 包注释「闲置自动退出」→ 已按实况改写（见 §3.1 订正）；③**lan `module.go` 注释仍宣称"结合 mDNS/主机名识别设备"而仓内无该实现（Hostname 恒空）——唯一在册**，建议二选一：实现或改文案，对外 Info 文案尤其要紧。
3. **测试覆盖不均**：安全/复杂敏感区最薄——**portkill（提权查杀）已破零但仍薄（1 份）**、bili23 缺 service/store 测试、quicklook service 测试已补（store 仍缺）、lan/wifi 无测试；subnetdesk/rustdesk 孪生体修一处漏一处。**wsl 是真机未验证**（不可测环境所迫，但属于已声明的风险）。
4. **小 bug 与气味**：wifi/ListProfiles 吞错误；bcu shouldIdleQuit 恒 false（决策固化件，非疏漏）；ddnsgo consoleWin 退出不显式 Close（首盘点口径，本轮未复核）。已销账：portkill 通知路由前缀不一致（已统一 `/ext/portkill`）、envcheck `probe_tmp/`（已删）；公网 IP 探测源清单两处重复已收一半——portscan 份上收为 `netx.EgressProvidersV4`，publicip 的更宽列表刻意暂不合并（合并会改其查询顺序与测试面，netx 注释如实登记欠账）。
5. **外部依赖单点**：guoheview 私有 JSON 接口、wechat 非公开网关协议、eartrumpet 短时效证书、piclite/keyviz 的 `msiexec /a` 提取路线——这些是托管闭源/非标准渠道工具的固有代价，代码内均有决策注释，属"已知可控"而非疏漏。
6. **真未落地的功能**（非缺陷，是边界）：extapi LevelExternal 子进程插件（契约预留）、Language 设置项（模型有字段无 UI）、quickmenu 触发参数外化（验证期刻意固化）。

## 6. 总体评价

- **完成度**：38 模块无一占位（首盘点口径，复核时点 50 模块、新增 12 个未深审）；`module.go` 包注释即 ADR 的"决策考古"文化仍是全仓最大资产（为什么不走模板、为什么只能这样校验，全部可考）。
- **公共包下沉**：首盘点"阈值已到"的判断（versionCompare 14 份拷贝）已被兑现——比较逻辑收口 versioncmp、托管主流程委托 artifact/supervisor 双内核（委托面 27/24，现值见 docs/FACTS.md）；残账转移到每模块 remote.go 与 resolveActiveVersion（§5.1），性质从"结构性债务"降为"顺手账"。
- **最需盯的三件事（复核换代）**：wsl 动盘操作的真机验证（原地未动）、portkill 的测试补齐（已破零仍薄）、lan 包注释的 mDNS 超前宣称（§5.2 唯一在册）；quickmenu 几何漂移一项已收口销账。
