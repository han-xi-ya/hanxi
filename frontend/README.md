# Hanxi 前端工程 (Frontend)

Hanxi 前端基于 **Vue 3 + TypeScript + Vite** 构建，配合 Wails v3 实现与 Go 后端的类型安全 RPC 通信与事件驱动。界面为**自研工作台设计系统**——CSS 变量设计 token + 各视图 scoped 样式，**不使用任何第三方 UI 组件库、不引入 vue-router、不引入 Pinia**（跨视图共享状态用 `composables/` 里的模块级单例 `ref`）。

> 架构规范与重构蓝图见仓库根 [`docs/FRONTEND.md`](../docs/FRONTEND.md)；视觉/交互设计语言权威见 `.claude/skills/hanxi-workbench-ui/references/design-system.md`。

---

## 🛠️ 技术栈与架构

- **框架**：Vue 3（`<script setup lang="ts">` 单文件组件）
- **语言 / 类型**：TypeScript + `vue-tsc`（`strict` 开启，但 `noImplicitAny:false`、`allowJs+checkJs`）
- **构建工具**：Vite 8 + `@vitejs/plugin-vue`
- **后端通信**：`@wailsio/runtime` + `frontend/bindings/`（由 `wails3 generate bindings` 自动生成的类型化 JS 封装）
- **路由**：**手写**——`App.vue` 内 `CORE_VIEWS`（route→component 映射）+ `<component :is>` + `KeepAlive`
- **状态**：**无状态库**——`composables/useToast.ts`、`useNotification.ts` 用模块级 `ref` 单例
- **样式**：**无 CSS 框架**——`App.vue` 全局 `:root` 设计 token + 各 `.vue` 自带 `<style scoped>`
- **运行时依赖**：仅 `vue`、`@wailsio/runtime`、`qrcode`

---

## 📁 目录结构（现状）

```text
frontend/
├─ bindings/                 # Wails v3 自动生成的 Go 结构体与 RPC 调用封装（DO NOT EDIT）
│  └─ hanxi/internal/        # app / extapi / modules(各业务模块) / notify / settings ...
├─ public/                   # 静态资源 + style.css（极简 reset）+ index.html 引用
├─ src/
│  ├─ App.vue                # 应用外壳：手写路由、侧边栏导航、模块懒激活门禁、全局事件桥、:root 设计 token
│  ├─ main.ts                # createApp(App).mount('#app')
│  ├─ views/                 # 各模块业务视图（每个后端模块一 *View.vue，含 Home/Logs/Settings/About 等系统页）
│  ├─ components/            # 复用组件
│  │  ├─ ConfirmDialog.vue           # 危险操作二次确认（Teleport+焦点陷阱+aria，可访问性标杆）
│  │  ├─ NotificationToast.vue       # 全局通知浮层（消费 useNotification 单例）
│  │  ├─ NotificationDrawer.vue      # 通知中心抽屉
│  │  ├─ FrpcProjectEditor.vue       # frpc 项目配置编辑器
│  │  ├─ FrpcVersionsTab.vue         # frp 版本矩阵下载
│  │  └─ envcheck/                   # 开发环境检测子组件（OfficialVersionsPanel / PackageManagerUpgradeHint / NpmToolActions）
│  ├─ composables/           # useToast / useNotification（模块级单例，事实上的共享状态层）
│  └─ utils/                 # errors.ts：getErrorMessage 统一异常转字符串
└─ package.json
```

> 说明：目录组织仍在演进中——组件多为各视图就近 `import`、无 barrel；视图普遍偏大、跨视图样式/逻辑存在重复。收敛方向与分阶段计划见 [`docs/FRONTEND.md`](../docs/FRONTEND.md)。

---

## 🚀 本地开发与构建

```powershell
# 安装依赖（根目录 .npmrc 有 minimum-release-age 供应链策略）
npm install

# 类型检查
npm run typecheck

# 生产构建（含 vue-tsc 类型检查）
npm run build
```

> **完整的桌面端开发请在项目根目录运行 `task dev`（Wails 热重载），构建/发布运行 `task build`**。前端 typecheck + build 已被根 `task check` 与 CI 作为硬门校验；`frontend/bindings/` 生成物由 CI 的 `verify:bindings` 守护不得漂移。
