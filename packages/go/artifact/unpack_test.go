package artifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipEnt 测试语料条目：名字以 "/" 结尾视为目录条目；mode 零值取 0644。
type zipEnt struct {
	name string
	body string
	mode fs.FileMode
}

func buildZip(t *testing.T, dir, name string, ents ...zipEnt) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range ents {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if strings.HasSuffix(e.name, "/") {
			hdr.SetMode(fs.ModeDir | 0755)
			if _, err := zw.CreateHeader(hdr); err != nil {
				t.Fatal(err)
			}
			continue
		}
		mode := e.mode
		if mode == 0 {
			mode = 0644
		}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustSHA(t *testing.T, s string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestUnpackZipContextCancelBeforeWork(t *testing.T) {
	src := t.TempDir()
	zipPath := buildZip(t, src, "cancel.zip", zipEnt{name: "a.txt", body: "payload"})
	target := filepath.Join(t.TempDir(), "stage")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := UnpackZipContext(ctx, zipPath, target, DefaultLimits, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，实际: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("预先取消不应创建 staging 目录，stat err=%v", err)
	}
}
func TestUnpackZipNormal(t *testing.T) {
	src := t.TempDir()
	zipPath := buildZip(t, src, "ok.zip",
		zipEnt{name: "sub/", body: ""},
		zipEnt{name: "a.txt", body: "AAA"},
		zipEnt{name: "sub/b.txt", body: "BBBB"},
	)
	target := filepath.Join(t.TempDir(), "fresh", "empty") // 不存在 → 自动新建
	if err := UnpackZip(zipPath, target, DefaultLimits, nil); err != nil {
		t.Fatalf("正常解包失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "sub", "b.txt"))
	if err != nil || string(got) != "BBBB" {
		t.Fatalf("解包内容异常: %q %v", got, err)
	}
}

func TestUnpackZipRejectsMaliciousEntries(t *testing.T) {
	bomb := strings.Repeat("\x00", 2<<20) // 2 MiB 全零：deflate 后压缩比轻松破百
	cases := []struct {
		name      string
		ents      []zipEnt
		lim       Limits
		allow     map[string]string
		wantError string
	}{
		{"ZipSlip 上跳", []zipEnt{{name: "../evil.txt", body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"ZipSlip 藏跳", []zipEnt{{name: "a/../../evil.txt", body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"绝对路径", []zipEnt{{name: "/etc/passwd", body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"盘符路径", []zipEnt{{name: "C:/Windows/evil.dll", body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"UNC 路径", []zipEnt{{name: `\\server\share\evil`, body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"反斜杠逃逸", []zipEnt{{name: `sub\..\..\evil`, body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"连续分隔符", []zipEnt{{name: "a//b.txt", body: "x"}}, DefaultLimits, nil, "非法路径"},
		{"保留名 consumer", []zipEnt{{name: "CON.txt", body: "x"}}, DefaultLimits, nil, "保留设备名"},
		{"保留名目录内", []zipEnt{{name: "dir/NUL/data", body: ""}, {name: "dir/NUL/x", body: "x"}}, DefaultLimits, nil, "保留设备名"},
		{"尾点组件", []zipEnt{{name: "evil.", body: "x"}}, DefaultLimits, nil, "点结尾"},
		{"尾空格组件", []zipEnt{{name: "badname ", body: "x"}}, DefaultLimits, nil, "空格结尾"},
		{"符号链接条目", []zipEnt{{name: "link", body: "../../etc/passwd", mode: fs.ModeSymlink | 0777}}, DefaultLimits, nil, "符号链接"},
		{"大小写重复", []zipEnt{{name: "A.txt", body: "1"}, {name: "a.txt", body: "2"}}, DefaultLimits, nil, "大小写冲突"},
		{"文件占目录位", []zipEnt{{name: "a", body: "1"}, {name: "a/b.txt", body: "2"}}, DefaultLimits, nil, "冲突"},
		{"压缩比炸弹", []zipEnt{{name: "bomb.bin", body: bomb}}, Limits{MaxRatio: 5}, nil, "压缩比"},
		{"单文件超限", []zipEnt{{name: "big.txt", body: strings.Repeat("y", 100)}}, Limits{MaxFileBytes: 16}, nil, "单文件上限"},
		{"总展开超限", []zipEnt{{name: "a.txt", body: strings.Repeat("y", 100)}}, Limits{MaxTotalBytes: 16}, nil, "总"},
		{"条目数超限", []zipEnt{{name: "a.txt", body: "1"}, {name: "b.txt", body: "2"}}, Limits{MaxEntries: 1}, nil, "条目数"},
		{"清单外文件", []zipEnt{{name: "a.txt", body: "AAA"}, {name: "extra.txt", body: "x"}}, DefaultLimits,
			map[string]string{"a.txt": mustSHA(t, "AAA")}, "清单外"},
		{"清单点名缺失", []zipEnt{{name: "a.txt", body: "AAA"}}, DefaultLimits,
			map[string]string{"a.txt": "", "missing.txt": ""}, "缺失"},
		{"逐文件摘要不符", []zipEnt{{name: "a.txt", body: "AAA"}}, DefaultLimits,
			map[string]string{"a.txt": mustSHA(t, "TAMPERED")}, "SHA256"},
		{"空清单拒绝一切", []zipEnt{{name: "a.txt", body: "AAA"}}, DefaultLimits, map[string]string{}, "清单外"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			zipPath := buildZip(t, src, "test.zip", tc.ents...)
			target := filepath.Join(t.TempDir(), "out")
			err := UnpackZip(zipPath, target, tc.lim, tc.allow)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("期望错误包含 %q，实际: %v", tc.wantError, err)
			}
		})
	}
}

func TestUnpackZipManifestValid(t *testing.T) {
	src := t.TempDir()
	zipPath := buildZip(t, src, "ok.zip",
		zipEnt{name: "sub/", body: ""},
		zipEnt{name: "a.txt", body: "AAA"},
		zipEnt{name: "sub/b.txt", body: "BBBB"},
	)
	target := filepath.Join(t.TempDir(), "out")
	allow := map[string]string{
		"a.txt":     mustSHA(t, "AAA"),
		"SUB/b.txt": mustSHA(t, "BBBB"), // 大小写宽容：清单键与包内条目大小写不敏感匹配
	}
	if err := UnpackZip(zipPath, target, DefaultLimits, allow); err != nil {
		t.Fatalf("合法清单解包失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "sub", "b.txt")); err != nil {
		t.Fatalf("清单文件未解出: %v", err)
	}
}

func TestUnpackZipTargetGate(t *testing.T) {
	src := t.TempDir()
	zipPath := buildZip(t, src, "ok.zip", zipEnt{name: "a.txt", body: "A"})

	t.Run("非空目录拒绝", func(t *testing.T) {
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "junk"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		err := UnpackZip(zipPath, target, DefaultLimits, nil)
		if err == nil || !strings.Contains(err.Error(), "必须为空") {
			t.Fatalf("期望空目录闸门触发: %v", err)
		}
	})

	t.Run("文件占位拒绝", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "as-file")
		if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		err := UnpackZip(zipPath, target, DefaultLimits, nil)
		if err == nil || !strings.Contains(err.Error(), "不是普通目录") {
			t.Fatalf("期望目录类型闸门触发: %v", err)
		}
	})
}

func TestUnpackZipNoOverwrite(t *testing.T) {
	// staging 内独占创建：预先塞同名文件（非空目录闸门只挡根层，
	// 深层嵌套的冲突由 O_EXCL 兜住——构造"目录非空检测通过但写入冲突"不可行，
	// 故直接验证深层 O_EXCL：先解一次到新建目录，再解到同目录必因非空闸门拒绝，
	// 证明不存在覆盖路径。
	src := t.TempDir()
	zipPath := buildZip(t, src, "ok.zip", zipEnt{name: "deep/a.txt", body: "A"})
	target := filepath.Join(t.TempDir(), "out")
	if err := UnpackZip(zipPath, target, DefaultLimits, nil); err != nil {
		t.Fatal(err)
	}
	err := UnpackZip(zipPath, target, DefaultLimits, nil)
	if err == nil || !strings.Contains(err.Error(), "必须为空") {
		t.Fatalf("二次解包必须被空目录闸门拒绝（不存在覆盖语义）: %v", err)
	}
}

// TestUnpackZipBackslashDirEntries 回归:部分打包器(实测 QuickLook 官方包)把目录
// 条目写成反斜杠结尾且不带 ModeDir 位。旧判定只认 f.FileInfo().IsDir(),这类条目
// 会误落成 0 字节同名文件,随后其子文件触发"祖先被文件占位"整包拒收。修复后按目录处理。
func TestUnpackZipBackslashDirEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// "Plugins\" 目录条目:名字反斜杠结尾,mode 刻意为普通 0644(不带 ModeDir)。
	hdr := &zip.FileHeader{Name: "Plugins\\", Method: zip.Store}
	hdr.SetMode(0644)
	if _, err := zw.CreateHeader(hdr); err != nil {
		t.Fatal(err)
	}
	// 其下真实文件:旧代码会因 "Plugins" 已被 0 字节文件占位而整包拒收。
	fh := &zip.FileHeader{Name: "Plugins/core.dll", Method: zip.Deflate}
	fh.SetMode(0644)
	w, err := zw.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "payload"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "out")
	if err := UnpackZip(zipPath, target, DefaultLimits, nil); err != nil {
		t.Fatalf("反斜杠目录条目包应可解: %v", err)
	}
	if st, err := os.Stat(filepath.Join(target, "Plugins")); err != nil || !st.IsDir() {
		t.Fatalf("Plugins 应为目录: err=%v", err)
	}
	body, err := os.ReadFile(filepath.Join(target, "Plugins", "core.dll"))
	if err != nil || string(body) != "payload" {
		t.Fatalf("子文件落盘异常: %v %q", err, body)
	}
}
