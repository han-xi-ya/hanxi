// Package gitconfig 读取 git 全局配置（git config --global --list）：
// 只读探测，敏感条目按脱敏词表就地打码后才离开本包，未安装/未配置/读取失败如实区分。
package gitconfig

// State git 全局配置的读取状态（前端按态出文案，失败不退化为空列表糊弄）。
type State string

const (
	StateConfigured   State = "configured"    // 读取成功且含配置条目
	StateUnconfigured State = "unconfigured"  // git 已安装，但全局配置不存在或无任何条目
	StateMissing      State = "not-installed" // PATH 中找不到 git
	StateError        State = "error"         // 非零退出/超时等，读取未能完成（detail 说明）
)

// Entry 单条配置键值对。Key 与 Value 均已过 Redact 脱敏，可安全出网与渲染。
type Entry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Overview GitGlobalConfig 的 Wails 绑定返回结构：状态 + 已脱敏条目 + 失败说明。
type Overview struct {
	State  State   `json:"state"`
	Items  []Entry `json:"items,omitempty"`
	Detail string  `json:"detail,omitempty"`
}
