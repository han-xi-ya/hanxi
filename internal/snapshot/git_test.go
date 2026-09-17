package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realGitProbe 取本机真实 git（沙箱无 git 时按环境受限跳过 git 路径测试，
// 降级影子拷贝路径另有 backup_test.go 全量覆盖）。
func realGitProbe(t *testing.T) gitProbe {
	t.Helper()
	exe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("环境无 git，跳过真实仓库链路测试")
	}
	out, err := defaultRunGitVersion(context.Background(), exe)
	if err != nil || !gitVersionRe.MatchString(out) {
		t.Skipf("本机 git 不可用（%v %q），跳过真实仓库链路测试", err, out)
	}
	return gitProbe{Available: true, Path: exe, Version: strings.TrimSpace(out)}
}

func newTestGitEngine(t *testing.T) (*gitEngine, string) {
	t.Helper()
	dataDir := t.TempDir()
	snapshotDir := filepath.Join(dataDir, snapshotsDirName)
	eng, err := newGitEngine(dataDir, snapshotDir, realGitProbe(t))
	if err != nil {
		t.Fatalf("newGitEngine: %v", err)
	}
	return eng, dataDir
}

func write(t *testing.T, dataDir, rel, content string) {
	t.Helper()
	p := filepath.Join(dataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestGitEngineLifecycle(t *testing.T) {
	eng, dataDir := newTestGitEngine(t)
	ctx := context.Background()

	write(t, dataDir, "config.json", `{"theme":"light"}`)
	write(t, dataDir, "state/memo.json", `[]`)
	write(t, dataDir, "runtime/frpc/frpc-1.toml", "token = 'secret'\n") // 明文雷区：永不得入库
	write(t, dataDir, "state/memo.json.tmp.4242", "debris")             // 原子写残骸：exclude 拦截
	write(t, dataDir, "config.json.corrupt-20260917-000000", "{}")      // 取证副本：exclude 拦截

	files, err := eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("changes = %v, want [config.json state/memo.json]", files)
	}
	for _, f := range files {
		if !Whitelisted(f) {
			t.Fatalf("越界变更混入: %s", f)
		}
	}
	if err := eng.commit(ctx, files); err != nil {
		t.Fatal(err)
	}

	// 提交后无变更
	again, err := eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("commit 后仍有变更: %v", again)
	}

	// 第二轮变更：改一个、删一个
	write(t, dataDir, "config.json", `{"theme":"dark"}`)
	if err := os.Remove(filepath.Join(dataDir, "state", "memo.json")); err != nil {
		t.Fatal(err)
	}
	files2, err := eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files2) != 2 {
		t.Fatalf("changes2 = %v", files2)
	}
	if err := eng.commit(ctx, files2); err != nil {
		t.Fatal(err)
	}

	revs, err := eng.revisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Fatalf("revisions = %+v", revs)
	}
	if !strings.Contains(revs[0].Summary, "config.json") {
		t.Errorf("最新摘要 = %q", revs[0].Summary)
	}

	details, err := eng.revisionFiles(ctx, revs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 2 {
		t.Errorf("rev0 files = %+v", details)
	}

	// 首版内容可从旧版本读回
	data, err := eng.file(ctx, revs[1].ID, "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"theme":"light"}` {
		t.Errorf("rev1 config.json = %q", data)
	}

	// 全库不得含 runtime/ 残骸内容：log --all --name-only 断言
	cmd := exec.CommandContext(ctx, eng.exe,
		"--git-dir="+eng.gitDir, "--work-tree="+eng.workTree,
		"log", "--all", "--name-only", "--format=")
	cmd.Dir = eng.workTree
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leak := range []string{"runtime/", ".tmp.4242", ".corrupt-"} {
		if strings.Contains(s, leak) {
			t.Fatalf("历史泄漏 %q：%s", leak, s)
		}
	}
	// .snapshots 自身也不得入保
	if strings.Contains(s, snapshotsDirName) {
		t.Fatalf("历史泄漏快照目录自身：%s", s)
	}
}

func TestGitEngineStaleIndexLock(t *testing.T) {
	eng, dataDir := newTestGitEngine(t)
	ctx := context.Background()

	write(t, dataDir, "config.json", `{"a":1}`)
	if err := eng.commit(ctx, []string{"config.json"}); err != nil {
		t.Fatal(err)
	}

	// 制造陈旧 index.lock（mtime 拨回 10 分钟前）+ 新变更
	write(t, dataDir, "state/x.json", `{}`)
	lock := filepath.Join(eng.gitDir, "index.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	files, err := eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.commit(ctx, files); err != nil {
		t.Fatalf("陈旧锁应被隔离并完成提交: %v", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("陈旧锁文件未被清理")
	}

	// 新鲜锁（<5min）不得抢删：commit 失败、锁保留
	if err := os.WriteFile(lock, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}
	write(t, dataDir, "state/y.json", `{}`)
	files, err = eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.commit(ctx, files); err == nil {
		t.Errorf("新鲜锁在场时 add 应失败（宁可少一拍）")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("新鲜锁不得被删除: %v", err)
	}
	_ = os.Remove(lock)
}

func TestGitEngineHeal(t *testing.T) {
	eng, dataDir := newTestGitEngine(t)
	ctx := context.Background()
	write(t, dataDir, "config.json", `{}`)
	if err := eng.commit(ctx, []string{"config.json"}); err != nil {
		t.Fatal(err)
	}
	// 破坏仓库：删 HEAD
	if err := os.Remove(filepath.Join(eng.gitDir, "HEAD")); err != nil {
		t.Fatal(err)
	}
	eng.heal(ctx)
	if _, err := os.Stat(filepath.Join(eng.gitDir, "HEAD")); err != nil {
		t.Fatalf("heal 后应重建仓库: %v", err)
	}
	// 旧仓库尽力保留为 .broken-*
	entries, err := os.ReadDir(filepath.Dir(eng.gitDir))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), gitRepoDirName+".broken-") {
			found = true
		}
	}
	if !found {
		t.Errorf("heal 应把损坏仓库改名保留，目录项: %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
