// ============================================================================
// TranslucentTB → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 TranslucentTBView 原编排段（启停/重设/版本/联动/仓库全部 RPC 与
// 文案零变化）。事件面：`translucenttb:instance-state` /
// `translucenttb:version-download`（单键=version）。版本 Tab 与共享
// ManagedVersionPanel 逐字同形，本件也是四件中版本区全量走共享面板的样本。
// 方言注记：
//  - 启动钮禁用条件含「无任何已装版本」——超出 ManagedControlVerb.disabledFor
//    (state) 的纯状态签名，故 adapter 自持 hasInstalled ref（listInstalled 旁路
//    更新：store.load() 恒调本回调，导入/卸载/下载完成等清单变化后自动刷新），
//    disabledFor/titleFor 读该 ref 复刻原视图 openDirTarget 判据；
//  - 「🪄 重设任务栏状态」按批 0 槽位清单走 adapter.reset 形状，钮体留在控制条
//    #primary-action 位自绘（原 DOM 位逐字等价；其执行器不复用 runControl——
//    现状 reset 成功后不刷快照，与 quit/primary 的恒刷语义不同）；
//  - 「🗂 安装目录」钮按 running > active > 任一已装解析目标目录，依赖版本清单，
//    亦走 #primary-action 位，点击复用 versions.openDir（store.runOpenDir，
//    与现状同为无 busy 闩的目录直达）；
//  - 崩溃处置升级（2026-09-26）：failed+AV(0xC0000005) 语境两件套——
//    B1 banner 挂上后异步经 SysInfoAPI.GetReport 体检一次，命中虚拟显卡/
//    空壳监视器名单即点名（sysinfo 停用/RPC 失败/无命中一律静默回通用文案）；
//    C-迷你 视图 #primary-action 位按 pickDowngradeTarget 出「⬇ 装 X 试」
//    单动作钮（只走既有 runDownload 链，无编排状态机）。
// ============================================================================

import { ref } from 'vue'
import * as TBAPI from '../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/translucenttb/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/translucenttb/version/models'
import * as SysInfoAPI from '../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

// ---------- AV 崩溃处置纯函数面（判据/点名/降级候选，spec 直测） ----------

/** TranslucentTB 原生访问违例退出码（与后端 avCrashCode 同判据；本机账目为正的 32 位值）。 */
export const AV_EXIT_CODE = 0xc0000005

/** failed 快照的退出码是否反解码为 AV（0xC0000005）——B1 点名与 C-迷你钮共用门。 */
export function isAVCrash(snap: { state: string; exitCode?: number }): boolean {
  return snap.state === 'failed' && ((snap.exitCode ?? 0) >>> 0) === AV_EXIT_CODE
}

/**
 * 虚拟显卡点名名单（踩坑 #85 实测归因族）：远程工具（ToDesk/蒲公英 oray/
 * GameViewer/TapTap 串流 tapgame/向日葵）注入的虚拟显示适配器、通用
 * virtual 关键字、Windows 自带 IddSample 驱动。**只认在场事实，命中≠归罪。**
 */
export const VIRTUAL_GPU_RE = /toDesk|oray|todesk|gameviewer|tapgame|sunlogin|virtual|iddsample/i

/** 系统档案里虚拟显示器判据消费的最小结构（Report 的结构性子集，便于直测）。 */
export interface TBReportFacts {
  gpus?: Array<{ desc?: string } | null> | null
  displays?: Array<{ name?: string; width?: number; height?: number; primary?: boolean } | null> | null
}

export interface TBVirtualArtifacts {
  /** 命中名单的显卡 desc（在场原样）。 */
  virtualGpus: string[]
  /** 非主屏且宽/高为 0 的"空壳监视器" name。 */
  shellDisplays: string[]
}

/** B1 判据表：虚拟显卡命中名单 或 非主屏 Width/Height==0（空壳监视器）。 */
export function detectVirtualArtifacts(report: TBReportFacts): TBVirtualArtifacts {
  const arts: TBVirtualArtifacts = { virtualGpus: [], shellDisplays: [] }
  for (const gpu of report.gpus ?? []) {
    if (gpu?.desc && VIRTUAL_GPU_RE.test(gpu.desc)) arts.virtualGpus.push(gpu.desc)
  }
  for (const dis of report.displays ?? []) {
    if (dis && !dis.primary && (dis.width === 0 || dis.height === 0)) arts.shellDisplays.push(dis.name ?? '(未命名监视器)')
  }
  return arts
}

