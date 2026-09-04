# Hanxi 全仓 Bug 审查报告

> 审查日期：2026-09-04  
> 审查分支：`dev`  
> 审查方式：全仓只读静态审查、跨模块实现对比、前后端调用链追踪、构建与测试验证  
> 当前状态：待修复  
> 说明：本文只记录具备明确触发条件的已确认问题；同构问题合并描述。审查期间未修改业务代码、未提交、未推送。

## 1. 结论摘要

项目当前风险主要集中在以下六个方向：

1. **FileShare 安全边界失效**：访问口令未实施、服务监听所有网卡、目录沙箱可被 Junction/符号链接绕过。
2. **下载供应链信任模型不成立**：部分模块从同一第三方代理获取资产和摘要，代理可同时伪造二者。
3. **Windows 进程生命周期不可靠**：存在 PID 复用误杀、重复启动覆盖进程句柄、JobObject 收编窗口和幽灵运行态。
4. **配置及凭据可能损坏或泄露**：损坏 JSON 被当空数据覆盖，DPAPI 失败被吞，持久化失败后内存与磁盘分叉。
5. **安装流程缺少事务边界**：下载锁过早释放、直接写最终目录、卸载与启动/下载并发、解压无配额。
6. **前端异步请求缺少代际隔离**：旧响应覆盖新状态、轮询复活、跨账号或跨项目串写。

建议先处理 P0/P1，再进行模块模板级重构，避免逐文件修补后继续复制同类错误。

---

## 2. P0：安全边界与供应链

### BUG-001：FileShare 的 `AuthToken` 配置完全未生效

- **严重度**：P0
- **位置**：
  - `internal/modules/fileshare/models.go:13`
  - `internal/modules/fileshare/server.go:119-145`
  - `internal/modules/fileshare/server.go:213-240`
  - `internal/modules/fileshare/service.go:38-43`
- **触发条件**：用户设置访问口令后，从未携带口令的局域网设备或网页调用 FileShare 接口。
- **错误结果**：仍可浏览、下载、上传文件和投递文本；访问控制形同虚设。
- **附加风险**：服务绑定 `0.0.0.0`，并允许任意来源 CORS；恶意网页可能直接访问本机局域网服务。
- **建议方向**：默认仅监听明确选择的地址；为所有业务路由统一增加鉴权中间件；使用恒定时间比较；限制 CORS 来源；无口令模式需明确警告。

### BUG-002：FileShare 可通过 Junction/符号链接逃逸共享根目录

- **严重度**：P0
- **位置**：
  - `internal/modules/fileshare/server.go:288-308`
  - `internal/modules/fileshare/server.go:311-376`
  - `internal/modules/fileshare/server.go:380-405`
  - `internal/modules/fileshare/server.go:430-499`
- **触发条件**：共享目录中存在指向目录外的 Windows Junction、重解析点或目录符号链接。
- **错误结果**：远程客户端可读取共享根之外的文件；启用上传时还可能向目录外写入文件。
- **根因**：当前只做 `Clean/Join/Rel` 词法检查，后续 `ReadDir`、`ServeFile`、`CreateTemp` 会跟随链接。
- **建议方向**：解析真实路径并验证仍位于固定根目录；逐路径组件拒绝重解析点；上传发布前再次验证父目录身份，避免 TOCTOU。

### BUG-003：第三方 GitHub 代理可同时伪造安装包与摘要

- **严重度**：P0
- **代表位置**：
  - `internal/modules/ddnsgo/version/remote.go:30-36`
  - `internal/modules/ddnsgo/version/remote.go:61-94`
  - `internal/modules/ddnsgo/version/remote.go:171-185`
  - `internal/modules/ddnsgo/version/downloader.go:14-22`
  - `internal/modules/ddnsgo/version/downloader.go:120-129`
- **同类模块**：`bcu`、`ccswitch`、`flclash`、`keyviz`、`litemonitor`、`mangodisk`、`nanazip`、`piclite`、`quicklook` 等。
- **触发条件**：GitHub 直连失败，第三方 API/下载代理被控制或遭入侵。
- **错误结果**：代理同时返回恶意资产和匹配的伪造 SHA-256，现有完整性校验仍通过，恶意 EXE 随后被安装执行。
- **建议方向**：摘要必须来自独立可信源；优先使用上游签名或固定公钥验证；代理只能传输资产，不能同时成为元数据和摘要信任根。

### BUG-004：PaperTodo 在缺少可信摘要时仍接受可执行文件

- **严重度**：P0
- **位置**：
  - `internal/modules/papertodo/version/remote.go:159-182`
  - `internal/modules/papertodo/version/downloader.go:14-22`
  - `internal/modules/papertodo/version/downloader.go:49-96`
  - `internal/modules/papertodo/version/manager.go:184-220`
  - `internal/modules/papertodo/version/manager.go:368-375`
