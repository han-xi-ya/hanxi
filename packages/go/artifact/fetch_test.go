package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/netx"
)

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serve 返回静态内容服务器与摘要。
func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(body); err != nil {
			return
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type progressRec struct {
	stages   []string
	lastDone int64
}

func (p *progressRec) cb(prog Progress) {
	if len(p.stages) == 0 || p.stages[len(p.stages)-1] != prog.Stage {
		p.stages = append(p.stages, prog.Stage)
	}
	if prog.Stage == StageDownload {
		p.lastDone = prog.Done
	}
}

func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.part-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("临时件未清理: %v", matches)
	}
}

func TestFetchNormal(t *testing.T) {
	body := bytes.Repeat([]byte("hanxi-artifact"), 400)
	srv := serve(t, body)
	dir := t.TempDir()
	dest := filepath.Join(dir, "sub", "pkg.zip")

	var rec progressRec
	src := Source{URL: srv.URL + "/pkg.zip", SHA256: strings.ToUpper(shaHex(body)), MaxBytes: 1 << 20, FileName: "pkg.zip"}
	if err := Fetch(context.Background(), src, dest, rec.cb, 30*time.Second); err != nil {
		t.Fatalf("正常下载失败: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("落盘内容与源不符")
	}
	// 阶段序：resolve → download → verify → done；download 报满字节数
	wantStages := []string{StageResolve, StageDownload, StageVerify, StageDone}
	if strings.Join(rec.stages, ",") != strings.Join(wantStages, ",") {
		t.Fatalf("进度阶段 = %v, want %v", rec.stages, wantStages)
	}
	if rec.lastDone != int64(len(body)) {
		t.Fatalf("download 进度字节 = %d, want %d", rec.lastDone, len(body))
	}
	assertNoPartFiles(t, filepath.Dir(dest))
}

func TestFetchFailureMatrix(t *testing.T) {
	body := bytes.Repeat([]byte("payload"), 600)
	goodSHA := shaHex(body)

	t.Run("摘要不符拒收", func(t *testing.T) {
		srv := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: srv.URL, SHA256: strings.Repeat("ab", 32), MaxBytes: 1 << 20}
		err := Fetch(context.Background(), src, dest, nil, 10*time.Second)
		if err == nil || !strings.Contains(err.Error(), "SHA256") {
			t.Fatalf("期望 SHA256 校验失败，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})

	t.Run("断流拒收", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			_, _ = w.Write(body[:30]) // 声明全长却只发一截
		}))
		defer srv.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: srv.URL, SHA256: goodSHA, MaxBytes: 1 << 20}
		err := Fetch(context.Background(), src, dest, nil, 10*time.Second)
		if err == nil || !(strings.Contains(err.Error(), "中断") || strings.Contains(err.Error(), "不完整")) {
			t.Fatalf("期望断流错误，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})

	t.Run("声明超限早拒", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", 1<<30))
			_, _ = w.Write(body[:8])
		}))
		defer srv.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: srv.URL, SHA256: goodSHA, MaxBytes: 1024}
		err := Fetch(context.Background(), src, dest, nil, 10*time.Second)
		if err == nil || !strings.Contains(err.Error(), "声明大小") {
			t.Fatalf("期望声明超限错误，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})

	t.Run("实收超限中拒", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 分块输出（无 Content-Length），持续超过上限
			chunk := bytes.Repeat([]byte("x"), 64*1024)
			for i := 0; i < 64; i++ { // 4 MiB > 128 KiB 上限
				if _, err := w.Write(chunk); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer srv.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: srv.URL, SHA256: goodSHA, MaxBytes: 128 * 1024}
		err := Fetch(context.Background(), src, dest, nil, 10*time.Second)
		if err == nil || !strings.Contains(err.Error(), "超过上限") {
			t.Fatalf("期望实收超限错误，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})

	t.Run("镜像故障转移", func(t *testing.T) {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer bad.Close()
		good := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: bad.URL, Mirrors: []string{good.URL}, SHA256: goodSHA, MaxBytes: 1 << 20}
		if err := Fetch(context.Background(), src, dest, nil, 20*time.Second); err != nil {
			t.Fatalf("镜像回退失败: %v", err)
		}
		assertPresent(t, dest)
	})

	t.Run("同协议重定向可用", func(t *testing.T) {
		target := serve(t, body)
		gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/moved.zip", http.StatusFound)
		}))
		defer gate.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: gate.URL, SHA256: goodSHA, MaxBytes: 1 << 20}
		if err := Fetch(context.Background(), src, dest, nil, 20*time.Second); err != nil {
			t.Fatalf("同协议重定向应成功: %v", err)
		}
		assertPresent(t, dest)
	})

	t.Run("跨协议重定向拒绝", func(t *testing.T) {
		gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.invalid/pkg.zip", http.StatusFound)
		}))
		defer gate.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: gate.URL, SHA256: goodSHA, MaxBytes: 1 << 20}
		err := Fetch(context.Background(), src, dest, nil, 10*time.Second)
		if err == nil || !strings.Contains(err.Error(), "跨协议") {
			t.Fatalf("期望跨协议重定向被拒，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})

	t.Run("超时预算生效", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(2 * time.Second):
				_, _ = w.Write(body)
			case <-r.Context().Done():
			}
		}))
		defer slow.Close()
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		src := Source{URL: slow.URL, SHA256: goodSHA, MaxBytes: 1 << 20}
		start := time.Now()
		err := Fetch(context.Background(), src, dest, nil, 80*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "超时") {
			t.Fatalf("期望超时错误，实际: %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("超时预算未被遵守: %v", time.Since(start))
		}
		assertNoPartFiles(t, dir)
	})

	t.Run("取消即止", func(t *testing.T) {
		srv := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		src := Source{URL: srv.URL, SHA256: goodSHA, MaxBytes: 1 << 20}
		err := Fetch(ctx, src, dest, nil, 10*time.Second)
		if err == nil {
			t.Fatal("取消后应返回错误")
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)
	})
}

