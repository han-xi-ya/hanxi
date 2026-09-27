package mcp

// tools_clipboard.go 是剪贴板历史只读检索工具（hanxi_clipboard_search，契约
// docs/plans/2026-09-26-clipboard-contract.md §8）：无头侧"我刚才复制的那条命令
// 是什么"式找回。四条纪律：
//
//   - 零落盘直读 <DataDir>/clipboard/index.json（memoDiskReader 同谱，包注释决策 3）：
//     不复用 clipboard.ClipboardService——其构造携带监听线程与写盘/淘汰副作用；
//     读侧只按共享读打开，文件/目录不存在是**合法真空库**（剪贴板没启用过就没有库），
//     绝不创建、不迁移、不隔离任何东西。
//   - 磁盘投影结构不绑 clipboard.Entry（契约 §9 并行期纪律的延续）：下方结构与契约
//     §3 的 JSON tag 逐字对齐即唯一同步面，A2 改落盘字段必须同步本表——同
//     memoFileNameRe 的"那里未导出、此处复制并保持同步"钉法。收口期（§12 v1.2.5）
//     仅放行 clipboard.ID 一个常量引用（授权键单一事实源，portkill 先例），
//     结构面解耦不变。
//   - 敏感条目双层剔除：sensitive=true 在 reader 即不进（密文不出 reader），
//     工具层再挡一次（假件或第二实现绕不过）。memo IsMasked 同纪律：整条不下发、
//     不计入命中数，连"过滤了几条"都不外泄（存在性本身即敏感）。
//   - 出机前一律 logging.RedactPII：契约 §8 点名的口径（IPv4→[ipv4]、邮箱→[email]、
//     token=/password= 赋值→掩码、sk-/ghp_ 类前缀密钥→[redacted-key]）。剪贴板正文是
//     机主无意间复制的任意文本，泄露面比机主亲手记进便签的正文更宽，故取 logs 工具的
//     PII 档（RedactPII 内含 Redact，是 memo 窄档的严格超集），而非 memo 的窄档。
//     打码是启发式的、会改形（§12.4 v1.4 裁决：安全优先，宁可失真不可泄密），工具描述
//     如实申报"不保证逐字还原"。
//   - 两级尺寸预算各管一头（§12 v1.4 M2/M3）：装载侧**先看档大小、再按密文长度预筛、
//     最后按累计明文预算（4MiB）决定是否解密**——预算在解密之前花，异常巨大的库最多只
//     把额度内的明文读进内存，超预算条目连密文都不留（只给元数据，置 textOmitted）；
//     出机侧按**脱敏并序列化后的实际字节**记 512KiB 页预算，元数据与文件列表同入账，
//     单条连元数据都放不下时保底只发纯身份行，绝不产出"整页空壳只剩 truncated"。
//   - 错误串只报文件名不报绝对路径（§12 v1.4 L1）：本工具的错误会进云端模型上下文，
//     带盘符的用户目录即机主画像。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/modules/clipboard"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	// toolClipboard 工具英文名是对外契约（客户端配置/授权键/文档引用），改名即断链。
	toolClipboard = "hanxi_clipboard_search"

	// clipboardAccessKey 是剪贴板工具在 access.json 中的授权键：直引 clipboard.ID
	// （clipboard/module.go，portkillAccessKey=portkill.ID 同款先例，契约 §12 v1.2.5），
	// 杜绝两处手打字符串漂移。三处同批扩表（knownModuleIDs / mcpwizard accessToolKeys /
	// guards_test 白名单）已由收口批落地，见 registerClipboardTools 注。
	clipboardAccessKey = clipboard.ID

	clipboardDirName      = "clipboard"
	clipboardIndexName    = "index.json"
	clipboardIndexVersion = 1 // 契约 §2：{"version":1,"entries":[...]}

	clipboardIndexMaxBytes = 32 << 20 // 单档读取上限（M2：先看尺寸再吞档）
	clipboardLoadPlainMax  = 4 << 20  // 单次装载的累计明文预算（M2：预算花在解密之前）

	defaultClipboardLimit = 50
	maxClipboardLimit     = 200 // 契约 §8 条数上限（低于 memo 的 300：单条正文可比便签长得多）

	// maxClipboardItemText 单条正文出机上限（契约 §8：>64KiB 截断并置 truncated）。
	maxClipboardItemText = 64 << 10
	// maxClipboardPageBytes 单页出机总预算（§12 v1.2 批准、v1.4 M3 改账）。
	// 记账口径是**脱敏并序列化后的实际字节**、覆盖条目全部字段（正文/摘要/来源/
	// 文件路径/元数据）：RedactPII 的赋值替换（$1="******"）与 JSON 转义（正文里的
	// 引号、换行）都会净膨胀，按脱敏前正文长度记账会系统性低估、整页冲破 1MB 载荷
	// 上限后被 listResult 对半砍成空壳——那正是本预算要消灭的形态。
	maxClipboardPageBytes = 512 << 10
	// maxClipboardItemFiles 单条文件路径出机上限（同样为页预算让路）。
	maxClipboardItemFiles = 200
)

