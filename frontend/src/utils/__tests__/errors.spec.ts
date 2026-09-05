// 特征测试：getErrorMessage 是 30+ 视图共用的异常归一入口，
// 迁移/重构期间其输出行为必须保持逐条一致。
import { describe, expect, it } from 'vitest'
import { getErrorMessage } from '../errors'

describe('getErrorMessage', () => {
  it('假值统一归为「未知错误」', () => {
    expect(getErrorMessage(null)).toBe('未知错误')
    expect(getErrorMessage(undefined)).toBe('未知错误')
    expect(getErrorMessage('')).toBe('未知错误')
    expect(getErrorMessage(0)).toBe('未知错误')
  })

  it('字符串原样返回', () => {
    expect(getErrorMessage('boom')).toBe('boom')
  })

  it('Error 实例取 message', () => {
    expect(getErrorMessage(new Error('出错了'))).toBe('出错了')
  })

  it('带 message 字段的普通对象优先取 message', () => {
    expect(getErrorMessage({ message: '来自后端的错误' })).toBe('来自后端的错误')
  })

  it('无 message 的对象序列化为 JSON', () => {
    expect(getErrorMessage({ code: 500 })).toBe('{"code":500}')
  })

  it('循环引用等序列化失败时回退 String()', () => {
    const circular: Record<string, unknown> = {}
    circular.self = circular
    expect(getErrorMessage(circular)).toBe(String(circular))
  })
})
