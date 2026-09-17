package settings

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Mode 数据根来源（F6 起为内部实现细节）。"便携/标准"双模式判定已整体退役——
// 同级即唯一存在方式，不再作为一种模式宣讲；此处只区分数据根来自同级默认
// 还是用户显式绑定，供诊断日志与存储分区文案使用。
type Mode string

const (
	modeSibling Mode = "sibling" // 数据根 = exe 同级 hanxidata/（无则自动创建）
	modeBound   Mode = "bound"   // 数据根 = exe 同级 hanxi.bind 显式绑定的家
)

// F6 数据根策略（BACKLOG F6 四条裁定，定稿见 docs/plans/PLAN_PATHS.md）：
//   - 解析链只有两级：绑定指针 → 应用同级；全程无 %APPDATA% 等用户目录静默兜底；
//   - 绑定指针是 exe 同级小文件 hanxi.bind（首个非空行 = 数据目录绝对路径），
//     存放位置不依赖被绑定目录，换绑只改一行文件内容，"绑定哪个用哪个"；
//   - 旧便携包 data/ 兼容正式废弃（零识别零迁移，仅在携旧数据特征时告警指路）；
//   - %APPDATA%\Hanxi 不再作为家，仅充当一次性搬迁的来源（见 migrate_home.go）。
const (
	siblingDataDirName = "hanxidata"
	bindFileName       = "hanxi.bind"
	legacyDataDirName  = "data"
)

// guideSuffix fail loud 时附给用户的处置指引。两种失败场景都能覆盖：
// exe 目录不可写（绑定指针也写不下）→ 搬家；exe 目录可写但同级建
// hanxidata 失败 / 绑定目标不可用 → 编辑或补建 hanxi.bind 显式绑定。
const guideSuffix = "Hanxi 不会把数据目录静默挪进用户目录。处置方式二选一：" +
	"① 将整个应用文件夹移到可写位置（如 D:\\Tools\\hanxi）后重启；" +
	"② 在 exe 同级新建文件 " + bindFileName + "，首行写数据目录的绝对路径（如 E:\\HanxiData），显式绑定数据之家。"

// Paths 数据目录布局快照，字段在 InitPaths 时一次性解析，之后只读，可并发访问。
type Paths struct {
	mode        Mode
	baseDir     string
	configDir   string
	dataDir     string
	stateDir    string
	logsDir     string
	versionsDir string
	runtimeDir  string
	// initErr 数据根解析失败原因（fail loud 载体）；为 nil 时布局可用。
	// 装配根必须在启动最早处检查并 ExitWithBindingGuide，不允许带病继续。
	initErr error
}

var (
	globalPaths *Paths
	once        sync.Once
)

// InitPaths 初始化路径解析（在应用启动最先调用）。
// 解析失败不在此处退出（无头模式需要自行决定报错通道），错误经 InitError()
// 返回，由调用方 fail loud；绝不静默回退用户目录。
func InitPaths() *Paths {
	once.Do(func() {
		globalPaths = resolvePaths()
		if globalPaths.initErr == nil {
			_ = ensureDirs(globalPaths)
		}
	})
	return globalPaths
}

// GetPaths 获取全局路径单例
func GetPaths() *Paths {
	if globalPaths == nil {
		return InitPaths()
	}
	return globalPaths
}

// InitError 返回数据根解析失败原因；nil 表示布局可用。
func (p *Paths) InitError() error { return p.initErr }

func resolvePaths() *Paths {
	exeDir := executableDir()
	base, mode, err := resolveDataRoot(exeDir)
	warnRetiredLegacyDataDir(exeDir)
	p := buildPaths(mode, base)
	p.initErr = err
	return p
}

func executableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

// resolveDataRoot F6 解析链（exeDir 注入，测试可 t.TempDir 造景）：
//  1. 绑定指针：exe 同级 hanxi.bind 存在即认绑定（声明无效也 fail loud，
//     绝不静默忽略——"用户说了算、无静默路径"）；
//  2. 应用同级 hanxidata/：存在即生效，无则自动创建（裸 exe 双击即活）；
//  3. 都不可用：返回带处置指引的错误，调用方弹窗退出。
func resolveDataRoot(exeDir string) (string, Mode, error) {
	target, declared, err := readBindFile(exeDir)
	if err != nil {
		return exeDir, modeSibling, err
	}
	if declared {
		if mkErr := ensureDir(target); mkErr != nil {
			return target, modeBound, fmt.Errorf("绑定的数据目录不可用（%s → %v）。\n%s", bindFileName, mkErr, guideSuffix)
		}
		return target, modeBound, nil
	}

	base := filepath.Join(exeDir, siblingDataDirName)
	if err := ensureDir(base); err != nil {
		return base, modeSibling, fmt.Errorf("无法在应用同级创建 %s/（%v）。\n%s", siblingDataDirName, err, guideSuffix)
	}
	return base, modeSibling, nil
}

// ensureDir 目录就绪判定：已存在的目录即就绪；不存在则逐级创建；
// 同名文件挡住或权限不足等一律返回错误（fail loud 的探测面）。
func ensureDir(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if fi.IsDir() {
			return nil
		}
		return fmt.Errorf("%s 已存在且不是目录", dir)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建 %s 失败: %w", dir, err)
	}
	return nil
}

