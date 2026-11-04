// ============================================================================
// omp（Oh My Pi）→ 官方安装通道 adapter
//
// 2026-11-04 机主拍板拆除自建版本树后，本 adapter 只剩官方路线动作面：
// getStatus（四事实现场探测）+ install/update/uninstallOfficial + 目录与
// 仓库导航。omp 是纯 TUI、模块无进程托管无控制台快照——与 douzy 同因，
// 刻意**不实现** ManagedModuleAdapter 总面，只暴露本动作面。
//
// 纪律照旧：危险确认（卸载）吃掉在 adapter 内，取消回 {} 零 RPC；后端 RPC
// 契约以 bindings/hanxi/internal/modules/omp 生成物为准；事件
// `omp:official-op` 载荷 {op,stage,message} 直传（非进度百分比形，不复用
// bindVersionProgress——那是版本树时代的下载票据形制，随版本树退役）。
// ============================================================================

import * as OmpAPI from '../../bindings/hanxi/internal/modules/omp/ompservice'
import type { OfficialOpProgress } from '../../bindings/hanxi/internal/modules/omp/models'
import { useConfirm } from '../composables/useConfirm'
import { useWailsEvent } from '../composables/useWailsEvent'
import type { ManagedActionResult } from '../components/managed/adapter'

export interface OmpAdapter {
  /** GetStatus 直传（officialDir/officialPresent/officialVersion/onPath/dataDir）。 */
  getStatus(): ReturnType<typeof OmpAPI.GetStatus>
  /** 官方方式一键安装（后台跑 install.ps1 -Binary；进度经 subscribeOfficialOp）。 */
  installOfficial(): Promise<ManagedActionResult>
  /** 官方方式一键更新（再跑一遍脚本拉最新，幂等；亦可当"修复/重装"用）。 */
  updateOfficial(): Promise<ManagedActionResult>
  /** 官方方式一键卸载（危险确认在本件：删 %LOCALAPPDATA%\omp + 剔 PATH；~/.omp 数据根保留）。 */
  uninstallOfficial(): Promise<ManagedActionResult>
  /** 官方装件状态事件订阅（setup 期内调用；事件名 'omp:official-op'）。 */
  subscribeOfficialOp(cb: (progress: OfficialOpProgress) => void): () => void
  /** 目录直达（官方目录/数据根，仅目录语义）。 */
  openDir(dir: string): Promise<void>
  /** 上游仓库地址（页脚展示与复制）。 */
  repositoryURL(): ReturnType<typeof OmpAPI.RepositoryURL>
  /** 浏览器打开上游仓库（成功静默）。 */
  openRepository(): Promise<void>
}

// officialOutcome 官方 RPC 回执词表（"started"/"in-progress" 两态；
// 无 already-installed 档——重复跑正是官方脚本的幂等语义）。
function officialOutcome(rpcReturn: string, startedMessage: string): ManagedActionResult {
  if (rpcReturn === 'in-progress') {
    return { message: '已有一趟官方脚本在跑，请等它结束' }
  }
  return { message: startedMessage, reloadVersions: false }
}

export function createOmpAdapter(): OmpAdapter {
  const { confirm } = useConfirm()

  return {
    getStatus: () => OmpAPI.GetStatus(),

    subscribeOfficialOp: (cb) => useWailsEvent<OfficialOpProgress>('omp:official-op', cb),

    async installOfficial() {
      return officialOutcome(await OmpAPI.InstallOfficial(), '安装已在后台进行，进度见页面')
    },

    async updateOfficial() {
      return officialOutcome(await OmpAPI.UpdateOfficial(), '更新已在后台进行，进度见页面')
    },

    async uninstallOfficial() {
      const accepted = await confirm({
        title: '卸载官方 omp？',
        description:
          '将删除官方安装目录 %LOCALAPPDATA%\\omp 并从用户 PATH 剔除该条目。你的配置 / 会话 / 记忆（在 ~/.omp）不受影响，保留原样。若有终端正在运行 omp，先退出再卸载。',
        tone: 'danger',
      })
      if (!accepted) return {}
      const msg = await OmpAPI.UninstallOfficial()
      return { message: msg || '已卸载', reloadVersions: true }
    },

    async openDir(dir) {
      await OmpAPI.OpenDir(dir)
    },

    repositoryURL: () => OmpAPI.RepositoryURL(),

    async openRepository() {
      await OmpAPI.OpenRepository()
    },
  }
}
