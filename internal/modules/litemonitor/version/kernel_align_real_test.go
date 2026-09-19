//go:build windows

// 一次性"样板反迁内核"真包对齐核验（LM_REAL_ZIP 注入真实官方 zip 时运行，
// 平时 skip——非 CI 资产）。验证 GBK 中文文件名/单层包装目录的真实上游包
// 能过内核 UnpackZip 全闸门 + 模块布局吸收 + 真 PE 核账 + Tree 落位/扫描。
package version

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

func TestKernelAlignRealUpstreamZip(t *testing.T) {
	zipPath := os.Getenv("LM_REAL_ZIP")
	if zipPath == "" {
		t.Skip("LM_REAL_ZIP 未设置，跳过真包对齐核验")
	}
	m := NewManager(t.TempDir()) // 真 fileVersion（不注入假账目）

	staging, discard, err := m.tree.StageDir("align-real")
	if err != nil {
		t.Fatal(err)
	}
	defer discard()
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("真包过内核解包闸门失败: %v", err)
	}
	root, err := resolveStagedRoot(staging)
	if err != nil {
		t.Fatalf("真包布局吸收失败: %v", err)
	}
	t.Logf("installRoot = %s", filepath.Base(root))
	if actual, err := versioninfo.FileVersion(filepath.Join(root, exeName)); err != nil {
		t.Fatalf("真包 PE 读取失败: %v", err)
	} else if normalizeFileVersion(actual) != "1.3.6" {
		t.Fatalf("真包 PE 核账不匹配: %q", actual)
	}
	if err := m.tree.Commit(root, "1.3.6", artifact.Meta{Entry: exeName, Source: artifact.SourceRemote}); err != nil {
		t.Fatalf("真包落位失败: %v", err)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != "v1.3.6" {
		t.Fatalf("真包安装后 ListInstalled 异常: %+v err %v", list, err)
	}
}
