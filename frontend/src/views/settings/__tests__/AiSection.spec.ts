// AI 接入分区（F4b MCP 安装向导 + R6 授权开关）特征测试：三客户端四态渲染、
// 预览→确认写链（令牌回传）、fail-closed 拒动呈现手动片段、
// access.json 四开关写链（建档/即时生效文案/拒写指引/损坏态修复确认流）与"打开所在目录"、
// 幂等 ZeroDiff 文案、安装前自检行（R2：通过/失败警示不阻断/重检走 refresh/不该 spawn 时不 spawn）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AiSection from '../AiSection.vue'
import { useToast } from '../../../composables/useToast'
import { useConfirm } from '../../../composables/useConfirm'

const { confirmState, settleConfirm } = useConfirm()

const wizardSvc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  PreviewInstall: vi.fn(),
  PreviewUninstall: vi.fn(),
  ConfirmInstall: vi.fn(),
  ConfirmUninstall: vi.fn(),
  GetAccessOverview: vi.fn(),
  SetToolAccess: vi.fn(),
  ResetAccess: vi.fn(),
  SelfCheck: vi.fn(),
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
  // 自检默认通过态（R2）；单个用例覆写失败/异常分支
  wizardSvc.SelfCheck.mockResolvedValue({
    state: 'ok', toolCount: 4, tools: ['hanxi_envcheck_detect', 'hanxi_file_search', 'hanxi_ocr_recognize', 'hanxi_memo_search'],
    message: 'hanxi mcp 握手成功，4 件工具就位', checkedAt: '2026-09-17T12:00:00+08:00', fresh: true,
  })
}

async function mountView() {
  const w = mount(AiSection)
  await flushPromises()
  return w
}