// 条目类型字面量与契约 §3 的 clipboard.Kind 取值逐字对齐。不 import 该包的代价
// 就是这三行手抄，改契约必须同步此处；读侧对未知 kind 降级为"仅元数据"，
// 未来新增 kind 不会炸现网。
const (
	clipKindText  = "text"
	clipKindImage = "image"
	clipKindFile  = "file"
)

// clipboardIndexFile 是 <DataDir>/clipboard/index.json 的落盘外层（契约 §2）。
// Entries 取 RawMessage 是"坏条目跳过不连坐"的前提：整库一次 Unmarshal 时，
// 单条字段类型写坏会连坐全库读不出，逐条延迟解码才谈得上局部容错。
type clipboardIndexFile struct {
	Version int               `json:"version"`
	Entries []json.RawMessage `json:"entries"`
}

// clipboardDiskEntry 是剪贴板条目的**磁盘投影**——与 internal/modules/clipboard
// 的 Entry（契约 §3）逐字段对齐，JSON tag 即对齐面；刻意不 import 该包（A2 并行
// 开发中，两路互不绊住），改契约必须同步本表（memoFileNameRe 同款钉法）。
// 落盘态的 Text 是 base64(DPAPI(UTF-8))，经 reader 解密后才是明文。
//
// 同步状态：2026-09-26 已与 clipboard/models.go 的 Entry 逐 tag 对拍一致（含
// Kind 取值为 text/image/file 三点）。收口阶段若把本投影换成直接 import clipboard.Entry，
// 本类型与上方 kind 常量一并删除即可，reader/工具层逻辑不依赖包身份。
type clipboardDiskEntry struct {
	ID         string   `json:"id"`
	Hash       string   `json:"hash"`
	Kind       string   `json:"kind"`
	Text       string   `json:"text,omitempty"`
	Preview    string   `json:"preview"`
	Files      []string `json:"files,omitempty"`
	Blob       string   `json:"blob,omitempty"`
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	ByteSize   int64    `json:"byteSize"`
	SourceApp  string   `json:"sourceApp,omitempty"`
	SourceExe  string   `json:"sourceExe,omitempty"`
	AutoTags   []string `json:"autoTags,omitempty"`
	Sensitive  bool     `json:"sensitive,omitempty"`
	Pinned     bool     `json:"pinned,omitempty"`
	Manual     bool     `json:"manual,omitempty"`
	CreatedAt  int64    `json:"createdAt"`
	LastUsedAt int64    `json:"lastUsedAt,omitempty"`
	UseCount   int      `json:"useCount,omitempty"`
	// BlobData 刻意不投影：契约 §3 注明它仅由 GUI 侧 Get 回填，index.json 里不存在。

	// TextOverBudget 不是落盘字段（reader 装载期置位）：本条正文因累计明文预算耗尽
	// 而**未经解密**，Text 已被清空（绝不让密文进下游）。工具层据此如实置
	// textOmitted，并知晓该条正文不在关键词匹配面内。
	TextOverBudget bool `json:"-"`
}

// textDecryptor 是正文解密面：base64(DPAPI) → 明文字节。
type textDecryptor func(cipherBase64 string) ([]byte, error)

// ClipboardSource 是 hanxi_clipboard_search 的后端取数面（真 = clipboardDiskReader；
// 单测注入假件）。返回条目已完成"逐条解码 + 敏感剔除 + text 类明文解密"；
// 过滤、排序、条数钳制与出机脱敏全在工具层。
type ClipboardSource interface {
	Load() ([]clipboardDiskEntry, error)
}

// clipboardDiskReader 零落盘直读通道（包注释决策 3 的剪贴板落点）。
type clipboardDiskReader struct {
	path string
	// decrypt 抽成字段而非直调 windows.DPAPIDecrypt，为的是让夹具能落"可解的假密文"：
	// DPAPI 与本机用户账户绑定，真密文既进不了版本库、异机 CI 也解不开——
	// "真件走磁盘、单测注假件"的仓库既有谱系。真装配见 newClipboardDiskReader。
	decrypt textDecryptor

	// 两级预算可在构造时收紧（0=默认值，见 indexMaxBytes/loadPlainMax）：抽成字段
	// 不是为了线上可调，而是让单测能用几 KB 的档位真实演练 M2 的预筛与耗尽路径，
	// 不必造 32MiB 夹具（同 portscan 的"扫一点即停"取向：演练真分支）。
	indexMaxBytes int64
	loadPlainMax  int
}