// readBindFile 读取 exe 同级绑定声明。返回 (target, declared, err)：
// 文件不存在或内容全空白 → ("", false, nil) 无声明，走同级默认；
// 声明有效绝对路径 → (path, true, nil)；
// 存在但读不出合法声明 → (_, true, err) 由解析链 fail loud。
func readBindFile(exeDir string) (string, bool, error) {
	path := filepath.Join(exeDir, bindFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", true, fmt.Errorf("无法读取绑定声明 %s（%v）。\n%s", path, err, guideSuffix)
	}
	target, declared, perr := parseBindContent(raw)
	if perr != nil {
		return "", true, fmt.Errorf("%v。\n%s", perr, guideSuffix)
	}
	return target, declared, nil
}

// parseBindContent 从声明内容提取绑定目标（纯函数）：取首个非空行并去
// 前后空白（容忍记事本 UTF-8 BOM 与尾随换行）；全空白视作无声明；
// 非绝对路径判无效——相对路径会随工作目录漂移，违背"绑定哪个用哪个"。
func parseBindContent(raw []byte) (string, bool, error) {
	for _, line := range strings.Split(strings.TrimPrefix(string(raw), "\uFEFF"), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			return "", true, fmt.Errorf("%s 内容「%s」不是绝对路径", bindFileName, line)
		}
		return filepath.Clean(line), true, nil
	}
	return "", false, nil
}

// writeBindFile 落绑定指针（LF 单行，记事本可直接编辑）。
func writeBindFile(exeDir, target string) error {
	return os.WriteFile(filepath.Join(exeDir, bindFileName), []byte(target+"\n"), 0644)
}

// BindDataDir 显式绑定数据根：校验绝对路径并预创建目标，随后写 exe 同级
// 指针文件。重启后生效（路径快照在 InitPaths 一次解析）。
func BindDataDir(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("数据目录路径不能为空")
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("数据目录必须使用绝对路径（如 E:\\HanxiData），收到：%s", target)
	}
	if err := ensureDir(target); err != nil {
		return fmt.Errorf("目标数据目录不可用: %w", err)
	}
	exeDir := executableDir()
	if err := writeBindFile(exeDir, target); err != nil {
		return fmt.Errorf("exe 同级无法写入 %s（%v）——请先把应用移到可写位置再绑定", bindFileName, err)
	}
	slog.Info("data dir bound for next launch", "target", target)
	return nil
}

// UnbindDataDir 清除绑定声明，回到应用同级默认（幂等）。重启后生效。
func UnbindDataDir() error {
	err := os.Remove(filepath.Join(executableDir(), bindFileName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// warnRetiredLegacyDataDir 旧 data/ 兼容已废弃（F6 裁定③）：零识别零迁移，
// 不参与任何路径决策；仅当其中残留旧便携数据特征时告警指路一次，
// 避免老便携包用户升级后找不到家却毫无线索。
func warnRetiredLegacyDataDir(exeDir string) {
	if isHanxiDataRoot(filepath.Join(exeDir, legacyDataDirName)) {
		slog.Warn("检测到旧版便携数据目录 data/：该兼容已废弃，不会自动识别或搬迁；如有数据请手工移入当前数据根",
			"path", filepath.Join(exeDir, legacyDataDirName))
	}
}

// isHanxiDataRoot 判定目录是否携带 Hanxi 数据根特征（配置或托管版本树至少其一）。
// F6 后仅服务于两处非决策性场景：旧 data/ 废弃告警、%APPDATA% 旧家搬迁闸门。
func isHanxiDataRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "versions")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// buildPaths 按数据根派生全套子目录布局（同级根与绑定根共用同一形态）。
// 根目录只留应用级锚点：config.json（数据根特征签名，isHanxiDataRoot 依赖，
// 不可下迁）、logs/、versions/、runtime/、installers/（与 versions 同级，
// 托管模块的安装包缓存/组件二进制锚）与各模块状态文件（state/，经
// StateDir() 访问，v0.3.x 起由启动迁移从根目录收拢，见 state_migrate.go）。
func buildPaths(mode Mode, base string) *Paths {
	return &Paths{
		mode:        mode,
		baseDir:     base,
		configDir:   base,
		dataDir:     base,
		stateDir:    filepath.Join(base, "state"),
		logsDir:     filepath.Join(base, "logs"),
		versionsDir: filepath.Join(base, "versions"),
		runtimeDir:  filepath.Join(base, "runtime"),
	}
}

func ensureDirs(p *Paths) error {
	dirs := []string{p.baseDir, p.configDir, p.stateDir, p.logsDir, p.versionsDir, p.runtimeDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// 以下只读访问器返回 InitPaths 时解析好的绝对路径，无副作用。
// 硬约束（F6）：本组访问器的签名与语义自解析链改造起一字未动，38 个模块无感。
func (p *Paths) Mode() Mode          { return p.mode }
func (p *Paths) BaseDir() string     { return p.baseDir }
func (p *Paths) ConfigDir() string   { return p.configDir }
func (p *Paths) DataDir() string     { return p.dataDir }
func (p *Paths) StateDir() string    { return p.stateDir }
func (p *Paths) LogsDir() string     { return p.logsDir }
func (p *Paths) VersionsDir() string { return p.versionsDir }
func (p *Paths) RuntimeDir() string  { return p.runtimeDir }
func (p *Paths) ConfigFile() string  { return filepath.Join(p.configDir, "config.json") }
