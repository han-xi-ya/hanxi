# 第三方软件告知

> 本文档登记 Hanxi 集成或托管的第三方上游软件及其许可证义务，覆盖 frpc（上游 fatedier/frp）与全部 25 款托管桌面工具。Hanxi 对所有第三方工具的默认合规基线是：**用户侧按需下载、不捆绑、不再分发、不修改上游二进制**；各条目末尾标注若未来改为捆绑/预装分发时需履行的额外义务。

## MangoDisk

- 项目：MangoDisk
- 上游仓库：https://github.com/harry0703/MangoDisk
- 许可证：GPL-3.0-only
- 对应源码：每个下载版本可通过 `https://github.com/harry0703/MangoDisk/tree/<tag>` 获取，例如 `v1.0.7` 对应 `https://github.com/harry0703/MangoDisk/tree/v1.0.7`。

Hanxi 对 MangoDisk 采用用户侧按需下载和独立进程托管：版本文件直接来自上游 GitHub Releases，Hanxi 不修改、不静态链接、不内嵌 MangoDisk 源码或二进制。MangoDisk 的磁盘扫描、清理、卸载和系统设置功能均由其原版 GUI 提供。

当前 Hanxi 仓库和安装包不包含 MangoDisk EXE。若未来改为预装或随 Hanxi 二进制分发，发布流程必须另行落实 GPLv3 许可证文本、版权告知和对应源代码提供义务。

## EarTrumpet

- 项目：EarTrumpet
- 上游仓库：https://github.com/File-New-Project/EarTrumpet
- 官方许可证说明：仓库 LICENSE 为 MIT License，但文件开头附带 Excluded Entities 条款，明确排除 Yellow Elephant Productions、Tidal Media Inc.、Articent Group LLC 三家公司行使许可权利（GitHub API 因此标记为 NOASSERTION 而非 MIT）。
- 发行方式：Microsoft Store（产品 ID 9NBLGGH516XP）与上游自托管官方直装渠道（https://install.eartrumpet.app 的 AppInstaller 包，Azure Code Signing 签名，版本与商店同步构建）双渠道发行。

Hanxi 对 EarTrumpet 只纳管官方直装渠道（用户拍板不代管商店渠道）：查询直装版注册状态与运行态、经 AUMID 激活启动、终止进程退出（上游无优雅退出通道，退出为指纹复核后的直接终止，配置文件为事务性写入故强杀安全）、为当前用户卸载，安装/更新通过解析官方 appinstaller 清单（钉死包名/发布者/主机 + winget-pkgs 清单 SHA-256 交叉比对，Windows 部署期校验包签名）后交给 Add-AppxPackage。商店渠道仅保留注册检测用于并存警告（两渠道共享单实例互斥体），不提供任何商店操作。Hanxi 不修改上游二进制、不缓存不再分发安装包（安装用临时文件，装完即删），不修改其配置与托盘行为，不绑定 JobObject。卸载前会明确提示包 LocalSettings 中的设置将随包删除。若未来改为捆绑或代管分发，发布流程必须随附 MIT 许可证文本（含 Excluded Entities 条款原文）并另行评估分发合规性。

## NanaZip

- 项目：NanaZip
- 上游仓库：https://github.com/M2Team/NanaZip
- 官方许可证说明：https://github.com/M2Team/NanaZip/blob/main/License.md
- 发行方式：Hanxi 按需从上游 GitHub Releases 下载官方 stable MSIXBundle，并交由 Windows 为当前用户安装；Hanxi 不修改、不静态链接，也不在自身安装包中预置 NanaZip 二进制。

NanaZip 不是单一 MIT 许可证项目。其自有源代码使用 MIT License；应用文件关联图标使用 CC BY-ND 4.0；7-Zip 及派生代码包含 7-Zip License、GNU LGPL、GNU LGPL 加 unRAR restriction；LZFSE 等部分使用 BSD 3-Clause，其他第三方组件沿用各自原许可证。unRAR restriction 禁止使用相关代码重建 RAR 压缩算法。

Hanxi 可能在用户侧缓存原始官方 MSIXBundle 以支持安装和升级，但不会修改其内容。若未来改为随 Hanxi 安装包预置或再分发 NanaZip，发布流程必须重新核对并随附 NanaZip/7-Zip 及所有适用第三方许可证、版权告知和限制条款，不能仅标记为 MIT。

## Recordly

- 项目：Recordly
- 上游仓库：https://github.com/webadderallorg/Recordly
- 许可证：AGPL-3.0-only（LICENSE.md 附加条款：不得使用 Recordly 名称/品牌于自有项目；基于其代码的衍生须在用户可见界面与仓库中署名 Recordly）
- 发行方式：官方 GitHub Releases 仅提供 Windows NSIS 在线安装器（约 214MB，**未数字签名**）、macOS dmg/zip 与 Linux AppImage；上游自 v1.3.5-beta.2 起因 Apple 公证凭证问题暂停 macOS 发布，Windows 安装器持续无签名（SmartScreen 风险提示见下）。