func (r *clipboardDiskReader) indexMaxBytesOrDefault() int64 {
	if r.indexMaxBytes > 0 {
		return r.indexMaxBytes
	}
	return clipboardIndexMaxBytes
}

func (r *clipboardDiskReader) loadPlainMaxOrDefault() int {
	if r.loadPlainMax > 0 {
		return r.loadPlainMax
	}
	return clipboardLoadPlainMax
}

// clipboardIndexPath 剪贴板整库落位（契约 §2：<DataDir>/clipboard/index.json）。
// 抽成纯函数是为让单测能验路径布局而不必触发 settings.GetPaths()——后者会
// ensureDirs 建目录，与"无头测试零落盘"的既有纪律冲突（包内无任何测试构造过真件）。
func clipboardIndexPath(dataDir string) string {
	return filepath.Join(dataDir, clipboardDirName, clipboardIndexName)
}

func newClipboardDiskReader() *clipboardDiskReader {
	return &clipboardDiskReader{
		path:    clipboardIndexPath(settings.GetPaths().DataDir()),
		decrypt: windows.DPAPIDecrypt,
	}
}

// readDoc 读档并解析整份 index.json。**刻意自持读取步而不用 jsonstore.Load**，两条
// 理由都在本文件职责内：
//   - M2 装载预算：index.json 是"500 条 × 单条可达数 MiB"的容器，必须先 stat 看尺寸
//     再吞档；jsonstore.Load 无尺寸门，而它是跨模块共享包（本线禁改，一行没动）。
//   - L1 错误串：jsonstore 的 ErrEmpty/ErrCorrupt 文案自带绝对路径，os 的 *PathError
//     同样自带；本工具错误串会进云端模型上下文=机主目录画像，故统一过 errText 降格。
//
// 返回 (nil, nil) 表示库不存在（合法真空态，绝不创建任何东西）。
func (r *clipboardDiskReader) readDoc() (*clipboardIndexFile, error) {
	f, err := os.Open(r.path) // Windows 共享读打开，不抢 GUI 写者锁；只读不建
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, r.errText(err)
	}
	defer f.Close()

	limit := r.indexMaxBytesOrDefault()
	fi, serr := f.Stat()
	if serr != nil {
		return nil, r.errText(serr)
	}
	if fi.IsDir() {
		return nil, r.errText(fmt.Errorf("%s 是目录而非剪贴板库文件（请核对 hanxi 数据目录布局）", r.fileName()))
	}
	if fi.Size() > limit {
		// 拒载而非吞档：真遇到这种库说明写入侧尺寸闸失守或库被手工灌过，
		// 无头侧把它读进内存只是把事故扩大成 OOM（GUI 侧清理/重建才是出路）。
		return nil, r.errText(fmt.Errorf("%s 体积 %d 字节超过无头读取上限 %d 字节，拒绝装载（请在 hanxi 主程序中清理或重建剪贴板历史）",
			r.fileName(), fi.Size(), limit))
	}
	data, rerr := io.ReadAll(io.LimitReader(f, limit+1))
	if rerr != nil {
		return nil, r.errText(rerr)
	}
	if len(data) == 0 {
		return nil, r.errText(fmt.Errorf("%s 为 0 字节（写入半途被打断的形态），请在 hanxi 主程序中修复/重建", r.fileName()))
	}
	var doc clipboardIndexFile
	if uerr := json.Unmarshal(data, &doc); uerr != nil {
		return nil, r.errText(fmt.Errorf("%s 解析失败（请在 hanxi 主程序中修复/重建该库）: %w", r.fileName(), uerr))
	}
	return &doc, nil
}

// fileName 只取路径末段（错误串与日志用的最小身份标识）。
func (r *clipboardDiskReader) fileName() string {
	if r.path == "" {
		return clipboardIndexName
	}
	return filepath.Base(r.path)
}

