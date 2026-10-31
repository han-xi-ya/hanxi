// 一次性探测程序（scratch，不入库）：验证 claude CLI 常驻双向 stream-json 协议。
// 一切日志只写 C:\tmp\aichat-probe\logs\，子进程 cwd 固定在 work\ 空目录，绝不落在 hanxi 仓库里。
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	mode  = flag.String("mode", "session", "session|resume")
	sidF  = flag.String("sid", "", "session id for resume mode")
	shape   = flag.String("shape", "A", "user-frame shape: A|B|C")
	tag     = flag.String("tag", "", "log filename suffix")
	skipF1  = flag.Bool("skipf1", false, "fields 模式跳过垃圾行测试（它会杀掉进程）")
)

const (
	workDir  = `C:\tmp\aichat-probe\work`
	logsDir  = `C:\tmp\aichat-probe\logs`
	truncLen = 300
)

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// userFrameBytes 三种候选形状（E6 限定总尝试 ≤3 种）。
func userFrameBytes(text, uid string) []byte {
	content := any([]any{map[string]any{"type": "text", "text": text}})
	switch strings.ToUpper(*shape) {
	case "B":
		b, _ := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": content,
			},
			"uuid":               uid,
			"parent_tool_use_id": nil,
		})
		return b
	case "C":
		b, _ := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": text, // content 直接是字符串
			},
			"uuid": uid,
		})
		return b
	default: // A：任务给的参考形状
		b, _ := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": content,
			},
			"uuid": uid,
		})
		return b
	}
}

type frame struct {
	at   time.Duration
	line string
}

type probe struct {
	cmd    *exec.Cmd
	bin    string
	args   []string
	stdin  io.WriteCloser
	frames chan frame
	exited chan error
	start  time.Time

	fmu  sync.Mutex
	fl   *os.File // 原始 stdout 帧全量
	sfl  *os.File // 原始 stderr 全量
	sew  sync.Mutex
	seLen int
	facc []byte // stdout 行缓冲
}

func baseArgs(sid string, resume bool) []string {
	a := []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "default",
		"--disallowedTools", "Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch",
	}
	if resume {
		a = append(a, "--resume", sid)
	} else {
		a = append(a, "--session-id", sid)
	}
	return a
}

func trunc(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) <= truncLen {
		return s
	}
	return s[:truncLen] + fmt.Sprintf("…[截断,全长%d]", len(s))
}

func (p *probe) logLine(name, line string) {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	off := time.Since(p.start)
	fmt.Fprintf(p.fl, "[%8.3fs][%s] %s\n", off.Seconds(), name, line)
}

// Write 让 exec 内部 io.Copy 直接喂给我们；Wait 会等 copy 结束且不强关管道，避免读丢。
func (p *probe) Write(stdout []byte) (int, error) {
	p.facc = append(p.facc, stdout...)
	for {
		i := bytes.IndexByte(p.facc, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(p.facc[:i]), "\r")
		p.facc = p.facc[i+1:]
		if line == "" {
			continue
		}
		p.logLine("frame", line)
		consoletu := fmt.Sprintf("[%6.1fs] FRAME %s", time.Since(p.start).Seconds(), trunc(line))
		fmt.Println(consoletu)
		select {
		case p.frames <- frame{at: time.Since(p.start), line: line}:
		default:
			fmt.Println(">>> frame channel overflow, dropped above")
		}
	}
	return len(stdout), nil
}

type stderrWriter struct{ p *probe }

func (w stderrWriter) Write(b []byte) (int, error) {
	w.p.sew.Lock()
	w.p.seLen += len(b)
	w.p.sew.Unlock()
	_, _ = w.p.sfl.Write(b)
	return len(b), nil
}

