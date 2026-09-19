# Hanxi 官方功能模块按需安装/卸载与 Monorepo 多产物改造计划

> **文档状态**：已批准方向的执行计划
> **2026-09-19 范围裁剪（[ADR-0003](../adr/ADR-0003-scope-cuts-for-personal-nas-first-tool.md)）**：Phase 1–3 与 Phase 5 的托管模块去重复已落地（版本面 20 家委托 artifact，其中 18 家的 instance 面同时委托 supervisor）；**签名 Catalog、声明式 manifest、key 轮换/撤回与 Phase 6 多产物发布的 DoD 对当前受众整体 N/A/缓行**（自用 + NAS 同步定位，供应链完整性由 HTTPS + 上游官方摘要必检承担）。相关章节保留为"若建立真实发布渠道时重开"的设计储备。
> **适用范围**：Hanxi 官方维护、官方审核、官方发布的功能模块与托管模块  
> **明确排除**：第三方插件市场、未知发布者代码、任意脚本扩展  
> **当前基线日期**：2026-09-18  
> **总工作量**：P50 **108 人日** / P80 **172 人日**  
> **用户价值 MVP**：**Phase 0–2**  
> **真正物理拆包**：**Phase 6**；此前以逻辑安装态、统一生命周期和声明式官方托管模块为主

---

## 1. 执行摘要

Hanxi 当前是“模块化单体 + 静态 Wails Service 注册 + 模块运行资源按需激活 + 第三方工具托管”。41 个模块均由 `internal/app/app.go` 静态装配，前端页面统一构建进 `frontend/dist`，最终由 `embedassets.go` 嵌入 `hanxi.exe`。现有“启用/停用”只影响导航、调用门和运行资源，不代表模块已经安装或从磁盘卸载。

本计划首先解决的不是“开放插件生态”，也不是立即把普通页面拆成动态 Vue Bundle，而是用户面对功能过多时的可理解、可选择和可治理问题：

1. 用户能在独立的“模块中心”看到官方模块是否可用、是否安装、是否启用、是否运行、是否健康；
2. 用户只启用和安装自己需要的官方功能及托管工具；
3. 宿主统一治理下载、校验、安装、进程、操作、回滚和卸载；
4. 普通托管模块优先从“每个模块复制一套管理代码”演进为“官方签名声明 + 共享托管内核”；
5. 复杂自建功能包最后才采用官方 sidecar 等物理拆包形态，首个样本为 OCR，第二个为 FileShare，WSL 后置；
6. 仓库逐步演进为可构建宿主、官方清单、官方托管模块包、复杂 sidecar 和发布索引等多产物的 Monorepo。

本计划的核心顺序是：

> **先把“装没装、开没开、跑没跑、是否健康”讲清楚，再统一调用门和生命周期；先平台化托管内核，再声明式迁移普通托管模块；复杂代码包和物理拆包后置。**

### 1.1 阶段主线

- **Phase 0：清点契约。** 建立模块分类、状态、数据、权限、操作和依赖基线。
- **Phase 1：逻辑安装态 + 独立模块中心。** 代码仍在宿主中，但用户可按“已安装/未安装”的产品模型管理功能集合。
- **Phase 2：统一调用门与生命周期。** 所有入口遵守 Enabled/Installed/Acquire/Operation；至此形成用户价值 MVP。
- **Phase 3：共享托管内核。** 建设 ArtifactManager、ProcessSupervisor 和 Operation，以 MarkerOn、Rufus 验证。
- **Phase 4：声明式官方托管模块 + 签名目录。** MarkerOn、Rufus、QuickLook 成为首批试点。
- **Phase 5：批量迁移普通托管模块。** 大规模减少宿主内重复托管代码，但不强拆复杂自建业务。
- **Phase 6：官方复杂功能包/sidecar。** OCR 首个、FileShare 第二、WSL 后置；至此真正实现复杂功能物理拆包。
- **Phase 7：CI/Release 治理。** 收口多产物、签名、兼容、灰度、撤回、回滚和文档治理。

### 1.2 两类“按需”必须区分

| 类型 | 用户价值 | 何时实现 |
| --- | --- | --- |
| 逻辑按需 | 模块中心管理、未安装/已安装语义、导航减负、禁用后不可调用、运行资源按需分配 | Phase 1–2 |
| 产物按需 | 模块程序、资源或托管资产独立下载，卸载后真实释放对应磁盘内容 | 普通托管模块 Phase 4–5；复杂功能包 Phase 6 |

Phase 1 的“未安装”是产品层逻辑安装态：模块代码仍随宿主发布，但不进入导航、不允许业务调用、不初始化资源。UI 和文档必须明确这一阶段不会减少 `hanxi.exe` 体积。只有独立托管资产或 Phase 6 sidecar 从磁盘移除时，才可宣称释放对应程序空间。

### 1.3 Monorepo 原则

> **源码同仓不等于产物同包。**

Monorepo 用于原子修改宿主、共享内核、官方声明、sidecar 和契约测试；多产物发布用于按需下载、独立更新、回滚和卸载。普通托管模块即使声明与宿主同仓，也可以只发布小型声明包并按需获取上游资产；复杂功能即使源码同仓，也可以在 Phase 6 构建为独立官方 sidecar。

---

## 2. 需求与非目标

### 2.1 核心需求

| 编号 | 需求 | 验收口径 |
| --- | --- | --- |
| R1 | 用户可在独立模块中心管理官方功能 | 首页仍是工作台，模块中心有独立入口 |
| R2 | 区分安装、启用、运行、健康/版本四类状态 | 不再用单一 `Enabled` 推断全部状态 |
| R3 | 所有模块入口经过统一调用门 | 未安装或停用模块不能从 RPC、托盘、热键、MCP 等旁路执行 |
| R4 | 生命周期和在途操作统一 | 初始化、Acquire、Operation、取消、停用、退出具有统一语义 |
| R5 | 托管软件能力平台化 | 下载、校验、解包、版本、进程、操作不再在每个模块重复实现 |
| R6 | 普通官方托管模块可声明式交付 | 声明不得包含任意脚本或任意命令执行 |
| R7 | 安装、更新、卸载可恢复 | journal 事务失败不破坏上一个可用版本 |
| R8 | 默认保留用户数据 | 删除程序、缓存、用户数据和被托管软件分别确认 |
| R9 | 官方供应链可验证 | 索引签名、包摘要、可执行文件验证、撤回机制 |
| R10 | 复杂功能可在成熟后物理拆包 | OCR、FileShare sidecar 验证后再评估其他模块 |
| R11 | Monorepo 支持多产物原子开发 | 宿主、声明、共享包和 sidecar 可在同一 PR 兼容演进 |
| R12 | 可回滚、可诊断、可撤回 | 每次操作有结构化状态、日志和恢复动作 |

### 2.2 用户需求

- 主页专注日常工作台，不被完整模块目录占据；
- 从独立模块中心选择自己需要的功能；
- 清楚模块“没安装、装了但没启用、正在运行、需要更新还是异常”；
- 不使用的功能不出现在导航、搜索、托盘和快捷入口；
- 安装或更新失败后仍可继续使用旧版本；
- 卸载官方模块时不意外删除个人数据或被管理的第三方软件；
- 能查看模块来源、权限、数据位置、磁盘占用和版本；
- 支持在线安装、官方包离线导入、版本固定和回滚；
- 复杂模块崩溃时不拖垮宿主，宿主退出时不遗留受管进程。

### 2.3 工程需求

- 保持 Windows 优先、Go、Wails v3、Vue 3/TypeScript 技术栈；
- Phase 0–5 不要求任意动态 Vue Bundle 或运行时动态 Wails Service；
- 普通托管模块优先使用宿主统一 UI 和静态受控策略；
- 不把内部 Go 接口直接作为外部稳定 ABI；
- 不使用 Go `plugin` 或进程内 DLL 插件；
- 模块安装根、用户数据根、安装包缓存和运行根分离；
- 数据根继续遵循 `docs/plans/PLAN_PATHS.md`；
- 先迁移代表样本，再批量迁移，不一次性重写 41 个模块；
- 私发 OCR 组件和其他非公开资产不得因 Monorepo 改造进入公开包。

### 2.4 非目标

本计划不包含：

1. 第三方插件市场、社区上传、付费市场或开放发布者注册；
2. 运行未知发布者代码；
3. 任意 PowerShell、批处理、JavaScript 或安装钩子；
4. Go `plugin`、进程内 DLL 插件、DLL 注入；
5. 在第一阶段建立动态微前端平台；
6. 为普通托管模块开放任意自带 Vue 页面；
7. 把 JobObject 宣传为安全沙箱；
8. 允许模块直接取得宿主 Secret Store 明文或提权句柄；
9. 为追求“插件率”强拆全部模块；
10. 改变 `hanxi.bind` 数据根裁决；
11. 自动删除模块用户数据；
12. 把逻辑未安装宣传成主程序体积减少；
13. 把 Hanxi 托管的上游软件冒充 Hanxi 自研程序；
14. 在没有真实收益数据前拆分 WSL 等高复杂模块。

---

## 3. 当前基线与差距

### 3.1 当前事实

| 维度 | 当前实现 | 证据路径 |
| --- | --- | --- |
| Go 工程 | 根目录单一 `go.mod` | `go.mod` |
| 前端工程 | 单一 npm package、单一 Vite Bundle | `frontend/package.json`、`frontend/vite.config.ts` |
| 模块装配 | 41 个模块静态构造和注册 | `internal/app/app.go`、`scripts/fixture/composition_contract.json` |
| 模块契约 | Info/Nav/Services/OnInit/OnDestroy/IsInitialized | `internal/extapi/module.go` |
| 生命周期 | Enabled、初始化、停用、in-flight drain | `internal/extapi/registry.go` |
| 前端路由 | route → Vue component 静态映射 | `frontend/src/constants/navigation.ts`、`frontend/src/App.vue` |
| 模块管理 | 首页展示已启用/未启用 | `frontend/src/views/HomeView.vue` |
| 数据根 | `hanxi.bind` 优先，否则 exe 同级 `hanxidata/` | `internal/settings/paths.go` |
| 成熟安装样板 | OCR 组件、LiteMonitor 版本管理 | `internal/modules/ocr/hosted.go`、`internal/modules/litemonitor/version/manager.go` |
| 进程治理 | 各托管模块分别实现，部分使用 JobObject | `internal/platform/windows/job.go`、`internal/modules/*/instance/` |
| Release | 单宿主 exe、portable zip、checksums | `.github/workflows/release.yml` |

