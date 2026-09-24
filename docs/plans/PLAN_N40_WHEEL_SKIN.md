# N40 ③ 轮盘对标 StarPie 美化 · 提案与拍板件（待机主选向）

> 排期出处 [PLAN_REMAINING_WORK](PLAN_REMAINING_WORK.md)「`| N40`」行的 ③——①②（几何
> 单点路由 `resolveHover`/`slotOf` 全扇面命中 + 圆心静区去闪烁）已于 2026-09-24 落地，
> 本件只管**皮**。StarPie 参照论证与牌面 F1-F4 见 [PLAN_N5_QUICKMENU](PLAN_N5_QUICKMENU.md) §0/§1。
>
> **对比样张**：[specimens/n40-wheel-starpie.html](specimens/n40-wheel-starpie.html)——
> 左右两块 512×512 SVG 轮盘（现状复刻 vs StarPie 化提案），明暗各一套（teal 色板实抄值），
> 同数据同悬停姿态，差异点 ①—⑥ 盘上角标 + 图例。拍板建议对着样张逐条勾。
>
> 定稿前不动任何产品代码；本文件与样张是纯新增文档件。

## 0. 一句话结论

现状牌面是 N6-F4 定调的"**连续玻璃盘**"（透明扇区 + 0.9° 发丝缝 + 一镜到底盘底）；
StarPie 化提案改语言为"**磨砂花瓣盘**"（大间隙圆角独立弧段 + 浮起图标座 + 强选中让位 +
hub 仪表核）。六条差异各自可独立取舍，**不构成捆绑**——逐条拍板（§2），全 A 即维持现状。

## 1. 差异点清单（现状值 → 提案值 → 涉及文件/选择器）

几何常量现状（[wheelGeometry.ts](../../frontend/src/components/quickmenu/wheelGeometry.ts) `WHEEL`）：
512 窗 / C=256 / rDisc=170 / rHub=62 / rSecIn=74 / rSecOut=162 / rCapIn=174 / rCapOut=236 / rCancel=244。
帽带张角公式（子数×24° clamp [父步长,180°]）为**命中与呈现共用的语义参数，本轮不动**。

### ① 扇区间隙：发丝缝连续面 → 大间隙圆角花瓣

- 现状：`mainSectorAngles(i,n,padDeg=0.9)`——每侧 0.9° 缝（外缘约 2.5px），扇区面
  `fill:transparent` + `stroke:var(--color-border)` 1px 发丝描边，盘底
  `.disc-face` 一镜到底；另有 `.ticks` 逐父扇区分界刻度线。
- 提案：缝角 0.9° → **≈4.2°**（外缘间隙 ≈12px，内缘 ≈2.7px，呈"内窄外宽"花瓣收拢感）；
  楔形端头圆角化（实现走法：路径 fill 与 stroke 同取一色、`stroke-width:6` +
  `stroke-linejoin:round` 的 SVG 膨胀接合技巧，圆角 ≈3px，零 path 布尔运算）；
  视觉环带从 74→162 收到 **80→158**（绘制值，命中不动，见红线），盘缘 162→176 一段
  留给连续亮环（渐变描边 `.disc-rim`），`.ticks` 刻度线退役（间隙本身已是分界语言）。
- 涉及：`wheelGeometry.ts`（`mainSectorAngles` 默认 `padDeg`——**仅渲染调用方传入值改**，
  `slotOf` 名义角域与 pad 无关，命中零影响）；`QuickMenuPopup.vue` 样式
  `.sector` `.disc-face` `.disc-edge` `.disc-glint` `.ticks` + 新增 `.disc-rim`。

### ② 帽带圆角：直角楔形贴纸 → 收圆帽底环 + 圆角厚弧段

- 现状：帽带底环 `.cap-band`（surface-hover 直角环段 r174→236）+ 子扇区
  `capSectorAngles(...,padDeg=0.9)` 直角楔形，暗色下 `fill:var(--surface-selected)`；
  外缘 `.cap-rim-accent` 主色提示弧。
