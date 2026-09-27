package translucenttb

// 打包版孤儿应用数据的探测与改名隔离（2026-09-27 机主真机事故改判落地件）。
//
// 事故实证链：瘦系统上卸载/失败安装把 %LOCALAPPDATA%\Packages\<包族名> 留成
// 孤儿（包已不在册、数据在场）→ 下次 Add-AppxPackage 的"删旧应用数据"步骤
// 遇 0x800703FA（缺组件）→ 0x80073D05 → 0x80073CF6 拒装。归因表孤儿档
// （msixOrphanFailure）指到真凶后，本文件提供受控破局能力。
//
// 主策略是**改名隔离不是删除**（机主真机一手实证）：孤儿目录里
// TempState\ExplorerTAP.dll 等旧 shell 扩展被 explorer 进程持柄，
// os.RemoveAll 实测 Partial-FAILED 留下半删状态更难收拾；Rename 整体挪开
// 实测成功，挪开后 Add-AppxPackage 立刻 INSTALL-OK。隔离可逆、零真删，
// 备份（<族名>.orphan-YYYYMMDD[-HHMMSS]）由机主手动处置。
//
// 唯一可动路径 = %LOCALAPPDATA%\Packages\28017CharlesMilette.TranslucentTB_v826wp6bftszj：
// 包族名取 models.go 实证常量，零入参零拼接面，不存在任何调用方（含前端/
// MCP/命令行）可影响的成分；<族名>.orphan-* 隔离备份是另一个名字，
// 不在探测与处理的射程内（重复隔离不覆盖既有备份，防撞名另起时间戳）。
//
// 运行纪律（机主授权口径）：本文件的删除级动作只由视图「🧹 清理后重试」钮
// 经 CleanMsixOrphan RPC 承载；开发/测试期任何真径不进盘（fs 缝单测注入）。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/platform/apppackage"
)

// msixAllUsersProber 全用户在册探测的可选能力接缝（消费侧声明；实现见
// internal/platform/windows IsRegisteredAllUsers——appPackage 脚本通道
// queryallusers 操作）。不吃 apppackage.API 必选面：其他模块的 API 替身
// 零改动（禁碰他模块纪律）；通道未提供该能力即探测失败 → 三道闸按
// "无法证明无注册"保守拒动。
type msixAllUsersProber interface {
	IsRegisteredAllUsers(ctx context.Context, identity apppackage.Identity) (bool, error)
}

// orphanSampleCap 删除前指纹清单的样本条数上限（超出只计数不落样本，
// Info 行不炸日志；总数/总字节恒为全量口径）。
const orphanSampleCap = 80

// orphanInventory 指纹清点的落账形（闸③凭据）。
type orphanInventory struct {
	files     int
	bytes     int64
	entries   []string // 相对路径样本（前 orphanSampleCap 条）
	truncated bool
}

// logField 样本清单的日志呈现形。
func (inv *orphanInventory) logField() string {
	if inv == nil {
		return ""
	}
	joined := strings.Join(inv.entries, "; ")
	if inv.truncated {
		return joined + fmt.Sprintf(" …(+%d 条未列)", inv.files-len(inv.entries))
	}
	return joined
}

// orphanHooks 孤儿能力的文件系统/时钟缝：生产路径恒走缺省真实现（零值即
// 真实现，构造期无需布线），单测注入替身——真删真挪只由 UI 钮在机主授权
// 后的运行期承载，测试与探测绝不动真实目录。
type orphanHooks struct {
	dir    func() (string, error)
	exists func(string) (bool, error)
	walk   func(string) (*orphanInventory, error)
	rename func(oldPath, newPath string) error
	now    func() time.Time
}

