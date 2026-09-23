# N4 RAMMap 集成正式方案（开工前机主拍板件）

> 排期出处 [PLAN_REMAINING_WORK](PLAN_REMAINING_WORK.md) W4/N4。机主原始诉求：
> "支持托管安装 RAMMap——微软的这个释放内存的软件"。**本方案先厘清一个关键
> 分歧再动手：要的是 RAMMap 这个 GUI，还是"一键释放内存"这个能力？**

## 0. 阶段 0 侦查实证（2026-09-23，本机网络侧完成）

| 事实 | 证据 |
|---|---|
| 官方 zip 可下载 | `download.sysinternals.com/files/RAMMap.zip` HEAD=200，737KB，Last-Modified 2026-03-26（仍在维护） |
| 包内布局 | `RAMMap.exe` / `RAMMap64.exe` / `RAMMap64a.exe` / `Eula.txt`（三架构并列，非版本目录） |
| **提权 manifest** | 二进制内嵌 `requestedExecutionLevel level="requireAdministrator"`——未提权宿主 CreateProcess 直拒 740（#17 三重契约适用） |
| **无官方哈希** | 微软不为单工具发布 sha256/checksums；下载链完整性只能走 WindTerm 同款降级三层 |
| **无版本历史** | 同址覆盖式"最新版"（URL 无版本号、Last-Modified 会变）——托管家族的"列版本/选版本/防漂移"模型**根本套不上**：Tree.Commit 对"同版本异摘要"必须拒装，而 RAMMap 每次上游更新都触发该拒 |
| 许可 | Sysinternals EULA：免费提供使用（软件按"试用件"分发，无再分发条款）——hanxi 转链/代下载自用边界内，不打包进仓 |

## 1. 路线对比

### 路线 A：托管 RAMMap.exe 本体（登记时字面理解）

按 integrate-github-tool 装一个"GUI 托管模块"：版本管理退化为"当前最新版"单行、
下载走降级三层、启动走 740 三重预告 + 一键提权重启、退出照常 WM_CLOSE+Job。

**代价与收益的错位**：
- 用户打开 RAMMap 后，"释放内存"动作仍然发生在 **RAMMap 自己的窗口里**（点
  Empty → Empty Standby List），hanxi 托管仅省了一次"去官网下载"；
- 但版本管理页对 RAMMap 无意义（没有可选版本这回事）；空有版本八件套的皮。
- 结论：**技术上可行（全部先例可抄），工程上是给一个一次性小工具套全家桶**。

### 路线 B（推荐）：自研"一键释放内存"——直接调 RAMMap 背后的同一个系统调用

RAMMap 的 Empty Standby List 本质是：提权后调
`NtSetSystemInformation(SystemMemoryListInformation, MemoryPurgeStandbyList)`，
令内核丢弃"待机清单"（可回收的文件缓存页；不碰任何进程私有内存、不丢数据——
微软官方对该操作语义，RAMMap 帮助文档同口径）。hanxi 实现：

1. **能力件** `internal/platform/windows/standby.go`：
   `EnablePrivilege(SeProfileSingleProcessPrivilege)` + NtSetSystemInformation 封装；
   非提权态明确报"需管理员"错误（不静默失败）。
2. **提权执行通道**：复用现成一键提权重启基建（#17，rufus/litemonitor/bcu 三件
   已验）——Go 单二进制"自举一次性子命令"先例在 MOOTOOL_ANALYSIS §1 已定调
   （`hanxi mcp` 同款思路）：未提权时经 ShellExecute runas 拉起
   `hanxi --purge-standby`，干完写结果即退，主进程读回收口。
3. **计量与呈现**：sysinfo 已实现的 MEMORYSTATUSEX 快照（AvailPhys）操作前后各
   测一次 →"本次释放 X.X GB"如实数字；入口先做在 **系统信息页内存卡**（天然
   同域），托盘/轮盘一键绑定后置为可选增强。
