// ============================================================================
// 托管控制台共享契约（Wave 5 · 批 0）
//
// 定位：纯前端类型层——把 19+ 个托管视图（ccswitch/markeron/ddnsgo/vscode/
// recordly/paseo/papertodo/everything…）五花八门的 service 绑定，投影成
// ManagedConsoleShell 一家组件共同消费的**单一接口面**。不改 Go、不改
// bindings：每个模块在 `src/adapters/<module>.ts` 里写一个工厂函数，把
// `<Module>API.*` 的调用/事件名/进度键/文案包成 adapter 对象注入共享件。
//
// 三条归一化铁律（差异全部在 adapter 内吃掉，共享件零模块知识）：
//
// ① 启停 7 种动词 → `control.primary` / `control.quit`：
//      primary 归一：OpenWindow（ccswitch/vscode/recordly/paseo/snipaste…）、
//                    Start（ddnsgo）、Launch（keyviz/piclite 类）、
//                    StartBackground（everything 后台驻留）、
//                    OpenConsole（ddnsgo 开面板，也可放 #primary-action 槽并列）、
//                    ToggleAnnotate（markeron 六态开关——建议整钮走槽，见 §槽位清单）、
//                    其余业务唤起动词。
//      quit 归一：  Quit / Stop / StopAnnotate / Terminate / Uninstall(强杀族) 等
//                    "让托管实例退出"的动词。
//      按钮的文案/样式类/禁用条件/title 悬停指引都是 state 的纯函数
//      （ManagedControlVerb.label/cssClass/disabledFor/titleFor），
//      共享件按声明渲染，绝不自行推断状态语义。
//      多钮模块（everything 启动后台+打开窗口）：adapter.control.primary 只挂
//      语义上的主钮，其余钮经 ManagedControlBar 的 #primary-action 具名槽注入。
//
// ② 2 种进度键 → NormalizedProgress.key（不透明字符串）：
//      单键模块（绝大多数）：key = t.version，与 release 行按版本对齐。
//      复合键模块：vscode 双形态 key = `${form}:${version}`（两形态同名版本
//      不串台）；everything 的 es 组件票据（component==='es'）不进版本表，
//      给 key='component' 之类的非版本键，面板天然匹配不到行即可隔离。
//      面板用 `versions.progressKey?(rel)` 反推行→键（缺省 rel.version）。
//
// ③ 事件名差异在 adapter 的 subscribe* 里吃掉：
//      实例态统一 `<module>:instance-state`；下载进度多数为
//      `<module>:version-download`，everything 是 `<module>:download`
//      （单事件双分发：es 组件票据自行路由，app 票据转 NormalizedProgress）。
//      subscribe* 必须在组件 setup 同步期内被共享件调用（内部经
//      useWailsEvent 注册、随宿主组件卸载自动注销），不要在工厂函数体里就订阅。
//
// ---------------------------------------------------------------------------
// 槽位清单（批 0 定型、后续批次已按需接线）：
//   adapter.toggle          markeron 六态标注开关（钮体走 #primary-action 槽）
//   adapter.log             ddnsgo/frpc 型进程日志（UI 走 #console-extra 槽）
//   adapter.channel         recordly/paseo 的 Get/SetReleaseChannel
//   adapter.variant         papertodo 的 Get/SetVariant
//   adapter.port            ddnsgo 的 Get/SetListenPort（UI 走 extras 行/槽）
//   adapter.reset           清态/复位类动词（如 Snipaste Dismiss 族）
//   adapter.multiInstance   vscode 双引擎快照（portable/installer 聚合）
//   ManagedConsoleShell 插槽：默认=控制台主体；#primary-action=主操作钮区替换/追加；
//     #console-extra=日志位等控制台内联大件；#danger-extra=强杀等危险动作位。
//   快照业务扩展字段（markeron 的 drawing 等）以**可选字段**并入模块快照类型
//   （S extends ManagedSnapshot），即可在 stateText/banner/hint 投影函数里读。
//
// ---------------------------------------------------------------------------
// 适配模板（以 ccswitch 为例，完整实现见 src/adapters/ccswitch.ts）：
//
//   import * as API from '<bindings>/ccswitchservice'
//   import { useWailsEvent } from '../composables/useWailsEvent'
//
//   export function createCCSwitchAdapter(): ManagedModuleAdapter {
//     return {
//       getStatus: () => API.GetStatus(),
//       subscribeInstanceState: (cb) =>
//         useWailsEvent<Snapshot>('ccswitch:instance-state', (s) => { if (s) cb(s) }),
//       subscribeProgress: (cb) =>
//         useWailsEvent<DownloadProgress>('ccswitch:version-download', (t) => {
//           if (!t || !t.version) return
//           cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
//         }),
//       versions: {
//         listInstalled: API.ListInstalledVersions,
//         listReleases: API.ListReleases,
//         getActive: API.GetActiveVersion,
//         setActive: async (v) => {
//           const ver = await API.SetActiveVersion(v)
//           return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
//         },
//         download: async (rel) => {
//           const res = await API.DownloadVersion(rel.version)
//           if (res === 'already-installed') return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
//           return {}
//         },
//         remove: async (v) => {           // 危险确认在 adapter 内（useConfirm 单例）
//           if (!(await confirm({ title: `确定卸载 CC Switch ${v.version}？`, …, tone: 'danger' }))) return {}
//           await API.RemoveVersion(v.version)
//           return { message: `已卸载 ${v.version}`, reloadVersions: true }
//         },
//         importLocal: async () => { … usePrompt 收编路径输入 … },
//         openDir: (v) => API.OpenDir(v.dir).then(() => ({})),
//       },
//       control: {
//         primary: { run: async () => ({ message: (await API.OpenWindow()).message }), label: '🗔 打开窗口', … },
//         quit:    { run: async () => ({ message: (await API.Quit()).message }), label: '⏻ 退出', … },
//       },
//       banner: (s) => …, hint: (s) => …, copy: { … }, extras: { … },
//     }
//   }
//
// 通用动作回执 toast（"X 已启动"/"已卸载"等**成功**消息）经 ManagedActionResult
// 由共享件统一弹出；**失败**前缀（'退出失败: '/'下载失败: '…）是跨模块逐字同形
// 的固定词，收在 store.ts 的 ACTION_ERROR_PREFIX 单一来源，adapter 只管抛错
// （增强批⑥起可经 copy.errorPrefix 逐动词覆写，「安装失败: 」方言回表）。
//
// ---------------------------------------------------------------------------
// Wave 5 · 共享契约增强批（本文件全部演进的向后兼容总纲）：
//   ① ManagedControlVerb.label 可为 (state, snap) 纯函数（mangodisk 动态主钮）；
//   ② banner/hint 新增第二参版本区投影 ManagedProjection（少收者零改动，
//      方法位双变；见 ManagedProjection 注记）；
//   ③ ManagedVersionRecord<V> 泛型化（方言字段包，paseo/mangodisk 视图去 cast）
//      + 面板 #version-row-extra 具名槽；
//   ④ ManagedCopySpec 面板词面覆写全集（徽标/钮词/下载词族/阶段词/chip 色调…），
//      缺省值 = 现 ccswitch 标准词；
//   ⑤ versions.statusOf/sameVersion/implicitActive/downloadBlock 等效钩子；
//   ⑥ store 动作（runControl/runToggle/runDownload/runChannel/runVariant）
//      统一消费 reloadVersions/activeVersion + errorPrefix 逐动词覆写；
//   ⑦ #danger-extra/#extras-action 槽补作用域 + dangerBusy 联动 + Shell
//      #versions-body/页头徽标/页签计数；
//   ⑧ statusTone 自定义色档 + channel 行收编为 ManagedChannelRow 共享块；
//   ⑨ 轮询停止→uptime 归零入 store；copy.dataDirRow「托管位置」数据行。
// 一切新能力均为可选/联合类型，既有 17 个 adapter 与全部共享件默认行为零变化。
// ============================================================================

