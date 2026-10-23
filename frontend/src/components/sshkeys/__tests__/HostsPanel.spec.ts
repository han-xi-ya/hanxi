// 主机清单面板的特征测试。锁的是这个面板的存在理由：
// ① 对账带五态（一致 / 漂移 / 还没导出过 / 缺 Include 接线 / 缺钥匙）逐态如实摊开，
//    文案取后端 message 原文，色彩档只看布尔位；操作面板语法（左状态右动作）、
//    成功后 confPath/backupPath 常驻、失败不塌成未导出、未导出给"下一步"；
// ② 写用户主目录只能是显式动作——账本增删改之后**绝不**自动调 ExportConf；
// ③ 别名是主键：编辑态锁死、机制文案讲"为什么"、重名保存前拦；
//    aliasIssue 与后端 sshconf TestValidateAlias 样本表逐条镜像核对（规则不变，表要对齐）；
// ④ 删除确认点名"不会删除磁盘上的私钥"；启停失败必须把 checkbox 拨回真实值；
// ⑤ 可读性三锁：密钥列只上屏基名（完整路径进 title）+ 钥匙"找不到"帽只由后端逐条
//    stat 名单驱动、启用列说明"= 是否导出给 ssh"（含 title/aria 与停用×drift 的行内呼应）；
// ⑥ 表单「私钥路径」下拉：台账只当**选项来源**（值=suggestPath 而非 privatePath、只有
//    hasPrivate 的行进下拉、选中缺公钥的给联动提示、纯手输必须始终可保存、读失败/空台账
//    落禁用占位项静默降级、开表单重拉一次）。挂载/开表单的 ListKeys 只服务这一个用途——
//    "缺不缺钥匙"仍只看 ⑤ 的后端名单，两套用途不许合并（旧误标事故见 useHosts 注）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import HostsPanel from '../HostsPanel.vue'
import { aliasIssue } from '../useHosts'

// 绑定面打桩：只 mock 到 SshKeysService 这一层，面板与 composable 的真实链路照跑。
// 键名必须与 bindings 导出命名空间逐字一致（SSHCheckPanel.spec 钉过"mock 用旧名、
// 测试绿但生产挂"的假绿事故）。vi.mock 工厂会被提升到文件顶部，桩件必须走
// vi.hoisted，否则报 "Cannot access 'svc' before initialization"。
const svc = vi.hoisted(() => ({
  ListHosts: vi.fn(),
  ListKeys: vi.fn(),
  AddHost: vi.fn(),
  UpdateHost: vi.fn(),
  RemoveHost: vi.fn(),
  ExportStatus: vi.fn(),
  ExportConf: vi.fn(),
  RemoveExport: vi.fn(),
}))
// 确认对话框宿主挂在 App 顶层，单测里不走真对话框：mock 掉 useConfirm 的裁决，
// 断言"问过什么"与"裁决怎么分流"。
const confirmMock = vi.hoisted(() => vi.fn())

