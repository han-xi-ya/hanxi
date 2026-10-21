# ADR-0002:共享托管内核 API 稳定门与逃生钩子审查规则(Wave 4)

- 状态:已接受(2026-10-21 状态核查确认仍生效,未被取代;内核面后续扩展见 §6)
- 日期:2026-09-18
- 关联:`PLAN_WORKBENCH_MODULE_ROADMAP.md` §9、`PLAN_OFFICIAL_MODULE_DISTRIBUTION.md` §4.2/§10、`WAVE5_MIGRATION_INVENTORY.md`、`ADR-0001`(Wave 0 契约门,本 ADR 的前置波次)、`ADR-0003`(其 §4 重申本 ADR §3 阈值,并把 §4 校验强度口径收编为供应链实况承担者)

## 1. 内核边界(冻结面)

| 包 | 冻结范围 | 证据样本 |
|---|---|---|
| `packages/go/artifact` | Fetch/UnpackZip/Tree 全公开面、DefaultLimits、版本令牌白名单 | markeron(portable zip)+ rufus(单 exe) |
| `packages/go/supervisor` | Probe/Spec(含 HideWindow/Env 受控字段)/Engine 方法面/ErrExternal/ErrBusy/Callbacks.OnLog | 同上双样本;控制台族预扩展已备料 |
| `packages/go/operation` | Journal 15 字段 schema v1、Store 五操作、Recover 背书法、Hub 观察面 | 双样本安装事务 |

升版规则:破坏性变更(字段语义/删除/状态机迁移)必须 schema 递增 + 本目录新 ADR + 更新双样本回归;新增可选字段属于兼容演进。

## 2. 稳定门判定(何时算"冻结")

1. 两个**差异样本**(zip 族 + 单 exe 族)全程走内核且无本地主流程副本残留(grep 基线:`downloadTo|extractAll|verifySHA256` 在被迁模块中清零);
2. 失败注入矩阵通过:断网/摘要错/超预算/staging 残留/强杀中途/文件锁在用拒卸/旧版完好;
3. Wave 5 前三个 S 批族(snipaste/everything/ccswitch)迁移中若出现内核改动需求,按 §3 流程走,不得私改后补文档。

## 3. 逃生钩子审查规则(防"manifest 脚本化"在 Go 层的镜像变体)

允许(白名单式):
- **枚举型受控字段**:如 Spec.HideWindow/Env(键值白名单语义)、Limits 数值预算;
- **接口注入**:Probe、Compensation、QuitHook、OnLog(回调只搬运不加工,脱敏/缓冲留模块);
- **模块层薄适配**:锚点校验、镜像 URL 表、状态/stage 词表映射、领域文案(elevateHint 等)。

禁止(出现即停止该模块迁移并上报架构评审):
- 内核接受任意 `[]string` 命令行拼接或 shell 字符串(Args 仅受控 argv,禁 `cmd /c` 包装);
- 内核执行来自模块数据的代码/脚本/模板;
- 为单一模块在共享包加布尔开关堆叠(≥3 家需求才入内核,单家需求留模块适配);
- `rustdesk/subnetdesk`(树监督)、`frpc`(多实例+嗅探)强行套壳——盘点账已裁定 bespoke,豁免即终点,不得反向扩内核迁它们;
- journal 记录 Secret/用户文件内容/敏感完整命令行(类型面封闭已挡,新增字段须复审)。

## 4. 已知遗留(登记不隐瞒)

- artifact `Fetch` 的 `.part-*` 下载残件不在 `AbandonedDirs` 目录盘点内——**已解决(2026-09-19)**:内核新增 `CleanStaleParts(dirs, olderThan)` 受控清理入口(只删超龄 `.part-<hex>` 形状普通文件,拒目录/链接),装配根启动恢复阶段对 `installers/`、`versions/` 根各扫一轮(`internal/ops.CleanStaleDownloadParts` 接线,残件年龄阈值 24h)。
- `Tree.CleanupAbandoned` 未接生产(共享根误伤风险),恢复走 journal 背书法;无背书孤儿仅报告。
- supervisor 对 markeron 的 `stopping→running` 词表映射属兼容妥协,Wave 5 收口 instance 词表时重审。
- 校验强度升级:资产安装要求官方 SHA-256(GitHub `asset.digest`),无摘要资产拒装;离线导入通道不受影响。

## 5. S 批迁移中确认的内核边界(2026-09-19,未达扩展阈值,走薄适配器)

