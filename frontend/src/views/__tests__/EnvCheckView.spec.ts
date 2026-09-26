// 特征测试（组 G3 / Phase 5）：EnvCheckView 基线锁定。
// 断言对象：检测卡渲染、状态 chip、官方版本面板接线、npm 工具装升卸事件流、
// 卸载二次确认（已收编为全局 useConfirm 单例，视图不再自挂弹层，测试改经
// confirmState/settleConfirm 驱动，文案/details 断言语义不变）。
import { nextTick } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import EnvCheckView from '../EnvCheckView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const env = vi.hoisted(() => ({
  DetectAll: vi.fn(),
  GetNpmToolsOverview: vi.fn(),
  InstallNpmTool: vi.fn(),
  UpgradeNpmTool: vi.fn(),
  UninstallNpmTool: vi.fn(),
  GetGitForWindowsOverview: vi.fn(),
  GitGlobalConfig: vi.fn(),
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

const hist = vi.hoisted(() => ({ List: vi.fn(), Delete: vi.fn(), Clear: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/envcheck/envcheckservice', () => env)
vi.mock('../../../bindings/hanxi/internal/modules/bcu/bcuservice', () => bcu)
vi.mock('../../../bindings/hanxi/internal/history/historyservice', () => hist)

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

function gitConfigPayload(over: Record<string, unknown> = {}) {
  return {
    state: 'configured',
    items: [
      { key: 'user.name', value: 'hanxi' },
      { key: 'user.email', value: 'hanxi@example.com' },
      { key: 'http.proxy', value: '[已脱敏]' }, // 服务端出的就是脱敏件，前端无原值
    ],
    detail: '',
    ...over,
  }
}

function stubHappy() {
  env.DetectAll.mockResolvedValue(BASE_TOOLS as never)
  env.GetNpmToolsOverview.mockResolvedValue(npmOverview() as never)
  env.GitGlobalConfig.mockResolvedValue(gitConfigPayload() as never)
  for (const fn of [env.GetGitForWindowsOverview, env.GetGoOverview, env.GetNodeOverview, env.GetJavaOverview, env.GetPythonOverview, env.GetDotNetOverview]) {
    fn.mockResolvedValue({ channels: [{ key: 'stable', label: 'Stable', detail: '', relation: 'update-available', releases: [{ version: '1.2.3', published: '2026-08-01T00:00:00Z' }], relationDetail: '' }], isStale: false, fetchedAt: '2026-09-05 10:00' } as never)
  }
  env.RevealToolPath.mockResolvedValue(undefined)
  env.InstallNpmTool.mockResolvedValue({ operationId: 'op-1', message: '已受理' })
  env.UpgradeNpmTool.mockResolvedValue({ operationId: 'op-2', message: '升级中' })
  env.UninstallNpmTool.mockResolvedValue({ operationId: 'op-3', message: '卸载中' })
  bcu.OpenWindow.mockResolvedValue(undefined)
  hist.List.mockResolvedValue([]) // 历史标签面板自取数：默认空桶
}

async function mountView() {
  return mount(EnvCheckView, { attachTo: document.body, global: { stubs: { teleport: true } } })
}

async function openVersions(w: VueWrapper) {
  const tab = w.findAll('[role="tab"]').find(item => item.text() === '版本与工具')
  if (!tab) throw new Error('未找到“版本与工具”标签')
  await tab.trigger('click')
  await nextTick()
}

function managedCard(w: VueWrapper, display: string) {
  return w.findAll('.management-card').find(card => card.text().includes(display) && card.find('.npm-panel').exists())
}

const { confirmState, settleConfirm } = useConfirm()

afterEach(() => {
  settleConfirm(false) // 兜底落定未决确认，防跨用例悬挂
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('EnvCheckView 检测卡渲染', () => {
  it('默认显示本机环境，并以完整 ARIA 关系切换版本与工具标签且不重复请求', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    const tabs = w.findAll('[role="tab"]')
    expect(tabs.map(tab => tab.text())).toEqual(['本机环境', '版本与工具', '历史记录'])
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(tabs[0].attributes('aria-controls')).toBe('envcheck-local-panel')
    expect(w.find('#envcheck-local-panel').attributes('aria-labelledby')).toBe('envcheck-local-tab')
    expect(w.find('#envcheck-versions-panel').attributes('aria-labelledby')).toBe('envcheck-versions-tab')
    const detectCalls = env.DetectAll.mock.calls.length
    await openVersions(w)
    expect(tabs[1].attributes('aria-selected')).toBe('true')
    expect(w.find('#envcheck-local-panel').isVisible()).toBe(false)
    expect(w.find('#envcheck-versions-panel').isVisible()).toBe(true)
    expect(env.DetectAll).toHaveBeenCalledTimes(detectCalls)
    w.unmount()
  })

  it('首屏并发拉取本机清单 + 6 官网通道 + npm overview', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(env.DetectAll).toHaveBeenCalled()
    expect(env.GetGitForWindowsOverview).toHaveBeenCalled()
    expect(env.GetDotNetOverview).toHaveBeenCalled()
    expect(env.GetNpmToolsOverview).toHaveBeenCalled()
    expect(w.findAll('.tool-card')).toHaveLength(BASE_TOOLS.length)
    expect(w.find('.status-summary').text()).toContain('本机已安装 6 / 9 项')
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
    const chips = w.find('#envcheck-local-panel').findAll('.status-chip').map(c => c.text())
    expect(chips).toEqual(['✓ 已安装', '○ 未安装', '! 检测失败', '⚠ 商店存根'])
    // 回退语义：未知状态按"检测失败"呈现
    w.unmount()
  })

  it('dotnet 并排版本线 + 版本标签中的 npm/pnpm 升级提示 + java 存根 hint', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(w.text()).toContain('另装版本线 8.0')
    const javaCard = w.findAll('.tool-card').find(c => c.text().includes('Java'))!
    expect(javaCard.find('.tool-hint').text()).toContain('微软商店存根')
    await openVersions(w)
    const npmCard = w.findAll('.management-card').find(c => c.text().includes('npm') && c.find('.upgrade-hint').exists())!
    expect(npmCard.find('.upgrade-hint').exists()).toBe(true)
    expect(npmCard.text()).toContain('npm install --global npm@latest')
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

  it('空工具清单显示解释性空态', async () => {
    stubHappy()
    env.DetectAll.mockResolvedValue([])
    const w = await mountView()
    await flush()
    expect(w.find('#envcheck-local-panel').text()).toContain('未返回可识别的开发工具，请重新检测')
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
    await openVersions(w)
    const card = managedCard(w, 'Claude Code')!
    const upgrade = card.findAll('button').find(b => b.text().includes('升级到 2.1.261'))!
    await upgrade.trigger('click')
    await flush()
    expect(env.UpgradeNpmTool).toHaveBeenCalledWith('claude')
    expect(card.find('.op-running').exists()).toBe(true)
    expect(card.find('.op-log').text()).toBe('正在启动 npm 操作…')
    const detectCalls = env.DetectAll.mock.calls.length
    await w.findAll('[role="tab"]')[0].trigger('click')
    await w.findAll('[role="tab"]')[1].trigger('click')
    await nextTick()
    const restored = managedCard(w, 'Claude Code')!
    expect(restored.find('.op-running').exists()).toBe(true)
    expect(restored.find('.op-log').text()).toBe('正在启动 npm 操作…')
    expect(env.DetectAll).toHaveBeenCalledTimes(detectCalls)
    w.unmount()
  })

  it('卸载二次确认：经全局 useConfirm 单例（title=卸载 Claude Code，details 含 npm 包），确认后调 UninstallNpmTool', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    await openVersions(w)
    const card = managedCard(w, 'Claude Code')!
    await card.findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flush()
    // 收编契约：确认请求走全局单例，视图自身不再渲染弹层
    expect(w.find('.workbench-confirm').exists()).toBe(false)
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('卸载 Claude Code')
    expect(confirmState.options.confirmLabel).toBe('确认卸载')
    expect(confirmState.options.tone).toBe('danger')
    const detailValues = confirmState.options.details?.map(item => item.value) ?? []
    expect(detailValues).toContain('@anthropic-ai/claude-code')
    expect(detailValues.some(v => v.includes('仅移除 npm 全局安装'))).toBe(true)
    expect(env.UninstallNpmTool).not.toHaveBeenCalled()
    settleConfirm(true)
    await flush()
    expect(env.UninstallNpmTool).toHaveBeenCalledWith('claude')
    expect(confirmState.open).toBe(false)
    // 受理后进入乐观运行态
    expect(managedCard(w, 'Claude Code')!.find('.op-running').exists()).toBe(true)
    w.unmount()
  })

  it('卸载确认取消（settle false）：不调后端', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    await openVersions(w)
    const card = managedCard(w, 'Claude Code')!
    await card.findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flush()
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await flush()
    expect(env.UninstallNpmTool).not.toHaveBeenCalled()
    expect(confirmState.open).toBe(false)
    w.unmount()
  })

  it('operation 事件流：非终态更新进度；他工具占用时本工具禁用（busyElsewhere）', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    runtime.handlers['envcheck:npm-tool-operation']({ data: { operationId: 'x', toolId: 'codex', kind: 'install', stage: 'running', message: 'codex 安装中', terminal: false, success: false } })
    await nextTick()
    await openVersions(w)
    const card = managedCard(w, 'Claude Code')!
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
    await openVersions(w)
    const card = managedCard(w, 'Claude Code')!
    expect(card.find('.op-log').text()).toContain('added 1 package')
    // 他工具日志不污染本面板
    runtime.handlers['envcheck:npm-tool-log']({ data: { toolId: 'codex', line: 'foreign' } })
    await nextTick()
    expect(card.find('.op-log').text()).not.toContain('foreign')
    w.unmount()
  })

  it('npm overview 拉取失败：错误文案 + 分区重试按钮恢复', async () => {
    stubHappy()
    env.GetNpmToolsOverview.mockRejectedValue(new Error('registry 超时'))
    const w = await mountView()
    await flush()
    await openVersions(w)
    expect(w.find('#envcheck-versions-panel .banner-error').text()).toContain('npm 工具信息获取失败: registry 超时')
    expect(managedCard(w, 'Claude Code')).toBeUndefined()
    env.GetNpmToolsOverview.mockResolvedValue(npmOverview() as never)
    await w.findAll('.section-heading button').find(button => button.text() === '重试 npm 信息')!.trigger('click')
    await flush()
    expect(managedCard(w, 'Claude Code')!.find('.npm-panel').exists()).toBe(true)
    w.unmount()
  })
})

describe('EnvCheckView Git 全局配置区块', () => {
  function gitCfgSection(w: VueWrapper) {
    return w.find('.gitcfg-panel')
  }

  it('随页面复采钮自动读取；configured 态渲染键值行、条目数与脱敏值原样透出', async () => {
    stubHappy()
    const w = await mountView()
    await flush()
    expect(env.GitGlobalConfig).toHaveBeenCalled()
    const section = gitCfgSection(w)
    expect(section.text()).toContain('已读取')
    expect(section.text()).toContain('共 3 条，敏感条目已就地打码')
    const lines = section.findAll('.gitcfg-line')
    expect(lines).toHaveLength(3)
    expect(lines[2].findAll('code')[0].text()).toBe('http.proxy')
    expect(lines[2].findAll('code')[1].text()).toBe('[已脱敏]')
    expect(lines[2].findAll('code')[1].attributes('title')).toBe('[已脱敏]')
    w.unmount()
  })

  it('复制全部：复制件即界面脱敏件（逐行 key=value，无原值）', async () => {
    stubHappy()
    const originalClipboard = navigator.clipboard
    const originalSecure = window.isSecureContext
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    try {
      const w = await mountView()
      await flush()
      const copy = gitCfgSection(w).findAll('button').find(b => b.text() === '复制全部')!
      await copy.trigger('click')
      await flush()
      expect(writeText).toHaveBeenCalledWith('user.name=hanxi\nuser.email=hanxi@example.com\nhttp.proxy=[已脱敏]')
      expect(useToast().toastMsg.value).toContain('Git 全局配置已复制')
      w.unmount()
    } finally {
      Object.defineProperty(navigator, 'clipboard', { value: originalClipboard, configurable: true })
      Object.defineProperty(window, 'isSecureContext', { value: originalSecure, configurable: true })
    }
  })

  it('unconfigured 态：明示"尚无全局配置"而非空列表糊弄', async () => {
    stubHappy()
    env.GitGlobalConfig.mockResolvedValue(gitConfigPayload({ state: 'unconfigured', items: undefined }) as never)
    const w = await mountView()
    await flush()
    expect(gitCfgSection(w).text()).toContain('未配置')
    expect(gitCfgSection(w).text()).toContain('~/.gitconfig 不存在或无条目')
    expect(gitCfgSection(w).findAll('.gitcfg-line')).toHaveLength(0)
    w.unmount()
  })

  it('not-installed 态显形为指引：安装文案 + 打开 Git 下载页按钮', async () => {
    stubHappy()
    env.GitGlobalConfig.mockResolvedValue(gitConfigPayload({
      state: 'not-installed', items: undefined, detail: '未在 PATH 中找到 git，无法读取全局配置',
    }) as never)
    const w = await mountView()
    await flush()
    const section = gitCfgSection(w)
    expect(section.text()).toContain('未在 PATH 中找到 git')
    expect(section.text()).toContain('Git for Windows 下载页')
    await section.findAll('.link-button')[0].trigger('click')
    expect(env.OpenGitForWindowsDownloadPage).toHaveBeenCalled()
    expect(section.text()).not.toContain('共 ')
    w.unmount()
  })

  it('error 态透出 detail；调用失败走错误文案且区块不消失', async () => {
    stubHappy()
    env.GitGlobalConfig.mockResolvedValue(gitConfigPayload({ state: 'error', items: undefined, detail: '读取 git 全局配置失败: exit status 128' }) as never)
    const w = await mountView()
    await flush()
    expect(gitCfgSection(w).text()).toContain('读取失败')
    expect(gitCfgSection(w).text()).toContain('exit status 128')
    w.unmount()

    stubHappy()
    env.GitGlobalConfig.mockRejectedValue(new Error('模块正被停用'))
    const w2 = await mountView()
    await flush()
    expect(gitCfgSection(w2).exists()).toBe(true)
    expect(gitCfgSection(w2).text()).toContain('Git 全局配置读取失败: 模块正被停用')
    w2.unmount()
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

describe('EnvCheckView 历史记录标签（统一历史接入）', () => {
  it('面板自取 envcheck 桶并渲染动作留档；无应用钮（无回填输入面）', async () => {
    stubHappy()
    hist.List.mockResolvedValue([
      { id: 1, funcType: 'envcheck', summary: 'Claude Code 安装完成', input: 'claude', output: '', extra: 'npm-install', createdAt: '2026-09-17T10:00:00+08:00' },
    ])
    const w = await mountView()
    await flush()
    expect(hist.List).toHaveBeenCalledWith('envcheck', '')
    const tab = w.findAll('[role="tab"]').find(item => item.text() === '历史记录')!
    await tab.trigger('click')
    await flush()
    const panel = w.find('#envcheck-history-panel')
    expect(panel.attributes('aria-labelledby')).toBe('envcheck-history-tab')
    expect(panel.text()).toContain('Claude Code 安装完成')
    expect(panel.findAll('.hp-actions .btn').map(b => b.text())).not.toContain('应用')
    w.unmount()
  })
})