Hanxi 对 Recordly 采用用户侧按需下载与独立进程托管：安装器直接来自上游 GitHub Releases（GitHub API digest sha256 + 官方 SHA256SUMS.txt 双源校验），经 NSIS `/S /D=` 静默安装进 Hanxi 托管目录，启动时注入官方开关 `RECORDLY_DISABLE_AUTO_UPDATES=1` 禁用上游自动更新；Hanxi 不修改、不静态链接、不内嵌 Recordly 源码或二进制，录制与剪辑功能均由原版 GUI 提供，录屏数据与配置存于 Recordly 自身的 `%APPDATA%\Recordly`。

因 AGPL 附加条款与安装器未签名两点，本模块刻意不做任何"品牌融合"展示（不改名、不套用 Recordly 图标于 Hanxi 自有 UI），风险提示文案如实指向杀毒软件误拦可能。当前 Hanxi 仓库和安装包不包含 Recordly EXE。若未来改为预装或随 Hanxi 二进制再分发，发布流程必须落实 AGPL-3.0 完整许可证文本、对应完整源代码提供义务与上游署名要求，并重新评估未签名二进制的分发责任。

## PaperTodo

- 项目：PaperTodo（极简 Windows 桌面便签，WPF/.NET 10）
- 上游仓库：https://github.com/snownico0722/PaperTodo
- 许可证：PolyForm Noncommercial License 1.0.0 + PaperTodo Individual Professional Use Additional Permission 1.0（© 2026 snownico0722）——自然人可免费使用（含工作、自由职业等营利活动）；不得销售本程序或将其作为商业产品/服务提供；**普通公司未经商业许可不得统一安装、部署或向员工分发**；教育、慈善、公共研究与政府机构可免费使用与内部部署；非商业性修改与分发必须保留许可证及原项目声明。
- 发行方式：官方 GitHub Releases 绿色单文件 exe——`self-contained`（内嵌 .NET 10 运行时）与 `no-runtime`（需系统安装 .NET 10 桌面运行时）双变体，无安装器、无自动更新器。

Hanxi 对 PaperTodo 采用用户侧按需下载与独立进程托管：exe 直接来自上游 GitHub Releases（实证上游资产未提供 GitHub 官方 digest，完整性采用镜像直链锚定 + 字节数 + MZ/PE 版本核对 + sha256 下载指纹存档的多层兜底，详见 docs/TROUBLESHOOTING.md），唤窗/收拢/退出全部走上游 SingleInstanceHelper 官方单实例命令通道（show/hide/exit）；Hanxi 不修改、不静态链接、不内嵌 PaperTodo 源码或二进制，便签在原版纸片窗口内编辑，数据（data.json、note-assets.lmdb、plugins/）存于 Hanxi 托管目录且卸载时原地保留。

因 PolyForm Noncommercial 为非商业许可证，本模块的合规红线是**不捆绑、不再分发**：Hanxi 仓库与安装包不包含任何 PaperTodo 二进制，下载行为全部由用户本机直接对上游发起。若未来改为预装或随 Hanxi 再分发，必须先取得上游作者授权（"普通公司统一部署"正是该许可证明文禁止的形态），并随附 PaperTodo 版权与许可证完整声明。

## frp（frpc 客户端）

- 项目：frp
- 上游仓库：https://github.com/fatedier/frp
- 许可证：Apache-2.0
- 发行方式：官方 GitHub Releases（Hanxi 集成国内加速镜像通道），SHA256 完整性校验、指数退避重试，支持本地 `frpc.exe` 导入。

Hanxi 对 frpc 采用多实例独立进程托管：二进制按需从上游获取，实例经 Windows JobObject 绑定启停，Token 经 DPAPI 加密落盘；Hanxi 不修改、不静态链接、不内嵌 frp 源码或二进制，穿透能力均由原版 frpc 提供。当前 Hanxi 仓库和安装包不包含 frpc.exe。若未来改为预装或随 Hanxi 再分发，须随附 Apache-2.0 许可证文本与 NOTICE、声明修改情况并履行相应义务。

## Bulk Crap Uninstaller (BCU)

- 项目：Bulk Crap Uninstaller
- 上游仓库：https://github.com/BCUninstaller/Bulk-Crap-Uninstaller
- 许可证：Apache-2.0
- 发行方式：官方 GitHub Releases 按需下载。

Hanxi 托管原版批量卸载 GUI：受管进程启停、闲置自动退出探测、`Global\BCU-singleinstance` 单实例唤窗、.NET 运行时依赖检测；不修改、不捆绑、不再分发。若未来改为预装或再分发，须履行 Apache-2.0 随附许可证与 NOTICE 义务。

