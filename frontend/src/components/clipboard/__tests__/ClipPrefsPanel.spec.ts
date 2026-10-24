// ClipPrefsPanel 行为规格（契约 §12 v1.8 设置面，提交 1 域：容量/删后贴/
// 排除表/按应用保留）。
//
// 钉死的契约行为：
//   ① 装载回显生效值；存量 appRetentionDays 必须展开成编辑行（漏同步则
//      首次保存折空 map——用户存量规则被静默清空，这是整表语义最阴的雷）；
//   ② 整表透传：面板未渲染的域（maskKinds/queueHotkey）原样回发，不许丢；
//   ③ 显式保存非防抖自动落盘（数值中途态不触发 RPC）；dirty 才给按；
//   ④ 钳制如实回显：保存成功用后端回值覆写展示与基线；
//   ⑤ 拒绝上浮：SetPrefs 报错（坏正则谱）红字可见、草稿不动、saved 不发；
//   ⑥ 失败可重试；「放弃修改」回卷基线并重展行。
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ClipPrefsPanel from '../ClipPrefsPanel.vue'
import { setClipboardTransport } from '../../../adapters/clipboard'
import type { ClipPrefs } from '../../../types/clipboard'

vi.mock('../../../composables/useToast', () => ({
  useToast: () => ({ showToast: vi.fn(), showErrorToast: vi.fn(), clearToasts: vi.fn() }),
}))

const fake = vi.hoisted(() => ({ GetPrefs: vi.fn(), SetPrefs: vi.fn() }))

const serverPrefs = (over: Partial<ClipPrefs> = {}): ClipPrefs => ({
  maxEntries: 800,
  maxBlobMB: 300,
  deleteAfterPaste: true,
  excludedFactory: ['1password.exe', 'keepass.exe'],
  excludedCustom: ['pgadmin.exe'],
  appRetentionDays: { 'slack.exe': 30 },
  queueHotkey: 'Ctrl+Alt+X',
  maskKinds: ['phone', 'email'],
  maskCustomPatterns: ['\\d{6}'],
  ...over,
})

async function flush(times = 10) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function openPanel(over: Partial<ClipPrefs> = {}) {
  fake.GetPrefs.mockResolvedValue(serverPrefs(over))
  const w = mount(ClipPrefsPanel)
  await flush()
  return w
}

beforeEach(() => {
  for (const fn of Object.values(fake)) fn.mockReset()
  setClipboardTransport(fake as never)
})

describe('装载与回显', () => {
  it('生效值上屏：数值/开关/出厂锁列表/custom 可删列表/存量规则成行', async () => {
    const w = await openPanel()
    expect((w.find('#cp-max-entries').element as HTMLInputElement).value).toBe('800')
    expect((w.find('#cp-max-blob').element as HTMLInputElement).value).toBe('300')
    expect((w.find('.cp-switch').element as HTMLInputElement).checked).toBe(true)
    expect(w.findAll('.cp-chip-locked')).toHaveLength(2)
    const customChips = w.findAll('.cp-group')[1].findAll('.cp-chip').filter((c) => !c.classes().includes('cp-chip-locked'))
    expect(customChips).toHaveLength(1)
    expect(customChips[0].text()).toContain('pgadmin.exe')
    // 存量规则展开成行（钉死雷区①）
    expect(w.findAll('.cp-retention-row')).toHaveLength(1)
    expect((w.find('.cp-ret-exe').element as HTMLInputElement).value).toBe('slack.exe')
    expect((w.find('.cp-ret-days').element as HTMLInputElement).value).toBe('30')
    expect(w.text()).toContain('设置均已生效') // 基线干净不显"未保存"
  })

  it('装载失败横幅带重试；重试成功即上屏', async () => {
    fake.GetPrefs.mockRejectedValueOnce(new Error('服务未接线')).mockResolvedValueOnce(serverPrefs())
    const w = mount(ClipPrefsPanel)
    await flush()
    expect(w.find('.banner-error').exists()).toBe(true)
    await w.find('.banner-error .btn').trigger('click')
    await flush()
    expect(w.find('#cp-max-entries').exists()).toBe(true)
    expect(fake.GetPrefs).toHaveBeenCalledTimes(2)
  })
})

