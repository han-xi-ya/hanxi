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

const appSvc = vi.hoisted(() => ({
  GetAppInfo: vi.fn(),
  GetGeneralSettings: vi.fn(),
  SetGeneralSettings: vi.fn(),
  ListTrayMenuOptions: vi.fn(),
  GetTrayMenu: vi.fn(),
  SetTrayMenu: vi.fn(),
  PickExeFile: vi.fn(),
  OpenPath: vi.fn(),
  OpenHostsFile: vi.fn(),
  OpenNetworkConnections: vi.fn(),
  OpenSystemEnvSettings: vi.fn(),
  OpenSystemTool: vi.fn(),
  SendTestNotification: vi.fn(),
  SendDelayedTestNotification: vi.fn(),
  GetTheme: vi.fn(),
  SetTheme: vi.fn().mockResolvedValue(undefined),
  SetWindowDarkMode: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))

function stubs() {
  appSvc.GetAppInfo.mockResolvedValue({
    name: 'Hanxi', version: '0.3.0', mode: 'portable', baseDir: 'D:\\hx',
    configDir: 'D:\\hx\\data', logsDir: 'D:\\hx\\logs', versionsDir: 'D:\\hx\\versions', runtimeDir: 'D:\\hx\\runtime',
  })
  appSvc.GetGeneralSettings.mockResolvedValue({ autoStart: false, minimizeToTray: true, logRetainDays: 7 })
  appSvc.SetGeneralSettings.mockResolvedValue(undefined)
  appSvc.ListTrayMenuOptions.mockResolvedValue([{ type: 'command', ref: 'frpc/start', label: '启动 frpc', moduleName: 'frpc' }])
  appSvc.GetTrayMenu.mockResolvedValue([{ type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true }])
  appSvc.SetTrayMenu.mockResolvedValue(undefined)
  appSvc.OpenPath.mockResolvedValue(undefined)
  appSvc.GetTheme.mockResolvedValue('light')
}

async function mountView(Component: typeof GeneralSection | typeof ThemeSection | typeof TraySection | typeof StorageSection | typeof SystemSection | typeof WorkbenchSection) {
  const w = mount(Component)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
  document.documentElement.removeAttribute('data-theme')
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
})

describe('外观主题分区', () => {
  it('三分段存在且点击深色：后端持久化 + DOM/标题栏联动', async () => {
    stubs()
    const { themeMode, resolvedTheme } = useTheme()
    const w = await mountView(ThemeSection)
    const segs = w.findAll('.theme-seg-btn')
    expect(segs).toHaveLength(3)
    expect(segs[2].text()).toBe('深色')
    await segs[2].trigger('click')
    await flushPromises()
    expect(themeMode.value).toBe('dark')
    expect(resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(appSvc.SetTheme).toHaveBeenCalledWith('dark')
    expect(appSvc.SetWindowDarkMode).toHaveBeenCalledWith(true)
    // 复位防串扰
    themeMode.value = 'light'
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
  it('回填四类目录路径与运行模式徽标，点击打开走 OpenPath', async () => {
    stubs()
    const w = await mountView(StorageSection)
    expect(w.text()).toContain('便携免安装模式')
    const rows = w.findAll('.setting-row')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('D:\\hx\\data')
    await rows[1].find('button').trigger('click')
    expect(appSvc.OpenPath).toHaveBeenCalledWith('D:\\hx\\logs')
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
