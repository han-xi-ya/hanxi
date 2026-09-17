# F1-F9 九件套开发进度账

> **用途**：多 agent 分波次开发的统一进度追踪。会话中断后，新会话读本文件即可恢复到断点。
> **维护纪律**：只有协调者（主会话）更新本文件；开发 agent 不得改动，避免合并冲突。
> 方案唯一依据是各任务卡片指向的 `docs/plans/PLAN_*.md` 与 `docs/BACKLOG.md` 卡片，本文件只记"做到哪了"。

## 基线与测试口径

- 基线 commit：`4f67581`（dev 分支；wave1 三分支 feat/f6-siblingdir、feat/f5-wxver、feat/f8-board 均自此检出，2026-09-17 开工）。
- 已知环境噪音：沙箱内 `bcu/instance`、`bili23/instance` 等报 `cmd.exe 不可用` 的测试失败是环境限制，不算回归；其余测试必须全绿。
- 前端 `npm run` 在 Git Bash 下损坏：build/test/typecheck 走 `frontend/node_modules/` 内入口直调（vitest、vue-tsc、vite）。
- worktree 内前端测试需要 node_modules：已用 junction 链回主仓 `frontend/node_modules`。

## 波次计划

| 波次 | 任务 | 并行理由与依赖 |
|---|---|---|
| 一 | F6 数据目录同级化 / F5 微信软件版本检测 / F8 桌面留言板 | 互相零文件交集；F6 是地基先行；F5/F8 是独立新模块（registry 一行级冲突协调者收） |
| 二 | F1 统一历史 / F2 剪贴板惯例 / F3 数据自动快照 | F6 合入后落点确定才能开工；F2 的悬浮卡硬依赖已在 dce4be3 解除 |
| 三 | F4 MCP 接入 / F7 hanxi-ocr 托管化 / F9 WSL USB 共享 | F4 硬依赖 F1/F3 合入（工具面引用其能力）；F7/F9 依赖 F6 落点 |

## 总账

| 任务 | 状态 | 分支 | 波次 | 合并 commit | 备注 |
|---|---|---|---|---|---|
| F1 统一历史记录 | ✅ 已合并 | feat/f1-history | 二 | fd830ab | PLAN_HISTORY.md，决策已全回写 |
| F2 剪贴板惯例 | ✅ 已合并 | feat/f2-clipboard | 二 | 6a38422 | PLAN_CLIPBOARD.md 三段全落；热键封装 internal/hotkey |
| F3 数据自动快照 | ✅ 已合并 | feat/f3-snapshot | 二 | 7a6cc78 | PLAN_SNAPSHOT.md 全量；三红线守住（永不 push/影子拷贝/对外"历史版本"） |
| F4a MCP 无头 server | ✅ 已合并 | feat/f4-mcp | 三 | 021ce5e | C1-C5 全落（含 ocr/memo 工具，默认全关）；S2 已收口；真机 stdio 管道冒烟已过；剩 C9 三客户端红队 |
| F4b MCP 安装向导 | ✅ 已合并 | feat/f4b-mcpwizard | 三 | aa48a73 | internal/mcpwizard 独立包零依赖 F4a；AI 接入第 8 分区（52/40/40）；坑占 #61；access.json 对账清单见详情 |
| F5 软件版本检测 | ✅ 已合并 | feat/f5-wxver | 一 | d79f235 | softver 模块；本机真机双口径实测 4.1.15.9 三读数一致；官方页实连 0.37s；winget 备用通道保守未做（裁定记录在卡） |
| F6 数据目录同级化 | ✅ 已合并 | feat/f6-siblingdir | 一 | 8ba24bd | 实弹全过：裸 exe 全新目录自动建同级 hanxidata ✓、mcp 无头握手 ✓、用户目录零写 ✓；绑定指针=同级 hanxi.bind |
| F7 hanxi-ocr 托管化 | ✅ 已合并(主仓侧) | feat/f7-ocrhosted | 三 | aee1119 | 真 paddle 36MB 包全链集成测过；三契约令全落；坑重排 #66；剩实 UI 冒烟入人工闸门 |
| F8 桌面留言板 | ✅ 已合并 | feat/f8-board | 一 | 00f1dc9 | KeepAwake 聚合器入 platform；msgboard 模块；导航计数并集已重算 |
| F9 WSL USB 共享 | ✅ 已合并 | feat/f9-wslusb | 三 | 91f5ce3 | **卡片合同有误已按上游实况纠偏**（list 无 --json，改 state 源）——BACKLOG 回写列入收尾批；真机闸门清单 7 条见 F9 报告 |
| F7k 后厨发布流水线 | ✅ 已交付(独立仓) | hanxi-ocr-dev `4300765`/`e8ab2b2`/`3330dbf` | 三 | - | 真双包已产出并核验；manifest 超集裁定+version 语义三令已转 F7 主仓侧；selftest 11/11 |
| R1 热键收编 | ✅ 已合并 | feat/r1-hotkey | 三 | d96aac6 | msgboard→internal/hotkey 槽位 msgboard/toggle；改键序内部升级为"先新后旧"，用户可见零差异 |

