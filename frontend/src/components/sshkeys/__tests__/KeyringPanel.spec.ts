// 钥匙环台账面板的特征测试。钉的是上一轮点名的五条硬伤的界面侧兑现：
// ① 一把钥匙（私钥 + 配套 .pub）只占一行——按文件列导致"同指纹两行、删错东西"；
// ② 整页级安全长句全页只出现一次（风险概要摊开处），行内只有短标签；
// ③ 空态给三步出路与页内唯一主按钮，不停在"暂无"；
// ④ guardKnown=false 有独立档位与文案（绝不塌成"无口令"）；
// ⑤ usedBy 直读后端值渲染成「被 N 台主机在用 / 清单里没有主机用它」（防误删列）。
// 另有：加载指名在读什么、读失败保留上次结果并明说 stale、私钥内容永不落地；
// 半把钥匙两类行的动作入口互斥矩阵（「补出公钥」只给独身私钥行且不看 guarded、
// 「删除公钥」只给孤儿 .pub 行，成对行两个都不给）。
//
// 绑定面打桩纪律（旧事故在案）：vi.mock 工厂提升 → 桩件必须 vi.hoisted；
// mock 的命名空间键名与 bindings 逐字一致；夹具按 models.js 的真实字段名
// （KeyEntry，camelCase）构造，字段漂移即红。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import KeyringPanel from '../KeyringPanel.vue'

const svc = vi.hoisted(() => ({ ListKeys: vi.fn() }))
vi.mock('../../../../bindings/hanxi/internal/modules/sshkeys', () => ({
  SshKeysService: svc,
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: () => vi.fn() } }))
vi.mock('../../../composables/useClipboard', () => ({
  useClipboard: () => ({ copy: vi.fn(async () => true), paste: vi.fn(), copyWithToast: vi.fn(async () => true) }),
}))

// —— 夹具：字段名逐一照 bindings/hanxi/internal/modules/sshkeys/models.js 的 KeyEntry ——
const FP1 = 'SHA256:AAAAbbbbCCCCddddEEEEffffGGGGhhhh'
// 私钥与配套公钥齐整、无口令、没人引用。
const PLAIN = {
  name: 'id_plain', hasPrivate: true, hasPublic: true,
  privatePath: '/home/u/.ssh/id_plain', publicPath: '/home/u/.ssh/id_plain.pub',
  type: 'ssh-ed25519', fingerprint: FP1, comment: 'laptop', bits: 256,
  guarded: false, guardKnown: true, mtimeUnix: 1730000000, usedBy: [], usedByKnown: true,
}
// 有口令、被两条清单主机引用（防误删信号的主样本）。
const GUARD = {
  ...PLAIN, name: 'id_guard',
  privatePath: '/home/u/.ssh/id_guard', publicPath: '/home/u/.ssh/id_guard.pub',
  guarded: true, comment: 'nas', usedBy: ['nas01', 'work'], usedByKnown: true,
}
// 第三态：guardKnown=false，判不出来。
const UNKNOWN = {
  ...PLAIN, name: 'id_weird',
  privatePath: '/home/u/.ssh/id_weird', publicPath: '/home/u/.ssh/id_weird.pub',
  guardKnown: false, guarded: false, comment: '',
}
// 缺配套 .pub 且带口令：指纹取不到，部署不可用。
const NOPUB = {
  ...PLAIN, name: 'id_nopub', hasPublic: false,
  privatePath: '/home/u/.ssh/id_nopub', publicPath: undefined,
  guarded: true, fingerprint: '', comment: '',
}
// 孤儿 .pub：只有公钥。后端给它的 guardKnown/guarded 是零值 false——若判定顺序
// 不先分诊"没有私钥"，这条会被误报成"判不出来"。夹具如实按零值构造，不美化。
const ORPHAN = {
  name: 'id_orphan', hasPrivate: false, hasPublic: true,
  privatePath: undefined, publicPath: '/home/u/.ssh/id_orphan.pub',
  type: 'ssh-rsa', fingerprint: 'SHA256:ORPHORPHORPHORPHORPHORPHORPHORP', comment: '', bits: 3072,
  // 未判（没有私钥路径可比）：既不能说"被在用"，也不能说"没人用"。
  guarded: false, guardKnown: false, mtimeUnix: 1730000001, usedBy: [], usedByKnown: false,
}

async function mountWith(keys: unknown[]) {
  svc.ListKeys.mockResolvedValue(keys)
  const w = mount(KeyringPanel)
  await flushPromises()
  return w
}

