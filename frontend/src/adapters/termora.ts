// ============================================================================
// Termora → 托管控制台 adapter（W4 托管批 · N9，模式照抄 windterm 姊妹件）
//
// 事件面：`termora:instance-state` / `termora:version-download`（单键=version）。
// 方言注记：
//  - 单实例信使家族：「打开窗口」三分支（信使激活自有/外部、冷启动）由后端
//    裁决，UI 恒一钮（与多实例 guoheview 形态同权不同语义——这里是转发激活，
//    不是另开窗口）；
//  - 外部实例退出 confirm-force 档：Quit(false) 首入 confirm-required → 全局
//    确认框呈后端风险原文 → 同意后 Quit(true) 重入（授权闸在后端）；
//  - 上游 2.x 线全量 prerelease 标记（beta 即事实主干）：列表全量呈现，
//    预发布徽标如实展示，不做通道开关藏行。
// ============================================================================

import * as TermoraAPI from '../../bindings/hanxi/internal/modules/termora/termoraservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/termora/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/termora/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

export function createTermoraAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  async function runQuit(confirmToken: boolean): Promise<ManagedActionResult> {
    const out = await TermoraAPI.Quit(confirmToken)
    if (out.action === 'confirm-required') {
      const accepted = await confirm({
        title: '强制结束外部 Termora？',
        description: out.risk || out.message,
        tone: 'danger',
      })
      if (!accepted) return { message: '已取消退出（外部实例保持运行）' }
      return runQuit(true)
    }
    return { message: out.message || undefined }
  }

  return {
    getStatus: () => TermoraAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('termora:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('termora:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    // 六态词表（引擎原生 quitting：关闭请求已投递、宽限收口前）
    stateText: (snapshot) =>
      ({
        stopped: '本会话未托管',
        starting: '正在启动',
        running: '托管实例运行中',
        quitting: '正在退出',
        failed: '实例异常',
        external: '外部实例运行中',
      })[snapshot.state] ?? '本会话未托管',

    statusTone: (snapshot) => (snapshot.state === 'quitting' ? 'starting' : snapshot.state),

    versions: {
      listInstalled: () => TermoraAPI.ListInstalledVersions(),
      listReleases: () => TermoraAPI.ListReleases(),
      getActive: () => TermoraAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await TermoraAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        const res = await TermoraAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        const accepted = await confirm({
          title: `确定卸载 Termora ${v.version}？`,
          description: '该版本托管目录（内含 data\\ 会话与密钥索引数据）将被整体删除，不可恢复。\n如需保留会话数据，请先「打开位置」手动迁出。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await TermoraAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '请输入本机 Termora 便携目录完整路径（含 Termora.exe 的解压目录）',
          description: 'data\\ 内的会话与密钥数据随目录一并收纳进托管；导入前建议先退出该目录正在运行的 Termora',
        })
        if (!path) return {}
        const info = await TermoraAPI.ImportLocal(path.trim())
        return { message: `已导入 Termora ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await TermoraAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await TermoraAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting' || state === 'quitting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起托管实例窗口（上游单实例激活通道）'
            : state === 'external'
              ? '唤回已有 Termora 窗口'
              : state === 'starting'
                ? '启动中…'
                : '启动托管实例并打开会话窗口',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          return runQuit(false)
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external' && state !== 'quitting',
        titleFor: (state) =>
          state === 'external'
            ? '优雅退出优先；不响应时经你确认后强制结束（会断开其活动 SSH 会话）'
            : '关闭托管窗口（活动会话会被要求确认断开；挂起时宽限后强杀兜底）',
      },
    },

    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到你在 Hanxi 之外启动的 Termora（单实例应用，托管与它互斥）：「打开窗口」唤回它；「退出」按会话类风险档治理——优雅优先，不响应时明示确认后才会强制结束。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Termora 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: '托管实例运行中：SSH/Sftp 会话在其自有窗口进行（portable 数据在版本目录 data\\ 内自包含）。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」冷启动托管实例（JVM 首启约 2~10 秒）。会话凭据与索引保存在版本目录 data\\ 内，卸载版本前请先迁出。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Termora（JVM + Swing 初始化，请稍候）…'
      }
      if (s.state === 'quitting') {
        return '已投递关闭请求：如存在活动会话，Termora 可能弹出断开确认框，请在其窗口内确认…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（windows-x86-64.zip，官方 sha256 摘要四层校验；自带 JRE，不依赖系统 Java）',
        '上游 2.x 线全部以预发布（beta）发布——beta 即事实主干，列表如实全列、预发布徽标不藏行',
        '上游双许可（AGPL-3.0 或商业许可）：hanxi 仅转链官方分发，不搬运二进制',
      ],
      firstUseEmpty: '尚未安装 Termora —— 下载官方便携包，或「导入本地安装」把现有解压目录收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 Termora',
    },

    extras: {
      followOnExit: {
        get: () => TermoraAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await TermoraAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭托管实例（含其活动会话）'
              : '已关闭：Hanxi 退出不影响 Termora，托管实例继续独立运行（下次启动生效）',
          }
        },
        note: '（你自行打开的 Termora 窗口从不受 Hanxi 退出影响）',
      },
      repo: {
        url: () => TermoraAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await TermoraAPI.OpenRepository()
          return {}
        },
        label: '上游仓库',
      },
    },
  }
}
