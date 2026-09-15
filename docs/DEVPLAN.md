# Hanxi 开发里程碑与执行现状

> **版本**：v1.3  
> **更新日期**：2026-09-14  
> **目标平台**：Windows 10 (22H2+) / Windows 11 x64  

---

## 1. 里程碑完成情况总览

| 里程碑 | 核心目标 | 状态 | 交付核心 |
|---|---|:---:|---|
| **M0: 框架与平台原语** | Wails v3 架构跑通，Windows 原生底层原语封装（JobObject/IP Helper/DPAPI） | 🟢 已达成 | `internal/platform/windows`、`internal/extapi` 骨架 |
| **M1: 基础服务与持久化** | 便携路径检测、配置原子写落盘、slog 凭据自动脱敏、托盘与开机自启 | 🟢 已达成 | `internal/settings`、`internal/logging`、`internal/app` |
| **M2: 网络诊断与局域网** | 出口 IP/IPv6 查询、网卡拓扑透视、ICMP Ping、Traceroute、LAN 设备发现 | 🟢 已达成 | `internal/modules/publicip`、`internal/modules/lan` |
| **M3: 端口查杀与安全提权** | IP Helper 端口占有表、进程指纹防误杀复核、Windows 原生 UAC 提权 | 🟢 已达成 | `internal/modules/portkill`、UAC 提权模式 |
| **M4: frpc 多实例工具** | 多实例并发、TOML生成、版本管理与重试、DPAPI 凭据加密、连接状态嗅探 | 🟢 已达成 | `internal/modules/frpc` |
| **M5: 扩展生态与工具套件** | 微信机器人助手、Gonmap 端口服务指纹扫描、内置日志查看器 | 🟢 已达成 | `modules/wechat`、`modules/portscan`、日志查看器 |
| **M6: 极致优化与便携交付** | 按需懒加载零开销架构、关闭最小化托盘常驻、单二进制构建 | 🟢 已达成 | `bin/hanxi.exe`、零内存泄漏保证 |
| **M7: 桌面协同与局域网套件** | WiFi 密码查看、局域网文件快传、极客随手记（快传联动）、全局通知中心、品牌断代迁移 | 🟢 已达成 | `modules/wifi`、`modules/fileshare`、`modules/memo`、`internal/notify`、`internal/product` |
| **M8: 开发环境检测增强** | Git/Go/Node/Java/Python/.NET 工具链盘点、官网最新版本通道（按本机版本线置顶）、包管理器升级提示、资源管理器定位安装路径、.NET 并排安装与官方支持线 | 🟢 已达成 | `modules/envcheck`（模块版本 0.4.0）、前端环境页 |
| **M9: 开源工具托管生态** | 托管模式框架化（`version/`+`instance/` 三件套）并规模化至 23 款桌面工具；安装布局矩阵（zip/MSI 管理提取/NSIS 静默/MSIX/AppInstaller/单文件下载即安装/MSI 安装版双形态）；唤窗与退出治理矩阵（含多实例上游进程名探测契约、退出三态如实上报无强杀兜底形态）；第三方许可合规登记 | 🟢 已达成 | `modules/{markeron,everything,ccswitch,snipaste,nanazip,eartrumpet,mangodisk,bcu,flclash,recordly,papertodo,piclite,keyviz,quicklook,litemonitor,guoheview,ddnsgo,rustdesk,subnetdesk,rufus,bili23,vscode,translucenttb}`、`platform/apppackage` |
| **M10: 系统就绪与桌面效率** | WSL2 十项流式只读体检与三态结论（根因诊断"硬件不满足 vs API 被拦"、双源降级）、安装形态取证与正规卸载、白名单固定参数提权通道；Quicker 式右键长按快捷菜单（低级鼠标钩子吞/放策略、frameless 弹窗 DIP 钳位）；全托管模块"随 Hanxi 关闭"默认翻转为关（Detached 独立运行） | 🟢 已达成 | `modules/wsl`（`netx`+`readiness`+`releases` 子包）、`modules/quickmenu`（`mousetrap` 子包） |
| **M11: WSL 发行版运维工作台** | 发行版六操作之上补齐：只读取证抽屉（VHDX 双口径/稀疏/df/IPv4，停止不误启动）、克隆快路径（拷 VHDX+`--import --vhd` 进度事件）、tar 导入面、/etc/wsl.conf 图形编辑（语法闸门+引用校验+写前备份+复验）、VHDX 三级瘦身工作流（强制备份/Tier1 Optimize-VHD/Tier2 重导入）、NAT 端口转发账本（netsh 批量单 UAC、IP 漂移重同步、外部转发不触碰）；收敛半成品（摘除 GetReadiness 死绑定、导出格式选择与多工件登记） | 🟢 已达成 | `modules/wsl`（`distro.go`/`forensics.go`/`clone.go`/`wslconf.go`/`compact.go`/`portproxy.go`） |

