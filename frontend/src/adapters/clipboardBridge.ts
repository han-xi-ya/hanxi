// ============================================================================
// 剪贴板内置模块 · transport 真桥（wiring-checklist §6.4 / 契约 §7 收口件）
//
// 唯一职责：把 bindings 生成的 ClipboardService 十法逐字映射进 kernel 门面
// （adapters/clipboard.ts 的 setClipboardTransport），视图层零改动即从
// "未接线可读错误"切到真后端。main.ts 副作用注入（createApp 前），工作台
// 主窗与 #clipboardoverlay 浮层窗共用同一份 transport。
//
// 纪律：
//   - 本文件是绑定面唯一合法 import 点（kernel §7"视图绝不静态 import
//     bindings/"的兑现位）；方法名与 service.go 冻结面逐字对位，不做别名。
//   - 不 import 绑定 models（piik adapter 同款"本地结构投影"纪律）：Go 侧
//     nil slice 上线是 null，kernel 契约形是"省略/空数组"，归一化收口在
//     wireEntry/wireStatus 两个函数里，视图永远见不到 null 线形。
// ============================================================================

import * as CLIPAPI from '../../bindings/hanxi/internal/modules/clipboard/clipboardservice'
import type { ClipEntry, ClipPrefs, ClipQueueHotkeyState, ClipStatus } from '../types/clipboard'
import { setClipboardTransport, type ClipboardTransport } from './clipboard'

/** 绑定线上的 Entry/Status 原始形（JSDoc 生成物，仅按弱类型信封接住再投影）。 */
type Wire = Record<string, unknown>

function wireEntry(raw: Wire): ClipEntry {
  return {
    ...raw,
    text: (raw.text as string | null | undefined) ?? undefined,
    files: (raw.files as string[] | null | undefined) ?? undefined,
    autoTags: (raw.autoTags as string[] | null | undefined) ?? undefined,
    blob: (raw.blob as string | null | undefined) ?? undefined,
    blobData: (raw.blobData as string | null | undefined) ?? undefined,
    sourceApp: (raw.sourceApp as string | null | undefined) ?? undefined,
    sourceExe: (raw.sourceExe as string | null | undefined) ?? undefined,
  } as ClipEntry
}

function wireStatus(raw: Wire): ClipStatus {
  return {
    ...raw,
    excludedExes: (raw.excludedExes as string[] | null | undefined) ?? [],
    queueLen: (raw.queueLen as number | null | undefined) ?? 0,
  } as ClipStatus
}

// wirePrefs 线形归一：Go 侧 nil slice/map 上线为 null。MaskKinds 的 null 折叠
// 成 [] 有语义风险（[]=显式全关）——但后端回显恒为非 nil（默认全开或显式
// 保存值，见 prefs.go wirePrefs），null 只可能来自旧服务端形态，折叠可接受。
function wirePrefs(raw: Wire): ClipPrefs {
  return {
    ...raw,
    excludedFactory: (raw.excludedFactory as string[] | null | undefined) ?? [],
    excludedCustom: (raw.excludedCustom as string[] | null | undefined) ?? [],
    appRetentionDays: (raw.appRetentionDays as Record<string, number> | null | undefined) ?? {},
    queueHotkey: (raw.queueHotkey as string | null | undefined) ?? '',
    maskKinds: (raw.maskKinds as string[] | null | undefined) ?? [],
    maskCustomPatterns: (raw.maskCustomPatterns as string[] | null | undefined) ?? [],
  } as ClipPrefs
}

const transport: ClipboardTransport = {
  List: async (q, kind, limit) => {
    const rows = (await CLIPAPI.List(q, kind, limit)) as Wire[] | null
    return (rows ?? []).map(wireEntry)
  },
  Get: async (id) => wireEntry(await CLIPAPI.Get(id)),
  Set: (id) => CLIPAPI.Set(id),
  CreateText: async (text) => wireEntry(await CLIPAPI.CreateText(text)),
  TogglePin: async (id) => wireEntry(await CLIPAPI.TogglePin(id)),
  Delete: (id) => CLIPAPI.Delete(id),
  ClearAll: () => CLIPAPI.ClearAll(),
  SetPaused: (paused) => CLIPAPI.SetPaused(paused),
  GetStatus: async () => wireStatus(await CLIPAPI.GetStatus()),
  CollapseOverlay: () => CLIPAPI.CollapseOverlay(),
  Paste: (id) => CLIPAPI.Paste(id),
  GetPrefs: async () => wirePrefs(await CLIPAPI.GetPrefs()),
  SetPrefs: async (prefs) => wirePrefs(await CLIPAPI.SetPrefs(prefs)),
  QueueAdd: (id) => CLIPAPI.QueueAdd(id),
  QueueRemove: (id) => CLIPAPI.QueueRemove(id),
  QueueClear: () => CLIPAPI.QueueClear(),
  QueueList: async () => ((await CLIPAPI.QueueList()) as string[] | null) ?? [],
  QueueStep: () => CLIPAPI.QueueStep(),
  GetQueueHotkey: async () => (await CLIPAPI.GetQueueHotkey()) as ClipQueueHotkeyState,
  SetQueueHotkey: async (accel) => (await CLIPAPI.SetQueueHotkey(accel)) as ClipQueueHotkeyState,
}

let installed = false

/** 注入真 transport（幂等；测试环境不调用——spec 用 setClipboardTransport(fake)）。 */
export function installClipboardBridge(): void {
  if (installed) return
  installed = true
  setClipboardTransport(transport)
}