当前 Composition Contract 固定 41 个模块、75 个关键事件，并包含 wechat、quickmenu、msgboard、wsl 四个预激活模块。它保护了静态单体装配，但尚不能表达逻辑安装态、官方声明包、可选 sidecar 和多产物兼容关系。

### 3.2 当前“启用”不等于“安装”

- `Enabled=false` 只表示隐藏导航和停用运行资源；
- 模块 Go 代码、Wails Service 和前端页面仍属于宿主产物；
- `Initialized=false` 只表示当前未分配运行资源；
- `LevelExternal` 是未来扩展预留，不是已实现的官方模块包；
- `defineAsyncComponent` 只延迟页面 Chunk 加载，不形成独立安装包；
- `versions/<tool>` 中的上游工具版本可删除，但提供管理能力的 Hanxi 模块代码仍在宿主中。

### 3.3 关键差距

| 差距 | 当前后果 | 目标能力 |
| --- | --- | --- |
| 安装态与启用态混合 | 用户无法理解功能是否真正可用 | 四维状态模型 |
| 首页承担模块管理 | 工作台与模块目录职责混杂 | 独立模块中心入口 |
| 调用门覆盖不完整 | RPC、托盘、热键等可能旁路 | 统一 Acquire/Operation 门 |
| 生命周期实现差异 | 停用、取消、退出残留不可统一验证 | 唯一 Registry/Supervisor 状态源 |
| 托管模块重复下载/安装/进程代码 | 修复需要复制到多个模块 | ArtifactManager + ProcessSupervisor + Operation |
| 无声明式官方托管模块 | 新增工具仍要复制完整模块 | 签名 manifest + 受控策略 |
| 安装实现分散 | 错误和回滚语义不一致 | journal 事务 |
| 无签名官方目录 | 无统一来源、兼容、撤回治理 | signed catalog |
| 单一构建产物 | 无法独立发布复杂功能 sidecar | Monorepo 多产物 |
| 数据保留语义未定义 | 容易误删或残留 | 程序/数据/缓存/上游软件分层策略 |

### 3.4 与既有插件化战略的关系

`docs/PLUGINIZATION_AND_PRODUCT_EVOLUTION.md` 的“先平台化，再外部化；先声明式，再代码化”继续作为硬约束。本计划中的声明式官方托管模块由 Hanxi 官方维护和签名，运行的是经过宿主审计的固定策略；它不是第三方 Tool Manifest 市场。Phase 6 的 sidecar 也仅面向官方复杂功能，不自动形成第三方代码插件承诺。

---

## 4. 目标架构

### 4.1 五层结构

1. **Core Host**  
   窗口、首页工作台、模块中心、设置、导航、Catalog、Registry、调用门、事务、日志、通知、数据根、签名验证、更新和恢复。
2. **Shared Managed Kernel**  
   ArtifactManager、ProcessSupervisor、Operation、标准下载源、安装策略、版本策略、进程探针和统一状态事件。
3. **Declarative Official Managed Modules**  
   官方签名 manifest 描述 MarkerOn、Rufus、QuickLook 等托管上游工具；宿主使用内建策略渲染统一 UI 并执行操作。
4. **Official Complex Function Packages**  
   OCR、FileShare 等官方复杂功能在 Phase 6 以后使用独立 sidecar、资源包或组合包；宿主保留调用门、权限和 UI 壳。
5. **Hosted Upstream Software**  
   MarkerOn、Rufus、QuickLook 等被 Hanxi 管理的上游程序。其发布者和许可证必须如实展示，不因 Hanxi 官方声明包而变成 Hanxi 自研程序。

### 4.2 普通托管模块运行路径

```text
模块中心 / 工作台入口
        │
        ▼
Module Catalog + 四维状态
        │
        ▼
统一调用门 Acquire/Operation
        │
        ├─ ArtifactManager：发现、下载、验证、安装、版本、回滚
        ├─ ProcessSupervisor：启动、探测、JobObject、唤窗、退出
        └─ Operation：进度、取消、错误、日志、恢复
                │
                ▼
        官方签名声明选择的受控宿主策略
                │
                ▼
        被托管的上游软件
```

声明只能选择宿主已经实现、经过审计和测试的策略，例如 portable zip、单文件 exe、固定 MSI 提取、进程名/互斥体/窗口探测等。manifest 不得注入任意命令、脚本或 shell 字符串。

### 4.3 复杂功能包运行路径

Phase 6 才引入官方 sidecar：

```text
宿主静态 UI / 宿主渲染 UI
        │
        ▼
Module Broker + 权限/状态/操作门
        │
        ▼
版本化本地 RPC（named pipe 或 stdio）
        │
        ▼
官方签名 sidecar（OCR / FileShare）
```

第一版不要求 sidecar 自带任意 Vue Bundle，也不要求 Wails 在运行时动态注册未知 Service。宿主可以继续编译模块管理页面和结果展示页面，通过稳定 Broker 调用独立 sidecar。这样先取得重资源、后端进程和故障隔离的物理拆包收益，避免同时建设微前端和动态 Wails 服务两个高风险平台。

### 4.4 控制平面保持内建

以下能力不可由声明或 sidecar 替换：

- 模块 Catalog、四维状态和调用门；
- ArtifactManager、ProcessSupervisor、Operation；
- journal、staging、激活指针、回滚和恢复；
- 官方签名根、公钥轮换、撤回和目录验证；
- 数据根、Secret Store、日志脱敏和诊断导出；
- JobObject、UAC、注册表、进程复核等高风险 Windows 原语；
- 首页、模块中心、设置、全局导航、通知和危险操作确认；
- 权限裁决和 Host API；
- 宿主自身更新。

---

## 5. Monorepo、数据根与产物结构

### 5.1 目标 Monorepo

Monorepo 采用渐进迁移，不先进行大规模目录搬家。目标职责结构如下：

```text
/
├─ go.work
├─ package.json                       # npm workspace 编排
├─ apps/
│  └─ hanxi-host/                     # 最终宿主应用
├─ packages/
│  ├─ go/
│  │  ├─ artifact/                    # 下载、验证、安全解包、journal
│  │  ├─ supervisor/                  # 进程、JobObject、探测、退出
│  │  ├─ operation/                   # 进度、取消、日志、错误
│  │  ├─ modulecatalog/               # 状态、声明、兼容、目录
│  │  └─ sidecarprotocol/             # Phase 6 官方 sidecar 协议
│  └─ web/
│     ├─ module-center/               # 模块中心通用 UI
│     └─ workbench-contracts/         # 工作台入口契约
├─ modules/
│  ├─ managed/
│  │  ├─ markeron/manifest.yaml
│  │  ├─ rufus/manifest.yaml
│  │  └─ quicklook/manifest.yaml
│  └─ sidecars/
│     ├─ ocr/
│     └─ fileshare/
├─ schemas/
│  ├─ managed-module.schema.json
│  ├─ official-catalog.schema.json
│  ├─ transaction-journal.schema.json
│  └─ sidecar-manifest.schema.json
├─ tools/
│  ├─ modulepack/
│  ├─ cataloggen/
│  └─ compatcheck/
├─ distribution/
│  ├─ catalog/
│  └─ policies/
└─ docs/
```

迁移纪律：

- Phase 0–3 可在现有目录中实现清晰边界，不以搬目录作为完成标志；
- Phase 4 建立 manifest/schema/tooling 的多产物骨架；
- Phase 6 为 sidecar 建立独立 `go.mod`，由 `go.work` 统一开发；
- 前端继续使用 npm；不为 Monorepo 单独引入 pnpm、Nx 或 Turborepo；
- 共享包只承载稳定重复能力，不抽取模块业务逻辑；
- 每次迁移保持全量构建和测试通过；
- 私有 OCR 资产继续走既有私发边界，不进入公开仓库或公开 Release。

### 5.2 数据根目标结构

继续使用 `hanxi.bind` 或 exe 同级 `hanxidata/` 解析出的同一数据根：

```text
hanxidata/
├─ config.json
├─ state/
│  ├─ modules/<module-id>/            # 用户配置、业务数据、迁移标记
│  ├─ module-catalog.json             # 本地状态投影
│  └─ module-data-retention.json      # 保留数据墓碑
├─ logs/
│  └─ modules/<module-id>/
├─ versions/                          # 被托管上游软件版本，兼容现有布局
├─ runtime/
│  └─ modules/<module-id>/            # pid、pipe、lease、临时数据
├─ installers/
│  ├─ managed/<module-id>/            # 托管上游安装包缓存
│  └─ official/<module-id>/           # 官方 sidecar/组件包缓存
└─ modules/
   ├─ receipts/                       # 逻辑/声明/sidecar 安装凭据
   ├─ sidecars/<module-id>/<version>/ # Phase 6 不可变版本目录
   ├─ active/<module-id>.json         # sidecar 原子激活指针
   ├─ staging/<transaction-id>/
   ├─ journals/<transaction-id>.json
   ├─ quarantine/
   └─ catalog/
      ├─ official.json
      └─ official.sig
```

要求：

- 为 `installers/`、`modules/` 等补正式 `settings.Paths` 访问器，不再由模块自行拼路径；
- `state/modules/<id>` 是用户数据，卸载默认不删；
- `versions/` 是被托管上游软件版本；
- `modules/sidecars/` 是 Hanxi 官方复杂功能代码包；
- `runtime/` 是易失数据，正常停止后清理；
- `installers/` 是缓存，不是唯一恢复来源；
- ID 必须来自经过 schema 校验的规范值。

### 5.3 多产物结构

最终发布可包含：

```text
hanxi-host-windows-amd64-vX.Y.Z.exe
hanxi-host-windows-amd64-portable-vX.Y.Z.zip
hanxi-managed-markeron-vA.B.C.hxmanaged
hanxi-managed-rufus-vA.B.C.hxmanaged
hanxi-managed-quicklook-vA.B.C.hxmanaged
hanxi-sidecar-ocr-windows-amd64-vA.B.C.hxmodule
hanxi-sidecar-fileshare-windows-amd64-vA.B.C.hxmodule
official-catalog-stable-vN.json
official-catalog-stable-vN.sig
checksums.txt
sbom-*.spdx.json
provenance-*.json
```

