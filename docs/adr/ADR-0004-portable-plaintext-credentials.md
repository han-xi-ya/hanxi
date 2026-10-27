# ADR-0004：凭据明文随数据根携带，撤销 DPAPI 本机绑定加密

- **状态**：Accepted（2026-10-24，机主裁决）
- **推翻**：[ADR-0003](./ADR-0003-scope-cuts-for-personal-nas-first-tool.md) 决议 3
- **相关**：clipboard v1.6（`a0ba43f`，2026-09-27 已取消 DPAPI）、`internal/jsonstore`、`internal/settings/paths.go` 数据根策略

## 背景

Hanxi 是自用工具，数据根 `hanxidata/` 整目录扔在 NAS 上随人走（见 `docs/MOTIVATION.md`）——"软件装在 NAS"是这个项目立项的理由。而 Windows DPAPI 的密文绑定**当前 Windows 用户 + 当前机器**：换电脑、换账户、甚至重装系统后都解不开。

这两件事天生冲突。ADR-0003 决议 3 当年选了前者让位于后者："仅跨机解密不可能——这是安全属性而非缺陷"，并要求解密失败时给出"疑似更换了电脑，请重新填写"级提示。实际结果是：那条提示从未落实（该 ADR 自己在 2026-10-21 的对账里把它记为**存疑项**，"疑似更换了电脑"字样全仓零命中），而 clipboard 已在 2026-09-27 先一步裁决取消 DPAPI。到本次为止，全仓只剩 `frpc` 一个写侧还在用密文。

机主复判：**换电脑还能用，比"本机磁盘上的凭据不被同机其他进程读走"更重要。**

## 决策

1. **删除 DPAPI 全套**：`internal/platform/windows/dpapi.go` 移除，仓内不留加密也不留解密能力（不留"以后也许能用"的死通道——上一轮 clipboard 保留的 `seal/open` 恒等缝已经证明这类缝只会让人误以为还有保护）。
2. **`frpc` 项目 Token 明文落盘**（`<数据根>/state/projects.json`），与 clipboard、Memo、微信 token 同谱口径。
3. **历史 `dpapi:` 密文判失效并清空**，不驻留、不"直通写回"：
   - 旧实现（BUG-031 回归线）刻意让解不开的密文原样驻留，是为了防止"把密文当明文再包一层"永久锁死原 Token；
   - 现在没有解密能力了，继续驻留只会让一个**非空而无意义**的字符串待在 Token 位上，用户去连服务器得到"认证失败"，而不是"这里没填"——那是更难查的假象。清空后该格为空，重填是唯一路径，根因由 `slog.Warn` 点名。
4. **文档口径同批更正**：`CLAUDE.md` 技术栈行不再宣称 DPAPI 加密；`docs/ARCHITECTURE.md` 架构图、frpc 第 3 条、bespoke 名单措辞、ADR-0003 摘要行同步；ADR-0003 决议 3 原文**不改写**，就地加 superseded 注记（本 ADR 的存在就是它的变更记录）。

## 代价（明确接受，不粉饰）

- 数据根一旦被同步到 NAS/网盘，`frpc` Token、微信 `BotToken`/`ContextToken`、剪贴板正文都是**明文可读**。任何能读那份备份的人=能读这些凭据。
- 同机其他进程/同用户下的恶意软件读取门槛降低（原本 DPAPI 至少要求"当前用户上下文内执行"这一层）。
- ADR-0003 决议 3 那句"配套人话提示"要求随本 ADR 作废：存储层已用"清空"消化掉失效语义，前端表现为必填项为空，不再单独做文案。**若以后要做，触发点是"用户反馈找不到该重填哪里"，不是"补完当年那条要求"。**

## 影响面

- 代码：`internal/modules/frpc/{store.go,store_token_test.go}`、`internal/platform/windows/{dpapi.go(删),platform.go}`；MCP 的 clipboard 读侧本就是恒等函数（v1.6），无需改动。
- 测试：`store_dpapi_test.go` 的两条 DPAPI 断言（密文驻留、加密可还原）随能力一起删除，替换为 `store_token_test.go` 的"明文逐字落盘"与"历史密文作废且不回写"。
- 既有数据：老库里的 `dpapi:` 值会在下一次 load+save 后消失，**不可恢复**，需重填 Token。本次裁决为"直接删干净、机主重填"，故不做一次性迁移解密。
