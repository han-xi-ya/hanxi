// ============================================================================
// DBX → 托管控制台 adapter（三线并行 · dbx 前端线，形制照 ccswitch/gonavi 家族）
//
// 事件面：`dbx:instance-state` / `dbx:version-download`（单键=version，与
// release 行按版本对齐）；动词面：OpenWindow→control.primary、Quit→control.quit。
// 远程行 form='portable' 由后端逐行回填，共享面板经 releaseFormWord 自动出
// 「便携」chip——adapter 纯透传，零干预（N13 契约）。
//
// 类型注记：绑定面（service + 各 models 生成物）由主会话在三线并流后统一
// regenerate，此前 vue-tsc 报缺符号属预期。本文件不 import 绑定 models，
// DbxStatus/DbxProgress 为**冻结契约的本地结构投影**（JSON 字段名与后端 tag
// 逐字对齐）——再生成后与绑定类型自然互认，若后端字段有更宽事实（如逐版本
// drifted），经此一处收口扩接。
//
// 语义要点（全部如实入文案，见 banner/hint/metaHints，不夸大不隐瞒）：
//  - 数据统一留存 hanxi 数据根（启动注入 DBX_DATA_DIR），卸载版本不删数据；
//    portable 标记文件随包保留；
//  - 上游日更节奏（月均 20+ 版）：远程版本列表天然长，按需停留稳定版即可；
//  - 上游自带应用内自更新，会在托管目录**原地替换 exe**——hanxi 账本漂移
//    检测据此标 drifted：状态区琥珀警示压过运行态横幅（mangodisk「完整性
//    压过运行态」同权先例），只警示、不自动处置；
//  - 校验口径：GitHub 官方 digest 四层完整；附带 .sig 为 minisign 签名，
//    本托管不验证（如实一句，不含糊）；
//  - 关窗驻托盘不退出：「退出」先优雅投递，可能被托盘弹问挂住，宽限后强杀
//    兜底；若用户开启应用自带「托管备份」，计划任务可能重新拉起进程——
//    hanxi 不追杀外部同名进程；下载无 confirm 闸（末版卸载放行新口径）。
// ============================================================================

import * as DBXAPI from '../../bindings/hanxi/internal/modules/dbx/dbxservice'
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
 * GetStatus 组合快照（冻结契约）：instance.Snapshot 投影面 + 账目四字段。
 * drifted/driftNote 供漂移警示（banner/状态灯/页头徽标三处同源）；使用版本
 * 走 store.getActive 单源不在本快照（绑定 DBXStatus 同形）；dataDir
 * 为数据目录钮的展示直读位（联动卡「数据目录」行经 OpenDir 打开）。
 */
export interface DbxStatus extends ManagedSnapshot {
  /** 账本漂移：当前托管版本的二进制已被应用自更新原地替换，与下载账目不一致。 */
  drifted: boolean
  /** 后端检测明细（漂移/无法比对等如实场景；空=未复查）。 */
  driftNote: string
  /** 数据目录（hanxi 数据根下，启动注入 DBX_DATA_DIR 的落点）。 */
  dataDir: string
}

/** `dbx:version-download` 事件载荷（家族单键=version 形态）。 */
interface DbxProgress {
  version: string
  stage: string
  done: number
  total: number
  message: string
}