- **触发条件**：下载代理返回相同声明长度、具有 `MZ` 文件头但没有有效版本资源的恶意 EXE。
- **错误结果**：文件通过弱校验并被安装执行。
- **根因**：本地 SHA-256 仅作事后记录，没有与可信值比较；PE 版本资源读取失败时降级放行。
- **建议方向**：没有可信签名或摘要时默认拒绝自动安装，或要求用户明确确认不受信来源。

### BUG-005：微信和 frpc 凭据保护不完整

- **严重度**：P1
- **位置**：
  - `internal/settings/store.go:15-19`
  - `internal/settings/store.go:318-324`
  - `internal/modules/frpc/store.go:70-103`
  - `internal/modules/frpc/service.go:292-355`
- **错误结果**：`BotToken`、`ContextToken`、frpc 代理 `SecretKey` 以及部分运行 TOML 以明文落盘；异常退出后临时配置可能长期残留。
- **建议方向**：所有凭据统一使用 DPAPI 封装；运行配置使用严格 ACL 的临时文件，并在正常退出、异常退出和应用 Shutdown 路径统一清理。

---

## 3. P0/P1：Windows 进程与托管实例生命周期

### BUG-006：提权查杀丢失进程身份，PID 复用时可能误杀

- **严重度**：P0
- **位置**：
  - `internal/modules/portkill/service.go:194-223`
  - `cmd/hanxi/main.go:51-70`
  - `internal/platform/windows/process.go:97-130`
- **触发条件**：目标进程在等待 UAC 或重新打开句柄期间退出，PID 被新进程复用。
- **错误结果**：管理员 helper 可能终止无关进程。
- **附加问题**：PowerShell 未显式传播 helper 退出码，查杀失败仍可能被报告为成功；提权 helper 没有完整复用关键系统进程保护策略。
- **建议方向**：传递并复核 PID、创建时间、映像路径；验证和终止必须使用同一个具备 query/terminate 权限的 handle；helper 通过可靠 IPC 返回结构化结果和真实退出码。

### BUG-007：子进程可在 JobObject 绑定前逃逸

- **严重度**：P1
- **代表位置**：`internal/modules/frpc/instance/instance.go:170-191`
- **同类位置**：BCU、CC Switch、Everything、FlClash、MangoDisk、MarkerOn、Snipaste 等实例引擎。
- **触发条件**：外部程序在 `cmd.Start()` 后、`job.Assign(pid)` 前立即创建 updater、daemon 或其他子进程。
- **错误结果**：事后把父进程加入 JobObject 不会追溯收编已存在的子进程；关闭 Hanxi 后可能残留进程树。
- **建议方向**：以 suspended 状态创建进程，先 Assign 到 JobObject，再 resume。

### BUG-008：重复启动覆盖 `cmd/job`，造成双重 Wait 和失管进程

- **严重度**：P1
- **代表位置**：
  - `internal/modules/frpc/instance/manager.go:36-50`
  - `internal/modules/frpc/instance/instance.go:141-205`
  - `internal/modules/frpc/instance/instance.go:312-330`
  - `internal/modules/bcu/instance/instance.go:94-120`
  - `internal/modules/bcu/service.go:239-270`
- **同类模块**：BCU、CC Switch、Everything、FlClash、MangoDisk、MarkerOn、ddns-go、GuoheView。
- **触发条件**：连续双击打开、并发 RPC、对同一 frpc 项目连续调用 Start。
- **错误结果**：第二次启动覆盖共享 `cmd/job/pid`；旧进程失去管理；两个 waiter 可能等待同一 `exec.Cmd`；Stop 只能终止新进程。
- **建议方向**：引擎在持有启动锁后必须拒绝 `starting/running`；使用单调 generation；每个 waiter 捕获本次私有 `cmd/job`。

### BUG-009：失败启动的旧 waiter 会等待并清理新进程

- **严重度**：P1
- **代表位置**：
  - `internal/modules/ddnsgo/instance/instance.go:206-227`
  - `internal/modules/ddnsgo/instance/instance.go:527-545`
  - `internal/modules/guoheview/instance/instance.go:380`
  - `internal/modules/ccswitch/instance/instance.go:128-149`
  - `internal/modules/ccswitch/instance/instance.go:371-389`
- **触发条件**：第一次启动在 JobObject 创建或绑定阶段失败，异步 waiter 尚未执行，用户立即重试。
- **错误结果**：旧 waiter 从共享 `e.cmd` 读到新命令，对新进程调用 `Wait`，并可能关闭新 Job、清空新状态。
- **建议方向**：改为 `wait(cmd, job, generation)`；清理前确认当前 generation 仍匹配。

### BUG-010：短命进程退出后被回写成幽灵 `running`

- **严重度**：P1
- **代表位置**：
  - `internal/modules/bcu/instance/instance.go:156-157,377-411`
  - `internal/modules/ccswitch/instance/instance.go:157-158,371-405`
  - `internal/modules/everything/instance/instance.go:179-180,412-447`
  - `internal/modules/frpc/instance/instance.go:203-207,312-347`
  - `internal/modules/ddnsgo/instance/instance.go:237-258`
  - `internal/modules/guoheview/instance/instance.go:159-160`
