// 密钥写动作的共用件：verdict 上色表、表单常量、半把钥匙的行内入口矩阵、
// 以及"提交即销毁"的口令清理。
//
// 为什么不塞进 useKeyring：useKeyring 管的是**只读台账**的领域状态（数据源
// ListKeys，没有任何密钥材料）；这里是六个显式写动作（生成 / 改口令 / 成对删除 /
// 删除公钥 / 补出公钥 / 部署公钥）与行内动作入口矩阵。两类东西的凭据边界完全
// 不同——台账面永远碰不到口令，写动作面的口令必须"任一出口即销毁"。混在一个
// composable 里，"台账不含密钥材料"这条
// 边界就要靠人自觉，而本仓的纪律是边界靠结构。
//
// 上色纪律（与 useSSHCheck 的 verdict 表同源）：按枚举值查表，**绝不解析中文
// reason/Message 文本来决定行为**——文案会改，码不会（types.go / manage.go 的
// 包注释都钉了这条）。表里没收录的值落到"未知结论"兜底档，绝不塌回成功。
import type { KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'

// neutral 只出现在表外兜底（deployToneOf）：后端加了新 verdict 而本表没跟上时，
// 宁可中性色 + 原样显示枚举值，也不许塌进任何一档误导。
export type DeployTone = 'positive' | 'information' | 'warning' | 'danger' | 'neutral'

// DEPLOY_TONE / DEPLOY_LABEL：DeployResult.verdict 的全部 8 个取值。
// 注意 bindings 的 typedef 注释只列了 7 个（hostkey-changed 是后补的 TOFU 防线，
// 生成物注释滞后）——以 deploy/deploy.go 的常量为准，这里 8 态全配。
// 色调对应任务口径：appended 绿 / already-present 中性偏正 / auth-rejected 与
// timeout 黄 / unreachable、not-ssh、refused 红 / hostkey-changed 红且走独立分支。
export const DEPLOY_TONE: Record<string, DeployTone> = {
  'appended': 'positive',
  'already-present': 'information',
  'auth-rejected': 'warning',
  'unreachable': 'danger',
  'timeout': 'warning',
  'not-ssh': 'danger',
  'hostkey-changed': 'danger',
  'refused': 'danger',
}

export const DEPLOY_LABEL: Record<string, string> = {
  'appended': '已写入',
  'already-present': '本来就有',
  'auth-rejected': '认证被拒',
  'unreachable': '没连上',
  'timeout': '超时',
  'not-ssh': '不是 SSH 服务',
  'hostkey-changed': '主机密钥变了',
  'refused': '本地拒发',
}

// 表外值兜底（后端加了新 verdict 而前端还没跟上的窗口期）：中性档 + 原样显示
// 枚举值本身。比"塌回成功色"或"抛 undefined"都诚实。
export function deployToneOf(verdict: string): DeployTone {
  return DEPLOY_TONE[verdict] ?? 'neutral'
}

export function deployLabelOf(verdict: string): string {
  return DEPLOY_LABEL[verdict] ?? verdict
}

// 生成表单的选项常量。范围与后端 keyops/keyops.go 一致（2048–16384，默认 3072），
// 但这里只列常用档——位数不是越大越好，4096 以上生成慢且没人真用得更多。
export const GENERATE_TYPES = [
  { value: 'ed25519', label: 'Ed25519（推荐）' },
  { value: 'rsa', label: 'RSA' },
] as const

export const RSA_BITS_OPTIONS = [2048, 3072, 4096, 8192]
export const RSA_BITS_DEFAULT = 3072

// 部署预算：DeployPublicKey 的 timeoutMs 是"单段"预算（manage.go 把总 ctx 定为
// 它的两倍，后端把 >30000 夹回 30000）。默认给 10s 而不是连接测试的 5s——部署比
// 试登录多出"远端执行写文件"一段，慢机器上 5s 容易把成功跑成 timeout。
export const DEPLOY_TIMEOUT_DEFAULT = 10000
export const DEPLOY_TIMEOUT_MAX = 30000

// RSA 位数输入夹带：非 rsa 类型时后端刻意忽略该值，这里也照传，
// 与 normalizeRSABits"为无害残留报错是自找麻烦"的口径对齐。
export function rsaBitsOf(type: string, bits: number): number {
  return type === 'rsa' ? (Number(bits) || RSA_BITS_DEFAULT) : 0
}

// —— "半把钥匙"两类行的动作入口矩阵（条件唯一真相）——
//
// 为什么收在一处：「补出公钥」与「删除公钥」的出现条件互斥且各有来历（一把只有
// 私钥、一把只剩公钥），散在模板里各写一遍判据，早晚漂成两套。
//
// **guarded 不参与判定**：OpenSSH 新格式只加密私钥段，公钥段在外层明文里，
// 带口令的私钥同样补得动公钥且全程不接触口令（keyops/pubpair.go 的实测事实）。
// 能不能补、是不是孤儿由后端裁判，前端只看台账的 hasPrivate/hasPublic 两字段。
export function canExportPub(k: KeyEntry): boolean {
  return k.hasPrivate && !k.hasPublic
}

// 孤儿 .pub（私钥本就不存在）才有「删除公钥」入口：成对的行走成对删除。
// 后端 DeletePubOnly 同样拒绝"配套私钥还在"的路径——入口条件与后端限制同形，
// 界面上给不出一个必被拒的按钮。
export function canDeletePub(k: KeyEntry): boolean {
  return !k.hasPrivate && k.hasPublic
}

// DeleteKeyDialog 的对象形态。**一个对话框两套话术**（复制一份对话框，两套文案
// 会各自演化，是本仓反复清的病）：pair=私钥与配套 .pub 成对删；
// orphan-pub=只删孤儿 .pub 文件本身。
export type DeleteMode = 'pair' | 'orphan-pub'

// nameTakenInLedger 用**台账已有条目**预判"目标已存在"：后端要 overwrite=true
// 才放行，而界面对应的必须是一个危险确认，不是替用户偷偷置真。
//
// 台账是"一把钥匙一条"、name 为不含 .pub 的基名（keyring.go），所以基名相等
// 同时覆盖"同名私钥在"与"只剩同名孤儿 .pub"两种碰撞——后者 generate.go 的
// checkOverwrite 同样拦（写出指纹对不上号的配对更糟）。
// 预判只是 UX 门闩；台账可能过期，真正的裁判仍是后端——被拒时错误原样显示。
export function nameTakenInLedger(keys: KeyEntry[], name: string): boolean {
  const n = name.trim()
  if (!n) return false
  return keys.some(k => k.name === n)
}

// 口令销毁的纪律（写在这里、由各对话框在 submit/close/wipe 三处各自执行）：
// **提交后任一出口（成功、失败、取消、关窗、卸载）都必须先在任何其它动作之前
// 把口令 ref 置空**。做不到运行期擦拭（字符串进了请求就归引擎管），能保证的是
// 前端不再留任何比这次调用更活的副本——展示面、复制面、结果对象里都不许有它。
// 各窗要清的 ref 组合不同（口令/旧口令/回显串），故不抽公共函数，逐窗显式清；
// 正确性由 KeyOps.spec 的"在途即空、回执不含、重开为空"三组断言钉。