describe('整表语义与显式保存', () => {
  it('改数值后 dirty 才给按；未渲染域（mask/queueHotkey）原样透传不丢', async () => {
    const w = await openPanel()
    const save = w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存设置'))!
    expect((save.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('#cp-max-entries').setValue('1200')
    expect((save.element as HTMLButtonElement).disabled).toBe(false)
    fake.SetPrefs.mockResolvedValue(serverPrefs({ maxEntries: 1200 }))
    await save.trigger('click')
    await flush()
    const sent = fake.SetPrefs.mock.calls[0][0] as ClipPrefs
    expect(sent.maxEntries).toBe(1200)
    expect(sent.maskKinds).toEqual(['phone', 'email']) // 未渲染域原样回发
    expect(sent.maskCustomPatterns).toEqual(['\\d{6}'])
    expect(sent.queueHotkey).toBe('Ctrl+Alt+X')
    expect(sent.appRetentionDays).toEqual({ 'slack.exe': 30 }) // 行折回 map 保存量
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('钳制如实回显：后端回值 2000 覆写展示与基线', async () => {
    const w = await openPanel()
    await w.find('#cp-max-entries').setValue('99999')
    fake.SetPrefs.mockResolvedValue(serverPrefs({ maxEntries: 2000 }))
    await w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存'))!.trigger('click')
    await flush()
    expect((w.find('#cp-max-entries').element as HTMLInputElement).value).toBe('2000')
    expect(w.text()).toContain('设置均已生效')
  })

  it('拒绝上浮：SetPrefs 报错红字可见、saved 不发、草稿留在原位', async () => {
    const w = await openPanel()
    await w.find('#cp-max-entries').setValue('700')
    fake.SetPrefs.mockRejectedValue(new Error('自定义遮蔽规则 "(" 不是合法正则'))
    await w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存'))!.trigger('click')
    await flush()
    expect(w.find('.cp-error').text()).toContain('合法正则')
    expect(w.emitted('saved')).toBeUndefined()
    expect((w.find('#cp-max-entries').element as HTMLInputElement).value).toBe('700')
    expect(w.text()).toContain('有未保存的修改')
  })

  it('放弃修改回卷基线并重展开行（乱改后一键还原）', async () => {
    const w = await openPanel()
    await w.find('#cp-max-entries').setValue('60')
    w.findAll('.cp-retention-row')[0].find('.cp-ret-exe').setValue('tampered.exe')
    await w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('放弃'))!.trigger('click')
    expect((w.find('#cp-max-entries').element as HTMLInputElement).value).toBe('800')
    expect((w.find('.cp-ret-exe').element as HTMLInputElement).value).toBe('slack.exe')
    expect(w.text()).toContain('设置均已生效')
  })

  it('custom 排除表：添加去重、移除即剪；空输入不给按', async () => {
    const w = await openPanel()
    const addBtn = w.findAll('.cp-addrow .btn')[0]
    expect((addBtn.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('.cp-add-input').setValue('PGAdmin.exe') // 已在表（大小写异）
    expect((addBtn.element as HTMLButtonElement).disabled).toBe(false)
    await addBtn.trigger('click')
    const customChips = () => w.findAll('.cp-group')[1].findAll('.cp-chip').filter((c) => !c.classes().includes('cp-chip-locked'))
    expect(customChips()).toHaveLength(1) // 去重（小写比对）
    await w.find('.cp-add-input').setValue('WeChat')
    await addBtn.trigger('click')
    expect(customChips()[1].text()).toContain('wechat') // 前端预小写
    await customChips()[0].find('.cp-chip-x').trigger('click')
    expect(customChips()).toHaveLength(1)
  })

  it('保留天数行：新增折叠进 map；天数留空=删除该规则（0 语义）', async () => {
    const w = await openPanel()
    await w.findAll('.cp-group .btn').find((b) => b.text().includes('添加应用'))!.trigger('click')
    const rows = w.findAll('.cp-retention-row')
    await rows[1].find('.cp-ret-exe').setValue('OldChat')
    await rows[1].find('.cp-ret-days').setValue('')
    fake.SetPrefs.mockImplementation(async (p: ClipPrefs) => p)
    await w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存'))!.trigger('click')
    await flush()
    const sent = fake.SetPrefs.mock.calls[0][0] as ClipPrefs
    expect(sent.appRetentionDays).toEqual({ 'slack.exe': 30, 'oldchat': 0 }) // 小写；空天=0=删规则
  })
})

describe('预览遮蔽域（提交 3）', () => {
  it('kind 丸回显服务端选中态；翻一颗后保存整表带新选中集（固定序由后端归一）', async () => {
    const w = await openPanel() // serverPrefs: maskKinds ['phone','email']
    const kinds = w.findAll('.cp-kind')
    expect(kinds).toHaveLength(4)
    expect(kinds.map((k) => k.classes().includes('active'))).toEqual([true, false, true, false])
    await kinds[1].trigger('click') // 打开 证件号
    fake.SetPrefs.mockResolvedValue(serverPrefs({ maskKinds: ['phone', 'idcard', 'email'] }))
    await w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存'))!.trigger('click')
    await flush()
    const sent = fake.SetPrefs.mock.calls[0][0] as ClipPrefs
    expect(sent.maskKinds).toEqual(['phone', 'email', 'idcard']) // 本地追加序，后端负责固定序回显
    expect(sent.maskCustomPatterns).toEqual(['\\d{6}']) // 未动域原样透传
  })

  it('自定义规则：添加去重、移除即剪（独立于排除表输入行，按 aria-label 分域）', async () => {
    const w = await openPanel()
    const patInput = w.find('input[aria-label="新增自定义遮蔽规则"]')
    const exclInput = w.find('input[aria-label="追加排除应用"]')
    expect(patInput.exists()).toBe(true)
    expect(exclInput.exists()).toBe(true)
    const maskGroup = w.findAll('.cp-group')[3]
    expect(maskGroup.text()).toContain('\\d{6}') // 服务端存量规则上屏
    await patInput.setValue('  订单号\\d{8}  ')
    await maskGroup.findAll('.cp-addrow .btn')[0].trigger('click')
    expect(maskGroup.findAll('.cp-chip')).toHaveLength(2)
    await patInput.setValue('\\d{6}') // 与存量重复
    await maskGroup.findAll('.cp-addrow .btn')[0].trigger('click')
    expect(maskGroup.findAll('.cp-chip')).toHaveLength(2) // 去重不加行
    await maskGroup.findAll('.cp-chip')[0].find('.cp-chip-x').trigger('click')
    expect(maskGroup.findAll('.cp-chip')).toHaveLength(1)
    expect(maskGroup.findAll('.cp-chip')[0].text()).toContain('订单号')
  })

  it('全关形态（服务端回显空集）：四丸全不亮；dirty 判定含遮蔽域翻动', async () => {
    const w = await openPanel({ maskKinds: [] })
    expect(w.findAll('.cp-kind').filter((k) => k.classes().includes('active'))).toHaveLength(0)
    await w.findAll('.cp-kind')[2].trigger('click') // 开 email
    const save = w.findAll('.cp-foot-btns .btn').find((b) => b.text().includes('保存设置'))!
    expect((save.element as HTMLButtonElement).disabled).toBe(false) // 只动遮蔽域也算脏
  })
})
