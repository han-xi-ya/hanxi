# Hanxi 前端架构与重构蓝图（Frontend Architecture）

> **文档定位**：前端（`frontend/`）的架构现状、设计语言约定、结构问题与**渐进式重构蓝图**的唯一权威规范。代码改动以本文为准；本文与 `.claude/skills/hanxi-workbench-ui` 冲突时，以设计技能的 `references/design-system.md`（视觉 token）为准。
> **技术基线**：Vue 3 + TypeScript + Vite · Wails v3 绑定 · **无 vue-router / 无 Pinia / 无第三方 UI 框架 / 无 CSS 框架**（VueUse 已引入作胶水层）
> **更新日期**：2026-09-05
> **进度快照**：Phase 0 ✅｜Phase 1 ✅｜Phase 2 ✅（共享层 + 视图异步化 + 崩溃兜底）｜Phase 3+4 ✅（托管家族 19/19，组 A–F 六提交 `e44621e`…`4e1da21`）｜**Phase 5 ✅**（工具视图与系统页：主线四页 `754ea2a`、G1 `82daa70`、G2 `121581a`、G3 `2bebbde`+tokens 修复 `741693a`、G4 `4afc126`、G5 `320fd8b`；全仓 515 用例、build/lint/typecheck/verify 全绿；三份路由清单收编为 navigation 单一来源）。**§9.5 跟进批首批 ✅**（家族级 bug 修复①②、`--terminal-*` 细分 token④、VersionsView 死码删除，516 用例全绿）；当前待办＝§9.5 残余（③ Snipaste 二次收编【下一刀】、⑤ running 文案/MSIX 壳抽取、AppIcon 图标集）与 Phase 6（巨型孤本拆分：WechatBot/FileShare/PublicIp/Everything）。全应用深色主题已实质可用，残余浅底仅限个别视图局部。

---

## 1. 角色与约束

- 前端是 Wails 桌面壳内的**本地 SPA**（WebView2），不面向公网、不做 SSR。
- 与 Go 后端**只经 `frontend/bindings/` 的生成封装**通信（类型化 RPC + 事件 `Events`）；绑定为自动生成物，**禁止手改**，由 CI `verify:bindings` 守护。
- 设计语言遵循 `hanxi-workbench-ui` 技能：冷静、紧凑、低噪声、状态清晰、浅/深双主题、无障碍与响应式内建。

---

## 2. 现状架构（如实描述）

```
main.ts ── createApp(App)                          # 无 router、无 store
App.vue  ── 应用外壳（手写一切）
  ├─ CORE_VIEWS: route → component 映射            # 手写路由
  ├─ ROUTE_MODULE_MAP: route → 后端 moduleId        # 路由切换前 EnsureModuleActive 门禁
  ├─ <component :is> + <KeepAlive :max=10>          # 动态视图 + 缓存
  ├─ Events 顶层桥：ext:changed / notify:received / tray:navigate
  └─ <style>:root                                   # 全局设计 token（当前仅浅色）
views/<模块>View.vue  ×33                            # 每后端模块一视图，普遍偏大（最大 2170 行）
components/                                         # ConfirmDialog / Notification* / Frpc* / envcheck/*
composables/  useToast · useNotification            # 模块级 ref 单例＝事实状态层
utils/        errors.ts: getErrorMessage
```

**已做对、应保留**：`useToast`/`useNotification` 单例模式；`getErrorMessage` 统一异常；`ConfirmDialog`（Teleport+焦点陷阱+`tone/busy/details`+`v-model:open`）是可访问性标杆；事件订阅均有 unlisten 清理、`onActivated/onDeactivated` 管轮询的生命周期纪律。

---

## 3. 已知结构问题（重构动因）

