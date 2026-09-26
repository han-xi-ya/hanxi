// 历史版本中文名映射账本（N33 批 B，§2 表尾行 + §5 v1 边界）：
// config.json 顶层键 → 中文名（与 internal/settings AppSettings 的 json tag 逐键
// 对齐，约 17 个）；state/<file>.json → 模块中文名（含 §2 实扫例外表）。
// 嵌套对象/数组一律计数化（"网络配置 (3 项)"），不做逐项字段级名（P5-A 裁决）。
// 诚实边界：查不到的键/文件一律原样回落显示，绝不瞎猜兜一个相近中文名。
// memo 文件走后端已注入的标题 resolver（Display），不在此表。
// 批 C 追加：观察窗/份数口径常量为全前端唯一数字源（散落裸数字一律经此插值），
// 计数整句与恢复生效语义徽标也单源在此（清单行与时间线行共用同一判组口径）。

import type { TrackedFile } from '../../bindings/hanxi/internal/snapshot/models'

/**
 * git 版本观察窗上限：与后端 `internal/snapshot` 的 maxListRevisions 同值镜像
 * （ListRevisions/FileHistory 截断、前端拉取 limit、超窗文案插值共用此一处）。
 */
export const REVISION_WINDOW = 50

/** 备份模式滚动保留份数：与后端 backupKeepCount 同值镜像（chip 与说明文案插值用）。 */
export const BACKUP_KEEP = 30

/** 受保清单三分组的展示名（与后端 TrackedFile.Group 词表对齐）。 */
export const groupLabels: Record<string, string> = {
  memo: '便签',
  config: '工作台设置',
  state: '模块状态',
}

/** FileRevision/RevisionFile 变化类型首字母 → 中文（备份模式恒 M 或差集算出）。 */
export const statusLabels: Record<string, string> = {
  M: '修改',
  A: '新增',
  D: '删除',
  R: '改名',
}

// ---------- 恢复生效语义（N33 §0.3-C，批 C 常驻徽标单源） ----------

/** 恢复生效组：热=写回即换装内存，冷=进 pending 链下次启动生效。 */
export type RestoreScope = 'hot' | 'cold'

/**
 * 白名单相对路径 → 恢复生效组（与后端 RestoreFile 分流判据同口径：
 * memo/ 热恢复，config/state 经 StagePendingRestore 重启生效）。
 * 白名单外路径返回 null，调用侧不出徽标（不硬兜一个猜测语义）。
 */
export function restoreScopeFor(path: string): RestoreScope | null {
  if (path.startsWith('memo/')) return 'hot'
  if (path === 'config.json' || path.startsWith('state/')) return 'cold'
  return null
}

/** 生效组 → 徽标文案与全局 chip 色类（色不是唯一通道：文案本就写着生效方式）。 */
export const restoreScopeLabels: Record<RestoreScope, { text: string; chip: string }> = {
  hot: { text: '即时生效', chip: 'chip-positive' },
  cold: { text: '重启生效', chip: 'chip-warning' },
}

// ---------- 计数整句（批 C §5：两口径不硬统一数字，各自说人话） ----------

/**
 * 清单行版本数整句——窗内摊平口径（ListFiles 按观察窗逐版累计，改名文件
 * 与时间线的沿链计数可差 1，故只承诺"窗内变化次数"，不称"全部历史"）。
 */
export function fileCountNote(n: number): string {
  return n > 0 ? `最近 ${n} 次变化` : '窗内暂无变化'
}

/** 清单行计数口径的悬停说明（数字进句、口径进 tooltip，各司其职）。 */
export function fileCountTitle(): string {
  return `统计口径：最近 ${REVISION_WINDOW} 版观察窗内该文件出现变化的次数（改名文件与时间线沿链计数可差 1）`
}

/** 时间线头版本事件数整句——沿链口径（FileHistory --follow 跨改名沿用）。 */
export function historyCountNote(m: number): string {
  return `共 ${m} 条版本事件（跨改名沿用）`
}

/** 观察窗裸数字句式的单源出口（超窗提示/上限 chip 共用，措辞一致）。 */
export function windowCapNote(): string {
  return `仅展示最近 ${REVISION_WINDOW} 版`
}

/**
 * config.json 顶层键 → 中文名。来源：internal/settings/store.go AppSettings
 * 字段注释（2026-09-25 实读，含 N5-C2 触发参数两键）。
 */
export const CONFIG_KEY_LABELS: Record<string, string> = {
  theme: '外观主题',
  accent: '强调色',
  language: '界面语言',
  autoStart: '开机自启',
  minimizeToTray: '关闭时最小化到托盘',
  logRetainDays: '日志保留天数',
  modules: '模块启用状态',
  lanRemarks: '局域网设备备注',
  trayMenu: '托盘菜单',
  quickMenuTwoTier: '快捷菜单二级展开',
  quickMenuHoldMs: '快捷菜单长按阈值',
  quickMenuMovePx: '快捷菜单拖拽阈值',
  historyOcrFullText: '历史收录 OCR 全文',
  wechat: '微信机器人配置',
  wechatAccounts: '微信账号列表',
  webAppEntries: '网页应用条目',
  snapshot: '历史版本偏好',
}

