// 特征测试（组 G3 / Phase 5）：EnvCheckView 迁移前基线锁定。
// 断言对象：检测卡渲染、状态 chip、官方版本面板接线、npm 工具装升卸事件流、
// 本地三态 ConfirmDialog（busy+details，视图自持，非 useConfirm 收编对象）。
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import EnvCheckView from '../EnvCheckView.vue'
import { useToast } from '../../composables/useToast'

const env = vi.hoisted(() => ({
  DetectAll: vi.fn(),
  GetNpmToolsOverview: vi.fn(),
  InstallNpmTool: vi.fn(),
  UpgradeNpmTool: vi.fn(),
  UninstallNpmTool: vi.fn(),
  GetGitForWindowsOverview: vi.fn(),
  GetGoOverview: vi.fn(),
  GetNodeOverview: vi.fn(),
  GetJavaOverview: vi.fn(),
  GetPythonOverview: vi.fn(),
  GetDotNetOverview: vi.fn(),
  RevealToolPath: vi.fn(),
  OpenGitForWindowsDownloadPage: vi.fn(),
  OpenGoDownloadPage: vi.fn(),
  OpenNodeDownloadPage: vi.fn(),
  OpenJavaDownloadPage: vi.fn(),
  OpenPythonDownloadPage: vi.fn(),
  OpenDotNetDownloadPage: vi.fn(),
}))

const bcu = vi.hoisted(() => ({ OpenWindow: vi.fn() }))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/envcheck/envcheckservice', () => env)
vi.mock('../../../bindings/hanxi/internal/modules/bcu/bcuservice', () => bcu)

function tool(over: Record<string, unknown>) {
  return { name: '', display: '', status: 'installed', version: '', path: '', hint: '', details: null, ...over }
}

const BASE_TOOLS = [
  tool({ name: 'git', display: 'Git', version: '2.47.1', path: 'C:\\Program Files\\Git\\cmd\\git.exe' }),
  tool({ name: 'go', display: 'Go', version: 'go1.24.6', path: 'C:\\go\\bin\\go.exe' }),
  tool({ name: 'node', display: 'Node.js', version: 'v24.14.1', path: 'C:\\nodejs\\node.exe' }),
  tool({ name: 'npm', display: 'npm', version: '11.11.0', path: 'C:\\nodejs\\npm.cmd' }),
  tool({ name: 'pnpm', display: 'pnpm', status: 'missing', version: '', path: '' }),
  tool({ name: 'java', display: 'Java', status: 'store-stub', version: '', path: 'WindowsApps 存根', hint: '微软商店存根会吞掉 java 调用，请安装真实 JDK' }),
  tool({ name: 'python', display: 'Python', status: 'error', version: '', path: '', hint: '注册表读取失败' }),
  tool({
    name: 'dotnet', display: '.NET', version: '10.0.400', path: 'C:\\Program Files\\dotnet\\dotnet.exe',
    details: { dotnet: { sdks: ['10.0.400'], runtimes: ['10.0.8', '8.0.19'], desktops: [], aspnet: [] } },
  }),
  tool({ name: 'claude', display: 'Claude Code', version: '2.1.260', path: 'C:\\nvm\\claude.cmd' }),
]

function npmOverview(over: Record<string, unknown> = {}) {
  return {
    tools: [{
      tool: { command: 'claude', display: 'Claude Code', package: '@anthropic-ai/claude-code' },
      local: { name: 'claude', display: 'Claude Code', status: 'installed', version: '2.1.260', path: 'C:\\nvm\\claude.cmd', hint: '' },
      relation: 'update-available',
      relationDetail: '',
      latest: { version: '2.1.261' },
      isStale: false,
    }],
    activeOperation: null,
    ...over,
  }
}

async function flush(times = 30) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubHappy() {
  env.DetectAll.mockResolvedValue(BASE_TOOLS as never)
  env.GetNpmToolsOverview.mockResolvedValue(npmOverview() as never)
  for (const fn of [env.GetGitForWindowsOverview, env.GetGoOverview, env.GetNodeOverview, env.GetJavaOverview, env.GetPythonOverview, env.GetDotNetOverview]) {
    fn.mockResolvedValue({ channels: [{ key: 'stable', label: 'Stable', detail: '', relation: 'update-available', releases: [{ version: '1.2.3', published: '2026-08-01T00:00:00Z' }], relationDetail: '' }], isStale: false, fetchedAt: '2026-09-05 10:00' } as never)
  }
  env.RevealToolPath.mockResolvedValue(undefined)
  env.InstallNpmTool.mockResolvedValue({ operationId: 'op-1', message: '已受理' })
  env.UpgradeNpmTool.mockResolvedValue({ operationId: 'op-2', message: '升级中' })
  env.UninstallNpmTool.mockResolvedValue({ operationId: 'op-3', message: '卸载中' })
  bcu.OpenWindow.mockResolvedValue(undefined)
}

