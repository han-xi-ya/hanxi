# Hanxi 需求与产品设计规范（PRD）

> **产品名称**：Hanxi
> **文档版本**：v1.1  
> **更新日期**：2026-09-03  
> **目标平台**：Windows 10 22H2+ / Windows 11 x64（纯 Windows 原生调用）  
> **发布形态**：**绿色便携单二进制**（默认便携版，支持同级 `data/` 目录免安装常驻）  
> **技术栈**：Go 1.24+ / Wails v3.0-beta / Vue 3 + TypeScript / TailwindCSS  

---

## 1. 产品定位与核心设计

Hanxi 是一个面向 Windows 的**开源工具工作台**，用于集中安装、管理与运行常用开源软件。产品由两条主线构成：

1. **自建功能模块**：frpc 内网穿透、微信机器人、端口扫描/查杀、局域网发现、公网诊断、WiFi 密码、开发环境检测、局域网文件快传、极客随手记等，能力原生内置于单二进制；
2. **第三方桌面工具托管**：Snipaste、Everything、QuickLook、Keyviz、LiteMonitor、CCSwitch、MarkerOn、FlClash、NanaZip、EarTrumpet、BCU、MangoDisk、Recordly、PaperTodo、PicLite、果核看图、ddns-go 等 17 款工具，以统一"托管模式"纳管（版本管理 + 完整性校验 + 进程监管 + 前端控制台）。

所有能力以平等模块身份注册到统一入口，按需懒加载启停。

> **v0.3.0 品牌断代**：Hanxi 使用全新的 `hanxi.exe`、`io.hanxi.desktop`、`Run\\Hanxi` 与 `%APPDATA%/Hanxi`，不迁移或读取旧产品数据。

### 1.1 核心设计原则

1. **单体零开销懒加载**：所有功能均内建在同一个 Go 单二进制中。用户未启用的功能 **0 协程常驻、0 缓冲区分配、0 定时器占用**；在设置页随时启停，模块导航由后端注册表动态驱动。
2. **内核级进程防护 (JobObject)**：所有受管子进程（`frpc.exe` 及托管工具 exe）均绑定到 Windows 内核 JobObject 作业对象，主进程退出或崩溃时由操作系统内核强制级联销毁，彻底杜绝孤儿进程与端口残留；闭源免费工具（如 Snipaste）采用**脱管模式**保留原生托盘，不强杀。
3. **硬件级凭据安全 (DPAPI)**：敏感密钥（frpc Token 等）使用 Windows 原生 `CryptProtectData` (DPAPI) 基于当前登录用户凭据硬件加密落盘，杜绝明文存储风险；停止实例时自动清除运行时生成的临时 TOML 文件。
4. **真实连接感知**：实时嗅探 frpc 日志流特征词，精准映射连接状态（已连接 / 认证失败 / 重连中 / 异常），消除"进程存活但穿透未通"的假绿状态；托管工具则以进程枚举/互斥体探测真实运行态。
5. **桌面深度集成**：支持系统托盘常驻（单击切换显隐）、关闭窗口最小化到托盘、Windows 开机自启动管理、原生 UAC 按需提权与全局分级通知中心。
6. **托管合规红线**：第三方工具一律**用户侧按需下载**，Hanxi 仓库与安装包**不捆绑、不再分发**任何第三方二进制；下载完整性采用多层校验兜底（GitHub digest / 官方 SHA256SUMS / 官方哈希清单 / 字节数 + PE 版本核对），各工具许可证义务在 `docs/THIRD_PARTY_NOTICES.md` 持续登记。

---

## 2. 模块能力矩阵

### 2.1 自建功能模块（10）

| 模块名称 | 模块标识 | 核心能力说明 | 安全与特权机制 |
| :--- | :--- | :--- | :--- |
| **frpc 穿透管理** | `frpc` | 多实例并发、全协议与 Visitor 支持、配置可视化 ⇋ TOML 双向同步、`frp://` 链接一键分享、批量端口映射导入、官方/镜像版本下载与本地导入、日志实时嗅探 | DPAPI 凭据加密、JobObject 绑定、临时 TOML 自动清理 |
| **微信机器人** | `wechat` | iLink 协议扫码登录、多账号会话保持、长轮询事件监听、图文/文件/Markdown 多模态加密推送、入站文件落盘 | AES 密钥加密传输、启动预激活常驻监听 |
| **端口扫描** | `portscan` | 高并发 TCP 探测、Gonmap 深度协议指纹识别（Nginx/MySQL/Redis 等）、常用端口预设、结果流式渲染 | 并发限流与超时熔断控制 |
| **端口查杀** | `portkill` | IP Helper 快速查占用、进程身份三重复核（PID+启动时间+路径）、一键释放 | Windows `ShellExecuteEx(runas)` 原生 UAC 按需提权 |
| **局域网扫描** | `lan` | IPv4 `/24` 自动探测、ICMP Echo 存活扫描、ARP/邻居表读取、MAC 厂商 OUI 识别、设备备注 | 只读网络枚举、取消上下文感知 |
| **网络诊断** | `publicip` | 双栈公网 IP 查询、网卡拓扑透视、ICMP Ping 稳定性统计、Traceroute 路由追踪 | 原生 API、无黑窗口静默调用 |
| **WiFi 密码** | `wifi` | 已保存 Wi-Fi 配置与明文密码查看（`netsh wlan`） | 仅限当前用户可见配置、GBK 解码归一 |
| **开发环境检测** | `envcheck` | Git/Go/Node/Java/Python/.NET 本机工具链盘点、官网最新版本通道查询与按本机版本线置顶、包管理器升级提示、资源管理器定位安装路径、.NET 并排安装与官方支持线对照 | 只读检测、外网仅访问官方站点 |
| **局域网文件快传** | `fileshare` | 零客户端局域网分享站（手机扫码互传）、监听端点枚举、收件箱管理、文本投递联动随手记 | 局域网边界监听、服务显式起停 |
| **极客随手记** | `memo` | 本地持久化备忘/代码片段、快速新建、置顶、一键复制、统计汇总 | 敏感内容脱敏开关 |

