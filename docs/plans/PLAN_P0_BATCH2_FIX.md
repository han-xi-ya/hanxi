# P0 批 2 正式修复计划（2a 已落地 / 2b 待机主选机制）

> 依据 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md) §3.2/§3.4/§3.5。本批按机主指示拆两步：**2a（journal fail-closed + artifact 原子边界）已完成**；**2b（租约生命周期=审查 §3.2 与 §3.4"中断在途副作用"的共同机制）待选案**。

## 2a ✅ 已完成（2026-09-21，本次提交）

### journal 步进 fail-closed（§3.4，`internal/ops`）
- `BeginTxn`：装配期 OpenStore/Recover 失败（app 侧 `journalFault` 汇总 → `MarkJournalDegraded`）或运行期任何一次记账写盘失败 → **新托管写事务一律拒绝**（"无账执行"焊死）；从未注入内核的无账设计模式（单测/headless）不受波及，以 `kernelLoaded` 区分；
- `Txn.Step` 改签名返回 error：记账失败即时全场挂降级牌 + 本事务按 `journal-degraded` 在观察面收口（Handle 幂等闩保证后续 Done/Fail 不得把失败翻案成成功）；返回错误供调用方提前终止（15 个模块的 emit 闭包暂未消费——在途中断属 2b，本批保证"不新增裸奔、已裸奔如实可见"）；
- 观察面暴露 `AppService.GetJournalHealth()`（批 3 前端据此挂降级横幅/置灰安装入口）;
- 测试：步进失败→降级→拒新事务→终态闩 全链（容器 `-race` 绿）。**如实声明的残余**：在途副作用不即时停止（2b 解决）；模块自身 toast 可能与 journal-degraded 终态不同步（2b 统一）。

### Artifact Commit 原子边界（§3.5，`packages/go/artifact`）
- meta.json **先在 staging 内写全，再整体 rename**——崩溃窗口从"落位后写账前"归零：任何时刻 target 要么不存在、要么自带可信账本；meta 写失败时 staging 原样（错误文案改"未落位可重试"，删除全部回退逻辑）;
- 黑户目录（历史崩溃残留/外来拷贝，无可信 meta）：重装同版本从"拒绝致死锁"改为 **改名 `.corrupt-` 隔离取证后放行重装**——不覆盖、不删除、扫描/解析隐形（与 `.tmp-`/`.removing-` 同族前缀纪律）；
- 测试：隔离放行×取证保留×扫描隐形×meta 先写回归（宿主全绿）；Windows 语义用例（占用致 rename 失败）既有 fault 套件覆盖。

## 2b 待选：事务生命周期机制（§3.2 租约 + §3.4 在途中断 一并解决）

审查两条建议实为同一缺口：**后台资产事务没有"生命周期把手"**——租约随 RPC 返回即释放（下载还在跑），journal 失败也无从中断（manager 硬编码 `context.Background()`）。

| | 方案 A：租约跟事务走 | 方案 B：事务持 ctx+租约（推荐） |
|---|---|---|
| 做法 | `BeginTxn` 传入模块 `LeaseHolder`，Acquire 由 Txn 持有、Done/Fail 释放；后台 goroutine 结束前模块不收口 | A 之上再加：Txn 内建 `ctx/cancel`；`manager.Download(ctx, ...)` 透传给 `artifact.Fetch`（Fetch 已支持 ctx 取消）；Step 记账失败/停用 drain → cancel → 下载解包即时中止、残件走既有背书清理 |
| 改动面 | 15 模块 BeginTxn 传 holder + goroutine 收口纪律 | 前者 + 15 个 manager `Download` 签名加 ctx + 少量闭包改 `emit` 消费 |
| 解决的审查项 | §3.2 全部；§3.4 半（不新增、可 Drain 等待） | §3.2 全部 + §3.4 全部（即时中断）+ 顺手复核 §5-1（cancellable 语义可借此补真 cancel 或诚实下发 false） |
| 风险 | drain 等待时长（后台任务不死则不收口——已有 lease 语义同款） | ctx 取消路径的中断清洁性需逐 manager 验证（Fetch/Unpack 取消点已具备，主要是接线） |
| 工作量 | ~半天 | **~一天**（15 模块机械接线 + 每类一集成测试） |

推荐 **B**：一次接线同时买断两条审查项，且为将来"用户取消下载"（§5-1）预留真通道。测试口径：`下载中停用模块`、`下载中退出应用`、`journal 中途断电（注入）`三类集成断言"OnDestroy 前无残留写、无复活、残件可背书清理"。

**待机主回复选 A / B（或"按推荐"）后开工；批 3 不受影响可并行。**
