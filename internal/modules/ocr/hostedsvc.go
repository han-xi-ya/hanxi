package ocr

// ---------- 前端 API：F7 托管版本面（列表 / 安装 / 卸载） ----------
//
// 引擎 zip 安装件（契约见 hosted.go）经本面进入托管版本树：安装=校验+落位+登记
// （指向 versions/hanxi-ocr/<engine>-<version>/ 内入口），与引用式导入共用
// ocr:file-drop-result（Kind=import）回执；卸载=删版本目录，登记件悬空时
// 复位自动发现（解析链自愈接管）。在用拒卸：托管实例运行中且生效解析落在
// 目标版本目录时拒绝并给中文指引，防"卸掉正跑的组件"自断。
// wechat 包只认本地拖入/对话框选取，不存在任何下载或公开索引入口（F7 三闸）。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/ocr/instance"
)

// ListHostedVersions 托管版本树清单（engineOrder 次序、engine 内版本降序），
// 并标记当前生效版本（活跃引擎解析结果所在目录）。未安装返回空列表。
func (s *OcrService) ListHostedVersions() ([]HostedVersion, error) {
	if s.hosted == nil {
		return nil, fmt.Errorf("托管版本树未接线")
	}
	list := s.hosted.list()
	if exe, _, err := s.resolveActiveExe(); err == nil {
		effectiveDir := hostedDirOfExe(s.hosted.versionsRoot, exe)
		for i := range list {
			list[i].Effective = effectiveDir != "" && list[i].Dir == effectiveDir
		}
	}
	return list, nil
}

// InstallHostedZip 安装引擎 zip 包（拖放/对话框共用同一校验链）。业务失败
// 折进 DropResult.Message 并广播 ocr:file-drop-result（Kind=import）。
// 成功后：登记该引擎指向托管入口并记录版本；paddle 沿用"登记即激活"语义
// （停旧起新/外部不越权判定收口 SetActiveEngine），wechat 仅登记不切换。
func (s *OcrService) InstallHostedZip(srcPath string) (DropResult, error) {
	if s.hosted == nil {
		return DropResult{}, fmt.Errorf("托管版本树未接线")
	}
	p := strings.TrimSpace(srcPath)
	if p == "" {
		return s.dropResultFail("import", "未收到安装包路径"), nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return s.dropResultFail("import", "路径解析失败: "+err.Error()), nil
	}
	m, _, err := inspectHostedZip(abs)
	if err != nil {
		return s.dropResultFail("import", err.Error()), nil
	}
	// 同版本重装：在用的旧入口先拒（Windows 文件锁会让换入必然失败）
	target := filepath.Join(s.hosted.versionsRoot, hostedVersionDirName(m.Engine, m.Version))
	if st, e := os.Stat(target); e == nil && st.IsDir() {
		if s.hostedInUse(target) {
			return s.dropResultFail("import", fmt.Sprintf(
				"%s v%s 正在被识别服务使用，无法覆盖安装；请先停止服务（或切换到其他引擎）后重新拖入",
				engineLabel(m.Engine), m.Version)), nil
		}
	}
	hv, err := s.hosted.installZip(abs)
	if err != nil {
		return s.dropResultFail("import", err.Error()), nil
	}
	if err := s.store.SetEnginePath(hv.Engine, hv.ExePath); err != nil {
		return DropResult{}, err
	}
	if err := s.store.SetEngineVersion(hv.Engine, hv.Version); err != nil {
		return DropResult{}, err
	}

	msg := fmt.Sprintf("已安装托管引擎 %s v%s（%s）", engineLabel(hv.Engine), hv.Version, fmtMB(hv.Size))
	if hv.Engine == EnginePaddle {
		// 登记即激活（与目录式 ImportPaddleDir 同语义）；外部实例占位时只登记不越权
		outcome, err := s.SetActiveEngine(EnginePaddle)
		if err != nil {
			return s.dropResultFail("import", "PP-OCR 引擎已安装，但切换为当前引擎失败: "+err.Error()), nil
		}
		if outcome.Action != "switched" && outcome.Action != "already-active" {
			return s.dropResultFail("import", msg+"；但"+outcome.Message), nil
		}
		msg += "；" + outcome.Message
	}
	s.refresh()
	res := DropResult{Kind: "import", Ok: true, ExePath: hv.ExePath, Message: msg}
	s.emitDropResult(res)
	return res, nil
}

