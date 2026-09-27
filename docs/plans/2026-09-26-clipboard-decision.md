# 2026-09-26 剪贴板内置 · 决策档案(A1 档案线)

> **文档定位**:把剪贴板历史做成 hanxi **内置模块**(六路并行之"档案线")的决策档案。本文只做裁决与剧本,**不落代码、不改契约**——接口、落盘、事件、MCP 输出形态全部冻结在 [2026-09-26-clipboard-contract.md](2026-09-26-clipboard-contract.md)(唯一事实源,本文只引用不复述;两文措辞有出入,以契约为准,契约盲区补议走⑤清单交主控)。
> **实证口径**:①表星数/许可证/活跃度为 2026-09-26 GitHub API 实扫(stargazers_count / license.spdx_id / pushed_at 三列),数字随时间漂移,引用须带日期。当场修正两处 owner 路径:任务登记口径的 `PasteBar` 实际仓库为 **PasteBar/PasteBarApp**,`mnardit/beetroot` 实际为 **mnardit/beetroot-releases**(releases 兼 issue 面)。
> 文内代码路径与先例引用以 `dev` 当前工作区为准,实施落地后有出入由实施线回填修正。

---

## ① 定位与谱系对照

定位一句话:**内置模块(LevelBuiltin,memo 同谱),纯本机剪贴板历史 + 浮层快粘 + 固定片段 + MCP 检索**——服务机主本人高频"复制了→要找→再粘"回路;不做跨设备、不做协作、不做富文本还原。

### 谱系对照表(七项目,各取什么、明确砍什么)

| 项目(2026-09-26 实测) | ★ | 栈 / 许可证 | 借鉴(→ 契约落点) | 不取 |
|---|---|---|---|---|
| hluk/CopyQ | 12.3k | C++/Qt,GPL-3.0 | **粘贴前编辑**(口径注见下)、类型图标、搜索(→§4 List q) | 脚本引擎、tabs 面板、条目多 MIME 数据 |
| sabrogden/Ditto | 7.2k | C,GPL-3.0 | **1-9 数字键快粘**(M2)、低资源常驻(→②风险 5 TTL 纪律)、持久化思路(去重/置顶/使用计数 →§2 hash/pinned/useCount) | 网络同步与集群、**以及"被托管"本身**(关键裁决见下) |
| Slackadays/Clipboard(Clipchop) | 5.9k | C++,GPL-3.0 | **置顶、"暂停记录"开关**(→§4 SetPaused+§5 paused 事件)、**500 条容量钳制**(→§2)、Win+V 观感(自绘浮层抄其观感,**非**接管系统,见风险 6) | 其系统集成面(QHotkey 等) |
| jimuzhe/tiez-clipboard | 2.9k | Tauri,GPL-3.0 | **自动分类标签**(→§3 AutoTags:url/email/phone/color/code/cjk) | ——(技术形态与我方同构:原生后端+WebView 前端,观感参考最便宜) |
| PasteBar/PasteBarApp | 2.2k | Electron,NOASSERTION(非标准 SPDX) | **Snippets 固定片段概念**(→§4 CreateText,manual=true) | 云服务、应用目录生态 |
| Ruszero01/clippi | 375 | Rust+GPUI,MIT | **轻量浮层审美**(A5 观感参照) | GPUI 自绘(我方走 WebView2) |
| mnardit/beetroot-releases | 217 | Apache-2.0(releases 仓) | OCR/AI 变换 → **列为后续线**(契约 §1 已钉"本轮不做",本文只给立项方向) | —— |

**明确不抄清单**(即契约 §1"本轮不做"七项,此处给理由):Ditto 网络同步(与②风险 1 纯本机裁决同根)、CopyQ 脚本引擎(个人自用无扩展市场)、HTML/RTF 保真(还原富文本需私有剪贴板格式多 MIME 栈,保真是无底洞,自用场景明文摘要+PNG 位图已够用)、延迟渲染(需 OLE 数据对象托管,与链监听架构不合)、正则搜索(机主场景子串够用,契约 §4 已钉非正则)、Win+V 接管(②风险 6)、OCR 内容入库(beetroot 线,后续)。