import type { Ref } from 'vue'

/** 条件提示条语义（tone 与 UiBanner 四档严格对齐）。 */
export interface ManagedBanner {
  tone: 'ok' | 'info' | 'warn' | 'error'
  text: string
}

/**
 * 方言扩展缺省位（增强批③）：ManagedVersionRecord<V> 的 V 不传时落此型——
 * 键集为空的 Record（等价 `{}`，交叉的单位元），与基型交叉即纯基型，旧消费方零感知。
 * 实现纪律：缺省不写 `Record<string, never>`——其字符串索引会把每个字段压成
 * never，任何带字段的绑定/方言型都赋不进；亦不用条件类型折叠缺省——条件类型
 * 会让 V 在接口比对里测成不变位，方言 adapter/store 便喂不进宽型消费方。
 */
export type ManagedVersionDialect = Record<never, never>

/**
 * 版本区投影上下文（增强批②）：banner/hint 的第二入参，携带 store 单源版本区。
 * 兼容性铁律：banner/hint 保持**方法双参**（snap, ctx）——既有 17+ 个只写
 * `banner: (s) => s.state…` 的 adapter 少收第二参零改动（TS 形参宽度豁免 +
 * 方法位双变），新 adapter 第二参读 installed/releases/active 扩展投影面。
 */
