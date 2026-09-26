package usbipd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// wingetOutput 脚本一步的固定回放。
type wingetOutput struct {
	stdout string
	stderr string
	err    error
}

// scriptedWinget 按脚本顺序回放 winget 调用并记录实际参数（脚本用尽后重复末步）。
func scriptedWinget(outputs ...wingetOutput) (WingetRunFunc, *[][]string) {
	var calls [][]string
	i := 0
	return func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, args)
		o := outputs[min(i, len(outputs)-1)]
		i++
		return o.stdout, o.stderr, o.err
	}, &calls
}

func TestWingetArgsAreFixedLiterals(t *testing.T) {
	install := strings.Join(WingetInstallArgs(), " ")
	if want := "install --id dorssel.usbipd-win -e --accept-source-agreements --accept-package-agreements --disable-interactivity"; install != want {
		t.Errorf("安装命令必须钉死为全字面量:\n got %q\nwant %q", install, want)
	}
	list := strings.Join(WingetListArgs(), " ")
	if want := "list --id dorssel.usbipd-win -e --accept-source-agreements"; list != want {
		t.Errorf("探测命令必须钉死为全字面量:\n got %q\nwant %q", list, want)
	}
}

func TestWingetMissingGuidesInsteadOfFailing(t *testing.T) {
	run, calls := scriptedWinget(wingetOutput{err: fmt.Errorf("w: %w", exec.ErrNotFound)})
	_, err := NewWingetInstallerWith(run).Install(context.Background())
	if !errors.Is(err, ErrWingetMissing) {
		t.Fatalf("winget 缺席必须命中 ErrWingetMissing 哨兵: %v", err)
	}
	if len(*calls) != 1 {
		t.Errorf("探测缺席后不得继续发起安装，calls=%d", len(*calls))
	}
}

func TestWingetAlreadyInstalledSkipsInstall(t *testing.T) {
	run, calls := scriptedWinget(wingetOutput{
		stdout: "名称         ID                   版本   源\nusbipd-win   dorssel.usbipd-win   5.3.0   winget",
	})
	res, err := NewWingetInstallerWith(run).Install(context.Background())
	if err != nil || res.State != InstallAlready {
		t.Fatalf("list 命中包 ID 应判已装: %+v %v", res, err)
	}
	if len(*calls) != 1 || strings.Join((*calls)[0], " ") != strings.Join(WingetListArgs(), " ") {
		t.Errorf("已装场景不得执行 install: %v", *calls)
	}
}

func TestWingetListNotFoundProceedsToInstall(t *testing.T) {
	run, calls := scriptedWinget(
		wingetOutput{stdout: "找不到与输入条件匹配的已安装程序包。", err: &fakeExit{code: -1978335228}},
		wingetOutput{stdout: "已安装 usbipd-win。成功: "},
	)
	res, err := NewWingetInstallerWith(run).Install(context.Background())
	if err != nil || res.State != InstallDone {
		t.Fatalf("未命中应继续安装并回 done: %+v %v", res, err)
	}
	if len(*calls) != 2 || strings.Join((*calls)[1], " ") != strings.Join(WingetInstallArgs(), " ") {
		t.Errorf("安装命令必须逐字取用字面量: %v", *calls)
	}
}

func TestWingetMissingAtInstallStage(t *testing.T) {
	run, _ := scriptedWinget(
		wingetOutput{stdout: "No installed package found matching input criteria.", err: &fakeExit{code: 1}},
		wingetOutput{err: exec.ErrNotFound},
	)
	_, err := NewWingetInstallerWith(run).Install(context.Background())
	if !errors.Is(err, ErrWingetMissing) {
		t.Fatalf("安装阶段发现 winget 缺席同样是前提问题: %v", err)
	}
}

// UAC 取消的三种可观测形态（1223 / HRESULT 有/无符号、中英文案）都必须落
// cancelled——谎报成功是红线。
func TestWingetCancelNeverReportsSuccess(t *testing.T) {
	cases := []struct {
		name  string
		code  int
		stdin string
	}{
		{"退出码1223", 1223, ""},
		{"HRESULT无符号", 2147943623, ""},
		{"HRESULT有符号", -2147023673, ""},
		{"文案0x800704C7", 1, "安装包执行失败: 0x800704C7"},
		{"英文取消文案", 1, "This operation was canceled by the user."},
		{"中文取消文案", 1, "此操作已被用户取消。"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run, _ := scriptedWinget(
				wingetOutput{stdout: "No installed package found matching input criteria.", err: &fakeExit{code: 1}},
				wingetOutput{stderr: c.stdin, err: &fakeExit{code: c.code, text: "winget exit"}},
			)
			res, err := NewWingetInstallerWith(run).Install(context.Background())
			if err != nil || res.State != InstallCancelled {
				t.Fatalf("取消必须归为 cancelled 而非成功/失败: %+v %v", res, err)
			}
		})
	}
}

func TestWingetFailedCarriesChineseAttribution(t *testing.T) {
	run, _ := scriptedWinget(
		wingetOutput{stdout: "No installed package found matching input criteria.", err: &fakeExit{code: 1}},
		wingetOutput{
			stdout: "尝试源协商…\n下载失败时连接到互联网。\n0x80072f7d : server unable to decrypt received data",
			err:    &fakeExit{code: -2147012739},
		},
	)
	res, err := NewWingetInstallerWith(run).Install(context.Background())
	if err != nil || res.State != InstallFailed {
		t.Fatalf("非零退出且非取消应归为 failed: %+v %v", res, err)
	}
	if !strings.Contains(res.Detail, "联网") {
		t.Errorf("网络类失败应中文化归因: %q", res.Detail)
	}
	// 兜底路径：无法识别的失败必须原样带出输出尾部与退出码，绝不吞信息。
	run2, _ := scriptedWinget(
		wingetOutput{stdout: "No installed package found matching input criteria.", err: &fakeExit{code: 1}},
		wingetOutput{stdout: "line one\nmystery line two", err: &fakeExit{code: 42}},
	)
	res2, err2 := NewWingetInstallerWith(run2).Install(context.Background())
	if err2 != nil || res2.State != InstallFailed ||
		!strings.Contains(res2.Detail, "mystery line two") || !strings.Contains(res2.Detail, "42") {
		t.Fatalf("未知失败应带尾部原文+退出码: %+v %v", res2, err2)
	}
}