/** 点名追加句（措辞纪律：陈述在场+特征吻合，不归罪）；无命中回空串。 */
export function virtualArtifactWording(arts: TBVirtualArtifacts): string {
  const parts: string[] = []
  if (arts.virtualGpus.length > 0) parts.push(`虚拟显卡「${arts.virtualGpus.join('、')}」`)
  if (arts.shellDisplays.length > 0) parts.push(`空壳监视器「${arts.shellDisplays.join('、')}」`)
  if (parts.length === 0) return ''
  return `。另检见${parts.join('与')}（在场事实）——与本机崩溃特征高度吻合（踩坑 #85），建议设备管理器禁用该显示器验证`
}

/** YYYY.N 数值分段比较（与后端 versioncmp 同域）；任一侧非纯数字段版本回 null=不可比。 */
export function cmpTBVersion(a: string, b: string): number | null {
  if (!/^\d+(\.\d+)*$/.test(a) || !/^\d+(\.\d+)*$/.test(b)) return null
  const pa = a.split('.').map(Number)
  const pb = b.split('.').map(Number)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0
    const y = pb[i] ?? 0
    if (x !== y) return x > y ? 1 : -1
  }
  return 0
}

/**
 * C-迷你降级直钮的候选：崩溃版本的「最近旧稳定版」（远程非预发布、严格更早、
 * 取最高）。两种情形不出钮（返回 null，文案引导由后端 AV 话术承担，不硬造）：
 *  - 本机已有旧版在场——话术已点名「你已装 X，设为使用即可试」，直钮只补
 *    "本地无旧版需一键下载"的缺口；
 *  - 远程无更早稳定版/不可达/崩溃版本自身非规范号（imported- 兜底）。
 */
export function pickDowngradeTarget(
  crashVersion: string,
  installed: ReadonlyArray<{ version: string }>,
  releases: ReadonlyArray<ManagedReleaseRecord>,
): ManagedReleaseRecord | null {
  if (!crashVersion) return null
  if (installed.some((v) => (cmpTBVersion(v.version, crashVersion) ?? 0) < 0)) return null
  let best: ManagedReleaseRecord | null = null
  for (const rel of releases) {
    if (rel.isPre) continue
    const c = cmpTBVersion(rel.version, crashVersion)
    if (c === null || c >= 0) continue
    if (!best || (cmpTBVersion(rel.version, best.version) ?? -1) > 0) best = rel
  }
  return best
}

