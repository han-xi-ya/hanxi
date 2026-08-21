package settings

import (
	"os"
	"path/filepath"
	"sync"
)

// Mode 运行模式
type Mode string

const (
	ModePortable Mode = "portable" // 便携模式：数据完全落在可执行文件同级 data/
	ModeStandard Mode = "standard" // 标准模式：%APPDATA%/HubKit
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

	// 1. 检查 exe 同级是否存在 data 目录
	candidateData := filepath.Join(exeDir, "data")
	if fi, err := os.Stat(candidateData); err == nil && fi.IsDir() {
		return &Paths{
			mode:        ModePortable,
			baseDir:     candidateData,
			configDir:   candidateData,
			dataDir:     candidateData,
			logsDir:     filepath.Join(candidateData, "logs"),
			versionsDir: filepath.Join(candidateData, "versions"),
			runtimeDir:  filepath.Join(candidateData, "runtime"),
		}
	}

	// 2. 标准模式：%APPDATA%/HubKit 或 ~/.config/hubkit
	appData := os.Getenv("APPDATA")
	var base string
	if appData != "" {
		base = filepath.Join(appData, "HubKit")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config", "hubkit")
	}

	return &Paths{
		mode:        ModeStandard,
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
