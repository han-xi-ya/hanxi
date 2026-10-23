// 密钥台账六个写动作对话框的特征测试。钉的不是样式，是会出事故的边界：
// ① 口令字段只进请求——任何展示面/复制面/回执里都不许出现它（生成、改口令、
//    部署三个窗各钉一次）；补出公钥窗反着钉一次：它**本就不该有**口令字段
//    （公钥段在私钥文件明文外层，带口令也补得动）。
// ② verdict 按枚举值分派，auth-rejected 的"两个可能"必须同屏，且**不依赖**
//    后端 reason 文案（测试故意给一条不含第二种可能的 reason，界面仍要说全）；
// ③ 确认回显（confirm）相等判定在后端——不符时后端错误一个字符不改地上屏，
//    删除被清单护栏拒绝时"是哪几条"必须原样可见；
// ④ 危险门默认关：overwrite 默认 false、留空注释不默默放行、去口令必须回显；
// ⑤ 删除的失去访问实话 + usedBy 别名直接打在对话框里；
// ⑥ 半把钥匙的两类行（只有私钥 / 只剩公钥）各走各的新出口，且删除对话框在
//    两种 mode 下**只有一份实现**（两份对话框文案会各自演化，本仓反复清的病）。
//
// 夹具形状 = bindings models.js 的 **KeyEntry**（一把钥匙一条）：字段漂移即红。
// mock 三条既有纪律（同目录头注，都是实打实的事故来源）：
// - vi.mock 工厂被提升，桩件必须走 vi.hoisted；
// - 绑定命名空间键名与 bindings 逐字一致（SshKeysService）——旧名 mock 永远
//   命中不到实现还照样绿，是在案的假绿事故形态；
// - 台账行内动作在展开区（次要区），测试先进展开再点动作——"动作常驻不靠 hover"
//   由面板 DOM 在位性证明（未 hover 也能查到并点击）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import KeyringPanel from '../KeyringPanel.vue'
import DeleteKeyDialog from '../DeleteKeyDialog.vue'
import ExportPubDialog from '../ExportPubDialog.vue'
import {
  DEPLOY_LABEL,
  DEPLOY_TONE,
  canDeletePub,
  canExportPub,
  deployLabelOf,
  deployToneOf,
  nameTakenInLedger,
  rsaBitsOf,
} from '../useKeyOps'

const svc = vi.hoisted(() => ({
  ListKeys: vi.fn(),
  GenerateKey: vi.fn(),
  SetKeyPassphrase: vi.fn(),
  DeleteKey: vi.fn(),
  DeletePublicKey: vi.fn(),
  ExportPublicKey: vi.fn(),
  DeployPublicKey: vi.fn(),
}))

vi.mock('../../../../bindings/hanxi/internal/modules/sshkeys', () => ({
  SshKeysService: svc,
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: () => vi.fn() } }))
vi.mock('../../../composables/useClipboard', () => ({
  useClipboard: () => ({ copy: vi.fn(async () => true), paste: vi.fn(), copyWithToast: vi.fn(async () => true) }),
}))

// useConfirm 的对话框实体由 App.vue 顶层单例挂载，组件级测试没有 App 宿主：
// 整模块打桩。默认放行（confirm→true）；要测"确认被否决"的用例用
// mockResolvedValueOnce(false) 翻一次。confirmState 形状保留给 KeyOpsModal 的
// escapeYield（测试里恒无确认框在场）。
const cf = vi.hoisted(() => ({ confirm: vi.fn(async () => true) }))
vi.mock('../../../composables/useConfirm', () => ({
  useConfirm: () => ({ confirm: cf.confirm, confirmState: { open: false }, settleConfirm: () => {} }),
}))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

// —— 夹具：字段名逐一照 bindings/hanxi/internal/modules/sshkeys/models.js 的 KeyEntry ——
const PRIV = {
  name: 'id_plain', hasPrivate: true, hasPublic: true,
  privatePath: '/home/u/.ssh/id_plain', publicPath: '/home/u/.ssh/id_plain.pub',
  type: 'ssh-ed25519', fingerprint: 'SHA256:AAAAbbbbCCCCddddEEEEffffGGGGhhhh',
  comment: 'laptop', bits: 256, guarded: false, guardKnown: true,
  mtimeUnix: 1730000000, usedBy: [], usedByKnown: true,
}
// 有口令 + 被两条清单主机引用（删除对话框要能看到别名清单的那把）。
const GUARDED = {
  ...PRIV, name: 'id_guard',
  privatePath: '/home/u/.ssh/id_guard', publicPath: '/home/u/.ssh/id_guard.pub',
  guarded: true, fingerprint: 'SHA256:GUAR GUAR GUAR GUAR GUAR GUAR GUAR',
  comment: 'nas', usedBy: ['nas01', 'work'], usedByKnown: true,
}
// 孤儿 .pub：只有公钥侧。
const PUB = {
  ...PRIV, name: 'id_orphan', hasPrivate: false, hasPublic: true,
  privatePath: undefined, publicPath: '/home/u/.ssh/id_orphan.pub',
  guarded: false, fingerprint: 'SHA256:PUBPUBPUBPUBPUBPUBPUBPUBPUBPUBPUBPUB',
  // 仅公钥的行：后端没有私钥路径可比对 → usedByKnown=false（**未判**，不是"没人用"）。
  comment: 'old-server', usedBy: [], usedByKnown: false,
}
// 只有私钥、缺配套 .pub 且**带口令**：从服务器直接下回来的私钥的典型形态。
// 「补出公钥」的入口条件必须对它成立（前端不看 guarded——公钥段在明文外层）。
const NOPUBG = {
  ...PRIV, name: 'id_srvkey', hasPrivate: true, hasPublic: false,
  privatePath: '/home/u/.ssh/id_srvkey', publicPath: undefined,
  guarded: true, fingerprint: '', comment: '',
}
// 同形状但无口令、台账读得到原注释：注释应沿用进输入框。
const NOPUBFREE = {
  ...NOPUBG, name: 'id_downloaded',
  privatePath: '/home/u/.ssh/id_downloaded',
  guarded: false, comment: 'downloaded',
}
const LEDGER = [PRIV, GUARDED, PUB]