vi.mock('../../../../bindings/hanxi/internal/modules/sshkeys', () => ({
  SshKeysService: svc,
}))
vi.mock('../../../composables/useConfirm', () => ({
  useConfirm: () => ({ confirm: confirmMock }),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

// 形状照 bindings models 的 HostEntry / ExportSummary 逐字段造：字段名一旦与
// Go 侧漂移，这里的断言就报红，而不是静默渲染出一片"—"。
const NAS = {
  alias: 'nas01', host: '192.168.1.10', port: 0, user: 'deploy',
  keyPath: 'C:\\Users\\u\\.ssh\\id_ed25519', note: 'NAS 上的黑群晖', enabled: true,
}
const OFF = {
  alias: 'srv', host: '10.0.0.5', port: 2222, user: '',
  keyPath: '', note: '', enabled: false,
}
const CONSISTENT = {
  written: false, confPath: 'C:\\Users\\u\\.ssh\\hanxi.conf', included: true,
  rendered: 2, drift: false, backupPath: '', keyMissing: 0, message: '产物与账本一致。',
}
const NEVER_EXPORTED = {
  ...CONSISTENT, drift: true, message: '还没导出过：ssh 目前读不到 hanxi 的清单。',
}

// —— 台账夹具（ListKeys 返回的 KeyEntry，形状照 bindings models 逐字段造）——
// privatePath 与 suggestPath **刻意不同形**：下拉的选项值必须是后端给的 suggestPath
// （`~/名字` 跨机器稳定），实现一旦改拿 privatePath 或前端自拼，这一族用例全红。
const KEY_FULL = {
  name: 'id_ed25519', hasPrivate: true, hasPublic: true,
  privatePath: 'C:\\Users\\u\\.ssh\\id_ed25519', publicPath: 'C:\\Users\\u\\.ssh\\id_ed25519.pub',
  type: 'ed25519', fingerprint: 'SHA256:1111222233334444abcdefgh', comment: 'laptop', bits: 256,
  guarded: false, guardKnown: true, mtimeUnix: 1750000000,
  suggestPath: '~/id_ed25519', usedBy: ['nas01'], usedByKnown: true,
}
const KEY_NOPUB = {
  ...KEY_FULL, name: 'id_rsa_work', hasPublic: false,
  privatePath: 'C:\\Users\\u\\.ssh\\id_rsa_work', publicPath: '',
  type: 'rsa', fingerprint: 'SHA256:aaaabbbbccccdddd0000', bits: 3072,
  suggestPath: '~/id_rsa_work', usedBy: [], usedByKnown: true,
}
// 孤儿 .pub：hasPrivate=false 且 suggestPath 为空——两条判据都该把它挡在下拉外。
const KEY_PUBONLY = {
  ...KEY_FULL, name: 'orphan_pub', hasPrivate: false, hasPublic: true,
  privatePath: '', publicPath: 'C:\\Users\\u\\.ssh\\orphan_pub.pub',
  fingerprint: 'SHA256:eeeeffff0000', guarded: false, guardKnown: false,
  suggestPath: '', usedBy: [], usedByKnown: false,
}

function mockDefaults() {
  svc.ListHosts.mockResolvedValue([NAS, OFF])
  svc.ListKeys.mockResolvedValue([KEY_FULL, KEY_NOPUB, KEY_PUBONLY])
  svc.AddHost.mockResolvedValue([NAS, OFF])
  svc.UpdateHost.mockResolvedValue([NAS, OFF])
  svc.RemoveHost.mockResolvedValue([NAS, OFF])
  svc.ExportStatus.mockResolvedValue({ ...CONSISTENT })
  svc.ExportConf.mockResolvedValue({ ...CONSISTENT })
  svc.RemoveExport.mockResolvedValue({ ...CONSISTENT })
  confirmMock.mockResolvedValue(true)
}

async function mountPanel() {
  mockDefaults()
  const w = mount(HostsPanel)
  await flushPromises()
  return w
}

/** 面板顶部的按钮组（表单头的"收起"也在 .card-top 家族里，取第一组）。 */
function topBtns(w: Awaited<ReturnType<typeof mountPanel>>) {
  return w.findAll('.card > .card-top .btn-group .btn')
}

afterEach(() => vi.clearAllMocks())

describe('useHosts.aliasIssue：每条拦截都讲机制', () => {
  it('合法形状放行；空串给出"它是什么"的解释', () => {
    expect(aliasIssue('nas01')).toBe('')
    expect(aliasIssue('a_b-c')).toBe('')
    expect(aliasIssue('')).toContain('别名必填')
  })
  it('换行 = 往 ssh 配置里注入可执行指令', () => {
    expect(aliasIssue('nas\n01')).toContain('注入可执行指令')
    expect(aliasIssue('nas\r01')).toContain('注入可执行指令')
  })
  it('空格 = Host 行多开一个匹配模式', () => {
    expect(aliasIssue('nas 01')).toContain('匹配模式')
    expect(aliasIssue('nas\t01')).toContain('匹配模式')
  })
  it('* ? = 通配符，会把专属配置套到别的机器头上', () => {
    expect(aliasIssue('nas*')).toContain('通配符')
    expect(aliasIssue('na?s')).toContain('通配符')
  })
  it('大写 = 匹配区分大小写，会静默失效', () => {
    expect(aliasIssue('Nas01')).toContain('区分大小写')
  })
  it('前导 - 会被命令行当选项；尾随 - 按同一规则拒（唯一键形制）', () => {
    expect(aliasIssue('-nas')).toContain('选项')
    expect(aliasIssue('nas-')).toContain('sshconf')
  })
  it('超长与越界字符都给回显', () => {
    expect(aliasIssue('n'.repeat(33))).toContain('32')
    expect(aliasIssue('a@b')).toContain('@')
    expect(aliasIssue('a@b')).toContain('只允许小写字母')
  })
})

// —— 镜像用例表对账 ——
// 逐条照抄后端 packages/go/sshconf/sshconf_test.go TestValidateAlias 的样本表
// （2026-10-23 对账抄入）。aliasIssue 是刻意镜像（useHosts 头注），规则本身不许改；
// 这里不重测镜像的中文措辞（上面已逐条测过），只锁**判定结果与后端样本一致**：
// 后端规则哪天变松/变严而镜像没跟上，对应样本在这里报红。
const BACKEND_ALIAS_SAMPLES: Array<[string, boolean, string]> = [
  // —— 合法形态（后端放行）——
  ['web', false, '纯字母'],
  ['web-1', false, '字母+连字符+数字'],
  ['srv_01', false, '下划线'],
  ['db-master-01', false, '多连字符'],
  ['a', false, '单字符'],
  ['0', false, '纯数字'],
  ['z'.repeat(32), false, '恰好 32 字符'],
  // —— 注入面与越界形态（后端必须拒）——
  ['', true, '空串'],
  ['Web', true, '大写'],
  ['web.Example', true, '点'],
  ['a/b', true, '斜杠'],
  ['web 1', true, '空格'],
  ["web'1", true, '单引号'],
  ['web"1', true, '双引号'],
  ['web\nHost evil', true, '换行'],
  ['web\r\nHost evil', true, 'CRLF'],
  ['-web', true, '前导连字符'],
  ['web-', true, '尾随连字符'],
  ['web\t1', true, '制表符'],
  ['a'.repeat(33), true, '超长 33 字符'],
  ['web$HOME', true, '美元符'],
  ['web;evil', true, '分号'],
  ['web|nc evil', true, '管道符'],
  ['web`id`', true, '反引号'],
  ['中文主机', true, '非 ASCII'],
  ['\x00web', true, 'NUL'],
]

describe('useHosts.aliasIssue × sshconf TestValidateAlias：镜像样本表逐条一致', () => {
  for (const [alias, wantErr, why] of BACKEND_ALIAS_SAMPLES) {
    it(`${why}：${wantErr ? '拒' : '放行'}`, () => {
      expect(!!aliasIssue(alias)).toBe(wantErr)
    })
  }
})

describe('HostsPanel 清单渲染', () => {
  it('进面板读三路：账本 + 导出对账 + 台账（ListKeys 只为下拉供选项，绝不再当缺钥匙的旁证）；端口 0 显式记成 :22，停用行给「已停用」标', async () => {
    const w = await mountPanel()
    expect(svc.ListHosts).toHaveBeenCalledTimes(1)
    expect(svc.ExportStatus).toHaveBeenCalledTimes(1)
    // 2026-10-23 恢复挂载时拉台账：用途只有"表单下拉的选项来源"。旧用途（拿台账
    // 基名猜"钥匙在不在"）造成的误标帽子仍由下面密钥列一族用例钉死——行级"找不到"
    // 只看后端 missingKeyAliases 名单，台账名单不参与判断，hasLedgerKey 不许复活。
    expect(svc.ListKeys).toHaveBeenCalledTimes(1)
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('nas01')
    expect(rows[0].text()).toContain('192.168.1.10:22')
    expect(rows[1].text()).toContain('10.0.0.5:2222')
    expect(rows[1].text()).toContain('已停用')
  })

  it('账本为空是正常空态：文案点名「导出」这步与"钥匙不跟着账本走"，不显示对账带', async () => {
    mockDefaults()
    svc.ListHosts.mockResolvedValue([])
    const e = mount(HostsPanel)
    await flushPromises()
    expect(e.find('.empty-state').exists()).toBe(true)
    expect(e.text()).toContain('账本还是空的')
    expect(e.text()).toContain('导出')
    // 没有账本就没有可对的账：对账带让位给列表空态，不双空态
    expect(e.find('.export-band').exists()).toBe(false)
    expect(e.find('.error-box').exists()).toBe(false)
    e.unmount()
  })

  it('账本读取失败如实报错，不塌成"空账本"的假象', async () => {
    svc.ListHosts.mockRejectedValue(new Error('读取账本失败: 文件损坏'))
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT })
    const w = mount(HostsPanel)
    await flushPromises()
    expect(w.find('.error-box').text()).toContain('文件损坏')
    expect(w.find('.empty-state').exists()).toBe(false)
    w.unmount()
  })

  it('删除先过确认，文案点名「从清单移除不会删除磁盘上的私钥」；裁决为否则不动后端', async () => {
    const w = await mountPanel()
    svc.RemoveHost.mockResolvedValue([OFF])
    await w.findAll('tbody tr')[0].findAll('.btn-micro')[1].trigger('click')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(confirmMock.mock.calls[0][0].description).toContain('从清单移除不会删除磁盘上的私钥')
    await flushPromises()
    expect(svc.RemoveHost).toHaveBeenCalledWith('nas01')
    expect(w.findAll('tbody tr')).toHaveLength(1)
    // 删账本同样不自动碰用户主目录——只重读对账状态
    expect(svc.ExportConf).not.toHaveBeenCalled()
    expect(svc.ExportStatus).toHaveBeenCalledTimes(2)

    confirmMock.mockResolvedValue(false)
    await w.findAll('tbody tr')[0].findAll('.btn-micro')[1].trigger('click')
    await flushPromises()
    expect(svc.RemoveHost).toHaveBeenCalledTimes(1)
  })
})

describe('HostsPanel 启停与写失败复位', () => {
  it('勾选框拨动走 UpdateHost 整条覆盖；成功后只刷新对账，绝不自动导出', async () => {
    const w = await mountPanel()
    svc.UpdateHost.mockResolvedValue([{ ...NAS, enabled: false }, OFF])
    // 照 LogsView/WslUsbPanel 先例：DOM 置值 + change，不依赖 setChecked
    const box = w.findAll('input.row-toggle')[0]
    ;(box.element as HTMLInputElement).checked = false
    await box.trigger('change')
    await flushPromises()
    expect(svc.UpdateHost).toHaveBeenCalledWith({ ...NAS, enabled: false })
    expect(svc.ExportConf).not.toHaveBeenCalled()
    expect(svc.ExportStatus).toHaveBeenCalledTimes(2)
    // 漂移由重读的对账状态如实亮着（这里 mock 仍回一致，形态断言在下面带状用例里）
  })

  it('启停被后端拒：checkbox 拨回真实值，错误上屏——界面不挂一条账本没接受的开关', async () => {
    const w = await mountPanel()
    svc.UpdateHost.mockRejectedValue(new Error('清单里没有该别名：nas01'))
    const box = w.findAll('input.row-toggle')[0]
    ;(box.element as HTMLInputElement).checked = false
    await box.trigger('change')
    await flushPromises()
    expect((box.element as HTMLInputElement).checked).toBe(true)
    expect(w.find('.error-box').text()).toContain('清单里没有该别名')
  })
})

describe('HostsPanel 密钥列：基名上屏、路径进 title、钥匙缺失标（后端名单驱动）', () => {
  it('密钥列只显基名，完整路径退到 title，行文本不再出现目录', async () => {
    const w = await mountPanel()
    const keyCell = w.findAll('tbody tr')[0].findAll('td')[4]
    expect(w.findAll('thead th')[4].text()).toContain('密钥')
    expect(keyCell.find('.cell-keyname').text()).toBe('id_ed25519')
    expect(keyCell.find('.cell-keyname').attributes('title')).toBe('C:\\Users\\u\\.ssh\\id_ed25519')
    expect(w.findAll('tbody tr')[0].text()).not.toContain('C:\\Users')
  })

  it('钥匙缺失标由后端名单驱动：missingKeyAliases 点了名才标，未设密钥的行永不标', async () => {
    mockDefaults()
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT, missingKeyAliases: ['nas01'] })
    const w = mount(HostsPanel)
    await flushPromises()
    const rows = w.findAll('tbody tr')
    expect(rows[0].text()).toContain('本机找不到这把钥匙')
    // warning 档不是 danger：换机后"钥匙不跟账本走"是常态，不是故障。
    expect(rows[0].find('.chip').classes()).toContain('chip-warning')
    expect(rows[1].text()).toContain('未设')
    expect(rows[1].text()).not.toContain('本机找不到这把钥匙')
    w.unmount()
  })

  it('钥匙放在 ~/.ssh 之外、文件真实存在时不标——后端逐条 stat 没点名就是"在"', async () => {
    // 这条锁的是旧实现的误报：旧版自取台账做基名比对，而台账只扫 ~/.ssh，
    // 于是 D:\keys\id_rsa 这类真存在的钥匙会被戴"找不到"的帽子——而这顶帽子
    // 挂在删除决策现场。判据交回后端后，前端没有任何推断空间。
    mockDefaults()
    svc.ListHosts.mockResolvedValue([{ ...NAS, keyPath: 'D:\\keys\\id_rsa' }])
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT, missingKeyAliases: [] })
    const w = mount(HostsPanel)
    await flushPromises()
    const row = w.findAll('tbody tr')[0]
    expect(row.text()).not.toContain('本机找不到这把钥匙')
    expect(row.find('.cell-keyname').text()).toBe('id_rsa')
    w.unmount()
  })

  it('还没对账过（拿不到后端名单）时一律不标：宁缺勿错', async () => {
    mockDefaults()
    svc.ExportStatus.mockRejectedValue(new Error('对账失败'))
    const w = mount(HostsPanel)
    await flushPromises()
    expect(w.findAll('tbody tr')[0].text()).not.toContain('本机找不到这把钥匙')
    w.unmount()
  })
})