- **触发条件**：程序因配置错误、缺运行库或单实例机制而瞬间退出。
- **错误结果**：waiter 先写入 stopped/failed 并清空进程，`Start()` 随后无条件写入 running；UI 显示运行中但没有有效 PID/命令。
- **建议方向**：先发布本代 running 状态再启动唯一 waiter，或在发布 running 前验证 generation 和进程仍存活。

### BUG-011：部分模块 JobObject 终止失败后不回退 `Process.Kill`

- **严重度**：P1
- **代表位置**：`internal/modules/bcu/instance/instance.go:241-252`
- **同类模块**：CC Switch、Everything、FlClash、MangoDisk。
- **触发条件**：`TerminateJobObject` 因句柄或权限异常失败。
- **错误结果**：即使仍持有进程句柄也直接返回错误，进程继续驻留。
- **建议方向**：统一采用 MarkerOn/frpc/Snipaste 已有的 Job 终止失败后回退 `Process.Kill` 模式。

### BUG-012：启动外部程序后未 `Wait` 或 `Process.Release`

- **严重度**：P2
- **位置**：
  - `internal/app/service.go:68-130`
  - `internal/platform/windows/shortcut.go:118-120`
  - `internal/modules/everything/service.go:346-367`
- **触发条件**：反复打开目录、文件、浏览器或系统面板。
- **错误结果**：Windows process handle 在 Hanxi 生命周期内持续累积。
- **建议方向**：无需等待结果时，在 Start 成功后立即 `Process.Release()`；需要退出信息时后台 `Wait()`。

---

## 4. P1：并发、服务和模块门禁

### BUG-013：FileShare 停服与 handler 回调形成锁循环

- **严重度**：P1
- **位置**：
  - `internal/modules/fileshare/service.go:117-128`
  - `internal/modules/fileshare/service.go:143-157`
  - `internal/modules/fileshare/server.go:168-190`
- **触发条件**：上传/下载完成回调与 StopServer 同时发生。
- **错误结果**：Shutdown 等待 handler，handler 等待 service 锁，稳定耗尽三秒超时；服务可能被标为停止但连接仍存活。
- **建议方向**：不要持 service/server 主锁调用 `Shutdown`；先在锁内交换本地 server 引用，再解锁关闭。

### BUG-014：FileShare Serve 协程引用可变 `s.server`

- **严重度**：P1
- **位置**：`internal/modules/fileshare/server.go:147-158`
- **触发条件**：Start 后立即 Stop，或快速 Stop→Start。
- **错误结果**：闭包可能读到 `nil` 导致 panic，或把旧 listener 交给新 server。
- **建议方向**：创建局部 `srv` 并在 goroutine 参数中捕获，不读取可变共享字段。

### BUG-015：FileShare 配置热更新与 handler 存在数据竞争

- **严重度**：P1
- **位置**：
  - 写：`internal/modules/fileshare/server.go:193-203`
  - 读：`internal/modules/fileshare/server.go:279-308,415-430,535-547`
- **触发条件**：处理请求期间修改共享目录或上传配置。
- **错误结果**：数据竞争；一次路径校验可能混用新旧 `SharePath`。
- **建议方向**：配置使用不可变快照或原子指针；每次请求只读取一次完整快照。

### BUG-016：FileShare 下载没有写入/空闲超时

- **严重度**：P1
- **位置**：`internal/modules/fileshare/server.go:147-152`
- **触发条件**：客户端持续极慢读取大文件。
- **错误结果**：连接、文件句柄和 goroutine 被无限期占用。
- **建议方向**：设置合理 WriteTimeout，或通过 `ResponseController` 按进度刷新 idle deadline。

### BUG-017：端口扫描旧任务会删除新任务的取消句柄

- **严重度**：P1
- **位置**：`internal/modules/portscan/service.go:50-64`
- **触发条件**：扫描 B 替换并取消扫描 A，A 随后退出并执行 defer。
- **错误结果**：A 无条件删除 `current`，实际删掉 B 的 cancel，之后 StopScan 无法停止 B。
- **建议方向**：任务使用唯一 ID/generation；defer 仅在 map 中仍是自身时删除。

### BUG-018：SOCKS5 不可取消 Dial 可永久泄漏 goroutine

- **严重度**：P1
- **位置**：`internal/modules/portscan/scanner.go:252-272`
- **触发条件**：代理失联且底层 `Dial` 永久卡住，扫描上下文被取消。
- **错误结果**：每个端口最多永久泄漏两条 goroutine。
- **建议方向**：使用原生支持 context/timeout 的代理拨号；不要用额外 goroutine 包装不可取消调用。

### BUG-019：微信监听快速 Stop/Start 会产生多代轮询

