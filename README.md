# Hanxi

> **开源工具工作台**（Go + Wails v3 + Vue 3）
> 集中安装、管理与运行常用开源软件。Hanxi 以两条主线组织能力：**自建功能模块**（frpc 内网穿透、网络诊断、端口查杀、开发环境检测、WSL2、局域网快传、随手记、右键快捷菜单等）与**第三方桌面工具托管**（Snipaste、Everything、QuickLook、Keyviz、LiteMonitor、NanaZip、EarTrumpet、果核看图、ddns-go、VS Code、TranslucentTB、Rufus、RustDesk / SubnetDesk 远程桌面、Bili23 Downloader、Paseo 编排器、WindTerm/Termora 终端、RAMMap 内存观察、抖音下载器等 28 款），统一提供版本管理、完整性校验、JobObject 进程托管、系统托盘与本地数据管理能力。
>
> **v0.3.0 品牌断代**：产品标识、进程名和标准数据目录已切换为 Hanxi，不读取旧版数据、自启项或单实例标识。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-%E2%89%A51.26-00ADD8?logo=go)](https://go.dev/)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0--beta-DF1C24?logo=wails)](https://v3.wails.io/)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vue.js)](https://vuejs.org/)

---

## 🌟 已集成工具

### 一、自建功能模块

#### 1. ⚡ frpc 多实例与沙箱进程管理
- **多实例独立并发**：每个项目对应独立 `frpc.exe` 进程，支持同时联调多个内网穿透服务端。
- **全协议与精细化配置**：
  - 支持 **TCP / UDP / HTTP / HTTPS / STCP / XTCP** 全类型穿透；
  - **STCP/XTCP 访客端 (Visitor)** 完整支持：通过 `[[visitors]]` 架构轻松打通安全点对点与 P2P 隧道；
  - **底层传输协议自由切换**：支持 `TCP (默认)`、`KCP (抗高丢包)`、`QUIC`、`WebSocket`、`WSS`；
  - **高级企业级特性**：支持上游代理 (`proxyURL`)、HTTP `hostHeaderRewrite` 虚拟主机重写、`proxyProtocolVersion` (v1/v2 真实 IP 透传) 及单规则带宽限速。
- **可视化表单 ⇋ TOML 源码双向实时同步**：支持在可视化表单与原始 TOML 之间无损一键切换编辑与语法校验。
- **穿透项目与版本管理统一工作台**：单入口 Tab 自由切换穿透项目与版本管理，官方 GitHub Releases / 国内镜像加速一键下载、SHA256 完整性校验、指数退避重试与本地 `frpc.exe` 快速导入。
- **Windows JobObject 隔离**：利用操作系统内核级作业对象（JobObject），主程序退出或崩溃时所有子进程一并被内核强制清理，**彻底告别孤儿进程与端口残留占用**。
- **连接状态实时嗅探**：实时分析 `frpc` 控制台日志特征词，毫秒级感知服务端连接状态（已连接 / 认证失败 / 重连中 / 异常），告别“进程存活但穿透未通”的假绿状态。
- **Windows DPAPI 硬件级凭据加密**：服务端连接 Token 采用 Windows 原生 `CryptProtectData` (DPAPI) 绑定当前用户凭据安全加密落盘，杜绝明文凭证泄露；停止实例时自动擦除运行时生成的临时 TOML 配置文件。
- **配置一键分享与导入**：支持 `frp://<base64>` 协议链接一键复制分享，支持多端一键导入 `frp://` 链接或原始 `frpc.toml` 文件内容。
- **代理规则批量端口区间导入**：支持 `8080-8085`、`8080,8081,8082` 等离散/连续端口范围批量映射生成规则。
- **实时日志与脱敏流**：单实例独立日志抽屉，自动脱敏 Token/Secret，ANSI 彩色终端日志过滤。

#### 2. 🤖 微信机器人助手 (WeChat ClawBot)
- **扫码快捷登录**：内置微信机器人协议，支持前端一键拉取二维码并轮询扫码确认状态，支持多账号与会话保持。
- **AES 安全加密通信**：支持配置 Token 与 EncodingAESKey 进行高强度消息加解密。
- **长轮询与事件监听**：支持长轮询实时监听微信事件，接收群聊/私聊指令，入站文件自动落盘。
- **多模态消息推送**：支持文本、图文卡片、Markdown 与多格式文件推送。
- **图片消息体验**：入站图片自动注册附件，聊天气泡内嵌缩略预览并支持打开/另存；出站图片显示本地预览，支持「打开图片」与「打开目录」直达系统查看器/资源管理器。

#### 3. 🔎 端口扫描与服务指纹识别 (PortScan)
- **高并发端口探测**：支持单端口、连续区间（`8000-8080`）、离散列表与常用预设（Web开发/数据库/TOP 100/系统保留端口）。
- **Nmap 深度指纹识别**：集成纯 Go Nmap 探针引擎（`gonmap`），自动识别协议类型、组件版本（Nginx/OpenSSH/MySQL/Redis 等）与 Web 状态。
- **快捷联动操作**：Web 端口浏览器一键直达，支持全量开放端口一键复制与结果流式实时渲染。

#### 4. 🎯 端口占用精准排查与一键释放 (PortKill)
- 快速查询指定本地端口（TCP/UDP）占用情况与全量监听端口表。
- 深度提取占用进程名称、PID、启动时间、可执行文件完整路径及命令行参数。
- 采用 Windows 原生 `ShellExecuteEx(runas)` 快速 UAC 提权，支持三级提权与强制安全释放。

#### 5. 📡 局域网活跃设备与服务发现 (LAN Scanner)
- 局域网 CIDR 自动探测与并发活跃 IP 扫描。
- MAC 地址与网络设备硬件厂商（OUI 数据库）精准匹配识别。
- 支持设备备注标注与 IP 一键复制，扫描进度实时推送、可随时取消。

#### 6. 🔍 网络全能诊断与公网探测
- **出口公网 IP & IPv6**：聚合国内外高可用查询源，双栈自动回退，精准识别家宽、专线与公网 IPv6。
- **全网卡拓扑透视**：物理网卡、虚拟网卡（Hyper-V/WSL/VPN）、网关、DNS、临时 IPv6 地址一览。
- **ICMP Ping 稳定性探测**：内置纯 Go 探测引擎，支持发包计数、实时丢包率、最小/最大/平均 RTT 统计。
- **Traceroute 路由追踪**：系统级路由跳跃追踪（Windows `tracert`），内置 `CREATE_NO_WINDOW` 静默无黑窗口执行。
- **WiFi 密码查看**：一键导出本机已保存 Wi-Fi 配置与明文密码（`netsh wlan`）。

#### 7. 🧪 开发环境检测与官网版本通道 (EnvCheck)
- **本机工具链一键盘点**：自动检测 Git、Go、Node.js、Java、Python、.NET 等开发工具的安装状态、版本与安装路径，支持在资源管理器中直接定位。
- **官网最新版本的查询**：实时拉取各工具官方发布通道的最新版本，按本机版本线置顶展示，并给出 winget 等包管理器升级命令提示。
- **.NET 深度检测**：并排列出全部已安装的 .NET 运行时/SDK，对照微软官方支持线（LTS/STS/EOL）标记生命周期状态。

#### 8. 📤 局域网文件快传 (FileShare)
- **零客户端分享站**：在局域网起一个文件/文本快传服务，手机扫码即可互传，无需安装任何 App。
- **访问口令保护**：可选口令门禁——访客需验证后进入工作台（会话 Cookie 30 天免重输，支持 Bearer 直连集成）；未设口令即免密共享。注意：服务默认监听全部网卡，跨网段收敛为待决策项。
- **端点自动枚举**：自动列出本机各网卡可用监听端点，收件箱目录统一管理收到的文件。
- **文本投递联动随手记**：收到的文本/剪贴板内容可自动落入「极客随手记」，形成收集闭环。

#### 9. 📝 极客随手记 (Memo)
- 本地持久化备忘与代码片段：快速新建、置顶、全文检索与一键复制。
- 敏感内容脱敏开关：开启后列表页遮蔽敏感片段，兼顾分享与隐私。
- 数据统计与实时事件同步（与快传等模块联动刷新）。

#### 10. 🐧 WSL2 就绪体检、版本管理、发行版运维工作台与端口转发 (WSL)
- **流式只读体检**：十项门槛（系统版本 / 架构 / 虚拟化 / 监控程序 / 可选功能·虚拟机平台 / VBS / Store / 安装形态 / GitHub 通道 / WSL 本体）先渲染骨架，探针、本机运行时、网络三路并发"先到先点亮"，完成逐项落章 ✓/⚠/✕，输出「可开启 / 注意项 / 硬阻塞」三态结论与修正建议。
- **「已禁止(403)」根因诊断**：Go 原生探测 `api.github.com`（HTTP 客户端自动跟随 WinINET 系统代理，与浏览器同出口），一键区分"硬件不满足"与"API 被网络侧拦截"两种装不上成因；列表数据源 API 优先、被拦自动降级 Releases Atom 订阅源并如实标注。
- **安装形态取证与正规卸载**：MSIX 用户包 / MSI 系统版 / 系统启动器三路信号 × 运行时版本互证，专治"设置只显一个、卸了另一个还活着"；卸载双路各走官方卸载器（msiexec /X + Remove-AppxPackage），Lxss 注册残留如实巡查报告而非暗中删注册表；虚拟机平台状态免管理员检测（WMI）+ 开启/关闭对称操作，单独预告 Hyper-V/模拟器共用影响。
- **官方版本管理与发行版**：microsoft/WSL Releases 列表 × 本机版本关系判定（过期缓存 stale 标注）；MSI 由**应用内下载器**直落系统"下载"文件夹（进度条 + 可取消 + 打开位置，不经过浏览器），并按本机架构自动标记"本机"、排序首选，异架构仅作备选；在线发行版清单白名单一键安装——但装前先过虚拟化装载门控：虚拟机平台未启用、或"已启用待重启生效"（CBS `RebootPending`）时 WSL2 起不了虚拟机，安装注定失败，此时前端直接亮黄条预告并禁用按钮、后端提权前拦下指路，不再让用户对着闪退的黑窗口反复点"成功"。
- **白名单提权通道**：「一键开启」固定 `--install --no-distribution` 语义——只装 WSL 本体与虚拟机平台，**绝不自动捆绑发行版**（Linux 系统由用户在版本页手动挑选）；所有变更操作参数后端固定，经 UAC 提权窗口执行且**退出码一律如实传播**——提权 `Start-Process` 加 `-PassThru` 回传子进程退出码（`wsl --install` 失败不再被误报"执行完毕"）、DISM 逐条传播 `$LASTEXITCODE`（首条失败不再被链尾成功吞没）；重启引导只打开系统设置页，不代点重启。
- **发行版实例管理控制台**：本机发行版从只读表升级为行内操作面——**唤终端 / 设默认 / 终止 / 导出 / 迁移 / 删除**。列表跨语言可靠：运行态与默认以 `wsl -l -q`（quiet 输出即发行版原名，与系统语言无关）为判据、本地化状态列只进 title 备查，安装路径与 VHDX 占用经 Lxss 注册表只读巡查补齐，辅助通道失效逐级降级不编造。安全口径与安装面同构：每个操作先对实时 `-l -q` 名单做白名单校验（前端字符串绝不直接进命令），后端每发行版单飞闸 + 迁移全局互斥闸兜并发，完成后一律复采取复采真相。语义如实：「启动」即唤终端会话、不做后台保活（WSL 无进程自动停机是平台语义）；导出支持 **tar.gz 压缩 / tar 未压缩** 两格式，固定落「下载\WSL 导出」（后端拼装文件名、同名不覆盖），本会话导出工件全量登记在「导出记录」逐份直达位置与复制路径；迁移提权单会话内 `--shutdown` → `--manage --move` →瞬时冲突重试至多 5 次→注册表复验位置；删除先 `--terminate` 再 `--unregister`（防注销挂起），数据销毁级确认链点名占用与路径。
- **发行版运维工作台**：在六操作之上补齐五项实用能力——**取证**（行内只读抽屉：VHDX 逻辑/实占双口径 + 稀疏标志、guest 根盘 df 用量、发行版 IPv4；停止的发行版绝不进 guest，防止"只读取证"顺手把它拉起）；**克隆**（目标卷空间预检 → 停源 → 流式拷贝 VHDX 带进度事件 → `--import --vhd` 挂为新实例，要求 WSL 2.7.3+，版本不足指路"导出→导入"；**全程可取消**：拷贝段取消自动清理半成品、挂载段取消保留已拷盘并点名路径）；**导入**（`wsl --import` 把导出 tar / 可信 rootfs 落成新增发行版，名称防撞与目标目录校验同迁移口径）；**wsl.conf 编辑器**（读→改→写回 /etc/wsl.conf：开机探针 + `test -f` 退出码断存在性（不猜跨语言文案）、语法闸门、`[user] default` 存在性经 guest `id -u` 求证、写前 root 备份 `.bak`、tee 覆盖后读回复验，保存成功即引导"终止使其生效"）；**磁盘瘦身**（VHDX 膨胀是 WSL 头号抱怨——三级工作流：强制全量 tar 备份（**目录可指到空闲卷**）→ fstrim+停机 → Tier1 `Optimize-VHD`（Hyper-V 模块在位才尝试）→ 省量不足转 Tier2 注销重导入；空间预检覆盖源盘与备份盘两卷；**备份/停机段可取消、动盘段拒绝取消**并说明中间态风险；任何一步失败都点名备份位置，数据盘路径一律由注册表推导、绝不接受前端传入）。
- **网络模式感知与 .wslconfig 编辑器**：解析宿主 `~/.wslconfig` 的 `[networking] networkingMode`，取证抽屉与端口转发页实时显示模式徽标（NAT / 桥接 / 镜像网络）；镜像模式下端口转发页明示"localhost 直通、通常无需转发"，不再对着 NAT 时代的药方吃错药。全局配置编辑沿用三步防线：INI 语法闸门 + **networkingMode 白名单**（nat/bridged/mirrored——写错全员失联，必须拦在落盘前）+ 备份 `.wslconfig.hanxi.bak` + 原子写读回复核；保存后引导「🌑 全停」（`wsl --shutdown`，与迁移/克隆/瘦身共重操作闸）使其即时生效。
- **NAT 端口转发管理**：WSL2 NAT 下本机访问不到发行版服务，正规解法 `netsh interface portproxy`。规则存于 Hanxi 自有账本（数据文件），默认监听 127.0.0.1、选 0.0.0.0 时明示局域网曝光；列表对照 netsh 现态逐条标注"已生效 / IP 漂移·需重应用 / 待应用 / 发行版未运行"（guest IP 并行探测），漂移后一键重同步；应用走**单批提权脚本单次 UAC**（先删后加幂等、逐条传播 `$LASTEXITCODE`、与迁移/克隆/瘦身共闸），防火墙放行用固定命名 `Hanxi WSL <端口>` 随规则同步；**非本工具登记的系统转发只展示不触碰**，「清理托管」一键摘除自家全部痕迹、账本损坏另有"仅清账本文件"逃生口；未运行的发行版被跳过且不会被转发流程顺手拉起。

#### 11. 🖱 右键快捷菜单 (QuickMenu)
- **Quicker 式最小验证**：任意界面右键长按（默认 450ms、松手前即弹）→ 光标处弹出无边框快捷菜单 → 点击条目即时启动。
- **普通右键零损失识别**：进程内 `WH_MOUSE_LL` 低级钩子（非注入）采用"吞按下、短按 SendInput 回放"策略，拖拽让位、任务栏矩形旁路；钩子随模块停用/进程退出由系统自动摘除，零残渣。
- **条目复用托盘配置**：与托盘右键菜单完全共用同一份配置与启动器分发，不新增第二份配置面；弹窗为常驻隐藏单例 frameless 窗口，失焦收起 + 全局点击观察兜底，多显示器/屏幕边缘经物理↔DIP 换算与工作区钳位。

### 二、第三方桌面工具托管（托管模式）

Hanxi 将 28 款常用开源/免费桌面工具纳入统一管理。除 frpc 等特例外，托管模块共享同一套标准骨架：

- **版本管理**：上游 Releases / 官网清单侦查 → 多层完整性校验（GitHub digest / SHA256SUMS / 官方哈希清单 / 字节数 + PE 版本核对）→ 下载、本地导入与版本删除；
- **进程托管**：Windows JobObject 绑定的启停引擎、进程枚举/互斥体探测运行态、Win32 唤窗 / 官方单实例命令通道唤起窗口、"跟随 Hanxi 退出"开关（**默认关闭**：Hanxi 退出不影响已托管工具独立运行）、桌面快捷方式创建；
- **合规红线**：**全部按需用户侧下载，Hanxi 仓库与安装包不捆绑、不再分发任何第三方二进制**（详见 [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)）。

| 模块 | 上游项目 | 许可证 | 托管要点 |
|---|---|---|---|
| **Snipaste 截图贴图** | 官方站点 www.snipaste.com（非 GitHub 分发） | 闭源免费软件 | **脱管模式**：保留原生托盘与快捷键，Hanxi 退出不强杀；官网 sha-1 清单校验下载 |
| **Everything 文件检索** | voidtools（官网分发） | 专有免费软件（ES.exe 为 MIT） | 托管之外**内嵌秒级搜索控制台**：经官方 ES.exe CLI 直查结果、后台索引托管、打开/定位文件 |
| **QuickLook 空格预览** | QL-Win/QuickLook | GPL-3.0 | 便携 zip 安装、命名管道 Quit/Reload 优雅退出、低级键盘钩子强杀零残渣 |
| **Keyviz 按键可视化** | mulaRahul/keyviz | GPL-3.0 | MSI 托管安装与提取、互斥体探测、常驻可视化工具强杀式退出 |
| **LiteMonitor 硬件监控** | Diorser/LiteMonitor | 上游未声明 | 处理 `requireAdministrator` 清单直拒与 UIPI 边界、Win32 直操作唤窗、首启配置种子关更 |
| **CCSwitch 供应商切换** | farion1231/cc-switch | MIT | Claude Code / Codex 供应商切换器纯托管，Tauri 单实例协议唤窗 |
| **MarkerOn 屏幕标注** | ifer47/markeron | MIT | 单实例二次拉起实现标注开关（开启/停止批注） |
| **FlClash 代理客户端** | chen08209/FlClash | GPL-3.0 | Clash 系跨平台客户端托管，第二实例不唤窗时改用 EnumWindows 直接置前台 |
| **NanaZip 归档压缩** | M2Team/NanaZip | 多元许可（MIT + 7-Zip/LGPL/unRAR 等） | **MSIX 安装管理型**：官方 stable MSIXBundle 交由 Windows 为当前用户安装/卸载 |
| **EarTrumpet 音量控制** | File-New-Project/EarTrumpet | MIT（含 Excluded Entities） | **官方直装渠道纳管**：AppInstaller 清单 + winget SHA-256 交叉校验、AUMID 激活启动 |
| **BCU 批量卸载** | BCUninstaller/Bulk-Crap-Uninstaller | Apache-2.0 | Bulk Crap Uninstaller 托管，闲置自动退出 + .NET 运行时依赖检测 |
| **MangoDisk 磁盘清理** | harry0703/MangoDisk | GPL-3.0 | 原版 GUI 纯托管，磁盘扫描/清理/卸载功能均由上游提供 |
| **Recordly 开源录屏** | webadderallorg/Recordly | AGPL-3.0（附加条款） | NSIS 静默安装进托管目录、双发布通道切换、注入开关禁用上游自动更新 |
| **PaperTodo 桌面便签** | snownico0722/PaperTodo | PolyForm Noncommercial 1.0.0 | self-contained / no-runtime 双变体切换，唤窗/收拢/退出走官方命令通道 |
| **PicLite 图片压缩** | amiaoapp/PicLite | GPL-3.0 | 上游仅提供 MSI：采用 `msiexec /a` 管理提取免管理员提权安装 |
| **果核看图** | 果核 ghxi.com（官方自建接口分发，非 GitHub） | 闭源免费软件（Certum 签名） | **多实例上游**：无单实例锁→进程名探测 + 按自有 PID 唤窗/关窗；官方发布接口仅 MD5 + 当前版本，便携 zip 顶层包装目录收割 |
| **ddns-go 动态域名** | jeessy2/ddns-go | MIT | 纯 CLI + Web 面板形态：`DDNS_GO_DAEMON=1` 注入绕开上游服务劫持分支、TCP 端口就绪判定、Quit 前配置写静默期防截断、面板走独立子 Webview 窗口 |
| **RustDesk 公网远控** | rustdesk/rustdesk | AGPL-3.0 | 跨公网 ID/中继远程桌面：rust-portable 单文件"下载即安装"与 MSI 安装版双形态，可自备自建信号/中继服务器，便携版无服务边界如实提示 |
| **SubnetDesk 局域网远控** | zibo-chen/SubnetDesk | AGPL-3.0 | RustDesk 局域网 fork（mDNS 发现、TCP 21118、账密认证），与 RustDesk 配成"远程控制"组合但协议互不兼容，同机并行互不冲突 |
| **Rufus 启动盘制作** | pbatard/rufus | GPL-3.0 | USB 启动盘工具纯托管：便携单文件 exe 三重校验，预置 `rufus.ini` 强制便携并关上游更新检查；上游 manifest 强制管理员——托管启动要求 Hanxi 本身以管理员运行 |
| **Bili23 视频下载器** | ScottSloan/Bili23-Downloader | GPL-3.0 | B 站视频下载器（PySide6 自带静态运行时整目录便携包）：命名互斥体 + QLocalServer 信使唤窗；上游关窗行为用户可配，退出三态如实上报、**不做静默强杀兜底** |
| **VS Code 编辑器** | Microsoft（官方 CDN 分发） | MIT（源码）+ 二进制许可条款 | **双形态托管**：ZIP 便携（`data/` 自包含激活器）与 User Installer 免 UAC 静默安装/升级确认闸；官方 CDN 三端点、哈希仅最新版可得的降级校验分治 |
| **TranslucentTB 任务栏透明** | TranslucentTB/TranslucentTB | GPL-3.0 | 任务栏透明/模糊特效工具：信使语义是"重设任务栏状态"而非唤窗，托盘消息窗口 WM_CLOSE 优雅退出（通用类名验属主防误伤），便携版 Win11 + 框架包双重系统前提预告 |
| **Paseo agent 编排器** | getpaseo/paseo | Apache-2.0 | coding agent 编排器（本机 daemon + 手机/桌面多端）：官方便携 zip 多版本目录托管、**共享数据模式**（与自装实例同 `%APPDATA%\Paseo`+`~/.paseo` 锁组）、进程名探测 + Win32 直唤（上游二次拉起语义是"开新窗"非聚焦，与 recordly 信使唤窗分家）、无空闲自动退出（daemon 宿主）、上游无更新禁用开关如实预告平行副本风险 |
| **WindTerm 终端** | kingToolbox/WindTerm | 部分开源（thirdparty 外 Apache-2.0） | Qt 单 exe 多实例：无信使，唤窗按 PID EnumWindows；上游全量发布无官方摘要——降级三层校验（字节数+CRC+布局自检）；外部实例按 N3 confirm-force 档治理 |
| **Termora 终端** | TermoraDev/termora | AGPL-3.0/商业双许可 | jpackage 自带 JRE 便携 zip 托管；命名互斥体信使唤窗；全资产 digest 主校验链；2.x 全为 prerelease 如实呈现 |
| **RAMMap 内存观察** | Microsoft Sysinternals（官方直链，非 GitHub） | 免费试用件（无再分发条款） | 单文件绿色 exe 借用托管骨架：Last-Modified 日期作版本令牌（同址覆盖式发布的唯一诚实语义）、降级三层校验、载荷 manifest 强制管理员走提权三重契约；另提供 hanxi 自研一键清理待机内存（N23，不依赖本工具） |
| **抖音下载器 Douzy** | jiji262/douyin-downloader | MIT | **仅版本+下载托管的特例**：上游内测期、Electron 壳源码未公开、Windows 仅 NSIS 安装版无便携 zip，故止步于版本列表侦查 → `Douzy-Setup-*.exe` 官方 sha256 + 字节数 + PE 魔数三重校验下载 → 一键拉起上游安装向导，**不接管进程运行**（决策记录见 TROUBLESHOOTING #47） |

### 三、桌面系统体验与通用设置

- **系统托盘与最小化常驻**：支持关闭主窗口时自动最小化到 Windows 系统托盘，后台静默守护托管进程；托盘**单击**切换主窗口显隐、菜单直达与彻底退出。
- **Windows 开机自启动**：设置页一键开关开机自启（管理注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`），支持后台静默拉起。
- **全局通知中心**：模块下载/实例状态/扫描进度等事件汇聚为分级通知（信息/成功/警告/错误），抽屉式查看与已读管理。
- **内置运行日志查看器**：提供应用全局运行日志文件列表检索、多行日志实时分页查看与历史日志一键清理（凭据自动脱敏）。
- **系统快捷直达 (Quick Launch)**：一键打开系统 `hosts` 文件、环境变量配置、网络适配器（`ncpa.cpl`）等。
- **按需懒加载模块架构**：全部 45 个功能模块支持在设置页按需启停，未启用模块 0 内存与 0 协程常驻；模块导航由后端注册表动态驱动前端渲染。

---

## 🛠️ 技术架构与优势

| 维度 | Hanxi (本项目) | 传统 Electron 类工具 | 传统 GUI 类工具 |
|---|---|---|---|
| **技术底座** | **Go + Wails v3 + Vue 3** | Electron + Node.js | Go + IMGUI / Fyne |
| **内存占用** | **~20 MB ~ 25 MB**（极低资源消耗） | 150MB ~ 300MB+ | 30MB ~ 50MB |
| **凭据安全** | **Windows DPAPI 硬件级加密** | 明文 JSON/TOML 落盘 | 明文存储 |
| **进程治理** | **Windows JobObject 绑定**（零孤儿进程） | Node 子进程（易残留） | 简单 Process Kill |
| **连接感知** | **日志特征词嗅探（毫秒级细粒度状态）** | 仅根据进程存活判定 | 仅根据进程存活判定 |
| **多实例支持** | **原生支持多项目独立并行运行** | 仅单配置单运行 | 仅单配置运行 |
| **配置分享** | **`frp://` 链接一键分享 + 批量端口导入** | 部分支持 | 不支持 |
| **第三方生态** | **28 款桌面工具统一托管**（版本管理+完整性校验+进程监管；抖音下载器为仅版本+下载托管的特例） | 不支持 | 不支持 |
| **系统集成** | **系统托盘常驻 + 开机自启 + UAC 提权** | 占用大/启动慢 | 功能单一 |
| **开发者套件** | **环境检测 + 端口扫描 + 局域网快传 + 随手记** | 仅单一功能 | 仅单一功能 |

---

## 🚀 快速开始

### 前置要求

- **操作系统**：Windows 10 (22H2+) / Windows 11 x64
- **Go** ≥ 1.26
- **Node.js** ≥ 20.19（或 ≥ 22.12）
- **Wails 3 CLI** & **Task**：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.10
go install github.com/go-task/task/v3/cmd/task@v3.53.1
```

### 安装依赖

```powershell
cd frontend && npm install && cd ..
go mod tidy
```

### 本地开发运行 (HMR 实时热重载)

```powershell
task dev
```

### 编译打包产物

```powershell
task build     # 产出便携版单二进制 bin/hanxi.exe
```

---

## 📁 目录结构

```text
hanxi/
├─ cmd/
│  └─ hanxi/                    # 应用入口（Main 装配与 UAC 提权模式分流）
├─ internal/
│  ├─ app/                      # Composition Root (Wails 窗口、托盘、生命周期与服务注入、45 模块统一注册)
│  ├─ product/                  # 品牌身份常量（名称/标识/版本/数据目录单一真相源）
│  ├─ domain/                   # 纯领域模型 (Project, ServerConfig, ProxyRule, Snapshot 等)
│  ├─ extapi/                   # 模块插件化抽象（Module 契约 / 懒加载注册中心 / 导航与启用状态）
│  ├─ notify/                   # 全局通知中心（分级通知 Hub 与前端事件推送）
│  ├─ modules/                  # 45 个功能模块
│  │  ├─ 自建能力               # frpc / wechat / portscan / portkill / lan / publicip
│  │  │                         # / wifi / envcheck / wsl / fileshare / memo / quickmenu
│  │  │                         # / sysinfo / ocr / softver / webapp / msgboard
│  │  └─ 工具托管               # snipaste / everything / quicklook / keyviz / litemonitor
│  │                            # / ccswitch / markeron / flclash / nanazip / eartrumpet
│  │                            # / bcu / mangodisk / recordly / papertodo / piclite / guoheview
│  │                            # / ddnsgo / translucenttb / vscode / rustdesk / subnetdesk
│  │                            # / rufus / bili23 / paseo / douzy / windterm / termora / rammap
│  │                            # （托管模块标准形态：version/ 版本管理子包 + instance/ 实例引擎子包）
│  ├─ platform/                 # 平台底层（Windows JobObject / DPAPI / 注册表自启 / IP Helper / Appx 包管理 / Shell 提权）
│  ├─ logging/                  # slog 结构化日志与凭据自动脱敏
│  └─ settings/                 # 便携化路径解析与通用设置持久化
├─ frontend/                    # Vue 3 + TypeScript + Vite 前端（自研样式层，无第三方 UI 框架）
│  ├─ src/
│  │  ├─ views/                 # 各模块业务视图与统一工作台布局（新增视图必须在 constants/navigation.ts 登记）
│  │  ├─ components/            # 复用组件（配置编辑/批量端口/分享模态框/状态指示器等）
│  │  └─ App.vue / main.ts      # 后端导航注册表驱动的侧边栏动态渲染
│  └─ bindings/                 # Wails v3 自动生成的 Go-JS API 绑定
├─ docs/                         # 项目文档（PRD / 架构设计 / 开发计划 / 开发指南 / 插件机制 / 第三方告知 / 踩坑记录）
└─ Taskfile.yml                  # 自动化开发与构建任务
```

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。托管的第三方工具版权归各自上游项目所有，许可证与合规说明见 [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)。
