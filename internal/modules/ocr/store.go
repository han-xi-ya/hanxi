package ocr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

// defaultListenPort 与上游 hanxi-ocr v0.2.0 内置默认端口一致。
const defaultListenPort = 53120

// ocrStore 持久化 hanxi-ocr 服务托管偏好（双引擎注册表形态，计划 §5.2）：
// 位置 <stateDir>/ocr.json，存——
//   - engines.{wechat,paddle}：各引擎 {path, version} 注册件（path 空 = 自动发现，
//     version 为导入时记录的组件版本，可空）；
//   - active：活跃引擎（单端口、单活引擎语义），默认 wechat；
//   - listenPort（服务端口）、followOnExit（随 Hanxi 退出联动，默认 true——托管
//     拉起的服务不该在宿主退出后变孤儿）、autoCopy（截屏后自动复制）。
//
// 兼容与演进：老单引擎配置的 exePath 在加载时迁移为微信引擎注册件；新写盘始终
// 双写 exePath 镜像（恒等于 engines.wechat.path），旧版 Hanxi 直接读新配置不炸。
// 原子写（tmp+rename）与损坏容忍（解析失败按默认值继续）收口至 internal/jsonstore。
type ocrStore struct {
	filePath string
	mu       sync.RWMutex

	// engines 注册表：键在构造时建齐、永增删（map 头可无锁读），表项可变以便
	// 调用方先过未知 ID 校验再取锁；path/version 的读写均由 mu 保护。
	engines map[string]*engineReg
	active  string

	followOnExit bool
	autoCopy     bool
	listenPort   int

	snipHotkeyEnabled bool   // 全局热键"剪贴板识图"开关（默认开）
	snipHotkey        string // 键位（规范化串，默认 Ctrl+Alt+T；纯键位不开——拍板决策）
}

// engineReg 单引擎注册项（内存形态）。
type engineReg struct {
	path    string // exe 绝对路径；空 = 自动发现
	version string // 最近登记的组件版本（导入时写入，可空）
}

// engineConfig 注册表项的 JSON 形态。
type engineConfig struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// engineRegistry ocr.json 的 engines 对象（字段序即键序：wechat 在前，读档友好）。
type engineRegistry struct {
	Wechat engineConfig `json:"wechat"`
	Paddle engineConfig `json:"paddle"`
}

// ocrConfig ocr.json 磁盘结构（exePath 为老字段兼容镜像，双写）。
type ocrConfig struct {
	ExePath      string         `json:"exePath"`
	Engines      engineRegistry `json:"engines"`
	Active       string         `json:"active"`
	FollowOnExit *bool          `json:"followOnExit"`
	AutoCopy     *bool          `json:"autoCopy"`
	ListenPort   *int           `json:"listenPort"`

	SnipHotkeyEnabled *bool   `json:"snipHotkeyEnabled"`
	SnipHotkey        *string `json:"snipHotkey"`
}

// defaultSnipHotkey 剪贴板识图热键默认键位（PLAN_CLIPBOARD 拍板：避让 snipaste
// 的 F1/Ctrl+Shift+A 与 keyviz 常用组合；纯 F 键不开放）。
const defaultSnipHotkey = "Ctrl+Alt+T"

func newOcrStore(dir string) *ocrStore {
	s := &ocrStore{
		filePath:     filepath.Join(dir, "ocr.json"),
		engines:      map[string]*engineReg{EngineWechat: {}, EnginePaddle: {}},
		active:       EngineWechat, // 默认微信引擎（当前分发唯一形态，老配置自然归位）
		followOnExit: true,
		autoCopy:     true, // 截屏识别后自动把文字复制进剪贴板（悬浮卡仍可手动复制）
		listenPort:   defaultListenPort,

		snipHotkeyEnabled: true, // 拍板：默认开（与截屏工作流同族；冲突时设置页红字引导改键）
		snipHotkey:        defaultSnipHotkey,
	}
	_ = s.load()
	return s
}

