package ocr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ServiceState 前端唯一状态视图模型：引擎快照 + /api/status 探测缓存合并。
// 事件 ocr:service-state 与 GetStatus 共用同一结构。
type ServiceState struct {
	State         string `json:"state"`    // stopped / starting / running / external / failed
	Online        bool   `json:"online"`   // 最近一次契约探测成功
	Managed       bool   `json:"managed"`  // 我方 JobObject 托管实例（running）
	External      bool   `json:"external"` // 外部实例（只转发不管理）
	PID           uint32 `json:"pid"`
	ListenAddr    string `json:"listenAddr"`  // 127.0.0.1:<port>
	ExePath       string `json:"exePath"`     // 生效的服务程序路径（发现结果或设定值）
	ExeAuto       bool   `json:"exeAuto"`     // 生效路径是否来自自动发现（false=用户显式指定）
	Version       string `json:"version"`     // 上游 status.version
	Engine        string `json:"engine"`      // 上游 status.engine（如 wxocr@8094 / pp-ocrv6-small）
	EngineMode    string `json:"engineMode"`  // 上游 status.engine_mode（组件如实上报，本侧不解释语义）
	EngineID      string `json:"engineID"`    // store 登记的活跃引擎（wechat / paddle，未在线时也可判定）
	EngineError   string `json:"engineError"` // 上游 status.error（引擎已就位但带病的中文原因，空=健康）
	EngineRunning bool   `json:"engineRunning"`
	Hung          bool   `json:"hung"`      // 上游报告引擎 Call 挂死
	Error         string `json:"error"`     // 中文人话错误（failed 时非空）
	CheckedAt     string `json:"checkedAt"` // 最近探测时刻（格式化，状态新鲜度）
}

