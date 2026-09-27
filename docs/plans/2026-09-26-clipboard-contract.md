# 剪贴板内置 · 多 agent 并行接口契约(唯一事实源)

> 本文件是剪贴板内置模块并行开发期所有 agent 的**冻结契约**。
> 与契约不符的实现一律在收口阶段修正;agent 不得擅自改本文件与共享文件。
> 借鉴来源与设计决策的论证不在本文(见 A1 产出的决策档案),本文只钉接口。

## 1. 范围边界(本轮明确不做)

Win+V 接管、HTML/RTF 保真、延迟渲染、网络同步、脚本引擎、正则搜索、OCR 图片内容入库。
以上全部登记为后续里程碑,本轮不实现、不预埋半成品。

## 2. 数据布局

```
<DataDir>/clipboard/
  index.json     {"version":1,"entries":[Entry...]}  新→旧排序,整库原子写(jsonstore)
  blobs/         内容寻址 PNG:<sha256>.png(图片条目)
```

- **同步红线**:`data/clipboard/` 必须进发布链同步排除集(收口阶段由主控落到
  scripts/release 的 .syncignore 契约;agent 不动该文件)。
- `Entry.text` 落盘恒为 `base64(DPAPI(UTF-8))`(internal/platform/windows/dpapi.go);
  图片 kind 的 text 为空串不加密。无头侧(MCP)解密用同一 windows 包函数。
- 去重:`hash = sha256(语义字节)`——text 取原文 utf8;image 取 CF_DIB 原始字节;
  file 取路径列表以 `\n` join。重复复制=顶置 + useCount++/lastUsedAt 刷新,pinned 保留。
- 容量钳制:500 条 / blobs 总量 100MiB / 单条图片 >16MiB 跳过不入库(如实 warn)。
  淘汰只对非 pinned 非 manual 生效,LRU 以 lastUsedAt(缺省 createdAt)。
- 敏感判定(入库时算,`Entry.sensitive`):SourceExe 命中排除表 **或** secret 启发式
  命中(正则: `sk-[A-Za-z0-9_-]{16,}` / `ghp_[A-Za-z0-9]{20,}` / `AKIA[0-9A-Z]{16}` /
  `xox[abprs]-` / `BEGIN [A-Z ]*PRIVATE KEY`)。命中排除表的条目**不入库**;
  仅命中启发式的入库但 sensitive=true(MCP 通道整条不下发)。
- 排除表默认(小写进程名):1password.exe, bitwarden.exe, keepass.exe, keepassxc.exe,
  lastpass.exe, 1password.exe, veracrypt.exe, truecrypt.exe;子串含 "password" 亦排除。

## 3. Go 包与数据模型(包 `clipboard`,目录 `internal/modules/clipboard`)

```go
type Kind string

const (
    KindText  Kind = "text"
    KindImage Kind = "image"
    KindFile  Kind = "file"
)

// Entry 一份剪贴板历史。JSON tag 即 wire/落盘字段名,与前端 types/clipboard.ts 逐字对齐。
type Entry struct {
    ID         string   `json:"id"`         // 16 hex 随机
    Hash       string   `json:"hash"`
    Kind       Kind     `json:"kind"`
    Text       string   `json:"text,omitempty"`       // wire 上=明文;落盘=base64(DPAPI)
    Preview    string   `json:"preview"`              // ≤120 rune 首行摘要,列表页用
    Files      []string `json:"files,omitempty"`      // CF_HDROP
    Blob       string   `json:"blob,omitempty"`       // "blobs/<sha>.png"
    Width      int      `json:"width,omitempty"`
    Height     int      `json:"height,omitempty"`
    ByteSize   int64    `json:"byteSize"`
    SourceApp  string   `json:"sourceApp,omitempty"`  // 复制时前台窗口标题
    SourceExe  string   `json:"sourceExe,omitempty"`  // 小写进程名,无路径
    AutoTags   []string `json:"autoTags,omitempty"`   // url/email/phone/color/code/cjk
    Sensitive  bool     `json:"sensitive,omitempty"`
    Pinned     bool     `json:"pinned,omitempty"`
    Manual     bool     `json:"manual,omitempty"`     // 固定片段(手建,不淘汰)
    CreatedAt  int64    `json:"createdAt"`            // ms
    LastUsedAt int64    `json:"lastUsedAt,omitempty"`
    UseCount   int      `json:"useCount,omitempty"`
    BlobData   []byte   `json:"blobData,omitempty"`   // 仅 Get 回填(PNG 原始字节)
}

type Status struct {
    Paused       bool     `json:"paused"`
    EntryCount   int      `json:"entryCount"`
    BlobBytes    int64    `json:"blobBytes"`
    MaxEntries   int      `json:"maxEntries"`    // 500
    MaxBlobBytes int64    `json:"maxBlobBytes"`  // 100<<20
    ExcludedExes []string `json:"excludedExes"`
}
```

