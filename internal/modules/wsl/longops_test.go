package wsl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- 取消通道 ----

func TestCancelIdleChannels(t *testing.T) {
	svc, _, _ := newTestService()
	if _, err := svc.CancelClone("Ubuntu"); err == nil {
		t.Fatal("无进行中克隆时取消必须报错")
	}
	if _, err := svc.CancelCompact(); err == nil {
		t.Fatal("无进行中瘦身时取消必须报错")
	}
	if _, err := svc.CancelMsiDownload(); err == nil {
		t.Fatal("无进行中下载时取消必须报错")
	}
}

func TestCancelCompactSafeStageInterruptsBackup(t *testing.T) {
	svc, _, _ := newTestService()
	compactFixture(t, svc, "n") // diskFree/exportDir/lxss 已注入（Ubuntu 在册且运行）
	// 备份段挂起直到 ctx 取消——真实还原"大 tar 导出中用户点取消"。
	svc.runWsl = func(ctx context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "--export" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		j := joined(args)
		if j == "-l -q" || j == "-l -q --running" {
			return "Ubuntu\x00", nil
		}
		return "", errors.New("意外的调用: " + j)
	}
	ch := make(chan CompactProgress, 8)
	svc.emit = func(_ string, payload any) {
		if p, ok := payload.(CompactProgress); ok && (p.Stage == "done" || p.Stage == "error") {
			select {
			case ch <- p:
			default:
			}
		}
	}
	if _, err := svc.CompactDistro("Ubuntu", ""); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CancelCompact() // 受理返回时已注册，stage=backup 属安全段
	if err != nil || !out.Success {
		t.Fatalf("备份段取消应放行: %v %+v", err, out)
	}
	select {
	case p := <-ch:
		if p.Stage != "error" || !strings.Contains(p.Error, "已按请求取消") || !strings.Contains(p.Error, "数据盘未被改动") {
			t.Fatalf("取消终态文案错误: %+v", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取消后备份段应尽快落终态")
	}
}

func TestCancelCompactRefusedInDestructiveStages(t *testing.T) {
	svc, _, _ := newTestService()
	for _, stage := range []string{"optimize", "reimport"} {
		svc.registerCompact(func() {})
		svc.setCompactStage(stage)
		if _, err := svc.CancelCompact(); err == nil || !strings.Contains(err.Error(), "拒绝") {
			t.Fatalf("%s 段取消必须被拒绝: %v", stage, err)
		}
	}
	svc.finishCompact()
}

// ---- 克隆空间预检 ----

func TestCloneRejectsFullTargetVolume(t *testing.T) {
	svc := newSvc(t)
	cloneFixture(t, svc, "2.7.13") // 已注入大剩余；再覆盖为紧张值
	diskFree = func(string) (uint64, error) { return 1 << 30, nil }
	dest := filepath.Join(t.TempDir(), "clone-full")
	if _, err := svc.CloneDistro("Ubuntu", "Ubuntu-Copy", dest); err == nil || !strings.Contains(err.Error(), "空间预检") {
		t.Fatalf("目标卷空间不足必须被预检拦下: %v", err)
	}
}

// ---- 账本逃生口 ----

func TestClearPortLedgerFile(t *testing.T) {
	svc, _ := ppFixture(t)
	if err := os.WriteFile(svc.ppPath, []byte("{坏的 json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListPortRules(); err == nil || !strings.Contains(err.Error(), "损坏") {
		t.Fatalf("损坏账本必须拒绝静默覆盖: %v", err)
	}
	out, err := svc.ClearPortLedgerFile()
	if err != nil || !out.Success {
		t.Fatalf("逃生口清账本失败: %v %+v", err, out)
	}
	stubPPShow(t, "")
	view, err := svc.ListPortRules()
	if err != nil || len(view.Rules) != 0 {
		t.Fatalf("清后应为空账本: %v %+v", err, view)
	}
}
