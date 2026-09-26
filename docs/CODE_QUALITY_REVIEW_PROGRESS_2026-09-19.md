# 工作台/模块化收敛后代码质量审查进度（2026-09-19）

> **状态：批 1/2/3 已全部收口入库（2026-09-21~23），2026-09-26 复核门禁全绿，本档从"暂停待批"订正为"代码线闭环"。**
>
> 批1（extapi overrides 值类型+ledger 命名空间 f63d9b2/c679764）、批2（ops Step/降级拒新、artifact 黑户隔离等 4725e52..338dac5）、批3（managed 前端契约 hub 取消锁 25d0ce2..bb7b84a）逐条按本文既定方案实施并带回归锁；踩坑沉淀 #83/#84/#85/#88/#89 在账。
> 仍开放的尾账：① §5 九项已于 2026-09-26 全部复核落文（含批1步骤3 "#7 Acquire×Health/blocked 复核结论"——纯核未修）：#1 已闭环，#3 仍仅 N26/bb75f3b 部分涉及；#2、#4、#6 确认仍存在，#8 代码面已收口、真机闭环转 P1——四条入 §5 尾"需修/待验移交清单"；#5、#7、#9 按现状裁决为可接受或符合设计意图；② `go test -race` Windows 本机因无 MinGW/CGO=0 维持 CI 容器（hanxi-dev:2404）兜底口径；③ 批3 人工余项（390px/200%、纯键盘、读屏器）挂 P1 真机清单随 F6 同场。

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

以下条目原为”有代码证据但未完成交叉核验/裁决”；**九项已于 2026-09-26 复核收口**（#2、#4–#9 按当前 HEAD 实读代码逐项给结论，#1、#3 注记既定状态；结论以复核时点代码为据，file:line 均可回溯）：

1. Operation 广泛标记 `cancellable=true`，但没有真实业务 cancel callback/context 传递；应选择”诚实下发 false”或补完整取消链；
   - **✅ 已闭环（批 3-e + N26）**：观察面按 kind 诚实标注——仅 install/update 为 true（批 2b 接通真取消链），其余如实 false（`packages/go/operation/hub.go:104-108`）；install/update 有真取消通道（`bb75f3b` activeTxns 登记表 + CancelOperation；前端钮走 cancellable∧txnId 双闸，`frontend/src/components/modules/OperationBanner.vue:157-166`；话术收口 `07f9f48`）。
2. compensated/resumable 的前端文案与后端单写占位语义可能相反；
   - **❌ 复核 2026-09-26：仍存在（文案级，需修，移交 F-a）**。后端单写占用只认 pending/running——compensated 明确放行新事务（`packages/go/operation/store.go:131-137`”现场已交回恢复流，不阻塞该模块的新事务”）；而启动恢复会把已登记模块的崩溃残留一律落账 compensated（`internal/app/app.go:370-381`），观察面回灌却不挑态、含 compensated 全量呈 resumable（`packages/go/operation/hub.go:34-38,391-414`）。结论：**恢复收口后的主流场景里，banner”单写约束：忽略残留前，该模块无法开始新的安装事务”（`OperationBanner.vue:136`）与确认框”收口前限制”条（:81）均不符后端事实**（仅补偿失败/无接管而滞留 pending/running 的少数残留为真）；未修句方向是”相反”的近亲——占位 overstated。无功能性误挡（resumable 不入 `activeOf` 禁用闸），回灌消息已带 `journal state=` 字面可作分支依据；`OperationBanner.spec.ts:86-99` 锁旧句需同改。
3. 2205 穷举主要锁定值域，不足以证明 action/summary/reason 语义一致；
   - **◐ 状态不变（本轮不复核，N26/批 3 仅部分涉及）**：值域不变量穷举在 `internal/extapi/catalog_test.go:221-246`；语义一致现按”每条状态机规则至少 1 例 + 优先级压制例 + 精确 Reason 文案断言”覆盖（`catalog_test.go:63` 起 TestProjectPrimaryAction），规则等价类充分，逐组合语义 oracle 仍无，维持原状。
