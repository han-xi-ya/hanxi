# Hanxi 前端工程 (Frontend)

Hanxi 前端是跑在 Wails v3 桌面壳（WebView2）内的**本地 SPA**，基于 **Vue 3 + TypeScript + Vite** 构建，与 Go 后端只经 `frontend/bindings/` 的类型化 RPC 与事件通信。界面为**自研工作台设计系统**——CSS 变量设计 token + 全局原子样式层 + 各视图 `<style scoped>`，**无第三方 UI 组件库、无 CSS 框架、不引入 vue-router、不引入 Pinia**（跨视图共享状态用 `composables/` 的模块级单例 `ref`）。

> **权威分层**（本 README 只是入口纲要，不维护细节与数字）：
> - 架构现状、迁移铁律、重构蓝图与设计 token 条款：仓库根 [`docs/FRONTEND.md`](../docs/FRONTEND.md)
> - 视觉/交互设计语言权威：`.claude/skills/hanxi-workbench-ui/references/design-system.md`
> - 主题色板档案（五套已落地）与逐值实算：[`docs/design/themes/README.md`](../docs/design/themes/README.md)
> - **计数类事实唯一出处**：[`docs/FACTS.md`](../docs/FACTS.md)（`scripts/docsfacts` 机器现推，本文件不写数）

## 🛠️ 技术栈与架构要点

- **框架 / 语言**：Vue 3 `<script setup lang="ts">` 单文件组件；TypeScript + `vue-tsc`（`strict` 开启，`noImplicitAny:false`、`allowJs+checkJs`）
- **构建 / 测试**：Vite 8 + `@vitejs/plugin-vue`；Vitest + `@vue/test-utils`（happy-dom，Wails 原生层以 `vi.mock` 打桩）
- **后端通信**：`@wailsio/runtime` + `frontend/bindings/`（`wails3 generate bindings` 自动生成，**禁止手改**，由 `task verify:bindings` 与 CI 守护）
- **运行时依赖**：`vue`、`@wailsio/runtime`、`@vueuse/core`（胶水层）、`qrcode`
- **路由**：无 router——`constants/navigation.ts` 为 route→组件/图标/标题单一来源（`defineAsyncComponent` 全异步）+ `App.vue` 薄编排（门禁 / `KeepAlive` / 事件桥）
- **状态**：无状态库——`useToast`/`useNotification`/`useTheme`/`useModuleCatalog`/`useOperations` 等同风格模块级单例（只缓存后端投影、事件到达只重拉，详见 docs/FRONTEND.md §2/§12）
- **样式**：无 CSS 框架——`src/styles/` 四件全局层：`tokens.css`（语义 token 与色板唯一真相）、`fonts.css`（字面栈与字体档位，N36/N40）、`base.css`（reset/基线）、`components.css`（`:where()` 全局原子类）；主题三轴 `data-theme` × `data-accent` × `data-font`，机制条款见 docs/FRONTEND.md §7.2
- **按钮形制速查（易踩，详条款见 docs/FRONTEND.md §7.3）**：`.btn-primary` 是全站唯一主钮档、形制为"蓝洗"（2026-10-20 四轮返工定稿），**禁造行内子档**；弱一档需求走 `.btn-secondary` / `.btn-accent-outline` / `.btn-ghost` / `.link-button`

## 📁 目录速览（现状）

```text
frontend/
├─ bindings/                 # Wails v3 自动生成 Go↔JS 封装（DO NOT EDIT）
├─ public/                   # 静态资源（style.css 极简 reset）
├─ src/
│  ├─ App.vue                # 应用外壳（薄编排：路由/门禁/事件桥/弹窗壳）
│  ├─ main.ts                # createApp + 四份 styles/ 全局样式导入
│  ├─ styles/                # tokens.css / fonts.css / base.css / components.css
│  ├─ constants/             # navigation.ts（路由单一来源）· status.ts（状态词表，
│  │                         #   含 Go↔TS contract-enum 门禁）· icons/appIcons/runtimeIcons · snapshotLabels
│  ├─ composables/           # 模块级单例投影家族 + 视图级 useXxx
│  ├─ components/            # ui/ 无业务原子件 · shell/ 双栏外壳 · modules/ · tool/ ·
│  │                         #   巨型视图拆分后的族目录（envcheck/everything/wsl/…）
│  ├─ views/                 # 后端模块各一视图 + 系统页；settings/ 为设置页八分区
│  └─ utils/                 # errors / format / paste 等领域工具
└─ package.json
```

各目录逐件清单与职责以 [`docs/FRONTEND.md`](../docs/FRONTEND.md) §2（现状架构）为准——本表只作认知地图，避免两处维护互相漂移。

## 🚀 本地开发与构建

```powershell
# 安装依赖（供应链实际防线 = 提交的 package-lock.json + CI npm ci；
# 更新依赖的 7 天冷却用法见 frontend/.npmrc 注释）
npm install

npm run typecheck   # vue-tsc --noEmit
npm run lint        # ESLint flat config（--max-warnings 阈值门禁）
npm run test        # Vitest 全量用例
npm run build       # typecheck + 生产构建（含 vue-tsc）
```

> **完整桌面端开发请在项目根目录运行 `task dev`（Wails 热重载），构建/发布运行 `task build`**。前端 typecheck + build 已被根 `task check` 与 CI 作为硬门校验；`frontend/bindings/` 生成物由 `verify:bindings` 守护不得漂移。
>
> 环境提示：Git Bash 下 `npm run` 存在坏 shim 坑（见 `docs/TROUBLESHOOTING.md` 相关条目），可 `node node_modules/<pkg>/...` 直调绕过；Windows 终端与根 `task` 入口不受影响。
