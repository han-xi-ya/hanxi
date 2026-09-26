# PLAN_PIIK：piik 托管集成决策档案与验收剧本

> **文档定位**：piik（上游 TNTcraftHIM/Piik）纳入 hanxi 托管家族的**决策档案**（五路并行线之"档案线"产出）。
> 本文只做裁决与剧本，**不落任何代码、不改 PRD、不改 THIRD_PARTY_NOTICES**——那三处涉及多线共享面，全部留**总闸批次**统一处理，防并行撞面。
> 实施线（A/B/C/D）以本文为唯一裁决依据；文内代码路径与先例引用以 `dev` 当前工作区为准，实施落地后有出入以实施线回填修正为准。
> 侦查方法与必查清单口径见个人 skill `integrate-github-tool` 阶段 0；本文①即其逐项实证结论摘要。

---

## ① 阶段 0 侦查结论摘要（实证，非猜测）

上游：**https://github.com/TNTcraftHIM/Piik**，许可证 **MIT**。

| # | 侦查项 | 结论 | 对集成的直接含义 |
|---|---|---|---|
| 1 | 发行形态 | **headless 服务器进程 + 系统浏览器 UI**：无自有窗口、无托盘、无 WebView2 依赖 | 窗口托管模板（互斥体唤窗 / EnumWindows 唤窗 / 信使二次拉起）**整族不适用**；见② |
| 2 | 发布节奏 | **22 个版本 / 14 天**，日更级风暴；上游项目当前"年龄"约 5 周 | 自动跟版必然被刷屏 → **钉版本 + 手动追**（④风险 1） |
| 3 | 便携资产 | **裸 zip** 资产名 **19 连版稳定**（命名无架构/形态变体，无多形态分叉） | 后缀排除式 `findPortableAsset` 需按**实际资产名**匹配，勿照抄 ccswitch 的 `*_x64_portable.zip` 直觉；A 线 remote.go 落点 |
| 4 | 官方哈希 | GitHub API **digest 覆盖率 100%** | 四层完整性第一层成立，走 **ccswitch 完整版**（官方 sha256 唯一信任根），无需 markeron 三层降级 |
| 5 | sidecar 资产 | **已停发**（历史版本曾挂，近 19 连版起不再发布） | **严禁依赖 sidecar**：解包与运行链路上任何"找 sidecar 文件"的假设都会在未来某版炸掉；布局自检不变式不得引用它 |
| 6 | zip 内部布局 | **根平铺**，runtime 是 exe 的**兄弟路径**，整体不可拆 | 保布局解压；ResolveExe 直指根内主 exe（bcu 的 innerExeRel 内层指向**不需要**，但"根目录 exe ≠ 真身"的排查结论要写进注释防后人复查）；A 线 extractAll 布局自检按**条目不变式**（存在主 exe + runtime 兄弟项）而非写死路径清单 |
| 7 | 端口 | **8787**，单实例互斥**由端口绑定事实达成**（无命名互斥体、无 second-instance 协议） | 探测面 = 端口监听 + 进程树（见②）；Start 失败若因端口被占，错误文案**必须点名 8787**并提示走 hanxi portkill/portscan 处置；"事实互斥"的另一面：外部已有人占了 8787 时托管启动会直败，不静默换端口 |
| 8 | 机读信号 | 浏览器自动拉起失败时 stdout 打 **`GATE_NO_BROWSER`** 标记 | B 线 wait() 前的 stdout 读取侧解析该标记；C 线在 instance-state/metaHints 面披露，控制台窗给**手动打开 `http://localhost:8787` 链接**兜底——别让用户面对"启动了但什么都没发生" |
| 9 | 优雅停止 | 进程监听 **stdin**：stdin 关闭/收到输入即**优雅退出**（官方设计） | Quit 首选通道：向 stdin 写/关 stdin → 宽限 → JobObject `TerminateJobObject` 兜底。这是"服务型无窗口"工具罕见的**官方优雅退出通道**，ccswitch 的 WM_CLOSE 信使在这里既不需要也不存在 |
| 10 | 提权清单 | manifest **asInvoker** | **无** TROUBLESHOOTING #17 三重契约（740/elevateHint/ElevateRestart 全部不接线），前端 stopped 引导行也不预告提权 |
| 11 | 前端依赖 | **无 WebView2 依赖**（UI 在用户系统浏览器里） | ccswitch 模板 wait() failed 分支"错误文案指向 WebView2/依赖"的固定措辞**照抄即错**，B 线改为如实指向"上游进程异常退出/端口占用" |
| 12 | 国内通道 | **官方 Gitee 镜像在案**（上游自己维护） | `assetMirrors` 回退链（ccswitch 模板四镜像纪律）把 **Gitee 官方镜像排第一**——官方源非第三方搬运，信任级别等同 GitHub，且直连 github.com 会被 reset 的实证教训（TROUBLESHOOTING bili23 条目）说明镜像链本身必须有可信首链 |
| 13 | 许可证义务 | MIT；且**包内自带 THIRD-PARTY-NOTICES** | hanxi 告知条目一句话 MIT 义务 + **包内自带文件随版保留**（卸载即随版本目录消失，不产生额外处置义务）；正文见⑥，实际写入留总闸 |

