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
| F4 MCP AI 接入 | ⬜ 未开始 | feat/f4-mcp | 三 | - | PLAN_MCP.md；含 S2 随批修 |
| F5 软件版本检测 | 🟡 开发中 | feat/f5-wxver | 一 | - | BACKLOG 卡片即方案；官方通道已实连验证；worktree wt-f5-wxver |
| F6 数据目录同级化 | 🟡 开发中 | feat/f6-siblingdir | 一 | - | BACKLOG 卡片；访问器面一字不动；worktree wt-f6-siblingdir |
| F7 hanxi-ocr 托管化 | ⬜ 未开始 | feat/f7-ocrhosted | 三 | - | 主仓侧改造；后厨侧只读参考 |
| F8 桌面留言板 | ✅ 已合并 | feat/f8-board | 一 | 00f1dc9 | KeepAwake 聚合器入 platform；msgboard 模块；导航计数并集已重算 |
| F9 WSL USB 共享 | ⬜ 未开始 | feat/f9-wslusb | 三 | - | 账本+重放范式；真机验证为合入后闸门 |

图例：⬜ 未开始 / 🟡 开发中 / 🔶 待审查 / ✅ 已合并 / ⛔ 阻塞

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

- **R1**：msgboard 热键（`internal/modules/msgboard/hotkey.go` 薄私有封装）收编进 F2 的 `internal/hotkey` 通用注册器——接缝级，随 wave3 合并批顺手做。
- **S2**：随 F4 批（PLAN_MCP 已含 ES.exe 文案改真话任务，F4 agent 合同内）。
