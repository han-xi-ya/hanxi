// ============================================================================
// LiteMonitor → 托管控制台 adapter（Wave 5 · 批 1）
//
// 逐字迁移自 LiteMonitorView 原编排段（启停/版本/联动全部 RPC 与文案零变化），
// 参照实现见 src/adapters/ccswitch.ts（批 0 黄金样本）。
// 第三路数据特化：GetRuntimeStatus（.NET 8 桌面运行时探测）收进 adapter——
// 视图生命周期内仅首拉一次（与原 onMounted 单发等价），结果以**快照扩展
// 字段** hasDesktop8 并入 LmConsoleSnapshot（ManagedSnapshot 业务附加形），
// 经状态头之后 #console-extra 槽消费；缺运行库警示与 740 提权重启按钮
// 同槽渲染（原文案逐字保留）。
// 辅助卡「📂 打开位置」为便携目录直达钮（无独立数据目录）：adapter 镜像
// 已装/活动/快照三源，open 时按原优先级（运行版本 → 活动版本 → 任一已装）
// 解析目标目录。
// 事件面：`litemonitor:instance-state` / `litemonitor:version-download`
// （进度键=version，单键最简形态）。
// ============================================================================

import * as LiteMonitorAPI from '../../bindings/hanxi/internal/modules/litemonitor/litemonitorservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/litemonitor/instance/models'
import type { LMVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/litemonitor/version/models'
import type { RuntimeStatus } from '../../bindings/hanxi/internal/modules/litemonitor/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

/** 托管快照 + .NET 8 运行时探测业务附加（批 0 契约的 S extends ManagedSnapshot 扩展位）。 */
export type LmConsoleSnapshot = Snapshot & { hasDesktop8?: boolean }

export function createLiteMonitorAdapter(): ManagedModuleAdapter<LmConsoleSnapshot> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // ---- 第三路数据：GetRuntimeStatus 视图生命周期内单发（原 onMounted 单拉语义），
  // 结果缓存并随快照回流（getStatus 首拉与 instance-state 事件两路都并入）----
  let runtime: RuntimeStatus | null = null
  let runtimeFetched = false
  async function ensureRuntime(): Promise<void> {
    if (runtimeFetched) return
    runtimeFetched = true
    try {
      runtime = (await LiteMonitorAPI.GetRuntimeStatus()) ?? null
    } catch (e) {
      // 探测失败静默：hasDesktop8 缺席则不警示（迁移前 loadRuntime 口径）
      console.warn('litemonitor GetRuntimeStatus failed:', getErrorMessage(e))
    }
  }
  const withRuntime = (s: Snapshot): LmConsoleSnapshot => ({ ...s, hasDesktop8: runtime?.hasDesktop8 })

  // ---- 「打开位置」三源镜像：已装列表 / 活动版本 / 最新快照（各在既有
  // 加载点同步，open 时按原视图优先级解析，与原 reactive computed 等价）----
  let installedMirror: LMVersionInfo[] = []
  let activeMirror = ''
  let snapMirror: LmConsoleSnapshot | null = null
  function preferredInstalled(): LMVersionInfo | null {
    const prefer = snapMirror?.state === 'running' && snapMirror?.version ? snapMirror.version : activeMirror
    return installedMirror.find((v) => v.version === prefer) ?? installedMirror[0] ?? null
  }

  return {
    async getStatus() {
      const runtimeJob = ensureRuntime()
      const s = await LiteMonitorAPI.GetStatus()
      await runtimeJob
      snapMirror = s ? withRuntime(s) : null
      return snapMirror
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('litemonitor:instance-state', (s) => {
        if (!s) return
        snapMirror = withRuntime(s)
        cb(snapMirror)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('litemonitor:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      async listInstalled() {
        const list = (await LiteMonitorAPI.ListInstalledVersions()) ?? []
        installedMirror = list
        return list
      },
      listReleases: () => LiteMonitorAPI.ListReleases(),
      async getActive() {
        const a = (await LiteMonitorAPI.GetActiveVersion()) ?? ''
        activeMirror = a
        return a
      },

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await LiteMonitorAPI.SetActiveVersion(version)
        activeMirror = ver
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await LiteMonitorAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 LiteMonitor ${v.version}？`,
          description: '该版本隔离目录将被删除（其中的 settings.json 设置、主题与插件一并移除），不可恢复。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await LiteMonitorAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入 LiteMonitor 便携目录完整路径（含 LiteMonitor.exe；可带一层解压目录）',
          description: '提示：settings.json、themes、plugins 随该目录走，将整套迁入 Hanxi 托管',
        })
        if (!path) return {}
        const info = await LiteMonitorAPI.ImportLocal(path.trim())
        return { message: `已导入 LiteMonitor ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await LiteMonitorAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await LiteMonitorAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running' ? '唤起监控条' : state === 'starting' ? '启动中…' : '启动 LiteMonitor 并显示监控条',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await LiteMonitorAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 LiteMonitor 托盘退出' : '关窗消息（异常滞留时宽限后强杀兜底）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）。
    // 注意：.NET 8 缺运行时警示是**独立常驻**提示（原视图双 banner 并列），
    // 不并入本投影——经 #console-extra 槽按快照扩展字段渲染，见视图。
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 LiteMonitor 实例（非 Hanxi 托管）。可唤起其监控条；如需彻底退出请在其托盘操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'LiteMonitor 异常退出' }
      }
      if (s.state === 'running') {
        return {
          tone: 'ok',
          text: 'LiteMonitor 正在运行：监控条/任务栏显示、主题与插件等全部配置在其界面内完成（settings.json 随本版本目录保存）。',
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 LiteMonitor，监控条将显示在桌面顶部/任务栏。上游要求管理员权限，首启会弹一次 UAC 确认；读取 CPU 温度时可能提示安装 PawnIO 驱动（一次性的内核驱动，用于硬件传感器）。'
      }
      if (s.state === 'starting') {
        return '正在拉起 LiteMonitor（首启含 UAC/驱动确认时请在系统弹窗中放行）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（win-x64.zip，官方 digest 校验）；或「导入本地」把你机器上已解压的便携套件整套收纳（settings.json/主题/插件一并迁入）',
      ],
      firstUseEmpty: '尚未安装 LiteMonitor —— 下载官方便携版，或「导入本地套件」把现有便携目录收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 LiteMonitor',
    },

    extras: {
      followOnExit: {
        get: () => LiteMonitorAPI.GetFollowOnExit(),
        note: '（关闭后 Hanxi 退出完全不影响该工具；开启监控条常驻可关除此项）',
        async set(next): Promise<ManagedActionResult> {
          await LiteMonitorAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await LiteMonitorAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '📂 打开位置',
        title: '打开便携目录（settings.json/主题/插件所在处）',
        async open(): Promise<ManagedActionResult> {
          const target = preferredInstalled()
          // 原钮无版本时禁用并以 title 指引；共享卡无禁用位，转为失败回执（toast 前缀统一）
          if (!target) throw new Error('尚未安装任何版本')
          await LiteMonitorAPI.OpenDir(target.dir)
          return {}
        },
      },
      repo: {
        url: () => LiteMonitorAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await LiteMonitorAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
