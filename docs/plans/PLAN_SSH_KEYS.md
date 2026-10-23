# PLAN_SSH_KEYS：sshkeys 模块分批实施计划（机主过目件）

> 状态：第 1 批（能力族分家 + 渲染校验包）✅ 已在工作区落地并实测绿，提交待落账；第 2–5 批 ⏳ 在途计划；其中第 3 批"写用户目录"存在**架构矛盾，⚑ 待机主裁决**（§③ 批 3、§⑥-B）——裁决前不开工。
> **文档定位**：`sshkeys` 模块（自 `portscan` 分家，搬迁动因与失真取证见 TROUBLESHOOTING #112）的分批实施台账，后续批次以本文为唯一裁决依据。界面设计归 `docs/plans/PLAN_SSH_KEYS_UX.md`（另线所有，本文不重复、不改动）。
> **计数口径**：本文**不写任何连锁计数**（模块总数、目录/条目数、ROUTES 数等由并行会话在改）；需要数字时以 `go run ./scripts/docsfacts -emit` 现推为准。文内代码路径与先例引用以当前工作区为准，各批落地后有出入以实施线回填修正为准。

---

## ① 一句话结论

把"跟人走的 SSH 机器账本"做成独立模块：**账本可随数据目录搬家，钥匙（私钥内容）永不跟账本走**；版本横幅探测、凭据测试、主机账本、导出投递、一键部署公钥五个能力族按批次生长，边界与副作用全部摆在明面上。

---

## ② 三条硬约束（先于任何功能，对所有批次生效）

1. **表里只存密钥路径，永不存私钥内容或密码**。理由链两条都是实证：① `hanxidata` 按机主用法整目录飞牛同步到 NAS 随身——私钥进该目录等于把 root 钥匙上 NAS；② DPAPI 与 Windows 账户绑定、换机解不开（`internal/modules/frpc/store.go` 已实证该行为，恰是它不能用来保护"要随身"的私钥）。密码只在本次调用的内存里活着，沿用 `internal/modules/sshkeys/sshauth.go` 口径：**不进结果、不进错误、过 `redactSecret`、不进配置**。
2. **别名字段是注入面**：允许换行就等于往 ssh 配置里注入 `ProxyCommand` 这类可执行本地命令的指令。已由双闸封堵——`packages/go/sshconf.ValidateAlias` 白名单（只放行 `[a-z0-9_-]`；实现另拒前导/尾随连字符，**比字面规则严**，收紧与否 ⚑ 见 §⑥-A）+ 渲染后 `selfCheck` 逐行自证（产物每行只认四种合法形态，Host 段头计数须等于渲染项数，堵死"注入行恰好复刻自家合法形态"）。
3. **MCP 面刻意不注册任何工具**：本模块的凭据测试会向对端发起**真实登录尝试**，而 `internal/mcp/mcp.go` 红线一规定纯查询族严格只读，破坏族唯一豁免 `portkill` 且系经 ADR-0006 显式升版（升版论证在 `internal/mcp/guarded.go` 文件头）。要让 AI 用本模块能力，需要机主**再次显式升版红线**，不是工程顺手事项。

---

## ③ 批次台账

### 批 1（✅ 已在工作区落地，提交未落账——本文不冒充提交号）