describe('HostsPanel 启用列：说清它是导出开关', () => {
  it('列头写明「启用 = 是否导出给 ssh」；行内 title 讲全机制并指路模块开关去处', async () => {
    const w = await mountPanel()
    const th = w.findAll('thead th')[0]
    expect(th.text()).toContain('启用')
    expect(th.text()).toContain('是否导出给 ssh')
    const box = w.find('input.row-toggle')
    expect(box.attributes('title')).toContain('是否导出给 ssh')
    expect(box.attributes('title')).toContain('账本保留')
    expect(box.attributes('title')).toContain('模块开关在 设置→模块管理')
    expect(box.attributes('aria-label')).toContain('nas01')
    expect(box.attributes('aria-label')).toContain('现在导出')
  })

  it('停用行遇 drift 与对账带呼应"旧产物里这台仍在"；一致态不造第二套状态提示', async () => {
    mockDefaults()
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT, drift: true, message: '账本已改动，产物还是旧的。' })
    const w = mount(HostsPanel)
    await flushPromises()
    const offCell = w.findAll('tbody tr')[1].findAll('td')[0]
    expect(offCell.text()).toContain('还没重新导出')
    expect(offCell.text()).toContain('ssh 暂时还认得它')
    // 启用行不在呼应范围
    expect(w.findAll('tbody tr')[0].findAll('td')[0].text()).not.toContain('还没重新导出')

    mockDefaults()
    const c = mount(HostsPanel)
    await flushPromises()
    expect(c.text()).not.toContain('还没重新导出')
    c.unmount()
    w.unmount()
  })

  it('勾选框仍是原生 checkbox（全仓无 switch 原子，不私造）', async () => {
    const w = await mountPanel()
    expect(w.find('input.row-toggle').attributes('type')).toBe('checkbox')
  })
})

