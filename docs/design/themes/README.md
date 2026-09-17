# Hanxi 主题配色提案 v2 · mica 通透系（已落地）

> 状态：**已接入应用**。双轴机制（`data-theme` 明暗 × `data-accent` 色板）与五套色板
> （teal 默认回退 + sky/iris/jade/onyx）已在 `frontend/src/styles/tokens.css` 生效，
> 设置页「外观主题」可切换并持久化（后端 `AppSettings.Accent`，便携随 `data/` 迁移）。
> 本目录其余文件为设计过程档案：`preview.html` 可视化评审稿（v2.1 定稿值）、
> `tokens/*.css` + `*-report.md` 为三套色板深化实算档案、`*-v1.*` 为被否的 v1 考古、
> `gen-palettes-v2.mjs` 为 OKLCH 生成器。**数值唯一真相 = `tokens.css`**。

## 1. 定稿一览（v2.1 = 并行精修后的值）

| 色板 | 参照 | 浅主色 | 深主色 | 特征 |
|---|---|---|---|---|
| `teal` 青壳 | 现状 | `#0f8b8d` | `#42b7b4` | 默认回退（`--color-primary` 现值原样即 teal，行为零变化） |
| `sky` 碧空 | Fluent mica | `#0064b5` | `#4ebdf7` | 霜白蓝灰面 + 天蓝强染色选中行；soft 由 .982→.979 补感知台阶 |
| `iris` 鸢尾 | Arc/Graf | `#6741ca` | `#bba9fa` | 薰衣草纸面；主色旋紫 290° 与 information 蓝拉开 27°；深选中行压暗使行上主色字 4.58 |
| `jade` 青瓷 | 品牌进化 | `#006869` | `#5dd1cf` | 墨青釉/松石夜釉；色相钉回品牌正色相 h196/194（现状 teal 实测 h193–197），与 positive 绿拉距 33°/31° |
| `onyx` 曜石 | AAA 无障碍 | `#0649b8` | `#79b7ff` | 全库唯一覆写 `--state-*`（含 soft）；正文 ≥17:1、弱注 ≥5.4:1、行上裸状态字 ≥4.88 |

三套精修的完整改动理由见 `tokens/` 对应 css 头注释与 report；全部配对比 preview 起点稿更严（jade 修出起点 5 处不达标、onyx 修出深选中行裸字掉线、sky 补台阶）。

## 2. 方法论（沿用）

OKLCH 感知色阶标定（生成器可复现）、mica 深色石墨面（L≈0.21 非纯黑）、Fluent 强染色选中行、
深模式"亮色+深字"、状态色跨色板恒定（onyx 例外）、终端色板永不反相。
对比度：五套浅深全过（AA 底线，onyx 按 AAA），`preview.html` 浏览器打开即可复算审计。

## 3. 已固化的使用纪律（视图层，来自精复审稿共识）

1. 强染色 `--surface-selected` 行内**禁放** muted/subtle 小字（各板实测 3.6–3.9 区段）；
2. 选中行上表达状态语义**必须走 chip 软底**，禁裸着色字（浅模式族债下裸字不可读）；
3. 裸 hex 仍只准出现在 `tokens.css`；设置页色板预览圆点是登记在 `ThemeSection.vue` 注释里的唯一豁免（预览点必须独立于当前主题）。

## 4. 外壳层 --surface-chrome（2026-09-17 追加，已落地）

DWM 标题栏与双栏导航共用一层壳底（浅色 ≈ 主色混白 9%，深色 = page/panel 中点），
内容区自 page→panel 浮起，左半 + 顶部连成 Fluent 式"壳"。各色板两态共 10 组值在
`tokens.css`；原生窗框侧的镜像表在 `internal/platform/windows/darkmode.go`——
**改 chrome/text 值必须两处分头同步**（表旁有纪律注释）。壳上导航 hover 用专用
`--surface-chrome-hover`（浅 = 壳底混 7.5% 文字色 / 深 = 混 8.5% 白），不得借内容层
`--surface-hover`（壳面上台阶不足）。链路：设置页/侧栏切换 → `useTheme` →
`SetWindowDarkMode(dark, accent)` → DWM caption/text；启动按持久化值预应用防白闪。
预览页 mock 顶部已画假标题栏直观看壳效果（`preview.html`）。

## 5. 遗留议题（待拍板，非阻塞）

- ~~R1 全族共债~~ **已修**（2026-09-17）：浅模式绿/黄/红深一档（`#067d4d`/`#955e00`/`#b33736`），危险软底提亮 `#fdf0ee` 补偿，chip 文字 4.54–5.37 全过 AA；色相不动、深模式与 onyx（自带覆写）不受影响。信息蓝 4.47 贴线放行。
- **真机目视**：构建与单测全绿（vue-tsc + vite + 831 用例 + go build/vet/test），但未运行 Wails 实机核 sky/jade 强染色选中行的观感。
- **提交拆分**：本次改动含 bindings 重生成（混有既有漂移与 ocr WIP），由主线定夺分组。