- `.hxmanaged`：声明、许可证、图标和可选静态元数据，不携带任意执行脚本；
- `.hxmodule`：Phase 6 官方 sidecar 包，包含 manifest、签名、逐文件摘要、后端程序、必要资源、许可证和 SBOM；
- 被托管上游软件仍按其真实来源下载并验证，不打包成 Hanxi 自研二进制；
- 源码同仓不意味着上述资产必须合并成一个下载包。

---

## 6. 四维状态模型

四个维度独立存储、独立展示，再派生用户摘要。

### 6.1 交付状态 Delivery

| 状态 | 含义 |
| --- | --- |
| `absent` | 逻辑未安装，或本机无可用声明/sidecar/所需资产 |
| `installing` | 正在安装逻辑凭据、托管资产或官方包 |
| `installed` | 已安装并有完整 receipt |
| `updating` | 更新事务进行中，旧版本尽量保持可用 |
| `removing` | 正在停止并移除 |
| `repairing` | 正在按可信清单修复 |
| `orphaned` | 磁盘内容与 receipt/Catalog 不一致 |

Phase 1 的内建模块可通过逻辑 receipt 表达 `absent/installed`，但 receipt 必须标记 `deliveryKind=builtin-logical`，UI 不得把它显示为可释放宿主代码空间。

### 6.2 策略状态 Policy

| 状态 | 含义 |
| --- | --- |
| `enabled` | 允许进入导航和调用门 |
| `disabled` | 已安装但停用 |
| `blocked` | 因不兼容、撤回、恢复或安全策略禁止启动 |
| `pending-consent` | 新权限或数据迁移等待确认 |
| `mandatory` | Core，不允许卸载或停用 |

### 6.3 运行状态 Runtime

| 状态 | 含义 |
| --- | --- |
| `inactive` | 没有运行资源 |
| `activating` | 正在初始化、启动或探测 |
| `active` | 可接受调用 |
| `busy` | 有在途操作 |
| `stopping` | 正在 drain/cancel/destroy |
| `crashed` | 受管进程异常退出 |
| `failed` | 初始化或恢复失败 |

### 6.4 健康与版本状态 Health/Currency

| 状态 | 含义 |
| --- | --- |
| `current` | 当前版本可用且无已知更新 |
| `update-available` | 有兼容更新 |
| `pinned` | 用户固定版本 |
| `incompatible` | 与宿主、策略、架构或上游环境不兼容 |
| `corrupt` | 摘要、布局或 receipt 不一致 |
| `revoked` | 官方目录撤回当前声明或包 |
| `unverified` | 缺少有效官方验证，不可激活 |
| `degraded` | 可用但部分探测、来源或能力异常 |
| `offline-stale` | 本地目录过期，已有版本按离线策略继续使用 |

### 6.5 派生规则

- `absent + disabled + inactive + current` → “未安装”；
- `installed + disabled + inactive + current` → “已安装，未启用”；
- `installed + enabled + active + update-available` → “运行中，有更新”；
- `installed + blocked + inactive + revoked` → “版本已撤回，已阻止运行”；
- `installed + enabled + crashed + degraded` → “模块异常退出，可重试或诊断”；
- `updating + enabled + active + current` → “正在准备更新，当前版本仍可用”。

### 6.6 权威来源

- journal：进行中事务；
- receipt/active pointer：已安装内容；
- 用户配置：Policy；
- Registry/ProcessSupervisor：Runtime；
- 已验证 Catalog、manifest 和完整性检查：Health/Currency；
- 前端只消费投影，不持久化第二份真相。

---

## 7. 模块分类与首批样本

### 7.1 分类

| 分类 | 例子 | 近期交付策略 |
| --- | --- | --- |
| Core 控制平面 | 首页、设置、模块中心、Catalog、事务、通知 | 随宿主、不可卸载 |
| 内建自建功能 | Memo、WiFi、PortScan 等 | Phase 1 逻辑安装态；不急于物理拆包 |
| 普通托管模块 | MarkerOn、QuickLook 等 | Phase 3 平台化，Phase 4–5 声明式迁移 |
| 特殊安装托管模块 | Rufus、MSI/MSIX 类工具 | 用受控内建策略验证共享内核边界 |
| 官方复杂功能包 | OCR、FileShare | Phase 6 sidecar/资源包物理拆分 |
| 高风险复杂模块 | WSL、PortKill、微信等 | 后置，逐项专项评估 |

### 7.2 Phase 3 样本：MarkerOn 与 Rufus

#### MarkerOn

相关路径：

- `internal/modules/markeron/`
- `internal/modules/markeron/version/`
- 对应 instance/service/frontend 文件

选择原因：普通便携托管工具，适合验证 ArtifactManager、版本目录、ProcessSupervisor、状态事件和统一操作。

#### Rufus

相关路径：

- `internal/modules/rufus/`

选择原因：与 MarkerOn 形成差异，验证单文件资产、版本发现、启动、外部实例、来源和许可证处理，防止共享内核只适配一种 zip 布局。

### 7.3 Phase 4 声明式试点：MarkerOn、Rufus、QuickLook

QuickLook 作为第三个样本，用于验证更复杂的安装/运行探测策略和声明模型覆盖范围。三者共同回答：

- manifest 是否能表达来源、资产筛选、验证证据、安装布局、启动和退出；
- 宿主内建策略是否足够而无需任意脚本；
- 模块中心是否能统一展示上游发布者、许可证、安装和运行状态；
- 声明包更新是否可以独立于宿主版本发布。

### 7.4 Phase 6 复杂功能包顺序

1. **OCR：首个样本**  
   已有托管引擎 manifest、逐文件 SHA256、安全解包、staging、原子落位和在用拒卸基础。先把重型引擎/服务和必要资源从宿主交付中独立出来，再通过稳定 Broker 调用。相关路径：`internal/modules/ocr/`。
2. **FileShare：第二个样本**  
   验证常驻本地服务、网络监听、传输进度、停止、端口和数据边界。相关路径：`internal/modules/fileshare/`。
3. **WSL：后置**  
   涉及提权、MSI、重操作、多状态账本和系统集成，不进入首轮 sidecar。必须在 OCR、FileShare 稳定后另做 ADR 和安全评审。相关路径：`internal/modules/wsl/`。

### 7.5 不再采用的首批结论

WiFi、PortScan、Softver 不再被指定为首批物理拆包样本。它们可以用于 Phase 1–2 验证逻辑安装态、调用门、异步 Operation 和事件一致性，但本计划不要求为它们建设动态 Vue Bundle 或独立 Wails Service。

---

## 8. 安装、更新、卸载 journal 事务

### 8.1 适用范围

同一事务框架服务三种交付：

- `builtin-logical`：逻辑安装 receipt，不复制宿主代码；
- `managed-declarative`：官方声明、被托管上游资产和版本；
- `official-sidecar`：Phase 6 官方复杂功能包。

三者步骤不同，但共享锁、journal、幂等、取消、恢复、数据策略和审计语义。

### 8.2 通用原则

- 每个 module ID 同时只允许一个写事务；
- 跨模块可并行，但下载、解压、安装受全局限流；
- journal 先落盘并 fsync，再执行副作用；
- 每一步记录 `pending/running/succeeded/failed/compensated`；
- 所有步骤幂等或有明确补偿；
- staging 与最终目录位于同卷；
- 已安装版本目录不可原地覆盖；
- 激活使用原子 JSON 指针，不依赖链接权限；
- 崩溃恢复发生在模块启动之前；
- journal 不记录 Secret、用户文件内容或敏感完整命令行；
- schema 版本化，至少支持上一个稳定宿主产生的未完成事务。

### 8.3 journal 最小字段

```json
{
  "schema": 1,
  "transactionId": "uuid",
  "operation": "install|update|rollback|uninstall|repair",
  "deliveryKind": "builtin-logical|managed-declarative|official-sidecar",
  "moduleId": "markeron",
  "fromVersion": null,
  "toVersion": "1.0.0",
  "phase": "verify",
  "state": "running",
  "createdAt": "RFC3339",
  "updatedAt": "RFC3339",
  "activeBefore": null,
  "dataPolicy": "retain",
  "steps": [],
  "error": null
}
```

### 8.4 逻辑安装事务

Phase 1 内建模块：

1. 校验模块存在于宿主内建 Catalog；
2. 创建 journal；
3. 检查依赖、冲突和现有数据；
4. 写逻辑 receipt；
5. 设置默认 Policy；
6. 更新 Catalog 投影；
7. 提交事务。

逻辑卸载：

1. 阻止新调用；
2. drain/cancel 在途 Operation；
3. `OnDestroy()`；
4. 移除逻辑 receipt 和导航入口；
5. 默认保留数据；
6. 明确报告“宿主内建代码仍随应用存在，未释放主程序空间”。

### 8.5 声明式托管模块安装/更新

1. 获取 module lock；
2. 创建 journal；
3. 验证官方 Catalog 和 `.hxmanaged` 签名；
4. 校验 manifest schema、策略白名单、宿主兼容和许可证；
5. 选择上游资产；
6. 下载到临时文件，限制大小、超时和重定向；
7. 验证官方提供的上游摘要、签名或其他证据；
8. 安全解包/安装到版本目录；
9. 校验布局、入口、PE 版本和禁止文件；
10. 写 receipt；
11. 原子切换活动版本；
12. 使用 ProcessSupervisor 做冷启动/探测；
13. 成功后提交；失败则回到旧版本；
14. 按策略保留安装包和旧版本。

更新不得覆盖旧目录。新增高风险能力、许可证变化、安装策略变化或不可逆数据迁移必须重新确认。

### 8.6 sidecar 安装/更新

Phase 6 在上述步骤基础上增加：

- package manifest 和逐文件 digest；
- sidecar exe Authenticode 验证；
- sidecar protocol/host min-max 兼容；
- 安装到 `modules/sidecars/<id>/<version>`；
- Broker 握手和健康检查；
- 新版本失败时恢复旧 active pointer 并重启旧版本。

### 8.7 卸载事务

1. 展示将删除的声明、程序、上游软件、缓存、数据和系统集成；
2. 默认 `dataPolicy=retain`；
3. Policy 进入 blocked-removal，拒绝新调用；
4. drain/cancel 在途 Operation；
5. 停止受管进程并确认 JobObject 无残留；
6. 对 sidecar 移除 active pointer；
7. 将待删除目录 rename 为 `.removing-<txid>`；
8. 删除 renamed 目录；被占用时标记待重启完成；
9. 按用户选择处理被托管上游软件、缓存和数据；
10. 写 retention 墓碑；
11. 更新 Catalog 投影并提交。

