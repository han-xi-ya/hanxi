# W6 · N13 全平台发布物标注——底账与契约方案（开工前评审件）

> 排期出处 [PLAN_REMAINING_WORK](PLAN_REMAINING_WORK.md) N13/W6（机主 2026-09-20
> 登记）："托管安装/发布列表里要把 win / macOS / Android APK 都显示出来，
> 便携版还是标准安装版要写清楚。"本文两件事：①**底账**——28 个托管/半托管
> 模块的上游真实发布形态逐个数清（2026-09-23 GitHub API 实拉 + 代码注释核对）；
> ②**契约方案**——这些数据要进哪个结构、UI 怎么标、边界在哪。

## 1. 底账矩阵

### 1.1 GitHub 上游 23 款（releases/latest 实拉，缓存于 .cache/w6probe/）

| 模块 | 最新 tag | 上游实际发布形态（平台:形态） | Hanxi 现收形态 |
|---|---|---|---|
| bcu | v6.3 | win: installer-exe；归档:7z/zip | win 便携 zip（innerExe 引导器形） |
| bili23 | v2.15.0 | **linux + macOS + win(安装器&便携)** | win 便携 zip |
| ccswitch | v3.20.4 | linux + macOS + win(msi & 便携 zip) | win 便携 zip |
| ddnsgo | v6.17.7 | linux + macOS + win zip | win zip（服务器 CLI 形态） |
| douzy | desktop-v0.12.0 | macOS(dmg) + 其余待复核 | win 安装器（仅版本+下载，不接管） |
| flclash | v0.8.98 | **android(APK) + linux + macOS + win(安装器&zip)** | win 便携 |
| frpc | v0.71.0 | linux + macOS + win zip | win zip（CLI） |
| keyviz | v2.1.1 | macOS + win(msi) | win msi（安装器托管） |
| litemonitor | v1.3.6 | win 便携 zip | 同 |
| mangodisk | v1.1.3 | macOS + win(安装器&便携) | win 便携 |
| markeron | v2.10.1 | macOS + 归档(zip) | win 便携 zip |
| nanazip | 7.0.1843.0 | GitHub 归档(msix/zip)；实际渠道=MS Store | 直装渠道纳管（不下载 zip） |
| papertodo | v3.31 | win exe | win exe 收纳 |
| paseo | v0.9.1 | **android(APK) + linux + macOS** + win | win 侧收 |
| piclite | v1.8.6 | macOS + win 便携 zip | win 便携 |
| quicklook | 4.5.0 | 归档(zip/msix) | win 便携 |
| recordly | v1.4.0 | linux + macOS + win 安装器(NSIS) | win NSIS 安装器 |
| rufus | v4.15 | win 单 exe + 归档 | win 单 exe（digest+MZ 校验） |
| rustdesk | 1.4.9 | **android(APK)** + macOS + 多平台 | win MSI/hub 双形态 |
| subnetdesk | v1.3.0 | **android(APK)** + macOS + win | win rust-portable 单 exe |
| termora | 1.0.17* | linux + macOS + win(安装器&zip) | win 便携 zip（2.x beta 线，*latest 只回稳定线 1.0.17——本表按 1.x 拉取，2.x beta 资产形态同构） |
| translucenttb | 2026.2 | 归档(zip/msix) | win zip 便携 |
| windterm | 2.7.0 | linux + macOS + win 便携 zip | win 便携 zip |

**读表要点**：①带 **粗体平台** 的上游（flclash/paseo/rustdesk/subnetdesk 的
APK、近十款的 macOS 线）= 机主要在列表里"看见但 Hanxi 不安装"的对象；
②`win(安装器&便携)` 双形态并存的 7 款（bili23/ccswitch/flclash/mangodisk/
termora/bcu/recordly 侧证）= "便携 vs 标准安装"标注的核心场景——同一版本
两条腿，现在列表只显示我们收的那条。

### 1.2 非 GitHub 上游 5 款

| 模块 | 源 | Hanxi 现收 | 上游全平台性 |
|---|---|---|---|
| everything | voidtools 官网 | win 便携/安装双族自持 | 仅 win 系（mac/linux 无官方原生） |
| guoheview | rj.lovestu.com 自建 API | win 便携 zip（beta 通道同收） | 官网并列发布"安装版"（资产名已标注便携版字样） |
| snipaste | snipaste.com | win zip 便携 | 官网有 macOS 线（现列表未取） |
| vscode | 微软 update 网关 | Inno 安装版 + zip 便携双 Engine | win/mac/linux 全平台，CDN feed 分平台取 |
| rammap | Sysinternals 直链 | win zip（三架构同包） | 三架构 exe 已在同一 zip 内（x86/x64/arm64） |

（MooTool 借鉴池、nanazip/eartrumpet 直装渠道等"不下载二进制"型纳管，平台
标注语义 = 渠道本身，见 §2 边界。）

## 2. 契约方案（候选设计，评审后定稿）

### 2.1 数据面：release 结构补"全发布物矩阵"，托管语义不变