// errText 把错误链里的绝对路径降格为文件名（L1）：os 的 *PathError 自带调用时传入
// 的完整路径，而这条串会经工具错误直达云端模型上下文——留文件名足够定位，带盘符的
// 用户目录只是机主画像。整体替换而非拆分，兼容错误串中间嵌路径的形态。
func (r *clipboardDiskReader) errText(err error) error {
	msg := err.Error()
	if r.path != "" && strings.Contains(msg, r.path) {
		msg = strings.ReplaceAll(msg, r.path, r.fileName())
	}
	// 兜底：路径末段之外的目录前缀（如某些错误只回目录）一并抹掉。
	if dir := filepath.Dir(r.path); dir != "" && dir != "." && strings.Contains(msg, dir) {
		msg = strings.ReplaceAll(msg, dir, "<数据目录>")
	}
	return errors.New(msg)
}

// clipSkipTally 坏条目告警聚合（§12 v1.4 L3）：一次装载最多出一条 warn——
// 各归因计数 + 首条样例。逐条 slog.Warn 的旧写法在"畸形库 × 高频调用"下会把
// 运行日志刷成洪水（500 条坏档 × 每次搜索 = 每次 500 行），聚合后事故规模仍
// 可复盘（skipped 总数 + 按原因分账 + 一样例），日志量与库大小脱钩。
type clipSkipTally struct {
	counts map[string]int
	sample string
	total  int
}

func (t *clipSkipTally) add(reason string, index int, id string, err error) {
	if t.counts == nil {
		t.counts = map[string]int{}
	}
	t.counts[reason]++
	t.total++
	if t.sample == "" {
		s := fmt.Sprintf("reason=%s index=%d", reason, index)
		if id != "" {
			s += " id=" + id
		}
		if err != nil {
			s += " err=" + err.Error()
		}
		t.sample = s
	}
}

// flush 落唯一一条聚合告警（无跳过则零输出）。原因名按字典序展开，保证同一库
// 两次装载的日志字节一致（map 无序漂移回归锚，同 memo 统计工具的确定性口径）。
func (t *clipSkipTally) flush(file string) {
	if t.total == 0 {
		return
	}
	reasons := make([]string, 0, len(t.counts))
	for reason := range t.counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	attrs := make([]any, 0, 4+2*len(reasons))
	attrs = append(attrs, "file", file, "skipped", t.total, "sample", t.sample)
	for _, reason := range reasons {
		attrs = append(attrs, reason, t.counts[reason])
	}
	slog.Warn("mcp clipboard: entries skipped while loading", attrs...)
}

// Load 装载并解密可用条目。失败语义分三档，各自如实：
//   - 文件不存在（含目录不存在）：真空库，返回空集且不创建任何东西；
//   - 整档过大 / 0 字节 / 解析失败 / version 非 1：fail-loud 指引，修复是 GUI 通道的
//     职责（无头不修不猜、不隔离改名）；
//   - 单条解码失败、无 id、正文解密失败（多为别的机器/账户的库）：跳过并计入聚合
//     告警，不连坐。
//
// 解密是**预算制**的（M2）：先按密文长度预筛（base64 明文≈密文 3/4），再按累计明文
// 预算决定是否真的解密；超预算条目原文清空（密文不留到下游）并置 TextOverBudget，
// 由工具层如实降级为"仅元数据 + textOmitted"。因此装载峰值内存与库的实际明文规模
// 解耦——库可能有历史超限条（A2 的入库尺寸闸是本轮新增，旧库不受它保护）。
//
// version 不匹配时的口径与 GUI 侧 Store.load（"警告后按现有字段尽量装载"）**刻意
// 分歧**（§12.2 v1.2 第 2 条已裁：无头面 fail-loud）：GUI 必须宽容是因为它装载完就
// 整库回写，拒读等于拿用户历史冒险；无头侧只读不写，读到契约外布局的最坏后果是把
// 已改形的字段当正确文本喂进云端模型上下文，报一句"请升级 hanxi"代价小得多。
func (r *clipboardDiskReader) Load() ([]clipboardDiskEntry, error) {
	doc, err := r.readDoc()
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return []clipboardDiskEntry{}, nil // 真空库
	}
	if doc.Version != clipboardIndexVersion {
		return nil, fmt.Errorf("剪贴板库 version=%d 不受支持（本工具契约 v%d）：请升级 hanxi 后重试",
			doc.Version, clipboardIndexVersion)
	}
	var tally clipSkipTally
	out := make([]clipboardDiskEntry, 0, len(doc.Entries))
	plainRoom := r.loadPlainMaxOrDefault()
	for i, raw := range doc.Entries {
		var e clipboardDiskEntry
		if uerr := json.Unmarshal(raw, &e); uerr != nil {
			tally.add("decode", i, "", uerr)
			continue
		}
		if strings.TrimSpace(e.ID) == "" {
			tally.add("missing-id", i, "", nil)
			continue
		}
		if e.Sensitive {
			continue // 双层剔除之一：密文不出 reader（工具层再挡一次）。不进 tally 计数
		}
		if e.Kind == clipKindText && e.Text != "" {
			if r.decrypt == nil {
				// 构造缺件（未注入解密面）是装配错误：整条剔除，不下发空正文——
				// 免得误接线把"库里有内容"的假象喂给模型。
				tally.add("no-decryptor", i, e.ID, nil)
				continue
			}
			// 密文预筛（先记账后解密）：估算明文超剩余额度即整条不解，顺带把密文
			// 从结构体里摘掉——超大条目不占住内存往下走，元数据照常保留。
			if len(e.Text)/4*3 > plainRoom {
				tally.add("over-budget", i, e.ID, nil)
				e.Text = ""
				e.TextOverBudget = true
				out = append(out, e)
				continue
			}
			plain, derr := r.decrypt(e.Text)
			if derr != nil {
				// DPAPI 与用户账户绑定：异机拷来的库解不开。如实跳过，不修不猜。
				tally.add("decrypt", i, e.ID, derr)
				continue
			}
			e.Text = string(plain)
			plainRoom -= len(plain)
			if plainRoom < 0 {
				plainRoom = 0
			}
		}
		out = append(out, e)
	}
	tally.flush(r.fileName())
	return out, nil
}

