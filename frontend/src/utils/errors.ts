/**
 * 安全解析各类未知错误类型为可读字符串
 */
export function getErrorMessage(err: unknown): string {
  if (!err) return '未知错误'
  // 空白消息（空串/纯空格）一律回落「未知错误」——界面禁出现没字的红条；
  // 同规矩覆盖字符串入参、Error.message、对象 message 三形态
  if (typeof err === 'string') return err.trim() || '未知错误'
  if (err instanceof Error) return err.message.trim() || '未知错误'
  if (typeof err === 'object') {
    const obj = err as Record<string, unknown>
    if (typeof obj.message === 'string') {
      return obj.message.trim() || '未知错误'
    }
    try {
      return JSON.stringify(err)
    } catch {
      return String(err)
    }
  }
  return String(err)
}