现状：各模块 `version.XxxRelease` 只带**我们收的那一条**资产（AssetName/
URL/Size/摘要），上游其余平台的发布物在 remote 解析时直接丢弃——列表
"看不见"不是 UI 没画，是**数据层就没留**。

设计（增量、向后兼容）：
```
// packages/go/artifact（或新建 packages/go/releasefeed）统一词汇表：
type Platform = "windows" | "macos" | "linux" | "android" | "other"
type Form     = "portable" | "installer" | "package" | "archive" | "cli" | "apk"

type AssetNote struct {   // 纯展示元数据，永不参与下载/校验路径
    Platform  Platform
    Form      Form
    Label     string   // 上游原始资产名（可复制）
    Managed   bool     // 本条是否即 Hanxi 托管所选（高亮"当前托管形态"）
}
// 各模块 XxxRelease 增字段：Assets []AssetNote `json:"assets,omitempty"`
```
- 各 remote.go 解析处顺手保留全资产清单（解析成本零增加——API 本来就
  返回全量），经**平台/形态分类器**（本次底账的分类型抽成共享纯函数 +
  表驱动测试，命名匹配规则按 §1 底账逐上游钉死）产出 Assets；
- 分类器进 packages/go（多模块消费、带 fixture 测试），模块 remote 只
  调用不改写；托管下载路径**继续走各模块自己的 findPortableAsset 判据
  不动**——Managed 标记反向由"选中的那条资产名"比对得出。

### 2.2 UI 面：ManagedVersionPanel 远程表加一列"平台/形态"

- 每行 release 的 Assets 渲染成徽标组：`🪟 便携` `🪟 安装器` `🍎 dmg`
  `🤖 APK` `🐧 tar.gz`（AppIcon 家族，禁 emoji 进主 UI——用徽标色形），
  Managed=true 的那枚加"本托管"主色描边；
- 非托管平台的条目**只标不点**（无下载钮）：APK/mac 对 Windows 宿主机的
  意义 = "官方还发这个，你要去别的设备用"——tooltip 写明；
- 版本行徽标位已有（#version-row-extra 方言槽），列级新增走面板契约扩展
  （批 3 域，改动集中共享壳一处，21 模块零方言自动受益）。

### 2.3 边界与不做

- ✗ 不做跨平台下载引导（给 mac/APK 放"下载"钮）：hanxi 是 Windows 宿主
  工具，显示=知情，安装动作仍限 Windows 资产；
- ✗ 直装渠道型（nanazip/eartrumpet）不编造假矩阵：其"平台/形态"徽标 =
  `MS Store/winget 渠道`单一标注（AppxManifest 里 msix/x64 信息可顺带，
  实施时定）；
- ✗ 历史版本全平台回溯（每版都拉全资产矩阵）按需做：latest-per-tag 数据
  GitHub API 逐 release 有量，首版**当前列表各 release 现拉现标 + 10min
  缓存**已够，列表分页深翻的速率账后续单独评；
- 上游资产改名风险：分类器失配时降级为 `other/archive + 原名展示`，
  绝不猜标（表驱动 fixture 测试钉住 28 家现行命名）。

## 2.9 实施进度

- ✅ **步骤 3 数据层（2026-09-23）**：20 家 GitHub 模块 remote 全量接
  hostfeed 矩阵（批一 windterm/termora/markeron/ccswitch → 批五
  rufus/ttb/bcu/papertodo，五批五提交），bindings 重生成 20 文件纯增量；
  nanazip/eartrumpet 商店直装型按 §2.3 不编矩阵；**非 GitHub 源 5 家
  （everything/guoheview/snipaste/vscode/rammap）待专项批**——官网/自建
  API/CDN 的资产矩阵来源各异（vscode 双形态本就自绘），单独评审接入方式。
- ✅ **步骤 1 地基（2026-09-23）**：`packages/go/hostfeed` 分类器落地——
  Platform/Form 枚举（含诚实的 `binary` 态：裸 exe 无便携/安装证据不猜标）、
  元数据过滤、Notes 托管标记；测试钉表 = curated 55 例真名 + 273 名全量
  枚举冒烟。**过程中真名面试出两处真 bug**：产品名 "BCUninstaller" 撞
  "install" 子串误判安装器（zip 判据收紧为 setup/nsis 强标记）；
  Notes 未过滤形态级 meta（SHASUMS 类拼写变体漏网）。

## 3. 实施拆分（评审通过后）

1. packages/go/releasefeed：Platform/Form 词汇 + 分类器 + 底账 fixture 测试；
2. ManagedRelease/面板契约 + ManagedVersionPanel 徽标列（含 spec）；
3. 逐模块 remote.go 填 Assets（21+5 家，可两三人畜无害的机械批 + 6 家
   非 GitHub 特殊源单批）；
4. bindings 重生成 + catalog 说明微调 + docs。
预估中等盘；与批 3 同域需错峰（W6 原登记"排在批 3 之后"——批 3 已收，
条件成立）。