| 维度 | 事实 | 影响 |
|---|---|---|
| 三套"方言" | 工具视图裸类名 / **15+ 托管家族视图逐字节复制**同一样板 / NanaZip·EarTrumpet 互为镜像 | 改一处要改十几处；新工具靠复制粘贴 |
| 原子样式复制 | `.btn`×26、`.tbl`×18、`header-row`×25、`empty-state`/`error-box`×19、`@keyframes`×23 | 视觉难统一、体积冗余、"加样式"要到处补 |
| Token 分裂 | App.vue 用 `--text-main/--accent:#2f6fed`；ConfirmDialog/envcheck/EarTrumpet 引用**从未定义**的 `--text-primary/--bg-main/--warning`（靠 `color:inherit` 侥幸）；FileShare 第三套 `--color-*` | 隐性 bug；与设计规定的主色（青 `#0f8b8d`）+ 深色主题完全脱节 |
| 胶水重复 | `setInterval` 轮询×31 视图、`Events.On+unlisten` 样板×25、`fmtSize/Date/Duration`、`stateText` 各 15–17 份 | 定时器/监听易漏清、格式化各写各的 |
| 危险操作用原生弹窗 | `window.confirm`×19、`window.prompt`×14、手搓 modal×5 | 绕过已有的可访问 `ConfirmDialog`，风格/无障碍不一致 |
| 清单三份并行 | App.vue 两张表 + HomeView 重复 `MODULE_META` | 增删模块易漂移 |
| 零测试/零 lint | 仅 `vue-tsc` + `build` + `verify:bindings` | 重构无回归防线 |

---

## 4. 目标架构（分层）

```
src/
  main.ts                     # import 三份全局样式；初始化主题
  styles/                     # 唯一样式来源（从 App.vue 抽出）
    tokens.css                #   设计 token 单一来源：浅+深双主题 + 全部遗留别名（消歧未定义变量）+ 字体栈 + --ansi-* 日志终端色板
    base.css                  #   reset/字族/:focus-visible/prefers-reduced-motion/100dvh/safe-area
    components.css            #   全局原子工具类：.btn家族/.card/.panel/.tbl/.chip/.banner/.empty-state/.mono/.link-button/页面骨架
  composables/                # VueUse 支撑 + 自研
    useWailsEvent  usePolling  useAsyncAction  useClipboard  useConfirm  usePrompt  useManagedTool
    useToast  useNotification  useTheme
  components/ui/              # 无业务原子件：UiButton/UiStatusChip/UiBanner/UiEmptyState/UiProgressBar/UiModal/UiPrompt(带输入)/ErrorBoundary/StatePanel/PageHeader/MainTabNav/AppIcon
  components/tool/            # 托管家族共用壳：ManagedConsoleShell / VersionGrid / StatusHeader
  constants/                  # status.ts（状态→{text,icon,tone} 单一表）；navigation.ts＝route→组件/图标/标题的**前端元数据**单一来源
  │                           #   ——后端注册表仍管"模块存在/启用"，两者在侧栏渲染处合并，navigation.ts 不吞并后者
  utils/                      # errors.ts + format.ts（fmtSize/fmtDate/fmtDuration）
  views/                      # 迁移后瘦身，只留业务差异
```

**分层原则**：皮与布局＝自研 `ui`/`styles`；通用行为（定时器/监听/存储/剪贴板）＝`composables`（优先 VueUse）；复杂交互件（命令面板/可搜索下拉/嵌套菜单）将来按需引无皮库 Reka UI，**当前不引入**。

**依赖策略**：新增 `@vueuse/core`（定时器/监听/媒体查询/存储/剪贴板的自动清理与少写胶水）；版本锁定，遵 `.npmrc` 供应链策略。

---

## 5. 分阶段路线图

