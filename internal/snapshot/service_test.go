package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunCheckpointKeepsCommitWindowWriteDirty(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, rootConfig)
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	beforeCommit, found := ScanMtime(dataDir)
	if !found {
		t.Fatal("应发现初始配置")
	}

	eng := &fakeEngine{changedFiles: []string{rootConfig}}
	eng.commitFn = func() error {
		if err := os.WriteFile(path, []byte(`{"v":2}`), 0644); err != nil {
			return err
		}
		written := beforeCommit.Add(2 * time.Second)
		return os.Chtimes(path, written, written)
	}
	svc := &CheckpointService{scannedMT: beforeCommit, scanMtimeFn: func() (time.Time, bool) {
		return ScanMtime(dataDir)
	}}
	svc.runCheckpoint(eng)

	afterCommit, found := ScanMtime(dataDir)
	if !found || !afterCommit.After(beforeCommit) {
		t.Fatalf("测试造景失败: before=%v after=%v found=%v", beforeCommit, afterCommit, found)
	}
	svc.mu.Lock()
	watermark := svc.scannedMT
	svc.mu.Unlock()
	if !watermark.Before(afterCommit) {
		t.Fatalf("commit 期间新写被水位吞掉: watermark=%v write=%v", watermark, afterCommit)
	}
}