**孙进程谱系**（④风险 5、⑤剧本步骤 6 的事实基础）：piik 主进程运行期会带起 **cloudflared**（公网邀请隧道）与 **piik-capture**（捕获子工），均为其 spawn 的孙进程——这正是不用 JobObject 就会在强杀/崩溃时漏尸的形态（rustdesk #24 家族的教训形状不同、风险同类）。

---

## ② 模板族判定：服务型骨架（headless service host）

**判定结论：piik 是托管家族第一个"服务型骨架"样本——不套任何窗口托管先例，Engine 生命周期锚定单个受 JobObject 隔离的常驻服务进程，交互面全部外置到系统浏览器。**

骨架取舍一览：

- **保留**（照抄 ccswitch 硬约定）：JobObject 绑定、`opts.Detached` 默认（"不随 Hanxi 关闭"统一语义）、`cmd.Dir = filepath.Dir(exe)`、wait() 四分类顺序（stopping → 外部接管 → exit0 → failed）、宽限期包级变量、四层完整性、版本八件套 RPC 面。
- **替换**：
  - 唤窗 → **无窗可唤**。`OpenWindow` 语义重定义为"用系统浏览器打开控制台"（复用 `windows.OpenURL`，webapp/内嵌模块同款通道）；前端按钮文案"在浏览器中打开"。
  - Quit 信使（WM_CLOSE/PostMessage） → **stdin 优雅停**（侦查项 9）+ 宽限 + `TerminateJobObject` 兜底（连带回收 cloudflared/piik-capture 孙进程）。
  - 探测通道（互斥体/窗口枚举） → **端口监听探测**（8787）+ 进程镜像路径前缀匹配托管目录（vscode 便携版先例的探测形制）双通道；外部感知 = 用户自己起的 piik（占住 8787）能被认出，且不与之抢停。
- **禁用**：空闲自动退出（followOnExit 开关保留，但**idleCheck 不接线**——理由同 rustdesk/subnetdesk 包注释裁决："服务在跑就有会话价值，闲置 ≠ 可杀"；公网邀请链接随时可能有人正连着，空闲杀服务是无告知的断服）。
- **无安装器通道**：裸 zip 解压即用，无 NSIS/Inno/MSI 确认闸（vscode confirm 先例不引入）。

**为什么这是"骨架"而不是"变体"**：现有家族里最接近的是内嵌控制台窗的 ddnsgo 与 frpc（都是"hanxi 自绘控制台 + 上游进程"），但两者都还有 hanxi 侧的窗口/事件面；piik 连产品 UI 都在上游自己手里（系统浏览器里的 8787 页面），hanxi 只剩"版本 + 起停 + 状态灯 + 打开浏览器"四件事——这正是服务型骨架的全部职责边界。后来者（下一个 headless 服务器类工具）应直接对照 **piik** 而非窗口家族。