## CC Switch

- 项目：CC Switch（Claude Code / Codex 供应商切换器）
- 上游仓库：https://github.com/farion1231/cc-switch
- 许可证：MIT
- 发行方式：官方 GitHub Releases 按需下载。

Hanxi 纯托管：Tauri 单实例协议唤窗、JobObject 启停与运行态探测；不修改、不捆绑、不再分发。若未来随 Hanxi 捆绑分发，须随附 MIT 许可证文本与版权声明。

## Visual Studio Code

- 项目：Visual Studio Code（代码编辑器）
- 上游官网：https://code.visualstudio.com
- 许可证：MIT（源代码）；官方二进制分发附带独立的许可证与商标条款，微软保留 VS Code 名称与标识权利
- 发行方式：微软官方 CDN（update.code.visualstudio.com）按需下载；GitHub Releases 仅源码归档，无二进制资产。

Hanxi 纯托管：双形态（ZIP 归档 data\ 自包含便携 + User Installer 免 UAC 静默安装/升级）、JobObject 启停、Electron 单实例协议唤窗与互斥体/注册表探测；不修改、不捆绑、不再分发，安装版卸载权交还系统「设置→应用」。若未来随 Hanxi 捆绑分发，须另行核对随附 MIT 源码条款与官方二进制的许可/商标条款。

## MarkerOn

- 项目：MarkerOn（屏幕标注）
- 上游仓库：https://github.com/ifer47/markeron
- 许可证：MIT
- 发行方式：官方 GitHub Releases 按需下载。

Hanxi 托管原版：标注开关经上游单实例二次拉起实现；不修改、不捆绑、不再分发。若未来捆绑分发，须随附 MIT 许可证文本与版权声明。

## Everything / ES（voidtools）

- 项目：Everything 文件检索、命令行搜索工具 ES (es.exe)
- 上游站点：https://www.voidtools.com （仅官网分发，非 GitHub）
- 许可证：Everything 主程序为专有免费软件（voidtools 官网条款免费使用）；ES.exe 为 MIT 许可证开源工具。
- 发行方式：官网下载，主程序完整性采用官方 sha256 清单锚定 + 多层兜底校验。

Hanxi 除版本托管外，内嵌官方 es.exe 在 Hanxi 控制台内实现秒级文件检索（结果上限 300 条），并提供后台索引托管与打开/定位文件操作。Hanxi 不修改上游二进制、不捆绑、不再分发；主程序托盘、快捷键与索引行为沿用原版。因主程序为专有免费软件，**严禁预装或再分发**。

## Snipaste

- 项目：Snipaste（截图 + 贴图）
- 官方站点：https://www.snipaste.com ，官方分发：https://download.snipaste.com （闭源免费软件，非 GitHub 开源）
- 发行方式：官网 zip 归档，官方 sha-1 清单校验。

Hanxi 采用**脱管托管**：保留 Snipaste 原生托盘与全局快捷键，Hanxi 退出时不强杀（截图工具需常驻）；版本下载、启动与运行态由 Hanxi 统一管理。Hanxi 不修改、不捆绑、不再分发，不做任何品牌衍生展示；若未来需随包分发，必须另行取得官方授权。

## GPL-3.0 托管工具（FlClash / Keyviz / QuickLook / PicLite / Bili23 Downloader / TranslucentTB / Rufus）

- FlClash：https://github.com/chen08209/FlClash —— GPL-3.0（Clash 系跨平台代理客户端；第二实例不唤窗，改用 EnumWindows 置前台）
- Keyviz：https://github.com/mulaRahul/keyviz —— GPL-3.0（按键可视化；MSI `msiexec /a` 管理提取安装，互斥体探测）
- QuickLook：https://github.com/QL-Win/QuickLook —— GPL-3.0（空格快速预览；便携 zip 安装，命名管道 Quit/Reload 优雅退出）
- PicLite：https://github.com/amiaoapp/PicLite —— GPL-3.0（图片/GIF 压缩；`msiexec /a` 管理提取免管理员提权）
- Bili23 Downloader：https://github.com/ScottSloan/Bili23-Downloader —— GPL-3.0（B 站视频下载器，Python/PySide6 自带静态运行时整目录便携包；命名互斥体 + QLocalServer 信使唤窗，关窗行为用户可配故退出三态如实上报、无静默强杀，详见 docs/TROUBLESHOOTING.md #27）
- TranslucentTB：https://github.com/TranslucentTB/TranslucentTB —— GPL-3.0（任务栏透明/模糊效果工具，C++/WinRT 注入 explorer；单实例互斥体探测、托盘消息窗口 WM_CLOSE 优雅退出、信使重设任务栏状态而非唤窗，详见 docs/TROUBLESHOOTING.md #32）
- Rufus：https://github.com/pbatard/rufus —— GPL-3.0（USB 启动盘制作工具；官方 Windows x64 便携单文件 exe，GitHub API digest + 字节数 + MZ 魔数三重校验、下载即安装，预置 `rufus.ini` 强制便携并关闭上游内置更新检查；上游 manifest 强制 `requireAdministrator`——托管启动要求 Hanxi 本身以管理员运行，第二实例弹模态错误框无唤窗契约改用 Win32 直操作置前台，详见 docs/TROUBLESHOOTING.md #26）。磁盘级写入属数据销毁高风险操作，制作流程全部在上游原版界面完成（纯托管决策），对应源代码可经上游仓库 tag 获取。