---

## 2. 核心功能验收清单

### 2.1 frpc 穿透与实例管理 (M4)
- [x] 多实例独立并发运行与状态隔离
- [x] Windows JobObject 进程树自动绑定与防孤儿进程清理
- [x] Windows DPAPI 本地 Token 凭据硬件加密存储
- [x] frpc 运行时临时 TOML 停机即时擦除
- [x] 控制台输出流实时特征词嗅探（精确反映连通/认证失败/重连状态）
- [x] `frp://` 协议链接一键导出与导入
- [x] 批量端口区间导入规则生成
- [x] 官方 GitHub Releases / 镜像拉取，SHA256 校验与 3 次重试机制

### 2.2 系统与网络工具 (M2, M3, M5)
- [x] 微信机器人助手（二维码扫码登录、AES 加密通信、长轮询、图文/文件推送、多账号）
- [x] 端口扫描与服务指纹探测（高并发探测、Gonmap 协议与 Nginx/Redis/MySQL 指纹识别）
- [x] 端口占用快速定位与三级安全释放（原生 UAC 提权支持）
- [x] 局域网活跃设备扫描（CIDR 探测、MAC 匹配、OUI 厂商识别、设备备注）
- [x] 公网 IPv4/IPv6 探测、网卡拓扑透视、ICMP Ping 统计、Traceroute 路由追踪
- [x] WiFi 已保存配置与明文密码导出
- [x] 内置日志文件查看器与历史清理

### 2.3 平台体验与底层健壮性 (M0, M1, M6, M7)
- [x] Windows 系统托盘常驻（单击切换主窗口显隐、菜单交互、安全彻底退出）
- [x] 关闭主窗口自动最小化到托盘配置
- [x] Windows 注册表开机自启动管理
- [x] 绿色免安装 Portable 模式（检测 `data/` 目录）
- [x] `extapi` 按需懒加载架构（未启用模块 0 内存与 0 协程占用，35 模块统一注册）
- [x] 全局分级通知中心（事件汇聚、历史查询、已读管理）
- [x] 局域网零客户端文件快传（扫码投递、文本联动随手记）

### 2.4 开发环境检测 (M8)
- [x] Git / Go / Node.js / Java / Python / .NET 本机安装检测与路径定位
- [x] 官网最新版本通道查询（GitHub Releases / 官方 API），按本机版本线置顶
- [x] winget 等包管理器升级命令提示
- [x] .NET 并排安装全列表与微软官方支持线（LTS/STS/EOL）对照
- [x] 在资源管理器中定位已安装工具路径

### 2.5 第三方工具托管 (M9)
- [x] 托管三件套标准骨架（`module.go` + `version/` + `instance/`）与前端控制台视图
- [x] 多层完整性校验链（GitHub digest / SHA256SUMS / 官方哈希清单 / 字节数 + PE 版本核对）
- [x] 安装布局矩阵：便携 zip、`msiexec /a` 管理提取、NSIS 静默、当前用户 MSIX、AppInstaller 直装、单文件 exe 下载即安装、MSI 安装版双形态纳管
- [x] 运行态探测（进程枚举 / 互斥体 / 命令通道 / Appx 注册态）与防误杀指纹复核
- [x] 唤窗通道矩阵（命令信使 / EnumWindows / AUMID / Win32 直操作）
- [x] 退出治理（命名管道优雅退出 / 命令通道 / 强杀兜底 + JobObject 内核兜底 + 跟随退出开关）
- [x] 闭源工具脱管模式（Snipaste 保留原生托盘）与 Everything 内嵌 ES.exe 秒级搜索
- [x] 本地版本导入、版本删除、桌面快捷方式、下载/实例事件推送
- [x] `THIRD_PARTY_NOTICES.md` 合规登记（GPL/AGPL/PolyForm/MIT/专有免费全覆盖）与"不捆绑不再分发"红线
- [x] 托管集成踩坑记录沉淀（TROUBLESHOOTING 托管与平台系列 #8–#36 持续积累）