AutoTags 判定(入库时,纯函数):http(s):// → `url`;邮箱正则 → `email`;
`^#[0-9a-fA-F]{6}$|^#[0-9a-fA-F]{3}$` → `color`;含 ``` 或 ≥3 行且命中关键字
(func|class|def|import|SELECT|{; 等)→ `code`;手机 `1[3-9]\d{9}` → `phone`;
含 CJK → `cjk`。多标签可共存,按上序去重输出。

## 4. 服务面(Wails 绑定;struct `ClipboardService`)

方法集冻结(名称/签名不得擅改;构造与生命周期 Start/Stop 照 memo.MemoService 同谱,
由收口阶段接线):

```go
List(q, kind string, limit int) ([]Entry, error)  // q 空=全量;kind 空/`"all"`=不过滤;limit≤500;不含 Text/BlobData
Get(id string) (Entry, error)                     // 含解密 Text 与 BlobData;记一次使用(useCount++/lastUsedAt)
Set(id string) error                              // 回填系统剪贴板(OpenClipboard 重试 ≤5 次)
CreateText(text string) (Entry, error)            // 手建片段 manual=true
TogglePin(id string) (Entry, error)
Delete(id string) error
ClearAll() error                                  // 含 blobs 全清;托盘一键擦除走这里
SetPaused(paused bool) error
GetStatus() (Status, error)
```

List 的 q:小写子串匹配(Preview/SourceApp/Files/AutoTags 拼接后 Contains),非正则。

## 5. 监听与事件

- message-only 窗口 + `AddClipboardFormatListener`,`WM_CLIPBOARDUPDATE` 触发读取
  (CF_UNICODETEXT → CF_DIB → CF_HDROP 优先序);跳过自身 Set 的回环
  (IsClipboardFormatAvailable + 自写标记:GetClipboardOwner==本进程则忽略)。
- 事件名 + 载荷(Wails Event.Emit,前端 EventsOn):

| 事件 | 载荷 | 时机 |
|---|---|---|
| `clipboard:updated` | Entry(不含 Text/BlobData,Preview 截 200 rune) | 新条目入库或去重顶置 |
| `clipboard:removed` | `{id: string}` | Delete/淘汰/清空(清空允许只发 paused 后静默,实现方标注) |
| `clipboard:paused` | `{paused: bool}` | SetPaused / 托盘开关 |

## 6. 浮层(quickmemo 同谱)

- 窗名 `clipboard-overlay`,URL hash 路由 `/#clipboardoverlay`(main.ts 接线归收口阶段)。
- 形态:frameless 真透明+置顶+不进任务栏;建窗即隐、摆位后一次性露出;
  收起后 2min 空闲 TTL 真销毁(#53 纪律照抄 memo/quicksheet.go)。
- 热键:槽名 `clipboard/overlay`,默认 `Ctrl+Alt+V`(hotkey.Registry.Bind)。
- 清稿事件:`clipboard:overlay:opening`(Void)。

## 7. 前端 kernel(已写好,所有 agent 只读不改)

- `frontend/src/types/clipboard.ts` —— ClipEntry/ClipStatus 等,与 §3 逐字对位。
- `frontend/src/adapters/clipboard.ts` —— transport 注入 + 事件名常量。
  视图**只经** `@/adapters/clipboard` 的函数访问后端(未接线时抛可读错误);
  测试里 `setClipboardTransport(fake)` 注入假件,不 import 生成绑定。
  事件订阅直接用既有 `useWailsEvent` + 常量。
- 收口阶段主控把真实 transport(生成绑定桥)接进 setClipboardTransport,前端零改动。

## 8. MCP 工具(`internal/mcp/tools_clipboard.go`,注册函数 `registerClipboardTools`)

- 工具名 `hanxi_clipboard_search`,参数:`keyword` / `tag` / `kind` / `limit`(≤200,默认 50)。
- 零落盘直读 `<DataDir>/clipboard/index.json`(照 tools_memo.go 的 memoDiskReader 谱系:
  不复用 ClipboardService——其构造带写盘副作用;解析失败单条跳过并 warn)。
- `sensitive=true` 整条不下发、不计入命中数(memo IsMasked 同口径)。
- 输出条目:`id/kind/preview/text(仅 text 类,明文,单条 >64KiB 截断置 truncated)/
  files/autoTags/pinned/sourceApp/createdAt`;image 只给元数据。
- 行内脱敏复用 mcp 包既有 sanitize 纪律(IPv4→[ipv4] 等)。
- 注册函数签名与 tools_memo.go 的注册函数对位,收口阶段 server.go 一行接线。

## 9. 文件所有权(越界=事故)

| agent | 独占文件(新建) |
|---|---|
| A1 决策档案 | `docs/plans/2026-09-26-clipboard-decision.md`;`docs/plans/PLAN_REMAINING_WORK.md` 追加一行 |
| A2 后端核心 | `internal/modules/clipboard/{doc,models,store,listener,service,overlay,hotkey}*.go` + 同目录 *_test.go |
| A3 MCP | `internal/mcp/tools_clipboard.go`、`tools_clipboard_test.go`、`internal/mcp/testdata/clipboard/` 夹具 |
| A4 主界面 | `frontend/src/views/ClipboardView.vue`、`components/clipboard/**`(含各自 __tests__) |
| A5 浮层 | `frontend/src/views/ClipboardOverlay.vue`、`views/__tests__/ClipboardOverlay.spec.ts` |
| A6 blob/DIB | `internal/modules/clipboard/{blobs,dib}.go` + `{blobs,dib}_test.go` |

**共享/在途脏文件全禁**:internal/app/**、internal/mcp/{mcp,server}.go、mcpwizard/**、
frontend/{vite,vitest}.config、main.ts、constants/**、styles/**、package.json、
scripts/**、go.mod/go.sum(禁新增依赖)、bindings/**。
git 一律不 commit/push/stash/checkout。

## 10. A6 冻结签名(A2 并行依赖,只按此调用)

```go
func NewBlobStore(dir string) *BlobStore                    // dir = <DataDir>/clipboard/blobs
func (b *BlobStore) SaveImage(img image.Image) (rel string, width, height int, err error)
    // PNG 编码→sha256 命名→幂等(同图同 rel);rel 形如 "<sha>.png"(相对 dir)
func (b *BlobStore) Load(rel string) ([]byte, error)        // rel 必须 sanitize(拒 ../ 与绝对路径)
func (b *BlobStore) Delete(rel string) error
func (b *BlobStore) TotalBytes() (int64, error)
func DecodeDIB(dib []byte) (image.Image, error)
    // CF_DIB 字节流 → image;BI_RGB 24/32bit + BI_BITFIELDS 32;底向上行序自适配;
    // 单色/8bit 调色板降级为近灰,不支持的压缩返回 error。
```

## 11. 每 agent 自检与汇报(修订见 §12)

- Go:`gofmt -l` 净;`go build ./internal/modules/clipboard/`(A2/A6;A3 可过 `go vet ./internal/mcp/`
  在 A2 落地前失败,标注即可);`go test ./internal/modules/clipboard/ -run Clip -count=1` 等自包测试。
- 前端:`node node_modules/vitest/vitest.mjs run <你的spec路径>`(npm run 的 shim 在本机 Git Bash 是坏的)。
- 汇报:文件清单 / 自检结果 / 契约偏差与残留(交收口)/ 不猜——拿不准的问题列表。

## 12. 修订记录(主控裁决,晚于本节的以本节为准)

**v1.1(2026-09-26,A1 交报采纳其缺口 1/2/4)**

1. **§9 A2 行补 `module.go`**:A2 独占文件增加 `module.go`——至少含
   `const ID = "clipboard"`(store.go 已在引用),并照 memo 模块的 ModuleID/目录键
   范式声明导航与 MCP 授权门所需的模块标识。**catalog 注册、mcpwizard 授权键、
   navigation.ts 入口仍归收口主控**,A2 不得触碰任何共享文件。
2. **access.json 第十键**:MCP 注册按 ModuleID 走授权门(memo 键先例),登记入收口
   清单;A3 的注册函数设计需接受该门控形态(与 tools_memo 对位即可,无额外要求)。
3. **§2 "scripts/release 的 .syncignore" 系契约错误**:实扫 scripts/ 无该机制。
   同步排除的落点改判为:收口阶段实测 `settings.GetPaths()` 的 DataDir 实体位置——
   若 DataDir 位于随飞牛同步的 hanxi 目录树内,则 clipboard 数据目录必须自定义
   落点或用 NAS 侧忽略规则实现,届时以主控新钉替换本条。**风险 1(DPAPI×同步)的
   实体缓解此项不可漏,禁止静默略过。**
4. **"粘贴前编辑"(§1 谱系内 CopyQ 借鉴项)本轮裁决降级**:冻结服务面不加
   `Edit(id,text)`;详情面板只读 + `CreateText` 旁路足够 MVP,就地编辑挂 M3 复议。
5. 星数与 owner 路径以 A1 档案复验版为准(`PasteBar/PasteBarApp`、
   `mnardit/beetroot-releases`);tiez 表述修正为"原生后端+WebView 前端"。

**v1.2(2026-09-26,A3 交报采纳其全部判断点与硬约束)**

1. §8 增补四项(批准入规):整页明文预算 512KiB(超置 `textOmitted`,条目身份保留);
   files 单条上限 200(`filesTruncated`);limit<1 回落默认 50、>200 钳上限;
   keyword 匹配面比 GUI List 多覆盖解密正文 + SourceExe(MCP 面职责="找回复制过什么")。
2. version 口径分面裁决:无头 MCP 面 version≠1 **fail-loud**(只读拒载优于把改形库
   喂给云端);GUI 面宽容装载允许,但见第 3 条改造。
3. **A2 store 装载容错改判**:整档 Unmarshal 失败不得连坐隔离改名——单条坏条目
   跳过 + warn(隔离保真原件,memo 隔离改名同谱);A2 若已实现整档 fail-closed,收口前改。
4. **无头表禁令(安全级)**:clipboard 模块禁入 mcpModules/registryGate 懒激活
   (NewStore 构造即建目录/空库回写,毁掉无头零落盘承诺);启用门照 memo 先例走
   config.json 直读 enabled + receipt。
5. 收口扩表三处同批(漏一处=整档 access.json fail-closed 连坐封死):
   `knownModuleIDs` / mcpwizard `accessToolKeys` / guards_test 白名单
   (`"hanxi_clipboard_search": "clipboard"`);`clipboardAccessKey` 字面量收口时
   改 `clipboard.ID` 引用。均归主控,登记决策档案⑤收口清单。
6. 未知 kind(未来形态)在 kind=all 中按仅元数据下发、kind 字段如实申报,不隐身。
7. TROUBLESHOOTING 待沉淀一条:构造即写盘的模块进无头 registry 的零落盘陷阱
   (收口阶段主控落笔,届时 TROUBLESHOOTING 若仍在他线在途则缓写并报)。

**v1.3(2026-09-26,A6 交报三偏差裁决:全数认可)**

1. `image/bmp` 已于 Go 1.26 物理移除——"与 stdlib 对拍"的任务书要求作废,
   A6 的三方比对(构造黄金像素×内置参考解码器×DecodeDIB)认可为等价强度;
   **本条同时是仓库级事实,记 TROUBLESHOOTING 候选。**
2. `TestDib*` 命名保 `-run 'Dib|Blob'` 真实覆盖——认可,防假绿纪律。
3. BI_RGB 32bpp 忽略垃圾 alpha 强制不透明(真 alpha 仅 BITFIELDS 通道)——
   定性为**正确性修复**而非偏差,与旧 GDI/bmp 阅读语义一致。
4. 遗留收口小项:BlobStore.Delete 无锁、与 Save 竞态可复活同字节文件
   (孤儿 blob,账目仍对)——收口时评估一行改法(Delete 纳入 b.mu)。

**v1.6(2026-09-27,机主双裁决:监听修复 + 取消 DPAPI)**

1. **监听窗根因修复**:listener.go 曾把 `HWND_MESSAGE` 误写为 `^uintptr(0)`
   (=HWND_BROADCAST -1),CreateWindowEx 报 1408 Invalid window handle,
   监听静默停摆、历史恒空——已改 `^uintptr(2)`(-3)并注释钉死。真机日志
   判据:`clipboard: 剪贴板监听启动失败` WARN。沉淀 TROUBLESHOOTING #103。
2. **取消 DPAPI 加密(机主明示"不需要")**:§2 加密条款作废——text 与 blobs
   一律**本机明文落盘**;seal/open 缝保留(默认恒等,测试假件与未来复议通道),
   MCP reader 解密面同步恒等。敏感兜底收敛为三件套:来源排除表 + sensitive
   标记(MCP 整条不下发)+ 面板默认关。**风险改口如实登记**:数据目录明文,
   一旦入 NAS/网盘同步即明文扩散——同步排除(v1.5.3 挂起)复议时必须回看本条。
3. 在库空态无迁移负担(裁决时 index.json entries 恒空);若未来出现历史密文条,
   open 缝回读失败按"坏条目保真隔离"既有通道处理,不静默吞。

**v1.4(2026-09-26,A8 对抗审查裁决:H1/M1-M4/L1-L4 全采纳,定向回炉)**

1. **H1 收窗机制重造**:§4 冻结面九法→**十法**,新增 `CollapseOverlay() error`
   (overlay.go 内部 hideOverlay 的无门版)。`input.blur()/window.blur()` 唤不动
   顶层窗 HWND 失活,收窗必走显式 RPC(QuickMemoSheet `HideQuickSheet` 同先例)。
   A2 实现;A5 回炉:collapseSelf 改调 RPC,spec 断言"RPC 被调"而非"blur 被调"。
   kernel 的 transport 面由主控同步扩(`clipboardCollapseOverlay`)。
2. **M2 双保险尺寸闸**:§2 增 text 单条 >8MiB 拒入库(warn 如实,与图片 16MiB
   同谱,A2 执行);MCP 装载侧按**解密前密文字节**预筛 + 累计解密预算 4MiB,
   超预算条目只给元数据+truncated(A3 回炉)。
3. **M4 裁决(产品口径)**:保留 RedactPII 打码——安全优先,宁可失真不可泄密;
   description 增"文本经启发式打码可能失真,不保证逐字还原"如实申报;
   logging 词边界改造另立后续票,本轮不动磁盘日志形状。
4. **M3 预算账改全**:页预算按**脱敏后**长度计;files 与元数据同入 512KiB;
   单条不可容纳设保底——至少下发纯身份行(A3 回炉)。
5. IME 三重盾对齐 CommandPalette(`isComposing || composing.value ||
   key==='Process'`)(A5 回炉,M1)。
6. L 项分派:L1 jsonstore 错误剥绝对路径留文件名、L2 注释锚点改 module.go、
   L3 坏条目 warn 聚合(N 条+一样例)——A3 回炉;L4 未知 kind 字形兜底——
   A5 浮层侧回炉,主界面侧登记收口清单。

**v1.5(2026-09-26,八线全收 · 用户四项拍板 · 收口挂账)**

1. 托盘「一键擦除」确认闸 = **跳回视图复用前端 useConfirm 富文案闸**(W 项照此执行)。
2. 导航图标 = **新增 clipboard 矢量键**(A7 清单附 d 串方案),module.go 的
   `i:clipboard` 声明保留不动。
3. **同步排除 = 用户裁决「先不考虑」**:§2 同步红线与 §12.1.3 外迁案本轮**挂起**——
   不外迁、不做 NAS 规则、不动任何落点。明文扩散风险(preview/来源/路径随飞牛
   同步上 NAS)由用户明示接受,决策档案②风险 1 状态改"已知情挂起",不静默销账;
   复议触发线:将来任何"给 hanxidata 加同步/备份"的动作须先回本条。
4. 收口时序 = **等 piik 线(另一会话)收线落定后串行开工**(cataloggen/bindings
   双生成闸不得交错;piik 在途红灯不计入剪贴板账)。
5. A3 台账更正:预算阶梯以其实现为准——放得下的最大正文+truncated,
   **连起步价残段都塞不进才降级身份行**(主控前令中"60KiB 配 8000 退身份行"作废)。
6. `internal/app/clipboard_contract_test.go` 查无此档系 **A2 首过临时桩已自删**(与
   其申报一致),非他人删除在制品;"已声明未接线"守卫职责移交收口 W 项
   (toolDefs/白名单/accessToolKeys 三表同批扩 + composition_contract 第四特例)。
7. TROUBLESHOOTING 待沉淀两条(收口人落笔,前提届时该文件非他线脏):
   无头 registry 零落盘陷阱;预算"起步价只拦残段不拦整条"实现陷阱。
