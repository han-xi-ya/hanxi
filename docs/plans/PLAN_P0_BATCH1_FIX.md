# P0 批 1 正式修复计划（开工前范围确认，依 P0 纪律）

> 依据 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md) §3.1/§3.3 已核实证据（本会话已对代码本体复核成立）。批 1 只含两项；§3.2 租约、§3.4 journal、§3.5 Commit 属批 2，§4 前端属批 3，本计划不触碰。

## 项一：Registry 覆盖态数据竞态（`internal/extapi/registry.go`）

**病因**（复核确认）：`overrideOf` 持锁取/建 `*stateOverride` 后**指针逃出锁域**，`SetMandatory/SetBlocked/SetHealth` 锁外写字段；`projectState` RLock 内取指针、解锁后读——`health` 与 `remoteVersion` 可来自不同轮更新（混合快照）+ 真 data race。

**修法**（消灭指针而非加锁补丁）：
1. `overrides` 由 `map[string]*stateOverride` 改为 **`map[string]stateOverride` 值类型**；
2. 三个 setter：全程持写锁做读-改-写；`projectState`/`IsMandatory` 等读者在 RLock 内**复制值**后离锁；
3. 全量清点 `overrides` 触点（含持久化/加载路径）随改（实施时 grep 收口）；
4. 新增并发回归测试：SetHealth/SetBlocked × ListStates 并行断言"health 与 remoteVersion 必同源一轮"；`go test -race ./internal/extapi/` 云侧容器先跑（该包 Linux 可构建已验证），Windows 面由 CI/真机流程兜底。

**行为不变量**：状态机、派生逻辑、投影输出零变化；前端与绑定零改动。

## 项二：receipt 与 known-modules 成组设计缺陷（`internal/settings/receipts.go`）

**修法四要点（a–d，对应审查 5 条）**：
- **(a) 命名空间分离**：账本迁出 receipts 目录 → 状态目录 `modules-ledger.json`；启动时一次性迁移（旧文件在场且新文件缺失→读入合并）；`known-modules` 列入模块 ID 保留名单，`sanitizeModuleID` 拒绝——路径冲突面永久关闭；
- **(b) 独立 schema**：`ledgerSchema=1` 自有版本线（不再蹭 `ModuleContractSchema`，前端契约升版不再无谓重置卸载记忆）；遇**更高未知版本**：只读+告警、不做补建也不做卸载变更（启动不炸，写动作 fail-closed）；
- **(c) 三态语义替代"空名单起步"**：
  | 现场 | 判定 | 动作 |
  |---|---|---|
  | 账本在场且版本合规 | 正常 | 按账本执行（seen 跳过、uninstalled 永不补建） |
  | 账本缺失 + receipts 目录非空 | 升级首次引入/账本丢失 | **按磁盘凭据事实重建**：在场=known；缺席=视为已卸载（不补建）——"缺席=卸载"保守语义根除复活 |
  | 账本缺失 + receipts 目录空 | 全新安装 | 全量认全+建账（现行为保留） |
  | 账本损坏/解析失败 | 灾备 | 读 `.bak` 副本；仍败按上一行"重建"路径走并记 error——永不静默空名单 |
- **(d) 卸载事务顺序（fail-closed）**：`MarkAbsent` **先**落账本 tombstone（主文件+`.bak` 双写成功）**才**删 receipt；账本写失败→卸载报错、凭据原样（可重试，卸载绝不"半成功"跨重启复活）；`EnsureSeen` 恒跳过 `seen ∪ uninstalled`；重新安装成功时才销 tombstone。账本结构 `{schema, seen[], uninstalled[]}`。

**测试**：三态重建×tombstone 顺序×`.bak` 回退×未知 schema 单测矩阵；故障注入（账本写失败/主文件损坏）；旧布局迁移用例（`receipts/known-modules.json` 在场）；`-race` 全跑。**文档**：ADR-0001 补"卸载跨重启"一节；行为变更记 TROUBLESHOOTING（若实施中踩新坑）。

**风险如实报**：(c) 让"升级用户手工清空某模块 receipt 但未走卸载流程"的场景被当成已卸载（不再自动补建）——这类手工清理本就意图卸载，方向一致；回滚到旧版 hanxi 时旧代码视"无账本"为全量补建，卸载记忆丢一次（重新卸载即恢复，已列入迁移文档）。

## 规模与节奏预估

| 步 | 内容 | 验证 |
|---|---|---|
| 1 | 项一（registry）单提交 | 容器 `-race` + win vet + 既有 extapi 全量测试 |
| 2 | 项二（receipts）单提交（可拆 2a 迁移/2b 语义 两笔） | 单测矩阵 + 装配根集成回归 + 容器 `-race` |
| 3 | §5 关联两项复核（#1 cancellable、#7 Acquire guard 与 Health/blocked 关系）随批登记裁决，**不**在本批修 | 复核结论落审查文档 |

**待机主确认**：项二 (d) 的 fail-closed 语义（账本写失败=卸载失败可重试）与 (c) 的"缺席=已卸载"保守判定。无异议回"按此干"即开工；两项均为行为语义决策，改与不改都已给利弊。