| 项 | 内容 | 验证（均 2026-10-22 实测） |
|---|---|---|
| 模块分家 | `internal/modules/sshkeys/`（module/service/prober/sshauth/sshprobe + 测试）自 `portscan` 迁出；`portscan` 的 `Description` 删去"SSH 连接测试"字样 | `go test ./internal/modules/sshkeys/ ./internal/modules/portscan/` 绿 |
| 前端迁址 | `frontend/src/components/sshkeys/`（SSHCheckPanel.vue、useSSHCheck.ts、spec）；bindings 再生至 `frontend/bindings/hanxi/internal/modules/sshkeys/` | **面板样式跨目录引用断链，阻塞级**——修复前前端构建/测试红，见 §⑤ 债 ① |
| 密钥台账（批 1 内新增能力） | `keyring.go`：扫 `~/.ssh`，**按内容判是不是密钥**（不按文件名猜——前缀清单列不全就会静默漏钥匙，而"漏一把钥匙"在密钥界面里是最糟的失败形态）；只回路径 / 算法 / 公钥指纹 / 注释 / 口令**三态** | `go test ./internal/modules/sshkeys`（用真生成的加密与未加密 ed25519 密钥跑，含"私钥正文不得出现在返回值"与"认不出形态时不许当成无口令"两条硬断言）；`KeyringPanel.spec.ts` |
| 显示名 | 同日两步改名：「SSH 连接测试」→「SSH 密钥与连接」（台账只读时）→**「SSH 密钥管理」**（生成、改口令、成对删除、公钥部署四件写动作都有界面入口之后）。**第二步的前提是能力落地，不是改名腾位置**（两块能力都在）。**刻意不叫「SSH 密钥管理」**：生成、改口令、部署公钥三件都没落地，名字不预先支取（同 #112 的病根，也见 `module.go` 注释）。机主若要"管理"二字，改一处 Name + regen 即可 | README/PRD 两处标签同批改；catalog 已 regen |
| 渲染校验包 | `packages/go/sshconf`：Host 段渲染 + `ValidateAlias`/字段白名单 + `selfCheck` 最后一道自证 | `go test ./packages/go/sshconf/ -v` 绿，现 107 条用例（含注入穷举）；用例数为 volatile 值，**验收口径是"全绿"而非"107"** |
| 托盘面 | `tray=31` 未变：新模块走**负裁决**（不实现 `extapi.TrayCommandsProvider`，理由见 `internal/modules/sshkeys/module.go` 头注"刻意不做的两件事"） | `internal/app/catalog_contract_test.go` 对"tray 入口 ⇔ 源码 `TrayCommands()` 证据"互为充要，缺席全链路零报错正是该测试在守 |

### 批 2（⏳）主机账本

- 落点：`<stateDir>/ssh-keys.json`。
- 存储纪律：取 frpc 的**严格型** store（`internal/modules/frpc/store.go`）——`internal/jsonstore` 原子写（tmp+rename）、`loadErr` 驻留、损坏即报错**禁止以空库覆盖落盘文件**、List/Get 返回深拷贝、每个可变方法开头 `if s.loadErr != nil` 即拒。
- 渲染一律走 `packages/go/sshconf`，不在 store 层手写配置文本。
- 本计划追加一条：`Alias` 是唯一键（`sshconf.Host` 注释口径），Add 须查重拒重——别名重复会同时破坏账本语义与 `selfCheck` 的"段头计数==项数"前提。
- 依赖：批 1 的 sshconf；风险：无新增外部面（纯本地 JSON）。

### 批 3（⏳，⚑ 形态裁决前不开工）导出投递到 ssh

- 内容：渲染产物写 `~/.ssh/hanxi.conf`，并在顶层 `~/.ssh/config` 幂等插入一行 `Include hanxi.conf`。
- **架构矛盾写明白，等机主裁决**：本仓数据纪律（`internal/settings/paths.go`）是"同级即唯一存在方式、不往用户目录静默落数据"；而 ssh 客户端**只认 `~/.ssh/config`**——这是 ssh 的硬约束，不是 hanxi 的偏好，二者正面相撞。
  - **建议形态**：用户在 UI 上**显式点"同步给 ssh"才写**、明写写了哪个路径、且**可撤销**。判据供参考：paths.go 禁止的是**静默**落盘（其 `guideSuffix` 原文"Hanxi 不会把数据目录**静默**挪进用户目录"），显式点击+路径可见+可撤销与"静默放置"不同类——但这是治理问题不是算数问题，终裁权在机主。
  - **退路形态**：只做"导出到自选路径 / 复制剪贴板"，绝不碰别人目录；此时模块语义是"导出工具"而非"ssh 配置管家"，批 5 与 UX 文案随之定，不得出现任何"自动同步"暗示。
- 落盘防线照 `internal/modules/wsl/hostconf.go` 的 `SaveWslHostConf` 四步：**语法/白名单闸门 → `.hanxi.bak` 备份 → tmp+rename 原子写 → 读回复核**。`Include` 行插拔须幂等（重复点不产生多行），撤销只删自己那一行，`~/.ssh/config` 的其他内容全程归备份步骤兜底。

### 批 4（⏳）一键部署公钥

- 凭据纪律：密码**只在本次调用内存里活着**（§②-1 四条件照旧）。
- 长动作通道：用 `EnterBackground`（样板 `internal/modules/bcu/service.go`），**别照抄 frpc `DownloadVersion` 的旧世代 fire-and-forget**——租约结束旧 goroutine 不得复活。
- 风险如实：部署公钥 = 一次成功登录 **+ 往对端写 `authorized_keys`**，副作用比"认证测试"更重（sshd 日志、fail2ban 面同 §②-3），UI 必须明写；**密钥对来源未定**（用户选已有公钥 vs hanxi 生成——后者即写 `~/.ssh/`，与批 3 的"显式才写用户目录"裁决同源，见 §⑥-C）。
- 依赖：批 2 账本（往哪台部署、用哪把钥匙都从账本来）。

