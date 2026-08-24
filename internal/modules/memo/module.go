package memo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/extapi"
	"hubkit/internal/settings"
)

const ID = "memo"

// Module 极客随手记模块
type Module struct {
	svc *MemoService
}

// New 实例化模块
func New(paths *settings.Paths) (extapi.Module, error) {
	svc, err := NewMemoService(paths)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// GetService 获取底层 Service 引用 (方便跨模块直接交互)
func (m *Module) GetService() *MemoService {
	return m.svc
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "极客随手记",
		Version:     "0.1.0",
		Description: "极轻量本地持久化备忘录与临时代码片段站，支持标签云、置顶与敏感脱敏",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "随手备忘录",
		Icon:    "📝",
		Route:   "/ext/memo",
		Section: extapi.SectionExt,
		Order:   36,
	}}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{}
}

func (m *Module) Protocol() int { return 1 }

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	return nil
}

func (m *Module) IsInitialized() bool {
	return true
}
