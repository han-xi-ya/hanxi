package envcheck

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/gitversion"
	"hanxi/internal/modules/envcheck/remoteversion"
)

type fakeOpener struct {
	url string
	err error
}

func (f *fakeOpener) OpenURL(url string) error {
	f.url = url
	return f.err
}

func TestGetGitForWindowsOverview(t *testing.T) {
	tests := []struct {
		name     string
		local    detect.ToolInfo
		releases []gitversion.Release
		want     gitversion.Relation
	}{
		{
			name:     "update available",
			local:    detect.ToolInfo{Version: "2.49.0.windows.1", Status: detect.StatusInstalled},
			releases: []gitversion.Release{{Version: "2.50.0.windows.1"}},
			want:     gitversion.RelationUpdateAvailable,
		},
		{
			name:     "not installed",
			local:    detect.ToolInfo{Status: detect.StatusMissing},
			releases: []gitversion.Release{{Version: "2.50.0.windows.1", Stale: true}},
			want:     gitversion.RelationNotInstalled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEnvCheckService(nil)
			svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) { return tt.local, nil }
			svc.recentReleases = func() ([]gitversion.Release, error) { return tt.releases, nil }
			overview, err := svc.GetGitForWindowsOverview()
			if err != nil {
				t.Fatal(err)
			}
			if overview.Relation != tt.want {
				t.Fatalf("relation = %q, want %q", overview.Relation, tt.want)
			}
			if overview.IsStale != tt.releases[0].Stale {
				t.Fatalf("isStale = %v", overview.IsStale)
			}
		})
	}
}

func TestGetGitForWindowsOverviewRemoteFailureKeepsLocal(t *testing.T) {
	svc := NewEnvCheckService(nil)
	wantLocal := detect.ToolInfo{Name: "git", Version: "2.50.0", Status: detect.StatusInstalled}
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) { return wantLocal, nil }
	svc.recentReleases = func() ([]gitversion.Release, error) { return nil, errors.New("offline") }

	overview, err := svc.GetGitForWindowsOverview()
	if err == nil || overview.Local != wantLocal || overview.Relation != gitversion.RelationUnknown {
		t.Fatalf("overview=%#v err=%v", overview, err)
	}
}

func TestGetGitForWindowsOverviewLocalFailure(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{}, errors.New("unknown detector")
	}
	svc.recentReleases = func() ([]gitversion.Release, error) {
		return []gitversion.Release{{Version: "2.50.0.windows.1"}}, nil
	}
	if _, err := svc.GetGitForWindowsOverview(); err == nil {
		t.Fatal("expected local detection error")
	}
}

func TestOpenGitForWindowsDownloadPage(t *testing.T) {
	opener := &fakeOpener{}
	svc := NewEnvCheckService(opener)
	if err := svc.OpenGitForWindowsDownloadPage(); err != nil {
		t.Fatal(err)
	}
	if opener.url != "https://git-scm.com/download/win" {
		t.Fatalf("url = %q", opener.url)
	}

	opener.err = errors.New("browser unavailable")
	if err := svc.OpenGitForWindowsDownloadPage(); err == nil {
		t.Fatal("expected opener error")
	}
}

