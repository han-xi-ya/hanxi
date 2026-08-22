# HubKit 单体零开销懒加载架构改造方案

## 一、 方案概述与目标

针对 HubKit 工具箱功能不断增多的现状，本方案采用 **「单体内建 + 严格生命周期懒加载」** 模式。在不改变单二进制文件（绿色免安装、零杀毒报毒）的前提下，实现**功能按需加载、物理级内存回收、开箱即用的插件化体验**。

### 核心指标
- **冷启动初始内存**：从当前 ~80MB 压降至 **18MB ~ 25MB**（仅运行 Wails 内核与主框架）。
- **零开销保障 (Zero Allocation)**：用户未启用或未点击的功能，**0 协程常驻、0 缓冲区分配、0 定时器占用**。
- **动态热启停**：在设置页停用某模块，立即销毁内部对象、停止后台协程，并主动通知系统回收内存。

---

## 二、 详细技术改造设计

```
                    ┌──────────────────────────────────────────┐
                    │               用户交互操作               │
                    └────┬────────────────────────────────┬────┘
                         │ 1. 首次点击进入页面            │ 2. 设置页禁用模块
                         ▼                                ▼
┌───────────────────────────────────────┐  ┌──────────────────────────────────────┐
│        extapi.Registry 调度中心       │  │        extapi.Registry 调度中心      │
│                                       │  │                                      │
│  [检测状态]                            │  │  [触发销毁]                          │
│  IsInitialized == false ?             │  │  1. module.OnDestroy()               │
│    -> 调用 module.OnInit()            │  │     - cancel() 终止协程              │
│    -> 分配缓存、初始化连接            │  │     - 关闭 Socket/文件句柄           │
│    -> 状态标为 Initialized            │  │     - 清空所有 Map 与切片            │
│                                       │  │  2. runtime.GC()                     │
│                                       │  │  3. debug.FreeOSMemory() 归还操作系统│
└───────────────────────────────────────┘  └──────────────────────────────────────┘
```

### 1. 核心生命周期规范升级 (`internal/extapi/module.go`)

现有 `Module` 接口补充完整的生命周期钩子：

```go
package extapi

import "context"

// ModuleLifecycle 定义模块的完整生命周期
type Module interface {
    ID() string
    Title() string
    Description() string
    Route() string
    Icon() string
    Level() ModuleLevel
    
    // --- 懒加载生命周期钩子 ---
    // OnInit 在模块首次被用户激活/使用时调用（分配内存、初始化服务）
    OnInit(ctx context.Context) error
    // OnDestroy 在模块被用户停用/应用退出时调用（关闭协程、释放内存）
    OnDestroy() error
    // IsInitialized 查询模块当前是否已分配运行时资源
    IsInitialized() bool
}
```

---

### 2. 调度注册中心改造 (`internal/extapi/registry.go`)

引入状态互斥锁与按需激活机制，并提供系统内存回收兜底：

```go
package extapi

import (
    "context"
    "runtime"
    "runtime/debug"
    "sync"
)

type ModuleWrapper struct {
    Module      Module
    Enabled     bool
    initialized bool
    mu          sync.Mutex
}

type Registry struct {
    mu      sync.RWMutex
    modules map[string]*ModuleWrapper
}

// EnsureActive 确保模块已按需完成初始化（在前端路由进入或 API 被调用时触发）
func (r *Registry) EnsureActive(moduleID string) error {
    r.mu.RLock()
    wrapper, ok := r.modules[moduleID]
    r.mu.RUnlock()
    if !ok || !wrapper.Enabled {
        return nil
    }

    wrapper.mu.Lock()
    defer wrapper.mu.Unlock()

    if !wrapper.initialized {
        if err := wrapper.Module.OnInit(context.Background()); err != nil {
            return err
        }
        wrapper.initialized = true
    }
    return nil
}

// SetEnabled 停用模块时，强制执行析构与内存回收
func (r *Registry) SetEnabled(moduleID string, enabled bool) error {
    r.mu.Lock()
    wrapper, ok := r.modules[moduleID]
    r.mu.Unlock()
    if !ok {
        return nil
    }

    wrapper.mu.Lock()
    defer wrapper.mu.Unlock()

    if !enabled && wrapper.initialized {
        // 1. 调用模块自身析构
        _ = wrapper.Module.OnDestroy()
        wrapper.initialized = false
        
        // 2. 主动触发 Go 垃圾回收并将空闲内存交还操作系统
        go func() {
            runtime.GC()
            debug.FreeOSMemory()
        }()
    }
    wrapper.Enabled = enabled
    return nil
}
```

---