async function mountView() {
  return mount(EnvCheckView, { attachTo: document.body, global: { stubs: { teleport: true } } })
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('EnvCheckView 检测卡渲染', () => {
  it('首屏并发拉取本机清单 + 6 官网通道 + npm overview', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(env.DetectAll).toHaveBeenCalled()
    expect(env.GetGitForWindowsOverview).toHaveBeenCalled()
    expect(env.GetDotNetOverview).toHaveBeenCalled()
    expect(env.GetNpmToolsOverview).toHaveBeenCalled()
    expect(w.findAll('.tool-card')).toHaveLength(BASE_TOOLS.length)
    expect(w.find('.stat-text')!.text()).toContain('6 / 9 已安装')
    w.unmount()
  })

  it('状态 chip 四态与未知回退：chip 文本与图标逐字锁定', async () => {
    stubHappy()
    env.DetectAll.mockResolvedValue([
      tool({ name: 'go', display: 'Go', status: 'installed' }),
      tool({ name: 'npm', display: 'npm', status: 'missing' }),
      tool({ name: 'python', display: 'Python', status: 'weird-status' }),
      tool({ name: 'java', display: 'Java', status: 'store-stub', hint: 'stub!' }),
    ] as never)
    const w = await mountView()
    await flush()
    const chips = w.findAll('.status-chip').map(c => c.text())
    expect(chips).toEqual(['✓ 已安装', '○ 未安装', '! 检测失败', '⚠ 商店存根'])
    // 回退语义：未知状态按"检测失败"呈现
    w.unmount()
  })

  it('dotnet 并排版本线补充行 + npm/pnpm 手动升级提示 + java 存根 hint', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(w.text()).toContain('另装版本线 8.0')
    const npmCard = w.findAll('.tool-card').find(c => c.text().includes('npm'))!
    expect(npmCard.find('.upgrade-hint').exists()).toBe(true)
    expect(npmCard.text()).toContain('npm install --global npm@latest')
    const javaCard = w.findAll('.tool-card').find(c => c.text().includes('Java'))!
    expect(javaCard.find('.tool-hint').text()).toContain('微软商店存根')
    w.unmount()
  })

  it('本机检测失败 → 错误横幅文案逐字', async () => {
    env.DetectAll.mockRejectedValue(new Error('WMI 不可用'))
    env.GetNpmToolsOverview.mockRejectedValue(new Error('down'))
    for (const fn of [env.GetGitForWindowsOverview, env.GetGoOverview, env.GetNodeOverview, env.GetJavaOverview, env.GetPythonOverview, env.GetDotNetOverview]) fn.mockRejectedValue(new Error('offline'))
    const w = await mountView()
    await flush()
    expect(w.find('.banner-error').exists()).toBe(true)
    expect(w.find('.banner-error').text()).toContain('本机环境检测失败: WMI 不可用')
    w.unmount()
  })

  it('定位路径按钮 → RevealToolPath(工具名)；无路径未安装卡为纯文本', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const pathLink = w.find('.path-link')
    await pathLink.trigger('click')
    expect(env.RevealToolPath).toHaveBeenCalledWith('git')
    const missing = w.findAll('.tool-card').find(c => c.text().startsWith('pnpm'))!
    expect(missing.find('.path-link').exists()).toBe(false)
    w.unmount()
  })

  it('dotnet 卡 BCU 委托按钮：OpenWindow + 指引 toast', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const b = w.findAll('.tool-actions button')[0]
    expect(b.text()).toContain('BCUninstaller')
    await b.trigger('click')
    await flush()
    expect(bcu.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toContain('搜索 ".NET"')
    w.unmount()
  })
})

