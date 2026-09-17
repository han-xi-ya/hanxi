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
| F1 统一历史记录 | 🟡 开发中 | feat/f1-history | 二 | - | PLAN_HISTORY.md，决策已全回写；worktree wt-f1-history |
| F2 剪贴板惯例 | 🟡 开发中 | feat/f2-clipboard | 二 | - | PLAN_CLIPBOARD.md；SnipCardView 依赖已解除；owns GlobalShortcut 封装 |
| F3 数据自动快照 | 🟡 开发中 | feat/f3-snapshot | 二 | - | PLAN_SNAPSHOT.md；对外命名"历史版本"；只调用路径访问器不碰 paths.go |
| F4 MCP AI 接入 | ⬜ 未开始 | feat/f4-mcp | 三 | - | PLAN_MCP.md；含 S2 随批修 |
| F5 软件版本检测 | 🟡 开发中 | feat/f5-wxver | 一 | - | BACKLOG 卡片即方案；官方通道已实连验证；worktree wt-f5-wxver |
| F6 数据目录同级化 | 🟡 开发中 | feat/f6-siblingdir | 一 | - | BACKLOG 卡片；访问器面一字不动；worktree wt-f6-siblingdir |
| F7 hanxi-ocr 托管化 | ⬜ 未开始 | feat/f7-ocrhosted | 三 | - | 主仓侧改造；后厨侧只读参考 |
| F8 桌面留言板 | 🟡 开发中 | feat/f8-board | 一 | - | 含防休眠引用计数自池收编；worktree wt-f8-board |
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

（协调者随合并滚动记录：合入 commit 清单、验收要点、遗留项）