func launch(sid string, resume bool) (*probe, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("LookPath(claude): %w", err)
	}
	_ = os.MkdirAll(workDir, 0o755)
	args := baseArgs(sid, resume)
	fmt.Printf(">>> argv: %s %s\n", bin, strings.Join(args, " "))

	fl, err := os.Create(logsDir + "\\frames-" + *mode + "-" + *tag + ".log")
	if err != nil {
		return nil, err
	}
	sfl, err := os.Create(logsDir + "\\stderr-" + *mode + "-" + *tag + ".log")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p := &probe{
		cmd: cmd, bin: bin, args: args, stdin: stdin,
		frames: make(chan frame, 2048),
		exited: make(chan error, 1),
		start:  time.Now(), fl: fl, sfl: sfl,
	}
	cmd.Stdout = p
	cmd.Stderr = stderrWriter{p}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	fmt.Printf(">>> child pid=%d cwd=%s\n", cmd.Process.Pid, workDir)
	go func() {
		err := cmd.Wait() // Stdout/Stderr 为 writer：Wait 等 copy 完再返回
		// 冲刷 stdout 残行（子进程最后一行可能没有换行）
		if rem := strings.TrimRight(string(p.facc), "\r\n"); rem != "" {
			p.facc = nil
			p.logLine("frame", rem)
			fmt.Printf("[%6.1fs] FRAME(final,no-EOL) %s\n", time.Since(p.start).Seconds(), trunc(rem))
			select {
			case p.frames <- frame{at: time.Since(p.start), line: rem}:
			default:
			}
		}
		close(p.frames)
		p.exited <- err
	}()
	return p, nil
}

func (p *probe) killTree() {
	pid := p.cmd.Process.Pid
	tk := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/T", "/F")
	tk.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, _ := tk.CombinedOutput()
	fmt.Printf(">>> taskkill(pid %d tree): %s\n", pid, strings.TrimSpace(string(out)))
}

func (p *probe) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

func (p *probe) waitFrame(d time.Duration) (frame, bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case f, ok := <-p.frames:
		return f, ok
	case <-t.C:
		return frame{}, false
	}
}

// frameType 只取 type/subtype，解析失败返回空。
func frameType(line string) (typ, subtype string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return "", ""
	}
	typ, _ = m["type"].(string)
	subtype, _ = m["subtype"].(string)
	return
}

func summarizeResult(line string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return "(result 帧 JSON 解析失败)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "result帧字段: %s\n", strings.Join(keys, ","))
	for _, k := range []string{"is_error", "duration_ms", "duration_api_ms", "num_turns", "total_cost_usd", "session_id", "subtype"} {
		if v, ok := m[k]; ok {
			fmt.Fprintf(&b, "  %s=%v\n", k, v)
		}
	}
	if u, ok := m["usage"].(map[string]any); ok {
		uk := make([]string, 0, len(u))
		for k := range u {
			uk = append(uk, fmt.Sprintf("%s=%v", k, u[k]))
		}
		fmt.Fprintf(&b, "  usage: %s\n", strings.Join(uk, " "))
	}
	if r, ok := m["result"].(string); ok {
		fmt.Fprintf(&b, "  result(截200): %s\n", trunc2(r, 200))
	}
	return b.String()
}

func trunc2(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sendUser 写一行 JSON+\n。
func (p *probe) sendUser(text string) error {
	uid := uuid4()
	payload := append(userFrameBytes(text, uid), '\n')
	p.logLine("write", fmt.Sprintf("uuid=%s %s", uid, string(payload)))
	fmt.Printf(">>> WRITE stdin %d bytes: %s\n", len(payload), trunc(string(payload)))
	if _, err := p.stdin.Write(payload); err != nil {
		return fmt.Errorf("stdin write: %w", err)
	}
	return nil
}

// runTurn 收帧直到 type==result；返回 (result帧内容, 是否超时, 是否进程先退)。
func (p *probe) runTurn(label string, budget time.Duration) (string, bool, bool) {
	deadline := time.After(budget)
	var types map[string]int
	_ = types
	n := 0
	for {
		select {
		case f, ok := <-p.frames:
			if !ok {
				fmt.Printf(">>> %s: 帧流结束（进程退出）,本回合已见 %d 帧\n", label, n)
				return "", false, true
			}
			n++
			typ, sub := frameType(f.line)
			fmt.Printf(">>> %s: 帧#%d type=%s subtype=%s\n", label, n, typ, sub)
			if typ == "result" {
				fmt.Print(summarizeResult(f.line))
				return f.line, false, false
			}
		case err := <-p.exited:
			fmt.Printf(">>> %s: 进程退出 err=%v,本回合已见 %d 帧\n", label, err, n)
			// 退出前帧流已 close，这里再排空缓冲里的残帧
			for {
				select {
				case f, ok := <-p.frames:
					if !ok {
						return "", false, true
					}
					fmt.Printf(">>> %s: 补收残帧 %s\n", label, trunc(f.line))
				default:
					return "", false, true
				}
			}
		case <-deadline:
			fmt.Printf(">>> %s: TIMEOUT(%v)，本回合已见 %d 帧\n", label, budget, n)
			return "", true, false
		}
	}
}

func main() {
	flag.Parse()
	_ = os.MkdirAll(logsDir, 0o755)
	if *tag == "" {
		*tag = strings.ToUpper(*shape)
	}
	switch *mode {
	case "session":
		runSession()
	case "resume":
		runResume()
	case "fields":
		runFields()
	case "junk":
		runJunk()
	default:
		fmt.Println("bad mode")
		os.Exit(2)
	}
}

// sendRaw 直接写一段字节+\n（探测用，绕开形状封装）。
func (p *probe) sendRaw(label string, payload []byte) error {
	p.logLine("write", label+": "+string(payload))
	fmt.Printf(">>> WRITE %s: %s\n", label, trunc(string(payload)))
	if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("stdin write: %w", err)
	}
	return nil
}

