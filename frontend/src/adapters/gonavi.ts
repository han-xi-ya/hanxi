// ============================================================================
// GoNavi → 托管控制台 adapter（三线并行 · gonavi 前端线，形制照 ccswitch/termora 家族）
//
// 事件面：`gonavi:instance-state`（引擎裸 Snapshot）/ `gonavi:version-download`
// （单键=version，与 release 行按版本对齐）；动词面：OpenWindow→control.primary、
// Quit→control.quit（执行前经 QuitAdvisory 取后端收口的预告话术弹确认）。
//
// 类型注记：绑定物（gonaviservice 等）由主会话三线并流后统一 regenerate，此前
// vue-tsc 报缺符号属预期。本文件不 import 绑定 models，GoNaviStatus/方言包为
// **骨架线实码 JSON tag 的本地结构投影**（gonavi/models.go、version/models.go
// 逐字段对齐）——再生成后与绑定类型自然互认。
//
// 语义要点（全部如实入文案，见 banner/hint/metaHints；骨架线实面口径）：
//  - 上游自带应用内更新器，会在托管目录**原地替换 exe**——version 线 VerifyLedger
//    只读复查据此标 drifted/driftNote（漂移事实只从 GetStatus 取，instance-state
//    事件不带）：状态区琥珀警示压过运行态横幅（mangodisk「完整性压过运行态」
//    同权先例），只报告、不自动处置；
//  - 数据在 %USERPROFILE%\.gonavi，跨版本共享，不随版本目录隔离、卸载不触碰；
//  - 关窗有未保存 SQL 确认框，托管退出可能被其挂住——宽限后 JobObject 强杀兜底
//    （预告话术静态收口在后端 QuitAdvisory，退出确认框必须消费它）；
//  - 便携态无单实例互斥：外部多开属用户自由，hanxi 只甄别与唤窗、不拦阻；
//  - state 五词表 stopped|starting|running|external|failed（引擎无 quitting 档，
//    退出治理的后端同步语义经 QuitAdvisory 预告 + Quit 回执承载）。
// ============================================================================

import * as GoNaviAPI from '../../bindings/hanxi/internal/modules/gonavi/gonaviservice'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import {
  soleVersionUninstallNote,
  type ManagedActionResult,
  type ManagedModuleAdapter,
  type ManagedSnapshot,
  type ManagedVersionRecord,
} from '../components/managed/adapter'

/**
 * GetStatus 组合快照（gonavi/models.go GoNaviStatus）：instance.Snapshot 经
 * JSON 展平 + 账目漂移两字段。drifted 供状态区警示徽章；driftNote 为漂移/无法
 * 比对的如实明细（空=未复查）。使用版本走 store.getActive 单源，不在本快照。
 */
export interface GoNaviStatus extends ManagedSnapshot {
  /** 退出码（未运行/在跑为 0）。 */
  exitCode?: number
  /** state==external 时为 true。 */
  external?: boolean
  /** 停用时刻（RFC3339）。 */
  stoppedAt?: string
  /** 使用版本 exe 发生账外漂移（只报告不处置）。 */
  drifted: boolean
  /** 漂移复查明细（含"账本无摘要无法比对"等如实场景；空=未复查）。 */
  driftNote: string
}

/** 已装版本记录的 gonavi 方言字段包（version/models.go GoNaviVersionInfo 子集）。 */
export interface GoNaviVersionDialect {
  /** 落位后 exe 是否发生账外漂移。 */
  hashDrifted?: boolean
  /** 漂移复查明细。 */
  driftNote?: string
}

/** `gonavi:version-download` 事件载荷（家族单键=version 形态）。 */
interface GoNaviProgress {
  version: string
  stage: string
  done: number
  total: number
  message: string
}

