// ============================================================================
// RAMMap → 托管控制台 adapter（W4 · N4，机主拍板"托管本体"路线）
//
// 方言注记：
//  - 上游"同址覆盖式最新版"：远程列表恒一条（Last-Modified 日期版），
//    download 入参传版本原样（空串=最新）；
//  - 提权三重契约之③：stopped 引导行与启动钮 title 经 ElevationStatus 预告
//    "需管理员"；未提权启动的 740 指引走状态广播（引擎改写层含"管理员"
//    关键词，ElevateRestart 一键通道自动挂载）；
//  - external 退出 force-free 档（N3 终裁低损类：零状态观察工具，优雅不生效
//    直接强杀不弹确认）；
//  - 无官方摘要：metaHints 如实披露降级三层与"未验官方哈希"语义。
// ============================================================================

import * as RAMMapAPI from '../../bindings/hanxi/internal/modules/rammap/rammapservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/rammap/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/rammap/version/models'
import { computed, ref } from 'vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

export function createRAMMapAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()
  // 宿主提权态（三重契约之③）：装载时拉一次，失败保持 null 不谎报
  const hostElevated = ref<boolean | null>(null)
  const executionLevel = ref<string>('unknown')
  const externalElevated = ref(false)
  const lastOutcome = ref<{ launchMode?: string; elevated?: boolean; managed?: boolean; canQuit?: boolean } | null>(null)

  async function runElevationChoice(): Promise<ManagedActionResult> {
    const accepted = await confirm({
      title: 'RAMMap 需要管理员权限',
      description: '可以只给 RAMMap 管理员权限（一次性启动，Hanxi 不负责自动关闭），也可以在页面中选择重启整个 Hanxi 后托管。',
      confirmLabel: '仅启动 RAMMap',
      cancelLabel: '取消',
      tone: 'warning',
    })
    if (!accepted) return { message: '已取消启动 RAMMap' }
    const out = await RAMMapAPI.OpenWindowElevated()
    lastOutcome.value = out
    externalElevated.value = out.launchMode === 'external-elevated' || (out.external && out.elevated === true)
    return { message: out.message }
  }
  void RAMMapAPI.ElevationStatus()
    .then((st) => {
      if (!st) return
      hostElevated.value = st.hostElevated
      executionLevel.value = st.executionLevel || 'unknown'
    })
    .catch(() => { /* 未激活模块被门拒属预期 */ })

  return {
    getStatus: () => RAMMapAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('rammap:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('rammap:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

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
      listInstalled: () => RAMMapAPI.ListInstalledVersions(),
      listReleases: () => RAMMapAPI.ListReleases(),
      getActive: () => RAMMapAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await RAMMapAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        const res = await RAMMapAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `RAMMap ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        const accepted = await confirm({
          title: `确定卸载 RAMMap ${v.version}？`,
          description: '该版本托管目录将被删除（RAMMap 为无状态观察工具，卸载无数据损失）。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await RAMMapAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '请输入本机 RAMMap 解压目录完整路径（含 RAMMap64.exe）',
          description: '适用于你已自行从官网下载解压的场合；导入后按当前系统架构取载荷',
        })
        if (!path) return {}
        const info = await RAMMapAPI.ImportLocal(path.trim())
        return { message: `已导入 RAMMap（${info.version}）`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await RAMMapAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          if (hostElevated.value === false && executionLevel.value === 'requireAdministrator') {
            return runElevationChoice()
          }
          const out = await RAMMapAPI.OpenWindow()
          lastOutcome.value = out
          externalElevated.value = out.launchMode === 'external-elevated' || (out.external && out.elevated === true)
          return { message: out.message }
        },
        label: '🗔 启动 RAMMap',
        cssClass: 'btn-primary',
        disabledFor: (state) => state === 'starting' || state === 'quitting',
        titleFor: (state) =>
          state === 'running'
            ? '唤起托管实例窗口'
            : state === 'external'
              ? '唤回已有 RAMMap 窗口'
              : state === 'starting'
                ? '启动中…'
                : hostElevated.value === false
                  ? 'RAMMap 要求管理员权限：当前 Hanxi 未提权，启动将要求以管理员身份重启'
                  : '启动 RAMMap（需管理员权限）',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          if (externalElevated.value) {
            return { message: '这是单独提权启动的外部 RAMMap，请在 RAMMap 窗口内关闭' }
          }
          const out = await RAMMapAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => externalElevated.value || (state !== 'running' && state !== 'starting' && state !== 'external' && state !== 'quitting'),
        titleFor: (state) =>
          externalElevated.value
            ? '这是单独提权启动的外部实例，请在 RAMMap 窗口内关闭'
            : state === 'external'
              ? '外部实例按低损档治理：优雅退出优先，不响应时直接结束（观察工具无状态损失）'
              : '关闭 RAMMap 窗口（关窗即退）',
      },
    },

    banner: (s) => {
      if (externalElevated.value) {
        return { tone: 'warn', text: 'RAMMap 已单独以管理员权限启动：这是外部实例，不属于 Hanxi 的托管范围，请在 RAMMap 窗口内关闭。' }
      }
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到你在 Hanxi 之外启动的 RAMMap（观察工具可多实例并看）：「启动 RAMMap」会唤回其窗口，也可再开一个 Hanxi 托管实例。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'RAMMap 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'RAMMap 正在运行：待机列表清空、页表查看等操作都在其自有窗口进行（本模块只负责官方直链安装与启停治理）。' }
      }
      return null
    },

    hint: (s) => {
      if (externalElevated.value) return '外部提权实例不会加入 Hanxi Job，也不会由 Hanxi 自动关闭。'
      if (s.state === 'stopped') {
        return hostElevated.value === false
          ? '尚未运行：RAMMap 要求管理员权限（上游 manifest 强制）——点击启动若被拒，请经页面提示以管理员身份重新启动 Hanxi。'
          : '尚未运行：点击「启动 RAMMap」打开内存占用明细窗口（关窗即退）。'
      }
      if (s.state === 'starting') {
        return '正在拉起 RAMMap…'
      }
      return null
    },

    copy: {
      metaHints: [
        '上游为微软 Sysinternals 官方直链：同址覆盖式最新版（无历史版本可选），日期即版本',
        '微软不发布单工具官方哈希：完整性走降级三层（字节数 + zip CRC 闸门 + 布局自检），自算摘要入账本防漂移',
        'RAMMap 按官方 EULA 免费使用；hanxi 仅代下载官方包，不打包分发二进制',
      ],
      firstUseEmpty: '尚未安装 RAMMap —— 下载微软官方包（自动取最新版），或「导入本地」收纳你已解压的目录',
      remoteUnavailable: '无法探测官方最新版（sysinternals 域名不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 RAMMap',
    },

    extras: {
      followOnExit: {
        get: () => RAMMapAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await RAMMapAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭托管实例'
              : '已关闭：Hanxi 退出不影响 RAMMap（下次启动生效）',
          }
        },
        note: '（你自行打开的 RAMMap 窗口从不受 Hanxi 退出影响）',
      },
      repo: {
        url: () => RAMMapAPI.OfficialSiteURL(),
        async open(): Promise<ManagedActionResult> {
          await RAMMapAPI.OpenOfficialSite()
          return {}
        },
        label: '官方工具页',
      },
    },
  }
}
