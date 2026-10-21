# KEY-NUGGETS — 归档线孤本摘入（T8 整理批次）

> **开档日期**：2026-10-21。本目录 `docs/archive/` 归 T8 会话名下专用，其他会话勿往里写。
> **收录标准**：只收"别处没有"的仍有效结论、实测数字与机主裁决；数值一律照抄原文（保真，未换算未改写）。
> **取证口径**：git log + `docs/FACTS.md` + `docs/TROUBLESHOOTING.md` + 当前代码实读对账，不采信文档自述。
> **原件处置**：来源文件本次只盖状态戳、不删不移；若机主日后确认删除原件，本档即孤本存续。
> **敏感边界**：§六为 hanxi-ocr 私发组件相关，wcocr/微信引擎口径**永不进公开渠道**（公开 README/发布说明/对外文案不得引用本节）。
> **存底地位（2026-10-21 机主授权删除）**：§一～§四 的来源四档——`docs/BUG_AUDIT.md`、`docs/PROJECT_REVIEW_2026-09-08.md`、`docs/CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md`、`docs/WSL_REDESIGN_PROGRESS_2026-09-08.md`——已从工作区删除，**本档自此是这些孤本结论的唯一现场档**（原文仍可走 git 历史找回，删除点前的最后一版在 `2c3fa57`）。引用处已改指本档：PRW P0 段、PLAN_P0_BATCH1/2/3 三处依据行。

---

## 一、BUG_AUDIT.md（2026-09-04 审查基线）——仍有效条目孤本

### 1.1 经代码实测确认**未修复**的条目（原文位置已随重构漂移，按现码重锚）

- **BUG-007（P1）子进程在 JobObject 绑定前逃逸**：supervisor 内核现行启动序仍是 `spawn → Job Create/Assign`（`packages/go/supervisor/engine.go:99,172`），未采纳审计建议的"suspended 创建→先 Assign→再 resume"。托管程序在 `cmd.Start()` 后、`job.Assign(pid)` 前创建 updater/daemon 仍可逃出收编；`KILL_ON_JOB_CLOSE` 只兜住"已入 Job 的子孙"，兜不住逃逸窗口。原建议保留：以 suspended 状态创建进程，先 Assign 再恢复。
- **BUG-012（P2）启动外部程序后未 Wait/Release**：`internal/app/open_service.go:28-44`（explorer/notepad/control/rundll32 等）仍为 `exec.Command(...).Start()` 单发，无后台 `Wait()` 亦无 `Process.Release()`；反复打开目录/文件/系统面板时 Windows 进程句柄随进程对象存续累积。原建议保留：不要退出信息的 Start 成功后立即 `Release()`，需要的改后台 `Wait()`。
- **BUG-019（P1）微信监听快速 Stop/Start 多代轮询**：`internal/modules/wechat/listener.go` 现码仍是 `running atomic.Bool` 旗标制——`Stop()` 只 cancel ctx 不等旧 `pollLoop` 退出（:99-110），旧循环 defer **无条件** `l.running.Store(false)`（:208-209）。旧循环未退净期间再次 `Start()`：新代旗标可被旧 defer 抹掉（显示已停实则在跑），亦可放第三条轮询导致重复消息与游标并发更新。原建议保留：Stop 等待本代完全退出，状态更新验证 generation。
- **BUG-027（P2）无版本 EXE 用当前秒作导入身份**：`internal/modules/ccswitch/version/manager.go:360` 仍是 `version = "imported-" + time.Now.Format("20060102-150405")`。同一无可信 FileVersion 的 EXE 跨秒重复导入生成新目录、绕过重复检测；现有测试同秒调用只是碰巧通过。原建议保留：拒导入或以文件 SHA-256 派生稳定身份。
- **BUG-018（P1）SOCKS5 不可取消 Dial 泄漏 goroutine**：端口扫描代理链的建连悬挂问题已在 TROUBLESHOOTING **#110** 沉淀（x/net SOCKS5 拨号器建连后同步读 greeting 且不看 ctx，`http.Client.Timeout` 与 ctx 都管不住），2026-10-21 已挂真机验证夹具（`2c3fa57`，默认 skip、凭据不入库），但"改用原生支持 context 的代理拨号、勿以额外 goroutine 包装不可取消调用"的根治仍是待办。

### 1.2 已转为**刻意设计**的条目（防"照原建议改回去"）