- **严重度**：P1
- **位置**：`internal/modules/wechat/listener.go:58-100,182-187`
- **触发条件**：停止后在旧 poll loop 退出前立即重新启动。
- **错误结果**：旧 defer 清除新 running 状态；再次 Start 可创建第三条轮询，导致重复消息和游标并发更新。
- **建议方向**：Stop 等待当前 generation 完全退出；状态更新必须验证 generation。

### BUG-020：Registry 对生命周期字段使用不同锁

- **严重度**：P1
- **位置**：`internal/extapi/registry.go:71-106,147-188`
- **触发条件**：SetEnabled/EnsureActive 与 List/IsActive 并发。
- **错误结果**：`Enabled/initialized` 数据竞争；停用过程中仍可能执行 OnInit。
- **建议方向**：统一锁域，或把每个 wrapper 的全部状态访问都放在 `wrapper.mu` 下；避免先检查后加锁的 TOCTOU。

### BUG-021：模块停用后已注册 Wails RPC 仍可调用

- **严重度**：P1
- **位置**：
  - `internal/extapi/registry.go:124-133`
  - `internal/app/app.go:172-179`
  - `internal/app/service.go:225-227`
- **触发条件**：模块停用后通过旧页面、开发者控制台或生成 binding 调用业务 Service。
- **错误结果**：仍可启动进程、下载版本或修改数据。
- **建议方向**：所有模块 RPC 入口统一经过 Enabled/Active 门禁，不能只依赖前端导航隐藏。

---

## 5. P1/P2：下载、安装与卸载事务

### BUG-022：`downloadMu` 在真实下载开始前释放

- **严重度**：P1
- **代表位置**：
  - `internal/modules/ddnsgo/service.go:126-139`
  - `internal/modules/guoheview/service.go:112-125`
  - `internal/modules/ccswitch/service.go:169-196`
  - `internal/modules/markeron/service.go:106-137`
- **同类模块**：BCU、Everything、FlClash、frpc、MarkerOn、ddns-go、GuoheView。
- **触发条件**：双击下载按钮或并发调用同版本下载 RPC。
- **错误结果**：多个 goroutine 并发创建/截断同一目标文件；失败任务可能 `RemoveAll` 删除另一任务刚完成的安装。
- **正确对照**：Snipaste、MangoDisk 会持锁到后台下载结束。
- **建议方向**：锁的所有权转交给下载 goroutine，并在 goroutine defer 解锁；更好的是使用按模块/版本 keyed singleflight。

### BUG-023：安装直接写最终目录，失败或重试留下混合版本

- **严重度**：P1
- **位置**：
  - `internal/modules/frpc/version/manager.go:398-430`
  - `internal/modules/ddnsgo/version/manager.go:176-177`
  - `internal/modules/guoheview/version/manager.go:179-180`
- **触发条件**：磁盘满、CRC/写回失败、上次中断留下残目录，或并发安装同版本。
- **错误结果**：最终目录出现截断 EXE、旧 DLL/资源残留或新旧文件混合。
- **建议方向**：下载和解压到同卷临时目录，完整校验后原子替换；失败只清理本次临时目录。

### BUG-024：启动或下载过程中仍允许卸载版本

- **严重度**：P1
- **代表位置**：
  - `internal/modules/bcu/service.go:166-174`
  - `internal/modules/ddnsgo/service.go:161-165`
  - `internal/modules/guoheview/service.go:147-151`
  - `internal/modules/everything/service.go:415-423`
  - `internal/modules/recordly/service.go:231-237`
- **触发条件**：实例处于 `starting` 或版本正在后台解压时调用 RemoveVersion。
- **错误结果**：启动文件被部分删除；卸载显示成功但下载随后重建目录；形成残缺安装。
- **Recordly 特殊问题**：`internal/modules/recordly/version/manager.go:250-268` 忽略请求版本并删除固定目录，running 保护还能被不匹配版本参数绕过。
- **建议方向**：启动、下载、激活和卸载共享同一版本操作协调器；按实际安装身份删除，不信任调用方版本字符串。

### BUG-025：ZIP 解压缺少体积配额并忽略 Close 错误

- **严重度**：P1
- **位置**：
  - `internal/modules/ccswitch/version/manager.go:295-344`
  - `internal/modules/bcu/version/manager.go:320-369`
  - `internal/modules/flclash/version/manager.go:300-349`
  - `internal/modules/markeron/version/manager.go:319-368`
  - `internal/modules/everything/version/manager.go:231-280`
- **触发条件**：异常或恶意 ZIP 包含大量 entry、高压缩比大文件或写盘错误只在 Close 暴露。
- **错误结果**：耗尽磁盘；不完整文件仍可能被判为安装成功。
- **建议方向**：限制 entry 数、单项大小和总解压量；拒绝特殊文件类型；检查输入输出 Close 错误。

### BUG-026：安装元数据写入失败仍报告成功

- **严重度**：P2
- **位置**：
  - `internal/modules/frpc/version/manager.go:164-174`
  - `internal/modules/everything/version/downloader.go:101-110`
  - `internal/modules/markeron/version/manager.go:170-180`
