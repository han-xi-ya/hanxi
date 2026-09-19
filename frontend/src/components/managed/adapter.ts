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
// 槽位清单（批 0 定型不实现，批 1-5 迁入时填）：
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
// 的固定词，收在 store.ts 的 ACTION_ERROR_PREFIX 单一来源，adapter 只管抛错。
// ============================================================================

/** 条件提示条语义（tone 与 UiBanner 四档严格对齐）。 */
export interface ManagedBanner {
  tone: 'ok' | 'info' | 'warn' | 'error'
  text: string
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

/** 已安装版本记录（各模块 VersionInfo 结构对齐；单目录模块同样逐条给出）。 */
export interface ManagedVersionRecord {
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
  /** 钮面完整文案（含图标 emoji），逐字沿用各视图现词。 */
  label: string
  /** .btn 变体类，缺省 'btn-secondary'（退出钮恒为 btn-danger-outline，不走本字段）。 */
  cssClass?: string
  /** 除共享 busy 闩之外的模块级禁用条件（入参为当前 state 词）。 */
  disabledFor?(state: string): boolean
  /** 悬停指引（状态相关文案）。 */
  titleFor?(state: string): string
}

/** 版本管理面：listInstalled/listReleases/download 全模块必备；其余按能力选装。 */
export interface ManagedVersionsSpec {
  listInstalled(): PromiseLike<ManagedVersionRecord[] | null | undefined>
  listReleases(): PromiseLike<ManagedReleaseRecord[] | null | undefined>
  /** 单目录模块（recordly/paseo/papertodo）无设定版本概念：省略即按空串渲染。 */
  getActive?(): PromiseLike<string | null | undefined>
  /** 省略时卡片不渲染「设为使用」钮。 */
  setActive?(version: string): PromiseLike<ManagedActionResult>
  /** 启动后台下载（进度经 subscribeProgress 回流）；'already-installed' 回执弹 toast。 */
  download(release: ManagedReleaseRecord): PromiseLike<ManagedActionResult>
  /** 卸载：危险确认在 adapter 内完成；取消回 {}（无 message 无 reload）。 */
  remove(info: ManagedVersionRecord): PromiseLike<ManagedActionResult>
  /** 导入本地：usePrompt 输入收编在 adapter 内；取消回 {}。省略则面板不渲染导入钮。 */
  importLocal?(): PromiseLike<ManagedActionResult>
  /** 「打开位置」：仅目录导航，成功静默。 */
  openDir(info: ManagedVersionRecord): PromiseLike<ManagedActionResult>
  /** 进度键反查（铁律②）：release 行 → NormalizedProgress.key，缺省 rel.version。 */
  progressKey?(release: ManagedReleaseRecord): string
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
}

/**
 * 托管模块适配总面。S 为模块快照扩展（可选字段携带业务态，如 markeron
 * drawing?: boolean）；共享件按基型 ManagedSnapshot 消费，模块 adapter 经
 * 方法双变直接传入。扩展槽（toggle/log/channel/variant/port/reset/
 * multiInstance）批 0 只定形状不接线，语义详见各类型旁注与文件头槽位清单。
 */
export interface ManagedModuleAdapter<S extends ManagedSnapshot = ManagedSnapshot> {
  /** 当前状态快照（首次拉取 + 轮询共用；失败由共享件静默保留上次快照）。 */
  getStatus(): PromiseLike<S | null>
  /** 订阅实例态事件（铁律③事件名在此吃掉；setup 期内调用）。 */
  subscribeInstanceState(cb: (snap: S) => void): void
  /** 订阅下载进度事件（键归一后回流；setup 期内调用）。 */
  subscribeProgress(cb: (progress: NormalizedProgress) => void): void

  versions: ManagedVersionsSpec
  /** 控制台启停钮区声明；整钮自绘的模块（markeron 六态）可省略 primary 改走槽。 */
  control?: {
    primary?: ManagedControlVerb
    quit?: ManagedControlVerb
  }

  // ---- 状态投影（读快照的纯函数；不弹副作用）----
  /** 覆写状态词（markeron running+drawing 的"标注已开启"类）；缺省 toolStateMeta。 */
  stateText?(snap: S): string
  /** 条件提示条（三变体互斥由各模块判定）；null=不渲染。 */
  banner?(snap: S): ManagedBanner | null
  /** banner 缺席时的引导行（stopped/starting 态提示）；null=不渲染。 */
  hint?(snap: S): string | null

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
  /** recordly/paseo：发布通道切换。 */
  channel?: {
    options: Array<{ value: string; label: string }>
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