- 提案：底环改**两端收圆帽的粗弧**（`arcPath` + `stroke-linecap:round`，或等价环段
  圆角化）；子扇区缝角 0.9° → **≈3.5°** 并同 ① 法圆角化（r177→233 绘制值）；
  底环色从 hover 档改 **primary ≈10% 软底**，级联归属自带主色血统，
  `.cap-rim-accent` 保留（提案弧收细到 1.5px）。
- 涉及：`wheelGeometry.ts`（`capSectorAngles` 默认 `padDeg`，同上仅观感面）；
  `.cap-band` `.cap-sector` `[data-theme='dark'] .cap-sector` `.cap-rim-accent`。

### ③ 图标座：面板色描边井 → 磨砂浮起座

- 现状：`.sector-icon` 圆井 `--d-well` 34/30/26/24 四档，明色 `background:var(--surface-panel)`
  + 1px border（暗色换 `--surface-hover`）；激活 primary-soft 底 + primary 描边。
- 提案：井升级为**浮起座**——明色：panel ≈85% 半透 + border-strong 细边 + 顶部高光渐变；
  暗色：`rgba(232,242,243,.08)` frost 底 + 16% 白描边（现"反向抬升一档面板"策略废除，
  改磨砂语言避免"贴片感"）；激活：primary 描边 + primary ≈16% 软底 + 复用 ⑤ 的辉光滤镜；
  直径四档各 +2px（36/32/28/26，密度对象 `density.well`）。
- 涉及：`.sector-icon` `.sector-btn.is-active .sector-icon` `[data-theme='dark'] .sector-icon`
  `.cap-icon` + `QuickMenuPopup.vue` 脚本内 `density` 对象（纯 CSS 变量下发值，非几何）。

### ④ 名称排版：两行折行 → 单行短名，全称完全交 hub

- 现状：`.sector-name` 10–12px 600 字重两行折行截断（N6-F1 v2 定案），图标 + 两行名
  挤在 `--d-btn-w` 48–64px 按钮宽里。
- 提案：**单行 11–12px** + 省略号，扇区视觉重心归图标座；全称/类型走 hub 大字号
  读数（F1 的"hub 干活"路线执行到底）。折行两行的容量让位给间隙变大后的留白秩序。
  组角标从 `▸ N` 文字改**圆形计数徽标**（座外上角，panel 底 primary 描边，数字居中）。
- 涉及：`.sector-name` `.sector-caret` `.cap-name` + `density.name` 各档 +2px 档距；
  角标若换 DOM 结构（span→svg circle 徽标）也只动模板呈现层，不碰 `aria-*` 语义。

### ⑤ 悬停顶出：4px + 变色 → 8px + 辉光 + 全场让位（静帧见样张）

- 现状：`POP_OUT = 4`（QuickMenuPopup.vue 脚本常量）径向外顶，激活扇区
  primary-soft 填充 + primary 描边 + `.rim-accent` 盘缘主色弧；其余扇区不动。
- 提案：外顶 **8px**；激活扇区加**主色软辉光**（CSS `filter: drop-shadow(0 3px 6px
  var(--color-primary-glow))`，token 现值 rgba(15,139,141,.18)/rgba(66,183,180,.2)）；
  未激活扇区本底 fill alpha 降半档（"让位"），形成"选中块拿起、全场退后"的层级叙事；
  `.rim-accent` 盘缘弧废除（辉光 + 顶出已足够，留与不留见拍板 5B）。
  时长仍 `--motion-fast`，缓动改 `cubic-bezier(0.16,1,0.3,1)`（与 `.cap` 入场同款弹簧尾）。
- 涉及：`POP_OUT` 常量、`.sector` `.sector.is-active` `.rim-accent` `.cap-sector`、
  `.disc` 投影串（提案为单层 `0 12px 24px` 软影，替代现双层 1px/14px）。

### ⑥ 圆心 hub 读数区：三行文字 → 仪表核（槽位刻度环 + 类型胶囊 + 常驻收起提示）

- 现状：`.hub-face` r62 panel 面 + `.hub-ring` r68 虚线环；读数三态文字
  （label 12px / kind 胶囊 / meta 10px），idle 显"快捷菜单"标题。