// runFields 补测 stdin 侧字段契约：垃圾行容错 / content 传字符串 / 无 uuid。
func runFields() {
	sid := uuid4()
	fmt.Printf(">>> SESSION_ID=%s (fields)\n", sid)
	p, err := launch(sid, false)
	if err != nil {
		fmt.Printf(">>> LAUNCH FAILED: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if p.alive() {
			p.killTree()
			<-p.exited
		} else {
			select {
			case e := <-p.exited:
				fmt.Printf(">>> waitErr(自然退出)=%v\n", e)
			default:
			}
		}
		fmt.Printf(">>> 全程 stderr 字节数=%d\n", p.seLen)
		p.close()
	}()
	// 排空启动期 hook 帧（2s）
	drain := time.After(2 * time.Second)
drainloop:
	for {
		select {
		case _, ok := <-p.frames:
			if !ok {
				break drainloop
			}
		case <-drain:
			break drainloop
		}
	}

	// F1: 非 JSON 垃圾行（实测会杀进程，可跳过）
	if !*skipF1 {
		if err := p.sendRaw("F1-not-json", []byte("NOT_A_JSON_LINE")); err != nil {
			fmt.Printf(">>> F1 写失败(进程已死?): %v\n", err)
			return
		}
		// 垃圾行后的任何报错帧，3s 窗口观察
		win := time.After(3 * time.Second)
	winloop:
		for {
			select {
			case f, ok := <-p.frames:
				if !ok {
					fmt.Println(">>> F1: 帧流结束——垃圾行导致进程退出!")
					return
				}
				typ, sub := frameType(f.line)
				fmt.Printf(">>> F1 后收到帧: type=%s subtype=%s raw=%s\n", typ, sub, trunc(f.line))
			case <-win:
				break winloop
			}
		}
		fmt.Printf(">>> F1 存活检查: %v\n", p.alive())
	}

	// F2: 数组 content、无 uuid（单独测 uuid 是否必需）
	b2, _ := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "text", "text": "只回一个字：乙"}}},
	})
	if err := p.sendRaw("F2-array-content-no-uuid", b2); err != nil {
		fmt.Printf(">>> F2 写失败: %v\n", err)
		return
	}
	res2, to2, dead2 := p.runTurn("F2", 90*time.Second)
	fmt.Printf(">>> F2 结论: result=%q timeout=%v dead=%v\n", trunc(res2), to2, dead2)

	// F3: content 直接为字符串、带 uuid（单独测字符串 content 是否可用）
	if p.alive() {
		b3, _ := json.Marshal(map[string]any{
			"type":    "user",
			"message": map[string]any{"role": "user", "content": "只回一个字：丙"},
			"uuid":    uuid4(),
		})
		if err := p.sendRaw("F3-string-content-uuid", b3); err != nil {
			fmt.Printf(">>> F3 写失败: %v\n", err)
			return
		}
		res3, to3, dead3 := p.runTurn("F3", 90*time.Second)
		fmt.Printf(">>> F3 结论: result=%q timeout=%v dead=%v\n", trunc(res3), to3, dead3)
	} else {
		fmt.Println(">>> F3 跳过：进程已不存活")
	}
}

