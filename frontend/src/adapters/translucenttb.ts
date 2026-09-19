// ============================================================================
// TranslucentTB → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 TranslucentTBView 原编排段（启停/重设/版本/联动/仓库全部 RPC 与
// 文案零变化）。事件面：`translucenttb:instance-state` /
// `translucenttb:version-download`（单键=version）。版本 Tab 与共享
// ManagedVersionPanel 逐字同形，本件也是四件中版本区全量走共享面板的样本。
// 方言注记：
//  - 启动钮禁用条件含「无任何已装版本」——超出 ManagedControlVerb.disabledFor
//    (state) 的纯状态签名，故 adapter 自持 hasInstalled ref（listInstalled 旁路
//    更新：store.load() 恒调本回调，导入/卸载/下载完成等清单变化后自动刷新），
//    disabledFor/titleFor 读该 ref 复刻原视图 openDirTarget 判据；
//  - 「🪄 重设任务栏状态」按批 0 槽位清单走 adapter.reset 形状，钮体留在控制条
//    #primary-action 位自绘（原 DOM 位逐字等价；其执行器不复用 runControl——
//    现状 reset 成功后不刷快照，与 quit/primary 的恒刷语义不同）；
//  - 「🗂 安装目录」钮按 running > active > 任一已装解析目标目录，依赖版本清单，
//    亦走 #primary-action 位，点击复用 versions.openDir（store.runOpenDir，
//    与现状同为无 busy 闩的目录直达）。
// ============================================================================

import { ref } from 'vue'
import * as TBAPI from '../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/translucenttb/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/translucenttb/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

export function createTBAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 已装清单旁路镜像（见文件头方言注记）：驱动启动钮的禁用与 title 分支
  const hasInstalled = ref(false)

  return {
    getStatus: () => TBAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('translucenttb:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('translucenttb:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: async () => {
        const rows = await TBAPI.ListInstalledVersions()
        hasInstalled.value = (rows?.length ?? 0) > 0
        return rows
      },
      listReleases: () => TBAPI.ListReleases(),
      getActive: () => TBAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await TBAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        const res = await TBAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例）。
        // TranslucentTB 的配置 settings.json 就在版本目录内，卸载连配置一起删——如实预告。
        const accepted = await confirm({
          title: `确定卸载 TranslucentTB ${v.version}？`,
          description:
            '该版本隔离目录将被删除，不可恢复。\n注意：你的透明样式配置（settings.json）就在该目录内，会一并删除。如需保留请先备份。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await TBAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案如实说明整套迁移语义
        const path = await prompt({
          title: '导入本地 TranslucentTB',
          description: '提示：配置（settings.json）跟着安装目录走，导入时整套迁入托管目录',
          label: '便携版目录完整路径（含 TranslucentTB.exe 与伴生 DLL）',
        })
        if (!path) return {}
        const info = await TBAPI.ImportLocal(path.trim())
        return { message: `已导入 TranslucentTB ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await TBAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run() {
          const out = await TBAPI.Start()
          return { message: out.message }
        },
        label: '🌫️ 启动',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'running' || state === 'starting' || state === 'external' || !hasInstalled.value,
        titleFor: (state) =>
          state === 'running' || state === 'starting'
            ? '已在运行'
            : state === 'external'
              ? '外部实例已在运行'
              : hasInstalled.value
                ? '启动 TranslucentTB（驻系统托盘）'
                : '尚未安装，请先在「版本管理」下载',
      },
      quit: {
        async run() {
          const out = await TBAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部实例请在 TranslucentTB 托盘菜单退出' : '优雅退出（保存设置后进程退出，任务栏还原）',
      },
    },

    // 契约 reset 槽：清态/复位类动词（任务栏外观异常时重放配置，等价上游托盘
    // 菜单 Reset dynamic state）。成功回执透出后端 message；失败为裸错误串——
    // 与原视图 resetState 词表逐字一致，由视图走槽钮的执行器承载 busy 闩。
    reset: {
      async run() {
        const out = await TBAPI.ResetState()
        return { message: out.message }
      },
    },

    // 条件提示条（三个变体互斥；文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 TranslucentTB 实例（非 Hanxi 托管）。可重设任务栏状态；如需彻底退出请在 TranslucentTB 托盘菜单操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'TranslucentTB 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'TranslucentTB 正在运行：任务栏透明样式在系统托盘图标菜单中设置（首次启动需在欢迎窗口确认许可）。退出进程后任务栏自动还原。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「🌫️ 启动」。样式设置在该程序的系统托盘图标菜单里完成（首次启动会弹欢迎窗口需先确认许可）。便携版仅支持 Windows 11，且依赖系统已装的 WinUI / VCLibs 框架包。'
      }
      if (s.state === 'starting') {
        return '正在拉起 TranslucentTB（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（TranslucentTB-portable-x64.zip，官方 digest 校验）；或「导入本地」把你机器上已有的便携版整套收纳进来',
        '注意：透明样式配置 settings.json 随版本目录走——多版本并存时各版本配置互相独立',
      ],
      firstUseEmpty: '尚未安装 TranslucentTB —— 下载官方便携版，或「导入本地安装」把现有便携版收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 TranslucentTB',
    },

    extras: {
      followOnExit: {
        get: () => TBAPI.GetFollowOnExit(),
        async set(next) {
          await TBAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具（任务栏透明随之消失）'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
        note: '（开启后 Hanxi 退出连带退出该工具，任务栏透明消失）',
      },
      repo: {
        url: () => TBAPI.RepositoryURL(),
        async open() {
          await TBAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