| Phase | 内容 | 交付 | 风险 |
|---|---|---|---|
| **0 安全网** ✅ | ESLint(flat)+Prettier（先 warn 不阻断）、Vitest+@vue/test-utils，给 ConfirmDialog/useToast/一个托管视图状态机写**特征测试** | lint/test 脚本、基线用例 | 低（additive） |
| **1 样式地基** ✅ | 落地 `styles/{tokens,base,components}.css`（**青绿 `#0f8b8d` + 浅/深双主题**，含全部别名消歧、`--ansi-*` 终端色板与字体栈，主题变量单一来源见 §7）；`useTheme` 三态切换（**后端 Settings 持久化** + 首帧缓存）+ 侧栏与设置页切换入口；后端 DWM 深色标题栏同步（铁律 8 唯一例外）；引入 VueUse；加 `@`→src 别名 | 主色全量转青 + 深色主题可切换（含窗框） | 中：一次性变色，单独可回滚 commit |
| **2 共享件** ✅ | `utils/format`、`constants/status`、`constants/navigation`、`components/ui/*`、`composables/*`；`useConfirm` 收编 confirm、**`usePrompt`（UiPrompt 带输入）收编 14 处 `window.prompt`**；`ErrorBoundary` + `app.config.errorHandler` 兜底（单视图崩溃不再整壳白屏）；`CORE_VIEWS` 视图**异步化**（`defineAsyncComponent` 点哪个加载哪个）+ 单测 | 共享层就绪 + 首屏瘦身 + 崩溃兜底（不迁移视图） | 低 |
| **3 试点** ✅ | 迁 `MarkerOnView`+`CCSwitchView` 到新骨架，锁特征测试一致，产出迁移模板+checklist（§9 手册） | 前后对照样板 | 中 |
| **4 铺开托管家族** ✅ | 五路并行 fork + 主线模板组，19/19 托管视图套 §9 手册迁移（含 rustdesk/subnetdesk/everything/ddnsgo 等特殊形态）；envcheck 对齐 token 留 Phase 5 | 家族重复基本消除 | 中：逐视图验活 |
| **5 工具视图顺手治理** | Wifi/Lan/PortScan/PublicIp/PortKill/FileShare/Memo/Wechat 等"改到哪治到哪"：`useWailsEvent/usePolling/useConfirm/usePrompt/format/UiXxx`；NanaZip/EarTrumpet 合并；**长尾认领：frpc 双视图（FrpcProjects/Versions + Frpc* 组件）、系统页 Home/Logs/Settings/About、ExtPlaceholderView** | 长尾收敛 | 低-中 |
| **6 后续（暂不做）** | 巨型孤本视图（WechatBot/FileShare/PublicIp/Everything）结构拆分；App.vue 进一步组件化 | — | — |
| **7 文档** | 技术栈漂移已修；本蓝图持续更新 | 文档一致 | 低 |

> **全量认领对账（35 视图，无漏网）**：Phase 4 托管家族 19（含 rustdesk/subnetdesk/envcheck）；Phase 5 工具与长尾 16（网络类 8 + frpc 2 + 系统页 4 + ExtPlaceholder + Memo/FileShare 重叠计一）。SettingsView 的主题切换器属 Phase 1 最小改动，其余治理在 Phase 5。

---

## 6. 迁移铁律

1. **不改业务契约**：视图的绑定调用、props/emits、事件名、路由字符串、`KeepAlive` 生命周期保持不变——只换骨架与胶水。
2. **一次一视图**，改完即 `typecheck`+`build`+真机目视 + 特征测试，绿了再下一个。
3. `usePolling` **必须显式实现 `onActivated/onDeactivated`**（与 `KeepAlive:max=10` 耦合），杜绝轮询重复/泄漏。
4. 定义 token 后，逐一目视校正"原本靠未定义变量侥幸生效"的组件（ConfirmDialog/envcheck/EarTrumpet）。
5. 统一 bindings 导入为 **flat-service** 风格（`import * as XxxAPI from '.../xxxservice'`），仅对**改到的视图**归一，不做大范围重排。
6. emoji 不作主图标：shell/状态图标逐步迁 `AppIcon` 内联 SVG（增量）。
7. **主题变量只住在 `styles/tokens.css`**：组件/视图样式一律 `var()` 引用语义 token，不得硬编码颜色、不得就近定义主题变量；主题切换能力依赖此纪律（详见 §7）。
8. 每 Phase 独立中文 Conventional Commit，可单独回滚；不碰 `internal/**` 后端——**唯一例外**：Phase 1 的 DWM 深色标题栏同步调用（§7.2）。
9. **新视图冻结线**：Phase 3 模板产出后，新增/增量视图（含新纳入的托管工具模块）必须直接按新骨架 + 共享胶水编写，禁止再复制旧方言样板——否则迁完 15 个又冒出新的复制体。
10. **独立分支**：重构在专用分支（`refactor/frontend`）小步提交、每 Phase 绿后合回 `dev`，与 dev 上的新功能迭代互不阻塞。

---

## 7. 主题与设计 token（权威、抽离与切换机制）