async function expandRow(w: ReturnType<typeof mount>, idx: number) {
  await w.findAll('.keyrow')[idx].find('.keyrow-toggle').trigger('click')
  await flushPromises()
}

function occurrences(hay: string, needle: string): number {
  return hay.split(needle).length - 1
}

afterEach(() => vi.clearAllMocks())

describe('KeyringPanel 台账渲染（一把钥匙一行）', () => {
  it('挂载即读一次；口令四态各有短标签，判不出来是独立档位', async () => {
    const w = await mountWith([PLAIN, GUARD, UNKNOWN, ORPHAN])
    expect(svc.ListKeys).toHaveBeenCalledTimes(1)

    const chips = w.findAll('.chip').map(c => c.text())
    expect(chips).toContain('无口令')
    expect(chips).toContain('有口令')
    expect(chips).toContain('判不出来')
    expect(chips).toContain('仅公钥')
  })

  it('guardKnown=false 有独立外观与文案：neutral 档 + 说"判不出来"，绝不塌成"无口令"', async () => {
    const w = await mountWith([PLAIN, UNKNOWN])
    const unknownRow = w.findAll('.keyrow')[1]
    const chip = unknownRow.find('.chip')
    expect(chip.text()).toBe('判不出来')
    expect(chip.classes()).toContain('chip-neutral')
    // 对照组：无口令是 warning 档——两态外观必须不同
    const plainChip = w.findAll('.keyrow')[0].find('.chip')
    expect(plainChip.text()).toBe('无口令')
    expect(plainChip.classes()).toContain('chip-warning')
    // 该行整行文字不得把未知报成已知
    expect(unknownRow.text()).not.toContain('无口令')
  })

  it('孤儿 .pub 行是"仅公钥"且明说公钥不涉及口令，不冒充"判不出来"', async () => {
    const w = await mountWith([ORPHAN])
    const row = w.find('.keyrow')
    expect(row.find('.chip').text()).toBe('仅公钥')
    expect(row.find('.chip').classes()).toContain('chip-neutral')
    expect(row.text()).toContain('公钥不涉及口令')
    expect(row.text()).not.toContain('判不出来')
  })

  // —— 锁①：一把钥匙的私钥与公钥只占一行 ——
  it('一把钥匙只占一行：四行台账 = 四个键名，.pub 不单独立行', async () => {
    const w = await mountWith([PLAIN, GUARD, UNKNOWN, NOPUB, ORPHAN])
    const rows = w.findAll('.keyrow')
    expect(rows).toHaveLength(5)
    const names = rows.map(r => r.find('.keyrow-name').text())
    expect(names).toEqual(['id_plain', 'id_guard', 'id_weird', 'id_nopub', 'id_orphan'])
    // "id_plain.pub" 只在展开的路径字段里出现，不作为行名/行标识存在
    expect(names).not.toContain('id_plain.pub')
    // 同一把的两条路径同在一行展开区里（合并呈现，而不是两行各一条路径）
    await expandRow(w, 0)
    const detail = w.findAll('.keyrow')[0].find('.keyrow-detail')
    expect(detail.text()).toContain('/home/u/.ssh/id_plain')
    expect(detail.text()).toContain('/home/u/.ssh/id_plain.pub')
  })

  it('「在用」列直读 usedBy：非空列别名，空说"清单里没有主机用它"', async () => {
    const w = await mountWith([GUARD, PLAIN])
    expect(w.findAll('.keyrow')[0].text()).toContain('被 2 台主机在用：nas01、work')
    expect(w.findAll('.keyrow')[1].text()).toContain('清单里没有主机用它')
  })

  it('整页级安全长句全页只说一次：概要摊开 + 全部行展开，复读次数仍为 1', async () => {
    const LONG = '任何能以你身份读到该文件的程序'
    const w = await mountWith([PLAIN, GUARD, UNKNOWN])
    // 概要一句话：分母是有私钥的 3 把，GUARD 有口令不计风险
    expect(w.text()).toContain('3 把里有 1 把无口令保护 · 1 把判不出来')
    // 收起时整页不出现长句（行内只有短标签）
    expect(occurrences(w.text(), LONG)).toBe(0)
    await w.find('.keyrisk-toggle').trigger('click')
    for (let i = 0; i < 3; i++) await expandRow(w, i)
    expect(occurrences(w.text(), LONG)).toBe(1)
  })

  it('风险概要只在存在风险时是展开钮；全都有口令时收成一句陈述', async () => {
    const w = await mountWith([GUARD])
    expect(w.find('.keyrisk-toggle').exists()).toBe(false)
    expect(w.find('.keyrisk-plain').text()).toBe('1 把都有口令保护')
  })

  it('部署公钥钮常驻动作段（不靠 hover）；缺配套 .pub 的行禁用并给原因', async () => {
    const w = await mountWith([NOPUB, PLAIN])
    const disabled = w.findAll('.kops-act-deploy')[0]
    expect((disabled.element as HTMLButtonElement).disabled).toBe(true)
    expect(disabled.attributes('title')).toContain('无法部署')
    expect((w.findAll('.kops-act-deploy')[1].element as HTMLButtonElement).disabled).toBe(false)
  })

  it('展开区给两条路径/完整指纹可复制/修改时间；空指纹说清为什么而不是给复制钮', async () => {
    const w = await mountWith([PLAIN, NOPUB])
    await expandRow(w, 0)
    const d0 = w.findAll('.keyrow')[0].find('.keyrow-detail')
    expect(d0.text()).toContain(FP1)
    const copy = d0.find('.kops-act-fpcopy')
    expect(copy.exists()).toBe(true)
    expect(copy.attributes('title')).toContain(FP1)

    await expandRow(w, 1)
    const d1 = w.findAll('.keyrow')[1].find('.keyrow-detail')
    expect(d1.text()).toContain('取不到')
    expect(d1.find('.kops-act-fpcopy').exists()).toBe(false)
    expect(d1.text()).toContain('缺配套 .pub')
  })

  it('次要动作收进展开区，删除带不可逆提示且仅私钥行有', async () => {
    const w = await mountWith([PLAIN, ORPHAN])
    expect(w.find('.kops-act-pass').exists()).toBe(false)
    expect(w.find('.kops-act-del').exists()).toBe(false)
    await expandRow(w, 0)
    expect(w.find('.kops-act-pass').exists()).toBe(true)
    expect(w.find('.kops-act-del').text()).toContain('不可逆')
    // 孤儿 .pub 展开也没有删除（后端 DeleteKey 拒 .pub 路径，给了必被拒）
    await expandRow(w, 1)
    const orphan = w.findAll('.keyrow')[1]
    expect(orphan.find('.kops-act-del').exists()).toBe(false)
    expect(orphan.find('.keyrow-detail').text()).toContain('孤儿 .pub')
  })

  // —— 半把钥匙的两类行：新入口互斥矩阵（验收锁「两类行各自只出现自己的按钮」）——
  it('两类半把行各给各的出口：独身私钥行「补出公钥」在动作段，孤儿行「删除公钥」在展开区，成对行两个都没有', async () => {
    const w = await mountWith([PLAIN, NOPUB, ORPHAN])
    const rows = w.findAll('.keyrow')
    // 成对行（两把都有）：维持现状——部署 + 展开区的改口令/成对删除，两个新钮都不在
    expect(rows[0].find('.kops-act-export').exists()).toBe(false)
    expect(rows[0].find('.kops-act-delpub').exists()).toBe(false)
    // 只有私钥的行：「补出公钥」常驻动作段（不靠展开、不靠 hover），且可点
    const exp = rows[1].find('.kops-act-export')
    expect(exp.exists()).toBe(true)
    expect((exp.element as HTMLButtonElement).disabled).toBe(false)
    await expandRow(w, 1)
    const r1 = w.findAll('.keyrow')[1]
    // 独身私钥行拿到的不是「删除公钥」（公钥本就不在），成对删除仍是正路
    expect(r1.find('.kops-act-delpub').exists()).toBe(false)
    expect(r1.find('.kops-act-del').exists()).toBe(true)
    // 孤儿 .pub 行：没有「补出公钥」（无私钥可算）；展开后只有「删除公钥」
    const w2 = await mountWith([ORPHAN])
    expect(w2.find('.kops-act-export').exists()).toBe(false)
    expect(w2.find('.kops-act-delpub').exists()).toBe(false) // 次要区：展开才在场
    await expandRow(w2, 0)
    const orow = w2.find('.keyrow')
    expect(orow.find('.kops-act-delpub').exists()).toBe(true)
    expect(orow.find('.kops-act-del').exists()).toBe(false)
    expect(orow.find('.kops-act-pass').exists()).toBe(false)
    expect(orow.find('.keyrow-detail').text()).toContain('不会被回收')
    w.unmount()
    w2.unmount()
  })

  it('「补出公钥」的出现与否不看 guarded：带口令与无口令的独身私钥行同等待遇', async () => {
    const free = { ...NOPUB, name: 'id_nofree', privatePath: '/home/u/.ssh/id_nofree', guarded: false, comment: 'downloaded' }
    const w = await mountWith([NOPUB, free]) // NOPUB 夹具本身 guarded:true
    expect(w.findAll('.kops-act-export')).toHaveLength(2)
    // 成对删除的 title/说明行仍按私钥分诊，不受影响
    w.unmount()
  })

  it('私钥内容一律不出现（界面只拿得到路径与指纹）', async () => {
    const w = await mountWith([PLAIN, GUARD])
    await expandRow(w, 0)
    const html = w.html()
    expect(html).not.toContain('PRIVATE KEY-----')
    expect(html).not.toContain('-----BEGIN')
  })

  it('元数据行是"算法 · 位数 · 注释"，路径与长指纹不占主视觉', async () => {
    const w = await mountWith([PLAIN])
    const meta = w.find('.keyrow-meta').text()
    expect(meta).toContain('ssh-ed25519')
    expect(meta).toContain('256')
    expect(meta).toContain('laptop')
    // 收起状态下，行主体不含路径/完整指纹（都在展开区）
    expect(w.find('.keyrow-head').text()).not.toContain('/home/u/.ssh/id_plain')
    expect(w.find('.keyrow-head').text()).not.toContain(FP1)
  })

  it('加载中指名在读什么', async () => {
    let release: (v: unknown[]) => void = () => {}
    svc.ListKeys.mockImplementation(() => new Promise(resolve => { release = resolve }))
    const w = mount(KeyringPanel)
    await flushPromises()
    expect(w.text()).toContain('正在读取')
    expect(w.text()).toContain('密钥台账')
    release([])
    await flushPromises()
    w.unmount()
  })
})

