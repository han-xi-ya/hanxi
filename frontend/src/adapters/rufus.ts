// ============================================================================
// Rufus → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 RufusView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化）。
// 事件面：`rufus:instance-state` / `rufus:version-download`（单键=version）。
// 方言注记：Rufus 为一次性工具（写盘完即关窗），无常驻/自启类辅助——
// extras 仅提供随关与仓库两条目，桌面快捷方式等缺项由 ManagedExtrasCard
// 自动缺席；「打开位置」钮依赖已装版本清单（running > active > 任一），
// 属状态相关钮，留在视图 extras 卡的 #extras-action 位经 store.runOpenDir 走。
// 阶段词表含 install 不含 extract（单文件落位而非解压），远程表的
// 「校验落位…」阶段文案与通道徽标为方言，表格留在视图方言区。
// ============================================================================

import * as RufusAPI from '../../bindings/hanxi/internal/modules/rufus/rufusservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/rufus/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/rufus/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

export function createRufusAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => RufusAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('rufus:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('rufus:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => RufusAPI.ListInstalledVersions(),
      listReleases: () => RufusAPI.ListReleases(),
      getActive: () => RufusAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await RufusAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        const res = await RufusAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const ok = await confirm({
          title: `确定卸载 Rufus ${v.version}？`,
          description: '该版本隔离目录将被删除（其中的 rufus.ini 便携设置一并移除），不可恢复。',
          tone: 'danger',
        })
        if (!ok) return {}
        await RufusAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入 Rufus 便携 exe 完整路径或所在目录（如 rufus.exe / rufus-4.15p.exe）',
          description: '提示：源旁若随行 rufus.ini（便携配置），将一并迁入 Hanxi 托管',
        })
        if (!path) return {}
        const info = await RufusAPI.ImportLocal(path.trim())
        return { message: `已导入 Rufus ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await RufusAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run() {
          const out = await RufusAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起 Rufus 窗口' : state === 'starting' ? '启动中…' : '启动 Rufus 并显示主窗口'),
      },
      quit: {
        async run() {
          const out = await RufusAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external'
            ? '外部实例请直接在其窗口关闭'
            : '关窗消息（异常滞留时宽限后强杀兜底；写盘中途终止会产出半成品盘）',
      },
    },

    // 条件提示条（三个变体互斥；running 态承载写盘不可中断的真实契约，逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 Rufus 实例（非 Hanxi 托管，可能是浏览器下载的原名版）。可唤起其窗口；如需退出请直接关窗。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Rufus 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'Rufus 正在运行：选盘、分区方案、镜像写入等全部操作在其窗口内完成。⚠ 正在写入 U 盘时切勿点「退出」或关闭 Hanxi——中途终止会产出半成品启动盘。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 Rufus，插入 U 盘后在其界面内选择镜像即可制作。注意：Rufus 上游清单强制管理员权限——需 Hanxi 本身以管理员身份运行（否则启动会直接报"要求提升"）。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Rufus…'
      }
      return null
    },

    extras: {
      followOnExit: {
        get: () => RufusAPI.GetFollowOnExit(),
        async set(next) {
          await RufusAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
        note: '（关闭后 Hanxi 退出完全不影响该工具；写盘任务进行中建议保持独立运行）',
      },
      repo: {
        url: () => RufusAPI.RepositoryURL(),
        async open() {
          await RufusAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
