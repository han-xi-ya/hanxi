# N37 双轴主题按钮配色返工——分析 + 矩阵对照稿（机主过目件）

> 排期出处：`docs/plans/PLAN_REMAINING_WORK.md` N37（四条返工要求：①逐板逐模式 on-color token 按 WCAG AA 4.5:1 逐对核、组件禁自取；②深色 glow 降级为实底+1px 提亮描边，彩色投影只留"进行中"脉冲一处；③产出五板×明暗按钮矩阵对照稿；④设计规范补 on-color 条款）。
> 本件只做分析与对照，未动 `tokens.css` / `components.css` / 任何视图。
> 静态样张（双击即开）：`docs/plans/specimens/n37-oncolor-matrix.html`。
> 对比度口径：WCAG 2.x 相对亮度公式，脚本临时计算（未留仓库）；四舍五入两位小数。

---

## 0. 一句话结论

排期病根判断需要修正一半：**`--color-on-primary` 其实已在 9adb7cb 逐板逐模式定义**（tokens.css 10 处），全局 `.btn-primary` 也确实在用它——真正的病灶不是"没定义"，而是：
1. **teal 青壳（默认板）浅色底不达标**：白字 on `#0f8b8d` 只有 **4.12:1**，机主"浅色实底按钮看不清"就是它；且经全前景穷举，**该底色下数学上不存在同时过 base(4.12/5.10) 与 hover(5.34/3.93) 两态的前景**（无论黑白），只能压暗底色解决；
2. **on-color 家族只覆盖了 primary 一种彩底**：danger/positive 实底全部在"借"——借 `--color-on-primary`（10 处）或借 `--color-text-inverse`（9 处），数字上碰巧大多过线，语义与 onyx 自定状态色上无人看守；
3. **随手取 `--color-text` 是真实风险面而非既成事实**：全库扫下来尚无"彩底实底按钮前景取 --color-text"的同块实例，但 `base.css` button `color: inherit` + `.btn` 基座不定义 color 的机制摆在那里，10 组合中黑字取 text / 白字硬码在深色底上全部爆掉（见矩阵）；
4. **glow 现状与主诉位置已钉死**："启动按钮外发光"实体 = FileShareHero 快传启动 CTA 的 `0 7px 16px` 彩色落影；另有 18 处状态灯/点常态 halo 环、5 处输入焦点环复用 glow token、13 处 banner 软描边借用 glow 值，清单见 §3。

---

## 1. 现状证据表（取值点逐个列，文件+行+现值）

### 1a. 取 `--color-on-primary` 的点（token 存在且被用——primary 底正当，彩底借用存疑）

| # | 位置 | 前景取值 | 实际底色 | 定性 |
|---|------|---------|---------|------|
| 1 | `styles/components.css:84` `.btn-primary` | `--color-on-primary` | `--color-primary` | 正当 |
| 2 | `views/LanScannerView.vue:361` `.btn-danger` | `--color-on-primary` | `--state-danger` 实底 | **借用**（danger 底吃 primary 的前景 token） |
| 3 | `views/PortScanView.vue:472` `.btn-danger` | `--color-on-primary` | `--state-danger` 实底 | **借用** |
| 4 | `components/ConfirmDialog.vue:89` 主按钮 | `--color-on-primary` | `--color-primary` | 正当 |
| 5 | `components/ConfirmDialog.vue:90` `.is-danger` 主按钮 | `--color-on-primary` | `--state-danger` 实底 | **借用** |
| 6 | `components/ui/UiPromptDialog.vue:107` | `--color-on-primary` | `--color-primary` | 正当 |
| 7 | `components/fileshare/FileShareHero.vue:119` `.title-icon` | `--color-on-primary` | 渐变 `primary→information` | **借用**（渐变底无人核过中间调） |
| 8 | `components/fileshare/FileShareSettings.vue:193` `.quick-port-chip.active` | `--color-on-primary` | `--color-primary` | 正当 |
| 9 | `components/NotificationDrawer.vue:169` `.badge-unread` | `--color-on-primary` | `--state-danger` 实底 | **借用** |
| 10 | `components/wsl/WslVersionsPanel.vue:261` `.plat-chip.current` | `--color-on-primary` | `--color-primary` | 正当 |
| 11 | `views/MemoView.vue:554` `.tag-chip.active` | `--color-on-primary` | `--color-primary` | 正当 |
| 12 | `views/MarkerOnView.vue:82` `.btn-toggle-primary/active` | `--color-on-primary` | `--color-primary` | 正当 |

### 1b. 彩底上取 `--color-text-inverse` 的点（"反色=页面底色"兼职彩底前景——正是"随手取"语义）