func runSession() {
	sid := uuid4()
	fmt.Printf(">>> SESSION_ID=%s shape=%s\n", sid, strings.ToUpper(*shape))

	p, err := launch(sid, false)
	if err != nil {
		fmt.Printf(">>> LAUNCH FAILED: %v\n", err)
		os.Exit(1)
	}

	// ---- E1: 第一帧 ----
	f, ok := p.waitFrame(40 * time.Second)
	if !ok {
		fmt.Println(">>> E1: 40s 内 stdout 无任何帧（TIMEOUT）")
	} else {
		typ, sub := frameType(f.line)
		fmt.Printf(">>> E1: 第一帧 type=%s subtype=%s at=%v\n", typ, sub, f.at.Round(time.Millisecond))
	}
	// 再听 3s，看 init 后是否还有帧
	{
		done := time.After(3 * time.Second)
		got := 0
	collect:
		for {
			select {
			case f2, ok2 := <-p.frames:
				if !ok2 {
					fmt.Println(">>> E1+: 帧流提前结束(进程退出!)")
					break collect
				}
				got++
				typ, sub := frameType(f2.line)
				fmt.Printf(">>> E1+: 额外帧 type=%s subtype=%s\n", typ, sub)
				// 把第一帧也塞不回去——超时语义由后续 turn 体现；这里把 E1 帧补进 turn 视野
				_ = f2
			case <-done:
				break collect
			}
		}
		if got == 0 && ok {
			// E1 帧已消费，重新投递一个"哨兵"不必要；仅提示
			fmt.Println(">>> E1: init 后 3s 内无更多帧（E1 帧已被消费，下面 E2 会显式重发）")
			// 把 E1 原文再打印一次，避免丢样本
			fmt.Printf(">>> E1-FRAME-AGAIN %s\n", trunc(f.line))
		}
	}

	// ---- E2: 第一句 ----
	fmt.Println(">>> ===== E2: 发送 user 帧#1 =====")
	if err := p.sendUser("只回一个字：甲"); err != nil {
		fmt.Printf(">>> E2: 写 stdin 失败: %v（形状被拒/进程已死）\n", err)
		p.killTree()
		return
	}
	res, to, dead := p.runTurn("E2", 150*time.Second)
	fmt.Printf(">>> E2 结论: result=%v timeout=%v 进程退出=%v\n", res != "", to, dead)
	if dead || to {
		if p.alive() { // dead 时 runTurn 已消费过 exited，不能再收，否则永久阻塞
			p.killTree()
			<-p.exited
		}
		fmt.Printf(">>> E2 后 stderr 字节数=%d\n", p.seLen)
		p.close()
		return
	}
	time.Sleep(2 * time.Second)
	fmt.Printf(">>> E2 后进程存活(2s 后检查): %v\n", p.alive())

	// ---- E3: 第二句（同进程）----
	fmt.Println(">>> ===== E3: 发送 user 帧#2（同进程多轮）=====")
	if err := p.sendUser("刚才我让你回什么？"); err != nil {
		fmt.Printf(">>> E3: 写 stdin 失败: %v\n", err)
	} else {
		res3, to3, dead3 := p.runTurn("E3", 150*time.Second)
		fmt.Printf(">>> E3 结论: result=%v timeout=%v 进程退出=%v\n", res3 != "", to3, dead3)
		if strings.Contains(res3, "甲") {
			fmt.Println(">>> E3 记忆证据: 第二句 result 文本含「甲」")
		} else {
			fmt.Println(">>> E3 注意: 第二句 result 文本不含「甲」——需人工看原文")
		}
	}

	// ---- E4 前半: 杀进程（模拟 hanxi 关窗）----
	fmt.Println(">>> ===== E4: kill 进程树 =====")
	p.killTree()
	err = <-p.exited
	fmt.Printf(">>> E4: 进程已退 waitErr=%v\n", err)
	fmt.Printf(">>> E4: SESSION_ID_FOR_RESUME=%s\n", sid)
	fmt.Printf(">>> 全程 stderr 字节数=%d\n", p.seLen)
	p.close()
}