- **错误结果**：用户收到完成事件，但版本缺少来源、摘要和安装时间，后续完整性判断错误。
- **建议方向**：元数据属于安装事务的一部分；写入失败必须回滚安装或明确标记为不完整。

### BUG-027：无版本 EXE 用当前秒作为导入身份

- **严重度**：P2
- **位置**：
  - `internal/modules/ccswitch/version/manager.go:245-252`
  - `internal/modules/ccswitch/version/version_test.go:312-315`
  - `internal/modules/everything/version/manager.go:144-151`
  - `internal/modules/bcu/version/manager.go:257-264`
  - `internal/modules/flclash/version/manager.go:237-244`
  - `internal/modules/frpc/version/manager.go:184-190`
- **触发条件**：同一无可信 FileVersion 的 EXE 跨秒再次导入。
- **错误结果**：生成新目录并绕过重复检测；版本列表出现重复副本。现有测试立即重复调用，只在同一秒时碰巧通过。
- **建议方向**：无法读取可信版本时拒绝导入，或以文件 SHA-256 派生稳定身份；导入时间只作元数据。

### BUG-028：部分 `ResolveExe` 只验证目录，不验证 EXE

- **严重度**：P2
- **位置**：
  - `internal/modules/markeron/version/manager.go:191-207`
  - `internal/modules/ccswitch/version/manager.go:211-232`
  - `internal/modules/bcu/version/manager.go:223-241`
- **触发条件**：EXE 被手工删除、杀毒软件隔离或安装目录为半成品。
- **错误结果**：损坏版本仍能被设为活动版本，错误延迟到启动阶段。
- **建议方向**：Resolve/SetActive 时验证实际可执行文件、必要资源和安装元数据。

---

## 6. P1/P2：配置、凭据与数据一致性

### BUG-029：旧配置缺失字段时不会应用默认值

- **严重度**：P1
- **位置**：`internal/settings/store.go:94-107,133`
- **触发条件**：旧版本或部分 `config.json` 缺少新增字段。
- **错误结果**：`MinimizeToTray` 由默认 `true` 变成 `false`，`LogRetainDays` 由 `7` 变成 `0`，Theme/Language/BaseURL 可能为空。
- **建议方向**：先创建 `DefaultSettings()`，再把 JSON 解码到默认对象；为配置结构增加显式 schema version 和迁移测试。

### BUG-030：持久化失败后内存状态和副作用不回滚

- **严重度**：P1
- **位置**：
  - `internal/settings/store.go:154-160,323-330`
  - `internal/extapi/registry.go:170-186`
- **同类范围**：多个模块 Store 的 `SetActive`、`SetFollowOnExit`。
- **触发条件**：磁盘满、目录只读、Rename 失败或安全软件占用文件。
- **错误结果**：API 返回错误，但当前进程已经使用新状态；重启后回退；后续任意成功保存还可能把此前失败修改延迟提交。
- **建议方向**：copy-on-write；先构造候选快照并原子保存，成功后再提交内存；生命周期副作用应在持久化成功后执行或支持补偿回滚。

### BUG-031：frpc DPAPI 失败处理会泄露或永久破坏 Token

- **严重度**：P1
- **位置**：`internal/modules/frpc/store.go:58-64,79-84`
- **触发条件**：配置复制到其他用户、DPAPI 数据损坏、用户配置异常或加密调用失败。
- **错误结果**：
  - 解密失败后把 `dpapi:` 密文当 Token 使用；
  - 下次保存对密文再次加密，原 Token 永久不可恢复；
  - 加密失败被吞时可能直接把明文写入配置。
- **建议方向**：任何加解密失败必须阻止加载/保存并给出可恢复错误；禁止自动覆盖原密文。

### BUG-032：损坏的 frpc/Memo JSON 被当空数据并覆盖

- **严重度**：P1
- **位置**：
  - `internal/modules/frpc/store.go:31,49-50,144-146`
  - `internal/modules/memo/service.go:31-34,167-170`
- **触发条件**：配置被截断或 JSON 损坏，随后用户新增或保存数据。
- **错误结果**：程序以空集合启动并覆盖原文件，造成不可逆数据丢失。
- **建议方向**：解析失败时进入只读故障状态；保留并隔离损坏文件；提供备份恢复，禁止以空数据自动覆盖。

### BUG-033：`Store.Get()` 返回共享的微信账号切片

- **严重度**：P2
- **位置**：`internal/settings/store.go:142-151`
- **错误结果**：调用者可绕过 Store 锁和持久化修改账号数据；并发访问产生 data race。
- **建议方向**：对所有 map、slice 和嵌套引用对象执行完整深拷贝，或返回不可变 DTO。

### BUG-034：微信附件覆盖失败时先删除原文件