### 批 5（⏳）清单 UI

- 设计说明已在 `docs/plans/PLAN_SSH_KEYS_UX.md`（本文不动那个文件；注意该文档 §0.2 取证路径仍写的是搬迁前的旧居 `components/portscan/`，UX 线须自行复核回锚）。
- 依赖：批 2 的 store RPC 面为硬前置；批 3/4 的动作入口挂在本批页面上。

---

## ④ 刻意不做（负裁决清单，后人勿当欠账顺手补）

- **不存私钥内容、不存密码**（含一切"加密后存进 hanxidata"的提案）——§②-1。
- **不注册任何 MCP 工具**——§②-3；机主显式升版红线前，此项永久关闭。
- **不挂托盘候选**——批 1 负裁决，`module.go` 注释在册。
- **不做静默/开机自动同步到 `~/.ssh`**——批 3 未裁决期间零写入；裁决后也只许显式触发。
- **不抽取 `redactSecret` 同源重复**——遵机主"重复家族保持独立可拆走"裁决，见 §⑤ 债 ②。

---

## ⑤ 已知债（照实登记，不藏）

1. **【已修并划账，2026-10-22】搬迁后的样式引用断链**：`SSHCheckPanel.vue` 迁入 `components/sshkeys/` 后，其 `<style scoped src="./portscanPanels.css">` 未随迁改路径，而该 css 唯一副本仍在 `components/portscan/`（家族共享单源，`ProxyCheckPanel.vue` 与 `PortScanView.vue` 同引一份，不复制第二份是对的）。**实测 `vitest run src/components/sshkeys/__tests__/SSHCheckPanel.spec.ts` 即红**（ENOENT `frontend\portscanPanels.css`），前端构建同理必挂。修选二：改指 `../portscan/portscanPanels.css`（临时耦合，登记在此）；或把家族共享形制上收为真共享层（更好，可与批 5 同批）。本文档为计划文档、不代改源码。
2. **`redactSecret` 两份同源实现**：`internal/modules/portscan/proxyprobe.go:294` 与 `internal/modules/sshkeys/prober.go:41` 各一份——按机主"重复家族保持独立可拆走、勿主动抽取"的裁决执行。代价如实登记：**这是安全函数（防凭据外泄进错误文本），改一处可能忘另一处**；出现第三处需求时再抽公共包。
3. **连锁计数与地图登记**：新增模块引发的各文档计数（模块数、目录/条目数、ROUTES 等）由另一会话批量在改，本文不写具体值，对账一律 `go run ./scripts/docsfacts -emit` 现推。另：`docs/DOCUMENT_MAP.md` 尚未收录本文档与 `PLAN_SSH_KEYS_UX.md`（该地图自述为快照、明示并行漂移），归地图所有权线下一轮并收，本文不代登。

---

## ⑥ 待裁汇总（⚑ 机主逐项拍板）

- **A. `ValidateAlias` 口径统一**：实现拒前导/尾随连字符，规则文字写作 `[a-z0-9_-]{1,32}`——实现比字面严。要不要把规则文字收紧成 `[a-z0-9_][a-z0-9_-]{0,31}` 与实现对齐？**本计划建议收紧**：实现与字面规则长期不一致，就是 #112 那条"理由与结论脱节"的下一个标本。
- **B. 批 3 落地形态**：显式"同步给 ssh"按钮（建议）vs 退路"只导出到自选路径/剪贴板，绝不碰别人目录"。判据与后果已在 §③ 批 3 写明。
- **C. 批 4 密钥对来源**：用户选已有公钥 vs hanxi 生成密钥对（后者即写 `~/.ssh/`，与 B 同源共裁）。
- **D.（记账不催办）** `portscan` 的 `Description` 仍留"另含代理连通性测试"尾巴——按 #112 同一信号，代理族是下一个分家候选；不在本计划范围，何时动由机主另开线。

---

## ⑦ 验收对账（逐批勾验；真机项归机主）