以 `hanxi-workbench-ui/references/design-system.md` 为准，落到 `styles/tokens.css`。核心承诺：**一切主题相关变量只抽离定义在 `tokens.css` 一处；换主色、加主题、做高对比度 = 只改变量块，不改任何组件代码**。

### 7.1 变量三层模型

| 层级 | 内容 | 定义位置 | 示例 |
|---|---|---|---|
| 原始色板 | 品牌色/中性色/状态色的完整色阶 | 仅 `tokens.css`，组件不可见 | `--teal-600: #0f8b8d` |
| 语义 token | 业务含义：主色、表面、文字、状态、边框、阴影、半径、动效、字族 | 仅 `tokens.css`，浅/深主题各定义一次 | `--color-primary`、`--surface-1..4`、`--text-1`、`--state-danger` |
| 组件局部变量 | 组件私有微调（尺寸、间距等） | 组件 `<style scoped>` 内，引用处给 `var()` 回退 | `--chip-dot-size: 8px` |

- **语义命名归一**（消歧 17 个未定义变量）：`--text-main/--text-primary` → `--color-text-primary`；`--bg-app/--bg-main/--bg-sidebar/--surface*` → `--surface-1..4` 家族；`--accent` → `--color-primary`；`--warning` → `--state-warning`。旧名只做**过渡期别名块**（如 `--text-main: var(--color-text-primary)`），Phase 4/5 迁移到哪族删到哪族，最终清空。
- **硬约束**：视图/组件样式**只允许 `var()` 引用语义 token，禁止硬编码颜色、禁止在组件内重新定义主题变量**；裸 hex 只允许出现在 `tokens.css`。

### 7.2 主题切换机制

- 主题由 `<html data-theme="light|dark">` 属性承载，`:root[data-theme='dark']` 覆盖语义 token；根上声明 `color-scheme: light dark`。
- **持久化以后端为唯一真相**：`internal/settings` 的 `AppSettings.Theme`（`light|dark|system`，字段已存在但一直闲置）经 SettingsService 绑定读写——设置统一存后端，且便携版随 `data/` 目录整体迁移不丢主题。localStorage 仅作**首帧缓存**（mount 前同步读、先定 `data-theme` 防闪白；启动后异步以后端为准校正）。
- **原生标题栏随主题**：深色内容需 DWM `ImmersiveDarkMode` 同步 Windows 原生窗框（否则"深窗口顶白标题栏"割裂），需在窗口层加少量 Win32 调用——这是迁移铁律第 8 条"不碰后端"的**唯一受控例外**，Phase 1 单独 commit。
- `composables/useTheme.ts`（VueUse `useMediaQuery` + 后端 Settings 读写）是主题**唯一读写入口**，模块级单例，三态：跟随系统 / 固定浅色 / 固定深色，系统切换自动跟随。
- 切换 UI：侧栏底部 + 设置页各一处，共用同一 composable，不各写各的。
- **深色不是简单反色**：表面四层与 `positive/information/warning/danger` 均有独立双值，按 `design-system.md` 逐 token 设计。
- 扩展位：将来若加"高对比度/品牌联名主题"，仅需新增一个 `[data-theme='xxx']` 变量块 + 后端 `Theme` 枚举值 + `useTheme` 分支，零组件改动。

### 7.3 关键 token 值（摘要）

- **主色**：`primary #0f8b8d`（浅）/ `#42b7b4`（深）；`positive/information/warning/danger` 双值。
- **表面四层公式**：`page → surface → surface-soft → surface-hover`，边框先于阴影。
- **字族**：`--font-text / --font-display / --font-mono`；仅机器值（路径/端口/版本/速率/日志）用 mono + `tabular-nums`。
- **半径/阴影/动效**：control 8 / element 12 / panel 16 / pill 999；动效 100–180ms、≤6px；**必须含 `prefers-reduced-motion` 全局块**。
- **无障碍**：`:focus-visible` 清晰环；`pointer:coarse ≥44px`、桌面按钮 ≥36–38px、资源行 ≥52–56px；状态不只靠颜色；网格文本子项 `min-width:0`。
- **字体栈写实**：`--font-text` 含中文回退（如 `"Segoe UI", "Microsoft YaHei UI", system-ui`），`--font-mono` 用 `Consolas, "Cascadia Mono", monospace`；组件视图不得再自带 `font-family` 声明。
- **日志终端色板**：frpc 日志抽屉 / 运行日志查看器是 ANSI 彩色终端风格——`--ansi-0..15` 在 `tokens.css` 单独成组（浅/深各一套，或固定深底永不反相，Phase 1 目测定夺），防止为黑底终端设计的色板浮在双主题界面上。