图例：⬜ 未开始 / 🟡 开发中 / 🔶 待审查 / ✅ 已合并 / ⛔ 阻塞

> **2026-09-18 停机实况**：F5/F6/F7/F7k/F9 五个在途 agent 曾被子夜停（全部零沉没成本），重派后装"里程碑即时切账"纪律全部交付。

## 完工总览（2026-09-18 收尾批后）

**九件套 9/9 全部入 dev**（F4 拆 a/b、F7 拆主仓/后厨共 11 支），另有 R1-R5 随批线四清一裁。收尾批已完成：gofmt 全仓清账（6cdf9d7）、F9 卡上游纠偏回写 + DEVPLAN 模块数核正 41（ac37090）。**最终门禁全家福**：`go build` ✅、`go test ./...` 除 instance 沙箱噪音 ✅、vitest **92 文件 923 用例** ✅、vue-tsc ✅、bindings 再生零 diff ✅。全部本地提交，未 push（等你指令）。

### 人工真机验收清单（按优先序，都是 agent 变不出显示器的项）

1. **F6**：把 bin 里新编的 hanxi.exe 拷到任意新目录双击 → 自动建 hanxidata；Program Files 场景弹窗引导；编辑 hanxi.bind 换绑；有 %APPDATA%\Hanxi 老数据的机器首启搬家不丢。
2. **F7**：拖 `hanxi-ocr-dev\dist\` 双 zip 实装（paddle 即装即生效、wechat 登记不切换）、卸载在用版本拒卸、MCP/服务链路复验。
3. **F8**：挂牌全屏观感（多 DPI/副屏）、防休眠实测、热键 Ctrl+Alt+B 抢键。
4. **F2**：Ctrl+Alt+T 与微信/QQ/snipaste 抢键实测；WebView2 剪贴板权限体验。
5. **F3**：失焦自动快照首拍、提权运行时 git 可用、"历史版本"分区回滚一单验证。
6. **F9**：按 F9 报告七步闸门（装 usbipd→插设备→bind/attach→⭐自动重放）。
7. **F5**：大数据目录首扫耗时/取消手感；有 3.x 微信的机器补验旧代口径。
8. **F1**：便携/标准两目录各开一次确认 history.json 落点（随 F6 搬家验）。
9. **F4**：真 Claude/Codex/Cursor 三客户端红队（C9）——向导装完各拉一次工具调用。

## 恢复协议（断了怎么接上）

1. 读本文件总账，找出非 ✅ 的任务。
2. `git branch --list 'feat/f*'` + `git log dev..feat/fX --oneline` 看分支做到哪。
3. 波内任务：agent 未完成即会话死亡 → 分支上有半成品 commit，可续派（新 agent 从分支 HEAD 接着做）或直接审查合入。
4. 协调者死亡（agent 完成但没合并）→ 各 worktree 路径 `E:\System\桌面\工具\hanxi\.claude\worktrees\wt-fX`，按上表波次顺序合并审查。
5. 合并顺序永远按波次：一(F6→F5→F8) → 二(F6 合入后再开)：F1→F2→F3 → 三：F4→F7→F9。
6. 每波全部合入 dev 并复跑全量测试后，才开下一波。

## 任务进度详情

### wave2 合并日战果（2026-09-17）

- **F1** `fd830ab`：internal/history 公共包（200/桶、4000 rune 截断、Redact 入口）+ ocr/portkill/envcheck 接入 + HistoryPanel.vue；保守裁定：`ListListeningPorts` 大表列举不入史。
- **F8** `00f1dc9`：platform.KeepAwakeAPI 租约式引用计数（SetThreadExecutionState 收拢 LockOSThread 唯一 owner 协程）+ msgboard 模块三通道 + 全屏挂牌真销毁；导航计数并集重算 51/40/40（webapp+msgboard+F3 分区叠加，F3 合并日修正）。
- **F2** `6a38422`：utils/paste.ts 三态分流 + UiClipboardField + 34 处复制收口 + `Ctrl+Alt+T` 剪贴板识图 + `internal/hotkey` 通用注册器（msgboard 薄封装待收编，见 R1）。
- **F3** `7a6cc78`：internal/snapshot（git 检查点、info/exclude 反向白名单、失焦/隐藏/空闲三源触发、影子拷贝 30 份降级、永不 push）+ 设置第七分区"历史版本" + memo 一条一文件幂等迁移。
- 合并修面：`0c8f0db` portkill fakePlat 补 KeepAwake（F1 mock × F8 接口扩展的跨分支语义冲突）；踩坑号定序：56 webapp / 57 历史 bindings 漂移 / 58 Vue Boolean casting+PATH 假失败 / 59 wails3 看不见 go / 60 git pathspec 白名单；新增坑编号从 **#61** 起。
- 合并后 dev 门禁全绿：go build ✅、go test 除 instance 噪音 ✅、vitest 90 文件 888 用例 ✅、vue-tsc ✅、`wails3 generate bindings -clean` 零 diff ✅。
- 真机验收闸门（人工）：F3「立即快照」首拍+提权下 git 可用；F8 全屏观感/防休眠实测；F2 Ctrl+Alt+T 抢键实测。

### 随批修项

- **R1**：✅ 已偿（d96aac6 + bindings 回补 bb7905a）。

### F4a↔F4b access.json 对账单（合并 F4a 时逐项核）

- F4b 按 PLAN §6 字面读：`<DataDir>/mcp/access.json`，`{"version":1,"tools":{"envcheck","everything","ocr","memo"}}`，只读呈现，额外键忽略。
- F4b 所有权回执在 `<DataDir>/mcp/install.json`（version/installs{configPath,fingerprint,installedAt}）。
- **若 F4a 实际落盘 `state/mcp/` 或字段超纲**：以 F4a 报告为准改 `mcpwizard/service.go` NewService 两行路径，字段扩展要求 F4a 报告单列。
- **R2**：✅ 已收口（eee224a）——SelfCheck spawn 握手 + 90s TTL 缓存 + 预览页呈现；阻断性裁定=自检失败**不阻断**写入（PLAN §2.5 口径落档）；access 缺档文案假承诺一并纠正；实弹冒烟（HANXI_EXE opt-in 真 hanxi.exe）已过；坑 #64 归 R2。
- **R5 登记（与 §6 写入口待裁项并案）**：`internal/mcp/server.go` gateMiddleware 拒绝文案引导用户"到 设置→AI 接入 开启"，但该分区对 access.json 只读无开启入口（写入口缺位=R4 上报的待裁项）——用户裁决"补写入口"则文案变真、裁决"维持手工放置"则该文案须改口径，二选一后此单自动关闭。
- 踩坑号定序（全部已占）：#61=F4b JSON 外科合并、#62=F4a 离线 GOMODCACHE 洞、#63=F4a windowsgui stdio；后续从 **#64** 起。
- **R3**：✅ 已收口（`8476ff3`/`f09fc19`，FF 合入）。goproxy.cn 可达实证，cast replace 已撤、tidy 内容级零 diff、离线自洽复核全绿；easyjson v0.9.0 维持（回退=逆钉红线，且符合"用新不用旧"）。附注：本机 `core.autocrlf=true`，`go mod tidy -diff` 对 go.sum 报**假全文件 diff（纯 EOL）**，内容级核验用 `go mod tidy`+`git diff`。
- **R4**：✅ 已收口（74f8fa7）——PLAN_MCP 注记回写/BACKLOG 销账/DEVPLAN v1.5 指针改口/MOOTOOL 打勾四合一入；其发现的 mcpwizard 文案与事实矛盾点已塞给 R2 顺路修；遗留待用户裁决：PLAN §6 两个 access.json 授权写入口均未实现（GUI 只读+无 auth 子命令），现状=手工放置文件。
- 文档陈旧计数待核（R4 上报，收尾批处理）：DEVPLAN §2.3 与 ARCHITECTURE"37 模块统一注册"疑落后于 webapp/msgboard 入账后的实际数。

### 待用户裁决清单（不阻塞合并，收尾时逐项拍板）

1. **MSIX 形态去留**（F6）：只读目录下连 hanxi.bind 都写不下，现行为=启动弹窗引导搬家；继续支持/正式放弃？
2. **access.json 授权写入口**（F4 §6 + R5 并案）：补 GUI 开关/auth 子命令 → MCP 拒权文案变真；或维持手工放置 → 改文案口径。
3. **48MB 单文件版去留**（F7 卡片遗留）：托管化落地后是否停产？
4. **paddle 源码拆仓**（F7 卡片遗留）。
5. **sha256 旁挂件缺失的宽容度**（F7 实现裁定）：现按契约"三件套"**必检拒收**；若私发链路存在只发 zip 单件的习惯，放宽为"缺旁挂仅告警"一行可改。

### F7k 后厨交付注记（2026-09-18，独立仓 hanxi-ocr-dev）

- 真包在 `E:\System\桌面\工具\hanxi-ocr-dev\dist\`：paddle-0.4.0-alpha.zip（36.2MB）/ wechat-4.1.15.9.zip（49.7MB）+ 旁挂 `.sha256`（**纯 hex 一行不含文件名**）。
- manifest 为契约六字段**超集**（额外 name/files/build_date）——主仓解析端必须容忍未知键；`version`=引擎版本，勿与组件 `/api/status` 的组件版本（微信 0.3.1）交叉核对；paddle status.engine 上报 `"paddle"` 非档位串。
- 微信引擎换版/补 minHanxi：按 RELEASE.md §一 传参重跑 `bash release.sh`（selftest 11 项含七反例必拒闸门）。
- **S2**：✅ 已随 F4a 收口（everything 工具诚实文案有测试锁定 + BACKLOG 收账提交 7392e44）。
