// ============================================================================
// omp（Oh My Pi）→ 版本管理 adapter
//
// omp 是纯 TUI 编码代理：本模块**只做版本管理**（远程列表 / 下载校验 /
// 激活切换 / 导入本地 exe），不托管进程——无状态灯、无启停、无唤窗，
// 运行由用户在自己的终端启动激活版本 exe。故本 adapter 刻意**不实现**
// ManagedModuleAdapter 总面（那是托管控制台共享件的契约，getStatus 快照
// 五态 / control 启停位在此无处安放），只暴露版本管理动作面，视图
// （OmpView.vue）按 DouzyView 自绘形制接线——douzy 同族"仅版本管理"先例，
// 差异在 omp 多了激活切换与本地导入两个动作（照 airsnip adapter 的
// 危险确认/输入收编纪律，全部吃掉在 adapter 内）。
//
// RPC 与事件契约以 bindings/hanxi/internal/modules/omp 生成物为准
// （ompservice.js + version/models.js + 根 models.js）：
// 事件 `omp:version-download` 进度键 = version（单键形，归一化铁律②），
// 接线走 managedCommon.bindVersionProgress；阶段词表实册 downloading/verify/
// extract/done/error（无 install 档）；setActive / download / 删除
// 三动词的回执词面同样单源收编（setActiveResult / downloadOutcome /
// uninstalledResult），本件不自造方言词。
//
// 形状注记：OmpService.SetActiveVersion / DeleteVersion 返回 void（与
// airsnip SetActiveVersion 回读新 active 串不同），激活回执由**入参版本串**
// 构造；视图卡片高亮吃 ListInstalledVersions 逐条 active 布尔（后端含
// "store 空时回退最新已装"的标注口径），GetStatus.activeVersion 仅供
// 页头"显式设定"展示、dataDir 与激活同源拉取。
// ============================================================================

import * as OmpAPI from '../../bindings/hanxi/internal/modules/omp/ompservice'
import type { OmpVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/omp/version/models'
import type { OfficialOpProgress } from '../../bindings/hanxi/internal/modules/omp/models'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useWailsEvent } from '../composables/useWailsEvent'
import type { ManagedActionResult, NormalizedProgress } from '../components/managed/adapter'
import { bindVersionProgress, downloadOutcome, setActiveResult, uninstalledResult } from './managedCommon'

// officialOutcome 官方 RPC 回执词表（"started"/"in-progress" 两态，
// 与 DownloadVersion 的三态刻意不同——官方脚本全局互斥一趟，无 already-installed 档：
// 重复跑正是"更新"语义，不该被拒）。
function officialOutcome(rpcReturn: string, startedMessage: string): ManagedActionResult {
  if (rpcReturn === 'in-progress') {
    return { message: '已有一趟官方脚本在跑，请等它结束' }
  }
  return { message: startedMessage, reloadVersions: false }
}

/**
 * omp 版本管理动作面（仅 OmpView 消费；命名与 ManagedVersionsSpec 动词位
 * 对齐——download/remove/importLocal/setActive/openDir——将来若并入共享
 * 面板编排可平滑升格）。
 */
