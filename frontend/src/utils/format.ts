// 托管家族与工具视图共用的标准格式化（重构蓝图 §4：utils/format）。
// Phase 4/5 迁移时替换各视图内联副本（现状 fmtSize×17、fmtDate×15、fmtDuration×15）。
// 各分支行为与 MarkerOnView 现实现严格逐字一致，保证迁移当天可见输出零变化。

/** 字节大小格式化：0/空值 → '—'；>1MB → 一位小数 MB；否则整数 KB（四舍五入）。 */
// N21①（2026-09-24）补 GB/TB 档：原上限 MB 令 16GB 内存显示成 "16384.0 MB"；
// 既有 KB/MB 分支逐字节不动（含"恰好 1MB 走 KB 档"的锁定怪癖），纯高位扩档，
// 26 家消费视图自动受益。
export function fmtSize(bytes?: number | null): string {
  if (!bytes) return '—'
  if (bytes >= 1024 ** 4) return `${(bytes / 1024 ** 4).toFixed(1)} TB`
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
}

/** 日期格式化：空值 → '—'；否则取 ISO 串前 10 位（yyyy-MM-dd）。 */
export function fmtDate(iso?: string | null): string {
  if (!iso) return '—'
  return iso.slice(0, 10)
}

/** 时长格式化：h>0 时 `h:mm:ss`，否则 `mm:ss`（运行时长/耗时共用）。 */
export function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}
