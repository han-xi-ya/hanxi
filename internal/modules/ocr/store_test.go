package ocr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreDefaults(t *testing.T) {
	s := newOcrStore(t.TempDir())
	if got := s.GetListenPort(); got != defaultListenPort {
		t.Fatalf("默认端口 = %d, want %d", got, defaultListenPort)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("followOnExit 默认应为 true（托管实例不做孤儿）")
	}
	if s.GetExePath() != "" {
		t.Fatal("exePath 默认应为空 = 自动发现")
	}
}

func TestStoreRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	s := newOcrStore(dir)

	if err := s.SetListenPort(99999); err == nil {
		t.Fatal("越界端口应被拒")
	}
	if err := s.SetListenPort(54000); err != nil {
		t.Fatalf("合法端口被拒: %v", err)
	}
	if err := s.SetExePath(`C:\nowhere\hanxi-ocr.exe`); err == nil {
		t.Fatal("不存在的 exe 路径应被拒")
	}
	if err := s.SetExePath(filepath.Join(dir, "notexe.txt")); err == nil {
		t.Fatal("非 .exe 应被拒")
	}

	// 重新加载验证持久化
	s2 := newOcrStore(dir)
	if s2.GetListenPort() != 54000 {
		t.Fatalf("重载端口 = %d, want 54000", s2.GetListenPort())
	}

	// 空串 = 恢复自动发现，合法
	if err := s2.SetExePath(""); err != nil {
		t.Fatalf("清空 exePath 应被允许: %v", err)
	}
	if newOcrStore(dir).GetExePath() != "" {
		t.Fatal("清空后重载仍应为空")
	}
}

func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ocr.json"), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newOcrStore(dir)
	if s.GetListenPort() != defaultListenPort || !s.GetFollowOnExit() {
		t.Fatal("损坏文件应回落默认值继续")
	}
	// 损坏容忍不得把注册表打回 nil/错乱：结构完整、活跃引擎回落默认
	if s.GetActiveEngine() != EngineWechat || s.GetEnginePath(EnginePaddle) != "" {
		t.Fatalf("损坏文件下注册表应回落默认: active=%s", s.GetActiveEngine())
	}
}

// TestStoreLegacyConfigMigrationAndDualWrite 老单引擎配置迁移与双写镜像（计划 §5.2）：
// 旧 exePath → engines.wechat.path；新写盘仍带 exePath 镜像（恒 = 微信注册件），
// 且旧字段之外的 engines.{wechat,paddle} 与 active 完整落盘可重载。
func TestStoreLegacyConfigMigrationAndDualWrite(t *testing.T) {
	dir := t.TempDir()
	legacyPath := `C:\Hanxi\hanxi-ocr\hanxi-ocr.exe`
	raw := fmt.Sprintf(`{"exePath":%q,"followOnExit":false,"listenPort":54000}`, legacyPath)
	if err := os.WriteFile(filepath.Join(dir, "ocr.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	s := newOcrStore(dir)
	if got := s.GetEnginePath(EngineWechat); got != legacyPath {
		t.Fatalf("exePath 应迁移为微信引擎注册件, got %q", got)
	}
	if s.GetActiveEngine() != EngineWechat || s.GetEnginePath(EnginePaddle) != "" {
		t.Fatalf("迁移后默认态失真: active=%s paddle=%q", s.GetActiveEngine(), s.GetEnginePath(EnginePaddle))
	}
	if s.GetExePath() != legacyPath {
		t.Fatal("兼容口 GetExePath 应读到迁移后的微信注册件")
	}
	if s.GetFollowOnExit() || s.GetListenPort() != 54000 {
		t.Fatal("旧配置其余字段应照常读入")
	}

	// 登记 Paddle 组件并切活跃（服务层切换 API 属 §5.4，这里验 store 原语与持久化）
	paddleExe := filepath.Join(t.TempDir(), "hanxi-ocr.exe")
	if err := os.WriteFile(paddleExe, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnginePath(EnginePaddle, paddleExe); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEngineVersion(EnginePaddle, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveEngine(EnginePaddle); err != nil {
		t.Fatal(err)
	}

	s2 := newOcrStore(dir)
	if s2.GetActiveEngine() != EnginePaddle {
		t.Fatalf("重载 active = %s, want paddle", s2.GetActiveEngine())
	}
	if s2.GetEnginePath(EnginePaddle) != paddleExe || s2.GetEngineVersion(EnginePaddle) != "0.4.0" {
		t.Fatalf("重载 paddle 注册件失真: path=%q version=%q", s2.GetEnginePath(EnginePaddle), s2.GetEngineVersion(EnginePaddle))
	}
	if s2.GetEnginePath(EngineWechat) != legacyPath {
		t.Fatalf("切引擎不得弄丢微信注册件: %q", s2.GetEnginePath(EngineWechat))
	}

	// 盘上双写核对：新结构齐全 + 老 exePath 镜像恒随微信件（旧版 Hanxi 读新配置不炸）
	data, err := os.ReadFile(filepath.Join(dir, "ocr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg ocrConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("新配置应为合法 JSON: %v", err)
	}
	if cfg.ExePath != legacyPath {
		t.Fatalf("exePath 镜像 = %q, want 微信注册件 %q", cfg.ExePath, legacyPath)
	}
	if cfg.Engines.Wechat.Path != legacyPath || cfg.Engines.Paddle.Path != paddleExe || cfg.Active != EnginePaddle {
		t.Fatalf("注册表盘上字段丢失: %s", data)
	}
}

// TestStoreEngineRegistryValidation 注册表校验：未知引擎 ID、非法路径一律中文报错。
func TestStoreEngineRegistryValidation(t *testing.T) {
	dir := t.TempDir()
	s := newOcrStore(dir)

	if err := s.SetActiveEngine("cuda"); err == nil || !strings.Contains(err.Error(), "未知") {
		t.Fatalf("未知引擎 ID 应被拒: %v", err)
	}
	if err := s.SetEnginePath("cuda", ""); err == nil {
		t.Fatal("未登记引擎不得可写")
	}
	if err := s.SetEnginePath(EnginePaddle, filepath.Join(dir, "notexe.txt")); err == nil {
		t.Fatal("非 .exe 路径应被拒")
	}
	if err := s.SetEnginePath(EnginePaddle, `C:\nowhere\hanxi-ocr.exe`); err == nil {
		t.Fatal("不存在的 exe 应被拒")
	}
	if err := s.SetEnginePath(EngineWechat, ""); err != nil {
		t.Fatalf("清空注册件（恢复自动发现）应被允许: %v", err)
	}
	if err := s.SetEngineVersion(EngineWechat, "0.3.2"); err != nil {
		t.Fatal(err)
	}
	if newOcrStore(dir).GetEngineVersion(EngineWechat) != "0.3.2" {
		t.Fatal("version 应可持久化重载")
	}
	// active 未变更时不落盘也无需落盘：幂等静默
	if err := s.SetActiveEngine(EngineWechat); err != nil {
		t.Fatalf("同值 SetActiveEngine 应幂等: %v", err)
	}
}
