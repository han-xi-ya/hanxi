# Wave 5 托管模块迁移分类账(supervisor 接线决策输入)

> 生成日期:2026-09-18,基于 Wave 4 共享内核(artifact/supervisor/operation)落地后的只读盘点。
> 口径基准:各模块 `internal/modules/<pkg>/{version,instance}/` 实测 + `packages/go/supervisor/` 包注释「与蓝本的刻意差异」(进程树监督、日志环形缓冲/脱敏、连接状态嗅探三类明确不进内核)。
> 本文档是规划输入,不是已实现事实;每族迁移完成前以契约测试与黄金样本为准。

## 台账(26 项托管形态模块)

HideWindow/Env/OnLog 列为 instance 引擎侧事实;难度按迁移到 supervisor+artifact 的当量。

| 模块 | 资产形态 | 探针族 | Hide? | Env? | OnLog? | 特例 | 版本目录锚点 | 难度 |
|---|---|---|---|---|---|---|---|---|
| markeron | portable zip | 互斥体 | 否 | 否 | 否 | ✅已迁(Wave4 黄金);Toggle 信使留模块层 | MarkerOn.exe + markeron.portable | (基准) |
| rufus | 单 exe | 互斥体/窗口 | 否 | 否 | 否 | ✅已迁(Wave4 差异样本) | rufus.exe + rufus.ini | (基准) |
| snipaste ✓ | 官网 zip(顶层目录剥离) | 无外部探针 | 否 | 否 | 否 | ✅已迁薄适配:下载校验 bespoke(SHA-1),归因留模块 | Snipaste.exe | S |
| ccswitch | zip+绿标 | 互斥体+消息窗 | 否 | 否 | 否 | ✅已迁;唤窗信使二次拉起(不入 Job) | cc-switch.exe + portable.ini | S |
| everything | 便携 zip(官方 sha256 清单) | 窗口类双通道 | 否(引擎) | 否 | 否 | ✅已迁;PreStart 迁 service;search/ 旁路未动 | Everything.exe(大小写宽容) | S |
| flclash | zip | 进程快照+EnumWindows | 否 | 否 | 否 | ✅已迁;唤窗 Win32 直操作留模块 | FlClash.exe | S |
| mangodisk | 单便携 exe | Tauri 互斥体+信号窗 | 否 | 否 | 否 | 就绪双条件可单 Inspect 表达 | MangoDisk exe | S |
| papertodo | 单 exe 双变体 | 裸互斥体 | 否 | 否 | 否 | Quit=信使携 exit 参数(官方通道) | PaperTodo.exe | S |
| piclite | MSI /a 提取 | 互斥体+PID 可见窗 | 否 | 否 | 否 | 无优雅退出:QuitHook 返错→引擎强杀,天然兼容 | piclite.exe | S |
| keyviz | MSI /a 提取 | 仅互斥体 | 否 | 否 | 否 | 无唤窗契约;Quit 直接强杀 | keyviz.exe | S |
| translucenttb | 便携 zip(dll 伴生锚点) | 裸 GUID 互斥体 | 否 | 否 | 否 | ResetState 独立操作;禁空闲退出 | TranslucentTB.exe + companions | S |
| paseo | 便携 zip(electron) | 进程名+EnumWindows | 否 | 否 | 否 | 唤窗优先 Win32;长宽限 | Paseo.exe + app.asar | S |
| bcu | 便携 zip 双层(bootstrapper+真身) | 互斥体+EnumWindows | 否 | 否 | 否 | Start 必须直指内层(外层秒退误判);740 提权特判 | 外 BCUninstaller + win-x64/内层 | M |
| recordly | NSIS 静默,单固定目录 | 进程名+EnumWindows | 否 | 是(RECORDLY_DISABLE_AUTO_UPDATES=1) | 否 | 版本面偏离 zip 模板(单目录非隔离) | Recordly.exe | M |
| quicklook | 便携 zip(longpath 特例) | 互斥体 | 否 | 否 | 否 | 热重启管道 Quit+Reload 双令——**缺命令通道泛化** | QuickLook.exe + portable.lock | M |
| litemonitor | 便携 zip(语言包锚点) | 进程枚举+按 PID 窗 | 否 | 否 | 否 | PreStart 播种;740 特判;Updater 子进程靠 Job | LiteMonitor.exe + zh.json | M |
| guoheview | 便携 zip(包装收割;**官方仅 MD5**) | 进程名(多实例)+按自有 PID | 否 | 否 | 否 | 【已迁 2026-09-19 薄适配】计数留模块(RunningBesides 排除自有 PID),未扩内核 | GuoheView.exe + portable.ini | M |
| ddnsgo | 官方 zip 单 exe | 进程名+TCP 端口就绪 | 是 | 是(DDNS_GO_DAEMON=1) | 是 | 端口预检/iframe 控制台;内核 Env/ReadyTimeout/OnLog 已备料 | ddns-go.exe | M |
| vscode | 双形态(便携 zip+Inno 注册表) | 分治:互斥体/镜像前缀枚举 | 否 | 否 | 否 | 一模块两台 Engine(双形态并行,可组合表达) | Code.exe + bin/ + product.json | M |
| ocr | 私有 zip 导入 only(manifest+旁挂 sha256) | 端口 TCP+HTTP 契约 | 是 | 否 | 是(ringbuf) | 资产侧私有契约与双探针留薄适配,只换进程治理段 | hanxi-ocr.exe(manifest.entry) | M(仅引擎段) |
| bili23 | 便携 zip(PE 版本不可信) | 互斥体 GUID+QLocalServer 信使 | 否 | 否 | 否 | 退出三态如实上报+**无强杀兜底**——与 Stop 固定 killSequence 冲突 | Bili23.exe + main.py | L |
| rustdesk | 双形态(packer+MSI 交互装) | 镜像前缀父 PID 闭包 | 否 | 否 | 否 | **树锚点特例**:外层秒退击穿"自有句柄=生命周期"公理 | rustdesk.exe + LOCALAPPDATA | L(不迁) |
| subnetdesk | 同 rustdesk | 同 rustdesk | 否 | 否 | 否 | 同 rustdesk | subnetdesk.exe + LOCALAPPDATA | L(不迁) |
| frpc | 官方 zip/本地导入 | 无外部探针 | 是 | 否 | 是(环形+DPAPI 脱敏) | 多实例 map+临时 TOML 擦除+连接嗅探,差异>共性 | frp_vX/frpc.exe | L(不迁) |
| douzy | NSIS Setup(下载+拉起向导) | N/A | — | — | — | 不做进程托管(版本+下载边界) | Douzy-Setup-*.exe | N/A |
| nanazip | MSIXBundle 验签拆包 | N/A | — | — | — | 无 instance 面;bundle 白名单契约 bespoke | MSIXBundle | N/A |
| fileshare / webapp | 内建 server / 外部 URL 窗 | N/A | — | — | — | 无托管面 | — | N/A |

