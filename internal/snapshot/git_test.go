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

func TestFollowChainEvents(t *testing.T) {
	// 查询名是改名后的新名：R 到达事件记 R（PrePath=旧名），链名回拨后，
	// 更老的记录（git 以旧名上报）折入同一条时间线。
	recs := []versionChanges{
		{ID: "cccc3333", Time: "t3", Summary: "c3", Changes: []fileChange{
			{Status: "R", Path: "memo/renamed.md", Orig: "memo/keep.md"},
		}},
		{ID: "aaaa1111", Time: "t1", Summary: "c1", Changes: []fileChange{
			{Status: "A", Path: "memo/keep.md"},
		}},
	}
	evts := followChainEvents("memo/renamed.md", recs)
	if len(evts) != 2 {
		t.Fatalf("evts = %+v", evts)
	}
	if evts[0].Status != "R" || evts[0].AtPath != "memo/renamed.md" || evts[0].PrePath != "memo/keep.md" {
		t.Errorf("改名到达 = %+v", evts[0])
	}
	if evts[1].Status != "A" || evts[1].AtPath != "memo/keep.md" || evts[1].PrePath != "memo/keep.md" {
		t.Errorf("链上更早事件 = %+v", evts[1])
	}

	// 兜底折算：流里把"改名离开"报成 R 对（而非 git 实探的 D）时，旧名侧仍记 D
	evts2 := followChainEvents("memo/keep.md", recs)
	if len(evts2) != 2 || evts2[0].Status != "D" || evts2[0].AtPath != "memo/keep.md" {
		t.Errorf("改名离开兜底 = %+v", evts2)
	}
	if evts2[1].Status != "A" {
		t.Errorf("旧名更早事件 = %+v", evts2[1])
	}
}

// TestGitEngineEmptyRepoReadsEmpty 首拍前空仓库：全部读面给"空"而非错误。
func TestGitEngineEmptyRepoReadsEmpty(t *testing.T) {
	eng, _ := newTestGitEngine(t)
	ctx := context.Background()
	recs, err := eng.fileChanges(ctx, 10)
	if err != nil || len(recs) != 0 {
		t.Fatalf("empty fileChanges = %v %v", recs, err)
	}
	revs, err := eng.revisions(ctx, 10)
	if err != nil || revs == nil || len(revs) != 0 {
		t.Fatalf("empty revisions = %+v %v (须非 nil)", revs, err)
	}
	hist, err := eng.fileHistory(ctx, "config.json", 10)
	if err != nil || len(hist) != 0 {
		t.Fatalf("empty fileHistory = %+v %v", hist, err)
	}
}

