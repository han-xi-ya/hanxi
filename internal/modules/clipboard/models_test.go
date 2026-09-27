package clipboard

import (
	"strings"
	"testing"
)

// TestClipAutoTags 契约 §3 标签判定：各规则单独命中、多规则共存按固定序输出、
// 去重与空文本形态。
func TestClipAutoTags(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // join(",")；"" 表示无标签
	}{
		{"url", "see https://example.com/a?b=1 or http://x.cn", "url"},
		{"email", "ping admin@hanxi.dev ok?", "email"},
		{"phone", "call 13812345678 now ok?", "phone"},
		{"color6", "#ff00aa", "color"},
		{"color3", "#ABC", "color"},
		{"color-must-fullmatch", "#ff00aa!", ""},
		{"fence", "```\nabc\ndef\n```", "code"},
		{"keyword-lines", "one line is short\ndef foo():\nthird line", "code"},
		{"two-lines-no-code", "import os\nprint(1)", ""}, // <3 行且无 ```，不算 code
		{"cjk", "你好世界", "cjk"},
		{"combo", "https://ex.com\nuser@ex.org 13800001111\nfunc main() {\n  x := 1;\n}\n你好", "url,email,phone,code,cjk"},
		{"empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(AutoTags(c.text), ",")
			if got != c.want {
				t.Fatalf("AutoTags = %q, want %q", got, c.want)
			}
		})
	}
}

// TestClipLooksSecret 契约 §2 secret 启发式五模式逐一命中 + 普通文本不误报。
func TestClipLooksSecret(t *testing.T) {
	hits := []string{
		"key: sk-" + strings.Repeat("aB3_-", 5) + "x", // sk- 后 ≥16 字符类
		"ghp_" + strings.Repeat("A1b2", 6),            // ghp_ 后 ≥20
		"AKIA" + strings.Repeat("A1", 8),              // AKIA + 16 大写数字
		"xoxb-123-456",
		"-----BEGIN RSA PRIVATE KEY-----",
	}
	for _, s := range hits {
		if !looksSecret(s) {
			t.Errorf("looksSecret(%q) = false, want true", s)
		}
	}
	misses := []string{"普通中文备忘", "ghp_short", "sk-abc", "AKIA123", "xoo-"}
	for _, s := range misses {
		if looksSecret(s) {
			t.Errorf("looksSecret(%q) = true, want false", s)
		}
	}
}

// TestClipDefaultExcludedExes 出厂默认表全量（v1.3.4 硬约束：7 项一个不能少）
// 且访问器返回副本（改副本不污染默认表）。
func TestClipDefaultExcludedExes(t *testing.T) {
	want := []string{"1password.exe", "bitwarden.exe", "keepass.exe", "keepassxc.exe",
		"lastpass.exe", "veracrypt.exe", "truecrypt.exe"}
	got := DefaultExcludedExes()
	if len(got) != len(want) {
		t.Fatalf("默认表应有 %d 项，得 %v", len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("默认表第 %d 项应为 %s，得 %s", i, w, got[i])
		}
	}
	got[0] = "mutated"
	if DefaultExcludedExes()[0] != want[0] {
		t.Fatal("DefaultExcludedExes 必须返回副本，改副本不得影响默认表")
	}
}

// TestClipMatchExcluded 排除匹配三分支：空名不排除、password 子串排除、活表
// 大小写不敏感精确命中；活表增删即时生效（service 层持活表的正确性基础）。
func TestClipMatchExcluded(t *testing.T) {
	table := DefaultExcludedExes()
	if !matchExcluded("1password.exe", table) || !matchExcluded("keepassxc.exe", table) ||
		!matchExcluded("truecrypt.exe", table) {
		t.Fatal("默认排除表未命中")
	}
	if !matchExcluded("1Password.exe", table) || !matchExcluded("KEEPASS.EXE", table) {
		t.Fatal("活表命中应大小写不敏感")
	}
	if !matchExcluded("my-password-tool.exe", table) {
		t.Fatal("password 子串规则未命中")
	}
	if matchExcluded("chrome.exe", table) || matchExcluded("", table) {
		t.Fatal("正常进程/空名被误排除")
	}
	live := append(table, "vault-app.exe")
	if !matchExcluded("vault-app.exe", live) {
		t.Fatal("活表新增项未生效")
	}
	if matchExcluded("vault-app.exe", table) {
		t.Fatal("默认表不应被 append 波及（append 必须产生新切片）")
	}
}