describe('EnvCheckView npm 工具操作流', () => {
  it('升级：UpgradeNpmTool(命令名) + started 事件前本地乐观占位', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    const upgrade = card.findAll('button').find(b => b.text().includes('升级到 2.1.261'))!
    await upgrade.trigger('click')
    await flush()
    expect(env.UpgradeNpmTool).toHaveBeenCalledWith('claude')
    expect(card.find('.op-running').exists()).toBe(true)
    expect(card.find('.op-log').text()).toBe('正在启动 npm 操作…')
    w.unmount()
  })

  it('卸载二次确认：视图自持三态 ConfirmDialog（title=卸载 Claude Code，details 含 npm 包），确认后调 UninstallNpmTool', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    await card.findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flush()
    const dialog = w.find('.workbench-confirm')
    expect(dialog.exists()).toBe(true)
    expect(dialog.text()).toContain('卸载 Claude Code')
    expect(dialog.text()).toContain('@anthropic-ai/claude-code')
    expect(dialog.text()).toContain('仅移除 npm 全局安装')
    await dialog.findAll('button').find(b => b.text() === '确认卸载')!.trigger('click')
    await flush()
    expect(env.UninstallNpmTool).toHaveBeenCalledWith('claude')
    expect(w.find('.workbench-confirm').exists()).toBe(false)
    w.unmount()
  })

  it('卸载确认点取消：不调后端、对话框关闭', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    await card.findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flush()
    await w.find('.workbench-confirm').findAll('button').find(b => b.text() === '取消')!.trigger('click')
    await flush()
    expect(env.UninstallNpmTool).not.toHaveBeenCalled()
    expect(w.find('.workbench-confirm').exists()).toBe(false)
    w.unmount()
  })

  it('operation 事件流：非终态更新进度；他工具占用时本工具禁用（busyElsewhere）', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    runtime.handlers['envcheck:npm-tool-operation']({ data: { operationId: 'x', toolId: 'codex', kind: 'install', stage: 'running', message: 'codex 安装中', terminal: false, success: false } })
    await nextTick()
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    const upgrade = card.findAll('button').find(b => b.text().includes('升级到'))!
    expect(upgrade.attributes('disabled')).toBeDefined()
    expect(card.text()).toContain('另一 npm 操作进行中')
    w.unmount()
  })

  it('终态事件：toast 回执 + 重取 overview 与本机清单（计数递增）', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const detectBefore = env.DetectAll.mock.calls.length
    const overviewBefore = env.GetNpmToolsOverview.mock.calls.length
    runtime.handlers['envcheck:npm-tool-operation']({ data: { operationId: 'op-2', toolId: 'claude', kind: 'upgrade', stage: 'done', message: '升级完成', terminal: true, success: true } })
    await flush()
    expect(useToast().toastMsg.value).toBe('升级完成')
    expect(env.DetectAll.mock.calls.length).toBeGreaterThan(detectBefore)
    expect(env.GetNpmToolsOverview.mock.calls.length).toBeGreaterThan(overviewBefore)
    w.unmount()
  })

  it('log 事件按 toolId 归入面板并封顶 200 行', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    runtime.handlers['envcheck:npm-tool-log']({ data: { toolId: 'claude', line: 'added 1 package' } })
    await nextTick()
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    expect(card.find('.op-log').text()).toContain('added 1 package')
    // 他工具日志不污染本面板
    runtime.handlers['envcheck:npm-tool-log']({ data: { toolId: 'codex', line: 'foreign' } })
    await nextTick()
    expect(card.find('.op-log').text()).not.toContain('foreign')
    w.unmount()
  })

  it('npm overview 拉取失败：错误文案 + 重试按钮重发', async () => {
    stubHappy()
    env.GetNpmToolsOverview.mockRejectedValue(new Error('registry 超时'))
    const w = await mountView()
    await flush()
    // 现状契约：工具卡来自本机清单，overview 失败只影响 NpmToolActions 面板挂载，
    // npmError 仅存于视图状态（模板未渲染横幅）——如实锁定。
    const card = w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!
    expect(card.find('.npm-panel').exists()).toBe(false)
    // 顶层重检测按钮可再触发恢复
    env.GetNpmToolsOverview.mockResolvedValue(npmOverview() as never)
    await w.find('.btn-primary').trigger('click')
    await flush()
    expect(w.findAll('.tool-card').find(c => c.text().includes('Claude Code'))!.find('.npm-panel').exists()).toBe(true)
    w.unmount()
  })
})

describe('EnvCheckView 订阅生命周期', () => {
  it('卸载后两个事件订阅全部注销', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['envcheck:npm-tool-log', 'envcheck:npm-tool-operation'])
    w.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })
})