afterEach(() => {
  settleConfirm(false) // 防未裁决的确认框悬挂到下一用例
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

  it('安装预览呈现自检通过行（通过 · N 工具），首轮走缓存口径 refresh=false', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'p',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk', diff: [],
    })
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(wizardSvc.SelfCheck).toHaveBeenCalledWith(false)
    const row = w.find('.check-row')
    expect(row.exists()).toBe(true)
    expect(row.text()).toContain('通过（4 工具）')
    expect(row.text()).toContain('握手成功')
  })

  it('自检失败红字警示 + 指引，不阻断确认按钮；重新自检走 refresh=true', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'p',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk', diff: [],
    })
    wizardSvc.SelfCheck.mockResolvedValue({
      state: 'failed', toolCount: 0, tools: null,
      message: '握手超时（15s 内未完成 initialize→tools/list，子进程已终止）', checkedAt: '2026-09-17T12:00:00+08:00', fresh: true,
    })
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button')[0].trigger('click')
    await flushPromises()
    const row = w.find('.check-row')
    expect(row.find('.chip-danger').exists()).toBe(true)
    expect(row.text()).toContain('未通过')
    expect(row.text()).toContain('握手超时')
    expect(row.find('.check-hint').exists()).toBe(true)
    // 不阻断：确认按钮仍在且可点
    const confirm = w.find('.modal-actions .btn-primary')
    expect(confirm.exists()).toBe(true)
    // 重新自检强制重 spawn
    await row.find('button').trigger('click')
    await flushPromises()
    expect(wizardSvc.SelfCheck).toHaveBeenLastCalledWith(true)
  })

  it('自检绑定层异常也定性失败呈现，不留空白行', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'p',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk', diff: [],
    })
    wizardSvc.SelfCheck.mockRejectedValue(new Error('bridge down'))
    const w = await mountView()
    await w.findAll('.client-row')[0].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(w.find('.check-row').text()).toContain('自检调用失败')
    expect(w.find('.check-row').text()).toContain('bridge down')
  })

  it('卸载预览与拒动安装预览均不触发自检（不落盘的预览不 spawn）', async () => {
    stubStatus([
      client('codex', 'Codex', { state: 'installed', canInstall: false, canUninstall: true }),
      client('claude', 'Claude Code'),
    ])
    wizardSvc.PreviewUninstall.mockResolvedValue({
      client: 'codex', clientName: 'Codex', configPath: 'p',
      allowed: true, zeroDiff: false, willCreate: false, reason: '', manualSnippet: '', token: 'tk-u', diff: [],
    })
    wizardSvc.PreviewInstall.mockResolvedValue({
      client: 'claude', clientName: 'Claude Code', configPath: 'p',
      allowed: false, zeroDiff: false, willCreate: false, reason: '不可安全合并', manualSnippet: 's', token: '',
      diff: [{ kind: 'keep', text: '已拒绝自动修改，不展示差异' }],
    })
    const w = await mountView()
    const codexBtns = w.findAll('.client-row')[0].findAll('button')
    await codexBtns[codexBtns.length - 1].trigger('click') // 卸载
    await flushPromises()
    expect(w.find('.check-row').exists()).toBe(false)
    const claudeBtns = w.findAll('.client-row')[1].findAll('button')
    await claudeBtns[0].trigger('click') // 拒动路径的安装预览
    await flushPromises()
    expect(w.find('.check-row').exists()).toBe(false)
    expect(wizardSvc.SelfCheck).not.toHaveBeenCalled()
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

  it('access 卡片：四开关行呈现读方视角状态与即时生效文案，打开所在目录传目录父路径', async () => {
    stubStatus([client('claude', 'Claude Code')])
    const w = await mountView()
    expect(w.text()).toContain('正常 · v1')
    expect(w.text()).toContain('保存即生效，无需重启 hanxi mcp')
    const rows = w.findAll('.tool-row')
    expect(rows).toHaveLength(4)
    const switches = w.findAll('.switch')
    expect((switches[0].element as HTMLInputElement).checked).toBe(true) // envcheck
    expect((switches[1].element as HTMLInputElement).checked).toBe(false) // everything
    expect(rows[0].text()).toContain('已授权')
    expect(rows[1].text()).toContain('未授权')
    expect(rows[0].text()).toContain('hanxi_envcheck_detect')
    expect(switches[0].attributes('disabled')).toBeUndefined() // 正常态可拨
    await w.findAll('.access-foot button')[0].trigger('click')
    expect(appSvc.OpenPath).toHaveBeenCalledWith('D:\\hx\\hanxidata\\mcp')
  })

  it('拨开关即写盘：SetToolAccess 回传呈现就地更新，toast 报告即时生效', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.SetToolAccess.mockResolvedValue({
      path: 'D:\\hx\\hanxidata\\mcp\\access.json', exists: true, readable: true, version: 1,
      tools: { envcheck: true, everything: true, ocr: false, memo: false }, note: '',
    })
    const w = await mountView()
    await w.findAll('.switch')[1].setValue(true)
    await flushPromises()
    expect(wizardSvc.SetToolAccess).toHaveBeenCalledWith('everything', true)
    expect((w.findAll('.switch')[1].element as HTMLInputElement).checked).toBe(true)
    expect(w.findAll('.tool-row')[1].text()).toContain('已授权')
    expect(useToast().toastMsg.value).toContain('全盘搜索已授权')
    expect(useToast().toastMsg.value).toContain('无需重启')
  })

  it('缺文件合法态：文案给"开关即建档"，开关不禁用（首拨凭空建档）', async () => {
    stubStatus([client('claude', 'Claude Code')], {
      exists: false, readable: false, version: 0,
      tools: { envcheck: false, everything: false, ocr: false, memo: false },
      note: '授权文件尚未生成——MCP 读者对此默认全关（fail-closed），这是合法默认态；拨动下方任一开关即自动建档并保存即生效',
    })
    const w = await mountView()
    expect(w.text()).toContain('尚未生成 · 默认全关')
    expect(w.text()).toContain('自动建档')
    const sw = w.findAll('.switch')[0]
    expect(sw.attributes('disabled')).toBeUndefined()
    expect(w.find('.access-repair').exists()).toBe(false) // 缺档≠损坏，不给修复按钮
  })

  it('写盘被拒（Go 侧拒盲写）：toast 中文指引并经 GetAccessOverview 回读真实态', async () => {
    stubStatus([client('claude', 'Claude Code')])
    wizardSvc.SetToolAccess.mockRejectedValue(new Error('授权文件当前读方不采信（含未知工具键），请走「修复（覆盖重置）」'))
    wizardSvc.GetAccessOverview.mockResolvedValue({
      path: 'p', exists: true, readable: false, version: 0,
      tools: { envcheck: false, everything: false, ocr: false, memo: false }, note: '损坏',
    })
    const w = await mountView()
    await w.findAll('.switch')[0].setValue(false)
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('授权改动未生效')
    expect(useToast().toastMsg.value).toContain('修复')
    expect(wizardSvc.GetAccessOverview).toHaveBeenCalled()
    expect(w.find('.chip-danger').text()).toContain('已损坏 · fail-closed')
  })

  it('损坏态：开关锁死 + 修复按钮走二次确认 → ResetAccess → 总览刷新', async () => {
    stubStatus([client('claude', 'Claude Code')], {
      exists: true, readable: false, version: 0,
      tools: { envcheck: false, everything: false, ocr: false, memo: false },
      note: '授权文件损坏或超纲——MCP 读者对其 fail-closed，视同全部未授权。可点「修复（覆盖重置）」',
    })
    const w = await mountView()
    expect(w.text()).toContain('已损坏 · fail-closed')
    expect(w.text()).toContain('fail-closed')
    for (const sw of w.findAll('.switch')) {
      expect(sw.attributes('disabled')).toBeDefined() // 危险态锁死，盲写无门
    }
    await w.find('.access-repair button').trigger('click')
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(true)
    await flushPromises()
    expect(wizardSvc.ResetAccess).toHaveBeenCalled()
  })

  it('修复链取消：不落任何写调用', async () => {
    stubStatus([client('claude', 'Claude Code')], {
      exists: true, readable: false, version: 0,
      tools: { envcheck: false, everything: false, ocr: false, memo: false },
      note: '损坏',
    })
    wizardSvc.ResetAccess.mockResolvedValue({ success: true, rolledBack: false, backupPath: 'D:\\x\\access.json.hanxi-bak-20260918-120000', message: '授权文件已重置为默认全关，旧档已另存' })
    const w = await mountView()
    await w.find('.access-repair button').trigger('click')
    settleConfirm(false)
    await flushPromises()
    expect(wizardSvc.ResetAccess).not.toHaveBeenCalled()
  })

  it('修复成功后刷新总览并 toast 备份去向', async () => {
    stubStatus([client('claude', 'Claude Code')], {
      exists: true, readable: false, version: 0,
      tools: { envcheck: false, everything: false, ocr: false, memo: false },
      note: '损坏',
    })
    wizardSvc.ResetAccess.mockResolvedValue({ success: true, rolledBack: false, backupPath: 'D:\\x\\access.json.hanxi-bak-20260918-120000', message: '授权文件已重置为默认全关，旧档已另存 D:\\x\\access.json.hanxi-bak-20260918-120000' })
    wizardSvc.GetAccessOverview.mockResolvedValue({
      path: 'D:\\hx\\hanxidata\\mcp\\access.json', exists: true, readable: true, version: 1,
      tools: { envcheck: false, everything: false, ocr: false, memo: false }, note: '',
    })
    const w = await mountView()
    await w.find('.access-repair button').trigger('click')
    settleConfirm(true)
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('hanxi-bak')
    expect(w.find('.chip-positive').text()).toContain('正常 · v1')
    expect((w.findAll('.switch')[0].element as HTMLInputElement).checked).toBe(false) // 全关
    expect(w.find('.access-repair').exists()).toBe(false) // 危险态解除
  })

  it('GetStatus 失败给 toast，不崩页面', async () => {
    wizardSvc.GetStatus.mockRejectedValue(new Error('boom'))
    const w = await mountView()
    expect(w.find('.page').exists()).toBe(true)
    expect(useToast().toastMsg.value).toContain('boom')
  })
})