被管理的上游软件与 Hanxi 模块必须分开选择。卸载 MarkerOn 管理模块不得默认删除用户已安装或外部运行的 MarkerOn。

### 8.8 恢复扫描

宿主启动早期：

1. 扫描未完成 journal；
2. 校验 receipt 和 active pointer；
3. 处理 staging、`.installing-*`、`.removing-*`；
4. 对 installed/versions 做轻量完整性检查；
5. 无 receipt 内容标为 orphaned，不自动执行；
6. 恢复完成后才开放业务调用。

---

## 9. 数据保留

### 9.1 数据分类

| 类别 | 示例 | 默认卸载行为 |
| --- | --- | --- |
| 逻辑安装凭据 | receipt、Policy | 删除/重置 |
| 官方 sidecar 程序 | OCR/FileShare sidecar | 删除 |
| 被托管上游软件 | MarkerOn、Rufus、QuickLook | 单独确认，不默认删除外部安装 |
| 用户业务数据 | 模块配置、历史、任务记录 | 保留 |
| Secret | DPAPI 密文、凭据引用 | 保留；单独危险操作删除 |
| 缓存 | 安装包、查询缓存、缩略图 | 可选删除 |
| Runtime | pid、pipe、临时输出 | 删除 |
| 日志 | 操作日志、崩溃报告 | 按全局保留策略 |
| 系统集成 | 热键、自启、快捷方式、协议注册 | 必须撤销或报告残留 |

### 9.2 卸载确认

- 默认：“卸载模块，保留个人数据”；
- 可选：“同时清理缓存和安装包”；
- 若有上游软件：“同时卸载被管理的软件”，默认不选；
- 危险项：“永久删除模块数据”，二次确认并列出路径和不可恢复范围；
- 对逻辑内建模块显示“不会减少 Hanxi 主程序体积”；
- 对 sidecar/托管资产显示可释放的实测字节数。

### 9.3 重装恢复

- 安装时发现 retention 墓碑和兼容数据则提示接回；
- 数据 schema 较新而模块较旧时阻断启动；
- 数据检查和迁移分开，用户确认前不写数据；
- 不可逆迁移必须先备份并明确取消哪些回滚能力；
- 首批 sidecar 优先要求可逆迁移或无需迁移。

---

## 10. 权限、签名与安全解包

### 10.1 权限与受控策略

声明式托管模块只能选择宿主内建能力：

- 下载来源和资产选择器；
- portable zip、单文件 exe、受控 MSI/MSIX/AppInstaller 策略；
- 版本目录和布局断言；
- 启动参数模板中的受控字段；
- 进程名、互斥体、窗口、端口和健康探针；
- 优雅退出、命令通道、JobObject 和强制退出策略；
- 快捷方式、跟随 Hanxi 退出等宿主选项。

manifest 禁止：

- 任意 shell/PowerShell/batch；
- 任意安装后脚本；
- 任意注册表路径和命令行拼接；
- 未在宿主策略中登记的可执行钩子；
- 从网络加载 UI 或执行代码。

sidecar 权限由 manifest 声明，敏感操作通过 Host API：

- `network.outbound`；
- `filesystem.user-selected`；
- `filesystem.module-data`；
- `process.enumerate`；
- `process.terminate`；
- `system.elevate`；
- `registry.read/write`；
- `clipboard`、`notifications`、`hotkey` 等。

同用户 sidecar 不是完整 OS 沙箱。无法通过 Broker 控制且风险高的模块不拆；WSL、PortKill 等必须后置。

### 10.2 签名层级

1. **官方 Catalog 签名**：证明目录、兼容、撤回和通道来自 Hanxi；
2. **`.hxmanaged` / `.hxmodule` manifest 签名**：证明声明或官方包来自 Hanxi；
3. **逐文件 SHA-256**：证明内容完整；
4. **Authenticode/上游签名**：验证 exe/dll 发布者；
5. **SBOM/Provenance**：关联源码、构建和依赖。

SHA256 不能单独证明发布者身份。镜像只作为传输来源，不作为信任根。

### 10.3 根信任、轮换与撤回

- 宿主内置 active 根公钥和预备轮换公钥；
- Catalog 含 key ID、有效期和签名时间；
- 新 key 由既有受信根授权或通过宿主升级带入；
- 私钥只存在于受保护发布环境；
- 撤回支持 `warn`、`block-new-install`、`block-run`；
- 离线时允许按最近一次已验证目录继续使用已有版本，但显示 stale；
- key 泄露、错误撤回和目录不可用必须有演练 runbook。

### 10.4 安全解包

复用 OCR 和 LiteMonitor 已验证经验：

- 拒绝绝对路径、盘符、UNC、`..` 和规范化逃逸；
- 拒绝 symlink、junction、hardlink、reparse point；
- 限制包大小、条目数、单文件、总展开大小和压缩比；
- Windows 大小写不敏感去重；
- 拒绝保留名、尾随点/空格和路径冲突；
- 只允许 manifest 列出的文件；
- staging 使用独占创建，不覆盖已有文件；
- CRC 后仍校验 SHA-256；
- staging 中不执行任何内容；
- 最终版本目录不可原地更新；
- 文件锁时保留可恢复状态，不以粗暴 `RemoveAll` 掩盖错误。

### 10.5 下载安全

- Catalog 和包使用 HTTPS；
- 限制重定向 host 和次数；
- 下载完成后全量 digest 校验；
- 不记录含 token 的完整 URL；
- 访问 `127.0.0.1` 的内部服务使用显式 `Transport{Proxy:nil}`；
- 下载进度、取消、错误统一进入 Operation。

---

## 11. UI 改造

### 11.1 信息架构

**首页继续是工作台。** `frontend/src/views/HomeView.vue` 不改造成 Catalog 首页，继续承载最近使用、常用入口、运行摘要和工作流价值。

新增独立“模块中心”入口：

- 侧栏或导航轨中的一级入口；
- 命令面板可搜索“打开模块中心”；
- 设置页只承载全局更新、缓存、通道和安全策略，不复制模块目录；
- 模块中心内部按“已安装、可安装、更新、异常”筛选；
- 工作台只展示用户已安装/启用的模块快捷入口和必要状态。

### 11.2 模块中心卡片

每张卡片显示：

- 名称、分类、官方管理标记；
- 上游发布者与许可证（托管模块）；
- 四维状态摘要；
- 声明版本、上游版本或 sidecar 版本；
- 下载大小、安装大小、数据占用；
- 权限/系统集成摘要；
- 主操作：安装、启用、打开、更新、重试；
- 次操作：版本、固定、回滚、停用、卸载、数据、诊断。

主操作由统一状态机计算，组件不得自行用大量 if/else 推断。

### 11.3 工作台与导航

- 工作台只显示 installed + enabled 的常用功能；
- 未安装模块不进入业务导航、命令动作、托盘候选和快捷键；
- 停用当前模块后安全回到工作台；
- 模块中心始终可进入，不能依赖可选模块；
- 运行/更新异常可在工作台显示摘要，但详细操作回模块中心；
- 所有入口消费同一后端 Catalog 投影。

### 11.4 操作体验

- 安装/更新显示下载、验证、安装、切换、健康检查阶段；
- 逻辑安装明确标注“功能已加入工作台，不影响 Hanxi 主程序体积”；
- 原子提交阶段不伪装可取消；
- 卸载分列模块本身、上游软件、缓存和个人数据；
- 失败提示提供用户动作和诊断 ID；
- 启动恢复 journal 时模块中心显示恢复条；
- revoked 使用安全告警，不等同普通更新；
- offline-stale 明确显示最后验证时间。

### 11.5 前端实现边界

- Phase 1–5 使用宿主内静态 Vue 页面和统一托管模块详情组件；
- 声明式托管模块由宿主按 schema 渲染，不加载声明自带 JS；
- Phase 6 sidecar 仍可使用宿主静态页面，通过 Broker 调用；
- 是否在未来支持官方动态 UI Bundle需另行 ADR，本计划不将其作为完成前提；
- 所有对话框继续使用全局 `usePrompt/useConfirm`；
- 状态有文本和无障碍标签，不只依赖颜色。

---

## 12. Phase 0–7 执行计划

### 12.1 估算口径

- 人日包含设计、实现、测试、评审和必要文档；
- P50 为边界稳定时的中位估算；
- P80 包含 Windows 文件锁、上游差异、生命周期清偿、签名和兼容返工；
- 单人日历近似人日，但外部等待会拉长；
- 小组按 3 人（平台后端、模块/Windows、前端/测试发布）估算；
- 协议/状态冻结、首个共享内核样本、首个声明迁移、首个 sidecar 均是串行关键路径；
- Phase 0–2 是用户价值 MVP，共 **24/36 人日**；总计 **108/172 人日**。

### 12.2 总览

| Phase | 主题 | P50 | P80 | 单人日历 | 3 人小组日历 | 关键产出 |
| --- | --- | ---: | ---: | --- | --- | --- |
| 0 | 清点契约 | 4 | 6 | 4–6 工作日 | 2–3 工作日 | 分类、状态、入口、数据、权限基线 |
| 1 | 逻辑安装态 + 模块中心 | 8 | 12 | 8–12 工作日 | 4–6 工作日 | 独立模块中心、准确安装语义 |
| 2 | 统一调用门与生命周期 | 12 | 18 | 12–18 工作日 | 6–9 工作日 | 所有入口受控，MVP 完成 |
| 3 | 共享托管内核 | 18 | 28 | 18–28 工作日 | 9–14 工作日 | ArtifactManager、ProcessSupervisor、Operation |
| 4 | 声明式官方托管模块 + 签名目录 | 14 | 22 | 14–22 工作日 | 7–11 工作日 | MarkerOn/Rufus/QuickLook 声明试点 |
| 5 | 批量迁移普通托管模块 | 18 | 30 | 18–30 工作日 | 10–16 工作日 | 普通托管家族去重复 |
| 6 | 官方复杂功能包 / sidecar | 24 | 40 | 24–40 工作日 | 13–22 工作日 | OCR、FileShare 真实物理拆包 |
| 7 | CI/Release 治理 | 10 | 16 | 10–16 工作日 | 5–8 工作日 | 多产物签名、兼容、灰度、撤回 |
| **总计** |  | **108** | **172** | **108–172 工作日** | **约 56–89 工作日（含受控重叠）** |  |