// 快照泛型收紧至模块绑定 Snapshot（B1 的 AV 判定要读 exitCode/stoppedAt，
// 基型 ManagedSnapshot 无此二位；markeron/paseo 同款声明形）。
export function createTBAdapter(): ManagedModuleAdapter<Snapshot> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 已装清单旁路镜像（见文件头方言注记）：驱动启动钮的禁用与 title 分支
  const hasInstalled = ref(false)

  // ---------- B1 虚拟显示器点名（AV 语境异步体检） ----------
  // 呈现通道仍是 banner computed——avInspect 是其读取的响应式 ref，体检落账
  // 即触发重投影。刻意不走 banner 同步推断：GetReport 是异步 RPC，且经 sysinfo
  // 调用门（模块被停用即 reject），任何失败静默降级回通用文案，不报错不打扰。
  const avInspect = ref<{ key: string; text: string }>({ key: '', text: '' })
  let inspectingKey = ''
  async function inspectVirtualArtifacts(key: string): Promise<void> {
    try {
      const report = await SysInfoAPI.GetReport()
      avInspect.value = { key, text: virtualArtifactWording(detectVirtualArtifacts(report)) }
    } catch {
      avInspect.value = { key, text: '' } // 停用/失败/无命中同路：通用文案照常，负结果也入账防重刷
    } finally {
      if (inspectingKey === key) inspectingKey = ''
    }
  }

  return {
    getStatus: () => TBAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('translucenttb:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('translucenttb:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: async () => {
        const rows = await TBAPI.ListInstalledVersions()
        hasInstalled.value = (rows?.length ?? 0) > 0
        return rows
      },
      listReleases: () => TBAPI.ListReleases(),
      getActive: () => TBAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await TBAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        const res = await TBAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例）。
        // TranslucentTB 的配置 settings.json 就在版本目录内，卸载连配置一起删——如实预告。
        const accepted = await confirm({
          title: `确定卸载 TranslucentTB ${v.version}？`,
          description:
            '该版本隔离目录将被删除，不可恢复。\n注意：你的透明样式配置（settings.json）就在该目录内，会一并删除。如需保留请先备份。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await TBAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案如实说明整套迁移语义
        const path = await prompt({
          title: '导入本地 TranslucentTB',
          description: '提示：配置（settings.json）跟着安装目录走，导入时整套迁入托管目录',
          label: '便携版目录完整路径（含 TranslucentTB.exe 与伴生 DLL）',
        })
        if (!path) return {}
        const info = await TBAPI.ImportLocal(path.trim())
        return { message: `已导入 TranslucentTB ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await TBAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run() {
          const out = await TBAPI.Start()
          return { message: out.message }
        },
        label: '🌫️ 启动',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'running' || state === 'starting' || state === 'external' || !hasInstalled.value,
        titleFor: (state) =>
          state === 'running' || state === 'starting'
            ? '已在运行'
            : state === 'external'
              ? '外部实例已在运行'
              : hasInstalled.value
                ? '启动 TranslucentTB（驻系统托盘）'
                : '尚未安装，请先在「版本管理」下载',
      },
      quit: {
        async run() {
          const out = await TBAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部实例请在 TranslucentTB 托盘菜单退出' : '优雅退出（保存设置后进程退出，任务栏还原）',
      },
    },

    // 契约 reset 槽：清态/复位类动词（任务栏外观异常时重放配置，等价上游托盘
    // 菜单 Reset dynamic state）。成功回执透出后端 message；失败为裸错误串——
    // 与原视图 resetState 词表逐字一致，由视图走槽钮的执行器承载 busy 闩。
    reset: {
      async run() {
        const out = await TBAPI.ResetState()
        return { message: out.message }
      },
    },

    // 条件提示条（三个变体互斥；文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 TranslucentTB 实例（非 Hanxi 托管）。可重设任务栏状态；如需彻底退出请在 TranslucentTB 托盘菜单操作。' }
      }
      if (s.state === 'failed') {
        const base = s.error || 'TranslucentTB 异常退出'
        if (!isAVCrash(s)) return { tone: 'error', text: base }
        // AV 档：逐字透传后端动态话术 + 命中时挂点名句。每次崩溃（version/码值/
        // 落终态时刻三元组为一笔）只体检一次；in-flight 去重防轮询重投影风暴。
        const key = `${s.version}|${s.exitCode}|${s.stoppedAt ?? ''}`
        if (avInspect.value.key !== key && inspectingKey !== key) {
          inspectingKey = key
          void inspectVirtualArtifacts(key)
        }
        const extra = avInspect.value.key === key ? avInspect.value.text : ''
        return { tone: 'error', text: base + extra }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'TranslucentTB 正在运行：任务栏透明样式在系统托盘图标菜单中设置（首次启动需在欢迎窗口确认许可）。退出进程后任务栏自动还原。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「🌫️ 启动」。样式设置在该程序的系统托盘图标菜单里完成（首次启动会弹欢迎窗口需先确认许可）。便携版仅支持 Windows 11，且依赖系统已装的 WinUI / VCLibs 框架包。'
      }
      if (s.state === 'starting') {
        return '正在拉起 TranslucentTB（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（TranslucentTB-portable-x64.zip，官方 digest 校验）；或「导入本地」把你机器上已有的便携版整套收纳进来',
        '注意：透明样式配置 settings.json 随版本目录走——多版本并存时各版本配置互相独立',
      ],
      firstUseEmpty: '尚未安装 TranslucentTB —— 下载官方便携版，或「导入本地安装」把现有便携版收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 TranslucentTB',
    },

    extras: {
      followOnExit: {
        get: () => TBAPI.GetFollowOnExit(),
        async set(next) {
          await TBAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具（任务栏透明随之消失）'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
        note: '（开启后 Hanxi 退出连带退出该工具，任务栏透明消失）',
      },
      repo: {
        url: () => TBAPI.RepositoryURL(),
        async open() {
          await TBAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