> 完整 token 表、组件组合模式、完工检查单：见 `.claude/skills/hanxi-workbench-ui/references/`。

---

## 8. 落地细节与提交纪律（评审补充结论）

- **测试 seam**：Vitest（happy-dom）没有 Wails 原生层，直接 import `frontend/bindings` 与 `Events` 会挂——Phase 0 先定对绑定服务/事件的 `vi.mock` 打桩约定，之后才有可写的托管视图特征测试。
- **scoped 特异性陷阱**：scoped 样式带属性选择器，压过全局原子类；迁移时旧 scoped 副本不删净会"改了全局没生效"。对策：`components.css` 原子类用 `:where()` 包装（零特异性），且"迁移即删该视图本地副本"。
- **Prettier 一次性**：全仓格式化独立成一个 `style:` 提交（`git blame --ignore-rev` 记录该 SHA 保护 blame），此后每 Phase 只格式化 touched 文件，不顺手全仓。
- **AppIcon 图标集**：emoji→内联 SVG 前需先列 20–30 个状态/操作图标清单；若取自开源图标库，按许可证登记 `docs/THIRD_PARTY_NOTICES.md`。
- **依赖兼容矩阵**：Vitest 大版本必须实测支持 Vite 8；ESLint flat config + `eslint-plugin-vue` + `@typescript-eslint`；全部锁进 devDeps（`.npmrc` 供应链策略下）。
- **生成物必须排除在 lint/format/test 之外**：`frontend/bindings/**` 进 ESLint ignores、`.prettierignore`、vitest include 白名单之外——生成物一旦被格式化，CI `verify:bindings`（git diff 校验）当场爆红。
- **WebView2 能力假设**：`:where()`、`color-mix()`、`100dvh` 等依赖较新 Chromium——WebView2 为 Evergreen 自动更新，Win10 22H2+ 基线基本无忧；若目标机器存在固定版本 Runtime 需先用后写。
- **localStorage 便携性缺口**：WebView2 的 localStorage 在系统 profile，**不随 `data/` 迁移**（`EverythingView` 列宽等既有用法同病）——需跨机保留的数据一律进后端 settings；localStorage 只配作首帧缓存这类可弃用途。
- **不受影响项（已核实）**：ddns-go 面板子窗口加载上游原生页面（外部 URL），不吃本项目 token，主题重构无需处理。

---

## 9. 视图迁移操作手册（Phase 3 模板产物，组 A 验证）

对每个视图按序执行，全程只允许触碰该视图与其特征测试文件：

1. **先锁行为**：无测试则补特征测试（范式：`views/__tests__/MarkerOnView.spec.ts`——
   `vi.hoisted`+`vi.mock` 打桩 bindings service/AppService/`@wailsio.runtime` Events；
   KeepAlive 宿主挂载；fake timers 场景用纯微任务排空，**禁用 flushPromises**）。
   断言以 DOM 文本/类名/禁用态/调用序列为准；测出"现状 bug"如实钉死并注释，不顺手修。
2. **胶水替换**：`Events.On+unlisten+onUnmounted` → `useWailsEvent(name, handler)`（handler 收解包后的 data）；
   `setInterval+startTimers/onActivated/onDeactivated` → `usePolling(fn, ms)`（多定时器就多次调用；
   进页首刷由 usePolling 首跑承担，onMounted 只留其余加载）；
   `window.confirm/prompt` → `useConfirm/usePrompt`（文案逐字拆 title/description；测试改为
   import 单例 + `settleConfirm/settlePrompt` 驱动）；剪贴板手搓 → `useClipboard`；`fmt*` → `utils/format`。
3. **模板替换**：页头 → `PageHeader`（右侧选项卡进 `#actions` + `MainTabNav v-model`）；
   条件提示条 → `UiBanner :tone`（banner computed 的 `cls` 改 `tone: ok/info/warn/error`）；
   `link-btn` → 全局 `link-button`。