// ---------- 接线（不改 server.go 的即插通道，portkill hook 同谱） ----------

var (
	clipboardHookMu sync.Mutex
	clipboardHook   ClipboardSource
)

// SetClipboardSource 注入剪贴板取数面（Run() 启动时调用一次；传 nil 复位为未装配态）。
// 未装配时工具一律返回指引错误而非 panic（fail-closed）。
//
// 走包级 hook 而非 Deps.Clipboard 字段是领地纪律使然（本批禁碰 server.go）；
// 收口阶段换成 Deps 字段形态同样成立（builder 改读 deps 即可，两形态等价）。
func SetClipboardSource(src ClipboardSource) {
	clipboardHookMu.Lock()
	defer clipboardHookMu.Unlock()
	clipboardHook = src
}

// clipboardSourceOf 读取当前接线快照（每次调用重读，不养缓存）。
func clipboardSourceOf() ClipboardSource {
	clipboardHookMu.Lock()
	defer clipboardHookMu.Unlock()
	return clipboardHook
}

// registerClipboardTools 把剪贴板只读检索工具挂进给定 server（契约 §8 冻结名，
// 收口阶段 server.go 一行接线：`registerClipboardTools(s, deps)`）。
//
// 两种接法二选一，**不可并用**（同名 AddTool 后者覆盖前者，白挂一次）：
//
//	① 表内接法（推荐）：toolDefs 追加
//	   {Name: toolClipboard, ModuleID: clipboardAccessKey, Build: buildClipboardSearchTool}
//	   ——本工具面既有的授权/启用门（gateMiddleware 的 byName 名称表取自 toolDefs）
//	   自动覆盖，无需另补。
//	② 直挂接法：调本函数，但必须同时给 gateMiddleware 的名称表补项，否则本工具
//	   会被"未知工具"一律拦死（拦死而非放行，不构成旁路，但功能为零）。
//
// 无论哪种，access.json 键位三处必须同批扩表（server.go 的 knownModuleIDs /
// mcpwizard 的 accessToolKeys / guards_test.go 的名称白名单）——漏一处，含
// clipboard 键的整档授权文件会被 fail-closed 拒读并连坐封死全部工具。
//
// 启用门禁侧还有一条硬约束：clipboard 模块**不得**进无头 registry（mcpModules），
// 必须照 memo 在 registryGate 里开特例直读 config.json 的 enabled 位 + receipt——
// clipboard.NewStore 构造即建目录、缺库回写空 index.json，进表经 Acquire 懒激活
// 会直接毁掉包注释决策 3 的无头零落盘承诺（memo 因同款问题当年就没进表）。
// 后端接线同理：Run() 里只需 SetClipboardSource(newClipboardDiskReader())。
func registerClipboardTools(s *server.MCPServer, deps Deps) {
	tool, handler := buildClipboardSearchTool(deps)
	s.AddTool(tool, handler)
}

