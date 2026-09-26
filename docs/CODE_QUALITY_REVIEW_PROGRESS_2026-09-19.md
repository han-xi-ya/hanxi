# 工作台/模块化收敛后代码质量审查进度（2026-09-19）

> **状态：批 1/2/3 已全部收口入库（2026-09-21~23），2026-09-26 复核门禁全绿，本档从"暂停待批"订正为"代码线闭环"。**
>
> 批1（extapi overrides 值类型+ledger 命名空间 f63d9b2/c679764）、批2（ops Step/降级拒新、artifact 黑户隔离等 4725e52..338dac5）、批3（managed 前端契约 hub 取消锁 25d0ce2..bb7b84a）逐条按本文既定方案实施并带回归锁；踩坑沉淀 #83/#84/#85/#88/#89 在账。
> 仍开放的尾账：① §5 九项复核仅 #1/#3 闭环，#2、#4–#9 待复核（含批1步骤3 "#7 Acquire×Health/blocked 复核结论落文"——纯核不修）；② `go test -race` Windows 本机因无 MinGW/CGO=0 维持 CI 容器（hanxi-dev:2404）兜底口径；③ 批3 人工余项（390px/200%、纯键盘、读屏器）挂 P1 真机清单随 F6 同场。

## 1. 审查背景与最终基线

工作台/模块化执行会话已完成并入库，最终基线包括：

- 共享托管控制台契约增强；
- 20/20 托管页面完成共享契约或非回归边界治理；
- VS Code 双形态与 Snipaste 专属控制/版本体收敛；
- Everything 按既定裁决保持自定义形态并通过非回归；
- 路线与踩坑文档完成事实对账；
- 前端全量门禁：112 个测试文件、1261 项测试、`vue-tsc`、生产构建、ESLint 0 error（12 条既有 warning）；
- Wave 4X（外部实例控制）维持延期，未落代码。

最终相关提交：

- `545a8b9` `feat(frontend): 增强托管控制台共享契约与动作编排`
- `81a67fb` `refactor(vscode): 收敛双形态托管控制台编排`
- `366e571` `refactor(snipaste): 收敛本会话托管控制台编排`
- `0291caa` `docs(troubleshooting): 沉淀 computed 短路漏收响应依赖`
- `d0144a2` `docs(roadmap): 校准 Wave 5 托管收口与完成口径`
- `5773ff9` `docs(roadmap): 同步官方模块分发范围裁剪`

## 2. 已执行的审查

已完成或取得有效结果的只读审查方向：

1. 路线与下一步优先级；
2. 测试、可靠性与发布门禁；
3. Go 并发、Registry 状态投影与 receipt/已见名单；
4. 前端托管控制台局部状态、加载竞态、busy 真相和无障碍；
5. 跨文件调用与删除/卸载持久化行为。

随后按机主指令，立即停止仍在运行的后端架构、前端架构、状态契约、操作生命周期与 Artifact 恢复深审。以下只保留停止前已经返回且有代码证据的结论。

## 3. 已核实的高优先级问题

### 3.1 Registry 覆盖状态存在数据竞态

涉及：`internal/extapi/registry.go:599-642,681-717`

`overrideOf` 在持锁时取得/创建 `*stateOverride`，随后解锁并把内部指针返回；`SetMandatory`、`SetBlocked`、`SetHealth` 在锁外写字段。`projectState` 也只在锁内取得指针，解锁后读取 `mandatory/blocked/health/remoteVersion`。

后台更新检查与前端 `ListModuleStates` 可并发执行，可能造成：

- Go data race；
- `health` 与 `remoteVersion` 来自不同轮更新的混合快照。

建议方向：setter 全程持 `Registry.mu`；投影在锁内复制完整值，不允许内部可变指针逃出锁域；补并发测试并在具备 CGO/GCC 的 Windows 环境运行 `go test -race`。

### 3.2 异步托管下载提前释放 Acquire 租约

代表：`internal/modules/markeron/service.go:133-210`，同模式见多个托管模块。

当前流程为：RPC 取得 lease → `defer release()` → 启动 goroutine → RPC 返回 → lease 立即释放；实际下载、解压、journal、版本落位仍在后台进行。

