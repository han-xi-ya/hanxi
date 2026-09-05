//go:build windows

package instance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/modules/subnetdesk/version"
)

const (
	// exeImageName 内层主程序名（上游 flutter-build.yml 将 rustdesk.exe 改名
	// SubnetDesk.exe 打进 packer 负载）；外层 packer 落定名 subnetdesk.exe 同族匹配。
	// 外层进程存活期极短且镜像路径在托管目录（不在提取目录前缀内），
	// 天然被路径过滤排除，纳入名单只是省一次 OpenProcess 试错。
	// 安装版主程序 SubnetDesk.exe 与内层同名（上游 File 元素 Name=$(var.Product).exe），
	// 进程名初筛一并收下，归属由安装目录前缀精确判别。
	exeImageName = "SubnetDesk.exe"
	// outerImageName 本模块落盘的 packer 外层定名（与 version.exeName 一致）
	outerImageName = "subnetdesk.exe"
	// unpackDirName packer 提取目录名：app_dir_name 取内层条目名小写（libs/portable 源码实证）
	unpackDirName = "subnetdesk"
	// installedDirTTL 安装目录探测缓存时长：装机/卸载是低频事件，但须支持
	// 应用运行期感知；监督轮询 400ms 一跑，TTL 挡住注册表扫描的重复开销。
	installedDirTTL = 60 * time.Second
)

type windowsSubnetDeskProbe struct{}

// NewSubnetDeskProbe Windows 实现：进程快照 + 镜像路径前缀判别（详见 prober.go 契约）。
func NewSubnetDeskProbe() SubnetDeskProbe { return &windowsSubnetDeskProbe{} }

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

// installedDirPrefix 安装版目录前缀（小写 + 结尾分隔符）；未安装返回 ""。
// 带 TTL 缓存：真机实证安装版服务与其 broker（Session 0 / SYSTEM）镜像路径
// 在非提权 OpenProcess 下不可读，会被 processImagePath 天然跳过，
// 因此前缀匹配到的必然只有用户态客户端进程——服务误判在权限层就关不住。
var (
	instDirMu    sync.Mutex
	instDirCache string
	instDirAt    time.Time
)

func installedDirPrefix() string {
	instDirMu.Lock()
	defer instDirMu.Unlock()
	if time.Since(instDirAt) < installedDirTTL {
		return instDirCache
	}
	instDirAt = time.Now()
	instDirCache = ""
	if si, ok := version.DetectSystemInstall(); ok {
		instDirCache = strings.ToLower(si.Dir) + string(filepath.Separator)
	}
	return instDirCache
}

// isInstancePath 进程镜像路径是否命中实例身份前缀（便携提取目录 ∪ 安装目录）。
func isInstancePath(path string) bool {
	if p := unpackDirPrefix(); p != "" && strings.HasPrefix(path, p) {
		return true
	}
	if p := installedDirPrefix(); p != "" && strings.HasPrefix(path, p) {
		return true
	}
	return false
}

// scanInstanceProcs Toolhelp32 快照：按进程名初筛（安装版内层同名，EqualFold
// 一并收下），再以镜像路径前缀定身份。OpenProcess 失败（SYSTEM 服务/权限不足）
// 一律跳过——宁漏报不误判。
func scanInstanceProcs() []portableProc {
	if unpackDirPrefix() == "" && installedDirPrefix() == "" {
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
		if !strings.EqualFold(name, exeImageName) && !strings.EqualFold(name, outerImageName) {
			continue
		}
		path, ok := processImagePath(entry.ProcessID)
		if !ok || !isInstancePath(path) {
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

func (p *windowsSubnetDeskProbe) FindInstancePIDs() []uint32 {
	procs := scanInstanceProcs()
	pids := make([]uint32, 0, len(procs))
	for _, pr := range procs {
		pids = append(pids, pr.pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids
}

// FindOwnPIDs 从 ancestors 出发做父链闭包：内层 UI、其拉起的 --server/--tray、
// 派生开窗的外层及其子代，全部经传递归入自有进程树。ancestors 本体若命中
// 实例目录前缀也直接计入——安装版直拉主进程无"外层秒退"环节，本体即实例；
// packer 外层镜像在托管目录不命中前缀，该种子对其恒为空操作。
func (p *windowsSubnetDeskProbe) FindOwnPIDs(ancestors []uint32) []uint32 {
	if len(ancestors) == 0 {
		return nil
	}
	procs := scanInstanceProcs()
	own := map[uint32]bool{}
	anc := make(map[uint32]bool, len(ancestors))
	for _, a := range ancestors {
		anc[a] = true
	}
	frontier := append([]uint32(nil), ancestors...)
	for _, pr := range procs {
		if anc[pr.pid] && !own[pr.pid] {
			own[pr.pid] = true
		}
	}
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

func (p *windowsSubnetDeskProbe) IsRunning() bool {
	return len(p.FindInstancePIDs()) > 0
}

func (p *windowsSubnetDeskProbe) HasVisibleWindow(pids []uint32) bool {
	return hasVisibleWindowByPIDs(toPIDSet(pids))
}

func (p *windowsSubnetDeskProbe) FocusWindows(pids []uint32) int {
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
