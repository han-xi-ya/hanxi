# HubKit 踩坑记录与问题排查指南 (Troubleshooting & Best Practices)

> 💡 **目的**：记录项目在开发、调试、编译与重构过程中遇到的典型错误、隐性 Bug 与技术陷阱，归纳**正确做法与标准规范**，避免重复踩坑。

---

## 目录

1. [前端：Wails v3 事件总线与插件启停热同步](#1-前端wails-v3-事件总线与插件启停热同步)
2. [前端：大流量日志流内存与 DOM 性能问题](#2-前端大流量日志流内存与-dom-性能问题)
3. [后端：Windows 孤儿进程与 JobObject 作业隔离](#3-后端windows-孤儿进程与-jobobject-作业隔离)
4. [后端：DPAPI 凭据加密与临时配置文件生命周期](#4-后端dpapi-凭据加密与临时配置文件生命周期)
5. [Git：规范化中文原子提交](#5-git规范化中文原子提交)
6. [构建：UPX 压缩 Go 程序导致运行时内存暴涨](#6-构建upx-压缩-go-程序导致运行时内存暴涨)
7. [WiFi 密码：netsh 解析三连坑（GBK 编码 / $ 行锚 / 提权输出通道）](#8-wifi-密码netsh-解析三连坑gbk-编码--行锚--提权输出通道)

---

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

- **问题现象**：HubKit 异常退出或被任务管理器结束时，启动的 `frpc.exe` 依然在后台运行并占用端口。
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

## 分片续传 O_APPEND 盲追加导致文件数据错位损坏

- **问题现象与错误原因**：局域网文件快传的分片上传用 `os.OpenFile(full, O_CREATE|O_APPEND|O_WRONLY)` 追加分片。若某分片在网络传输中**超时中断、但服务端已写入部分字节**（前 4MB 正常、后续分片卡住即是此症候选），客户端错误地把 `uploaded` 停留在旧偏移，重试时又从头传同一段 → 服务端在文件末尾重复追加 → 最终文件内容错位损坏。
- **排查过程**：单测仅验证「连续完整分片」路径；真实场景超时重试路径从未覆盖。审查 `handleUploadAppend` 时发现追加前完全不校验片文件当前长度与客户端续传起点的一致性。
- **正确做法与标准修复方案**：追加请求必须携带 `offset` 参数（客户端已确认上传完成的字节数）；服务端追加前 `os.Stat(full)` 校验 `info.Size() == offset`，不匹配则返回 **409 `{uploaded: 实际长度, reset: true}`**；前端收到 409 后先 `DELETE /api/upload/abort` 清理残留分片、`uploaded=0` 从零重传。取消上传（`xhr.abort()`）注意监听 `onabort` 事件让 Promise 落定，否则 await 永远挂起。
- **避坑建议**：凡涉及「断点续传 + 追加写」的传输协议，一律把**服务端权威字节数**（append 响应的 `uploaded`、status 查询）作为唯一真源，客户端偏移只能由服务端响应驱动；追加写前必须做偏移对齐校验，宁可 409 清片重传，也不静默容忍错位。

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