后果：下载进行中停用/退出模块时，Registry 可能误判 `inFlight=0` 并执行 `OnDestroy`，而后台任务继续写磁盘、发事件或通知，甚至在逻辑卸载后重新产生资产。

建议方向：后台任务自身持有 lease 直到真实结束，或引入与 Operation/事务生命周期绑定的后台租约；必须用真实 service 集成测试验证“下载中停用/退出”。

### 3.3 receipt 与 `known-modules.json` 设计存在成组缺陷

涉及：`internal/settings/receipts.go:178-292`

已核实问题：

1. 名单缺失、损坏、不可读或 schema 不匹配时按空名单处理，会重新创建已卸载模块的 receipt；
2. 旧版本升级首次引入名单时，没有足够信息区分“主动卸载”与“从未登记”，可能复活模块；
3. receipt 先写、名单后写，不是一个原子事务；名单落盘失败后，下次启动仍会复活模块；
4. 名单错误复用 `ModuleContractSchema`，未来前端/状态契约升版会无关地重置卸载记忆；
5. `known-modules.json` 与 receipt 共用 `*.json` 命名空间，`Installed()` 会把它当作模块 receipt 扫描并误报警；未来名为 `known-modules` 的合法模块还会路径冲突。

建议方向：把名单移出 receipt 命名空间；使用独立 ledger schema；区分首次迁移、文件损坏与版本迁移；为卸载建立可靠 tombstone 或 fail-closed/备份恢复语义；保证名单与 receipt 的提交顺序和失败行为不会让卸载跨重启失效。

### 3.4 journal 步进失败会继续执行副作用

涉及：`internal/ops/ops.go:132-159,246-253`

`Txn.Step()` 的 journal `Advance` 失败只告警，下载/解包/目录落位仍继续。强杀恢复时，账本阶段可能落后于真实磁盘现场。

相关启动路径中，journal store 打不开或恢复失败也主要是告警后继续开放托管写操作，可能形成“无账执行”。

建议方向：

- `Begin` 落盘失败时零副作用；
- `Advance` 失败不得进入下一副作用阶段；
- store/recover 失败时托管写事务 fail-closed，只开放诊断/清理；
- UI 明确显示 journal degraded，而不是只写日志。

### 3.5 Artifact Commit 存在 rename 后、meta 前的崩溃窗口

涉及：`packages/go/artifact/tree.go:210-250`、`packages/go/artifact/artifact.go:95-102`

当前先把 staging rename 成最终版本目录，再写最终目录内的 `meta.json`。两步之间崩溃会留下：

- 最终目录存在；
- 没有可信 meta；
- Resolve 跳过；
- 同版本重装又拒绝覆盖；
- 恢复器不认领该最终目录。

现有测试主要证明“坏目录不会被误用”，尚未证明可恢复或可重装。

建议方向：重新设计 Commit 原子边界（meta 在 staging 内完整写好后再 rename，或增加可恢复标记/隔离策略），补 helper 子进程强杀测试。

## 4. 已核实的前端问题

### 4.1 旧运行快照可能冒充实时状态

`frontend/src/components/managed/store.ts:213-219`

曾取得 `running` 后，如果状态 RPC 持续失败且无事件到达，页面会无限期保留绿色运行态、PID 和版本。建议保留最后快照但增加 `stale/statusError/lastStatusAt`，停止 live 动画并显示“状态暂不可确认”。

### 4.2 本地/远程版本并行加载可能误显“尚未安装”

- `frontend/src/composables/loadManagedVersions.ts:18-40`
- `frontend/src/components/managed/ManagedVersionPanel.vue:149-161`

远程先返回会提前清除统一 `loading`，本地扫描尚未完成时可能显示首用空态。建议拆分 local/remote loading 与 resolved 状态，并用 deferred Promise 测两种返回顺序。

### 4.3 `already-installed` 清票未复用版本互认规则

`frontend/src/components/managed/store.ts:319-338`

adapter 可把预发布 tag 与核心版本判为同版本，但清 pending 票据仍做精确字符串比较，可能永久停在“安装中”。建议清票复用统一 `sameVersion/statusOf`。

### 4.4 VS Code / Everything 自定义加载存在旧响应覆盖

