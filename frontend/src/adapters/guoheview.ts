// ============================================================================
// 果核看图 → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 GuoheViewView 原编排段（启停/版本/联动/官网全部 RPC 与文案零变化）。
// 事件面：`guoheview:instance-state` / `guoheview:version-download`（单键=version）。
// 方言注记：
//  - 多实例上游的 OpenWindow「聚焦/唤回/另开」三分支由后端裁决，UI 恒为一钮，
//    钮面 title 按 state 投影指引（control.primary.titleFor），视图零分支；
//  - 官方发布接口无发布时间、带 stable/beta 通道列，ManagedVersionPanel 的
//    通用表（发布时间列）会丢失通道信息，远程表留在视图方言区；
//    ViewRelease 缺 published 字段，按契约以空串占位补齐（方言表不消费该列）。
// ============================================================================

import * as GuoheViewAPI from '../../bindings/hanxi/internal/modules/guoheview/guoheviewservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/guoheview/instance/models'
import type { DownloadProgress, ViewRelease } from '../../bindings/hanxi/internal/modules/guoheview/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

/** 方言远程行：ViewRelease + 契约要求的 published 占位（接口无发布时间列，空串）。 */
export type GuoheReleaseRow = ViewRelease & Pick<ManagedReleaseRecord, 'published'>

export function createGuoheViewAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => GuoheViewAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('guoheview:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('guoheview:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => GuoheViewAPI.ListInstalledVersions(),
      listReleases: async () => {
        const rows = ((await GuoheViewAPI.ListReleases()) ?? []) as ViewRelease[]
        return rows.map((r) => ({ ...r, published: '' }))
      },
      getActive: () => GuoheViewAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await GuoheViewAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        const res = await GuoheViewAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载果核看图 ${v.version}？`,
          description: '该版本托管目录（含其 config.ini 设置）将被删除，不可恢复。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await GuoheViewAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入本机果核看图便携目录完整路径（整个解压目录，含 GuoheView.exe）',
          description: '设置（config.ini）随目录一并收纳进托管；安装版目录也可导入，将自动转为便携模式',
        })
        if (!path) return {}
        const info = await GuoheViewAPI.ImportLocal(path.trim())
        return { message: `已导入果核看图 ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await GuoheViewAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run() {
          // 多实例三分支（聚焦自有窗口 / 唤回外部窗口 / 另开独立新窗）由后端
          // OpenWindow 内部裁决，回执 message 如实透出——UI 恒为一钮。
          const out = await GuoheViewAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起托管实例窗口'
            : state === 'external'
              ? '唤回已有窗口，无窗可聚焦时另开独立新窗'
              : state === 'starting'
                ? '启动中…'
                : '启动托管实例并打开浏览窗口',
      },
      quit: {
        async run() {
          const out = await GuoheViewAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external'
            ? '外部窗口请在其窗口内关闭（Hanxi 不代关）'
            : '优雅退出：向托管窗口投递关窗消息，设置随窗口落盘',
      },
    },

    // 条件提示条（三个变体互斥；文案承载多实例与更新器两条真实契约，逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到你在 Hanxi 之外打开的看图窗口（双击图片等）。「打开窗口」会唤回它或另开独立新窗口；这类窗口不受 Hanxi 管退。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || '果核看图异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: '托管实例正在运行。浏览、缩放与色彩管理在 GuoheView 自有窗口完成（关窗即退出）；上游自带更新检查，版本升级请回这里走托管安装，避免内置更新覆写托管目录。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动托管实例，浏览 RAW/HEIC/WebP 等格式在它自己的窗口内完成（关窗即退）。便携托管实例的设置保存在版本目录内的 config.ini。'
      }
      if (s.state === 'starting') {
        return '正在拉起果核看图（极速内核，通常 1 秒内）…'
      }
      return null
    },

    extras: {
      followOnExit: {
        get: () => GuoheViewAPI.GetFollowOnExit(),
        async set(next) {
          await GuoheViewAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭托管实例'
              : '已关闭：Hanxi 退出不影响该工具，托管实例继续独立运行（下次启动生效）',
          }
        },
        note: '（关闭后 Hanxi 退出完全不影响托管实例；你自行打开的看图窗口从不受影响）',
      },
      repo: {
        url: () => GuoheViewAPI.RepositoryURL(),
        async open() {
          await GuoheViewAPI.OpenRepository()
          return {}
        },
        label: '官网',
        copyToast: '官网地址已复制',
      },
    },
  }
}
