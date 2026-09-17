// 路由清单单一来源（docs/FRONTEND.md §4/§6）：route → 组件 + 后端 moduleId 门禁。
// 职责边界：后端注册表（GetNavs/ext:changed）管"模块存在与启用"，
// 本表只管前端侧"route→组件、哪个 route 需要 EnsureModuleActive"，两者在侧栏渲染处合并。
//
// 视图全部 defineAsyncComponent 异步化：首屏只加载当前路由，点哪个加载哪个；
// KeepAlive(max=10) 缓存已加载实例，二次进入零开销。新增视图必须在此登记，
// 禁止再回到 App.vue 手抄两张表（迁移铁律第 1、9 条）。
import { defineAsyncComponent } from 'vue'
import type { Component } from 'vue'
import type { IconName } from './icons'

/** 双栏外壳的一级分组（左栏目录节点）：六组固定，顺序由 GROUP_META.order 决定。 */
export type NavGroup = 'network' | 'system' | 'desktop' | 'efficiency' | 'media' | 'developer'

export const GROUP_META: Record<NavGroup, { title: string; desc: string; icon: IconName; order: number }> = {
  network:   { title: '网络与传输', desc: '隧道 · 扫描 · 代理 · 远控', icon: 'globe',    order: 1 },
  system:    { title: '系统管理',   desc: '端口 · 清理 · 监控 · 系统盘', icon: 'cpu',   order: 2 },
  desktop:   { title: '桌面增强',   desc: '标注 · 解压 · 音量 · 快捷工具', icon: 'layout', order: 3 },
  efficiency:{ title: '效率办公',   desc: '备忘 · 搜索 · 截图 · 待办',  icon: 'check-square', order: 4 },
  media:     { title: '媒体影音',   desc: '录屏 · 压图 · 下载',        icon: 'film',    order: 5 },
  developer: { title: '开发者工具', desc: '编辑器 · 模型切换 · Agent · WSL', icon: 'code', order: 6 },
}

export interface RouteDef {
  component: Component
  /** 对应后端模块 ID：路由切换前需 EnsureModuleActive 门禁（核心页缺省） */
  moduleId?: string
}

// —— 设置页分区视图（拆分自原单页 SettingsView）：'/settings' 为兼容落地入口
//（托盘固定项 / 通知点击路由 / 快捷菜单直达均引用该串），内容等同「常规偏好」。
// 分区菜单单一来源见 SETTINGS_SECTIONS，二级面板（AppSidebar）据此渲染。
const SettingsGeneral = defineAsyncComponent(() => import('@/views/settings/GeneralSection.vue'))
const SettingsTheme = defineAsyncComponent(() => import('@/views/settings/ThemeSection.vue'))
const SettingsTray = defineAsyncComponent(() => import('@/views/settings/TraySection.vue'))
const SettingsStorage = defineAsyncComponent(() => import('@/views/settings/StorageSection.vue'))
const SettingsSystem = defineAsyncComponent(() => import('@/views/settings/SystemSection.vue'))
const SettingsWorkbench = defineAsyncComponent(() => import('@/views/settings/WorkbenchSection.vue'))

