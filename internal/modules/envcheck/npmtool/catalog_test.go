package npmtool

import "testing"

// TestDefaultVersionPattern 锁定目录默认解析式对两类真实输出的提取。
func TestDefaultVersionPattern(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"claude", "2.1.260 (Claude Code)", "2.1.260"},
		{"codex", "codex-cli 0.153.2", "0.153.2"},
		{"vprefix", "v1.2.3", "1.2.3"},
		{"leading-blank", "\n10.8.3\n", "10.8.3"},
		{"prerelease", "0.1.2-beta.5", "0.1.2-beta.5"},
		{"garbage", "not a version", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := ToolSpec{Command: "x", Package: "x"}
			if got := newDetector(spec).Parse(tt.in); got != tt.want {
				t.Fatalf("Parse(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPackageNameValidation 校验目录包名白名单式，杜绝 shell 语义字符。
func TestPackageNameValidation(t *testing.T) {
	valid := []string{"cowsay", "@openai/codex", "@anthropic-ai/claude-code"}
	invalid := []string{"@evil; rm -rf", "foo bar", "", "../etc", "$PKG"}
	for _, name := range valid {
		if !packageNamePattern.MatchString(name) {
			t.Errorf("expected valid package %q", name)
		}
	}
	for _, name := range invalid {
		if packageNamePattern.MatchString(name) {
			t.Errorf("expected invalid package %q", name)
		}
	}
}

// TestSpecLookup 目录按 ID 精确查找与未知拒绝。
func TestSpecLookup(t *testing.T) {
	if _, ok := Spec("claude"); !ok {
		t.Fatal("claude should be in catalog")
	}
	if _, ok := Spec("codex"); !ok {
		t.Fatal("codex should be in catalog")
	}
	if _, ok := Spec("nope"); ok {
		t.Fatal("unknown id should not resolve")
	}
}