### Phase 0：清点契约（4/6 人日）

**目标**

建立模块分发改造的唯一事实基线，明确哪些是 Core、普通托管、自建功能、复杂功能和高风险模块。

**任务**

1. 清点 41 个模块的代码形态、入口、运行资源、数据、权限、上游来源和跨模块依赖；
2. 清点所有调用入口：Wails RPC、导航、托盘、热键、独立窗口、MCP、启动预激活、后台事件；
3. 定义四维状态、delivery kind、receipt、Operation 和错误码；
4. 定义逻辑安装态语义，明确不释放主程序空间；
5. 标记 Core 不可卸载集合；
6. 输出普通托管候选和复杂 sidecar 候选；
7. 记录当前 host 体积、前端体积、启动时间和托管重复代码基线；
8. 建立 ADR/决策点和 owner。

**依赖**

- `internal/extapi/module.go`
- `internal/extapi/registry.go`
- `internal/app/app.go`
- `scripts/fixture/composition_contract.json`
- `docs/PLUGINIZATION_AND_PRODUCT_EVOLUTION.md`

**可并行项**

- 模块清点、入口清点、数据权限清点、UI 信息架构草案可并行；
- 状态和 delivery kind 需统一评审。

**影响文件（预计）**

- `docs/` 下 ADR/契约文档；
- `schemas/` 初稿；
- `scripts/` 基线测量工具。

**DoD**

- 41 个模块均有分类和 owner；
- 所有入口有清单，无“未知旁路”；
- 四维状态和逻辑安装语义获确认；
- MarkerOn、Rufus、QuickLook、OCR、FileShare、WSL 的阶段定位无歧义；
- 基线可重复测量。

**风险**

- 清点遗漏后台入口；控制：代码搜索、Composition Contract、托盘/MCP/热键专项复核；
- 分类争议拖延；控制：不确定模块默认留宿主，不阻塞基础模型。

### Phase 1：逻辑安装态 + 独立模块中心（8/12 人日）

**目标**

在代码仍位于宿主的前提下，先让用户按需选择功能，解决模块过多和状态难懂问题。

**任务**

1. 新增 Module Catalog 聚合层；
2. 为内建模块建立 `builtin-logical` receipt；
3. 将 Installed 与 Enabled 分离；
4. 新增独立模块中心页面和导航入口；
5. 保持 `HomeView.vue` 为工作台，改为只消费已安装/启用模块摘要；
6. 模块中心实现已安装、可安装、更新、异常筛选；
7. 统一侧栏、导航轨、命令面板和托盘对 Catalog 投影的消费；
8. 添加逻辑安装/卸载 journal 最小实现；
9. 展示占用、数据和“不会减少宿主体积”的准确文案；
10. 保持旧 Enabled 配置兼容迁移。

**依赖**

- Phase 0 状态、分类和 receipt schema；
- 现有 Registry 与 `ext:changed` 事件。

**可并行项**

- 后端 Catalog/receipt 与模块中心 UI 可并行；
- 工作台摘要和导航过滤可在 API 稳定后联调。

**影响文件（预计）**

- `internal/extapi/registry.go`
- `internal/app/service.go`
- `internal/app/app.go`
- `frontend/src/views/HomeView.vue`
- 新增模块中心 View/组件
- `frontend/src/constants/navigation.ts`
- `frontend/src/App.vue`
- `frontend/src/components/shell/`
- `frontend/src/constants/status.ts`

**DoD**

- 首页仍是工作台，模块中心有独立稳定入口；
- 用户可逻辑安装/卸载内建模块；
- 未安装模块不进入业务导航和工作台入口；
- UI 准确区分 Installed、Enabled、Runtime、Health；
- 明确说明逻辑卸载不释放宿主代码空间；
- 现有 Enabled 用户状态无损迁移；
- 41 个模块现有功能不回归。

**风险**

- 用户误解逻辑卸载；控制：delivery kind 和空间文案强制由后端提供；
- 首页与模块中心重复；控制：首页只放工作流与已选功能，完整目录只在模块中心。

### Phase 2：统一调用门与生命周期（12/18 人日）

**目标**

确保“未安装/停用”不仅隐藏 UI，而且所有业务入口都无法绕过；统一初始化、在途操作、停用和退出。完成后达到用户价值 MVP。

**任务**

1. 建立统一 `Acquire(moduleID)` / operation lease；
2. 调用门依次检查 Installed、Enabled、Blocked、生命周期状态；
3. Wails Service 方法、托盘、热键、MCP、独立窗口、后台任务全部接入；
4. Registry 成为生命周期唯一权威源；
5. 统一 `OnInit/OnDestroy`、并发初始化、失败重试和停用 drain；
6. 定义 Operation 状态、进度、取消、超时和结构化错误；
7. 消除预激活模块 ID 特例，改为显式策略；
8. 建立事件信封和诊断上下文；
9. 增加资源残留测试：Goroutine、Ticker、连接、句柄、进程；
10. 建立非法状态转换和竞态测试。

**依赖**

- Phase 1 Catalog 和逻辑安装态；
- 现有 `internal/extapi/registry.go` 在途操作机制。

**可并行项**

- 调用门封装、入口迁移、Operation 模型、资源残留测试可分工；
- 预激活策略需统一评审。

**影响文件（预计）**

- `internal/extapi/registry.go`
- `internal/app/app.go`
- `internal/app/service.go`
- 各模块 Service、托盘、热键、MCP 入口
- `internal/app/composition_contract_test.go`
- `scripts/fixture/composition_contract.json`
- 前端统一 Operation composable

**DoD**

- 未安装或停用模块从任何受控入口都不能执行业务；
- 初始化并发只有一次真实执行；
- 停用等待/取消在途操作，无死锁；
- 应用退出后的受管孤儿进程为 0；
- 生命周期非法迁移为 0；
- 代表模块停用后资源残留为 0；
- 模块中心、工作台、托盘和 MCP 状态一致；
- Phase 0–2 MVP 端到端演示通过。

**风险**

- 41 模块入口迁移范围大；控制：先核心入口和高风险入口，再自动化清单防漏；
- 旧模块不支持取消；控制：先实现拒绝新操作和有界 drain，逐模块补取消。

### Phase 3：ArtifactManager + ProcessSupervisor + Operation 共享托管内核（18/28 人日）

**目标**

把普通托管模块重复的发现、下载、验证、安装、版本、启动、探测、退出和操作状态收敛为宿主能力。样本为 MarkerOn、Rufus。

**任务**

1. 实现 ArtifactManager：来源、缓存、下载、摘要、安全解包、staging、版本、激活和回滚；
2. 实现 ProcessSupervisor：所有权、JobObject、进程指纹、外部实例、就绪、唤窗、退出；
3. 实现 Operation：统一 ID、阶段、进度、取消、日志、错误、重试；
4. 实现 journal、锁、receipt、恢复扫描；
5. 抽取受控安装策略接口，禁止任意脚本；
6. 迁移 MarkerOn 到共享内核；
7. 迁移 Rufus，验证不同资产和运行形态；
8. 建立统一事件和模块中心操作 UI；
9. 建立下载失败、磁盘满、文件锁、进程占用、更新回滚测试；
10. 记录旧实现差异，不要求本阶段批量迁移。

**依赖**

- Phase 2 调用门和 Operation 基础；
- OCR/LiteMonitor 事务经验；
- Windows JobObject 和进程工具。

**可并行项**

- ArtifactManager、ProcessSupervisor、Operation/UI、故障注入可并行；
- MarkerOn 先作为黄金样本，Rufus 在接口稳定后并行迁移。

**影响文件（预计）**

- `internal/platform/` 新增 artifact/supervisor/operation 包
- `internal/settings/paths.go`
- `internal/modules/markeron/`
- `internal/modules/rufus/`
- `internal/platform/windows/job.go`
- `internal/platform/windows/process.go`
- 模块中心组件

**DoD**

- MarkerOn、Rufus 不再各自复制下载/解压/版本/进程主流程；
- 更新任意步骤失败时旧版本仍可用；
- journal 崩溃恢复通过故障注入；
- 外部实例、受管实例和脱管状态可区分；
- 宿主退出后受管进程残留为 0；
- 普通新托管模块可复用大部分主流程；
- 特殊差异通过受控策略表达，不出现 shell 逃生钩子。

**风险**

- 过度抽象抹平真实差异；控制：MarkerOn/Rufus 双样本、只抽稳定共性；
- 文件锁导致卸载失败；控制：rename-first、待重启、恢复 journal。

### Phase 4：声明式官方托管模块 + 签名目录（14/22 人日）

**目标**

让普通官方托管模块的差异主要由官方签名声明表达，并建立官方目录、兼容和撤回治理。试点 MarkerOn、Rufus、QuickLook。

**任务**

1. 定义 `.hxmanaged` 和 manifest schema；
2. 声明身份、上游发布者、许可证、来源、资产、摘要、安装策略、布局、探针、退出和数据；
3. 实现 manifest 静态校验和受控策略绑定；
4. 建立签名官方 Catalog、通道、兼容、固定、撤回；
5. 建立 package/catalog 签名、公钥轮换和离线缓存；
6. 将 MarkerOn、Rufus 从代码差异迁为声明差异；
7. 新增 QuickLook 作为第三种试点；
8. 模块中心展示 Hanxi 官方管理与真实上游发布者；
9. 支持官方包离线导入，但不允许绕过签名；
10. 建立 manifest 正反 fixture 和恶意声明测试。

**依赖**

- Phase 3 共享内核稳定；
- 签名算法和测试根方案确定。

**可并行项**

- schema/tooling、Catalog 签名、QuickLook 适配、模块中心来源展示可并行；
- 生产根接入可后于测试根全链路，但不能晚于 stable 发布。

**影响文件（预计）**

- `schemas/managed-module.schema.json`
- `schemas/official-catalog.schema.json`
- `modules/managed/markeron/`
- `modules/managed/rufus/`
- `modules/managed/quicklook/`
- `tools/modulepack/`
- `tools/cataloggen/`
- Catalog/签名验证后端
- 模块中心 UI

**DoD**

- 三个试点由声明驱动共享内核；
- manifest 无任意脚本、任意命令执行和远程 UI；
- 篡改 Catalog、manifest 或策略参数会被拒绝；
- 上游发布者和 Hanxi 管理身份不混淆；
- 撤回可阻止新安装，并按等级警告或阻止运行；
- 离线导入与在线安装使用同一验证链；
- 声明更新可独立于宿主发布，但必须满足宿主策略兼容。