export interface ManagedProjection<V = ManagedVersionDialect> {
  /** 已安装版本列表（store 单源，方言字段经 V 展开）。 */
  installed: ManagedVersionRecord<V>[]
  /** 远程可用版本列表（store 单源）。 */
  releases: ManagedReleaseRecord[]
  /** 设定使用版本（store 单源，空串=未设定）。 */
  active: string
}

/**
 * 托管引擎快照的共享投影面：只收录共享件（控制条四件套 + 运行时长 ticker）
 * 消费的字段。模块快照（instance.Snapshot / vscode 聚合 Status 等）以
 * `S extends ManagedSnapshot` 携带业务扩展（可选字段，如 markeron 的 drawing）。
 * 未识别 state 一律经 toolStateMeta 回退"未运行"——状态不明宁可报无。
 */
export interface ManagedSnapshot {
  /** instance.State 五态词表：stopped / starting / running / failed / external。 */
  state: string
  /** 当前运行/解析版本（未运行为空串）。 */
  version: string
  /** 托管进程 PID（未运行为 0）。 */
  pid: number
  /** 异常退出原因（failed 态由 banner 投影展示）。 */
  error: string
  /** 启动时刻（RFC3339；uptime ticker 输入）。 */
  startedAt: string
}

/**
 * 下载进度归一（见文件头铁律②）：key 为不透明进度键（version / form:version /
 * 'component'…）；stage 沿用后端原始词表 resolve/downloading/verify/extract/
 * (install)/done/error，共享件只特判 downloading/done/error 三档，其余按进行中
 * 附言透出 message——不在前端重命名后端阶段词。
 */
export interface NormalizedProgress {
  key: string
  stage: string
  done: number
  total: number
  message: string
}

/**
 * 业务动词的统一回执：message!==undefined 时共享件弹 toast（undefined=静默，
 * 用户取消/无需播报都回 {}）；reloadVersions=true 时动作完成后重拉版本区。
 * 动词内部失败请**直接抛错**，失败 toast 前缀由共享件统一拼接（单一词源）。
 */
export interface ManagedActionResult {
  message?: string
  reloadVersions?: boolean
  /** setActive 专用：回传实际生效版本，共享件据此更新高亮。 */
  activeVersion?: string
}

/** 已安装版本记录基型（各模块 VersionInfo 结构对齐；单目录模块同样逐条给出）。 */
interface ManagedVersionRecordBase {
  version: string
  exePath: string
  dir: string
  size: number
  installedAt: string
  /** 是否经「导入本地」安装（缺省按官方下载处理）。 */
  isImport?: boolean
  /** 导入来源目录（仅导入安装有值）。 */
  source?: string
}

/**
 * 已安装版本记录（增强批③：泛型化去 cast）。
 * V 为模块方言字段包（如 paseo 的 `{ verifiedHash: boolean }`、mangodisk 的
 * IntegrityState 族）：`ManagedVersionRecord<V>` = 基型 ∩ V，listInstalled
 * 直接回传绑定记录即可（绑定结构 ⊇ V 时逐字段互认）；V 缺省
 * {@link ManagedVersionDialect}（交叉单位元）时即纯基型，旧消费方零感知。
 * **实现纪律：只用纯交叉——条件类型会让 V 测成不变位，方言 adapter/store
 * 便喂不进宽型消费方（ControlBar/Shell 等按缺省实例化）。**
 * 用法：adapter 声明 `ManagedModuleAdapter<Snapshot, { verifiedHash: boolean }>`，
 * store.installed / 面板 `#version-row-extra` 槽位随之带上方言字段，视图直读免断言。
 */