- 提案：hub 改**独立浮起的仪表核**——r64 磨砂面（panel ≈85% + 顶部亮环，与盘面以
  ① 的内圈间隙分离）；**8 枚槽位刻度环**沿 r50→57 布一圈（点亮当前悬停槽，
  指针式仪表语言，接替退役的 `.ticks`）；名称升 14px/700，类型胶囊 primary ≈12% 底 +
  primary 字；**常驻底行"点按中心收起"**（现该语义只活在 `aria-label` 里，操作者看不见——
  hub=收起是 StarPie 中心核动作，值得显性教学）。虚线 `.hub-ring` 废除。
- 涉及：`.hub-face` `.hub-ring` `.hub-label` `.hub-kind` `.hub-meta` `.hub-title` +
  模板 hub 区新增刻度环 `<g>`（由 `items.length` + `active` 派生的纯呈现）；
  **`hub-hit` 的 r 与 dismiss 点击语义不动**。

## 2. 机主拍板点（逐条 A=现状 / B=提案，可对样张直接批）

| # | 差异点 | A（保留现状） | B（采纳提案） | 我的推荐与理由 |
|---|---|---|---|---|
| 拍板1 | ① 扇区间隙与花瓣化 | 0.9° 发丝缝连续玻璃盘 | ≈4.2° 间隙圆角花瓣 + 盘缘亮环 | **B**——机主原话"不舒服"的最大来源就是"一大块没缝的玻璃 + 发丝线"的模糊分界；间隙即分界，刻度线可退役 |
| 拍板2 | ② 帽带圆角 | 直角楔形 + hover 底环 | 收圆帽底环 + 圆角厚弧 + primary 软底 | **B**——但依赖拍板1 同向（一边圆角一边直角会精神分裂；若 1 选 A 则本条强制 A） |
| 拍板3 | ③ 图标座 | panel 描边井 | 磨砂浮起座 | **B**——成本低收益稳，暗色"黑洞"补丁（hover 反向抬升）一并废除 |
| 拍板4 | ④ 名称单行化 | 两行折行 | 单行 + hub 全称 | **B，但有真实取舍**——两行是 N6-F1 为"字多看不清"定的案；单行赌的是"短名上盘 + 全称进 hub"心智成立。长名条目多的话 A 更稳。样张上"浏览器"三字看不出差别，可自报两三个常用长名条目复验 |
| 拍板5 | ⑤ 悬停强度 | 4px 顶出 + 盘缘弧 | 8px 顶出 + 辉光 + 全场让位 | **B 主体 + 保留盘缘弧待定**——外顶翻倍建议配让位降透明一起要，否则 12 条目密度档下 8px 会与邻缝打架（真机验收项 3） |
| 拍板6 | ⑥ hub 仪表核 | 三行文字现状 | 刻度环 + 大读数 + 常驻收起提示 | **B**——尤其"点按中心收起"显性化；刻度环是纯增量呈现，不满意可单独摘除不伤其余 |
| 拍板7 | 总方向 | 全 A（本轮收工，N40③ 关闭） | 全 B（按 §3 批次实施） | 逐条勾完自然得出；无捆绑，A/B 混选合法 |

## 3. 实施拆分与红线

### 批次拆分（拍板后一或两笔原子提交）

- **P1 皮批（主体）**：只动 `QuickMenuPopup.vue`——`<style scoped>` 全部选择器
  （§1 所列）+ 模板呈现层（角标徽标、hub 刻度环、盘缘亮环、`.ticks` 退役）+
  脚本内观感常量（`POP_OUT`、`density` 四档值）。零逻辑变更。
- **P2 几何观感参数**：`wheelGeometry.ts` 的 `mainSectorAngles`/`capSectorAngles`
  默认 `padDeg` 0.9 → 拍板值，绘制半径口径若随 ① 收窄（74/162→80/158 仅 `mainWedge`/
  `capWedge` 调用实参）——**渲染调用方改传参，函数本体与 `WHEEL` 命中常量不动**；
  `__tests__/wheelGeometry.spec.ts` 同步改期望。P2 单独一笔，回滚只回观感。

### 红线（实施任务开工须知，逐字有效）

