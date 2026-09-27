package snapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeDirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("当前 Windows 环境不允许创建目录符号链接: %v", err)
		}
		t.Fatal(err)
	}
}

func TestSafeEnumerationRejectsWhitelistLinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.json", `{"token":"outside"}`)
	makeDirLink(t, outside, filepath.Join(dataDir, rootState))

	if roots := WhitelistRoots(dataDir); len(roots) != 0 {
		t.Fatalf("链接根不得进入白名单根: %v", roots)
	}
	if _, found := ScanMtime(dataDir); found {
		t.Fatal("ScanMtime 不得跟随链接根")
	}
	if _, err := fingerprint(dataDir); err == nil {
		t.Fatal("备份指纹应拒绝链接根，而非读取数据根外内容")
	}
}

func TestSafeEnumerationRejectsNestedLinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, rootState), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, outside, "secret.json", `{"token":"outside"}`)
	makeDirLink(t, outside, filepath.Join(dataDir, rootState, "linked"))

	if _, found := ScanMtime(dataDir); found {
		t.Fatal("ScanMtime 不得跟随嵌套链接")
	}
	if _, err := fingerprint(dataDir); err == nil {
		t.Fatal("备份指纹应拒绝嵌套链接")
	}
}

func TestValidatePathLinksRejectsLinkAncestor(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	makeDirLink(t, outside, filepath.Join(dataDir, rootMemo))
	guard := whitelistDiskGuard
	guard.allowMissing = true
	if err := validatePathLinks(dataDir, "memo/a.md", guard); err == nil {
		t.Fatal("git 路径门卫应拒绝链接祖先")
	}
}

// TestValidatePathLinksMergedGuards 波 1 修复 4：恢复链与观察链双份实现合一后，
// 两侧既有用例语义都要兜住——白名单强制、叶子可缺、中间段缺失放行、链接祖先
// 拒绝、拒绝措辞按链保留。
func TestValidatePathLinksMergedGuards(t *testing.T) {
	dataDir := t.TempDir()
	write(t, dataDir, "state/live.json", `{}`)

	// 观察链强制白名单；恢复链不设第二道白名单闸（上游 normalizeWhitelistPath 已判）
	write(t, dataDir, "runtime/x.toml", "s") // 盘上真实存在，只差白名单口径
	if err := validatePathLinks(dataDir, "runtime/x.toml", whitelistDiskGuard); err == nil {
		t.Error("观察链白名单外应拒")
	}
	if err := validatePathLinks(dataDir, "runtime/x.toml", pendingTargetGuard); err != nil {
		t.Errorf("恢复链口径：白名单判定不在本闸重复设卡: %v", err)
	}
	// 叶子缺失：默认面报错，allowMissing 面放行
	if err := validatePathLinks(dataDir, "state/none.json", whitelistDiskGuard); err == nil {
		t.Error("叶子缺失默认应报错")
	}
	if err := validatePathLinks(dataDir, "state/none.json", pendingTargetGuard); err != nil {
		t.Errorf("allowMissing 应容忍叶子缺失: %v", err)
	}
	// 中间段缺失：恢复链旧语义是 continue 续查后放行，合并后同为放行
	if err := validatePathLinks(dataDir, "state2/sub/none.json", pendingTargetGuard); err != nil {
		t.Errorf("中间段缺失应放行: %v", err)
	}
	// 现存祖先安全时叶子可缺（pending 首写场景）
	if err := validatePathLinks(dataDir, "state/new.json", pendingTargetGuard); err != nil {
		t.Errorf("祖先安全+叶子缺应放行: %v", err)
	}
	// 链接祖先：两链一律拒绝，且各自保留措辞口径（链接挂 memo/ 下，不与前文
	// state/ 真实目录场景相互干扰）
	outside := t.TempDir()
	makeDirLink(t, outside, filepath.Join(dataDir, rootMemo))
	if err := validatePathLinks(dataDir, "memo/x.md", pendingTargetGuard); err == nil {
		t.Fatal("恢复链应拒绝链接祖先")
	} else if !strings.Contains(err.Error(), "恢复目标拒绝符号链接") {
		t.Errorf("恢复链拒绝措辞漂移: %v", err)
	}
	guard := whitelistDiskGuard
	guard.allowMissing = true
	if err := validatePathLinks(dataDir, "memo/x.md", guard); err == nil {
		t.Fatal("观察链 allowMissing 面同样应拒绝链接祖先")
	} else if !strings.Contains(err.Error(), "快照白名单拒绝符号链接") {
		t.Errorf("观察链拒绝措辞漂移: %v", err)
	}
}
