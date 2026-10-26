// ============================================================================
// HEU KMS Activator → 托管控制台 adapter（照 litemonitor/ccswitch 模板）
//
// 与 zip 家族 adapter 的两点形态差异（后端事实的如实投影，非前端选择）：
//  1. **解包器是 RAR5 而非 zip**：上游当前发 iso+rar，而 iso 的真实内容在 UDF 侧
//     （go-diskfs 无 udf、系统 bsdtar 也读不到），Hanxi 解的是 rar。可安装集因此
//     比 zip 家族窄——后端只把"有 rar 且 GitHub 挂了官方 digest"的版本列进远程表
//     （内核 Fetch 强制摘要校验，无摘要根本装不了），所以前端无需"看得见点不动"
//     的行禁用逻辑，download 就是真下载。
//  2. **保留发布页直连**：远程表每行「↗ 发布页」走后端 OpenReleasePage（系统
//     浏览器），用途是看该版说明、拿免密的 iso 或取早期不可自动安装的版本，
//     之后用「导入本地」收纳——它是兜底通道而非主路径。
//
// 提权面：manifest requireAdministrator（PE 资源实证），后端 elevateHint 文案
// 统一含"管理员"关键词 → 视图按该词挂 ElevateRestart 一键通道；hint 在
// stopped 态就预告，不让人首撞才知道。
// 事件面：`heukms:instance-state` + `heukms:version-download`（进度键=version）。
// ============================================================================

import * as HeuKmsAPI from '../../bindings/hanxi/internal/modules/heukms/heukmsservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/heukms/instance/models'
import type { HeuVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/heukms/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'
import { bindVersionProgress, downloadOutcome, setActiveResult, uninstalledResult } from './managedCommon'

// 版本区镜像（「打开位置」按运行版本 → 活动版本 → 任一已装的既有优先级解析目标目录）。
let installedMirror: HeuVersionInfo[] = []
let activeMirror = ''
let runningVersion = ''

export function createHeuKmsAdapter(): ManagedModuleAdapter<Snapshot> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  const trackSnap = (s: Snapshot | null | undefined) => {
    runningVersion = s && s.state === 'running' && s.version ? s.version : ''
  }

  return {
    async getStatus() {
      const s = await HeuKmsAPI.GetStatus()
      trackSnap(s)
      return s ?? null
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('heukms:instance-state', (s) => {
        if (!s) return
        trackSnap(s)
        cb(s)
      })
    },

    subscribeProgress: (cb) => bindVersionProgress<DownloadProgress>('heukms:version-download', cb),

    versions: {
      async listInstalled() {
        const list = (await HeuKmsAPI.ListInstalledVersions()) ?? []
        installedMirror = list
        return list
      },
      listReleases: () => HeuKmsAPI.ListReleases(),
      async getActive() {
        const a = (await HeuKmsAPI.GetActiveVersion()) ?? ''
        activeMirror = a
        return a
      },

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await HeuKmsAPI.SetActiveVersion(version)
        activeMirror = ver
        return setActiveResult(ver)
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        return downloadOutcome(await HeuKmsAPI.DownloadVersion(rel.version), rel.version)
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        const accepted = await confirm({
          title: `确定卸载 HEU KMS Activator ${v.version}？`,
          description: '仅删除 Hanxi 托管目录内的该版本主程序，不可恢复；上游自身的数据不受影响。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await HeuKmsAPI.RemoveVersion(v.version)
        return uninstalledResult(v.version)
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '请输入 HEU 主程序所在目录的完整路径',
          description:
            '可填挂载后的光盘镜像盘符（如 F:\\）或解压出来的目录；目录内须有且仅有一枚主程序 HEU_KMS_Activator_vX.Y.Z.exe（请保留上游原始文件名，Hanxi 靠它识别运行实例）。常规安装请直接点版本行的「下载并安装」，本入口用于兜底与收纳已有主程序',
        })
        if (!path) return {}
        const info = await HeuKmsAPI.ImportLocal(path.trim())
        return { message: `已导入 HEU KMS Activator ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await HeuKmsAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await HeuKmsAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running' ? '唤起已开着的 HEU 窗口' : state === 'starting' ? '启动中…' : '启动 HEU KMS Activator 并显示主窗口',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await HeuKmsAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部自行启动的实例请在它自己的窗口里退出' : '投递关窗消息（异常滞留时宽限后由 Job Object 强杀兜底）',
      },
    },

    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 HEU KMS Activator 实例（非 Hanxi 托管，可能是你自己开的）。可尝试唤起它的窗口；如需退出请在其窗口内操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'HEU KMS Activator 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'HEU KMS Activator 正在运行：全部激活操作都在它自己的窗口里完成，Hanxi 只负责启停与版本收纳。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动。该程序 manifest 强制管理员权限——Hanxi 未提权时会直接启动失败，届时本页会出现「以管理员身份重启 Hanxi」的一键入口；另因主程序无代码签名，Windows Defender 可能拦截删除，反复失败请先查安全中心隔离历史。'
      }
      if (s.state === 'starting') {
        return '正在拉起 HEU KMS Activator（若弹出 UAC 请在系统对话框中放行）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '下载并安装走上游 GitHub Releases 的 rar 资产（官方 digest 整包校验 → 纯 Go RAR5 解包 → PE 身份与版本三重闸 → 原子落位）；同版双发的 iso 镜像 Hanxi 不解析，需要它请到发布页自取',
        '主程序文件名随版本漂移（HEU_KMS_Activator_vX.Y.Z.exe）——导入本地时请保留上游原始文件名，Hanxi 靠该名字前缀识别与唤起运行实例',
        '主程序无代码签名且属激活工具，实时防护类安全软件常会删除或隔离它；"突然变成未安装"多半是这个原因，重新安装前请先处理排除项',
        '上游 62.0.0 及更早版本未发布官方摘要，不进可安装列表；确有需要时用「导入本地」收纳你自行取得的 exe',
      ],
      firstUseEmpty: '尚未安装任何版本 —— 在下方「远程可用版本」点「下载并安装」，或把你已有的主程序「导入本地」',
      remoteUnavailable: '无法加载上游版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 HEU KMS Activator',
    },

    extras: {
      followOnExit: {
        get: () => HeuKmsAPI.GetFollowOnExit(),
        note: '（默认关闭：Hanxi 退出不影响该工具；开启后随 Hanxi 一并关闭）',
        async set(next): Promise<ManagedActionResult> {
          await HeuKmsAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      dataDir: {
        label: '📂 打开位置',
        title: '打开该版本主程序所在的托管目录',
        async open(): Promise<ManagedActionResult> {
          const prefer = installedMirror.find((v) => v.version === (runningVersion || activeMirror)) ?? installedMirror[0] ?? null
          if (!prefer) throw new Error('尚未安装任何版本')
          await HeuKmsAPI.OpenDir(prefer.dir)
          return {}
        },
      },
      repo: {
        url: () => HeuKmsAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await HeuKmsAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}

/** 逐版打开上游发布页（看该版说明 / 取免密 iso / 取不可自动安装的旧版），视图 #release-actions 槽消费。 */
export async function openHeuReleasePage(version: string): Promise<void> {
  await HeuKmsAPI.OpenReleasePage(version)
}
