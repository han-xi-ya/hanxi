// ============================================================================
// 剪贴板内置模块 · 前端服务门面（视图层访问后端唯一入口）
//
// 纪律：视图层**只经本文件**访问后端，方法名与 Go ClipboardService 冻结面
// 一致（契约 §4/§7）。真实 transport 由 adapters/clipboardBridge 在启动期
// 注入（main.ts，createApp 前）；transport 未注入时调用抛可读错误；
// 测试一律 setClipboardTransport(fake) 注假件。本门面不静态 import
// bindings/（绑定面唯一合法 import 点在桥）。
// 事件订阅不走本文件：直接用既有 useWailsEvent + 下方事件名常量。
// ============================================================================

import type { ClipEntry, ClipPrefs, ClipQueueHotkeyState, ClipStatus } from '../types/clipboard'

/** 冻结方法面：与 internal/modules/clipboard ClipboardService 一一对位。 */
export interface ClipboardTransport {
  List(q: string, kind: string, limit: number): Promise<ClipEntry[]>
  Get(id: string): Promise<ClipEntry>
  Set(id: string): Promise<void>
  CreateText(text: string): Promise<ClipEntry>
  TogglePin(id: string): Promise<ClipEntry>
  Delete(id: string): Promise<void>
  ClearAll(): Promise<void>
  SetPaused(paused: boolean): Promise<void>
  GetStatus(): Promise<ClipStatus>
  /** 契约 §12 v1.4 H1:浮层显式收窗 RPC(blur 唤不动顶层窗 HWND,收窗必走此路)。 */
  CollapseOverlay(): Promise<void>
  /** 契约 §12 v1.7 R-G1:回填剪贴板 + 焦点还给唤出浮层时的原窗 + 模拟 Ctrl+V(仅 text;失败降级为已回填)。 */
  Paste(id: string): Promise<void>
  /** 契约 §12 v1.8:偏好回显（钳制/规范化后的生效值）。 */
  GetPrefs(): Promise<ClipPrefs>
  /** 契约 §12 v1.8:整表校验落盘并热应用；返回回显同 GetPrefs（含钳制值）。 */
  SetPrefs(prefs: ClipPrefs): Promise<ClipPrefs>
  /** v1.8 队列：仅 text 准入、同 id 幂等、上限 50；变化即广播快照。 */
  QueueAdd(id: string): Promise<void>
  /** v1.8 队列：按 id 摘除（条目本体不动；不在队内幂等静默）。 */
  QueueRemove(id: string): Promise<void>
  /** v1.8 队列：清空（不删账）。 */
  QueueClear(): Promise<void>
  /** v1.8 队列：全量 id 快照（数组序=贴出序=徽标序号）。 */
  QueueList(): Promise<string[]>
  /** v1.8 队列：贴出队首到当前前台（不还焦点；sendKeys 失败照常前进）。 */
  QueueStep(): Promise<void>
  /** v1.8 队列热键实况（accel ""=停用；registered 为 Registry 在位实况）。 */
  GetQueueHotkey(): Promise<ClipQueueHotkeyState>
  /** v1.8 改键事务：校验即绑定、败不落盘；空串=停用。 */
  SetQueueHotkey(accel: string): Promise<ClipQueueHotkeyState>
}

/** Wails 事件名常量（契约 §5/§6，前后端逐字对位）。 */
export const CLIP_EV = {
  updated: 'clipboard:updated',
  removed: 'clipboard:removed',
  paused: 'clipboard:paused',
  overlayOpening: 'clipboard:overlay:opening',
  /** v1.8 队列全量快照事件（载荷 {ids}；入队/摘除/清队/消费/联动 prune 五源共用）。 */
  queueChanged: 'clipboard:queue:changed',
} as const

let transport: ClipboardTransport | null = null

/** 注入/摘除后端桥（clipboardBridge 启动注入与测试专用）。 */
export function setClipboardTransport(t: ClipboardTransport | null): void {
  transport = t
}

function need(): ClipboardTransport {
  if (!transport) {
    throw new Error('剪贴板服务尚未接线：等待收口阶段绑定注入，或在测试中 setClipboardTransport(fake)')
  }
  return transport
}

export const clipboardList = (q: string, kind: string, limit = 200) => need().List(q, kind, limit)
export const clipboardGet = (id: string) => need().Get(id)
export const clipboardSet = (id: string) => need().Set(id)
export const clipboardCreateText = (text: string) => need().CreateText(text)
export const clipboardTogglePin = (id: string) => need().TogglePin(id)
export const clipboardDelete = (id: string) => need().Delete(id)
export const clipboardClearAll = () => need().ClearAll()
export const clipboardSetPaused = (paused: boolean) => need().SetPaused(paused)
export const clipboardGetStatus = () => need().GetStatus()
export const clipboardCollapseOverlay = () => need().CollapseOverlay()
export const clipboardPaste = (id: string) => need().Paste(id)
export const clipboardGetPrefs = () => need().GetPrefs()
export const clipboardSetPrefs = (prefs: ClipPrefs) => need().SetPrefs(prefs)
export const clipboardQueueAdd = (id: string) => need().QueueAdd(id)
export const clipboardQueueRemove = (id: string) => need().QueueRemove(id)
export const clipboardQueueClear = () => need().QueueClear()
export const clipboardQueueList = () => need().QueueList()
export const clipboardQueueStep = () => need().QueueStep()
export const clipboardGetQueueHotkey = () => need().GetQueueHotkey()
export const clipboardSetQueueHotkey = (accel: string) => need().SetQueueHotkey(accel)