4. 后端产生 retry/repair 主操作，但模块中心目前可能只显示”后续版本开放”；
   - **❌ 复核 2026-09-26：仍存在，但面收窄（retry 需修，移交 F-b；repair/update 休眠）**。retry 真实可达：OnInit 失败滞留 RuntimeFailed（`internal/extapi/registry.go:242-257,678`）→ Project 规则 6 给 `ActionRetry`（`internal/extapi/catalog.go:301-302`），而模块中心兜底分支弹”将在后续版本开放”死路钮（`frontend/src/views/ModuleCenterView.vue:174-178`）。且 `ensureActive` 对失败无闭锁、下次进入天然重跑 OnInit（`registry.go:221-241`）——retry 语义其实早已存在，只差把钮路由到 openModule。repair/update 当前生产不可达：投影 delivery 只有 installed/absent（`registry.go:722-725`）、health 只有 updatewatch 写 current/update-available（`internal/updatewatch/scheduler.go:91,189-195`），orphaned/corrupt 无生产者；Project() 从不赋 `ActionUpdate`（`catalog.go:265-305`），update-available 只改摘要族（:336-341），更新链走各托管页版本面板。附带：`ModuleCenterView.vue:175` 注释”后端尚无对应事务”对 retry 半过时，F-b 同批订正。
5. DPAPI 换机只有日志保护，缺少”凭据无法在本机解密，请重录”的用户可见状态；
   - **⚠️ 复核 2026-09-26：仍存在，按现状设计可接受（裁决）**。载入解密失败本身仍只有日志点名根因（`internal/modules/frpc/store.go:62-71`），但 09-19 后已加固两层：密文驻留 + 直通写回，杜绝”密文当明文再包一层”永久锁死（`store.go:77-91`）；下游可见链成型——认证失败全局通知”请检查 Token 或鉴权配置”（`internal/modules/frpc/service.go:80-81`）、页面连接态”鉴权失败 (Token错误)”（`frontend/src/views/FrpcProjectsView.vue:124`）、Token 输入框直读 `dpapi:` 密文形态（:213 原样透传）即扎眼信号。边界：症状可见而根因不点名（界面无法区分”录错”与”换机”）；全仓 DPAPI 消费者仅 frpc 一家。若收紧：List 投影带每项目”本机不可解密”旗标 + 一句文案（成本低，随 frpc 下一批顺带即可，不单独开账）。
6. Release 可能没有把 tag 版本正确注入运行时和 PE 资源，发布工作流本身也未跑 Vitest/ESLint；
   - **❌ 复核 2026-09-26：仍存在（需修，移交 F-c）**。运行时注入机制已落但发布链未接线：`build/windows/Taskfile.yml:59-65` 支持 APP_VERSION→`-X hanxi/internal/product.Version`（兜底 `internal/product/identity.go:17`=0.3.0），而 `release.yml:82` 裸跑 `task build` 不传参、`scripts/build_and_compress.bat` 亦不传——发布产物运行时版本 = 仓库兜底值而非 tag。PE 资源彻底未接：`generate:syso` 只吃 `build/windows/info.json`（`Taskfile.yml:112-118`），该文件静态 0.3.0，FileVersion 全靠手工同步。工作流门禁：release.yml Verify Source 有 typecheck/vet/`go test -race`/bindings diff，**仍无 Vitest/ESLint**；ci.yml 两者齐（`ci.yml:67-68`）但 push 触发仅 main——dev 直切 tag 或 workflow_dispatch 发布不过前端关。修法：release.yml build 步骤传 `APP_VERSION`（tag 去 v）+ Verify Source 补 `npm run test`/`npm run lint` 两行；info.json 动态改写（生成后注入 tag）中等成本。
