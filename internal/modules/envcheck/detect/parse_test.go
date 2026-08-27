package detect

import "testing"

// TestParseVersions 表驱动验证各探测器版本正则对真实输出样本的解析，
// 重点覆盖 Windows 特有格式（java 引号版本、node v 前缀/CRLF、go devel）。
func TestParseVersions(t *testing.T) {
	cases := []struct {
		name string
		det  Detector
		in   string
		want string
	}{
		{"git-windows", gitDetector{}, "git version 2.46.0.windows.1", "2.46.0.windows.1"},
		{"git-macos-suffix", gitDetector{}, "git version 2.39.3 (Apple Git-145)", "2.39.3"},
		{"git-garbage", gitDetector{}, "some garble\nno version here", ""},
		{"node-plain", nodeDetector{}, "v20.15.1\n", "20.15.1"},
		{"node-crlf", nodeDetector{}, "v18.20.4\r\n", "18.20.4"},
		{"node-garbage", nodeDetector{}, "node is not recognized", ""},
		{"java-openjdk", javaDetector{}, `openjdk version "17.0.9" 2023-10-17`, "17.0.9"},
		{"java-1.8", javaDetector{}, `java version "1.8.0_151"`, "1.8.0_151"},
		{"java-full-block", javaDetector{}, "openjdk version \"21.0.2\" 2024-01-16 LTS\nOpenJDK Runtime Environment Temurin-21.0.2+13 (build 21.0.2+13-LTS)", "21.0.2"},
		{"java-garbage", javaDetector{}, "some garble\nno version here", ""},
		{"python-plain", pythonDetector{}, "Python 3.12.4\n", "3.12.4"},
		{"python-crlf", pythonDetector{}, "Python 3.9.13\r\n", "3.9.13"},
		{"python-garbage", pythonDetector{}, "Python was not found", ""},
		{"npm-plain", npmDetector{}, "10.8.3\n", "10.8.3"},
		{"npm-leading-blank", npmDetector{}, "\n6.14.18\n", "6.14.18"},
		{"pnpm-plain", pnpmDetector{}, "9.7.1\n", "9.7.1"},
		{"go-release", goDetector{}, "go version go1.22.5 windows/amd64", "1.22.5"},
		{"go-devel", goDetector{}, "go version devel go1.24-8a0e33a linux/amd64", "1.24"},
		{"go-garbage", goDetector{}, "go version unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.det.Parse(c.in); got != c.want {
				t.Errorf("Parse(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}