package ddnsgo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/jsonstore"
	"hanxi/internal/modules/ddnsgo/version"
)

// TestConsoleURLOf 监听地址 → 面板 URL 拼接。
func TestConsoleURLOf(t *testing.T) {
	if got := consoleURLOf("127.0.0.1:9876"); got != "http://127.0.0.1:9876/" {
		t.Fatalf("consoleURLOf = %q", got)
	}
}

// TestExternalConsoleCandidates 外部面板候选：设定端口优先、默认 9876 兜底、去重。
func TestExternalConsoleCandidates(t *testing.T) {
	// 设定端口与默认不同 → 两个候选，设定在前
	if got := externalConsoleCandidates(8080); !reflect.DeepEqual(got,
		[]string{"127.0.0.1:8080", "127.0.0.1:9876"}) {
		t.Errorf("candidates(8080) = %v", got)
	}
	// 设定端口即默认端口 → 去重只剩一个
	if got := externalConsoleCandidates(defaultListenPort); !reflect.DeepEqual(got,
		[]string{"127.0.0.1:9876"}) {
		t.Errorf("candidates(9876) = %v, want 单元素", got)
	}
}

// TestValidateListenPort 端口合法区间：1024~65535 通过，越界拒绝。
// （校验实现已收口至 jsonstore.ValidateListenPort，与 ocr 共用，断言原样保留。）
func TestValidateListenPort(t *testing.T) {
	for _, p := range []int{1024, 9876, 65535} {
		if err := jsonstore.ValidateListenPort(p); err != nil {
			t.Errorf("端口 %d 应合法，却报错 %v", p, err)
		}
	}
	for _, p := range []int{0, 80, 1023, 65536, -1} {
		if err := jsonstore.ValidateListenPort(p); err == nil {
			t.Errorf("端口 %d 应被拒绝", p)
		}
	}
}

// TestVersionCompare 数值分段比较：6.9.0 < 6.10.0（字典序会反），
// 主/次/补丁位逐级生效。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v6.10.0", "v6.9.0", 1},
		{"v6.9.0", "v6.10.0", -1},
		{"v6.17.6", "v6.17.6", 0},
		{"v7.0.0", "v6.99.99", 1},
		{"v6.17.7", "v6.17.6", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// newTestDdnsGoService 装配下载时序单测的最小 service（termora harness 同法：
// 结构体字面量注入；调用门未挂 = Enter/EnterBackground 直通，ops 账本未注入
// = 事务降级 no-op，engine 不在下载路径上故留空）。
func newTestDdnsGoService(t *testing.T) *DdnsGoService {
	t.Helper()
	return &DdnsGoService{
		manager: version.NewManager(t.TempDir()),
		store:   newDdnsgoStore(t.TempDir()),
		holder:  extapi.NewLeaseHolder(ID),
	}
}

// downloadReceipt 下载事件票据：stage/version 为回执载荷，activeAtBroadcast
// 由探针在广播位（downloadProbe 与生产 Wails Emit 同一点位）于下载 goroutine
// 内同步快照——即"前端此刻复刷 GetActiveVersion 会读到什么"的实况。
type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

// 锁死 termora 2ac9b3b 同型病灶的修复：前端共享 store 收到 done 成功回执即
// 复刷 GetActiveVersion，"首装自动设使用"的落账必须先于事件广播完成（断言取
// 广播位快照，非事后补读——事后读会与被测代码赛跑，负序回归可能漏网）。假驱动
// 经 seam 隔离网络（绝不真下载），事件时序与 journal/租约全走生产闭包实现。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := newTestDdnsGoService(t)
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(p version.DownloadProgress) {
		seen <- downloadReceipt{stage: p.Stage, version: p.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "verify"})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	// DownloadVersion 归一化 v 前缀，落账与回执都应按归一化后的口径核对。
	if state, err := svc.DownloadVersion("6.11.1"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v6.11.1" {
		t.Fatalf("done 回执版本应为归一化的 v6.11.1，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v6.11.1" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 v6.11.1，实得 %q", rr.activeAtBroadcast)
	}
	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"，语义随迁移保持不变）。
	if state, err := svc.DownloadVersion("6.12.0"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v6.12.0" {
		t.Fatalf("第二次下载应广播 v6.12.0 的 done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v6.11.1" {
		t.Fatalf("active 已有值时广播位不应被后续下载改写，实得 %q", rr.activeAtBroadcast)
	}
}

// waitDoneReceipt 等下载 goroutine 广播到 done 事件（drain 中间阶段），返回其
// 广播位快照；超时判失败。
func waitDoneReceipt(t *testing.T, seen <-chan downloadReceipt) downloadReceipt {
	t.Helper()
	for {
		select {
		case r := <-seen:
			if r.stage == "done" {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatal("等待下载 done 事件超时")
			return downloadReceipt{}
		}
	}
}