7. `Acquire` 对 Health 与 blocked 的关系、状态转换 guard 的生产使用情况需继续核验；
   - **✅ 复核 2026-09-26：关系符合设计意图（裁决），另登记三处休眠边界**。
   - 调用方矩阵（现状）：GUI RPC 全量走 `LeaseHolder.Enter`→`gateView.Acquire`→`Registry.Acquire`，由 AST 穷举门逐模块校验（`internal/app/rpc_gate_matrix_test.go`；装配注入 `registry.go:89-101`）；无头 MCP 同走 `registry.Acquire`（`internal/mcp/mcp.go:63-84`）；后台任务走 `EnterBackground`→`acquireBackground`（租约可转移、停用先 Cancel 再 drain，`registry.go:791-816`）。”未安装/停用不得旁路”业务面成立。
   - Acquire × blocked：blocked 为第一闸、先于 receipt 检查与懒激活（`registry.go:741-757`），与 `SetBlocked` 注释（:619）及 Project 裁决一致，测试锁定（`registry_state_test.go:226-243`）。
   - Acquire × health：**Acquire 不消费 health 即设计意图**——health 定为观察维度（`catalog.go:387-388`”Health 为观察值不设转换门”；`registry.go:624-627` 明示不进裁决），撤回类的设计口径是”以 blocked 语义收口”（`catalog.go:343-348` Visible 注释）；生产唯一写入方 updatewatch 值域 = {current, update-available} 皆 benign（`scheduler.go:88-98,189-195`），故当前不存在”该拦未读”的落空面。
   - 休眠边界（登记不修）：① `SetBlocked`/`SetMandatory` **无任何生产调用方**（全仓 grep 仅测试命中；装配 Catalog 亦无 “core” 品类），blocked/mandatory 两闸当前是死代码（测试面完备）；② “撤回类 health 收口到 blocked”**无机制强制**——签名目录/Wave 4X 复活时若新增 revoked/incompatible 生产者而不同步 SetBlocked，投影显示”已撤回”而 RPC 照常放行，复活该生产者当日必须二选一接线（写方同步 blocked，或给 Acquire 加 health 拒绝集）；③ 懒激活 `EnsureActive`（`registry.go:195-207`）与导航投影 `GetEnabledNavs`（:297 起）均不看 blocked——blocked 模块路由进入仍会执行 OnInit，同属复活配套件；当前无写方故无实害。另 projectState 中 mandatory 优先于 blocked（`registry.go:714-718`）与 Acquire 的 blocked 无条件拒绝存在理论分叉（mandatory∧blocked 并存时），但 `policyTransitions` 表规定 mandatory 为封闭态（`catalog.go:374`），组合按契约不可达。
   - 状态转换 guard：`Can{Delivery,Policy,Runtime}Transition` 生产变更路径**零调用**，仅 `catalog_test.go:396-455` 穷举锁表——guard 现状 = “规格锚点 + 测试锁定”而非运行时强制；生产变更各有局部闸（SetEnabled 的 mandatory/安装闸 `registry.go:370-378`、Uninstall 同闸、Begin 单写）且与 guard 表无矛盾。裁决：按现状可接受；第二个生产写方（事务引擎/签名目录）接入时必须改走 guard。