func TestDecideTick(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	written := base.Add(-10 * time.Minute) // 空闲已超 300s
	okDefault := tickInput{
		Enabled:     true,
		Found:       true,
		MaxMT:       written,
		ScannedMT:   written.Add(-time.Hour),
		Idle:        5 * time.Minute,
		Interval:    5 * time.Minute,
		Deactivated: true,
	}
	cases := []struct {
		name string
		mut  func(*tickInput, *time.Time)
		want tickDecision
	}{
		{"默认-失活+脏+空闲→拍", func(*tickInput, *time.Time) {}, tickGo},
		{"开关关", func(i *tickInput, _ *time.Time) { i.Enabled = false }, skipDisabled},
		{"在途", func(i *tickInput, _ *time.Time) { i.InFlight = true }, skipBusy},
		{"手动直通（免脏判/免空闲）", func(i *tickInput, _ *time.Time) {
			i.Force, i.Found, i.Deactivated = true, false, false
		}, tickGo},
		{"无文件", func(i *tickInput, _ *time.Time) { i.Found = false }, skipQuiet},
		{"mtime 未推进", func(i *tickInput, _ *time.Time) { i.ScannedMT = i.MaxMT }, skipQuiet},
		{"未空闲且未失活", func(i *tickInput, n *time.Time) {
			i.Deactivated = false
			i.MaxMT = n.Add(-time.Minute)
		}, skipActive},
		{"间隔闸", func(i *tickInput, _ *time.Time) { i.LastCommitAt = written.Add(9 * time.Minute) }, skipInterval},
		{"间隔闸已过", func(i *tickInput, _ *time.Time) { i.LastCommitAt = written.Add(-time.Hour) }, tickGo},
		{"从未提交", func(i *tickInput, _ *time.Time) { i.LastCommitAt = time.Time{} }, tickGo},
		{"空闲命中（未失活但静默够久）", func(i *tickInput, n *time.Time) {
			i.Deactivated = false
			i.MaxMT = n.Add(-6 * time.Minute)
		}, tickGo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := okDefault
			now := base
			c.mut(&in, &now)
			if got := decideTick(in, now); got != c.want {
				t.Errorf("decideTick = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParsePorcelain(t *testing.T) {
	// fixture 逐字节模拟 `git status --porcelain=v1 -z` 输出（记录以 NUL 收尾，
	// R/C 记录后多一段原路径 NUL）
	raw := strings.Join([]string{
		" M config.json",
		"?? state/new.json",
		"A  state/added.json",
		" D state/gone.json",
		"R  memo/new.md",
		"memo/old.md",
	}, "\x00") + "\x00"
	entries := parsePorcelain(raw)
	if len(entries) != 5 {
		t.Fatalf("want 5 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Status != " M" || entries[0].Path != "config.json" {
		t.Errorf("entry0 = %+v", entries[0])
	}
	if entries[4].Path != "memo/new.md" || entries[4].Orig != "memo/old.md" {
		t.Errorf("rename entry = %+v", entries[4])
	}
	if entries[3].Path != "state/gone.json" {
		t.Errorf("delete entry = %+v", entries[3])
	}
}

// tempRoot 注入服务层的数据根替身（settings.Paths 是全局单例测试不可造，
// dataRootPaths 接口收窄后以最小实现代替）。
type tempRoot struct{ dir string }

func (r tempRoot) DataDir() string { return r.dir }

func TestParseFileLog(t *testing.T) {
	// fixture 逐字节模仿实探的 `git log -z --name-status --format=%H%x1f%ct%x1f%s`
	// token 流：头 token 干净、每版首个状态 token 带前置换行残留（"\nM"）、后续
	// 裸态（"D"）；路径各自独立成 token；R 记录后跟两个路径 token（旧名在前）。
	h1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	unix1 := time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local).Unix()
	unix2 := time.Date(2026, 9, 16, 8, 30, 0, 0, time.Local).Unix()
	raw := h1 + "\x1f" + strconv.FormatInt(unix1, 10) + "\x1fcheckpoint: config.json, gone.md\x00" +
		"\nM\x00config.json\x00" +
		"D\x00memo/gone.md\x00" +
		"R100\x00runtime/out.md\x00memo/in.md\x00" + // 自保外改名而来：只报新名 R
		"R100\x00memo/away.md\x00runtime/ghost.md\x00" + // 改名出保：旧名折算 D
		h2 + "\x1f" + strconv.FormatInt(unix2, 10) + "\x1fcheckpoint: state/new.json\x00" +
		"\nA\x00state/new.json\x00"

	recs := parseFileLog(raw)
	if len(recs) != 2 {
		t.Fatalf("want 2 revisions, got %d: %+v", len(recs), recs)
	}
	r0 := recs[0]
	if r0.ID != h1 || r0.Summary != "config.json, gone.md" {
		t.Errorf("rec0 head = %q %q", r0.ID, r0.Summary)
	}
	if got, err := time.Parse(time.RFC3339, r0.Time); err != nil || !got.Equal(time.Unix(unix1, 0)) {
		t.Errorf("rec0 time = %q (err %v)", r0.Time, err)
	}
	want0 := []fileChange{
		{Status: "M", Path: "config.json"},
		{Status: "D", Path: "memo/gone.md"},
		{Status: "R", Path: "memo/in.md"},
		{Status: "D", Path: "memo/away.md"},
	}
	if len(r0.Changes) != len(want0) {
		t.Fatalf("rec0 changes = %+v", r0.Changes)
	}
	for i, c := range r0.Changes {
		if c != want0[i] {
			t.Errorf("rec0 change %d = %+v, want %+v", i, c, want0[i])
		}
	}
	if recs[1].ID != h2 || recs[1].Summary != "state/new.json" {
		t.Errorf("rec1 = %+v", recs[1])
	}
	if len(recs[1].Changes) != 1 || recs[1].Changes[0] != (fileChange{Status: "A", Path: "state/new.json"}) {
		t.Errorf("rec1 changes = %+v", recs[1].Changes)
	}
}

func TestAggregateFileEvents(t *testing.T) {
	recs := []versionChanges{
		{ID: "cccc3333", Time: "2026-09-17T10:00:00+08:00", Summary: "c3", Changes: []fileChange{
			{Status: "R", Path: "memo/new.md", Orig: "memo/old.md"},
		}},
		{ID: "aaaa1111", Time: "2026-09-16T08:00:00+08:00", Summary: "c1", Changes: []fileChange{
			{Status: "A", Path: "memo/old.md"},
			{Status: "", Path: "config.json"}, // 空态兜底为 M
		}},
	}
	per := aggregateFileEvents(recs)
	if len(per["memo/new.md"]) != 1 || per["memo/new.md"][0].Status != "R" || per["memo/new.md"][0].RevisionID != "cccc3333" {
		t.Errorf("new 侧事件 = %+v", per["memo/new.md"])
	}
	// 改名离开：旧名时间线收到 D（"恢复被删内容"据此回取最后存在版本）
	if len(per["memo/old.md"]) != 2 || per["memo/old.md"][0].Status != "D" || per["memo/old.md"][1].Status != "A" {
		t.Errorf("old 侧事件 = %+v", per["memo/old.md"])
	}
	if per["config.json"][0].Status != "M" {
		t.Errorf("空态兜底 = %+v", per["config.json"])
	}
	// 事件序保持新→旧
	if per["memo/old.md"][0].RevisionID != "cccc3333" {
		t.Error("事件未按新→旧排列")
	}
}

func TestFileHistoryGateAndTruncate(t *testing.T) {
	hist := []FileRevision{
		{RevisionID: "bbbb2222", Status: "D"},
		{RevisionID: "aaaa1111", Status: "M"},
	}
	svc := &CheckpointService{eng: &fakeEngine{historyRevs: hist}}
	got, err := svc.FileHistory("memo/gone.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RevisionID != "bbbb2222" {
		t.Fatalf("history = %+v", got)
	}
	if got, _ := svc.FileHistory("config.json", 1); len(got) != 1 {
		t.Errorf("limit 截断失效: %+v", got)
	}
	if got, _ := svc.FileHistory("config.json", 0); len(got) != 2 {
		t.Errorf("limit≤0 应回落全窗: %+v", got)
	}
	// 门卫：路径穿越 / 白名单外 / 服务未启动
	if _, err := svc.FileHistory("../evil", 10); err == nil {
		t.Error("穿越应拒")
	}
	if _, err := svc.FileHistory("runtime/frpc/x.toml", 10); err == nil {
		t.Error("白名单外应拒")
	}
	if _, err := (&CheckpointService{}).FileHistory("config.json", 10); err == nil {
		t.Error("未启动应给可读错误")
	}
}

// TestDeletedMemoRestoreChain N33 §0.3-A 热修复回归：被删便签的恢复目标 =
// 时间线上第一条非 D 版本（最后存在版本），内容经热恢复钩子复活。
// 引擎真实读面（"D 版本本身取不到内容"）在 git_test.go 覆盖。
func TestDeletedMemoRestoreChain(t *testing.T) {
	last := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	del := "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"
	fe := &fakeEngine{
		historyRevs: []FileRevision{{RevisionID: del, Status: "D"}, {RevisionID: last, Status: "M"}},
		data:        []byte("---\nid: memo_9\n---\n最后存在内容"),
	}
	svc := &CheckpointService{eng: fe}
	var gotID, gotContent string
	svc.SetMemoRestorer(func(id, content string) error {
		gotID, gotContent = id, content
		return nil
	})

	revs, err := svc.FileHistory("memo/memo_9.md", 50)
	if err != nil {
		t.Fatal(err)
	}
	// 前端同款目标解算（SnapshotSection.resolveRestoreTarget 的镜像）
	target := ""
	for _, r := range revs {
		if r.Status != "D" {
			target = r.RevisionID
			break
		}
	}
	if target != last {
		t.Fatalf("恢复目标 = %q, want %q", target, last)
	}
	if err := svc.RestoreFile(target, "memo/memo_9.md"); err != nil {
		t.Fatal(err)
	}
	if gotID != "memo_9" || gotContent != "---\nid: memo_9\n---\n最后存在内容" {
		t.Fatalf("热恢复入参 = %q %q", gotID, gotContent)
	}
	// 全 D（最后存在版本已滚出观察窗）→ 解算不出目标，前端如实提示而非盲恢复
	fe.historyRevs = []FileRevision{{RevisionID: del, Status: "D"}}
	revs, _ = svc.FileHistory("memo/memo_9.md", 50)
	for _, r := range revs {
		if r.Status != "D" {
			t.Fatal("全 D 列表不应解算出目标")
		}
	}
}

func TestListFilesMergesDiskAndEvents(t *testing.T) {
	dataDir := t.TempDir()
	write(t, dataDir, "config.json", `{"a":1}`)
	write(t, dataDir, "state/live.json", `{}`) // 从未入版：也要有行，版本数如实为 0
	t1 := time.Date(2026, 9, 16, 8, 0, 0, 0, time.Local)
	t2 := t1.Add(2 * time.Hour)
	fe := &fakeEngine{versionRecs: []versionChanges{
		{ID: "bbbb2222", Time: t2.Format(time.RFC3339), Summary: "c2", Changes: []fileChange{
			{Status: "D", Path: "memo/gone.md"}, {Status: "M", Path: "config.json"},
		}},
		{ID: "aaaa1111", Time: t1.Format(time.RFC3339), Summary: "c1", Changes: []fileChange{
			{Status: "A", Path: "memo/gone.md"}, {Status: "A", Path: "config.json"},
		}},
	}}
	svc := &CheckpointService{paths: tempRoot{dataDir}, eng: fe}
	svc.SetMemoTitleResolver(func(rel string) (string, bool) {
		if rel == "memo/gone.md" {
			return "被删的便签", true
		}
		return "", false
	})

	files, err := svc.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	// 排序：memo 组最前 → config → state；组内按 Display
	if files[0].Path != "memo/gone.md" || files[1].Path != "config.json" || files[2].Path != "state/live.json" {
		t.Fatalf("顺序 = %+v", files)
	}
	gone := files[0]
	if gone.Display != "被删的便签" || gone.Group != "memo" || gone.Revisions != 2 || gone.Alive {
		t.Errorf("gone 行 = %+v", gone)
	}
	if gone.LastChange != t2.Format(time.RFC3339) {
		t.Errorf("gone LastChange = %q", gone.LastChange)
	}
	if files[1].Display != "config.json" || files[1].Group != "config" || !files[1].Alive || files[1].Revisions != 2 {
		t.Errorf("config 行 = %+v", files[1])
	}
	live := files[2]
	if live.Revisions != 0 || !live.Alive || live.Group != "state" {
		t.Errorf("live 行 = %+v", live)
	}
	// RFC3339 无亚秒位：与截到秒的 mtime 对拍
	if got, perr := time.Parse(time.RFC3339, live.LastChange); perr != nil {
		t.Errorf("live LastChange = %q", live.LastChange)
	} else if !got.Equal(liveMtime(dataDir).Truncate(time.Second)) {
		t.Errorf("mtime 口径 = %v want %v", got, liveMtime(dataDir))
	}
}

func liveMtime(dataDir string) time.Time {
	fi, err := os.Stat(filepath.Join(dataDir, "state", "live.json"))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func TestListFilesGates(t *testing.T) {
	if _, err := (&CheckpointService{}).ListFiles(); err == nil {
		t.Error("未启动服务应给可读错误")
	}
	// 盘上枚举不可得（paths nil）：Alive 降级为"末事件非 D"口径
	fe := &fakeEngine{versionRecs: []versionChanges{
		{ID: "cccc3333", Time: "2026-09-17T10:00:00+08:00", Changes: []fileChange{
			{Status: "D", Path: "memo/gone.md"}, {Status: "M", Path: "config.json"},
		}},
	}}
	files, err := (&CheckpointService{eng: fe}).ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]TrackedFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if byPath["memo/gone.md"].Alive || !byPath["config.json"].Alive {
		t.Errorf("降级 Alive 口径 = %+v", files)
	}
}

func TestDiffFileTruncatesAndGates(t *testing.T) {
	huge := strings.Repeat("x", maxPreviewBytes+8)
	fe := &fakeEngine{diffRaw: fileDiffRaw{Status: "M", Old: huge, New: "tiny", Summary: "s"}}
	svc := &CheckpointService{eng: fe}
	fd, err := svc.DiffFile("aaaabbbb1234", "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if fd.Status != "M" || fd.New != "tiny" || len(fd.Old) != maxPreviewBytes || !fd.OldTruncated || fd.NewTruncated {
		t.Errorf("diff = %+v", fd)
	}
	fe.diffRaw = fileDiffRaw{Status: "A", Old: "x", New: huge}
	if fd, _ := svc.DiffFile("20260917-143000", "config.json"); !fd.NewTruncated || fd.OldTruncated {
		t.Errorf("new 截断 = %+v", fd)
	}
	if _, err := svc.DiffFile("zzz", "config.json"); err == nil {
		t.Error("非法版本标识应拒")
	}
	if _, err := svc.DiffFile("aaaabbbb1234", "runtime/x.toml"); err == nil {
		t.Error("白名单外应拒")
	}
	fe2 := &fakeEngine{diffErr: errors.New("窗口外")}
	if _, err := (&CheckpointService{eng: fe2}).DiffFile("aaaabbbb1234", "config.json"); err == nil {
		t.Error("引擎错误应透传")
	}
}

func TestPreviewRevisionDispatch(t *testing.T) {
	dataDir := t.TempDir()
	write(t, dataDir, "config.json", `{"cur":true}`)

	// revision 空 = 盘上现值（无需引擎）
	svc := &CheckpointService{paths: tempRoot{dataDir}}
	pv, err := svc.PreviewRevision("config.json", "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Content != `{"cur":true}` || pv.Truncated || pv.Size != int64(len(`{"cur":true}`)) {
		t.Errorf("现值预览 = %+v", pv)
	}
	if _, err := svc.PreviewRevision("state/nope.json", ""); err == nil {
		t.Error("盘上不存在的现值应给错误")
	}
	// 老绑定面 PreviewFile(id, path) = 版本预览包装
	fe := &fakeEngine{data: []byte("hist")}
	svc2 := &CheckpointService{paths: tempRoot{dataDir}, eng: fe}
	if pv2, err := svc2.PreviewFile("aaaabbbb1234", "config.json"); err != nil || pv2.Content != "hist" {
		t.Fatalf("PreviewFile 包装 = %+v %v", pv2, err)
	}
	if len(fe.calls) == 0 || fe.calls[len(fe.calls)-1] != "file:aaaabbbb1234:config.json" {
		t.Errorf("引擎调用参数 = %v", fe.calls)
	}
	// 超 512KB 截断
	fe.data = []byte(strings.Repeat("y", maxPreviewBytes+1))
	if pv3, _ := svc2.PreviewRevision("config.json", "aaaabbbb1234"); !pv3.Truncated || len(pv3.Content) != maxPreviewBytes {
		t.Errorf("截断 = %+v", pv3)
	}
	// 门卫
	if _, err := svc2.PreviewRevision("../config.json", ""); err == nil {
		t.Error("穿越应拒")
	}
	if _, err := svc2.PreviewRevision("runtime/x.toml", ""); err == nil {
		t.Error("白名单外应拒")
	}
	if _, err := svc2.PreviewRevision("config.json", "--help"); err == nil {
		t.Error("非法版本标识应拒")
	}
	if _, err := (&CheckpointService{}).PreviewRevision("config.json", "aaaabbbb1234"); err == nil {
		t.Error("未启动服务读版本应给可读错误")
	}
}

func TestParseNameStatus(t *testing.T) {
	out := "M\tconfig.json\nA\tstate/memo.json\nD\tstate/gone.json\nR100\tmemo/old.md\tmemo/new.md\n"
	files := parseNameStatus(out)
	if len(files) != 4 {
		t.Fatalf("want 4, got %+v", files)
	}
	if files[0] != (RevisionFile{Path: "config.json", Status: "M"}) {
		t.Errorf("files0 = %+v", files[0])
	}
	if files[3] != (RevisionFile{Path: "memo/new.md", Status: "R"}) {
		t.Errorf("rename row = %+v", files[3])
	}
}

func TestSummarizeFiles(t *testing.T) {
	if got := summarizeFiles([]string{"config.json"}); got != "config.json" {
		t.Errorf("summarize = %q", got)
	}
	five := []string{"a.json", "state/b.json", "state/c.json", "memo/d.md", "e.json"}
	if got := summarizeFiles(five); got != "a.json, b.json, c.json, d.md, e.json" {
		t.Errorf("summarize5 = %q", got)
	}
	six := append(five, "state/f.json")
	if got := summarizeFiles(six); got != "a.json, b.json, c.json, d.md, e.json 等 6 个文件" {
		t.Errorf("summarize6 = %q", got)
	}
}

func TestCheckRevisionID(t *testing.T) {
	for _, ok := range []string{"abc1234", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "20260917-143000", "20260917-143000-2"} {
		if err := checkRevisionID(ok); err != nil {
			t.Errorf("checkRevisionID(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "zz1234", "--help", "2026-09-17", "../x"} {
		if err := checkRevisionID(bad); err == nil {
			t.Errorf("checkRevisionID(%q) should fail", bad)
		}
	}
}

func TestNormalizeWhitelistPath(t *testing.T) {
	if got, err := normalizeWhitelistPath("config.json"); err != nil || got != "config.json" {
		t.Errorf("config.json: %v %v", got, err)
	}
	if got, err := normalizeWhitelistPath(`state\memo.json`); err != nil || got != "state/memo.json" {
		t.Errorf("backslash: %v %v", got, err)
	}
	for _, bad := range []string{"", "/config.json", "../config.json", "state/../config.json", "runtime/x.toml", "logs/x.log"} {
		if _, err := normalizeWhitelistPath(bad); err == nil {
			t.Errorf("normalizeWhitelistPath(%q) should fail", bad)
		}
	}
}