export const ROUTES: Record<string, RouteDef> = {
  '/': { component: defineAsyncComponent(() => import('@/views/HomeView.vue')) },
  '/frpc': { component: defineAsyncComponent(() => import('@/views/FrpcProjectsView.vue')), moduleId: 'frpc' },
  '/ext/fileshare': { component: defineAsyncComponent(() => import('@/views/FileShareView.vue')), moduleId: 'fileshare' },
  '/ext/memo': { component: defineAsyncComponent(() => import('@/views/MemoView.vue')), moduleId: 'memo' },
  '/ext/lan': { component: defineAsyncComponent(() => import('@/views/LanScannerView.vue')), moduleId: 'lan' },
  '/ext/portscan': { component: defineAsyncComponent(() => import('@/views/PortScanView.vue')), moduleId: 'portscan' },
  '/ext/wechat': { component: defineAsyncComponent(() => import('@/views/WechatBotView.vue')), moduleId: 'wechat' },
  '/ext/publicip': { component: defineAsyncComponent(() => import('@/views/PublicIpView.vue')), moduleId: 'publicip' },
  '/ext/portkill': { component: defineAsyncComponent(() => import('@/views/PortKillView.vue')), moduleId: 'portkill' },
  '/ext/wifi': { component: defineAsyncComponent(() => import('@/views/WifiView.vue')), moduleId: 'wifi' },
  '/ext/markeron': { component: defineAsyncComponent(() => import('@/views/MarkerOnView.vue')), moduleId: 'markeron' },
  '/ext/everything': { component: defineAsyncComponent(() => import('@/views/EverythingView.vue')), moduleId: 'everything' },
  '/ext/ccswitch': { component: defineAsyncComponent(() => import('@/views/CCSwitchView.vue')), moduleId: 'ccswitch' },
  '/ext/snipaste': { component: defineAsyncComponent(() => import('@/views/SnipasteView.vue')), moduleId: 'snipaste' },
  '/ext/nanazip': { component: defineAsyncComponent(() => import('@/views/NanaZipView.vue')), moduleId: 'nanazip' },
  '/ext/eartrumpet': { component: defineAsyncComponent(() => import('@/views/EarTrumpetView.vue')), moduleId: 'eartrumpet' },
  '/ext/mangodisk': { component: defineAsyncComponent(() => import('@/views/MangoDiskView.vue')), moduleId: 'mangodisk' },
  '/ext/bcu': { component: defineAsyncComponent(() => import('@/views/BCUView.vue')), moduleId: 'bcu' },
  '/ext/flclash': { component: defineAsyncComponent(() => import('@/views/FlClashView.vue')), moduleId: 'flclash' },
  '/ext/recordly': { component: defineAsyncComponent(() => import('@/views/RecordlyView.vue')), moduleId: 'recordly' },
  '/ext/papertodo': { component: defineAsyncComponent(() => import('@/views/PaperTodoView.vue')), moduleId: 'papertodo' },
  '/ext/piclite': { component: defineAsyncComponent(() => import('@/views/PicLiteView.vue')), moduleId: 'piclite' },
  '/ext/keyviz': { component: defineAsyncComponent(() => import('@/views/KeyvizView.vue')), moduleId: 'keyviz' },
  '/ext/quicklook': { component: defineAsyncComponent(() => import('@/views/QuickLookView.vue')), moduleId: 'quicklook' },
  '/ext/litemonitor': { component: defineAsyncComponent(() => import('@/views/LiteMonitorView.vue')), moduleId: 'litemonitor' },
  '/ext/guoheview': { component: defineAsyncComponent(() => import('@/views/GuoheViewView.vue')), moduleId: 'guoheview' },
  '/ext/ddnsgo': { component: defineAsyncComponent(() => import('@/views/DdnsGoView.vue')), moduleId: 'ddnsgo' },
  '/ext/subnetdesk': { component: defineAsyncComponent(() => import('@/views/SubnetDeskView.vue')), moduleId: 'subnetdesk' },
  '/ext/rustdesk': { component: defineAsyncComponent(() => import('@/views/RustDeskView.vue')), moduleId: 'rustdesk' },
  '/ext/rufus': { component: defineAsyncComponent(() => import('@/views/RufusView.vue')), moduleId: 'rufus' },
  '/ext/bili23': { component: defineAsyncComponent(() => import('@/views/Bili23View.vue')), moduleId: 'bili23' },
  '/ext/vscode': { component: defineAsyncComponent(() => import('@/views/VSCodeView.vue')), moduleId: 'vscode' },
  '/ext/translucenttb': { component: defineAsyncComponent(() => import('@/views/TranslucentTBView.vue')), moduleId: 'translucenttb' },
  '/ext/paseo': { component: defineAsyncComponent(() => import('@/views/PaseoView.vue')), moduleId: 'paseo' },
  '/ext/douzy': { component: defineAsyncComponent(() => import('@/views/DouzyView.vue')), moduleId: 'douzy' },
  '/ext/ocr': { component: defineAsyncComponent(() => import('@/views/OcrView.vue')), moduleId: 'ocr' },
  '/ext/envcheck': { component: defineAsyncComponent(() => import('@/views/EnvCheckView.vue')), moduleId: 'envcheck' },
  '/ext/wsl': { component: defineAsyncComponent(() => import('@/views/WSLView.vue')), moduleId: 'wsl' },
  '/ext/quickmenu': { component: defineAsyncComponent(() => import('@/views/QuickMenuView.vue')), moduleId: 'quickmenu' },
  '/ext/msgboard': { component: defineAsyncComponent(() => import('@/views/MsgBoardView.vue')), moduleId: 'msgboard' },
  '/logs': { component: defineAsyncComponent(() => import('@/views/LogsView.vue')) },
  '/settings': { component: SettingsGeneral },
  '/settings/general': { component: SettingsGeneral },
  '/settings/theme': { component: SettingsTheme },
  '/settings/tray': { component: SettingsTray },
  '/settings/storage': { component: SettingsStorage },
  '/settings/system': { component: SettingsSystem },
  '/settings/workbench': { component: SettingsWorkbench },
  '/about': { component: defineAsyncComponent(() => import('@/views/AboutView.vue')) },
}

