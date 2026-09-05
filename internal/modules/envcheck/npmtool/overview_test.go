package npmtool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

func fixedDeps(local map[string]detect.ToolInfo, latest map[string]string, prefix string, npmErr error) Deps {
	return Deps{
		Detect: func(_ context.Context, name string) (detect.ToolInfo, error) {
			info, ok := local[name]
			if !ok {
				return detect.ToolInfo{Name: name, Status: detect.StatusMissing}, nil
			}
			return info, nil
		},
		Latest: func(pkg string) (remoteversion.Release, bool, time.Time, error) {
			version, ok := latest[pkg]
			if !ok {
				return remoteversion.Release{}, false, time.Time{}, errors.New("offline")
			}
			return remoteversion.Release{Version: version}, false, time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local), nil
		},
		GlobDir: func(context.Context) (string, error) { return prefix, npmErr },
	}
}

func firstTool(t *testing.T, deps Deps) ToolOverview {
	t.Helper()
	overview, err := buildOverview(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Tools) == 0 {
		t.Fatal("no tools")
	}
	return overview.Tools[0] // catalog[0] = claude
}

func TestOverviewUpdateAndLatest(t *testing.T) {
	// claude 已装且落后 → update-available；无 prefix 提醒（命中目录内）。
	deps := fixedDeps(
		map[string]detect.ToolInfo{"claude": {Name: "claude", Display: "Claude Code", Version: "2.1.260", Status: detect.StatusInstalled, Path: `C:\npm\claude.cmd`}},
		map[string]string{"@anthropic-ai/claude-code": "2.1.261"},
		`C:\npm`, nil,
	)
	got := firstTool(t, deps)
	if got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("relation=%q", got.Relation)
	}
	if got.RelationDetail != "" {
		t.Fatalf("unexpected detail=%q", got.RelationDetail)
	}
	if got.FetchedAt == "" || got.IsStale {
		t.Fatalf("fetchedAt=%q stale=%v", got.FetchedAt, got.IsStale)
	}
}

func TestOverviewNotInstalled(t *testing.T) {
	deps := fixedDeps(map[string]detect.ToolInfo{}, map[string]string{"@anthropic-ai/claude-code": "2.1.261"}, `C:\npm`, nil)
	got := firstTool(t, deps)
	if got.Relation != remoteversion.RelationNotInstalled {
		t.Fatalf("relation=%q", got.Relation)
	}
	if got.Local.Status != detect.StatusMissing {
		t.Fatalf("status=%q", got.Local.Status)
	}
}

func TestOverviewRegistryFailureDegrades(t *testing.T) {
	// registry 离线（latest 表空）→ LatestError + unknown，本机状态仍保留。
	deps := fixedDeps(
		map[string]detect.ToolInfo{"claude": {Name: "claude", Version: "2.1.260", Status: detect.StatusInstalled, Path: `C:\npm\claude.cmd`}},
		map[string]string{},
		`C:\npm`, nil,
	)
	got := firstTool(t, deps)
	if got.LatestError == "" || got.Relation != remoteversion.RelationUnknown {
		t.Fatalf("err=%q relation=%q", got.LatestError, got.Relation)
	}
	if got.Local.Version != "2.1.260" {
		t.Fatalf("local lost: %#v", got.Local)
	}
}

func TestOverviewPrefixMismatchDetail(t *testing.T) {
	// PATH 命中 ~/.local/bin 而 npm prefix 是 %AppData%\npm → 触发拷贝不一致提醒。
	deps := fixedDeps(
		map[string]detect.ToolInfo{"claude": {Name: "claude", Version: "2.1.260", Status: detect.StatusInstalled, Path: `C:\Users\me\.local\bin\claude.exe`}},
		map[string]string{"@anthropic-ai/claude-code": "2.1.260"},
		`C:\Users\me\AppData\Roaming\npm`, nil,
	)
	got := firstTool(t, deps)
	if got.Relation != remoteversion.RelationLatest {
		t.Fatalf("relation=%q", got.Relation)
	}
	if !strings.Contains(got.RelationDetail, "npm 全局目录之外") {
		t.Fatalf("detail=%q", got.RelationDetail)
	}
}

func TestOverviewMissingNpmWhenNotInstalled(t *testing.T) {
	deps := fixedDeps(map[string]detect.ToolInfo{}, map[string]string{"@anthropic-ai/claude-code": "2.1.261"}, "", errors.New("未找到 npm"))
	got := firstTool(t, deps)
	if !strings.Contains(got.RelationDetail, "未检测到可用的 npm") {
		t.Fatalf("detail=%q", got.RelationDetail)
	}
}

func TestOverviewActiveOperationPassthrough(t *testing.T) {
	// 无进行中操作时 Overview.ActiveOperation 为 nil。
	overview, err := buildOverview(context.Background(), fixedDeps(map[string]detect.ToolInfo{}, map[string]string{}, `C:\npm`, nil))
	if err != nil {
		t.Fatal(err)
	}
	if overview.ActiveOperation != nil {
		t.Fatalf("expected no active operation, got %#v", overview.ActiveOperation)
	}
}