- **BUG-016 FileShare 下载无超时**：现行码为 `WriteTimeout: 0` 且带注释"大文件下载无限制"（`internal/modules/fileshare/server.go:117`），上传侧以 `ResponseController` 按进度刷新读截止（`upload.go:36-40`）。即"不限时"已是落档取舍，不是漏配；若要收紧按审计原建议走产品决策，不得静默改回。

### 1.3 供应链信任模型的现行边界（BUG-003/004 未完全闭合部分）

- 有官方 digest 的模块已"硬校验+缺失拒装"：如 ccswitch `上游未提供 CC Switch %s 的官方 SHA-256 摘要，拒绝无校验安装`（`version/manager.go:193`）。
- **papertodo 明示降级链**：GitHub 2026-09 才逐批回刷 digest，集成实证期全部资产无 digest，照搬硬过滤会清空历史版本表——"digest 在场走内核 Fetch 硬校验，缺失走降级链"（`version/remote.go:167-169`）。guoheview 官方仅 MD5，bespoke 链同属披露态。
- 元数据面仍走第三方 GitHub 镜像回退（如 ddnsgo `gh-proxy.com`/`ghproxy.net`，`version/remote.go:42-43`）——"镜像只作传输来源，不作完整性信任根"（STRATEGY §7.2）在该面上依赖的是资产摘要与元数据**不同源**这一前提；镜像同时伪造二者的场景未被代码级证伪。

### 1.4 待复核（判定无法闭合，原文保留参考，勿当已修亦勿当必炸）

BUG-036（ddnsgo 仅凭 TCP 端口开放判就绪，未见 PID 归属+特征响应核验）、BUG-037/038（guoheview 就绪接受任意同名进程窗口、超时后保留 running）、BUG-039（guoheview beta/stable 比较——现码改为"beta 构建号不高于 stable 时剔除"，主版本段比较语义未核）、BUG-040（recordly 忽略预发布后缀判 already-installed，现仍按数值核心互认）、BUG-044/051/052（FileShareView/LanScannerView/PortKill+PublicIp+FrpcProjectEditor 旧响应覆盖——未见 generation 门卫）、BUG-054（托盘 `.bat/.cmd` 报错 193——现已改走 `internal/launcher` 的 `exec.Command`，原结论绑定旧 CreateProcess 路径，真机是否仍复现待验；文件过滤器仍允许 `*.bat;*.cmd;*.lnk` 选择，`elevate_tray.go:231`）。

## 二、PROJECT_REVIEW_2026-09-08.md——孤本

- **机主"明确后置"裁决（09-08 时点）**：「用户明确后置：B06 凭据迁移、B08 下载信任策略、CI/CD 门禁调整。本轮未修改这些敏感范围。」——B06 后续以候选身份活在 FUTURE §3.3/STRATEGY §6.5（仍未落地，微信凭据至今明文）；B08 部分被内核"拒绝无校验安装"兑现（见 §1.3 边界）；CI/CD 尾账即 PLAN_REMAINING_WORK 的 F-c。
- **B0x↔BUG-0xx/F0x 对账锚**（BUG_AUDIT 与修复轮之间的少数逐条映射，判"哪条被谁修"时别错记批次）：B01≈frpc 删除边界（BUG-023/024 族）、B02=BUG-002（os.Root 全链）、B03=BUG-008（frpc 每运行独立代次对象）、B04=BUG-022（**当时仅部分**：service 层在途登记，manager 层下载/导入/卸载互斥与 staging 无损提交其时未做——后由批 2b 租约买断收口）、B05=BUG-030/032、B07=BUG-013/014/015、B09=WSL 体检 generation、F01~F07≈BUG-041~046 前端族（F07 当时仅部分：公共轮询 tick 重叠与 Stop/Start 代次）。
- **§7.4 性能测量六项基线清单（孤本，别处无完整列表）**：冷启动与首次可交互耗时；页面首次加载和后续切换延迟；连续访问多页后的缓存内存与事件订阅数；连续启停模块后的 goroutine、句柄、连接数；版本目录扫描/重复哈希/日志读取与渲染成本；下载和大文件传输的峰值内存与吞吐。配套纪律：先确认热点再优化；**不要为工作集数字频繁强制 GC，不在无基准时承诺绝对内存目标**。
- 验证快照（史账）：09-08 轮前端 77 测试文件 / 633 项通过；`-race` 因本机无 GCC/CGO 未跑，不记为通过；快传真实全网监听集成测试未运行。

