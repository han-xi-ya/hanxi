// ============================================================================
// Recordly → 托管控制台 adapter（Wave 5 · 批 0 真实消费件 · 缺省 active 首样）
//
// 逐字迁移自 RecordlyView 原编排段（启停/版本/通道/仓库全部 RPC 与文案零变化）。
// 与黄金样本 ccswitch 的三处契约差异（批 0 成色实测，均不改共享件、在 adapter 内吃掉）：
//  ① NSIS 单目录覆盖式托管（至多一条安装记录）：后端无 GetActiveVersion /
//     SetActiveVersion RPC——versions 家族整体省略 getActive/setActive 可选位，
//     本件即「契约首次消费可缺省 active」的真实模块（store.load 回退空串）。
//  ② 本模块动词词面是「安装」（NSIS 静默装进托管目录）而非「下载」：store 统一
//     失败前缀 '下载失败: ' 与本视图逐字现词 '安装失败: ' 冲突——download 内
//     自捕获自弹 toast（成功路径仍走 ManagedActionResult，共享件零感知）。
//  ③ hint 需要版本表数据（stopped 引导行含已装版本号）：契约 hint(snap) 只投
//     快照，无法承载——引导行留视图侧，本件只提供 banner（纯状态投影）。
// 事件面：`recordly:instance-state` / `recordly:version-download`（进度键=version）。
// ============================================================================

import * as RecordlyAPI from '../../bindings/hanxi/internal/modules/recordly/recordlyservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/recordly/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/recordly/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

export function createRecordlyAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()
  const { showToast } = useToast()

  return {
    getStatus: () => RecordlyAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('recordly:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('recordly:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => RecordlyAPI.ListInstalledVersions(),
      listReleases: () => RecordlyAPI.ListReleases(),
      // 单目录覆盖式托管无「设定版本」概念：getActive/setActive 缺省——
      // 共享件消费面（store 空串回退 / 面板不渲染使用中位）见 ManagedVersionPanel.spec。

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        try {
          const res = await RecordlyAPI.DownloadVersion(rel.version)
          if (res === 'already-installed') {
            return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
          }
          return {}
        } catch (e) {
          // 本模块词面「安装失败: 」与 store 统一前缀「下载失败: 」不同（差异②），
          // 自弹保持逐字现词后静默回执，避免双 toast。
          showToast(`安装失败: ${getErrorMessage(e)}`)
          return {}
        }
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 Recordly ${v.version}？`,
          description: '托管安装目录将被删除。\n（%APPDATA%\\Recordly 中的配置与录像不受影响）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await RecordlyAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          description: '默认安装位置：%LOCALAPPDATA%\\Programs\\Recordly\n提示：配置与录像恒在 %APPDATA%\\Recordly，与安装位置无关',
          label: '请输入 Recordly 安装目录完整路径（含 Recordly.exe 与 resources\\app.asar 的整套目录）',
          placeholder: '%LOCALAPPDATA%\\Programs\\Recordly',
        })
        if (!path) return {}
        const info = await RecordlyAPI.ImportLocal(path.trim())
        return { message: `已导入 Recordly ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await RecordlyAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await RecordlyAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 Recordly 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await RecordlyAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 Recordly 窗口内退出' : '关闭其窗口（多窗口收尾不及会兜底强杀）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 Recordly 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 Recordly 窗口内关闭。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Recordly 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'Recordly 正在运行：录制与剪辑在其窗口内操作（配置与录像存于 %APPDATA%\\Recordly，跨版本共享）。闲置 5 分钟自动退出。' }
      }
      return null
    },

    // hint 缺位说明（差异③）：stopped 引导行需引用已装版本号（纯快照投影给不出），
    // 引导行留在视图模板内由 store 数据渲染。

    channel: {
      options: [
        { value: 'stable', label: 'Stable 稳定' },
        { value: 'beta', label: 'Beta 预发布' },
      ],
      get: () => RecordlyAPI.GetReleaseChannel(),
      async set(value: string): Promise<ManagedActionResult> {
        await RecordlyAPI.SetReleaseChannel(value)
        // 通道变更影响远程列表取数口径：请消费方重拉版本区
        return { reloadVersions: true }
      },
    },

    extras: {
      followOnExit: {
        get: () => RecordlyAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await RecordlyAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await RecordlyAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向托管安装）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 Recordly 用户数据目录（%APPDATA%\\Recordly，配置与录像库）',
        async open(): Promise<ManagedActionResult> {
          await RecordlyAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => RecordlyAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await RecordlyAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
