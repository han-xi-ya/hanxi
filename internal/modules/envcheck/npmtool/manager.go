package npmtool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/history"
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

// historyStore 为装配根注入的统一历史存储（nil=未接线，静默不记）。
// 本包操作是异步终态型动作（Q2：动作类全记），注入缝做成包级变量而非
// service 字段：runOperation 走的是包级函数链（Install/Upgrade/Uninstall 亦如此）。
var historyStore *history.Store

// SetHistory 接线统一历史记录（envcheck 桶）。只记动作摘要，绝不存 npm 原始日志行。
func SetHistory(s *history.Store) { historyStore = s }

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
		recordOutcome(op, message, false)
		return
	}
	message := fmt.Sprintf("%s %s完成", op.display, kindText(op.kind))
	emitProgress(OperationProgress{
		OperationID: op.id, ToolID: op.toolID, Kind: op.kind,
		Stage: "done", Message: message, Terminal: true, Success: true,
	})
	notify.Success(moduleID, op.display+" 操作完成", message, navigateURL)
	recordOutcome(op, message, true)
}

// recordOutcome npm 动作终态落统一历史（Q2：动作类全记）。
// 只记自组摘要，绝不存 npm 输出正文（runNpm 的 tail 返回即弃——日志行可能含
// 私有源凭据，且原始日志已走 envcheck:npm-tool-log 事件流供前端当场查看）；
// err 通道仅 exec 包装文本（"npm … 执行失败: exit status"形态），无凭据面。
func recordOutcome(op *operationState, message string, success bool) {
	if historyStore == nil {
		return
	}
	extra := "npm-" + op.kind
	if !success {
		extra += "|fail"
	}
	_ = historyStore.Save(history.Record{
		FuncType: moduleID,
		Summary:  message,
		Input:    op.toolID,
		Extra:    extra,
	})
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