const GEN_OK = {
  privatePath: '/home/u/.ssh/id_new', publicPath: '/home/u/.ssh/id_new.pub',
  fingerprint: 'SHA256:NEWNEWNEW', type: 'ssh-ed25519', guarded: false,
  code: 'ok', message: '已生成 ssh-ed25519 密钥 id_new，未设置口令（私钥在磁盘上是明文可读的）',
}

const PASSPHRASE_RESULT = {
  path: '/home/u/.ssh/id_guard', fingerprint: 'SHA256:GUAR', guarded: true, code: 'ok',
  message: '已更换口令；指纹 SHA256:GUAR 未变，已部署到服务端的公钥授权不受影响，'
    + '配套 .pub 无需重新分发；未取回原注释（带口令的私钥没有配套 .pub 时读不到注释），需要时用 ssh-keygen -c 补',
}

const DEPLOY_BASE = {
  verdict: 'appended', ok: true, reason: '已追加一行', hostKeyFingerprint: 'SHA256:HOSTHOST',
  authorizedKeysPath: '/home/deploy/.ssh/authorized_keys', pubKeyFingerprint: 'SHA256:PUBPUB',
  code: 'ok',
}

function deployWith(patch: Partial<typeof DEPLOY_BASE>) {
  return { ...DEPLOY_BASE, ...patch }
}

// ExportPublicKey 的成功回执：KeyOpResult.path 回的是**私钥**解析后真身
// （新 .pub 就在它旁边），fingerprint 是算出来的公钥指纹。
const EXPORT_OK = {
  path: '/home/u/.ssh/id_srvkey', fingerprint: 'SHA256:EXPORTED', guarded: true, code: 'ok',
  message: '已从私钥算出公钥并写入 id_srvkey.pub（指纹 SHA256:EXPORTED）；私钥未作任何修改',
}

async function mountPanel() {
  return mountRows(LEDGER)
}

async function mountRows(rows: unknown[]) {
  svc.ListKeys.mockResolvedValue(rows)
  const w = mount(KeyringPanel)
  await flushPromises()
  return w
}

async function openGenerate(w: ReturnType<typeof mount>) {
  await w.find('#kops-open-generate').trigger('click')
  await flushMicrotasks()
}

/**
 * 台账行内动作：rows[0]=id_plain（无口令），rows[1]=id_guard（有口令、被两条清单引用），
 * rows[2]=id_orphan（仅公钥）。改口令/成对删除/删除公钥在展开区（次要区），先进展开
 * 再点；部署与补出公钥是动作段常驻动作，直接点。
 */
async function openFromRow(w: ReturnType<typeof mount>, idx: number, act: 'pass' | 'del' | 'delpub' | 'deploy' | 'export') {
  if (act !== 'deploy' && act !== 'export') {
    await w.findAll('.keyrow')[idx].find('.keyrow-toggle').trigger('click')
    await flushMicrotasks()
  }
  await w.findAll('.keyrow')[idx].find(`.kops-act-${act}`).trigger('click')
  await flushMicrotasks()
}

afterEach(() => {
  vi.clearAllMocks()
  cf.confirm.mockReset()
  cf.confirm.mockImplementation(async () => true)
})

