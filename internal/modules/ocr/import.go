package ocr

// ---------- 组件导入（拖放 / 对话框） ----------
//
// 两条落位通道并存：exe/目录=引用式（校验通过即指向，不复制，旧行为不动）；
// .zip 引擎安装包=托管式（F7，契约校验后解压落位 versions/，见 hostedsvc.go）。

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// minSingleExeBytes 单文件版 hanxi-ocr.exe 体积下限（v0.3.0 实测 ≈48MB，
// 目录版启动器仅 ~9MB 且必须与引擎文件同级才能独立运行）。
const minSingleExeBytes = 35 << 20

// ImportServiceExe 导入用户提供的组件，按文件/目录分流（计划 §5.3）：
//   - 文件 → 微信版校验（文件名/体积/布局不变），登记为微信引擎；
//   - 目录 → PP-OCR 开源引擎包（含 manifest.json 自检），转 ImportPaddleDir。
//
// 校验失败不置 error（属业务结果），统一折进 DropResult.Message 并广播
// ocr:file-drop-result 事件；取消对话框等无操作场景不发事件。
func (s *OcrService) ImportServiceExe(srcPath string) (DropResult, error) {
	p := strings.TrimSpace(srcPath)
	if p == "" {
		return s.dropResultFail("import", "未收到文件路径"), nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return s.dropResultFail("import", "路径解析失败: "+err.Error()), nil
	}
	// 目录形态只可能是开源引擎包（微信版为单文件 exe），先分流再走文件校验链
	if st, e := os.Stat(abs); e == nil && st.IsDir() {
		return s.ImportPaddleDir(abs)
	}
	// 进程名扫描与上游自动发现均锚定该文件名，改名件直接拒收（防导入不可托管的假件）
	if !strings.EqualFold(filepath.Base(abs), serviceExeName) {
		return s.dropResultFail("import", fmt.Sprintf("只能导入名为 %s 的文件或引擎目录（收到：%s），请勿改名", serviceExeName, filepath.Base(abs))), nil
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return s.dropResultFail("import", "文件不存在: "+abs), nil
	}
	if st.Size() < minSingleExeBytes {
		// 疑似 v0.2 目录版启动器：同级必须有引擎文件，否则导入后必然启动失败
		if _, e := os.Stat(filepath.Join(filepath.Dir(abs), engineDLLName)); e != nil {
			return s.dropResultFail("import", fmt.Sprintf(
				"该文件仅 %.1f MB 且同级没有引擎文件，无法独立运行（v0.3 起请收发单文件版 %s，约 48 MB）", float64(st.Size())/(1<<20), serviceExeName)), nil
		}
	}
	if err := s.store.SetEnginePath(EngineWechat, abs); err != nil {
		return DropResult{}, err
	}
	s.refresh()
	res := DropResult{
		Kind: "import", Ok: true, ExePath: abs,
		Message: fmt.Sprintf("已导入 %s（%.1f MB），正在启动服务", serviceExeName, float64(st.Size())/(1<<20)),
	}
	s.emitDropResult(res)
	return res, nil
}

// ImportPaddleDir 导入 PP-OCR 开源引擎目录（引用式登记，不复制）：
// 目录须同时含 hanxi-ocr.exe（契约文件名与微信版同名，计划 §3）与 manifest.json
// （组件自检清单，读其 version 字段登记版本）。登记成功后切 active=paddle——
// 在跑的托管实例会停旧起新，外部实例占位时只登记不切换（不越权）。
// 回执与事件复用 ocr:file-drop-result（Kind=import）。
func (s *OcrService) ImportPaddleDir(srcDir string) (DropResult, error) {
	p := strings.TrimSpace(srcDir)
	if p == "" {
		return s.dropResultFail("import", "未收到目录路径"), nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return s.dropResultFail("import", "路径解析失败: "+err.Error()), nil
	}
	if st, e := os.Stat(abs); e != nil || !st.IsDir() {
		return s.dropResultFail("import", "引擎目录不存在: "+abs), nil
	}
	exe := filepath.Join(abs, serviceExeName)
	if !isRegularFile(exe) {
		return s.dropResultFail("import", fmt.Sprintf("不是有效的 PP-OCR 引擎目录：%s 内缺少 %s", abs, serviceExeName)), nil
	}
	version := ""
	if raw, e := os.ReadFile(filepath.Join(abs, manifestName)); e != nil {
		// manifest 缺失多半是把微信单文件件的目录误拖进来，给出分流指引
		return s.dropResultFail("import", fmt.Sprintf(
			"不是有效的 PP-OCR 引擎目录：%s 内缺少 %s（微信版组件请直接拖 %s 文件）", abs, manifestName, serviceExeName)), nil
	} else {
		var m struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(raw, &m) == nil {
			version = strings.TrimSpace(m.Version)
		}
	}
	if err := s.store.SetEnginePath(EnginePaddle, exe); err != nil {
		return DropResult{}, err
	}
	if err := s.store.SetEngineVersion(EnginePaddle, version); err != nil {
		return DropResult{}, err
	}

	// 登记即激活：切换语义（含停旧起新/外部不越权判定）收口在 SetActiveEngine
	outcome, err := s.SetActiveEngine(EnginePaddle)
	if err != nil {
		return s.dropResultFail("import", "PP-OCR 引擎已登记，但切换为当前引擎失败: "+err.Error()), nil
	}
	switched := outcome.Action == "switched" || outcome.Action == "already-active"
	msg := fmt.Sprintf("已登记 PP-OCR 引擎目录 %s", abs)
	if version != "" {
		msg += fmt.Sprintf("（v%s）", version)
	}
	if !switched {
		// 典型为 external-unmanaged：登记成功但未激活
		return s.dropResultFail("import", msg+"；但"+outcome.Message), nil
	}
	res := DropResult{Kind: "import", Ok: true, ExePath: exe, Message: msg + "；" + outcome.Message}
	s.emitDropResult(res)
	return res, nil
}

