package gitconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// withSeams 替换 lookPath / runConfigList 两个包级 seam，测试结束自动还原（不真跑 git）。
func withSeams(t *testing.T, lp func(string) (string, error), run func(context.Context, string) (string, error)) {
	t.Helper()
	oldLP, oldRun := lookPath, runConfigList
	lookPath, runConfigList = lp, run
	t.Cleanup(func() {
		lookPath, runConfigList = oldLP, oldRun
	})
}

var errLookPath = errors.New(`exec: "git": executable file not found in $PATH`)

func TestGlobalOverviewStates(t *testing.T) {
	t.Run("not-installed", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "", errLookPath },
			nil,
		)
		got := GlobalOverview(context.Background())
		if got.State != StateMissing || got.Detail == "" || got.Items != nil {
			t.Fatalf("unexpected missing result: %+v", got)
		}
		if !strings.Contains(got.Detail, "PATH") {
			t.Fatalf("missing detail should guide PATH: %q", got.Detail)
		}
	})

	t.Run("unconfigured-exit1-empty-output", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return `C:\Program Files\Git\cmd\git.exe`, nil },
			func(context.Context, string) (string, error) {
				return "", errors.New("git 退出异常: exit status 1")
			},
		)
		got := GlobalOverview(context.Background())
		if got.State != StateUnconfigured || got.Items != nil || got.Detail != "" {
			t.Fatalf("unexpected unconfigured result: %+v", got)
		}
	})

	t.Run("unconfigured-legacy-fatal", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "/usr/bin/git", nil },
			func(context.Context, string) (string, error) {
				return "fatal: unable to read config file '/home/x/.gitconfig': No such file or directory",
					errors.New("git 退出异常: exit status 128")
			},
		)
		got := GlobalOverview(context.Background())
		if got.State != StateUnconfigured {
			t.Fatalf("unexpected legacy-fatal result: %+v", got)
		}
	})

	t.Run("exit0-empty-output", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "/usr/bin/git", nil },
			func(context.Context, string) (string, error) { return "  \r\n ", nil },
		)
		if got := GlobalOverview(context.Background()); got.State != StateUnconfigured {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("error-timeout", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "/usr/bin/git", nil },
			func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w（%s）", errTimeout, readTimeout)
			},
		)
		got := GlobalOverview(context.Background())
		if got.State != StateError || !strings.Contains(got.Detail, "超时") {
			t.Fatalf("unexpected timeout result: %+v", got)
		}
	})

	t.Run("error-corrupt-config", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "/usr/bin/git", nil },
			func(context.Context, string) (string, error) {
				return "error: bad config line 3 in file /home/x/.gitconfig", errors.New("git 退出异常: exit status 128")
			},
		)
		got := GlobalOverview(context.Background())
		if got.State != StateError || !strings.Contains(got.Detail, "bad config line") {
			t.Fatalf("unexpected corrupt result: %+v", got)
		}
	})

	t.Run("configured-multi-line", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return `C:\Program Files\Git\cmd\git.exe`, nil },
			func(context.Context, string) (string, error) {
				return strings.Join([]string{
					"core.repositoryformatversion=0\r",
					"core.filemode=false\r",
					"core.autocrlf=true\r",
					"user.name=hanxi\r",
					"user.email=hanxi@example.com\r",
					"credential.helper=manager\r",
					"http.https://github.com/.proxy=http://127.0.0.1:7890\r",
					"url.https://alice:s3cr3t@github.com/.insteadof=github:\r",
					"alias.st=status\r",
					"", // 末尾空行
				}, "\n"), nil
			},
		)
		got := GlobalOverview(context.Background())
		if got.State != StateConfigured || len(got.Items) != 9 {
			t.Fatalf("unexpected configured result: state=%s items=%+v", got.State, got.Items)
		}
		want := []Entry{
			{Key: "core.repositoryformatversion", Value: "0"},
			{Key: "core.filemode", Value: "false"},
			{Key: "core.autocrlf", Value: "true"},
			{Key: "user.name", Value: "hanxi"},
			{Key: "user.email", Value: "hanxi@example.com"},
			{Key: "credential.helper", Value: "manager"},
			{Key: "http.https://github.com/.proxy", Value: MaskedValue},
			{Key: MaskedValue, Value: MaskedValue},
			{Key: "alias.st", Value: "status"},
		}
		for i, entry := range got.Items {
			if entry != want[i] {
				t.Fatalf("items[%d] = %+v, want %+v", i, entry, want[i])
			}
		}
	})
}

func TestParseEntriesSkipsGarbage(t *testing.T) {
	items := parseEntries("user.name=hanxi\nno-equals-line\n\ncore.pager=\n")
	if len(items) != 2 {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[1].Key != "core.pager" || items[1].Value != "" {
		t.Fatalf("empty value entry broken: %+v", items[1])
	}
}
