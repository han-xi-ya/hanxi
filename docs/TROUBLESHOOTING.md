# HubKit 踩坑记录与问题排查指南 (Troubleshooting & Best Practices)

> 💡 **目的**：记录项目在开发、调试、编译与重构过程中遇到的典型错误、隐性 Bug 与技术陷阱，归纳**正确做法与标准规范**，避免重复踩坑。

---

## 目录

1. [前端：Wails v3 事件总线与插件启停热同步](#1-前端wails-v3-事件总线与插件启停热同步)
2. [前端：大流量日志流内存与 DOM 性能问题](#2-前端大流量日志流内存与-dom-性能问题)
3. [后端：Windows 孤儿进程与 JobObject 作业隔离](#3-后端windows-孤儿进程与-jobobject-作业隔离)
4. [后端：DPAPI 凭据加密与临时配置文件生命周期](#4-后端dpapi-凭据加密与临时配置文件生命周期)
5. [Git：规范化中文原子提交](#5-git规范化中文原子提交)

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