- **严重度**：P1
- **位置**：`internal/modules/wechat/download.go:105-111`
- **触发条件**：首次 Rename 失败，删除旧目标后第二次 Rename 又因权限、占用或 I/O 错误失败。
- **错误结果**：原文件已丢失，新文件也未成功发布。
- **建议方向**：不要通过先删除旧文件模拟原子覆盖；使用同目录临时文件和 Windows 原子替换 API，失败时保留旧目标。

### BUG-035：微信附件未校验最终解密长度

- **严重度**：P2
- **位置**：`internal/modules/wechat/download.go:19-59`
- **触发条件**：CDN 返回截断内容、错误对象，或 expectedSize 为零但 AES padding 合法。
- **错误结果**：损坏或串错附件被保存并报告成功。
- **建议方向**：校验最终明文长度、内容摘要和媒体元数据；不一致时删除临时文件并报错。

---

## 7. 新模块专项

### BUG-036：ddns-go 仅凭 TCP 端口开放判断就绪

- **严重度**：P1
- **位置**：
  - `internal/modules/ddnsgo/instance/instance.go:266-267`
  - `internal/modules/ddnsgo/service.go:243`
- **触发条件**：端口预检后被其他程序抢占，或默认端口运行着其他 HTTP 服务。
- **错误结果**：无关服务被判为 ddns-go running，并被嵌入控制台；用户凭据可能发送到错误服务。
- **建议方向**：校验本次 PID、监听端口归属及服务特征响应；外部实例发现不能只探测默认端口。

### BUG-037：GuoheView 就绪探测接受任意同名进程窗口

- **严重度**：P1
- **位置**：`internal/modules/guoheview/instance/probe_windows.go:60-68`
- **触发条件**：托管版本启动期间，用户手工打开另一个 GuoheView 实例。
- **错误结果**：外部窗口使托管启动提前成功，即使本次启动进程尚未出窗或随后失败。
- **建议方向**：只接受本次托管 PID 或明确子进程树拥有的窗口。

### BUG-038：GuoheView 就绪超时后仍保留 running 实例

- **严重度**：P1
- **位置**：`internal/modules/guoheview/service.go:256`
- **触发条件**：进程存活但初始化卡死、弹窗阻塞或没有主窗口。
- **错误结果**：OpenWindow 返回超时，但实例保持 running；后续只尝试 Focus，无法通过重试恢复。
- **建议方向**：超时后终止本代实例或转换为可恢复 failed 状态。

### BUG-039：GuoheView beta/stable 版本比较只比较最后一段

- **严重度**：P2
- **位置**：`internal/modules/guoheview/version/remote.go:182`
- **错误结果**：旧主版本但 build 较大的 beta 被误判为更新，或更高主版本但 build 较小的 beta 被隐藏。
- **建议方向**：使用完整语义版本比较，并明确 prerelease 通道排序规则。

### BUG-040：Recordly 忽略预发布后缀判断已安装

- **严重度**：P2
- **位置**：
  - `internal/modules/recordly/service.go:201-205`
  - `internal/modules/recordly/version/manager.go:291-306`
- **触发条件**：稳定版与相同 core 的 beta 互相切换，或 beta.1 升 beta.2。
- **错误结果**：返回 `already-installed`，无法切换通道或升级预发布构建。
- **建议方向**：安装身份使用完整 tag，而不是只比较 core version。

---

## 8. 前端异步与状态同步

> 已核对当前代码：此前“`App.vue` 先切路由后激活模块”的报告来自过期隔离工作树，当前 `frontend/src/App.vue:152-167` 已先等待 `EnsureModuleActive`，并使用 `navigationRequestID` 防止乱序，因此该项明确排除。

### BUG-041：Everything 搜索吞掉新查询并回填旧结果

- **严重度**：P1
- **位置**：`frontend/src/views/EverythingView.vue:284-329`
- **触发条件**：搜索 A 尚未完成时输入 B，或清空输入框。
- **错误结果**：B 因 `searching` 被直接丢弃且不排队；A 返回后覆盖当前结果，空输入下也可能重新出现旧结果。
- **建议方向**：输入变化立即递增 generation；允许取消或以 latest-wins 模式排队执行最新查询。

### BUG-042：frpc 日志抽屉跨项目串写

- **严重度**：P1
- **位置**：`frontend/src/views/FrpcProjectsView.vue:378-397`
- **触发条件**：A 日志请求未返回时快速打开 B。
- **错误结果**：A 的迟到响应覆盖 B 抽屉，随后 B 的实时日志继续混入。
- **建议方向**：请求携带项目 ID 和 generation；写入前同时校验当前抽屉目标。

### BUG-043：微信二维码弹窗关闭后旧异步流程复活

- **严重度**：P1
- **位置**：
  - `frontend/src/views/WechatBotView.vue:148-178`
  - `frontend/src/views/WechatBotView.vue:203-222`
- **触发条件**：获取二维码过程中关闭弹窗，或关闭后 800ms 内重新打开。
- **错误结果**：在途请求返回后重新启动隐藏轮询；旧 timeout 还可能关闭新弹窗并向错误账号写入旧成功消息。
- **建议方向**：每次绑定流程使用 session generation；关闭时使整代请求、timer 和回调失效。

