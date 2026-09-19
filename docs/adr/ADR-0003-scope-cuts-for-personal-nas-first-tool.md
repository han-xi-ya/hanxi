# ADR-0003:面向"自用 + NAS 同步搬家"定位的范围裁剪(签名/物理拆包/加密搬家化)

- 状态:已接受
- 日期:2026-09-19
- 关联:`docs/MOTIVATION.md`(项目缘起)、`ADR-0002`(内核稳定门)、两份专项计划的 Wave 5/6 段

## 背景

分发专项为"官方对外按需分发"设计了签名目录、撤回、多产物发布与 sidecar 物理拆包;roadmap 将其排为 Wave 5/6。经与项目缘起(个人自用、飞牛同步整目录="软件装在 NAS"、无公开发布渠道)对账,用户裁决按实际受众裁剪范围。

## 决议

1. **官方 Catalog 签名链 / 密钥基建 / 撤回演练:不实施。**
   供应链完整性的实际承担者改为:HTTPS + 上游官方摘要必检(GitHub `asset.digest` / 官方 sha256 清单,已由 `artifact.Fetch` 全线强制)。「弱摘要/缺摘要上游」按 ADR-0002 §5 薄适配器处理,不为凑信任根自建签名体系。
   影响:`PLAN_OFFICIAL_MODULE_DISTRIBUTION.md` Phase 4/7 的签名相关 DoD 视为 N/A;`schemas/official-catalog` 与 key 轮换相关产物不生成。
2. **Wave 6 物理拆包(OCR/FileShare sidecar、多产物发布):缓行,无排期。**
   触发条件改为实测需求:宿主产物体积成为实际痛点(当前 production exe ≈ 22MB)或发布通道真实建立之后。在此之前 OCR/FileShare 继续内建;`.hxmanaged/.hxmodule` 包格式不设计。
3. **DPAPI 凭据保持本机绑定,不改造为可迁移加密。**
   搬家语义已由数据根整体同步覆盖(逻辑安装凭据 receipt、版本资产、用户数据均在 `<数据根>` 内随行);仅"跨机解密"不可能——这是安全属性而非缺陷。配套要求:解密失败路径必须给出「疑似更换了电脑,请重新填写」级人话提示(W3 批次落实),不改存储格式。
4. **单家需求不扩内核(重申 ADR-0002 §3 阈值的执行案例):**
   - quicklook 的 Reload 命名管道命令通道 → 留模块直投,其 instance 引擎明示 bespoke;version 侧照常迁 artifact。
   - bili23 的"退出不强杀 + 三态如实上报" → 同上,instance 留 bespoke,version 侧迁移。
   两者并入"留 bespoke 名单",不属于欠账。

## 后果

- Wave 5 的实质完成定义收缩为:托管模块去重复(本 ADR 前已完成)+ 更新感知(health=update-available 由真实上游对比驱动,`internal/updatewatch`)。**声明式 manifest 引擎与签名 Catalog 不再列入路线图。**
- Wave 6 从"计划波次"降级为"条件触发项";roadmap 的发布节奏与渠道纪律(dev/beta/stable、签名演练)对单用户自用场景整体标记 N/A。
- 若未来出现真实发布渠道(给他人分发),重开本 ADR 取代为 v2,签名链回到台面。