func TestGetChannelOverviews(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		local  detect.ToolInfo
		remote []remoteversion.Channel
		setup  func(*EnvCheckService, func() ([]remoteversion.Channel, bool, time.Time, error))
		call   func(*EnvCheckService) ([]remoteversion.Channel, bool, error)
		want   remoteversion.Relation
	}{
		{
			name: "go update", tool: "go",
			local:  detect.ToolInfo{Version: "1.26.8", Status: detect.StatusInstalled},
			remote: []remoteversion.Channel{{Key: "stable", Releases: []remoteversion.Release{{Version: "1.27.1"}}}},
			setup:  func(s *EnvCheckService, f func() ([]remoteversion.Channel, bool, time.Time, error)) { s.goChannels = f },
			call: func(s *EnvCheckService) ([]remoteversion.Channel, bool, error) {
				o, e := s.GetGoOverview()
				return o.Channels, o.IsStale, e
			},
			want: remoteversion.RelationUpdateAvailable,
		},
		{
			name: "node missing", tool: "node",
			local:  detect.ToolInfo{Status: detect.StatusMissing},
			remote: []remoteversion.Channel{{Key: "lts", Releases: []remoteversion.Release{{Version: "24.20.0"}}}, {Key: "current", Releases: []remoteversion.Release{{Version: "26.8.1"}}}},
			setup: func(s *EnvCheckService, f func() ([]remoteversion.Channel, bool, time.Time, error)) {
				s.nodeChannels = f
			},
			call: func(s *EnvCheckService) ([]remoteversion.Channel, bool, error) {
				o, e := s.GetNodeOverview()
				return o.Channels, o.IsStale, e
			},
			want: remoteversion.RelationNotInstalled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEnvCheckService(nil)
			svc.detectOne = func(_ context.Context, name string) (detect.ToolInfo, error) {
				if name != tt.tool {
					t.Fatalf("tool=%q", name)
				}
				return tt.local, nil
			}
			tt.setup(svc, func() ([]remoteversion.Channel, bool, time.Time, error) {
				return tt.remote, true, time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local), nil
			})
			channels, stale, err := tt.call(svc)
			if err != nil || !stale || len(channels) != len(tt.remote) {
				t.Fatalf("channels=%#v stale=%v err=%v", channels, stale, err)
			}
			for _, channel := range channels {
				if channel.Relation != tt.want {
					t.Fatalf("relation=%q want=%q", channel.Relation, tt.want)
				}
			}
		})
	}
}

func TestOpenGoAndNodeDownloadPages(t *testing.T) {
	opener := &fakeOpener{}
	svc := NewEnvCheckService(opener)
	if err := svc.OpenGoDownloadPage(); err != nil || opener.url != "https://go.dev/dl/" {
		t.Fatalf("go url=%q err=%v", opener.url, err)
	}
	if err := svc.OpenNodeDownloadPage(); err != nil || opener.url != "https://nodejs.org/en/download" {
		t.Fatalf("node url=%q err=%v", opener.url, err)
	}
}

func TestGetChannelOverviewPrioritizesLocalLine(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{Version: "1.25.12", Status: detect.StatusInstalled}, nil
	}
	svc.goChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{
			{Key: "stable", Releases: []remoteversion.Release{{Version: "1.26.8"}}},
			{Key: "oldstable", Releases: []remoteversion.Release{{Version: "1.25.13"}}},
		}, false, time.Time{}, nil
	}
	overview, err := svc.GetGoOverview()
	if err != nil {
		t.Fatal(err)
	}
	if got := overview.Channels[0]; got.Key != "oldstable" || got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("first channel=%#v", got)
	}
	if got := overview.Channels[1]; got.Key != "stable" || got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("second channel=%#v", got)
	}
}

func TestGetNodeOverviewPrioritizesLocalLine(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{Version: "v26.1.2", Status: detect.StatusInstalled}, nil
	}
	svc.nodeChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{
			{Key: "lts", Releases: []remoteversion.Release{{Version: "24.10.0"}}},
			{Key: "current", Releases: []remoteversion.Release{{Version: "26.1.3"}}},
		}, false, time.Time{}, nil
	}
	overview, err := svc.GetNodeOverview()
	if err != nil {
		t.Fatal(err)
	}
	if got := overview.Channels[0]; got.Key != "current" || got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("first channel=%#v", got)
	}
	if got := overview.Channels[1]; got.Key != "lts" || got.Relation != remoteversion.RelationAhead {
		t.Fatalf("second channel=%#v", got)
	}
}