### 2.6 系统就绪与桌面效率 (M10)
- [x] WSL2 十项门槛流式只读体检（三源并发分相推送、骨架先渲染先到先点亮、✓/⚠/✕ 逐项落章、三态结论）
- [x] 「已禁止(403)」根因诊断（Go 原生探测跟随系统代理，区分硬件不满足与 API 网络侧拦截；REST 优先、被拦降级 Releases Atom 双源）
- [x] WSL 安装形态三路信号互证与正规双路卸载（msiexec /X + Remove-AppxPackage，Lxss 残留巡查报告不暗删注册表）
- [x] 白名单提权通道：固定参数 UAC 提权（一键开启 `--install --no-distribution` 绝不捆绑发行版）、DISM 逐条传播退出码
- [x] 应用内 MSI 下载器（进度事件、落系统下载文件夹、本机架构自适应标记，异架构仅作备选）
- [x] 右键长按快捷菜单：`WH_MOUSE_LL` 吞按下/短按回放识别（普通右键零损失）、frameless 置顶弹窗失焦收起、多显示器/边缘 DIP 钳位
- [x] 全托管模块"随 Hanxi 一起关闭"开关默认翻转为关（Detached 独立运行，退出/崩溃不连带）

### 2.7 WSL 发行版运维工作台 (M11)
- [x] 只读取证抽屉：VHDX 逻辑/实占双口径（`GetCompressedFileSizeW`）+ 稀疏标志、guest `df` 根盘用量、发行版 IPv4；运行态通道不可得或未运行时不进 guest（防顺手启动）
- [x] 导出支持 tar.gz/tar 双格式；本会话导出工件全量登记（ListDistroExports），逐份"打开位置/复制路径"
- [x] 克隆快路径：源停机制、8MB 流式拷贝带 `wsl:clone` 进度事件、`--import --vhd` 挂载（WSL 2.7.3+ 能力门控）、导入失败保留拷贝盘并尽力回收半成品实例；源+目标双单飞闸
- [x] 导入面：`wsl --import` 落成新增发行版（扩展名白名单、文件存在性、名称防撞、目标目录同迁移口径、成功后名单复验）
- [x] /etc/wsl.conf 编辑器：自研 INI 语法闸门、`[user] default` 经 `id -u` 存在性求证、写前 root `cp` 备份 `.bak`、stdin `tee` 覆盖 + 读回复验一致才报成功、CRLF→LF 归一、版本门控警示不硬拦
- [x] VHDX 三级瘦身：受理期空间预检（源卷+导出卷 实占+2GB）、强制 tar 备份先行、fstrim+停机确认、Tier1 `Optimize-VHD`（Hyper-V 模块探测在位才弹 UAC）、省量 <100MB 自动转 Tier2 注销重导入、失败路径点名备份位置、稀疏属性尽力恢复
- [x] NAT 端口转发账本：规则持久化 `<DataDir>/wsl-portproxy.json`、netsh 现态对照（已生效/IP 漂移/待应用/未运行）、应用批量单 UAC 先删后加逐条传播退出码、防火墙 `Hanxi WSL <port>` 命名联动、外部转发只展示不触碰、清理托管一键摘除
- [x] 半成品收敛：`GetReadiness` 死绑定摘除（同步通道从未接线）、`WSLView.spec` stale mock 清理
- [x] 优化批次：克隆目标卷空间预检；MSI 下载/克隆全程可取消、瘦身备份/停机段可取消（动盘段拒绝并说明中间态风险）；wsl.conf 读写以开机探针+`test -f` 退出码断存在性（消灭跨语言文案猜测，堵住"停止实例跳过备份直接 tee"的丢数据路径）；瘦身备份目录可指空闲卷；端口转发默认 127.0.0.1+曝光警示、IP 并行探测、应用/清理挂重操作闸、账本损坏逃生口；wsl.conf 保存后一键终止生效引导
- [x] 网络模式三件套：`~/.wslconfig` networkingMode 检测（取证/转发页徽标，mirrored 明示 localhost 直通）；.wslconfig 全文编辑器（语法闸门 + networkingMode 白名单 + 备份 + 原子写复核）；`ShutdownWsl` 全停通道（重操作闸）与保存后生效引导
- [x] 测试面：Go 新增 45+ 用例（命令面形态锁、白名单矩阵、三级流程事件序、备份失败零销毁断言），`WSLView.spec` 21→29 例；全仓 653 前端测试通过

---

## 3. 后续演进方向（未排期）

- **LevelExternal 子进程插件**：`extapi` 契约已预留 manifest + JSON-RPC over stdio 形态，支持模块独立安装/卸载（`Removable` 字段）。
- **托管工具扩量**：持续按"integrate-github-tool"托管模式集成更多开源桌面工具，每新增一款同步登记第三方告知与踩坑记录。
- **v0.3.0 正式发布**：当前版本常量 `product.Version = 0.3.0` 尚未打 git tag（最新 tag 为 v0.2.0），待 dev 分支收敛后发布。
