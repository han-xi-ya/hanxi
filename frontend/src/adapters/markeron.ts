// ============================================================================
// MarkerOn → 托管控制台 adapter（Wave 5 · 批 0 共享契约 · #primary-action 槽首例实战）
//
// 逐字迁移自 MarkerOnView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化），
// 模式对齐 src/adapters/ccswitch.ts 黄金样本。
//
// 特殊处：标注开关是六态钮（state × drawing × busy × installedCount 四维），
// ManagedControlVerb 的"label 定值 + state 纯函数"形状装不下 → 钮体不进 control，
// 按批 0 槽位约定走 adapter.toggle + #primary-action 槽：
//   - adapter.toggle.run：ToggleAnnotate 动词注入（成功回 { message }，失败抛错，
//     裸串 toast 由视图钮处理器负责——原 toggleAnnotate 口径）；
//   - annotateToggleView()：六态 label/sub/variant/disabled/hint 矩阵纯函数
//     （契约注释所指"矩阵输入由模块自算"即收在此处，视图只做拼装）；
//   - annotateBusy：切换进行中闩，视图钮（处理中…文案/禁用）与
//     control.quit.disabledFor（停止钮交叉禁用）共读，等效原视图单一 busy。
// 事件面：`markeron:instance-state` / `markeron:version-download`（进度键=version，
// 单键最简形态）；快照业务扩展字段 drawing 走 ManagedSnapshot 可选扩展
// （S=binding Snapshot 直接充当泛型实参，projection 函数里直读，零 cast）。
// 注意：ToggleAnnotate/StopAnnotate 引发的状态迁移由后端 emit 的 instance-state
// 事件即时回推（原视图动作后的显式 refreshStatus 由事件+2.5s 轮询等价承接）。
// ============================================================================

import { ref } from 'vue'
import * as MarkerAPI from '../../bindings/hanxi/internal/modules/markeron/markeronservice'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/markeron/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/markeron/version/models'
import type { ToggleOutcome, StopOutcome } from '../../bindings/hanxi/internal/modules/markeron/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import type { ManagedActionResult, ManagedModuleAdapter } from '../components/managed/adapter'

/**
 * 标注切换进行中闩（视图六态钮与 quit.disabledFor 共读；等效原视图 busy 的
 * toggle 半边——钮文案"处理中…"、钮禁用、以及切换期间禁点停止钮）。
 */
export const annotateBusy = ref(false)

/** 六态矩阵单帧渲染声明（label/sub/variant/disabled/hint 语义与原视图逐字一致）。 */
export interface AnnotateToggleView {
  /** 钮面主文案（不含 ✎ 前缀图标）。 */
  label: string
  /** 悬停指引副文案（hint 缺席时的 title 回落）。 */
  sub: string
  /** 钮体样式变体类（btn-toggle-* 五档）。 */
  variant: string
  /** 禁用条件（共享 busy 闩/切换闩之外：starting、external 且无信使可借）。 */
  disabled: boolean
  /** 禁用指引用提示条文案（空串=无）。 */
  hint: string
  /** title 合成：hint 优先，否则 sub（原 :title="toggleHint || toggleSubLabel"）。 */
  title: string
}

/**
 * 标注开关六态矩阵（busy × starting × running±drawing × external × failed × stopped）。
 * 入参 busy 为共享件 store.busy（停止/导入等动作闩），切换闩 annotateBusy 在内部
 * 合并——与"处理中…"同源的禁用判定绝不拆成两处。
 */
