# Hanxi 开发里程碑与执行现状

> **版本**：v1.0  
> **更新日期**：2026-08-24  
> **目标平台**：Windows 10 (22H2+) / Windows 11 x64  

---

## 1. 里程碑完成情况总览

| 里程碑 | 核心目标 | 状态 | 交付核心 |
|---|---|:---:|---|
| **M0: 框架与平台原语** | Wails v3 架构跑通，Windows 原生底层原语封装（JobObject/IP Helper/DPAPI） | 🟢 已达成 | `internal/platform/windows`、`internal/extapi` 骨架 |
| **M1: 基础服务与持久化** | 便携路径检测、配置原子写落盘、slog 凭据自动脱敏、托盘与开机自启 | 🟢 已达成 | `internal/settings`、`internal/logging`、`internal/app` |
| **M2: 网络诊断与局域网** | 出口 IP/IPv6 查询、网卡拓扑透视、ICMP Ping、Traceroute、LAN 设备发现 | 🟢 已达成 | `internal/modules/publicip`、`internal/modules/lan` |
| **M3: 端口查杀与安全提权** | IP Helper 端口占有表、进程指纹防误杀复核、Windows 原生 UAC 提权 | 🟢 已达成 | `internal/modules/portkill`、UAC 提权模式 |
| **M4: frpc 多实例工具** | 多实例并发、TOML生成、版本管理与重试、DPAPI 凭据加密、连接状态嗅探 | 🟢 已达成 | `internal/modules/frpc` |
| **M5: 扩展生态与工具套件** | 微信机器人助手、Gonmap 端口服务指纹扫描、内置日志查看器 | 🟢 已达成 | `modules/wechat`、`modules/portscan`、日志查看器 |
| **M6: 极致优化与便携交付** | 按需懒加载零开销架构、关闭最小化托盘常驻、单二进制构建 | 🟢 已达成 | `bin/hanxi.exe`、零内存泄漏保证 |

---

## 2. 核心功能验收清单

### 2.1 frpc 穿透与实例管理 (M4)
- [x] 多实例独立并发运行与状态隔离
- [x] Windows JobObject 进程树自动绑定与防孤儿进程清理
- [x] Windows DPAPI 本地 Token 凭据硬件加密存储
- [x] frpc 运行时临时 TOML 停机即时擦除
- [x] 控制台输出流实时特征词嗅探（精确反映连通/认证失败/重连状态）
- [x] `frp://` 协议链接一键导出与导入
- [x] 批量端口区间导入规则生成
- [x] 官方 GitHub Releases / 镜像拉取，SHA256 校验与 3 次重试机制

### 2.2 系统与网络工具 (M2, M3, M5)
- [x] 微信机器人助手（二维码扫码登录、AES 加密通信、长轮询、图文/文件推送）
- [x] 端口扫描与服务指纹探测（高并发探测、Gonmap 协议与 Nginx/Redis/MySQL 指纹识别）
- [x] 端口占用快速定位与三级安全释放（原生 UAC 提权支持）
- [x] 局域网活跃设备扫描（CIDR 探测、MAC 匹配、OUI 厂商识别）
- [x] 公网 IPv4/IPv6 探测、网卡拓扑透视、ICMP Ping 统计、Traceroute 路由追踪
- [x] 内置日志文件查看器与历史清理

### 2.3 平台体验与底层健壮性 (M0, M1, M6)
- [x] Windows 系统托盘常驻（双击唤醒、菜单交互、安全彻底退出）
- [x] 关闭主窗口自动最小化到托盘配置
- [x] Windows 注册表开机自启动管理
- [x] 绿色免安装 Portable 模式（检测 `data/` 目录）
- [x] `extapi` 按需懒加载架构（未启用模块 0 内存与 0 协程占用）