**风险**

- manifest 演化成编程语言；控制：只允许枚举型受控策略，特殊流程留代码适配；
- 上游摘要不稳定；控制：定义验证等级和不可验证时的保守策略。

### Phase 5：批量迁移普通托管模块（18/30 人日）

**目标**

把适合声明化的普通托管模块批量迁入共享内核，显著减少重复代码和宿主维护面。

**任务**

1. 按策略族分组：portable zip、单 exe、MSI/MSIX/AppInstaller、互斥体/窗口/命令通道；
2. 为每一族选择代表模块，完善宿主受控策略；
3. 批量迁移普通托管模块的版本和实例主流程；
4. 删除已迁移模块的重复下载、解压、版本、进程代码；
5. 对不适合声明化的模块保留薄代码适配器，不使用脚本逃生；
6. 建立迁移模板、契约测试和自动清单；
7. 统一模块中心卡片、版本、操作、诊断和卸载体验；
8. 建立上游来源、许可证、第三方声明和网络行为审计；
9. 对旧状态和目录执行幂等迁移；
10. 以分批发布降低一次性回归风险。

**依赖**

- Phase 4 三个试点至少经过一个稳定周期；
- 受控策略和测试 harness 可复用。

**可并行项**

- 不同策略族可由多人并行；
- 公共内核变更必须先经代表样本验证再批量落地。

**影响文件（预计）**

- `internal/modules/*/version/`
- `internal/modules/*/instance/`
- `modules/managed/`
- `internal/platform/apppackage/`
- `internal/platform/windows/`
- 模块中心和统一操作组件
- `docs/THIRD_PARTY_NOTICES.md`

**DoD**

- 适合声明化的普通托管模块完成迁移清单；
- 已迁移模块不再复制主流程；
- 每个模块通过安装、更新、回滚、启动、外部实例、退出、卸载契约测试；
- 目录迁移不覆盖用户现有版本和配置；
- 不适合声明化的模块有书面原因和薄适配边界；
- 未出现任意脚本或无审计命令模板；
- 公共缺陷可在共享内核修复一次覆盖全部迁移模块。

**风险**

- 批量迁移放大回归；控制：按策略族分批、灰度、保留旧路径短期回滚；
- 特殊模块拖垮抽象；控制：允许留在代码适配层，不追求 100% 声明率。

### Phase 6：官方复杂功能包 / sidecar（24/40 人日）

**目标**

对无法由声明式托管模型表达、确有独立交付价值的官方复杂功能进行物理拆包。OCR 为首个样本，FileShare 第二，WSL 后置。至此实现复杂业务代码/重资源真正按需安装。

**任务**

1. 定义最小 sidecar manifest 和版本化本地 RPC；
2. 实现 Broker、握手、超时、取消、事件、健康和优雅停止；
3. sidecar 纳入 ProcessSupervisor 和 JobObject；
4. 建立 Host API 与能力声明，敏感操作由宿主复核；
5. 定义 `.hxmodule`、逐文件摘要、Authenticode、安装和 active pointer；
6. OCR 首个迁移：优先外置引擎服务、重资源和后端进程；
7. 保留宿主静态 OCR UI，通过 Broker 调用，不以动态 Vue Bundle为前提；
8. 验证 OCR 本地导入、引擎版本、识别取消、崩溃隔离和数据保留；
9. FileShare 第二迁移：验证常驻服务、端口、传输、停止、网络权限和恢复；
10. 建立 host × sidecar 兼容矩阵和 side-by-side 更新回滚；
11. 测量 host 体积、初始下载、sidecar 包和启动开销；
12. 对 WSL 输出后置 ADR，不在本阶段强行迁移。

**依赖**

- Phase 3 ArtifactManager/ProcessSupervisor/Operation；
- Phase 4 签名 Catalog；
- Phase 5 平台稳定性；
- OCR/FileShare 数据和权限边界通过评审。

**可并行项**

- sidecar 协议、包格式、Broker 测试可并行；
- OCR 必须先作为黄金样本；
- OCR 稳定后 FileShare 与兼容/故障测试可并行；
- WSL 只做研究和 ADR，不与交付关键路径绑定。

**影响文件（预计）**

- `internal/modules/ocr/`
- `internal/modules/fileshare/`
- `internal/modules/wsl/`（仅后置评估）
- `packages/go/sidecarprotocol/` 或过渡等价路径
- `modules/sidecars/ocr/`
- `modules/sidecars/fileshare/`
- Broker Wails Service
- `internal/platform/windows/job.go`
- `schemas/sidecar-manifest.schema.json`
- 构建和打包工具

**DoD**

- clean host 产物不再携带已拆出的 OCR/FileShare 后端重型程序与资源；
- 未安装 sidecar 时 UI 给出安装入口而非崩溃；
- 安装无需替换宿主即可启用对应 sidecar；
- 卸载后 sidecar 程序和资源真实移除，保留数据策略正确；
- 更新失败自动回到旧 sidecar；
- sidecar 崩溃不导致宿主崩溃；
- 宿主退出后受管 sidecar 孤儿进程为 0；
- 未授予 Host API 能力时默认拒绝；
- OCR 和 FileShare 均通过协议、事务、权限和长稳测试；
- WSL 未经专项批准不进入物理拆包。

**风险**

- sidecar 协议过早固化；控制：仅官方、最小契约、有限支持窗；
- 同用户进程被误认为沙箱；控制：明确边界，高风险操作留宿主；
- FileShare 网络面扩大；控制：绑定、鉴权、端口、日志和权限专项评审；
- OCR 私有资产误入公开包；控制：allowlist 打包和独立私发流水线。

### Phase 7：CI/Release 治理（10/16 人日）

**目标**

收口 Monorepo 多产物构建、兼容矩阵、签名、灰度、撤回、回滚和文档治理，使前六阶段可持续发布。

**任务**

1. 建立 `go.work`、npm workspace 和多产物构建图；
2. CI 按受影响范围构建 host、声明包、sidecar 和 Catalog；
3. 建立 schema、签名、恶意包、journal 恢复和兼容测试；
4. Release 生成版本、build ID、SBOM、provenance、checksums 和签名；
5. 建立 dev/beta/stable 通道和灰度提升；
6. 建立 key 轮换、撤回、Kill Switch 和回滚 runbook；
7. 修正现有 Release 版本注入和陈旧数据根文案；
8. 使用资产 allowlist，防止私有组件或临时文件误上传；
9. 建立发布后安装/更新/卸载 smoke；
10. 清偿过渡双写、更新架构、前端、排障和第三方声明文档。

**依赖**

- 前六阶段产物和 schema 已稳定；
- 生产签名密钥托管方案可用。

**可并行项**

- CI matrix、SBOM/provenance、签名集成、runbook、文档可并行；
- stable 发布必须等生产根和恢复演练完成。

**影响文件（预计）**

- `go.mod`、新增 `go.work`
- 根 `package.json`、`frontend/package.json`
- `Taskfile.yml`、`build/Taskfile.yml`、`build/windows/Taskfile.yml`
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `tools/modulepack/`、`tools/cataloggen/`、`tools/compatcheck/`
- `docs/ARCHITECTURE.md`
- `docs/FRONTEND.md`
- `docs/TROUBLESHOOTING.md`
- `docs/THIRD_PARTY_NOTICES.md`

**DoD**

- host、`.hxmanaged`、`.hxmodule` 和 Catalog 可独立构建、签名、发布；
- 协议/共享内核变更会触发全部受影响兼容测试；
- 最终资产可追溯到源码 tag、SBOM、provenance 和签名 key；
- production secret 不进入 fork PR 或日志；
- 撤回、错误撤回、key 轮换、离线和回滚均演练；
- Release 文案与实际数据根、资产和能力一致；
- Monorepo 改造不要求把所有产物合并发布。

**风险**

- 多产物发布增加运维负担；控制：自动生成 Catalog、统一版本元数据、可复跑流水线；
- 密钥托管阻断 stable；控制：测试根先打通，生产根不满足则不发布 stable。

---

## 13. 风险矩阵

| 风险 | 概率 | 影响 | 预警信号 | 缓解措施 | Owner |
| --- | --- | --- | --- | --- | --- |
| 逻辑安装被误解为物理卸载 | 高 | 中 | 用户期待 exe 变小 | 强制显示 delivery kind 和释放空间 | 产品/前端 |
| 调用门遗漏旁路 | 中 | 高 | 停用后托盘/MCP仍可执行 | 入口清单、契约测试、统一 Acquire | 平台负责人 |
| 生命周期 drain 死锁 | 中 | 高 | 停用或退出卡死 | 有界等待、取消、竞态测试 | 平台负责人 |
| 共享内核抽象过早 | 中 | 高 | 大量逃生钩子 | MarkerOn/Rufus 双样本，特殊项留薄适配 | 架构负责人 |
| manifest 演化成脚本语言 | 中 | 高 | 出现任意 command/hook | 枚举策略白名单，代码评审守卫 | 安全负责人 |
| 上游来源或资产被篡改 | 中 | 高 | 摘要/发布者异常 | 官方签名目录、上游证据、事务回滚 | Release Owner |
| Windows 文件锁阻断卸载 | 高 | 中 | `.removing-*` 残留 | rename-first、待重启、恢复器 | 平台负责人 |
| journal 恢复遗漏 | 中 | 高 | orphan receipt/active | 每步故障注入、fsync、模型测试 | 平台负责人 |
| 批量迁移回归 | 中 | 高 | 多模块同时异常 | 按策略族分批、灰度、短期回滚 | 模块负责人 |
| 托管模块身份混淆 | 中 | 中 | 用户以为上游由 Hanxi 开发 | 同时展示 Hanxi 管理与真实发布者 | 产品负责人 |
| sidecar 协议兼容负担 | 中 | 高 | shim 快速增多 | 最小协议、官方限定、支持窗口 | 架构负责人 |
| JobObject 被误宣称沙箱 | 中 | 高 | 文案声称限制系统访问 | 明确边界，高风险模块不拆 | 安全/产品 |
| OCR 私有资产误公开 | 低 | 极高 | CI artifact 出现私有文件 | allowlist、独立私发、仓库扫描 | Release Owner |
| FileShare 网络边界缺陷 | 中 | 极高 | 未授权访问或错误绑定 | 专项威胁模型和真机安全测试 | 安全负责人 |
| 数据 schema 阻断回滚 | 中 | 高 | 新版数据无法被旧版读取 | 首批禁止不可逆迁移、备份点 | 模块 Owner |
| 用户误删数据或上游软件 | 低 | 极高 | 卸载默认勾选删除 | 默认保留、分层确认、墓碑 | 产品/前端 |
| 签名密钥泄露 | 低 | 极高 | 异常签名或权限扩大 | 隔离签名、轮换、撤回、双人审批 | Release Owner |
| 错误撤回正常版本 | 低 | 高 | 大量模块 blocked | 分级撤回、灰度、快速更正 | Release Owner |
| Monorepo 迁移长期红构建 | 中 | 高 | 双入口和 lock 漂移 | 渐进迁移、drift CI、每步可构建 | Build Owner |
| WSL 复杂度拖累主线 | 高 | 高 | 提权/系统状态返工 | 明确后置、另行 ADR 和批准 | 架构/安全 |