describe('① 生成密钥对话框', () => {
  it('发请求带 overwrite:false——危险门默认关，替用户置真是禁区', async () => {
    svc.GenerateKey.mockResolvedValue(GEN_OK)
    const w = await mountPanel()
    await openGenerate(w)

    // 覆盖勾选在位且默认未勾（界面对应危险确认，不是偷偷置真）
    const ovw = w.find('#kops-gen-overwrite')
    expect(ovw.exists()).toBe(true)
    expect((ovw.element as HTMLInputElement).checked).toBe(false)

    await w.find('#kops-gen-name').setValue('id_new')
    await w.find('#kops-gen-comment').setValue('laptop@2026')
    await w.find('#kops-gen-submit').trigger('click')
    await flushMicrotasks()

    expect(svc.GenerateKey).toHaveBeenCalledWith({
      name: 'id_new', type: 'ed25519', rsaBits: 0,
      comment: 'laptop@2026', passphrase: '', overwrite: false,
    })
    w.unmount()
  })

  it('注释留空：警告后果并卡一道显式勾选，绝不默默放行', async () => {
    const w = await mountPanel()
    await openGenerate(w)
    await w.find('#kops-gen-name').setValue('id_new')

    // 留空即警告，且点名"日后在 authorized_keys 里认钥匙"这个唯一用途
    expect(w.text()).toContain('authorized_keys')
    const submit = w.find('#kops-gen-submit')
    expect((submit.element as HTMLButtonElement).disabled).toBe(true)

    // 显式承认后果后放行——门闩是勾选动作，不是界面替用户决定
    await w.find('#kops-gen-ack').setValue(true)
    expect((w.find('#kops-gen-submit').element as HTMLButtonElement).disabled).toBe(false)
    w.unmount()
  })

  it('口令只进请求：输入即掩码，发出请求时即清空、回执不含口令，成功后刷新台账', async () => {
    // 用"在途挂起"证明销毁排在调用之后、结果之前——成功回调里的补救不算销毁纪律。
    let release: (v: unknown) => void = () => {}
    svc.GenerateKey.mockImplementation(() => new Promise(resolve => { release = resolve }))
    const w = await mountPanel()
    await openGenerate(w)
    await w.find('#kops-gen-name').setValue('id_new')
    await w.find('#kops-gen-comment').setValue('c@c')

    const pass = w.find('#kops-gen-pass')
    expect(pass.attributes('type')).toBe('password')
    await w.find('#kops-gen-show').setValue(true)
    expect(w.find('#kops-gen-pass').attributes('type')).toBe('text')
    await w.find('#kops-gen-show').setValue(false)

    const SECRET = 'Sup3r-Passphrase'
    await pass.setValue(SECRET)
    await w.find('#kops-gen-submit').trigger('click')
    await flushMicrotasks()

    expect(svc.GenerateKey.mock.calls[0][0].passphrase).toBe(SECRET)
    // 请求在途，字段已空，页面任何角落不再含口令
    expect((w.find('#kops-gen-pass').element as HTMLInputElement).value).toBe('')
    expect(w.text()).not.toContain(SECRET)
    expect(w.html()).not.toContain(SECRET)

    release(GEN_OK)
    await flushMicrotasks()
    // 回执亮出指纹，且台账随即刷新（ListKeys 初挂 1 次 + changed 1 次）
    expect(w.text()).toContain('SHA256:NEWNEWNEW')
    expect(w.html()).not.toContain(SECRET)
    expect(svc.ListKeys).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('台账已有同名：亮危险警告；勾覆盖前恒发 overwrite=false', async () => {
    svc.GenerateKey.mockRejectedValue(new Error('目标文件已存在，未覆盖：拒绝写入'))
    const w = await mountPanel()
    await openGenerate(w)
    await w.find('#kops-gen-name').setValue('id_plain')
    await w.find('#kops-gen-comment').setValue('c@c')

    expect(w.text()).toContain('台账里已有')
    await w.find('#kops-gen-submit').trigger('click')
    await flushMicrotasks()
    expect(svc.GenerateKey.mock.calls[0][0].overwrite).toBe(false)
    // 后端拒绝原文上屏，不粉饰成"生成失败"
    expect(w.find('.keyops-generate .error-box').text()).toContain('目标文件已存在')

    await w.find('#kops-gen-overwrite').setValue(true)
    svc.GenerateKey.mockResolvedValue(GEN_OK)
    await w.find('#kops-gen-submit').trigger('click')
    await flushMicrotasks()
    expect(svc.GenerateKey.mock.calls[1][0].overwrite).toBe(true)
    w.unmount()
  })

  it('选 rsa 才传位数，默认 3072；ed25519 时传 0（后端忽略，别用假值冒充）', async () => {
    svc.GenerateKey.mockResolvedValue(GEN_OK)
    const w = await mountPanel()
    await openGenerate(w)
    await w.find('#kops-gen-name').setValue('id_rsa_new')
    await w.find('#kops-gen-comment').setValue('c@c')
    await w.find('#kops-gen-type').setValue('rsa')
    expect(w.find('#kops-gen-bits').exists()).toBe(true)
    await w.find('#kops-gen-submit').trigger('click')
    await flushMicrotasks()
    expect(svc.GenerateKey.mock.calls[0][0].rsaBits).toBe(3072)
    w.unmount()
  })

  it('预告"这一步会创建 ~/.ssh（若不存在）"，这是模块唯一新建该目录的动作', async () => {
    const w = await mountPanel()
    await openGenerate(w)
    expect(w.text()).toContain('创建')
    expect(w.text()).toContain('~/.ssh')
    w.unmount()
  })
})

describe('② 改口令对话框', () => {
  it('两个口令字段都掩码，「显示」是显式勾选而不是默认明文', async () => {
    const w = await mountPanel()
    await openFromRow(w, 1, 'pass')

    expect(w.find('#kops-pass-old').attributes('type')).toBe('password')
    expect(w.find('#kops-pass-new').attributes('type')).toBe('password')
    await w.find('#kops-pass-showold').setValue(true)
    expect(w.find('#kops-pass-old').attributes('type')).toBe('text')
    w.unmount()
  })

  it('两条实测事实必须在场：就地升级新格式（不可逆）、注释可能取不回；去口令要回显文件名', async () => {
    const w = await mountPanel()
    await openFromRow(w, 1, 'pass') // GUARDED：新口令留空即"去口令"降级态

    // ① 格式升级的事实（文件头会变，不说用户会以为钥匙被换）
    expect(w.text()).toContain('就地升级')
    expect(w.text()).toContain('BEGIN OPENSSH PRIVATE KEY')
    // ② 注释取不回的前置说明（成功回执里若后端再提，见下一条的"不得吞掉"）
    expect(w.text()).toContain('取不回')
    // ③ 降级门：解释"文件被读到就能用"，并要求原样回显文件名
    expect(w.text()).toContain('文件被读到就能用')
    expect(w.find('#kops-pass-confirm').exists()).toBe(true)
    // 回显为空不许提交（相等判定在后端，前端只把门呈现出来）
    expect((w.find('#kops-pass-submit').element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()
  })

  it('回显不符：后端错误一个字符不改地上屏；口令当场销毁不进任何面', async () => {
    svc.SetKeyPassphrase.mockRejectedValue(new Error(
      'confirm-mismatch: 去掉口令保护需要把文件名原样回一遍（期望 "id_guard"）'))
    const w = await mountPanel()
    await openFromRow(w, 1, 'pass')

    await w.find('#kops-pass-old').setValue('old-pass-1')
    await w.find('#kops-pass-confirm').setValue('id_wr0ng')
    await w.find('#kops-pass-submit').trigger('click')
    await flushMicrotasks()

    // 请求照后端契约发出（confirm 传用户输入，判定权在后端）
    expect(svc.SetKeyPassphrase).toHaveBeenCalledWith({
      path: GUARDED.privatePath, oldPassphrase: 'old-pass-1', newPassphrase: '', confirm: 'id_wr0ng',
    })
    const box = w.find('.keyops-pass .error-box')
    expect(box.exists()).toBe(true)
    expect(box.text()).toContain('去掉口令保护需要把文件名原样回一遍')
    expect(box.text()).toContain('期望 "id_guard"')
    // 旧口令不进展示面，且提交后即销毁
    expect(w.text()).not.toContain('old-pass-1')
    expect((w.find('#kops-pass-old').element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('成功回执不得吞掉"未取回原注释"与"指纹未变"；台账随即刷新', async () => {
    let release: (v: unknown) => void = () => {}
    svc.SetKeyPassphrase.mockImplementation(() => new Promise(resolve => { release = resolve }))
    const w = await mountPanel()
    await openFromRow(w, 1, 'pass')

    await w.find('#kops-pass-old').setValue('old-pass-1')
    await w.find('#kops-pass-new').setValue('new-pass-2')
    await w.find('#kops-pass-submit').trigger('click')
    await flushMicrotasks()

    expect(svc.SetKeyPassphrase).toHaveBeenCalledWith({
      path: GUARDED.privatePath, oldPassphrase: 'old-pass-1', newPassphrase: 'new-pass-2', confirm: '',
    })
    // 在途即已销毁：两个口令字段都清空，页面不含口令
    expect((w.find('#kops-pass-old').element as HTMLInputElement).value).toBe('')
    expect((w.find('#kops-pass-new').element as HTMLInputElement).value).toBe('')
    expect(w.html()).not.toContain('new-pass-2')
    expect(w.html()).not.toContain('old-pass-1')

    release(PASSPHRASE_RESULT)
    await flushMicrotasks()
    // 后端 Message 里的两句实话逐字在屏（"未变"是"已部署授权不失效"的依据）
    expect(w.text()).toContain('未取回原注释')
    expect(w.text()).toContain('指纹 SHA256:GUAR 未变')
    expect(w.text()).not.toContain('new-pass-2')
    expect(svc.ListKeys).toHaveBeenCalledTimes(2)
    w.unmount()
  })
})

describe('③ 删除对话框', () => {
  it('成对删与失去访问的实话讲在明面；回显为空不许提交', async () => {
    const w = await mountPanel()
    await openFromRow(w, 0, 'del')
    expect(w.text()).toContain('成对删除')
    expect(w.text()).toContain('id_plain.pub')
    // 本轮新增的必含实话（后端实测边界，藏了等于骗人）
    expect(w.text()).toContain('只接受密钥登录的服务器')
    expect(w.text()).toContain('删掉后你会失去访问')
    expect(w.text()).toContain('本地无法知道它被装到了哪些机器')
    expect(w.find('#kops-del-confirm').exists()).toBe(true)
    expect((w.find('#kops-del-submit').element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()
  })

  // —— 锁⑤：usedBy 非空时，别名清单直接打在删除对话框里 ——
  it('usedBy 非空：对话框里直接列出在用别名，点名是清单在拦而不是界面在拦', async () => {
    const w = await mountPanel()
    await openFromRow(w, 1, 'del') // GUARDED：usedBy ['nas01','work']
    expect(w.text()).toContain('nas01')
    expect(w.text()).toContain('work')
    expect(w.text()).toContain('2 条正在用它')
    // 实话同屏：删掉后失去访问 + 本地无从知道装在哪些机器
    expect(w.text()).toContain('本地无法知道它被装到了哪些机器')
    w.unmount()
  })

  it('清单护栏拒绝：是哪几条别名原样可见，台账不误刷', async () => {
    svc.DeleteKey.mockRejectedValue(new Error(
      '清单里还有 3 条在用这把钥匙：nas01、work、home——请先在清单里改掉或删掉它们，再删钥匙'))
    const w = await mountPanel()
    await openFromRow(w, 0, 'del')
    await w.find('#kops-del-confirm').setValue('id_plain')
    await w.find('#kops-del-submit').trigger('click')
    await flushMicrotasks()

    const box = w.find('.keyops-delete .error-box')
    expect(box.text()).toContain('清单里还有 3 条在用这把钥匙：nas01、work、home')
    // 护栏拒绝不是"删除失败"四个字的粉饰对象，也不是"路径不存在"的误导场景
    expect(w.text()).not.toContain('你输入的路径不存在')
    // 失败不触发台账刷新（成功才 changed）
    expect(svc.ListKeys).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('成功回执显示解析后真身路径并标注"不同形属正常"，不做字符串相等叙事', async () => {
    svc.DeleteKey.mockResolvedValue({
      path: 'C:\\Users\\ADMINI~1\\.ssh\\id_plain', fingerprint: '', guarded: false, code: 'ok',
      message: '已删除私钥 id_plain（指纹 SHA256:AAAA），并一并删除配套公钥 id_plain.pub',
    })
    const w = await mountPanel()
    await openFromRow(w, 0, 'del')
    await w.find('#kops-del-confirm').setValue('id_plain')
    await w.find('#kops-del-submit').trigger('click')
    await flushMicrotasks()

    expect(svc.DeleteKey).toHaveBeenCalledWith({ path: PRIV.privatePath, confirm: 'id_plain' })
    expect(w.text()).toContain('C:\\Users\\ADMINI~1\\.ssh\\id_plain')
    expect(w.text()).toContain('解析后的路径')
    expect(w.text()).toContain('不同形')
    expect(svc.ListKeys).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  // —— 锁⑥：孤儿 .pub 的删除走同一份对话框实现的 orphan-pub 话术 ——
  it('孤儿行入口开的是**同一个** DeleteKeyDialog：标题与话术换掉，成对删主句不在场', async () => {
    const w = await mountPanel()
    await openFromRow(w, 2, 'delpub')
    // 只有一份删除对话框实现（复制第二份窗体，两套文案会各自演化——本仓反复清的病）
    expect(w.findAllComponents(DeleteKeyDialog)).toHaveLength(1)
    expect(w.find('.cmodal-title').text()).toBe('删除这个公钥')
    const dlg = w.find('.keyops-delete')
    expect(dlg.text()).toContain('私钥本就不存在')
    expect(dlg.text()).toContain('远端机器上若装过它，那一行授权不会被回收')
    // pair 模式的主句必须缺席——两种话术真的分了家，不是同一套文案贴个新标题
    expect(dlg.text()).not.toContain('只接受密钥登录的服务器')
    expect(dlg.text()).not.toContain('删掉后你会失去访问')
    w.unmount()
  })

  it('孤儿删除确认门：回显非空才可提交；相等判定归后端——任意非空串照发，拒绝原文上屏、台账不误刷', async () => {
    const MSG = 'confirm-mismatch: 要把文件名原样输入一遍（id_orphan.pub）'
    svc.DeletePublicKey.mockRejectedValue(new Error(MSG))
    const w = await mountPanel()
    await openFromRow(w, 2, 'delpub')
    // 回显为空不许提交（前端只把门呈现出来，不比相等——比字面会因 8.3 短名分叉）
    expect((w.find('#kops-del-submit').element as HTMLButtonElement).disabled).toBe(true)
    await w.find('#kops-del-confirm').setValue('故意填错的也无所谓')
    expect((w.find('#kops-del-submit').element as HTMLButtonElement).disabled).toBe(false)
    await w.find('#kops-del-submit').trigger('click')
    await flushMicrotasks()
    // 走的是 DeletePublicKey（孤儿专用 RPC），confirm 原样传、path 是公钥路径
    expect(svc.DeletePublicKey).toHaveBeenCalledWith({ path: PUB.publicPath, confirm: '故意填错的也无所谓' })
    expect(svc.DeleteKey).not.toHaveBeenCalled()
    expect(w.find('.keyops-delete .error-box').text()).toBe(MSG)
    // 被拒不触发台账刷新（成功才 changed）——删失败却刷台账会把"还在"说成"没了"
    expect(svc.ListKeys).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('孤儿删除成功：后端 message 原样上屏（"远端授权不会被回收"不许被吞）、台账刷新', async () => {
    const MSG = '已删除孤儿公钥 id_orphan.pub（配套私钥本就不存在）；远端机器上若装过它，那行授权不会被回收'
    svc.DeletePublicKey.mockResolvedValue({
      path: '/home/u/.ssh/id_orphan.pub', fingerprint: PUB.fingerprint, guarded: false, code: 'ok',
      message: MSG,
    })
    const w = await mountPanel()
    await openFromRow(w, 2, 'delpub')
    await w.find('#kops-del-confirm').setValue('id_orphan.pub')
    await w.find('#kops-del-submit').trigger('click')
    await flushMicrotasks()
    expect(w.find('.keyops-delete').text()).toContain(MSG)
    expect(w.text()).toContain('实际删除位置')
    expect(svc.ListKeys).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('两种 mode 共用实现（回归锁）：pair 开窗与孤儿开窗之间只换一个组件、不换窗体', async () => {
    const w = await mountPanel()
    await openFromRow(w, 1, 'del') // GUARDED：成对删
    expect(w.find('.cmodal-title').text()).toBe('删除这把钥匙')
    expect(w.find('.keyops-delete').text()).toContain('成对删除')
    expect(w.find('.keyops-delete').text()).toContain('与配套公钥')
    await w.find('.cmodal-close').trigger('click')
    await flushMicrotasks()
    await openFromRow(w, 2, 'delpub')
    expect(w.findAllComponents(DeleteKeyDialog)).toHaveLength(1)
    expect(w.find('.cmodal-title').text()).toBe('删除这个公钥')
    expect(w.find('.keyops-delete').text()).toContain('私钥本就不存在')
    w.unmount()
  })
})

describe('④ 部署公钥对话框', () => {
  it('从某一行进入即预选该把的 .pub；下拉只列有配套公钥的把，私钥路径绝不进选项', async () => {
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    const sel = w.find('#kops-dep-pub')
    expect((sel.element as HTMLSelectElement).value).toBe(PUB.publicPath)
    // 台账三把都有 .pub —— 选项 = 占位 + 三条 publicPath；私钥路径不在其中
    const values = (sel.findAll('option')).map(o => (o.element as HTMLOptionElement).value)
    expect(values).toEqual(['', PRIV.publicPath, GUARDED.publicPath, PUB.publicPath])
    expect(values).not.toContain(PRIV.privatePath)
    expect(values).not.toContain(GUARDED.privatePath)
    w.unmount()
  })

  it('缺配套 .pub 的钥匙不进下拉（后端只读 .pub 文件，列出来就是必挂选项）', async () => {
    svc.ListKeys.mockResolvedValue([...LEDGER, { ...PRIV, name: 'id_nopub', hasPublic: false, publicPath: undefined }])
    const w = mount(KeyringPanel)
    await flushPromises()
    await w.findAll('.kops-act-deploy')[0].trigger('click') // id_plain 行
    await flushMicrotasks()
    const values = (w.find('#kops-dep-pub').findAll('option')).map(o => (o.element as HTMLOptionElement).value)
    // 占位 + 三条有 .pub 的把；id_nopub 没有 .pub，绝不以私钥路径混进选项
    expect(values.filter(v => v !== '')).toHaveLength(3)
    expect(values).not.toContain('/home/u/.ssh/id_nopub')
    expect(values.filter(v => v !== '').every(v => v.endsWith('.pub'))).toBe(true)
    w.unmount()
  })

  it('后果横幅常驻：真实登录尝试、sshd 日志、fail2ban，一句都不能少', async () => {
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    expect(w.text()).toContain('真实登录尝试')
    expect(w.text()).toContain('sshd 日志')
    expect(w.text()).toContain('fail2ban')
    w.unmount()
  })

  it('提交发出前过一次确认步；确认被否决则一个请求都不发', async () => {
    cf.confirm.mockResolvedValueOnce(false)
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-user').setValue('deploy')
    await w.find('#kops-dep-pass').setValue('pw-once')
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()
    expect(cf.confirm).toHaveBeenCalledTimes(1)
    expect(svc.DeployPublicKey).not.toHaveBeenCalled()
    w.unmount()
  })

  it('口令在请求成形的那一刻销毁：在途与成功后所有面都不含它', async () => {
    const SECRET = 'p@ssw0rd-ONE'
    let release: (v: unknown) => void = () => {}
    svc.DeployPublicKey.mockImplementation(() => new Promise(resolve => { release = resolve }))
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')

    const pass = w.find('#kops-dep-pass')
    expect(pass.attributes('type')).toBe('password')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-port').setValue(2222)
    await w.find('#kops-dep-user').setValue('deploy')
    await pass.setValue(SECRET)
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()

    expect(svc.DeployPublicKey).toHaveBeenCalledWith({
      host: '10.0.0.9', port: 2222, user: 'deploy',
      password: SECRET, pubKeyPath: PUB.publicPath, timeoutMs: 10000,
    })
    // 请求已在途，字段就已清空——销毁排在调用之前，不是成功回调里的补救
    expect((w.find('#kops-dep-pass').element as HTMLInputElement).value).toBe('')
    expect(w.text()).not.toContain(SECRET)
    expect(w.html()).not.toContain(SECRET)

    release(deployWith({}))
    await flushMicrotasks()
    expect(w.text()).not.toContain(SECRET)
    expect(w.html()).not.toContain(SECRET)
    w.unmount()
  })

  it('关窗（✕）即销毁口令：重新开窗时字段是空的', async () => {
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-pass').setValue('still-secret')
    // 只有当前开窗会渲染 .cmodal-close（KeyOpsModal 的 v-if），全局唯一
    await w.find('.cmodal-close').trigger('click')
    await flushMicrotasks()
    // 重新打开（换一行再点）：口令不随窗体复活
    await openFromRow(w, 2, 'deploy')
    expect((w.find('#kops-dep-pass').element as HTMLInputElement).value).toBe('')
    expect(w.text()).not.toContain('still-secret')
    w.unmount()
  })

  it('appended：绿档 + authorizedKeysPath 标作"远端告知的写入位置"，界面不为它背书"已验证"', async () => {
    svc.DeployPublicKey.mockResolvedValue(deployWith({}))
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-user').setValue('deploy')
    await w.find('#kops-dep-pass').setValue('x')
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()

    expect(w.find('.keyops-deploy .chip-positive').text()).toBe('已写入')
    expect(w.text()).toContain('远端告知的写入位置')
    expect(w.text()).toContain('/home/deploy/.ssh/authorized_keys')
    expect(w.text()).toContain('SHA256:PUBPUB') // 装上去的那把的指纹
    expect(w.text()).not.toContain('已验证')
    // 部署不动台账：不触发 changed
    expect(svc.ListKeys).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('auth-rejected：黄档，"两个可能"由界面按 verdict 补全——reason 只给一半也要说全', async () => {
    // 故意只给半句：证明界面没有"从 reason 文案里找第二种可能"的依赖
    svc.DeployPublicKey.mockResolvedValue(deployWith({
      verdict: 'auth-rejected', ok: false, reason: 'ssh: unable to authenticate', code: 'auth-rejected',
      authorizedKeysPath: '', pubKeyFingerprint: '',
    }))
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-user').setValue('deploy')
    await w.find('#kops-dep-pass').setValue('x')
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()

    expect(w.find('.keyops-deploy .chip-warning').text()).toBe('认证被拒')
    expect(w.text()).toContain('密码不对')
    expect(w.text()).toContain('不接受密码登录')
    expect(w.text()).toContain('ssh: unable to authenticate') // 后端原文也保留
    w.unmount()
  })

  it('hostkey-changed：独立红档分支——什么都没发出、给清钉出口、亮出本次指纹', async () => {
    svc.DeployPublicKey.mockResolvedValue(deployWith({
      verdict: 'hostkey-changed', ok: false, reason: '与钉定指纹不符',
      hostKeyFingerprint: 'SHA256:NEWHOSTKEY', authorizedKeysPath: '', pubKeyFingerprint: '',
      code: 'hostkey-changed',
    }))
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-user').setValue('deploy')
    await w.find('#kops-dep-pass').setValue('x')
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()

    const chip = w.find('.keyops-deploy .chip-danger')
    expect(chip.text()).toBe('主机密钥变了')
    // 关键实话：认证之前中止，密码根本没出网——这决定了"对端日志里没有记录"
    expect(w.text()).toContain('密码都没有发出去')
    expect(w.text()).toContain('SHA256:NEWHOSTKEY')
    // 出口给到具体动作与具体地址，不是"请检查配置"这类空话
    expect(w.text()).toContain('清除该地址的钉定')
    expect(w.text()).toContain('10.0.0.9')
    w.unmount()
  })

  it('refused：红档"本地拒发"，明说一个字节都没对外发送', async () => {
    svc.DeployPublicKey.mockResolvedValue(deployWith({
      verdict: 'refused', ok: false, reason: '公钥文件形态非法', code: 'refused',
    }))
    const w = await mountPanel()
    await openFromRow(w, 2, 'deploy')
    await w.find('#kops-dep-host').setValue('10.0.0.9')
    await w.find('#kops-dep-user').setValue('deploy')
    await w.find('#kops-dep-pass').setValue('x')
    await w.find('#kops-dep-submit').trigger('click')
    await flushMicrotasks()
    expect(w.find('.keyops-deploy .chip-danger').text()).toBe('本地拒发')
    expect(w.text()).toContain('一个字节都没有对外发送')
    w.unmount()
  })
})

describe('⑤ 补出公钥对话框', () => {
  it('带口令的私钥照样开窗补公钥：入口不看 guarded，窗内无口令字段，请求里只有 path 与 comment', async () => {
    svc.ExportPublicKey.mockResolvedValue(EXPORT_OK)
    const w = await mountRows([NOPUBG])
    const btn = w.find('.kops-act-export')
    expect(btn.exists()).toBe(true) // guarded:true 也在场——前端不看 guarded
    expect((btn.element as HTMLButtonElement).disabled).toBe(false)
    await btn.trigger('click')
    await flushMicrotasks()
    // 这一步本就不接触口令：窗里不该有任何 password 字段（也永不会加出来）
    expect(w.findAll('.keyops-export input[type="password"]')).toHaveLength(0)
    await w.find('#kops-exp-submit').trigger('click')
    await flushMicrotasks()
    const req = svc.ExportPublicKey.mock.calls[0][0]
    expect(req).toEqual({ path: NOPUBG.privatePath, comment: '' })
    expect(Object.keys(req).sort()).toEqual(['comment', 'path']) // 绝无 passphrase
    w.unmount()
  })

  it('成功：亮出算出的指纹、后端 message 原样上屏、台账刷新', async () => {
    svc.ExportPublicKey.mockResolvedValue(EXPORT_OK)
    const w = await mountRows([NOPUBG])
    await w.find('.kops-act-export').trigger('click')
    await flushMicrotasks()
    await w.find('#kops-exp-submit').trigger('click')
    await flushMicrotasks()
    expect(w.text()).toContain('SHA256:EXPORTED') // 回执里把指纹摆出来
    expect(w.text()).toContain('私钥未作任何修改') // 后端 message 不被界面覆盖
    expect(svc.ListKeys).toHaveBeenCalledTimes(2) // 成功才 changed → 台账刷新
    w.unmount()
  })

  it('后端拒绝：原文一个字符不改上屏，台账不误刷，不演成功', async () => {
    const MSG = 'already-exists: 配套公钥 id_srvkey.pub 已存在，无需补出'
    svc.ExportPublicKey.mockRejectedValue(new Error(MSG))
    const w = await mountRows([NOPUBG])
    await w.find('.kops-act-export').trigger('click')
    await flushMicrotasks()
    await w.find('#kops-exp-submit').trigger('click')
    await flushMicrotasks()
    expect(w.find('.keyops-export .error-box').text()).toBe(MSG)
    expect(svc.ListKeys).toHaveBeenCalledTimes(1) // 失败绝不触发刷新
    expect(w.find('.keyops-export .banner-ok').exists()).toBe(false) // 不演成功
    w.unmount()
  })

  it('注释：台账读得到的沿用进输入框；留空时后果实话在屏，带口令的"本就取不到"常驻', async () => {
    // 带口令的独身行：读不到原注释 → 空串 + 两句实话都在
    const w = await mountRows([NOPUBG])
    await w.find('.kops-act-export').trigger('click')
    await flushMicrotasks()
    expect((w.find('#kops-exp-comment').element as HTMLInputElement).value).toBe('')
    expect(w.text()).toContain('本就取不到')
    expect(w.text()).toContain('只能靠指纹认这把')
    expect(w.text()).toContain('authorized_keys')
    // 填了注释 → 留空后果句撤回；带口令"本就取不到"仍常驻（与输入无关的事实）
    await w.find('#kops-exp-comment').setValue('laptop@2026')
    expect(w.text()).not.toContain('只能靠指纹认这把')
    expect(w.text()).toContain('本就取不到')
    w.unmount()
    // 无口令的独身行：台账 comment 沿用到输入框（后端明说"沿用台账里的"）
    const w2 = await mountRows([NOPUBFREE])
    await w2.find('.kops-act-export').trigger('click')
    await flushMicrotasks()
    expect((w2.find('#kops-exp-comment').element as HTMLInputElement).value).toBe('downloaded')
    expect(w2.text()).not.toContain('只能靠指纹认这把')
    expect(w2.text()).not.toContain('本就取不到')
    w2.unmount()
  })

  it('补出窗只有一份实现：从台账开合多次，ExportPubDialog 组件恒为一枚', async () => {
    const w = await mountRows([NOPUBG, NOPUBFREE])
    expect(w.findAllComponents(ExportPubDialog)).toHaveLength(1)
    await w.findAll('.kops-act-export')[0].trigger('click')
    await flushMicrotasks()
    await w.find('.cmodal-close').trigger('click')
    await flushMicrotasks()
    await w.findAll('.kops-act-export')[1].trigger('click')
    await flushMicrotasks()
    expect(w.findAllComponents(ExportPubDialog)).toHaveLength(1)
    expect((w.find('#kops-exp-comment').element as HTMLInputElement).value).toBe('downloaded')
    w.unmount()
  })
})

describe('verdict 上色表（按值查表，绝不解析文本）', () => {
  it('8 个取值全覆盖，色调与任务口径逐一对齐', () => {
    expect(DEPLOY_TONE).toEqual({
      'appended': 'positive',
      'already-present': 'information',
      'auth-rejected': 'warning',
      'unreachable': 'danger',
      'timeout': 'warning',
      'not-ssh': 'danger',
      'hostkey-changed': 'danger',
      'refused': 'danger',
    })
    expect(Object.keys(DEPLOY_LABEL).sort()).toEqual(Object.keys(DEPLOY_TONE).sort())
  })

  it('表外值不塌回任何已知档：中性兜底 + 枚举原样可见', () => {
    expect(deployToneOf('brand-new-verdict')).toBe('neutral')
    expect(deployLabelOf('brand-new-verdict')).toBe('brand-new-verdict')
  })
})

describe('辅助判定（与后端口径逐字对齐）', () => {
  it('nameTakenInLedger 按基名判（台账一把一条，name 不含 .pub）', () => {
    expect(nameTakenInLedger(LEDGER, 'id_plain')).toBe(true)
    expect(nameTakenInLedger(LEDGER, 'id_guard')).toBe(true)     // 私钥条目
    expect(nameTakenInLedger(LEDGER, 'id_orphan')).toBe(true)    // 只剩同名 .pub 的孤儿条目同样拦
    expect(nameTakenInLedger(LEDGER, 'id_fresh')).toBe(false)
    expect(nameTakenInLedger(LEDGER, '  ')).toBe(false)
  })

  it('rsaBitsOf：非 rsa 传 0，rsa 空值走 3072 缺省（normalizeRSABits 同口径）', () => {
    expect(rsaBitsOf('ed25519', 4096)).toBe(0)
    expect(rsaBitsOf('rsa', 0)).toBe(3072)
    expect(rsaBitsOf('rsa', NaN)).toBe(3072)
    expect(rsaBitsOf('rsa', 4096)).toBe(4096)
  })

  it('canExportPub：只有私钥的行才真，且与 guarded 无关（公钥段在明文外层）', () => {
    expect(canExportPub(PRIV)).toBe(false)     // 两把都有：维持现状
    expect(canExportPub(GUARDED)).toBe(false)
    expect(canExportPub(PUB)).toBe(false)      // 孤儿公钥行给的是另一个钮
    expect(canExportPub(NOPUBG)).toBe(true)    // 带口令的独身私钥——照样真
    expect(canExportPub(NOPUBFREE)).toBe(true)
  })

  it('canDeletePub：只有孤儿 .pub 行才真；两钮对任意行形状互斥（不可能同时出现）', () => {
    expect(canDeletePub(PUB)).toBe(true)
    for (const k of [PRIV, GUARDED, NOPUBG, NOPUBFREE]) expect(canDeletePub(k)).toBe(false)
    // 互斥不变式：穷举 hasPrivate×hasPublic 四种形状，两条件永不同时成立
    for (const hp of [true, false]) {
      for (const hb of [true, false]) {
        const shape = { ...PRIV, hasPrivate: hp, hasPublic: hb }
        expect(canExportPub(shape) && canDeletePub(shape)).toBe(false)
      }
    }
  })
})