/** 设置页分区（二级面板设置态菜单的单一来源）。 */
export interface SettingsSection {
  id: string
  title: string
  desc: string
  icon: IconName
  route: string
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { id: 'general', title: '常规偏好', desc: '启动 · 窗口 · 日志保留', icon: 'sliders', route: '/settings/general' },
  { id: 'theme', title: '外观主题', desc: '浅色 · 深色 · 跟随系统', icon: 'palette', route: '/settings/theme' },
  { id: 'tray', title: '托盘菜单', desc: '右键快捷入口自定义', icon: 'inbox', route: '/settings/tray' },
  { id: 'storage', title: '存储目录', desc: '配置 · 日志 · 版本仓', icon: 'hard-drive', route: '/settings/storage' },
  { id: 'system', title: '系统直达', desc: 'hosts · 组件 · 通知诊断', icon: 'wrench', route: '/settings/system' },
  { id: 'workbench', title: '工作台入口', desc: '运行日志 · 关于', icon: 'file-text', route: '/settings/workbench' },
]

/** route → 设置分区 id；'/settings' 与未注册子段回落 general；非设置路由返回 null。 */
export function settingsSectionOf(route: string): string | null {
  if (route !== '/settings' && !route.startsWith('/settings/')) return null
  const seg = route.slice('/settings/'.length)
  return SETTINGS_SECTIONS.some((s) => s.id === seg) ? seg : 'general'
}

/** 占位视图同样异步（仅在后端注册了前端未建档的路由时才加载）。 */
const extPlaceholder = defineAsyncComponent(() => import('@/views/ExtPlaceholderView.vue'))

export function moduleIdOf(route: string): string | undefined {
  return ROUTES[route]?.moduleId
}

/** 已建档路由的组件；未知 route 返回 undefined，由外壳决定占位或回退。 */
export function routeComponent(route: string): Component | undefined {
  return ROUTES[route]?.component
}

/** 后端注册表里存在、但前端未建档的扩展 route → 占位视图。 */
export function placeholderComponent(): Component {
  return extPlaceholder
}

/** 未知 route 的统一回退：首页。 */
export function fallbackComponent(): Component {
  return ROUTES['/'].component
}

/** 模块 → 一级分组归属（与后端注册表分组口径一致；双栏左栏按此聚簇）。 */
export const MODULE_GROUP: Record<string, NavGroup> = {
  frpc: 'network',
  fileshare: 'network',
  lan: 'network',
  portscan: 'network',
  publicip: 'network',
  wifi: 'network',
  flclash: 'network',
  ddnsgo: 'network',
  subnetdesk: 'network',
  rustdesk: 'network',
  portkill: 'system',
  bcu: 'system',
  litemonitor: 'system',
  rufus: 'system',
  envcheck: 'system',
  markeron: 'desktop',
  nanazip: 'desktop',
  eartrumpet: 'desktop',
  mangodisk: 'desktop',
  translucenttb: 'desktop',
  quickmenu: 'desktop',
  keyviz: 'desktop',
  quicklook: 'desktop',
  guoheview: 'desktop',
  msgboard: 'desktop',
  memo: 'efficiency',
  everything: 'efficiency',
  snipaste: 'efficiency',
  papertodo: 'efficiency',
  wechat: 'efficiency',
  ocr: 'efficiency',
  recordly: 'media',
  piclite: 'media',
  bili23: 'media',
  douzy: 'media',
  ccswitch: 'developer',
  vscode: 'developer',
  paseo: 'developer',
  wsl: 'developer',
}