// runJunk 专测两类坏输入是否杀进程：非 JSON 行、合法 JSON 但 type 未知。
func runJunk() {
	for i, payload := range []string{"NOT_A_JSON_LINE", `{"type":"banana"}`} {
		fmt.Printf(">>> ===== JUNK#%d: %s =====\n", i+1, payload)
		p, err := launch(uuid4(), false)
		if err != nil {
			fmt.Printf(">>> LAUNCH FAILED: %v\n", err)
			return
		}
		// 等 hook 帧期 2s，确保进程已就绪
		time.Sleep(2 * time.Second)
		t0 := time.Now()
		if err := p.sendRaw(fmt.Sprintf("JUNK%d", i+1), []byte(payload)); err != nil {
			fmt.Printf(">>> JUNK%d 写失败: %v\n", i+1, err)
		}
		select {
		case e := <-p.exited:
			fmt.Printf(">>> JUNK%d: 进程退出 waitErr=%v（写入后 %v）\n", i+1, e, time.Since(t0).Round(time.Millisecond))
		case <-time.After(10 * time.Second):
			fmt.Printf(">>> JUNK%d: 写后 10s 进程仍存活=该行被忽略不致命\n", i+1)
			p.killTree()
			<-p.exited
		}
		fmt.Printf(">>> JUNK%d stderr 全文:\n%s\n", i+1, head(p.sfl, 500))
		_ = p.fl.Close()
		_ = p.sfl.Close()
	}
}

func runResume() {
	sid := *sidF
	if sid == "" {
		fmt.Println(">>> resume 需要 -sid")
		os.Exit(2)
	}
	fmt.Printf(">>> RESUME sid=%s shape=%s\n", sid, strings.ToUpper(*shape))
	p, err := launch(sid, true)
	if err != nil {
		fmt.Printf(">>> LAUNCH FAILED: %v\n", err)
		os.Exit(1)
	}
	f, ok := p.waitFrame(40 * time.Second)
	if ok {
		typ, sub := frameType(f.line)
		fmt.Printf(">>> R-E1: 第一帧 type=%s subtype=%s\n", typ, sub)
		// 检查 init 帧里 session_id 是否等于传入值
		var m map[string]any
		if json.Unmarshal([]byte(f.line), &m) == nil {
			if s, _ := m["session_id"].(string); s != "" {
				fmt.Printf(">>> R-E1: init 帧 session_id=%s (match=%v)\n", s, s == sid)
			}
		}
	} else {
		fmt.Println(">>> R-E1: 40s 无帧 TIMEOUT")
	}
	fmt.Println(">>> ===== E4 后半: 问「我们之前聊过什么」 =====")
	if err := p.sendUser("我们之前聊过什么？只答关键词"); err != nil {
		fmt.Printf(">>> E4R: 写 stdin 失败: %v\n", err)
		p.killTree()
		return
	}
	res, to, dead := p.runTurn("E4R", 150*time.Second)
	fmt.Printf(">>> E4R 结论: result=%v timeout=%v 进程退出=%v\n", res != "", to, dead)
	if strings.Contains(res, "甲") {
		fmt.Println(">>> E4 记忆证据: resume 会话 result 含「甲」→ 跨进程记忆成立")
	} else {
		fmt.Println(">>> E4 注意: result 不含「甲」，人工核对原文（见 frames 日志）")
	}
	if p.alive() {
		p.killTree()
		<-p.exited
	}
	fmt.Printf(">>> 全程 stderr 字节数=%d\n", p.seLen)
	p.close()
}

func (p *probe) close() {
	fmt.Fprintf(os.Stderr, ">>> stderr sample(head 800B):\n%s\n", head(p.sfl, 800))
	_ = p.fl.Close()
	_ = p.sfl.Close()
}

func head(f *os.File, n int) string {
	b, _ := os.ReadFile(f.Name())
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + fmt.Sprintf("\n…[截断,全长%d字节]", len(b))
}
