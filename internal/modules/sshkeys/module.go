// Package sshkeys 内置模块：SSH 密钥管理与端点连接测试。
//
// 能力面（2026-10-22 一天内分两步建起来，**名字跟着实现走**，见 Info() 注释）：
//   - 密钥台账：扫 `~/.ssh` 按**内容**认钥匙，只回路径/算法/公钥指纹/口令三态，不读私钥内容；
//   - 密钥写操作：生成密钥对、加/换/去口令、私钥与配套 `.pub` 成对删除（`keyops` 子包）；
//   - 公钥部署：一次性密码换会话 + 固定远端脚本 + **公钥只走 stdin**，配 host key 的
//     TOFU 钉定（`deploy` 子包；写操作不能沿用探测那套"不校验指纹"的宽松口径）；
//   - 主机清单与导出：账本在数据根，ssh 读的 `~/.ssh/hanxi.conf` 是**渲染产物**，
//     只在用户显式点「同步给 ssh」时才写（可撤销），见 export.go 的架构张力说明；
//   - 连接测试：`CheckSSH` 版本横幅探测（不碰凭据、不在对端留记录）与 `CheckSSHAuth`
//     一次性密码认证（显式动作），两件均自 portscan 搬入。
//
// **为什么从 portscan 搬出来**：这两件事与"端口扫描"没有归属关系——扫描是无凭据的
// 并发探测，而凭据测试会真的登录、会在对端 sshd 日志留下 Failed password、可能触发
// fail2ban。它们长在同一个页面里，只是历史上的就近堆放；portscan 的 Scanner 又刻意
// 无状态，把带凭据的能力挂在它身上，等于让"扫描引擎"承担与扫描无关的凭据职责。
//
// **本模块刻意不做的两件事（负裁决，后人勿当欠账顺手补上）**：
//
// 一、不实现 extapi.TrayCommandsProvider。托盘候选的语义是"启动/激活外部应用"
// （见 extapi.Module 契约注释），本模块是纯探测动作、没有"打开一个界面"的常驻实体，
// 挂常驻托盘项没有使用价值。这不是遗漏——可选契约的缺席全链路零报错
// （docs/TROUBLESHOOTING.md #107：termora/windterm 就是这样静默漏掉的），故把裁决
// 显式记在这里。**且切勿"顺手"实现该签名**：internal/app/catalog_contract_test.go
// 把源码证据与 catalog 入口互为充要，一旦包里出现 TrayCommands() 而 catalog 未 regen，
// 那道锁会直接红。
//
// 二、不在 MCP 面注册任何工具。本模块的凭据测试是**会向对端发起登录尝试**的主动动作，
// 而 internal/mcp/mcp.go 的红线一规定"查询类严格只读；破坏族唯一豁免 portkill，
// 系经显式升版、四道闸门执行（升版论证见 internal/mcp/guarded.go 文件头）"。
// 把它挂进 MCP 等于让 AI 能对任意主机发起登录尝试，需要机主再次显式升版红线，
// 不是工程顺手事项。
package sshkeys

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "sshkeys"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *SshKeysService
}

// New 在 app 装配期创建模块。构造无 IO——探针无状态，暂不触盘。
func New() extapi.Module {
	return &Module{
		svc: NewSshKeysService(extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID: ID,
		// 名称与描述只写**当前真有的能力**。本模块 2026-10-22 当天从「SSH 连接测试」
		// →「SSH 密钥与连接」→「SSH 密钥管理」走了两步：**第二步的前提是四件写动作
		// 都已落地且界面上有入口**（生成、加/换/去口令、成对删除、公钥部署，外加主机
		// 清单与导出对账）。先建能力再改名，不反过来——本仓刚为一句超前文案清过档：
		// portscan 的 Description 曾写「集成 Nmap 深度服务指纹识别」而仓内无该引擎，
		// 一句话经 cataloggen 流入 catalog、fixture 与前端 Info 面板三处，2026-10-12
		// 才清掉（见其包注释与 TROUBLESHOOTING #112）。名字比实现大，就是这个病。
		Name: "SSH 密钥管理",
		// Description 经 scripts/cataloggen 流入 internal/app/catalog.go、
		// scripts/fixture/module_catalog.json 与前端 Info 面板，改此处须 regen 两产物。
		Description: "本机 SSH 密钥台账与写操作（生成、加/换/去口令、成对删除；不读取私钥内容）、公钥一键部署（带 host key 钉定）、主机清单与导出对账，以及端点连接测试（版本横幅探测 / 一次性密码认证）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口。Order 26 紧跟亲缘最近的"网络测试"（portscan=25），
// piik=27 之前——空隙由 2026-10-22 实查 network 组现行 Order 得出。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "SSH 密钥管理",
		Route:   "/ext/sshkeys",
		Icon:    "i:server",
		Section: extapi.SectionExt,
		Order:   26,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error { return nil }

// OnDestroy 无需收口：探针是同步一次性 RPC，不持有在途任务、不起 goroutine，
// 停用时的 drain 由调用门 lease 归零保证（对照 portscan 的 stopScan 内部版）。
func (m *Module) OnDestroy() error { return nil }

// IsInitialized 本模块 OnInit 无副作用，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }
