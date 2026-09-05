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

### 12. 托管 Electron 应用（Recordly）：NSIS 在线安装器四连坑 + 进程名探测契约

- **问题现象与错误原因**：Recordly 上游 Windows 仅有 electron-builder NSIS 在线安装器（无便携 zip），集成托管连续踩四类坑：
  1. **`/D=<目录>` 传参坑**：NSIS 规定 `/D=` 必须是命令行最后一个参数且路径**不带引号**；Go 的 `exec.Command` 给含空格路径自动整体加引号，NSIS 取 `/D=` 之后整段时把尾引号算进目录名，安装落到错误目录；
  2. **oneClick 静默卸载旧安装**：electron-builder NSIS 默认 `oneClick: true`，安装器启动时按 HKCU 卸载注册表 `InstallLocation` **静默卸载上一个安装**——多版本共存目录形同虚设（每装一版抹掉注册表指向的那版），若注册表指向用户自装副本还会被托管动作连带删掉；
  3. **PE 版本抹平预发布后缀**：`v1.3.5-beta.2` 的 `Recordly.exe` FileVersion 读出 `1.3.5`（electron-builder 把 prerelease 抹平进 Windows 数字版本），按 PE 版本识别 beta 安装必错；
  4. **空壳 tag**：上游 `v1.3.4` 有 git tag 但 `releases/tags/v1.3.4` 返回 404（打了 tag 从未发布），另有 v1.2.0 缺安装器只剩 blockmap、v1.3.5-beta.2 刻意不发 `latest.yml`——按 tag 给前端列版本会展示下载不到的版本。
- **排查过程**：`electron-builder.json5` 实证 win target 只有 nsis 且未覆写 oneClick（默认 true）、`artifactName: ${productName}-windows-${arch}.${ext}`；releases API 全量 46 版逐一核对资产名与 digest；GitHub API 交叉验证 tag vs release 存在性；上游 `main.ts`/`updater.ts` 源码实证 `requestSingleInstanceLock` 与 `RECORDLY_DISABLE_AUTO_UPDATES` 官方开关。
- **正确做法与标准修复方案**：
  - 静默安装显式接管原始命令行绕开 Go 引号化：`cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"C:\installer.exe" /S /D=C:\path with space\target`}`（exe 段带引号、`/D=` 段不带）；
  - 托管目录收敛为**单目录** `versions/recordly`（放弃 recordly_X.Y.Z 多版本布局），"切换版本=覆盖安装"；安装前扫描 HKCU Uninstall 子键做**外部安装卫兵**（`DisplayName=Recordly` 且 `InstallLocation` 真实存在且不在托管目录 → 拒绝安装并指引用户），绝不让 oneClick 抹掉用户自装副本；卸载只 `RemoveAll` 自家目录，**绝不调用 Uninstall.exe**（注册表指向哪套安装不可靠）；
  - beta 身份以安装时写入 `hanxi-meta.json` 的远程 tag 为准，PE FileVersion 只做兜底；版本比较必须 semver 预发布感知（`1.3.4-beta.1 < 1.3.4 < 1.3.5-beta.2`，数值核心互认函数供 beta/PE 版本互查）；
  - 远程列表以 `/releases?per_page=60` 为唯一数据源，tag 正则放宽到 `vX.Y.Z(-pre.N)` 后按 `prerelease || tag带后缀` 双依据标 IsPre，stable/beta 双通道（默认 stable），缺 digest/缺安装器资产的残缺发布一律不入列；校验链 = GitHub digest sha256（主）+ 官方 `SHA256SUMS.txt` 交叉比对（第二只眼，网络失败降级放行、两源矛盾硬失败）；
  - Electron 探测无稳定互斥体名可依赖（Chromium 进程单例对象名不可预期），一律**进程名 `Recordly.exe` + EnumWindows 按进程名过滤**（窗口要求可见且带标题）；`wait()` 判外部接管前先等 ~500ms 让 Chromium 子进程树收敛——进程名探测是树级信号，自有主进程刚退时 helper 残影会被误判成外部实例（ccswitch/bcu 的互斥体方案没有这个问题，这是移植托管模板到 Electron 时的结构性差异）；
  - 启动注入 `RECORDLY_DISABLE_AUTO_UPDATES=1`（上游官方 env 开关），否则 electron-updater 的 `quitAndInstall` 会按注册表覆写托管目录；WM_CLOSE 必须按进程名过滤，**绝不能** `FindWindow("Chrome_WidgetWin_1")`——那是全体 Chromium 应用共享的窗口类，会误伤用户浏览器。
- **避坑建议**：
  1. 托管 NSIS 类上游前先读 `electron-builder.json5` 的 win/nsis 配置判定 oneClick/perMachine/shortcut 默认值，再决定目录布局；"GitHub 有 zip 资产"≠ Windows 便携版（electron-builder 的 `Recordly-x64.zip` 是 macOS 自动更新产物）；
  2. NSIS 安装器会顺手创建桌面/开始菜单快捷方式（指向托管目录、可绕过 Hanxi 启停），安装成功后立即清理，"桌面快捷方式"作为 Hanxi 侧显式功能提供；上游若存在真托盘语义则另议；
  3. 未签名安装器的信任链完全押在双源 sha256 上：`SHA256SUMS.txt` 这类官方旁证资产要利用起来，格式解析容忍 GNU（`<hash>  <name>`）与 BSD（`<hash> *<name>`）两式；
  4. 安装器体积 ~214MB、装后 ~700MB，远大于既有 tauri 模块：下载超时给到分钟级，多版本目录设计前先量磁盘成本；
  5. Electron 冷启动到主窗口就绪实测 2~10s，`readyTimeout` 给 30s，勿照抄 tauri 的 20s。

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
  - `seedManagedSettings`：**仅当 settings.json 不存在**时写最小种子 `{"AutoCheckUpdate":false}`——上游反序列化大小写不敏感且缺失字段回属性默认值（源码实证），最小种子=全默认首启+关内置更检查；文件已存在**一字节不动**（用户后续在 LiteMonitor 内改的配置是明确意图，不越权覆盖，everything ini 改写先例的收敛版）；
  - `normalizeFileVersion()` 仅当第四段为 `0` 时裁剪三段再比对；布局自检锚点弃用 settings.json（zip 不含）改用 `resources/lang/zh.json`；GBK 乱码文件名条目照常读满保 CRC，自检只锚定 exe+语言包不受干扰；ImportLocal 因 settings/themes/plugins 全随 exe 目录，**整套目录递归迁移**（收单层包装目录形态），并跳过 `settings.json.tmp/.bak` 运行期垃圾。
- **避坑防重犯建议**：
  1. **侦查清单第 4 项要读互斥体名的构造代码**：名称是否"路径/机器派生"决定探测可行性——固定 identifier 名（tauri/QuickLook）可 OpenMutex，路径派生名只能进程枚举；
  2. **`grep requestedExecutionLevel` 纳入必查项**：requireAdministrator 应用给出三重预告——740 特判文案、UIPI 下唤窗/关窗仅在 Hanxi 同或更高权限时可靠、首启 UAC 属预期交互；
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