export function createGoNaviAdapter(): ManagedModuleAdapter<GoNaviStatus, GoNaviVersionDialect> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 账目事实（drifted/driftNote）只随 GetStatus 组合快照回带；instance-state
  // 事件按后端实码只发引擎裸 Snapshot——事件现值缺字段时沿用最近轮询事实，
  // 防漂移警示在事件轮次闪失（后端事件若升级为全量则以现值为准）。
  let lastLedger: { drifted: boolean; driftNote: string } = { drifted: false, driftNote: '' }
  // 下载 confirm 闸的现态来源（冻结契约「若运行实例在」）：轮询与事件双路都喂。
  let latestState = ''

  return {
    async getStatus() {
      const s = await GoNaviAPI.GetStatus()
      if (s) {
        if (s.drifted !== undefined) lastLedger = { drifted: s.drifted, driftNote: s.driftNote ?? '' }
        latestState = s.state ?? ''
      }
      return s
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<GoNaviStatus>('gonavi:instance-state', (s) => {
        if (!s) return
        latestState = s.state ?? ''
        if (s.drifted === undefined) {
          cb({ ...s, ...lastLedger })
        } else {
          lastLedger = { drifted: s.drifted, driftNote: s.driftNote ?? '' }
          cb(s)
        }
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<GoNaviProgress>('gonavi:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => GoNaviAPI.ListInstalledVersions(),
      listReleases: () => GoNaviAPI.ListReleases(),
      getActive: () => GoNaviAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await GoNaviAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        // confirm 闸（冻结契约「若运行实例在」口径）：实例在跑时下载若命中正被
        // 占用的版本目录会被文件锁挡下，且可能与上游应用内更新器撞车——如实
        // 预告后由用户裁决。
        if (['running', 'starting', 'external'].includes(latestState)) {
          const accepted = await confirm({
            title: `实例正在运行，仍要下载 ${rel.version}？`,
            description:
              'GoNavi 实例在运行时下载：新版本仅写入独立目录不影响当前进程，但覆写正在使用的版本目录会被文件锁挡下；' +
              '若该版本已被其应用内更新器原地替换，hanxi 将标为「账外漂移」如实警示。建议先退出再下载。',
            tone: 'warning',
          })
          if (!accepted) return {}
        }
        const res = await GoNaviAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        if (res === 'in-progress') {
          return { message: `版本 ${rel.version} 正在下载中，请留意进度` }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 末版放行清账新模块口径（后端实码）：允许卸到零版本，卸空后 active
        // 清空、下次冷启动给「先下载或导入」指引；确认框先行探测并如实预告。
        const soleNote = await soleVersionUninstallNote(() => GoNaviAPI.ListInstalledVersions())
        const accepted = await confirm({
          title: `确定卸载 GoNavi ${v.version}？`,
          description:
            '该版本隔离目录将被删除，不可恢复。\n（数据与配置在 %USERPROFILE%\\.gonavi，不随版本目录删除）' + soleNote,
          tone: 'danger',
        })
        if (!accepted) return {}
        await GoNaviAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '导入本地 GoNavi',
          description: '提示：数据恒在 %USERPROFILE%\\.gonavi，与安装位置无关',
          label: '安装目录完整路径（含 GoNavi.exe；绿色版解压目录或本机安装目录均可）',
        })
        if (!path) return {}
        const info = await GoNaviAPI.ImportLocal(path.trim())
        return { message: `已导入 GoNavi ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await GoNaviAPI.OpenDir(v.dir)
        return {}
      },
    },

    // 五态词表（实面 stopped|starting|running|external|failed，引擎无 quitting
    // 档）；未识别态兜底"本会话未托管"——状态不明宁可报无。
    stateText: (s) =>
      ({
        stopped: '本会话未托管',
        starting: '正在启动',
        running: '托管实例运行中',
        failed: '实例操作失败',
        external: '外部实例运行中',
      })[s.state] ?? '本会话未托管',

    // 状态灯色档（⑧）：漂移属完整性事实，压过运行态色（failed 例外——取态
    // 失败/异常退出是当下更要紧的事实）。
    statusTone: (s) => (s.state === 'failed' ? 'failed' : s.drifted ? 'warn' : s.state),

    // 条件提示条：failed 恒先（当下故障更要紧）；账外漂移警示压过运行态横幅
    // （mangodisk 完整性先例同权）——漂移中即使 running 也不给 ok 绿横幅。
    banner: (s) => {
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'GoNavi 异常退出' }
      }
      if (s.drifted) {
        return {
          tone: 'warn',
          text: `当前使用版本的二进制已被应用自更新替换，与下载账目不一致（账外漂移）——hanxi 只警示、不自动处置${s.driftNote ? `；检测明细：${s.driftNote}` : ''}。如需对齐账目，删除该版本后重新下载即可。`,
        }
      }
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 GoNavi 实例（非 Hanxi 托管；便携态无单实例，多开属用户自由）：Hanxi 只甄别与唤窗、不代管其退出——如需关闭请在其窗口内操作。' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'GoNavi 正在运行：SQL 工作在其自有窗口进行（数据在 %USERPROFILE%\\.gonavi，跨版本共享）。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 GoNavi（便携态无单实例互斥，重复点击由 Hanxi 唤回既有窗口，不另开进程）。数据在 %USERPROFILE%\\.gonavi，与托管版本目录无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 GoNavi（需 WebView2 Runtime，Win11 系统自带）…'
      }
      return null
    },

    // metaHints 四条如实披露（骨架报告口径）：①来源与官方摘要信任根（宁拒不列）
    // ②漂移徽章语义 ③数据目录不随版本隔离、卸载不触碰 ④WebView2 依赖与便携
    // 无单实例——多开属用户自由、托管只甄别，退出宽限强杀兜底。
    copy: {
      metaHints: [
        '便携包下载自官方 GitHub Releases，以官方摘要为信任根校验（Release API digest 主源＋官方 SHA256SUMS 备用，双核不过宁拒不装）；「上游发布」列标注各资产平台/形态，当前托管形态高亮；或「导入本地安装」收纳既有安装',
        'GoNavi 自带应用内更新器，会在托管目录原地换 exe——该版本随即被标为「账外漂移」（状态区琥珀警示）：二进制已被应用自更新替换，与下载账目不一致，hanxi 只如实警示、不自动处置，对齐账目可删除该版本重新下载',
        '数据与 SQL 在 %USERPROFILE%\\.gonavi，跨版本共享——不随托管版本目录隔离，卸载任何版本（含末版）都不触碰它',
        'GoNavi 依赖 WebView2 Runtime（Win11 系统自带，Win10 需已安装运行时）；便携态无单实例互斥，外部多开属用户自由，hanxi 只甄别与唤窗、不拦阻；托管退出可能被「未保存 SQL」确认框挂住——宽限后强杀兜底',
      ],
      firstUseEmpty: '尚未安装 GoNavi —— 下载官方发布包，或「导入本地安装」把现有 GoNavi 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 GoNavi',
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await GoNaviAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起窗口（便携态无单实例：Hanxi 直操作唤回既有窗口，绝不二次拉起）'
            : state === 'external'
              ? '唤回已有 GoNavi 窗口（外部实例按其 PID 定位）'
              : state === 'starting'
                ? '启动中…'
                : '启动 GoNavi 并打开窗口',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          // 退出预告话术收口在后端 QuitAdvisory（静态文案单点，防各入口漂移）：
          // 取回原文呈确认框，同意才投递关闭——预告「未保存 SQL 挂窗与宽限强杀」。
          const advisory = await GoNaviAPI.QuitAdvisory()
          const accepted = await confirm({
            title: '退出 GoNavi',
            description: advisory || '托管退出在宽限后将强制终止进程，未保存内容可能丢失。',
            tone: 'warning',
          })
          if (!accepted) return {}
          const out = await GoNaviAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external'
            ? '外部实例不越权代杀：确认框后将只得到其窗口内退出的指引'
            : '投递关闭消息：有未保存 SQL 时会弹确认框，可能被挂住——宽限后强杀兜底',
      },
    },

    extras: {
      followOnExit: {
        get: () => GoNaviAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await GoNaviAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具（有未保存 SQL 时可能被其确认框挂住，宽限后强杀兜底）'
              : '已关闭：Hanxi 退出不影响该工具，托管实例继续独立运行（下次启动生效）',
          }
        },
        note: '（你自行打开的 GoNavi 窗口从不受 Hanxi 退出影响）',
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await GoNaviAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 GoNavi 数据目录（%USERPROFILE%\\.gonavi：SQL 与配置，跨版本共享、不随卸载删除）',
        async open(): Promise<ManagedActionResult> {
          await GoNaviAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => GoNaviAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await GoNaviAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
