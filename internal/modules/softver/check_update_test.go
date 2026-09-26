package softver

// 雷达接入面（extapi.UpdateChecker）单测：probeLocal/fetchPage 全桩注入，
// 离线断言"本机装了什么形态的微信 × 官方页读数"→ update-available 裁决。
// 版本字符串夹具取真机实证形态（HKLM\SOFTWARE\WOW6432Node\...\Uninstall\
// Weixin：DisplayName"微信"/DisplayVersion 4.1.15.9，2026-09-18/09-26 双验证）。

import (
	"context"
	"errors"
	"testing"
)

func installOf(id, best string) LocalInstall {
	return LocalInstall{ID: id, BestVersion: best}
}

// fetchCounter 统计官方页实际外呼次数（TTL 复用/未装不外呼都要用它钉死）。
type fetchCounter struct {
	n    int
	body string
	err  error
}

func (f *fetchCounter) page(context.Context) (string, error) {
	f.n++
	return f.body, f.err
}

const fixtureOfficial4116 = `<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.16.exe">下载</a>`

func TestRadarCheckUpdateAvailable(t *testing.T) {
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "4.1.15.9")}}, nil
	})
	f := &fetchCounter{body: fixtureOfficial4116}
	s.fetchPage = f.page

	local, remote, has, err := s.checkUpdate(context.Background())
	if err != nil || !has {
		t.Fatalf("应判可更新，got local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
	if local != "4.1.15.9" || remote != "4.1.16" {
		t.Errorf("读数错位: local=%q remote=%q", local, remote)
	}
	if f.n != 1 {
		t.Errorf("应外呼官方页一次，got %d", f.n)
	}
	// 判定成功即回写页内共享缓存：Snapshot 无需再手动刷新就有官方读数。
	snap, _ := s.Snapshot()
	if snap.Official == nil || snap.Official.Version != "4.1.16" {
		t.Errorf("官方缓存未与页内同源: %+v", snap.Official)
	}
}

func TestRadarTTLCacheReuseNoSecondFetch(t *testing.T) {
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "4.1.15.9")}}, nil
	})
	f := &fetchCounter{body: fixtureOfficial4116}
	s.fetchPage = f.page

	if _, _, _, err := s.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 第二轮（模拟雷达再次感知）：10 分钟新鲜窗口内复用缓存，不再外呼。
	if _, _, _, err := s.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.n != 1 {
		t.Errorf("TTL 窗口内应只外呼一次，got %d", f.n)
	}
}

func TestRadarNoFalsePositiveOnSegmentCount(t *testing.T) {
	// 官方页只出三段（4.1.15）而本机四段（4.1.15.9）：同版本不同段数不得点亮雷达。
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "4.1.15.9")}}, nil
	})
	s.fetchPage = func(context.Context) (string, error) {
		return `<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe">下载</a>`, nil
	}
	_, remote, has, err := s.checkUpdate(context.Background())
	if err != nil || has || remote != "4.1.15" {
		t.Fatalf("应为无更新，got remote=%q has=%v err=%v", remote, has, err)
	}
}

func TestRadarDualGenerationTakesBest(t *testing.T) {
	// 3.x/4.x 双代共存夹具：本机侧取可比的最高读数参与对照。
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{
			installOf("wechat", "3.9.12.45"),
			installOf("weixin", "4.1.15.9"),
		}}, nil
	})
	s.fetchPage = func(context.Context) (string, error) { return fixtureOfficial4116, nil }
	local, _, has, err := s.checkUpdate(context.Background())
	if err != nil || !has || local != "4.1.15.9" {
		t.Fatalf("双代取最高口径失灵: local=%q has=%v err=%v", local, has, err)
	}
}

func TestRadarOnlyLegacyGenStillSignalsUpgrade(t *testing.T) {
	// 只装 3.9 的机器（他机形态，本仓从未有真机样本）：官方 4.1.x  universal
	// 包同样是真实可升级通道，点亮"有可用更新"。
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("wechat", "3.9.12.45")}}, nil
	})
	s.fetchPage = func(context.Context) (string, error) { return fixtureOfficial4116, nil }
	local, remote, has, err := s.checkUpdate(context.Background())
	if err != nil || !has || local != "3.9.12.45" || remote != "4.1.16" {
		t.Fatalf("3.x 单代机器应点亮: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
}

func TestRadarNotInstalledNoOutreach(t *testing.T) {
	f := &fetchCounter{body: fixtureOfficial4116}
	s := newTestService(func() (localData, error) { return localData{}, nil })
	s.fetchPage = f.page

	local, remote, has, err := s.checkUpdate(context.Background())
	if err != nil || has || local != "" || remote != "" {
		t.Fatalf("未装应回无可比事实: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
	if f.n != 0 {
		t.Errorf("未装不得外呼官方页，got %d 次", f.n)
	}
}

func TestRadarIncomparableVersionNoOutreach(t *testing.T) {
	// BestVersion 兜底成非数字段读数的形态（enrichInstall 注记路径）：
	// 无可比事实 = 不外呼、不点亮，页内仍有如实注记。
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "微信最新版")}}, nil
	})
	f := &fetchCounter{body: fixtureOfficial4116}
	s.fetchPage = f.page
	_, _, has, err := s.checkUpdate(context.Background())
	if err != nil || has || f.n != 0 {
		t.Fatalf("非规范读数不应判定/外呼: has=%v err=%v fetch=%d", has, err, f.n)
	}
}

func TestRadarRemoteFailureKeepsHealthAndRecordsError(t *testing.T) {
	probeErr := errors.New("官方页改版解析不出")
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "4.1.15.9")}}, nil
	})
	s.fetchPage = func(context.Context) (string, error) { return "", probeErr }
	local, remote, has, err := s.checkUpdate(context.Background())
	if !errors.Is(err, probeErr) || has || remote != "" || local != "4.1.15.9" {
		t.Fatalf("失败应上抛且不谎报: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
	// 失败原因与页内降级口径同源（OfficialError 可见，不建第二份真相）。
	snap, _ := s.Snapshot()
	if snap.OfficialError == "" {
		t.Error("雷达感知失败应同步记录 officialErr")
	}
}

func TestRadarProbeErrorPropagates(t *testing.T) {
	probeErr := errors.New("本机探测仅支持 Windows")
	s := newTestService(func() (localData, error) { return localData{}, probeErr })
	_, _, _, err := s.checkUpdate(context.Background())
	if !errors.Is(err, probeErr) {
		t.Fatalf("探测失败应原样上抛，got %v", err)
	}
}

// 过期缓存须重取：把 FetchedAt 拨回 TTL 之外，freshOfficial 必须判失效。
func TestRadarStaleCacheRefetch(t *testing.T) {
	s := newTestService(func() (localData, error) {
		return localData{Installs: []LocalInstall{installOf("weixin", "4.1.15.9")}}, nil
	})
	f := &fetchCounter{body: fixtureOfficial4116}
	s.fetchPage = f.page
	if _, _, _, err := s.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.official.FetchedAt = "2026-01-01T00:00:00+08:00" // 拨回很久以前
	s.mu.Unlock()
	if _, _, _, err := s.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.n != 2 {
		t.Errorf("缓存过期应重取，got 外呼 %d 次", f.n)
	}
}