**"粘贴前编辑"口径注**:借鉴自 CopyQ,但契约 §4 冻结服务面**没有**"就地编辑条目文本"方法——实施线可用"编辑即 CreateText 新建 manual + Set"旁路达成(代价:产生孪生条目),或在浮层/主界面做编辑态、保存时落新条。是否为 §4 追加 `Edit(id, text)` 小面,**收口主控复议,六线不私扩**。

**Snippets 与「随手记」共概念、分实现**:manual=true 固定片段与 memo 便签两库互不引用、互不设跳转入口——片段回答"这段文本我还要粘",随手记回答"这事我记下了";概念重合由本档案钉死边界,防止收口期有人提"合并成一个池子"。

### 关键裁决:为什么内置,而不是托管 Ditto/CopyQ

托管家族有成熟通路(integrate-github-tool,已落地 25 款),此处**刻意不走**,四条理由:

1. **生命周期语义冲突(主因)**:托管模型的锚点是 JobObject 组启停 + `opts.Detached` 默认"不随 Hanxi 关闭"(PLAN_PIIK_HOSTING ② 家族硬约定全量适用)。而剪贴板管理器的价值在**常驻监听**——"hanxi 起它才起"意味着每次 hanxi 未运行的时段记录断链,"hanxi 退它是否该退"又撞上 Detached 默认;外部实例探测面只能感知不能管理。托管八件套对这类工具退化成纯版本目录:义务照担(资产漂移护栏、THIRD_PARTY_NOTICES、版本 Tab),管理收益归零。
2. **串联面只在进程内**:DPAPI 加密落盘(`internal/platform/windows/dpapi.go`)、全局热键槽(`internal/hotkey`,槽名谱照 memo `quickHotkeySlot`)、浮层窗组(quickmemo 同谱,`internal/modules/memo/quicksheet.go`)、MCP 零落盘直读家族(`internal/mcp/tools_memo.go` memoDiskReader 谱系)、托盘(`internal/app/app.go:767` newTrayMenuBuilder)——托管外部进程全部够不着;一个与本机所有既有基建互不相通的剪贴板=信息孤岛,违背本项目"每个模块都串联"的设计原则。
3. **UI 一致性**:工作台设计系统自研(无第三方 UI 框架),外来 Qt/wx 皮肤不可统一,机主双风格并存的观感债比功能债更疼。
4. **包装体积**:为约两千行 Go + 几百行 Vue 的实现面,往 NAS 随身目录里再塞几十 MB 的 Qt 应用,与"软件装在 NAS"的便携初衷逆向。

**许可负裁决(防后人蛇足)**:本模块**只借概念、零代码复制**(谱系里五家 GPL-3.0,复制一行即传播判定),故 **THIRD_PARTY_NOTICES 不新增条目、托管计数不动**——此负裁决写进档案;若实施线引用了上游代码片段,必须 STOP 上报总闸复议,不得静默入库。

---

## ② 风险登记表