Hanxi 对上述工具均采用用户侧按需下载与独立进程托管：二进制直接来自上游 GitHub Releases（GitHub API digest 等多层完整性校验），Hanxi 不修改、不静态链接、不内嵌其源码或二进制，全部功能均由原版 GUI 提供；对应源代码可通过各下载版本对应的上游 tag 获取。

GPL-3.0 合规红线为**不捆绑、不再分发**：当前 Hanxi 仓库和安装包不包含上述任何二进制，下载行为全部由用户本机直连上游发起。若未来改为预装或随 Hanxi 二进制再分发，发布流程必须落实 GPL-3.0 完整许可证文本与对应完整源代码（或书面报价）的提供义务。

## AGPL-3.0 远程桌面托管工具（RustDesk / SubnetDesk）

- RustDesk：https://github.com/rustdesk/rustdesk —— AGPL-3.0（跨公网 ID/中继远程桌面；默认官方公共信令，支持自备自建 rendezvous/relay 服务器；便携 exe 为 rust-portable packer 单文件"下载即安装"，内层解压至 `%LOCALAPPDATA%\rustdesk`；另有 MSI 安装版双形态纳管；端口 TCP 21116/21117，数据目录 `%APPDATA%\RustDesk`）
- SubnetDesk：https://github.com/zibo-chen/SubnetDesk —— AGPL-3.0（RustDesk 的独立局域网 fork：移除公网设备 ID、rendezvous/中继与云账户路径，局域网/VPN 直连，mDNS 发现、端口 TCP 21118、用户名/密码 Argon2id 认证；数据目录 `%APPDATA%\SubnetDesk`；同样为便携单文件 + MSI 安装版双形态）

Hanxi 对上述两工具均为**纯托管**（不内嵌界面）：远程桌面画面即上游 GUI 本体，内嵌重做不可行；二进制直接来自上游 GitHub Releases（多层完整性校验），Hanxi 不修改、不静态链接、不内嵌其源码或二进制。两模块在 Hanxi 导航中配成"远程控制"组合，但**协议互不兼容**（ID+中继认证 vs 子网端点+账密认证），两端需装对应软件——页面文案如实说明，非互操作承诺。便携版边界如实提示：不装 Windows 服务，被控仅在便携进程存活期间可被连接，锁屏/安全桌面场景为安装版专属能力。两工具端口与数据目录错开，同机并行运行互不冲突。

AGPL-3.0 合规红线同为**不捆绑、不再分发**：当前 Hanxi 仓库和安装包不包含上述任何二进制。若未来改为预装或随 Hanxi 再分发，发布流程必须落实 AGPL-3.0 完整许可证文本与对应完整源代码提供义务，并重新评估网络服务条款（§13）的适用边界。

## LiteMonitor

- 项目：LiteMonitor（桌面硬件监控）
- 上游仓库：https://github.com/Diorser/LiteMonitor
- 许可证：**上游仓库未声明 LICENSE 文件**（截至 2026-09，GitHub API license 字段为空）
- 发行方式：官方 GitHub Releases 单文件 exe（带 digest），上游主程序清单为 `requireAdministrator`。

Hanxi 采用用户侧按需下载与独立进程托管：针对管理员清单直拒与 UIPI 边界，采用首启配置种子（关自动更新）+ Win32 直操作唤窗方案（详见 docs/TROUBLESHOOTING.md #17）；Hanxi 不修改上游二进制。

**风险说明**：未声明许可证的作品依法默认"保留所有权利"，再分发权不明确。本模块严格执行按需下载、不捆绑、不再分发红线；若未来考虑任何预装方案，必须先在上游取得明确授权或补见许可证声明。

## 果核看图（GuoheView）

