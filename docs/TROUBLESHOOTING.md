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