describe('HostsPanel 导出对账带', () => {
  it('一致态：chip positive + 后端 message 原文 + 产物路径明写', async () => {
    const w = await mountPanel()
    expect(w.find('.export-band .chip').classes()).toContain('chip-positive')
    expect(w.find('.export-band .chip').text()).toContain('产物与账本一致')
    expect(w.text()).toContain('C:\\Users\\u\\.ssh\\hanxi.conf')
  })

  it('漂移态：chip warning 亮「账本已改动，产物还是旧的」，等人点不代点', async () => {
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT, drift: true, message: '账本已改动，产物还是旧的。' })
    const w = mount(HostsPanel)
    await flushPromises()
    expect(w.find('.export-band .chip').classes()).toContain('chip-warning')
    expect(w.find('.export-band .chip').text()).toContain('账本已改动，产物还是旧的')
    expect(w.find('.export-band .btn-primary').text()).toContain('同步给 ssh')
    w.unmount()
  })

  it('未导出过 + 缺 Include 接线（未接线态）：两句单独摊开，不绿给假安心', async () => {
    svc.ExportStatus.mockResolvedValue({ ...NEVER_EXPORTED, included: false })
    const w = mount(HostsPanel)
    await flushPromises()
    expect(w.find('.export-band .chip').text()).toContain('还没导出过：ssh 目前读不到 hanxi 的清单')
    const warn = w.findAll('.banner-warn')
    expect(warn.map(b => b.text()).join()).toContain('Include')
    w.unmount()
  })

  it('缺钥匙态：数出条数，并解释为什么这正常（账本搬家、私钥不在数据根）', async () => {
    svc.ExportStatus.mockResolvedValue({ ...CONSISTENT, keyMissing: 3 })
    const w = mount(HostsPanel)
    await flushPromises()
    const miss = w.findAll('.banner-warn').map(b => b.text()).join()
    expect(miss).toContain('3 条的私钥文件在本机不存在')
    expect(miss).toContain('数据根')
    w.unmount()
  })

  it('「同步给 ssh」成功后把落盘路径原样上屏，有备份也一并显示', async () => {
    const w = await mountPanel()
    svc.ExportConf.mockResolvedValue({
      ...CONSISTENT, written: true,
      backupPath: 'C:\\Users\\u\\.ssh\\hanxi.conf.hanxi.bak',
      message: '已写入并复验一致：C:\\Users\\u\\.ssh\\hanxi.conf',
    })
    await w.find('.export-band .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.ExportConf).toHaveBeenCalledTimes(1)
    expect(w.text()).toContain('C:\\Users\\u\\.ssh\\hanxi.conf')
    expect(w.text()).toContain('C:\\Users\\u\\.ssh\\hanxi.conf.hanxi.bak')
    expect(w.text()).toContain('已写入并复验一致')
  })

  it('写失败态：后端原文上屏，对账带保留上一次摊开的账，不塌成"没导出过"', async () => {
    const w = await mountPanel()
    svc.ExportConf.mockRejectedValue(new Error('写入 C:\\Users\\u\\.ssh\\hanxi.conf 失败: permission denied'))
    await w.find('.export-band .btn-primary').trigger('click')
    await flushPromises()
    expect(w.find('.export-band .banner-error').text()).toContain('permission denied')
    expect(w.find('.export-band .chip').text()).toContain('产物与账本一致')
    // 失败绝不伪装成新状态：既不能说"还没导出过"，也不能亮"下一步"把人绕回起点
    expect(w.find('.export-band').text()).not.toContain('还没导出过：')
    expect(w.find('.export-band').text()).not.toContain('下一步')
  })

  it('操作面板语法：头行左一句状态、右一组动作，主/次同组同档（都在 .btn-group 里）', async () => {
    const w = await mountPanel()
    const head = w.find('.export-band .band-head')
    expect(head.find('.band-status .chip').text()).toContain('产物与账本一致')
    const btns = head.findAll('.btn-group .btn')
    expect(btns).toHaveLength(2)
    expect(btns[0].classes()).toContain('btn-primary')
    expect(btns[0].text()).toContain('同步给 ssh')
    expect(btns[1].classes()).toContain('btn-secondary')
    expect(btns[1].text()).toContain('撤销导出')
  })

  it('还没导出过 = 空态语法：给"下一步：点同步"的动线，不写"暂无数据"', async () => {
    mockDefaults()
    svc.ExportStatus.mockResolvedValue({ ...NEVER_EXPORTED })
    const w = mount(HostsPanel)
    await flushPromises()
    expect(w.find('.export-band .chip').text()).toContain('还没导出过')
    expect(w.find('.export-band').text()).toContain('下一步')
    expect(w.find('.export-band').text()).toContain('同步给 ssh')
    expect(w.text()).not.toContain('暂无数据')
    w.unmount()
  })

  it('导出成功后 confPath 与 backupPath 常驻可见：mono 上屏、各带复制钮', async () => {
    const w = await mountPanel()
    svc.ExportConf.mockResolvedValue({
      ...CONSISTENT, written: true,
      backupPath: 'C:\\Users\\u\\.ssh\\hanxi.conf.hanxi.bak',
      message: '已写入并复验一致：C:\\Users\\u\\.ssh\\hanxi.conf',
    })
    await w.find('.export-band .btn-primary').trigger('click')
    await flushPromises()
    const band = w.find('.export-band')
    expect(band.text()).toContain('C:\\Users\\u\\.ssh\\hanxi.conf')
    expect(band.text()).toContain('C:\\Users\\u\\.ssh\\hanxi.conf.hanxi.bak')
    const monos = band.findAll('.mono').map(m => m.text())
    expect(monos.some(m => m.includes('hanxi.conf'))).toBe(true)
    expect(monos.some(m => m.includes('.hanxi.bak'))).toBe(true)
    // 两条路径各挂一颗复制钮（凭据要能一键带走）
    expect(band.findAll('.link-button')).toHaveLength(2)
    // 已落盘且无漂移：下一步提示撤走，不留假待办
    expect(band.text()).not.toContain('下一步')
  })

  it('「撤销导出」先确认且讲清"删产物、摘自己的 Include、你的 config 不删"；回执与重读都跟上', async () => {
    const w = await mountPanel()
    svc.RemoveExport.mockResolvedValue({
      ...CONSISTENT, written: true, included: false, rendered: 0, drift: true,
      message: '已撤销：产物已删除，顶层 config 里那段 Include 也已摘掉；你自己的 config 未被删除。',
    })
    await w.find('.export-band .btn-secondary').trigger('click')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    const desc = confirmMock.mock.calls[0][0].description
    expect(desc).toContain('Include')
    expect(desc).toContain('不删')
    await flushPromises()
    expect(svc.RemoveExport).toHaveBeenCalledTimes(1)
    expect(w.text()).toContain('已撤销')
    // 撤销后重读对账：产物没了就如实说"还没导出过"，状态不留幽灵
    expect(svc.ExportStatus).toHaveBeenCalledTimes(2)
  })
})