- 项目：果核看图 GuoheView（原 MagicView，Windows 极速 RAW 图片查看器）
- 上游官网：https://pic.ghxi.com （果核 ghxi.com 出品，闭源免费软件）
- 发布接口：https://rj.lovestu.com/download/gh_view （官方自建 JSON 接口，返回当前版本 stable/beta 双通道与便携 zip 官方 MD5；非 GitHub 分发，无历史版本归档）
- 许可证：官方声明"免费提供使用；未经授权，不得恶意修改、重打包、捆绑广告或以误导性方式再分发"；发行文件带 Certum Code Signing 2021 签名的 Authenticode 数字签名（主体：成都极光以太科技有限公司）。
- 发行方式：官方便携 zip（顶层 `GuoheViewPortable/` 包装目录，exe + 自研解码内核 DLL + resources + portable.ini 全便携标记）。

Hanxi 对该工具采用用户侧按需下载与独立进程托管：下载由用户本机直连官方发布接口发起，以官方 MD5 + 字节数 + zip 内建 CRC + 解压布局自检四层校验完整性；**不修改任何上游二进制/DLL**，不在仓库与安装包中捆绑或再分发。两点托管侧行为如实登记：①「导入本地」若源目录缺少官方 `portable.ini`（该文件为上游文档公开的便携模式开关、纯注释且程序只读不改）会**补写同名开关文件**，属官方支持的配置形态切换，不构成修改/重打包；②上游内置更新器无官方关闭开关（实测 config.ini 仅有检查频率节流键），Hanxi 不改写上游配置语义，仅在页面提示引导版本管理回 Hanxi。

**风险说明**：闭源 freeware 无再分发授权，且发布依赖个人自建域名接口（稳定性不受 GitHub 生态保障）。本模块严格执行按需下载、不捆绑、不再分发红线；若未来考虑预装分发，必须先取得上游书面授权。

## ddns-go

- 项目：ddns-go（动态域名解析 DDNS 工具，Web 管理面板）
- 上游仓库：https://github.com/jeessy2/ddns-go
- 许可证：MIT
- 发行方式：官方 GitHub Releases 按需下载（`ddns-go_*_windows_x86_64.zip`，GitHub API digest 官方 sha256 四层校验）。

Hanxi 纯托管：JobObject 启停、进程名存活探测、TCP 端口就绪判定、DNS 解析配置全程由上游原版 Web 面板完成（Hanxi 经独立子 Webview 窗口提供入口，不改写其 `~/.ddns_go_config.yaml` 配置语义）；不修改、不捆绑、不再分发上游二进制。托管侧安全加固如实登记：监听地址固定 127.0.0.1（上游默认绑全网卡），进程 stdout 日志行经敏感参数脱敏后展示。若未来随 Hanxi 捆绑分发，须随附 MIT 许可证文本与版权声明。

## Paseo

- 项目：Paseo（coding agent 编排器：本机 daemon + 桌面/移动/Web 客户端，统一管理 Claude Code、Codex 等 CLI agent）
- 上游仓库：https://github.com/getpaseo/paseo
- 官方许可证说明：根 LICENSE 为自定义文本——除第三方组件外整体按 **Apache License 2.0** 授权（第三方组件沿用各自许可证；GitHub API 因此标 NOASSERTION 而非 Apache-2.0）
- 发行方式：官方 GitHub Releases 按需下载 Windows 便携 zip（electron-builder win zip target，`Paseo-Setup-<ver>-<arch>.zip`，约 180MB；GitHub API digest sha256 + 字节数 + zip 内建 CRC + 解压布局自检四层校验）。

Hanxi 纯托管：官方便携 zip 解压进多版本目录（versions/paseo_X.Y.Z），JobObject 绑定的启停引擎、进程名 Paseo.exe 探测、WM_CLOSE 优雅退出（宽限覆盖 daemon 清理生命周期）+ 强杀兜底、Win32 直唤窗口；agent 编排全程在 Paseo 自有窗口完成（其 second-instance 语义是"再开新窗"而非聚焦，唤窗因此优先直唤、无窗才二次拉起请求开新窗）。**数据模式为用户目录共享（与 cc-switch 同构，集成拍板）**：Electron 数据恒在 `%APPDATA%\Paseo`、daemon 数据恒在 `~/.paseo`（含手机端配对凭据），托管实例与用户自装实例同数据同锁组——删托管版本不毁配对，Hanxi 不注入上游官方 env 隔离通道（PASEO_HOME / PASEO_ELECTRON_USER_DATA_DIR），不改写任何配置语义，不修改上游二进制。上游自动更新无禁用开关（源码实证）：`autoInstallOnAppQuit=false` 下安装动作仅在用户于 Paseo 界面内点击时发生（zip 形态会装出 `%LOCALAPPDATA%` 平行副本），页面提示条如实引导"版本升级走 Hanxi 版本管理"，不做拦截。若未来随 Hanxi 捆绑分发，须随附 Apache-2.0 许可证文本与 NOTICE/第三方组件清单。

## 抖音下载器（Douzy）