---

## ③ 谱系对照表（与三条窗口托管先例逐维对照）

| 维度 | ccswitch（主模板） | rustdesk（反模板） | DBX（服务注入先例） | **piik（本裁决）** |
|---|---|---|---|---|
| 资产形态 | 便携 zip，多形态后缀 | rust-portable 自解压单 exe | 便携 zip（保布局 + portable 标记） | 裸 zip 单形态，名 19 连版稳定 |
| 官方 digest | 有 | 无单文件哈希链 | 有（minisign 不消费，宁拒不猜） | **有，100% 覆盖** |
| 生命周期锚点 | 主进程，外层=真身 | 外层秒退，锚点=提取目录父 PID 闭包（#24） | 主进程 | 主进程；**孙进程 cloudflared/piik-capture 归 Job 闭包** |
| 单实例机制 | tauri 命名互斥体 + 信使 | 无契约（反例） | tauri 互斥体（同 ccswitch 族） | **无互斥体；8787 端口绑定事实互斥** |
| 唤窗通道 | 无参二次拉起（信使 handoff） | 无（进程名探测+强杀） | 信使 handoff | **无窗：OpenWindow = 系统浏览器开 8787** |
| 优雅退出 | WM_CLOSE + 宽限 + 强杀兜底 | **无优雅通道**（指纹复核后直杀） | WM_CLOSE 族 | **stdin 优雅停** + 宽限 + Job 兜底（官方通道） |
| 探测通道 | OpenMutex | 路径闭包 | 互斥体 + 注入账目 | 端口 8787 + 镜像路径前缀 |
| 数据落盘 | `~/.cc-switch` 固定用户目录 | 自身配置目录 | **DBX_DATA_DIR 改道 Hanxi 数据根**（删版本不丢数据） | 上游自管落盘（侦查未见改道接口）→ **不强改造道**；"数据目录直达"家族标配照给（#28 批量补目录先例）；末版卸载沿用 **dbx 末版可卸口径**（`internal/modules/dbx/service.go:333`：允许卸到零版本 + 指引下载），文案如实提示数据目录去留（⑤步骤 7） |
| 提权 | asInvoker | asInvoker | asInvoker | asInvoker（**无 #17 三重契约**） |
| 空闲退出 | 有 idleCheck | **禁用**（服务价值论证） | 有 | **禁用**（沿用 rustdesk 裁决理由，见②） |
| 前端形态 | 双 Tab（控制台+版本管理） | 同款家族壳 | 家族壳 + DataDir 披露行 | 家族壳；控制台无"唤窗"按钮语义，改"在浏览器中打开"+ 8787/LAN 链接展示 |

**注入先例的取舍**：DBX 证明的是"上游给了改道开关时，注入要进受控不变式 + 幂等 + GetStatus 有账（metaHints 披露依据）"（`internal/modules/dbx/service.go:447 ensureDataDir`、`models.go` 的 `dataDir/dataDirInjected`）。piik 的官方注入位是 **CLI 参数**（`--config`/`--log-dir`，侦查第 6 条在册）而**非环境变量**——落盘实现据此起/停注入走 CLI 正门（DBX 受控不变式同款：幂等+GetStatus 有账+metaHints 披露）。
**负裁决修正版（2026-09-26 C 线上线对质后定稿，原稿"没有对等改道开关故不注入"系档案误判，以实现为准）**：禁止的是"无中生有造 env/伪造上游不支持的开关"；官方给了 CLI 位就用 CLI 位，防止后人"照 DBX 补一个 PIIK_DATA_DIR 环境变量"式的画蛇添足。

---

## ④ 风险登记表