/**
 * state 目录文件名（含相对 config 根的路径尾段）→ 模块中文名。
 * 常规 `<id>.json` 直接命中；下表显式登记 §2 例外清单（不可纯约定推导）。
 */
export const STATE_NAME_LABELS: Record<string, string> = {
  'projects.json': 'frpc 项目配置', // frpc store 落盘名不是模块 ID（§2 例外）
  'wsl-portproxy.json': 'WSL2 端口映射',
  'wsl-install-pref.json': 'WSL2 安装偏好',
  'wsl-usbipd.json': 'WSL2 USB 直通',
  'history.json': '统一历史', // 公共件非模块
  'memo.json': '便签旧库', // 旧库回落脚本，正常不存在（出现时兜底显示）
  'updates.json': '软件版本检查', // updatewatch 缓存
  'bcu.json': 'BC 卸载工具',
  'bili23.json': 'Bili23 Downloader',
  'ccswitch.json': 'CC Switch',
  'ddnsgo.json': 'ddns-go',
  'everything.json': 'Everything 搜索',
  'flclash.json': 'FlClash 代理',
  'guoheview.json': '果核看图',
  'keyviz.json': 'Keyviz',
  'litemonitor.json': 'LiteMonitor',
  'mangodisk.json': 'MangoDisk',
  'markeron.json': 'MarkerOn 标注',
  'msgboard.json': '桌面留言板',
  'ocr.json': '文字识别',
  'papertodo.json': 'PaperTodo',
  'paseo.json': 'Paseo',
  'piclite.json': 'PicLite',
  'quicklook.json': 'QuickLook',
  'rammap.json': 'RAMMap',
  'recordly.json': 'Recordly',
  'rufus.json': 'Rufus',
  'rustdesk.json': 'RustDesk',
  'snipaste.json': 'Snipaste',
  'subnetdesk.json': 'SubnetDesk',
  'termora.json': 'Termora',
  'translucenttb.json': 'TranslucentTB',
  'vscode.json': 'VS Code',
  'windterm.json': 'WindTerm',
}

/** RFC3339 → 本地可读 `MM-DD HH:mm`（快照三件套紧凑列共用；解析失败原样回传）。 */
export function fmtTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** 按路径查 state 中文名（state/xxx.json → 名；查不到 null，调用侧原样回落）。 */
export function stateLabel(path: string): string | null {
  const base = path.split('/').pop() ?? path
  return STATE_NAME_LABELS[base] ?? null
}

/**
 * 单个 config 顶层键的描述：`中文名`；嵌套对象/数组计数化 `中文名 (N 项)`。
 * 查不到的键原样用键名（计数照常给，数字不撒谎）。
 */
export function describeConfigKey(key: string, value: unknown): string {
  const label = CONFIG_KEY_LABELS[key] ?? key
  if (Array.isArray(value)) return `${label} (${value.length} 项)`
  if (value !== null && typeof value === 'object') return `${label} (${Object.keys(value).length} 项)`
  return label
}

/**
 * old/new config JSON 逐顶层键对比（解析失败一侧视为空对象→全算新增）。
 * 已知粗粒度：序列化等值对比对键序敏感（整篇重排会虚报变更），只作 diff
 * 视图的辅助标注行，不作为任何恢复/判定依据。
 */
export function changedConfigKeys(oldJson: string, newJson: string): string[] {
  const o = parseTopLevel(oldJson)
  const n = parseTopLevel(newJson)
  const keys = new Set([...Object.keys(o), ...Object.keys(n)])
  const changed: string[] = []
  for (const k of keys) {
    if (JSON.stringify(o[k]) !== JSON.stringify(n[k])) {
      changed.push(describeConfigKey(k, n[k] ?? o[k]))
    }
  }
  return changed
}

/** 顶层对象键快照：非对象/坏 JSON 回落空对象（调用侧只用于对比呈现）。 */
function parseTopLevel(json: string): Record<string, unknown> {
  if (!json.trim()) return {}
  try {
    const v: unknown = JSON.parse(json)
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) return v as Record<string, unknown>
  } catch {
    /* 截断坏 JSON 常态：视为不可对比 */
  }
  return {}
}

/**
 * 白名单相对路径 → 前端侧中文名（查不到 null，调用侧原样回落）。
 * memo 路径返回 null：标题是账本信息，走后端口注入的 resolver（Display 字段）。
 */
export function labelForPath(path: string): string | null {
  if (path === 'config.json') return '工作台设置'
  if (path.startsWith('state/')) return stateLabel(path)
  return null
}

/** 清单/时间线共用的文件显示名：前端映射表优先，其次后端 Display，最后文件名。 */
export function fileDisplay(f: TrackedFile): string {
  return labelForPath(f.path) ?? f.display ?? baseName(f.path)
}

function baseName(path: string): string {
  return path.split('/').pop() ?? path
}