describe('KeyringPanel 空态 / 错误 / stale', () => {
  // —— 锁④（验收清单里的"空态含三步与主按钮"）——
  it('空态给三步出路与页内唯一主按钮，不出现"暂无"', async () => {
    const w = await mountWith([])
    expect(w.find('.empty-state').exists()).toBe(true)
    const t = w.text()
    expect(t).toContain('本机还没有 SSH 密钥。三步开始')
    expect(t).toContain('生成一把')
    expect(t).toContain('把公钥部署到目标服务器')
    expect(t).toContain('登记进主机清单')
    expect(t).not.toContain('暂无')
    // 页内唯一主按钮：空态下 .btn-primary 恰一颗，就是"生成第一把钥匙"
    const primaries = w.findAll('.btn-primary')
    expect(primaries).toHaveLength(1)
    expect(primaries[0].text()).toBe('生成第一把钥匙')
    expect(w.find('#kops-empty-generate').exists()).toBe(true)
    // 空态的顶栏不出现第二颗生成入口（只有刷新在场）
    expect(w.find('#kops-open-generate').exists()).toBe(false)
  })

  it('只有一把钥匙时也不出现"暂无"', async () => {
    const w = await mountWith([PLAIN])
    expect(w.text()).not.toContain('暂无')
    expect(w.findAll('.keyrow')).toHaveLength(1)
  })

  it('~/.ssh 不存在是正常空态，不报故障', async () => {
    const w = await mountWith([])
    expect(w.text()).toContain('正常空态')
    expect(w.find('.error-box').exists()).toBe(false)
  })

  it('首次读取失败：如实报错，不假 empty-state、不画空表', async () => {
    svc.ListKeys.mockRejectedValue(new Error('读取 /home/u/.ssh 失败: permission denied'))
    const w = mount(KeyringPanel)
    await flushPromises()
    expect(w.find('.error-box').exists()).toBe(true)
    expect(w.find('.empty-state').exists()).toBe(false)
    expect(w.find('.keyrow').exists()).toBe(false)
  })

  it('读失败保留上次结果并明说 stale（不只靠降透明度表达）', async () => {
    const w = await mountWith([PLAIN, GUARD])
    svc.ListKeys.mockRejectedValue(new Error('读取 /home/u/.ssh 失败: 设备忙'))
    await w.find('#kops-refresh').trigger('click')
    await flushPromises()
    expect(w.find('.error-box').text()).toContain('设备忙')
    expect(w.findAll('.keyrow')).toHaveLength(2)
    expect(w.text()).toContain('可能已过期')
    expect(w.text()).toContain('下表仍是')
  })
})