| # | 风险 | 事实与依据 | 裁决 | 缓解落点 |
|---|---|---|---|---|
| 1 | **DPAPI 与飞牛 NAS 整目录同步互斥** | text 落盘=base64(DPAPI)(契约 §2),CryptProtectData CURRENT_USER 面绑定本机+本用户,同步到他机=永不可解的死库(假备份安全感);反向,含明文的剪贴板池若经 NAS 盘扩散=头号泄露面。而 hanxi 缘起模型恰是"整目录飞牛同步"(docs/MOTIVATION.md"NAS 同步=软件随身") | 剪贴板定性**纯本机数据**,永不进同步集,**永不做**"跨机剪贴板"概念 | `data/clipboard/` 入发布链同步排除集(契约 §2 同步红线,收口主控落 .syncignore——**实扫现仓库无此先例文件**,落点形态由主控定,此项不可漏);一切页面/文案不得出现云端同步字样 |
| 2 | **剪贴板=事实密码收集池** | 机主一切复制内容(含密码管理器复制输出)入库,且 MCP 通道把可检索明文递给 AI 客户端 | 三道闸+一条退路 | ①来源进程排除表命中=**不入库**(契约 §2 默认 1password/bitwarden/keepass/keepassxc/lastpass/veracrypt/truecrypt+子串 "password");②secret 启发式命中=入库但 sensitive=true,**MCP 整条不下发且不计入命中数**(契约 §8,memo IsMasked 严口径同谱);③视图侧 sensitive 徽章+明文不渲染按 MemoCard masked 纪律延伸(A4/A5 落点);④托盘"一键擦除"=ClearAll 含 blobs 全清 |
| 3 | **浮层抢焦点后"自动粘回原窗口"难** | 唤出浮层自身即夺前台、原窗口失焦;Win11 前台锁使自动回焦不可靠(platform 只有 SetForegroundForce 借权先例,非保证);合成 Ctrl+V 键入打错目标不可控,UIPI 下提权窗口打不进 | 首版**仅"选中即回填剪贴板"**(Set),粘贴动作交机主手按 Ctrl+V;自动键入延后 M+ 再议 | 契约 §4 Set 语义本就是"只回填";M2 剧本按"回填成功即收"设计(③);Ditto 的 QuickPaste 是多年专注焦点恢复的产物,本轮不追平 |
| 4 | **剪贴板链竞态 / OpenClipboard 被占** | `AddClipboardFormatListener` 回调时序不受我方控制;他进程持剪贴板(Office/浏览器大对象切分块期间)时 OpenClipboard 即失败;自写 Set 会再触发自家回调成环 | 失败如实 warn,不静默丢、不风暴重试;回环跳过硬闸 | Set 重试 ≤5 次(契约 §4);读取失败 logging warn 落账;GetClipboardOwner==本进程即忽略(契约 §5) |
| 5 | **WebView2 浮层常驻成本** | 每多一个活窗=多一份 WebView2 实例,内存百元级成本已被 F10 冒烟债点名(PLAN_REMAINING_WORK P1-1"关窗后 WebView2 内存回落、收起 TTL 释放") | 浮层不常驻:照抄 memo 速记卡纪律(契约 §6 已钉) | 建窗即隐+收起后 2min 空闲 TTL **真销毁**(先例 `internal/modules/memo/quicksheet.go` sheetIdleTTL,#53 纪律);M2 剧本勾验回落 |
| 6 | **Win+V 接管** | 接管需注册表停用系统剪贴板历史(HKCU ClipboardHistory)+底层键盘钩子截 Win+V,两项均属系统配置触点与无契约逆向面,企业策略还可使其整体失效 | **本轮否决**(契约 §1"不做"在册),登记 M 后续 | 复议前置条件:机主用惯浮层后仍报"肌肉记忆错乱",届时另立侦查线实测注册表与钩子可行性;不无侦查预逆向 |

**已知残留(不隐瞒)**:含密码的**截图**(image 条目)本轮无内容级识别——OCR 入库明示不做(契约 §1),默认排除表亦不含截图工具进程名,密码窗口可被机主自己截入库。该残留挂 OCR 联动线立项时补裁决(①表 beetroot 行)。

---

## ③ 机主真机验收剧本(逐项勾验)

前置:六线全落 + ⑤总闸清单跑清后的 dev 构建。⚙ = 离线单测可先行钉死、真机缺条件时降级为"单测锁+如实标注"的项。

### M1 日常动线(主界面 ClipboardView)

| 步 | 操作 | 预期观察点 | 勾 |
|---|---|---|---|
| 1 | 浏览器复制一段文本 | 主界面列表即时冒新行(clipboard:updated 事件驱动,无轮询);Preview 首行摘要 ≤120 rune;来源窗口标题与小写进程名在行 | ☐ |
| 2 | 复制图片(截图/复制图像) | 图片卡带宽高/字节;`blobs/<sha>.png` 落盘;**重复复制同一图**:不新增行、只顶置 useCount++ | ☐ |
| 3 | 资源管理器复制一个文件夹 | files 行列路径;选中回填后可在他处粘贴 | ☐ |
| 4 | 搜索框输关键词 + kind 过滤 | 小写子串命中 preview/来源/标签拼接面,非正则(契约 §4) | ☐ |
| 5 | 置顶若干行 | pinned 恒浮前段;容量压力淘汰不碰 pinned/manual ⚙ | ☐ |
| 6 | 选行回填 | 回填系统剪贴板,记事本/资源管理器 Ctrl+V 得对应内容(text/image/file 三类各验一次) | ☐ |
| 7 | 暂停记录(页开关与托盘开关任一) | 后续复制不入库;暂停态两页面可见;恢复即录 | ☐ |
| 8 | 复制假密钥串 `sk-abcdefghij0123456789` | 入库带 sensitive 徽标;M3-2 中 MCP 不可见;密码管理器内复制→库中不现该条(机主装了才做)⚙ | ☐ |
| 9 | 托盘"一键擦除" | 确认弹窗 → 列表清零、blobs 目录清空、Status 计数归零 | ☐ |

### M2 浮层动线(clipboard-overlay)

| 步 | 操作 | 预期观察点 | 勾 |
|---|---|---|---|
| 1 | Ctrl+Alt+V | 浮层现于光标屏,frameless 真透明+置顶+不进任务栏;建窗即隐、摆位后一次性露出(无半帧闪) | ☐ |
| 2 | 数字键 1-9 / Enter | 对应条目回填系统剪贴板且浮层收起;回原窗口手按 Ctrl+V 得该内容(**不自动键入**——②风险 3 裁决回归) | ☐ |
| 3 | 点其它窗口 / Esc | 失焦即收 | ☐ |
| 4 | 收起后静置 2min+ | 任务管理器观察 WebView2 内存回落(TTL 真销毁);再唤起经重建路径正常可用 | ☐ |
| 5 | 浮层唤出事件 | `clipboard:overlay:opening` 到达,前端清稿生效(A5 域,可降级单测锁)⚙ | ☐ |
| 6 | Ctrl+Alt+V 被外部占用 ⚙ | 热键槽绑定失败如实 warn 与指引(hotkey.Registry 冲突口径),不崩不静默丢键位 | ☐ |

### M3 联动动线(AI 与片段)

| 步 | 操作 | 预期观察点 | 勾 |
|---|---|---|---|
| 1 | AI 客户端调 hanxi_clipboard_search(keyword/tag/kind/limit) | 返回 id/kind/preview/text(仅 text 类明文,>64KiB 截断置 truncated)/files/autoTags/pinned/sourceApp/createdAt;image 类只给元数据 | ☐ |
| 2 | 同工具检索含 M1-8 敏感条目的关键词 | 整条不可见**且不计入命中数**;行级脱敏(IPv4→[ipv4] 等 mcp 包既有纪律)生效 | ☐ |
| 3 | AI 接入面板未授权 clipboard 键时调用 | 报指引错误(access fail-closed 既有引擎行为)——**前提:⑤清单 2 第十键已落** | ☐ |
| 4 | 页面"新建固定片段"(CreateText) | manual=true 行;容量压力不淘汰 ⚙;可搜/可钉/可回填 | ☐ |
| 5 | 抽查随手记页 | 两库互不混入(共概念分实现负裁决回归,①) | ☐ |

---

## ④ 数据契约(引用,不复述)

数据布局(index.json 整库原子写 + blobs 内容寻址)、Entry 全字段与 wire/落盘命名、DPAPI 落盘口径、去重与容量钳制、敏感判定与排除表、服务九法、三事件、浮层窗制、MCP 输出形态、A6 冻结签名——**全部以 [docs/plans/2026-09-26-clipboard-contract.md](2026-09-26-clipboard-contract.md) 为唯一事实源**(§2–§8、§10)。本文与之冲突处以契约为准;本档案发现的契约盲区(module.go 装配文件、access 第十键、"粘贴前编辑"无编辑方法)按⑤/①交主控处置,不擅改契约。

---

## ⑤ 六线分工与合入序列

分工与独占文件见契约 §9(唯一口径,不复制)。依赖与合入序:

**A6 → A2 →(A3);A4/A5 全程可并行;A1(本档案+N45 行)纯文档零冲突面,任意时点可入。**

- A2 编译依赖 A6 冻结签名(契约 §10):并行开发可按签名先写,**合入须 A6 先落**,否则 clipboard 包不完整;
- A3 零落盘直读不复用 ClipboardService,但解析需 clipboard 包 Entry 类型——`go vet ./internal/mcp/` 在 A2 前失败系预期中间态(契约 §11 已预告,标注即可);
- A4/A5 只依赖前端 kernel(契约 §7,`frontend/src/types/clipboard.ts` 与 `frontend/src/adapters/clipboard.ts` 已在工作区在途),收口时真实 transport 桥接入,前端零改动。

**总闸收口清单**(并行六线一律不碰,主控一次执行;编号即建议顺序):

1. **app.go 服务注册与模块装配**(ClipboardService 构造/生命周期照 memo.MemoService 同谱)。**缺口已裁(契约 §12.1,v1.1)**:`internal/modules/clipboard/module.go` 由 **A2 补**(`const ID = "clipboard"`,照 memo 键范式声明导航/授权门所需标识);**catalog 注册(含 cataloggen 重跑与 composition fixture 契约文件)、mcpwizard 授权键、navigation.ts 入口仍归总闸主控**(piik D 线先例格式),A2 不得触碰共享文件。
2. **internal/mcp/server.go 一行接 registerClipboardTools**(契约 §8)。**配套缺口**:access.json 第十键 `clipboard` + 「AI 接入」面板开关(mcpwizard 面)+ 对拍矩阵扩列——server.go 注册按 ModuleID 走授权门(memo 键"授权粒度=模块"先例),**无键注册即死**,契约未列此面,主控补齐不算画蛇添足。
3. **navigation.ts ROUTES + MODULE_PRESENTATION 两表** + **main.ts `/#clipboardoverlay` 路由挂载**(A5 视图);navigation.spec.ts 路由计数锁与 contract-enum 模块计数锁按**实扫**改数(装配前重扫,别信快照——piik 档案纪律)。
4. **图标**:i: 矢量池实扫 71 键**无 clipboard 候选**(archive/layers 形近易混),主控在 `frontend/src/constants/icons.ts` 补一枚 24×24 stroke 矢量(constants/** 属并行禁区,只能总闸落)。
5. **托盘菜单三项**:唤出浮层 / 暂停记录(状态镜像两页联动)/ 一键擦除(必须确认弹窗——ClearAll 是全模块唯一不可逆全灭面);落点 newTrayMenuBuilder 族(internal/app/app.go、elevate_tray.go 选项面)。
6. **发布链 .syncignore 追加 `data/clipboard`**(②风险 1 落点;现仓库无此先例文件,契约 §2 所称 "scripts/release 的 .syncignore" 实扫不存在,落点形态主控定)。
7. **bindings 再生**:`task common:generate:bindings`(禁裸 wails3,会清空输出目录——piik 总闸先例措辞);与在途欠账(wheel/webapp/piik/portkill)同闸一跑,clipboard 面并五批。
8. **全量闸门**:gofmt -l 净 + go build/test 全仓 + 前端 build + vitest 全量 + vue-tsc + ESLint 0 error。
9. **TROUBLESHOOTING 沉淀**(四段式);三个可预见踩坑候选先登记:DPAPI×同步口径(②1)、浮层前台/借权与失焦即收竞态(②3)、CF_DIB 长尾形态——A6 口径只承诺 BI_RGB/BI_BITFIELDS,遇 BI_PNG/BI_JPEG 压缩段必须 error 如实落 warn,**不得伪造灰图凑数**。
10. **THIRD_PARTY_NOTICES 不动**(①许可负裁决:零代码复制无新增义务;若实施线有引用代码片段,回到总闸复议,不在本清单内解决)。

---

## 遗留与如实声明(不隐瞒项)

- ①表借鉴全部是**形态级**(交互概念/数据模型方向),未逐行读过上游源码——"借鉴"的信心基于 README/截图级理解;若实施线发现某概念的实际机制与设想不符(典型:CopyQ 的编辑含条目 MIME 全套,我方降级为纯文本重做),按实况回改①表措辞,不硬凑。
- PasteBar 许可证 NOASSERTION(非标准 SPDX):零复制口径下无义务,但未来 Snippets 若强化到需参考其行为细节,先复核其 license 再动。
- ⑤清单 1、2 两缺口与"粘贴前编辑"无方法一项,是本档案对契约的**补议**而非改判:契约为唯一事实源,修订权在主控;若主控不采,撤回对应勾验步骤即可,实施线不得援引本文私扩。
- M2-4 TTL 回落观察与 F10 webapp 冒烟(P1-1)同病灶同形态,建议并入同一真机验收场,别各开一轮往返。