export function createDbxAdapter(): ManagedModuleAdapter<DbxStatus> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 账目事实（drifted/note/dataDir/activeVersion）只随 GetStatus 组合快照
  // 回带；instance-state 事件按引擎惯例可能只发基础 Snapshot——事件现值缺
  // 字段时沿用最近轮询事实，防漂移警示与数据目录在事件轮次闪失（后端事件
  // 若已携全量字段则以现值为准）。
  let lastLedger: { drifted: boolean; driftNote: string; dataDir: string } = {
    drifted: false,
    driftNote: '',
    dataDir: '',
  }

  return {
    async getStatus() {
      const s = await DBXAPI.GetStatus()
      if (s && s.drifted !== undefined) {
        lastLedger = { drifted: s.drifted, driftNote: s.driftNote ?? '', dataDir: s.dataDir ?? '' }
      }
      return s
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<DbxStatus>('dbx:instance-state', (s) => {
        if (!s) return
        cb(s.drifted === undefined ? { ...s, ...lastLedger } : s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DbxProgress>('dbx:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => DBXAPI.ListInstalledVersions(),
      listReleases: () => DBXAPI.ListReleases(),
      getActive: () => DBXAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await DBXAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        // 无 confirm 闸（冻结契约新口径）：新版本写独立目录，不碰在用目录。
        const res = await DBXAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 末版放行清账新模块口径：后端 guard 已放行唯一版本卸载（成功后清账），
        // 确认框先行探测并如实预告；数据留存 hanxi 数据根，不随版本目录删除。
        const soleNote = await soleVersionUninstallNote(() => DBXAPI.ListInstalledVersions())
        const accepted = await confirm({
          title: `确定卸载 DBX ${v.version}？`,
          description:
            '该版本隔离目录将被删除，不可恢复。\n（数据统一留存 Hanxi 数据根（启动注入 DBX_DATA_DIR），不随版本目录删除）' + soleNote,
          tone: 'danger',
        })
        if (!accepted) return {}
        await DBXAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '导入本地 DBX',
          description: '提示：数据统一留存 Hanxi 数据根（DBX_DATA_DIR），与安装位置无关',
          label: '便携目录完整路径（目录内含 DBX 主程序 exe）',
        })
        if (!path) return {}
        const info = await DBXAPI.ImportLocal(path.trim())
        return { message: `已导入 DBX ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await DBXAPI.OpenDir(v.dir)
        return {}
      },
    },

    // 六态词表（引擎含 quitting 档：退出消息已投递、宽限收口前——dbx 的
    // 托盘弹问正落在此档语义里）；未知态兜底"本会话未托管"。
    stateText: (s) =>
      ({
        stopped: '本会话未托管',
        starting: '正在启动',
        running: '托管实例运行中',
        quitting: '正在退出',
        failed: '实例操作失败',
        external: '外部实例运行中',
      })[s.state] ?? '本会话未托管',

    // 状态灯色档（⑧）：漂移属完整性事实，压过运行态色（failed 例外——取态
    // 失败/异常退出是当下更要紧的事实）；quitting 走 starting 琥珀脉冲。
    statusTone: (s) => {
      if (s.state === 'failed') return 'failed'
      if (s.drifted) return 'warn'
      return s.state === 'quitting' ? 'starting' : s.state
    },

    // 条件提示条：账本漂移警示压过运行态横幅（mangodisk 完整性先例同权）；
    // failed 恒先（当下故障更要紧），漂移仍在页头徽标与状态灯（statusTone
    // 例外序一致）。
    banner: (s) => {
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'DBX 异常退出' }
      }
      if (s.drifted) {
        const who = s.version || '当前使用版本'
        return {
          tone: 'warn',
          text: `${who} 的二进制已被应用自更新替换，与下载账目不一致（账本漂移）——hanxi 只警示、不自动处置${s.driftNote ? `；检测附言：${s.driftNote}` : ''}。如需对齐账目，删除该版本后重新下载即可。`,
        }
      }
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 DBX 实例（非 Hanxi 托管）：可唤起其窗口；如需彻底退出请在其托盘菜单操作（Hanxi 不代关非托管进程）。' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'DBX 正在运行：数据统一留存 Hanxi 数据根（启动注入 DBX_DATA_DIR），跨版本共享；关窗后驻托盘不退出。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 DBX。数据统一留存 Hanxi 数据根（DBX_DATA_DIR），与托管版本目录无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 DBX…'
      }
      if (s.state === 'quitting') {
        return '已投递优雅关闭：驻托盘形态可能被托盘弹问挂住，宽限期后强杀兜底…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自官方 GitHub Releases（官方 digest 四层完整校验；附带 .sig 为 minisign 签名，本托管不验证）；或「导入本地安装」把你机器上已有的便携版收纳进来',
        '上游日更节奏（月均 20+ 版本），远程版本列表天然很长：更新频繁，按需停留稳定版即可',
        '上游应用内自更新会在托管目录原地换 exe——该版本即被标为「账本漂移」徽章并琥珀警示：二进制已被应用自更新替换，与下载账目不一致，hanxi 只如实警示、不自动处置；如需对齐账目，删除该版本后重新下载即可',
        '数据统一留存 Hanxi 数据根（启动注入 DBX_DATA_DIR），卸载版本不删数据；portable 标记文件随包保留',
        '关窗驻托盘不退出：「退出」为优雅关闭，可能被托盘弹问挂住，宽限后强杀兜底；若你开启了应用自带「托管备份」，计划任务可能重新拉起进程——hanxi 不追杀外部同名进程',
      ],
      firstUseEmpty: '尚未安装 DBX —— 下载官方便携版，或「导入本地安装」把现有 DBX 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 DBX',
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await DBXAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting' || state === 'quitting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起窗口（已驻托盘时点击即恢复窗口）'
            : state === 'external'
              ? '唤回已有 DBX 窗口'
              : state === 'starting'
                ? '启动中…'
                : '启动 DBX 并打开窗口',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await DBXAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'quitting' && state !== 'external',
        titleFor: (state) =>
          state === 'external'
            ? '外部实例请在其托盘菜单退出（Hanxi 不代关非托管进程）'
            : '优雅关闭可能被托盘弹问挂住，宽限后强杀兜底；若开启了应用自带「托管备份」，计划任务可能重新拉起（Hanxi 不追杀外部同名进程）',
      },
    },

    extras: {
      followOnExit: {
        get: () => DBXAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await DBXAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具（驻托盘形态可能被托盘弹问挂住，宽限后强杀兜底）'
              : '已关闭：Hanxi 退出不影响该工具，继续驻托盘独立运行（下次启动生效）',
          }
        },
        note: '（仅管 Hanxi 托管实例；外部自启或被「托管备份」计划任务拉起的进程不受影响）',
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 DBX 数据目录（统一留存 Hanxi 数据根，启动注入 DBX_DATA_DIR；卸载版本不删数据）',
        async open(): Promise<ManagedActionResult> {
          // 现值优先（轮询/事件双路喂入）；从未取得过组合快照时现拉一次兜底。
          let dir = lastLedger.dataDir
          if (!dir) dir = (await DBXAPI.GetStatus())?.dataDir ?? ''
          if (!dir) throw new Error('尚未取得数据目录路径，请稍后重试')
          await DBXAPI.OpenDir(dir)
          return {}
        },
      },
      repo: {
        url: () => DBXAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await DBXAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