| # | 风险 | 事实 | 裁决 | 缓解落点 |
|---|---|---|---|---|
| 1 | **日更风暴** | 22 版 / 14 天，日更级 | **钉版本手动追**：远程表默认只呈现已钉版与相邻更新项，不自动弹红点升级轰炸；追版由机主在版本管理 Tab 手动点下载 | C 线 service：followLatest 语义不接（或默认关）；前端徽章如实显示"当前非最新上游版"，不谎报 |
| 2 | **0.0.0.0 暴露** | piik 监听 0.0.0.0，局域网内任何人可访问 8787 | **知情不阻断**：控制台经 metaHints 固定披露绑定面事实，并把 **LAN 链接（含本机内网 IP）原样展示**给用户——链接本身就是产品用法，藏起来反而是假安全感 | C 线 GetStatus → metaHints 位（DBX 同款"账上有话"口径）；本机 LAN IPv4 取值复用 platform 网卡枚举（`internal/platform/windows/net.go` 的 `Adapters` 通道，lan 模块同源），不自写 `InterfaceAddrs` 散落实现 |
| 3 | **cloudflared 公网隧道** | 公网邀请链接由 piik 在 Web UI 侧发起，hanxi 托管进程树里会凭空多出 cloudflared | **Web UI 侧发起，hanxi 不代管**：不做 hanxi 侧"一键开公网"按钮、不注入隧道参数、不复用 frpc 事件/告警面；hanxi 只承担 JobObject 回收义务（关在笼子里）与状态如实（进程树里有 cloudflared 时状态面可见） | B 线：Job 闭包断言单测（孙进程清点）；C 线：状态投影暴露"隧道进程在/不在"一灯，文案点明"公网链接由上游页面生成，离开本托管也会随 Job 终止" |
| 4 | **上游 5 周大龄 / 布局漂移** | 项目年轻、迭代快，但裸 zip 名 19 连版稳定、根平铺布局未变 | **漂移护栏常规档**（不升特殊档）：digest 100% 已是最强信任根，布局自检按**条目不变式**（主 exe 存在 + runtime 兄弟项存在），sidecar 停发已冻结为"不得引用"负不变式 | A 线 manager.go：布局自检 + VerifyLedger 漂移只报告不处置（DBX 同款克制）；TROUBLESHOOTING 候选条目：若未来布局真漂移再升档 |
| 5 | **孙进程漏尸** | cloudflared/piik-capture 为 piik spawn；若只杀 piik 主进程（或 Hanxi 崩溃路径），孙进程变孤儿继续占端口/耗资源 | **JobObject 全程罩住**：`CREATE_BREAKAWAY_FROM_JOB` 未设即继承；`KILL_ON_JOB_CLOSE` 兜 Hanxi 崩溃面；但 **Detached 默认**与 Job 连杀冲突时以托管家族统一语义为准（默认不连带，机主显式开"随 Hanxi 关闭"才罩死）——两态都必须在⑤剧本步骤 6 里各验一遍 | B 线 instance.go 硬约定（照抄家族）；剧本用任务管理器实证零残留 |
| 6 | **stdin 优雅停失效** | 优雅通道是官方设计，但版本漂移后行为无契约保证 | 宽限期 + TerminateJobObject 兜底照家族纪律；**优雅是否真生效以真机验收为准**（⑤步骤 6），离线单测只锁"先 stdin 后强杀"的调用顺序，不伪造优雅成功 | B 线：`closeGracePeriod` 包级变量 + wait() 分类；剧本勾验项 |

---

## ⑤ 机主真机验收剧本（阶段 5，逐项勾验）

前置：hanxi 本机 dev 构建，webapp/其它在途工作区改动与本剧本无关。目标版本 **v1.6.5**（钉版基准，追版后顺延）。

