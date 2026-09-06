//go:build windows

package version

import (
	"strings"
	"testing"
)

func TestDecodeInstallerExit(t *testing.T) {
	cases := map[int]string{
		1:          "取消",
		2:          "内部错误",
		3:          "系统调用失败",
		4:          "初始化被拒",
		0xC0000005: "崩溃",
		0xC0000374: "异常终止", // 其他 NTSTATUS 归族
	}
	for code, want := range cases {
		got := decodeInstallerExit(code)
		if !strings.Contains(got, want) {
			t.Errorf("退出码 %d 文案应含 %q，实际 %q", code, want, got)
		}
	}
}

// TestDetectInstalledSmoke 本机冒烟：无安装版时返回零值；有则字段自洽
// （开发机常装 VS Code，此测试同时兜住注册表键路径与 exe 拼装正确性）。
func TestDetectInstalledSmoke(t *testing.T) {
	info := DetectInstalled()
	if !info.Installed {
		t.Log("本机未检测到安装版 VS Code，跳过字段校验")
		return
	}
	if info.ExePath == "" || !strings.EqualFold(strings.TrimSuffix(info.ExePath, "\\"), strings.TrimSuffix(info.Dir, "\\")+"\\Code.exe") {
		t.Errorf("路径拼装错误: %+v", info)
	}
	if !strings.HasSuffix(info.ExePath, exeName) {
		t.Errorf("ExePath 应以 Code.exe 结尾: %s", info.ExePath)
	}
	t.Logf("检测到安装版: %s @ %s", info.Version, info.Dir)
}