describe('HostsPanel 新增/编辑表单', () => {
  it('编辑态锁别名：输入框禁用、值可见，并如实解释主键语义', async () => {
    const w = await mountPanel()
    await w.findAll('tbody tr')[0].findAll('.btn-micro')[0].trigger('click')
    const alias = w.find('#hosts-alias')
    expect((alias.element as HTMLInputElement).disabled).toBe(true)
    expect((alias.element as HTMLInputElement).value).toBe('nas01')
    expect(w.find('.hosts-form').text()).toContain('主键')
    // 编辑走 UpdateHost 整条覆盖，其余字段原样带回
    svc.UpdateHost.mockResolvedValue([NAS, OFF])
    await w.find('#hosts-note').setValue('改过的备注')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.UpdateHost).toHaveBeenCalledWith({ ...NAS, note: '改过的备注' })
    expect(svc.AddHost).not.toHaveBeenCalled()
  })

  it('别名机制文案即时挂出：大写/空格/通配符/前导连字符各说各的为什么', async () => {
    const w = await mountPanel()
    await topBtns(w)[0].trigger('click') // 新增主机
    const alias = w.find('#hosts-alias')
    await alias.setValue('Nas01')
    await alias.trigger('blur')
    expect(w.find('#hosts-alias-msg').text()).toContain('区分大小写')
    await alias.setValue('na s')
    expect(w.find('#hosts-alias-msg').text()).toContain('匹配模式')
    await alias.setValue('nas*')
    expect(w.find('#hosts-alias-msg').text()).toContain('通配符')
    await alias.setValue('-nas')
    expect(w.find('#hosts-alias-msg').text()).toContain('选项')
    expect(w.find('#hosts-alias-msg').classes()).toContain('field-error')
  })

  it('重名在保存前拦：账本已有 nas01 时新增同名，AddHost 不被调用', async () => {
    const w = await mountPanel()
    await topBtns(w)[0].trigger('click')
    await w.find('#hosts-alias').setValue('nas01')
    await w.find('#hosts-alias').trigger('blur')
    expect(w.find('#hosts-alias-msg').text()).toContain('已有一台')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.AddHost).not.toHaveBeenCalled()
  })

  it('新增发出结构化字段（端口空按 0）；成功后表单收起、账本整表更新、只重读对账', async () => {
    const w = await mountPanel()
    const added = [NAS, OFF, { alias: 'lab', host: '10.1.1.2', port: 0, user: 'ubuntu', keyPath: '', note: '实验室机', enabled: true }]
    svc.AddHost.mockResolvedValue(added)
    await topBtns(w)[0].trigger('click')
    await w.find('#hosts-alias').setValue('lab')
    await w.find('#hosts-host').setValue('10.1.1.2')
    await w.find('#hosts-user').setValue('ubuntu')
    await w.find('#hosts-note').setValue('实验室机')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.AddHost).toHaveBeenCalledWith({
      alias: 'lab', host: '10.1.1.2', port: 0, user: 'ubuntu', keyPath: '', note: '实验室机', enabled: true,
    })
    expect(svc.ExportStatus).toHaveBeenCalledTimes(2)
    expect(svc.ExportConf).not.toHaveBeenCalled()
    expect((w.find('.hosts-form').element as HTMLElement).style.display).toBe('none')
    expect(w.findAll('tbody tr')).toHaveLength(3)
  })

  it('校验红时保存钮仍可点；后端拒绝的原文落回表单现场，草稿不丢', async () => {
    const w = await mountPanel()
    await topBtns(w)[0].trigger('click')
    await w.find('#hosts-alias').setValue('nas01-ok')
    await w.find('#hosts-host').setValue('10.0.0.9')
    await w.find('.hosts-form .btn-primary').trigger('click')
    // 端口越界的即时文案（不预设后端会先拒什么，前端轻拦先讲清记法）
    await w.find('#hosts-port').setValue('70000')
    await w.find('#hosts-port').trigger('blur')
    expect(w.find('#hosts-port-msg').text()).toContain('65535')
    await w.find('#hosts-port').setValue('22')
    svc.AddHost.mockRejectedValue(new Error('主机 "nas01-ok": HostName 只填 IP 或域名'))
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(w.find('.hosts-form .error-box').text()).toContain('HostName 只填 IP 或域名')
    // 表单没关：草稿留在原地改，而不是让人从头再填一遍
    expect((w.find('#hosts-alias').element as HTMLInputElement).value).toBe('nas01-ok')
  })
})

