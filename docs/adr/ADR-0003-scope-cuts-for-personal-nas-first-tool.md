# ADR-0003:面向"自用 + NAS 同步搬家"定位的范围裁剪(签名/物理拆包/加密搬家化)

- 状态:已接受(2026-10-21 状态核查确认仍生效,未被取代;裁剪执行实况见"后续实况")
- 日期:2026-09-19
- 关联:`docs/MOTIVATION.md`(项目缘起)、`ADR-0002`(内核稳定门;其 §3 阈值在本 ADR §4 重申,其 §4 校验口径被本 ADR 决议 1 收编)、`ADR-0001`(工作台契约;其 §1.4 登记的"装配 setter 待 Wave 5 声明化清偿"路径因本 ADR 裁剪而失去承载波次,其 §2 `pending-consent`/`blocked` 的签名启用前提同受本裁剪影响)、两份专项计划的 Wave 5/6 段

## 背景

分发专项为"官方对外按需分发"设计了签名目录、撤回、多产物发布与 sidecar 物理拆包;roadmap 将其排为 Wave 5/6。经与项目缘起(个人自用、飞牛同步整目录="软件装在 NAS"、无公开发布渠道)对账,用户裁决按实际受众裁剪范围。

## 决议

1. **官方 Catalog 签名链 / 密钥基建 / 撤回演练:不实施。**
   供应链完整性的实际承担者改为:HTTPS + 上游官方摘要必检(GitHub `asset.digest` / 官方 sha256 清单,已由 `artifact.Fetch` 全线强制)。「弱摘要/缺摘要上游」按 ADR-0002 §5 薄适配器处理,不为凑信任根自建签名体系。
   影响:`PLAN_OFFICIAL_MODULE_DISTRIBUTION.md` Phase 4/7 的签名相关 DoD 视为 N/A;`schemas/official-catalog` 与 key 轮换相关产物不生成。
2. **Wave 6 物理拆包(OCR/FileShare sidecar、多产物发布):缓行,无排期。**
   触发条件改为实测需求:宿主产物体积成为实际痛点(当前 production exe ≈ 22MB)或发布通道真实建立之后。在此之前 OCR/FileShare 继续内建;`.hxmanaged/.hxmodule` 包格式不设计。
3. **DPAPI 凭据保持本机绑定,不改造为可迁移加密。**
   > **已被 [ADR-0004](./ADR-0004-portable-plaintext-credentials.md) 撤销（2026-10-24 机主复判）**：换电脑还能用优先于本机绑定这一安全属性，本决议连同其"配套人话提示"要求一并作废（那条提示当年也未落实，本文 2026-10-21 对账已记为存疑项）。原文按 ADR 惯例保留不改写，代价清单见 ADR-0004。
   搬家语义已由数据根整体同步覆盖(逻辑安装凭据 receipt、版本资产、用户数据均在 `<数据根>` 内随行);仅"跨机解密"不可能——这是安全属性而非缺陷。配套要求:解密失败路径必须给出「疑似更换了电脑,请重新填写」级人话提示(W3 批次落实),不改存储格式。
4. **单家需求不扩内核(重申 ADR-0002 §3 阈值的执行案例):**
   - quicklook 的 Reload 命名管道命令通道 → 留模块直投,其 instance 引擎明示 bespoke;version 侧照常迁 artifact。
   - bili23 的"退出不强杀 + 三态如实上报" → 同上,instance 留 bespoke,version 侧迁移。
   两者并入"留 bespoke 名单",不属于欠账。

## 后果

- Wave 5 的实质完成定义收缩为:托管模块去重复(本 ADR 前已完成)+ 更新感知(health=update-available 由真实上游对比驱动,`internal/updatewatch`)。**声明式 manifest 引擎与签名 Catalog 不再列入路线图。**
- Wave 6 从"计划波次"降级为"条件触发项";roadmap 的发布节奏与渠道纪律(dev/beta/stable、签名演练)对单用户自用场景整体标记 N/A。
- 若未来出现真实发布渠道(给他人分发),重开本 ADR 取代为 v2,签名链回到台面。

## 后续实况(2026-10-21 增补,只登记事实,不改历史决议正文)

- **裁剪执行核对(与决议一致)**:①声明化/签名降级已入 roadmap——"声明式 manifest、签名 Catalog、撤回与 key 轮换不再是默认路线,统一记为 N/A 条件路径;Wave 6 物理拆包与多产物发布同样仅在触发条件满足时重开"(`docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md` 2026-09-19 进度行,另见第 41、489 行);②更新感知已入库(`internal/updatewatch` 首笔 `87e8a2a`,2026-09-19);③决议 4 的 quicklook/bili23 "仅版本面迁入、instance 留 bespoke"由 roadmap 进度行复核在案。
- **决议 1 的"签名产物不生成"坐实**:`schemas/` 目录从未出现在 git 历史(`git log --diff-filter=A -- schemas/` 为空),无 key 轮换产物入库。
- **决议 2 缓行至今未触发**:`.hxmanaged/.hxmodule` 包格式无实现,该扩展名仅存在于 `docs/plans/PLAN_OFFICIAL_MODULE_DISTRIBUTION.md` 350-354 行的文本图例;ocr/fileshare 仍内建在册。"production exe ≈ 22MB"为 2026-09-19 快照口径,仓内无权威新值(`docs/FACTS.md` 未登记体积),触发条件继续等实测,不因无新数而视为失效。
- **决议 3 搬家语义持续兑现,配套提示存疑**:后续新增托管模块(最近一笔 `3bc27ec`,2026-10-21,heukms)均沿用数据根随行,未出现"可迁移加密"改造;`internal/platform/windows/dpapi.go` 仍为本机账户绑定。但"疑似更换了电脑,请重新填写"级人话提示未能证实已落实——该字样全仓零命中,近旁证据仅有剪贴板库解密失败的降级剔除(`internal/modules/clipboard/store.go:175`)与 MCP 侧"多为其它机器/账户的库"说明(`internal/mcp/tools_clipboard.go:314`),是否以其它文案落实需专查(存疑项)。
- **决议 4 口径出现反例案例**:`469e3e1`(2026-10-20)将显式代理拨号内核自 portscan 上收为 `packages/go/netx` 公共件,上收时唯一消费面即 portscan 一家,与本决议"单家需求不扩内核"的执行口径存在张力;账目与裁量归属登记于 ADR-0002 §6"后续实况",两处同案。
