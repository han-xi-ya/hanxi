// ============================================================================
// 剪贴板内置模块 · 前端服务门面（多 agent 并行 kernel，所有实现 agent 只读）
//
// 纪律：视图层**只经本文件**访问后端，方法名与 Go ClipboardService 冻结面
// 一致（契约 §4/§7）。真实 transport（生成绑定桥）由收口阶段注入；在此之前
// 任何调用抛可读错误——视图测试一律 setClipboardTransport(fake) 注假件，
// 不 import bindings/（尚未生成，静态 import 会炸解析）。
// 事件订阅不走本文件：直接用既有 useWailsEvent + 下方事件名常量。
// ============================================================================

import type { ClipEntry, ClipStatus } from '../types/clipboard'

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
}

/** Wails 事件名常量（契约 §5/§6，前后端逐字对位）。 */
export const CLIP_EV = {
  updated: 'clipboard:updated',
  removed: 'clipboard:removed',
  paused: 'clipboard:paused',
  overlayOpening: 'clipboard:overlay:opening',
} as const

let transport: ClipboardTransport | null = null

/** 注入/摘除后端桥（收口阶段与测试专用）。 */
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
