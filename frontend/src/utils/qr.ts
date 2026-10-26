import QRCode from 'qrcode'

/**
 * 生成二维码 SVG（type=svg、margin=1、纯白底，两个历史调用点逐字一致的公共形制）。
 *
 * 二维码必须保持深墨+纯白硬对比——扫码器不认主题色，dark/light 是功能参数
 * 而非装饰色，刻意不参与 token 化（承 fileshare 原注裁决；Wifi 页现用
 * #1f2328、文件共享页现用 #1e293b，两档墨色历史漂移暂随调用方原样保留，
 * 如需归一属视觉裁决，见 docs/FRONTEND_QUALITY_REVIEW_2026-10-26.md §4-4）。
 */
export function makeQrSvg(text: string, width: number, dark = '#1e293b'): Promise<string> {
  return QRCode.toString(text, {
    type: 'svg',
    margin: 1,
    width,
    color: { dark, light: '#ffffff' },
  })
}