4. **样式去重**：删除与 components.css 原子同义的 scoped 块（.btn 家族/.tbl/.header-row/.subtitle/
   .error-box/.mono 基样式/.empty-state/.banner-*/@keyframes pulse→改引全局 hx-pulse）；
   保留视图独有布局样式并把其中颜色/圆角/动效换成语义 token（`--color-*`/`--surface-*`/`--state-*`，
   硬编码 hex/rgba 清零）；状态灯类名带视图前缀防全局碰撞的惯例保留。
5. **验证闭环**：定向 vitest 全绿 → vue-tsc 无本视图错误 → **npm run build 必过**（vitest 默认 `css:false` 不解析 `<style>` 块，CSS 注释星杠截断一类病灶只有 build 能抓，Phase 5 实证） → 主线跑全量 → 真机目视（用户）。
6. **差异决策表**（组 A 实例）：`hint-banner`→`.banner` 类名前缀更名＝测试同步改选择器；
   卸载确认载体从原生换可访问对话框＝测试改单例驱动；radius 6px→8px 等 token 收敛＝设计意图不算回归；
   任何"业务调用变了"＝违规，立即回退。

### 9.5 迁移期发现登记（Phase 3/4 各组上交，均已被特征测试按事实锁定，修复合入独立跟进提交）

1. ✅ **onFollowToggle 家族潜伏 bug**（已修复）：原成功路径不回写 `followOnExit`（toast 用 stale 值，文案可能反向）、失败"回滚"实为翻转提交值。14 视图统一改「点击乐观翻转 ref → 成功保持 / 失败回写复位」（`:checked` 单向绑定与 DOM 勾选态由此自洽）；FlClash（失败复位）、RustDesk/SubnetDesk（成功回写+文案跟随新值）特征测试同步；MangoDisk 本已正确不涉。
2. ✅ **下载失败详情死分支**（已修复）：13 样板视图操作列新增 `v-else-if="statusOf==='error'"` 分支渲染 `.dl-error` 详情；FrpcVersionsTab 复活缺失的重试入口（link-button）；BCU 双变体外层/内层条件纳入 error；PaperTodo 下载模板并含 error 态（retryDownload/lastVariant 复活）。死分支锁定测试翻转（FrpcVersionsTab/PaperTodo），MarkerOn 新增家族代表用例。VersionsView.vue（frpc 版本页旧死码孤本、活体为 FrpcVersionsTab 组件）经用户确认已删除。
3. **Snipaste 模板二次收编**：待 MainTabNav 增 aria id 前缀 props、badge/state-box/version-table 上收原子后进行；同时清除其 `var(--danger, #hex)` 兜底写法。
4. ✅ **`--terminal-*` 细分 token 组**（已落地）：tokens.css 新增 `--terminal-{head-bg,head-fg,border,row-fg,meta-fg,faint-fg,warn,error-bg}` 八枚（由 bg/fg 派生、深色块不重写——终端固定深底约定）；DdnsGo/FrpcProjects/LogsView 三终端面 15 处就近 color-mix 全部收敛，散值归一（边框 22/18/14→18、行文本 88/92/100→92、辅助 65/60/55→60 等）；视图内此后禁止再写终端色 color-mix 派生。
5. **running 文案家族分歧**（运行中/已启动）与 **MSIX 管理型壳抽取**（NanaZip/ET 剩余差异 ≈80 行模板 + 40 行样式可抽）：并入 Phase 5/4b 决策。
6. 已接受的微差：fmtSize 恰 1MB 边界（`>=`→`>`）、确认按钮"确定"→"确认"、复制失败文案收敛、slim banner ~2px 内距、badge→chip 观感——均为设计归一，真机目视项。

## 10. 提交拆分（示例）

`chore(frontend): 引入 ESLint/Vitest 与依赖基线` → `feat(frontend): 落地设计 token/全局样式层与青绿+深色双主题` → `refactor(frontend): 抽取共享 composable/原子组件/格式化与常量单一来源` → `refactor(frontend): 试点迁移托管视图到新骨架(MarkerOn/CCSwitch)` → `refactor(frontend): 铺开托管家族迁移(分批)` → `refactor(frontend): 工具视图顺手治理与 window.confirm/prompt 收编`。
