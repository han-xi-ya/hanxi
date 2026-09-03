package detect

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// withSeams 替换 lookPath / runVersionOutput 两个包级 seam，测试结束自动还原。
func withSeams(t *testing.T, lp func(string) (string, error), run func(context.Context, string, []string) (string, error)) {
	t.Helper()
	oldLP, oldRun := lookPath, runVersionOutput
	lookPath, runVersionOutput = lp, run
	t.Cleanup(func() {
		lookPath, runVersionOutput = oldLP, oldRun
	})
}

// TestDetectOne 覆盖探测流程 5 条分支：missing / store-stub / error(run) / installed / error(parse)。
func TestDetectOne(t *testing.T) {
	t.Run("lookpath-missing", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "", errors.New("not found") },
			nil,
		)
		info := DetectOne(context.Background(), gitDetector{})
		if info.Status != StatusMissing || info.Path != "" || info.Version != "" || info.Hint == "" {
			t.Fatalf("unexpected missing result: %+v", info)
		}
	})

	t.Run("grpc-go-missing-hint", func(t *testing.T) {
		withSeams(t,
			func(string) (string, error) { return "", errors.New("not found") },
			nil,
		)
		info := DetectOne(context.Background(), goGRPCDetector{})
		if info.Status != StatusMissing || !strings.Contains(info.Hint, "go install") || !strings.Contains(info.Hint, "go.mod") {
			t.Fatalf("unexpected grpc-go missing result: %+v", info)
		}
	})

	t.Run("store-stub", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) {
				return `C:\Users\x\AppData\Local\Microsoft\WindowsApps\python.exe`, nil
			},
			func(context.Context, string, []string) (string, error) {
				return "", errors.New("exit status 9009")
			},
		)
		info := DetectOne(context.Background(), pythonDetector{})
		if info.Status != StatusStoreStub || !strings.Contains(info.Hint, "Store") {
			t.Fatalf("unexpected store-stub result: %+v", info)
		}
	})

	t.Run("run-error", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) { return `C:\Program Files\Git\cmd\git.exe`, nil },
			func(context.Context, string, []string) (string, error) {
				return "", errors.New("boom")
			},
		)
		info := DetectOne(context.Background(), gitDetector{})
		if info.Status != StatusError || !strings.Contains(info.Hint, "boom") {
			t.Fatalf("unexpected run-error result: %+v", info)
		}
	})

	t.Run("installed", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) { return `C:\Program Files\nodejs\node.exe`, nil },
			func(context.Context, string, []string) (string, error) { return "v20.15.1\n", nil },
		)
		info := DetectOne(context.Background(), nodeDetector{})
		if info.Status != StatusInstalled || info.Version != "20.15.1" || info.Hint != "" {
			t.Fatalf("unexpected installed result: %+v", info)
		}
	})

	t.Run("java-structured-details", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) { return `C:\Java\bin\java.exe`, nil },
			func(context.Context, string, []string) (string, error) {
				return "openjdk version \"21.0.2\" 2024-01-16 LTS\nOpenJDK Runtime Environment Temurin-21.0.2+13 (build 21.0.2+13-LTS)\nOpenJDK 64-Bit Server VM Temurin-21.0.2+13 (build 21.0.2+13-LTS, mixed mode)", nil
			},
		)
		info := DetectOne(context.Background(), javaDetector{})
		if info.Status != StatusInstalled || info.Details == nil || info.Details.Java == nil || info.Details.Java.Vendor != "Eclipse Temurin" {
			t.Fatalf("unexpected Java details: %+v", info)
		}
	})

	t.Run("parse-fail", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) { return `C:\Program Files\nodejs\node.exe`, nil },
			func(context.Context, string, []string) (string, error) { return "totally garble\n", nil },
		)
		info := DetectOne(context.Background(), nodeDetector{})
		if info.Status != StatusError || !strings.Contains(info.Hint, "无法识别") {
			t.Fatalf("unexpected parse-fail result: %+v", info)
		}
	})

	t.Run("empty-output-not-stub", func(t *testing.T) {
		withSeams(t,
			func(name string) (string, error) { return `C:\Python312\python.exe`, nil },
			func(context.Context, string, []string) (string, error) { return "", nil },
		)
		info := DetectOne(context.Background(), pythonDetector{})
		if info.Status != StatusError || !strings.Contains(info.Hint, "无输出") {
			t.Fatalf("unexpected empty-output result: %+v", info)
		}
	})
}