## 三、CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md——§5 复核锁定的裁决孤本

（批 1/2/3 与 §5 九项复核结论均已入库，其中 F-a/F-b 已修 `78df697`，F-c/F-d 由 PLAN_REMAINING_WORK :152/:175 追踪；以下四条深度裁决别处没有，且**含未来复活时的硬接线义务**）

- **#7 裁决 + 条件登记（Wave 4X/签名目录复活时必办，全仓唯一记录）**：
  - Acquire 不消费 health 即设计意图——health 为观察维度（`catalog.go` "Health 为观察值不设转换门"）；生产唯一写入方 updatewatch 值域 `{current, update-available}` 皆 benign，故当前无"该拦未读"落空面。blocked 为第一闸，先于 receipt 检查与懒激活。
  - **三处休眠边界**：① `SetBlocked`/`SetMandatory` 无任何生产调用方（全仓 grep 仅测试命中），两闸当前是死代码（测试面完备）；② "撤回类 health 收口到 blocked"无机制强制——签名目录/Wave 4X 复活时若新增 revoked/incompatible 生产者而不同步 SetBlocked，投影显示"已撤回"而 RPC 照常放行；③ 懒激活 `EnsureActive` 与导航投影 `GetEnabledNavs` 均不看 blocked，blocked 模块路由进入仍会执行 OnInit。
  - **条件登记（不修，复活当日必须二选一接线）**：任何 revoked/incompatible 生产者上线同批，写方同步 `SetBlocked` 或给 Acquire 加 health 拒绝集，并让 `EnsureActive`/`GetEnabledNavs` 认 blocked；第二个状态写方（事务引擎/签名目录）上线当日改走 `Can{Delivery,Policy,Runtime}Transition` guard（现状：guard 生产路径零调用，仅 2205 穷举测试锁表——"规格锚点+测试锁定"而非运行时强制）。
- **#8 全局退出预算的如实结论**：drain 逐模块有界（停用 30s / 退出 60s），规则"仍存在但可接受"——`OnShutdown` 阻塞至返回、`ShutdownAll` 逐 wrapper 串行、`OnDestroy` 不计超时，理论最坏 = Σ60s + 任一 OnDestroy 挂死；真卡住由用户强杀兜底，进程侧无泄漏：JobObject `KILL_ON_JOB_CLOSE` 连强杀都清零 Job 内子孙（`platform/windows/job.go:31-34`）；Snipaste 类 detached 属设计存活。欠账性质是**验证而非代码**（真机"退出/强杀→进程树零残留"从未系统冒烟，即 F-d）。
- **#3 裁决（防把"2205 穷举"当语义证明）**：2205 规模锚只锁值域不变量（`internal/extapi/catalog_test.go:221-246`）；语义一致按"每条状态机规则至少 1 例+优先级压制例+精确 Reason 文案断言"覆盖，规则等价类充分，**逐组合语义 oracle 仍无**，维持原状。
- **#9 残余缝（机械保证的边界）**：Go 枚举 ↔ 前端 `constants/status.ts` 逐字对齐靠测试注释纪律（`catalog_test.go:3`），**非**机械解析 TS——现兜底是 bindings diff（`-clean=true`+`git diff --exit-code`，CI 与 release Verify Source 双处）与 vue-tsc `satisfies` 编译期穷举双闸。若未来枚举跨进程分发，此纪律不再自足。

## 四、WSL_REDESIGN_PROGRESS_2026-09-08.md——孤本