func TestFetchConfigErrors(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.zip")
	cases := []struct {
		name      string
		src       Source
		dest      string
		wantError string
	}{
		{"缺摘要", Source{URL: "https://example.invalid/a.zip", MaxBytes: 1024}, dest, "SHA-256"},
		{"摘要形状错", Source{URL: "https://example.invalid/a.zip", SHA256: "not-hex", MaxBytes: 1024}, dest, "十六进制"},
		{"非 http(s) 协议", Source{URL: "ftp://example.invalid/a.zip", SHA256: strings.Repeat("0a", 32), MaxBytes: 1024}, dest, "不支持的协议"},
		{"非回环明文", Source{URL: "http://example.invalid/a.zip", SHA256: strings.Repeat("0a", 32), MaxBytes: 1024}, dest, "HTTPS"},
		{"镜像配置坏", Source{URL: "https://example.invalid/a.zip", Mirrors: []string{"file:///etc/passwd"}, SHA256: strings.Repeat("0a", 32), MaxBytes: 1024}, dest, "镜像地址"},
		{"落地名含分隔", Source{URL: "https://example.invalid/a.zip", SHA256: strings.Repeat("0a", 32), MaxBytes: 1024, FileName: `sub\name.zip`}, dest, "分隔符"},
		{"destPath 为空", Source{URL: "https://example.invalid/a.zip", SHA256: strings.Repeat("0a", 32), MaxBytes: 1024}, "", "destPath"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec progressRec
			err := Fetch(context.Background(), tc.src, tc.dest, rec.cb, 5*time.Second)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("期望错误包含 %q，实际: %v", tc.wantError, err)
			}
			// 错误必须同步进入 error 阶段
			if len(rec.stages) == 0 || rec.stages[len(rec.stages)-1] != StageError {
				t.Fatalf("失败必须以 error 阶段收尾: %v", rec.stages)
			}
			assertMissing(t, dest)
		})
	}
}

// TestFetchLoopbackBypassesProxy N25 G5：锁死 fetchTransport 所装混合闸
// （netx.LoopbackAwareProxyFunc）的回环短路语义——发往本机测试靶的请求
// 永远拿不到代理，用户环境/系统代理不得劫持下载内核的回环候选源。
// 纪律：注入式纯函数断言，不在进程内改环境变量（踩坑 #35：
// http.ProxyFromEnvironment 按进程缓存，测试中改 env 不生效且会泄露语义）。
func TestFetchLoopbackBypassesProxy(t *testing.T) {
	if fetchTransport.Proxy == nil {
		t.Fatal("fetchTransport 必须挂代理解析闸（netx.LoopbackAwareProxyFunc）")
	}
	srv := serve(t, []byte("x"))
	// 回环形态全集：httptest 真靶（127.0.0.1）、localhost、127/8 全段、IPv6 ::1。
	for _, raw := range []string{
		srv.URL + "/pkg.zip",
		"http://localhost:8080/pkg.zip",
		"http://127.0.0.2:9/a.zip",
		"http://[::1]:9/a.zip",
	} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		px, err := fetchTransport.Proxy(req)
		if err != nil {
			t.Fatalf("%s: 闸返回错误: %v", raw, err)
		}
		if px != nil {
			t.Errorf("%s: 回环请求必须恒定直连，实际代理 %v", raw, px)
		}
	}
	// 非回环端点：闸不得短路，必须如实转交代理链（env→系统代理→直连）。
	// 与同一链的独立解析结果比对而非断言具体值——测试机可能没配任何代理，
	// 此时 nil 属"链路尽头直连"的正常回落，锁回环短路语义即可。
	const nonLoop = "https://example.invalid/pkg.zip"
	req, err := http.NewRequest(http.MethodGet, nonLoop, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetchTransport.Proxy(req)
	if err != nil {
		t.Fatalf("%s: 闸返回错误: %v", nonLoop, err)
	}
	want, wantErr := netx.ProxyFunc()(req)
	if (err == nil) != (wantErr == nil) || (got == nil) != (want == nil) ||
		(got != nil && want != nil && got.String() != want.String()) {
		t.Errorf("%s: 非回环请求未经代理链解析：闸=%v，链=%v", nonLoop, got, want)
	}
}