/** 模块所属分组；未登记模块返回 undefined（由外壳决定归入"其他"或隐藏）。 */
export function groupOfModule(id: string): NavGroup | undefined {
  return MODULE_GROUP[id]
}

/**
 * 模块展示元数据（图标 + 首选路由）：自 HomeView 的 MODULE_META 收编，
 * 终结"三份并行清单"（App.vue 两张表已并入 ROUTES）。后端注册表仍是
 * "模块存在/启用"的真相，本表只供首页卡片渲染；缺省模块走 fallbackIcon。
 * icon 统一 `i:` 前缀引用 constants/icons 注册表（与后端 Nav().Icon 同名），
 * 消费端（HomeView）经 AppIcon 渲染 SVG；无前缀值按文本回退。
 */
export const MODULE_PRESENTATION: Record<string, { icon: string; route: string }> = {
  frpc: { icon: 'i:zap', route: '/frpc' },
  fileshare: { icon: 'i:share-2', route: '/ext/fileshare' },
  memo: { icon: 'i:sticky-note', route: '/ext/memo' },
  lan: { icon: 'i:radar', route: '/ext/lan' },
  portscan: { icon: 'i:search', route: '/ext/portscan' },
  wechat: { icon: 'i:message-circle', route: '/ext/wechat' },
  publicip: { icon: 'i:globe', route: '/ext/publicip' },
  portkill: { icon: 'i:x-octagon', route: '/ext/portkill' },
  wifi: { icon: 'i:wifi', route: '/ext/wifi' },
  markeron: { icon: 'i:pen-line', route: '/ext/markeron' },
  everything: { icon: 'i:search-code', route: '/ext/everything' },
  ccswitch: { icon: 'i:shuffle', route: '/ext/ccswitch' },
  snipaste: { icon: 'i:scissors', route: '/ext/snipaste' },
  nanazip: { icon: 'i:archive', route: '/ext/nanazip' },
  eartrumpet: { icon: 'i:volume-2', route: '/ext/eartrumpet' },
  mangodisk: { icon: 'i:hard-drive', route: '/ext/mangodisk' },
  bcu: { icon: 'i:trash-2', route: '/ext/bcu' },
  flclash: { icon: 'i:shield', route: '/ext/flclash' },
  recordly: { icon: 'i:video', route: '/ext/recordly' },
  papertodo: { icon: 'i:clipboard-list', route: '/ext/papertodo' },
  piclite: { icon: 'i:image-down', route: '/ext/piclite' },
  keyviz: { icon: 'i:keyboard', route: '/ext/keyviz' },
  quicklook: { icon: 'i:eye', route: '/ext/quicklook' },
  litemonitor: { icon: 'i:activity', route: '/ext/litemonitor' },
  guoheview: { icon: 'i:image', route: '/ext/guoheview' },
  ddnsgo: { icon: 'i:link', route: '/ext/ddnsgo' },
  subnetdesk: { icon: 'i:network', route: '/ext/subnetdesk' },
  rustdesk: { icon: 'i:cast', route: '/ext/rustdesk' },
  rufus: { icon: 'i:disc', route: '/ext/rufus' },
  bili23: { icon: 'i:tv', route: '/ext/bili23' },
  vscode: { icon: 'i:code', route: '/ext/vscode' },
  translucenttb: { icon: 'i:layers', route: '/ext/translucenttb' },
  paseo: { icon: 'i:paw', route: '/ext/paseo' },
  douzy: { icon: 'i:film', route: '/ext/douzy' },
  wsl: { icon: 'i:terminal', route: '/ext/wsl' },
  quickmenu: { icon: 'i:mouse-pointer', route: '/ext/quickmenu' },
  msgboard: { icon: 'i:message-circle', route: '/ext/msgboard' },
  envcheck: { icon: 'i:wrench', route: '/ext/envcheck' },
  ocr: { icon: 'i:scan-text', route: '/ext/ocr' },
}

export const FALLBACK_MODULE_ICON = 'i:box'