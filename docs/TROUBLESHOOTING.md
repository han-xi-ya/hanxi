# Hanxi 踩坑记录与问题排查指南 (Troubleshooting & Best Practices)

> 💡 **目的**：记录项目在开发、调试、编译与重构过程中遇到的典型错误、隐性 Bug 与技术陷阱，归纳**正确做法与标准规范**，避免重复踩坑。

---

## 目录

### 0. 产品更名：品牌断代必须同步源码、数据目录与安装身份
- **问题现象与错误原因**：仅替换界面名称会遗留旧的 Go module、Wails bindings、进程名、单实例 ID、Windows 版本资源、NSIS/MSIX 模板和数据目录，导致安装包名称错乱、版本分裂或新旧实例共享状态。
- **排查过程**：分别扫描源码 import、构建脚本、Windows manifest、安装器模板、发布 workflow、前端 bindings 与 `%APPDATA%` 路径，确认产品身份并非单一字符串。
- **正确做法与标准修复方案**：品牌断代版本必须统一迁移源码命名空间、`cmd` 入口、bindings、exe/资产名、单实例 ID、自启项和标准数据目录，并集中维护名称、版本、描述与标识符。Hanxi v0.3.0 明确不读取或迁移旧产品数据。
- **避坑防重犯建议**：发布前执行旧品牌残留扫描，并对运行时版本、PE 资源、NSIS/MSIX 和 Release tag 做一致性校验；真实第三方语义（如 `frpc.exe`、`frp://`）不得机械替换。

