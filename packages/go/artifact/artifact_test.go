package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateVersionToken(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"常规语义化版本", "1.2.3", false},
		{"带 v 前缀", "v2.0.1", false},
		{"带预发布后缀", "1.0.0-rc1", false},
		{"带下划线", "1_2_3", false},
		{"首尾空格被容忍", "  1.2.3  ", false},
		{"空令牌", "", true},
		{"纯点", ".", true},
		{"路径逃逸", "../x", true},
		{"正斜杠分隔", "a/b", true},
		{"反斜杠分隔", `a\b`, true},
		{"盘符", "C:x", true},
		{"尾点", "1.2.", true},
		{"尾连字符", "1.2-", true},
		{"连续点", "a..b", true},
		{"超长(65字符)", strings.Repeat("a", 65), true},
		{"恰上限(64字符)", strings.Repeat("a", 64), false},
		{"非 ASCII", "版本1", true},
		{"含空格", "1.2 3", true},
		{"入口名前缀", "hanxi-ocr", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVersionToken(tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateVersionToken(%q) err = %v, wantErr = %v", tc.token, err, tc.wantErr)
			}
		})
	}
}

func TestFileSHA256AndHelpers(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/x.bin"
	content := []byte("hanxi")
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("fileSHA256 = %q", got)
	}
	if _, err := fileSHA256(dir + "/missing"); err == nil {
		t.Fatal("缺失文件应报错")
	}
	if !isSHA256Hex(hex.EncodeToString(sum[:])) || isSHA256Hex("0a") || isSHA256Hex(strings.Repeat("z", 64)) {
		t.Fatal("isSHA256Hex 判定异常")
	}
	if got := sanitizeFilePart(`..\evil name*.zip`); got != "evilname.zip" {
		t.Fatalf("sanitizeFilePart 未清洗危险字符: %q", got)
	}
	if sanitizeFilePart("") != "artifact" {
		t.Fatal("sanitizeFilePart 空输入应回退默认名")
	}
}

func TestMetaUnmarshalCompat(t *testing.T) {
	// 旧账本 "2006-01-02 15:04:05" 与 RFC3339 两种 installedAt 写法都要能吃下
	old := []byte(`{"schema":1,"tool":"x","installedAt":"2026-09-18 08:30:00","zipSHA256":"ab"}`)
	var m Meta
	if err := m.UnmarshalJSON(old); err != nil {
		t.Fatalf("旧格式解析失败: %v", err)
	}
	if m.InstalledAt.IsZero() || m.Schema != 1 {
		t.Fatalf("旧格式字段丢失: %+v", m)
	}
	rfc := []byte(`{"schema":1,"installedAt":"2026-09-18T08:30:00Z"}`)
	var m2 Meta
	if err := m2.UnmarshalJSON(rfc); err != nil {
		t.Fatalf("RFC3339 解析失败: %v", err)
	}
	if !m2.InstalledAt.Equal(time.Date(2026, 9, 18, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("RFC3339 时间不符: %v", m2.InstalledAt)
	}
}
