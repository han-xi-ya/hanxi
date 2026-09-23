// netaddrs.ts 网络接口地址呈现排序（N21②）：采集面本就含 IPv4（真机实证），
// 但 Windows 临时 IPv6 隐私扩展动辄十余条，IPv4 被淹没在 join 串尾部——呈现层
// 断点非数据断点。排序口径：IPv4（排除 169.254 APIPA）→ 全局 IPv6（排除 fe80
// 链路本地）→ APIPA → 链路本地；组内保持后端原序（稳定排序）。
const LINK_LOCAL_V6 = 'fe80'
const APIPA_V4 = '169.254'

function addrRank(a: string): number {
  const isV4 = !a.includes(':')
  if (isV4) return a.startsWith(APIPA_V4) ? 2 : 0
  return a.toLowerCase().startsWith(LINK_LOCAL_V6) ? 3 : 1
}

/** 按 rank 升序稳定排序；空/缺省回原样（视图对 null 有 '—' 兜底）。 */
export function sortNetAddresses(addrs?: string[] | null): string[] {
  if (!addrs || addrs.length === 0) return addrs ?? []
  return addrs
    .map((a, i) => ({ a, i }))
    .sort((x, y) => addrRank(x.a) - addrRank(y.a) || x.i - y.i)
    .map((x) => x.a)
}