- **弱摘要上游(SHA-1/MD5,非 SHA-256)**:snipaste 官网清单仅 SHA-1、guoheview 官方仅 MD5。`artifact.Fetch` 以 SHA-256 为唯一信任根,不放宽(放宽=削弱分发专项 §10.2"digest 仅证完整性、镜像不作信任根"的安全裁定,且 Authenticode/发布者验证是 Wave 5 签名层的职责)。**裁定**:这两家"下载+官方哈希校验"段留模块 bespoke,解包仍委托 `artifact.UnpackZip`(闸门更全)。家数=2,未达 §3 的 ≥3 阈值,暂不加弱摘要 Fetch 变体;若第三个弱摘要上游出现再议"显式声明算法的完整性校验(标注 weak)"窄接口。
- **MSI 提取**:piclite/keyviz 用 `msiexec /a` 管理提取(GitHub 资产有 SHA-256,下载校验段可委托 Fetch,提取搬运留模块)。**裁定**:MSI 提取策略不进 artifact(策略族是 Wave 5 签名 manifest 批次的事,现在造=预支且无签名背书)。keyviz agent 被要求审 msiexec `TARGETDIR=` 的路径注入防护。
- **版本目录命名变体**:核实 artifact.Tree `dirName=EntryName_"_"+version` **能**表达 `<entry>_v<version>`(markeron 传 `v2.10.1`、snipaste 传 `v13.0` 同构)——snipaste agent 报告"多一枚下划线表达不了"是误判(其 bespoke 结论因 Meta 字段/SHA-1 两条真理由仍成立,不受影响)。留 bespoke 的**有效**内核边界是:各模块 VersionInfo 的前端契约字段(verificationMode/officialHash/hashAlgorithm/isImport 等)是模块业务态,`artifact.Meta` 依"共享包不抽模块业务"纪律不收,故需要这些字段的模块自持 meta 落位。
- **Quit 分层归因**:snipaste 确认 `sup.Stop(grace)` 对"宽限内自然退出"与"强制终止成功"都返回 nil,四态归因在内核返回值坍缩——正解是归因决策留模块 instance 层(verifyToken+settle 通道观察),内核只承担末级强杀。此为通用范式:凡 Quit 需多态归因的(snipaste 类)照此,不为塞进内核而造第二返回值(假抽象)。

## 6. 后续实况(2026-10-21 增补,只登记事实,不改历史决议正文)

- **§1 冻结面表已非共享内核全貌——出现第四个公共包 `packages/go/netx`**:`572c0a0`(2026-09-25)将代理链公共件下沉为 netx(回环闸、代理感知/直连客户端:`client.go` `Transport/NewClient/DirectClient/ProxyFunc/LoopbackAwareProxyFunc`);`469e3e1`(2026-10-20)再将显式代理拨号内核自 portscan 上收(`proxydial.go` `ParseProxy/DialProxy/QueryEgress` + 出口源常量,commit message 自述"行为零变化")。netx 现消费面 39 个模块(`grep -rl hanxi/packages/go/netx internal/modules`,主要吃传输面)。**netx 未按本 ADR §1–§3 口径另立冻结面表/升版规则/逃生钩子审查**,其治理缺口在此登记(存疑项)。
- **§3 "≥3 家需求才入内核"出现反例案例**:netx 传输面自"孤例泛型化"起步、扩至多家消费,不与阈值冲突;但 `469e3e1` 上收 `DialProxy/QueryEgress` 时唯一消费面是 portscan 模块自身(portscan/scanner.go 与 proxyprobe 两面,现仍仅此一家),与 §3"单家需求留模块适配"及 ADR-0003 §4 重申的执行口径存在张力,且该上收未走"上报架构评审"留账。是否追认(上收搬运 ≠ 开关堆叠)待裁。
- **§4 已知遗留与内核符号抽查全部有效**:`artifact.CleanStaleParts`(`packages/go/artifact/fetch.go:333`)与装配接线 `ops.CleanStaleDownloadParts`(`internal/ops/residue.go:94`,启动恢复调用于 `internal/app/app.go:452`)在案;三包冻结符号 Fetch/UnpackZip/Tree/Meta/DefaultLimits(`fetch.go:73`、`unpack.go:75`、`tree.go:45,117`)、QuitHook/ErrExternal/ErrBusy(`packages/go/supervisor/engine.go:93,122,208`)、Journal/Store/Recover/Hub(`packages/go/operation/journal.go:148`、`store.go:65`、`recover.go:60`、`hub.go:39`)均仍在原位置。
- **§2.3/§5 S 批与薄适配器裁决落地核对**:roadmap 2026-09-19 进度行确认托管控制台 20/20 完成迁移、Everything 按机主裁决保持现形态;snipaste/guoheview 弱摘要留 bespoke、piclite/keyviz MSI 提取不进 artifact 的 §5 裁定未被后续提交推翻。quicklook/bili23 的 instance bespoke 由 ADR-0003 §4 定案,frpc/rustdesk/subnetdesk 豁免维持(模块均在 `internal/modules/` 原位于册)。
- **§4 校验强度升级口径已被 ADR-0003 §1.1 收编**:"官方 SHA-256 必检、无摘要拒装"自此同时是供应链完整性的实际承担者(HTTPS + 上游摘要),两文一致,非取代关系。
