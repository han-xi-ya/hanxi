// SSH 主机清单（账本）的领域状态与动作。
//
// 一条贯穿性纪律，与后端 export.go 同源：**账本改动绝不自动导出**。写用户主目录
// ~/.ssh 只能是显式动作——所以这里的每一笔增删改只把对账状态重新拉一遍（漂移
// 亮着等人点），没有人点「同步给 ssh」就绝不碰 ExportConf。
//
// 校验单源在后端 packages/go/sshconf（ledger.go 头注明令「本文件不重复写一份
// 校验规则」）。aliasIssue 是唯一的例外镜像，只镜像**别名**这一个字段：它是
// 最现实的配置注入面，规则必须在输入现场讲透机制，让用户被拦时学到规则的形状；
// 其余字段前端只做轻拦（地址、端口格式），最终以后端原文为准，不造第二判据。
import { computed, ref, shallowRef } from 'vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { ExportSummary, HostEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'

// aliasIssue 镜像 sshconf.ValidateAlias：只允许 [a-z0-9_-]{1,32}，拒前导/尾随
// 连字符。返回给用户看的中文原因（含机制），空串 = 合法。文案逐条解释"为什么"：
// 换行=注入指令、空格=多开一个匹配模式、*?=通配符、大写=匹配区分大小写会静默
// 失效、前导 -= 被命令行当选项。顺序上先查危害最具体的字符，再查一般形状。
export function aliasIssue(alias: string): string {
  if (!alias) {
    return '别名必填：它就是 ssh nas01 里敲的那个名字，也是导出配置里 Host 行的匹配名。'
  }
  if (/[\r\n]/.test(alias)) {
    return '别名不能有换行：导出时它紧跟在 Host 行后，换行会让后半截被 ssh 当成一条独立配置读——等于往 ssh 的配置里注入可执行指令。'
  }
  if (/\s/.test(alias)) {
    return '别名不能含空白（空格/制表符）：Host 行以空白分列，一个空格就会让 ssh 把后半截当成第二个匹配模式，等于替你在配置里多开了一扇门。'
  }
  if (/[*?]/.test(alias)) {
    return `别名不能含 * 或 ?（当前「${alias}」）：它们在 Host 行是通配符——写成 nas* 会把这台机器的专属配置套到所有 nas 开头的连接上。`
  }
  if (/[A-Z]/.test(alias)) {
    return `别名用小写（当前「${alias}」）：ssh 的 Host 匹配区分大小写，NAS 和 nas 是两个不同的名字；账本里并存时人眼分不出，导出的配置里会多出一条你永远打不中的条目。`
  }
  if (alias.startsWith('-')) {
    return `别名不能以 - 开头（当前「${alias}」）：前导连字符会被 ssh 命令行当成选项参数——ssh -nas 读作「带参数 nas」，不是机器名。`
  }
  if (alias.endsWith('-')) {
    return '别名不能以 - 结尾：后端 sshconf 把首尾连字符一并拒绝（- 只放中间），这是唯一键的形制约束，不是偏好。'
  }
  if (alias.length > 32) {
    return `别名最长 32 字符（当前 ${alias.length}）：它是列表、命令行、配置里天天出现的名字，长了会在窄窗口里断行、在 ssh 后头难以辨认——描述机器请写备注。`
  }
  const bad = alias.replace(/[a-z0-9_-]/g, '').slice(0, 1)
  if (bad) {
    return `只允许小写字母、数字、_、-（当前含「${bad}」）：user@host 这类信息有专门的「用户」「地址」栏，写进别名只会让 Host 名带上符号，ssh 匹配不上。`
  }
  return ''
}

export type SaveResult = { ok: boolean; error: string }

// 行级"钥匙找不到"的判据来自后端 ExportSummary.missingKeyAliases（逐条 stat 的事实）。
//
// 这里**曾经**是自取台账做路径/基名比对，已废弃：台账只扫 ~/.ssh，指向别处的钥匙
// （D:\keys\id_rsa 这类真实存在的文件）会被误标成"找不到"。而这顶帽子挂在删除决策
// 现场，误标的代价是一次真误删。改由后端给确切别名名单后，前端不再有任何推断空间。

// baseOfKeyPath：私钥路径的基名（id_ed25519 形态），也是密钥列唯一上屏的可读片段。
export function baseOfKeyPath(p: string): string {
  const segs = p.trim().split(/[\\/]+/).filter(Boolean)
  return segs.length ? segs[segs.length - 1] : ''
}

