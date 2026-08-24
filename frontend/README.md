# HubKit 前端工程 (Frontend)

HubKit 前端基于 **Vue 3 + TypeScript + Vite + TailwindCSS** 构建，配合 Wails v3 实现与 Go 后端的类型安全 RPC 通信与事件驱动。

---

## 🛠️ 技术栈与架构

- **UI 框架**：Vue 3 (`<script setup lang="ts">`) + Vue Router
- **样式方案**：TailwindCSS + 自定义 Modern Clean 浅色主题
- **构建工具**：Vite + `@wailsio/runtime`
- **类型系统**：TypeScript + `vue-tsc`
- **后端绑定**：`frontend/bindings/`（由 `wails3 generate bindings` 自动生成）

---

## 📁 目录结构

```text
frontend/
├─ bindings/            # Wails v3 自动生成的 Go 结构体与 RPC 调用绑定
│  └─ hubkit/internal/  # 包含 app, extapi, modules (frpc, wechat, lan, portscan, portkill, publicip 等)
├─ src/
│  ├─ components/       # 可复用业务组件
│  │  ├─ FrpcProjectEditor.vue   # frpc 项目配置编辑器（支持批量端口、高级参数、实时 TOML 预览）
│  │  ├─ FrpcShareModal.vue      # frp:// 协议分享与导入模态框
│  │  ├─ ModuleCard.vue          # 首页功能模块磁贴卡片
│  │  └─ ...
│  ├─ views/            # 核心业务视图
│  │  ├─ HomeView.vue            # 仪表盘首页（快捷入口、模块状态、系统快捷启动）
│  │  ├─ FrpcProjectsView.vue    # frpc 项目多实例管理、运行状态徽标、实时日志抽屉
│  │  ├─ FrpcVersionsView.vue    # frp 官方版本矩阵下载、校验与本地导入
│  │  ├─ WechatBotView.vue       # 微信机器人助手（扫码登录、长轮询监听、消息推送）
│  │  ├─ PortScanView.vue        # 端口高并发扫描与 Gonmap 服务指纹识别
│  │  ├─ PortKillView.vue        # 本地端口占用查杀与 UAC 提权释放
│  │  ├─ LanScannerView.vue      # 局域网活跃设备扫描与 MAC/OUI 厂商识别
│  │  ├─ PublicIpView.vue        # 公网 IP/IPv6、网卡拓扑透视、Ping 统计与 Traceroute
│  │  ├─ LogsView.vue            # 应用全局运行日志查看器
│  │  └─ SettingsView.vue        # 设置中心（常规开机自启/托盘常驻、模块按需启停）
│  ├─ App.vue           # 根布局与动态侧边栏导航
│  └─ main.ts           # 应用入口
└─ package.json
```

---

## 🚀 本地开发与构建

### 安装依赖

```powershell
npm install
```

### 独立类型检查与构建

```powershell
# 运行 TypeScript 类型检查与 Vite 生产打包
npm run build
```

> **注意**：完整的桌面端开发请在项目根目录运行 `task dev`，构建发布请在项目根目录运行 `task build`。