| 步 | 操作 | 预期观察点 | 勾 |
|---|---|---|---|
| 1 | 版本管理 Tab → 远程表定位 **v1.6.5** → 下载安装 | 下载校验徽章显示官方 sha256 已验证（非降级三层）；解压后安装目录**根平铺**：主 exe 与 runtime 兄弟项在位，**无** sidecar 相关断言失败 | ☐ |
| 2 | 控制台 → 启动 | 状态灯 running；PID 出现；无 UAC 弹窗（asInvoker 实证）；错误文案不得出现 WebView2 字样（侦查项 11 回归） | ☐ |
| 3 | 启动完成后观察浏览器 | **系统默认浏览器自动打开 8787 页面**；若被默认浏览器策略拦截，控制台应因 `GATE_NO_BROWSER` 展示**手动打开链接**，点击可达 | ☐ |
| 4 | 在 piik 页面走一次 **LAN 邀请** | 同局域网设备（手机连同一 Wi-Fi）用 LAN 链接可入会；hanxi 控制台 metaHints/LAN 展示行与页面链接一致（知情披露生效） | ☐ |
| 5 | 在 piik 页面走一次**公网邀请** | 页面生成公网链接（cloudflared 隧道）；hanxi 状态面可见隧道进程在位；**hanxi 侧全程无"开公网"按钮**（不代管负裁决回归） | ☐ |
| 6 | 控制台 → 停止，然后开任务管理器（详细信息页签）清点 | **`piik-app`、`cloudflared`、`piik-capture` 三个进程名零残留**；8787 端口监听消失（可用 hanxi 端口查杀页复核）；停止响应应明显走优雅通道（秒级、非强杀卡顿），若观察到宽限期后才退，如实记 TROUBLESHOOTING 候选 | ☐ |
| 7 | 卸载**最后一个已装版本** | 卸载确认文案如实提示**数据目录去留**（上游自管落盘位置点名）；hanxi 只删版本隔离目录，不谎称"已清除全部数据"；对照 ③ 修正版"CLI 正门改道"裁决 | ☐ |

附加勾验（家族标准项，离线难验）：多实例外部感知（机主自行双击裸 exe 起一个 piik，hanxi 能报"外部实例"且 Quit 不碰它）；"随 Hanxi 一起关闭"开关开/关两态各重启验证一次（关态退 Hanxi 后 8787 仍监听、开态连带且孙进程同灭）。

---

## ⑥ 五线分工、合入顺序与收口清单

**分工**（本档案为五路之四；各路只碰自己面，共享面一律留总闸）：

| 线 | 职责 | 落点（只读指向，不预写代码） |
|---|---|---|
| A 线 | version 子包：remote（裸 zip 资产名匹配、钉版呈现策略）/ downloader（assetMirrors 含 Gitee 官方镜像首链）/ manager（四层完整性 + 根平铺布局不变式） | `internal/modules/piik/version/` |
| B 线 | instance 子包：端口+镜像路径双探测、stdout 读 `GATE_NO_BROWSER`、stdin 优雅停 + Job 兜底、孙进程闭包单测 | `internal/modules/piik/instance/` |
| C 线 | 骨架 + 前端：module.go（包注释写明②③全部负裁决）/ service.go（metaHints 两披露位）/ store.go / PiikView.vue + bindings | `internal/modules/piik/`、`frontend/src/views/` |
| D 线 | catalog / 装配收口：app.go 两处事件 + modulesToRegister、navigation.ts 两表、cataloggen 重跑、fixture 契约文件 | `internal/app/app.go`、`scripts/`、`frontend/src/constants/` |
| **本档案（五路之四，档案线）** | 决策与验收的唯一文字依据 | 本文 |

**合入顺序**：**A → B → C → D**。理由：B 依赖 A 的 ResolveExe 面；C 依赖 A/B 类型；D 的 cataloggen/fixture 收口**必须等 A/B/C 全部落库**再跑——cataloggen 对"目录有/fixture 无"的模块 ID **fail loud 拒绝生成**（`scripts/cataloggen/main.go` 包注释），提前跑会把 D 线卡死在半成品状态。本档案独立提交，任意时点可合入（纯新增文档，零冲突面）。

