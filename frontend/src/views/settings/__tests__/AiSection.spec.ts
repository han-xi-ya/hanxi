// AI 接入分区（F4b MCP 安装向导）特征测试：三客户端四态渲染、
// 预览→确认写链（令牌回传）、fail-closed 拒动呈现手动片段、
// access.json 只读呈现与"打开所在目录"、幂等 ZeroDiff 文案。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AiSection from '../AiSection.vue'
import { useToast } from '../../../composables/useToast'

const wizardSvc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  PreviewInstall: vi.fn(),
  PreviewUninstall: vi.fn(),
  ConfirmInstall: vi.fn(),
  ConfirmUninstall: vi.fn(),
  GetAccessInfo: vi.fn(),
}))
const appSvc = vi.hoisted(() => ({ OpenPath: vi.fn() }))
vi.mock('../../../../bindings/hanxi/internal/mcpwizard', () => ({ McpWizardService: wizardSvc }))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

function client(id: string, name: string, overrides: Record<string, unknown> = {}) {
  return {
    id, name, format: id === 'codex' ? 'toml' : 'json',
    configPath: `C:\\Users\\u\\${id}.cfg`, dirExists: true, configExists: true,
    state: 'not-installed', detail: '', canInstall: true, canUninstall: false, installedAt: '',
    ...overrides,
  }
}

function stubStatus(clients: unknown[], access: Record<string, unknown> = {}) {
  wizardSvc.GetStatus.mockResolvedValue({
    server: { command: 'D:\\hx\\hanxi.exe', args: ['mcp'], ready: true },
    clients,
    access: {
      path: 'D:\\hx\\hanxidata\\mcp\\access.json', exists: true, readable: true, version: 1,
      tools: { envcheck: true, everything: false, ocr: false, memo: false }, note: '',
      ...access,
    },
  })
}

async function mountView() {
  const w = mount(AiSection)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
})

