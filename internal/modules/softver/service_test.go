package softver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// 服务面单测：探测/取页/扫描/外呼全部桩注入，离线断言不碰注册表与网络。

type fakeOpener struct{ urls []string }

func (f *fakeOpener) OpenURL(url string) error { f.urls = append(f.urls, url); return nil }

func newTestService(probe func() (localData, error)) *SoftverService {
	return &SoftverService{
		probeLocal:    probe,
		fetchPage:     func(context.Context) (string, error) { return "", errors.New("未配置") },
		walk:          walkDirSize,
		canonicalPath: func(path string) (string, error) { return strings.ToLower(path), nil },
		emit:          func(string, any) {},
		reveal:        func(string) error { return nil },
		sizes:         map[string]*DirSize{},
		scans:         map[string]*scanJob{},
		activePaths:   map[string]string{},
	}
}

func slot(path string) DirSlot {
	return DirSlot{ID: dirSlotID("install", path), Kind: "install", Label: "安装目录", Path: path, Exists: true}
}

func TestSnapshotMergesCachedSizeAndCopiesSlots(t *testing.T) {
	base := slot(`D:\WeChat`)
	cached := &DirSize{Bytes: 42, Files: 1, ScannedAt: "2026-09-18T00:00:00Z"}
	probe := func() (localData, error) {
		return localData{Installs: []LocalInstall{{ID: "weixin", BestVersion: "4.1.15.9", Sources: []VersionSource{{Kind: "pe", Value: "4.1.15.9"}}}}, Dirs: []DirSlot{base}}, nil
	}
	s := newTestService(probe)
	s.sizes[base.ID] = cached

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Dirs[0].Size != cached {
		t.Fatalf("缓存未挂接: %+v", snap.Dirs[0].Size)
	}
	if base.Size != nil {
		t.Error("隔层改写了探测方返回的槽位（拷贝纪律失守）")
	}
	if len(snap.Scanning) != 0 {
		t.Errorf("无进行中扫描应为空，got %v", snap.Scanning)
	}
	if snap.Update == nil || snap.Update.Status != "noOfficial" {
		t.Errorf("官方未取时应为 noOfficial，got %+v", snap.Update)
	}
}

func TestRefreshOfficialCachesResultAndError(t *testing.T) {
	fixture := `<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe">下载</a>`
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{{ID: "weixin", BestVersion: "4.1.15.9"}}, Dirs: []DirSlot{slot(`E:\Program Files\Weixin`)}}, nil
	})
	s.fetchPage = func(context.Context) (string, error) { return fixture, nil }
	rel, err := s.RefreshOfficial()
	if err != nil || rel.Version != "4.1.15" || rel.FetchedAt == "" {
		t.Fatalf("RefreshOfficial = %+v, %v", rel, err)
	}
	snap, _ := s.Snapshot()
	if snap.Official == nil || snap.Official.Version != "4.1.15" {
		t.Fatalf("官方缓存未进快照: %+v", snap.Official)
	}
	// 本机 4.1.15.9 × 官方 4.1.15：同版本不同段数不得误报"可更新"。
	if snap.Update == nil || snap.Update.Status != "latest" {
		t.Errorf("Update = %+v, 期望 latest（前缀版本不误报）", snap.Update)
	}

	// 失败：缓存原因、保留旧读数。
	s.fetchPage = func(context.Context) (string, error) { return "", errors.New("网络不可达") }
	if _, err := s.RefreshOfficial(); err == nil {
		t.Fatal("失败应回报错误")
	}
	snap, _ = s.Snapshot()
	if snap.OfficialError == "" || snap.Official == nil {
		t.Errorf("失败应留原因不清旧值: err=%q off=%+v", snap.OfficialError, snap.Official)
	}
}

func TestRefreshOfficialParseFailureKeepsDegradePath(t *testing.T) {
	s := newTestService(func() (localData, error) { return localData{}, nil })
	s.fetchPage = func(context.Context) (string, error) { return "<html>改版了</html>", nil }
	if _, err := s.RefreshOfficial(); err == nil {
		t.Fatal("解析失配应报错（前端据此降级打开官方页）")
	}
	op := &fakeOpener{}
	s.opener = op
	if err := s.OpenUpdatesPage(); err != nil || len(op.urls) != 1 || op.urls[0] != UpdatesPageURL {
		t.Errorf("OpenUpdatesPage = %v urls=%v", err, op.urls)
	}
}