// ImportPaddleDirDialog 目录选择框式导入 PP-OCR 引擎包（系统文件夹框为原生
// 能力，拖放之外的通道；取消返回空回执）。选定后走 ImportPaddleDir 同一套校验。
func (s *OcrService) ImportPaddleDirDialog() (DropResult, error) {
	app := application.Get()
	if app == nil {
		return DropResult{}, fmt.Errorf("应用实例不可用")
	}
	dialog := app.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).
		SetTitle("选择 PP-OCR 开源引擎目录（含 hanxi-ocr.exe 与 manifest.json）")
	dir, err := dialog.PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dir) == "" {
		return DropResult{Kind: "import"}, nil // 用户取消，静默
	}
	return s.ImportPaddleDir(dir)
}

// ImportServiceExeDialog 对话框式导入（与拖放共用同一套校验）。取消返回空回执。
func (s *OcrService) ImportServiceExeDialog() (DropResult, error) {
	path, err := s.BrowseServiceExeDialog()
	if err != nil {
		return DropResult{}, err
	}
	if path == "" {
		return DropResult{Kind: "import"}, nil // 用户取消，静默
	}
	return s.ImportServiceExe(path)
}

// HandleNativeDrop Wails 主窗原生文件拖放入口（app.go 接线；只有落在 OcrView
// 标记 data-file-drop-target 元素上的文件才会到达）。分流：.zip → 托管安装包
// （F7：契约校验后自动解压落位）；目录 → PP-OCR 引擎包导入；.exe → 微信件导入；
// 图片 → 真实路径直接选为待识别对象（免走 dataURL 全量 IPC）；其余给中文提示。
func (s *OcrService) HandleNativeDrop(files []string) {
	if len(files) == 0 {
		return
	}
	src := strings.TrimSpace(files[0])
	ext := strings.ToLower(filepath.Ext(src))
	// F7 托管安装通道：.zip 引擎安装包（校验链与错误回执收口 InstallHostedZip）
	if ext == ".zip" {
		if _, err := s.InstallHostedZip(src); err != nil {
			slog.Warn("ocr 托管安装失败", "path", src, "err", err)
		}
		return
	}
	// 目录与 .exe 一律进导入分流（ImportServiceExe 内部按形态派发到
	// 微信件校验 / ImportPaddleDir 引擎包登记），其余按扩展名判图片
	if st, err := os.Stat(src); (err == nil && st.IsDir()) || ext == ".exe" {
		if _, err := s.ImportServiceExe(src); err != nil {
			slog.Warn("ocr 导入失败", "path", src, "err", err)
		}
		return
	}
	if ext != "" && imgExtOK[ext] {
		ref, err := s.InspectImage(src)
		if err == nil {
			s.emitDropResult(DropResult{Kind: "image", Ok: true, Image: &ref})
			return
		}
		s.dropResultFail("image", err.Error())
		return
	}
	s.dropResultFail("image", fmt.Sprintf("无法识别的拖放文件类型（%s）：支持图片文件、引擎安装包 .zip、%s 与 PP-OCR 引擎目录", ext, serviceExeName))
}

func (s *OcrService) dropResultFail(kind, msg string) DropResult {
	res := DropResult{Kind: kind, Ok: false, Message: msg}
	s.emitDropResult(res)
	return res
}

func (s *OcrService) emitDropResult(res DropResult) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ocr:file-drop-result", res)
	}
}