// resolve 零值槽位回补缺省真实现（s.orphan 允许为结构体零值）。
func (h orphanHooks) resolve() orphanHooks {
	if h.dir == nil {
		h.dir = msixOrphanDataDir
	}
	if h.exists == nil {
		h.exists = msixPathExists
	}
	if h.walk == nil {
		h.walk = msixOrphanInventory
	}
	if h.rename == nil {
		h.rename = os.Rename
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}

// msixOrphanDataDir 唯一可动路径的组装：%LOCALAPPDATA%\Packages\<包族名常量>。
// 零入参——包族名来自 models.go 三源实证常量，全链无调用方可影响成分。
func msixOrphanDataDir() (string, error) {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return "", errors.New("环境变量 LOCALAPPDATA 缺失，无法定位孤儿数据目录")
	}
	return filepath.Join(localAppData, "Packages", MsixPackageFamily), nil
}

// msixPathExists 目录在场判定（闸②口径）：不存在回 (false,nil)；存在但
// 不是目录（同名文件等畸形态）回错误——畸形态不进隔离链，如实报。
func msixPathExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("路径存在但不是目录: %s", path)
	}
	return true, nil
}

// msixOrphanInventory 闸③指纹清点：整目录递归枚举文件数/总字节/相对路径
// 样本（前 orphanSampleCap 条）。任何枚举错误如实上抛（清点失败=不留指纹=
// 拒动，绝不带盲区动手）。
func msixOrphanInventory(dir string) (*orphanInventory, error) {
	inv := &orphanInventory{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		inv.files++
		inv.bytes += info.Size()
		if rel, rerr := filepath.Rel(dir, p); rerr == nil {
			if len(inv.entries) < orphanSampleCap {
				inv.entries = append(inv.entries, rel)
			} else {
				inv.truncated = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// orphanRegisteredAllUsers 闸①的通道侧：要求 packages 通道实现可选探测
// 能力；未实现（其他平台/测试替身）与探测失败同为 error——宁可拒动。
func (s *TranslucentTBService) orphanRegisteredAllUsers(ctx context.Context) (bool, error) {
	prober, ok := s.packages.(msixAllUsersProber)
	if !ok {
		return false, errors.New("系统包管理通道不支持全用户（-AllUsers）在册探测")
	}
	return prober.IsRegisteredAllUsers(ctx, msixIdentity)
}

// probeMsixOrphanData GetMsixState 的孤儿只读探测（无删除副作用）。
// 成本序与隔离主链相反——先一次 os.Stat 查目录在场，不在场连 PowerShell
// 都不起（从未有过打包版的机器零额外开销）；在场才花全用户注册查询。
// 只认包族名精确目录，<族名>.orphan-* 隔离备份不在射程。任何一环无法
// 确证（fs 报错/探测失败/能力缺席）一律判"无孤儿"——状态面宁缺毋滥，
// 误报会弹出指向不存在之物的清理钮；真正的动手前判定由 cleanMsixOrphanData
// 的三道闸再走一遍全量。
func (s *TranslucentTBService) probeMsixOrphanData(ctx context.Context) (bool, string) {
	h := s.orphan.resolve()
	dir, err := h.dir()
	if err != nil {
		slog.Debug("translucenttb 孤儿数据探测：路径组装失败", "error", err)
		return false, ""
	}
	ok, err := h.exists(dir)
	if err != nil {
		slog.Debug("translucenttb 孤儿数据探测：目录检查失败", "error", err)
		return false, ""
	}
	if !ok {
		return false, ""
	}
	registered, err := s.orphanRegisteredAllUsers(ctx)
	if err != nil || registered {
		slog.Debug("translucenttb 孤儿数据探测：全用户注册无法证明无册", "error", err, "registered", registered)
		return false, ""
	}
	return true, dir
}

// cleanMsixOrphanData 孤儿数据改名隔离主链。三道闸全过才动，任何一闸不过
// 即拒并回中文如实话（闸序按授权语义 ①注册 ②在场 ③指纹）：
//
//	① 全用户在册闸：Get-AppxPackage -AllUsers 查得任何注册即拒——"包仍在册
//	  请直接卸载"，在册包的数据是活数据不是孤儿；能力缺席/探测失败同样拒
//	  （无法证明无注册不许动手）。探测错误按值并入话术不再进归因表：那是
//	  查询通道的错误码，裹进隔离话术里会开出"缺 Store"等不相干指引。
//	② 目录在场闸：唯一精确路径不在场 = 无事可做，拒空转。
//	③ 指纹闸：改名前整目录清点并 slog.Info 留档（文件数/总字节/样本清单，
//	  对齐机主"删除前留指纹"习惯——隔离虽可逆也要有现场凭据）；清点失败
//	  即拒动。
//
// 落点名 <族名>.orphan-YYYYMMDD，同日二次占用退到 -HHMMSS，再占用拒撞名。
// 成功后 slog.Warn 一行审计（from/to/清单数字）。
func (s *TranslucentTBService) cleanMsixOrphanData(ctx context.Context) (string, error) {
	registered, err := s.orphanRegisteredAllUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("孤儿数据隔离取消：无法确认打包版无注册（全用户在册探测失败）: %v", err)
	}
	if registered {
		return "", errors.New("TranslucentTB 打包版包仍在册，请先直接卸载（孤儿隔离不动在册包的数据）")
	}

	h := s.orphan.resolve()
	dir, err := h.dir()
	if err != nil {
		return "", err
	}
	present, err := h.exists(dir)
	if err != nil {
		return "", fmt.Errorf("检查孤儿数据目录失败: %w", err)
	}
	if !present {
		return "", fmt.Errorf("未检测到孤儿数据目录（%s 不在场）——没有可隔离的残留；若安装仍失败请按回执指引排查", dir)
	}

	inv, err := h.walk(dir)
	if err != nil {
		return "", fmt.Errorf("改名隔离前指纹清点失败，已拒绝隔离（目录保持原样便于排查）: %w", err)
	}
	slog.Info("translucenttb 打包版孤儿数据隔离前指纹留档",
		"path", dir, "files", inv.files, "bytes", inv.bytes, "entries", inv.logField())

	target, err := s.orphanQuarantineTarget(dir, h)
	if err != nil {
		return "", err
	}
	if err := h.rename(dir, target); err != nil {
		return "", fmt.Errorf("孤儿数据改名隔离失败（旧数据可能仍被 explorer 等进程持柄占用）: %w", err)
	}
	slog.Warn("translucenttb 打包版孤儿数据已改名隔离（备份可手动删除）",
		"from", dir, "to", target, "files", inv.files, "bytes", inv.bytes)
	return target, nil
}

// orphanQuarantineTarget 隔离备份落点名：<dir>.orphan-YYYYMMDD（机主实证
// 形制）；同日已占用退到 <dir>.orphan-YYYYMMDD-HHMMSS；仍占用/状态查询
// 失败按撞名拒动（绝不覆盖既有备份）。
func (s *TranslucentTBService) orphanQuarantineTarget(dir string, h orphanHooks) (string, error) {
	now := h.now()
	candidates := []string{
		dir + ".orphan-" + now.Format("20060102"),
		dir + ".orphan-" + now.Format("20060102-150405"),
	}
	for _, cand := range candidates {
		taken, err := h.exists(cand)
		if err != nil || taken {
			continue
		}
		return cand, nil
	}
	return "", fmt.Errorf("隔离备份落点撞名（%s.orphan-* 均已占用），请稍后重试", filepath.Base(dir))
}

// CleanMsixOrphan 打包版孤儿应用数据的受控清理 RPC（经调用门；命名沿用
// 任务书冻结的动词语面，实际策略为改名隔离——见文件头与 cleanMsixOrphanData）。
// 唯一可动路径由包族名常量拼接（零入参），三道闸缺一不动；失败/无事可
// 做/仍受阻都回中文如实话，成功时隔离落点已随 WARN 审计入账（话术里
// 前端按「.orphan-日期 备份」口径如实转述）。
func (s *TranslucentTBService) CleanMsixOrphan() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), msixQueryTimeout)
	defer cancel()
	if _, err := s.cleanMsixOrphanData(ctx); err != nil {
		slog.Warn("translucenttb 打包版孤儿数据清理未执行", "summary", strings.Join(strings.Fields(err.Error()), " "))
		return err
	}
	return nil
}
