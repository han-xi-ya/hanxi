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
