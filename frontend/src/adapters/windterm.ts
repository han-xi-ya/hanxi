// ============================================================================
// WindTerm → 托管控制台 adapter（W4 托管批，模式照抄 flclash 黄金形态）
//
// 事件面：`windterm:instance-state` / `windterm:version-download`（单键=version）。
// 方言注记：
//  - 外部实例退出为 N3 终裁 confirm-force 档：Quit(false) 首入回执
//    confirm-required → 全局确认框明示风险 → 同意后 Quit(true) 重入执行
//    （vscode 安装闸同款往返契约，授权门在后端、UI 只收集同意）；
//  - 上游 releases 无官方摘要（实测 32 个 release 全量无 digest）：metaHints
//    如实披露降级三层口径，不装"官方校验"。
// ============================================================================

import * as WindTermAPI from '../../bindings/hanxi/internal/modules/windterm/windtermservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/windterm/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/windterm/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

export function createWindTermAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  async function runQuit(confirmToken: boolean): Promise<ManagedActionResult> {
    const out = await WindTermAPI.Quit(confirmToken)
    if (out.action === 'confirm-required') {
      const accepted = await confirm({
        title: '强制结束外部 WindTerm？',
        description: out.risk || out.message,
        tone: 'danger',
      })
      if (!accepted) return { message: '已取消退出（外部实例保持运行）' }
      return runQuit(true)
    }
    return { message: out.message || undefined }
  }

  return {
    getStatus: () => WindTermAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('windterm:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('windterm:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    // 六态词表（snipaste 先例）：引擎原生含 quitting（关闭请求已投递、宽限收口前）
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
      listInstalled: () => WindTermAPI.ListInstalledVersions(),
      listReleases: () => WindTermAPI.ListReleases(),
      getActive: () => WindTermAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await WindTermAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        const res = await WindTermAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        const accepted = await confirm({
          title: `确定卸载 WindTerm ${v.version}？`,
          description: '该版本托管目录（内含 portable 会话与密钥索引数据）将被整体删除，不可恢复。\n如需保留会话数据，请先「打开位置」手动迁出。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await WindTermAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '请输入本机 WindTerm 便携目录完整路径（含 WindTerm.exe 的解压目录）',
          description: '会话/密钥索引数据随目录一并收纳进托管；导入前建议先退出该目录正在运行的 WindTerm',
        })
        if (!path) return {}
        const info = await WindTermAPI.ImportLocal(path.trim())
        return { message: `已导入 WindTerm ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await WindTermAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          // 多实例三分支（启动托管 / 聚焦自有 / 唤回外部窗口）由后端 OpenWindow
          // 内部裁决，回执 message 如实透出——UI 恒为一钮。
          const out = await WindTermAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting' || state === 'quitting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起托管实例窗口'
            : state === 'external'
              ? '唤回已有 WindTerm 窗口'
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

    // 条件提示条（互斥变体）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到你在 Hanxi 之外启动的 WindTerm：「打开窗口」唤回它；「退出」按会话类风险档治理——优雅优先，不响应时明示确认后才会强制结束。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'WindTerm 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: '托管实例运行中：SSH/Sftp/Serial 会话在其自有窗口进行（portable 数据在版本目录内自包含）。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」冷启动托管实例（Qt 全家桶首窗约 1~5 秒）。会话凭据与索引保存在版本目录内，卸载版本前请先迁出。'
      }
      if (s.state === 'starting') {
        return '正在拉起 WindTerm（Qt 首次加载稍慢，请稍候）…'
      }
      if (s.state === 'quitting') {
        return '已投递关闭请求：如存在活动会话，WindTerm 可能弹出断开确认框，请在其窗口内确认…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（*_Windows_Portable_x86_64.zip）；上游未发布官方哈希（实测），完整性走降级三层：字节数 + zip CRC 闸门 + 布局自检，自算摘要入账本防漂移',
        '上游为部分开源项目（README 自述完全免费使用，开源部分 Apache-2.0，仓库根无 LICENSE 文件）：hanxi 仅转链官方分发，不搬运二进制',
      ],
      firstUseEmpty: '尚未安装 WindTerm —— 下载官方便携版，或「导入本地安装」把现有解压目录收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 WindTerm',
    },

    extras: {
      followOnExit: {
        get: () => WindTermAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await WindTermAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭托管实例（含其活动会话）'
              : '已关闭：Hanxi 退出不影响 WindTerm，托管实例继续独立运行（下次启动生效）',
          }
        },
        note: '（你自行打开的 WindTerm 窗口从不受 Hanxi 退出影响）',
      },
      repo: {
        url: () => WindTermAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await WindTermAPI.OpenRepository()
          return {}
        },
        label: '上游仓库',
      },
    },
  }
}