### BUG-044：FileShare 停服后被旧轮询覆盖回运行中

- **严重度**：P2
- **位置**：`frontend/src/views/FileShareView.vue:110-131,205-232`
- **触发条件**：较早的状态请求在用户停止服务后才返回。
- **错误结果**：UI 被旧快照重新写成运行中。
- **建议方向**：状态轮询使用递增序号；Stop/Start 作为更高优先级状态代际使旧请求失效。

### BUG-045：PortScan 快速停止可能使用上一次任务 ID

- **严重度**：P1
- **位置**：`frontend/src/views/PortScanView.vue:78-123,157-174`
- **触发条件**：扫描 B 启动后、第一条进度事件到达前立即点击停止。
- **错误结果**：调用 `StopScan(A_ID)`；B 继续后台扫描，但 UI 显示已终止。
- **建议方向**：启动 RPC 直接返回新任务 ID；启动前清空旧 ID；停止动作只针对当前 generation。

### BUG-046：`followOnExit` 开关成功后不更新本地状态

- **严重度**：P1
- **已确认位置**：
  - `frontend/src/views/CCSwitchView.vue:265-272`
  - `frontend/src/views/FlClashView.vue:265-272`
  - `frontend/src/views/BCUView.vue:302-309`
  - `frontend/src/views/MarkerOnView.vue:294-301`
  - `frontend/src/views/DdnsGoView.vue:320-329`
  - `frontend/src/views/GuoheViewView.vue:253-260`
- **同构排查范围**：Recordly、QuickLook、LiteMonitor、Keyviz、PicLite、PaperTodo 等托管视图。
- **错误结果**：toast 使用旧值而提示相反；复选框重渲染后跳回；快速双击会提交两次相同目标值，当前会话无法切回原状态。
- **建议方向**：使用 `v-model` 和 busy guard；计算目标值后乐观更新并在失败时回滚，或成功后统一重新加载快照。

### BUG-047：微信 Token 刷新结果写入错误账号

- **严重度**：P2
- **位置**：`frontend/src/views/WechatBotView.vue:263-291`
- **触发条件**：账号 A 刷新期间切换到账号 B。
- **错误结果**：实际刷新 A，但成功/失败系统消息写入 B 的聊天记录。
- **建议方向**：捕获操作开始时的账号 ID，完成后的全部状态和消息都使用该 ID，并验证账号仍存在。

### BUG-048：微信图片/文件发送失败后永久显示发送中

- **严重度**：P2
- **位置**：
  - `frontend/src/views/WechatBotView.vue:417-446`
  - `frontend/src/views/WechatBotView.vue:464-495`
- **错误结果**：异常分支只添加系统错误消息，没有把原乐观消息更新为 failed。
- **建议方向**：为每条乐观消息分配本地 ID，所有完成路径必须进入 sent/failed 终态。

### BUG-049：Memo 查询响应乱序覆盖

- **严重度**：P2
- **位置**：`frontend/src/views/MemoView.vue:51-74,231-241`
- **触发条件**：快速输入多个关键词或查询期间切换标签。
- **错误结果**：旧请求覆盖最新列表和统计；任一旧请求还可能提前清除 loading。
- **建议方向**：防抖加 generation，只有最新请求能更新数据和 loading。

### BUG-050：日志清理后仍选中已删除文件并显示缓存内容

- **严重度**：P2
- **位置**：`frontend/src/views/LogsView.vue:21-27,85-93`
- **触发条件**：选中历史日志后执行清理。
- **错误结果**：`selectedFile` 指向不存在文件；读取失败时旧 `logContent` 未清空，用户误以为日志仍存在。
- **建议方向**：刷新列表后验证 selection；文件不存在时选择首项或清空选择和内容。

### BUG-051：LAN 停止后过早允许再次扫描

- **严重度**：P2
- **位置**：`frontend/src/views/LanScannerView.vue:43-72`
- **触发条件**：点击停止后，在旧 Scan Promise 完全退出前立即再次开始。
- **错误结果**：新请求命中 `scan already in progress`；旧扫描结果还可能继续覆盖页面。
- **建议方向**：取消请求返回不代表任务退出；等待本代 Scan Promise 收尾或以后端任务 ID/完成事件为准。

### BUG-052：PortKill、PublicIP 和 frpc TOML 预览存在旧响应覆盖

- **严重度**：P2
- **位置**：
  - `frontend/src/views/PortKillView.vue:41-67,153-163`
  - `frontend/src/views/PublicIpView.vue:44-85,336-343,428-434`
  - `frontend/src/components/FrpcProjectEditor.vue:342-355`
- **触发条件**：快速切换端口、Ping/Traceroute 目标或连续修改 frpc 表单。
- **错误结果**：较旧请求最后返回，覆盖当前目标或当前表单对应的结果；loading 也可能被旧请求提前清除。
- **建议方向**：采用统一 latest-request-wins composable，集中实现 generation、取消和 loading 所有权。