export type ManagedVersionRecord<V = ManagedVersionDialect> = ManagedVersionRecordBase & V

/** 远程可用版本记录（通道/快照等方言列不进通用表，由模块自留表格承接）。 */
export interface ManagedReleaseRecord {
  version: string
  /** 发布时间（RFC3339，面板 fmtDate 截前 10 位）。 */
  published: string
  size: number
  isPre?: boolean
}

/** 启停归一动词（见文件头铁律①）：label/cssClass/disabledFor/titleFor 均为渲染声明。 */
export interface ManagedControlVerb {
  run(): PromiseLike<ManagedActionResult | void>
  /**
   * 钮面完整文案（含图标 emoji），逐字沿用各视图现词。
   * 增强批①：可为状态纯函数（mangodisk 动态主钮词：running/external→「打开窗口」，
   * 其余→「启动 …」）——共享件与自定义视图统一经 store.primaryLabel/quitLabel 取词。
   */
  label: string | ((state: string, snap: ManagedSnapshot | null) => string)
  /** .btn 变体类，缺省 'btn-secondary'（退出钮恒为 btn-danger-outline，不走本字段）。 */
  cssClass?: string
  /** 除共享 busy 闩之外的模块级禁用条件（入参为当前 state 词）。 */
  disabledFor?(state: string): boolean
  /** 悬停指引（状态相关文案）。 */
  titleFor?(state: string): string
}

/** 远程版本行「状态」单元格终态（进度票据先行，两态由已装判定给出）。 */
export type ManagedReleaseRowState = 'installed' | 'idle'

/** 版本管理面：listInstalled/listReleases/download 全模块必备；其余按能力选装。 */
export interface ManagedVersionsSpec<V = ManagedVersionDialect> {
  /**
   * 版本编排归属：shared（缺省）由 store 首拉并管理标准票据；custom 仅保留
   * subscribeProgress→onProgress 事件通道，版本数据与票据生命周期交模块专属 controller。
   */
  orchestration?: 'shared' | 'custom'
  listInstalled(): PromiseLike<Array<ManagedVersionRecord<V>> | null | undefined>
  listReleases(): PromiseLike<ManagedReleaseRecord[] | null | undefined>
  /** 单目录模块（recordly/paseo/papertodo）无设定版本概念：省略即按空串渲染。 */
  getActive?(): PromiseLike<string | null | undefined>
  /** 省略时卡片不渲染「设为使用」钮。 */
  setActive?(version: string): PromiseLike<ManagedActionResult>
  /** 启动后台下载（进度经 subscribeProgress 回流）；'already-installed' 回执弹 toast。 */
  download(release: ManagedReleaseRecord): PromiseLike<ManagedActionResult>
  /** 卸载：危险确认在 adapter 内完成；取消回 {}（无 message 无 reload）。入参宽型即可（只读基型字段）。 */
  remove(info: ManagedVersionRecord): PromiseLike<ManagedActionResult>
  /** 导入本地：usePrompt 输入收编在 adapter 内；取消回 {}。省略则面板不渲染导入钮。 */
  importLocal?(): PromiseLike<ManagedActionResult>
  /** 「打开位置」：仅目录导航，成功静默。入参宽型即可（只读基型字段）。 */
  openDir(info: ManagedVersionRecord): PromiseLike<ManagedActionResult>
  /** 进度键反查（铁律②）：release 行 → NormalizedProgress.key，缺省 rel.version。 */
  progressKey?(release: ManagedReleaseRecord): string
  /**
   * 增强批⑤：版本同一性钩子（recordly 的「核心版本互认」类方言——去预发布
   * 后缀的数值核心对比）。缺省逐字符相等；影响已装卡「运行中」徽标、
   * 远程表已装判定（未声明 statusOf 时的缺省口径）与 set 钮高亮迁移。
   */
  sameVersion?(a: string, b: string): boolean
  /**
   * 增强批⑤：远程行已装状态覆写钩子（面板状态列先行判进度票据后调用；
   * recordly/paseo 的 coreOf 版本互认经此进面板，缺省 =
   * installedList 中存在与 rel.version 按 {@link ManagedVersionsSpec.sameVersion}
   * 同一的记录）。
   */
  statusOf?(rel: ManagedReleaseRecord, installedList: ManagedVersionRecord<V>[], active: string): ManagedReleaseRowState
  /**
   * 增强批⑤：隐式使用版本（「active 为空 = 自动最新已装」的模块，如 paseo）：
   * 返回隐式生效的版本号（'' = 无）。生效时最新已装卡亮「使用中」徽标（词面经
   * copy.activeBadge 覆写，如「使用版本」）且**不显**「设为使用」钮。
   */
  implicitActive?(installedList: ManagedVersionRecord<V>[]): string
  /**
   * 增强批④：下载钮的运行态封锁（recordly NSIS 语义：安装器运行中禁覆写）——
   * 返回非空串即禁用远程表「下载安装」钮并以该串为 title 指引；null=不封锁。
   */
  downloadBlock?(state: string): string | null
}