- **许可证与反模式红线（孤本）**：参考项目 `wsl-dashboard`（Rust+Slint）为 **GPL-3.0-only**——只借鉴思路（命令用法、"备份先行/代次过滤/白名单"类工程思路），**零代码引入**，实现全自研且离线单测锁命令面。**明确不借鉴的三反模式：编译期过期 kill-switch、调试构建禁 TLS 校验、`curl | sh` root 远程脚本**。
- **设计红线摘录**：VHDX 路径**只从注册表推导**（提权注入红线）；"只读取证"入口对停止实例绝不进 guest（`wsl -d` 会顺手开机，详见 TROUBLESHOOTING #39）；瘦身空间预检 = 源卷+导出卷 **实占 + 2GB**，强制先 tar 备份；`/etc/wsl.conf` 的 `[user] default` 须经 `id -u` 求证、stdin `tee` 覆盖、读回复验、CRLF 归一；`.wslconfig` 的 networkingMode 值白名单落盘前拦截（**写错 = 所有发行版集体失联**）。
- **当时点"真机未验"诚实账（09-14）**：克隆/瘦身/导入/netsh 应用等真机端到端**未执行**，全部结论基于离线桩单测与命令面断言；首次真机建议先在小发行版上跑导入导出闭环。（后续 usbipd 一族已真机终裁 N31，其余未单独记录——引用时先复测。）
- **遗留候选消费状态（2026-10-21 对账）**：usbipd USB 直通→已落地（N31，`65c3512`/`1085172` 等）；`wsl --mount` 磁盘挂载→已入 BACKLOG P2 池；schtasks 计划任务 GUI→BACKLOG 已裁"不做"；**仍未入册的两件**：单发行版 WSL1↔2 转换（`wsl --manage --set-version`，补上可解锁 WSL1 克隆前置）、镜像测速多源下载。tar 克隆兜底未实现属现状（现指路人工导出→导入）；瘦身后 Tier2 默认用户恢复按"如实提示"处理。

## 五、MOOTOOL_ANALYSIS.md——孤本

- **架构约定（全仓唯一出处）**：`OnInit` 内**禁同步外部进程/网络 IO**，防"首次进入卡页面"——懒启动核源裁决的配套条款（`registry.EnsureActive` 三入口懒激活、停用即 `OnDestroy`+GC 归还的机制描述见本文 A 表注，其结论已散见于 ARCHITECTURE，本条禁令只在本文）。
- **仍未落地项**：轮询自适应降频（页面离开时 flclash 等 5s 轮询降为 60s，P3，BACKLOG 候选池在案）；devkit 第一批本身未开工（目录无 devkit 模块）。
- **核源修正范例（史账）**：懒启动一条曾误判"hanxi 缺失"，核源后确认已有更完整等价机制并改判——教训是**借鉴分析动笔前先核自家代码**。
- ⚠ 与现状冲突项（已在原件盖戳，勿照抄）："明确不学"第 4 条"不做全局剪贴板监听（隐私）"已被 clipboard 模块（2026-09-27 线）实际推翻——全局监听+Paste 粘回原窗已入库，且明主裁决取消 DPAPI 明文落盘（`a0ba43f`）。本文保留为当年口径记录。

## 六、OCR_DUAL_ENGINE_PLAN.md——孤本数字与契约（私发敏感，勿进公开口径）

