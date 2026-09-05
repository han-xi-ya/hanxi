// 路由清单单一来源（docs/FRONTEND.md §4/§6）：route → 组件 + 后端 moduleId 门禁。
// 职责边界：后端注册表（GetNavs/ext:changed）管"模块存在与启用"，
// 本表只管前端侧"route→组件、哪个 route 需要 EnsureModuleActive"，两者在侧栏渲染处合并。
//
// 视图全部 defineAsyncComponent 异步化：首屏只加载当前路由，点哪个加载哪个；
// KeepAlive(max=10) 缓存已加载实例，二次进入零开销。新增视图必须在此登记，
// 禁止再回到 App.vue 手抄两张表（迁移铁律第 1、9 条）。
import { defineAsyncComponent } from 'vue'
import type { Component } from 'vue'

export interface RouteDef {
  component: Component
  /** 对应后端模块 ID：路由切换前需 EnsureModuleActive 门禁（核心页缺省） */
  moduleId?: string
}

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
  '/ext/envcheck': { component: defineAsyncComponent(() => import('@/views/EnvCheckView.vue')), moduleId: 'envcheck' },
  '/logs': { component: defineAsyncComponent(() => import('@/views/LogsView.vue')) },
  '/settings': { component: defineAsyncComponent(() => import('@/views/SettingsView.vue')) },
  '/about': { component: defineAsyncComponent(() => import('@/views/AboutView.vue')) },
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

/**
 * 模块展示元数据（图标 + 首选路由）：自 HomeView 的 MODULE_META 收编，
 * 终结"三份并行清单"（App.vue 两张表已并入 ROUTES）。后端注册表仍是
 * "模块存在/启用"的真相，本表只供首页卡片渲染；缺省模块走 fallbackIcon。
 */
export const MODULE_PRESENTATION: Record<string, { icon: string; route: string }> = {
  frpc: { icon: '⚡', route: '/frpc' },
  fileshare: { icon: '📁', route: '/ext/fileshare' },
  memo: { icon: '📝', route: '/ext/memo' },
  lan: { icon: '◉', route: '/ext/lan' },
  portscan: { icon: '🔍', route: '/ext/portscan' },
  wechat: { icon: '💬', route: '/ext/wechat' },
  publicip: { icon: '≋', route: '/ext/publicip' },
  portkill: { icon: '✕', route: '/ext/portkill' },
  wifi: { icon: '📶', route: '/ext/wifi' },
  markeron: { icon: '✎', route: '/ext/markeron' },
  everything: { icon: '🔎', route: '/ext/everything' },
  ccswitch: { icon: '🔀', route: '/ext/ccswitch' },
  snipaste: { icon: '✂', route: '/ext/snipaste' },
  nanazip: { icon: 'NZ', route: '/ext/nanazip' },
  eartrumpet: { icon: '🔊', route: '/ext/eartrumpet' },
  mangodisk: { icon: '🥭', route: '/ext/mangodisk' },
  bcu: { icon: '🧹', route: '/ext/bcu' },
  flclash: { icon: '⚡', route: '/ext/flclash' },
  recordly: { icon: '🎬', route: '/ext/recordly' },
  papertodo: { icon: '📄', route: '/ext/papertodo' },
  piclite: { icon: '🖼️', route: '/ext/piclite' },
  keyviz: { icon: '⌨️', route: '/ext/keyviz' },
  quicklook: { icon: '👁️', route: '/ext/quicklook' },
  litemonitor: { icon: '📊', route: '/ext/litemonitor' },
  guoheview: { icon: '🏞️', route: '/ext/guoheview' },
  ddnsgo: { icon: '🌐', route: '/ext/ddnsgo' },
  subnetdesk: { icon: '🖥', route: '/ext/subnetdesk' },
  rustdesk: { icon: '🌍', route: '/ext/rustdesk' },
}

export const FALLBACK_MODULE_ICON = '📦'