// InstallHostedZipDialog 对话框式安装（与拖放同一校验链；取消静默）。
func (s *OcrService) InstallHostedZipDialog() (DropResult, error) {
	app := application.Get()
	if app == nil {
		return DropResult{}, fmt.Errorf("应用实例不可用")
	}
	dialog := app.Dialog.OpenFile().CanChooseFiles(true).CanChooseDirectories(false).
		SetTitle("选择引擎安装包（hanxi-ocr-<engine>-<version>.zip，旁挂同名 .sha256）")
	dialog.AddFilter("引擎安装包 (*.zip)", "*.zip")
	path, err := dialog.PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return DropResult{Kind: "import"}, nil // 用户取消，静默
	}
	return s.InstallHostedZip(path)
}

// UninstallHostedVersion 卸载一个托管版本（删 versions/hanxi-ocr/<engine>-<version>/）。
// 在用拒卸：托管/启动中实例的生效解析正落在该目录 → 拒绝并给指引。
// 成功后该引擎登记件若指向被删目录则复位自动发现（解析链自愈/落回旧锚点）。
func (s *OcrService) UninstallHostedVersion(engine, version string) (ControlOutcome, error) {
	if s.hosted == nil {
		return ControlOutcome{}, fmt.Errorf("托管版本树未接线")
	}
	id := strings.ToLower(strings.TrimSpace(engine))
	if !isKnownEngine(id) {
		return ControlOutcome{}, fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", id)
	}
	v := strings.TrimSpace(version)
	if err := validateVersionToken(v); err != nil {
		return ControlOutcome{}, err
	}
	target := filepath.Join(s.hosted.versionsRoot, hostedVersionDirName(id, v))
	if s.hostedInUse(target) {
		return ControlOutcome{Action: "refused-in-use", Message: fmt.Sprintf(
			"%s v%s 正在被识别服务使用，无法卸载；请先停止服务（或切换到其他引擎）再卸载",
			engineLabel(id), v)}, nil
	}
	dir, err := s.hosted.remove(id, v)
	if err != nil {
		return ControlOutcome{}, err
	}
	// 登记悬空复位：指向被删目录的登记件改回自动发现（树内还有别的版本则解析自愈）
	if stored := s.store.GetEnginePath(id); hostedDirOfExe(s.hosted.versionsRoot, stored) == dir {
		if err := s.store.SetEnginePath(id, ""); err != nil {
			return ControlOutcome{}, err
		}
	}
	s.refresh()
	return ControlOutcome{Action: "uninstalled",
		Message: fmt.Sprintf("已卸载 %s v%s", engineLabel(id), v)}, nil
}

// hostedInUse 目标版本目录是否正被在跑的托管实例使用（生效解析入口落在目录内）。
// 纯判定拆出便于单测；external 实例不归本引擎管，其 exe 是否同路径无从探知，
// 由删目录时的文件锁报错兜底。
func (s *OcrService) hostedInUse(targetDir string) bool {
	snap := s.engine.Snapshot()
	exe, _, err := s.resolveActiveExe()
	if err != nil {
		return false
	}
	return hostedExeInUse(snap.State, exe, targetDir)
}

func hostedExeInUse(state instance.State, activeExe, targetDir string) bool {
	if state != instance.StateRunning && state != instance.StateStarting {
		return false
	}
	if strings.TrimSpace(activeExe) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(filepath.Dir(activeExe)), filepath.Clean(targetDir))
}

func fmtMB(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