8. drain 超时与全局退出预算、Electron/Paseo 子孙进程残留仍缺真机闭环；
   - **⚠️ 复核 2026-09-26：代码面已收口（按现状可接受）；真机验收债移交（F-d）**。drain 逐模块有界（停用 30s/退出 60s，`registry.go:20-23`），先关门→Cancel 后台租约→有界等待→超时强制收口 + Error 显式告警（:389-418,819-846）；批 2b 已把 19/19 journal 模块迁移到 ctx+租约买断并落 service 级集成测试（`TestDeactivateCancelsInFlightTxn`，账见 PLAN_REMAINING_WORK 批 2），取消链畅通时 60s 纯属兜底。全局退出预算仍无：OnShutdown 阻塞至返回（`internal/app/app.go:578-586`）、ShutdownAll 逐 wrapper 串行、OnDestroy 不计超时——最坏理论 = Σ60s + 任一 OnDestroy 挂死，规则为”仍存在但可接受”（真卡住由用户强杀兜底，进程侧无泄漏：JobObject `KILL_ON_JOB_CLOSE` 连强杀都清零 Job 内子孙，`internal/platform/windows/job.go:31-34`；Snipaste 类 detached 属设计存活）。欠账性质是**验证而非代码**：真机”退出/强杀→进程树零残留”从未系统冒烟（批 2b 记录”真机冒烟后补跑”未闭）。
9. schema 兼容判断、Operation 枚举漂移门、bindings 精确导出集合需继续核验；
   - **✅ 复核 2026-09-26：基本已覆盖，残余可接受（裁决）**。① schema 兼容：盘上侧批 1-② 已立独立版本线——`ledgerSchemaVersion` 自有版本、高版本 schema fail-closed 只读、低版本迁移（`internal/settings/receipts.go:272-347`）；投影侧（ModuleState/Operation 的 schema 字段）前端无消费分支、仅详情展示（`frontend/src/components/modules/ModuleDetailPanel.vue:104`）——前后端同二进制交付 + bindings 同源，投影无版本偏差窗口，判定暂不需（若未来投影落盘缓存则必须补消费端裁决）。② Operation 枚举漂移：三段链条就位——Go 冻结表 + 2205 规模锚（`catalog_test.go:461-521`）、`wails3 generate bindings -clean=true` + `git diff --exit-code`（ci.yml:50-55 与 release.yml Verify Source 双处）、前端词表 `satisfies` 编译期穷举 + `Object.keys` 逐键比对（`frontend/src/constants/status.ts:66-87,221-282`、`constants/__tests__/status.spec.ts:85-131,205-235`）。残余缝：Go↔status.ts 逐字对齐靠测试注释纪律（`catalog_test.go:3`）非机械解析 TS——但改 Go 枚举必被 bindings diff 与 vue-tsc 双闸夹住，可接受。③ bindings 精确导出集合：`-clean=true` 生成 + clean diff 门禁连删除/改签名一起兜，覆盖。

### §5 复核移交清单（2026-09-26 派生，均不动手于本轮）

- **F-a（#2）**：OperationBanner 单写约束句与确认框”收口前限制”条按 journal 态区分——滞留 pending/running 的残留保留”无法开始新事务”如实警告；compensated（恢复已接管）改为”不占写位、忽略仅为收口账本”。同改 `OperationBanner.spec.ts:86-99`。成本：文案 + 一个状态分支，小。
- **F-b（#4）**：模块中心主操作 `retry` 从兜底 toast 改路由 openModule（懒激活即真重试），订正 `ModuleCenterView.vue:175` 过时注释；repair/update 维持”后续版本开放”如实兜底（当前投影不可达）。成本约一行 + spec。
- **F-c（#6）**：release.yml build 步骤传 `APP_VERSION`（tag 去 `v` 前缀）；Verify Source 补 `npm run test`/`npm run lint`；`info.json` 动态改写使 PE FileVersion 跟 tag。成本小→中。
- **F-d（#8）**：P1 真机清单补一项——托管 GUI 家族（Paseo/Termora/VS Code 等）在途操作 + 正常退出/强杀 → 进程树零残留冒烟；实测出现退出拖沓再议全局退出预算 / OnDestroy 超时包装。
- **条件登记（不修，#7②③）**：签名目录/Wave 4X 复活或任何 revoked/incompatible 生产者上线当日，必须同批接线 blocked（写方同步 SetBlocked 或 Acquire 加 health 拒绝集），并让 EnsureActive/GetEnabledNavs 认 blocked；第二个状态写方上线当日改走 Can*Transition guard。

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
