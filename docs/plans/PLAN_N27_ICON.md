# N27 侦查收口：托管软件真实图标进 hanxi（可行性 + 素材底账 + 分批方案）

> 排期出处 [PLAN_REMAINING_WORK](PLAN_REMAINING_WORK.md) N27（机主诉求 2026-09-23）。
> 本文=排期要求的"半天侦查"交付物：只读侦查+一次性探针实证，**未动产品代码**。

## 1. 侦查结论（四项逐条）

### ① 取材通道：定"一次性 ExtractIconEx 脚本 → 静态 PNG 入库"，否决运行时提取

- **实证**（2026-09-24，本机）：PowerShell `shell32!ExtractIconEx(index=0)` 从
  `versions/ccswitch_3.20.1/cc-switch.exe` 取主图标 → `Icon.ToBitmap()` →
  **32×32 PNG 2137 字节**，通道成立。
- 排期判断照单全收：**静态入库优于运行时提取**——版本目录可能未装、前端取图
  通道（asset 协议）改造不值当；图标是"集成时的一次性工序"产物，与
  hanxi-ocr 私发组件同范式（制作入库一次，运行期零依赖）。
- 落点 `frontend/src/assets/apps/<module>.png`（32px 归一），命名即模块 ID。
- **不引入常驻 Go PE 图标解析件**：一次性脚本即可（本侦查的 PowerShell 形态
  收编成 `scripts/` 工具脚本，或开发期手工执行留档步骤——量小、非 CI 必需）。

### ② 展示面：三级回退（真图标 → `i:` 矢量 → emoji/类型图标）

- 现有 AppIcon 注册表 54 枚全自绘矢量（`i:` 前缀）；真图标作第二来源，
  命名口径 **`app:<moduleId>`** 进 nav/module 数据层，AppIcon 家族扩一个前缀
  分支（import.meta.glob 静态表，编译期打包），其余消费位（rail/侧栏/模块卡/
  托管列表行/托盘子菜单）自动受益，零散点不改。

### ③ 取不到的如实降级

- 无 GUI 头件（ddnsgo/frpc 核心=控制台二进制）、图标藏在伴生 DLL 的：
  统一"托管工具通用徽标"（新绘一枚 `app:generic`，不装真）。
- 机主手头官方 logo 可人工投喂（"机主交卷"范式），优先级最低。
- **许可红线**：每个上游 logo 入库前逐个过许可（多为 MIT/免费软件自带资源；
  Sysinternals 系特殊——微软条款禁再分发位图，RAMMap 落通用徽标），
  实施批次逐个记 THIRD_PARTY_NOTICES，不一次性含糊过账。

### ④ 托盘子菜单图标：**硬不确定项解除——Wails v3 beta.10 已开口子**

- 侦查实证（vendored `wails/v3@v3.0.0-beta.10`）：
  - 公开 API：`application.MenuItem.SetBitmap(bitmap []byte) *MenuItem`
    （`pkg/application/menuitem.go:328`）。
  - Windows 通路：`pkg/w32/icon.go:SetMenuIcons` 吃 **PNG 字节** →
    `pngToImage` → `CreateHBITMAPFromImage` → `SetMenuItemBitmaps(MF_BYCOMMAND)`
    ——**无需 owner-draw/MFT_BITMAP hack**（排期最坏剧本被否）。
  - 托盘右键走 `popupmenu_windows.go`，同一 SetMenuIcons 通路；hanxi 侧
    `internal/app/tray.go` 的 `newTrayMenuBuilder` 建项处可直接挂。
- **残余风险（真机定）**：`SetMenuItemBitmaps` 是传统 Win32 菜单语义——
  位图按原生尺寸绘制、PNG alpha 压平可能白底描边、Win11 深色菜单观感需亲验；
  不达标就整批降级为"仅前端展示图标、托盘文字菜单"，**不 hack 原生菜单**。

## 2. 素材底账（本机已装 24 版本目录，实施批次逐个跑脚本提取）

| 类别 | 模块 | 头件 |
|---|---|---|
| 有 GUI 头件可直提 | ccswitch✅(已验)、bcu(BCUninstaller.exe)、everything、flclash、guoheview、keyviz、litemonitor、markeron、mangodisk、nanazip、papertodo、paseo、piclite、quicklook、rufus、snipaste、subnetdesk、translucenttb、douzy、windterm | 各 versions/<mod>_*/<主 exe> |
| 控制台/无主图标 | ddnsgo、frpc | 落通用徽标 |
| 许可受限 | rammap(Sysinternals) | 落通用徽标（除非人工投喂+条款过审） |

（"✅已验"=本侦查实测提取成功；其余为同通道推断，实施时逐个跑脚本核实，
取不出的如实落通用徽标，不硬凑。）

## 3. 分批实施方案（侦查后拆分，替代排期原"前后端两批"的粗线条）

- **批 A（前端件+工具脚本）**：`scripts/` 提取脚本留档；AppIcon 扩 `app:`
  前缀（glob 静态表 + 类型）；`app:generic` 通用徽标新绘；对已验模块先落
  1~2 枚真图标跑通展示面三级回退 + 单测。
- **批 B（导航数据接线）**：模块 Nav/目录 `Icon` 字段支持 `app:` 值
  （后端 catalog/nav 注册处逐模块改），前端零改动自动渲染；emoji/矢量旧值
  不动，回退链天然兼容。
- **批 C（托盘子菜单，独立可砍）**：tray.go 建项处 SetBitmap 挂同一批静态
  PNG（Go 侧读 frontend/dist 内嵌 FS 或双份 embed——实施时定，注意 bundle
  体积）；真机验收不过 → 整批降级为文字菜单并在排期如实记"④不过"。
- 每批独立绿门禁+原子提交；C 依赖 A 的 PNG 底账，B 只依赖 A 的注册表。

## 4. 验收口径

- 前端：AppIcon `app:` 单测（存在/缺回落）、模块卡/rail 快照断言。
- 托盘：真机——图标渲染无白框、尺寸协调、深浅菜单皆可读；不过按批 C 降级。
- 门禁：`hanxi check` 全量 + 新 PNG 资产逐个过 THIRD_PARTY_NOTICES 清单。