export function useHosts() {
  const hosts = shallowRef<HostEntry[]>([])
  const summary = shallowRef<ExportSummary | null>(null)
  const listError = ref('')     // 账本读取/删除失败——落表格上方的 error-box
  const exportError = ref('')   // 导出对账一道的失败——落对账带里，不跨界遮表
  const exportNote = ref('')    // 上一次显式导出动作的回执原文（后端 message）
  const checkedAt = ref('')     // 上次对账时刻，仅界面自证"这不是陈旧截图"

  // 缺钥匙的别名集合：直接来自对账结果，**不另开一次 ListKeys 请求**（少一路状态、
  // 少一套会过期的旁证）。summary 为 null（还没对账过）时不标任何行——宁缺勿错。
  const missingKeys = computed<Set<string>>(() => new Set(summary.value?.missingKeyAliases ?? []))

  // 两把单飞锁分道：账本读写（ListHosts/增删改）与导出对账（Status/Conf/Remove）互不
  // 阻塞——挂载时并发；若共用一把锁，useAsyncAction 的重入守卫会把正当的并发报成
  // 「操作进行中」那种要人猜的假错误。
  const { busy: listBusy, run: runList } = useAsyncAction()
  const { busy: exportBusy, run: runExport } = useAsyncAction()

  async function refreshHosts() {
    listError.value = ''
    const res = await runList(() => SshKeysAPI.SshKeysService.ListHosts())
    if (res.ok) {
      // 空数组是正常空态（还没登记过机器），不是错误——与钥匙环同口径。
      hosts.value = res.data ?? []
    } else {
      listError.value = getErrorMessage(res.error)
    }
  }

  async function refreshStatus() {
    exportError.value = ''
    const res = await runExport(() => SshKeysAPI.SshKeysService.ExportStatus())
    if (res.ok) {
      // 三态（一致 / 已改动 / 还没导出过）一律取后端 message 原文展示：判据在
      // export.go 里（文件存在性 + 内容比对 + Include + 逐条钥匙 stat），前端
      // 不拿 drift/rendered 二次猜，也不解析文本来决定行为——行为只看布尔位。
      summary.value = res.data ?? null
      checkedAt.value = new Date().toLocaleTimeString()
    } else {
      // 读对账失败时保留上一次摊开的账继续看，错误走 exportError，
      // 不把已知的状态塌成「没导出过」的假象。
      exportError.value = getErrorMessage(res.error)
    }
  }

  // keyMissing：这一行的钥匙在本机存不存在，取自后端逐条 stat 的名单。
  // 未对账过（summary 为 null）时一律返回 false——**不标**胜过误标：
  // "找不到这把钥匙"会被当作"那删了也没事"的凭据，误标的代价就是一次真误删。
  function keyMissing(alias: string): boolean {
    return missingKeys.value.has(alias)
  }

  function refreshAll() {
    return Promise.all([refreshHosts(), refreshStatus()])
  }

  // saveHost 新增/整条覆盖。返回 ok/error 给表单就地显示（后端拒绝的原文要落
  // 在编辑现场，不是滚去表格上方的横幅里找）。成功后**只刷新对账状态**——
  // 漂移由此亮起来等人点「同步给 ssh」，本函数绝不代点。
  async function saveHost(h: HostEntry, isNew: boolean): Promise<SaveResult> {
    const res = await runList(() => (isNew
      ? SshKeysAPI.SshKeysService.AddHost(h)
      : SshKeysAPI.SshKeysService.UpdateHost(h)))
    if (!res.ok) return { ok: false, error: getErrorMessage(res.error) }
    hosts.value = res.data ?? []
    await refreshStatus()
    return { ok: true, error: '' }
  }

  // toggleEnabled 行内启停（Update 整条覆盖）。返回成败：失败时 UI 必须把
  // checkbox 拨回真实值——FRONTEND §9.5-1 onFollowToggle 的教训，失败要回写
  // 复位，不能让界面挂着一条账本没接受的开关状态。
  async function toggleEnabled(h: HostEntry, enabled: boolean): Promise<boolean> {
    const res = await runList(() => SshKeysAPI.SshKeysService.UpdateHost({ ...h, enabled }))
    if (res.ok) {
      hosts.value = res.data ?? []
      await refreshStatus()
      return true
    }
    listError.value = getErrorMessage(res.error)
    return false
  }

  async function removeHost(alias: string): Promise<boolean> {
    const res = await runList(() => SshKeysAPI.SshKeysService.RemoveHost(alias))
    if (res.ok) {
      hosts.value = res.data ?? []
      await refreshStatus()
      return true
    }
    listError.value = getErrorMessage(res.error)
    return false
  }

  // syncToSSH 是全面板唯一"往用户主目录写文件"的动作，只由按钮触发。
  // 成功后 summary 直接采用返回的 ExportSummary（含 Written/Included/KeyMissing），
  // 并把后端 message 留在 exportNote——confPath 的明写展示在面板层。
  async function syncToSSH() {
    exportError.value = ''
    exportNote.value = ''
    const res = await runExport(() => SshKeysAPI.SshKeysService.ExportConf())
    if (res.ok) {
      summary.value = res.data ?? null
      exportNote.value = res.data?.message ?? ''
    } else {
      exportError.value = getErrorMessage(res.error)
    }
  }

  // undoExport 撤销导出：删产物、摘自己那段 Include；账本不动。返回的 message
  // 先留作回执（"你自己的 config 未被删除"那句是给人的定心丸），再重读对账状态。
  async function undoExport() {
    exportError.value = ''
    exportNote.value = ''
    const res = await runExport(() => SshKeysAPI.SshKeysService.RemoveExport())
    if (res.ok) {
      exportNote.value = res.data?.message ?? ''
      await refreshStatus()
    } else {
      exportError.value = getErrorMessage(res.error)
    }
  }

  return {
    hosts, summary, listError, exportError, exportNote, checkedAt,
    missingKeys, keyMissing,
    listBusy, exportBusy,
    refreshAll, refreshHosts, refreshStatus,
    saveHost, toggleEnabled, removeHost, syncToSSH, undoExport,
  }
}
