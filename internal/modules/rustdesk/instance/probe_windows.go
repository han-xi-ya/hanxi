//go:build windows

package instance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// exeImageName 内层主程序名与外层 packer 落盘定名同为 rustdesk.exe
	//（上游未改名，与 SubnetDesk 把内层改名 SubnetDesk.exe 不同）。
	// 外层进程存活期极短且镜像路径在托管目录（不在提取目录前缀内），
	// 天然被路径过滤排除；同名单下自有/外部身份全部由提取目录前缀裁决。
	exeImageName = "rustdesk.exe"
	// unpackDirName packer 提取目录名：app_dir_name 取内层条目名小写（libs/portable 源码实证）
	unpackDirName = "rustdesk"
)

type windowsRustDeskProbe struct{}

// NewRustDeskProbe Windows 实现：进程快照 + 镜像路径前缀判别（详见 prober.go 契约）。
func NewRustDeskProbe() RustDeskProbe { return &windowsRustDeskProbe{} }

// unpackDirPrefix 便携实例提取目录（小写 + 结尾分隔符，供路径前缀判别）。
// LOCALAPPDATA 恒为用户会话级环境变量，进程存续期不会变，一次解析缓存。
var unpackDirPrefix = sync.OnceValue(func() string {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return strings.ToLower(filepath.Join(base, unpackDirName)) + string(filepath.Separator)
})

type portableProc struct {
	pid  uint32
	ppid uint32
}

// scanPortableProcs Toolhelp32 快照：按进程名初筛，再以镜像路径前缀定身份。
// OpenProcess 失败（SYSTEM 服务/权限不足）一律跳过——宁漏报不误判；
// 安装版（Program Files\RustDesk）不在提取目录前缀内，天然排除。
func scanPortableProcs() []portableProc {
	prefix := unpackDirPrefix()
	if prefix == "" {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var out []portableProc
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if !strings.EqualFold(name, exeImageName) {
			continue
		}
		path, ok := processImagePath(entry.ProcessID)
		if !ok || !strings.HasPrefix(path, prefix) {
			continue
		}
		out = append(out, portableProc{pid: entry.ProcessID, ppid: entry.ParentProcessID})
	}
	return out
}

// processImagePath QueryFullProcessImageNameW（最小权限 PROCESS_QUERY_LIMITED_INFORMATION）。
func processImagePath(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH + 1]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	return strings.ToLower(syscall.UTF16ToString(buf[:size])), true
}

func (p *windowsRustDeskProbe) FindInstancePIDs() []uint32 {
	procs := scanPortableProcs()
	pids := make([]uint32, 0, len(procs))
	for _, pr := range procs {
		pids = append(pids, pr.pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids
}

// FindOwnPIDs 从 ancestors 出发做父链闭包：内层 UI、其拉起的 --server/--tray、
// 派生开窗的外层及其子代，全部经传递归入自有进程树。
func (p *windowsRustDeskProbe) FindOwnPIDs(ancestors []uint32) []uint32 {
	if len(ancestors) == 0 {
		return nil
	}
	procs := scanPortableProcs()
	own := map[uint32]bool{}
	frontier := append([]uint32(nil), ancestors...)
	for len(frontier) > 0 {
		var next []uint32
		for _, pr := range procs {
			if own[pr.pid] {
				continue
			}
			for _, anc := range frontier {
				if pr.ppid == anc {
					own[pr.pid] = true
					next = append(next, pr.pid)
					break
				}
			}
		}
		frontier = next
	}
	pids := make([]uint32, 0, len(own))
	for pid := range own {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids
}

func (p *windowsRustDeskProbe) IsRunning() bool {
	return len(p.FindInstancePIDs()) > 0
}

func (p *windowsRustDeskProbe) HasVisibleWindow(pids []uint32) bool {
	return hasVisibleWindowByPIDs(toPIDSet(pids))
}

func (p *windowsRustDeskProbe) FocusWindows(pids []uint32) int {
	if len(pids) == 0 {
		return 0
	}
	return focusWindowsByPIDs(toPIDSet(pids))
}

func toPIDSet(pids []uint32) map[uint32]struct{} {
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return set
}