**总闸批次清单**（四路 + 本档案全部合入后，由总闸一次性处理，本次并行一律不碰）：

1. **`docs/THIRD_PARTY_NOTICES.md` 新增 Piik 条目**：MIT 义务一句话 + **包内自带 THIRD-PARTY-NOTICES 随版保留、卸载随版本目录消失**的沿用口径（参照 DBX/ccswitch 条目句式）；同时把该文档首段的托管款数计数改为新值（现文写"全部 25 款"，+1 后如实改）。
2. **`go run ./scripts/cataloggen` 重跑**：先同步 `scripts/fixture/composition_contract.json`（piik 条目 + 如非预激活则不动 preactivatedModules），生成 `internal/app/catalog.go` 与 `module_catalog.json`，与 `catalog_contract_test.go` 同口径。
3. **bindings 再生 + 计数锁**：`task common:generate:bindings`（禁裸 `wails3 generate bindings`，会清空输出目录）；`frontend/src/constants/__tests__/navigation.spec.ts` 路由回归锁现钉"**登记了全部 60 条路由**"，piik 路由入表后同步改数并复核门禁集合长度。
4. `gofmt -w` + `go test ./...` + `task check` + 前端 build/test 全绿；原子提交按 skill 阶段 6 模板拆。

**给 D 线（catalog/装配）的 Order/Icon 冲突自查提示**：

- **Order 按组扫描口径**：`grep -rn "Order:" internal/modules/*/module.go`。当前 developer 组占用 `{70, 73, 74, 90, 92, 94, 95}`（70=ccswitch、73=gonavi、74=dbx——注意 gonavi 73 曾是"并行线在途"占位，**每次装配前重扫，别信快照**）。piik 归组二选一：GroupDeveloper（空位如 71/72/96+）或 GroupNetwork（当前网络簇 80/85/86/87 带内）；按②的"服务型 + LAN 协作"性质建议 **Developer 组紧邻 DBX 簇**，具体取值 D 线终调并在 navigation 终调注释里点名依据（DBX Nav 注释即先例格式）。
- **Icon**：`frontend/src/assets/apps/` **无 piik.png**——图标提取属红线候选（"候选另议"，勿擅自从上游包抽图标入库），本轮用**池内矢量 `i:` 键**，同族先例：gonavi=`i:database`、dbx=`i:server`、webapp=`i:monitor`。候选池（`frontend/src/constants/icons.ts`）：`cast`、`link`、`network`、`wifi`、`globe`、`server` 等；选定前 `grep -rh "Icon: \"i:" internal/modules/*/module.go` 确认池内键未被其它模块占作首选造成同组撞脸（dbx 与 gonavi 的"刻意错开免混淆"注释即此纪律）。
- **Route/组件挂载**：只改 `frontend/src/constants/navigation.ts` 的 `ROUTES` + `MODULE_PRESENTATION` 两表（App.vue 手写挂载已退役，勿再找）；改表即触发上条计数锁。

---

## 遗留与如实声明（不隐瞒项）

- 侦查事实（①表）来自并行侦查线的实证摘要转写；本文引用先例代码均已按当前工作区核对，但**上游仓库仍在日更**，实施前 A 线有义务对 v1.6.5 资产做一次**复验快照**（digest/资产名/布局三查），漂移即回报升档本文④风险 4。
- "piik 数据落盘位置、是否可改道"侦查深度有限：按**不改道**负裁决执行；若实施中在上游源码发现官方 env 改道开关，停下来在总闸批次提出来复议，不私自追加注入。
- 浏览器自动打开（步骤 3）由 piik 自己实现，hanxi 不代开也不禁开；`GATE_NO_BROWSER` 仅在失败态出现，验收需人为制造一次失败（临时改默认浏览器关联）才能勾到兜底链接展示，若机主嫌麻烦此步可降级为"代码审读 + 单测锁"并在本文档追记。