/** 随关/快捷方式/数据目录/仓库等辅助面条目（全可选，缺项自动隐藏）。 */
export interface ManagedFollowOnExitSpec {
  get(): PromiseLike<boolean>
  /** 失败请抛错（extras 卡统一 toast '设置失败: ' 并回滚勾选）。成功回执放 message。 */
  set(next: boolean): PromiseLike<ManagedActionResult | void>
  /** 勾选行主文案，缺省「随 Hanxi 一起关闭」。 */
  label?: string
  /** 主文案后的 dim 括注（模块差异话术）。 */
  note?: string
}

export interface ManagedShortcutSpec {
  /** 失败请抛错（extras 卡统一 toast '创建快捷方式失败: '）。 */
  create(): PromiseLike<ManagedActionResult | void>
  /** 钮面文案，缺省「🖥 创建桌面快捷方式」。 */
  label?: string
}

export interface ManagedDataDirSpec {
  /** 钮面文案（含图标），如「🗂 数据目录」。 */
  label: string
  title?: string
  /** 失败请抛错（extras 卡统一 toast '打开目录失败: '）。 */
  open(): PromiseLike<ManagedActionResult | void>
}

export interface ManagedRepoSpec {
  url(): PromiseLike<string>
  /** 失败请抛错（extras 卡统一 toast '打开失败: '）。 */
  open(): PromiseLike<ManagedActionResult | void>
  /** 仓库行标签，缺省「GitHub 仓库」（vscode 型官网模块覆写）。 */
  label?: string
  /** 复制成功回执，缺省「仓库地址已复制」。 */
  copyToast?: string
}

/** 联动卡「托管位置」数据行（增强批⑨）：当前托管版本目录直读+直达。 */
export interface ManagedHostDirRowSpec {
  /** 行标签，缺省「托管位置」。 */
  label?: string
  /**
   * 当前托管目录纯投影（store 现态入参；返回空串=不渲染本行）。
   * bili23 现词形：优先运行版本目录，其次 active 版本，最后任一已装。
   */
  dirFor(ctx: { snap: ManagedSnapshot | null; active: string; installed: ManagedVersionRecord[] }): string
  /** 打开该目录（失败请抛错，extras 卡统一 toast '打开目录失败: '）。 */
  open(dir: string): PromiseLike<ManagedActionResult | void>
}

/** 动作失败 toast 前缀覆写位（增强批⑥；键与 store ACTION_ERROR_PREFIX 对齐）。 */
export type ManagedErrorPrefixSpec = Partial<
  Record<'quit' | 'download' | 'setActive' | 'remove' | 'import' | 'openDir' | 'channel' | 'variant', string>
>

/** 版本区/表格里跨模块无法归并的业务文案位（逐字沿用各视图现词）。 */
export interface ManagedCopySpec {
  /** meta-info 区「已安装 N 个版本 · …」后半句（缺省 `远程版本 ${n} 个`）。 */
  remoteSummary?(releaseCount: number): string
  /** meta-info 区的补充 hint 行（便携包来源、绿色版标记等模块差异话术）。 */
  metaHints?: string[]
  /** 首用空态引导段（含模块名）；省略则空态无引导文字。 */
  firstUseEmpty?: string
  /** 远程表拉取失败空行（不可达原因各模块不同）。 */
  remoteUnavailable?: string
  /** 卸载钮禁用 title（如「请先退出 CC Switch」，含模块名）。 */
  uninstallRunningHint?: string