---

## 14. 兼容迁移与回滚

### 14.1 从现有 Enabled 迁移

1. 首次升级时为现有模块生成 `builtin-logical` receipt；
2. 原 Enabled=true 映射为 installed + enabled；
3. 原 Enabled=false 默认映射为 installed + disabled，避免升级后数据或入口意外丢失；
4. 用户可在模块中心逻辑卸载；
5. 迁移写 versioned marker，幂等且失败保留旧配置；
6. Catalog 投影始终以迁移后权威状态生成。

### 14.2 托管模块目录迁移

- 现有 `versions/<tool>` 尽量原地接管，不重复下载；
- 新 receipt 通过扫描布局和版本信息生成；
- 同名目标存在时新位置优先，不覆盖；
- 不满足完整性要求的目录标为 external/orphaned，不自动执行；
- 原模块代码保留短期回滚开关，稳定后删除双轨；
- 被托管软件的用户配置和外部安装不迁移或删除，除非策略明确。

### 14.3 声明回滚

- Catalog 保留已支持的上一声明版本；
- 新声明导致安装/探测失败时恢复旧声明和活动版本；
- 撤回声明不能作为回滚目标；
- manifest schema 至少向后兼容一个稳定宿主版本；
- 宿主不理解的新策略必须安全拒绝，不能猜测执行。

### 14.4 sidecar 兼容

由四层决定：

1. host SemVer 区间；
2. sidecar protocol min/max；
3. capability 集；
4. data schema version。

规则：

- 启动前完成静态兼容检查；
- major 不兼容阻断，minor 能力通过协商降级；
- 当前宿主至少测试当前和上一个受支持 sidecar；
- 宿主回滚时不兼容 sidecar进入 blocked，不删除程序和数据；
- sidecar 回滚前检查数据 schema、签名和撤回；
- 回滚操作本身进入 journal。

### 14.5 全局回滚与 Kill Switch

事故时可：

- 停止 Catalog 新安装；
- 撤回指定声明、上游版本或 sidecar；
- 按 `warn/block-new-install/block-run` 分级；
- 关闭自动更新但保留本地稳定版本；
- 恢复上一 Catalog；
- 切回上一活动版本；
- 不以删除全部模块目录作为应急方案。

---

## 15. 测试矩阵

### 15.1 状态与调用门

| 区域 | 覆盖 |
| --- | --- |
| 四维状态 | 合法组合、非法转换、派生文案 |
| 逻辑安装 | receipt 创建/删除、旧 Enabled 迁移 |
| 调用门 | Wails、托盘、热键、MCP、窗口、后台入口 |
| 生命周期 | 并发 Init、失败重试、drain、cancel、Destroy |
| 资源残留 | Goroutine、Ticker、连接、句柄、受管进程 |
| UI 一致性 | 工作台、模块中心、导航、命令面板、托盘 |

### 15.2 ArtifactManager 与事务

- 下载成功、取消、断网、超时、重定向异常；
- Content-Length 错误、摘要错误、来源切换；
- 磁盘满、权限拒绝、Defender/文件占用；
- staging、rename、active pointer 每一步强杀恢复；
- 安装、更新、回滚、repair、卸载；
- 同模块并发写锁、跨模块限流；
- 旧版本保护和待重启删除；
- receipt/orphaned/quarantine 恢复。

### 15.3 ProcessSupervisor

- 受管、外部、脱管实例；
- JobObject 归属和宿主退出；
- ready 探测、超时、崩溃、hang；
- 唤窗、互斥体、窗口、端口、命令通道；
- 优雅退出、强制退出、用户拒绝终止外部实例；
- crash loop 退避和上限；
- PID 复用和进程指纹。

### 15.4 manifest 与签名

- schema 正反 fixture；
- 未知策略、非法命令、任意路径、额外文件；
- Catalog/manifest 篡改；
- 未知 key、过期 key、轮换、错误签名；
- 撤回和离线 stale；
- 上游摘要、Authenticode、PE 版本不符；
- 发布者/许可证展示一致性。

### 15.5 安全解包 corpus

- ZipSlip、绝对路径、UNC、盘符；
- 大小写重复、Unicode 混淆、保留名；
- symlink/junction/hardlink/reparse；
- zip bomb、超大单文件、超多条目；
- CRC 正确但 SHA256 错误；
- manifest 外额外可执行文件；
- 文件锁与半删除恢复。

### 15.6 sidecar

- host current × sidecar current；
- host current × previous supported；
- host current × future protocol fixture；
- previous host × current sidecar；
- 握手伪造、ID/version/digest 不符；
- 超大消息、乱序响应、背压、超时、取消；
- sidecar crash/hang/消息洪泛；
- Host API 未授权和参数逃逸；
- OCR 识别取消、引擎切换、私有包边界；
- FileShare 绑定地址、鉴权、端口冲突、传输中停止；
- sidecar 更新失败和数据 schema 回滚。

### 15.7 UI

- 首页保持工作台；
- 模块中心独立入口和全部筛选；
- 逻辑安装空间文案；
- 安装/更新/卸载阶段；
- 删除数据和上游软件的分层确认；
- revoked/incompatible/corrupt/offline-stale；
- 键盘、焦点、屏幕阅读器、主题；
- 当前模块停用时回工作台；
- 恢复 journal 提示和诊断 ID。

### 15.8 Windows 真机

- Windows 11 当前支持版本；
- 普通用户和管理员；
- 中文、空格、长路径；
- 数据根同盘、跨盘、外置盘绑定；
- Defender 开启；
- 有代理、无代理、离线；
- portable、裸 exe、旧版升级；
- 多开宿主、休眠恢复、强制关机；
- 上游软件已外部安装/已运行场景。

### 15.9 性能与稳定性

- host 冷启动和内存与基线比较；
- 模块中心 41/100 项渲染；
- 并行下载/安装限流；
- 24h 反复启停和资源残留；
- ArtifactManager 大包内存峰值；
- ProcessSupervisor crash loop；
- OCR/FileShare sidecar 启动 P50/P95；
- 迁移前后重复代码、host 体积和初始下载量。

---

## 16. CI 与 Release

### 16.1 CI 分层

1. **快速 PR**：format、lint、typecheck、schema、受影响单测；
2. **契约层**：Catalog、调用门、Operation、manifest、sidecar protocol；
3. **安全层**：恶意包 corpus、签名负例、Secret 和禁止脚本扫描；
4. **Windows 集成层**：安装、更新、回滚、卸载、JobObject、恢复；
5. **全量构建层**：host、全部 `.hxmanaged`、sidecar、Catalog、SBOM；
6. **夜间层**：race、fuzz、长稳、故障注入和历史兼容。

### 16.2 受影响矩阵

- 修改调用门/Registry：测试全部模块入口契约；
- 修改 ArtifactManager：测试全部策略 fixture 和代表模块；
- 修改 ProcessSupervisor：测试全部进程探针族；
- 修改 manifest schema：构建全部声明包；
- 修改 sidecar protocol：测试全部受支持 sidecar；
- 修改单个声明：只构建该声明及 host 兼容测试；
- Release tag：全量不可跳过。

### 16.3 发布流程

1. 校验 tag；
2. checkout 精确 tag；
3. clean dependency install；
4. source、bindings、schema、Catalog drift；
5. 单测、race、lint、typecheck；
6. 构建 host、声明包和 sidecar；
7. 显式注入 version/build ID；
8. 生成 manifest、SBOM、provenance；
9. Authenticode 签名 exe/dll；
10. 生成并签名 `.hxmanaged/.hxmodule`；
11. package self-check；
12. 生成并签名 Catalog；
13. 从最终资产执行安装/更新/卸载 smoke；
14. 发布 dev/beta；
15. 灰度观测后提升 stable；
16. 保留撤回、旧 Catalog 和回滚材料。

### 16.4 当前 Release 基线需修正

- `.github/workflows/release.yml` 显式注入 tag 版本；
- portable 文案与 `docs/plans/PLAN_PATHS.md` 对齐，移除陈旧 `%APPDATA%` 语义；
- `checksums.txt` 只作辅助完整性，不代替签名；
- 增加前端测试/lint、SBOM、provenance 和签名；
- 上传采用资产 allowlist，禁止私有 OCR 组件误发布；
- Release notes 区分 Hanxi 官方模块与托管上游软件。

### 16.5 通道与发布节奏

- `dev`：测试根签名，可频繁生成；
- `beta`：候选用户和有限支持；
- `stable`：正式根签名并经过灰度；
- 声明包可独立更新，但不能使用宿主未知策略；
- sidecar 可独立安全修复，但必须通过兼容矩阵；
- 新增敏感权限、许可证变化和不可逆迁移不得静默自动更新。

---

## 17. 文档治理

### 17.1 权威边界

- 本计划：实施路线和阶段门；
- `docs/ARCHITECTURE.md`：只记录已实现事实；
- `docs/PLUGINIZATION_AND_PRODUCT_EVOLUTION.md`：第三方扩展边界和长期战略；
- `docs/plans/PLAN_PATHS.md`：数据根唯一裁决；
- schema/协议文档：机器契约；
- Release runbook：签名、灰度、撤回、回滚；
- `docs/TROUBLESHOOTING.md`：实际踩坑、排查和标准修复。

每个 Phase 完成后再更新架构事实，不提前把目标态写成现状。

### 17.2 术语守卫

使用：

