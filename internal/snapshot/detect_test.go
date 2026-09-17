package snapshot

import (
	"context"
	"errors"
	"testing"
)

func withDetectSeams(t *testing.T, lp func(string) (string, error), rv func(context.Context, string) (string, error)) {
	t.Helper()
	origLP, origRV := lookPath, runGitVersion
	lookPath, runGitVersion = lp, rv
	t.Cleanup(func() { lookPath, runGitVersion = origLP, origRV })
}

func TestDetectGit(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		withDetectSeams(t,
			func(string) (string, error) { return "", errors.New("not found") },
			nil,
		)
		if p := detectGit(); p.Available {
			t.Fatalf("missing git should be unavailable: %+v", p)
		}
	})

	t.Run("installed", func(t *testing.T) {
		withDetectSeams(t,
			func(string) (string, error) { return `C:\Program Files\Git\cmd\git.exe`, nil },
			func(context.Context, string) (string, error) { return "git version 2.46.0.windows.1\n", nil },
		)
		p := detectGit()
		if !p.Available || p.Version != "2.46.0.windows.1" {
			t.Fatalf("unexpected probe: %+v", p)
		}
	})

	t.Run("store-stub", func(t *testing.T) {
		withDetectSeams(t,
			func(string) (string, error) { return `C:\Users\x\AppData\Local\Microsoft\WindowsApps\git.exe`, nil },
			func(context.Context, string) (string, error) { return "", errors.New("exit status 9009") },
		)
		if p := detectGit(); p.Available {
			t.Fatalf("store stub should degrade to unavailable: %+v", p)
		}
	})

	t.Run("unrecognized-output", func(t *testing.T) {
		withDetectSeams(t,
			func(string) (string, error) { return `C:\Tools\git.exe`, nil },
			func(context.Context, string) (string, error) { return "not a git", nil },
		)
		if p := detectGit(); p.Available {
			t.Fatalf("unrecognized output should be unavailable: %+v", p)
		}
	})
}

func TestIsStoreStub(t *testing.T) {
	cases := map[string]bool{
		`C:\Users\x\AppData\Local\Microsoft\WindowsApps\git.exe`: true,
		`C:\WINDOWSAPPS\git.exe`:                                 true,
		`C:\Program Files\Git\cmd\git.exe`:                       false,
		"":                                                       false,
	}
	for in, want := range cases {
		if got := isStoreStub(in); got != want {
			t.Errorf("isStoreStub(%q) = %v, want %v", in, got, want)
		}
	}
}