- 项目：抖音下载器 Douzy（抖音桌面下载器，上游为 jiji262/douyin-downloader 的桌面产品形态）
- 上游仓库：https://github.com/jiji262/douyin-downloader
- 许可证：MIT（上游仓库 LICENSE，截至 2026-09 GitHub API 标记 MIT；桌面版尚处内测期，其分发条款以上游页面为准）
- 发行方式：官方 GitHub Releases 仅提供 Windows NSIS 在线/离线安装器（`Douzy-Setup-*.exe`），无便携 zip 形态。

Hanxi 对 Douzy 采用降级托管形态——**仅版本管理 + 安装包下载，不做进程托管**：上游桌面版尚处内测期、Electron 壳源码未公开、Windows 仅有 NSIS 安装版，三者在"托管启停/探测/唤窗"上都是硬伤，故 Hanxi 只做到 Releases 版本列表侦查 → 官方 sha256 + 字节数 + PE 魔数三重校验下载 → 一键拉起上游 NSIS 安装向导交还用户（集成决策与踩坑详见 docs/TROUBLESHOOTING.md #47）；安装后的运行行为、更新策略与数据目录均由上游本体决定，Hanxi 不修改、不静态链接、不内嵌其源码或二进制。

当前 Hanxi 仓库和安装包不包含 Douzy 任何二进制。若未来改为预装或随 Hanxi 再分发，发布流程须随附 MIT 许可证文本与版权声明，并另行评估内测产品的分发责任。

## 托管工具真图标（N27 批 A + 批 B-2 + 尾巴放行 + 开源上游仓库取材入库件）

Hanxi 前端 `src/assets/apps/*.png` 内嵌以下上游软件的**主图标位图**。
**2026-09-26 升切（不改许可口径）**：全册按"源资产真实尺寸、64px 封顶"重切——
早期 32px 件多为源 256px 画幅白白降采样，本轮从官方发行 exe 的 PE
`RT_GROUP_ICON/RT_ICON` 最大真实画幅（多为 256，quicklook 512、keyviz 128、
rufus 恰 64）等比重取到 64px，绝不上采样凑数；开源仓库取材件按源最大真实尺寸取
（bili23 icns 1024→64、eartrumpet 包资产 256→64、rustdesk/termora/nanazip 原生 64、
ddnsgo/frpc favicon 源天花板 48 保持 48）。逐枚出货边长单一来源见
`internal/app/appicons/sources.go`（由 `sources_test.go` 逐枚解码锁死，防漂移）。
许可口径不变，来源不变（下表"来源软件/上游许可"照旧，仅注尺寸见各件备注与本段）。
用途仅为在 hanxi 界面中标识该被托管软件本身（识别性展示），不暗示任何隶属或
背书关系。逐个许可登记：