## 三名单结论

- **① 直接套模板(S 批 10 个)**:snipaste、ccswitch、everything、flclash、mangodisk、papertodo、piclite、keyviz、translucenttb、paseo(+vscode 双 Engine、ddnsgo 既有字段全够用,工作量半档)。
- **② 需先扩内核再迁**:quicklook(Reload 第二命令钩子或裁定留模块直投)、bili23(Stop 三态+NoForceKill 让渡)、frpc(OnLog 键控+map 上提——若迁);guoheview 已按薄适配器落地(计数无需 InspectAll)。扩展遵循"枚举型受控字段"纪律,禁任意脚本钩子。
- **③ 明示留 bespoke(不迁,防假抽象第二样本反例)**:frpc、rustdesk、subnetdesk(树监督形态)、douzy、nanazip(版本面各自成族,待声明式批次)、ocr 资产侧、fileshare/webapp(无托管面)。

## 迁移批次建议(Wave 5 执行序)

1. S 批按族分 3 小批(每批 3-4 模块,一个黄金族样本先行);
2. ②名单先补内核扩展(带 supervisor 测试矩阵),再随批迁;
3. 每模块迁移必须过:安装/更新/回滚/启停/在用拒卸/断网摘要错/强杀恢复契约测试 + `TestRPCGateCoverage` 保持绿;
4. 迁移后删除该模块 version/instance 内被替代的复制实现,重复代码基线按"迁移清单"核对(ROADMAP §15:每策略族先黄金样本,再并行)。

## 已知跨包边角(接线时处理)

- artifact `Fetch` 的 `<file>.part-*` 下载残件不在 operation `AbandonedDirs`(只认目录三前缀);启动恢复接线时对 `installers/`、`versions/` 根补 `.part-*` 文件清理。
- operation journal 由托管事务生成方(模块 manager 委托层)持 `DirInstallingPrefix` 等常量拼目录,与 artifact Tree 的 `.tmp-/.removing-` 前缀已核对一致。

## 迁移执行状态(2026-09-19 收口时点,真相对账)

- **✅ 已迁内核(19)**:markeron、rufus(Wave 4 双样本)+ ccswitch、snipaste†、ddnsgo、papertodo†、translucenttb、keyviz、flclash、everything、paseo、mangodisk、bcu、litemonitor、piclite、vscode、guoheview、recordly‡(†薄适配器:下载/落位段留 bespoke,理由见 ADR-0002 §5;‡NSIS 覆盖式单目录,无 Tree 可登记)。全部 journal 事务化 + `.tmp-<txnID>` 背书法启动恢复登记(app.go versionTrees,recordly/piclite 无树例外),真机冒烟确定性化(-count=5 零 flake)。
- **⬜ ②名单未迁(待内核扩展裁定)**:quicklook(Reload 第二命令钩子)、bili23(Stop 三态+NoForceKill)。
- **⬜ ③明示 bespoke 未动**:frpc、rustdesk、subnetdesk、douzy、nanazip、ocr 资产侧(其 instance 测试在本会话为既有环境红,非回归)。
- **登记节奏**:S 批后新增两笔待裁——"安装器执行族"已 3 家(keyviz/piclite MSI + recordly NSIS),达 ADR-0002 §3 阈值,是否入 artifact 策略族(受控枚举 `installPolicy`)进 Wave 5 签名批次一并裁;信任根缺位第 3 家(vscode 历史版无 digest)同理复议弱摘要窄接口。
- **重复代码基线**:25 份 version 下载链、24 份 instance 进程治理主流程已收敛至 1+19(19 委托,余 5=②③ 名单);downloader.go 样板平均 −110 行/模块。