// TestCleanStaleParts 覆盖强杀残件收尸入口：超龄 .part-<hex> 形状文件删除、
// 新件保留、形状外文件保留、目录/符号链接拒删、坏目录静默跳过。
func TestCleanStaleParts(t *testing.T) {
	const shape = "hanxi-demo-1234.zip.pkg.zip.part-abcdef123456" // Fetch 实际落地形状
	stale := time.Now().Add(-48 * time.Hour)

	writeAt := func(path string, age time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, age, age); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	gone := filepath.Join(dir, shape)
	writeAt(gone, stale)
	keepFresh := filepath.Join(dir, "pkg.zip.d.part-abcdef123456") // 形状命中但未超龄：在途下载
	writeAt(keepFresh, time.Now())
	keepShape := filepath.Join(dir, "report.part-1.zip") // 含 .part- 字样但非收尸形状
	writeAt(keepShape, stale)
	keepOther := filepath.Join(dir, "readme.md")
	writeAt(keepOther, stale)
	if err := os.Mkdir(filepath.Join(dir, "dir.part-abcdef123456"), 0755); err != nil { // 目录占名拒删
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "dir.part-abcdef123456"), stale, stale); err != nil {
		t.Fatal(err)
	}

	// 符号链接占名（能建才测；Windows 无符号链接权限时跳过该分支）。
	linkPath := filepath.Join(dir, "link.part-abcdef123456")
	linkTarget := filepath.Join(dir, "link-target.bin")
	writeAt(linkTarget, stale)
	symlinkOK := true
	if err := os.Symlink(linkTarget, linkPath); err != nil {
		symlinkOK = false
		t.Logf("当前环境无法创建符号链接（%v），跳过链接拒删分支", err)
	}

	ghost := filepath.Join(dir, "not-exist-dir")
	removed := CleanStaleParts([]string{dir, ghost, "  "}, 24*time.Hour)
	if len(removed) != 1 || removed[0] != gone {
		t.Fatalf("删除清单应只含超龄形状文件，实际: %v", removed)
	}
	assertMissing(t, gone)
	assertPresent(t, keepFresh)
	assertPresent(t, keepShape)
	assertPresent(t, keepOther)
	if _, err := os.Lstat(filepath.Join(dir, "dir.part-abcdef123456")); err != nil { // 目录（Size 为 0，不适用 assertPresent）
		t.Fatalf("目录占名应保留: %v", err)
	}
	assertPresent(t, linkTarget)
	if symlinkOK {
		if _, err := os.Lstat(linkPath); err != nil {
			t.Fatalf("符号链接拒删却被移除: %v", err)
		}
	}

	// 非正 olderThan 视为配置错误：宁可不删。
	again := filepath.Join(dir, "pkg.zip.x.part-0123456789ab")
	writeAt(again, stale)
	if got := CleanStaleParts([]string{dir}, 0); got != nil {
		t.Fatalf("olderThan<=0 应拒绝删除，实际: %v", got)
	}
	assertPresent(t, again)

	// 正常阈值下该件会被下一轮收走（守卫只是本轮不删）；收干净后重扫无副作用。
	if got := CleanStaleParts([]string{dir}, 24*time.Hour); len(got) != 1 || got[0] != again {
		t.Fatalf("第二轮应删除超龄形状件 %s，实际: %v", again, got)
	}
	if got := CleanStaleParts([]string{dir}, 24*time.Hour); len(got) != 0 {
		t.Fatalf("已收干净的重扫应为空，实际: %v", got)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("文件不应存在: %s", path)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("文件应存在且非空: %s (%v)", path, err)
	}
}