// TestClipHashOf 语义字节口径（契约 §2）：同文同哈希、跨类不同、file 以 \n join、
// image 以 DIB 原始字节。
func TestClipHashOf(t *testing.T) {
	h1 := hashOf(KindText, "abc", nil, nil)
	h2 := hashOf(KindText, "abc", nil, nil)
	if h1 != h2 {
		t.Fatal("同文本哈希不稳定")
	}
	if h1 == hashOf(KindText, "abd", nil, nil) {
		t.Fatal("不同文本哈希相同")
	}
	hJoin := hashOf(KindFile, "", []string{"a", "b"}, nil)
	hRaw := hashOf(KindFile, "", []string{"a\nb"}, nil)
	if hJoin != hRaw {
		t.Fatal("file 语义字节应以 \\n join")
	}
	hImg := hashOf(KindImage, "", nil, []byte{1, 2})
	if hImg == hashOf(KindText, "ab", nil, nil) {
		t.Fatal("image/text 命名空间不应混同（Kind 由 Add 比对兜底，哈希本身也应不同）")
	}
}

// TestClipPreviewAndTruncate Preview 首行语义、120 rune 上限、多字节安全截断。
func TestClipPreviewAndTruncate(t *testing.T) {
	p := previewOf(KindText, "\n\n第三行为首行内容\n第四行", nil, 0, 0)
	if p != "第三行为首行内容" {
		t.Fatalf("preview 应取首个非空行，得 %q", p)
	}
	long := strings.Repeat("汉", previewMaxRunes+50)
	if got := previewOf(KindText, long, nil, 0, 0); len([]rune(got)) != previewMaxRunes {
		t.Fatalf("preview 应截到 %d rune，得 %d", previewMaxRunes, len([]rune(got)))
	}
	if previewOf(KindImage, "", nil, 640, 480) != "图片 640×480" {
		t.Fatal("image preview 形态错误")
	}
	if previewOf(KindFile, "", []string{"D:\\a.txt", "D:\\b.txt"}, 0, 0) != "D:\\a.txt D:\\b.txt" {
		t.Fatal("file preview 应为空格连接的路径串")
	}
	if previewOf(KindText, "   ", nil, 0, 0) != "(空文本)" {
		t.Fatal("全空白文本 preview 占位错误")
	}
}

// TestClipNewEntryID ID 形态：16 hex 且随机不重复。
func TestClipNewEntryID(t *testing.T) {
	seen := map[string]bool{}
	for i := range 200 {
		id, err := newEntryID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 16 {
			t.Fatalf("ID 应为 16 hex: %q (i=%d)", id, i)
		}
		for _, r := range id {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Fatalf("ID 含非 hex 字符: %q", id)
			}
		}
		if seen[id] {
			t.Fatalf("ID 撞车: %q", id)
		}
		seen[id] = true
	}
}

// TestClipCloneEntryWireStrip 事件出口拷贝：Text/BlobData 剥离、Preview 截 200、
// 切片深拷贝不共享底层。
func TestClipCloneEntryWireStrip(t *testing.T) {
	src := Entry{
		Text:     "secret body",
		BlobData: []byte{1, 2, 3},
		Preview:  strings.Repeat("字", 250),
		Files:    []string{"a", "b"},
		AutoTags: []string{"cjk"},
	}
	w := cloneEntry(src, true)
	if w.Text != "" || w.BlobData != nil {
		t.Fatal("事件拷贝应剥 Text/BlobData")
	}
	if len([]rune(w.Preview)) != wirePreviewMax {
		t.Fatalf("事件 Preview 应截 %d rune，得 %d", wirePreviewMax, len([]rune(w.Preview)))
	}
	full := cloneEntry(src, false)
	if len(full.BlobData) != 3 {
		t.Fatal("全量拷贝应保留 BlobData")
	}
	full.Files[0] = "mutated"
	full.AutoTags[0] = "mutated"
	if src.Files[0] != "a" || src.AutoTags[0] != "cjk" {
		t.Fatal("切片应为深拷贝，改副本不应波及原账本")
	}
}