| 文件 | 来源软件 | 上游许可（本文对应节） | 备注 |
|---|---|---|---|
| ccswitch.png | CC Switch | MIT | 图标随仓库同源 |
| keyviz.png | Keyviz | GPL-3.0 | 识别性使用 |
| everything.png | Everything (voidtools) | 免费软件（见上节） | |
| snipaste.png | Snipaste | 免费软件自有 EULA（见上节） | 风险最高的入库件；如权利方异议即摘除回落通用徽标 |
| markeron.png | MarkerOn | 见上节 | |
| bcu.png | Bulk Crap Uninstaller | Apache-2.0（见上节） | 识别性使用 |
| douzy.png | Douzy 全能下载器 | MIT（见上节） | 识别性使用；提取源为官方 NSIS 安装器（上游无便携形态，主图标随安装器同源） |
| flclash.png | FlClash | GPL-3.0（见 GPL 族节） | 识别性使用 |
| mangodisk.png | MangoDisk | GPL-3.0（见上节） | 识别性使用 |
| papertodo.png | PaperTodo | PolyForm Noncommercial 1.0.0 + 个人附加许可（见上节） | 识别性使用；以 Hanxi 个人非商业项目形态入库，如权利方异议即摘除 |
| paseo.png | Paseo | Apache-2.0（见上节） | 识别性使用 |
| piclite.png | PicLite | GPL-3.0（见 GPL 族节） | 识别性使用 |
| quicklook.png | QuickLook | GPL-3.0（见 GPL 族节） | 识别性使用 |
| rufus.png | Rufus | GPL-3.0（见 GPL 族节） | 识别性使用 |
| subnetdesk.png | SubnetDesk | AGPL-3.0（见 AGPL 族节） | 识别性使用；提取源为上游官方便携单文件 packer exe（非 Hanxi 构建件） |
| translucenttb.png | TranslucentTB | GPL-3.0（见 GPL 族节） | 识别性使用 |
| windterm.png | WindTerm | 仓库根无 LICENSE 文件，README 自述完全免费商用/非商用（开源部分 Apache-2.0） | 识别性使用；许可口径以自述为据，随批登记留痕 |
| litemonitor.png | LiteMonitor | 上游仓库未声明 LICENSE（见上节，默认"保留所有权利"） | 机主拍板放行（2026-09-25，按 Snipaste 同口径）；识别性使用，权利方异议即摘 |
| guoheview.png | 果核看图 GuoheView | 闭源 freeware，官方声明无再分发授权（见上节） | 机主拍板放行（2026-09-25，按 Snipaste 同口径）；识别性使用，权利方异议即摘 |
| rustdesk.png | RustDesk | AGPL-3.0（见 AGPL 族节） | 识别性使用；来源=上游仓库 github.com/rustdesk/rustdesk `res/icon.ico`（升切：出货 64px，取该 ico 内原生 64×64 BMP 画幅零重采样直解——源含 16/32/48/64/128 全画幅） |
| bili23.png | Bili23-Downloader | GPL-3.0（见 GPL 族节） | 识别性使用；来源=上游仓库 github.com/ScottSloan/Bili23-Downloader `assets/app.icns`（实为 1024px PNG；升切：出货 64px，等比缩 64，仓库内无更大现成件与更优位图通道） |
| termora.png | Termora | 仓库根无 LICENSE 文件，README 自述 AGPL-3.0/专有双许可 | 识别性使用；来源=上游仓库 github.com/TermoraDev/termora `src/main/resources/icons/termora_64x64.png`（升切：改取官方原生 64px 件零改作直用，源天花板 64；仓库无 128+ 位图通道）；许可口径以 README 自述为据，随批登记留痕 |
| nanazip.png | NanaZip | 代码 MIT；应用图标资产 CC BY-ND 4.0（见上节） | 识别性使用；来源=上游仓库 github.com/M2Team/NanaZip `Assets/PackageAssets/Square44x44Logo.targetsize-64_altform-unplated.png`（升切：官方原生 64px 小尺寸件零改作直用，规避 ND 禁改作条款——不缩放/不裁剪/不合成；源天花板 64） |
| ddnsgo.png | ddns-go | MIT（见上节） | 识别性使用；来源=上游仓库 github.com/jeessy2/ddns-go 根目录 `favicon.ico`（官方 Web 管理面板图标；升切：出货 48px，该 ico 最大画幅即 48——源天花板 48，不为凑 64 放大；控制台程序无 GUI 头件，仓库现成位图即官方脸面） |
| frpc.png | frp (fatedier/frp) | Apache-2.0（见上节） | 识别性使用；来源=上游仓库 github.com/fatedier/frp `web/frpc/public/favicon.ico`（官方 frpc 控制面板图标；升切：出货 48px，单画幅 48 即源天花板，不放大凑 64；控制台程序无 GUI 头件，仓库现成位图即官方脸面） |
| eartrumpet.png | EarTrumpet | MIT + Excluded Entities 前言（见上节） | 识别性使用；来源=上游仓库 github.com/File-New-Project/EarTrumpet `EarTrumpet.Package/Assets/Square44x44Logo.altform-unplated_targetsize-256.png`（升切：改取官方 256px 无底版原生画幅等比缩 64 出货，替代原 32px targetsize 小件）；Excluded Entities 条款仅点名排除三家商事主体，Hanxi 非排除对象，MIT 授权照常成立 |
| generic.png | —— | hanxi 自绘 | 取不到真图标/许可受限的统一回落徽标 |
| ——（不入库） | RAMMap (Sysinternals) | 微软 Sysinternals 软件许可条款：禁再分发其二进制资产 | 位图不落仓库、不随发布物分发：默认矢量 `i:gauge`，装有机主本机官方 RAMMap64.exe 时走「红线图标运行期本机提取」节通道 |

未入库（如实登记）：**Recordly**——AGPL 附加条款明文禁将 Recordly 名称/品牌
图标套用于 Hanxi 自有 UI 展示（上节"刻意不做品牌融合"），入库分发即违约，
默认矢量 `i:video`，永不入库；**vscode**——微软商标条款对品牌资产同样收紧，
与 Sysinternals 同判不入库，默认矢量 `i:code`。三者的"装了就画自己的脸"
诉求由下节运行期本机提取通道满足——不改变本节任何"不入库"结论；**softver/msgboard/quickmenu/sysinfo/envcheck/wechat/wifi 等
Hanxi 自研功能模块**（wechat/wifi 为桥接/诊断入口，非托管载荷）永不入库。
（2026-09-26 尾巡改判注：**ddnsgo/frpc** 原以"控制台程序无 GUI 头件"落通用徽标，
后经官方仓库实证存在现成品牌位图（favicon），按开源取材通道补录入库（见上表）；
同轮补录 **eartrumpet**（官方包原生 32px 件）。三枚自入库起同时供托盘子菜单消费。）
若随 Hanxi 安装包再分发构成任何权利方异议，
按"摘除位图 → 回落 generic"处理，不动摇功能。