### 2.2 第三方工具托管模块（15）

统一骨架：`version/` 版本管理子包（ListReleases / DownloadVersion / RemoveVersion / 本地导入）+ `instance/` 实例引擎子包（JobObject 启停、状态探测、唤窗、跟随退出）+ 前端托管控制台视图。

| 模块名称 | 模块标识 | 上游项目 | 许可证 | 托管形态与差异点 |
| :--- | :--- | :--- | :--- | :--- |
| **Snipaste 截图** | `snipaste` | 官网 download.snipaste.com | 闭源免费 | **脱管模式**：保留原生托盘/快捷键，退出不强杀；官网 sha-1 清单校验 |
| **Everything 检索** | `everything` | 官网 www.voidtools.com | 专有免费（ES.exe MIT） | 托管 + **内嵌秒级搜索**（官方 ES.exe CLI）+ 后台索引托管 |
| **QuickLook 预览** | `quicklook` | QL-Win/QuickLook | GPL-3.0 | 便携 zip 安装、命名管道 Quit/Reload 优雅退出 |
| **Keyviz 键显** | `keyviz` | mulaRahul/keyviz | GPL-3.0 | MSI 托管安装提取、互斥体探测 |
| **LiteMonitor 监控** | `litemonitor` | Diorser/LiteMonitor | 上游未声明 | 处理 `requireAdministrator` 直拒与 UIPI 边界、首启配置种子 |
| **CCSwitch 切换** | `ccswitch` | farion1231/cc-switch | MIT | Tauri 单实例协议唤窗纯托管 |
| **MarkerOn 标注** | `markeron` | ifer47/markeron | MIT | 单实例二次拉起实现标注开关 |
| **ddns-go 动态域名** | `ddnsgo` | jeessy2/ddns-go | MIT | 纯 CLI + Web 面板：后门变量绕服务劫持、端口就绪判定、退出配置写静默期、独立子 Webview 面板 |
| **FlClash 代理** | `flclash` | chen08209/FlClash | GPL-3.0 | 第二实例不唤窗时 EnumWindows 直接置前台 |
| **NanaZip 压缩** | `nanazip` | M2Team/NanaZip | 多元许可 | **MSIX 安装管理型**：官方 MSIXBundle 当前用户安装/卸载，不绑 JobObject |
| **EarTrumpet 音量** | `eartrumpet` | File-New-Project/EarTrumpet | MIT+EE 条款 | **官方直装渠道纳管**：AppInstaller 清单 + winget SHA-256 交叉校验、AUMID 激活 |
| **BCU 卸载** | `bcu` | BCUninstaller/Bulk-Crap-Uninstaller | Apache-2.0 | 闲置自动退出、`Global\BCU-singleinstance` 唤窗、.NET 运行时依赖检测 |
| **MangoDisk 清理** | `mangodisk` | harry0703/MangoDisk | GPL-3.0 | 原版 GUI 纯托管 |
| **Recordly 录屏** | `recordly` | webadderallorg/Recordly | AGPL-3.0+条款 | NSIS 静默安装进托管目录、双发布通道、禁用上游自动更新、未签名风险提示 |
| **PaperTodo 便签** | `papertodo` | snownico0722/PaperTodo | PolyForm Noncommercial | self-contained / no-runtime 双变体、官方命令通道唤窗/收拢/退出 |
| **PicLite 压图** | `piclite` | amiaoapp/PicLite | GPL-3.0 | `msiexec /a` 管理提取免管理员提权安装 |

### 2.3 宿主服务