// TestGitEngineFileAxis N33 批 A 文件为轴读面全链路（真实仓库）：
// fileChanges 事件流、fileHistory 改名链、fileDiff 新旧双读、
// 以及热修复 A 的"被删文件 → 最后存在版本可读"。
func TestGitEngineFileAxis(t *testing.T) {
	eng, dataDir := newTestGitEngine(t)
	ctx := context.Background()

	// c1: 三个文件诞生
	write(t, dataDir, "memo/gone.md", "v1")
	write(t, dataDir, "memo/keep.md", "keep")
	write(t, dataDir, "config.json", `{"theme":"light"}`)
	if err := eng.commit(ctx, []string{"config.json", "memo/gone.md", "memo/keep.md"}); err != nil {
		t.Fatal(err)
	}
	// c2: 改 config、删 gone
	write(t, dataDir, "config.json", `{"theme":"dark"}`)
	if err := os.Remove(filepath.Join(dataDir, "memo", "gone.md")); err != nil {
		t.Fatal(err)
	}
	if err := eng.commit(ctx, []string{"config.json", "memo/gone.md"}); err != nil {
		t.Fatal(err)
	}
	// c3: keep → renamed（内容不变，git 按 R100 报）
	write(t, dataDir, "memo/renamed.md", "keep")
	if err := os.Remove(filepath.Join(dataDir, "memo", "keep.md")); err != nil {
		t.Fatal(err)
	}
	if err := eng.commit(ctx, []string{"memo/keep.md", "memo/renamed.md"}); err != nil {
		t.Fatal(err)
	}

	recs, err := eng.fileChanges(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("fileChanges = %+v", recs)
	}
	// 新→旧：c3(R) / c2(M config, D gone) / c1(三个 A)
	per := aggregateFileEvents(recs)
	if got := statusSeq(per["memo/gone.md"]); got != "DA" {
		t.Errorf("gone 时间线（聚合） = %s", got)
	}
	if got := statusSeq(per["memo/renamed.md"]); got != "R" {
		t.Errorf("renamed 时间线（聚合） = %s", got)
	}
	if got := statusSeq(per["memo/keep.md"]); got != "DA" {
		t.Errorf("keep 时间线（聚合，改名离开折算 D） = %s", got)
	}
	if got := statusSeq(per["config.json"]); got != "MA" {
		t.Errorf("config 时间线（聚合） = %s", got)
	}

	// fileHistory：--follow 沿链（新名查得 R 到达 + 旧名时代的 A）
	hNew, err := eng.fileHistory(ctx, "memo/renamed.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusSeq(hNew); got != "RA" {
		t.Errorf("renamed 时间线（follow） = %s", got)
	}
	// 热修复核心：被删文件的时间线首个非 D 版本 = 最后存在版本，内容可读回
	hGone, err := eng.fileHistory(ctx, "memo/gone.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusSeq(hGone); got != "DA" {
		t.Fatalf("gone 时间线（follow） = %s", got)
	}
	lastAlive := ""
	for _, e := range hGone {
		if e.Status != "D" {
			lastAlive = e.RevisionID
			break
		}
	}
	if lastAlive != recs[2].ID {
		t.Fatalf("最后存在版本 = %q, want c1 %q", lastAlive, recs[2].ID)
	}
	if data, err := eng.file(ctx, lastAlive, "memo/gone.md"); err != nil || string(data) != "v1" {
		t.Fatalf("最后存在版本内容 = %q err=%v", data, err)
	}
	// 对照：删除版本自身取不到内容（病灶的引擎侧根因）
	if _, err := eng.file(ctx, recs[1].ID, "memo/gone.md"); err == nil {
		t.Error("已删除版本读内容应失败")
	}

	// fileDiff：删除 / 改名 / 新增 / 短 hash / 窗外
	d, err := eng.fileDiff(ctx, recs[1].ID, "memo/gone.md")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "D" || d.New != "" || d.Old != "v1" {
		t.Errorf("D 对比 = %+v", d)
	}
	d, err = eng.fileDiff(ctx, recs[0].ID, "memo/renamed.md")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "R" || d.New != "keep" || d.Old != "keep" {
		t.Errorf("R 对比（父版本按旧名取） = %+v", d)
	}
	d, err = eng.fileDiff(ctx, recs[2].ID, "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "A" || d.Old != "" || d.New != `{"theme":"light"}` {
		t.Errorf("A 对比（首版无旧） = %+v", d)
	}
	d, err = eng.fileDiff(ctx, recs[1].ID[:9], "memo/gone.md") // 短 hash 前缀
	if err != nil || d.Status != "D" {
		t.Errorf("短 hash = %+v err=%v", d, err)
	}
	if _, err := eng.fileDiff(ctx, recs[0].ID, "config.json"); err == nil ||
		!strings.Contains(err.Error(), "无变化记录") {
		t.Errorf("未触及该文件的版本应如实报错, got %v", err)
	}
	if _, err := eng.fileDiff(ctx, recs[0].ID, "runtime/x.toml"); err == nil {
		t.Error("白名单外对比应拒")
	}
	if _, err := eng.fileDiff(ctx, "20260917-143000", "config.json"); err == nil {
		t.Error("备份形态标识在 git 模式应拒")
	}
}

func statusSeq(revs []FileRevision) string {
	var b strings.Builder
	for _, r := range revs {
		b.WriteString(r.Status)
	}
	return b.String()
}
