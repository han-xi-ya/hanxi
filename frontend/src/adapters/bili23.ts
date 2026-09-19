// ============================================================================
// Bili23 Downloader → 托管控制台 adapter（Wave 5 · 批 0 契约迁万件）
//
// 模式照抄 ccswitch 黄金样本：Bili23View 原编排段逐字迁移（启停/版本/联动全部
// RPC 与文案零变化）。Bili23 的 bespoke 差异面全部在本 adapter 内吃掉：
//
// ① 退出三态如实回执（本模块的核心语义）：后端 QuitOutcome 按
//    stopped（真退）/ hidden（收入其托盘，进程仍托管）/ asked（弹窗询问中）
//    分叉（另有 external 不越权指引），message 已是逐态差异化的用户话术。
//    control.quit 原样返回 { message: out.message }，经 store.settle 弹 toast
//    上墙——禁止合并成单一"已退出"、禁止吞态；hidden/asked 后界面现态
//    （运行中 · 已收入托盘 / 窗口在前的询问态）由动作后统一 refresh 的
//    statusText/banner 投影补全。布尔分叉字段前端无第二消费方（原视图亦仅
//    透出 message），ManagedActionResult.message 即其等价表达。
//
// ② 「强制结束」（ForceStop）不属于共享启停动词：经 danger 私有槽交给视图，
//    由 ManagedConsoleShell 的 #danger-extra 契约位渲染；强杀回执
//    （stopped/external 两态 message）同样如实上墙。增强批⑦收编：
//    钮现态改由槽作用域 {snap,state,busy,store} 提供、在途闩经 adapter.dangerBusy
//    并进共享 busy（强杀进行中启停/导入钮一并闩住）——视图自绘的现态镜像与
//    并行 busy 自此退役，disabledFor/titleFor 降维为 state 纯函数。
//
// ③ 事件面归一在订阅层完成：`bili23:instance-state` 推的是引擎
//    instance.Snapshot（不带 windowVisible——那是 service 聚合视图字段），
//    故收到迁移信号一律回读 GetStatus 以聚合 Status 为单一状态源（原视图
//    "无类型 handler 只当信号用"的语义在此定型为带类型订阅 + 回读归一）；
//    `bili23:version-download` 进度键 = version（单键，与 ccswitch 同形）。
// ============================================================================

import { ref } from 'vue'
import * as Bili23API from '../../bindings/hanxi/internal/modules/bili23/service'
import type { Status } from '../../bindings/hanxi/internal/modules/bili23/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/bili23/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/bili23/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { toolStateMeta } from '../constants/status'
import type { ManagedActionResult, ManagedModuleAdapter } from '../components/managed/adapter'

/**
 * 危险动作私有槽（#danger-extra 的 bili23 落形；增强批⑦收编版）：
 * ForceStop 族动词不在共享七动词内，钮体由视图自绘；钮现态/在途改由
 * 槽作用域 {snap,state,busy,store} + adapter.dangerBusy 提供（视图不再
 * 自持快照镜像与并行 busy 源），disabledFor/titleFor 降维为 state 纯函数。
 * run() 返回 true=已实际执行（调用方经 scoped store.refresh 回读现态），
 * false=未裁决/在途闩拒入（确认框取消或强杀进行中）。
 */
export interface Bili23DangerSpec {
  /** 钮面完整文案（含图标 emoji），逐字沿用视图原词。 */
  label: string
  run(): Promise<boolean>
  /** 模块级禁用条件：running/starting/external 可点（external 点了也只得到"不强制执行"的回执）。 */
  disabledFor(state: string): boolean
  /** 悬停指引（external 态给管辖边界说明）。 */
  titleFor(state: string): string
}

/** 总适配面：快照以聚合 Status 携带业务扩展（windowVisible），经投影函数消费。 */
export interface Bili23Adapter extends ManagedModuleAdapter<Status> {
  danger: Bili23DangerSpec
}

