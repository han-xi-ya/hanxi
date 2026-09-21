// 特征测试：设置页拆分后的六个分区视图主链路。
// 承接原 SettingsView.spec.ts 的断言语义（常规读写回环、外观分段联动后端、
// 托盘加载渲染、工作台入口上抛 navigate），并补存储/系统直达的骨架锁。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GeneralSection from '../GeneralSection.vue'
import ThemeSection from '../ThemeSection.vue'
import TraySection from '../TraySection.vue'
import StorageSection from '../StorageSection.vue'
import SystemSection from '../SystemSection.vue'
import WorkbenchSection from '../WorkbenchSection.vue'
import { useToast } from '../../../composables/useToast'
import { useTheme } from '../../../composables/useTheme'
import { usePrompt } from '../../../composables/usePrompt'
import { useConfirm } from '../../../composables/useConfirm'

const { promptState, settlePrompt } = usePrompt()
const { confirmState, settleConfirm } = useConfirm()

const appSvc = vi.hoisted(() => ({
  GetAppInfo: vi.fn(),
  GetGeneralSettings: vi.fn(),
  SetGeneralSettings: vi.fn(),
  ListTrayMenuOptions: vi.fn(),
  GetTrayMenu: vi.fn(),
  SetTrayMenu: vi.fn(),
  PickExeFile: vi.fn(),
  OpenPath: vi.fn(),
  BindDataDir: vi.fn(),
  UnbindDataDir: vi.fn(),
  DataRootUsage: vi.fn(),
  OpenHostsFile: vi.fn(),
  OpenNetworkConnections: vi.fn(),
  OpenSystemEnvSettings: vi.fn(),
  OpenSystemTool: vi.fn(),
  SendTestNotification: vi.fn(),
  SendDelayedTestNotification: vi.fn(),
  GetTheme: vi.fn(),
  SetTheme: vi.fn().mockResolvedValue(undefined),
  GetAccent: vi.fn(),
  SetAccent: vi.fn().mockResolvedValue(undefined),
  SetWindowDarkMode: vi.fn().mockResolvedValue(undefined),
}))
const histSvc = vi.hoisted(() => ({
  GetOcrFullText: vi.fn(),
  SetOcrFullText: vi.fn(),
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('../../../../bindings/hanxi/internal/history/historyservice', () => histSvc)
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))

// F6 后 mode 为数据根来源内部标记（sibling/bound），不再是运行模式
function appInfoStub(over: Record<string, unknown> = {}) {
  return {
    name: 'Hanxi', version: '0.3.0', mode: 'sibling', baseDir: 'D:\\hx',
    configDir: 'D:\\hx', logsDir: 'D:\\hx\\logs', versionsDir: 'D:\\hx\\versions', runtimeDir: 'D:\\hx\\runtime',
    ...over,
  }
}

function stubs() {
  appSvc.GetAppInfo.mockResolvedValue(appInfoStub())
  appSvc.BindDataDir.mockResolvedValue(undefined)
  appSvc.UnbindDataDir.mockResolvedValue(undefined)
  appSvc.DataRootUsage.mockResolvedValue([])
  appSvc.GetGeneralSettings.mockResolvedValue({ autoStart: false, minimizeToTray: true, logRetainDays: 7 })
  appSvc.SetGeneralSettings.mockResolvedValue(undefined)
  appSvc.ListTrayMenuOptions.mockResolvedValue([{ type: 'command', ref: 'frpc/start', label: '启动 frpc', moduleName: 'frpc' }])
  appSvc.GetTrayMenu.mockResolvedValue([{ type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true }])
  appSvc.SetTrayMenu.mockResolvedValue(undefined)
  appSvc.OpenPath.mockResolvedValue(undefined)
  appSvc.GetTheme.mockResolvedValue('light')
  appSvc.GetAccent.mockResolvedValue('teal')
  histSvc.GetOcrFullText.mockResolvedValue(true)
  histSvc.SetOcrFullText.mockResolvedValue(undefined)
}

async function mountView(Component: typeof GeneralSection | typeof ThemeSection | typeof TraySection | typeof StorageSection | typeof SystemSection | typeof WorkbenchSection) {
  const w = mount(Component)
  await flushPromises()
  return w
}

afterEach(() => {
  // 防御上抛未落定的全局对话框请求，不让 Promise 悬挂串扰下个用例
  settlePrompt(null)
  settleConfirm(false)
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.removeAttribute('data-accent')
})

describe('常规偏好分区', () => {
  it('加载常规偏好回填控件', async () => {
    stubs()
    const w = await mountView(GeneralSection)
    const switches = w.findAll('.switch')
    expect((switches[0].element as HTMLInputElement).checked).toBe(false) // autoStart
    expect((switches[1].element as HTMLInputElement).checked).toBe(true) // minimizeToTray
    expect((w.find('.input-number').element as HTMLInputElement).value).toBe('7')
  })

  it('切换开机自启：SetGeneralSettings 携带完整三字段', async () => {
    stubs()
    const w = await mountView(GeneralSection)
    await w.findAll('.switch')[0].setValue(true)
    await flushPromises()
    expect(appSvc.SetGeneralSettings).toHaveBeenCalledWith({ autoStart: true, minimizeToTray: true, logRetainDays: 7 })
    expect(useToast().toastMsg.value).toBe('常规偏好设置已更新')
  })

  it('保存失败回滚：重拉后端值', async () => {
    stubs()
    appSvc.SetGeneralSettings.mockRejectedValue(new Error('注册表写入失败'))
    const w = await mountView(GeneralSection)
    const callsBefore = appSvc.GetGeneralSettings.mock.calls.length
    await w.findAll('.switch')[0].setValue(true)
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('注册表写入失败')
    expect(appSvc.GetGeneralSettings.mock.calls.length).toBeGreaterThan(callsBefore)
  })

  it('历史·OCR 全文档位（Q1）：默认回填开，切换走 SetOcrFullText', async () => {
    stubs()
    const w = await mountView(GeneralSection)
    const switches = w.findAll('.switch')
    expect((switches[2].element as HTMLInputElement).checked).toBe(true) // historyOcrFull
    await switches[2].setValue(false)
    await flushPromises()
    expect(histSvc.SetOcrFullText).toHaveBeenCalledWith(false)
  })
})

describe('外观主题分区', () => {
  it('三分段存在且点击深色：后端持久化 + DOM/标题栏联动', async () => {
    stubs()
    const { themeMode, resolvedTheme } = useTheme()
    const w = await mountView(ThemeSection)
    const segs = w.findAll('.seg-theme .theme-seg-btn')
    expect(segs).toHaveLength(3)
    expect(segs[2].text()).toBe('深色')
    await segs[2].trigger('click')
    await flushPromises()
    expect(themeMode.value).toBe('dark')
    expect(resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(appSvc.SetTheme).toHaveBeenCalledWith('dark')
    // 双轴同步：明暗 + 当前色板（标题栏外壳配色随色板）
    expect(appSvc.SetWindowDarkMode).toHaveBeenCalledWith(true, 'teal')
    // 复位防串扰
    themeMode.value = 'light'
  })

  it('色板五分段存在且点击碧空：SetAccent 持久化 + DOM data-accent 联动，预览圆点硬编码主色', async () => {
    stubs()
    const { accent } = useTheme()
    const w = await mountView(ThemeSection)
    const segs = w.findAll('.seg-accent .theme-seg-btn')
    expect(segs.map((s) => s.text())).toEqual(['青壳', '碧空', '鸢尾', '青瓷', '曜石'])
    expect(segs[0].classes()).toContain('active') // 默认青壳
    // 圆点为各色板浅色主色硬编码预览（不随当前色板联动）
    expect(segs[1].find('.accent-dot').attributes('style')).toContain('background: #0064b5')
    await segs[1].trigger('click')
    await flushPromises()
    expect(accent.value).toBe('sky')
    expect(document.documentElement.dataset.accent).toBe('sky')
    expect(appSvc.SetAccent).toHaveBeenCalledWith('sky')
    expect(segs[1].classes()).toContain('active')
    // 复位防串扰
    accent.value = 'teal'
  })
})

describe('托盘菜单分区', () => {
  it('加载后渲染现有条目与候选目录，保存上抛完整清单', async () => {
    stubs()
    const w = await mountView(TraySection)
    expect(w.text()).toContain('托盘右键菜单')
    expect(w.findAll('.tray-row')).toHaveLength(1)
    expect((w.find('.tray-name').element as HTMLInputElement).value).toBe('日志')
    expect(w.findAll('.option-item')).toHaveLength(1)
    // 勾选候选（新增一条）→ 脏标记放行保存钮
    await w.findAll('.option-item input')[0].setValue(true)
    expect(w.findAll('.tray-row')).toHaveLength(2)
    await w.find('.tray-footer .btn-primary').trigger('click')
    await flushPromises()
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    const saved = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(saved).toHaveLength(2)
    expect(useToast().toastMsg.value).toBe('托盘右键菜单已保存并即时生效')
  })

  it('分组条目：新建分组 → 展开子条目面板 → 从候选添加子条目并入保存清单', async () => {
    stubs()
    appSvc.GetTrayMenu.mockResolvedValue([])
    const w = await mountView(TraySection)
    // 新建分组（轮盘二级扇区）：自动展开子条目编辑面板
    const toolsBtn = w.find('.tray-tools button')
    await toolsBtn.trigger('click')
    expect(w.findAll('.tray-row-group')).toHaveLength(1)
    expect(w.find('.tray-children').exists()).toBe(true)
    expect(w.find('.tray-child-empty').text()).toContain('保存会被拒绝')
    // 收起再展开切换（openGroup 复位语义）
    const toggleBtn = w.findAll('.tray-row-group button')[0]
    expect(toggleBtn.text()).toContain('收起子条目')
    await toggleBtn.trigger('click')
    expect(w.find('.tray-children').exists()).toBe(false)
    await toggleBtn.trigger('click')
    expect(w.find('.tray-children').exists()).toBe(true)
    // 从候选添加子条目（select 变更 → 钮解禁 → 追加）
    await w.find('.tray-child-select').setValue('command|frpc/start')
    const addBtn = w.findAll('.tray-child-add .btn')[0]
    await addBtn.trigger('click')
    expect(w.findAll('.tray-child-row')).toHaveLength(1)
    // 保存：清单含 group 及其 children
    await w.find('.tray-footer .btn-primary').trigger('click')
    await flushPromises()
    const saved = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(saved[0].type).toBe('group')
    expect(saved[0].children).toHaveLength(1)
    expect(saved[0].children[0].ref).toBe('frpc/start')
  })

  it('加载失败呈现可感知的空目录态（候选为空提示）', async () => {
    stubs()
    appSvc.ListTrayMenuOptions.mockResolvedValue([])
    appSvc.GetTrayMenu.mockResolvedValue([])
    const w = await mountView(TraySection)
    expect(w.findAll('.tray-row')).toHaveLength(0)
    expect(w.text()).toContain('尚未配置托盘条目')
    expect(w.text()).toContain('暂无候选条目')
  })
})

describe('存储目录分区', () => {
  it('F6 后呈现当前数据根与同级来源徽标，目录行直达走 OpenPath，无运行模式字样', async () => {
    stubs()
    const w = await mountView(StorageSection)
    expect(w.text()).toContain('应用同级 · ./hanxidata')
    expect(w.text()).not.toContain('便携')
    expect(w.text()).not.toContain('标准模式')
    const rows = w.findAll('.setting-row')
    expect(rows).toHaveLength(5) // 数据根 + 日志/版本仓/运行时 + 占用一览头行（N11）
    expect(rows[0].text()).toContain('D:\\hx') // 数据根 = baseDir
    const rootBtns = rows[0].findAll('button')
    expect(rootBtns).toHaveLength(2) // 未绑定：打开目录 + 更改位置，无"回到同级"
    await rows[1].find('button').trigger('click')
    expect(appSvc.OpenPath).toHaveBeenCalledWith('D:\\hx\\logs')
  })

  it('绑定来源：解锁"回到同级"，解绑走全局确认后落 UnbindDataDir', async () => {
    stubs()
    appSvc.GetAppInfo.mockResolvedValue(appInfoStub({ mode: 'bound', baseDir: 'E:\\HanxiData' }))
    const w = await mountView(StorageSection)
    expect(w.text()).toContain('已绑定数据之家')
    const rootBtns = w.findAll('.setting-row')[0].findAll('button')
    expect(rootBtns).toHaveLength(3)
    await rootBtns[2].trigger('click')
    expect(confirmState.open).toBe(true)
    settleConfirm(true)
    await flushPromises()
    expect(appSvc.UnbindDataDir).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('重启')
  })

  it('数据根占用一览（N11）：挂载即测、GiB 呈现、Partial 行标"≥下限"、重测走 force', async () => {
    stubs()
    appSvc.DataRootUsage.mockResolvedValue([
      { name: 'everything', isDir: true, bytes: 3221225472, files: 81234, partial: false, errorCount: 0 },
      { name: 'snapshots', isDir: true, bytes: 5368709120, files: 42, partial: true, errorCount: 3 },
    ])
    const w = await mountView(StorageSection)
    await flushPromises()
    expect(appSvc.DataRootUsage).toHaveBeenCalledWith(false) // 挂载走缓存路径
    const rows = w.findAll('.usage-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('everything')
    expect(rows[0].text()).toContain('3.00 GiB')
    expect(rows[0].text()).toContain('81,234 个文件')
    expect(rows[1].text()).toContain('≥')
    expect(rows[1].find('.chip-warning').exists()).toBe(true) // 下限徽标
    const usageHead = w.findAll('.setting-row')[4]
    await usageHead.find('button').trigger('click')
    await flushPromises()
    expect(appSvc.DataRootUsage).toHaveBeenLastCalledWith(true) // 重新测量穿透缓存
  })

  it('更改位置：prompt 取消不调后端，提交绝对路径走 BindDataDir 并提示重启生效', async () => {
    stubs()
    const w = await mountView(StorageSection)
    const changeBtn = w.findAll('.setting-row')[0].findAll('button')[1]
    await changeBtn.trigger('click')
    expect(promptState.open).toBe(true)
    settlePrompt(null) // 取消
    await flushPromises()
    expect(appSvc.BindDataDir).not.toHaveBeenCalled()

    await changeBtn.trigger('click')
    settlePrompt('E:\\HanxiData')
    await flushPromises()
    expect(appSvc.BindDataDir).toHaveBeenCalledWith('E:\\HanxiData')
    expect(useToast().toastMsg.value).toContain('重启 Hanxi 后生效')
  })

  it('绑定失败：后端中文错误经 toast 呈现，不留成功假象', async () => {
    stubs()
    appSvc.BindDataDir.mockRejectedValue(new Error('exe 同级无法写入 hanxi.bind'))
    const w = await mountView(StorageSection)
    const changeBtn = w.findAll('.setting-row')[0].findAll('button')[1]
    await changeBtn.trigger('click')
    settlePrompt('E:\\HanxiData')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('绑定失败')
    expect(useToast().toastMsg.value).toContain('hanxi.bind')
  })
})

describe('系统直达分区', () => {
  it('七路系统组件行存在，点击按 id 走对应后端入口', async () => {
    stubs()
    appSvc.OpenHostsFile.mockResolvedValue(undefined)
    appSvc.OpenSystemTool.mockResolvedValue(undefined)
    const w = await mountView(SystemSection)
    const rows = w.findAll('.tool-list')[0].findAll('.setting-row')
    expect(rows).toHaveLength(7)
    await rows[0].find('button').trigger('click') // hosts
    expect(appSvc.OpenHostsFile).toHaveBeenCalled()
    await rows[4].find('button').trigger('click') // regedit
    expect(appSvc.OpenSystemTool).toHaveBeenCalledWith('regedit')
  })

  it('通知诊断行触发前台/延迟测试管道', async () => {
    stubs()
    appSvc.SendTestNotification.mockResolvedValue(undefined)
    appSvc.SendDelayedTestNotification.mockResolvedValue(undefined)
    const w = await mountView(SystemSection)
    const notifyBtns = w.findAll('.tool-list')[1].findAll('button')
    await notifyBtns[0].trigger('click')
    await notifyBtns[1].trigger('click')
    expect(appSvc.SendTestNotification).toHaveBeenCalled()
    expect(appSvc.SendDelayedTestNotification).toHaveBeenCalledWith(4)
  })
})

describe('工作台入口分区', () => {
  it('日志/关于行存在，点击上抛 navigate（由宿主门禁链换页）', async () => {
    stubs()
    const w = await mountView(WorkbenchSection)
    const rows = w.findAll('.setting-row')
    expect(rows[0].text()).toContain('运行日志')
    expect(rows[1].text()).toContain('关于 Hanxi')
    await rows[0].find('button').trigger('click')
    await rows[1].find('button').trigger('click')
    expect(w.emitted('navigate')).toEqual([['/logs'], ['/about']])
  })
})