## 红线图标运行期本机提取（N27 尾巴：rammap / recordly / vscode）

上述三枚品牌图标因许可红线**永不入库、不随发布物分发**（上一节口径原样成立）。
hanxi 另开一条纯运行期通道：当机主本机装有官方载荷（托管版本目录内的
RAMMap64.exe / Recordly.exe / Code.exe，或系统安装版 VS Code）时，
由 `packages/go/peicon` 就地解析该 exe 的 PE 图标资源取最大真实画幅帧，
PNG 字节仅进 hanxi 进程内存与本机页面渲染（不落盘、不进构建产物、不出机器），
用于在 hanxi 界面中标识被托管软件本身（识别性展示）。

要点与边界：

- **不分发**：任何位图字节都不写入 hanxi 仓库、发布安装包或托管元数据；
  卸载 hanxi 或移除上游软件即无任何残留资产。这是"提取仅发生在机主自己的
  机器、仅供机主自己看"的自用边界，与 Sysinternals"禁再分发二进制资产"
  、Recordly AGPL 品牌条款、微软商标条款均不冲突（若权利方对"本机就地
  显示"口径提出异议，删除 `internal/app/runtimeicon_service.go` 一家即可
  整批回落矢量徽标，功能无损）。
- **宁缺毋滥**：载荷不在位（未安装）、模块停用、解析失败一律如实回错误，
  前端按 `rt:<id>|<fallback>` 串自带的矢量回落（i:gauge / i:video / i:code），
  观感与提取通道上线前零差，绝不伪造或猜测图标。
- **画幅实测**（2026-09-26，`peicon_test.go` 真件集成用例锁底）：
  RAMMap64.exe 最大真实画幅 32×32（DIB 组，32² 帧不入 RT_GROUP_ICON 组目录，
  全量 RT_ICON 择优口径下可得）；Recordly / VS Code 本机暂不在位，
  其官方出货 exe 含 256² PNG 帧为厂商公开事实，前端台账
  （`wheelIconBudget.ts` RUNTIME_ICON_SRC_LEDGER）按 256 封上限——
  台账只封顶不放大，实际画幅更小只会更保守，无糊图风险。
- **实现**：提取源路径由各模块经 `extapi.IconSourceProvider` 供出（各一行
  转发既有版本解析）；集中服务门语义=「已装+未停用」即放行、绝不因取图标
  唤醒模块 OnInit；结果进程内按源文件 size+mtime 指纹缓存（重装/换版本
  自动失效），失败同键记负缓存防每次渲染撞解析。

## 前端内嵌字体（N36）

- 项目：前端工作台字族三件——LXGW WenKai GB Screen Regular v1.522（正文中文主字族）、
  Noto Sans SC Variable（文楷缺字按字符回落补集）、JetBrains Mono 400/500/600/700（代码等宽族）
- 上游仓库：https://github.com/lxgw/LxgwWenKai-Screen 、
  https://github.com/google/fonts （`ofl/notosanssc`，即思源黑体 SC 的 Google 发行上游）、
  https://github.com/JetBrains/JetBrainsMono
- 许可证：三件均为 **SIL OFL 1.1**（文楷另含作者自定的保留名称附加条款，全文见
  `frontend/src/assets/fonts/licenses/OFL-LXGW-WenKai.txt`；Noto 与 JetBrains Mono
  许可文本同目录随附）
- 发行方式：与全文"按需下载、不捆绑"基线不同，本组属 **仓库与安装包内嵌分发**——
  OFL 正是允许字体原件/改作随证再分发的许可，六枚 WOFF2 落
  `frontend/src/assets/fonts/`，经 `src/styles/fonts.css` @font-face 由 WebView 加载。

分发形态豁免说明（为何文楷子集可继续沿用原名）：OFL 第 3 条要求衍生件不得沿用保留名称，
但文楷许可文本 [ADDITIONAL PERMISSION] 明文豁免"仅为网页端渲染优化（Web Font）之目的而
子集化或转 WOFF/WOFF2、且不将其作为可安装的桌面字体文件提供给公众下载"的改作。
Hanxi 分发包内不存在 TTF/OTF 等可安装形态、仅以 @font-face 供应用内嵌 WebView 消费，
按该条款文义适用豁免；Noto 子集的 Adobe 保留名仅 'Source'（本名不含），JetBrains Mono
为官方 WOFF2 零改动直嵌。如权利方对口径另有异议，处置预案为改字族名重新发布（字形数据不变）。
溯源：源件 URL/SHA-256/子集字符集与复建命令全部记录于 fonts.css 文件头注释；
子集化属一次性构建动作（fontTools 4.66 + brotli，--user 构建期工具，不入项目依赖清单）。