describe('HostsPanel 表单密钥下拉：台账当选项源（值=suggestPath、可手输、缺公钥提示、静默降级）', () => {
  // 打开新增表单并取下拉。表单是 v-show 常驻 DOM，但一律走真实路径先开窗再断言。
  async function openAddGetSelect(w: Awaited<ReturnType<typeof mountPanel>>) {
    await topBtns(w)[0].trigger('click')
    await flushPromises()
    return w.find('#hosts-keypath-select')
  }

  it('选项值取 suggestPath 而非 privatePath；label 是「名字（算法 · 指纹前 8）」且不放完整路径', async () => {
    const w = await mountPanel()
    const sel = await openAddGetSelect(w)
    const opt = sel.findAll('option').find(o => o.text().includes('id_ed25519'))
    expect(opt).toBeDefined()
    expect(opt!.attributes('value')).toBe('~/id_ed25519')
    expect(opt!.text()).toContain('ed25519')
    expect(opt!.text()).toContain('11112222') // 指纹剥掉 SHA256: 前缀后取前 8 位
    expect(opt!.text()).not.toContain('C:\\Users') // 完整路径不进 label
    // 夹具的 privatePath 与 suggestPath 刻意不同形：任何选项的值都不许是绝对路径——
    // 拿 suggestPath（换用户名不漂）当值是后端契约，前端不许改拿 privatePath 或自拼。
    expect(sel.findAll('option').some(o => o.attributes('value') === 'C:\\Users\\u\\.ssh\\id_ed25519')).toBe(false)
  })

  it('只有 hasPrivate 且 suggestPath 非空的行进下拉；孤儿 .pub 不出现', async () => {
    const w = await mountPanel()
    const sel = await openAddGetSelect(w)
    const pathValues = sel.findAll('option')
      .map(o => o.attributes('value'))
      .filter(v => v?.startsWith('~/'))
      .sort()
    expect(pathValues).toEqual(['~/id_ed25519', '~/id_rsa_work'])
    expect(sel.text()).not.toContain('orphan_pub')
  })

  it('选中项同步输入框；缺公钥的当场联动提示（hint-line + warning 字色档），有公钥的安静；首项可清空；保存发出 ~/ 形态', async () => {
    const w = await mountPanel()
    const sel = await openAddGetSelect(w)
    await sel.setValue('~/id_rsa_work')
    expect((w.find('#hosts-keypath').element as HTMLInputElement).value).toBe('~/id_rsa_work')
    const hint = w.find('#hosts-keypath-hint')
    expect(hint.exists()).toBe(true)
    expect(hint.classes()).toContain('hint-line')
    expect(hint.classes()).toContain('state-warning') // 既有全局原子档，不新造样式
    expect(hint.text()).toContain('还没有配套公钥')
    expect(hint.text()).toContain('部署公钥')
    expect(hint.text()).toContain('补出公钥')
    // 有配套公钥的不搅局
    await sel.setValue('~/id_ed25519')
    expect(w.find('#hosts-keypath-hint').exists()).toBe(false)
    // 「清空选择」的出口：第一项未指定把输入框抹平
    await sel.setValue('')
    expect((w.find('#hosts-keypath').element as HTMLInputElement).value).toBe('')
    // 再选回并保存：发出的 keyPath 就是 suggestPath
    await sel.setValue('~/id_ed25519')
    await w.find('#hosts-alias').setValue('work01')
    await w.find('#hosts-host').setValue('10.9.9.9')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.AddHost).toHaveBeenCalledWith({
      alias: 'work01', host: '10.9.9.9', port: 0, user: '', keyPath: '~/id_ed25519', note: '', enabled: true,
    })
  })

  it('仍可纯手输任意路径并成功保存：下拉落到禁用的「自定义」哨兵，不据此猜出假提示', async () => {
    const w = await mountPanel()
    const sel = await openAddGetSelect(w)
    await w.find('#hosts-keypath').setValue('D:\\keys\\id_rsa')
    expect((sel.element as HTMLSelectElement).value).toBe('__custom_keypath__')
    const shown = sel.findAll('option').find(o => (o.element as HTMLOptionElement).selected)
    expect(shown!.text()).toContain('自定义')
    expect(shown!.attributes('disabled')).toBeDefined() // 哨兵只回显状态，用户选不走
    // 手输的路径台账里没有：不猜它有没有公钥，联动提示不许冒出来
    expect(w.find('#hosts-keypath-hint').exists()).toBe(false)
    await w.find('#hosts-alias').setValue('lab')
    await w.find('#hosts-host').setValue('10.1.1.2')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.AddHost).toHaveBeenCalledWith({
      alias: 'lab', host: '10.1.1.2', port: 0, user: '', keyPath: 'D:\\keys\\id_rsa', note: '', enabled: true,
    })
  })

  it('读台账失败静默降级：下拉里说清楚，输入框照常手输保存，不弹错不阻断', async () => {
    mockDefaults()
    svc.ListKeys.mockRejectedValue(new Error('台账目录读取失败'))
    const w = mount(HostsPanel)
    await flushPromises()
    await topBtns(w)[0].trigger('click') // 打开表单还会重拉一次——依旧失败，依旧静默
    await flushPromises()
    const sel = w.find('#hosts-keypath-select')
    expect(sel.text()).toContain('本机密钥列表读取失败，可直接输入路径')
    expect(sel.findAll('option').filter(o => o.attributes('value')?.startsWith('~/'))).toHaveLength(0)
    // 静默 = 不把错误文案/错误框抬到面板上：读不到台账绝不能劫持清单保存
    expect(w.text()).not.toContain('台账目录读取失败')
    expect(w.findAll('.error-box')).toHaveLength(0)
    await w.find('#hosts-alias').setValue('lab4')
    await w.find('#hosts-host').setValue('10.4.4.4')
    await w.find('#hosts-keypath').setValue('D:\\k\\id_ed')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.AddHost).toHaveBeenCalledWith({
      alias: 'lab4', host: '10.4.4.4', port: 0, user: '', keyPath: 'D:\\k\\id_ed', note: '', enabled: true,
    })
  })

  it('台账为空给禁用的「本机还没有密钥，可先去台账生成」项，不留空下拉；输入框照常可用', async () => {
    mockDefaults()
    svc.ListKeys.mockResolvedValue([])
    const w = mount(HostsPanel)
    await flushPromises()
    await topBtns(w)[0].trigger('click')
    await flushPromises()
    const sel = w.find('#hosts-keypath-select')
    const ph = sel.findAll('option').find(o => o.text().includes('本机还没有密钥'))
    expect(ph).toBeDefined()
    expect(ph!.attributes('disabled')).toBeDefined()
    // 空台账与读取失败是两种情况，文案不许混
    expect(sel.text()).not.toContain('读取失败')
    await w.find('#hosts-keypath').setValue('~/self_written')
    expect((w.find('#hosts-keypath').element as HTMLInputElement).value).toBe('~/self_written')
  })

  it('打开表单/开始编辑都重拉一次台账：刚在台账生成的钥匙当场选得到', async () => {
    mockDefaults()
    svc.ListKeys.mockResolvedValueOnce([]).mockResolvedValue([KEY_FULL, KEY_NOPUB, KEY_PUBONLY])
    const w = mount(HostsPanel)
    await flushPromises()
    expect(svc.ListKeys).toHaveBeenCalledTimes(1) // 挂载随 refreshAll 拉第一遍
    await topBtns(w)[0].trigger('click')
    await flushPromises()
    expect(svc.ListKeys).toHaveBeenCalledTimes(2) // 开表单重拉
    const values = w.findAll('#hosts-keypath-select option').map(o => o.attributes('value'))
    expect(values).toContain('~/id_ed25519') // 第二遍才出现的钥匙直接进下拉，不必重开面板
    await w.findAll('tbody tr')[0].findAll('.btn-micro')[0].trigger('click')
    await flushPromises()
    expect(svc.ListKeys).toHaveBeenCalledTimes(3) // 编辑同样重拉
  })

  it('编辑既有主机的绝对路径显示「自定义」且不改写存值——下拉只回显，不擅动已有的账', async () => {
    const w = await mountPanel()
    await w.findAll('tbody tr')[0].findAll('.btn-micro')[0].trigger('click')
    await flushPromises()
    expect((w.find('#hosts-keypath').element as HTMLInputElement).value).toBe('C:\\Users\\u\\.ssh\\id_ed25519')
    // NAS 的 keyPath 恰好等于 KEY_FULL.privatePath：若下拉错把 privatePath 当匹配值，
    // 这里会选中那把钥匙而不是落到自定义哨兵——此断言钉死"只认 suggestPath"。
    expect((w.find('#hosts-keypath-select').element as HTMLSelectElement).value).toBe('__custom_keypath__')
    await w.find('.hosts-form .btn-primary').trigger('click')
    await flushPromises()
    expect(svc.UpdateHost).toHaveBeenCalledWith(NAS)
  })
})
