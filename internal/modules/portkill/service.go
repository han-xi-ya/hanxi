package portkill

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"hubkit/internal/notify"
	"hubkit/internal/platform"
)

// PortOccupant 端口占用实体模型
type PortOccupant struct {
	Port        int       `json:"port"`
	Protocol    string    `json:"protocol"` // "TCP" | "UDP"
	LocalIP     string    `json:"localIp"`
	State       string    `json:"state"`
	PID         uint32    `json:"pid"`
	ProcessName string    `json:"processName"`
	ExePath     string    `json:"exePath"`
	StartedAt   time.Time `json:"startedAt"`
	IsProtected bool      `json:"isProtected"` // 是否受系统红线保护
}

// KillResult 查杀操作反馈结果
type KillResult struct {
	Success      bool   `json:"success"`
	NeedElevate  bool   `json:"needElevate"` // 是否需要管理员 UAC 提权
	ErrorMessage string `json:"errorMessage"`
}

type PortKillService struct {
	plat platform.Platform
}

func NewPortKillService(plat platform.Platform) *PortKillService {
	return &PortKillService{plat: plat}
}

// QueryPort 查询指定端口号的占用情况 (TCP + UDP)
func (s *PortKillService) QueryPort(port int) ([]PortOccupant, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", port)
	}

	targetPort := uint16(port)
	var occupants []PortOccupant

	// 1. 扫描 TCP (IPv4 + IPv6)
	for _, fam := range []platform.Family{platform.FamilyIPv4, platform.FamilyIPv6} {
		rows, err := s.plat.Port().TCPTable(fam)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r.LocalPort == targetPort {
				pInfo, _ := s.plat.Process().Query(r.PID)
				isProt := s.plat.Process().IsProtected(r.PID, pInfo)

				occupants = append(occupants, PortOccupant{
					Port:        port,
					Protocol:    "TCP",
					LocalIP:     r.LocalIP,
					State:       string(r.State),
					PID:         r.PID,
					ProcessName: pInfo.Name,
					ExePath:     pInfo.ExePath,
					StartedAt:   pInfo.StartedAt,
					IsProtected: isProt,
				})
			}
		}
	}

	// 2. 扫描 UDP (IPv4 + IPv6)
	for _, fam := range []platform.Family{platform.FamilyIPv4, platform.FamilyIPv6} {
		rows, err := s.plat.Port().UDPTable(fam)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r.LocalPort == targetPort {
				pInfo, _ := s.plat.Process().Query(r.PID)
				isProt := s.plat.Process().IsProtected(r.PID, pInfo)

				occupants = append(occupants, PortOccupant{
					Port:        port,
					Protocol:    "UDP",
					LocalIP:     r.LocalIP,
					State:       "—",
					PID:         r.PID,
					ProcessName: pInfo.Name,
					ExePath:     pInfo.ExePath,
					StartedAt:   pInfo.StartedAt,
					IsProtected: isProt,
				})
			}
		}
	}

	return occupants, nil
}

// ListListeningPorts 列举当前系统处于 LISTEN / 占用的常见活跃端口
func (s *PortKillService) ListListeningPorts() ([]PortOccupant, error) {
	var list []PortOccupant
	seen := make(map[string]bool)

	// 获取 TCP LISTEN 列表
	for _, fam := range []platform.Family{platform.FamilyIPv4, platform.FamilyIPv6} {
		rows, err := s.plat.Port().TCPTable(fam)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r.State != platform.TCPStateListen {
				continue
			}
			key := fmt.Sprintf("TCP:%d:%d", r.LocalPort, r.PID)
			if seen[key] {
				continue
			}
			seen[key] = true

			pInfo, _ := s.plat.Process().Query(r.PID)
			isProt := s.plat.Process().IsProtected(r.PID, pInfo)

			list = append(list, PortOccupant{
				Port:        int(r.LocalPort),
				Protocol:    "TCP",
				LocalIP:     r.LocalIP,
				State:       string(r.State),
				PID:         r.PID,
				ProcessName: pInfo.Name,
				ExePath:     pInfo.ExePath,
				StartedAt:   pInfo.StartedAt,
				IsProtected: isProt,
			})
		}
	}

	// 排序：按端口升序排列
	sort.Slice(list, func(i, j int) bool {
		if list[i].Port != list[j].Port {
			return list[i].Port < list[j].Port
		}
		return list[i].PID < list[j].PID
	})

	return list, nil
}

// KillProcess 通过安全令牌终止目标进程
func (s *PortKillService) KillProcess(pid uint32, exePath string, startedAtUnix int64) KillResult {
	var startedAt time.Time
	if startedAtUnix > 0 {
		startedAt = time.Unix(startedAtUnix, 0)
	}

	token := platform.VerifyToken{
		PID:       pid,
		ExePath:   exePath,
		StartedAt: startedAt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.plat.Process().KillVerified(ctx, token, true)
	if err == nil {
		notify.Success("portkill", "进程已终止", fmt.Sprintf("已成功终止进程 PID: %d (%s)", pid, exePath), "/portkill")
		return KillResult{Success: true}
	}

	if err == platform.ErrAccessDenied {
		return KillResult{
			Success:      false,
			NeedElevate:  true,
			ErrorMessage: "权限不足，目标进程需要管理员 UAC 权限终止",
		}
	}

	return KillResult{
		Success:      false,
		ErrorMessage: fmt.Sprintf("终止失败: %v", err),
	}
}

// KillProcessElevated 触发 UAC 提权 Helper 查杀管理员进程
func (s *PortKillService) KillProcessElevated(pid uint32) KillResult {
	if pid == 0 || pid == 4 || pid == uint32(os.Getpid()) {
		return KillResult{
			Success:      false,
			ErrorMessage: "受系统保护的关键进程不可查杀",
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return KillResult{Success: false, ErrorMessage: "无法定位宿主程序路径"}
	}

	// 使用 powershell 的 Start-Process -Verb RunAs 调起同二进制的 helper 模式
	args := fmt.Sprintf(`Start-Process -FilePath "%s" -ArgumentList "-mode=killhelper", "-pid=%d" -Verb RunAs -Wait -WindowStyle Hidden`, exe, pid)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", args)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if strings.Contains(outStr, "canceled by the user") || strings.Contains(outStr, "1223") {
			return KillResult{Success: false, ErrorMessage: "用户取消了 UAC 授权"}
		}
		return KillResult{Success: false, ErrorMessage: fmt.Sprintf("提权查杀失败: %v %s", err, outStr)}
	}

	return KillResult{Success: true}
}