1. **不碰 ①② 刚落地的命中路由**：`resolveHover` / `slotOf` / `keepHalfDeg` /
   `wrap180` 的极坐标归属逻辑、`HIT_HYST=±4` 迟滞、圆心静区
   `r < R_SEC_IN-4` 清态——**一行都不许动**。缝隙变大是"绘制缝"，命中始终按
   名义角域归属（这正是 N40① 的设计红利：pad 改多大都不会打出死区）。
2. **不碰 `useWheelRingState` dwell/迟滞/钉住/取消状态机**与 120ms/180ms 时序。
3. **`WHEEL` 半径常量表是前后端 + GDI 裁剪契约**：rSecIn/rSecOut/rCapIn/rCapOut/
   rCancel/rHub 的**命中语义值不改**；若 ①② 采 B，视觉收窄只发生在
   `mainWedge`/`capWedge` 的**绘制实参**上（新建 VIS 前缀观感常量，与命中常量分名），
   Go 侧 `popupWidth=512/rCapOut=236/rCancel=244/popupMargin=20` 联动冻结。
4. 键盘/触屏通道（`moveFocus`/`:focus-visible`/`role=menu*` ARIA 语义）零回归。
5. 颜色一律 token 引用，禁裸色进组件（tokens.css 三层模型红线）。

### 已知技术风险（主动提出）

- **真透明窗上的 `backdrop-filter` 未验证**：提案的"磨砂"若真机 WebView2 在
  frameless 透明窗口上不生效（或掉帧），退路是纯半透 panel 色近似（样张 B 盘的
  实际呈现就是半透近似，观感已接近）；**不构成翻案风险**，但要在 P1 首日验证。
- **投影出圈**：提案单层投影 `0 12px 24px` 的扩散半高约 12+24×2=60px > 现 20px
  透明边距——`ClipWindowEllipse` 是"区域裁剪从命中测试中剪掉四角"，需真机确认
  投影是否被窗口边界截断出硬边；若截断，方案是投影收窄回双层现值或（动后端）
  扩窗口边距——后者触红线 3，**默认不做，接受裁切**。

## 4. 真机验收清单（P1+P2 落地后过一遍）

1. **DPI×双显矩阵**：Win11 主显 100% / 125% / 150% 各唤盘一轮；副显（不同缩放）
   跨屏唤盘——间隙/圆角/辉光是否随缩放等比（SVG viewBox 口径应天然等比，异常即
   说明有 px 裸值漏进几何路径）；投影有无硬边截断（见上风险 2）。
2. **明暗 × 色板走查**：teal/sky/iris/jade/onyx 五色板 × 明暗，重点 onyx（AAA 高对比
   色板，提案的软底 alpha 档在它上面最可能"糊成一团"）。
3. **四密度档手感**：6/8/12/14 条目各配一套真实数据唤盘——⑤ 采 B 时 12+ 条目
   8px 外顶与 ≈4.2° 缝的间距是否仍读得出"没粘上"（必要时高档位降回 4px，
   `POP_OUT` 按 `density` 分档下发即可，仍是皮批范围内）。
4. **①② 回归**：全扇面命中（含大缝隙带）零死区；圆心静区清态无闪烁；
   楔形↔帽带无缝穿越；120ms dwell 开环/180ms 迟滞收起如旧；外甩 rCancel 取消态正常。
5. **交互面**：hub 点按收起（含新常驻提示行不挡 hub-hit）、点击钉住/再点解除、
   方向键循环 + `:focus-visible` 描边在花瓣皮上仍清晰、Esc 分级收起。
6. **性能**：辉光 filter + 磨砂面在低端副显上的 pointermove 帧率目测（激活段才挂
   filter，代价应可控）。

## 5. 本件与样张的口径声明

- 样张是**单文件静态稿**：角标数字与引线是注释，不在产品里；B 盘背后的彩色底纹
  只用于演示磨砂半透（真窗口下即桌面内容）；名称、图标为演示数据。
- 样张几何由内联的同源公式现算（polar/wedgePath/帽带张角与 wheelGeometry.ts 同款），
  "现状复刻"一侧可信度等同代码，不是一张凭印象的画。
- 拍板完成后：本文 §2 勾选结果回填，§1 中被否条目划除，另起实施任务按 §3 批次干；
  实施与验收结论续写进 PLAN_REMAINING_WORK 的 N40 行。