  // ---- 增强批④：面板词面 copy 覆写全集（缺省值 = 现 ccswitch 标准词）----
  /**
   * meta-info 区首句引导（如 paseo「使用版本 自动最新（0.8.0）」、recordly
   * 「当前托管 1.0.0」）：返回空串=不渲染该行；标准「已安装 N 个版本 · …」行照旧。
   */
  metaLead?(ctx: { installed: ManagedVersionRecord[]; active: string; installedCount: number; releasesCount: number }): string
  /** 已装区分节标题（recordly「托管安装」/paseo「托管版本」；缺省「已安装版本」，计数后缀面板自加）。 */
  installedSectionTitle?: string
  /** 远程区分节标题（缺省「远程可用版本」）。 */
  remoteSectionTitle?: string
  /** 使用中徽标词（缺省「使用中」；paseo 覆写「使用版本」，含隐式自动最新卡）。 */
  activeBadge?: string
  /** 「设为使用」钮词（缺省「设为使用」）。 */
  setActiveLabel?: string
  /** 「设为使用」钮悬停说明（如 Paseo：仅影响下次启动）。 */
  setActiveTitle?: string
  /** 导入钮词（缺省「⇥ 导入本地安装」）。 */
  importLabel?: string
  /** 官方徽标词（缺省「官方下载」）。 */
  officialBadge?: string
  /**
   * 下载钮词族（缺省「下载安装」）：ctx 携带行态——recordly 覆写为
   * `installedCount ? '覆盖安装' : '安装'`、paseo 覆写「安装」。
   */
  downloadLabel?(ctx: { release: ManagedReleaseRecord; installedCount: number; state: string }): string
  /** 首用空态一键下载钮词（缺省 `下载最新版 ${version}`；recordly/paseo 带「约 size」后缀）。 */
  firstUseDownloadLabel?(release: ManagedReleaseRecord): string
  /** 状态列进行词（缺省「下载中」；NSIS/zip 语义模块覆写「安装中」）。 */
  downloadingWord?: string
  /**
   * 阶段进度词映射（缺省：verify/extract→「校验解压安装…」，其余进行态透出
   * 票据 message）：返回空串=走 message 兜底（recordly「校验并静默安装…」族）。
   */
  stageWord?(stage: string, progress: NormalizedProgress): string
  /** 远程表已装 chip 色调（缺省 'ghost' 幽灵钮形；'positive' 语义色片形）。 */
  installedChipTone?: 'ghost' | 'positive'
  /** 卸载钮常态 title（非运行中版本卡；缺省无 title）。 */
  uninstallIdleHint?: string

  // ---- 增强批⑥/⑨：失败前缀覆写 + 「托管位置」数据行 ----
  /** 动作失败 toast 前缀覆写（guoheview/recordly/paseo 的「安装失败: 」词表位）。 */
  errorPrefix?: ManagedErrorPrefixSpec
  /** 「托管位置」数据行（联动卡内；缺省不渲染，见 ManagedHostDirRowSpec）。 */
  dataDirRow?: ManagedHostDirRowSpec
}

/**
 * 托管模块适配总面。S 为模块快照扩展（可选字段携带业务态，如 markeron
 * drawing?: boolean）；V 为已装版本记录方言字段包（增强批③，如 paseo 的
 * verifiedHash——store.installed/面板行槽随之免 cast）。共享件按基型
 * ManagedSnapshot 消费，模块 adapter 经方法双变直接传入。扩展槽按需由共享 store、
 * Shell 或模块专属组件消费，语义详见各类型旁注与文件头槽位清单。
 */
export interface ManagedModuleAdapter<S extends ManagedSnapshot = ManagedSnapshot, V = ManagedVersionDialect> {
  /** 当前状态快照（首次拉取 + 轮询共用；失败由共享件静默保留上次快照）。 */
  getStatus(): PromiseLike<S | null>
  /** 订阅实例态事件（铁律③事件名在此吃掉；setup 期内调用）。 */
  subscribeInstanceState(cb: (snap: S) => void): void
  /** 订阅下载进度事件（键归一后回流；setup 期内调用）。 */
  subscribeProgress(cb: (progress: NormalizedProgress) => void): void

