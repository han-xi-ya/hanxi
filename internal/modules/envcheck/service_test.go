package envcheck

import (
	"context"
	"errors"
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
	if !overview.IsStale || overview.Channels[0].Relation != remoteversion.RelationUnknown || overview.Channels[0].RelationDetail == "" {
		t.Fatalf("overview=%#v", overview)
	}
	if overview.Channels[1].Relation != remoteversion.RelationUpdateAvailable {
		t.Fatalf("minor relation=%q", overview.Channels[1].Relation)
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