func TestUpdateHintMatrix(t *testing.T) {
	inst := func(v string) []LocalInstall { return []LocalInstall{{ID: "weixin", BestVersion: v}} }
	off := func(v string) *OfficialRelease { return &OfficialRelease{Version: v} }
	cases := []struct {
		name      string
		installs  []LocalInstall
		official  *OfficialRelease
		wantStat  string
		wantLocal string
	}{
		{"无本机", nil, off("4.1.15"), "noLocal", ""},
		{"本机四段×官方三段同版本", inst("4.1.15.9"), off("4.1.15"), "latest", "4.1.15.9"},
		{"官方更高可更新", inst("4.1.12.9"), off("4.1.15"), "available", "4.1.12.9"},
		{"官方未取", inst("4.1.15.9"), nil, "noOfficial", "4.1.15.9"},
		{"两套安装取更高", []LocalInstall{{ID: "wechat", BestVersion: "3.9.12.51"}, {ID: "weixin", BestVersion: "4.1.15.9"}}, off("4.2.0"), "available", "4.1.15.9"},
	}
	for _, c := range cases {
		h := updateHintFor(c.installs, c.official)
		if h.Status != c.wantStat {
			t.Errorf("%s: Status = %s, 期望 %s (%+v)", c.name, h.Status, c.wantStat, h)
		}
		if h.LocalVersion != c.wantLocal {
			t.Errorf("%s: LocalVersion = %q, 期望 %q", c.name, h.LocalVersion, c.wantLocal)
		}
	}
}

func TestScanLifecycleDoneCachesAndCancelsNotCache(t *testing.T) {
	st := slot(`D:\data40`)
	s := newTestService(func() (localData, error) {
		return localData{Dirs: []DirSlot{st}}, nil
	})
	events := make(chan ScanProgress, 8)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }

	// done：走真实小目录树。
	dir := t.TempDir()
	writeFileTree(t, dir)
	st.Exists = true
	st.Path = dir
	st.ID = dirSlotID("install", dir)
	s.local = localData{Dirs: []DirSlot{st}}
	if err := s.StartDirScan(st.ID); err != nil {
		t.Fatalf("StartDirScan: %v", err)
	}
	ev := waitScanEvent(t, events)
	if ev.State != "done" || ev.Bytes != 350 || ev.Files != 2 {
		t.Fatalf("done 事件异常: %+v", ev)
	}
	snap, _ := s.Snapshot()
	if snap.Dirs[0].Size == nil || snap.Dirs[0].Size.Bytes != 350 {
		t.Fatalf("结果未缓存: %+v", snap.Dirs[0].Size)
	}

	// canceled：假扫描阻塞到取消，半成品不得入缓存。
	slowID := dirSlotID("data40", `D:\slow`)
	s.local = localData{Dirs: []DirSlot{{ID: slowID, Kind: "data40", Path: `D:\slow`, Exists: true}}}
	s.walk = func(ctx context.Context, _ string, _ func(scanStat, string)) (scanStat, error) {
		<-ctx.Done()
		return scanStat{Bytes: 999}, ctx.Err()
	}
	if err := s.StartDirScan(slowID); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelDirScan(slowID); err != nil {
		t.Fatal(err)
	}
	ev = waitScanEvent(t, events)
	if ev.State != "canceled" {
		t.Fatalf("取消事件异常: %+v", ev)
	}
	s.mu.Lock()
	_, halfCached := s.sizes[slowID]
	s.mu.Unlock()
	if halfCached {
		t.Fatal("取消的半成品进了缓存")
	}
}

func TestScanGuards(t *testing.T) {
	st := slot(`D:\busy`)
	s := newTestService(func() (localData, error) { return localData{Dirs: []DirSlot{st}}, nil })
	if _, err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan("nosuch|d:\\x"); err == nil {
		t.Error("未知槽位应拒绝")
	}
	if err := s.StartDirScan("  "); err == nil {
		t.Error("空 ID 应拒绝")
	}
	// 同槽位重入拒绝 + 取消通道唯一（等终态事件后再断"无进行中"，无竞态）。
	events := make(chan ScanProgress, 4)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }
	s.walk = func(ctx context.Context, _ string, _ func(scanStat, string)) (scanStat, error) {
		<-ctx.Done()
		return scanStat{}, ctx.Err()
	}
	if err := s.StartDirScan(st.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan(st.ID); err == nil || !strings.Contains(err.Error(), "排队或扫描") {
		t.Errorf("重入应报忙: %v", err)
	}
	if err := s.CancelDirScan(st.ID); err != nil {
		t.Fatal(err)
	}
	if ev := waitScanEvent(t, events); ev.State != "canceled" {
		t.Fatalf("取消应出 canceled 终态: %+v", ev)
	}
	if err := s.CancelDirScan(st.ID); err == nil {
		t.Error("无进行中扫描的取消应报错")
	}
	// RevealDir 只认后端槽位面（任意路径字符串进不来）。
	var revealed []string
	s.reveal = func(p string) error { revealed = append(revealed, p); return nil }
	if err := s.RevealDir(`C:\Windows`); err == nil {
		t.Error("任意路径应被拒")
	}
	if err := s.RevealDir(st.ID); err != nil || len(revealed) != 1 || revealed[0] != `D:\busy` {
		t.Errorf("RevealDir = %v revealed=%v", err, revealed)
	}
}