// NormalizeSnipHotkey 校验并规范化热键键位串（导出供服务层/单测复用）：
// 接受 "ctrl+alt+t" 等任意大小写与空格形态，产出 "Ctrl+Alt+T" 规范形。
// 约束（PLAN_CLIPBOARD 拍板）：至少一个修饰键（Ctrl/Alt/Shift/Win）——纯 F 键等
// 无修饰组合不开放，避让 snipaste(F1)/keyviz 等全局快捷键；主键单字符取大写，
// F 区键取 F+数字（≤F24），其余识别键收小写名（space/tab/escape…）。
func NormalizeSnipHotkey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("热键不能为空")
	}
	parts := strings.Split(raw, "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", fmt.Errorf("热键必须以主键结尾（如 Ctrl+Alt+T）")
	}
	seen := map[string]bool{}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			p = "Ctrl"
		case "alt", "option", "opt":
			p = "Alt"
		case "shift":
			p = "Shift"
		case "win", "windows", "super", "cmd":
			p = "Win"
		default:
			return "", fmt.Errorf("不支持的修饰键 %q（仅 Ctrl/Alt/Shift/Win）", strings.TrimSpace(p))
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		mods = append(mods, p)
	}
	if len(mods) == 0 {
		return "", fmt.Errorf("热键须含 Ctrl/Alt/Shift/Win 修饰键组合（纯键位不开，避让系统工具全局快捷键）")
	}
	norm := key
	switch {
	case len(key) == 1 && key[0] < 0x80:
		norm = strings.ToUpper(key) // 单字符主键（字母/数字）取大写
	case fKeyRe.MatchString(key):
		n, err := strconv.Atoi(key[1:])
		if err != nil || n < 1 || n > 24 {
			return "", fmt.Errorf("无法识别的主键 %q（F 区仅 F1~F24）", key)
		}
		norm = fmt.Sprintf("F%d", n)
	case namedKeyRe.MatchString(key):
		lower := strings.ToLower(key) // 具名键：space/tab/escape/page up…（展示取首字母大写形）
		norm = strings.ToUpper(lower[:1]) + lower[1:]
	default:
		return "", fmt.Errorf("无法识别的主键 %q（限单字符、F1~F24 或 space/escape 等具名键）", key)
	}
	return strings.Join(append(mods, norm), "+"), nil
}

// 主键识别三形态：单 ASCII 字符、F 区数字键、英文具名键（含 "page up" 类双词）。
var (
	fKeyRe     = regexp.MustCompile(`^[Ff]\d{1,2}$`)
	namedKeyRe = regexp.MustCompile(`^[A-Za-z]+( [A-Za-z]+)?$`)
)

func (s *ocrStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg ocrConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，字段自然兜底默认值
		return err
	}

	wechatPath := strings.TrimSpace(cfg.Engines.Wechat.Path)
	if wechatPath == "" {
		// 老单引擎配置迁移：exePath 归位为微信引擎注册件
		wechatPath = strings.TrimSpace(cfg.ExePath)
	}
	*s.engines[EngineWechat] = engineReg{
		path:    wechatPath,
		version: strings.TrimSpace(cfg.Engines.Wechat.Version),
	}
	*s.engines[EnginePaddle] = engineReg{
		path:    strings.TrimSpace(cfg.Engines.Paddle.Path),
		version: strings.TrimSpace(cfg.Engines.Paddle.Version),
	}
	if isKnownEngine(cfg.Active) {
		s.active = cfg.Active
	}
	if cfg.FollowOnExit != nil {
		s.followOnExit = *cfg.FollowOnExit
	}
	if cfg.AutoCopy != nil {
		s.autoCopy = *cfg.AutoCopy
	}
	if cfg.ListenPort != nil && *cfg.ListenPort >= 1024 && *cfg.ListenPort <= 65535 {
		s.listenPort = *cfg.ListenPort
	}
	if cfg.SnipHotkeyEnabled != nil {
		s.snipHotkeyEnabled = *cfg.SnipHotkeyEnabled
	}
	if cfg.SnipHotkey != nil {
		// 手改坏值容忍：规范化失败则回落默认键位（仿 listenPort 越界兜底策略）
		if norm, err := NormalizeSnipHotkey(*cfg.SnipHotkey); err == nil {
			s.snipHotkey = norm
		} else {
			s.snipHotkey = defaultSnipHotkey
		}
	}
	return nil
}

func (s *ocrStore) saveLocked() error {
	return jsonstore.Save(s.filePath, ocrConfig{
		ExePath: s.engines[EngineWechat].path, // 老字段镜像恒随微信注册件双写
		Engines: engineRegistry{
			Wechat: engineConfig{Path: s.engines[EngineWechat].path, Version: s.engines[EngineWechat].version},
			Paddle: engineConfig{Path: s.engines[EnginePaddle].path, Version: s.engines[EnginePaddle].version},
		},
		Active:       s.active,
		FollowOnExit: &s.followOnExit,
		AutoCopy:     &s.autoCopy,
		ListenPort:   &s.listenPort,

		SnipHotkeyEnabled: &s.snipHotkeyEnabled,
		SnipHotkey:        &s.snipHotkey,
	})
}

// ---------- 引擎注册表 ----------