### BUG-053：MangoDisk 运行时长不基于实际启动时间

- **严重度**：P2
- **位置**：`frontend/src/views/MangoDiskView.vue:158-181`
- **触发条件**：进入已运行一段时间的实例，或 KeepAlive 离开后重新进入。
- **错误结果**：运行时长从页面本地计数继续/重置，未计入页面未激活期间的真实运行时间。
- **建议方向**：每次显示时根据后端 `startedAt` 计算 `Date.now() - startedAt`，ticker 只用于刷新显示。

### BUG-054：托盘菜单允许 `.bat/.cmd`，但直接启动会失败

- **严重度**：P2
- **位置**：
  - `internal/app/service.go:384`
  - `internal/app/tray.go:137-147`
- **触发条件**：用户选择正常存在的 `.bat` 或 `.cmd` 作为托盘自定义菜单命令。
- **错误结果**：`os.Stat` 通过，但 `CreateProcess` 把脚本当 PE 映像启动，通常返回 Windows 错误 193。
- **建议方向**：对批处理显式使用 `%ComSpec% /d /s /c`，并按 `cmd.exe` 引用规则构造命令行；不能直接复用 argv 拆分逻辑。

---

## 9. 验证记录

### 已通过

- `npm --prefix frontend run build`
  - `vue-tsc` 类型检查通过
  - Vite production build 通过
- `go test -count=1 ./internal/...`
- `go vet ./internal/...`
- `go build ./internal/...`
- `git diff --check`

### 环境或构建顺序限制

- 在没有先生成 `frontend/dist` 的隔离工作树中执行 `go test ./...`、全仓 `go vet` 或全仓 `go build`，会在 `embedassets.go:8-9` 的 `//go:embed all:frontend/dist` 失败。
- 这是构建顺序前置条件，不是内部 Go 业务包编译错误。
- `go test -race` 无法在当前环境执行：默认 `CGO_ENABLED=0`；启用 CGO 后环境缺少 GCC。
- 前端没有 lint script，仓库也没有 ESLint、Stylelint 或 Biome 配置。

### 明确排除的误报

- **已排除**：`App.vue` 先切路由后激活模块。
- **依据**：当前 `frontend/src/App.vue:152-167` 先等待 `EnsureModuleActive`，失败时直接返回；只有成功且 `navigationRequestID` 仍有效时才更新 `activeRoute`。
- **注意**：后端“模块停用后 Wails RPC 仍可直接调用”是独立问题，仍然成立。

---

## 10. 推荐修复计划

### 第一阶段：安全止血

- [ ] BUG-001 FileShare 鉴权
- [ ] BUG-002 FileShare 真实路径沙箱
- [ ] BUG-003 第三方镜像独立信任链
- [ ] BUG-004 PaperTodo 无可信摘要安装
- [ ] BUG-006 PortKill 进程身份与提权结果

### 第二阶段：统一托管实例状态机

- [ ] 引入 instance generation
- [ ] waiter 捕获私有 `cmd/job`
- [ ] 引擎内拒绝重复 Start
- [ ] suspended 创建后绑定 JobObject
- [ ] 统一 Stop 的 Job/Process 回退策略
- [ ] 统一 starting/running/external 的版本操作门禁

### 第三阶段：下载与安装事务化

- [ ] keyed singleflight 防并发下载
- [ ] 临时目录解压和原子发布
- [ ] 解压体积、entry 数和文件类型限制
- [ ] 元数据写入纳入安装事务
- [ ] 下载、启动、激活、卸载使用统一协调锁

### 第四阶段：持久化与凭据

- [ ] 默认配置合并与 schema migration
- [ ] Store copy-on-write
- [ ] 损坏 JSON 隔离和备份恢复
- [ ] DPAPI 失败禁止静默降级
- [ ] 微信/frpc 全量敏感字段保护

### 第五阶段：前端异步治理

- [ ] 提取 latest-request-wins composable
- [ ] 为轮询、弹窗流程和长任务引入 generation
- [ ] 统一托管视图开关组件
- [ ] 启动 RPC 直接返回任务 ID
- [ ] 所有乐观消息进入明确终态

---

## 11. 提交拆分建议

建议按下列职责拆分，且每个提交保持可编译、可测试：

1. `fix(fileshare): 修复访问鉴权与共享目录逃逸`
2. `fix(version): 强化第三方下载信任链与安装校验`
3. `fix(portkill): 修复提权查杀进程身份复核`
4. `refactor(instance): 统一托管进程代际与等待状态机`
5. `fix(version): 修复并发下载与安装目录事务`
6. `fix(settings): 修复配置迁移及持久化回滚`
7. `fix(frpc): 修复凭据加解密与损坏配置保护`
8. `fix(frontend): 修复异步响应乱序与轮询跨代`
9. `test(instance): 补充重复启动与瞬退进程回归测试`
10. `test(version): 补充并发下载与重复导入回归测试`