func TestScanMissingDirRejected(t *testing.T) {
	ghost := DirSlot{ID: dirSlotID("data40", `D:\ghost`), Kind: "data40", Path: `D:\ghost`, Exists: false}
	s := newTestService(func() (localData, error) { return localData{Dirs: []DirSlot{ghost}}, nil })
	if _, err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan(ghost.ID); err == nil {
		t.Error("不存在目录应拒绝扫描")
	}
}

func TestDirScanGlobalLimitFIFOAndQueuedCancel(t *testing.T) {
	slots := []DirSlot{
		{ID: "a", Path: `D:\a`, Exists: true},
		{ID: "b", Path: `D:\b`, Exists: true},
		{ID: "c", Path: `D:\c`, Exists: true},
	}
	s := newTestService(func() (localData, error) { return localData{Dirs: slots}, nil })
	s.local = localData{Dirs: slots}

	started := make(chan string, 3)
	releases := map[string]chan struct{}{
		`D:\a`: make(chan struct{}),
		`D:\b`: make(chan struct{}),
		`D:\c`: make(chan struct{}),
	}
	s.walk = func(ctx context.Context, path string, _ func(scanStat, string)) (scanStat, error) {
		started <- path
		select {
		case <-releases[path]:
			return scanStat{}, nil
		case <-ctx.Done():
			return scanStat{}, ctx.Err()
		}
	}
	events := make(chan ScanProgress, 32)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }

	for _, id := range []string{"a", "b", "c"} {
		if err := s.StartDirScan(id); err != nil {
			t.Fatalf("StartDirScan(%s): %v", id, err)
		}
	}
	if got := waitStartedPath(t, started); got != `D:\a` {
		t.Fatalf("首个运行任务 = %q，期望 a", got)
	}
	waitEventForIDState(t, events, "a", "running")
	waitEventForIDState(t, events, "b", "queued")
	waitEventForIDState(t, events, "c", "queued")
	select {
	case got := <-started:
		t.Fatalf("全局并发上限失效，a 未释放时启动了 %q", got)
	case <-time.After(100 * time.Millisecond):
	}

	if err := s.CancelDirScan("b"); err != nil {
		t.Fatalf("取消排队任务: %v", err)
	}
	if ev := waitEventForIDState(t, events, "b", "canceled"); !strings.Contains(ev.Message, "排队") {
		t.Fatalf("排队取消事件不诚实: %+v", ev)
	}
	close(releases[`D:\a`])
	if got := waitStartedPath(t, started); got != `D:\c` {
		t.Fatalf("FIFO/取消后下一任务 = %q，期望 c", got)
	}
	close(releases[`D:\c`])
	waitEventForIDState(t, events, "c", "done")
}

func TestDirScanFIFOAcrossDifferentIDs(t *testing.T) {
	slots := []DirSlot{
		{ID: "a", Path: `D:\a`, Exists: true},
		{ID: "b", Path: `D:\b`, Exists: true},
		{ID: "c", Path: `D:\c`, Exists: true},
	}
	s := newTestService(func() (localData, error) { return localData{Dirs: slots}, nil })
	s.local = localData{Dirs: slots}
	started := make(chan string, 3)
	release := make(chan struct{}, 3)
	events := make(chan ScanProgress, 16)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }
	s.walk = func(ctx context.Context, path string, _ func(scanStat, string)) (scanStat, error) {
		started <- path
		select {
		case <-release:
			return scanStat{}, nil
		case <-ctx.Done():
			return scanStat{}, ctx.Err()
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := s.StartDirScan(id); err != nil {
			t.Fatal(err)
		}
	}
	wants := []struct {
		id   string
		path string
	}{{"a", `D:\a`}, {"b", `D:\b`}, {"c", `D:\c`}}
	for i, want := range wants {
		if got := waitStartedPath(t, started); got != want.path {
			t.Fatalf("第 %d 个运行任务 = %q，期望 %q", i+1, got, want.path)
		}
		waitEventForIDState(t, events, want.id, "running")
		release <- struct{}{}
	}
}