- **M0 spike 实测数字（2026-09-17，孤本）**：路线修正后公开版**零 cgo**——RapidOCR clib 停更不支撑 v6、本机无 C 编译器，实际落地"纯 Go + 动态装载 onnxruntime.dll + 手写 DB 前后处理"（`spike\recognize` ~1200 行端到端跑通）；冷启动 **178-395ms**；snip 典型框选 **P50 0.5-1.0s**（det 须配 `limit_type=max/960`）；v6 small 模型 **~30MB**；内存 snip **169MB**、整屏大图 **1.8GB**。M1 内存治理后：整屏 **1.8GB→329MB 封顶**、snip **132MB**、**72MP 硬拒**。首个真实引擎包 **44MB**。
- **打包硬红线**：msvcp140 系列 VC 运行时 **14.40+ 必须 app-local 随件**——ORT 1.30 在旧运行时 `CreateEnv` 直接 AV 崩，干净机器必炸；pack_public 清单与 manifest 需含这三个 DLL。
- **微信引擎基线（`hanxi-ocr-dev\testkit\`，60 张七类语料）**：冷启动 **837ms**；热路径 **P50 478ms / P95 779ms / max 1200ms**；已知失败面：低对比印刷体（5 空）+ LED 点阵（5 空）+ 竖排碎裂。labels\ 60 份需人工校对后方可作精度参考。M2 跑分工具 `testkit\bin\runner.exe/score.exe`；对比基准件口径 = `dist\hanxi-ocr.exe` 当时点内嵌版（原文带版本字样，引擎/组件版本现值不在本面复抄，以机主侧发布台账为准）。
- **契约 v0.4 增量与定名裁决（对 Hanxi 侧行为有约束）**：`/api/status` 的 `name` 固定 `"hanxi-ocr"`，探活校验放宽为契约名集合；`engine` 值域 `wechat-4.0 | pp-ocrv6-small | pp-ocrv6-medium | pp-ocrv5`（回退档）；`/api/ocr`、`/api/ocr/upload`、`/api/shutdown` 逐字段兼容 v0.3（这决定 Hanxi 转发与 snip 链路零改动）；公开版按 `manifest.json` 自检启动，找不到模型即 `engine_running:false`+中文 error，**不崩不退端口**。包内 manifest 身份字段统一为 `name`（=HTTP 契约名，组件自检强校验 `name=="hanxi-ocr"`）；服务器侧总清单定名 `index.json`（与包内 manifest 消歧）。
- **战略级裁决**：公开版形态 = 目录 zip（放弃单文件洁癖，DLL 必须落盘）；组件升级三层（模型文件级/运行时级/v7 换代级）对 Hanxi **均为 0 提交**——契约不含模型语义；自动下载必须用户明示触发，静默拉 45MB 不可接受；Hanxi 主包体积零增长。
- **计划与现实的偏差点（2026-10-21 对账）**：M3"自建下载源 + `DataDir()/ocr-engines/`"路线实际被 **F7 托管化六连**（`4aaa56c`…`569ae5a`，zip 校验链+sha256 旁挂必检+原子落位+托管优先解析链）取代，数字签名 manifest 走的是 hanxi 托管内核口径；M4 未做——`internal/modules/ocr/module.go:38` 模块版本仍 **0.1.0**，`Info().Description` 未改口径。**仍未拍板**：cls 方向分类器（截屏场景默认关省 ~10MB，M2 盲测定夺）、公开组件自有代码 LICENSE（建议 Apache-2.0，合规检查单卡此项）。M2 待办不变：labels 人工校对、真手写/拍照补料、medium 档量化、运行中切换分支人工核验。

## 七、不摘入本档、但有账可查的流转指针（防重复沉淀）

- FileShare 鉴权（BUG-001）→ 2026-09-14 `authGate`（回归锁 `fileshare/auth_test.go`，原行内注见本档 §一 与 git 历史原档 `BUG_AUDIT.md`）；os.Root 沙箱（BUG-002）→ TROUBLESHOOTING #38。
- receipt/known-modules（3.3）→ 批 1 `f63d9b2/c679764` 独立 ledger 命名空间 + `receipts.go:272-347` fail-closed。
- Registry 竞态（3.1）→ 批 1 值类型投影；journal fail-open（3.4）/下载租约（3.2）→ 批 2a `4725e52` + 批 2b 19/19 模块 ctx+租约买断（`TestDeactivateCancelsInFlightTxn`）。
- Artifact Commit 崩溃窗口（3.5）→ 批 2a 黑户隔离放行重装；解压配额（BUG-025）→ `packages/go/artifact/unpack.go`（MaxEntries 5000 / 单文件 1GiB / 总展开 4GiB / 压缩比 200）。
- F-a/F-b 文案与 retry 路由 → `78df697`；F-c/F-d → `docs/plans/PLAN_REMAINING_WORK.md` :152/:175。
- 微信凭据 DPAPI 化：未做（settings 无 DPAPI 痕迹），账在 FUTURE §3.3 候选与 STRATEGY §7.3-2。
- **登记一处正面矛盾（2026-10-21，裁决归机主，T10 移交）**：`docs/plans/PLAN_P0_BATCH2_FIX.md` **标题**仍挂"（2a 已落地 / 2b 待机主选机制）"，与 CODE_QUALITY"批 2 已收口"相撞。本会话取证裁向**批 2b 已收口为实**：`667006f`（2026-09-23，租约+ctx 贯通 19 个 journal 模块）、`338dac5`（同日，下载中停用取消全链集成测试·真 Registry+真 journal）、`f3f63fe`（同日，排期对账收工登记），且 PLAN_REMAINING_WORK P0 批 2 行与机主 09-22 选案 B 记录在案；PLAN_P0_BATCH2 **自身的状态帽**也已写"已完成……建议归档"——旧标题属 T6 冻结档残留。该档在 T6 名下且 T6 已冻结收报，此处只登记不代改。
- 托管款数跨文档差（PLAN_W6 底账 28 款 vs FACTS 现值 32）：凡引用处已加"以 `docs/FACTS.md` 现值为准"指针（STRATEGY 头行历史链、FUTURE 适用基线帽），不改史数。