| # | 位置 | 前景取值 | 实际底色 |
|---|------|---------|---------|
| 13 | `styles/components.css:594-595` `.btn-bind-account` | `--color-text-inverse` | `--state-positive` 实底 |
| 14 | `components/wechatbot/WechatBotChatInput.vue:133` `.attachment-send` | `--color-text-inverse` | `--state-positive` 实底 |
| 15 | `components/wechatbot/WechatBotChatInput.vue:141` `.btn-send-message` | `--color-text-inverse` | `--state-positive` 实底 |
| 16 | `components/wechatbot/WechatBotBindModal.vue:200` `.btn-retry-qr` | `--color-text-inverse` | `--state-positive` 实底 |
| 17 | `components/shell/AppNavRail.vue:371` `.nav-badge` | `--color-text-inverse` | `--state-danger` 实底 |
| 18 | `components/wechatbot/WechatBotSidebar.vue:219` 等（rename/header 同族） | `--color-text-inverse` | 彩底动作钮 |
| 19 | `views/MangoDiskView.vue:202` `.md-switch` 滑块钮 | `--color-text-inverse` | 压 primary 底上（非文 3:1 口径，五板深值≈2.1-2.9 边缘，浅色板=白 on 深底达标） |

### 1c. 硬编码白字

| # | 位置 | 值 | 现状备注 |
|---|------|----|---------|
| 20 | `App.vue:241` `.global-toast` | `color: #ffffff` | 注释已声明"深底浮层刻意设计"，可豁免但要入册 |
| 21 | `WechatBotChatFlow.vue:313` / `ChatHeader.vue:146` / `Sidebar.vue:257` 头像字 | `#ffffff` | 已注"功能例外"，豁免入册 |

### 1d. "彩底随手取 --color-text"的机制隐患（尚无实例，结构性缺口）

- `styles/base.css:10,29`：`body{color:var(--color-text)}` + `button{color:inherit}`；`styles/components.css:60` `.btn` 基座**不定义 color**——任何彩底按钮忘写前景即自动吃到 `--color-text`。矩阵证明这条路的后果：**10 组合全部 <4.5 爆掉**（浅 2.43–3.83 / 深 1.37–2.13）。修复要在 token 契约上封死（前景一律显式取 on-color 族），不是逐处抓现行。

---

## 2. WCAG 违规矩阵（相对亮度标准公式，AA 4.5:1）

### 2a. 主矩阵：五板×两模式 × 三种"随手可得"的前景候选（bg = `--color-primary` 常态底）

**30 组中 20 组 <4.5**（加 hover 底再测 30 组，同样 20 组违规，60 组合计 40 违规）。✗ = <4.5：

| 板·模式 | 底 (primary) | 白 #ffffff | 黑 #000000 | `--color-text` 现值 | 现 token 值 |
|--------|-------------|-----------|-----------|--------------------|-------------|
| teal 浅 | `#0f8b8d` | **4.12 ✗** | 5.10 ✓ | `#14252c` 3.83 ✗ | `#fff` 4.12 ✗ |
| teal 深 | `#42b7b4` | 2.42 ✗ | 8.66 ✓ | `#e8f2f3` 2.13 ✗ | `#052326` 6.80 ✓ |
| sky 浅 | `#0064b5` | 6.02 ✓ | 3.49 ✗ | `#192029` 2.73 ✗ | `#fff` 6.02 ✓ |
| sky 深 | `#4ebdf7` | 2.11 ✗ | 9.95 ✓ | `#edf0f6` 1.85 ✗ | `#00243e` 7.52 ✓ |
| iris 浅 | `#6741ca` | 6.59 ✓ | 3.19 ✗ | `#211d29` 2.50 ✗ | `#fff` 6.59 ✓ |
| iris 深 | `#bba9fa` | 2.07 ✗ | 10.17 ✓ | `#f1eff5` 1.81 ✗ | `#27124b` 7.98 ✓ |
| jade 浅 | `#006869` | 6.60 ✓ | 3.18 ✗ | `#142222` 2.48 ✗ | `#fff` 6.60 ✓ |
| jade 深 | `#5dd1cf` | 1.83 ✗ | 11.49 ✓ | `#eaf2f2` 1.61 ✗ | `#002525` 8.90 ✓ |
| onyx 浅 | `#0649b8` | 7.92 ✓ | 2.65 ✗ | `#0a0f14` 2.43 ✗ | `#fff` 7.92 ✓ |
| onyx 深 | `#79b7ff` | 2.09 ✗ | 10.03 ✓ | `#f2f6fb` 1.93 ✗ | `#04223f` 7.68 ✓ |