  versions: ManagedVersionsSpec<V>
  /** 控制台启停钮区声明；整钮自绘的模块（markeron 六态）可省略 primary 改走槽。 */
  control?: {
    primary?: ManagedControlVerb
    quit?: ManagedControlVerb
  }

  // ---- 状态投影（读快照/版本区的纯函数；不弹副作用）----
  /** 覆写状态词（markeron running+drawing 的"标注已开启"类）；缺省 toolStateMeta。 */
  stateText?(snap: S): string
  /**
   * 增强批⑧：状态灯色档覆写（返回 .status-light 的后缀类词）。缺省 = state 五态；
   * bili23 running+hidden 回 'warn'（琥珀）即走本位——色档词表与
   * ManagedControlBar scoped 样式一一对应（running/starting/external/failed/warn）。
   */
  statusTone?(snap: S): string
  /**
   * 条件提示条（三变体互斥由各模块判定）；null=不渲染。增强批②：新增第二
   * 参版本区投影（少收者零改动）——mangodisk 的「完整性压过运行态」横幅
   * 自此可进 adapter。
   */
  banner?(snap: S, ctx: ManagedProjection<V>): ManagedBanner | null
  /** banner 缺席时的引导行（stopped/starting 态提示）；null=不渲染。入参同 banner。 */
  hint?(snap: S, ctx: ManagedProjection<V>): string | null

  /**
   * 增强批⑦：危险动作（#danger-extra 位）在途闩联动位——声明后共享 busy 闩
   * OR 上本 ref（强制结束进行中，启停/导入钮一并闩住），钮现态改由槽作用域
   * {snap,state,busy,store} 提供（bili23 自绘镜像自此退役）。
   */
  dangerBusy?: Ref<boolean>

  copy?: ManagedCopySpec
  extras?: {
    followOnExit?: ManagedFollowOnExitSpec
    shortcut?: ManagedShortcutSpec
    /** 数据目录直达钮（OpenConfigDir 族），位于 extras-row 按钮组。 */
    dataDir?: ManagedDataDirSpec
    repo?: ManagedRepoSpec
  }

  /** 进度事件旁路观察钩子（如 vscode 在 done 时额外刷双形态快照），先于共享处理调用。 */
  onProgress?(progress: NormalizedProgress): void

  // ---- 业务扩展槽（批 0 预留形状，批 1-5 迁入时定型细节）----
  /** markeron：标注开关矩阵（钮经 #primary-action 槽渲染，此槽供动词与状态注入）。 */
  toggle?: {
    run(): PromiseLike<ManagedActionResult>
    /** 六态按钮文案矩阵输入由模块自算（label/sub/variant/disabled/hint），批 1 定型。 */
  }
  /** ddnsgo/frpc：进程日志位（UI 走 #console-extra 槽；subscribe 需 setup 期调用）。 */
  log?: {
    subscribe(cb: (line: string) => void): void
    /** 进页首屏回补（Logs() RPC 族）。 */
    pull?(): PromiseLike<string[] | null | undefined>
  }
  /**
   * recordly/paseo：发布通道切换。增强批⑧：UI 收编为面板上方共享块
   * （ManagedChannelRow.vue，options/get/set 形状自批 0 定形后不变，视图不再互抄）。
   */
  channel?: {
    options: Array<{ value: string; label: string; /** 该通道选中时的琥珀警示注记（beta 尝鲜话术）。 */ warn?: string }>
    get(): PromiseLike<string>
    set(value: string): PromiseLike<ManagedActionResult>
  }
  /** papertodo：显示形态切换。 */
  variant?: {
    options: Array<{ value: string; label: string }>
    get(): PromiseLike<string>
    set(value: string): PromiseLike<ManagedActionResult>
  }
  /** ddnsgo：Web 监听端口读写。 */
  port?: {
    get(): PromiseLike<number>
    set(port: number): PromiseLike<ManagedActionResult>
  }
  /** 清态/复位动词（dismiss/reset 族；UI 走 #danger-extra 位）。 */
  reset?: {
    run(): PromiseLike<ManagedActionResult>
  }
  /** vscode：双引擎（portable/installer）聚合位——getStatus 返回主形态快照，分形态状态经本槽与自定义槽渲染。 */
  multiInstance?: {
    instances: Array<{ key: string; label: string }>
  }
}