// TestIsStoreStub Python 商店存根路径判定（大小写不敏感）。
func TestIsStoreStub(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`C:\Users\x\AppData\Local\Microsoft\WindowsApps\python.exe`, true},
		{`C:\USERS\X\APPDATA\LOCAL\MICROSOFT\WINDOWSAPPS\PYTHON.EXE`, true},
		{`C:\Python312\python.exe`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := (pythonDetector{}).IsStoreStub(c.in); got != c.want {
			t.Errorf("IsStoreStub(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestRegistry 注册表完整性：8 个探测器、Name 无重复、按字典序排序。
func TestRegistry(t *testing.T) {
	ds := Detectors()
	if len(ds) != 8 {
		t.Fatalf("Detectors() len = %d, want 8", len(ds))
	}
	want := []string{"git", "go", "java", "node", "npm", "pnpm", "protoc-gen-go-grpc", "python"}
	for i, d := range ds {
		if d.Name() != want[i] {
			t.Errorf("Detectors()[%d] = %s, want %s", i, d.Name(), want[i])
		}
	}
}

// TestRunAll 全量并发探测：结果数与注册表一致、顺序稳定、结果完整。
func TestRunAll(t *testing.T) {
	withSeams(t,
		func(name string) (string, error) { return `C:\fake\` + name + ".exe", nil },
		func(_ context.Context, exe string, _ []string) (string, error) {
			switch exe {
			case `C:\fake\node.exe`:
				return "v20.15.1\n", nil
			default:
				return "", errors.New("not supported in test")
			}
		},
	)
	results := RunAll(context.Background())
	if len(results) != 8 {
		t.Fatalf("RunAll len = %d, want 8", len(results))
	}
	byName := map[string]ToolInfo{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if v := byName["node"]; v.Status != StatusInstalled || v.Version != "20.15.1" {
		t.Errorf("node result unexpected: %+v", v)
	}
	if v := byName["git"]; v.Status != StatusError {
		t.Errorf("git result unexpected: %+v", v)
	}
	if results[0].Name != "node" || results[0].Status != StatusInstalled {
		t.Errorf("installed tool should sort first, got: %+v", results[0])
	}
}

func TestRunAllStatusOrder(t *testing.T) {
	withSeams(t,
		func(name string) (string, error) {
			switch name {
			case "git", "node":
				return `C:\fake\` + name + ".exe", nil
			default:
				return "", errors.New("not found")
			}
		},
		func(_ context.Context, exe string, _ []string) (string, error) {
			switch exe {
			case `C:\fake\node.exe`:
				return "v20.15.1\n", nil
			case `C:\fake\git.exe`:
				return "", errors.New("boom")
			default:
				return "", errors.New("unexpected executable")
			}
		},
	)

	results := RunAll(context.Background())
	seenNonInstalled := false
	seenMissing := false
	for _, result := range results {
		if result.Status != StatusInstalled {
			seenNonInstalled = true
		} else if seenNonInstalled {
			t.Fatalf("installed result appeared after unavailable result: %+v", results)
		}
		if result.Status == StatusMissing {
			seenMissing = true
		} else if seenMissing {
			t.Fatalf("non-missing result appeared after missing result: %+v", results)
		}
	}
}

// TestRunOne 按名探测与未知名报错。
func TestRunOne(t *testing.T) {
	withSeams(t,
		func(name string) (string, error) { return `C:\fake\node.exe`, nil },
		func(context.Context, string, []string) (string, error) { return "v20.15.1\n", nil },
	)
	info, err := RunOne(context.Background(), "node")
	if err != nil || info.Status != StatusInstalled {
		t.Fatalf("RunOne(node) = %+v, err %v", info, err)
	}
	if _, err := RunOne(context.Background(), "not-a-tool"); err == nil {
		t.Fatal("RunOne(unknown) should error")
	}
}