hover 底（`--color-primary-hover`）补充行（同样 20/30 违规）：

| 板·模式 | 底 (hover) | 白 | 黑 | text | 现 token |
|--------|-----------|-----|-----|------|---------|
| teal 浅 | `#0b7779` | 5.34 ✓ | **3.93 ✗** | 2.95 ✗ | 5.34 ✗→✓(值同白, 唯 base 挂) |
| teal 深 | `#59c8c5` | 2.00 ✗ | 10.49 ✓ | 1.76 ✗ | 8.23 ✓ |
| sky 浅 | `#005093` | 8.18 ✓ | 2.57 ✗ | 2.01 ✗ | 8.18 ✓ |
| sky 深 | `#74ccfe` | 1.78 ✗ | 11.81 ✓ | 1.56 ✗ | 8.93 ✓ |
| iris 浅 | `#542dae` | 8.89 ✓ | 2.36 ✗ | 1.86 ✗ | 8.89 ✓ |
| iris 深 | `#c7b8ff` | 1.79 ✗ | 11.73 ✓ | 1.57 ✗ | 9.20 ✓ |
| jade 浅 | `#005657` | 8.51 ✓ | 2.47 ✗ | 1.93 ✗ | 8.51 ✓ |
| jade 深 | `#81dfdc` | 1.55 ✗ | 13.52 ✓ | 1.37 ✗ | 10.47 ✓ |
| onyx 浅 | `#003999` | 10.27 ✓ | 2.05 ✗ | 1.87 ✗ | 10.27 ✓ |
| onyx 深 | `#9bc9ff` | 1.72 ✗ | 12.19 ✓ | 1.59 ✗ | 9.34 ✓ |

**实锤三个结论**：
1. **方向性铁律**：浅色模式彩底必须白字（黑字 5 爆 4）、深色模式彩底必须深字（白字 5 爆 5）——"各组件随手取"必然顾此失彼；取 `--color-text` 在 10 组合中 10 爆（0 过线）。
2. **现 `--color-on-primary` 10 组里 9 过 1 爆，爆的恰是默认回退板 teal 浅**（4.12）——机主主诉①的唯一数字实锤。
3. **teal 浅无前景解**：白挂 base(4.12)、黑挂 hover(3.93)、任何深字只会更挂 hover——唯一干净解是把底色压暗一档（§4 方案 A）。

### 2b. 借用现状的连带核账（danger/positive 实底）

- `--state-danger` 浅 `#b33736` × 白 = **5.97 ✓**；深 `#ff7974` × 各板 on-primary 深值 = **6.23–6.47 ✓**——借用组数字碰巧全过，**但**：深色底 × 白字只有 **2.55 ✗**（哪个组件"随手取白"哪个爆），且 onyx 板自定 `#b3261e/#ff9690` 无人逐对核过（实测 6.54 / 7.67 ✓，过但没账）。
- `--state-positive` 浅 `#067d4d` × 白 = **5.19 ✓**；深 `#47c792` × 各板深色前景 = **7.45–7.73 ✓**；× 白 = **2.13 ✗**。
- warning `#f0ac45` × 深字 8.09–9.59 ✓；information `#72a5ff` × 深字 6.46–7.66 ✓（深底配白字一律 2.1–2.5 爆）。
- 结论：**on-danger/on-positive 即使按现值落地也是 10 组全过**，补 token 的意义是把"碰巧对"变成"契约对"，并把 onyx 例外纳入核账。

### 2c. 顺带发现的"主色字配软底"次级缺口（accent 文字族，非彩底前景但同属 on-color 账）

- 浅：`primary` 字 on panel——teal **4.12 ✗**，其余 6.02–7.92 ✓；primary 字 on selected——teal **3.55 ✗**、sky **4.30 ✗**、iris 4.60 ✓、jade 4.77 ✓、onyx 5.88 ✓。
- 深：primary 字 on selected——sky **3.51 ✗**、teal 4.83 ✓、iris 4.58 ✓、jade 4.59 ✓、onyx 4.88 ✓。
- teal 若按方案 A 压暗底色，其"主色字 on panel"自动跟到 4.62 ✓；sky 深浅两处 selected 上的主色字是独立旧账，建议入拍板点决定是否本轮顺带。

---

## 3. glow 使用点全清单与降级分类

（grep `glow` / `box-shadow` / `color-mix` 于 tokens/components 与全部视图，逐处定性）

### A 类：彩底/按钮**常态**发光投影——按②降级为实底+1px 提亮描边