4. **诚实文案**：释放的是文件缓存——待机清单被清后，热数据下次读盘会慢一轮
   （重新预热）；对"游戏前腾内存/清理后立竿见影的观感"如实标注收益与代价。
   **不做** RAMMap 的 Working Sets/Process Lists 类操作（业界共识是观感工程、
   页错误回抽反而更卡，hanxi 不提供=不背锅）。
5. **红线**：该动作**永不开放给 MCP/AI 面**（MOOTOOL_ANALYSIS §1 对 portkill
   类提权操作的同一纪律）；只存在于 UI 按钮/托盘。

**对比总表**：

| 维度 | A 托管 RAMMap | B 自研能力 |
|---|---|---|
| 满足诉求"一键释放内存" | ✗ 动作仍在 RAMMap 窗口里 | ✓ hanxi 内一键 + 释放量数字 |
| 版本管理价值 | ✗ 无版本可选，全家桶空转 | 不需要 |
| 无哈希/无历史问题 | 撞上（降级三层+防漂移死锁） | 不存在（无第三方二进制） |
| 提权 | 撞 740 三重契约（基建现成） | 同撞提权（基建现成） |
| 新增代码面 | ~2000 行模块全家桶 | ~400 行能力件+接线 |
| 维护风险 | 上游改包布局需跟 | 单系统调用，数十年稳定 |

### 路线 C：最廉价的"够用"版
不托管不开发——在"内存卡"放一个"打开 RAMMap 官方下载页"跳转（浏览器下载后
用户以管理员自运行）。零维护，但"一键"体验没有。可作 B 落地前的一行过渡。

## 1.5 拍板结果（机主 2026-09-23）

**路线 A：托管 RAMMap 本体**（按登记字面诉求，B 推荐未采纳）。实施要点按侦查事实收敛：
- 版本模型：**Last-Modified 日期作版本令牌**（rammap_2026-03-26 形态；"同址覆盖式
  最新版"下的唯一诚实版本语义——日期变=上游发新，CheckUpdate 即比日期）；
- 完整性：无官方摘要 → WindTerm 同款降级三层（bespoke 下载 + 字节数 + CRC + 布局自检）；
- 载荷架构：x64 取 RAMMap64.exe（arm64 取 RAMMap64a.exe，按 GOARCH 解析）；
- 提权三重契约（#17）：Start 走 runas 一次性拉起或提示提权重启 Hanxi——**外部非提权
  Hanxi 对 elevated 子进程：Job 绑不了、WM_CLOSE 投不进、Terminate 打不动**（UIPI），
  托管生命周期语义按 litemonitor 先例收敛（其 requireAdministrator 同族已蹚路）。

## 2. 推荐与拍板点

**推荐 B**（+可选叠加 C 作兜底入口）。核心理由：托管 A 路线的三个硬不适配
（无版本历史/无官方摘要/动作在人家窗口里）说明"托管 RAMMap"是手段性误读，
"一键释放内存"才是目的；B 用现成基建把目的直接做进 hanxi。

**需机主拍板**：①选 A/B/C；②B 的入口先"系统信息页内存卡按钮"还是同时要
托盘命令（建议先页内，托盘下次批）；③释放前弹提权确认属预期打扰，接受度。

## 3. 若选 B 的实施拆分（拍板后）

1. `platform/windows/standby.go` + 单测（非提权路径/privilege 申请失败路径，
   提权成功路径 CI 不可验→真机验收项）；
2. `--purge-standby` 一次性子命令 + ShellExecute runas 拉起通道 + 结果回收
   （临时文件握手，写完即删）；
3. sysinfo 服务新增 `PurgeStandby()` RPC（走调用门；返回 前/后 AvailPhys +
   释放量 + 提权态/拒绝原因），前端内存卡按钮 + 结果 toast；
4. 文案与红线（不接 MCP）落码 + docs 同步；每步独立编译绿的原子提交。