export function annotateToggleView(input: {
  state: string
  drawing: boolean
  busy: boolean
  installedCount: number
}): AnnotateToggleView {
  const { state, drawing, installedCount } = input
  const busy = input.busy || annotateBusy.value
  // external 且无已装版本：借力的"信使程序"无从拉起，只能指引快捷键
  const externalNoHost = state === 'external' && installedCount === 0

  let label: string
  if (busy) label = '处理中…'
  else
    switch (state) {
      case 'starting':
        label = '启动中…'
        break
      case 'running':
        label = drawing ? '退出标注' : '开启标注'
        break
      case 'external':
        label = '切换标注'
        break
      case 'failed':
        label = '重试启动'
        break
      default:
        label = '启动 MarkerOn'
    }

  let sub: string
  if (busy) sub = '正在切换标注状态…'
  else
    switch (state) {
      case 'running':
        sub = drawing ? '桌面覆盖层已开启，点击关闭' : 'MarkerOn 已在后台运行'
        break
      case 'external':
        sub = '外部实例状态未知，点击切换'
        break
      case 'failed':
        sub = '上次异常退出，点击重新启动'
        break
      case 'starting':
        sub = '等待 MarkerOn 就绪（≤20 秒）'
        break
      default:
        sub = '后台静默运行，不进入标注'
    }

  let variant: string
  switch (state) {
    case 'running':
      variant = drawing ? 'btn-toggle-active' : 'btn-toggle-outline'
      break
    case 'external':
      variant = 'btn-toggle-warn'
      break
    case 'failed':
      variant = 'btn-toggle-danger'
      break
    default:
      variant = 'btn-toggle-primary'
  }

  const hint = externalNoHost
    ? '外部实例无法代为切换（无可用信使程序），请使用快捷键 Ctrl+Shift+D'
    : ''

  return {
    label,
    sub,
    variant,
    disabled: busy || state === 'starting' || externalNoHost,
    hint,
    title: hint || sub,
  }
}

export function createMarkerOnAdapter(): ManagedModuleAdapter<Snapshot> {
  const { confirm } = useConfirm()

  return {
    getStatus: () => MarkerAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('markeron:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('markeron:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => MarkerAPI.ListInstalledVersions(),
      listReleases: () => MarkerAPI.ListReleases(),
      getActive: () => MarkerAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await MarkerAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        const res = await MarkerAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const versionShort = v.version.replace(/^v/, '')
        const accepted = await confirm({
          title: `确定卸载 MarkerOn ${versionShort}？`,
          description: '该版本隔离目录及其便携数据将被删除，不可恢复。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await MarkerAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${versionShort}`, reloadVersions: true }
      },

      // 打开安装目录必须传「目录」而非 exe——explorer.exe 收文件参数会执行文件
      // （对 MarkerOn.exe 会直接拉起实例），收目录才稳定打开文件夹窗口
      async openDir(v): Promise<ManagedActionResult> {
        await AppAPI.AppService.OpenPath(v.dir)
        return {}
      },
    },

    // 主钮位空置：六态标注开关整钮走 #primary-action 槽；退出钮（停止）恒居钮区末位
    control: {
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out: StopOutcome = await MarkerAPI.StopAnnotate()
          return { message: out.message }
        },
        label: '⏻ 停止',
        // 原 v-if="isRunningOrStarting" 收编为常驻禁用；切换进行中同样禁（原单一 busy 口径）
        disabledFor: (state) => (state !== 'running' && state !== 'starting') || annotateBusy.value,
      },
    },

    toggle: {
      async run(): Promise<ManagedActionResult> {
        annotateBusy.value = true
        try {
          const out: ToggleOutcome = await MarkerAPI.ToggleAnnotate()
          return { message: out.message }
        } finally {
          annotateBusy.value = false
        }
      },
    },

    // 状态词覆写：running 按 drawing 细分两态（快照业务扩展字段的消费点）
    stateText: (s) => {
      switch (s.state) {
        case 'running':
          return s.drawing ? '标注已开启' : '已启动（未标注）'
        case 'starting':
          return '启动中…'
        case 'failed':
          return '异常退出'
        case 'external':
          return '外部运行'
        default:
          return '未运行'
      }
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 MarkerOn 实例（非 Hanxi 托管）。可切换标注；如需彻底退出请在 MarkerOn 托盘操作。' } as const
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'MarkerOn 异常退出' } as const
      }
      if (s.state === 'running' && s.drawing) {
        return { tone: 'ok', text: '标注已开启，可将屏幕涂画演示；再次点击开关或按 Ctrl+Shift+D 退出。' } as const
      }
      return null
    },

    copy: {
      remoteSummary: (n) => `远程可用 ${n} 个版本`,
      metaHints: ['便携包下载自上游 ifer47/markeron；SmartScreen 拦截时选「更多信息 → 仍要运行」'],
      firstUseEmpty: '尚未安装 MarkerOn —— 下载官方便携版后即可一键标注',
      remoteUnavailable: '无法加载远程版本列表（GitHub 可能被限流），可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先停止标注',
    },

    extras: {
      followOnExit: {
        get: () => MarkerAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await MarkerAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await MarkerAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      repo: {
        url: () => MarkerAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await MarkerAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
