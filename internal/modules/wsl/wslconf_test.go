package wsl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateWslConf(t *testing.T) {
	good := "[boot]\nsystemd=true\n# comment\n; another\n[automount]\noptions=metadata\nenable=\n[user]\ndefault=lars\n"
	warns, err := validateWslConf(good)
	if err != nil {
		t.Fatalf("合法内容被拒: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "[boot]") {
		t.Fatalf("boot 版本警示缺失/多余: %v", warns)
	}
	// 重复节只警一次
	if warns, _ := validateWslConf("[boot]\nsystemd=true\n[boot]\ncommand=x\n"); len(warns) != 1 {
		t.Fatalf("重复节应只警一次: %v", warns)
	}
	for _, bad := range []string{"[boot\nsystemd=true\n", "=novalue\n", "[bad section!]\n", "random text line\n"} {
		if _, err := validateWslConf(bad); err == nil {
			t.Fatalf("非法内容应被拒: %q", bad)
		}
	}
}

func TestDefaultUserOf(t *testing.T) {
	text := "[boot]\nsystemd=true\n[user]\ndefault=lars # primary\n[automount]\nenable=true\n"
	if got := defaultUserOf(text); got != "lars" {
		t.Fatalf("default 提取错误（含行尾注释剔除）: %q", got)
	}
	if got := defaultUserOf("[boot]\nsystemd=true\n"); got != "" {
		t.Fatalf("无 [user] 节应为空: %q", got)
	}
}

// wslconfStub：Ubuntu 在册；guest 命令行为由 opts 覆写。
type wslconfTee struct {
	stdin string
	calls []string
}

func wslconfFixture(t *testing.T, svc *WslService, idOut string, idErr error, testFErr error, catOut string) *wslconfTee {
	t.Helper()
	svc.wslVersion = func(context.Context) string { return "2.7.13" }
	tee := &wslconfTee{}
	svc.runWsl = func(_ context.Context, args ...string) (string, error) {
		j := joined(args)
		tee.calls = append(tee.calls, j)
		switch {
		case j == "-l -q":
			return "Ubuntu\x00", nil
		case j == "-d Ubuntu -- true": // 开机探针
			return "", nil
		case j == "-d Ubuntu -- test -f "+wslConfPath: // Get 存在性判定（退出码口径）
			if strings.Contains(catOut, "No such file") {
				return "", errors.New("exit status 1")
			}
			return "", nil
		case strings.HasPrefix(j, "-d Ubuntu -u root -- id -u"):
			return idOut, idErr
		case strings.HasPrefix(j, "-d Ubuntu -u root -- test -f"):
			return "", testFErr
		case strings.HasPrefix(j, "-d Ubuntu -u root -- cp"):
			return "", nil
		case strings.HasPrefix(j, "-d Ubuntu -- cat"):
			return catOut, nil
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}
	svc.runWslIn = func(_ context.Context, stdin string, args ...string) (string, error) {
		tee.stdin = stdin
		tee.calls = append(tee.calls, joined(args)+" <STDIN>")
		return "", nil
	}
	return tee
}

func TestSaveWslConfHappyPathWithBackup(t *testing.T) {
	svc, _, _ := newTestService()
	const conf = "[boot]\nsystemd=true\n[user]\ndefault=lars"
	tee := wslconfFixture(t, svc, "1000", nil, nil, conf)
	res, err := svc.SaveWslConf("Ubuntu", strings.ReplaceAll(conf, "\n", "\r\n")) // CRLF 输入
	if err != nil || !res.Success {
		t.Fatalf("保存应成功: %v %+v", err, res)
	}
	if tee.stdin != conf+"\n" {
		t.Fatalf("写回必须 LF 归一 + 行尾换行: %q", tee.stdin)
	}
	var sawID, sawTest, sawCP bool
	for _, c := range tee.calls {
		switch {
		case strings.Contains(c, "id -u lars"):
			sawID = true
		case strings.Contains(c, "test -f "+wslConfPath):
			sawTest = true
		case strings.Contains(c, "cp -f "+wslConfPath+" "+wslConfBackup):
			sawCP = true
		}
	}
	if !sawID || !sawTest || !sawCP {
		t.Fatalf("三步防线顺序缺失: id=%v test=%v cp=%v calls=%v", sawID, sawTest, sawCP, tee.calls)
	}
	if !strings.Contains(res.Message, "复验一致") {
		t.Fatalf("回执应点明复验: %q", res.Message)
	}
}

func TestSaveWslConfRejectsUnknownUser(t *testing.T) {
	svc, _, _ := newTestService()
	tee := wslconfFixture(t, svc, "id: 'ghost': no such user", errors.New("exit status 1"), nil, "")
	_, err := svc.SaveWslConf("Ubuntu", "[user]\ndefault=ghost")
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("写坏默认用户必须拒绝: %v", err)
	}
	for _, c := range tee.calls {
		if strings.Contains(c, "tee") || strings.Contains(c, "cp") {
			t.Fatalf("引用校验失败后不得有任何写动作: %v", tee.calls)
		}
	}
}

func TestSaveWslConfRejectsBadSyntaxBeforeGuest(t *testing.T) {
	svc, _, _ := newTestService()
	tee := wslconfFixture(t, svc, "1000", nil, nil, "")
	if _, err := svc.SaveWslConf("Ubuntu", "[boot\nsystemd=true"); err == nil {
		t.Fatal("非法节头必须拒绝")
	}
	for _, c := range tee.calls {
		if strings.HasPrefix(c, "-d Ubuntu -u root") {
			t.Fatalf("语法闸门前不得触达 guest 写通道: %v", tee.calls)
		}
	}
}

func TestSaveWslConfNewFileSkipsBackup(t *testing.T) {
	svc, _, _ := newTestService()
	const conf = "[automount]\nenable=true"
	tee := wslconfFixture(t, svc, "", nil, errors.New("exit status 1"), conf) // test -f 失败 = 文件不存在
	if _, err := svc.SaveWslConf("Ubuntu", conf); err != nil {
		t.Fatal(err)
	}
	for _, c := range tee.calls {
		if strings.Contains(c, "cp -f") {
			t.Fatalf("首建无需备份却执行了 cp: %v", tee.calls)
		}
	}
}

func TestSaveWslConfReverifyMismatch(t *testing.T) {
	svc, _, _ := newTestService()
	tee := wslconfFixture(t, svc, "1000", nil, errors.New("not exist"), "tampered-content") // 读回与写入不一致
	_, err := svc.SaveWslConf("Ubuntu", "[automount]\nenable=true")
	if err == nil || !strings.Contains(err.Error(), "复验不一致") {
		t.Fatalf("复验失配必须报假成功红线错误: %v", err)
	}
	_ = tee
}

func TestGetWslConfMissingAndWarnings(t *testing.T) {
	svc, _, _ := newTestService()
	wslconfFixture(t, svc, "", nil, nil, "cat: /etc/wsl.conf: No such file or directory")
	// cat 失败且输出含 no such file → 走的是 Save 通道？Get 的 cat 桩：
	doc, err := svc.GetWslConf("Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Missing {
		t.Fatalf("文件不存在应报 Missing 空态: %+v", doc)
	}
	svc2, _, _ := newTestService()
	wslconfFixture(t, svc2, "", nil, nil, "[boot]\nsystemd=true")
	doc2, err := svc2.GetWslConf("Ubuntu")
	if err != nil || doc2.Missing || len(doc2.Warnings) == 0 {
		t.Fatalf("读取带版本警示内容错误: %v %+v", err, doc2)
	}
}