// OcrLine 单行识别结果与左上角坐标（4.0 引擎不提供宽高/置信度）。
type OcrLine struct {
	Text string `json:"text"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

// OcrOutcome 识别操作结果（业务失败折进 Error 中文，error 通道留给程序性错误）。
type OcrOutcome struct {
	Ok        bool      `json:"ok"`
	Text      string    `json:"text"`
	Lines     []OcrLine `json:"lines"`
	ElapsedMs int64     `json:"elapsedMs"`
	Error     string    `json:"error"`
}

// ImageRef 统一"已选图"模型：对话框/拖拽/粘贴三通道汇流。
type ImageRef struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	PreviewURL string `json:"previewUrl"` // dataURL；超限为空
	Temporary  bool   `json:"temporary"`  // 临时件（RuntimeDir/ocr），生命周期归本模块
}

// ControlOutcome 启停操作结果（仿 KillResult/QuitOutcome 口径）。
type ControlOutcome struct {
	Action   string `json:"action"` // started / already-running / starting / external / stopped / already-stopped / external-unmanaged
	External bool   `json:"external"`
	Message  string `json:"message"`
}

// SnipResult 框选截屏识别结果（悬浮卡事件 ocr:snip-result 与 SnipAndRecognize
// 返回值共用）。Cancelled=true 表示用户放弃选区（静默路径，前端不打扰）。
type SnipResult struct {
	Ok        bool   `json:"ok"`
	Text      string `json:"text"`
	LineCount int    `json:"lineCount"`
	ElapsedMs int64  `json:"elapsedMs"`
	Error     string `json:"error"`
	Copied    bool   `json:"copied"`    // 已按开关自动复制进剪贴板
	Cancelled bool   `json:"cancelled"` // 选区超时取消
}

// DropResult 原生文件拖放/组件导入的统一回执（事件 ocr:file-drop-result 与各
// 导入方法共用）。按 Kind 分流：import 结果刷状态，image 结果直接设为待识别图。
type DropResult struct {
	Kind    string    `json:"kind"` // import / image
	Ok      bool      `json:"ok"`
	ExePath string    `json:"exePath"` // 导入成功后的托管副本路径
	Image   *ImageRef `json:"image"`   // 图片通道成功时的选图结果
	Message string    `json:"message"` // 中文人话（成功说明或失败原因）
}

// HostedVersion 托管版本清单一件（F7：引擎 zip 安装进 versions/hanxi-ocr 的落位件）。
// State：ready=入口可执行 / broken=目录在位但入口缺失损坏（列表照列，供卸载清理）。
// Effective 由服务层填充：当前活跃引擎的服务解析结果正落在该版本目录内。
type HostedVersion struct {
	Engine      string `json:"engine"`      // wechat / paddle
	Version     string `json:"version"`     // manifest 版本号（目录名 = engine-version）
	Dir         string `json:"dir"`         // 版本目录绝对路径
	ExePath     string `json:"exePath"`     // 入口 hanxi-ocr.exe 路径
	Size        int64  `json:"size"`        // 入口 exe 字节数
	InstalledAt string `json:"installedAt"` // 安装时间（meta.json / 目录 mtime）
	Note        string `json:"note"`        // manifest 中文说明（可空）
	State       string `json:"state"`       // ready / broken
	Effective   bool   `json:"effective"`   // 生效版本（活跃引擎解析命中）
	Error       string `json:"error"`       // broken 态中文原因
}

// EngineInfo 引擎注册表单件（GetEngines 列表元素，前端"引擎列表"两行卡按此渲染）。
// Installed 语义：登记件或默认锚点可解析出可用 exe；未安装时 Path 为空、
// Error 给中文原因（未找到指引 / 登记路径失效）。
type EngineInfo struct {
	ID        string `json:"id"`        // wechat / paddle
	Label     string `json:"label"`     // 中文显示名（微信引擎 / PP-OCR 开源引擎）
	Installed bool   `json:"installed"` // 组件可解析（登记或自动发现）
	Active    bool   `json:"active"`    // 当前活跃引擎（单活语义，至多一行为真）
	Version   string `json:"version"`   // 登记版本；活跃引擎在线时为 /api/status 实测版本
	Path      string `json:"path"`      // 解析出的 exe 路径（未安装为空）
	Auto      bool   `json:"auto"`      // 路径来自自动发现（false = 用户设定/导入登记）
	Error     string `json:"error"`     // 不可解析的中文原因（Installed 为真相空）
}

// 上游组件文件名（自动发现、导入校验与布局判定共用同一字面量，改名字一处收口）。
// 注：instance 子包为探测进程还存在自己的一份私有常量——跨包共享需导出并引入依赖，
// 两处各自就近定义，改名字时务必同改。
const (
	serviceExeName = "hanxi-ocr.exe"
	// serviceExeDirName 私发组件默认落位目录名（Hanxi 主程序同级）。
	serviceExeDirName = "hanxi-ocr"
	// engineDLLName v0.2 目录版启动器必须同级携带的引擎文件；v0.3 起单文件版不再需要。
	engineDLLName = "wcocr.dll"
	// serviceContractName /api/status 的 name 字段固定值（计划 §3.1）：微信版与
	// PP-OCR 开源版同名——两型号对 Hanxi 是"同一上游的两个型号"。
	serviceContractName = "hanxi-ocr"
	// paddleExeDirName PP-OCR 引擎的 exe 同级落位目录名（Hanxi 主程序同级）。
	paddleExeDirName = "hanxi-ocr-paddle"
	// enginesDirName PP-OCR 引擎主发现锚点目录名（DataDir 下，按 manifest.json 自检，计划 §6）。
	enginesDirName = "ocr-engines"
	// manifestName 开源引擎目录自检清单（组件版本+文件校验，计划 §2 资产清单）。
	manifestName = "manifest.json"
)

// 引擎 ID（ocr.json 注册表键，计划 §5.2）：双引擎并存、单端口单活。
// wechat = 微信版私发组件（现状唯一形态，兼老配置 exePath 的归属）；
// paddle = PP-OCR 开源引擎（目录形态 + manifest 自检，公开版）。
const (
	EngineWechat = "wechat"
	EnginePaddle = "paddle"
)

// engineOrder 已知引擎有序清单：注册表枚举与展示顺序的单一来源，
// 未来新增引擎在此挂一行即可被 store/引擎列表自然覆盖。
var engineOrder = []string{EngineWechat, EnginePaddle}

func isKnownEngine(id string) bool {
	return slices.Contains(engineOrder, id)
}

// contractNames /api/status 的 name 字段合法值集合——探活契约名放宽（计划 §5.1）。
// 当前仍只有 "hanxi-ocr" 一个成员（两版本组件同名），扩展位在此收口。
// 注：instance 子包 IsOCRService 另有一份硬编码判定（零改动区），新增契约名时两处务必同改。
var contractNames = map[string]bool{serviceContractName: true}

func isContractName(name string) bool { return contractNames[name] }

// resolveServiceExe 纯函数：按引擎 ID 解析组件路径（锚点目录注入便于单测，计划 §5.3）。
//
// F7 托管优先（BACKLOG F7：引用式→托管式）：
//  1. 登记件指向托管树（versions/hanxi-ocr/<engine>-<version>/）内 → 托管语义：
//     在位即用；被卸载/损坏时自愈回退到该引擎托管树内最新版本；全树已空则
//     视同自动发现继续走旧锚点（卸载即回到托管前世界，不留悬空报错）；
//  2. 无登记 → 托管树最新版本优先（zip 装好即生效，压过旧同级/ocr-engines 锚点）；
//  3. 托管树之外的设定/登记路径保留旧语义：存在即用、失效一律以 error 明示，
//     不静默回退自动发现（48MB 单文件版与手动指路径兜底行为不动）；
//  4. 旧自动发现锚点殿后兼容——wechat：Hanxi 同级 ../hanxi-ocr/ → PATH；
//     paddle：DataDir/ocr-engines/（含 manifest.json 自检）→ 同级 ../hanxi-ocr-paddle/。
//     paddle 不做 PATH 兜底：开源件与微信件契约同名 hanxi-ocr.exe，PATH 上无法区分型号。
//
// versionsRoot 为空（托管未接线/旧测试注入）时托管两闸自然跳过，行为与改造前一致。
func resolveServiceExe(exeDir, dataDir, versionsRoot, engineID, stored string) (path string, fromStore bool, err error) {
	return resolveServiceExeWithHostedResolver(exeDir, dataDir, versionsRoot, engineID, stored, hostedResolveLatest)
}

func resolveServiceExeWithHostedResolver(
	exeDir, dataDir, versionsRoot, engineID, stored string,
	resolveLatest func(string, string) (string, string, bool),
) (path string, fromStore bool, err error) {
	if !isKnownEngine(engineID) {
		return "", false, fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", engineID)
	}
	if p := strings.TrimSpace(stored); p != "" {
		if hostedDirOfExe(versionsRoot, p) != "" {
			// 登记件在托管树内：生效版本以托管树为准（含被卸载后的自愈回退）
			if isRegularNonEmpty(p) {
				return p, true, nil
			}
			if exe, _, ok := resolveLatest(versionsRoot, engineID); ok {
				return exe, false, nil
			}
			// 引擎整体卸载：落到下方托管树（必空）与旧锚点链
		} else {
			if isRegularNonEmpty(p) {
				return p, true, nil
			}
			if engineID == EnginePaddle {
				return "", true, fmt.Errorf("登记的 PP-OCR 引擎路径已失效：%s，请重新导入引擎目录", p)
			}
			return "", true, fmt.Errorf("设置的服务路径已失效：%s，请在设置中重新指定", p)
		}
	}
	if exe, _, ok := resolveLatest(versionsRoot, engineID); ok {
		return exe, false, nil
	}
	switch engineID {
	case EngineWechat:
		cand := filepath.Join(filepath.Dir(exeDir), serviceExeDirName, serviceExeName)
		if st, e := os.Stat(cand); e == nil && !st.IsDir() {
			return cand, false, nil
		}
		if p, e := exec.LookPath(serviceExeName); e == nil {
			return p, false, nil
		}
		return "", false, fmt.Errorf("未找到 hanxi-ocr.exe，默认预期位置 %s；可将引擎安装包（.zip）拖入文字识别页托管安装，或在设置中手动指定路径", cand)
	case EnginePaddle:
		return resolvePaddleExe(exeDir, dataDir)
	default:
		return "", false, fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", engineID)
	}
}

// resolvePaddleExe PP-OCR 开源引擎旧发现链（托管树与登记件已在主函数前置处理；
// 约定见计划 §3.3/§6：目录按 manifest 自检）。
func resolvePaddleExe(exeDir, dataDir string) (path string, fromStore bool, err error) {
	enginesRoot := filepath.Join(dataDir, enginesDirName)
	if p := findPaddleInDir(enginesRoot); p != "" {
		return p, false, nil
	}
	cand := filepath.Join(filepath.Dir(exeDir), paddleExeDirName, serviceExeName)
	if st, e := os.Stat(cand); e == nil && !st.IsDir() {
		return cand, false, nil
	}
	return "", false, fmt.Errorf(
		"未找到 PP-OCR 引擎组件：预期位于 %s\\<组件目录>（含 %s）或 Hanxi 同级 %s\\；请先在文字识别页导入引擎包",
		enginesRoot, manifestName, paddleExeDirName)
}

// findPaddleInDir 在引擎根目录下扫描"含 manifest.json + hanxi-ocr.exe"的组件目录，
// 按名称升序取最后一个命中（版本目录名通常带递增版本号，取末位即最新版式样）。
func findPaddleInDir(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	found := ""
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(root, ent.Name())
		if !isRegularFile(filepath.Join(dir, serviceExeName)) || !isRegularFile(filepath.Join(dir, manifestName)) {
			continue
		}
		found = filepath.Join(dir, serviceExeName)
	}
	return found
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// engineLabel 引擎中文显示名（引擎列表与切换提示共用；新增引擎在 engineOrder 挂名同改此）。
func engineLabel(id string) string {
	switch id {
	case EnginePaddle:
		return "PP-OCR 开源引擎"
	case EngineWechat:
		return "微信引擎"
	default:
		return id
	}
}