| 服务 | 归属 | 核心能力说明 |
| :--- | :--- | :--- |
| **常规与系统** | `app` (AppService) | 系统托盘常驻（单击切换显隐）、关闭最小化、开机自启（注册表 Run 项）、系统快捷直达、模块启停与懒激活调度（`EnsureModuleActive`）、运行日志查看与清理 |
| **通知中心** | `notify` (NotificationService) | 全局分级通知（信息/成功/警告/错误）、`notify:received` 事件推送、历史查询与已读管理 |
| **品牌身份** | `product` | 名称/标识/可执行文件名/数据目录/版本号单一真相源（当前 `0.3.0`） |

---

## 3. 核心工作流设计

### 3.1 frpc 多实例与生命周期管理

```
[新建/编辑项目] ──► [DPAPI 加密保存] ──► [生成临时 TOML] ──► [拉起 frpc.exe (JobObject)]
                                                                       │
[停止项目/退出] ◄── [擦除临时 TOML] ◄── [释放进程与监听] ◄── [日志实时嗅探状态]
```

1. **新建项目**：填写 frps 服务端地址、端口及 Token（保存时自动使用 DPAPI 加密为密文存储）。
2. **规则配置**：支持添加 TCP、UDP、HTTP、HTTPS、STCP、XTCP 等代理规则，提供批量端口区间（如 `8080-8085`）一键导入。
3. **启动联调**：生成对应配置 TOML，拉起沙箱进程；实时监听输出流，精准更新连接状态（`connected`、`auth_failed`、`reconnecting` 等）。
4. **配置分享**：支持导出为 `frp://<base64>` 协议链接，便于团队成员一键导入完整配置。
5. **停止与清理**：停止实例时立即安全杀灭子进程，并即时删除 `runtime/frpc/frpc-<id>.toml` 临时文件。

### 3.2 第三方工具托管标准工作流

```
[上游 Releases/官网清单侦查] ──► [多层完整性校验下载] ──► [安装进托管目录]
                                                              │
[跟随 Hanxi 退出/优雅退出] ◄── [运行态探测/唤窗] ◄── [JobObject 拉起受管进程]
```

1. **版本侦查与下载**：`version/` 子包拉取上游 Releases 或官网哈希清单，完整性按工具形态多层兜底（GitHub API digest → 官方 SHA256SUMS → 官方清单 → 字节数 + MZ/PE 版本核对），支持镜像加速与指数退避重试；同时支持本地文件导入脱管网版本。
2. **安装布局适配**：按上游发行形态选择免提权优先的安装策略——便携 zip 直解、`msiexec /a` 管理提取（MSI 无 zip 场景）、NSIS `/S /D=` 静默安装、MSIX/AppInstaller 交 Windows 为当前用户部署。
3. **运行态与唤窗**：`instance/` 子包以进程枚举/命名互斥体探测运行态；唤起窗口按上游能力择优——官方单实例命令通道（show/hide/exit）、`EnumWindows` 直接置前台、AUMID 激活；全部操作先做进程指纹复核防误杀。
4. **退出治理**：提供"跟随 Hanxi 退出"开关；有优雅通道（命名管道、命令信使）优先优雅退出，无通道则指纹复核后强杀（上游事务性写入保证安全）；受管进程绑定 JobObject 由内核兜底清理。
5. **合规边界**：下载由用户本机直连上游发起，Hanxi 不捆绑不再分发；上游数据（配置/笔记/录制）存于 Hanxi 托管目录且卸载时原地保留；AGPL/PolyForm 等强约束工具不做品牌融合展示。

### 3.3 端口查杀与安全特权

1. **端口检索**：输入端口号（TCP/UDP），通过 Windows IP Helper API 毫秒级返回占用进程详情（PID、进程名、启动时间、路径及命令行）。
2. **系统保护检测**：内置核心受保护系统进程白名单（PID 0, PID 4, `csrss.exe`, `explorer.exe` 等），防止误杀系统关键服务。
3. **释放与提权**：优先尝试普通权限终止；若遇到权限不足，调用 Windows 原生 `ShellExecuteEx` 触发标准 UAC 弹窗提权执行，完成端口释放。

### 3.4 微信机器人助手

1. **扫码认证**：通过前端展示登录二维码，长轮询检测扫码确认状态。
2. **消息监听与推送**：支持长轮询接收外部指令，并可通过配置向微信群/好友推送文本、Markdown、图文和文件。

---

## 4. 平台与非功能性要求

1. **运行环境**：Windows 10 22H2 及以上 / Windows 11 x64。
2. **冷启动内存**：初始加载仅 **18MB ~ 25MB**（仅 Wails 内核与主框架），极低系统资源占用。
3. **绿色便携**：
   - 优先检测可执行文件同级 `data/` 目录；若存在则以便携模式运行，数据全落入 `data/`；
   - 若不存在则默认落入 `%APPDATA%/Hanxi/`，不污染系统其它目录；
   - frpc 版本/运行时、托管工具版本与实例数据统一落在对应模块的托管子目录下，随 `data/` 整体拷贝迁移。
4. **编译与交付**：纯 Go 原生编译（`CGO_ENABLED=0`），无 GCC / MinGW 外部依赖，产出单一 `hanxi.exe`。