1. [前端：Wails v3 事件总线与插件启停热同步](#1-前端wails-v3-事件总线与插件启停热同步)
2. [前端：大流量日志流内存与 DOM 性能问题](#2-前端大流量日志流内存与-dom-性能问题)
3. [后端：Windows 孤儿进程与 JobObject 作业隔离](#3-后端windows-孤儿进程与-jobobject-作业隔离)
4. [后端：DPAPI 凭据加密与临时配置文件生命周期](#4-后端dpapi-凭据加密与临时配置文件生命周期)
5. [Git：规范化中文原子提交](#5-git规范化中文原子提交)
6. [构建：UPX 压缩 Go 程序导致运行时内存暴涨](#6-构建upx-压缩-go-程序导致运行时内存暴涨)
7. [WiFi 密码：netsh 解析三连坑（GBK 编码 / $ 行锚 / 提权输出通道）](#7-wifi-密码netsh-解析三连坑gbk-编码--行锚--提权输出通道)
8. [托管 Tauri 应用：单实例互斥体契约 / GitHub digest 校验 / 关窗退出语义](#8-托管-tauri-应用单实例互斥体契约--github-digest-校验--关窗退出语义)
9. [Windows 开机自启参数必须与入口解析及窗口状态保持一致](#14-windows-开机自启参数必须与入口解析及窗口状态保持一致)

> **补录说明（2026-09-16）**：以上 1–9 为早期编制，编号与锚点已随正文演进发生漂移（如正文中 UPX 实为 7、WiFi 实为 8，托管系列 8–17 与平台系列重号）；本日志只追加不改写正文，以下按正文标题补全缺失条目的完整索引，链接以实际锚点为准。

- [6. 后端：Windows 网卡与网络配置枚举性能陷阱 (GetAdaptersAddresses vs PowerShell/WMI)](#6-后端windows-网卡与网络配置枚举性能陷阱-getadaptersaddresses-vs-powershellwmi)
- [iOS Safari 大文件上传：单次二进制流优先于连续分片请求](#ios-safari-大文件上传单次二进制流优先于连续分片请求)
- [前后端默认上传上限不一致导致超大文件被误拒绝](#前后端默认上传上限不一致导致超大文件被误拒绝)
- [7. 构建：UPX 压缩 Go 程序导致运行时内存暴涨](#7-构建upx-压缩-go-程序导致运行时内存暴涨)
- [8. WiFi 密码：netsh 解析三连坑（GBK 编码 / $ 行锚 / 错误归因权限）](#8-wifi-密码netsh-解析三连坑gbk-编码---行锚--错误归因权限)
- [9. Wails v3 类型化事件：Emit 载荷必须与 RegisterEvent 注册类型「完全一致」，指针≠值类型](#9-wails-v3-类型化事件emit-载荷必须与-registerevent-注册类型完全一致指针值类型)
- [10. Win32 PE 版本字符串：VerQueryValueW 两个实测坑（valLen 单位 + utf16.Decode 忽略 NUL）](#10-win32-pe-版本字符串verqueryvaluew-两个实测坑vallen-单位--utf16decode-忽略-nul)
- [11. explorer.exe 参数语义：传文件路径 = 执行该文件；定位文件必须用 /select,](#11-explorerexe-参数语义传文件路径--执行该文件定位文件必须用-select)
- [12. ES.exe 命令行契约三连坑（-utf8 不存在 / 控制台 OEM 编码 / 无配置时单列输出布局）](#12-esexe-命令行契约三连坑-utf8-不存在--控制台-oem-编码--无配置时单列输出布局)
- [13. 编排互斥锁不可重入：嵌套调用自带锁的入口方法会永久死锁](#13-编排互斥锁不可重入嵌套调用自带锁的入口方法会永久死锁)
- [15. GitHub Releases 列表响应体会被资产元数据放大](#15-github-releases-列表响应体会被资产元数据放大)
- [16. Go 与 Node.js 官网版本通道不能靠字符串或历史规律猜测](#16-go-与-nodejs-官网版本通道不能靠字符串或历史规律猜测)
- [17. Java 与 Python 的“最新版”必须先限定发行版和版本线](#17-java-与-python-的最新版必须先限定发行版和版本线)
- [18. npm 与 pnpm 升级命令取决于安装来源](#18-npm-与-pnpm-升级命令取决于安装来源)
- [19. .NET 是双编号体系：卡片显示 SDK、版本关系却只能按 runtime 比较](#19-net-是双编号体系卡片显示-sdk版本关系却只能按-runtime-比较)
- [9. 托管 .NET WinForms 应用：tag/资产版本不同形 + Global 互斥体 + EnumWindows 退出](#9-托管-net-winforms-应用tag资产版本不同形--global-互斥体--enumwindows-退出)
- [10. 应用退出钩子未接线：托管的工具进程不随 Hanxi 退出而关闭](#10-应用退出钩子未接线托管的工具进程不随-hanxi-退出而关闭)
- [11. 上游发行侦查：只查 GitHub Releases 会漏掉自托管官方渠道（EarTrumpet）](#11-上游发行侦查只查-github-releases-会漏掉自托管官方渠道eartrumpet)
- [12. 托管 Electron 应用（Recordly）：NSIS 在线安装器五连坑 + 进程名探测契约](#12-托管-electron-应用recordlynsis-在线安装器五连坑--进程名探测契约)
- [13. 托管 WPF 绿色单文件应用（PaperTodo）：GitHub digest 并非全量存在 + 自建单实例命令契约](#13-托管-wpf-绿色单文件应用papertodogithub-digest-并非全量存在--自建单实例命令契约)
- [14. 托管安装器-only 的 Tauri 应用（PicLite）：MSI 管理提取路线 + `-siw` 恒可见探针陷阱 + 退出通道归零](#14-托管安装器-only-的-tauri-应用piclitemsi-管理提取路线---siw-恒可见探针陷阱--退出通道归零)
- [15. 托管 Keyviz：single-instance 空回调=唤窗契约不存在 + MSI 资产名定制 + 常驻可视化工具无空闲语义](#15-托管-keyvizsingle-instance-空回调唤窗契约不存在--msi-资产名定制--常驻可视化工具无空闲语义)
- [16. 托管 QuickLook：低级键盘钩子非注入=强杀零残渣 + 命名管道 Quit/Reload 优雅退出通道 + 便携 zip 反斜杠布局](#16-托管-quicklook低级键盘钩子非注入强杀零残渣--命名管道-quitreload-优雅退出通道--便携-zip-反斜杠布局)
- [17. 托管 LiteMonitor：路径派生互斥体名不可复现 + requireAdministrator 的 740 直拒与 UIPI 边界 + 首启 settings.json 种子关更](#17-托管-litemonitor路径派生互斥体名不可复现--requireadministrator-的-740-直拒与-uipi-边界--首启-settingsjson-种子关更)
- [20. Wails v3 托盘：动态右键菜单重建线程安全与 Dialog 取消语义](#20-wails-v3-托盘动态右键菜单重建线程安全与-dialog-取消语义)
- [21. 托管果核看图（GuoheView）：多实例上游击穿互斥体探测模板 + 便携 zip 顶层包装目录 + 官方仅 MD5](#21-托管果核看图guoheview多实例上游击穿互斥体探测模板--便携-zip-顶层包装目录--官方仅-md5)
- [22. 托管 ddns-go：kardianos 服务劫持后门变量 + 端口冲突僵活 + 裸写配置截断 + 内嵌 iframe SameSite](#22-托管-ddns-gokardianos-服务劫持后门变量--端口冲突僵活--裸写配置截断--内嵌-iframe-samesite)
- [23. envcheck 定向开洞：npm 全局工具一键装升卸的安全边界与三处非典型陷阱](#23-envcheck-定向开洞npm-全局工具一键装升卸的安全边界与三处非典型陷阱)
- [24. 托管 rust-portable 应用（RustDesk/SubnetDesk）：外层秒退进程不能当生命周期锚点 + 提取目录归属判别 + 便携/安装版双同名陷阱](#24-托管-rust-portable-应用rustdesksubnetdesk外层秒退进程不能当生命周期锚点--提取目录归属判别--便携安装版双同名陷阱)
- [26. 托管 Rufus：固定名互斥体可探但"二次拉起弹模态错误框"反成噪音 + 磁盘级写入工具的强杀安全边界 + 便携 ini 存在即生效兼关更](#26-托管-rufus固定名互斥体可探但二次拉起弹模态错误框反成噪音--磁盘级写入工具的强杀安全边界--便携-ini-存在即生效兼关更)
- [27. 托管用户可配关窗行为的下载器（Bili23 Downloader）：Quit 兜底强杀从"安全网"反转为"数据事故源" + 改名壳 exe 版本探测迁移到源码常量](#27-托管用户可配关窗行为的下载器bili23-downloaderquit-兜底强杀从安全网反转为数据事故源--改名壳-exe-版本探测迁移到源码常量)
- [25. 前端重构 Phase 0/1：ESLint flat 的 TS base 压掉 .vue 解析器 + Vitest 5 API 变更与 fake timers 死锁 + CSS 注释星杠截断](#25-前端重构-phase-01eslint-flat-的-ts-base-压掉-vue-解析器--vitest-5-api-变更与-fake-timers-死锁--css-注释星杠截断)
- [28. 托管模块批量补数据目录：path_provider Windows 目录名按 PE 版本信息拼接的实证链 + 模板拷贝族"文件级 grep 盘点功能"误判](#28-托管模块批量补数据目录path_provider-windows-目录名按-pe-版本信息拼接的实证链--模板拷贝族文件级-grep-盘点功能误判)
- [29. 全局鼠标钩子 + frameless 弹窗（quickmenu MVP）："吞起不吞按"真机卡死系统输入的反转，外加 beta.10 三处认知差](#29-全局鼠标钩子--frameless-弹窗quickmenu-mvp吞起不吞按真机卡死系统输入的反转外加-beta10-三处认知差)
- [30. 安装版（MSI 系统安装）纳管：魔数错配拒好包、同镜像服务误判与 spec mock 缺方法团灭](#30-安装版msi-系统安装纳管魔数错配拒好包同镜像服务误判与-spec-mock-缺方法团灭)
- [31. 托管 VS Code：官方 CDN 单源分发 + 哈希仅最新版可得 + 互斥体按安装形态分治 + Inno 强关确认闸](#31-托管-vs-code官方-cdn-单源分发--哈希仅最新版可得--互斥体按安装形态分治--inno-强关确认闸)
- [32. 托管 TranslucentTB：无设置窗口信使语义不是唤窗 + 通用窗口类名须验属主 + 便携版 Win11/框架包双重前提](#32-托管-translucenttb无设置窗口信使语义不是唤窗--通用窗口类名须验属主--便携版-win11框架包双重前提)
- [33. 前端深色主题：未迁移视图的 scoped 原子副本携带裸色值，只在深色下暴露](#33-前端深色主题未迁移视图的-scoped-原子副本携带裸色值只在深色下暴露)
- [34. WSL 就绪模块：wsl.exe 的 UTF-16 解码不能用 NUL 密度启发式，中文提示行会同时打穿解码与守卫](#34-wsl-就绪模块wslexe-的-utf-16-解码不能用-nul-密度启发式中文提示行会同时打穿解码与守卫)
- [35. Hanxi 内 Go 网络请求"浏览器能上、程序 403"：系统代理不进环境变量，api.github.com 对云出口 IP 区域性拦截](#35-hanxi-内-go-网络请求浏览器能上程序-403系统代理不进环境变量apigithubcom-对云出口-ip-区域性拦截)
- [36. 「虚拟机平台开着没」：HypervisorPresent=true 会骗人——内核隔离也造监控程序，功能开关的免管理员判据要靠服务存在性并做 DISM 基准校准](#36-虚拟机平台开着没hypervisorpresenttrue-会骗人内核隔离也造监控程序功能开关的免管理员判据要靠服务存在性并做-dism-基准校准)
- [37. 「发行版怎么装不上还报成功」：`Start-Process -Wait` 从不回传子进程退出码——裸提权通道的假成功是 #36 分号链的同族病灶](#37-发行版怎么装不上还报成功start-process--wait-从不回传子进程退出码裸提权通道的假成功是-36-分号链的同族病灶)
- [38. 托管工具可靠性：路径词法校验、goroutine 外层互斥与取消请求都不是生命周期边界](#38-托管工具可靠性路径词法校验goroutine-外层互斥与取消请求都不是生命周期边界)
- [39. WSL 只读取证的三个 guest 探测坑：`wsl -d` 会顺手开机、`ip -o` 的 brd 地址截胡、df 折行](#39-wsl-只读取证的三个-guest-探测坑wsl--d-会顺手开机ip--o-的-brd-地址截胡df-折行)
- [40. 开发环境：Git Bash 会话派生 Windows 子进程时 PATH 被截断，wails3/go test 找不到 go 与 cmd.exe](#40-开发环境git-bash-会话派生-windows-子进程时-path-被截断wails3go-test-找不到-go-与-cmdexe)
- [41. FileShare「访问口令」假在：配置字段、绑定、设置存储全链路齐活，唯独服务端没人读它](#41-fileshare访问口令假在配置字段绑定设置存储全链路齐活唯独服务端没人读它)
- [42. Paseo 集成：同是 Electron 单实例锁，`second-instance` 语义可完全相反——唤窗不能照抄信使族](#42-paseo-集成同是-electron-单实例锁second-instance-语义可完全相反唤窗不能照抄信使族)
- [43. WSL 唤终端「点了没反应」：cmd start + HideWindow 时新控制台窗口继承隐藏显示状态](#43-wsl-唤终端点了没反应cmd-start--hidewindow-时新控制台窗口继承隐藏显示状态)
- [44. 数据目录实测迁移：Paseo 进程疑似"逃托"退出清理，外加迁移与环境的三枚次级坑](#44-数据目录实测迁移paseo-进程疑似逃托退出清理外加迁移与环境的三枚次级坑)
- [45. 便携标记目录更名 hanxidata：泛化名歧义、旧包兼容与 Compress-Archive 丢空目录](#45-便携标记目录更名-hanxidata泛化名歧义旧包兼容与-compress-archive-丢空目录)
- [46. WSL 名单连环误判：`wsl -l -q` 管道输出的分隔符是 \r\n 而非 NUL（首诊"传播延迟"系误归因，二次实锤修正）](#46-wsl-名单连环误判wsl--l--q-管道输出的分隔符是-rn-而非-nul首诊传播延迟系误归因二次实锤修正)
- [47. 集成「内测 + 闭源壳 + 无便携版」产品：托管降级为「仅版本+下载」的决策与连环环境坑（douzy 集成实战）](#47-集成内测--闭源壳--无便携版产品托管降级为仅版本下载的决策与连环环境坑douzy-集成实战)
- [48. WSL「默认 D:\wsl 却还是装到 C」：落位三坑同源——偏好粘滞、装完即迁无归因、能力无闸门](#48-wsl默认-dwsl-却还是装到-c落位三坑同源偏好粘滞装完即迁无归因能力无闸门)
- [49. 全库确认框「\n\n 分段」被 HTML 折叠成文字墙：设计防线在渲染层一秒归零](#49-全库确认框nn-分段被-html-折叠成文字墙设计防线在渲染层一秒归零)
- [50. 快捷菜单轮盘连环白边：旧注释谎称"Wails 无透明能力"，GDI 区域硬裁与近白 canvas 双重露底](#50-快捷菜单轮盘连环白边旧注释谎称wails-无透明能力gdi-区域硬裁与近白-canvas-双重露底)
- [51. 阴影 token 当颜色用：`box-shadow: 0 8px 32px var(--shadow-panel)` 七处整条声明被解析器静默丢弃](#51-阴影-token-当颜色用box-shadow-0-8px-32px-varshadow-panel-七处整条声明被解析器静默丢弃)
- [52. GUI 子系统双击启动"日志恒空"：`io.MultiWriter(os.Stderr, f)` 被无效 stderr 句柄中断，落盘跟着拖垮](#52-gui-子系统双击启动日志恒空iomultiwriterosstderr-f-被无效-stderr-句柄中断落盘跟着拖垮)
- [53. Wails v3 beta.10 无公开 Destroy() ≠ 无法销毁窗口：摘掉 WindowClosing 拦截 hook 再 Close 即走内部真销毁](#53-wails-v3-beta10-无公开-destroy--无法销毁窗口摘掉-windowclosing-拦截-hook-再-close-即走内部真销毁)
- [54. Go 直调 COM 式 vtable：uintptr 跨函数转发违反 unsafe 规则，栈增长后 C++ 回写旧副本必崩（ORT 三坑）](#54-go-直调-com-式-vtableuintptr-跨函数转发违反-unsafe-规则栈增长后-c-回写旧副本必崩ort-三坑)

---

### 0.1 Windows 安装包验证依赖未进入 PATH
- **问题现象与错误原因**：`task package INSTALL_SCOPE=user` 完成应用构建后，在调用 `makensis` 时报告 `executable file not found in $PATH`。项目打包任务依赖 NSIS，但本机未安装 NSIS 或安装目录未加入 PATH。
- **排查过程**：确认 `task build`、Wails bindings、前端构建与 `hanxi.exe` 均成功，失败点仅发生在 `create:nsis:installer` 的 `makensis` 调用。
- **正确做法与标准修复方案**：安装 NSIS，并确保 `makensis.exe` 可由终端直接调用；随后重新执行 `task package INSTALL_SCOPE=user`。CI 也应显式安装 NSIS，而不是假定 runner 已提供。
- **避坑防重犯建议**：打包任务增加 `command -v makensis` 前置检查和清晰错误提示，将编译成功与安装包生成成功分开报告。

### 1. 前端：Wails v3 事件总线与插件启停热同步

- **问题现象**：在工作台启用/停用插件模块后，主界面虽然变了，但侧边栏菜单未刷新，必须重启应用才能看到新路由入口。
- **错误根源**：
  1. 插件状态仅在后端内存变更，未主动通知各挂载组件；
  2. 前端事件监听没有在组件销毁时及时注销，导致多次挂载后产生重复监听与内存泄漏。
- **正确做法**：
  - 在插件启停后，通过 Wails 事件广播 `Events.Emit('ext:changed', ...)`。
  - 组件内使用 `onMounted` 监听并保存取消句柄，在 `onUnmounted` 中精准释放：
    ```ts
    let unlisten: (() => void) | null = null
    onMounted(() => {
      unlisten = Events.On('ext:changed', () => loadData())
    })
    onUnmounted(() => {
      if (unlisten) unlisten()
    })
    ```
- **避坑建议**：所有订阅全局事件的地方必须配对注销；跨模块通知优先通过 Wails 事件总线分发。

---

### 2. 前端：大流量日志流内存与 DOM 性能问题

- **问题现象**：frpc 高频输出日志或并发端口扫描时，前端变卡、内存占用持续上涨（甚至崩溃）。
- **错误根源**：
  1. 使用普通数组无限制 `push`，未做上限截断（RingBuffer / 滑动窗口）；
  2. 频繁触发 Vue 深度响应式计算与大量 DOM 重绘。
- **正确做法**：
  - 使用 `shallowRef` 减少响应式开销。
  - 限制最大日志行数（例如保留最近 1000~2000 行），超出时进行切片丢弃。
  - 使用防抖/节流合并前端渲染帧，或者采用虚拟滚动（Virtual Scroll）。
- **避坑建议**：日志类、流式数据类不要放入深层 `ref` 或 `reactive`，必须设定内存硬上限。

---

### 3. 后端：Windows 孤儿进程与 JobObject 作业隔离

- **问题现象**：Hanxi 异常退出或被任务管理器结束时，启动的 `frpc.exe` 依然在后台运行并占用端口。
- **错误根源**：Windows 普通 `exec.Command` 生成的子进程生命周期脱离父进程，父进程退出不会触发子进程自动终止。
- **正确做法**：
  - 在 Windows 下创建内核级 `JobObject`（作业对象），并配置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 标志位。
  - 将所有启动的子进程绑定到该 JobObject，操作系统将在主进程退出时由内核强制清理所有关联子进程。
- **避坑建议**：涉及外部子进程（frpc、nmap、脚本等）必须全部挂载进 JobObject，并在应用退出钩子中再次做兜底清理。

---

### 4. 后端：DPAPI 凭据加密与临时配置文件生命周期

- **问题现象**：将包含敏感 Token 的 TOML 配置文件存盘后，遗留在磁盘被其他程序读取，或明文存储存在安全合规风险。
- **错误根源**：直接明文保存配置文件到 AppData 目录，且实例停止后未清理临时文件。
- **正确做法**：
  - 敏感 Token 使用 Windows 原生 `CryptProtectData` (DPAPI) 绑定当前登录用户密钥加密存储。
  - 运行时动态生成临时 TOML 启动 `frpc.exe`，进程结束/停止时立即擦除临时文件。
- **避坑建议**：绝不落盘明文 Token，临时运行配置用完即删。

---

### 5. Git：规范化中文原子提交

- **问题现象**：把多天、多模块的改动全部揉进一个提交 `fix: update`，出现问题时无法定位和回滚。
- **正确做法**：
  - 每次提交采用 `<type>(<scope>): <中文描述>`，如 `feat(wechat): ...`、`fix(frpc): ...`。
  - 单一职责提交，提交前检查 `git status` 与 `git diff`，确保不夹带无关产物。

---

### 6. 后端：Windows 网卡与网络配置枚举性能陷阱 (GetAdaptersAddresses vs PowerShell/WMI)

- **问题现象**：局域网扫描、公网 IP 等模块打开或切换时，获取网卡下拉列表卡顿数秒（2~4 秒延迟）。
- **错误根源**：
  - 调用 `exec.Command("powershell", "-Command", "Get-CimInstance ...")` 执行 WMI/CIM 脚本获取网卡详细信息（如 DNS、临时 IPv6、网关等）。
  - Windows 每起一个 `powershell.exe` 子进程冷启动耗时极高（0.8~2.5 秒），且解析输出文本容易受系统语言环境干扰。
- **正确做法**：
  - 采用 Windows 原生系统库 `iphlpapi.dll` 的 `GetAdaptersAddresses` API，通过内存系统调用（Syscall）遍历 `IpAdapterAddresses` 链表。
  - 直接在内存中提取单播 IPv4/IPv6、临时 IPv6 标记 (`SuffixOrigin == IpSuffixOriginRandom`)、网关及 DNS 服务器列表。
  - 耗时从 **2000~4000 毫秒降低至 0.5 毫秒以内**（亚毫秒级瞬时返回），免除管理员提权与子进程开销。
- **避坑建议**：在 Windows 平台获取系统网络拓扑时，坚决避免起子进程执行 PowerShell/WMI 脚本，统一使用 Win32 原生 Syscall。

### iOS Safari 大文件上传：单次二进制流优先于连续分片请求

- **问题现象与错误原因**：iPhone Safari 从相册选择 2.74 GB 视频后，分片上传反复卡在第 1 或第 2 个 4 MiB 分片，页面显示速度 0。即便每个上传 API 强制 `Connection: close`、增加 XHR 无进度 watchdog 和服务端读取 deadline，WebKit 对连续 Blob 分片请求及相册文件提供器的组合仍可能挂起；同时 append 在完整分片结束后才累计统计，无法从“速度 0”区分慢速读取与真正停滞。
- **排查过程**：确认 Go 服务端没有大文件总读写超时，文件大小使用 `int64`，2.74 GB 不存在整数溢出；实机多轮验证显示故障来自 Safari 对连续 Blob 请求的调度，而不是固定发生在某一字节位置。继续维护多请求上传状态机会提高复杂度，仍无法保证 WebKit 请求稳定。
- **正确做法与标准修复方案**：局域网 Web 上传统一改为单文件单请求：`POST /api/upload?dir=&name=&size=`，请求体直接发送 `File`，类型为 `application/octet-stream`。浏览器和 Go 都采用流式 I/O，不把整个文件载入内存；服务端使用 1 MiB 复用缓冲区边读边写隐藏临时文件、边累计实时上传字节，实际接收长度必须等于声明 size，成功后用 `os.Rename` 原子发布，失败或取消自动删除临时文件，同名目标通过非冲突命名保留旧文件。首页响应设置 `Cache-Control: no-store`，避免 Safari 继续运行旧上传脚本。
- **避坑建议**：单次流式上传不是“整文件载入内存”，只是一个长 HTTP 请求，局域网吞吐通常比连续小请求更稳定且协议开销更低；代价是断网、锁屏或切后台后不能断点续传，必须从头重传。对于以易用性和局域网满速为优先的工具，不应为了理论续传能力长期维护一个在目标浏览器上不稳定的复杂分片状态机。

### 前后端默认上传上限不一致导致超大文件被误拒绝

- **问题现象与错误原因**：iPhone Safari 选择小文件可以上传，但相册中的 2.74 GB 视频立即失败。Go 服务默认 `MaxUploadSizeMB=0`（不限制），而桌面设置页的临时表单曾默认 `1024`；当用户在配置尚未加载完成前启动服务或保存规则时，这个 1 GB 临时值会写入运行配置，移动端随后按 `/api/config` 返回值拒绝大于 1 GB 的文件。
- **排查过程**：2.74 GB 的字节数仍在 JavaScript 安全整数范围内，Go 参数也使用 `int64`，并不存在 2 GB 整数溢出。沿 `FileShareView.vue -> SaveConfig -> /api/config -> uploadFileStream` 追踪后，确认故障来自前后端默认值契约不一致，而不是 Safari 或文件大小类型。
- **正确做法与标准修复方案**：前端配置模型与服务端统一默认 `MaxUploadSizeMB=0`；设置页明确暴露“单文件上传上限”，并提示 0 表示不限制；服务端拒绝负数配置。为大于 2 GiB 的 size 增加仅解析参数、不分配巨量内存的边界测试，验证配置为 0 时接受，配置为 1024 MB 时明确拒绝。
- **避坑建议**：同一配置项的前端初始值、后端默认值和公开 API 语义必须完全一致。即使页面挂载后会异步加载后端配置，也不能让临时默认值在加载竞态中成为可保存的错误配置；大文件边界测试应使用参数解析验证，避免为尺寸测试实际分配数 GB 内存。

---

### 7. 构建：UPX 压缩 Go 程序导致运行时内存暴涨

- **问题现象与错误原因**：生产/发布构建用 `upx --best` 压缩 exe（约 15MB → 4.9MB）。运行后任务管理器显示进程工作集高达 90~170MB，而同一源码的未压缩版本仅 9MB。原因：UPX 启动时把完整压缩镜像读入内存并**解压出整个原始镜像常驻**——解压副本无磁盘 backing，所有页都是不可换出的私有工作集；Go 二进制节多、镜像大，放大了该效应。本质是「省磁盘、耗内存」的交易，且 UPX 壳是杀软误报高发源。
- **排查过程**：功能全部关闭的状态下做灰度对比（压缩版 vs 未压缩版、同一源码、同一机器）：压缩版 93MB+、未压缩版 9MB，差异全部来自 UPX 解压镜像常驻；WebView2 是独立进程，不参与该差异。
- **正确做法与标准修复方案**：移除 UPX，改用 Go 原生 `-ldflags="-w -s"` 去符号表/DWARF 减体积（生产构建在 `build/windows/Taskfile.yml` 已配置，含 `-H windowsgui`）。体积从 18.6MB 降到 13.4MB，运行时内存回到 9~30MB 量级；panic 栈/报错函数名不受影响（Go pclntab 不依赖符号表），仅失去调试器断点与崩溃 dump/pprof 符号化能力。
- **避坑建议**：Go 程序不要用 UPX 追 "极致压缩"，不要以运行时内存和 Defender 误报为代价。减体积统一使用原生 `-s -w`（去符号），panic 栈仍可读，只有调试器/崩溃分析会失去符号；本仓库 UPX 相关链路（`scripts/build_and_compress.bat`、`.github/workflows/release.yml` 的 Compress Binary 步骤）已移除。

---

### 8. WiFi 密码：netsh 解析三连坑（GBK 编码 / $ 行锚 / 错误归因权限）

- **问题现象与错误原因**：实现「查看本机 WiFi 密码」(`internal/modules/wifi`) 时连环踩坑——
  1. **Go 正则 `$` 不等于行尾**：`regexp.MustCompile(\`...:\s*(.+?)\s*$\`)` 的 `$` 默认只锚定**整个字符串末尾**。netsh 输出是多行文本，密钥行 `Key Content : 12345` 后还有 `Cost settings...` 等，导致该正则永远匹配不上——**输出里明明有明文密码，却解析为空**。必须加 `(?m)` 多行标志让 `$` 按行匹配。
  2. **netsh 输出是 OEM 代码页**：中文系统 netsh 管道输出为 GBK 字节，直接拿 UTF-8 字面量 `strings.Contains(line, "关键内容")` 匹配永远为假——必须先 `simplifiedchinese.GBK` 解码。
  3. **大量排查浪费源于错误归因**：解析层坏掉时误判「普通权限 netsh 不给密钥行 → 需要提权」，为此写了整套 UAC helper + 文件协议 + 在 DPAPI 上花大量时间。真相：**本机普通权限直接执行 `netsh key=clear` 即有明文**（Windows 10/11 大部分普通用户可读；仅部分机器/组策略网络需管理员）。修复正则后普通权限直接读出密码，提权兜底只在个别网络真读不到时出现。
- **正确做法与标准修复方案**：
  - 枚举与密码全走 `netsh wlan show profiles` + `netsh wlan show profile name=X key=clear`，普通权限直接读取（demo 同款体验），解析按行 + `strings.Cut`（首个冒号切分，密码本身可能含冒号）。
  - 提权兜底：仅当密码页有读不到的项时才提供一键 UAC 获取（helper 用「主进程预创建临时文件 + 写文件回传」的 JSON 协议，GUI 子系统下 stdout 管道不可靠；`Start-Process -Verb RunAs` + 文件回传是稳定组合）。
- **避坑建议**：
  - 凡解析「命令输出」类文本（netsh、ipconfig、tasklist…），一律先做代码页解码 + 按行解析；凡 Go 正则匹配多行文本，立即检查是否需要 `(?m)`。
  - **先定位后归因**：行为异常时先确认「解析层是否工作」再怀疑权限/系统——本次在坏正则下做了一整套提权基建，属根因错判的连锁浪费。修好解析后优先用「最朴素路径」复验（直接跑一遍命令），再决定要不要加复杂度。

---

### 9. Wails v3 类型化事件：Emit 载荷必须与 RegisterEvent 注册类型「完全一致」，指针≠值类型

- **问题现象与错误原因**：全局统一通知中心实现后，LAN 扫描/端口扫描完成等所有模块通知**在前台永远弹不出卡片**；但 `lan:progress` 进度条、`wechat:message-received` 私信事件一切正常，且前端 `Events.On('notify:received')` 从未收到任何回调。根因：`internal/app/app.go` 用 `application.RegisterEvent[notify.Notification]("notify:received")` 注册的是**值类型**，而 `internal/notify/hub.go` 里 `Emit("notify:received", n)` 传的是 `*Notification` **指针**。Wails v3 (beta.10) 的 `EventProcessor.Emit` 会对全局注册事件做 `reflect.TypeOf(event.Data) == RegisteredType` **精确到值/指针的严格校验**（`pkg/application/events.go` `validateCustomEvent`），不匹配直接 `event.Cancel()` 丢弃并仅向错误处理器上报——**事件被静默吞掉，前端毫无感知**。
- **排查过程**：长时间被困在「前端解包结构不对」的假象上（反复改 App.vue 的事件对象/包装/数组兼容逻辑、加/删私有微信监听、怀疑单层监听架构），甚至靠「前端直接 pushToast 绕过事件总线」的补偿代码让设置页测试卡片假性通过。最终对比「能工作的事件（lan:progress 传值类型）vs 不能工作的事件（notify:received 传指针）」，翻 Wails v3 beta.10 源码 `events.go` 锁定严格类型校验。
- **正确做法与标准修复方案**：`Emit` 载荷与 `RegisterEvent[T]` 的 `T` 必须类型一致——注册值是值就解引用 `Emit("notify:received", *n)`，注册值是指针就传指针。在 emit 处加注释说明这一约束，防止后人误改。
- **避坑建议**：
  - 凡是「后端明明 `Emit` 了但前端 `Events.On` 收不到」的事件，第一步先核对**注册类型与发射类型的值/指针一致性**，再看前端解包结构；不要在渲染层盲目堆兼容代码。
  - 不要在业务层写「前端直接调 pushToast 绕过后端事件总线」的补偿逻辑掩盖链路故障——测试按钮会假性通过，真实业务事件仍全灭；正确路径是修好管道本身，保留单一信源。

---

### 10. Win32 PE 版本字符串：VerQueryValueW 两个实测坑（valLen 单位 + utf16.Decode 忽略 NUL）

- **问题现象与错误原因**：实现 Everything 模块的 `exeFileVersion`（读取 Everything.exe 的 FileVersion 做导入版本识别）时，纯 Go syscall 实现连续踩两个坑，读出 `"1.5.0."` 或 `"1.5.0.1422b\x006\v\x01InternalName..."` 这类截断/越界垃圾，而真值应为 `"1.5.0.1422b"`：
  1. **`VerQueryValueW` 对 StringFileInfo 字符串返回的 `valLen` 是字符数而非字节数**（实测 voidtools Everything.exe：12 个 WCHAR 返回 12）。按 MSDN 常见理解的「字节数/2」切分会把字符串拦腰截断。
  2. **`unicode/utf16.Decode` 不因 NUL 停止**——它老老实实 decode 每一个 unit，`szValue` 的终止 NUL 后面的下一个 String 条目头（wLength/wValueLength/wType + 下一个 key）会被一并吞进结果字符串。
- **排查过程**：假 exe 单测只覆盖了「读不出版本走兜底」路径，两个坑全部被真实 PE 的活体验证暴露（对用户机器上的 `Everything.exe` 实测）。用临时单测逐 unit dump `valPtr` 处 40 个 uint16，确认 unit[11]==0x0000 为终止符、valLen=12，锁定两个根因。
- **正确做法与标准修复方案**：
  - 彻底不信任 `valLen`：解码边界 = 「指针到版本信息缓冲末尾的距离」（`uintptr(len(buf)) - (uintptr(valPtr) - uintptr(&buf[0]))`），除以 2 得 unit 数并封顶 512。
  - 解码用 `golang.org/x/sys/windows.UTF16ToString(units)`——内部在**首个 NUL 截断**，天然免疫坑 2；不要手写 `utf16.Decode`。
- **避坑建议**：
  - 涉及 Win32 API 的版本资源/字符串读取，**必须用真实 PE 文件做一次活体验证**（本仓库 `Everything.exe` 即现成样本），单测假样例无法覆盖这类「API 语义与文档/直觉不符」的坑。
  - 任何「从裸指针转 Go slice」的代码，长度一律从自有缓冲边界推导，不要相信被调方返回的长度语义。

---

### 11. explorer.exe 参数语义：传文件路径 = 执行该文件；定位文件必须用 /select,

- **问题现象与错误原因**：markeron「打开安装目录」按钮传了 **exe 路径**给 `explorer.exe`（`internal/app/service.go` 的 `AppService.OpenPath` 对目录/文件不加区分、一律 `explorer.exe <path>`），结果点按钮**没打开文件夹而是直接启动了 MarkerOn 程序**。根因：explorer.exe 收到**文件**路径参数时按「默认打开方式」处理（.exe → 执行），只有收到**目录**路径才稳定打开文件夹窗口。
- **正确做法与标准修复方案**：
  - 「打开安装目录」类诉求：**只传目录路径**（EverythingView/MarkerOnView 均传 `v.dir`，绝不传 `v.exePath`）。
  - 「在资源管理器中定位/选中文件」：`exec.Command("explorer.exe", "/select,"+path)`——**`/select,` 与路径必须是同一个参数**（逗号是语法一部分），拆成两个参数无效。
  - 「用默认程序打开文件」：`exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)`（目录同样适用，比 explorer 更通用）——Everything 模块的 `OpenTarget`/`RevealTarget` 即此二分实现。
- **避坑建议**：任何走 explorer.exe 的通用「打开路径」工具方法，先把目录/文件语义想清楚再传参；对已有通用方法（如 `AppService.OpenPath`）给非目录调用前先确认其文件语义，必要时另立专用 RPC，别让一个「打开目录」按钮承担隐式的文件语义。

---

### 12. ES.exe 命令行契约三连坑（-utf8 不存在 / 控制台 OEM 编码 / 无配置时单列输出布局）

- **问题现象与错误原因**：Everything 模块内嵌搜索首次真机联调报 `搜索引擎执行失败: Error 6: Unknown switch.`，随后排查又发现中文路径乱码风险与列布局误判。三个坑全部源于**照记忆写 ES 契约而没有先跑 `es.exe -help` 实测**：
  1. **`-utf8` 开关不存在**——help 里只有 `-utf8-bom`（且仅作用于导出文件）。注入不存在的开关 → 退出码 6。
  2. **直写控制台时输出经 OEM 代码页**（中文系统 GBK），且没有控制台 UTF-8 开关；中文路径经 Go exec 捕获必然乱码。
  3. **无 es.ini 配置时 stdout 与 `-export-tsv` 都是「每行一个全路径」的单列布局**——文档里常见的 Name/Path/Size/DateModified 多列布局只有写入 es.ini 配置后才会出现。按多列解析会整表错位。
- **正确做法与标准修复方案**：
  - 输出通道一律 `-export-tsv <临时文件> -utf8-bom -no-header`：导出文件恒为 UTF-8（BOM 双保险），彻底绕开控制台代码页；读回后删除临时文件。加 `-timeout <ms>` 交给 ES 自控等待。
  - 结果行为全路径单列：逐行解析取路径，**大小/修改时间/目录标记用本地 `os.Stat` 权威补齐**（顺带吞掉索引与磁盘的毫秒级漂移）；超长路径 stat 失败时加 `\?\` 前缀重试；查询后瞬间消失的目标直接跳过。
  - 错误翻译按退出码：6 = Unknown switch、8 = IPC not found（实例未运行，提示先启动后台）。
- **避坑建议**：引入任何外部 CLI 工具（ES.exe、netsh、tasklist…）前，第一步在真机上跑 `-help` 拿到**当前版本**的开关清单，并以真实输入做一次字节级输出 dump（`od -c`）再写解析器；文档/记忆中的旧契约会随版本漂移，实测字节流才是不动的真相。

---

### 13. 编排互斥锁不可重入：嵌套调用自带锁的入口方法会永久死锁

- **问题现象与错误原因**：Everything 模块真机联调报「在没启动的情况下直接搜索就不动了」——前端永远停在「搜索中…」，后端无任何错误。根因：`EverythingService.Search` 进函数即 `controlMu.Lock()`，随后为了"无实例先懒启动"调用了 `StartBackground()`——后者同样第一行 `controlMu.Lock()`。Go 的 `sync.Mutex` 不可重入，同一 goroutine 二次加锁直接永久阻塞，RPC 挂死。这类死锁不 panic、不超时，症状就是"没反应"。
- **排查过程**：单测全部走引擎直调从未经过 service 编排层，因此 CI 全绿；真机复现后按调用链 grep 各入口方法的锁首行，锁定 Search→StartBackground 重入。
- **正确做法与标准修复方案**：凡"编排中枢"模式的 service（frpc/markeron/everything 同构），每个可能被编排方法调用的公开入口一律拆成公开带锁壳 + `xxxLocked` 无锁实现，编排方恒调用 Locked 版；在 Locked 版函数头写注释标注"controlMu 不是可重入锁，嵌套调用会死锁"，警示后来者新增编排路径时沿用约定。
- **避坑建议**：服务层采用单一编排锁时，把「锁 + 私有无锁实现」的拆分作为模板纪律；出现"点一下就再也不动"的症状，第一时间怀疑锁重入而非网络/超时。

---

### 14. Windows 开机自启参数必须与入口解析及窗口状态保持一致

- **问题现象与错误原因**：设置页开启“开机自动启动”后，注册表成功写入 `"Hanxi.exe" --minimized`，但用户登录 Windows 时 Hanxi 没有驻留。根因是注册表启动命令传入了 `--minimized`，而 `cmd/hanxi/main.go` 未注册该 flag；Go `flag.Parse()` 遇到未知参数会打印错误并直接退出。即使只补参数解析，窗口创建若不消费该状态，开机启动仍会弹出主窗口，与“静默常驻后台”的设置说明不符。
- **排查过程**：从设置页 RPC 追踪到 `windows.SetAutoStart` 写入的 Run 键命令，再全局搜索 `--minimized`，发现只有写入点、没有解析点；继续检查 Wails `WebviewWindowOptions`，确认可通过 `Hidden` 控制首次创建时不显示窗口。
- **正确做法与标准修复方案**：入口显式注册 `--minimized`，通过应用启动 `Options` 传入 Composition Root，并将其绑定到 `WebviewWindowOptions.Hidden`；正常手动启动保持默认显示，注册表自启则隐藏主窗口但正常创建托盘。
- **避坑建议**：任何写入注册表、快捷方式或任务计划程序的 CLI 参数，都必须能在程序入口搜索到对应解析与消费点；修改启动命令后至少执行一次“用完整注册命令直接启动”的契约验证，避免出现注册成功但进程秒退的假成功。

---

### 15. GitHub Releases 列表响应体会被资产元数据放大

- **问题现象与错误原因**：开发环境检测查询 Git for Windows 近期稳定版时，请求 `releases?per_page=30`，即使只需要 tag、发布时间和预发布状态，GitHub 仍返回每个 Release 的完整 body 与 assets 元数据；实际响应超过 2 MiB，触发客户端响应体安全上限，前端显示“GitHub Releases 响应超过 2097152 字节限制”。
- **排查过程**：实测同一 API 在 `per_page=10` 时响应约 1.1 MiB、`15` 时约 1.26 MiB、`20` 时约 1.89 MiB，确认失败不是官网不可用，而是请求条数和 GitHub 完整 Release DTO 共同放大了响应体。
- **正确做法与标准修复方案**：只展示 5 个稳定版时将 API 请求缩小到 `per_page=10`，保留 draft/prerelease/非法 tag 过滤；响应硬上限适度提高到 4 MiB，为上游资产元数据增长留余量，同时继续通过 `io.LimitReader` 防止无界读取。用户下载入口使用 Git 官方网站 `https://git-scm.com/download/win`，版本数据仍使用 Git for Windows 官方 GitHub Releases。
- **避坑防重犯建议**：使用 GitHub Releases API 前按真实仓库测量响应体，不要按所需字段估算 JSON 大小；`per_page` 应贴近业务展示数量，并保留合理但有限的响应体余量。

---

### 16. Go 与 Node.js 官网版本通道不能靠字符串或历史规律猜测

- **问题现象与错误原因**：Go 的全历史下载 JSON 会返回约 2.2 MiB 文件元数据和预发布版本，而页面只需要当前受支持版本线；Node.js `index.json` 的 `lts` 又是 `false`/字符串联合类型，历史 EOL 版本仍保留 LTS codename。直接抓 Go 全历史、把 Node `lts:false` 当 Current，或按 major 奇偶性推断 LTS，都会造成响应膨胀或通道误判。另一个隐患是 `go version devel go1.28-...` 会被宽松正则误当正式版 `1.28`。
- **排查过程**：实测 `https://go.dev/dl/?mode=json` 仅约 22 KiB并返回当前两条受支持稳定线，而 `include=all` 约 2.2 MiB；核对 Node Release 工作组 `schedule.json` 后确认 Current/LTS/EOL 必须结合日期判断，不能只看发行索引单个字段。
- **正确做法与标准修复方案**：Go 使用默认 `mode=json`，要求 `stable=true` 且严格匹配 `goX.Y[.Z]`，将最新两条 minor 线归类为 Stable/Oldstable；Go devel 保留 `-devel` 标识并判为不可严格比较。Node 同时读取官方 `index.json` 与 `schedule.json`，严格接受 `vX.Y.Z` 正式版，按系统日期选择仍受支持的最高 LTS 和当前 Current major；`lts` 用自定义 JSON 联合类型解析。
- **避坑防重犯建议**：远程版本接口应选贴近产品需求的最小官方数据源；版本通道必须以官方生命周期数据为准并注入时间测试，不能依赖数组顺序、字符串比较或历史奇偶规律。Windows 上执行 `go test -race` 还需要启用 CGO 并安装可用的 C 编译器（如 MinGW-w64 GCC）；仅设置 `CGO_ENABLED=1` 而 PATH 中没有 `gcc` 会在 `runtime/cgo` 阶段失败，这不代表业务测试失败。

### 17. Java 与 Python 的“最新版”必须先限定发行版和版本线

- **问题现象与错误原因**：Java 本机 `java -version` 可能来自 Temurin、Oracle、Microsoft、Corretto、Zulu 等不同发行版，同一 feature line 的补丁号和 build 不能跨 vendor 直接解释为可升级关系；Java 8 还使用 `1.8.0_402-b06` 旧格式。Python 的最新正式版与本机 `major.minor` 维护线也不是同一类升级，直接拿 `3.12.x` 与 `3.14.x` 比较并显示“可用更新”会误导用户把跨 minor 迁移当作普通补丁升级。
- **排查过程**：核对 Eclipse Adoptium、Python.org 发布记录及 Python Developer Guide 生命周期表，并用 Temurin/Oracle/OpenJ9、Java 8/21、Python bugfix/security/EOL fixture 验证。确认 Java 只有在同 vendor、同 feature line 时才适合比较补丁；Python 只有同 minor line 才适合给出补丁更新关系。
- **正确做法与标准修复方案**：Java 远程源明确标为 Eclipse Temurin JDK HotSpot GA，展示 LTS 与当前 Feature 通道；本机 detector 提取 vendor/runtime/VM，非 Temurin 或不可识别 vendor 的同 feature 补丁关系返回 unknown 并解释原因。Python 严格过滤正式 CPython `X.Y.Z`，最新稳定版与本机受支持 minor line 分开显示；跨 minor 仅提示存在新版本线，同 minor 才计算 latest/update-available。所有生命周期页面解析都应在结构漂移时明确失败，不能猜测。
- **避坑防重犯建议**：任何“运行时最新版”功能先定义发行版、通道和可比较边界；不要把版本数值更大等同于安全原位升级。HTML 生命周期源必须配 fixture 契约测试，远程失败时只显示有证据的数据。

### 18. npm 与 pnpm 升级命令取决于安装来源

- **问题现象与错误原因**：`npm install --global npm@latest` 与 `pnpm self-update` 技术上都能升级，但 npm/pnpm 可能由 nvm、fnm、Volta、Corepack、Scoop、Chocolatey 或其他管理器提供。Hanxi 在未知来源下直接执行全局命令，可能覆盖 shim、写入错误 prefix、绕过项目锁定版本或触发权限问题。
- **排查过程**：对照 npm 官方升级说明和 pnpm 安装/self-update 文档，确认“可查询最新版本”不等于“当前安装可用同一个命令安全升级”；尤其 Corepack 与系统包管理器拥有各自的生命周期和写入位置。
- **正确做法与标准修复方案**：当前 envcheck 只展示并复制参考命令，不提供后端任意命令执行 RPC、不自动运行、不自动提权；同时提示用户优先遵循原安装管理器。未来若增加应用内升级，必须先识别安装来源、Node 兼容范围、目标写入目录和权限，并由后端生成精确版本白名单计划。
- **避坑防重犯建议**：不要把前端确认框当安全边界，也不要向 Wails 暴露 executable/args 或自由命令字符串；未知来源、多 PATH 命中、权限或兼容性不明确时默认拒绝自动修改全局环境。

### 19. .NET 是双编号体系：卡片显示 SDK、版本关系却只能按 runtime 比较

- **问题现象与错误原因**：`.NET` 的官方 `releases-index.json` 中 `latest-release` 是**运行时**编号体系（`9.0.8`），而 `dotnet --version`、多数用户心智与实机 `ToolInfo.Version`（SDK 优先策略）是 **SDK** 编号（`9.0.100`）。两套数字形状同为 `X.Y.Z` 但不可互比：拿本机 SDK `9.0.100` 与通道最新 `9.0.8` 做数值比较会得出"本机领先 92 个补丁"的荒谬 ahead 结论。另外 `dotnet --version` 在纯运行时机器（本机实装即此形态）直接以非零退出报错；`--info` 的节标题与提示语会随系统语言本地化，只有数据行（`Microsoft.NETCore.App 8.0.13 [path]`、`9.0.100 [path]`）语言无关。
- **排查过程**：本机（无 SDK、仅 8.0.13 运行时 + 桌面运行时）实测 `--version` 失败、`--info` 恒退出 0。首版远程实现凭记忆把索引地址写成 `raw.githubusercontent.com/dotnet/release-metadata/main/releases-index.json`，上线即 HTTP 404——`release-metadata` 是微软 CDN 的路径段而非 GitHub 仓库名，正确来源是 `dotnet/core` 仓库 `release-notes/` 或官方镜像 `builds.dotnet.microsoft.com/dotnet/release-metadata/releases-index.json`（后者为国内可达的微软官方 CDN，最终采用）。拉取真实数据后才暴露记忆偏差：字段是 `channel-version` 而非 `release-version`；`support-phase` 实测取值为 `active/maintenance/preview/eol`；预览线（如 11.0）**没有 `eol-date` 且 `latest-release/latest-runtime` 带 `-preview.x` 后缀**——若把"必须是 X.Y.Z 正式版"当全局校验，一条在研预览线就能让整个面板永久报错。`raw.githubusercontent.com` 在本机还被连接重置，GitHub 系源对国内网络本就不可靠。
- **正确做法与标准修复方案**：detector 用 `--info`，只按数据行形状正则提取；`Parse` 返回 SDK 优先版本、`ParseDetails` 按框架族收集**全部并排安装**的版本（去重升序，末位最高——只报最高值会让"装了 10 之后 8 去哪了"看起来像被覆盖）。远程比较一律取 `Details.DotNet.Runtimes` 末位对照官方 `latest-runtime`，runtime 解析不出时关系返回 unknown 并说明原因，绝不回退用 SDK 版本；`latest-sdk` 只进通道说明（"LTS · 活跃支持 · SDK 10.0.400 · 支持至 …"）。`normalize` 中 `preview/eol` 分类整条过滤（预览线不套 GA 校验、可无 eol-date），受支持线中 `eol-date` 已过期同样过滤，未知 support-phase/release-type 报错防漂移。本机线不在受支持集合时展示最新线并给 `RelationDetail` 说明"已超出官方支持范围"。修复后以 `HANXI_OFFICIAL_SMOKE=1` 对真实 CDN 完成端到端冒烟。
- **避坑防重犯建议**：任何"运行时环境"官方版本功能先问版本号数字属于哪个产物体系；远程 provider 的 URL、字段名与枚举值必须以实测拉取的真实数据为准，记忆与文档示例都不可信（本次连字段名都记错）。国内交付优先选微软/官方自有 CDN 而非 GitHub raw；严格校验虽会拦住自己的错误 URL，但也要求预览/过期等合法多态被显式建模。SDK-only/runtime-only/双装三类实机 fixture 都要进测试。

---

### 8. 托管 Tauri 应用：单实例互斥体契约 / GitHub digest 校验 / 关窗退出语义

cc-switch 模块（托管 Tauri 2 应用）的三条硬核实测结论——都是"不看源码就会想当然"的坑，任何托管 Tauri 应用的模块（未来第四个托盘工具）直接沿用。

- **问题现象**：托管一个没有 CLI 的 Tauri 应用时，三个问题没有现成答案：① 外部实例如何探测（不能猜注册表）；② 下载资产如何做官方级完整性校验；③ "退出"按钮拿什么做优雅退出。
- **排查过程**（上游源码 + GitHub API 实测）：
  1. `tauri-plugin-single-instance` 在 Windows 上的互斥体命名是 **`{identifier}-sim`**（无 `Local\` 前缀），窗口类是 `{identifier}-sic`、窗口名是 `{identifier}-siw`。`identifier` 来自 `tauri.conf.json`（cc-switch 为 `com.ccswitch.desktop`）；仅当依赖启用 `semver` feature 时名字才附加版本号（默认关，跨版本恒定）。→ 探测直接用 `OpenMutex(SYNCHRONIZE)`，与 markeron 同构。
  2. GitHub Releases API 的资产对象自带 **`digest: "sha256:<hex>"`** 字段（官方计算）。比 Everything 官网要猜 manifest URL 强一档——sha256 校验成为下载四层完整性的第一主依据。注意 `browser_download_url` 走 github.com→release-assets.githubusercontent.com 重定向，Git Bash + 弱网下 curl 可能 DNS 失败，但 Go 的 http.Client 正常。
  3. Tauri 的"关窗"行为由应用自己在 `on_window_event(CloseRequested)` 里决定（cc-switch 按用户设置 exit(0) 或 hide 驻托盘）→ 外部发 `WM_CLOSE` 只保证"尽力优雅"，**必须带宽限超时 + JobObject Terminate 兜底**；SQLite(WAL) + 原子写配置让进程级强杀是安全的。
- **正确做法**：
  - 探测 = 互斥体（构建期从上游 `Cargo.toml` + `tauri.conf.json` 确认 identifier 与 feature 开关，别猜）；
  - 打开窗口 = 无参二次拉起 exe（单实例插件回调无条件 show+focus 主窗口，信使进程 Start+Release 不 Wait、不进 Job）；
  - 退出 = `FindWindowW(sic, siw)` → `PostMessage(WM_CLOSE)` → 宽限轮询（2s，包级变量供单测压缩）→ JobObject 强杀兜底；`stopping` 标记必须在发消息前置位，防 wait() 误判异常退出；
  - 便携 zip = 单 exe + `portable.ini` 标记（仅用于禁用内置 Updater，**数据目录仍是 `~/.cc-switch`，不随 exe 走**）——导入本地只需搬 exe，配置天然跨版本共享。
- **避坑建议**：托管任何"自动更新"类上游工具前，先确认官方绿色版的 Updater 开关机制（此处 portable.ini）；`FindWindowW` 的类名/窗口名必须与插件源码一致（错一个字符静默 no-op，表现为"退出按钮毫无反应"）。

---

### 9. 托管 .NET WinForms 应用：tag/资产版本不同形 + Global 互斥体 + EnumWindows 退出

BCU（Bulk Crap Uninstaller）模块的三条上游契约坑——"看资产名想当然"都会翻车：

- **问题现象**：① GitHub tag 只有两段（`v6.2`）而真实版本在资产名里（`BCUninstaller_6.2.0_portable.zip` / `6.1.0.1` 四段），按 tag 做版本号会得到 6.2 与 6.2.0 并存的双重身份；② 单实例互斥体是 `Global\BCU-singleinstance`（带 `Global\` 前缀）；③ WinForms 窗口类名不可预测（进程内注册随机类），无法像 tauri 那样 FindWindowW 固定类名。
- **排查过程**：源码实证——`EntryPoint.cs` 的 `MUTEX_NAME` 常量与 `HandleBeingSecondInstance`（第二实例枚举进程 → `SetForegroundWindow(MainWindowHandle)` 唤窗）；`Directory.Build.props` 证实 `net8.0-windows` 目标框架，76MB portable 资产是 self-contained（运行时内置）。
- **正确做法与标准修复方案**：
  - 版本号以资产名为准（正则 `BCUninstaller_(\d+(\.\d+)+)_portable(?:-x64)?\.zip`），tag 只做按段前缀一致性校验（`v6.2` vs `6.2.0` → tag 段是资产版本的前缀），防未来串版；
  - probe 用 `OpenMutex(SYNCHRONIZE, "Global\BCU-singleinstance")`（Global 前缀要原样拼进字符串）；唤窗直接无参拉起 exe 作信使（比 CC Switch 更简单，不用窗口类名）；
  - 退出与窗口探测走 `EnumWindows` + `GetWindowThreadProcessId` 按 PID 过滤（回调里 `syscall.NewCallback` 同步枚举，命中即 PostMessage WM_CLOSE 后停止）；空闲退出的"窗口豁免"信号 = 该 PID 存在 `IsWindowVisible` 顶层窗口——最小化到任务栏不算豁免。
- **避坑建议**：便携数据若与 exe 同目录（BCU 的 `BCUninstaller.settings`），ImportLocal 用**黑名单整搬**（只排 tmp/wal/desktop.ini），别用 everything 的白名单模式套；自包含 .NET 应用冷启动慢于 tauri，WaitReady 超时相应放宽（25s）。

---

### 10. 应用退出钩子未接线：托管的工具进程不随 Hanxi 退出而关闭

- **问题现象**：从托盘退出 Hanxi（或关闭窗口退出）后，FlClash / BCU 等模块托管的工具进程仍存活——用户"正在上网（代理）"或"正在卸载"时关闭 Hanxi，工具的窗口与进程独立残留，体验突兀（"不随着 hanxi 结束而关闭"）。
- **排查过程**：逐步核对了三条理论防线——①各模块 Engine 的 JobObject KILL_ON_JOB_CLOSE 兜底（创建/Assign 正常）；②Shutdown 链：`Registry.ShutdownAll()` 早已实现（遍历 initialized 模块调 OnDestroy→engine.Stop→job.Terminate）；③**致命发现：`ShutdownAll` 从未被任何地方调用**——`OnDestroy` 只在 registry.SetEnabled(false)（用户停用插件）时触发，app.go 的 `Options` 里既没有 OnShutdown 钩子也没有退出前清理。
- **正确做法与标准修复方案**：在 `application.New` 的 `Options.OnShutdown`（wails 阻塞式退出钩子，优雅退出路径都会走到）里调 `registry.ShutdownAll()`——所有已初始化模块先 OnDestroy（JobObject Terminate 连根带走工具进程树），主进程才结束。强杀/崩溃场景仍由 JobObject 句柄关闭时的内核 KILL_ON_JOB_CLOSE 兜底。
- **避坑建议**：凡新增"模块退出清理"接口，必须当场核实它的调用点是否在应用退出路径上被接线（grep 调用计数）；只剩内核兜底的架构在优雅退出路径上会漏掉一切需要握手收尾的资源。已知边界：被托管工具 UAC 提权派生的子进程（如卸载器的提权清理器）会 breakaway 出 JobObject，内核级兜底也杀不掉——此类工具退出前尽量在其窗口内先完成操作。

---
### 11. 上游发行侦查：只查 GitHub Releases 会漏掉自托管官方渠道（EarTrumpet）

- **问题现象**：评估集成 EarTrumpet 时，GitHub Releases API 实证"最新 release 停在 1.3.2.0 且所有资产为空"，据此得出"无官方离线包、只能商店跳转、不能做安装"的结论。用户追问后顺 winget 社区仓库摸到上游其实有**活跃的自托管直装渠道**：`https://install.eartrumpet.app/<branch>/EarTrumpet.Package.appinstaller`（在线版本号 2.3.0.20，与商店同步构建，CI 三渠道 AppInstaller/Store/Chocolatey 都在 master main.yml 里）。
- **排查过程**：`winget search eartrumpet` 命中社区源 `File-New-Project.EarTrumpet 2.3.0.0` → 读 `winget-pkgs` 的 `manifests/f/.../installer.yaml`：`InstallerType: appx`、`InstallerUrl: https://install.eartrumpet.app/...appxbundle`、`InstallerSha256`、`PackageFamilyName: ..._725pr5jq8wr8a`（**与商店版 PFN 后缀不同**）→ curl 线上 appinstaller XML 核实 → 上游 CI 确认 bundle 用 Azure Code Signing 签名。
- **正确做法与标准修复方案**：
  - 侦查发行渠道的完整清单必须是：GitHub Releases **和** winget-pkgs 仓库（`manifests/<首字母>/<Publisher>/<Name>/` 目录）+ Chocolatey 社区包 + 官网域名的 appinstaller/下载子域——winget 清单相当于社区审查过的"官方源索引"，是发现隐藏渠道与逐版本 SHA-256 交叉校验源的最佳线索；
  - 多包身份渠道：检测/启动/卸载全部按渠道身份（PFN+Publisher）各查一遍，并存时警告（两渠道包共享 `Local\{程序集名}-{GUID}` 互斥体——**不同包身份 ≠ 能并行运行**，且配置各存各的 LocalSettings）；
  - appinstaller 清单必须钉死校验：MainBundle 的 Name、Publisher 与预期常量全等，Bundle URL 主机白名单（防域名接管/清单篡改指向他人包），依赖仅收 https。
- **避坑建议**：
  1. Azure Code Signing 签的 MSIX 使用约 3 天短时效证书 + RFC3161 时间戳：`Get-AuthenticodeSignature` 报 Valid，但用当前时钟 `X509Chain.Build` 会报 NotTimeValid（链里叶子已过期），老版 PowerShell 甚至不显示 TimeSigned——判断可信性别只看这两处，以 `Add-AppxPackage` 真机部署为准。本仓库实机（2026-09-03）已成功部署过期证书的 2.3.0.20，证明部署栈接受时间戳组合；`0x800B0101 CERT_E_EXPIRED` 也已归入签名类错误映射。
  2. **内嵌 PowerShell 协议脚本必须在改动后用临时文件冒烟执行三条路径（成功/已知失败/未知操作）**：apppackage 脚本的错误映射函数里 `[uint32]$exception.HResult` 对负数（.NET HResult 全是有符号 int32，如 -2146233079）在 Windows PowerShell 5.1 直接溢出抛异常，导致**所有错误响应从未成功送达过 Go 侧**（表现为"协议响应无效"而非友好分类错误），单测用 fake executor 完全测不到这层。改用 `$exception.HResult.ToString('X8')` 修复；十六进制字面量 `-band 0xFFFFFFFF` 因 PS 数字字面量类型提升规则也不可靠，别用。
  3. 每次包操作 = 一次 PowerShell 冷启动（实测 ~1.4s 起，完整查询往返 ~1.8s）：能合并进同一脚本分支的校验绝不要拆成多次 RPC（activate 分支本就查包，Go 侧不要再前置 Query）；多目标查询必须并发（GetStatus 双渠道从串行 3.6s 降到单次延迟）；KeepAlive 页面 onMounted 后必跟一次 onActivated，双刷新要去重。

---

### 12. 托管 Electron 应用（Recordly）：NSIS 在线安装器五连坑 + 进程名探测契约

- **问题现象与错误原因**：Recordly 上游 Windows 仅有 electron-builder NSIS 在线安装器（无便携 zip），集成托管连续踩五类坑：
  1. **`/D=<目录>` 传参坑**：NSIS 规定 `/D=` 必须是命令行最后一个参数且路径**不带引号**；Go 的 `exec.Command` 给含空格路径自动整体加引号，NSIS 取 `/D=` 之后整段时把尾引号算进目录名，安装落到错误目录；
  2. **oneClick 静默卸载旧安装**：electron-builder NSIS 默认 `oneClick: true`，安装器启动时按 HKCU 卸载注册表 `InstallLocation` **静默卸载上一个安装**——多版本共存目录形同虚设（每装一版抹掉注册表指向的那版），若注册表指向用户自装副本还会被托管动作连带删掉；
  3. **PE 版本抹平预发布后缀**：`v1.3.5-beta.2` 的 `Recordly.exe` FileVersion 读出 `1.3.5`（electron-builder 把 prerelease 抹平进 Windows 数字版本），按 PE 版本识别 beta 安装必错；
  4. **空壳 tag**：上游 `v1.3.4` 有 git tag 但 `releases/tags/v1.3.4` 返回 404（打了 tag 从未发布），另有 v1.2.0 缺安装器只剩 blockmap、v1.3.5-beta.2 刻意不发 `latest.yml`——按 tag 给前端列版本会展示下载不到的版本。
  5. **退出码直译误导 + 超时死代码**（真机复发）：安装失败控制台显示"退出码 3221225477（安装中止？请确认没有正在运行的 Recordly 实例）"——3221225477 = **0xC0000005 访问违例**，是安装器进程崩溃（Recordly 未签名安装器被杀软注入扫描干扰的典型死法），与"文件占用中止"毫不相干，兜底文案把用户引向完全错误的排查方向。连带发现：15 分钟超时的 ctx 从未接入 `exec.Command`（裸 Command 不 watch ctx，事后 `ctx.Err()` 判定永假），装死在隐藏对话框的安装器会永久吊死下载 goroutine，超时文案是不可达死代码。
- **排查过程**：`electron-builder.json5` 实证 win target 只有 nsis 且未覆写 oneClick（默认 true）、`artifactName: ${productName}-windows-${arch}.${ext}`；releases API 全量 46 版逐一核对资产名与 digest；GitHub API 交叉验证 tag vs release 存在性；上游 `main.ts`/`updater.ts` 源码实证 `requestSingleInstanceLock` 与 `RECORDLY_DISABLE_AUTO_UPDATES` 官方开关。
- **正确做法与标准修复方案**：
  - 静默安装显式接管原始命令行绕开 Go 引号化：`cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"C:\installer.exe" /S /D=C:\path with space\target`}`（exe 段带引号、`/D=` 段不带）；
  - 托管目录收敛为**单目录** `versions/recordly`（放弃 recordly_X.Y.Z 多版本布局），"切换版本=覆盖安装"；安装前扫描 HKCU Uninstall 子键做**外部安装卫兵**（`DisplayName=Recordly` 且 `InstallLocation` 真实存在且不在托管目录 → 拒绝安装并指引用户），绝不让 oneClick 抹掉用户自装副本；卸载只 `RemoveAll` 自家目录，**绝不调用 Uninstall.exe**（注册表指向哪套安装不可靠）；
  - beta 身份以安装时写入 `hanxi-meta.json` 的远程 tag 为准，PE FileVersion 只做兜底；版本比较必须 semver 预发布感知（`1.3.4-beta.1 < 1.3.4 < 1.3.5-beta.2`，数值核心互认函数供 beta/PE 版本互查）；
  - 远程列表以 `/releases?per_page=60` 为唯一数据源，tag 正则放宽到 `vX.Y.Z(-pre.N)` 后按 `prerelease || tag带后缀` 双依据标 IsPre，stable/beta 双通道（默认 stable），缺 digest/缺安装器资产的残缺发布一律不入列；校验链 = GitHub digest sha256（主）+ 官方 `SHA256SUMS.txt` 交叉比对（第二只眼，网络失败降级放行、两源矛盾硬失败）；
  - Electron 探测无稳定互斥体名可依赖（Chromium 进程单例对象名不可预期），一律**进程名 `Recordly.exe` + EnumWindows 按进程名过滤**（窗口要求可见且带标题）；`wait()` 判外部接管前先等 ~500ms 让 Chromium 子进程树收敛——进程名探测是树级信号，自有主进程刚退时 helper 残影会被误判成外部实例（ccswitch/bcu 的互斥体方案没有这个问题，这是移植托管模板到 Electron 时的结构性差异）；
  - 启动注入 `RECORDLY_DISABLE_AUTO_UPDATES=1`（上游官方 env 开关），否则 electron-updater 的 `quitAndInstall` 会按注册表覆写托管目录；WM_CLOSE 必须按进程名过滤，**绝不能** `FindWindow("Chrome_WidgetWin_1")`——那是全体 Chromium 应用共享的窗口类，会误伤用户浏览器；
  - 退出码经 `decodeInstallerExit()` 分族翻译（与 rustdesk/subnetdesk 的 `installExitError` 同思想）：Win32 小码给语义指引（1223=UAC 取消、5=拒绝访问）、`0xC0000005` 明示"崩溃非文件占用→查杀软注入、覆盖重装即可"、其余 ≥`0xC0000000` 一律按 NTSTATUS 报十六进制；超时改 `exec.CommandContext` 真接线，且 `ctx.Err()` 判定必须排在 `ExitError` 判定之前（被 ctx 杀死后 Run 同样返回 ExitError，顺序颠倒会把超时误报成异常退出码）。
- **避坑建议**：
  1. 托管 NSIS 类上游前先读 `electron-builder.json5` 的 win/nsis 配置判定 oneClick/perMachine/shortcut 默认值，再决定目录布局；"GitHub 有 zip 资产"≠ Windows 便携版（electron-builder 的 `Recordly-x64.zip` 是 macOS 自动更新产物）；
  2. NSIS 安装器会顺手创建桌面/开始菜单快捷方式（指向托管目录、可绕过 Hanxi 启停），安装成功后立即清理，"桌面快捷方式"作为 Hanxi 侧显式功能提供；上游若存在真托盘语义则另议；
  3. 未签名安装器的信任链完全押在双源 sha256 上：`SHA256SUMS.txt` 这类官方旁证资产要利用起来，格式解析容忍 GNU（`<hash>  <name>`）与 BSD（`<hash> *<name>`）两式；
  4. 安装器体积 ~214MB、装后 ~700MB，远大于既有 tauri 模块：下载超时给到分钟级，多版本目录设计前先量磁盘成本；
  5. Electron 冷启动到主窗口就绪实测 2~10s，`readyTimeout` 给 30s，勿照抄 tauri 的 20s；
  6. **外部安装器退出码先分族再写文案**：无符号大数（≥0xC0000000）是 NTSTATUS 异常=崩溃，NSIS/MSI 业务返回都是小码——把两类混进一句"安装中止？"必然误导；`context.WithTimeout` 必须配 `exec.CommandContext` 才算生效，裸 Command + 事后 `ctx.Err()` 是装饰。

### 13. 托管 WPF 绿色单文件应用（PaperTodo）：GitHub digest 并非全量存在 + 自建单实例命令契约

- **问题现象与错误原因**：按 ccswitch 模板（"缺官方 digest 的 release 不入列表"）集成 PaperTodo 会让版本表**整表为空**——实证 `api.github.com/repos/snownico0722/PaperTodo/releases` 全部资产的 JSON 中根本没有 `digest` 字段；GitHub 的资产 sha256 digest 是 2024 年起**逐批回刷**的可选字段，并非"所有仓库所有资产全量存在"，"实测全量覆盖"的先例结论不能外推。次要坑三枚：上游 tag 为 `v3.31/v3.3/v3.2.1` 混用 2~3 段，字典序会判 `v3.3 > v3.31`；历史上有 `v2.1rc1` 这类 rc tag（带 `prerelease:true`）；win7 变体资产 `…-win7BestEffort-win-x64-self-contained.exe` 与标准完整版同后缀，用"后缀匹配"筛选会误纳。
- **排查过程**：curl 直连 GitHub API 核对最新与近 30 个 release 的资产 JSON（确认 digest 键缺失、未收录 winget、release body 无哈希）；读上游源码 `src/App.xaml.cs`、`src/SingleInstanceHelper.cs`、`src/StartupCommand.cs` 实证单实例契约为 WPF 自建协议——**裸名互斥体** `PaperTodo-SingleInstance-Mutex`（.NET `new Mutex(name)` 落 Local 会话命名空间，非 tauri 的 `{identifier}-sim` 惯例）+ 命名管道转发**命令行参数**，命令词表 `show|open / hide / toggle / new-todo / new-note / exit|quit`，空参默认 show。数据形态读 README：绿色单文件、`data.json` 等便签数据恒在 exe 同目录。
- **正确做法与标准修复方案**：
  - 完整性降级链（不照搬 digest 硬过滤）：下载 URL 恒由 repo+tag+**全名精确匹配**的资产名拼接（`^PaperTodo-v{版本}-win-x64-{self-contained|no-runtime}\.exe$`，锚定整名排除 win7）→ 字节数 == API `size` → MZ 魔数 → `versioninfo.FileVersion` 与目标版本数值核对（可读取而不符即拒，资源缺失降级放行并如实入档）→ 落盘后计算 sha256 写入 `hanxi-meta.json` 作下载指纹；解析层保留 digest 字段识别，上游哪天补上即自动升级为官方哈希硬校验。信任根如实承认：github.com TLS 与镜像运营方。
  - tag 正则 `^v\d+(\.\d+){1,2}$` + `prerelease` 标志**双重**过滤；版本比较必须数值分段（service 层 versionCompare 同款）。
  - 单目录覆盖布局（用户拍板）：便签数据与 exe 同目录，多版本目录=每次切版本都要迁移创作内容，风险远大于收益；固定 `versions/papertodo/`，升级=旧 exe 备份 `.bak` → 同目录临时文件原子 rename 换入 → 失败回滚；卸载只删 exe/meta，**数据原地保留**；ImportLocal 必须整套目录随行（与 ccswitch"只搬 exe"不同——数据就在 exe 旁边，只搬 exe 等于丢便签）。
  - 生命周期全走官方命令信使：探测 `OpenMutex(SYNCHRONIZE, "PaperTodo-SingleInstance-Mutex")`；唤窗/收拢/退出 = 二次拉起携带 `show/hide/exit`（信使进程 Start+Release、不 Wait、不进 Job）；`Quit` 先 exit 信使 + 宽限轮询、超时才 JobObject 强杀——比 tauri 模板的 WM_CLOSE 方案更干净，且天然覆盖"优雅退出保存数据"。
  - 不做空闲自动退出：桌面便签是常驻环境型工具，"3 分钟无人点就收走纸片"与产品语义相反（对照 ccswitch 的 idle 豁免逻辑，这里是整体不接线）。
- **避坑防重犯建议**：把"上游给官方哈希"当模板常量前，先对目标仓库的 releases JSON 实证 `digest` 键存在性——按模块写死校验策略，别写进共享假设；WPF/.NET 应用的单实例实现五花八门（.NET 自建 Mutex+Pipe、tauri-plugin、Electron lock），互斥体名与唤窗协议必须读源码取实证，不可套 tauri `-sim/-sic` 命名规律猜；绿色应用"数据随 exe"这一条直接决定目录布局、卸载语义与导入语义，侦查清单第 5 项（数据落盘）在单文件应用上要升级为"数据文件清单+归属"逐问。

### 14. 托管安装器-only 的 Tauri 应用（PicLite）：MSI 管理提取路线 + `-siw` 恒可见探针陷阱 + 退出通道归零

PicLite 模块集成实证——上游 46 个版本全部只发安装器（无便携 zip），把托管模板按 zip 思路硬套会连安装都立不起来；连带挖出一条影响既有模块的知识错误：

- **问题现象与错误原因**：
  1. 上游 Windows 资产只有 NSIS `x64-setup.exe`（`tauri.conf.json` 配 `installMode: perMachine`：要 UAC 提权、写卸载注册表、建公共快捷方式，与托管隔离原则全面冲突）和 WiX `x64_en-US.msi`——没有先例走过的第三条路；
  2. **`-siw` 消息窗口恒可见陷阱**：`tauri-plugin-single-instance` 源码实证其事件目标窗口创建后立即 `SetWindowLongPtrW(GWL_STYLE, WS_VISIBLE | WS_POPUP)`（注释：必须可见才收得到 WM_PAINT 事件泵），靠 `WS_EX_LAYERED|TOOLWINDOW` 对用户隐形——**`FindWindowW(-sic,-siw)+IsWindowVisible` 恒为 true**，完全不反映主窗口显隐。ccswitch 模板的 `IsMainWindowOpen` 用的正是这个组合（注释还断言"关窗驻托盘时 FindWindowW 仍可命中但不可见"，与插件源码矛盾）→ 依赖它做"空闲自动退出主窗口豁免"的路径在 ccswitch/mangodisk/flclash 同构模块上疑似恒豁免、空闲退出形同虚设（未逐个真机复验，见避坑建议 3）；
  3. **向 `-siw` 投 WM_CLOSE 是净损害**：插件窗口过程对该消息落 `DefWindowProcW` → DestroyWindow——既不会让应用退出（它不是 tao 窗口、走不到 `CloseRequested`），又拆掉单实例协议的 WM_COPYDATA 载体，此后所有"信使唤窗"失联；
  4. **退出通道为零**：上游主窗口 `CloseRequested` 无条件 `prevent_close+hide`（关窗驻托盘），连 `RunEvent::ExitRequested` 也在未置 `quitting` 标志时 `prevent_exit`——WM_CLOSE、`app.exit` 类外部信号全部无效，也没有 `-quit` CLI 或管道命令词表（papertodo 路不存在）。
- **排查过程**：全量 46 版 releases 扫资产形态（零 zip）；`tauri.conf.json`（identifier/installMode/窗口定义）+ `Cargo.toml`（single-instance 无 semver feature）+ `lib.rs` 关窗/退出/托盘路径逐段实证；`msiexec /a` 管理提取真机走通（`PFiles\PicLite\piclite.exe` 直接可跑、`--minimized` 无窗存活、一次性 Go 探针实锤互斥体 `com.piclite.desktop-sim`）；对照插件源码推翻模板注释的可见性断言。
- **正确做法与标准修复方案**：
  - **MSI 管理提取（administrative install）**：`msiexec /a <msi> /qn TARGETDIR=<stage>` 只按 Directory 表展开载荷——免管理员、不写注册表、不建快捷方式；管理映像对 ProgramFiles 用固定字面 `PFiles`（不随系统语言变），载荷布局 `<stage>\PFiles\PicLite\piclite.exe`；实现不硬编码层级——递归收割 `piclite.exe` 所在目录平铺进 `versions/piclite_X.Y.Z/`，映像根部自动复制进来的源 msi 副本一并丢弃；msiexec.exe 是 Installer 客户端、`/qn` 下同步到装完才返回（`CombinedOutput` 即可等待），另加 10s 轮询等落盘竞态兜底；完整性四层改为 官方 digest sha256 + 字节数 + Installer cabinet 内建校验 + 布局自检；
  - **窗口在用信号按 PID 枚举**：`EnumWindows + GetWindowThreadProcessId` 过滤自有 PID 的"可见 + 非 TOOLWINDOW + 尺寸≥80px"顶层窗口（悬浮结果/拖放区任一可见都算在用，豁免语义比"仅主窗口"更保守正确）；引擎仅在 running 态持 PID 时探测；
  - **Quit 直接 JobObject 强杀**：不投任何窗口消息；数据安全性依据=上游配置前端修改即写盘（`app-profile.json`），进程级终止不丢设置，代价仅为进行中批量压缩中断（前端按钮 title 如实告知）；
  - 资产名精确锚定 `PicLite_{ver}_x64_en-US.msi`，天然排除 arm64 msi / setup.exe / dmg / deb / AppImage；`-setup.exe` 与 perMachine NSIS 一律不碰。
- **避坑防重犯建议**：
  1. 集成侦查清单第 1 项"便携资产"若失败，先验证 `msiexec /a` 再考虑 NSIS 静默安装（Recordly 路线）：Tauri WiX MSI 大多可管理提取出单 exe；NSIS perMachine 则直接出局（提权+注册表副作用）；
  2. 插件类上游契约（互斥体/窗口/消息）**以插件当前源码为准，不以模板注释为准**——同一插件不同版本行为会变，模板结论迁移前重新实证；
  3. 遗留核查项：ccswitch/mangodisk/flclash 的 `IsMainWindowOpen`（`-siw` + IsWindowVisible）大概率恒 true，其"空闲自动退出"未生效——真机复验方法：托管启动后关闭主窗口驻托盘，3 分钟内不再操作，观察是否自动退出；若确认失效，按 PicLite 的 PID 枚举探针逐个修正（属独立修复提交，勿混入功能集成）；
  4. 对 `-sic/-siw` 消息窗口的 `WM_CLOSE` 从任何模块移除在案：piclite 不接线，其余模块它只是空耗 2s 宽限后仍走强杀；
  5. 上游发版极快（PicLite 三天 1.1.8→1.4.1）：远程列表 `per_page=60` 起步并保留 10 分钟缓存，前端表格无需担忧分页。

### 15. 托管 Keyviz：single-instance 空回调=唤窗契约不存在 + MSI 资产名定制 + 常驻可视化工具无空闲语义

Keyviz 模块集成实证——套用 ccswitch/piclite 模板前必须逐项重新上游实证，本次在"看起来同构"的 tauri v2 应用上撞出三个模板假设全部失真的点：

- **问题现象与错误原因**：
  1. **单实例插件存在 ≠ 唤窗契约存在**：keyviz 挂了 `tauri-plugin-single-instance`，但回调是空函数 `.plugin(tauri_plugin_single_instance::init(|_, __, ___| {}))`——二次无参拉起只让信使进程发完 WM_COPYDATA 后 `exit(0)`，第一实例无任何 show+focus 动作。照抄模板的 `OpenWindow` 信使路径会得到"按钮按下什么也没发生"的假功能；设置窗口唯一入口是托盘菜单（左键即弹），程序化唤起不存在；
  2. **MSI 资产命名不跟 tauri 默认**：keyviz 发布 workflow 定制了资产名 `keyviz_2.1.1_windows.msi`——piclite 沉淀的后缀 `_x64_en-US.msi` 匹配模式直接套会一个都匹不到；且 v2 正式版线零便携 zip（唯一 windows.zip 停留在 v2.0.0a3 预发布），zip 路线无从谈起；
  3. **退出通道为零 + 空闲语义不成立**：退出仅存在于托盘回调 `process::exit(0)`；overlay 主窗口 `visible:false focusable:false`，WM_CLOSE 无可送达目标，`-siw` 消息窗口按 #14 结论不可投（投了净损害）；而"空闲自动退出"在按键可视化这类常驻环境型工具上语义荒谬（运行即在用）——papertodo 已有"整体不接线"先例。
- **排查过程**：releases 全量扫描（正式版仅 msi/dmg，alpha 才有 zip）→ 真机下载 v2.1.1 msi 核对官方 digest 一致 → `msiexec /a /qn TARGETDIR=` 实测布局 `PFiles\keyviz\keyviz.exe`（单 exe，FileVersion=2.1.1）→ 拉起实测互斥体 `org.keyviz-sim` OpenMutex 命中、`%APPDATA%\org.keyviz\store.json` 落盘、`Stop-Process -Force` 干净终止 → 读 `lib.rs`/`tauri.conf.json`/插件 `windows.rs` 源码实证回调为空、identifier 无 semver、退出/唤窗路径归零。
- **正确做法与标准修复方案**：
  - 前端控制台不提供"打开窗口/设置"按钮，banner/hint 如实指引"左键系统托盘图标 → Settings"；引擎删除 OpenWindow/信使路径与 close 信使文件，API 面收敛为 StartKeyviz/Quit/GetStatus；
  - 资产匹配按上游实证形状精确锚定 `keyviz_{ver}_windows.msi` + 后缀兜底 `_windows.msi`（天然排除 macos.dmg/linux deb/rpm）；MSI 管理提取/四层完整性/递归收割整体复用 piclite 已验证机制，仅换常量与文案；
  - Quit 直接 JobObject 强杀：store.json autoSave 1s 修改即写盘，强杀不丢历史设置；无 idle watcher、无 IsMainWindowOpen 探针，probe 接口收敛为 IsRunning/WaitForReady 两方法；
  - 决策记录写入 module package 注释（纯托管、无唤窗、强杀退出、GPL-3.0 仅启动不链接）。
- **避坑防重犯建议**：
  1. 侦查清单第 3 项升级为"**读 single-instance 回调源码**判断是否无条件 show+focus"——插件挂载只代表第二实例会自退，唤窗能力取决于回调内容，空回调即契约不存在；
  2. MSI/zip 资产名模式**逐仓库实测**，不把 piclite/ccswitch 的后缀当作"Tauri 应用通用形状"（tauri 默认命名可被 workflow 任意改名）；
  3. 常驻环境型工具（可视化/音量管理类）默认不做空闲自动退出，除非上游存在明确的"闲置驻留形态"（如关窗藏托盘）；
  4. 用户已拍板：上游契约缺失导致的体验退化（无唤窗/强杀退出）在托管模式如实呈现，不 fork 上游补齐（fork 维护成本已明确报价并被否决）。

### 16. 托管 QuickLook：低级键盘钩子非注入=强杀零残渣 + 命名管道 Quit/Reload 优雅退出通道 + 便携 zip 反斜杠布局

QuickLook（空格预览，托盘 Manager + 全局键盘钩子）集成实证——初判与结论的两次反转，价值在于纠正"系统级热键工具不可托管"的草率假设：

- **问题现象与错误原因**：
  1. **把"系统级键盘监听"误判为"系统注入/持久化"**：初查见 `QuickLook.Native32/`（含 Shell32/Everything/DOpus/WoW64HookHelper）与 `QuickLook.Installer/Product.wxs`（WiX MSI），据此断定它是"必须注册进 explorer.exe 的 shell 扩展、强杀留钩子残渣、与 JobObject 沙箱根本冲突"，一度建议**不集成**。实为误判——空格捕获走 `App.xaml.cs→KeystrokeDispatcher` 的 `GlobalKeyboardHook`，其实现是 `SetWindowsHookEx(WH_KEYBOARD_LL,...)`，配合 `SetWinEventHook(...WINEVENT_OUTOFCONTEXT)`；两者都是**进程内钩子，回调跑在 Manager 自身进程，不向任何目标进程注入代码**，原生 helper DLL 也只是被 Manager `LoadLibrary` 加载来查询焦点窗口选中项。WH_KEYBOARD_LL 钩子随持有它的进程终止由内核自动摘除——**强杀同样零残渣**；
  2. **误以为退出通道为零（套 keyviz #15 先例）**：QuickLook 实际有命名管道服务端 `PipeServerManager`，管道名 `QuickLook.App.Pipe.<当前用户SID>`（.NET `WindowsIdentity.GetCurrent().User.Value`），行协议 `消息|路径|参数`，支持 `Quit`/`Reload`/`Toggle` 等——优雅退出通道现成，比 WM_CLOSE 可靠（其主/托盘窗口常态隐形，WM_CLOSE 无可送达目标）；
  3. **五资产择一 + zip 反斜杠布局**：每个正式版并列 `.7z/.appx/.exe/.msi/.zip`，唯有 `.zip` 是免安装便携包（`.exe/.msi/.appx` 写系统、`.7z` 标准库解不了）。实测 `QuickLook-4.5.0.zip` 顶层即 `QuickLook.exe + portable.lock + QuickLook.Native{32,64}.dll` 外加 `QuickLook.Plugin\` 子树；且 **zip 条目名用反斜杠 `\` 分隔**（非标准惯例的 `/`），直接 `filepath.Join` 在 Windows 下易生歧义。
- **排查过程**：`curl` 直连 GitHub API 核对 4.5.0 zip 官方 digest（真机下载 sha256 一致 `852d8bcc…`）；`unzip -l` 实证便携布局与反斜杠；读 `App.xaml.cs`（`EnsureFirstInstance` 裸名互斥体 `QuickLook.App.Mutex`、`OnStartup/OnExit`、`IsPortable=SettingHelper.IsPortableVersion()`）、`GlobalKeyboardHook.cs`（WH_KEYBOARD_LL）、`KeystrokeDispatcher.cs`（OUTOFCONTEXT）、`PipeServerManager.cs`（管道名与消息表）、`SettingHelper.cs`（`portable.lock` 判据、LocalDataPath）逐一坐实。
- **正确做法与标准修复方案**：
  - 版本锁 `.zip`：`findPortableAsset` 精确匹配 `QuickLook-<ver>.zip`（小写比对天然排除 7z/appx/exe/msi）；官方 digest 缺失的老版本（≤4.0.2）不入列表；tag 无 `v` 前缀，`plainSemverTag=^\d+\.\d+\.\d+$` 同时挡掉 `latest` 滚动预发布与 `0.3.6.1` 四段；
  - 解压：`extractAll` 先 `strings.ReplaceAll(f.Name,"\\","/")` 归一再 `filepath.FromSlash/Clean`，落地嵌套目录；布局自检硬要求 `QuickLook.exe`+`portable.lock`+`QuickLook.Native64.dll`，并防御性 `ensureFile(portable.lock)` 保证配置随 exe；ImportLocal **整套目录递归迁移**（配置随便携标记落此目录，与 ccswitch 单 exe 白名单相反）；
  - 退出：`Quit` = 先向管道投 `Quit`（`OpenProcessToken→GetTokenUser().User.Sid.String()` 拼名，`os.OpenFile` 当文件写，goroutine+dialTimeout 防假死卡住）→ `closeGracePeriod` 内轮询引擎状态自然翻停即返回 → 否则 JobObject `Terminate` 强杀兜底；投递入口抽成包级变量 `sendSignal` 供单测桩，避免测试误伤真机上运行的 QuickLook；额外提供 `Reload`（管道 `Reload`）增值控制；
  - 生命周期：`followOnExit` 开关（同 keyviz，默认随 Hanxi 退出，关闭则 Detached 独立常驻，贴合其开机常驻本性）；前端不设"打开窗口"假按钮，指引托盘左键开设置（同 keyviz）。
- **避坑防重犯建议**：
  1. **"按热键/全局监听"≠"注入/持久化"**：托管前先看钩子类型——`WH_KEYBOARD_LL`/`SetWinEventHook(OUTOFCONTEXT)` 是进程内、随进程清理的可托管；只有 `WH_GETMESSAGE`/`WH_CBT` 或注册表 shell 扩展/`AppInit_DLLs` 才是注入型持久化。别只看仓库里有 `.cpp` 原生工程和 MSI 安装器就判死刑；
  2. **退出通道别默认"归零"**：keyviz 的"无优雅通道"教训不可外推。先 grep 上游有无命名管道/本地 socket/`-quit` CLI——QuickLook 这类 .NET 托盘应用常用命名管道做单实例 IPC，同一管道往往自带 `Quit`；
  3. **多资产发布务必真机 `unzip -l` 定布局**：五资产同名不同扩展极易选错；反斜杠 zip 条目（Windows 端打包工具产出常见）务必归一分隔符，否则嵌套项会塌成含 `\` 的畸形文件名；
  4. **可注入信号 + 可压缩宽限期**：引擎任何"对外发控制消息"的路径都应抽成可替换变量并让单测桩化，否则自动化测试可能对用户真实实例下命令（本模块 `sendSignal`）。

### 17. 托管 LiteMonitor：路径派生互斥体名不可复现 + requireAdministrator 的 740 直拒与 UIPI 边界 + 首启 settings.json 种子关更

LiteMonitor（C# WinForms 桌面/任务栏硬件监控）集成实证——"GitHub releases + 官方 digest"看似 ccswitch 同构，实际单实例/权限/配置三个契约全部变形，逐项记录：

- **问题现象与错误原因**：
  1. **命名互斥体 ≠ 可探测互斥体**：上游 `Program.cs` 的单实例锁名是 **安装路径派生** 的（`Global\LiteMonitor_SingleInstance_{exe 目录小写、\ : / 空格→_}_Mutex`，超长退哈希、异常退固定回退名）。ccswitch 模板的 `OpenMutex` 探测套路对**外部实例完全失效**——用户自行解压的安装路径不可预知，名称无法构造；第二实例抢锁失败**静默 `return`，无任何 show+focus 回调**，"无参二次拉起"信使语义也不存在（照抄模板会得到无声响的假按钮）。
  2. **requireAdministrator 清单双重杀伤**：`app.manifest` 活动节点是 `requireAdministrator`。其一，未提权父进程 `CreateProcessW` 直接失败 **`ERROR_ELEVATION_REQUIRED`(740)**——Go `exec.Command` 不会像 ShellExecute `runas` 那样代弹 UAC，Start 得到裸 errno，默认错误文案用户看不懂；其二，若以绕过方式让高权限 LiteMonitor 跑起来而 Hanxi 仍中权限，**UIPI 会静默拦截** Hanxi 发出的 `PostMessage(WM_CLOSE)`/`ShowWindow`/`SetForegroundWindow`（跨完整性级别窗口消息默认禁入），Quit/唤窗全部"调用了没反应"。
  3. **上游全默认首启与托管打架**：zip **不含 settings.json**（首启由 C# 默认值生成），`AutoCheckUpdate` 默认 `true`——内置更新检查与 Hanxi 版本管理双通道必然冲突；`IsPawnIORequiredByConfig() => IsAnyEnabled("CPU")` 而默认监控项含 CPU，首启可能拉起 PawnIO 内核驱动安装交互（弹窗+可能二次 UAC），无法无人值守。
  4. **PE FileVersion 四段 ≠ tag 三段**：csproj 显式 `<FileVersion>1.3.6.0</FileVersion>`，`versioninfo.FileVersion` 读出 `1.3.6.0`，与 tag `v1.3.6` 直接比对必假阳——snipaste 先例的同款比对逻辑会拒绝一切正常安装。
- **排查过程**：GitHub API 实测 29 个 release 全量带 digest 与稳定 `LiteMonitor_v<ver>-win-x64.zip` → 真机下载核对 zip 布局（单层包装目录 36 文件、无 runtime DLL→框架依赖 net8.0-windows、**含 GBK 编码中文文件名条目**、无 settings.json）→ 读 `Program.cs`（互斥体命名与静默退出）、`MainForm_Transparent.cs`/`MainFormBizHelper.cs`（MainForm 类实际定义处；无 FormClosing 拦截、托盘双击=Hide/Show、退出菜单=form.Close()）、`app.manifest`（requireAdministrator）、`SettingsHelper.cs`（BaseDirectory 便携、`PropertyNameCaseInsensitive=true`、缺字段回默认）、`DriverInstaller.cs`（PawnIO 触发条件）逐条坐实。
- **正确做法与标准修复方案**：
  - 探测/唤窗整体改走 **FlClash 先例**：`CreateToolhelp32Snapshot` 进程名枚举 + `EnumWindows` 按 PID `SW_RESTORE+SetForegroundWindow`/`postCloseByPID`——外部实例探测、自有实例唤窗、WM_CLOSE 优雅退出三件套统一，不再依赖任何互斥体；
  - Start 失败经 `elevateHint()` 特判 `syscall.Errno(740)` 输出"请以管理员身份重新启动 Hanxi"指引；控制台 stopped 引导行如实预告"首启弹一次 UAC、可能提示装 PawnIO 驱动"；失败态文案指向 .NET 8 桌面运行时（框架依赖版真实依赖，`GetRuntimeStatus` 探测 `Microsoft.WindowsDesktop.App` 8.x 存在性供前端常驻警示条）；
  - **一键产品化出口**（取代"让用户手动右键管理员重启"的纯文案指引）：`platform/windows/elevate_windows.go` 三原语——`IsElevated`（Token.IsElevated）、`RestartElevated`（powershell `Start-Process -Verb RunAs` 复用 portkill 通道，同步阻塞到 UAC 出结果，用户拒绝时按英文/中文文案 + 1223/0x800704C7 多形态识别取消）、`WaitProcessGone`。交接协议：RPC 携带 `-takeover=<旧PID> -route=<当前路由>`，新实例在 `application.New` 抢单实例锁**之前**等旧进程退出——否则被 Wails 判为第二实例静默自退、交接失败；`-route` 以 `#<路由>` hash 挂进窗口 URL，App.vue 挂载后按已注册导航回航原页面。**跨层契约**：前端 `ElevateRestart` 组件以"错误文案含『管理员』"判定 740 横幅显隐，由 bcu/rufus/litemonitor 三模块 `TestElevateHint` 钉死，改写 elevateHint 文案必须保关键词。
  - `seedManagedSettings`：**仅当 settings.json 不存在**时写最小种子 `{"AutoCheckUpdate":false}`——上游反序列化大小写不敏感且缺失字段回属性默认值（源码实证），最小种子=全默认首启+关内置更检查；文件已存在**一字节不动**（用户后续在 LiteMonitor 内改的配置是明确意图，不越权覆盖，everything ini 改写先例的收敛版）；
  - `normalizeFileVersion()` 仅当第四段为 `0` 时裁剪三段再比对；布局自检锚点弃用 settings.json（zip 不含）改用 `resources/lang/zh.json`；GBK 乱码文件名条目照常读满保 CRC，自检只锚定 exe+语言包不受干扰；ImportLocal 因 settings/themes/plugins 全随 exe 目录，**整套目录递归迁移**（收单层包装目录形态），并跳过 `settings.json.tmp/.bak` 运行期垃圾。
- **避坑防重犯建议**：
  1. **侦查清单第 4 项要读互斥体名的构造代码**：名称是否"路径/机器派生"决定探测可行性——固定 identifier 名（tauri/QuickLook）可 OpenMutex，路径派生名只能进程枚举；
  2. **`grep requestedExecutionLevel` 纳入必查项**：requireAdministrator 应用给出三重预告——740 特判文案、UIPI 下唤窗/关窗仅在 Hanxi 同或更高权限时可靠、首启 UAC 属预期交互；
     **复发实证（BCU）**：集成 BCUninstaller 时漏执行本条，真机控制台直接裸 errno 740（"The requested operation requires elevation"）才补上 `elevateHint` 特判——BCU 的 Start 与信使唤窗两条 `exec` 路径都吃 740（外部实例几乎都是用户自提权启动的，唤窗失败率反而更高）。必查项要在写码前执行，不是文档里备着；
  3. **seed 上游配置先实证反序列化宽容度**：大小写策略/缺字段行为/文件缺失回退路径决定"最小种子"是否安全，不实证就是全量覆盖用户配置的隐患；
  4. **FileVersion 四段陷阱**：.NET 应用 csproj 常把 `<FileVersion>` 写成 `X.Y.Z.0` 而 tag 是 `X.Y.Z`，任何"PE 版本==目录版本"核对先做归一；
  5. **上游无 LICENSE 文件**（GitHub API `license: null`）的项目：托管模式仅代下载官方 release、不镜像分发，并在前端"关于"卡如实标注——法律姿态透明，风险交用户知情。

### 20. Wails v3 托盘：动态右键菜单重建线程安全与 Dialog 取消语义

实现"可配置托盘右键菜单"时源码实测（v3.0.0-beta.10），两个易误判点：

- **问题现象与误判风险**：
  1. 想在配置保存后热更新托盘菜单，直觉担心 `tray.SetMenu()` 在非 UI 线程重复调用会崩溃或无效（Win32 菜单句柄归托盘消息线程所有）；
  2. `Dialog.OpenFile().PromptForSingleSelection()` 用户点"取消"时**不是**返回空串，而是返回 error（internal `cfd.ErrorCancelled = "cancelled by user"`），直接透传给前端会把"取消"渲染成"打开文件选择框失败"。
- **正确做法与标准修复方案**：
  - 重建安全：`SystemTray.SetMenu` 内部经 `InvokeSync` marshal 回主线程，Windows 实现 `updateMenu` 是 destroy+`NewPopupMenu` 重建，**任何 goroutine 里可随时重复调用**；本项目 `trayMenuBuilder.Rebuild` 据此实现热更新；
  - 取消归一化：`cfd` 在 wails 的 `internal/` 包下无法 import 做 `errors.Is` 哨兵比较，只能在 Go 边界按文案 `strings.Contains(lower, "cancel")` 归一化为"取消 → 空串、无错"，前端对空串保持静默；
  - 附带实测：托盘 `WM_RBUTTONUP` 会先触发 `OnRightClick` 再弹菜单，若两件事都挂会造成重复动作——只设 `SetMenu` 不设 `OnRightClick` 即为标准形态。
- **避坑防重犯建议**：Wails beta 的菜单/托盘行为勿凭 v2 经验外推，beta.10 的 `updateMenu`/`setMenu` 实现直接读 module cache 源码定夺；对上游 internal 包的 error，Go 侧边界归一化优于前端匹配文案。

### 21. 托管果核看图（GuoheView）：多实例上游击穿互斥体探测模板 + 便携 zip 顶层包装目录 + 官方仅 MD5

果核看图是**闭源原生**极速看图器（非 Tauri/Electron/.NET，窗口类 `UiCore_Window` 为其自研 core-ui 框架共享）。真机实测 3.2.7 后，三个与前序托管模板相悖的硬事实：

- **问题现象与误判风险**：
  1. 家族模板（ccswitch/piclite/everything）探测实例一律首选**命名互斥体** `OpenMutex`。但果核看图是**多实例应用**：二次无参拉起得到的是并存的新窗口，候选互斥体名（`GuoheView`/`MagicView`/`{id}-sim`/`com.guohe.view` 等）`OpenMutex` 全部返回 `ERROR_FILE_NOT_FOUND`——根本不存在单实例锁。若照抄互斥体探测，实例永远探测为"未运行"。
  2. 官方便携 zip 顶层是**包装目录** `GuoheViewPortable/`（exe 不在 zip 根）。照抄 ccswitch 的 `extractAll`（假定平铺布局）会把整棵目录树原样解进隔离目录，`ResolveExe` 找 `版本目录/GuoheView.exe` 落空、exe 深了一层。
  3. 上游发布接口（果核自建 `rj.lovestu.com/download/gh_view`，非 GitHub）**只提供 MD5**、无 sha256，且每次只返回**当前版本**（无历史列表）；`config.ini` 的 `[update]` 仅有 `min_check_interval` 节流键，**无官方关闭自动更新开关**。
- **排查过程**：真机跑便携版 → 二次拉起观察进程数不降、`Get-Process GuoheView` 出现多个 PID；`OpenMutex` 逐一验证候选名全 miss；`WM_CLOSE` 全投递后进程 3 秒归零（证明关窗即退、无托盘驻留，退出通道存在且有效，与 piclite 相反）；解压 zip 观察顶层 `GuoheViewPortable/` 包装目录；解析接口确认 `files[].md5` 而非 `digest`。
- **正确做法与标准修复方案**：
  - 探测改**进程名 Toolhelp32 快照 + EnumWindows 按 PID 过滤**（recordly/bcu 同族），彻底放弃互斥体路径；"打开窗口"三分支：自有实例在跑→`SwitchToThisWindow` 聚焦自有 PID 窗口，仅有外部实例→唤回外部窗口或另开**独立**窗口（不进 Job、不随 Hanxi 退出），都没跑→拉起托管实例；
  - Quit 向**自有 PID** 的窗口投 `WM_CLOSE` + 宽限 + Job 兜底——刻意不按进程名全投：多实例下全名投递会**误伤用户双击图片自开的窗口**（区别于 recordly 单实例全投是安全的）；
  - `extractAll` 先定位 exe 所在 entry 的父目录作 **payload 根**，只收割根内内容并平铺，根外杂质（如 `README-outside.txt`）一概不收；布局自检要求 exe 非空 **且** `portable.ini` 在场（标记缺失=配置会外溢 `%APPDATA%`，视为损坏）；
  - 完整性四层以官方 MD5 + HTTPS + 字节数 + zip CRC + 布局自检兜底（注释如实说明 MD5 抗碰撞弱，是上游唯一官方哈希）；`ImportLocal` 缺 `portable.ini` 时**补写官方开关**（该文件程序只读不改，仅标志存在性，语义安全），保证托管实例配置永不外溢；
  - 无空闲自动退出：多实例下"进程活着=窗口开着=用户在看图"，空闲退出只会打断浏览（与 piclite 关窗藏托盘的空闲豁免语义本质不同）；内置更新器不改写上游配置键，改由页面提示条引导版本管理回 Hanxi，Updater 子进程由 JobObject 继承兜底。
- **避坑防重犯建议**：托管闭源原生应用**先真机验证有无单实例锁**再选探测方案，别默认家族模板的互斥体路径成立——探测契约由上游实例模型决定，不由模板决定。WM_CLOSE 投递范围务必对齐"是否多实例"：单实例可按名全投，多实例必须按自有 PID 收敛。

### 22. 托管 ddns-go：kardianos 服务劫持后门变量 + 端口冲突僵活 + 裸写配置截断 + 内嵌 iframe SameSite

ddns-go（jeessy2/ddns-go，Go 后台 DDNS + Web 面板）是家族里**第一个纯 CLI 控制台程序 + Web UI**形态的托管对象（此前 markeron/ccswitch/everything 皆 GUI 桌面程序）。源码实证（v6.17.6 main.go / util/user.go / config/config.go / web/login.go）后，四个与既有 GUI 托管模板相悖、且每一个都会造成"看着启动了其实没启动 / 一退就毁用户数据"的硬坑：

- **问题现象与误判风险**：
  1. **服务劫持**：上游用 `kardianos/service`，`main` 默认分支先查 `s.Status()`——若用户机器上曾用 `ddns-go -s install` 装过同名 **Windows 服务**，裸执行 exe（哪怕只是 Hanxi 想托管拉起一个普通子进程）会被拽进 `s.Run()` 服务控制路径，脱离控制台环境直接失败/秒退。若照搬 GUI 模板"无参拉起即开窗"的假设，托管实例永远起不来。
  2. **端口冲突僵活**：上游 web 服务 `net.Listen` 失败后**不立即退出**——goroutine 打印"监听端口发生异常"后 `time.Sleep(1 * time.Minute)` 才 `os.Exit(1)`。这击穿了"进程还活着 = 启动成功"的直觉：9876 被外部实例/服务抢绑时，我们的子进程会僵活整整一分钟，状态机若以存活判 running 就是彻底误判。
  3. **裸写配置截断**：上游 `SaveConfig` 是**裸 `os.WriteFile` 全量覆写** `~/.ddns_go_config.yaml`（非 tmp+rename 原子写）。托管 Quit 走 JobObject `Terminate` 强杀，若恰好撞上用户在面板点"保存"的写窗口，会把用户唯一的域名解析配置**截断成半截坏档**——DDNS 从此静默失效，且文件在用户主目录、Hanxi 不负责备份。
  4. **内嵌面板 iframe 死路**：管理界面只有 Web（`/`、`/login`）。最初直觉用主窗口 `<iframe>` 内嵌上游页面最省事——但上游登录 Cookie 是 Go 默认 `SameSite`（未显式设置 = `Lax`），跨站 iframe 不发该 Cookie，**登录后立刻掉会话回到登录页**。
- **排查过程**：通读上游 main.go 启动分支定位 kardianos 劫持路径与那个 sleep-1min 的 web goroutine 错误处理；grep `os.WriteFile`/`SaveConfig` 确认无原子 rename；查 `web/login.go` cookie 构造确认无 `SameSite`/`Secure` 字段；对照 markeron 事故（`explorer.exe <exe>` 会执行该文件）确认"打开安装目录"仍须走模块自有 `OpenDir`。
- **正确做法与标准修复方案**：
  - **启动恒注入 `DDNS_GO_DAEMON=1`**（源码实证的官方后门：`os.Getenv("DDNS_GO_DAEMON")==\"1\"` 时直接 `run()`，完全跳过 `service.Status()` 检测）——这是用户机器残留同名服务时托管启动不被劫持的唯一可靠防线，写进 `StartOptions`→`cmd.Env`；
  - **就绪判定走 TCP 端口探测，不走进程存活**：`Start` 先 `PortOpen(listenAddr)` 预检——端口已开则先甄别（进程名扫描命中 = 外部实例，落 external 不覆盖）再决定接管/报错；预检通过才拉起，拉起后同步轮询 TCP 可连（上限 20s）方判 running，超时即 JobObject 主动终止僵活进程并落 failed。探测层用 flclash 的 `CreateToolhelp32Snapshot` 进程名枚举（ddns-go 无命名互斥体），非模板默认 `OpenMutex`；
  - **Quit 前置"配置写静默期"**：终止前 stat `~/.ddns_go_config.yaml`，若 mtime 距今 < 1.5s 判定"用户正在保存"，轮询等待至静默（上限 5s）再强杀——把 JobObject 终止撞裸写的概率压到窗口外。`Stop`（应用退出 OnShutdown 通道）**刻意跳过**该等待：主程序退出必须限时返回，宁可不优雅也不能阻塞宿主关闭；
  - **面板用独立顶层 `WebviewWindow`（`app.Window.NewWithOptions`）而非 iframe**：顶层文档 Cookie 视作第一方，登录态正常；关窗语义 `RegisterHook(WindowClosing)→Cancel+Hide`（保留 WebView2 会话免重复登录，且永不销毁子窗口 → 不触发"最后一个窗口关闭退出应用"策略）；改端口重启用经 `SetURL` 导航到新地址；
  - 绑定地址恒 `127.0.0.1:port`（上游默认 `:9876` 绑全网卡，会把带 DNS 服务商凭据的面板暴露到局域网），首启即用 `-l 127.0.0.1:9876`；配置沿用上游 `%USERPROFILE%` 固定路径（与用户自跑实例共享同一份，托管即无缝接管）；stdout/stderr 环形日志（复用 `internal/ringbuf` 共享包）经正则脱敏 DNS 凭据（`token/secret/accesskey=…`→`***`，上游 web 日志页有脱敏但 stdout 通道没有）。
- **避坑防重犯建议**：托管**纯 CLI 后台程序**（非 GUI）前，必读其 main 启动分支——kardianos/service 形态的 Go 工具极易有"检测到已装服务则走 SCM"的劫持路径，常配 `*_DAEMON`/`-d` 类旁路开关，找到它比对抗它省力。端口型 web 工具的"启动成功"信号必须是**端口 TCP 可连**，绝不能用进程存活（僵活/后台化太常见）。任何**裸写用户主目录配置**的上游，托管侧强杀通道前都要加"写静默期"防御——数据在用户目录=你毁的是用户真实资产且无从恢复。内嵌第三方 Web UI 首选独立顶层 Webview，别 iframe：SameSite/CSP/X-Frame-Options 任一项都能让 iframe 静默失效，而顶层窗口天然规避。

---

### 23. envcheck 定向开洞：npm 全局工具一键装升卸的安全边界与三处非典型陷阱

把 Claude Code / Codex 的「安装/版本对比/升级/卸载」做进开发环境检测（envcheck）模块，抽象成配置驱动的通用 npm 工具框架（目录加一行即扩展）。过程中撞出若干「看起来同构、实则反直觉」的点，尤其前两条是环境检测这类"只读模块"引入执行面时的通用决策。

- **问题现象与错误原因**：
  1. **零执行面破例的安全边界**：envcheck 既有叙事是"只探测 + 只开网页"（`PackageManagerUpgradeHint.vue`、`RevealToolPath` 注释多处声明"Hanxi 不执行"）。新增 `npm install -g` 是对该哲学的**显式破例**——最危险的直觉是"前端传包名，后端拿去执行"。`npm install -g <任意串>` 里一个 `@evil; rm -rf` 或带空格/引号的参数就能把命令语义劫持进 `cmd /C`；`--registry`、`--prefix`、`-l` 等参数还能把落点改到任意目录。破例若不做死边界，等于把整台机器的全局环境交给前端字符串。
  2. **插件式注册 × 计数断言**：`detect` 走 `init()` 里 `Register()` 的无中央清单模式。claude/codex 探测器由**兄弟包 `npmtool` 的 `init()`** 注册（npmtool → detect 单向依赖）。而 `detect` 包自带的 `TestRegistry`/`TestRunAll` 硬断言"恰好 9 个探测器"。直觉担心新工具撞坏该断言。
  3. **测试 seam 的 `t.Cleanup` LIFO 泄漏**：`npmtool` 的 `runNpm`/`lookNpm` 是包级函数变量（仿 detect 的 `lookPath` seam）。`TestManagerLockConflict` 用一个"阻塞直到 release"的桩占住 npm 全局锁来测互斥。首版把 `close(release)` 放 `t.Cleanup`，且**先**注册、`withManagerSeams` 的还原**后**注册——结果 cleanup 里还原 runNpm 的钩子先跑、放行 goroutine 的钩子后跑。
- **排查过程**：全仓 `go test ./...` 里 `TestManagerLockConflict` 卡到 waitIdle 超时，且**后续** `TestManagerSuccess/FailureTerminal` 报"Claude Code 正在执行安装"——锁被上一个用例的 goroutine 带着跨用例泄漏。反推根因：后台 goroutine 延迟读取 `runNpm` **包变量**（不是启动时快照值），若它在 seam 被还原成 `defaultRunNpm` 之后才真正调用，就会**在开发机上真的跑一次 `npm install -g @anthropic-ai/claude-code@latest`**（副作用 + 10min 超时占锁）。另实测确认第 2 点：`go test ./detect` 的二进制不链接 npmtool，注册数恒为 9；只有链接了 npmtool 的 envcheck/app 二进制才见 11。
- **正确做法与标准修复方案**：
  - 安全边界三铁律：① service 方法只收**目录 ID**（claude/codex），包名/命令参数一律取自后端 `catalog.go` 常量，永不接受前端传入的包名/版本/路径；② 目录 `init()` 用 `packageNamePattern` 白名单式校验（`^(@scope/)?name$`，禁一切 shell 语义字符）、`detect.Registered()` 撞键自检，**编程错误即 panic**（与 nodeversion URL 校验同哲学）；③ 执行层隐藏窗口（`CREATE_NO_WINDOW`+`HideWindow`）、`.cmd/.bat` 经 `cmd /C` 包装、npm 全局树一把**包级互斥锁 singleflight**、不提权、卸载仅前端 `ConfirmDialog` 二次确认（且如实告知"不删 `~/.claude` 等配置"）。同步把副标题与 `openBCUForUninstall` 注释从"零执行面"改为"除受管 npm 工具外仍零执行面"。
  - 兄弟包注册**不改** detect 的计数断言：这是插件式注册的正确用法（谁 import 谁生效）；只需注意任何"全量枚举"断言都归属 detect 包内、天然不受下游包影响。
  - 桩测试的释放/等待放在**测试体内、seam 仍生效时**完成（`fire(); waitIdle(t)`），`t.Cleanup` 仅兜底放行；杜绝"还原 seam 早于放行 goroutine"的 LIFO 顺序坑。
  - npm 子进程 stdout/stderr 合并进一条 `os.Pipe`：`cmd.Start()` 后**父进程立即 `pw.Close()`**——否则 `cmd.exe→node.exe` 孙子进程继承的写句柄与父进程互持，读端在直连子进程退出后**永不 EOF**，`Scanner` 卡死。
  - npm 版本**不复用**通用 `platform/versioncmp`：它对 `"260-beta"` 这类段整体退化字典序，会把预发布误判 ahead；`npmregistry/compare.go` 自建 semver-lite（数值核心 + 预发布 < 正式版 + `+build` 忽略 + 不可解析落 unknown）。scoped 包名走 `url.PathEscape`（`@scope%2Fname`），registry 两种写法都吃、单测锁转义式。
  - 本机 PATH 命中目录 ≠ npm 全局 `prefix`（实测 `~\.local\bin` 优先于 `%AppData%\npm` 的双拷贝）时，只加 `RelationDetail` 安全提醒、**不改 status**：一键升级/卸载只作用于 npm 全局那份，命中结果可能不变。
- **避坑防重犯建议**：
  1. 任何"只读检测"模块要引入执行面，先定死**入参只能是后端白名单 ID**、执行参数全部来自常量，把前端字符串挡在命令拼接之外；破例范围（仅目录内工具、仅装升卸、不提权）写进代码注释与 UI 文案，不留"顺手加个自由命令"的口子。
  2. 包级 seam（函数变量）+ 后台 goroutine 是危险组合：**goroutine 读的是变量当前值不是快照**。测"占锁/长任务"这类阻塞桩，放行与等待必须在 seam 生效期内于测试体收尾，别依赖 `t.Cleanup`（其 LIFO 顺序易先还原 seam）。CI 全绿但真机误触发副作用，就是这类顺序坑的signature。
  3. 流式外部命令用 `os.Pipe` 合并输出时，父进程写端一定要 `Start()` 后立刻关，交给子进程独占；跨 `cmd.exe` 再 spawn 孙进程的链路，句柄继承会让"等 EOF"变成"等超时"。
  4. "最新版对比"的版本号先问它属于哪套编号体系：npm 语义化版本含预发布/构建元数据，通用宽松比较器会误判，宁可为该数据源单独写一个贴合其规范的比较函数。
  5. 前端渲染受管工具集**按后端目录回传的名字集合驱动**，不写死 `claude/codex`——目录加一条，卡片、检测、装升卸、前端全链路零改动，这才是"通用框架"相对"两个 if"的价值兑现。

### 24. 托管 rust-portable 应用（RustDesk/SubnetDesk）：外层秒退进程不能当生命周期锚点 + 提取目录归属判别 + 便携/安装版双同名陷阱

RustDesk 与其 LAN fork SubnetDesk（协议互不兼容的两个 AGPL 应用，Hanxi 以"远程控制"组合同时托管）是家族里第一类 **rust-portable packer 自解压单 exe** 形态。若照抄 markeron/ccswitch/litemonitor 三套引擎模板，会在四个层面静默失效：

- **问题现象与误判风险**：
  1. **外层进程不是本体**：`libs/portable/src/main.rs`（两仓库同源）把内层负载解压到 `%LOCALAPPDATA%\{内层条目名小写}\` 后 `cmd.spawn()` 即返回——外层 packer 在拉起后 ~1 秒**自退且退出码不反映内层存亡**。模板的 `wait(){ ...; job.Close(); cmd=nil }` 会在内层 UI 刚出现时把状态误判为"已退出"，更要命的是 `job.Close()` 恰好解除 KILL_ON_JOB_CLOSE 联动——**内层进程树全体放飞成孤儿**，Hanxi 退出不再连带。
  2. **探测锚点的归属两难**：提取目录是所有同镜像便携实例**共享**的（外层改名无济于事——`app_dir_name` 取的是包内条目名）。"目录里有进程"既可能是自有也可能是外部用户双击的；安装版 + 其 Windows 服务（`Program Files\RustDesk\rustdesk.exe --service`，SYSTEM）与便携版**进程名完全相同**，纯进程名探测（recordly/litemonitor 模板做法）会把"服务常驻"误判为"实例在跑"，external 灯常亮、Quit 永远"不越权"。
  3. **无退出/唤窗双契约缺失**：上游无 `--quit` 类 CLI、无单实例互斥体；关主窗 = 窗口销毁但内层驻留托盘继续被控；`WM_CLOSE` 模板信使只会"再藏一次窗"；二次无参拉起**不是**唤窗信使而是**又开一个新窗**。"优雅退出+宽限"在关窗驻托盘态必然空转 2s 再强杀，纯演戏。
  4. **`--install` 首启陷阱**（假警报但要实证排除）：`core_main.rs` 的 `click_setup` 会在特定条件下给 args 注入 `--install` 弹出安装向导——实跑证实触发条件是**外层文件名以 `install.exe` 结尾**（`is_setup()`），官方资产名/托管定名均不命中；但 `ImportLocal` 若收了用户改名的 `*-install.exe`，等于托管一个开机自安装炸弹。
- **排查过程**：curl GitHub API 确认两仓库 Windows 资产只有单文件 exe + msi（无 portable zip）且 digest 全量在场；读 `libs/portable/src/main.rs` 定位 spawn-不-wait 与 `data_local_dir()` 提取路径；读 `src/core_main.rs`/`src/common.rs` 逐条核对 click_setup/is_setup/is_quick_support/`--connect` 语义；确认 SubnetDesk 自有 hbb_common fork 中 `APP_NAME="SubnetDesk"`、监听 21118（RustDesk 21116/21117），两模块并行无端口/目录冲突；RustDesk 侧 tag 无前缀 v 而 SubnetDesk 有，资产名分别还需排除 `-x86-sciter`/`-aarch64` 变体。
- **正确做法与标准修复方案**：
  - **生命周期锚点 = 提取目录内的自有进程树**：引擎 `supervise()` 两相轮询——phase1 等内层出现（与外层 cmd.Wait 并行，外层挂死也能到 startGrace 判失败）、出现即 running 并上报内层 PID；phase2 树消失即 stopped；`job.Close()` 严格推迟到树确认后。外层退出码仅作启动早期判死的辅助信号；
  - **归属用父 PID 闭包**：`FindOwnPIDs(ancestors)` 从自有外层 PID（含派生开窗的外层）沿 `ParentProcessID` 传递闭包收树；身份过滤 = Toolhelp32 进程名 → `QueryFullProcessImageNameW` 路径前缀必须落在 `%LOCALAPPDATA%\{rustdesk|subnetdesk}\`——安装版/服务天然出局（SYSTEM 进程在非提权下还会多一道 OpenProcess 失败保险）；`ImportLocal` 与落盘定名保证外层名不触发 `install.exe`/`-qs` 规则；
  - **契约缺失的诚实实现**：Quit 直接 `TerminateJobObject` + 宽限等收尾（文案如实"进行中的会话会断开"）；`OpenWindow` 三分支——有可见/隐藏窗按 PID `SW_RESTORE+SetForegroundWindow`，托盘无窗则**派生第二 packer 并 assign 进同一 Job**（重新开窗的唯一正路，新外层 PID 并入闭包），external 态只唤窗绝不代拉起；**不做空闲自动退出**（被控端常驻是产品语义不是泄露）；
  - 版本层：单文件下载免解压，完整性 = 官方 digest + 声明字节数 + MZ 魔数（防镜像 HTML 错误页伪装 exe）；RustDesk tag 无 v → 列表层规范化 `Version="v"+tag` 且保留 `Tag` 原值构造下载 URL（**tag 参与 URL，绝不能拿展示值拼**）。
- **避坑防重犯建议**：遇到"官方 exe 只有一个文件"的发布形态，先读它的打包器源码再谈托管——rust-portable/自解压类"启动器进程"一律不满足模板的"cmd = 本体"前提，凡照搬 `cmd.Wait` 锚定的方案都会在真机上出现"状态秒跳未运行但窗口明明开着"。进程名撞车（便携 vs 安装 vs 服务同名）时，唯一可靠的判别是**镜像路径前缀**（`QueryFullProcessImageName` + PROCESS_QUERY_LIMITED_INFORMATION），并在设计文档里写清"安装版不归我管、互不感知"的边界，别让探测语义含糊。

  **家族第二变体·复发实证（BCU 官方 bootstrapper，2026-09-06）**：Bulk-Crap-Uninstaller 便携 zip 是"根目录小体积 `BCUninstaller.exe` 启动器 + `win-x64\` 完整应用"的 6.x 布局——外层拉起内层真身后 **~20ms 自退**，且真身诞生早于我方 `job.Assign`（不在 Job 管辖）。症状与前四坑首坑完全同型：托管实例被 wait() 的"我方进程秒退 + 互斥体存活"规则误判成 external（横幅常驻"检测到外部实例"）、退出联动失效。BCU 的修复比 rustdesk 轻一个数量级：**根本不必启动外层——`version.ResolveExe` 直指 `win-x64\BCUninstaller.exe`（缺内层的旧布局回退外层兜底），`cmd.Dir` 仍钉版本根**（实证真身按工作目录解析便携设置，退出写回版本根、win-x64 无分家文件），JobObject/锚点/wait 模板一字不动。教训：外层秒退家族不以"自解压打包器"为限——普通多文件 zip 也能藏 bootstrapper；侦查阶段除读打包脚本外，最省事的实锤是**直接起一次入口 exe、盯 20ms 粒度的 PPID 树**（或留意根目录 exe 异常小巧、子目录还有同名 apphost）。

### 26. 托管 Rufus：固定名互斥体可探但"二次拉起弹模态错误框"反成噪音 + 磁盘级写入工具的强杀安全边界 + 便携 ini 存在即生效兼关更

Rufus（C/Win32 原生 USB 启动盘制作，GPL-3.0）看似 litemonitor(#17) 的"原生 GUI + requireAdministrator"同族，实际在探测、退出安全、配置三处再次变形，且带一个全家族独有的**数据安全**维度：

- **问题现象与错误原因**：
  1. **互斥体可探，但信使路径"有害"而非仅"无效"**：`src/rufus.c` 单实例锁是 `CreateMutexA(NULL, TRUE, "Global/" APPLICATION_NAME)`，APPLICATION_NAME 固定为 `Rufus`——与 LiteMonitor 的路径派生名不同，**名称可预测，OpenMutex 探测完全可行**（存活权威信号）。但第二实例抢锁失败不是静默 return，而是先 `MessageBoxExU(...MB_SYSTEMMODAL...)` 弹一个"已在运行"错误框、等用户点掉才退出——所以"无参二次拉起当唤窗信使"比 LiteMonitor 更糟：不仅唤不了窗，还平白多弹一个要人手动确认的系统模态框。
  2. **磁盘级写入 × 强杀兜底 = 半成品盘风险**：Quit 模板的"WM_CLOSE + 宽限 + JobObject 强杀兜底"在监控/预览类工具上无害，但 Rufus 正写 ISO 到 U 盘时收到 `WM_CLOSE` 会弹上游"任务进行中，确定退出？"确认框挂住消息循环——宽限期（2s）内无人点击即落入 `Terminate`，**强杀正在写盘的进程产出损坏启动盘**。这是本家族第一个"强杀兜底路径本身有数据破坏后果"的模块。
  3. **便携开关是"文件存在即生效"，且顺带控制更新检查**：`src/rufus.c` 检测 exe 同目录有 `rufus.ini`（哪怕空文件）即 `ini_file=...` 且 `app_data_dir=app_dir`——全设置走 ini 而非注册表；`src/net.c` 的 `CheckForUpdatesThread` 实证 `ReadSetting32(UpdateCheckInterval) == -1 → "Check for updates disabled"`，即写 `UpdateCheckInterval = -1` 可永久关闭内置更新检查。上游安装版 `rufus-X.Y.exe` 与便携版 `rufus-X.Yp.exe` 是**同一二进制**（GitHub digest 实测同哈希）。
  4. **两段式版本 + imported 兜底目录在 versioncmp 下误排最前**：tag 是 `v4.15`（X.Y，非 X.Y.Z）；`ListInstalled` 用 `versioncmp.Compare` 降序取"最新已装"作冷启动回退，但 `imported-时间戳` 目录名非纯数字，versioncmp 退化 `strings.Compare` 后字典序"i…" > "4…"，会被排到列表**首位**——回退逻辑选中版本未知的导入目录。
- **排查过程**：GitHub API 实测 v4.9~v4.15 连续多版稳定发 `rufus-X.Yp.exe` 且全带 digest、安装版与便携版逐版同哈希 → 确认取 `p.exe` 形态语义最明确；`curl` 拉 `src/rufus.c` 逐行确认 `"Global/" APPLICATION_NAME`、`MessageBoxExU(MB_SYSTEMMODAL)`、`ini_file/app_data_dir`、`-g/-i/-l/-f/-w/-x` 全为启动参数无退出信使；拉 `src/settings.h`+`src/net.c` 坐实 `UpdateCheckInterval = -1` 禁用更新检查；`src/rufus.manifest` 确认 `requireAdministrator`（740 直拒路径复用 #17 的 `elevateHint`）。
- **正确做法与标准修复方案**：
  - **探测混合、唤窗直操作**：`IsRunning` 以 `OpenMutex(Global\Rufus, SYNCHRONIZE)` 为权威（不随 exe 改名漂移，外部实例浏览器原名 `rufus-X.Yp.exe` 也命中），再退到进程枚举兜底；`FindPIDs` 用 Toolhelp32 匹配 `rufus.exe` 或 `rufus-*.exe` 形态族供 PID；唤窗统一 `EnumWindows 按 PID SW_RESTORE+SetForegroundWindow`——**信使路径彻底不用**（`RestoreWindow` 与 `service_test.TestNoMessengerOpenWindow` 双重钉死）；
  - **强杀安全边界靠前端常驻警示而非取消兜底**：Quit 保留 WM_CLOSE+宽限+强杀结构（异常滞留仍需能收尾），但 running 态横幅（`UiBanner tone=ok`）与 stopped 引导行**常显**"⚠ 正在写盘时切勿退出/关 Hanxi，中途终止产出半成品盘"，并提示"写盘任务中建议关闭『随 Hanxi 一起关闭』保持独立运行"；
  - **种子 ini 一石二鸟**：`seedPortableSettings` 仅当 `rufus.ini` 不存在时写 ASCII 种子（含 `UpdateCheckInterval = -1`），激活便携零注册表 + 关内置更；文件已在则一字节不动（`ImportLocal` 若源旁有 `rufus.ini` 会随行搬运保住用户既有配置）；
  - **版本层适配**：`plainSemverTag=^v\d+\.\d+$` 收两段式；单文件三重校验（digest+字节数+MZ，无 zip 布局自检，承 #24 rustdesk）；`ListInstalled` 引入 `importedRank` 让 `imported-` 目录排序**沉底**，`resolveActiveVersion` 冷启动回退才不误选版本未知的导入目录。
- **避坑防重犯建议**：
  1. **"无唤窗契约"要分级**：静默 return（LiteMonitor）与弹模态错误框（Rufus）都属"二次拉起不能唤窗"，但后者让信使路径从"无用"升级为"主动伤害"——侦查时不仅看第二实例是否 show+focus，还要看它退出前有没有 MessageBox/Dialog 副作用；固定名互斥体虽可探存活，**不能**因此推断它支持唤窗。
  2. **托管"有状态副作用"的工具（写盘/联网改配置/起驱动）前，先审计强杀兜底的后果**：只要 `Terminate` 可能中断一个不可逆 I/O，就必须在前端把该风险讲成用户能看见的常驻警示，而不是默默信任 2s 宽限；
  3. **便携模式若靠"某文件存在"激活，种子文件可同时承载关更配置**，但务必"已存在即绝不改写"，并让 ImportLocal 感知搬运，避免收纳用户配置时反而覆盖；
  4. **版本号非标准段（时间戳兜底目录）参与 `versioncmp` 排序前必须先按"是否规范版本"分层**，否则退化字典序会把兜底目录顶上"最新"，污染一切"取第一个当默认"的逻辑。

### 27. 托管用户可配关窗行为的下载器（Bili23 Downloader）：Quit 兜底强杀从"安全网"反转为"数据事故源" + 改名壳 exe 版本探测迁移到源码常量

Bili23 Downloader（B 站视频下载器，Python/PySide6 + 自带静态运行时整目录便携包，GPL-3.0）表面是 ccswitch 模板族（命名互斥体 + 二次拉起信使唤窗），实则在退出语义与安装形态两处击穿模板前提：

- **问题现象与错误原因**：
  1. **WM_CLOSE 的结局由用户设置决定，不由代码决定**：`src/gui/interface/main_window.py` 的 `closeEvent → on_close` 三分叉——`EXIT` 放行优雅退出、`MINIMIZE` 仅 `hide()` 收入托盘（`e.ignore()`）、`ALWAYS_ASK`（**出厂默认**）弹模态询问对话框。照抄 ccswitch 的"WM_CLOSE + 2s 宽限 + JobObject 强杀兜底"，默认设置下 2s 只会换来对话框挂起然后**强杀一个刚弹出"您要退出吗"的下载器**；`MINIMIZE` 设置下每次"退出"都是静默强杀，在途分片下载直接中断。
  2. **优雅退出本身是慢动作，快轮询会误判**：上游退出收尾要 `downloader_manager.shutdown()`（等在途分片线程收敛）→ `AsyncTask.safe_quit()` → `task_manager.shutdown()`（异步写队列落盘），`hide()` 之后进程可能还要活十几秒——"窗口没了"与"收入托盘"在这一相位外观完全相同，短宽限会把"正在收尾"误报成"驻留托盘"。
  3. **改名壳 exe 让 PE 版本探测全线失效**：入口 `Bili23.exe` 是 Python-Static 的 pythonw 改壳（内嵌解释器，`_pystand_static.int` 引导 chdir 加载 `script/main.py`），FileVersion 资源恒为 Python 版本号——`ImportLocal` 沿用 `versioninfo.FileVersion(exe)` 会把 3.13 当应用版本入账。
  4. **外层"秒退"假象险些误判为 rustdesk 型启动器**（#24 类疑）：真机冒烟时 Hanxi 拉起的 PID 瞬间消失、另一 PID 健在——实为**用户机器已常驻一个提权实例**，新进程命中单实例锁、唤醒旧窗后按契约自退（信使语义的现场实证），`Bili23.exe` 本身仍是常驻本体。幸而按 #24 教训先验尸再下结论。
- **排查过程**：GitHub API 实测 v2.10~v2.15 便携 zip 全带官方 digest；真机下载实测**直连 github.com 在 ~6MB 处 Connection reset**，gh-proxy 镜像跑完且 sha256 与官方一致；`unzip -l` 实测 zip 带顶层单目录 `Bili23-Downloader/`（#21 果核同款复发，CI 的 `7z a ... Bili23-Downloader\*` 在 PowerShell 下通配符未展开）；读 `main.py` 坐实互斥体 GUID（与 Inno `AppMutex` 同值）、QLocalServer 命令字（`activate`/`ensure-running`，未知命令按 activate 兼容旧版）、AppData 落盘与 `os._exit` 收尾哲学；读 `enum.py`/`config.py` 确认 `WhenClose` 默认 3=ALWAYS_ASK、托盘无条件创建（`init_deferred_ui` 硬编码 show）；冒烟二次拉起证实信使自退 + 主进程单实例常驻。
- **正确做法与标准修复方案**：
  - **Quit 三态化，彻底移除静默强杀兜底**：`Engine.Quit()` 返回 `exited / hidden / windowUp`——第一阶段 3s 宽限收快退场；存活且有可见窗口 → `windowUp`（弹窗询问中，把决定权还给用户）；无窗口 → 进入 25s 收尾观察期，期满退出记 `exited`、仍存活才记 `hidden`（真驻托盘）。三态全部如实映射到 `QuitOutcome` 文案；强杀唯一入口是用户显式「强制结束」（`confirm` 说明"在途下载中断、可断点续传"）；**不做空闲自动退出**（同 #24：外部无法感知任务活跃度，误杀代价高）；
  - **窗口探测按 PID 不走类名**：Qt 顶层窗口类名含 Qt 版本号（随 PySide 升级漂移），`EnumWindows + GetWindowThreadProcessId` 过滤（recordly 族手法）+ 标题非空剔除 Qt 隐形消息窗；`HasVisibleWindow` 同时服务 Quit 分叉与"运行中·已收入托盘"琥珀灯；
  - **整目录形态的安装单元适配**：解压剥离顶层目录（`commonTopDirPrefix` 通用探测，上游改扁平布局自动兼容）；布局三锚点自检（`Bili23.exe` + `_pystand_static.int` + `script/main.py`）；`ImportLocal` 整目录复制（~108MB，拒绝非常规文件），版本探测改解析 `script/util/common/config.py` 的 `app_version = "X.Y.Z"` 常量（安装版/便携版目录同构，失败落 `imported-时间戳` 兜底）；
  - tag 侧 `v2.00.7` 前导零变体天然通过 `\d+\.\d+\.\d+` 结构过滤，`-rc` 后缀 tag 被同一正则拒入列表（预发布不进托管面）。
- **避坑防重犯建议**：
  1. **托管前先读上游 closeEvent/退出收尾源码，别只看有没有互斥体**：单实例契约健全 ≠ 退出契约存在——凡是"关窗行为用户可配"（Electron 的 `before-quit`、Qt 的 closeEvent 分叉、Tauri 的 RunEvent）或"退出前要做长收尾"（下载器/同步盘/数据库）的应用，Quit 的强杀兜底都必须从模板默认里显式拆除，改成结果分叉如实上报；
  2. **"宽限 + 强杀"兜底的适用性按数据后果分级**：预览/监控类误杀零成本可保留兜底，写入类（#26 Rufus 写盘、本例下载分片）一律禁静默兜底并给常驻风险文案；
  3. **入口 exe 是"壳"的（改名解释器/自解压/打包器），导入版本探测必须找上游源码里的版本常量**，`versioninfo.FileVersion` 只对"自有 exe"成立；
  4. 真机冒烟遇到"我拉的进程死了、另一个同名进程活着"，**先查同名进程的创建时间与安装路径**再谈启动器结论——用户可能正开着它（本次实为提权常驻实例，`Path` 因权限读空易误导）。

### 25. 前端重构 Phase 0/1：ESLint flat 的 TS base 压掉 .vue 解析器 + Vitest 5 API 变更与 fake timers 死锁 + CSS 注释星杠截断

引入 ESLint(flat)/Vitest 安全网与样式地基时踩中五个工具链坑：

- **问题现象与错误原因**：
  1. **所有 `.vue` 报 `Parsing error: '>' expected (1:8)`**：flat config 中 `typescript-eslint` 的 base 配置不带 `files` 限制、全局把 `languageOptions.parser` 设成 TS parser；若它排在 `eslint-plugin-vue` 之后，`.vue` 原始文件（`<script setup…`）被 TS parser 直接啃就必炸。flat 合并是"后者胜"。
  2. **`mock.fn.callCount` 返回 `undefined`、`.mock.callCount()` 又报 not a function**：Vitest 5 移除了 `.callCount` 属性直读；断言调用次数应改用 matcher。
  3. **fake timers 下 `flushPromises()` 永久挂起**：test-utils 的 `flushPromises` 实现是 `new Promise(setTimeout)`，`vi.useFakeTimers()` 后宏任务不推进——测试死锁在挂载处。
  4. **`ConfirmDialog` 以 `open=true` 直接挂载时 Escape 监听不挂、焦点不落**：内部 `watch(() => props.open)` 非 `immediate`，特征测试首次 dispatch Escape 无效果——这是既有语义（实际使用永远从 false 打开），测试必须按事实锁定而不是按期望。
  5. **CSS 注释内含 `*/` 序列会提前截断注释**：token 注释里写 `--color-*/--surface-*` 这类"星杠"连缀（变量族通配写法）让注释提前闭合，dev 模式不报、生产构建才被 lightningcss（Vite 8 默认 minifier）以 `Unexpected token Delim('*')` 打回。
  6. **别名层同名自引用制造 var() 循环（Phase 1 tokens.css 真 bug，Phase 5 由并行会话揪出）**：兼容别名 `--color-text-muted: var(--color-text-muted);` 写在语义定义之后，后者胜 → 自引用非法 → 该属性 IACVT（invalid at computed-value time）静默失效，所有引用它且无兜底的文字退回 inherit，连指向它的别名一起失效。CSS 自定义属性没有编译期报错，肉眼也难察觉（颜色"看起来差不多"）。
- **正确做法与标准修复方案**：`.vue` 工程 flat config 顺序固定为 `tseslint.configs.recommended → pluginVue.configs['flat/essential'] → .vue 的 parserOptions.parser 覆写`；次数断言用 `expect(fn).toHaveBeenCalledTimes(n)`；fake timers 场景用纯微任务排空（循环 `await Promise.resolve()`）替代 `flushPromises`；生命周期敏感的行为先以特征测试钉死现状；CSS 注释中的通配族写成 `--color-* / --surface-*`（星与杠之间加空格）；别名层落笔后必须做自引用扫描（正则 `(--[a-z0-9-]+):.*var\(\1[,)]` 应零命中），语义块内同名只允许定义一次、别名只桥接不同名旧称。
- **避坑防重犯建议**：新工具链第一步永远先跑"空转基线"（lint 应 0 error、单测应全绿）再接 CI；给 lint 定"只报告不阻断"的过渡纪律时，把降级逻辑写进配置文件并注明恢复 error 的时机，防止 warn 状态被后人当成永久默认。绑定生成目录（`bindings/**`）必须同时进 ESLint ignores、`.prettierignore` 与测试 include 白名单之外——格式化生成物会击穿 `verify:bindings`。

### 28. 托管模块批量补数据目录：path_provider Windows 目录名按 PE 版本信息拼接的实证链 + 模板拷贝族"文件级 grep 盘点功能"误判

给全部托管模块批量加"数据目录直达"与"首个下载版本自动设为使用版本"时，两处结论靠猜/靠粗对账都会翻车：

- **问题现象与错误原因**：
  1. **"配置在 %APPDATA%" 是不完整结论**：FlClash 模块注释只写了"配置数据在 %APPDATA% 用户目录"。按直觉取 `%APPDATA%\FlClash` 会指向不存在的目录——path_provider 的 `getApplicationSupportDirectory` 在 Windows 上读 **exe 的 PE 版本信息**拼 `%APPDATA%\<CompanyName>\<ProductName>`，上游 `windows/runner/Runner.rc` 实测 `CompanyName=com.follow`、`ProductName=clash`（CMake `project(FlClash)` 管二进制名、管不到版本资源），真实数据目录是 `%APPDATA%\com.follow\clash`（`FlClash.lock`/`config.yaml` 均在其中）。
  2. **文件级 grep 坐实"功能已有"是误判**：对账"哪些模块下载后自动设使用版本"时，`grep -l 'GetActive() == ""'` 命中 17/17 全绿——但逐处看上下文，既有兜底几乎全在**启动成功路径**（冷启动把实际采用版本回写 active），下载完成点并不激活；唯 snipaste 一处在 `manager.Download` 成功后判空收编。模板拷贝族在同一函数的不同调用点存在行为分叉，用户"下载完还要再点一下"的体感恰恰是真的。
- **排查过程**：读 `path_provider_windows_real.dart` 源码确认取名规则（ProductName 经非法字符清洗，缺失回退 exe 文件名；有 CompanyName 则多拼一层）→ 对照上游 Runner.rc/CMakeLists 定死 com.follow\clash；Tauri 族（Keyviz `org.keyviz`、PicLite `com.piclite.desktop`）按 identifier 走 appData；已运行过的工具用本机目录实存交叉印证（RustDesk/SubnetDesk/Bili23/`.cc-switch` 均在，Snipaste 便携实证 config.ini 落 exe 同目录）。
- **正确做法与标准修复方案**：9 模块补 `OpenConfigDir`（八种目录语义；ddns-go 上游是 home 下单文件，用 `explorer /select,` 定位而非整个打开主目录；frpc 为 CLI 托管无自有数据，按钮语义是 Hanxi 实例 TOML 配置目录并先 MkdirAll）；16 模块 `DownloadVersion` 的 goroutine 在 `err == nil` 后补"未设使用版本时自动收编刚下载版本 + 错误分支补 return"，与 snipaste 既有行为对齐。前端无需改：各视图 `done` 事件既有 `loadVersions()` 刷新链自动反映激活结果。
- **避坑防重犯建议**：
  1. **托管/文档化"配置在用户目录"必须落到具体子目录名**，实证链 = 上游版本资源/identifier 配置 → 路径库取名源码 → 本机实存印证；Flutter 应用尤其注意 CompanyName\ProductName 两层拼接与"ProductName ≠ 产品名"的历史残留；
  2. **模板拷贝族盘点功能差异必须到函数/hunk 级**（`grep -B/-A` 看调用上下文），文件级命中只能证明"仓里某处有这行字"，不能证明"同一时机做了同一件事"——同构模板的坑恰恰在"99% 相同 + 关键 1% 分叉"。

### 29. 全局鼠标钩子 + frameless 弹窗（quickmenu MVP）："吞起不吞按"真机卡死系统输入的反转，外加 beta.10 三处认知差

实现"右键长按唤出快捷菜单"时，把 Win32 低级钩子缝进 Wails v3 beta.10 多窗口体系，八个点与直觉不符：

- **问题现象与错误原因**：
  1. **`x/sys/windows` 没有鼠标钩子**：`WH_MOUSE_LL`、`MSLLHOOKSTRUCT`、`SetWindowsHookEx`/`CallNextHookEx`/`GetMessageW` 全部缺席（包里只有 `GetCurrentThreadId` 可蹭），照 Go 社区笔记找常量会一头雾水。低级钩子还要求安装线程自泵消息，goroutine 随时可能被调度迁移，不 `runtime.LockOSThread()` 钩子形同虚设。
  2. **`go vet` 报 `possible misuse of unsafe.Pointer`**：回调里 `(*MSLLHOOKSTRUCT)(unsafe.Pointer(lParam))` 是 Win32 胶水层的标准写法，但 vet 的 unsafeptr 检查不认 uintptr→Pointer 直转。
  3. **`HiddenOnTaskbar` 不在 `WebviewWindowOptions` 顶层**：它挂在 `options.Windows`（`WindowsWindow` 子结构）里，顶层直写编译错；`WebviewWindow` 也没有公开 `Destroy()`（`Close()` 只是 fire `WindowClosing`，会被 Cancel 钩子拦下），子窗口想"释放"根本没有门。
  4. **两套坐标系**：LL 钩子的 `pt` 是物理像素，而 `SetPosition/Size/Width/Height` 全是 DIP（Windows 实现内部 `DipToPhysicalRect`）。拿钩子坐标直接 SetPosition，125%/150% 缩放屏上弹窗会画到偏右下位置。
  5. **`CreateThreadTimer` 直接把进程打崩，两轮才对**：它走的是内核 APC——初版按 `SetTimer` 参数序写 `Call(0, ms, 0)`，把毫秒数当 APC 函数指针，到期内核跳地址 0x1C2，秒崩且 Go 无栈可 recover；改成正序 `Call(ms, NULL, NULL)` 后仍然崩——**它的回调不是 optional**（"NULL→投 WM_TIMER"是 SetTimer 的行为，两者语义不同），内核 APC 到地址 0 照样 AV。最终弃用：`time.AfterFunc` 到期只 `PostThreadMessage` 回泵线程，零内核 APC。
  6. **`SendInput` 参数序错误 = "短按回放从未注入成功"的真凶**：签名是 `(cInputs, pInputs, cbSize)`，初版写成 `(count, size, ptr)`——cbSize 与指针互换，连纯 MOVE 都返回 0 / `ERROR_INVALID_PARAMETER`。这个返回值一直被我丢弃，直到探针测试才暴露。此前"XAML 托盘拒收合成回放"只是为解释现象编的假说，真凶就是参数序；教训：**Win32 胶水调用的返回值必须当场检查并计数，不要为失败现象编机制故事**。
  7. **任务栏区域判定**：`WindowFromPoint` 在 Win11 XAML 任务栏整带上直接返回 0（连 DPI 感知修正后也如此），基于命中测试的旁路永远不生效；改用 `FindWindowW("Shell_TrayWnd"/"Shell_SecondaryTrayWnd"/"TopLevelWindowForOverflowXamlIsland") + GetWindowRect 矩形相交`——纯查询、每手势一次、物理坐标与钩子 pt 同系。
  8. **（真机反转，代价最大的一课）初版"吞起不吞按"策略把系统输入卡死**：以为"按下放行、抬起才判长按、长按才吞 `WM_RBUTTONUP`"是最保守的拦截点——真机反馈却是"弹窗 1-2s 才能操作、弹窗期间其它窗口全被卡、点桌面菜单也不消失"。根因：按下已放行，系统与前后台应用的**异步按键状态已记为"右键按着"**，吞掉抬起后这个状态永远不会自己清零——前台切换被 Windows 拒绝（按键按住时不允许 SetForegroundWindow），弹窗抢不到焦点故 `LostFocus` 永不触发（点外部不收起的直接原因），被吞那一下的宿主应用等不到抬起，输入处理挂起。
- **排查过程**：grep `x/sys/windows` 符号表确认缺口→按仓内既有惯例（`syscall.NewLazyDLL("user32.dll")`）就近声明常量与结构体；读 `webview_window_windows.go` 的 `setPosition/setBounds` 确认 DIP 语义与 `Windows.HiddenOnTaskbar` 应用点；读 `webview_window.go` 确认无公开销毁 API（与 ddns 面板窗口"永不销毁、隐藏驻留"既有决策互相印证）。真机卡死后复盘推理：症状三连（其它窗口卡 + 抢不到焦点 + 失焦不收起）同时成立，唯一能串起三者的解释就是"全局按键状态停在按下"——于是拦截点从抬起侧整体翻转到按下侧。
- **正确做法与标准修复方案**：钩子态只存在安装线程（`atomic.Pointer` 装载，回调零锁零阻塞零分配，触发经容量 1 通道非阻塞投递，满则丢弃）；**拦截点必须在按下侧——"吞按 + SendInput 回放 + 按住即弹"**：`WM_RBUTTONDOWN` 一律吞掉（按键状态永不脏）并同时 `time.AfterFunc` 起阈值表；按住到阈值（松手前）即在泵线程投触发弹窗并标记 suppressed，随后抬手吞净（应用全程未见右键，干净）；阈值前抬手→吞起并向泵 `PostThreadMessage` 投递请求，由泵 `SendInput(cInputs, pInputs, cbSize)` 回放一组合成 down+up（普通右键照常弹菜单，回调内零重入）；位移超容差（右键拖拽）→立刻补回放 down、后续移动/抬起原样放行让应用收尾拖拽。**自家回放事件在 `dwExtraInfo` 盖魔术值、回调据此精确放行**（比"逢 `LLMHF_INJECTED` 即放行"更强：外部自动化的注入右键仍与真实按键同权参与判定，自家事件绝不自我吞入）；uintptr→结构体用"取局部参数地址再解引用"惯用法 `*(**T)(unsafe.Pointer(&lParam))` 消解 vet；弹窗常驻 `Hidden:true` 创建、失焦 Cancel+Hide 复用，并加两重保险：`SetForegroundForce`（AttachThreadInput 借前台特权抢焦点，绕 ForegroundLockTimeout）+ 全局按键观察通道做"点击弹窗物理矩形之外即收起"的兜底（焦点方案失效时依然成立）；定位走 `Screen.PhysicalToDipPoint` + `ScreenNearestPhysicalPoint(...).WorkArea` 钳位。
- **避坑防重犯建议**：
  1. 缝 Win32 消息机制进 Go 时先跑 `go vet`——unsafeptr 惯用法要第一时间固化，别等 review 才发现；
  2. beta.10 无公开窗口销毁 API：所有子窗口按"隐藏驻留复用"设计（省 WebView2 重建、也绕开最后窗口退出策略），别指望 OnDestroy 里释放；
  3. 涉及屏幕坐标的一切换算显式标注物理/DIP，多显示器混 DPI 下用物理像素 API（钩子、GetWindowRect）与 Wails DIP API 之间只允许经 ScreenManager 单点转换；
  4. **键鼠识别类钩子只能吞"按下"、短动作靠 SendInput 回放，永远不要吞"抬起"**——按键状态机一旦被拦断在中间态，受害的是全系统而不是自己；这是本轮真机付出最大代价换来的一条；
  5. **待真机确认项（升级）**：中完整性进程对 elevated 前台窗口的钩子感知/拦截未实证；且"吞按回放"在 elevated 界面上会因 UIPI 拒绝注入而**丢这一下右键**（比初版"卡按键状态"体面，但仍是功能损失），需要时以提权运行 Hanxi 验证后再定策略。

### 30. 安装版（MSI 系统安装）纳管：魔数错配拒好包、同镜像服务误判与 spec mock 缺方法团灭

rustdesk/subnetdesk 增加"下载安装版"通道（MSI perMachine + Windows 服务，解锁无人值守/锁屏被控），三个环节与便携版直觉相反：

- **问题现象与错误原因**：
  1. **MSI 不是 PE**：便携链路的 `verifyPEMagic`（MZ 头断言）若被顺手复用到 MSI 校验，会把所有好包全部误拒——MSI 是 OLE 复合文档，签名 `D0 CF 11 E0`；
  2. **服务与客户端同镜像**：安装版服务 binPath 就是 `"Program Files\X\X.exe" --service`（上游 WiX `CreateStartService` 自定义操作实证），便携时代"探测只认提取目录、与安装版两清"的契约在纳管安装版后反转——按目录前缀识别实例会把 LocalSystem 服务一并数进去，"服务常驻"被误判为"实例运行中"；
  3. **卸载注册表双键 + 四段版本**：真机在 `HKLM\...\Uninstall` 同时命中产品名遗留键与 `{ProductCode}` GUID 键（均 DisplayName=产品名），GUID 键反而无 `InstallLocation`；`DisplayVersion` 为四段 `1.4.9.29722256`，直接进 semver 比较必炸；
  4. **前端特征测试团灭**：service 新增 RPC（`GetActiveForm` 等）后 spec 的 `svc` mock 对象没同步，视图 `loadVersions` 里同步 TypeError 被 catch 吞成 listError，installed/releases 双双为空——8 个断言失败全是症状，病根只有一个 mock。
- **排查过程**：上游 WiX 工程源码实证（`res/msi`：`Scope="perMachine"`、File 元素 `$(var.Product).exe`、服务名=产品名、`preprocess.py` 的 UpgradeCode 派生与卸载键写入）+ 真机装双产品交叉核对（`Get-ItemProperty` 双键、`sc qc` binPath、`Get-Process`/`Get-CimInstance` 可见性对照）；前端侧以 vitest 复现，8 个失败收敛到 mock 缺一个方法。
- **正确做法与标准修复方案**：魔数断言按形态分家（`verifyPEMagic` 只管便携 exe，`verifyMSIMagic` 独立）；进程身份裁决 = "镜像路径**可读**且命中（提取目录 ∪ 安装目录）前缀"，服务与其派生 broker 的排除靠非提权下 `OpenProcess` 失败这一权限边界（真机实证 Session 0 / SYSTEM 进程路径不可读），代码与注释都不许把裁决建立在"路径可读"之上——**若 Hanxi 将来提权运行，须补 Session/token 判别**；注册表探测只认确定证据（InstallLocation 非空且 exe 实存），版本截前三段、畸形退 PE 版本资源再退"未安装"；`activeVersion`/`activeForm` 成对落盘（旧配置缺 form 按 portable 兼容）；安装版卸载不代执行 `msiexec /x`，引导 `ms-settings:appsfeatures`。
- **避坑防重犯建议**：
  1. 写"二进制魔数断言"前先确认格式真实签名——PE(MZ)、OLE/MSI(D0CF11E0)、zip(PK) 是三套，别拿"校验可执行体"的名义跨形态复用；
  2. 同镜像多身份（客户端/服务/broker）场景先定义判别维度（路径前缀 + 可读性 + session/token）并逐条真机实证，别沿用上一形态"天然两清"的侥幸假设；
  3. 给 service 加/改 RPC 时，同批检查：bindings 再生成 + 特征测试 `svc` mock 补方法——mock 缺方法比断言写错更毒，它会拖垮整个 `Promise.all`；
  4. 系统级形态（装机、服务、卸载）纳管的深度边界要显式写死：Hanxi 只做"取包校验 + 发起向导 + 探测拉起"，服务生命周期与卸载交还系统，页面文案如实标注。

### 31. 托管 VS Code：官方 CDN 单源分发 + 哈希仅最新版可得 + 互斥体按安装形态分治 + Inno 强关确认闸

VS Code 双形态托管（便携 zip + User Installer 静默安装）集成，六个契约与既有模板直觉相反（全部实证于上游 1.136.1）：

- **问题现象与错误原因**：
  1. **GitHub releases 无二进制**：microsoft/vscode 的 release 只挂源码归档，照 ccswitch 模板走 GitHub API digest 路线会整层落空——二进制只经官方 CDN（update.code.visualstudio.com → vscode.download.prss.microsoft.com）分发；
  2. **官方 sha256 只给最新版**：更新清单端点 `/api/update/{platform}/stable/{任意旧commit}` 恒返回**最新版** manifest（`sha256hash`），历史版本无任何官方哈希可查——"四层完整性校验"对旧版不可达，必须显式降级三层并在 UI 如实标注（meta.json 记 `verifiedHash`）；
  3. **便携 zip 无根目录**：`Code.exe` 直接躺在 zip 根部，Electron 运行时目录名 = **commit 前 10 位**（随版本漂移，如 `a44adf7f53/resources/app/product.json`）——布局自检若照"包内单根目录"直觉或写死路径，解压即误判损坏；
  4. **`"vscode"` 命名互斥体只属于安装版**：源码 `app.ts installMutex()` 有 `isInnoSetupInstall()` 门控，便携版（data\ 自包含）运行实例**不创建**互斥体——便携版拿 OpenMutex 探测永远查无此人；
  5. **Inno user 安装器的 `CloseApplications=force`**：静默升级会**强杀全部运行中安装版实例**（包括用户日常使用、可能有未保存内容的窗口）；且确认闸若不带 `confirm` 参数，用户确认后重入会被同一道闸再拦——死锁；
  6. **安装位置以注册表为准**：实测本机用户把安装版装到 `E:\Program Files\Microsoft VS Code`（自定义目录），硬编码 `%LOCALAPPDATA%\Programs\Microsoft VS Code` 必漏——须读 `HKCU\...\Uninstall\{771FD6B0-…}_is1\InstallLocation`（AppId GUID 取自发行版 product.json）。
- **排查过程**：官方 API 逐端点 curl 实测（旧版 `/api/releases/stable` 列表、版本化 302 直链含 commit、feed JSON 三平台 200 样例）；zip 中央目录经 **HTTP Range 拉尾部 512KB 离线解析**（不下载 332MB 全量即拿到全部条目布局，含 `*/resources/app/product.json` 与 CRC）；product.json 从 CDN zip 内 range+inflateRaw 直读；`build/win32/code.iss` 与 `src/vs/code/electron-main/app.ts` 源码实证安装器参数、AppMutex 与单实例锁归属；真机 OpenMutex("vscode") 与 `reg query` 双验证。
- **正确做法与标准修复方案**：远程列表 = 官方版本列表 + 逐版本 HEAD 重定向解析直链/大小/commit（并发限 4、10min 缓存、单项失败仅剔除）；最新版哈希用"列表第二项 commit 调 feed"补齐；探测分治——安装版 `OpenMutexW("vscode")`、便携版 `Code.exe 进程镜像路径 ∈ 托管 versions 前缀`（QueryFullProcessImageNameW，目录边界相等比较）；唤窗统一走 Electron 单实例信使（二次无参拉起转发后自退，**信使不 Wait 不进 Job**，markeron 先例），portable 与安装版 user-data 不同 → 实例组天然隔离可并行；解压后自动补建 `data\`（官方便携激活器），service 启动前 `EnsureDataDir` 幂等兜底（data 缺失会把配置写回 %APPDATA% 破坏隔离）；安装版 RPC `DownloadVersion(v, form, confirm)`：命中运行实例且未确认 → 返回 `confirm-required` 由前端全局确认框放行后带 `confirm=true` 重入；安装器静默参数固定 `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /MERGETASKS=!runcode,!addtopath,!associatewithfiles,...`（addtopath/文件关联默认勾选必须 ! 掉——托管不越权改 PATH 与关联）；VS Code 无托盘驻留、关窗即退，**不设空闲自动退出**（窗口开着 = 用户正在编辑，3 分钟强退反需求）。
- **避坑防重犯建议**：
  1. 集成前必查"GitHub releases 是否真的有二进制资产"——知名大厂项目（VS Code、.NET 工具链等）常把二进制留在自有 CDN，release 页只剩源码；侦查以 `expanded_assets` HTML 为准，别只看 release tag 数量；
  2. 大 zip 布局侦查用 Range + 中央目录离线解析（尾部几百 KB），一次下载全量 332MB 是浪费也是限流风险；
  3. 互斥体探测前先在发行版 product.json / 安装脚本里找**名字来源与创建条件**，"存在性=存活"的前提是"该形态必然持有"；
  4. 一切"安装器会强关运行实例"的静默通道必须配显式确认闸，且确认闸要设计成**可重入**（confirm 参数），否则用户确认后永远出不去；
  5. Inno 用户安装器的安装目录以注册表 InstallLocation 为唯一事实，%LOCALAPPDATA% 只是默认值；
  6. 官方哈希"仅最新版"的上游属实时，降级路径（三层）与标注（UI 徽章 + meta.verifiedHash）要一次做全，让老版本的完整性状态可审计。

### 32. 托管 TranslucentTB：无设置窗口信使语义不是唤窗 + 通用窗口类名须验属主 + 便携版 Win11/框架包双重前提

TranslucentTB（任务栏透明工具，C++/WinRT + 注入 explorer 的 TAP/Hooks）纯托管集成，契约与 tauri/electron 模板直觉相反（全部实证于上游 release 分支源码与 2026.2 便携 exe 二进制）：

- **问题现象与错误原因**：
  1. **根本没有可唤的设置窗口**：上游全部设置 UI 在系统托盘 XAML 飞控（`MainAppWindow` 就是托盘消息窗口）。二次拉起"信使"的真实效果是给运行实例投 `TTB_NewInstanceStarted` → 弹"已在运行"气泡 + `ResetState(true)`，**不唤任何窗**——照 ccswitch 模板做「打开窗口」按钮等于交付假功能；
  2. **`TrayWindow` 是极通用窗口类名**（互斥体也是裸 GUID 名 = Local 命名空间）：`FindWindowW` 命中不能证明属主是 TranslucentTB，盲目 PostMessage WM_CLOSE 可能误伤他杀；
  3. **便携版有双重系统前提**：README 明言仅支持 Windows 11；且 unpackaged WinUI 经 `TryCreatePackageDependency` 解析系统已装的 `Microsoft.UI.Xaml.2.8` / VCLibs 框架包——机器缺包时上游弹模态提示后 `ExitProcess(1)`。托管失败文案不预告这两条，用户首撞无从自查；
  4. **settings.json 在 exe 同目录**（`DetermineConfigPath` unpackaged 分支）：与 ccswitch 的 `~/.cc-switch` 用户目录相反——卸载版本连带删用户配置，「导入本地」必须整套搬（exe+4 DLL+resources.pri+Assets+settings.json）；
  5. **首启欢迎授权窗口被关闭 = 退出码 1 的正常路径**（`Shutdown()` 删未确认配置 + `PostQuitMessage(1)`），wait() 分类若不区分成因，failed 文案会把"没同意许可"误诊成"依赖坏了"；
  6. **GitHub digest 仅覆盖 2026.1+**：2025.1 及更早 release 无 digest 字段——按 ccswitch"无 digest 不入库"硬口径远程列表只剩新版，这是合规取舍而非 bug。
- **排查过程**：`main.cpp`（互斥体创建与信使分支）、`mainappwindow.cpp`（WM_CLOSE→Exit、`PostNewInstanceNotification` 与 `TRAY_WINDOW` 定位）、`Common/constants.hpp`（MUTEX_GUID `344635E9-…`/`TTB_NewInstanceStarted`）、`Common/appinfo.hpp`（APP_NAME 稳定构建 = `TranslucentTB`）、`uwp.cpp`+`configmanager.cpp`（storage nullopt → 配置同目录）、`dynamicdependency.cpp`（框架包解析失败即弹框 ExitProcess）逐文件源码实证；再下载 2026.2 portable-x64.zip（sha256 与 API digest 一致）确认扁平布局（无 Dependencies 目录=依赖系统包）并在 exe 二进制里 UTF-16 串扫出四常量与 FileVersion `2026.2.0.d4636e4`；releases API 实测 digest 覆盖范围。
- **正确做法与标准修复方案**：探测 `OpenMutexW("344635E9-9AE4-4E60-B128-D53E25AB70A7", SYNCHRONIZE)`（互斥体先于一切 UI 创建，WaitReady 不受欢迎窗阻塞）；退出 = `FindWindowW("TrayWindow","TranslucentTB")` 命中后 `GetWindowThreadProcessId`+`QueryFullProcessImageNameW` 验属主映像名再 PostMessage WM_CLOSE（上游 Exit=SaveConfig+优雅收尾）→ 宽限 → JobObject 强杀兜底；不设 OpenWindow，RPC 面为 `Start/ResetState/Quit/OpenDir`，`ResetState` 即信使拉起（不 Wait 不进 Job），stopped 态明确报错**不**伪装成冷启动；failed 文案同时预告欢迎窗关闭与框架包缺失两种成因；版本 tag 过滤 `^\d{4}\.\d+$` + `-portable-x64.zip` 后缀 + 64-hex digest 必备三重；导入 PE FileVersion 归一化 `2026.2.0.<sha>→2026.2`（非规范退化 imported-时间戳）；禁用空闲自动退出（常驻特效工具，退出=特效消失，同 rustdesk 口径）。
- **避坑防重犯建议**：
  1. 套信使唤窗前必须先查"接收通知的回调干了什么"——信使 ≠ 唤窗，按钮语义按上游实际行为命名；
  2. 通用窗口类名（TrayWindow 这类）投递消息前必验属主进程映像名，FindWindow 裸命中不可信；
  3. unpackaged WinUI/WinRT 应用先看 zip 有无 Dependencies 目录：没有 = 依赖系统预装框架包，失败文案与页面引导行都要继承该前提；
  4. 配置落点看 `DetermineConfigPath` 的 unpackaged 分支再下结论——"配置随目录"会让卸载语义与导入范围完全不同，卸载确认框必须预告连配置一起删；
  5. 上游"未同意许可即退出码 1"这类**把正常路径当异常**的场最隐蔽：wait() 文案要把可辨识成因并列预告，别让用户对着依赖提示排查授权问题。

### 33. 前端深色主题：未迁移视图的 scoped 原子副本携带裸色值，只在深色下暴露

设置页切到深色后，「托盘右键菜单」区块的 ↑ / ↓ / ✕ 移除 / 📂 浏览… 按钮整块泛白、文字几乎不可读（实际影响全页 `.btn-secondary`：四处「打开目录」、「打开管理 / 打开设置 / 前台卡片」同病）。

- **问题现象与错误原因**：`SettingsView.vue` 的 `<style scoped>` 里 `.btn-secondary { background: #fff; color: var(--color-text); }` 把底色写成了裸色值。深色主题下 `--color-text` 是 `#e8f2f3`（近白），于是白底 + 近白文字 = 按钮糊成一块白。它盖掉了全局标准形 `components.css` 的 `:where(.btn-secondary) { background: var(--surface-panel); }`。
  **为什么能盖掉、又为什么长期没人看见，是两层设计叠加的结果**：
  1. 全局原子层刻意全部写成 `:where()` 零特异性（见 `components.css` 头部注释——目的是"全局层落地当天所有旧视图视觉零变化"），而 scoped 样式自带 `[data-v-*]` 属性选择器，特异性 ≥ 0,1,0，**永远压过全局**。副作用即：未迁移视图里的历史硬编码值一直生效，改全局 / 改 token 都修不动它；
  2. 浅色主题下 `--surface-panel` 恰好就是 `#ffffff`，裸色与 token **逐字同值**，所以浅色走查永远"看起来没问题"。而 §7.2 明确"深色是独立标定、不是简单反色"，浅色通过对外推无效——这个 bug 只在深色下现身。
- **排查过程**：devtools 选中按钮看 computed `background` 与**命中规则的来源**，发现生效的是带 `[data-v-*]` 的 scoped 规则而非 `:where(.btn-secondary)` → 一眼定位到"视图本地副本"而非全局层坏了。随后全仓分诊同类：`grep` 视图 / 组件 `<style>` 块内的裸 hex / `rgb()` / `#fff`，逐个判定「装饰色违规」还是「功能色例外」。
  **例外白名单（有意不随主题，勿误伤，均自带注释自证）**：`WifiView` 二维码码面 `#ffffff` 与 qrcode 深墨/纯白双色、`useFileShareServer` 二维码对比度参数（扫码器不认主题色）、`WechatBotChat*` 微信品牌绿 `#07c160` / `#95ec69` 及气泡中性覆层、`MemoView` 用户可选标签五色（数据值）。扫下来**全仓真正的深色地雷只有 SettingsView 这一处**，其余皆为主题无关的功能色或数据色。
  同批清掉同型第二处：`.toast { background: var(--color-text); color: #fff; }`——模板从未引用（提示条早已收编进全局 `NotificationToast`），且它引用的 `@keyframes fadeIn` 在本块也不存在，是纯死代码；`LanScannerView` 与 `PortKillView` 此前已按同一口径清除并留注释，SettingsView 是最后一处残留。
- **正确做法与标准修复方案**：
  1. 把本地副本的裸色改回语义 token：`.btn-secondary { background: var(--surface-panel); }`，与全局标准形逐字对齐。浅色下值完全相同（零视觉变化），深色下落到 `#0e1c22`，压在 `--surface-page: #071318` 的条目行上层次与描边都正常；
  2. 删除死代码 `.toast` 副本，就地留注释说明"模板从未引用 + 同型地雷"，沿用 LanScanner / PortKill 的清理口径；
  3. 在该 `.btn` 族副本上方补注释写死两条纪律：本视图副本即最终生效值、只准引用 token；以及下面这条**不可拆族半迁移**的陷阱；
  4. （另开 refactor，不混入 fix）整族迁移回全局原子。看似"本地 `.btn-secondary` 改完 token 后与全局逐字一致，可以顺手删"——**不行**：本地 `.btn` 的 `border: 1px solid transparent` 是 shorthand（特异性 ≥ 0,1,0），层叠按长写属性逐条比较，它会盖掉全局 `:where(.btn-secondary)` 的 `border-color: var(--color-border)`（0,0,0）。只删 `.btn-secondary` 而留 `.btn`，全页次级按钮描边会**静默消失**——比原 bug 更隐蔽。整族一起删才落得回全局标准形；代价是可见尺寸归一：全局 `.btn` 有 `min-height: 36px` 与 `border-radius: var(--radius-control)`（8px），本地是 6px 且无 min-height（按 padding 6×2 + border 1×2 + 13px 行高推算实际约 32–33px，**本就低于 §7.3 无障碍"桌面按钮 ≥36–38px"底线**），`.btn-small` padding 也从 4px 10px 变 4px 12px。迁移顺带修掉尺寸越线，但必须目测双主题验收。
  验证：`npx vitest run src/views/__tests__/SettingsView.spec.ts` 4 例通过；改动纯 CSS，不影响 `vue-tsc`。
- **避坑防重犯建议**：
  1. **凡触碰 `<style>`、token 或主题相关代码，浅 / 深双主题各走查一遍才算完工**——"深色独立标定"意味着浅色通过不能外推，反之亦然；
  2. 裸 hex 只允许出现在 `tokens.css`（§7.1 硬约束）。视图若必须保留与全局**同名但有意不同形**的本地副本（范例：`EnvCheckView` 的描边强调变体），必须 token 化 + 注释写明"非全局实底语义"，禁止裸值；
  3. 排查"深色下某处颜色不对"的第一动作是查**谁在盖全局原子**（devtools 命中规则看是否带 `[data-v-*]`），而不是去改 `components.css` / `tokens.css`——零特异性层修不动 scoped 副本，硬改还会波及其他已迁移视图；
  4. 原子族迁移要**整族迁或不迁**，半删一族 = 引入静默失效；
  5. 建议补一道机器闸门：加个单测 / CI 步骤 grep 视图与组件 `<style>` 块内的裸 hex / `rgb(` / `#fff`，白名单只放 `tokens.css` 与带"功能例外"注释的行。§7.1 目前是纯人审约束，本轮是它的一次实证漏网。

### 34. WSL 就绪模块：wsl.exe 的 UTF-16 解码不能用 NUL 密度启发式，中文提示行会同时打穿解码与守卫

`wsl -l -v` 在"已装 WSL 但零发行版"的真机输出（中文 Win11）被当成发行版行进表，且状态列乱码——一次故障两层成因：

- **问题现象与错误原因**：
  1. **NUL 密度阈值失判**：wsl.exe 输出为无 BOM UTF-16LE。初版解码用"NUL 占比 ≥ 1/3 判 UTF-16"，但 CJK 码位（U+4E00–U+9FFF）高字节永远非零——实机样本 270 字节仅 79 个 NUL（29%），整段被误判为单字节码流；
  2. **守卫串被 NUL 打散**：误判产物里每个 ASCII 字符后跟着隐形 `\x00`（渲染不可见），`strings.Contains(line, "wsl.exe")` 这类关键词守卫全部漏检，帮助行穿过守卫、按 2+ 空格切列、堂而皇之成为表行——**编码失判会顺带击穿所有下游字符串防线**。
- **排查过程**：`cmd /c wsl -l -v > out.bin` 抓原始字节（绕开 PowerShell 管道的自动转码），十六进制画像确认无 BOM UTF-16LE 与 29% NUL 占比；再以真实字节文件跑一次性取证单测验证修复后的解码与拒收。
- **正确做法与标准修复方案**：判据换成强不变量——UTF-16 文本的 `\r\n` 必产生 NUL（`0D 00 0A 00`），而 UTF-8/GBK 字节流永不含 `0x00`，故"含 NUL 且偶数长度即 UTF-16"；解析器再加结构约束双保险：表尾列必须是 `1`/`2`（帮助行绝无此形态），关键词守卫从唯一防线降级为第一道防线。
- **避坑防重犯建议**：
  1. 对"输出编码混杂"的原生命令行（wsl.exe、老式 Win32 工具），解码判据优先选**编码的结构性不变量**（NUL 存在性、BOM、偶数长度），不要用比例阈值——语言与文案配比会漂移；
  2. 修解码 bug 必须拿**用户实机原始字节**回归，合成样本（ASCII 为主）恰好全绿而真机（CJK 为主）翻车，就是本坑画像；
  3. 下游任何"按内容关键字拒收"的守卫，都要问一句：如果上游解码错了，这个关键字还完整吗？——答案通常是"不"，所以文本型解析要配结构型断言（列数、末列取值域）兜底。

### 35. Hanxi 内 Go 网络请求"浏览器能上、程序 403"：系统代理不进环境变量，api.github.com 对云出口 IP 区域性拦截

WSL 模块用户报障：开着代理、浏览器逛 GitHub 一切正常，但版本列表与通道探测始终 HTTP 403。两层成因叠加：

- **问题现象与错误原因**：
  1. **代理传导断层**：主流代理客户端（Clash/v2rayN）默认只写 WinINET 系统代理（注册表 `Internet Settings\ProxyEnable/ProxyServer`），浏览器跟随；Go 标准库 `http.ProxyFromEnvironment` **只认环境变量**（HTTPS_PROXY 等）——环境变量没设，Go 就裸奔直连，系统代理形同不存在；
  2. **api.github.com 独立拦截**：直连被墙只是表象之一——GitHub 对部分云厂商 IP 段（实测新加坡 AWS 出口）直接拒绝 `api.github.com`（403），而 `github.com` 网站域不拦。"浏览器能开 github.com"证明不了 API 可达，两个域名的封禁策略是两套。
- **排查过程**：读注册表确认代理形态（`ProxyEnable=1 / ProxyServer=127.0.0.1:7897`，环境变量表无 PROXY 项）→ PowerShell 分别以直连与显式 `-Proxy` 打 API 域，双双 403（证明代理兜底该做但做完仍不够）→ 经代理查 `api.ipify.org` 得出口 IP 归属云段 → 实测 `github.com/.../releases.atom` 与文件下载直链 200 畅通，锁定可用的救生通道。
- **正确做法与标准修复方案**：新建 `wsl/netx` 叶子包（避免 readiness/releases 与根包成环），客户端代理链 = **环境变量优先 → WinINET 系统代理兜底 → 直连**；发布列表改双源：REST API 首选，403/失败自动降级 `releases.atom` 订阅源（tag/日期取真值，MSI 文件名按官方规律 `wsl.<四段版本>.<arch>.msi` 合成，Size 未知置 0 前端渲染"—"，绝不编数字）。前端 Overview.fallback 时如实挂"订阅源降级"标注，错误文案区分"API 被拦"与"网络全挂"。
- **避坑防重犯建议**：
  1. Windows 桌面应用内任何 Go/原生 HTTP 请求，默认都要想一遍"用户浏览器的代理我走不走"——需要与浏览器同进退就实现 WinINET 注册表回落（PAC 形态不猜、如实直连）；
  2. 诊断 GitHub 可达性时把 `api.github.com` 与 `github.com` 当两个独立探针（本模块体检项已是双探），单测 github.com 通而断言 API 可用是错误推理；
  3. 降级数据源要逐项核字段可得性（Atom 无资产清单/预发布标记/大小），缺什么显示什么，命名规律合成 URL 必须实测直链 200 后才算立住；
  4. 代理链代码拆纯函数（chainProxy 注入 env/sys 两路）单测四态：env 命中 / env 空系统代理兜底 / 双空直连 / 系统代理畸形值安全兜底——`http.ProxyFromEnvironment` 按进程缓存环境，测试别试图在进程内改环境变量验证链路。

### 36. 「虚拟机平台开着没」：HypervisorPresent=true 会骗人——内核隔离也造监控程序，功能开关的免管理员判据要靠服务存在性并做 DISM 基准校准

WSL2 模块要做虚拟机平台状态查看与开关，先被"现成信号"坑了一轮：

- **问题现象与错误原因**：
  1. **HypervisorPresent ≠ 虚拟机平台已启用**：Win11 内核隔离（HVCI/VBS）默认开启时系统同样跑着虚拟机监控程序——实机基准对照：两个功能 DISM 读到"已禁用"，而 `Win32_ComputerSystem.HypervisorPresent` 依旧 true。拿它当"平台开着"的证据会把"发行版根本起不来"的机器误判成就绪；
  2. **候选信号接连扑空**：CBS 包注册表（`Component Based Servicing\Packages\HyperV-Feature-VirtualMachinePlatform-*`）键值 InstallState 读不到且 FOD 包键常驻（连 "Disabled-FOD-Package-Wrapper" 都在），存在性≠启用；`HvHost` 服务任何现代 Windows 都有（Start=3 不随功能变）；`bcdedit /enum` 连查询都要管理员。
- **排查过程**：先枚举候选信号做免管理员实测（注册表三路 + 服务四项），无一定论；提权跑一次 `Get-WindowsOptionalFeature`/DISM 拿地面真值（双功能=已禁用），反推与服务画像对照——`vmcompute` 服务恰随虚拟机平台安装/卸载（WSL2 建 VM 的硬依赖，缺席与 DISM 结论吻合），`LxssManager` 同理对应旧版 WSL 功能。
- **正确做法与标准修复方案**：功能开关状态用 **`Win32_OptionalFeature` WMI（`InstallState` 1=启用 / 2=禁用，免管理员可读，与 DISM 真值逐项对照校准）**——服务存在性启发已被家庭版证伪（`vmcompute` 属 Hyper-V 全套件，家庭版启用虚拟机平台/旧版 WSL 均不产生对应服务，拿"服务缺席"反推"功能禁用"会全线误伤）。"已启用但未生效"另用 CBS 官方台账（`Component Based Servicing\RebootPending` 键）单独判定；DISM 提权通道保留作写操作（enable/disable 均 /norestart，重启生效由引导条提示）；体检项对"平台未启用"定级 warn 而非 info——内核隔离制造的"监控程序在运行"假象必须被结论条点名，文案明说"HVCI 不能替代本功能"。
- **避坑防重犯建议**：
  1. Windows 功能状态判断优先取该功能的**权威台账**（WMI `Win32_OptionalFeature`、DISM 输出——都是服务栈自己维护的），而非**附属产物**（服务/驱动存在性）：同一功能在不同 SKU（家庭/专业/企业）装出的附属件并不一样，产物缺席不等于功能缺席（vmcompute 在家庭版缺席即本例实证）；对 HypervisorPresent、VBS 状态这类"多来源共用"字段，永远问一句"还有谁会点亮它"（内核隔离、Hyper-V、WSL、沙盒都可能）；
  2. 新加系统状态判据必须找一条**提权侧真值通道**做一次基准对照（跑一次 DISM/Get-WindowsOptionalFeature），别拿单信号直接上生产；
  3. 需要管理员才能读的命令（bcdedit、dism）不做常规探针——免管理员链路里出现它们，等于给每次体检插一次 UAC。

### 37. 「发行版怎么装不上还报成功」：`Start-Process -Wait` 从不回传子进程退出码——裸提权通道的假成功是 #36 分号链的同族病灶

WSL2 模块一键开机会话之后，用户在版本页点发行版"⬇ 安装"，多次点击全部回执"操作已执行完毕"，但 `wsl -l -v` 始终零发行版：

- **问题现象与错误原因**：
  1. **假成功的根因在提权通道本身**：`runElevatedProcess` 用 `Start-Process -Verb RunAs -Wait` 执行 `wsl.exe --install -d <id>`，但 **PowerShell 的 `Start-Process` 从不设置 `$LASTEXITCODE`**——无论目标程序以何种退出码结束，宿主 powershell 都以 0 退出。于是提权窗口里 `wsl` 已红字报错退出，Go 侧 `CombinedOutput()` 拿到的却是退出码 0，判成功。这与 #36 的 `dism; dism` 分号链吞码是同一病灶——当时只给 DISM 链补了 `if ($LASTEXITCODE -ne 0){exit}`，**裸提权通道这条一直漏着**；
  2. **为什么注定失败**：本机虚拟机平台处于"已启用、待重启生效"（WMI `InstallState=1` 但 CBS `RebootPending` 在账），`wsl --status` 原话"WSL2 无法启动，因为此计算机上未启用虚拟化"——WSL2 起不了虚拟机，任何发行版安装都在首次拉起环节崩，只是崩了被假成功吞掉看不见。
- **排查过程**：`wsl --status`/`wsl -l -v` 重定向到文件按 UTF-16 解码读原文（#34 口径），锁定"未启用虚拟化 + 零发行版 + RebootPending=True"三态；顺 `InstallDistro` → `elevateWsl` → `runElevatedProcess` 回溯，坐实 `Start-Process` 无 `-PassThru` 则拿不到子退出码。
- **正确做法与标准修复方案**：
  1. **总闸修复**：`runElevatedProcess` 改 `-PassThru` 拿进程对象并 `if ($null -ne $p -and $p.ExitCode -ne 0) { exit $p.ExitCode }` 显式传播，Go 侧 `errors.As` 捕 `*exec.ExitError` 点名退出码（提权窗口保持可见以展示进度与红字详情）；抽成纯函数 `buildElevatedPS` 上形状回归锁。
  2. **前置拦截**：`InstallDistro` 在提权之前加 `virtualizationGate`——探针 `FeatureVM=false`（平台没启用）或 `FeatureVM=true && RebootPending=true`（启用但没重启加载）时，直接返回指路回执、根本不弹 UAC（探针自身不可得时保守放行，由退出码通道兜底）；前端版本页 `distroBlockedReason` 在清单上方挂黄色警告条并禁用安装按钮，**点之前就讲清**。
  3. 用户侧动作：还掉欠的那次重启，重启后重新体检，`RebootPending` 归零、gate 放行，发行版即可正常安装。
- **避坑防重犯建议**：
  1. **凡用 `Start-Process` 判断成败，一律 `-PassThru` + 手动传播 `$p.ExitCode`**——`-Wait` 只保证时序不保证回码，这是 PowerShell 反直觉的头号陷阱；写提权执行器时把"回码从哪来"当作第一性问题，别让"命令跑完了"偷换成"命令成功了"；
  2. **修一类 bug 要扫全同类通道**：#36 修了 DISM 分号链吞码却没修裸 `wsl.exe` 提权链，同族假成功隔了一轮又炸——落地一个"退出码传播"缺陷时，横向 grep 所有 `Start-Process`/子进程封装，一次性补齐；
  3. **有确定性失败前提的操作先设闸门再弹 UAC**：明知"待重启时装必失败"，就该在提权前拦下并指路，而不是让用户对着闪退的黑窗口反复点"成功"。事前禁用 + 事中拦截 + 事后如实回码，三层缺一不可。

### 38. 托管工具可靠性：路径词法校验、goroutine 外层互斥与取消请求都不是生命周期边界

- **问题现象与错误原因**：托管模块在正常路径可用，但异常输入或快速重复操作会暴露三类同源问题：① `filepath.Join/Rel` 只能防字符串穿越，不能阻止共享树内 junction/symlink 指向根外；② service 在启动 goroutine 前加锁，RPC 返回就解锁，实际下载仍可重复写同一安装目录；③取消旧 context 或清 timer 后，已经在途的请求仍可能晚返回并覆盖新状态。进程实例若让旧 `wait()` 从可变共享 `cmd/job` 取资源，还会回收新运行代。
- **排查过程**：从最终副作用反向追踪——递归删除、文件打开、安装目录提交、状态事件写回、JobObject 关闭——检查“校验/锁/取消”是否一直覆盖到该副作用发生。用临时根目录外哨兵文件、延迟 Promise/通道、重复 Start/Download 和旧轮次晚返回构造确定性回归，而不是靠人工快速点击。
- **正确做法与标准修复方案**：文件共享全链使用 Go 1.26 `os.Root`，列表、下载、上传临时文件、发布和清理不得在校验后退回裸绝对路径 API；下载在途登记必须活到后台任务完成，安装应在同卷独立 staging 中完成后无损提交；每次进程运行持有独立 `cmd/job/done/redact`，wait/pump 只操作本代；异步轮询和体检采用 generation，请求结果在最终写回处验证，轮次切换与事件发布建立明确顺序；本地版本加载与远程版本请求分别写回，外网挂起不阻断本地资产。
- **避坑防重犯建议**：① 安全边界以“最终文件句柄/最终删除目标”为准，不以一次字符串校验为准；② 锁的作用域必须覆盖真实后台任务，不是只覆盖 goroutine 创建；③ `cancel()` 不等于旧函数不会返回，所有 await/探针结果都要做代次门卫；④ Stop 要锁内摘取、锁外等待，超时 Close 并等待采样/handler 回收；⑤ 配置加载失败不能伪装成空配置，候选数据落盘成功后才能替换内存。

### 39. WSL 只读取证的三个 guest 探测坑：`wsl -d` 会顺手开机、`ip -o` 的 brd 地址截胡、df 折行

- **问题现象与错误原因**：发行版取证抽屉（VHDX 双口径/根盘用量/IPv4）首版联调暴露三处：① 对停止的发行版执行 `wsl -d <name> -- df` 会被平台语义**顺手拉起发行版**——"只读取证"产生了真实副作用，用户只是点开抽屉，任务栏却多出一个正在运行的发行版；② `ip -4 -o addr show` 输出为 `inet 172.25.9.42/24 brd 172.25.9.255 ...`，带 CIDR 后缀的地址使 `net.ParseIP` 判失败被跳过，"第一个合法 IPv4"于是**截胡成广播地址**；③ `df -B1M /` 在文件系统名过长时折行，按"最后一列是挂载点"的行解析会整行落空。
- **排查过程**：用 `wsl -l -q --running` 名单先行门控后确认"未运行即不发 guest 命令"；实抓 `hostname -I`、`ip -o`、`df` 三种输出逐 token 核对解析路径；把 -q 通道不可得的场景也走保守分支（宁可不采，不猜测运行态）。
- **正确做法与标准修复方案**：① 所有"只读"入口对停止实例一律不进 guest，并把缺项如实写进 Notes（前端不编数字）；② IP 提取先按 `/` 截断再 `net.ParseIP`，取首个命中即网卡主地址（`hostname -I` 优先，`ip -o` 兜底）；③ df 解析对整段输出做**搜索式**四元组匹配 `(\d+)M\s+(\d+)M\s+(\d+)M\s+(\d+)%`，天然免疫折行；④ VHDX"实占"必须走 `GetCompressedFileSizeW`（`os.Stat` 只给逻辑大小，稀疏盘两者可差数倍，磁盘瘦身决策依赖实占口径）。
- **避坑防重犯建议**：① 在 WSL 语境里"只读"要以 guest 视角成立才算数，宿主侧再只读也拦不住 `wsl -d` 的开机语义；② 解析 Linux 工具输出优先选结构稳定通道（`-o`/quiet/JSON），并警惕"跳过坏 token"策略让位给语义更差的次优 token；③ 磁盘占用永远区分逻辑/实占双口径并注明。

### 40. 开发环境：Git Bash 会话派生 Windows 子进程时 PATH 被截断，wails3/go test 找不到 go 与 cmd.exe

- **问题现象与错误原因**：在同一 bash 里 `go build`/`go test` 正常，但 `wails3 generate bindings` 报 `go command required, not found: exec: "go"`；`cmd //c "where go"` 连 `where` 都找不到；批量测试中依赖 `cmd.exe` 的托管实例测试报「cmd.exe 不可用」。原因是该环境把 bash 的超长 POSIX PATH 转换为 Windows 格式时**被截断**（只剩前两百余字符，Go SDK 与 System32 全部丢失），受影响的是 bash 派生的 Windows 原生子进程，而非 bash 内建工具。
- **排查过程**：对照 `echo $PATH`（完整）与 `cmd //c "echo %PATH%"`（截断）确认是转换层丢量而非配置丢失；给子进程**显式指定最小 Windows 目录集 PATH** 后全部恢复。
- **正确做法与标准修复方案**：需要 bash 派生 Windows 工具链（wails3、npm 触发的 node、go test 的 exec 用例）时，用行内环境覆盖：`PATH="/c/Users/<u>/sdk/go<ver>/bin:/c/Windows/system32:/c/Windows:<node>/bin" <命令>`；批量测试直接 `task check`/在 cmd 或 PowerShell 会话中跑，避开 bash 的 PATH 转换。
- **避坑防重犯建议**：① 遇到"父进程能用、子进程找不到"先分清 POSIX→Win 的 PATH 转换是否截断，别急着改系统 PATH 或怀疑工具损坏；② CI/门禁命令以 Windows 原生 shell 为准；③ 单测失败信息里"不可用"类文案要报出执行名（如 cmd.exe），本轮测试因此一次定位。

### 41. FileShare「访问口令」假在：配置字段、绑定、设置存储全链路齐活，唯独服务端没人读它

- **问题现象与错误原因**：`ShareConfig.AuthToken` 从 models 到 Wails 绑定到前端 configForm 一路都在，`SaveConfig` 也原样落存，唯独 `server.go` 的任何 handler 从未读取比对——设不设口令，局域网内浏览、下载、上传、投递全部放行（BUG_AUDIT BUG-001，P0）。同源病灶还有第二处：`connTracker` 对全响应下发 `Access-Control-Allow-Origin: *`，访客页与 API 本就同源、根本不需要 CORS，通配反而放行了「恶意网页借用户浏览器跨源打局域网端点」。
- **排查过程**：对 `AuthToken` 做全引用扫描——除字段定义与配置读写外无任何消费点；比对 REVIEW 09-08「安全止血」清单，B02 只做了 os.Root 目录沙箱，鉴权不在其中；确认属「功能登记在表、实现悬空」而非回归。
- **正确做法与标准修复方案**：新增 `authGate` 中间件挂完整处理链（`connTracker(authGate(mux))`），口令为空保持免密原语义；非空时 `/api/*` 数据面全部要求会话，仅 `/`、`/assets/`、`/api/login`、`/api/config` 白名单放行。会话为**口令 HMAC 派生值**的 HttpOnly + SameSite=Lax Cookie（30 天）：免服务端会话表、口令不落 Cookie、`UpdateConfig` 换口令即令全部旧登录态作废；另留 `Authorization: Bearer <口令>` 通道给非浏览器集成。口令与会话比较一律 `subtle.ConstantTimeCompare`，登录失败路径统一延时压制爆破；移除通配 CORS。访客页加全屏口令门禁（401 事件重弹、静默探测免回访重输），主程序设置面板补口令输入框。回归锁在 `internal/modules/fileshare/auth_test.go`。
- **避坑防重犯建议**：① 新增配置字段必须同 PR 指出**消费点**，「存得进读不出」的字段比没有字段更危险——它制造已在防护的错觉；② 判断是否真需要 CORS：页面与 API 同源即不需要，任何 `*` 都要能讲出跨源调用方是谁；③ 安全声明（README 写「口令保护」）落笔前先问代码里谁在执行它——本次 README 文案缺口即按此补记；④ 审计文档里「待修复」条目动手前先在当前代码复核，09-04 清单到 09-08 修复轮之间行号与状态都已漂移。

### 42. Paseo 集成：同是 Electron 单实例锁，`second-instance` 语义可完全相反——唤窗不能照抄信使族

- **问题现象与错误原因**：Paseo 与 recordly 都是 Electron 应用、都靠 `requestSingleInstanceLock`（无命名互斥体可探），初判可直接套 recordly 的"信使唤窗"模板（二次无参拉起 → `second-instance` 回调 `restoreWindowSafely` show+focus）。但 Paseo 的 `second-instance` 回调走 `openAdditional`——**再开一个新工作区窗口，而非聚焦既有窗口**（main.ts 实证）。若照抄信使唤窗，"打开窗口"按钮每次点都会堆出一扇新窗，与用户"唤起当前窗口"的直觉相反。
- **排查过程**：`curl` 上游 `packages/desktop/src/main.ts` 的 `setupSingleInstanceLock`，逐行读 `app.on("second-instance", …)` 分支：Paseo 调 `desktopWindowOwner.openAdditional({ pendingProjectPath })`；而 recordly 同位置是 `show()+focus()`。同一 Electron API 家族，行为由应用自己的回调决定，**不能从"是 Electron"这个事实推断唤窗语义**。
- **正确做法与标准修复方案**：① 唤窗优先 **Win32 直唤已有可见窗口**（`EnumWindows` 过滤进程名 + `ShowWindow(SW_RESTORE)` + `SetForegroundWindow`，litemonitor 家族），**仅当无可见窗口时**才二次拉起信使请求开新窗——`instance.Engine` 因此把 recordly 的单一 `OpenWindow` 拆成 `FocusWindow()` 与 `OpenMessenger(exe)` 两个原语，service 层按 `Snapshot.State` 编排；② 探测仍走进程名（锁按 userData 派生、无可依赖互斥体，与 recordly 同纪律）；③ 共享数据模式下 Electron 数据恒在 `%APPDATA%\Paseo`、daemon 在 `~/.paseo`（`PASEO_HOME`），托管与自装实例同锁组 → 全局至多一个桌面主实例，自有/外部天然互斥，故**不需要 vscode 便携那种"按镜像路径前缀区分托管 vs 外部"的第二探测通道**，`wait()` 外部接管分类直接可靠；④ 资产甄别：Windows 便携 zip 必须匹配 `Paseo-Setup-<ver>-<arch>.zip`，与 macOS 产物 `Paseo-<ver>-<arch>.zip` 只隔 `Setup-` 前缀，误收即跨平台炸弹（recordly 已点名过的同类陷阱，此处再次命中，正则强制前缀）。
- **避坑防重犯建议**：① 集成 Electron 工具前，阶段 0 侦查必须读到 `second-instance` 回调体本身（是 `openAdditional` 还是 `show+focus`），据实选唤窗策略，别在"Electron 信使族"标签下照抄 recordly；② 无托盘 + `window-all-closed → app.quit` 的上游，WM_CLOSE 优雅退出可达，但宽限期要按"关闭时还牵扯 daemon/子进程树"上调（Paseo 取 8s，recordly 3s）；③ daemon 宿主类工具（关窗无窗 ≠ 空闲）禁用空闲自动退出，误杀毁在途 agent 会话（rustdesk 先例）；④ 共享用户目录数据的工具，卸载/删版本一律不碰 `%APPDATA%` 与 home 下数据目录。

### 43. WSL 唤终端「点了没反应」：cmd start + HideWindow 时新控制台窗口继承隐藏显示状态

- **问题现象与错误原因**：发行版列表点「⌨ 终端」，toast 报"已启动终端会话"、发行版也确实进入运行态，但屏幕上没有任何终端窗口弹出。旧实现 `exec.CommandContext(ctx, "cmd.exe", "/c", "start", "", "wsl.exe", -d, name)` 配 `SysProcAttr{HideWindow: true}`——本意"只藏 cmd 壳，start 开的新窗口不受影响"，实际 cmd.exe 的 STARTUPINFO 带 `STARTF_USESHOWWINDOW + SW_HIDE` 时，`start` 内建命令为子进程新建控制台窗口会**继承这一隐藏 show-state**：窗口存在但完全不可见。
- **排查过程**：点终端后 `wsl -l -v` 确认发行版已被拉起、任务栏闪现后消失 → 排除 wsl.exe 未启动；单独双击 cmd（可见）跑同款 `start` 正常弹窗 → 锁定差异在父进程的 SW_HIDE 启动信息传递。
- **正确做法与标准修复方案**：不再借 cmd 转手，直接 `exec.Command("wsl.exe", "-d", name)` 配 `SysProcAttr{CreationFlags: createNewConsole}`（CREATE_NEW_CONSOLE=0x10）：子进程自带全新**可见**控制台，Win11 由"默认终端应用"接管呈现（设了 Windows Terminal 出 WT，否则 conhost）。起后即 `Process.Release()` 脱手——Windows 无僵尸语义，且 GUI 宿主退出/体检 ctx 超时都不牵连会话；刻意不用 `CommandContext`（wsl.exe 活到用户 exit 为止，挂 60s ctx 会把会话击杀）。
- **避坑防重犯建议**：① `HideWindow` 藏的不止 cmd 壳本身——经 `start`/`cmd /c` 转手拉起的 GUI/控制台窗口会继承隐藏态，"藏壳露窗"想当然必翻车，需要可见窗口就直接 CreateProcess 新控制台，不要套 cmd 壳；② 长生命周期的用户会话进程绝不能挂在带超时的 `context` 上（CommandContext 取消即杀子进程）；③ toast 报成功≠用户看得见结果，"拉起窗口"类操作验证要盯窗口本身。
- **二次踩坑补记（同日，v2）**：修复 v1 后改用 `exec.Command("wsl.exe") + CREATE_NEW_CONSOLE`，黑窗**一闪而过**——Go 的 os/exec 在 Stdin 未连接时递给子进程 NUL/EOF 句柄，bash 启动即读到 EOF 秒退。控制台交互程序要真实 stdin，必须走 ShellExecute（系统壳语义：新控制台 + std 接控制台输入 + 显式 SW_SHOWNORMAL），或 `cmd start` 转手（但带回 v1 隐藏态继承坑）。**最终形态**：`windows.ShellExecute(0, nil, "wsl.exe", "-d <名>", 0, SW_SHOWNORMAL)`——两坑同免。**泛化教训**：从 GUI 宿主（-H windowsgui）spawn"需要人交互的控制台程序"时，句柄继承面（stdin）与窗口显示面（wShowWindow）都要经壳层语义接管，Go exec 的"便利默认"（nil stdin→NUL）对交互场景是致命的。

### 44. 数据目录实测迁移：Paseo 进程疑似"逃托"退出清理，外加迁移与环境的三枚次级坑

- **问题现象与错误原因**：将标准模式数据目录（`%APPDATA%\Hanxi`，实测 1.6GB，`versions/` 占 99.9%，配置类 JSON 合计 <1MB）整体迁移到便携模式（exe 同级 `data/`）。托盘退出 Hanxi、复制验证、新位置启动正常后删除旧目录时，`versions/paseo_0.8.0/resources/app.asar` 报 Device or resource busy——`tasklist` 实锤 3 个 `Paseo.exe`（`Get-CimInstance` 确认全部从**旧托管路径**启动）仍存活。§3 的 JobObject KILL_ON_JOB_CLOSE 内核兜底与 §10 的 OnShutdown→ShutdownAll 优雅链两条理论防线，在 Electron 托管工具上疑似同时失守（§42 记录过 Paseo 关窗即 quit 的优雅退出可达性，但"托管方退出"路径未实测覆盖）。
- **排查过程**：**根因未闭环，列为待办**。进程已被 taskkill、出生时刻证据随之丢失，无法区分两种可能：① 本次托盘退出真的漏杀（Electron relaunch 派生真实进程 breakaway 出 Job，或 daemon 子进程树从未 assign，或优雅退出宽限期内未完成）；② 系更早残留的孤儿（此前某次异常退出漏杀，本次只是被删除动作撞见）。复现路径：启动托管 Paseo → 托盘退出 → `Get-CimInstance Win32_Process -Filter "Name='Paseo.exe'"` 扫残留并按 ExecutablePath 判归属，命中即查进程树 ParentProcessId 与 JobObject 归属（Process Explorer / `NtQueryInformationProcess` Job 类）。无论哪种，§10 的结论都要补一条：**"接线了"≠"对 Electron 进程树有效"**，验收必须含托管进程残留扫描。
- **正确做法与标准修复方案（迁移流程，本次全程走通且可回退）**：① **先复制不动源**：`robocopy /E` 整树拷至新位置 `data\` → 双侧文件数核对 → 新位置启动验证（exe 同级存在 `data/` 时 `resolvePaths` 便携模式优先，旧 `%APPDATA%` 原地未动，任一步失败直接回退）→ 用户确认后才删旧目录；② 自启 `HKCU\...\Run` 键记录的是 exe 绝对路径，迁址后必须在设置页"关→开"重注册（让新实例按自身路径写回，勿手改注册表）；③ 桌面 `.lnk` 快捷方式指向 `versions/` 下绝对路径，迁移必悬空，需重新"创建桌面快捷方式"；④ 日常使用 exe 与开发构建 exe 分居两目录——删数据后开发目录 exe 自动新建空白 `%APPDATA%` 当沙盒，意外实现了开发/日常数据隔离。
- **避坑防重犯建议**：① 托管 Electron 工具的"退出"语义验收，必须在真机上做"Hanxi 托盘退出 → 托管进程树残留扫描"，互斥体可达（§42 的 WM_CLOSE 路径）不代表**退出清理**可达；② Git Bash 里调 robocopy 等斜杠参数 Windows 原生工具，必须前缀 `MSYS_NO_PATHCONV=1`——否则 `/E` `/COPY:DAT` 被 MSYS 改写成 `E:/` 之类路径报 `Invalid Parameter`；③ Wails/WebView2 在 `%APPDATA%\Roaming\` 下生成字面名 `hanxi.exe` 的缓存目录（内含 EBWebView 浏览器缓存）——目录名酷似可执行文件，易被误判为恶意残留，属低优先观感问题，宜显式指定 user-data-dir 归入自家数据目录。

### 45. 便携标记目录更名 hanxidata：泛化名歧义、旧包兼容与 Compress-Archive 丢空目录

- **问题现象与错误原因**：便携模式此前以 exe 同级**任意存在的 `data/` 目录**为开关，泛化名有两重歧义：任何同名第三方目录会误撞，且路过误建一个空 `data` 即静默把标准模式切成便携（用户以为数据"丢了"，实为读写了那个空目录）。同时排查发现发布链的隐患：`release.yml` 以 `New-Item` 建**空** `data` 目录后用 `Compress-Archive` 打便携 zip，而 Compress-Archive 会**静默丢弃空目录**——zip 解压后若无标记目录，"便携版"首启直接回落 `%APPDATA%` 标准模式，便携语义名存实亡（依赖被丢弃的目录存在做模式开关，是打包器行为与运行时探测的契约断裂）。
- **正确做法与标准修复方案**：① 标记目录更名为 `hanxidata/`（自带产品归属，空目录即生效——显式创建本身就是用户意图）；② 旧包兼容：同名 `data/` **仅当已含数据根特征**（`config.json` 或 `versions/`）时才识别，空 `data` 不触发，升级已发布便携包不丢数据、新用户不误撞；探测逻辑提取为 `detectPortableBaseDir(exeDir)` 纯函数配表驱动单测（`os.Executable` 不可注入,可测边界就设在参数上），标准/便携两套 `Paths` 字面量收敛为共用 `buildPaths(mode, base)`；③ CI 便携目录内落 `portable.txt` 占位说明（目录非空，zip 必保留，兼作用户向文档），同步改写 Release 文案。
- **避坑防重犯建议**：① 凡"目录存在性"做模式开关，目录名必须带产品归属；② 凡走 `Compress-Archive` 打包，目录树里不允许任何**语义依赖其存在**的空目录——要么放占位文件，要么改用 `tar -a -cf`（PS7 的 tar 保留空目录）并实测 zip 内容清单；发布流水线里"运行时契约依赖的产物形态"（本例：解压后必须有标记目录）应在 CI 加一步 `Expand-Archive` 到临时目录复验，而非只看 zip 生成成功。

### 46. WSL 名单连环误判：`wsl -l -q` 管道输出的分隔符是 \r\n 而非 NUL（首诊"传播延迟"系误归因，二次实锤修正）

- **问题现象与错误原因**：真机连环两案同源。① 克隆 kali-linux 报"导入命令返回成功，但名单中未见 kali-linux-Copy"——实查 `wsl -l -v` 早已在册，克隆完全成功；② 随后删除 kali-linux 被白名单拒"不在本机已安装清单中"，而 `wsl -l -v` 明晃晃两条都在。`od -c` 实测 Go exec 捕获的 `wsl -l -q` 字节：**分隔符是 `kali-linux\r\nkali-linux-Copy\r\n`**——wsl.exe 按 stdout 类型切换分隔形态，接控制台（伪终端）时 NUL 分隔（微软 CLI 约定），重定向到管道（Go exec 捕获恰然后者）时改 \r\n。旧 `parseQuietList` 只按 NUL 拆分，整串成"一个名字"：白名单 Contains 必 miss、复验 EqualFold 永无命中。首诊只盯"时机"未复核"编码"，把根因误记为传播延迟（本条目原版的"错误原因/排查过程"两段即错例，保留以自警）。
- **排查过程**：② 案发后读 distroAllowed 链路顺藤摸瓜，直接在真机对 `wsl -l -q` 管道输出做 `od -c`——一锤定音。复盘 ①：当时机上已有 2 条发行版，快照复验其实**永远**读不到第二个名字，与延迟无关；而本模块开发期单测桩全部喂 NUL 形态，真机冒烟期机器又只有单条发行版（单条无内部分隔符，trim 后恰好完好）——**样本单一性同时蒙蔽了实机验证与测试基线**，直到克隆造出第二条才连环引爆。
- **正确做法与标准修复方案**：`parseQuietList` 改 `FieldsFunc` 按 `\x00` 或 `\n` 双分隔切分（发行版名不允许含两者，天然兼容；`\r` 由逐段 TrimSpace 清除），回归锁 `\r\n`/混合分隔/端到端白名单放行三用例。`waitForRegistration` 轮询复验保留——它对真实的最终一致窗口仍是正当加固，只是不再冒充本案根因。
- **避坑防重犯建议**：① 解析外部命令输出前，必须用 `od -c` 对**管道形态**（与生产同款捕获方式）取证，控制台肉眼看格式≠程序读到的字节；伪终端相关约定（NUL 分隔、进度 \r 覆写）在重定向下全部作废；② 测试桩的输入形态要覆盖"多条目"边界——单条样本是编码 bug 的完美温床；③ 多枚症状（复验假失败 + 白名单拒删）先后出现时优先怀疑**共同上游数据源**，而不是各修各的（首诊即犯此病）；④ 归因未闭环时文档要写明"待证"，本条目把错误结论写成了确定性根因，二次修正成本翻倍。

### 47. 集成「内测 + 闭源壳 + 无便携版」产品：托管降级为「仅版本+下载」的决策与连环环境坑（douzy 集成实战）

- **问题现象与错误原因**：评估集成 jiji262/douyin-downloader 桌面版 Douzy 时，按托管模板阶段 0 侦查命中三个降级信号——① Windows 仅有 NSIS `Douzy-Setup-*.exe`，**无 portable zip**（rustdesk 式"解压即托管"不成立）；② Electron 壳源码不在任何公开仓库（单实例契约/userData/退出语义**无法审计**，只能黑盒）；③ 上游 README 红字标注**内测中**。三者叠加意味着即便走 vscode/recordly 的"外部安装器"次优模板，也要为用户自担的内测产品维护一套探测/唤窗/退出契约，投入产出比极差。实施阶段连环遭遇环境问题：git bash 里 `go`/`node` 明明 `which` 可见，但 `task generate`（go mod tidy 127）、`wails3 generate bindings`（"go: executable file not found in %PATH%"）、`npx vitest`（'"node" is not recognized'）全部失败，且 `cmd //c "where"` 连 System32 命令都找不到——**该 bash 传给原生 Windows 子进程的 PATH 是残缺的**，MSYS 路径转换不可信。
- **排查过程**：逐项实证资产表（每版 digest 齐全、tag 序列 desktop-v* 连续、mac 有 dmg 但 Windows 只有 Setup.exe）后向用户提出三选一边界，最终拍板**降级集成**：只保留 version 子包 + 精简 service（版本八件套 − 启停 − store − instance/），新增 `LaunchInstaller`（exec 安装器后不管）；`instance/` 七文件整体不建。环境问题上，`cmd //c '...'` 传参丢失（仅弹 banner 即退），PowerShell 显式重建 PATH 一次成功；`-clean=true` 首跑失败但 Processed 0 packages 未及写盘，bindings 幸而未被清空（245 文件完好，先 `git status` 确认再重试是对关键生成物的必要保险）。
- **正确做法与标准修复方案**：① 决策框架落档——上游实例模型命中「无便携资产 + 壳闭源 + 内测/停更」任意两项即**不硬套托管模板**，降级为版本+下载管理器（照抄 rustdesk 单文件下载路径，删 zip/MSI/探测分支，前端无 console Tab、常驻内测警示横幅、文案诚实区分"删除安装包 ≠ 卸载程序"）；② 本环境跑生成/前端工具链用 `powershell -NoProfile -Command "$env:PATH='C:\Windows\System32;C:\Windows;<node目录>;<go bin目录>'; <命令>"` 显式重建；③ bindings 是全量 `-clean` 重生——工作区有**他人未提交的 Go 改动**时会顺带刷新对应绑定（本次 wsl 3 文件 + `eventdata.d.ts` 因此变 M），提交切分必须把「本模块绑定」与「他线联动刷新」分开说明。
- **避坑防重犯建议**：① 阶段 0 的结论不只是"选哪个模板"，还应给出"要不要做满托管"的否决判断——集成范围的止损线比实施技巧更值钱；② 在共享工作区做全量生成（bindings/代码生成器）前先看 `git status`，向用户预告生成物可能覆盖到未提交改动所属的产物；③ Windows 下 bash 工具链报 "not found in %PATH%" 而 `which` 正常时，不要反复重试，直接判定 MSYS 子进程 PATH 转换失效，第一步就切 PowerShell 显式 PATH；④ 「运行安装程序」这类"发射后不管"操作，前端文案必须写明后续 UAC/向导由用户完成、Hanxi 不感知安装结果——诚实预告黑盒边界，别让按钮语义过度承诺。

### 48. WSL「默认 D:\wsl 却还是装到 C」：落位三坑同源——偏好粘滞、装完即迁无归因、能力无闸门

- **问题现象与错误原因**：e444ca6 把商店安装/克隆默认落位设为 `D:\wsl` 后，用户仍投诉"安装和克隆到了 C 盘"。实机取证（注册表 Lxss BasePath 两条均在 `D:\wsl`、C 盘无 ext4.vhdx 残留、exe 构建晚于该提交）证明**最终落位正确**，观感落差来自三坑叠加：① `wsl --install` 无目标目录参数，"装完即迁"必然是**先落 C 再搬 D**，中途窗口期被当成"装到 C"；② 偏好记在 localStorage（key `wsl.distroInstallDir`），`stored === null` 才回默认——一旦存入空串（改动又清空）或手滑的 C 路径，默认值 `D:\wsl` 一去不回，且 WebView2 数据目录跟随构建形态（dev/安装/便携各记各的），"记忆时好时坏"；③ 提权链 `install → shutdown → manage --move` 用 `$LASTEXITCODE` 原样传播，迁移段失败（如 `wsl --manage` 是 **2.4.4+ 才有**的能力，旧 inbox WSL 报 unknown option 连撞 5 次重试）时发行版已静默躺在 C，报错只有一句笼统退出码、失败后 `runOp` catch 不刷新列表，用户以为"没装上"，日后在列表发现它才成"装到 C 了"。克隆路径本身经 `moveTarget` 把关（空/相对/盘不存在/非空目录全拒），不存在静默落 C 的代码分支——旧包（v0.2.0 及以前，特性未进任何 release tag）才有克隆预填为空的形态。
- **排查过程**：本机取证先行（`wsl -l -v` + Lxss 注册表 + C/D 盘 vhdx 扫描）排除"真 Bug 在落位写路径"；再全链路推演前端默认值→localStorage→`InstallDistroTo`→提权 PowerShell→`moveTarget/underDir`，并行 agent 佐证"特性未进 release、旧包无此逻辑"；`--manage --move` 版本下限经上游 2.4.4 发布说明与微软 virtual-disk 文档双重核实（本仓与 clone 的 2.7.3 闸门是 `--import-in-place` 的能力线，不同族，勿混）。
- **正确做法与标准修复方案**：① 偏好迁后端 `<DataDir>/wsl-install-pref.json`（与 wsl-portproxy.json 同款 tmp+rename 原子写），返回 `InstallDirPref{set,dir}` **三态**——`set=false`=从未设置（回默认 D:\wsl）与 `set=true,dir=""`=显式系统默认判然分开，localStorage 退役；② `InstallDistroTo` 三道防线：**版本闸门**（wslVersion < 2.4.4 在任何副作用前拦下，指路"升级本体"或"留空接受系统默认"两条显式出路）、**分段哨兵退出码**（install 段 `exit 80`、move 段 `exit 81`，经 ops.go 类型化 `elevatedExitError` 承载——`exec.ExitError.ExitCode()` 依赖 ProcessState，单测伪造不了）、**成功后注册表 BasePath 复验**（MoveDistro 同款，退出码 0 ≠ 真落位）；③ **在册幂等兜底**：`wsl -l -q` 已含该 id 时跳过 install 只补迁移，消灭"重跑 --install 被掐死、永远搬不动"的死循环，位置已正确直接好话回绝；④ 迁移段失败回执点名"数据已就位、**无需重装**、到列表点🧭迁移补救"，前端 catch 补复采，半成功状态当场可见。
- **避坑防重犯建议**：① 凡"装完即迁/下完即搬"类二段式操作，回报必须分段归因并给补救指路——二段链整体报"失败"会把已生效的一半变成用户认知里的失踪人口；② 依赖外部 CLI 子命令的能力闸门，**动手前**查上游 release notes 定版本线（2.4.4 `--manage --move` / 2.7.3 `--import-in-place` 各管各段），别让链中段撞 unknown option；③ 用户偏好落 localStorage 只配当"首帧缓存"（useTheme 范式），跨构建形态要一致的记忆必须落 DataDir——WebView2 数据目录跟着 exe 走，不同包的记忆天然不通；④ "默认值 + 可清空"的输入框，空串必须与"未设置"区分（三态哨兵），否则清空一次=永久禁用默认；⑤ 新特性未进 release 前收到的"不生效"投诉，第一步核对现场 exe 构建版本/注册表终态，别默认用户跑的是你眼前的代码。

### 49. 全库确认框「\n\n 分段」被 HTML 折叠成文字墙：设计防线在渲染层一秒归零

- **问题现象与错误原因**：各模块 confirm 的 description 一贯用 `\n\n` 手工分段（操作语义、后果预告、UAC 说明各一段），但渲染侧 `ConfirmDialog.vue` 的 `<p>{{ description }}</p>` 无任何 `white-space` 规则——CSS 默认 `normal` 把换行折叠成空格，300+ 字的防呆说明糊成一块无结构密文。文案层的动线设计全部白做，用户直接跳过不看，是"交互难受、提示看不见"投诉的头号成因；同族第二坑：小窗口下超长 description 把确认/取消按钮顶出视口（面板无 max-height）；第三坑：错误 toast 2.5s 蒸发且 `pointer-events:none` 不可选中复制，克隆/导入的长 stderr 一闪而过无法排查。
- **排查过程**：从"文案都分段了为什么读起来是墙"反向到渲染层，确认全库模板里唯一丢换行的就是裸 `<p>` 插值；toast 侧核实 useToast 早已支持 `{duration}` 而调用方从未用过。
- **正确做法与标准修复方案**：① `.workbench-confirm p { white-space: pre-line }`（一行 CSS 全局生效，顺带 `&lt;br&gt;` 手拼的旧文案不受影响）；② 面板 `max-height: min(72vh, 640px); overflow-y: auto`；③ `.global-toast` 开 `pointer-events: auto; user-select: text; max-width: 480px; overflow-wrap: anywhere`——可选中复制长错误路径；④ 失败类 toast 调用方显式 `{ duration: 8000 }`，默认 2500ms 不动（长保活是调用方的语义决定，不是全局样式决定）；⑤ 附带修 aria：`:aria-labelledby="`${title}-dialog-title`"` 里 title 含中文/空格即非法 IDREF，改固定 `hx-confirm-title`。
- **避坑防重犯建议**：① 模板插值渲染带 `\n` 的多行文案，**渲染层必须先声明 `white-space: pre-line`**——这是文案设计与 CSS 的隐式契约，新对话框组件落地时容易漏；② 有滚动容器就要锁 `max-height`，否则小窗/长文案场景按钮被顶出可视区，确认框变成"只能 Esc 取消"；③ 组件写"分段"的单元测试要断言**换行真的可见**（pre-line 规则存在性即可，happy-dom 拿不到 scoped 计算样式时用 `?raw` 读 SFC 断言），别只断言 `textContent` 含整段——折叠成空格它照样绿；④ toast 是"路过型"反馈，失败详情要行内驻留（progress.error 分支），toast 只做指路。

### 50. 快捷菜单轮盘连环白边：旧注释谎称"Wails 无透明能力"，GDI 区域硬裁与近白 canvas 双重露底

- **问题现象与错误原因**：右键长按弹窗从列表改为圆形轮盘后（钩子与弹窗基础坑见 #29，其"三处认知差"不含本条的透明能力误判），实机先后出现三种"白边"。① 细碎白边+边缘发虚：按 QuickMenuPopup 旧注释"Wails beta.10 Windows 侧无窗口透明能力"的断言，用 `SetWindowRgn(CreateEllipticRgn)` 硬裁方形窗模拟圆——GDI 区域无抗锯齿，圆周呈阶梯锯齿；且 `BackgroundColour` 固定浅灰在深色主题下于 AA 混色像素处露底成白圈，入场 `scale(0.94)` 动画还把 SVG 整体缩出一圈窗底白环。② 大白环：切到 `BackgroundTypeTransparent` 真透明后，全局 `body { background: var(--surface-page) }`（近白）铺满方形窗口，GDI 圆裁（半径=窗口半宽，含为投影留的边距）后剩下一个比圆盘大一圈的**不透明白圆**，盘缘外露出 18px 大白环——solid 时代这层底被铺满窗口的圆盘盖住，从未现形，透明化才引爆。
- **排查过程**：读 beta.10 源码（`webview_window_windows.go`）实锤 `BackgroundTypeTransparent` 存在且走 DirectComposition（`WS_EX_NOREDIRECTIONBITMAP` + chromium 背景 alpha=0）——旧注释是错的；白环宽度恰等于边距、外缘恰为裁剪圈 → 反推是页面 canvas 而非窗口底。
- **正确做法与标准修复方案**：① 弹窗用 `BackgroundType: application.BackgroundTypeTransparent` + `BackgroundColour: NewRGBA(0,0,0,0)`，圆盘视觉边缘（含抗锯齿、投影）全部由页面绘制；`SetWindowRgn` 降级为纯命中测试（把四角从鼠标命中剪掉让点击穿透），裁剪圈半径设在**投影淡出后的全透明区**，GDI 硬边落在无像素处即不可见；② 弹窗类透明窗口须把 canvas 一并打穿：main.ts 按 hash 给 `<html>` 打 `.popup-shell` 标记，base.css 覆写 `html/body background: transparent`；③ 入场动画不得缩放"能露出底色的整层"，且新窗口透明后边距区点击会被 WebView 吃掉——"点外收起"判定半径要按**盘半径**（窗口半宽 − 边距×scale）而非窗口半宽。
- **避坑防重犯建议**：① 代码注释里的"框架不支持 X"是**历史断言不是事实**——动手支持性判断前回一遍当前版本源码/grep 能力位（beta 版特性表变得快），本轮为一句旧注释多走了一整轮 GDI 弯路；② 给"未来可能透明"的浮窗立规：**页面根 canvas 不允许携带实底全局背景**，底色职责下放到显式绘制的容器层，否则任何透明化尝试都会把 `body` 底色原样暴露；③ 透明窗 + 区域裁剪混用时，二者半径职责必须分开——区域管命中、CSS 管观感，裁剪圈永远画在内容透明处；④ 带 scale 入场动画的浮层，动画层下方必须无可见底（透明窗/同色底），否则每次弹出都闪一圈露边。

### 51. 阴影 token 当颜色用：`box-shadow: 0 8px 32px var(--shadow-panel)` 七处整条声明被解析器静默丢弃

- **问题现象与错误原因**：字号/按钮体系化治理的上收审计中，B/D 两组各自登记了"阴影从未渲染"的组件（FrpcProjectEditor 模式钮选中态与导入模态、FileShareHero/Overview/Settings/Workspace 四处卡片投影）。根因同一：把 `--shadow-*` token 当 `<color>` 填进自定义阴影的偏移位——token 值是完整的 `0 18px 45px rgba(...)`（含偏移/模糊/扩散），拼接后整条 box-shadow 变成 6+ 个长度值，CSS 解析当场丢弃**整条声明**，浏览器控制台零报错，肉眼只会"以为没做阴影"。
- **排查过程**：跨组上收候选汇总时逐条比对 shadow 声明，发现同库 `.modal-card` 在 FrpcProjectsView 正确直挂 token、在 FrpcProjectEditor 却是拼接形——正确写法与错误写法同屏存活，实锤是复制走样而非能力缺失；grep 模式 `box-shadow:[^;]*0[^;]*var\(--shadow` 全库扫出共 7 处。
- **正确做法与标准修复方案**：阴影只有两档语义（`--shadow-small` 卡片 / `--shadow-panel` 浮层模态），要投影就**整值直挂** `box-shadow: var(--shadow-small)`；确有第三种投影需求（如右侧抽屉专用 `--shadow-drawer`）才在 tokens.css 增设命名档，禁止视图内拼接改参。7 处已全部改正（修复后投影开始真实渲染，列入目视核对项）。
- **避坑防重犯建议**：① token 化的复合值（阴影/渐变/字族）永远整值引用，**组合器（box-shadow 逗号并列）只允许并列多条完整 shadow**，不允许给单条加偏移前缀；② code review 见到 `box-shadow:` 行里 `var(--shadow` 前面还有裸数字即红灯；③ 此类"声明无效但无报错"的静默失效，vitest（happy-dom 不解析 scoped CSS）与 vue-tsc 均抓不住，只有 grep 审计或真机目视能兜底——大治理波务必带一次全库失效声明扫描。

### 52. GUI 子系统双击启动"日志恒空"：`io.MultiWriter(os.Stderr, f)` 被无效 stderr 句柄中断，落盘跟着拖垮

- **问题现象与错误原因**：便携包 `hanxidata/logs/` 下每天的 `app-*.log` 都被正常创建，却恒为 0 字节，任何排障都"无日志可查"；从终端/`go run` 启动时日志却又完好。根因两步叠加：① 生产构建带 `-H windowsgui`（PE 子系统 2，见 `build/windows/Taskfile.yml`），双击/开机启动的进程没有控制台，`GetStdHandle(STD_ERROR_HANDLE)` 返回无效句柄，对 `os.Stderr` 的每次写入都报 `write /dev/stderr: The handle is invalid.`；② `logging.InitLogger` 用 `io.MultiWriter(os.Stderr, f)` 双路输出，而 MultiWriter 按参数序逐路写、**首路报错即中断**——stderr 排第一，磁盘文件永远轮不到。文件存在纯属 `O_CREATE` 的副作用，制造了"日志在写"的假象。
- **排查过程**：先确认启动路径（InitLogger 唯一调用点在 `app/app.go`，`slog.Info("Hanxi starting")` 紧随其后，理应有内容）；读 PE 头验证 `bin/hanxi.exe` subsystem=2；用同 `-H windowsgui` 编译的最小复现程序对照三种启动方式：从 Git Bash 启动（继承控制台句柄，stderr 有效→一切正常，解释了"开发时看不见这个 Bug"）、经 explorer 启动（无句柄：`multiwriter n=0 err=handle invalid`、同一 fd 直写 `fileonly n=10` 成功）——锁定 MultiWriter 的中断语义而非 slog/文件权限。
- **正确做法与标准修复方案**：控制台路包一层尽力而为 writer（`consoleWriter`，写失败吞掉、恒返 `(len(p), nil)`，seam 变量 `consoleOut` 供单测注入），stderr 有无句柄不再影响落盘；回归测试 `TestInitLoggerFileWritesSurviveBadConsole` 注入必错 console 断言文件收全量。实机验收：production flags 构建双击启动，`app-<今天>.log` 非空。
- **避坑防重犯建议**：① Windows GUI 子系统的 `os.Stdin/Stdout/Stderr` 一律视为"可能必错"的 writer——凡与关键路径（日志、崩溃报告）并路输出，先包吞错壳再进 MultiWriter，或把文件路排在最前；② "文件被创建"≠"管道是通的"，双路输出上线前要做一次**无控制台环境**（explorer/计划任务/服务）冒烟，终端里永远测不出这类坑；③ 从 bash 直接 `./xxx.exe` 会继承控制台句柄，复现 GUI 坑必须经 explorer/Start-Process 换环境，否则得出"无法复现"的错误结论；④ 日志是排障的最后生命线，InitLogger 之后的任何启动早退路径都要保证至少一条记录已落盘。

### 53. Wails v3 beta.10 无公开 Destroy() ≠ 无法销毁窗口：摘掉 WindowClosing 拦截 hook 再 Close 即走内部真销毁

- **问题现象与错误原因**：quickmenu/ocr 悬浮窗长期按"beta.10 无公开窗口销毁 API"的结论做**常驻隐藏复用**，导致开机即养着不可见的 WebView2 视图（每个渲染器进程 + DOM/JS 堆几十 MB），任务管理器分组总内存被推到 300MB+。但"无销毁 API"的结论只对了一半：`WebviewWindow` 确实没有公开的 `Destroy()`，可 `Close()` 的完整销毁通路一直都在——`WM_CLOSE` 处理里只要 `unconditionallyClose` 未被置位就先派发 `WindowClosing` 事件，若**没有任何 hook 取消它**，Wails 在创建时自动注册的内部监听器会自行置位 `unconditionallyClose`、`markAsDestroyed`、二次 `Close()` 进入真销毁（`chromium.ShuttingDown()` + `DestroyWindow` + 从窗口管理器除名），页面内存随之释放，且同名窗口可再 `NewWithOptions` 重建。此前弹窗注册的"Cancel+Hide"拦截 hook 恰好挡死了这条通路，让人误以为根本关不掉。
- **排查过程**：读 v3.0.0-beta.10 源码链：`webview_window.go` 创建时注册的内部 `WindowClosing` 监听器（置位 unconditionallyClose + Remove）→ `webview_window_windows.go` 的 `WM_CLOSE` 分支（`chromium.ShuttingDown` + `unregisterWindow`）→ `HandleWindowEvent` 派发顺序（hook 先跑、可取消，取消后监听器不执行）→ `RegisterHook` 返回注销闭包。四处拼起来即"摘 hook → Close → 真销毁 → 可同名重建"闭环。
- **正确做法与标准修复方案**：需要空闲释放的常驻弹窗按「按需创建 + 收起后定时销毁」实现，参考 `quickmenu/service.go`：`createPopup` 把 `RegisterHook` 返回的注销闭包存进 `popupClosing`；`destroyPopup` 先 `off()` 摘钩再 `popup.Close()`；重建路径同名复用。显隐判定用服务层状态机（`popupShown`），不要在持 `s.mu` 时调 `IsVisible/Hide/Close`——它们是主线程 `InvokeSync`，而主线程侧（如 `navigateMain`）可能反向要拿同一把锁，构成锁反转；`OnShutdown` 链上的销毁安全，因为 `dispatchOnMainThread` 检测到已在主线程会直接内联执行。
- **避坑防重犯建议**：① 断言"框架做不到 X"前先翻一遍依赖源码的事件派发与清理通路，注释里的旧结论会自我繁殖（本次三处"beta.10 无销毁 API"注释互相引用，实则只差一个注销闭包）；② 拦截型 `WindowClosing` hook 必须预留"合法关闭"通道（注销闭包或放行标志），否则连模块停用/空闲释放都做不到；③ WebView2 多窗的内存大头在每个视图的渲染器与页面堆，隐藏≠省钱，常驻隐藏要过"这窗值得几十 MB 吗"的评审。

### 54. Go 直调 COM 式 vtable：uintptr 跨函数转发违反 unsafe 规则，栈增长后 C++ 回写旧副本必崩（ORT 三坑）

- **问题现象与错误原因**：PP-OCRv6 管线收编（hanxi-ocr 开源版 paddle 后端，纯 Go 动态装载 `onnxruntime.dll` + vtable 直调）时，spike 版"能跑通大多数图"但在固定一张图（呀哈哟 1484×1081 第 17 批）确定性崩溃。根因：跨调用传递的 Go 侧出参地址以 `uintptr(unsafe.Pointer(&栈局部))` 形态经**普通函数**（`rt.call`）转发，违反 unsafe 规则 3（uintptr 不得作为活指针跨函数存活）——函数入口栈若增长/拷贝，C++ 拿到的是**旧栈副本**地址，`GetTensorTypeAndShape` 把形状写进废内存、出参恒 0，随后 `NULL+0x60` AV。spike 还埋着第二颗雷：`Run1In1Out` 返回指向 ORT 自有内存的切片且随即 `ReleaseValue`（use-after-free，此前只是侥幸未踩中脏页）。
- **排查过程**：崩溃点无 Go 栈可归因（AV 在 ORT 内部），先以二分裁批锁定"第 N 批必崩"的确定性，再对照 unsafe 规则清单审 vtable 胶水——发现全部出参走 `uintptr→普通函数` 转发形态；`runtime.Pinner` 预案（spike 报告 §7 已预警"移动栈"）落实后 5/5 稳定。同轮扫出 spike 报告 §7 三个已知坑：ORT `Run` 参数序、input/output names 须二级指针数组、`CreateEnv` 传错 logid 等级直接崩。
- **正确做法与标准修复方案**：① 凡 Go 分配的内存地址要被 C/汇编 callee 读写并跨调用存活，一律 `runtime.Pinner.Pin` 后再 `unsafe.Pointer`→`uintptr` 取址，调用结束 `Unpin`（Go 1.21+ 的合规通道，等价旧 `runtime.KeepAlive` 但覆盖栈拷贝场景）；② C 侧拥有的返回缓冲**当场 MoveMemory 复制进 Go 自有缓冲再 ReleaseValue**，绝不外带切片；③ 原生地址→Go 全程 `uintptr` 算术 + `RtlMoveMemory` 搬运（与主仓 `internal/modules/ocr/snip` 包 Windows API 胶水的 vet-clean 纪律同谱）；④ ORT API 调用以 1.30.0 官方头文件机械提取的 vtable 索引表为准（`spike/downloads/ortapi_130.txt`），不手抄。
- **避坑防重犯建议**：① "spike 里没崩"≠没有 use-after-free：ORT 释放后的内存常被下一批分配复用，脏数据恰好等于期望值时测试全绿，**换图/换批次就翻车**——收编 spike 代码必须把 unsafe 审计列为第一遍扫描，不跑功能先跑 `go vet`（本报告修复后双模式 vet 零告警）；② vtable/COM 式胶水评审三查：出参地址归属（Go 栈/C 堆）、跨函数存活形态（禁裸 uintptr 转发）、返回缓冲所有权（拷贝后即释）；③ 确定性崩溃优先做"最小输入二分"而不是加日志——本例"第 17 批必崩"一句话就把嫌疑收敛到批次间缓冲复用；④ msvcp140 系 VC 运行时必须 app-local 随件（ORT 1.30 在 14.36 旧运行时 `CreateEnv` 直接 AV，干净虚拟机必炸，见 spike 报告）。

### 55. frpc 运行时明文 TOML"停机即擦"只覆盖手动路径：停用/退出/崩溃全残留 token（S0）

- **问题现象与错误原因**：DEVPLAN §2.1 验收清单长期标注"frpc 运行时临时 TOML 停机即时擦除 ✅"，实际擦除点只有 `StopProject` 与 `DeleteProject` 两处；模块停用/应用退出走的 `OnDestroy → Shutdown → engine.StopAll` 链路全程只停进程不删文件，崩溃与 JobObject 强杀更无从执行任何清理，`OnInit` 也无孤儿扫描——`hanxidata/runtime/frpc/frpc-<id>.toml`（`docgen` 产物，含 `[auth] token` 明文）长期驻盘。根因不是漏写一行 Remove，而是**验收口径只枚举了"用户点停止"这一条路**，敏感文件生命周期的其余出口（停用/正常退出/崩溃/强杀）从未对照检查，文案先行固化了一个不成立的承诺。
- **排查过程**：快照工程（`docs/plans/PLAN_SNAPSHOT.md` §2.3）做入库安全审计时 grep frpc 包 `os.Remove`：仅 service.go 两处（StopProject/DeleteProject）；顺 `Shutdown()` 与 `instance/manager.go StopAll` 全链核对无删除动作；结合 `projects.json` 侧 token 已 DPAPI 密文存储的既有设计，确认运行时 TOML 是凭据的唯一明文落盘面。
- **正确做法与标准修复方案**：收口为白名单 glob 清扫函数 `pruneRuntimeConfigs(dir)`（只删 `frpc-*.toml`，`ErrNotExist` 容忍并发），挂两个生命周期点：`OnInit`（此刻必无实例在跑，盘上文件一律按上次崩溃/强杀的孤儿收编）+ `Shutdown`（引擎停净后擦除本次）。失败仅 `slog.Warn` 不阻断生命周期——文件可能被尚未退净的子进程短暂占用，下次启动兜底重试。回归测试 `runtime_cleanup_test.go` 锁死"只删匹配 TOML、同目录其它文件与子目录零误伤、目录缺失良性"。
- **避坑防重犯建议**：① 敏感临时文件的清理承诺必须**枚举全部退出路径**（手动/停用/正常退出/崩溃/强杀）逐一对照，与其在五条路上各挂一个清理，不如"启动侧无条件扫孤儿"兜底——崩溃路径根本挂不上钩子；② 验收清单里凡"即时/永不/自动"字样的生命周期承诺，应能在代码里指出对应挂点，review 时拿 `grep os.Remove` 对质（本条蒙混过关数月即反例）；③ 消费 `hanxidata/` 的任何新功能（快照、备份、导出）默认把 `runtime/` 整目录按"不可信残留"排除，勿逐个文件判敏；④ 本修复存在一个如实边界：崩溃后若用户**从未再启用** frpc 模块，孤儿残留等到下次激活才被收编——窗口与"整个数据目录躺在盘上"同生死，快照/备份侧已由 ③ 兜住。

### 56. 子窗"关闭即真销毁"被过虑的退出恐惧拦住：beta.10 退出判据是 windowMap 存活总数（Hide 不除名），不挂 Cancel hook 即默认走销毁路径

- **问题现象与错误原因**：给 webapp 模块（外部网址独立子窗）定"X 关闭即真销毁"语义时，撞见一个业界与本项目注释（ddnsgo `ensureConsoleWindow` 旧口径）共有的一层顾虑：担心子窗成为"最后一个可见窗口"，销毁会触发平台退出分支带走整个应用，于是宁选 Cancel+Hide 永不销毁。代价与 #53 同病——每个常驻隐藏子窗养着几十上百 MB 的 WebView2 渲染器与页面堆。顾虑源于对判据的误读：beta.10 的退出触发数的是 windowMap **存活总数**而非可见数，`Hide` 根本不把窗口从计数表除名，且 hanxi 主窗被 Cancel+Hide 拦截恒存在，任何子窗真销毁都归不了零。
- **排查过程**：读 v3.0.0-beta.10 源码链钉死唯一退出点——`PostQuitMessage` 只在 `unregisterWindow`（`pkg/application/application_windows.go:343-352`）里 `len(windowMap)==0 && !options.Windows.DisableQuitOnLastWindowClosed` 成立时发出；窗口按原生 HWND 入 windowMap（`webview_window_windows.go:523`），而 **Hide 只是 `ShowWindow(SW_HIDE)` 不除名**（`:1305-1308`），可见与否不进判据。X 关闭时无 Cancel hook 拦截即走创建期注册的内部真销毁监听器（`webview_window.go:358-365`：置 `unconditionallyClose` → `markAsDestroyed` → 二次 `Close()` → 真销毁 + 除名）。主窗 × 的 Cancel+Hide 拦截在 `internal/app/app.go:455-464`，windowMap 恒 ≥1；旁证：quickmenu `destroyPopup` 真销毁链线上从未出事，吃的就是这个保证。
- **正确做法与标准修复方案**：要"关闭即销毁"的子窗，**不注册任何 WindowClosing Cancel hook 即可**——真销毁是零成本的默认路径，无需注销闭包（#53 讲的是反方向：要隐藏驻留才需要"拦截 hook + 预留注销闭包"成套）。销毁后的收尾（服务层摘窗表、发状态事件）挂在 `OnWindowEvent(Common.WindowClosing)` 监听器里做：同一事件既驱动拦截判定也通知销毁，且 Wails 把监听器包成 goroutine 异步派发（`webview_window.go` emit 内 `go func()`），不阻塞主线程消息循环，拿服务层锁也没有 #53 警示的 InvokeSync 锁反转之虞。
- **避坑防重犯建议**：① "最后一个窗口"的正确判据是 **windowMap 存活总数（含隐藏窗）**——既勿把"唯一可见窗"当"最后窗口"去规避销毁，也别忘了该结论的前提是主窗 Cancel+Hide 恒在计数表，主窗退出策略若改动，本条保证随之失效，需重估；② 残余三重边缘如实记录、不设防：仅当用户**关闭"最小化到托盘"且 × 掉主窗（此时主窗真销毁）、webapp 子窗成为唯一存活窗、再由 TTL 定时器销毁** → windowMap 归零静默退出——此时用户面前本已无主窗，退出正是标准语义；③ `Size()/Position()` 等 InvokeSync 系列在窗口 `isDestroyed` 后返回 0 且**不可恢复**（`webview_window.go:764-772` 守门），几何记忆须在程序化收起（Hide/TTL 销毁）前拉取回写；原生 X 销毁路径无从抢跑，跳过记忆、下次打开落默认几何即可，勿为此给 X 路径重新挂 hook。
### 57. 统一历史接入时撞出两坑：存量 bindings 注释漂移混提交、worktree 裸跑 `go build ./...`/`generate bindings` 缺 dist

- **问题现象与错误原因**：① 在基于 dev 的 worktree 里首次跑 `wails3 generate bindings`，产出的 diff 不止包含本次新增的 `internal/history` 服务，还裹进 frpc `Shutdown`、quickmenu `Dismiss` 两处**与历史毫无关系的文档注释刷新**——这两条 Go 侧注释早已随功能提交合入 dev，但提交者没重跑 bindings，`frontend/bindings` 与源码静默漂移（生成物文件头的中文注释是从 Go doc comment 抄写的，注释一变绑定必变）。若顺手揉进功能提交，就违反原子性；若手工剔掉，`task check` 的 `verify:bindings`（重新生成后 `git diff --exit-code`）又永远红着。② 新 worktree 从未构建过前端，根包 `embedassets.go` 的 `//go:embed all:frontend/dist` 直接报 `pattern all:frontend/dist: no matching files found`——`go build ./...` 与 `wails3 generate bindings`（内部要编译 cmd/hanxi）双双失败，看起来像 Go 代码坏了，实为产物缺失。
- **排查过程**：① 逐文件看 bindings diff：frpc/quickmenu 仅注释行变化、无方法/签名变化，`git log` 对到 dev 上的 S0 清理与轮盘提交；确认 dev 基线本身就过不了 `verify:bindings`，属存量欠账而非本分支引入。② 对照 `git ls-files frontend/dist` 为空（dist 本就被 gitignore）与 DEVELOPMENT.md 构建顺序（先 `npm run build` 产出 dist 再 go build），定位为 worktree 缺构建步骤。
- **正确做法与标准修复方案**：① 漂移清账**单独成提交**（`chore(bindings): 回补 frpc/quickmenu 存量注释漂移`），先提功能绑定生成提交，`frontend/bindings` 整体保持"再生即无 diff"的可验证态；若 diff 里出现非本人改动的**方法级**变化（不止注释），必须停下来对账，勿盲提。② worktree 内先 `node node_modules/vite/bin/vite.js build`（npm run 在 Git Bash 下另见坏 shim 坑）产出 `frontend/dist/` 再跑 `go build ./...` 与 bindings 生成；`wails3 generate bindings` 报 `exec: "go": executable file not found in %PATH%` 是既有 #40（子进程 PATH 截断）本尊，显式前置 `PATH="/c/Users/<user>/sdk/go<ver>/bin:$PATH"` 即可，无需怀疑新代码。
- **避坑防重犯建议**：① **凡动过会被绑定的 Go 方法或其 doc comment，提交前必须重跑 `wails3 generate bindings` 并把产物一并提交**——漂移一旦入 dev，下一个跑生成的人就会替全仓背"大杂烩 diff"；评审时看到 bindings 里冒出无关模块的变化即黄牌。② worktree/CI 起步脚本化：`vite build → go build ./...`（或走 `task`）顺序不可倒。③ `verify:bindings` 只在 bindings 目录整体再生后才有意义，半手工挑 diff 会制造"看着干净再生又红"的死循环。
### 58. Vue Boolean prop  casting 吞掉"三态开关"：UiClipboardField 首版整排按钮不渲染；Git Bash 里 wails3/instance 测试的 PATH 假失败

- **问题现象与错误原因**：① 新组件 `UiClipboardField` 设计用 `showPaste?: boolean` 三态（undefined=跟随 readonly 默认，显传 true/false=覆写），首版挂载后粘贴/复制钮**整体消失**、模板里 `v-if="label || wantPaste || wantCopy"` 恒假。根因是 Vue 运行时对 Boolean 型 prop 的 casting 规则：**缺省即 cast 成 `false`**（发生在 `withDefaults` 合并之前），`props.showPaste ?? !props.readonly` 里 `??` 永远等不到 `undefined`——三态语义被静默压成两态，默认分支全灭且无任何报错。② 另一环境级假象：在受限 Git Bash 里 `wails3 generate bindings` 报 `go command required, not found`、`go test ./...` 出现一批 `instance` 包 "cmd.exe 不可用 / exec: cmd not found" —— 非代码问题，是子进程 PATH 缺 `C:\Windows\System32`（bash 内建命令查找正常，Windows 原生子进程 exec 却找不到 go.exe/cmd.exe）。
- **排查过程**：① 用一次性 spec 把组件挂载后 `throw new Error(w.html())` 导出 HTML，发现 head 区整体是 `<!--v-if-->`，顺推 `wantPaste` 恒 false 才定位到 Boolean casting；② 给失败测试所在 shell 补 `PATH="/c/Windows/System32:$PATH"` 后同一命令全绿，确认与改动无关。
- **正确做法与标准修复方案**：① 组件布尔开关一律设计成**默认态即 false 的"否定/追加"形**（`hidePaste`/`hideCopy`/`allowCopy`），缺省 false 与 casting 结果同值，语义自明；需要"跟随另一 prop"的派生态放 computed 里组合，不依赖 undefined 三态。② 生成绑定与跑 instance 系测试用 `cmd //c "set PATH=C:\Windows\System32;%PATH% && wails3 generate bindings -clean=true -i ./cmd/hanxi ./internal/..."` 形态（Taskfile 口径 `-f` 参数在 cmd 下会被吞成畸形 flag，可省略）；报告 Go 测试基线前先在补好 PATH 的环境复测一次。
- **避坑防重犯建议**：① 写可选 Boolean prop 前先问"缺省 false 是不是我要的语义"——是，则随便 cast；不是（需要三态），**必须**改成否定命名或 string 枚举，别指望 `?? undefined` 分支；happy-dom 的 spec 是最便宜的探针，组件"整块不渲染"优先 dump HTML 看 v-if 落点；② Windows 上"测试红一批但只红同一类（都要 spawn cmd.exe/go）"先怀疑 shell PATH，再怀疑代码；给同事的复现命令请附 PATH 前提；③ 组件的粘贴钮/复制钮显隐属交互契约，spec 里"默认见 X 不见 Y"要逐态钉死（本次补的 8 例已锁）。
### 59. `wails3 generate bindings` 报 "go not found"：原生工具看不见 bash profile 注入的 PATH

- **问题现象与错误原因**：worktree 根执行 `wails3 generate bindings -clean=true -i ./cmd/hanxi ./internal/...`，先打出 `Processed: 0 Packages, 0 Services...` 随即 `ERROR err: go command required, not found: exec: "go": executable file not found in %PATH%`。同一 shell 里 `go build` 明明正常。根因：`go` 目录只写进了 bash profile（Git Bash 的 `$PATH`），**原生 Windows 可执行文件 wails3.exe 解析的是进程环境块**，`cmd //c "go version"` 同样不识别——即宿主 PATH 里根本没有 go。更险的是 `-clean=true` 若先于失败执行会清空 `frontend/bindings/`（本次幸而失败发生在分析阶段前、git 里也有存量兜底）。
- **排查过程**：`which go` 有 → `cmd //c "echo %PATH%"` 无 go 目录 → 定性为"bash 环境与原生环境分叉"，非 wails 问题。
- **正确做法与标准修复方案**：经 cmd 显式扩 PATH 再调原生工具：`cmd //c "cd /d <worktree> && set PATH=C:\Users\<u>\sdk\go<ver>\bin;C:\Users\<u>\go\bin;%PATH% && wails3 generate bindings -clean=true -i .\cmd\hanxi .\internal\..."`。生成后必须 `git diff --exit-code -- frontend/bindings` 前后对照（Taskfile 的 `verify:bindings` 同一口径），发现存量绑定漂移（如本次 frpc/quickmenu 注释同步）一并提交归零。
- **避坑防重犯建议**：① 凡"bash 里 A 工具调起原生 B 工具"的链路（wails3、git hooks、npm 脚本调 go/exe），报"找不到命令"先怀疑**环境块分叉**而不是安装缺失——判据是一条 `cmd //c "<tool> --version"`；② `-clean=true` 类破坏性开关执行前确认产物在 git 跟踪内可回滚；③ 绑定漂移（生成器输出与提交内容不一致）是 `verify:bindings` 门禁的存在意义，动过任何被绑定的 Go 注释/签名后必须重生成并把 diff 一起提交。

### 60. git 白名单圈定勿用命令级 pathspec：目录"整体消失"后增删再也拍不进去（快照工程）

- **问题现象与错误原因**：快照引擎初版按 PLAN 原案用 `git status --porcelain -z -- config.json state memo` 与 `git add -A -- <存在的根>` 圈作用域，存在两个坑：① `git add -A -- memo` 在 memo/ 不存在时直接 `fatal: pathspec ... did not match any files` 报错（首版实现用 Go 侧 Stat 过滤只传存在的根绕开）；② 更隐蔽——一旦某白名单根**整个消失**（如 memo 库全删），Go 过滤就不再传它，git 对"已跟踪文件被连目录删除"这件事永远没机会上报，历史停在幽灵的最后一版。
- **排查过程**：真实仓库回归时发现"删光 memo/ 后 status 干净"；对照 git 语义：ignore/exclude 只作用于**未跟踪**文件，已跟踪文件的删除永远会进 status。
- **正确做法与标准修复方案**：作用域下沉到 `git-dir` 的 `info/exclude` 反向白名单（`/*` 顶层全忽略 + `!/config.json`、`!/state/`、`!/memo/` 逐条放行 + 中间产物黑名单），命令一律不带 pathspec（`status --porcelain=v1 -z -uall` / `add -A`）；Go 侧 `Whitelisted()` 再做一层纵深过滤兜底（防存量仓库 exclude 落后）。exclude 每次 ensureRepo 重写，升级新增排除模式能补进存量仓库。`--git-dir` 隔离保证这份 exclude 不落进用户目录任何可见文件。
- **避坑防重犯建议**：① "列存在的根再传 pathspec"看似稳妥，实则把**目录消失**这种合法状态当成错误吞掉——作用域优先表达为"仓库自身规则"（exclude），让 git 的已跟踪语义替你兜删除；② 用 git 管非代码数据时，`core.hooksPath` 置空不可靠（空串语义含糊），用 `commit --no-verify` 才是明确跳过用户钩子的口径；③ 判定"内容是否变化"若要精确到字节（原子写原样重写不算变更），别信 mtime，对 KB 级文件直接 sha256 manifest，成本可忽略、碎历史免疫。

### 61. JSON 外科合并的两副暗面：RawMessage 子树被压成单行、回滚守卫把"写坏"误判为"被改"

- **问题现象与错误原因**：MCP 安装向导（`internal/mcpwizard`）为保住用户配置的顶层键与注释外内容，采用 `map[string]json.RawMessage` 解析-改键-回写。两个连撞的坑：① `json.Marshal` 对 `RawMessage` 值只做 compact 不做 indent——嵌套的 `mcpServers` 子树整棵被压成一行写回用户文件，"外科合并"变成"格式毁容"；② 写链按 PLAN 裁定"仅当盘上仍等于我们写的字节才自动回滚"，单测最初用"写坏内容"注入验证回滚，结果回滚被守卫正确拦下——守卫无法区分"自家 writeFn 写坏"与"第三方毫秒级抢改"，此时强行回滚就会覆盖第三方改动，测试前提与实现语义相抵触。
- **排查过程**：① 对照 encoding/json 文档与实测：`MarshalIndent`/`Indent` 都不递归美化 RawMessage；② 回滚用例失败输出里 `Message: …未能自动回滚` 触发对 PLAN 原文再读——守卫的本意正是竞态护栏，不能为测试方便放宽。
- **正确做法与标准修复方案**：① 回写走两步：先 `json.Marshal`（子树压平但语义完整）再对整篇 `json.Indent(&buf, compact, "", 探测缩进)` 统一重排，嵌套层级恢复一致缩进；幂等判定不信字节，比较 `canonicalJSON`（Unmarshal→Marshal 键序归一）后的语义指纹，键序不同视为零改动。② 回滚链测试拆开注入面：`writeFn` 注入"写成功但盘上≠意图"只用来验证**不**回滚（第三方竞态护栏），还原/删除半成品分支改为直接构造 plan 单测 `applyChain`（指纹故意错配、字节仍等于 newData）。
- **避坑防重犯建议**：① 凡"保留未知键"的 JSON 改写，落笔前先确认序列化器对 RawMessage/JsonNode 的缩进行为，测试必须含"嵌套对象不被压平"断言；② 写类安全守卫的语义要写进包注释并据此设计测试，不要为了"测到回滚成功"而绕开守卫的初衷；③ Windows 下路径解析单测期望值一律 `filepath.Join` 拼装，别用 `/` 字面串（分隔符口径必翻车）。
### 62. 离线构建 mcp-go：本地 GOMODCACHE 缺传递依赖 zip，go mod tidy/go get 连环失败（F4 无头 server）

- **问题现象与错误原因**：`GOPROXY=off go mod tidy` 报 `gopkg.in/check.v1: module lookup disabled by GOPROXY=off`；绕开 tidy 直接 `go get github.com/mark3labs/mcp-go/mcp` 又报 `missing go.sum entry`。根因：① tidy 需要为依赖的**测试导入**（yaml.v3→check.v1）解析"最新版本"，离线时无法做 latest 查询；② mcp-go 的传递依赖被 MVS 抬高到本机没货的版本——`spf13/cast` 被 wails v3.0.0-beta.10 的 require 抬到 v1.10.0（下载缓存只有 .mod 无 .zip）、`mailru/easyjson` 被 wk8/go-ordered-map 抬到 v0.7.7（连 .mod 都不全），而 mcp-go 编译需要真正 import 这些包，zip 缺失即断链；③ 试图用显式 `go get mod@低版本` 钉回缓存里有的版本会触发**连环降级**，go get 中途把 wails 判为可移除，一把改坏 go.mod（`removed github.com/wailsapp/wails/v3`）。
- **排查过程**：`ls $GOMODCACHE/cache/download/<mod>/@v/` 逐模块核对 .mod/.zip/.ziphash 三件套完整性（只看 `pkg/mod` 解包目录会误判——解包存在不代表 zip 与 ziphash 还在）；确认 `curl proxy.golang.org` 不可达后放弃补拉，转缓存内自洽方案。
- **正确做法与标准修复方案**：只允许**升级方向**的钉版本：`easyjson` 升到缓存完整的 v0.9.0（MVS max，无降级风险）；`cast` 被 wails 图锁死在 v1.10.0 且其 zip 无缓存，用 `go mod edit -replace github.com/spf13/cast=github.com/spf13/cast@v1.7.1`（v1.7.1 恰是 mcp-go 自身要求的版本，wails 源码并不 import cast，替换仅影响取源不影响语义）。随后 `GOPROXY=off GOFLAGS=-mod=mod go get github.com/mark3labs/mcp-go/mcp@v0.41.1 github.com/mark3labs/mcp-go/server@v0.41.1` 一次成功，`go build ./... && go mod verify` 全绿。恢复网络后必须跑 `go mod tidy` 复核并酌情撤除 replace。
- **避坑防重犯建议**：① 离线可行性以 `cache/download/<mod>/@v/*.ziphash` 为准，不以解包目录为准；② 永远不要用 `go get mod@低版本` 逆着 MVS 钉依赖——会引发包括"误删在用模块"在内的连锁改写，升级方向（max 语义）才是安全的；③ 无网时 `go mod tidy` 因"依赖的测试依赖"报错属已知行为，用显式 `go get` 逐包补齐替代，但**最终验收仍要在有网环境跑一次 tidy**；④ `go get` 前 `git stash` 或确保 go.mod 干净，失败即 `git checkout -- go.mod go.sum` 回滚重试，别在手脏状态下继续 get。
- **复核结果（2026-09-17，R3）**：网络部分恢复（proxy.golang.org 仍 dial tcp 超时，goproxy.cn 可用）。已 `go mod edit -dropreplace github.com/spf13/cast` + 有网 `go mod tidy` 撤除绕行：cast 按 wails 图自然落 v1.10.0 且 zip 已补拉入缓存；easyjson 维持 v0.9.0（升级方向钉，回退上游 v0.7.7 即本条目红线禁止的逆钉操作）；离线期 go get 多记的间接依赖 `josharian/intern` 被正常 tidy 剪除。`go build ./...`、`go mod verify`、`GOPROXY=off go build ./...`（新 go.mod 离线仍自洽）、`go test ./internal/mcp/... ./internal/mcpwizard/... ./internal/app` 抽查、`go mod tidy -diff` 全绿，绕行 replace 正式退场。

### 63. `-H=windowsgui` 下 stdio MCP 管道实测可用；帧级测试必须自己关写端（否则 io.Pipe 死锁挂 120s）

- **问题现象与错误原因**：PLAN_MCP §1-4 曾把"生产构建 GUI 子系统下 stdio 句柄是否可用"列为需真机验证的工程风险；实测结论：MCP 客户端经管道 CreateProcess 拉起时 stdin/stdout 句柄继承正常（`printf '...' | hanxi-mcp.exe mcp` 得到纯 JSON-RPC 帧、stderr 收 slog、EOF 后退出码 0），风险解除——但**从 cmd 手敲仍看不到输出**（GUI 子系统无控制台），验证必须走管道。测试侧另踩一刀：用 `io.Pipe` 做 stdout 流断言"逐行皆协议帧"时，`StdioServer.Listen` 返回后没人关 `outWriter`，`bufio.Scanner` 永阻塞，测试挂到 120s 超时无信息。
- **排查过程**：临时把 `server.NewStdioServer(...).Listen(ctx, in, out)` 的 out 换成 os.Pipe 复现——确认 Listen 在 stdin EOF 正常返回，是测试读取端没等到 EOF。
- **正确做法与标准修复方案**：Listen 跑在 goroutine，返回即 `_ = outWriter.Close()`；读取端再套 `select { case <-done / case <-time.After(60s) }` 兜底给出明确失败。生产路径 `server.ServeStdio` 自带 os.Stdin/os.Stdout 生命周期，无此问题。
- **避坑防重犯建议**：① 管道冒烟命令模板已验证可用：`printf '<init帧>\n<initialized通知帧>\n<tools/list帧>\n' | <exe> mcp`；release 包验收直接复用；② MCP 日志必须钉死 stderr（`server.WithErrorLogger(log.New(os.Stderr,...))` + InitLogger 的控制台路本身走 stderr），任何库默认写 stdout 都要显式改道；③ io.Pipe 是无缓冲同步管道，测试里"写完关读端"或"读端等不到关写端"都会死锁——goroutine 生命周期必须配对收尾。

### 64. 真子进程「超时强杀」单测勿用 cmd.exe：横幅输出会赢下 deadline 竞态，把超时支验成 EOF 支（R2 安装前自检）

- **问题现象与错误原因**：`mcpwizard` 安装前自检要单测 spawnProbe 的 ctx 超时强杀路径（证明"管杀"接线真实有效）。首版选 `cmd.exe` 冒充挂死子进程、deadline 100ms，预期报「握手超时」——实际 cmd.exe 冷启动后**立刻**向 stdout 吐版权横幅（实测 0.01s 内），probe 读到非协议行提前返回，断言在「超时」与「污染/EOF」两支之间随机器负载摇摆（flaky）。根因：超时支要求子进程在 deadline 前**既不出声也不退场**，而 cmd.exe 两样都不满足；用"随便找个不存在的命令"当挂死替身是对 OS 行为想当然。
- **排查过程**：`-v` 跑出 BADCHILD 日志见横幅行 `"Microsoft Windows [Version ...]"`；换 deadline 到 10ms 又反向竞态（杀进程与读管道互抢）。结论：与其调时序赌运气，不如换行为确定的替身。
- **正确做法与标准修复方案**：改用 `powershell.exe`（OS 组件、Win10/11 必在；开发 shell 可能剥其 PATH，用 `exec.LookPath` + `$SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe` 绝对路径兜底）——其 .NET 冷启动必然 >80ms，且参数非法的报错走 **stderr** 而 stdout 全程静默：deadline 到点时它必然还活着且没出声，ctx Kill 支**确定性**触发。坏协议支反过来仍用 cmd.exe（横幅非 JSON 恰好就是真实污染样本，0.01s 收敛）。
- **避坑防重犯建议**：① 断言"子进程超时被杀"的用例，替身选择标准是 **stdout 静默时长 ≫ deadline**，控制台横幅/欢迎语类程序（cmd、ssh、telnet）一律不合格；② 真 exec 单测一律双保险：测试自身设 watchdog（`select` + `time.After`）+ 子进程侧有 ctx deadline，绝不允许挂到包级超时；③ 自检客户端的纪律与 #63 服务端镜像对称：stdout 非协议行=污染即败（勿容错跳过继续找 id——垃圾能进一次就能进一串），失败原因附 stderr 尾部（本包 `syncBuffer` 截 4KB 展示 200 字）供排障。

### 65. Go 源码里写字面 BOM 即编译失败；GNU sed 替换又会把 \u 当大小写指令吞字（F6 绑定指针解析）

- **问题现象与错误原因**：绑定指针要容忍记事本 UTF-8 BOM，解析函数里写了 `strings.TrimPrefix(string(raw), "<U+FEFF>")`——第二个参数用的是**字面 BOM 字符**（不可见）。`go vet`/`gofmt` 直接报 `paths.go:165:70: illegal byte order mark`：Go 编译器只允许 BOM 出现在**文件首字节**，源码字符串内部的字面 BOM 属于非法输入；测试文件里为造 BOM 场景写下的同类字面量一并爆雷。随后用 `sed -i` 想把 BOM 改回 `"\uFEFF"` 转义，替换串里的 `\u` 又被 GNU sed 解释为"下一字符转大写"的大小写指令，`\uFEFF` 落进文件成了 `FEFF`（u 被吞、F 被大写），越修越花。
- **排查过程**：`sed -n 165p | od -c` 直读字节确认真凶是 `357 273 277`（EF BB BF）三字节字面 BOM；确认 Go 对源码内 BOM 的非法判定后，改走转义路线；转义落盘用 perl（`s/"\x{ef}\x{bb}\x{bf}"/"\\uFEFF"/`）替代 sed 并回读 od 验证。
- **正确做法与标准修复方案**：① 源码中一律用 `"\uFEFF"` 转义写 BOM（零宽字符同理用 `\u200B` 等），永不嵌字面不可见字符；② 批量替换含反斜杠语义的文本用 perl -pe 或编辑器精确替换，不用 GNU sed 的 `\u`-敏感替换串；③ 改完立刻过 `gofmt -l` + `go vet` 门禁（本坑正是 vet 抓出来的）。
- **避坑防重犯建议**：① 凡代码/测试涉及 BOM、NBSP、零宽空格一类不可见字符，成文即转义，评审时 `od -c` 抽查可疑行；② 处理"用户可能用记事本手工编辑的配置文件"（如 hanxi.bind）时，BOM 容忍必须进解析层并有测试用例钉死；③ Windows 路径有效性判定统一 `filepath.IsAbs`——注意其 Windows 口径**要求带卷名**（`\HanxiData` 判相对），跨平台单测构造绝对路径要用 `t.TempDir()` 而非手拼 `\` 前缀。
