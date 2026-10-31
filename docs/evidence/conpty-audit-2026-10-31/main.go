// 只读审计的实证探针（仓外，不碰 hanxi 树）：
// A. windows.StringToUTF16Ptr 遇内嵌 \x00 是否 panic（conpty_windows.go:159 Env 路径）
// B. os.Pipe 阻塞 Read 与并发 Close：返回 error 还是 panic/hang（清单3）
// C. WaitForSingleObject 挂着时 CloseHandle 同句柄：等待会怎样（清单2 watchExit）
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func main() {
	probeA()
	probeB()
	probeC()
}

// ---------- A ----------

func probeA() {
	fmt.Println("== A: StringToUTF16Ptr 与内嵌 NUL ==")
	joined := strings.Join([]string{"A=1", "B=2"}, "\x00") + "\x00\x00"
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("A1 现形（包内 Env 写法,2项）: PANIC: %v\n", r)
			} else {
				fmt.Println("A1 现形（包内 Env 写法,2项）: 未 panic")
			}
		}()
		_ = windows.StringToUTF16Ptr(joined)
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("A2 单条 Env: PANIC: %v\n", r)
			} else {
				fmt.Println("A2 单条 Env: 未 panic")
			}
		}()
		_ = windows.StringToUTF16Ptr("A=1" + "\x00\x00")
	}()
	p := windows.StringToUTF16Ptr("plain")
	fmt.Printf("A3 无 NUL 串正常, first=%d\n", *p)
	// 建议修法的块形状验证：逐项 UTF16FromString（自带头终止 0）+ 块尾再补一个 0
	var block []uint16
	for _, e := range []string{"A=1", "EMPTY=", "NOEQ"} {
		u, err := windows.UTF16FromString(e)
		if err != nil {
			fmt.Println("A4 项编码失败:", e, err)
			continue
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	fmt.Printf("A4 手工块 len=%d 末两位=%d,%d（期望 0,0=双终止成立）\n",
		len(block), block[len(block)-2], block[len(block)-1])
}

// ---------- B ----------

func probeB() {
	fmt.Println("== B: 阻塞 Read 与并发 Close ==")
	const iters = 2000
	var wg sync.WaitGroup
	errKinds := map[string]int{}
	var mu sync.Mutex
	hang := 0
	for i := 0; i < iters; i++ {
		r, w, err := os.Pipe()
		if err != nil {
			fmt.Println("B pipe 失败:", err)
			return
		}
		done := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(done)
			defer func() {
				if rrec := recover(); rrec != nil {
					mu.Lock()
					errKinds[fmt.Sprintf("PANIC:%v", rrec)]++
					mu.Unlock()
				}
			}()
			buf := make([]byte, 4)
			_, rerr := r.Read(buf)
			mu.Lock()
			if rerr == nil {
				errKinds["nil-error(!)"]++
			} else {
				errKinds[rerr.Error()]++
			}
			mu.Unlock()
			w.Close()
		}()
		time.Sleep(200 * time.Microsecond) // 让 Read 先挂住
		_ = r.Close()                      // 与阻塞 Read 并发关
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			mu.Lock()
			hang++
			mu.Unlock()
		}
	}
	wg.Wait()
	fmt.Printf("B %d 轮完成: 卡死=%d 错误分布=%v\n", iters, hang, errKinds)
}

// ---------- C ----------

func probeC() {
	fmt.Println("== C: 挂起的 WaitForSingleObject 遇 CloseHandle ==")
	cmd := exec.Command("cmd.exe", "/c", "ping -n 10 127.0.0.1 >nul")
	if err := cmd.Start(); err != nil {
		fmt.Println("C start 失败:", err)
		return
	}
	pid := uint32(cmd.Process.Pid)
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		fmt.Println("C OpenProcess 失败:", err)
		return
	}
	result := make(chan string, 2)
	go func() { // 模拟 watchExit
		w, werr := windows.WaitForSingleObject(h, windows.INFINITE)
		gle := windows.GetLastError()
		var code uint32
		gerr := windows.GetExitCodeProcess(h, &code)
		result <- fmt.Sprintf("wait=%d(258=WAIT_TIMEOUT,%d=WAIT_FAILED) werr=%v gle=%d getExitCodeErr=%v code=%d",
			w, windows.WAIT_FAILED, werr, gle, gerr, code)
	}()
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("C 子进程活着时 CloseHandle(h=%d)\n", h)
	if err := windows.CloseHandle(h); err != nil {
		fmt.Println("C CloseHandle 报错:", err)
	}
	select {
	case s := <-result:
		fmt.Println("C 等待在子进程未死时就返回:", s)
	case <-time.After(500 * time.Millisecond):
		fmt.Println("C CloseHandle 后 500ms: 等待未返回（仍在挂）")
	}
	ht, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		fmt.Println("C OpenProcess(TERMINATE) 失败:", err)
		return
	}
	_ = windows.TerminateProcess(ht, 42)
	windows.CloseHandle(ht)
	select {
	case s := <-result:
		fmt.Println("C 子进程死亡后等待结果:", s)
	case <-time.After(5 * time.Second):
		fmt.Println("C 子进程死亡后 5s 等待仍未返回 → goroutine 永久悬挂成立")
	}
	_ = cmd.Wait()
}
