package npmtool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/notify"
)

// moduleID 与通知路由：操作成败经系统通知回跳环境检测页。
const (
	moduleID    = "envcheck"
	navigateURL = "/ext/envcheck"
)

const (
	kindInstall   = "install"
	kindUpgrade   = "upgrade"
	kindUninstall = "uninstall"
)

// npm 全局树（%AppData%\npm 与全局 node_modules）同一时刻只允许一个写操作，
// 跨目录条目共享一把包级锁（nanazip operationMu/beginOperation 模式）。
var (
	operationMu sync.Mutex
	currentOp   *operationState
)

type operationState struct {
	id      string
	toolID  string
	display string
	kind    string
}

// Install 经 npm 全局安装目录工具（`npm install -g <pkg>@latest`，天然幂等）。
func Install(id string) (OperationAccepted, error) {
	return startOperation(id, kindInstall, func(s ToolSpec) []string {
		return []string{"install", "-g", s.Package + "@latest"}
	})
}

// Upgrade 升级到 registry 最新版（命令与 Install 相同，@latest 覆盖式安装）。
func Upgrade(id string) (OperationAccepted, error) {
	return startOperation(id, kindUpgrade, func(s ToolSpec) []string {
		return []string{"install", "-g", s.Package + "@latest"}
	})
}

// Uninstall 卸载目录工具（仅移除 npm 全局安装，配置目录不受影响）。
func Uninstall(id string) (OperationAccepted, error) {
	return startOperation(id, kindUninstall, func(s ToolSpec) []string {
		return []string{"uninstall", "-g", s.Package}
	})
}

// ActiveOperation 返回当前进行中操作的快照（无操作时 nil），供 Overview 恢复按钮忙碌态。
func ActiveOperation() *OperationProgress {
	operationMu.Lock()
	defer operationMu.Unlock()
	if currentOp == nil {
		return nil
	}
	return &OperationProgress{
		OperationID: currentOp.id,
		ToolID:      currentOp.toolID,
		Kind:        currentOp.kind,
		Stage:       "running",
		Message:     currentOp.display + kindText(currentOp.kind) + "进行中",
	}
}

// startOperation 同步三重校验（目录白名单、npm 存在、全局锁未被占），
// 通过即落锁起 goroutine 返回受理；任一失败同步 return error（前端当场报错）。
func startOperation(id, kind string, args func(ToolSpec) []string) (OperationAccepted, error) {
	spec, ok := Spec(strings.TrimSpace(id))
	if !ok {
		return OperationAccepted{}, fmt.Errorf("未知 npm 工具: %s", id)
	}
	if _, err := lookNpm("npm"); err != nil {
		return OperationAccepted{}, fmt.Errorf("未检测到可用的 npm，请先安装 Node.js 后再一键%s", kindText(kind))
	}
	op, err := beginOperation(spec, kind)
	if err != nil {
		return OperationAccepted{}, err
	}
	go runOperation(op, args(spec))
	return OperationAccepted{
		OperationID: op.id,
		Kind:        kind,
		Message:     fmt.Sprintf("%s %s已开始", spec.Display, kindText(kind)),
	}, nil
}

func beginOperation(spec ToolSpec, kind string) (*operationState, error) {
	operationMu.Lock()
	defer operationMu.Unlock()
	if currentOp != nil {
		return nil, fmt.Errorf("%s 正在执行%s，npm 全局树同一时刻仅支持一个操作", currentOp.display, kindText(currentOp.kind))
	}
	op := &operationState{
		id:      fmt.Sprintf("npmtool-%d", time.Now().UnixNano()),
		toolID:  spec.Command,
		display: spec.Display,
		kind:    kind,
	}
	currentOp = op
	return op, nil
}

func finishOperation(op *operationState) {
	operationMu.Lock()
	if currentOp == op {
		currentOp = nil
	}
	operationMu.Unlock()
}

func runOperation(op *operationState, args []string) {
	defer finishOperation(op)
	emitProgress(OperationProgress{
		OperationID: op.id, ToolID: op.toolID, Kind: op.kind,
		Stage: "started", Message: fmt.Sprintf("正在执行 npm %s", strings.Join(args, " ")),
	})

	_, err := runNpm(context.Background(), args, func(line string) {
		emitLog(OperationLog{OperationID: op.id, ToolID: op.toolID, Line: line})
	})

	if err != nil {
		message := fmt.Sprintf("%s %s失败：%v", op.display, kindText(op.kind), err)
		emitProgress(OperationProgress{
			OperationID: op.id, ToolID: op.toolID, Kind: op.kind,
			Stage: "error", Message: message, Terminal: true,
		})
		notify.Error(moduleID, op.display+" 操作失败", message, navigateURL)
		return
	}
	message := fmt.Sprintf("%s %s完成", op.display, kindText(op.kind))
	emitProgress(OperationProgress{
		OperationID: op.id, ToolID: op.toolID, Kind: op.kind,
		Stage: "done", Message: message, Terminal: true, Success: true,
	})
	notify.Success(moduleID, op.display+" 操作完成", message, navigateURL)
}

func kindText(kind string) string {
	switch kind {
	case kindInstall:
		return "安装"
	case kindUpgrade:
		return "升级"
	case kindUninstall:
		return "卸载"
	default:
		return kind
	}
}

// emitProgress / emitLog 推送事件；无 application 运行时（单测）安全跳过。
func emitProgress(progress OperationProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("envcheck:npm-tool-operation", progress)
	}
}

func emitLog(entry OperationLog) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("envcheck:npm-tool-log", entry)
	}
}