export interface OmpVersionAdapter {
  listReleases(): ReturnType<typeof OmpAPI.ListReleases>
  listInstalled(): ReturnType<typeof OmpAPI.ListInstalledVersions>
  /** GetStatus 直传（activeVersion/exePath/dataDir/officialDir/officialPresent/officialVersion/onPath；视图 active 通道、页脚目录行与官方安装区同源本件）。 */
  getStatus(): ReturnType<typeof OmpAPI.GetStatus>
  /** 官方方式一键安装（后台跑 install.ps1 -Binary；进度经 subscribeOfficialOp）。 */
  installOfficial(): Promise<ManagedActionResult>
  /** 官方方式一键更新（再跑一遍脚本拉最新，幂等）。 */
  updateOfficial(): Promise<ManagedActionResult>
  /** 官方方式一键卸载（危险确认在本件：删 %LOCALAPPDATA%\omp + 剔 PATH 条目；~/.omp 数据根保留）。 */
  uninstallOfficial(): Promise<ManagedActionResult>
  /** 官方装件状态事件订阅（setup 期内调用；事件名 'omp:official-op'）。 */
  subscribeOfficialOp(cb: (progress: OfficialOpProgress) => void): () => void
  /** 下载进度订阅（setup 期内调用；事件名 'omp:version-download' 在此吃掉）。 */
  subscribeProgress(cb: (progress: NormalizedProgress) => void): void
  /** 下载指定版本（后台起跑，进度经事件回流）；'already-installed' 回执带重拉标记。 */
  download(version: string): Promise<ManagedActionResult>
  /** 设为激活版本（回执含 activeVersion + 标准词 toast message）。 */
  setActive(version: string): Promise<ManagedActionResult>
  /** 删除已装版本（危险确认在本件；取消回 {} 零 RPC）。 */
  remove(version: string): Promise<ManagedActionResult>
  /** 导入本地 omp.exe（usePrompt 收编路径输入；取消/空白回 {} 零 RPC）。 */
  importLocal(): Promise<ManagedActionResult>
  /** 目录直达（参数为绝对路径，仅目录语义；成功静默）。 */
  openDir(dir: string): Promise<void>
  /** 上游仓库地址（页脚信息行展示，DouzyView 同位）。 */
  repositoryURL(): ReturnType<typeof OmpAPI.RepositoryURL>
  /** 浏览器打开上游仓库（成功静默）。 */
  openRepository(): Promise<void>
}

export function createOmpAdapter(): OmpVersionAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    listReleases: () => OmpAPI.ListReleases(),
    listInstalled: () => OmpAPI.ListInstalledVersions(),
    getStatus: () => OmpAPI.GetStatus(),

    subscribeProgress: (cb) => bindVersionProgress<DownloadProgress>('omp:version-download', cb),

    async download(version) {
      return downloadOutcome(await OmpAPI.DownloadVersion(version), version)
    },

    async setActive(version) {
      // RPC 返回 void：回执按入参版本构造（词面单源 managedCommon，
      // 共享词「已将 X 设为使用版本」）。
      await OmpAPI.SetActiveVersion(version)
      return setActiveResult(version)
    },

    async remove(version) {
      const accepted = await confirm({
        title: `确定删除 omp ${version}？`,
        description:
          '该版本隔离目录（含 omp.exe）将被删除，不可恢复。若删的是当前激活版本，激活会清空并自动回退到最新已装版本；正在终端运行的该版本 exe 会占用文件，需先退出对应终端会话。',
        tone: 'danger',
      })
      if (!accepted) return {}
      await OmpAPI.DeleteVersion(version)
      return uninstalledResult(version)
    },

    async importLocal() {
      const path = await prompt({
        title: '导入本地 omp exe',
        label: 'omp.exe 完整路径或所在目录',
        description:
          '把本机已有的 omp 可执行文件收纳进托管版本区（如官方安装脚本落位的 %LOCALAPPDATA%\\omp\\omp.exe；校验字节数与 sha256 后登记，原文件不受影响）。',
      })
      // 先 trim 再判空：只敲空格/误粘空白串按「取消」处理（airsnip 同款闸），
      // 否则空路径进后端只会换来一句看不懂的路径错误。
      const trimmed = path?.trim() ?? ''
      if (!trimmed) return {}
      const info: OmpVersionInfo = await OmpAPI.ImportLocalVersion(trimmed)
      return { message: `已导入 omp ${info.version}`, reloadVersions: true }
    },

    subscribeOfficialOp: (cb) => useWailsEvent<OfficialOpProgress>('omp:official-op', cb),

    async installOfficial() {
      const out = await OmpAPI.InstallOfficial()
      return officialOutcome(out, '安装已在后台进行，进度见下方')
    },

    async updateOfficial() {
      const out = await OmpAPI.UpdateOfficial()
      return officialOutcome(out, '更新已在后台进行，进度见下方')
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
