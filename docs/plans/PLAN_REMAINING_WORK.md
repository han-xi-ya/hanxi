# Hanxi 剩余工作排期(项目暂停基线)

> **文档状态**:唯一复入口。项目于 2026-09-20 暂告一段落,恢复工作时以本文为起点,不必再跨多个文档自行拼接现状。
>
> **基线**:`dev` @ `5773ff9`,工作区无未提交代码改动;Wave 0–5 全部落地;41 个业务模块;前端全量门禁绿(112 测试文件 / 1261 项 / `vue-tsc` / 生产构建 / ESLint 0 error)。并行会话(F1–F10、Wave 系列)的工作均已入库,经全 worktree 扫描确认**无任何未提交的代码欠账**。

---

## P0:代码质量修复(复进第一件事)

证据与逐条代码定位全部在 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md),已暂停待批,恢复时按原批次执行、不重做审查:

| 批次 | 内容 | 关键落点 |
|---|---|---|
| 批 1 | Registry 覆盖状态数据竞态;receipt 与 `known-modules.json` 成组设计缺陷 | `internal/extapi/registry.go`、`internal/settings/receipts.go` |
| 批 2 | 异步托管下载提前释放 Acquire 租约;journal 步进失败仍执行副作用(fail-open);Artifact Commit rename 后/meta 前崩溃窗口 | `internal/modules/*/service.go`、`internal/ops/ops.go`、`packages/go/artifact/` |
| 批 3 | 前端旧快照冒充实时状态、本地/远程版本加载竞态、`already-installed` 清票未走版本互认、旧响应覆盖(generation)、多份 busy 真相、无障碍缺口 | `frontend/src/components/managed/`、`frontend/src/composables/loadManagedVersions.ts` |

- 每批开工前先形成正式修复计划确认范围;批 1、2 完成后补 `go test -race`(需具备 CGO/GCC 的 Windows 环境)与组合级故障注入。
- 审查文档 §5 另有 9 项"已发现待复核/裁决"事项,随批次一并收口。

## P1:真机验收债

1. **F10 网页应用(webapp)冒烟**:task dev 开微信扫码、X 关窗后 WebView2 内存回落、收起 TTL 释放——唯一纯新未验项,成本最低,建议最先做。
2. **F6(存储目录)、F3(快照)、F4/F4b(MCP 与向导)、F7(OCR 托管)、F9(WSL USB)**:按 [PROGRESS.md](../PROGRESS.md) 既有人工清单补跑。
3. **A 机 → NAS → 干净 B 机黄金路径**:先完整走一次手工验收(建立模块迁移等级:可携带/需重建缓存/需系统注册/需重填凭据/不适合临时电脑),据结果再决定机器绑定恢复提示与临时电脑模式是否立项。

## P2:文档与工程卫生

- [x] 三份未跟踪文档已随本排期一并入库:本文、CODE_QUALITY_REVIEW_PROGRESS_2026-09-19、FUTURE_FEATURE_RECOMMENDATIONS。
- [x] 卫生清理已执行完毕(2026-09-20,机主点名批准):28 个 worktree 目录(全 MERGED)注销删除,含 hubkit 旧名时代 3 个失效 locked 登记;已合并分支(`feat/f*`、`fix/q*`、`feat/r*`、`fix/nine-suite-quality`)与全部无主 `worktree-agent-*`  scratch 分支删除。现仓库仅存 `dev`/`main`/`pre-split-backup` 三分支、单一工作树。注:8 个 scratch 分支同指 hubkit v0.2.0 时期 release 合并点 `60aff0c`(不在任何 tag/远程/备份可达范围),删除后仅 reflog 可回捞(约 90 天窗口)。
- [ ] 事实漂移收口:README/PRD/PROJECT_STRATEGY 的 37/38 旧统计 → 41;旧数据根语义(`hanxidata` 标记、`%APPDATA%` 回退、旧 `data/` 兼容)→ 现行"exe 同级 + `hanxi.bind`",含 Release workflow 便携说明;BACKLOG 中 WebApp 状态更新。
- [ ] gofmt 闸门红在 8 个历史文件(`logging/everything/ocr/platform` 等):机主裁决——建议单开一次 `style(frontend)`/`chore` 性格式化提交,不与功能混淆。

## P3:已暂停批次(恢复与否由机主定)

- **前端打磨批次一**:审查与实施计划已完成,三路实现已停止且主工作区无本轮改动;恢复时直接重新并行实施,不必重做审查。

## 路线参考(未排期)

- 中长期三阶段(搬家与数据真相 → 统一治理 → 灾备与恢复)见 [FUTURE_FEATURE_RECOMMENDATIONS](../FUTURE_FEATURE_RECOMMENDATIONS.md);仅方向,非承诺。
- Wave 4X(外部实例控制)维持延期;manifest、签名 Catalog、物理拆包为 N/A 条件路径(ADR-0003),非默认欠项。

## 复进建议顺序

1. 提交三份文档、执行 worktree 清理(收尾本次暂停);
2. P1-1 F10 冒烟(半小时级,先清最便宜的债);
3. P0 批 1(竞态 + receipt 设计)→ 批 2(租约/journal/Commit)→ 批 3(前端真相);
4. P1-2/3 真机与黄金路径验收;
5. P2 文档漂移随各批提交顺带收口;P3 视意愿插空恢复。