// GetActiveEngine 返回活跃引擎 ID（EngineWechat / EnginePaddle）。
func (s *ocrStore) GetActiveEngine() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// SetActiveEngine 设定活跃引擎并落盘（单活语义的持久化原语；启停联动在服务层）。
func (s *ocrStore) SetActiveEngine(id string) error {
	if !isKnownEngine(id) {
		return fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == id {
		return nil
	}
	s.active = id
	return s.saveLocked()
}

// GetEnginePath 返回引擎注册件路径（空字符串 = 自动发现）；未登记 ID 返回空。
func (s *ocrStore) GetEnginePath(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if reg := s.engines[id]; reg != nil {
		return reg.path
	}
	return ""
}

// GetEngineVersion 返回引擎登记的组件版本（可空）；未登记 ID 返回空。
func (s *ocrStore) GetEngineVersion(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if reg := s.engines[id]; reg != nil {
		return reg.version
	}
	return ""
}

// SetEnginePath 设定引擎 exe 路径并落盘；""=恢复自动发现；
// 非空必须是存在的 .exe 文件（校验中文报错，风格仿 jsonstore.ValidateListenPort）。
func (s *ocrStore) SetEnginePath(id, path string) error {
	path = strings.TrimSpace(path)
	if path != "" {
		if !strings.EqualFold(filepath.Ext(path), ".exe") {
			return fmt.Errorf("服务路径必须以 .exe 结尾，当前选择的是 %q", filepath.Base(path))
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return fmt.Errorf("服务程序不存在：%s", path)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reg := s.engines[id]
	if reg == nil {
		return fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", id)
	}
	reg.path = path
	return s.saveLocked()
}

// SetEngineVersion 记录引擎组件版本并落盘；与当前相同则静默跳过（避免无谓写盘）。
func (s *ocrStore) SetEngineVersion(id, version string) error {
	version = strings.TrimSpace(version)
	s.mu.Lock()
	defer s.mu.Unlock()
	reg := s.engines[id]
	if reg == nil {
		return fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", id)
	}
	if reg.version == version {
		return nil
	}
	reg.version = version
	return s.saveLocked()
}

// ---------- 单引擎时代兼容口（双引擎过渡期） ----------
//
// 语义即"微信引擎注册件"——老配置 exePath 迁移后的自然归属。服务层的解析与
// 导入改走活跃引擎（计划 §5.3/§5.4）落地后，本组方法连同 exePath 镜像的读侧
// 依赖一起退役；写侧镜像永久保留（旧版 Hanxi 兼容）。

// GetExePath 返回微信引擎设定路径（空字符串 = 自动发现）。
func (s *ocrStore) GetExePath() string { return s.GetEnginePath(EngineWechat) }

// SetExePath 设定微信引擎 exe 路径并落盘；""=恢复自动发现。
func (s *ocrStore) SetExePath(path string) error { return s.SetEnginePath(EngineWechat, path) }

// ---------- 通用偏好 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *ocrStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（下次启动/停止托管实例时生效）。
func (s *ocrStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// GetAutoCopy 返回「截屏识别后自动复制文字」开关（默认 true）。
func (s *ocrStore) GetAutoCopy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoCopy
}

// SetAutoCopy 设定自动复制开关并落盘（下一次截屏识别生效）。
func (s *ocrStore) SetAutoCopy(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoCopy = v
	return s.saveLocked()
}

// GetListenPort 返回服务端口（默认 53120）。
func (s *ocrStore) GetListenPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listenPort
}

// SetListenPort 设定端口并立即落盘（校验范围 1024~65535）。
func (s *ocrStore) SetListenPort(port int) error {
	if err := jsonstore.ValidateListenPort(port); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenPort = port
	return s.saveLocked()
}

// ---------- 剪贴板识图热键偏好 ----------

// GetSnipHotkey 返回热键开关与规范化键位（默认 true / Ctrl+Alt+T）。
func (s *ocrStore) GetSnipHotkey() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snipHotkeyEnabled, s.snipHotkey
}

// SetSnipHotkeyEnabled 设定"全局热键剪贴板识图"开关并落盘（下一次接线/热重载生效）。
func (s *ocrStore) SetSnipHotkeyEnabled(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snipHotkeyEnabled = v
	return s.saveLocked()
}

// SetSnipHotkey 校验并规范化键位后落盘；返回规范化结果供上层回显。
// 校验失败（纯键位 / 未知修饰键 / 非法主键）以中文 error 返回，不落盘。
func (s *ocrStore) SetSnipHotkey(raw string) (string, error) {
	norm, err := NormalizeSnipHotkey(raw)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snipHotkey = norm
	if err := s.saveLocked(); err != nil {
		return norm, err
	}
	return norm, nil
}