func TestDirScanDeduplicatesCanonicalPathAliases(t *testing.T) {
	slots := []DirSlot{
		{ID: "primary", Path: `D:\WeChat`, Exists: true},
		{ID: "alias", Path: `D:\alias`, Exists: true},
	}
	s := newTestService(func() (localData, error) { return localData{Dirs: slots}, nil })
	s.local = localData{Dirs: slots}
	s.canonicalPath = func(string) (string, error) { return `d:\wechat`, nil }
	s.walk = func(ctx context.Context, _ string, _ func(scanStat, string)) (scanStat, error) {
		<-ctx.Done()
		return scanStat{}, ctx.Err()
	}
	if err := s.StartDirScan("primary"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan("alias"); err == nil || !strings.Contains(err.Error(), "同一目录") {
		t.Fatalf("最终路径别名应拒绝重复扫描: %v", err)
	}
	if err := s.CancelDirScan("primary"); err != nil {
		t.Fatal(err)
	}
}

func TestDirScanPanicReleasesSlotAndContinuesQueue(t *testing.T) {
	slots := []DirSlot{
		{ID: "panic", Path: `D:\panic`, Exists: true},
		{ID: "next", Path: `D:\next`, Exists: true},
	}
	s := newTestService(func() (localData, error) { return localData{Dirs: slots}, nil })
	s.local = localData{Dirs: slots}
	started := make(chan string, 2)
	s.walk = func(_ context.Context, path string, _ func(scanStat, string)) (scanStat, error) {
		started <- path
		if path == `D:\panic` {
			panic("boom")
		}
		return scanStat{}, nil
	}
	events := make(chan ScanProgress, 16)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }
	if err := s.StartDirScan("panic"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan("next"); err != nil {
		t.Fatal(err)
	}
	waitEventForIDState(t, events, "panic", "error")
	if got := waitStartedPath(t, started); got != `D:\panic` {
		t.Fatalf("首个启动 = %q", got)
	}
	if got := waitStartedPath(t, started); got != `D:\next` {
		t.Fatalf("panic 后队列未继续，启动 = %q", got)
	}
	waitEventForIDState(t, events, "next", "done")
}

func TestCancelAllDirScansCancelsRunningAndQueued(t *testing.T) {
	slots := []DirSlot{
		{ID: "running", Path: `D:\running`, Exists: true},
		{ID: "queued", Path: `D:\queued`, Exists: true},
	}
	s := newTestService(func() (localData, error) { return localData{Dirs: slots}, nil })
	s.local = localData{Dirs: slots}
	started := make(chan string, 1)
	s.walk = func(ctx context.Context, path string, _ func(scanStat, string)) (scanStat, error) {
		started <- path
		<-ctx.Done()
		return scanStat{}, ctx.Err()
	}
	events := make(chan ScanProgress, 16)
	s.emit = func(_ string, p any) { events <- p.(ScanProgress) }
	if err := s.StartDirScan("running"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDirScan("queued"); err != nil {
		t.Fatal(err)
	}
	waitStartedPath(t, started)
	s.cancelAllDirScans()
	waitEventForIDState(t, events, "queued", "canceled")
	waitEventForIDState(t, events, "running", "canceled")

	s.mu.Lock()
	remaining := len(s.scans)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("取消全部后仍有 %d 个任务登记", remaining)
	}
}

func waitStartedPath(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case path := <-ch:
		return path
	case <-time.After(5 * time.Second):
		t.Fatal("等待扫描启动超时")
		return ""
	}
}

func waitEventForIDState(t *testing.T, ch <-chan ScanProgress, id, state string) ScanProgress {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			if ev.ID == id && ev.State == state {
				return ev
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("等待事件 %s/%s 超时", id, state)
			return ScanProgress{}
		}
	}
}

func TestDirSlotIDStable(t *testing.T) {
	a := dirSlotID("data40", `E:\System\文档\xwechat_files\`)
	b := dirSlotID("data40", `e:\system\文档\xwechat_files`)
	if a != b {
		t.Errorf("槽位 ID 跨大小写/尾斜杠应稳定: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "data40|") || strings.Contains(a, `\\`) {
		t.Errorf("ID 形态异常: %q", a)
	}
}

// waitScanEvent 等终态事件（queued/running 状态事件按需出现，跳过不算异常）。
func waitScanEvent(t *testing.T, ch chan ScanProgress) ScanProgress {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			if ev.State == "queued" || ev.State == "running" {
				continue
			}
			return ev
		case <-time.After(5 * time.Second):
			t.Fatal("等待扫描终态事件超时")
			return ScanProgress{}
		}
	}
}
