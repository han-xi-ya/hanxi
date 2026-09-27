// Package clipboard 内置模块：系统剪贴板历史（借鉴 Ditto/Win+V 的本地自用形态）。
// 数据面以整库 index.json（<数据根>/clipboard/，version:1，新→旧排序，jsonstore
// 原子写）为权威，图片内容另存内容寻址 blobs/；Entry.text 落盘恒为
// base64(DPAPI(UTF-8))（internal/platform/windows/dpapi.go，与随手记遮罩同谱的
// 用户级透明加密），内存态持明文，读出即解密。
//
// 采集面（listener.go）：message-only 窗口 + AddClipboardFormatListener 订阅
// WM_CLIPBOARDUPDATE，读序 CF_UNICODETEXT → CF_DIB → CF_HDROP；本进程自写
// （Set 回填）经 GetClipboardOwner==本进程 抑制回环。入库口（store.Add 前）执行
// 排除表（密码管理器等敏感来源不入库）与 secret 启发式（入库但置 sensitive，
// MCP 通道整条不下发）；去重顶置、500 条 / 100MiB / 单图 16MiB 钳制与 LRU
// 淘汰（pinned/manual 不淘汰）全部在 store.go 收口。
//
// 服务面（service.go）：ClipboardService 方法集逐字冻结于
// docs/plans/2026-09-26-clipboard-contract.md §4，事件名与载荷见 §5；浮层
// （overlay.go，随手记速记卡同谱）与全局热键（hotkey.go，槽 clipboard/overlay，
// Ctrl+Alt+V）。
//
// 生命周期纪律（与 memo 同谱）：构造期只装载数据；start/stop 由模块装配驱动，
// 应用未运行（无头单测/装配前）时监听不启动、浮层唤出返回可读错误，全程不 panic。
// 除监听消息泵与浮层空闲销毁计时器外无常驻 goroutine，无网络面。
//
// 本包 blobs.go / dib.go（BlobStore 与 CF_DIB 解码）由并行线 A6 落地，
// 本线按契约 §10 冻结签名直调。
package clipboard