// buildClipboardSearchTool 剪贴板只读检索。与 buildMemoTool 对位的 toolDefs.Build
// 形态（收口接法①直接用它，接法②经本文件 registerClipboardTools 间接调它）。
//
// 检索口径（写进 description 即对外契约）：keyword 小写子串匹配
// Preview/正文明文/SourceApp/SourceExe/Files/AutoTags，非正则——与 GUI 侧 List 的
// q 同谱但**多匹配正文明文**（GUI 的 List 不解密，MCP 面"找回我复制过什么"必须
// 能命中正文，这是两个界面各自的职责，不是同一函数的两种实现）。
// 排序按 createdAt 倒序（同毫秒按 id 升序定序，保证同库两次调用字节一致）。
//
// 出机预算耗尽（M3）时**停止追加并在信封如实置 truncated**：宁可少给几条，
// 也不产出"整页空壳只剩 truncated"那种形态——单条最坏退到纯身份行（约 90 字节），
// 512KiB 额度足以装下 limit=200 的全部身份，因此页满只会由正文体积造成，
// 而条目身份始终在场（模型据 id/时间还能再窄查）。绝不等 listResult 的
// 1MiB 对半砍来兜底。
func buildClipboardSearchTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolClipboard,
		mcp.WithDescription("按关键词/标签/类型检索 hanxi 内置剪贴板历史（只读，不改动任何条目，"+
			"也不回填系统剪贴板）。安全约定：入库判定为敏感（sensitive，正文命中密钥启发式）的条目"+
			"整条不下发、不计入命中数（命中数因此可能少于库内实况）；图片条目只下发元数据"+
			"（宽高/字节数/来源/时间，图片内容不经本通道）；所有文本（摘要/正文/来源/文件路径）"+
			"都经过行级脱敏（IPv4→[ipv4]、邮箱→[email]、token=/password= 赋值与 sk-/ghp_ 类密钥→掩码）。"+
			"单条正文 >64KiB 截断并置 truncated；整页出机预算（按脱敏后的实际字节计）耗尽后，"+
			"条目降级为只给元数据并置 textOmitted，连元数据都放不下时只给身份行并置 identityOnly，"+
			"页满则停止追加并在信封如实置 truncated（不产出无内容的空壳条目）。"+
			"文本经启发式打码可能失真，不保证逐字还原。"+
			"keyword 留空时按创建时间倒序返回最近条目；正文解密失败的条目（多为其它机器或账户的库）"+
			"跳过并告警，因装载预算而未解密的正文不参与关键词匹配（命中数可能偏少）。"+
			"库不存在（剪贴板模块未启用过）时返回空结果。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("keyword", mcp.Description("关键词（小写子串匹配摘要/正文明文/来源应用/来源进程/文件路径/自动标签，忽略大小写；留空=列最近。注：因装载预算未经解密的正文不在匹配面内）")),
		mcp.WithString("tag", mcp.Description("自动标签精确过滤（url/email/phone/color/code/cjk 之一，# 前缀可选，忽略大小写）")),
		mcp.WithString("kind", mcp.Description("条目类型过滤：text / image / file（files 同义）/ all，留空或 all=不过滤")),
		mcp.WithNumber("limit", mcp.Description("返回条数上限（1-200，默认 50；<1 回落默认值，>200 钳到上限）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		src := clipboardSourceOf()
		if src == nil {
			return mcp.NewToolResultError("clipboard 后端未装配：本 hanxi 无头版本未启用剪贴板检索通道"), nil
		}
		kind, kerr := normalizeClipboardKind(req.GetString("kind", ""))
		if kerr != nil {
			// 静默忽略非法参数比报错更糟（模型会误信"已按类型过滤"），一律 fail-loud。
			return mcp.NewToolResultError(kerr.Error()), nil
		}
		entries, err := src.Load()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("读取剪贴板库失败: %v", err)), nil
		}
		kw := strings.ToLower(strings.TrimSpace(req.GetString("keyword", "")))
		tag := strings.TrimSpace(req.GetString("tag", ""))
		limit := clipboardLimitClamp(req.GetInt("limit", defaultClipboardLimit))

		var matched []clipboardDiskEntry
		for _, e := range entries {
			if e.Sensitive {
				continue // 双层剔除之二：工具层不信任任何实现的 Load（假件亦然）
			}
			if kind != "" && e.Kind != kind {
				continue
			}
			if tag != "" && !clipboardTagMatched(e.AutoTags, tag) {
				continue
			}
			if kw != "" && !clipboardKeywordHit(e, kw) {
				continue
			}
			matched = append(matched, e)
		}
		sort.SliceStable(matched, func(i, j int) bool {
			if matched[i].CreatedAt != matched[j].CreatedAt {
				return matched[i].CreatedAt > matched[j].CreatedAt
			}
			return matched[i].ID < matched[j].ID
		})
		capped := len(matched) > limit
		if capped {
			matched = matched[:limit]
		}
		// 出机预算（M3）：逐条按**序列化后实际字节**扣额，放不下正文退元数据、
		// 放不下元数据退身份行，连身份行都放不下才判定"这页到此为止"（信封置
		// truncated）——条目身份始终在场，绝不等 listResult 的 1MiB 对半砍兜底。
		itemsOut := make([]any, 0, len(matched))
		room := maxClipboardPageBytes
		budgetCut := false
		for _, e := range matched {
			item, size, fits := clipboardBuildItem(e, room)
			if !fits {
				budgetCut = true
				break
			}
			itemsOut = append(itemsOut, item)
			room -= size
			if room < 0 {
				room = 0
			}
		}
		return listResult(itemsOut, capped || budgetCut)
	}
	return tool, handler
}