func TestGetJavaOverviewPrioritizesFeatureLine(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{
			Version: "25.0.1+8", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{Java: &detect.JavaDetails{Vendor: "Eclipse Temurin"}},
		}, nil
	}
	svc.javaChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{
			{Key: "lts", Releases: []remoteversion.Release{{Version: "21.0.6+7"}}},
			{Key: "feature", Releases: []remoteversion.Release{{Version: "25.0.2+9"}}},
		}, false, time.Time{}, nil
	}
	overview, err := svc.GetJavaOverview()
	if err != nil {
		t.Fatal(err)
	}
	if got := overview.Channels[0]; got.Key != "feature" || got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("first channel=%#v", got)
	}
}

func TestGetJavaOverviewVendorAware(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{
			Version: "21.0.5+11", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{Java: &detect.JavaDetails{Vendor: "Oracle"}},
		}, nil
	}
	svc.javaChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{{Key: "21", Releases: []remoteversion.Release{{Version: "21.0.6+7"}}}}, false, time.Time{}, nil
	}
	overview, err := svc.GetJavaOverview()
	if err != nil {
		t.Fatal(err)
	}
	if got := overview.Channels[0]; got.Relation != remoteversion.RelationUnknown || got.RelationDetail == "" {
		t.Fatalf("channel=%#v", got)
	}
}

func TestGetPythonOverviewChannels(t *testing.T) {
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) {
		return detect.ToolInfo{Version: "3.12.4", Status: detect.StatusInstalled}, nil
	}
	svc.pythonChannels = func(string) ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{
			{Key: "stable", Releases: []remoteversion.Release{{Version: "3.14.1"}}},
			{Key: "python-3.12", Releases: []remoteversion.Release{{Version: "3.12.11"}}},
		}, true, time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local), nil
	}
	overview, err := svc.GetPythonOverview()
	if err != nil {
		t.Fatal(err)
	}
	if !overview.IsStale {
		t.Fatalf("overview=%#v", overview)
	}
	// 本机 3.12.4 所在版本线通道置顶，stable 仍在其后并保留跨线说明。
	if got := overview.Channels[0]; got.Key != "python-3.12" || got.Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("first channel=%#v", got)
	}
	if got := overview.Channels[1]; got.Key != "stable" || got.Relation != remoteversion.RelationUnknown || got.RelationDetail == "" {
		t.Fatalf("stable channel=%#v", got)
	}
}

func dotnetService(t *testing.T, local detect.ToolInfo) *EnvCheckService {
	t.Helper()
	svc := NewEnvCheckService(nil)
	svc.detectOne = func(context.Context, string) (detect.ToolInfo, error) { return local, nil }
	svc.dotnetChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
		return []remoteversion.Channel{
			{Key: "dotnet-10.0", Releases: []remoteversion.Release{{Version: "10.0.1"}}},
			{Key: "dotnet-9.0", Releases: []remoteversion.Release{{Version: "9.0.8"}}},
			{Key: "dotnet-8.0", Releases: []remoteversion.Release{{Version: "8.0.16"}}},
		}, false, time.Time{}, nil
	}
	return svc
}

