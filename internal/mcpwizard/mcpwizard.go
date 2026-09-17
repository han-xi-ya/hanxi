// Package mcpwizard 是「AI 接入」设置分区（MCP，F4b）的 GUI 配置编辑半边：
// 探测 Claude Code / Codex / Cursor 三家客户端配置文件，按
// 「预览 → 确认 → 备份 → 原子写 → 复验 → 失败回滚」红线流程，写入/摘除
// hanxi MCP stdio server 启动条目（command = 当前 hanxi exe，args = ["mcp"]）。
//
// 边界（F4a/F4b 拆分）：本包不 import internal/mcp、不依赖无头 server 的任何
// 编译期符号——`hanxi mcp` 在写入链里只是将要写进配置的运行时字符串，安装前
// 自检（selfcheck.go）也只按线上协议自发帧握手（唯一软耦合是协议版本号字面量）。授权文件
// `<DataDir>/mcp/access.json`（字段按 PLAN_MCP §6：version + tools 四键）归
// F4a 引擎写，本包只读取呈现、绝不写。所有权回执 `<DataDir>/mcp/install.json`
// 由本包读写，记录上次写入指纹，供四态判定与"同名条目被用户改过即拒绝"。
//
// fail-closed 裁定（PLAN_MCP §2.5 / §8 拍板 4、5，照办不外溢）：
//  1. 不可安全合并的文件（JSONC 注释、UTF-8 BOM、JSON/TOML 解析失败、
//     mcpServers 形状异常）一律拒绝自动修改，只给手动片段；
//  2. 同名条目被用户改过（指纹与回执不符）时，安装与卸载都拒绝，绝不静默覆盖；
//  3. 预览出令牌后文件被第三方改动（确认时校验失败）则整体拒绝，须重新预览。
//
// 写入格式策略：JSON 客户端顶层键原样保留（map[string]json.RawMessage 外科
// 合并），缩进跟随原文件探测；Codex 用成对哨兵 `# >>> hanxi mcp >>>` /
// `# <<< hanxi mcp <<<` 末尾追加托管区块、写后 BurntSushi/toml 复验、绝不
// 重排用户文件，哨兵出现 0 对或 ≥2 对判冲突拒绝整块删除。
package mcpwizard

// 写入条目的身份锚：所有客户端统一用 "hanxi" 作 server 名，参数固定 ["mcp"]
// （位置参数形态优于 -mode=mcp，PLAN_MCP §2.1；`hanxi mcp` 运行时由 F4a 提供）。
const entryName = "hanxi"

var serverArgs = []string{"mcp"}

// 状态四态（PLAN §2.5）+ 呈现扩展态 blocked（不可安全合并，fail-closed 专用）。
// 前端按字面量映射徽章。
const (
	stateNotInstalled = "not-installed"
	stateInstalled    = "installed"
	stateNeedsRepair  = "needs-repair"
	stateConflict     = "conflict"
	stateBlocked      = "blocked"
)