- “官方功能模块”；
- “逻辑安装态”；
- “官方托管模块”；
- “被托管上游软件”；
- “官方复杂功能包/sidecar”；
- “模块中心”；
- “工作台首页”。

禁止混用：

- 官方托管模块 ≠ 上游软件由 Hanxi 开发；
- 官方模块分发 ≠ 第三方插件市场；
- 逻辑卸载 ≠ 物理释放宿主代码；
- JobObject ≠ 安全沙箱；
- 声明式策略 ≠ 任意脚本。

### 17.3 变更规则

- schema 和协议变更必须版本化并带迁移说明；
- 稳定字段删除经过弃用周期；
- 每个模块维护 owner、来源、许可证、数据、权限、安装和卸载说明；
- 新策略必须安全评审和契约测试；
- 新 sidecar 必须证明声明式模型不能覆盖且物理拆包收益成立；
- 发现构建/运行陷阱按项目规范写入 `docs/TROUBLESHOOTING.md`。

---

## 18. 决策点

| 决策点 | 最晚阶段 | 候选 | 默认保守选择 |
| --- | --- | --- | --- |
| 逻辑卸载默认 Policy | Phase 1 | disabled/移除 receipt | 移除 receipt，数据保留 |
| 首页与模块中心关系 | Phase 1 | 合并/独立 | 独立，首页保持工作台 |
| 托管策略扩展方式 | Phase 3 | 任意 hook/枚举策略 | 枚举型受控策略 |
| 外部实例处理 | Phase 3 | 接管/提示/忽略 | 提示并区分 ownership |
| Catalog 签名格式 | Phase 4 | 成熟离线签名方案 | canonical 数据 + 现代签名算法 |
| 自动更新默认值 | Phase 4 | 自动/通知/手动 | 通知更新 |
| 新权限更新 | Phase 4 | 自动批准/重新确认 | 必须重新确认 |
| Monorepo 前端工具 | Phase 7 | npm/pnpm | npm workspace |
| sidecar IPC | Phase 6 | named pipe/stdio | named pipe，stdio 作测试/引导 |
| sidecar UI | Phase 6 | 宿主静态/动态 Bundle | 宿主静态或 schema 渲染 |
| 数据不可逆迁移 | Phase 6 | 允许/禁止 | 首批禁止 |
| WSL sidecar | Phase 6 后 | 迁移/留宿主 | 留宿主，专项批准后再动 |
| 扩大复杂拆包范围 | Phase 7 后 | 扩大/停止 | 未达指标则停止 |

### 18.1 MVP 进入门

- 四维状态和逻辑安装语义确认；
- 首页/模块中心职责确认；
- 全部业务入口完成清点；
- Core、普通托管、复杂功能分类完成。

### 18.2 MVP 完成门（Phase 2）

- 用户可在独立模块中心选择功能；
- 未安装/停用模块所有入口均不可调用；
- 生命周期和 Operation 统一；
- 工作台只展示已选功能；
- UI 不把逻辑卸载宣传成物理拆包。

### 18.3 声明式迁移门（Phase 3 → 4）

- MarkerOn/Rufus 共享内核稳定；
- journal 恢复和回滚通过；
- 受控策略无需任意脚本；
- ProcessSupervisor 能正确区分实例所有权。

### 18.4 sidecar 进入门（Phase 5 → 6）

- 普通托管主线已稳定，不再被复杂包设计反复打断；
- 签名目录、ArtifactManager、ProcessSupervisor、Operation 可复用；
- OCR 物理拆包收益和边界得到验证；
- Broker/协议原型不要求动态 Vue Bundle；
- 数据、权限和回滚方案通过评审。

### 18.5 扩大范围门

- OCR、FileShare 至少经过两个稳定发布周期；
- 无 P0/P1 供应链、数据或网络事故；
- host 体积、初始下载、独立更新或故障隔离有实测收益；
- sidecar 维护成本可控；
- WSL 等高风险模块仍需单独批准。

---

## 19. 明确不做

即使本计划完成，也明确不做：

1. 公共插件商店；
2. 任意 URL 安装模块；
3. 未签名包“一键忽略继续”；
4. manifest 携带任意安装/卸载脚本；
5. Go `plugin` 或进程内未知 DLL；
6. 第一阶段动态 Vue Bundle 平台；
7. 第一阶段动态注册 Wails Service；
8. 模块直接取得完整宿主对象、Secret Store 或提权句柄；
9. 仅靠 SHA256 建立官方身份；
10. 仅靠 JobObject 宣称沙箱；
11. 卸载模块默认删除用户数据或上游软件；
12. 把所有 41 个模块都强制物理拆包；
13. 把 WiFi、PortScan、Softver 定为首批物理拆包样本；
14. 未经专项评审拆分 WSL；
15. 把首页改造成完整 Catalog 页面；
16. 为 Monorepo 引入无必要的重型编排平台；
17. 把逻辑安装态宣传为减少 `hanxi.exe` 体积；
18. 因源码同仓而强制产物同包。

---

## 20. 成功指标

### 20.1 用户价值 MVP（Phase 0–2）

| 指标 | 目标 |
| --- | ---: |
| 用户可区分 Installed/Enabled/Runtime/Health | 可用性测试正确率 100% |
| 未安装模块仍可从旁路执行业务 | 0 |
| 停用后资源残留 | 0（纳入测试的代表模块） |
| 首页被完整模块目录占据 | 否，保持工作台定位 |
| 模块中心可完成安装/停用/卸载/诊断 | 100% 代表流程 |
| 逻辑卸载被描述为释放宿主空间 | 0 |
| 生命周期非法转换 | 0 |
| 宿主退出后的受管孤儿进程 | 0 |

### 20.2 共享托管内核（Phase 3–5）

| 指标 | 目标 |
| --- | ---: |
| MarkerOn/Rufus/QuickLook 重复主流程 | 迁移后为 0 |
| 普通托管模块复用共享主流程 | 适合迁移者达到明确多数 |
| 安装中断后旧版本可用率 | 100% |
| 更新失败回滚成功率 | 100%（测试矩阵） |
| 未签名/篡改声明被执行 | 0 |
| manifest 任意脚本或任意命令入口 | 0 |
| 上游发布者被错误标为 Hanxi | 0 |
| 共享缺陷需逐模块重复修复 | 显著下降并可量化 |

### 20.3 真正物理拆包（Phase 6）

| 指标 | 目标 |
| --- | ---: |
| 已拆 OCR/FileShare 重型后端仍进入 host | 0 |
| sidecar 安装需要替换 host | 0 |
| sidecar 崩溃导致 host 崩溃 | 0 |
| 不兼容 sidecar 被启动 | 0 |
| 卸载后的 sidecar 程序残留 | 0，或明确 pending reboot |
| 保留数据重装恢复成功率 | 100%（支持 schema） |
| 更新失败后旧 sidecar 可恢复 | 100% |
| WSL 未经批准被纳入拆包 | 0 |

### 20.4 发布与治理

- host、声明包、sidecar、Catalog 可独立构建和签名；
- 每个产物可追溯到源码 tag、SBOM、provenance 和签名 key；
- 撤回、key 轮换、错误撤回和离线恢复完成演练；
- Release 文案与实际数据根、来源和能力一致；
- 私有 OCR 资产进入公开仓库或 Release 的次数为 0；
- 新普通托管模块主要工作是声明差异和证据，而非复制基础设施；
- 新复杂 sidecar 必须证明声明模型不能覆盖且独立交付收益成立。

### 20.5 停止条件

出现以下任一情况，暂停后续物理拆包：

- 共享托管内核仍不稳定，普通模块迁移持续回归；
- sidecar 需要暴露大面积宿主私有 API 才能工作；
- 需要放宽签名、权限或网络安全边界；
- 数据迁移和回滚事故不可接受；
- OCR、FileShare 的体积、独立更新或故障隔离收益不足；
- 签名、撤回和事故响应无法可靠运维；
- 高风险模块只能以接近宿主等价权限运行。

暂停复杂拆包不影响 Phase 0–5 的模块中心、调用门、生命周期、共享托管内核和声明式目录价值。

---

## 21. 关键相对路径索引

- 战略与架构：`docs/PLUGINIZATION_AND_PRODUCT_EVOLUTION.md`、`docs/PROJECT_STRATEGY.md`、`docs/ARCHITECTURE.md`
- 数据根：`docs/plans/PLAN_PATHS.md`、`internal/settings/paths.go`
- 模块契约：`internal/extapi/module.go`、`internal/extapi/registry.go`
- 装配与服务：`internal/app/app.go`、`internal/app/service.go`
- Composition Contract：`internal/app/composition_contract_test.go`、`scripts/fixture/composition_contract.json`
- 首页工作台：`frontend/src/views/HomeView.vue`
- 导航与 Shell：`frontend/src/App.vue`、`frontend/src/constants/navigation.ts`、`frontend/src/components/shell/`
- 托管事务样板：`internal/modules/ocr/hosted.go`、`internal/modules/litemonitor/version/manager.go`
- Phase 3 样本：`internal/modules/markeron/`、`internal/modules/rufus/`
- Phase 4 第三样本：`internal/modules/quicklook/`
- Phase 6 样本：`internal/modules/ocr/`、`internal/modules/fileshare/`
- 后置模块：`internal/modules/wsl/`
- Windows 进程治理：`internal/platform/windows/job.go`、`internal/platform/windows/process.go`
- 构建：`Taskfile.yml`、`build/Taskfile.yml`、`build/windows/Taskfile.yml`
- CI/Release：`.github/workflows/ci.yml`、`.github/workflows/release.yml`
- 当前整体前端嵌入：`embedassets.go`

---

## 22. 最终结论

Hanxi 官方模块按需使用的正确主线，不是先建设动态 UI 平台并拆分几个简单页面，而是先解决功能过多、状态不清和入口不受控：Phase 0–2 用独立模块中心、逻辑安装态、统一调用门和生命周期交付用户价值 MVP；Phase 3–5 用 ArtifactManager、ProcessSupervisor、Operation 和签名声明目录平台化普通托管模块；Phase 6 才对 OCR、FileShare 等复杂官方功能实施 sidecar 物理拆包，WSL 保持后置。

整个改造必须持续守住四条边界：

1. **官方模块按需分发，不是第三方插件市场；**
2. **首页是工作台，模块中心是独立管理入口；**
3. **源码同仓不等于产物同包；**
4. **逻辑安装、托管资产安装和复杂功能物理拆包必须如实区分。**
