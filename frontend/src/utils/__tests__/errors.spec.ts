// 特征测试：getErrorMessage 是全库共用的异常归一入口，
// 其输出行为必须保持逐条一致（改动先补此册）。
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
    // 空白字符串与空白 Error.message 同规矩兜底
    // （getErrorMessage('') 走 !err 分支归「未知错误」，两路行为一致）
    expect(getErrorMessage('   ')).toBe('未知错误')
  })

  it('Error 实例取 message', () => {
    expect(getErrorMessage(new Error('出错了'))).toBe('出错了')
  })

  // 契约：空白 message 不原样透传，一律回落「未知错误」——
  // 空 Error 直接上屏会让界面弹出没字的红条。
  it('Error 实例 message 为空/纯空格回落「未知错误」', () => {
    expect(getErrorMessage(new Error())).toBe('未知错误')
    expect(getErrorMessage(new Error('   '))).toBe('未知错误')
  })

  it('对象 message 为空/纯空格同样回落「未知错误」', () => {
    expect(getErrorMessage({ message: '' })).toBe('未知错误')
    expect(getErrorMessage({ message: '  \t ' })).toBe('未知错误')
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