| 批 | 勾验项 | 状态 |
|---|---|---|
| 1 | 三包 `go test` 绿；tray 计数未被触动（负裁决 + 契约测试互证）；bindings 再生在位 | ✅（2026-10-22 实测） |
| 1 | 前端 build/test 绿 | ⚠ 现红——债 ① 修好才许入库 |
| 2 | 注入损坏样例：报错驻留 `loadErr`、原文件未被空库覆盖；List/Get 深拷贝（改返回值再取仍原样）；重复 Alias 被拒 | ☐ |
| 3 | 连点两次"同步"只有一行 `Include`（幂等）；撤销只删自己那行；写失败时 `.hanxi.bak` 在位且原文件未动；读回复核失败即中止并报错；`ssh -G <alias>` 实测解析生效 | ☐ |
| 4 | 结果/错误/落盘面 grep 无密码残留（`redactSecret` 路径有单测锁）；租约结束后旧世代无 goroutine 复活；UI 明写对端副作用 | ☐ |
| 5 | 按 `PLAN_SSH_KEYS_UX.md` 的验收剧本真机过一遍 | ☐ |

## ⑧ 前端接线后新增的三条欠账（2026-10-22，实测所得，勿当已完成）

1. **`Code` 字段在失败路径上到不了前端**。Wails 对 `(T, error)` 的绑定语义是 error 非空时 reject message、**T 被丢弃**，所以写动作被拒时前端只能拿 message 原文。已按此现实实现（不猜码），并在 `types.go` 顶部写明。真要兑现"失败也回码"，须把这些 RPC 改成 **single-result**（业务拒绝算结论不算异常，与 `CheckSSH` 的"error 通道只装没能开测"同口径）——Go 侧与四个对话框要同步改，不是加个字段就完事。
2. **孤儿 `.pub` 行是操作死角**：`keyops.DeletePair` 明确拒绝 `.pub` 路径（防"只删一半"），于是"私钥已不在、公钥还躺在台账"的行没有任何动作可点。要么给一个只删公钥的显式路径（公钥本就公开，删除风险低），要么在界面上说明"请手工处理"——**当前界面沉默**，属可感知缺陷。
3. **默认超时曾有两层缺省打架，已修但要留痕**：父包一度用 `sshTimeoutOf` 的 5s（那是给"看一眼横幅"定的）去填三段动作的部署，会压掉 `deploy` 子包自己的 15s 缺省。现在父包只在给了值时透传并封顶、没给就留 0 让子包按缺省走。**同类形状（两层各设缺省）值得当成一个通用检查项**：它不报错，只是偶尔超时。

另有两条是**限度不是缺陷**，写在这里防被当成待修：首连 TOFU 天然无法防住"第一次就遇上 MITM 且口令本身正确"（正解是带外核对一次指纹，OpenSSH 首连同样如此）；`AuthorizedKeysPath` 是**远端自报**的路径，界面上标作"远端告知的写入位置"，不是已验证事实。

## ⑨ ⑧ 里三条欠账的处置（2026-10-23 同日）

1. **`Code` 失败路径不可达** —— **未修，仍成立**。已在 `types.go` 顶部写成显式说明，且前端明确按"失败只能显示原文"实现（不假装按码分支）。根治要改 single-result，涉及 Go 与全部对话框，属独立批次。
2. **孤儿 `.pub` 是操作死角** —— **已修**。新增 `keyops.DeletePubOnly` + RPC `DeletePublicKey`：只接受 `.pub`、**有配套私钥时坚决拒**（逼这类场景走成对删除，不留半把钥匙）、回显确认与成对删除同一道门。
3. **缺配套公钥的钥匙部署不出去** —— **已修**。新增 `keyops.ExportPub` + RPC `ExportPublicKey`：从私钥算出公钥写出 `.pub`。
   - 关键实现事实（实测得出，别再凭印象改回去）：**带口令的私钥也能补**——OpenSSH 新格式只加密私钥段，`pubblob` 在外层明文里；x/crypto 的 `ParsePrivateKey` 先要口令所以"解不动"≠"拿不到"。
   - 反面教训两条，都留下了测试：① 首版把 `pem.Decode` 已解码的 `block.Bytes` 又 base64 解一遍——**明文私钥走 x/crypto 捷径碰不到这个分支，只有加密用例能暴露**；② 首版直调 `atomicWrite` 绕过了包内唯一落盘接缝 `writeNewFile`，故障注入用例当场打回。
   - 注释（comment）在加密私钥里取不到：**留空就真空着**，不拿文件名编一个——那会在别人机器的 `authorized_keys` 里留下认不出的标签。