| 位置 | 现状 | 定性 |
|------|------|------|
| `FileShareHero.vue:165` `.hero-action` | `box-shadow: 0 7px 16px --color-primary-soft`——**"启动快传服务"CTA 彩色落影，即机主主诉"启动按钮外发光"本尊** | 降级 |
| `FileShareHero.vue:123` `.title-icon` | `0 7px 16px --color-primary-glow`（hero 图标块彩影） | 降级 |
| `ManagedControlBar.vue:112,114,115,118` | `.status-light.running/external/failed/warn` `0 0 0 3px *-glow` 常亮 halo 环 | 降级（running 是稳态非进行中） |
| `VSCodeControlBar.vue:99,101,102` 同族 | 同上 | 降级 |
| `EverythingConsoleBar.vue:118,120,121` 同族 | 同上 | 降级 |
| `RustDeskView.vue:636,638,639` 同族 | 同上 | 降级 |
| `SubnetDeskView.vue:637,639,640` 同族 | 同上 | 降级 |
| `AppSidebar.vue:654` `.status-dot.online` | `0 0 0 2px --state-positive-glow` | 降级 |
| `AppNavRail.vue:359` `.rail-dot` | `0 0 6px --state-positive-glow` | 降级 |
| `FrpcProjectsView.vue:666` `.project-card.active` | `border+0 0 0 1px --state-positive-glow` 选中卡常亮 | 降级/改 soft token |

### B 类："进行中"脉冲——彩色光**保留**（②允许的唯一去处）

| 位置 | 现状 |
|------|------|
| `SnipCardView.vue:193-195` `.snip-pulse` | primary 点 + `0 0 0 3px --color-primary-glow` + 1.2s 呼吸动画——**全库唯一"脉冲+彩色光"合体，正牌保留件** |
| `*.status-light.starting` 六族（`ManagedControlBar:113`、`VSCodeControlBar:100`、`EverythingConsoleBar:119`、`RustDeskView:637`、`SubnetDeskView:638`、`MsixToolHeader.vue:37`）| `hx-pulse` 透明度呼吸、**无投影**——保留（如需按②升级为"脉冲带光"是唯一合法加光点，见拍板点 6） |
| `ver-status.downloading::before` 三族（`EverythingReleaseTable:126`、`ManagedVersionPanel:325`、`FrpcVersionsTab:305`）+ `components.css:360-366` `.live-pulse` | opacity 脉冲，无投影 |

### C 类：焦点环借用 glow 值（非投影，口径治理项）

- `EverythingConsoleBar.vue:139` `.search-input:focus` `0 0 0 2px --color-primary-glow`
- `LanScannerView.vue:481` `.input-inline` `0 0 0 2px --color-primary-glow`
- `FileShareHero.vue:174` / `FileShareSettings.vue:446` / `FileShareWorkspace.vue:126` `outline: 2px solid --color-primary-glow`
- 全局 `base.css:39` `:focus-visible` 已用 `--focus-ring`——以上 5 处属漏网散差，建议顺带归一（拍板点 5）。

### D 类：`border-color: *-glow` 软描边族（把 glow 值当"12-24% 透明度同色描边"用，非投影、无刺眼问题）

`components.css:317-320,345`（banner 四色+error-box）、`BCUView.vue:328-329`、`FrpcProjectsView.vue:729`、`HomeView.vue:656`、`NpmToolActions.vue:137`、`ErrorBoundary.vue:46`、`FileShareOverview.vue:116`、`FileShareSettings.vue:342`、`AdapterCard.vue:246`——共 13 处。视觉保留，token 语义建议另日正名（`--*-border` 派生档），不属本轮强制项。

---

## 4. 拟补 on-color token 草案（逐板逐模式建议值 + 实测数字）

命名沿用现库 `--color-` 前缀；**浅色五板实底前景一律白、深色五板一律本板深值**（即现 `--color-on-primary` 十字段扩成一族，danger/positive 实底从"借用"转正）：

```css
/* 每板每模式新增一对（值按上表实测选定）：
   --color-on-primary  主色实底前景（已有，仅 teal-light 需随底案联动重核）
   --color-on-accent   状态彩底（danger/positive/warning/information 实底）前景 —— 新 */
```

