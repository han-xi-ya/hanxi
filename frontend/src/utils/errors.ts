/**
 * 安全解析各类未知错误类型为可读字符串
 */
export function getErrorMessage(err: unknown): string {
  if (!err) return '未知错误'
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message
  if (typeof err === 'object') {
    const obj = err as Record<string, any>
    if (obj.message && typeof obj.message === 'string') {
      return obj.message
    }
    try {
      return JSON.stringify(err)
    } catch {
      return String(err)
    }
  }
  return String(err)
}