### 3. 现有业务模块适配（以 `wechat` 与 `portscan` 为例）

#### (1) 微信机器人模块改造 (`internal/modules/wechat/`)
- **改造前**：启动即开辟 Client，并自动拉起 `getupdates` 长轮询协程（即使不用也在后台占资源）。
- **改造后**：
  - `OnInit()`：读取配置，只有配置了有效 Token 时才按需启动 Client。
  - `OnDestroy()`：停止 Listener 协程，清空消息历史与内存中的图片缓存。

#### (2) 端口扫描模块改造 (`internal/modules/portscan/`)
- **改造前**：初始化即分配扫描配置缓存。
- **改造后**：
  - `OnInit()`：轻量注册。
  - `OnDestroy()`：如果扫描仍在进行，立即触发 `cancel()` 中断扫描，清空扫描结果切片。

---

### 4. 前端协同与状态联动 (`frontend/src/`)

1. **路由守卫与按需激活动作**：
   在 `App.vue` 切换视图时，增加轻量激活通知，通知后端初始化对应服务。
2. **设置页即时生效**：
   在 `SettingsView.vue` 中切换禁用开关，左侧导航栏即刻移除，后端即刻释放内存，无需重启应用。

---

## 三、 改动时间与工期评估

本方案对现有代码侵入性低，重构难度可控，预计总工作量为 **1.5 ~ 2 个工作日**：

| 阶段 / 任务项 | 具体改动范围 | 预计工时 | 复杂度 |
| :--- | :--- | :--- | :--- |
| **Phase 1: 核心协议层重构** | 1. 改造 `internal/extapi/module.go`<br>2. 改造 `internal/extapi/registry.go`<br>3. 在 `internal/app/service.go` 中提供 `ActiveModule` 接口 | 0.5 天 | 低 |
| **Phase 2: 现有模块生命周期适配** | 适配现有 6 个内置模块：<br>- `frpc`、`versions`<br>- `portscan`、`lan`<br>- `wechat`、`portkill`<br>实现各模块的 `OnInit()` 与 `OnDestroy()` | 0.5 ~ 0.8 天 | 中 |
| **Phase 3: 前端路由与设置页联动** | 1. `App.vue` 路由切换时调用按需激活<br>2. `SettingsView.vue` 停用逻辑联动<br>3. 重新生成 Wails 绑定 | 0.3 天 | 低 |
| **Phase 4: 内存压测与回归验证** | 1. 验证冷启动内存是否稳定在 20MB 左右<br>2. 压测模块开启 -> 运行 -> 停用 -> 内存是否 100% 释放<br>3. 检查是否有并发竞争 (Race Check) | 0.4 天 | 低 |
| **合计总工期** | **完整落地 + 验证交付** | **~ 2 工作日** | **整体中偏低** |

---

## 四、 复杂度与潜在风险点评估

### 1. 复杂度评级：**低 ~ 中等 (Low-Medium)**
- **代码重构量小**：不需要推翻现有架构，不需要写复杂的跨进程 IPC/RPC 协议。
- **完全兼容现有 UI**：前端页面无需做微前端改造，继续保持单页 Vue 3 的极佳渲染流畅度。

### 2. 潜在风险点与防范措施

| 风险点 | 表现场景 | 防范措施 |
| :--- | :--- | :--- |
| **未初始化即调用 API** | 用户未通过 UI 进入页面，直接通过快捷键或外部调用了模块 API | 在每个 Service 的公共方法入口，统一先调用 `s.registry.EnsureActive(moduleID)` 保证自动懒初始化 |
| **并发读写竞争 (Race)** | 多个协程同时触发同一个模块的 `OnInit` 或 `OnDestroy` | 使用 `sync.Mutex` 保证单一模块生命周期转换的串行化与幂等性 |
| **Goroutine 残留泄露** | 模块停用后后台协程仍在运行导致无法 GC | 每个模块统一使用带 `context.CancelFunc` 的上下文，`OnDestroy` 时必须显式 `cancel()` |
| **Windows 内存不立即释放** | Go 默认 GC 后不会立刻把物理内存退还给 Windows OS | 在 `SetEnabled(false)` 后显式调用 `debug.FreeOSMemory()` 强制归还物理内存 |

---

## 五、 改造收益总结

1. **立竿见影的性能提升**：冷启动内存从 ~80MB 降至 **~20MB**，停用不用的模块直接腾出内存。
2. **极简纯净的界面**：用户在设置里关掉的功能，左侧菜单完全消失，打造个人专属定制网络工具箱。
3. **架构更易维护**：后续每新增一个新工具，只需继承 `Module` 并实现两个生命周期方法即可，各模块之间完全互不干扰。
