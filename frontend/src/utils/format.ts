// 托管家族与工具视图共用的标准格式化（单一正源）：各分支行为与原视图实现
// 严格逐字一致（含刻意保留的怪癖，见各函数注），视图不再持有内联副本。

/** 字节大小格式化：0/空值 → '—'；≥TB/≥GB → 一位小数高档；>1MB → 一位小数 MB；
 *  否则整数 KB（四舍五入）。 */
// GB/TB 档为纯高位扩档（原上限 MB 令 16GB 内存显示成 "16384.0 MB"），判档含等号；
// 既有 KB/MB 分支逐字节不动——含"恰好 1MB 走 KB 档"（> 不含等号）的锁定怪癖。
export function fmtSize(bytes?: number | null): string {
  if (!bytes) return '—'
  if (bytes >= 1024 ** 4) return `${(bytes / 1024 ** 4).toFixed(1)} TB`
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
}

/**
 * 字节大小 IEC 二进制档：0/空值 → '0 B'；≥1GiB 两位小数 GiB；≥1MiB/≥1KiB 一位小数
 * MiB/KiB；其余裸 B（无 TiB 档，封顶 GiB）。与上方 fmtSize 刻意分叉——本档服务磁盘
 * 占用/存储统计类展示，'0 B' 实词比占位符 '—' 更如实。口径原样承自
 * EnvCheckView 与 StorageSection 曾持的内联副本（两份逐字符全同，现已改吃本函数）；
 * 扩 TiB 档须连同消费视图一起核。
 */
export function fmtSizeIec(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${bytes} B`
}

/** 日期格式化：空值 → '—'；否则取 ISO 串前 10 位（yyyy-MM-dd）。 */
export function fmtDate(iso?: string | null): string {
  if (!iso) return '—'
  return iso.slice(0, 10)
}

/**
 * 智能日期时间：同一天的多条记录必须靠时分秒区分（"同图重识两遍两行完全一样"病灶），
 * 跨范围逐级带日期——今天只给时间、本年补月日、往年退全日期。
 * 解析失败原样回传（不吞机器值）。
 */
export function fmtDateTimeSmart(iso?: string | null, now: Date = new Date()): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  const hm = `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
  if (d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) return hm
  if (d.getFullYear() === now.getFullYear()) return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${hm}`
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${hm}`
}

/** 时长格式化：h>0 时 `h:mm:ss`，否则 `mm:ss`（运行时长/耗时共用）。 */
export function fmtDuration(sec: number): string {
  const total = Math.floor(sec) // 入参可能含小数（毫秒换算），先取整再分档
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}