func TestGetDotNetOverview(t *testing.T) {
	t.Run("runtime only local line prioritized", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{
			Version: "8.0.13", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{DotNet: &detect.DotNetDetails{Runtimes: []string{"8.0.13"}, Desktops: []string{"8.0.13"}}},
		})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if len(overview.Channels) != 2 || overview.Channels[0].Key != "dotnet-8.0" || overview.Channels[1].Key != "dotnet-10.0" {
			t.Fatalf("channels=%v", overview.Channels)
		}
		// 本机 8.0.13 < 8.0.16，且跨线到 10.0.1 也是 update-available（SDK 版本不参与比较）。
		for _, channel := range overview.Channels {
			if channel.Relation != remoteversion.RelationUpdateAvailable || channel.RelationDetail != "" {
				t.Fatalf("channel=%#v", channel)
			}
		}
	})
	t.Run("side by side lines use highest runtime", func(t *testing.T) {
		// 真实升级场景：8.0.13 与 10.0.11 并排共存，比较与置顶均按最高线。
		svc := dotnetService(t, detect.ToolInfo{
			Version: "10.0.400", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{DotNet: &detect.DotNetDetails{
				SDKs:     []string{"10.0.400"},
				Runtimes: []string{"8.0.13", "10.0.11"},
			}},
		})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if len(overview.Channels) != 1 || overview.Channels[0].Key != "dotnet-10.0" {
			t.Fatalf("channels=%v", overview.Channels)
		}
		if got := overview.Channels[0]; got.Relation != remoteversion.RelationAhead || got.RelationDetail != "" {
			t.Fatalf("channel=%#v", got)
		}
	})
	t.Run("sdk version not compared", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{
			Version: "9.0.100", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{DotNet: &detect.DotNetDetails{SDKs: []string{"9.0.100"}, Runtimes: []string{"9.0.8"}}},
		})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if overview.Channels[0].Key != "dotnet-9.0" || overview.Channels[0].Relation != remoteversion.RelationLatest {
			t.Fatalf("first channel=%#v", overview.Channels[0])
		}
	})
	t.Run("missing runtime detail unknown", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{
			Version: "9.0.100", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{DotNet: &detect.DotNetDetails{SDKs: []string{"9.0.100"}}},
		})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if overview.Channels[0].Relation != remoteversion.RelationUnknown || overview.Channels[0].RelationDetail == "" {
			t.Fatalf("channel=%#v", overview.Channels[0])
		}
	})
	t.Run("eol local line detail", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{
			Version: "6.0.36", Status: detect.StatusInstalled,
			Details: &detect.ToolDetails{DotNet: &detect.DotNetDetails{Runtimes: []string{"6.0.36"}}},
		})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if len(overview.Channels) != 1 || overview.Channels[0].Key != "dotnet-10.0" {
			t.Fatalf("channels=%v", overview.Channels)
		}
		if !strings.Contains(overview.Channels[0].RelationDetail, "超出官方支持范围") {
			t.Fatalf("detail=%q", overview.Channels[0].RelationDetail)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{Status: detect.StatusMissing})
		overview, err := svc.GetDotNetOverview()
		if err != nil {
			t.Fatal(err)
		}
		if overview.Channels[0].Relation != remoteversion.RelationNotInstalled {
			t.Fatalf("channel=%#v", overview.Channels[0])
		}
	})
	t.Run("remote failure keeps local", func(t *testing.T) {
		svc := dotnetService(t, detect.ToolInfo{Version: "8.0.13", Status: detect.StatusInstalled})
		svc.dotnetChannels = func() ([]remoteversion.Channel, bool, time.Time, error) {
			return nil, false, time.Time{}, errors.New("offline")
		}
		overview, err := svc.GetDotNetOverview()
		if err == nil || overview.Local.Version != "8.0.13" || len(overview.Channels) != 0 {
			t.Fatalf("overview=%#v err=%v", overview, err)
		}
	})
}

func TestOpenDotNetDownloadPage(t *testing.T) {
	opener := &fakeOpener{}
	svc := NewEnvCheckService(opener)
	if err := svc.OpenDotNetDownloadPage(); err != nil || opener.url != "https://dotnet.microsoft.com/download/dotnet" {
		t.Fatalf("url=%q err=%v", opener.url, err)
	}
}

func TestOpenJavaAndPythonDownloadPages(t *testing.T) {
	opener := &fakeOpener{}
	svc := NewEnvCheckService(opener)
	if err := svc.OpenJavaDownloadPage(); err != nil || opener.url != "https://adoptium.net/temurin/releases/" {
		t.Fatalf("java url=%q err=%v", opener.url, err)
	}
	if err := svc.OpenPythonDownloadPage(); err != nil || opener.url != "https://www.python.org/downloads/windows/" {
		t.Fatalf("python url=%q err=%v", opener.url, err)
	}
}