// clipboardLimitClamp 钳制条数：越界不报错而是收敛（<1 回落默认 50，>200 钳上限）。
// 与 memo 的"越界一律钳到上限"略异——limit=0 要 200 条是反直觉的放大，宁给保守默认。
func clipboardLimitClamp(limit int) int {
	if limit < 1 {
		return defaultClipboardLimit
	}
	if limit > maxClipboardLimit {
		return maxClipboardLimit
	}
	return limit
}

// normalizeClipboardKind kind 入参归一（大小写/空白容错，files 为 file 的同义写法）。
// 返回空串表示"不过滤"；集合外取值 fail-loud 报指引。
func normalizeClipboardKind(raw string) (string, error) {
	switch k := strings.ToLower(strings.TrimSpace(raw)); k {
	case "", "all", "*":
		return "", nil
	case clipKindText, clipKindImage:
		return k, nil
	case clipKindFile, "files":
		return clipKindFile, nil
	default:
		return "", fmt.Errorf("kind 参数 %q 非法：仅支持 text / image / file / all（留空=不过滤）", raw)
	}
}

// clipboardKeywordHit 小写子串匹配面（与 GUI List 的 q 同谱，另多覆盖正文明文与
// 来源进程名，见 buildClipboardSearchTool 注记）。正文仅 text 类参与（其它 kind
// 的 Text 不经本通道下发，拿它匹配等于"命中了就搜不到"的假阳性）。
func clipboardKeywordHit(e clipboardDiskEntry, kwLower string) bool {
	if strings.Contains(strings.ToLower(e.Preview), kwLower) ||
		strings.Contains(strings.ToLower(e.SourceApp), kwLower) ||
		strings.Contains(strings.ToLower(e.SourceExe), kwLower) {
		return true
	}
	if e.Kind == clipKindText && strings.Contains(strings.ToLower(e.Text), kwLower) {
		return true
	}
	for _, p := range e.Files {
		if strings.Contains(strings.ToLower(p), kwLower) {
			return true
		}
	}
	for _, t := range e.AutoTags {
		if strings.Contains(strings.ToLower(t), kwLower) {
			return true
		}
	}
	return false
}

// clipboardTagMatched 自动标签精确匹配（容忍 # 前缀差异与大小写，对齐 memo 标签口径）。
func clipboardTagMatched(tags []string, want string) bool {
	trimWant := strings.TrimPrefix(want, "#")
	for _, t := range tags {
		if strings.EqualFold(t, want) ||
			strings.EqualFold(strings.TrimPrefix(t, "#"), trimWant) {
			return true
		}
	}
	return false
}

// floorMinTextAttach 正文起步价：剩余额度连这么点正文都装不下，就不如整块不给
// 并置 textOmitted——几十上百字节的残段既读不出上下文，还占一次序列化。
const floorMinTextAttach = 512

// clipboardBuildItem 按剩余出机额度装配单条载荷，返回（载荷, 占用字节, 是否可下发）。
//
// 记账一律用**脱敏并序列化后的实际字节**（M3）：RedactPII 的赋值替换带 `$1="******"`
// 净膨胀，正文里的引号/换行经 JSON 转义还会再膨胀，按脱敏前长度估账会系统性低估，
// 整页冲破 1MiB 载荷上限后就被 listResult 对半砍成空壳——那正是本函数要消灭的形态。
// 降级阶梯：正文 → 只元数据（置 textOmitted）→ 纯身份行（置 identityOnly）→
// 连身份行都放不下才回 false（调用方据此停页并在信封如实置 truncated）。
func clipboardBuildItem(e clipboardDiskEntry, room int) (resultPayload, int, bool) {
	item := clipboardMetaPayload(e)
	if size, ok := clipboardItemSize(item); ok && size <= room {
		if e.Kind == clipKindText {
			attachClipboardText(item, e, room)
		}
		if size, ok = clipboardItemSize(item); ok && size <= room {
			return item, size, true
		}
	}
	floor := clipboardIdentityPayload(e)
	if size, ok := clipboardItemSize(floor); ok && size <= room {
		return floor, size, true
	}
	return nil, 0, false
}

