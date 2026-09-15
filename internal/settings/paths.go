package settings

import (
	"os"
	"path/filepath"
	"sync"

	"hanxi/internal/product"
)

// Mode 运行模式
type Mode string

const (
	ModePortable Mode = "portable" // 便携模式：数据完全落在可执行文件同级 hanxidata/
	ModeStandard Mode = "standard" // 标准模式：%APPDATA%/Hanxi
)

// 便携模式标记目录（exe 同级）。hanxidata 自带产品归属，避免泛化名 data 与他软件
// 或误建的空目录相撞；v0.3.0 及以前的便携包用 "data"，仍**有条件**兼容识别
// （必须含 config.json 或 versions/，空目录不触发），既不让升级便携版静默重置，
// 也消除"路过误建 data 就切便携"的歧义。
const (
	portableDataDirName       = "hanxidata"
	legacyPortableDataDirName = "data"
)

type Paths struct {
	mode        Mode
	baseDir     string
	configDir   string
	dataDir     string
	logsDir     string
	versionsDir string
	runtimeDir  string
}

var (
	globalPaths *Paths
	once        sync.Once
)

// InitPaths 初始化路径解析（在应用启动最先调用）
func InitPaths() *Paths {
	once.Do(func() {
		globalPaths = resolvePaths()
		_ = ensureDirs(globalPaths)
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

func resolvePaths() *Paths {
	exePath, err := os.Executable()
	var exeDir string
	if err != nil {
		exeDir = "."
	} else {
		exeDir = filepath.Dir(exePath)
	}

	// 1. 便携模式：exe 同级存在有效标记目录（hanxidata，或旧包遗留的真 data 数据根）
	if base, ok := detectPortableBaseDir(exeDir); ok {
		return buildPaths(ModePortable, base)
	}

	// 2. 标准模式：%APPDATA%/Hanxi 或 ~/.config/hanxi
	appData := os.Getenv("APPDATA")
	var base string
	if appData != "" {
		base = filepath.Join(appData, product.DataDirName)
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config", product.ExecutableName)
	}

	return buildPaths(ModeStandard, base)
}

// detectPortableBaseDir 探测 exe 同级的便携数据根目录：
// 首选 hanxidata（存在即生效，含空目录——归属明确，创建它本身就是用户的显式意图）；
// 回退兼容旧便携包的 data/——仅当其中已有 Hanxi 数据根特征（config.json 或 versions/）
// 时才认定，空 data 目录不触发，防止与无关同名目录相撞。
func detectPortableBaseDir(exeDir string) (string, bool) {
	primary := filepath.Join(exeDir, portableDataDirName)
	if fi, err := os.Stat(primary); err == nil && fi.IsDir() {
		return primary, true
	}

	legacy := filepath.Join(exeDir, legacyPortableDataDirName)
	if fi, err := os.Stat(legacy); err == nil && fi.IsDir() && isHanxiDataRoot(legacy) {
		return legacy, true
	}
	return "", false
}

// isHanxiDataRoot 判定目录是否携带 Hanxi 数据根特征（配置或托管版本树至少其一）。
func isHanxiDataRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "versions")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// buildPaths 按数据根派生全套子目录布局（便携与标准模式共用同一形态）。
func buildPaths(mode Mode, base string) *Paths {
	return &Paths{
		mode:        mode,
		baseDir:     base,
		configDir:   base,
		dataDir:     base,
		logsDir:     filepath.Join(base, "logs"),
		versionsDir: filepath.Join(base, "versions"),
		runtimeDir:  filepath.Join(base, "runtime"),
	}
}

func ensureDirs(p *Paths) error {
	dirs := []string{p.baseDir, p.configDir, p.logsDir, p.versionsDir, p.runtimeDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (p *Paths) Mode() Mode          { return p.mode }
func (p *Paths) BaseDir() string     { return p.baseDir }
func (p *Paths) ConfigDir() string   { return p.configDir }
func (p *Paths) DataDir() string     { return p.dataDir }
func (p *Paths) LogsDir() string     { return p.logsDir }
func (p *Paths) VersionsDir() string { return p.versionsDir }
func (p *Paths) RuntimeDir() string  { return p.runtimeDir }
func (p *Paths) ConfigFile() string  { return filepath.Join(p.configDir, "config.json") }