并发刷新与下载事件可能让旧请求晚返回并覆盖新状态；应增加 generation token，旧 generation 不得提交状态或清除新请求的 loading。

### 4.5 专属动作形成多份 busy 真相

DdnsGo、PaperTodo、ManagedExtrasCard、Everything 等专属动作未全部通过统一互斥执行入口，快速交叉点击可能并发。建议统一 `store.runExclusive()` / `store.busy`，下载点击时立即建立 pending 票据。

### 4.6 可访问性缺口

- `MainTabNav` 缺少标准 Tab 的 roving tabindex、方向键和 Home/End；
- 多处下载进度缺少 progressbar 语义；
- 部分 retry 用无 href 的 `<a @click>`，键盘不可达；
- 共享面板在 390px/200% 下仍需真实 WebView2 验收。

## 5. 其他已发现但待复核/裁决事项

以下有代码证据，但尚未完成全部交叉核验或产品裁决，恢复审查时继续：

1. Operation 广泛标记 `cancellable=true`，但没有真实业务 cancel callback/context 传递；应选择“诚实下发 false”或补完整取消链；
2. compensated/resumable 的前端文案与后端单写占位语义可能相反；
3. 2205 穷举主要锁定值域，不足以证明 action/summary/reason 语义一致；
4. 后端产生 retry/repair 主操作，但模块中心目前可能只显示“后续版本开放”；
5. DPAPI 换机只有日志保护，缺少“凭据无法在本机解密，请重录”的用户可见状态；
6. Release 可能没有把 tag 版本正确注入运行时和 PE 资源，发布工作流本身也未跑 Vitest/ESLint；
7. `Acquire` 对 Health 与 blocked 的关系、状态转换 guard 的生产使用情况需继续核验；
8. drain 超时与全局退出预算、Electron/Paseo 子孙进程残留仍缺真机闭环；
9. schema 兼容判断、Operation 枚举漂移门、bindings 精确导出集合需继续核验。

`delivery` → `deliveryKind` 未升 schema 的事项不列为缺陷：ADR-0001 已明确记录该变化发生在契约未对外发布的冻结收口阶段，schema 维持 1 是项目已接受裁决。

## 6. 路线判断（暂停时版本）

当前完成度可概括为：

- Wave 0–3：主体完成；
- Wave 4 Artifact/Supervisor/Operation：主体落地，但组合生命周期与强杀恢复仍有缺口；
- Wave 4S 第二机/NAS 黄金路径：尚无完整证据；
- Wave 5 更新感知与页面收敛：已完成；
- Wave 4X 外部实例控制：延期，不是当前任务；
- manifest、签名 Catalog、sidecar/物理拆包：已裁剪，不是默认欠项。

在正确性问题修复后，产品层下一优先任务应是 **A 机 → NAS → 干净 B 机** 的真实验收，而不是继续扩建平台：

1. 建立模块迁移等级（可直接携带 / 需重建缓存 / 需系统注册 / 需重新输入凭据 / 外部数据缺失 / 不适合临时电脑）；
2. 验证 receipt、版本资产、active 状态、DPAPI、绝对路径、MSIX/User Installer、外部 `%APPDATA%`；
3. 明确当前只支持“关闭 A 后同步，再启动 B”，不默认承诺两机同时运行同一 NAS 根；
4. 根据验收结果再考虑机器绑定恢复提示、临时电脑模式、NAS 冲突防护与统一更新执行。

## 7. 恢复工作时建议顺序

收到机主继续指令后，建议按以下顺序重新启动，不需要重做已完成审查：

1. 先把已核实问题整理成正式修复计划并确认范围；
2. 第一批修 Registry 数据竞态与 receipt/known-ledger 设计；
3. 第二批修异步 lease 生命周期、journal fail-open 与 Artifact Commit 崩溃窗口；
4. 第三批修前端 stale/loading/generation/busy 真相；
5. 跑组合级故障注入和 Windows 真机验证；
6. 最后建立第二机/NAS 黄金路径测试与验收记录。

## 8. 暂停边界

- 所有后台 Agent 已停止；
- 本轮只读审查未修改任何业务代码；
- 本文仅为进度与证据快照；
- **未经机主后续明确指令，不实施上述修复、不恢复 Agent、不新增任务。**