// clipboardMetaPayload 不含正文的条目载荷（契约 §8 字段集 + 防御性标志）。
func clipboardMetaPayload(e clipboardDiskEntry) resultPayload {
	item := resultPayload{
		"id":        e.ID,
		"kind":      e.Kind,
		"preview":   logging.RedactPII(e.Preview),
		"pinned":    e.Pinned,
		"createdAt": clipboardTimeOf(e.CreatedAt),
	}
	if e.SourceApp != "" {
		item["sourceApp"] = logging.RedactPII(e.SourceApp)
	}
	if len(e.AutoTags) > 0 {
		item["autoTags"] = e.AutoTags // 封闭词表（url/email/phone/color/code/cjk），非机主文本，不再脱敏
	}
	if e.Kind == clipKindImage {
		// 图片只给元数据：blob 相对路径刻意不下发（它是指向机主磁盘的文件名，
		// 而图片字节本身不经本通道，给路径既无用又白增泄露面）。
		item["width"] = e.Width
		item["height"] = e.Height
		item["byteSize"] = e.ByteSize
	}
	if len(e.Files) > 0 {
		files := e.Files
		if len(files) > maxClipboardItemFiles {
			files = files[:maxClipboardItemFiles]
			item["filesTruncated"] = true
		}
		out := make([]string, 0, len(files))
		for _, p := range files {
			out = append(out, logging.RedactPII(p))
		}
		item["files"] = out
	}
	return item
}

// clipboardIdentityPayload 保底身份行（M3 末档）：三件最小身份 + 标志。
// 存在意义是"页预算耗尽时条目不至于人间蒸发"——模型仍能看到这条历史的存在、
// 时间与其 id，可据此改小 limit 或窄查再取。
func clipboardIdentityPayload(e clipboardDiskEntry) resultPayload {
	return resultPayload{
		"id":           e.ID,
		"kind":         e.Kind,
		"createdAt":    clipboardTimeOf(e.CreatedAt),
		"identityOnly": true,
	}
}

// attachClipboardText 在整页剩余额度内尽量给正文：先按单条 64KiB 上限与剩余额度取小，
// 每次挂完都重新序列化计量（转义/替换的净膨胀不可预测），放不下就对半砍，
// 砍到起步价以下仍放不下则整块撤回并置 textOmitted。截断事实一律如实反映在
// truncated/textOmitted 上，包括"装载预算内未解密"（TextOverBudget）那种形态。
func attachClipboardText(item resultPayload, e clipboardDiskEntry, room int) {
	if e.TextOverBudget {
		item["textOmitted"] = true // 正文根本没解密（密文已被 reader 摘除），不是"太长被裁"
		return
	}
	base, ok := clipboardItemSize(item)
	if !ok {
		return
	}
	limit := len(e.Text)
	if limit > maxClipboardItemText {
		limit = maxClipboardItemText
	}
	if room-base > 0 && limit > room-base {
		limit = room - base // 起步估算（键名与转义开销由下面的实测循环收口）
	}
	if limit <= 0 {
		item["textOmitted"] = true
		return
	}
	for {
		text, _ := truncateUTF8(e.Text, limit)
		item["text"] = logging.RedactPII(text)
		if size, ok := clipboardItemSize(item); ok && size <= room {
			if len(text) < len(e.Text) {
				item["truncated"] = true // 单条上限、额度收敛或 rune 边界回退，统称未给全
			}
			return
		}
		// 起步价只拦"残段"：整条正文本来就短于起步价（一条命令、一个 URL）也必须
		// 能发出去，否则阈值本身就成了功能杀手——只在还得继续砍时判退。
		if limit < floorMinTextAttach {
			break
		}
		limit /= 2
	}
	delete(item, "text")
	item["textOmitted"] = true
}

// clipboardItemSize 出机实际字节（紧凑 JSON，与 listResult 同一编码器口径）。
func clipboardItemSize(item resultPayload) (int, bool) {
	data, err := json.Marshal(item)
	if err != nil {
		return 0, false
	}
	return len(data), true
}

// clipboardTimeOf 落盘 ms 时间戳 → RFC3339（出机口径与 memo 一致：模型读不了 epoch；
// 0 值给空串而非 1970，区分"没有时间信息"与"epoch 起点"）。
func clipboardTimeOf(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}
