//go:build windows

package instance

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// pipeBaseName QuickLook 服务端命名管道名前缀；完整名 = 前缀 + 当前用户 SID。
// 对应上游 PipeName = "QuickLook.App.Pipe." + WindowsIdentity.GetCurrent().User.Value。
const pipeBaseName = `\\.\pipe\QuickLook.App.Pipe.`

// dialTimeout 连接命名管道的最长等待：服务端 WaitForConnection 才接受，
// 若实例假死则不无限阻塞调用方；超时即放弃优雅路径转强杀兜底。
const dialTimeout = 800 * time.Millisecond

// currentUserPipeName 组装 QuickLook 服务端管道全名：QuickLook.App.Pipe.<用户SID>。
func currentUserPipeName() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	return pipeBaseName + sid, nil
}

// currentUserSID 取当前进程令牌的用户 SID 字符串（形如 S-1-5-21-...），
// 与 .NET WindowsIdentity.GetCurrent().User.Value 同值，故能命中服务端拼出的管道名。
func currentUserSID() (string, error) {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
		return "", fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer tok.Close()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("GetTokenUser: %w", err)
	}
	return tu.User.Sid.String(), nil
}

// sendPipeMessage best-effort 向 QuickLook 管道写一行控制消息。
// Go 在 Windows 下可直接把命名管道当文件打开（os.OpenFile O_WRONLY），写入的字节
// 由服务端 StreamReader.ReadLine() 收取。开管道会阻塞至服务端接受连接，故放
// goroutine 并用 dialTimeout 兜底，防止实例假死时卡住 Quit/Reload 调用方。
func sendPipeMessage(msg string) error {
	name, err := currentUserPipeName()
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		f, oerr := os.OpenFile(name, os.O_WRONLY, 0)
		if oerr != nil {
			done <- fmt.Errorf("open pipe %s: %w", name, oerr)
			return
		}
		defer f.Close()
		_, werr := f.Write([]byte(msg + "\n"))
		done <- werr
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(dialTimeout):
		return fmt.Errorf("连接 QuickLook 命名管道超时")
	}
}
