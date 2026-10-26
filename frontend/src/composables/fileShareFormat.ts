// 局域网文件快传（FileShare）家族专属的字节/速率格式化（自 FileShareView.vue 拆出，
// 供概览卡与传输审计页签共用）。

// 字节格式化: 1536 -> "1.5 KB"。
// 有意保留本地实现而非 utils/format.fmtSize：本家族口径为"0 B 起点 + 千进制对数进位 +
// 三位有效数字 + B/KB/MB/GB/TB 档"；fmtSize 则 0/空值显 '—' 且无裸 B 档——语义不同，
// 属业务差异非胶水副本。
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const v = bytes / Math.pow(1024, i)
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ' + units[i]
}

// 速率格式化: 1.5 KB/s
export function formatSpeed(bps: number): string {
  return formatBytes(bps) + '/s'
}
