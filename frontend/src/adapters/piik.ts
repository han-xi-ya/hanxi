// ============================================================================
// Piik → 托管控制台 adapter（五路并行 · piik 前端线 C，服务型骨架首个消费件）
//
// 上游定位（PLAN_PIIK_HOSTING ①/②裁决）：TNTcraftHIM/Piik，headless 屏幕分享
// 服务——无自有窗口、无托盘，操作界面在用户系统浏览器（http://127.0.0.1:<port>/，
// 默认 8787 被占即上移）。Hanxi 只剩「版本 + 起停 + 状态灯 + 打开浏览器」四件事。
//
// 动词面（与 internal/modules/piik/service.go 冻结面对位）：
//   Start→control.primary（起服务=在局域网敞开分享端口，须机主明示，**不代开浏览器**
//   ——上游自己尽力自动拉浏览器，失败打 GATE_NO_BROWSER，前端据 noBrowser 给兜底）；
//   OpenWindow→openUI 扩展动词（「打开界面」，负裁决：绝不冷启动，stopped 态
//   后端如实拒绝；钮位经视图 #primary-action 槽渲染，ddnsgo 第三钮同族形制）；
//   QuitAdvisory→退出确认框预告（话术单点收口后端，gonavi 同纪律——优雅停=
//   当场断播这句话必须说在点确认之前）；Quit→control.quit。
// 事件面：`piik:instance-state`（载荷 A 线裸 Snapshot，携 GateView 机读字段但
//   **无** B 线账目位）/ `piik:version-download`（单键=version）。裸事件按
//   dbx「lastLedger 回填」纪律补账，防界面链接/漂移警示在事件轮次闪失。
// metaHints：**透传后端六条**（service.go MetaHints() RPC，风险登记表④逐条
//   对位的账目话术单点收口在后端——前端零自造，取回经 ref+getter 供面板响应式
//   消费，端口/使用版本变动后重取一次）。
//
// 类型注记：绑定面（piikservice + models 生成物）随 A/B 线合入后统一
// regenerate（总闸批次），此前 vue-tsc 报缺符号属预期。本文件不 import 绑定
// models，PiikStatus 为**冻结契约的本地结构投影**（JSON 字段名与
// internal/modules/piik/models.go 的 tag 逐字对齐；口令结构性不落前端——
// gateViewFromSnapshot 只可能给出 passwordSet 布尔，值无处可取）。
//
// 语义纪律（档案①#7/#11）：启动失败文案直取后端原话（恒点名端口与
// portkill 处置指引）；本页任何措辞不得出现 WebView2/窗口模板话术。
// ============================================================================

import * as PIKAPI from '../../bindings/hanxi/internal/modules/piik/piikservice'
import { ref } from 'vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import {
  soleVersionUninstallNote,
  type ManagedActionResult,
  type ManagedControlVerb,
  type ManagedModuleAdapter,
  type ManagedSnapshot,
  type ManagedVersionRecord,
} from '../components/managed/adapter'

/**
 * GetStatus 组合快照（models.go PiikStatus 的本地投影）：A 线 Snapshot 基础面
 * 展平 + 端口账 + GateView 机读投影 + 数据目录三账 + 漂移复查。
 * instance-state 事件只带 Snapshot（含 GateView 字段），consoleUrl/listenPort/
 * dataDir 族/drifted 族属 B 线账目位——事件轮次经 lastLedger 回填（见工厂内注记）。
 */
export interface PiikStatus extends ManagedSnapshot {
  /** 界面访问端口（8787 起被占时由分配器上移；stopped/external 给候选首位）。 */
  listenPort: number
  /** 界面地址（http://127.0.0.1:<port>/；「打开界面」钮与 GATE_NO_BROWSER 兜底链接共用）。 */
  consoleUrl: string
  /** 本地访问是否开放（免口令可用）。 */
  localAccessOpen: boolean
  /** 访问口令**有无**布尔——口令值结构性不落本快照，前端也只渲染此布尔。 */
  passwordSet: boolean
  /** LAN 邀请链接（上游自拼含本机内网 IP，原样转呈；空=未开放）。 */
  lanInvitation: string
  /** 公网邀请链接（cloudflared Cloudflare 临时隧道，Web UI 侧发起；空=隧道未建立）。 */
  publicInvitation: string
  /** 上游自动拉浏览器失败（stdout GATE_NO_BROWSER）：前端给手动打开兜底。 */
  noBrowser: boolean
  /** 托管数据目录（配置 client.json 与日志所在；跨版本共享，删版本不删数据）。 */
  dataDir: string
  /** 配置文件路径（CLI --config 改道落点）。 */
  configPath: string
  /** 日志目录（CLI --log-dir 改道落点）。 */
  logDir: string
  /** 使用版本主 exe 发生账外漂移（上游无自更新器，漂移多属手工换件/磁盘异常）。 */
  drifted: boolean
  /** 漂移复查明细（空=未复查）。 */
  driftNote: string
}

