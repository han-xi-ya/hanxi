//go:build windows

package conpty

// 真机夹具（默认 skip，HANXI_CONPTY_LIVE=1 才跑）。断言取**外部事实**：
// 让子进程自己现生成一个命令行里不存在的 GUID——输出里能 grep 到它，才证明
// "字节真的从伪控制台流回了宿主"，而不是输入回显骗过断言
// （"断言抄实现等于没测"的教训：回显会让命令行本身出现在输出里，喂假绿）。

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestLiveConPTYRoundTrip(t *testing.T) {
	if os.Getenv("HANXI_CONPTY_LIVE") == "" {
		t.Skip("未设 HANXI_CONPTY_LIVE=1：跳过 ConPTY 真机往返")
	}
	p, err := Spawn("powershell.exe",
		[]string{"-NoProfile", "-NonInteractive", "-Command", "[guid]::NewGuid().ToString()"},
		Options{Cols: 100, Rows: 30, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	guid := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	frames := make(chan []byte, 64)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				frames <- cp
			}
			if err != nil {
				close(frames)
				return
			}
		}
	}()

	deadline := time.After(30 * time.Second)
	var acc bytes.Buffer
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("输出流提前 EOF 且未见 GUID；已收 %d 字节：%q", acc.Len(), acc.String())
			}
			acc.Write(f)
			if g := guid.Find(acc.Bytes()); g != nil {
				// 外部事实成立：子进程真的在伪控制台里跑了起来并流回了输出。
				if err := p.Resize(120, 40); err != nil {
					t.Fatalf("Resize: %v", err)
				}
				if err := p.Close(); err != nil {
					t.Logf("Close 报（多为自然退场竞态，记录不判死）: %v", err)
				}
				select {
				case <-p.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("Close 后主进程未退场——收口链破了")
				}
				t.Logf("往返 %d 字节，GUID=%s", acc.Len(), g)
				return
			}
		case <-deadline:
			t.Fatalf("30s 未见 GUID 输出；样本 %q", fmt.Sprint(acc.String()))
		}
	}
}

// TestLiveConPTYEnvBlock 审计 P0 的回归钉：旧写法（StringToUTF16Ptr 拼接
// 含 NUL 串）对任何非 nil Env 100% panic，且 Env 恰是探针没走过的侧巷。
// 本用例传真实 Env 并断言子进程**读得到值**——panic 修复与块格式正确
// 一次钉死（外部事实断言：值由子进程在伪控制台里回显，不是本地拼出来的）。
func TestLiveConPTYEnvBlock(t *testing.T) {
	if os.Getenv("HANXI_CONPTY_LIVE") == "" {
		t.Skip("未设 HANXI_CONPTY_LIVE=1：跳过 ConPTY 环境块真机用例")
	}
	marker := fmt.Sprintf("HX-%d", time.Now().UnixNano())
	p, err := Spawn("powershell.exe",
		[]string{"-NoProfile", "-NonInteractive", "-Command", "$env:HANXI_CONPTY_PROBE"},
		conptyOptionsWithEnv(t, marker))
	if err != nil {
		t.Fatalf("带 Env 的 Spawn 失败（panic 修复破了？）: %v", err)
	}
	defer p.Close()

	frames := make(chan []byte, 64)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				frames <- cp
			}
			if err != nil {
				close(frames)
				return
			}
		}
	}()
	re := regexp.MustCompile(regexp.QuoteMeta(marker))
	var acc bytes.Buffer
	deadline := time.After(30 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("流结束仍未见环境值回显；样本 %q", acc.String())
			}
			acc.Write(f)
			if re.Match(acc.Bytes()) {
				return
			}
		case <-deadline:
			t.Fatalf("30s 未见环境值；样本 %q", acc.String())
		}
	}
}

func conptyOptionsWithEnv(t *testing.T, marker string) Options {
	t.Helper()
	// 完整环境 = 现进程环境 + 探针项（Env 是"完整供给"语义，不能只给一条）。
	base := os.Environ()
	return Options{
		Cols: 100, Rows: 30, Dir: t.TempDir(),
		Env: append(base, "HANXI_CONPTY_PROBE="+marker),
	}
}
