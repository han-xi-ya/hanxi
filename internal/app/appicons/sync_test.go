package appicons

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// 双副本防漂移对账（文件头纪律的执法条）：内嵌的每个 PNG 必须与前端
// assets/apps 下同名文件逐字节一致——任何一侧单独改图（重提取/换素材）
// 都会在这里变红，杜绝"托盘和界面各画各的"。
func TestEmbeddedMatchesFrontendAssets(t *testing.T) {
	frontendDir := filepath.FromSlash("../../../frontend/src/assets/apps")
	if _, err := os.Stat(frontendDir); err != nil {
		t.Skipf("前端资产目录不可达（非常规仓库布局）: %v", err)
	}
	embedded, err := fs.Glob(FS(), "*.png")
	if err != nil || len(embedded) == 0 {
		t.Fatalf("内嵌 PNG 清单异常: %v %v", embedded, err)
	}
	for _, name := range embedded {
		want, err := os.ReadFile(filepath.Join(frontendDir, filepath.Base(name)))
		if err != nil {
			t.Errorf("前端缺少同名图标 %s: %v", name, err)
			continue
		}
		got, err := FS().(fs.ReadFileFS).ReadFile(name)
		if err != nil {
			t.Errorf("读取内嵌 %s 失败: %v", name, err)
			continue
		}
		if sha256.Sum256(want) != sha256.Sum256(got) {
			t.Errorf("%s 双副本漂移：前端与 Go 内嵌字节不一致，请同步两侧（scripts/extract_app_icons.ps1 重新提取后 cp）", name)
		}
	}
}

// For 语义：已知模块出 PNG 字节流（魔数校验证），未知模块恒 nil。
func TestForKnownAndUnknown(t *testing.T) {
	png := For("ccswitch")
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Fatalf("ccswitch 内嵌件缺失或非 PNG 流: len=%d", len(png))
	}
	if For("no-such-module") != nil {
		t.Error("未内嵌模块必须返回 nil（回落文字菜单，不给占位图）")
	}
	if For("") != nil {
		t.Error("空模块 ID 必须返回 nil")
	}
}