/** `piik:version-download` 事件载荷（家族单键=version 形态）。 */
interface PiikProgress {
  version: string
  stage: string
  done: number
  total: number
  message: string
}

/** metaHints 账目位（B 线账目事实，裸 instance-state 事件缺省时回填）。 */
interface PiikLedger {
  listenPort: number
  consoleUrl: string
  dataDir: string
  configPath: string
  logDir: string
  drifted: boolean
  driftNote: string
}

/**
 * Piik 适配面：ManagedModuleAdapter 全家 + openUI 扩展动词
 * （control 契约只有 primary/quit 双位，「打开界面」经视图 #primary-action
 * 槽渲染——ddnsgo openConsole 同族形制）。
 */
export interface PiikAdapter extends ManagedModuleAdapter<PiikStatus> {
  openUI: ManagedControlVerb
}

export function createPiikAdapter(): PiikAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 账目事实（consoleUrl/端口/三账目录/漂移）只随 GetStatus 组合快照回带；
  // 引擎惯例的 instance-state 事件可能只发 A 线裸 Snapshot——事件现值缺账目位
  // 时沿用最近轮询事实，防界面链接与漂移警示在事件轮次闪失（后端事件若携全量
  // 字段则以现值为准；dbx lastLedger 同纪律）。
  let lastLedger: PiikLedger = {
    listenPort: 0,
    consoleUrl: '',
    dataDir: '',
    configPath: '',
    logDir: '',
    drifted: false,
    driftNote: '',
  }

  // metaHints 透传后端六条（service.go MetaHints()——日更钉版 / 0.0.0.0 暴露 /
  // 公网隧道 / 隧道不代管 / 子进程归属 / 数据留存）：话术单点在后端，前端零
  // 自造。ref+getter 使面板渲染即订阅（首读懒拉）；端口或使用版本变动的动词
  // （start/quit/remove/import/setActive）成功后置 null 触发重取。
  const metaHintsRef = ref<string[] | null>(null)
  let hintsInFlight = false
  let hintsStale = false
  function ensureMetaHints() {
    if (hintsInFlight) return
    hintsInFlight = true
    Promise.resolve()
      .then(() => PIKAPI.MetaHints())
      .then((h) => {
        if (Array.isArray(h)) metaHintsRef.value = h
      })
      .catch(() => {
        // 拉取失败保持旧账/空账：披露位缺席不装样子，最终以后端为准
      })
      .finally(() => {
        hintsInFlight = false
        // 合流不吞新账：在途期间来过动词 refresh（改端口/改钉版）则收口后
        // 补拉一次，杜绝拿动作前的旧账当动作后的披露。
        if (hintsStale) {
          hintsStale = false
          ensureMetaHints()
        }
      })
  }
  // 动词后的重取只由 refreshMetaHints 触发合流标记——getter 的懒拉读
  // （面板每轮渲染都可能碰到）不误标 stale。
  const refreshMetaHints = () => {
    metaHintsRef.value = null
    if (hintsInFlight) hintsStale = true
    else ensureMetaHints()
  }

  return {
    async getStatus() {
      const s = await PIKAPI.GetStatus()
      if (s && s.consoleUrl !== undefined) {
        lastLedger = {
          listenPort: s.listenPort ?? 0,
          consoleUrl: s.consoleUrl ?? '',
          dataDir: s.dataDir ?? '',
          configPath: s.configPath ?? '',
          logDir: s.logDir ?? '',
          drifted: s.drifted ?? false,
          driftNote: s.driftNote ?? '',
        }
      }
      return s
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<PiikStatus>('piik:instance-state', (s) => {
        if (!s) return
        // consoleUrl 缺位 = 引擎裸快照：账目位沿用最近轮询事实，机读字段
        // （lanInvitation 等）以事件现值为准。
        cb(s.consoleUrl === undefined ? { ...s, ...lastLedger } : s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<PiikProgress>('piik:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => PIKAPI.ListInstalledVersions(),
      listReleases: () => PIKAPI.ListReleases(),
      getActive: () => PIKAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await PIKAPI.SetActiveVersion(version)
        refreshMetaHints() // 第 1 条含"当前使用版本由您设定"账
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel): Promise<ManagedActionResult> {
        // 无 confirm 闸（服务型骨架无安装器通道）：新版本写独立目录。
        const res = await PIKAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        if (res === 'in-progress') {
          return { message: `版本 ${rel.version} 已在下载中` }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 末版放行清账口径 + 数据留存如实明示（档案⑤步 7：只删版本隔离目录，
        // 不谎称"已清除全部数据"——本模块不提供删数据通道）。
        const soleNote = await soleVersionUninstallNote(() => PIKAPI.ListInstalledVersions())
        const accepted = await confirm({
          title: `确定卸载 Piik ${v.version}？`,
          description:
            '该版本隔离目录将被删除，不可恢复。\n（配置 client.json 与日志统一留存 Hanxi 数据根，卸载任何托管版本都不删数据——本模块不提供删数据通道）' + soleNote,
          tone: 'danger',
        })
        if (!accepted) return {}
        await PIKAPI.RemoveVersion(v.version)
        refreshMetaHints() // 使用版本可能被后端清空，第 1 条要跟着改口
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '导入本地 Piik',
          description: '提示：配置与日志由托管改道统一留存 Hanxi 数据根，与安装位置无关',
          label: '便携目录完整路径（目录内含 piik-app.exe 与 runtime 兄弟项）',
        })
        if (!path) return {}
        const info = await PIKAPI.ImportLocal(path.trim())
        refreshMetaHints()
        return { message: `已导入 Piik ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await PIKAPI.OpenDir(v.dir)
        return {}
      },
    },

    // 六态词表；running 话术恒含「界面在浏览器」——服务型骨架最重要的事实，
    // 杜绝用户等一个不存在的主窗口（WebView2/窗口模板措辞全页禁绝，档案①#11）。
    stateText: (s) =>
      ({
        stopped: '本会话未托管',
        starting: '正在启动',
        running: '托管实例运行中（界面在浏览器）',
        quitting: '正在退出',
        failed: '实例操作失败',
        external: '外部实例运行中',
      })[s.state] ?? '本会话未托管',

    // 色档：漂移属完整性事实压过运行态（DBX 同款克制——只报告不处置）；
    // failed 恒先；quitting 走 starting 琥珀脉冲。
    statusTone: (s) => {
      if (s.state === 'failed') return 'failed'
      if (s.drifted) return 'warn'
      return s.state === 'quitting' ? 'starting' : s.state
    },

    banner: (s) => {
      if (s.state === 'failed') {
        // 后端失败文案恒点名端口与「端口查杀」处置指引，直取原话不重写。
        return { tone: 'error', text: s.error || 'Piik 进程异常退出' }
      }
      if (s.drifted) {
        const who = s.version || '当前使用版本'
        return {
          tone: 'warn',
          text: `${who} 的主程序 exe 摘要与下载账目不符（账外漂移）——piik 上游无自更新器，多为手工换件/磁盘异常，hanxi 只警示、不自动处置${s.driftNote ? `；检测附言：${s.driftNote}` : ''}。如需对齐账目，删除该版本后重新下载即可。`,
        }
      }
      if (s.state === 'external') {
        return { tone: 'warn', text: `检测到外部 piik 实例（自行启动，通常占着 ${s.listenPort || 8787}）：可直接「打开界面」访问它；如需退出请在启动它的那一侧操作（Hanxi 不越权接管或终止外部进程）。` }
      }
      if (s.state === 'running') {
        if (s.noBrowser) {
          // GATE_NO_BROWSER 兜底（档案①#8）：别让用户面对"启动了但什么都没发生"。
          return { tone: 'warn', text: `Piik 正在运行，但上游自动拉起浏览器未成功：点「打开界面」或手动访问 ${s.consoleUrl || '界面地址'}。` }
        }
        return {
          tone: 'ok',
          text: `Piik 正在运行：无自有窗口，界面在系统浏览器${s.consoleUrl ? `（${s.consoleUrl}）` : ''}；开播/邀请/观众入会都在页面内进行，本机邀请链接见下方「邀请与链接」。`,
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点「启动」拉起 piik 服务（机读模式不自动弹浏览器），就绪后点「打开界面」在系统浏览器进入控制台。注意：起服务即在局域网敞开分享端口，是否启动由您明示。'
      }
      if (s.state === 'starting') {
        return '正在拉起 piik 并等待界面端口就绪…'
      }
      if (s.state === 'quitting') {
        return '正在经上游官方优雅通道退出（宽限后 JobObject 兜底，子进程树一并回收）…'
      }
      return null
    },

    copy: {
      // getter 形：面板渲染读 ref → 订阅其变化；值本体是后端 MetaHints() RPC
      // 的六条账目话术，前端零自造（透传）。
      get metaHints(): string[] {
        if (metaHintsRef.value === null) ensureMetaHints()
        return metaHintsRef.value ?? []
      },
      firstUseEmpty: '尚未安装 Piik —— 下载官方便携版（裸 zip，GitHub 官方 sha256 唯一信任根），或「导入本地安装」把你机器上已有的 Piik 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达、镜像回退链亦未成功）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 Piik',
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await PIKAPI.Start()
          refreshMetaHints() // 第 2 条含现场分配端口账
          return { message: out.message }
        },
        label: '▶ 启动',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting' || state === 'quitting',
        titleFor: (state) =>
          state === 'running'
            ? '已在运行：再次点击幂等直返界面地址'
            : state === 'external'
              ? '外部实例在场时托管启动不执行（不与之抢端口、不越权接管）'
              : state === 'starting'
                ? '启动中…'
                : '托管启动 piik 服务（起服务即在局域网敞开分享端口，需您明示；不代开浏览器，上游自动拉页失败时有「打开界面」兜底）',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          // 退出预告话术单点收口后端 QuitAdvisory（gonavi 同纪律）：优雅停=
          // 当场断播，这句话必须说在确认框里，不能等点完退出才让浏览器变黑。
          const advisory = await PIKAPI.QuitAdvisory()
          const accepted = await confirm({
            title: '退出 Piik 托管实例',
            description:
              advisory || '退出经上游官方优雅通道（stdin）+ 宽限，超时由 JobObject 兜底强杀并连带回收子进程树。',
            tone: 'warning',
          })
          if (!accepted) return {}
          const out = await PIKAPI.Quit()
          refreshMetaHints() // 第 2 条端口账随退出改口
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'quitting' && state !== 'external',
        titleFor: (state) =>
          state === 'external'
            ? '外部实例不归 Hanxi 管辖：确认后也只回指引、不越权终止（请在启动它的那一侧退出）'
            : '优雅停 = 正在观看的观众当场断播、公网隧道随之失效（确认框内有后端预告全文）',
      },
    },

    // 「打开界面」：control 双位之外的扩展动词，视图经 #primary-action 槽渲染
    // （ddnsgo openConsole 同族形制）。负裁决对位：后端绝不冷启动，stopped/
    // failed 态本钮如实禁用。
    openUI: {
      async run(): Promise<ManagedActionResult> {
        const out = await PIKAPI.OpenWindow()
        return { message: out.message }
      },
      label: '🌐 打开界面',
      cssClass: 'btn-primary',
      disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
      titleFor: (state) =>
        state === 'running'
          ? '用系统浏览器打开 piik 界面（Piik 无自有窗口，界面恒在浏览器）'
          : state === 'external'
            ? '拨测候选端口后在浏览器打开外部实例界面'
            : state === 'starting'
              ? '启动临界区，请稍候'
              : '服务未运行时无界面可开：起服务=在局域网敞开分享端口，须点「启动」明示',
    },

    extras: {
      followOnExit: {
        get: () => PIKAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await PIKAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时连带终止 piik 与其子进程树（cloudflared/piik-capture 同灭，在途分享即断；下次启动生效）'
              : '已关闭：Hanxi 退出后 piik 整棵进程树原地驻留继续开播（下次启动生效）',
          }
        },
        note: '（仅管 Hanxi 托管实例；外部自行启动的实例不受影响）',
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 Hanxi 数据根下的 piik 托管数据目录（配置 client.json 与日志所在；跨版本共享，卸载版本不删数据；从未托管启动过时目录尚不存在，将如实报错）',
        async open(): Promise<ManagedActionResult> {
          await PIKAPI.OpenDataDir()
          return {}
        },
      },
      repo: {
        url: () => PIKAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await PIKAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
