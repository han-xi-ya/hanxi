package omp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// OmpService 向前端暴露「Oh My Pi (omp)」的官方安装通道：一键安装/更新/卸载
// （代跑官方 install.ps1，见 official.go）与目录/仓库导航。
// 刻意窄于 ccswitch/rustdesk 等托管模块：**不接管进程运行，也不维护自建版本树**
// （机主拍板"安装=跑官方脚本"，版本树方案已于 2026-11-04 整体拆除——omp 的
// 版本追更感知由 envcheck 的官方 Releases 对照卡负责，两模块分工见各自包注释）。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）。
type OmpService struct {
	plat   platform.Platform
	ops    *officialOps
	holder *extapi.LeaseHolder

	officialMu   sync.Mutex
	officialBusy bool // 同一时刻只允许一趟官方脚本（install/update 共用槽位）
}

// NewOmpService 装配官方路线操作集；构造无 IO 副作用（状态全部现场探测，
// 本模块不再持久化任何偏好——无版本树自然无"激活记账"）。
func NewOmpService(plat platform.Platform, holder *extapi.LeaseHolder) *OmpService {
	return &OmpService{
		plat:   plat,
		ops:    newOfficialOps(),
		holder: holder,
	}
}

// GetStatus 模块状态快照：官方装件四事实 + omp 数据根（~/.omp）。
func (s *OmpService) GetStatus() (OmpStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return OmpStatus{}, gateErr
	}
	defer release()

	st := OmpStatus{}
	st.OfficialDir, st.OfficialPresent, st.OfficialVersion, st.OnPath = s.ops.probe()
	if home, herr := os.UserHomeDir(); herr == nil {
		st.DataDir = filepath.Join(home, ".omp")
	}
	return st, nil
}

// ---------- 官方安装方式（主通道，全部动作在此）----------

// InstallOfficial 一键安装=后台跑官方 install.ps1 -Binary（下载最新单文件、
// 官方自校验、落 %LOCALAPPDATA%\omp、写 PATH、Git Bash 接线全套官方行为）。
// 进度经事件 omp:official-op 回流；已在跑则报 in-progress。
func (s *OmpService) InstallOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.startOfficial("install")
}

// UpdateOfficial 一键更新=再跑一遍官方脚本（脚本语义即"拉最新覆盖装"，幂等）。
func (s *OmpService) UpdateOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.startOfficial("update")
}

// startOfficial install/update 共用编排（调用门由两个导出 RPC 各自持有，本私有件
// 承接已放行路径；互斥 officialMu 槽位）。
func (s *OmpService) startOfficial(op string) (string, error) {
	s.officialMu.Lock()
	if s.officialBusy {
		s.officialMu.Unlock()
		return "in-progress", nil
	}
	s.officialBusy = true
	s.officialMu.Unlock()

	go func() {
		defer func() {
			s.officialMu.Lock()
			s.officialBusy = false
			s.officialMu.Unlock()
		}()
		emit := func(stage, msg string) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("omp:official-op", OfficialOpProgress{Op: op, Stage: stage, Message: msg})
			}
		}
		emit("running", "正在执行官方安装脚本（下载校验全程由脚本自理）…")
		lease, leaseErr := s.holder.EnterBackground(context.Background())
		if leaseErr != nil {
			emit("error", leaseErr.Error())
			return
		}
		defer lease.Release()
		out, err := s.ops.runOfficialScript(lease.Context())
		if err != nil {
			emit("error", fmt.Sprintf("%s: %v", firstLineFallback(out, "脚本无输出"), err))
			notify.Error("omp", "官方脚本失败", err.Error(), "/ext/omp")
			return
		}
		verb := "安装"
		if op == "update" {
			verb = "更新"
		}
		emit("done", fmt.Sprintf("omp 已%s：%s", verb, s.ops.probeVer(s.ops.exePath())))
		notify.Success("omp", "官方"+verb+"完成", "新开终端敲 omp 即可使用（凭据与数据在 ~/.omp 未动）", "/ext/omp")
	}()
	return "started", nil
}

// UninstallOfficial 一键卸载=删官方目录 + 从用户 PATH 剔除该条目 + 广播；
// ~/.omp 数据根（凭据/会话/记忆）按官方口径保留。危险确认在前端适配层。
func (s *OmpService) UninstallOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.ops.uninstall()
}

// firstLineFallback 取脚本输出首行做错误简报，空则用兜底文案。
func firstLineFallback(out, fallback string) string {
	if line := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]; line != "" {
		return line
	}
	return fallback
}

// ---------- 目录与仓库导航 ----------

// OpenDir 在资源管理器中打开指定目录（官方安装目录 / ~/.omp 数据根）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe
// <dir> 直启；刻意不走 explorer.exe <file> 的"执行"语义（markeron 事故教训）。
func (s *OmpService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *OmpService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return officialRepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *OmpService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(officialRepoURL())
}