| 板·模式 | `--color-on-primary`（现值→处置） | `--color-on-accent`（新值） | on-accent 实测（danger底 / positive底 / warning底 / info底） |
|--------|--------------------------------|---------------------------|------------------------------------------------------|
| teal 浅 | `#ffffff`（4.12 挂→拍板点 1） | `#ffffff` | 5.97 / 5.19 / 5.41 / 5.17 ✓ |
| teal 深 | `#052326` ✓ 保留 | `#052326` | 6.46 / 7.73 / 8.39 / 6.70 ✓ |
| sky 浅 | `#ffffff` 6.02 ✓ | `#ffffff` | 同 teal 浅（状态色跨板恒定） ✓ |
| sky 深 | `#00243e` ✓ 保留 | `#00243e` | 6.23 / 7.45 / 8.09 / 6.46 ✓ |
| iris 浅 | `#ffffff` 6.59 ✓ | `#ffffff` | ✓ |
| iris 深 | `#27124b` ✓ 保留 | `#27124b` | 6.47 / 7.73 / 8.39 / 6.70 ✓ |
| jade 浅 | `#ffffff` 6.60 ✓ | `#ffffff` | ✓ |
| jade 深 | `#002525` ✓ 保留 | `#002525` | 6.38 / 7.63 / 8.28 / 6.61 ✓ |
| onyx 浅 | `#ffffff` 7.92 ✓ | `#ffffff` | 按 onyx 自定状态色核：6.54 / 6.56 / 6.45 / 5.89 ✓（AAA 边缘，见拍板点 4） |
| onyx 深 | `#04223f` ✓ 保留 | `#04223f` | 7.67 / 8.93 / 10.26 / 8.67 ✓ |

**teal 浅解法（拍板点 1 的两案）**：
- **案 A（推荐）**：底色压暗一档 `--color-primary: #0f8b8d→#0d8284`、hover `#0b7779→#0b7577`，白字实测 **4.62 / 5.49 双过**；连带"主色字 on panel"从 4.12 修到 4.62。色相/观感变化一档，需样张过目。
- 案 B：改前景无解——黑字 5.10/3.93 挂 hover，任何近黑 #02090b 也只有 4.87/3.75，数学上不存在双过前景（§2a 结论 3），**不建议再往这个方向试**。

**glow 降级方案（②落地形态）**：
- 深色模式实底控件取消一切彩色投影：`box-shadow: none` + `border: 1px solid color-mix(in srgb, var(--color-primary) 60%, white)`（等效"提亮描边"；灯点族等效改 1px 提亮环）。hover 态现 `--color-primary-hover` 本就是提亮值，描边取其同族。
- A 类 18 处 halo 环统一降级；B 类脉冲原样保留；彩色投影此后只许 `.snip-pulse` 式"进行中"脉冲使用。

---

## 5. 机主拍板点清单（共 7 条）

1. **teal 浅底色压暗案**（观感向，须看样张 A/B）：`#0f8b8d→#0d8284`+hover 微压，白字 4.12→4.62 达标 vs 维持现状容忍 4.12（默认板默认态不达 AA，名分难看）。样张已并排呈现。
2. **`--color-on-accent` 命名与覆盖面**：单一对（所有状态彩底共用）还是拆 `--on-danger/--on-positive/…`（未来各状态色独立调底时更稳）？草案按单一对出。
3. **深色状态灯 halo 降级范围**：18 处 A 类里，5 个托管家族控制条的 running/external/failed 灯环是"活体感"最强的一族——全降为提亮描边，还是灯点族豁免保留弱 halo（文字/按钮零豁免）？
4. **onyx 板特例**：AAA 定位板，light 状态色白字 5.89–6.56 已过 AA 但未到 AAA 7.0——onyx 的 on-accent 要不要按自家 AAA 口径继续压深/压底，还是 AA 即止？
5. **焦点环散差顺带归一**：C 类 5 处 `*-glow` 焦点环并入 `--focus-ring`——本轮顺手做，还是只记 D 类账不动？
6. **"进行中"脉冲要不要带光**：现 starting 灯只有 opacity 呼吸无彩色投影；②"彩色投影只留脉冲一处"是升格加光（视觉上更活）还是仅"保留 .snip-pulse 现状不加码"？
7. **次级缺口（§2c）是否并案**：sky 深 selected 上主色字 3.51、sky 浅 selected 4.30、teal 浅 selected 3.55（拍板点 1 案 A 只修 teal 的 panel 账，不修 selected 账）——本轮一并核 or 另立小账？

---

## 6. 样张说明

`docs/plans/specimens/n37-oncolor-matrix.html`：五板×明暗 10 区块，每区块左"现状"右"提案"两列，真实渲染 primary/secondary/danger 实底按钮 + 启动 CTA（含彩色落影降级对照），每枚按钮下标注实测对比度；token 值全部自 `tokens.css` 现文实抄，明暗块并排。双击即开，无外部依赖，无 emoji。

> 边界重申：本件与样张仅新增两文件，未触 tokens/components/视图，未 commit。
