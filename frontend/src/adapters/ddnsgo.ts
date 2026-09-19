// ============================================================================
// ddns-go → 托管控制台 adapter（Wave 5 · 批 0：log 槽 / port 槽首个实战件）
//
// 逐字迁移自 DdnsGoView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化），
// 模式照抄 src/adapters/ccswitch.ts 黄金样本。与 ccswitch 的差异即批 0 在
// adapter.ts 槽位清单预留、本模块首次接线的两个扩展槽：
//  - log：`ddnsgo:instance-log` 事件流逐行归一（subscribe 需 setup 期调用，
//    视图侧完成）+ pull() 回补 Logs(200) 历史；面板 UI 由视图渲染在
//    ManagedConsoleShell #console-extra 槽（共享件只定形状不渲染日志）。
//  - port：Get/SetListenPort 读写面；端口行 UI 同样留在视图 #console-extra
//    （批 0 extras 卡未接线端口位，输入态校验/回滚随输入框留在视图）。
// 动词面：Start→control.primary（钮区首位，保持原三钮 DOM 顺序）、
// Quit→control.quit；OpenConsole 为契约 control 双位之外的第三钮，经扩展面
// openConsole 声明、视图以 #primary-action 槽渲染（label/title 逐字沿用现词）。
// 事件面：`ddnsgo:instance-state` / `ddnsgo:version-download`（进度键=version，
// 单键最简形态）。
// ============================================================================

import * as DdnsGoAPI from '../../bindings/hanxi/internal/modules/ddnsgo/ddnsgoservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/ddnsgo/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/ddnsgo/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type {
  ManagedActionResult,
  ManagedControlVerb,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

/**
 * ddns-go 适配总面：批 0 log/port 两个可选扩展槽在本模块收紧为必选
 * （视图直接消费，免去非空断言）；openConsole 为 control 双位之外的
 * 模块扩展动词声明位。
 */
export interface DdnsGoAdapter extends ManagedModuleAdapter {
  log: {
    subscribe(cb: (line: string) => void): void
    pull(): PromiseLike<string[] | null | undefined>
  }
  port: {
    get(): PromiseLike<number>
    set(port: number): PromiseLike<ManagedActionResult>
  }
  openConsole: ManagedControlVerb
}

export function createDdnsGoAdapter(): DdnsGoAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => DdnsGoAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('ddnsgo:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('ddnsgo:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    // ---------- log 槽（批 0 预留形状的首个实战）----------
    // 事件流经视图 setup 期 subscribe 接入累积面板；pull 供首屏与
    // "非运行→running" 切换时回补（引擎环形缓冲随新进程重启）。
    log: {
      subscribe: (cb) => {
        useWailsEvent<{ line?: string }>('ddnsgo:instance-log', (entry) => {
          if (entry?.line) cb(entry.line)
        })
      },
      pull: () => DdnsGoAPI.Logs(200),
    },

    // ---------- port 槽 ----------
    // 1024~65535 输入校验与失败回滚属端口行输入态，留在视图；本槽只做读写投影。
    port: {
      get: () => DdnsGoAPI.GetListenPort(),
      async set(port: number): Promise<ManagedActionResult> {
        const res = await DdnsGoAPI.SetListenPort(port)
        return {
          message:
            res === 'pending'
              ? `端口已设为 ${port}（当前运行实例不变，下次启动生效）`
              : `监听端口已设为 ${port}`,
        }
      },
    },

    versions: {
      listInstalled: () => DdnsGoAPI.ListInstalledVersions(),
      listReleases: () => DdnsGoAPI.ListReleases(),
      getActive: () => DdnsGoAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await DdnsGoAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await DdnsGoAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `卸载 ddns-go ${v.version}`,
          description: '该版本隔离目录将被删除，不可恢复。',
          details: [{ label: '域名配置', value: '~/.ddns_go_config.yaml 不受影响，后续版本继续共用' }],
          tone: 'danger',
          confirmLabel: '卸载',
        })
        if (!accepted) return {}
        await DdnsGoAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          description: '含 ddns-go.exe 的官方下载解压目录即可。域名配置恒在 ~/.ddns_go_config.yaml，与 exe 位置无关',
          label: 'ddns-go 所在目录完整路径',
        })
        if (!path) return {}
        const info = await DdnsGoAPI.ImportLocal(path.trim())
        return { message: `已导入 ddns-go ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await DdnsGoAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await DdnsGoAPI.Start()
          return { message: out.message }
        },
        label: '▶ 启动',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '已运行，无需重复启动' : '后台拉起 ddns-go（不打开面板）'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await DdnsGoAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在其面板或 Windows 服务中退出' : '终止托管实例（含配置写静默期保护）'),
      },
    },

    // 第三钮：内嵌 Web 控制台子窗（control 契约只有 primary/quit 两位，
    // 本动词经 #primary-action 槽由视图渲染，钮序保持"启动/打开控制台/退出"）。
    openConsole: {
      async run(): Promise<ManagedActionResult> {
        const out = await DdnsGoAPI.OpenConsole()
        return { message: out.message }
      },
      label: '🖥 打开控制台',
      cssClass: 'btn-primary',
      // 与原禁用条件逐字一致：五态皆可点，仅快照未载的空态禁用
      disabledFor: (state) =>
        state !== 'running' && state !== 'starting' && state !== 'external' && state !== 'stopped' && state !== 'failed',
      titleFor: () => '打开内嵌 Web 控制台（未运行时先自动启动）',
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s: Snapshot) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 ddns-go 实例（自行启动或 Windows 服务，非 Hanxi 托管）。可直接打开其面板查看；托管启动需先退出外部实例。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'ddns-go 异常退出' }
      }
      if (s.state === 'running') {
        const listenAddr = s.listenAddr ?? ''
        return {
          tone: 'ok',
          text: `ddns-go 正在运行：Web 面板 ${listenAddr ? 'http://' + listenAddr : ''}（仅回环，不外露局域网）。DNS 服务商与域名配置在其页面内完成，保存即生效。`,
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开控制台」启动并在窗口内配置 DNS 服务商与域名；域名配置恒存 ~/.ddns_go_config.yaml，与你自行运行的 ddns-go 无缝共享。'
      }
      if (s.state === 'starting') {
        return '正在拉起 ddns-go 并等待 Web 端口就绪…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（ddns-go_*_windows_x86_64.zip，官方 digest 校验）；或「导入本地」把你机器上已有的解压目录收纳进来',
        '域名配置（~/.ddns_go_config.yaml）各版本共享，升级/切换版本不影响现有解析设置',
      ],
      firstUseEmpty: '尚未安装 ddns-go —— 下载官方 Windows x64 包，或「导入本地」把现有解压目录收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 ddns-go',
    },

    extras: {
      followOnExit: {
        get: () => DdnsGoAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await DdnsGoAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭 ddns-go'
              : '已关闭：Hanxi 退出不影响 ddns-go，继续独立解析',
          }
        },
        note: '（关闭后 Hanxi 退出不影响 ddns-go，继续独立解析）',
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '在资源管理器中定位 ddns-go 配置文件（%USERPROFILE%\\.ddns_go_config.yaml）',
        async open(): Promise<ManagedActionResult> {
          await DdnsGoAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => DdnsGoAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await DdnsGoAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