export function createBili23Adapter(): Bili23Adapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()
  const { showToast } = useToast()

  // ---------- 强杀在途闩（⑦：经 adapter.dangerBusy 并进共享 busy 闩） ----------
  const busy = ref(false)

  async function runForceStop(): Promise<boolean> {
    if (busy.value) return false
    // 确认框也属于危险动作在途期：先置闩，防用户在确认未裁决时并发 Quit/Open。
    busy.value = true
    try {
      // 危险操作经全局确认框（useConfirm 单例）：下载器强杀必须让用户知道代价
      // （在途下载中断，可靠续传兜底），文案逐字保留
      const accepted = await confirm({
        title: '强制结束 Bili23？',
        description:
          '立即终止进程，跳过优雅收尾：在途下载将中断（下次启动可断点续传），等待落盘的任务状态可能回退。\n建议优先使用「退出」或到 Bili23 窗口/托盘内正常退出。',
        tone: 'danger',
      })
      if (!accepted) return false
      const out = await Bili23API.ForceStop()
      // 强杀回执如实上墙：stopped（已终结）/ external（不越权执行）两态原样透出
      showToast(out.message)
      return true
    } catch (e) {
      showToast(`强制结束失败: ${getErrorMessage(e)}`)
      return false
    } finally {
      busy.value = false
    }
  }

  const adapter: Bili23Adapter = {
    getStatus: () => Bili23API.GetStatus(),

    // ⑦：强杀进行中并入共享 busy 闩（启停/导入钮一并禁用）
    dangerBusy: busy,

    subscribeInstanceState: (cb) => {
      // 归一铁律③：payload 带类型声明为引擎快照、只当迁移信号用——
      // 回读聚合 Status（含 windowVisible）后再交付共享件，单一状态源。
      useWailsEvent<Snapshot>('bili23:instance-state', (s) => {
        if (!s) return
        Bili23API.GetStatus().then((fresh) => {
          if (fresh) cb(fresh)
        }).catch((e) => console.warn('bili23 instance-state refetch failed:', getErrorMessage(e)))
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('bili23:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => Bili23API.ListInstalledVersions(),
      listReleases: () => Bili23API.ListReleases(),
      getActive: () => Bili23API.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await Bili23API.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        const res = await Bili23API.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 Bili23 ${v.version}？`,
          description: '该版本隔离目录（含内置 Python 运行时）将被删除，不可恢复。\n（你的下载任务与配置存于 %APPDATA%\\Bili23 Downloader，不受影响，后续版本继续共用）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await Bili23API.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地 Bili23 Downloader',
          description: '提示：安装版（Program Files）或手动解压的便携版均可；整目录复制约 108MB，导入前请先退出正在运行的 Bili23',
          label: '安装目录完整路径（含 Bili23.exe 的目录，或其外层目录）',
        })
        if (!path) return {}
        const info = await Bili23API.ImportLocal(path.trim())
        return { message: `已导入 Bili23 ${info.version}`, reloadVersions: true }
      },

      async openDir(v): Promise<ManagedActionResult> {
        await Bili23API.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await Bili23API.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口（含从托盘唤回）' : state === 'starting' ? '启动中…' : '启动 Bili23 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          // 三态回执核心位：QuitOutcome.message 已按 stopped/hidden/asked
          // （及 external 指引）逐态分化，原样透出、不吞态不分叉加工。
          const out = await Bili23API.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting',
        titleFor: () => '请求优雅退出：结果取决于 Bili23 的「关闭窗口」设置（退出/托盘/询问），不会静默强杀',
      },
    },

    danger: {
      label: '⛔ 强制结束',
      run: runForceStop,
      // ⑦：钮现态入参降维为 state（槽作用域直给）；external 可点但只得到"不强制执行"回执
      disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
      titleFor: (state) => (state === 'external' ? '外部实例不在 Hanxi 管辖范围' : '立即终止进程（在途下载中断，可靠续传兜底）'),
    },

    // 状态词覆写：运行中但窗口收入托盘时如实报"已收入托盘"（措辞先行）
    stateText: (s) => {
      if (s.state === 'running' && !s.windowVisible) return '运行中 · 已收入托盘'
      return toolStateMeta(s.state).text
    },

    // ⑧色档覆写：running+hidden 状态灯转琥珀（warn）——五态灯位语义不吞，仅灯色如实
    statusTone: (s) => (s.state === 'running' && !s.windowVisible ? 'warn' : s.state),

    // 条件提示条（四个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 Bili23 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 Bili23 窗口或其托盘操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Bili23 异常退出' }
      }
      if (s.state === 'running' && !s.windowVisible) {
        return { tone: 'warn', text: 'Bili23 窗口已隐藏（收入其自身托盘），进程仍在运行、下载不中断。「打开窗口」可唤回。' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'Bili23 正在运行：解析与下载任务在其窗口内操作（配置与任务库存于 %APPDATA%\\Bili23 Downloader，跨版本共享）。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 Bili23，解析与下载任务在其窗口内完成。登录态、任务库与配置存于用户目录，与托管版本切换无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Bili23（Qt 首帧约 2~5 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（windows_x64_portable.zip，官方 digest 校验）；或「导入本地安装」把你机器上已有的安装版/便携版整目录收纳进来',
      ],
      firstUseEmpty: '尚未安装 Bili23 —— 下载官方便携版（约 43MB），或「导入本地安装」把现有 Bili23 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 Bili23',

      // ⑨：「托管位置」数据行回契约（迁移批被删的联动卡行）——优先运行版本目录、
      // 其次 active 版本、最后任一已装；打开走 OpenDir 族（与「打开位置」同源）。
      dataDirRow: {
        dirFor: (ctx) => {
          const running = ctx.snap?.state === 'running' && ctx.snap.version ? ctx.snap.version : ctx.active
          return (ctx.installed.find((v) => v.version === running) ?? ctx.installed[0])?.dir ?? ''
        },
        async open(dir: string): Promise<ManagedActionResult> {
          await Bili23API.OpenDir(dir)
          return {}
        },
      },
    },

    extras: {
      followOnExit: {
        get: () => Bili23API.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await Bili23API.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await Bili23API.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 Bili23 的用户数据目录（配置 / 任务库 / 日志）',
        async open(): Promise<ManagedActionResult> {
          await Bili23API.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => Bili23API.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await Bili23API.OpenRepository()
          return {}
        },
      },
    },
  }

  return adapter
}
