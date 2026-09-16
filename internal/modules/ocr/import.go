package ocr

// ---------- 组件导入（拖放 / 对话框，引用式：校验通过即指向，不复制） ----------

import (
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

// ImportServiceExe 导入用户提供的组件：校验文件名/体积/布局后把服务路径指向它。
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
	// 进程名扫描与上游自动发现均锚定该文件名，改名件直接拒收（防导入不可托管的假件）
	if !strings.EqualFold(filepath.Base(abs), serviceExeName) {
		return s.dropResultFail("import", fmt.Sprintf("只能导入名为 %s 的文件（收到：%s），请勿改名", serviceExeName, filepath.Base(abs))), nil
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
	if err := s.store.SetExePath(abs); err != nil {
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
// 标记 data-file-drop-target 元素上的文件才会到达）。按扩展名分流：.exe → 组件
// 导入；图片 → 真实路径直接选为待识别对象（免走 dataURL 全量 IPC）；其余给中文提示。
func (s *OcrService) HandleNativeDrop(files []string) {
	if len(files) == 0 {
		return
	}
	src := strings.TrimSpace(files[0])
	ext := strings.ToLower(filepath.Ext(src))
	if ext == ".exe" {
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
	s.dropResultFail("image", fmt.Sprintf("无法识别的拖放文件类型（%s）：支持图片文件与 %s", ext, serviceExeName))
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