describe('AI 接入分区', () => {
  it('渲染三客户端四态徽章、路径与启动命令', async () => {
    stubStatus([
      client('claude', 'Claude Code'),
      client('codex', 'Codex', { state: 'installed', canInstall: false, canUninstall: true, installedAt: '2026-09-17T12:00:00+08:00' }),
      client('cursor', 'Cursor', { state: 'blocked', detail: '不可安全合并：含注释（JSONC）', canInstall: false }),
    ])
    const w = await mountView()
    const rows = w.findAll('.client-row')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('未安装')
    expect(rows[1].text()).toContain('已安装')
    expect(rows[1].text()).toContain('卸载')
    expect(rows[2].text()).toContain('拒绝自动改')
    expect(w.text()).toContain('D:\\hx\\hanxi.exe')
    // 冲突/拒动行给"查看指引"（预览弹窗只读态）
    expect(rows[2].text()).toContain('查看指引')
  })

  it('安装链：预览弹窗展示差异 → 确认回传令牌 → 刷新状态', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'C:\\Users\\u\\.claude.json',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk-1',
      diff: [
        { kind: 'keep', text: '{' },
        { kind: 'add', text: '  "hanxi": ...' },
      ],
    })
    wizardSvc.ConfirmInstall.mockResolvedValue({ success: true, rolledBack: false, backupPath: 'C:\\x.bak', message: '安装成功' })
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button')[0].trigger('click') // 安装
    await flushPromises()
    expect(wizardSvc.PreviewInstall).toHaveBeenCalledWith('claude')
    expect(w.find('.diff-pane').exists()).toBe(true)
    expect(w.find('.d-add').exists()).toBe(true)
    await w.find('.modal-actions .btn-primary').trigger('click')
    await flushPromises()
    expect(wizardSvc.ConfirmInstall).toHaveBeenCalledWith('claude', 'tk-1')
    expect(w.text()).toContain('完成')
    expect(w.text()).toContain('安装成功')
    expect(wizardSvc.GetStatus).toHaveBeenCalledTimes(2) // 写后刷新
  })

  it('fail-closed 预览：Allowed=false 呈现手动片段且无确认按钮', async () => {
    stubStatus([client('cursor', 'Cursor', { state: 'blocked', canInstall: false })])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'cursor', clientName: 'Cursor', configPath: 'C:\\Users\\u\\.cursor\\mcp.json',
      allowed: false, zeroDiff: false, willCreate: false,
      reason: '文件含注释（JSONC），自动合并会丢注释', manualSnippet: '"hanxi": { "command": ... }', token: '',
      diff: [{ kind: 'keep', text: '已拒绝自动修改，不展示差异' }],
    })
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button').at(-1)!.trigger('click') // 查看指引
    await flushPromises()
    expect(w.find('.refuse-block').exists()).toBe(true)
    expect(w.text()).toContain('JSONC')
    expect(w.find('.snippet').text()).toContain('hanxi')
    expect(w.find('.modal-actions .btn-primary').exists()).toBe(false)
    expect(wizardSvc.ConfirmInstall).not.toHaveBeenCalled()
  })

  it('ZeroDiff 幂等预览：按钮文案不含备份提示', async () => {
    stubStatus([client('claude', 'Claude Code', { state: 'installed', canInstall: true, canUninstall: true })])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'p',
      allowed: true, zeroDiff: true, willCreate: false, reason: '目标条目已与期望一致，无需改动文件（幂等）',
      manualSnippet: '', token: 'tk', diff: [],
    })
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(w.text()).toContain('零改动（幂等）')
    expect(w.find('.modal-actions .btn-primary').text()).toBe('确认安装')
  })

  it('卸载预览链走 ConfirmUninstall；回滚结果呈现警告态', async () => {
    stubStatus([client('codex', 'Codex', { state: 'installed', canInstall: false, canUninstall: true })])
    wizardSvc.PreviewUninstall.mockResolvedValue({
      client: 'codex', clientName: 'Codex', configPath: 'p',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk-u',
      diff: [{ kind: 'del', text: '# >>> hanxi mcp >>>' }],
    })
    wizardSvc.ConfirmUninstall.mockResolvedValue({ success: false, rolledBack: true, backupPath: 'b.bak', message: '写入后复验不通过，已自动回滚' })
    const w = await mountView()
    const btns = w.findAll('.client-row')[0].findAll('button')
    await btns[btns.length - 1].trigger('click') // 卸载（installed 行无查看指引按钮）
    await flushPromises()
    await w.find('.modal-actions .btn-primary').trigger('click')
    await flushPromises()
    expect(wizardSvc.ConfirmUninstall).toHaveBeenCalledWith('codex', 'tk-u')
    expect(w.text()).toContain('已回滚')
  })

  it('access 卡片：只读呈现四开关与备注，打开所在目录传目录父路径', async () => {
    stubStatus([client('claude', 'Claude Code')])
    appSvc.OpenPath.mockResolvedValue(undefined)
    const w = await mountView()
    expect(w.text()).toContain('正常 · v1')
    const chips = w.findAll('.access-tools .chip')
    expect(chips[0].text()).toBe('已授权')
    expect(chips[1].text()).toBe('未授权')
    await w.findAll('.access-foot button')[0].trigger('click')
    expect(appSvc.OpenPath).toHaveBeenCalledWith('D:\\hx\\hanxidata\\mcp')
  })

  it('access 损坏呈现危险态', async () => {
    stubStatus([client('claude', 'Claude Code')], { exists: true, readable: false, note: '授权文件已损坏——MCP server 对其 fail-closed' })
    const w = await mountView()
    expect(w.text()).toContain('已损坏 · fail-closed')
    expect(w.text()).toContain('fail-closed')
  })

  it('GetStatus 失败给 toast，不崩页面', async () => {
    wizardSvc.GetStatus.mockRejectedValue(new Error('boom'))
    const w = await mountView()
    expect(w.find('.page').exists()).toBe(true)
    expect(useToast().toastMsg.value).toContain('boom')
  })
})
