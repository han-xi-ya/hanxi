// 特征测试：三个格式化函数的分支行为逐字锁定 MarkerOnView 现实现，
// Phase 4/5 迁移以此为回归底线（含刻意保留的口径怪癖）。
import { describe, expect, it } from 'vitest'
import { fmtDate, fmtDuration, fmtSize } from '../format'

describe('fmtSize', () => {
  it('0/空值统一显示占位符', () => {
    expect(fmtSize(0)).toBe('—')
    expect(fmtSize(null)).toBe('—')
    expect(fmtSize(undefined)).toBe('—')
    expect(fmtSize()).toBe('—')
  })

  it('≤1MB 用整数 KB（四舍五入），恰好 1MB 仍走 KB 分支', () => {
    expect(fmtSize(1)).toBe('0 KB') // 亚 KB 舍入为 0——现实现既有口径
    expect(fmtSize(1023)).toBe('1 KB')
    expect(fmtSize(1024)).toBe('1 KB')
    expect(fmtSize(1536)).toBe('2 KB') // 1.5 KB 进位
    expect(fmtSize(1024 * 1024)).toBe('1024 KB') // `>` 判定不含等号
  })

  it('>1MB 用一位小数 MB', () => {
    expect(fmtSize(1024 * 1024 + 1)).toBe('1.0 MB')
    expect(fmtSize(4 * 1024 * 1024)).toBe('4.0 MB')
    expect(fmtSize(1536 * 1024)).toBe('1.5 MB')
  })
})

describe('fmtDate', () => {
  it('空值 → 占位符', () => {
    expect(fmtDate(null)).toBe('—')
    expect(fmtDate(undefined)).toBe('—')
    expect(fmtDate('')).toBe('—')
  })

  it('取 ISO 串前 10 位日期部分', () => {
    expect(fmtDate('2026-08-02T00:00:00Z')).toBe('2026-08-02')
    expect(fmtDate('2026-08-02')).toBe('2026-08-02')
  })

  it('短于 10 位时原样返回（slice 特性，锁定现状）', () => {
    expect(fmtDate('26-1')).toBe('26-1')
  })
})

describe('fmtDuration', () => {
  it('不足一小时用 mm:ss 并补零', () => {
    expect(fmtDuration(0)).toBe('00:00')
    expect(fmtDuration(9)).toBe('00:09')
    expect(fmtDuration(59)).toBe('00:59')
    expect(fmtDuration(60)).toBe('01:00')
    expect(fmtDuration(3599)).toBe('59:59')
  })

  it('满一小时切 h:mm:ss，小时不补零', () => {
    expect(fmtDuration(3600)).toBe('1:00:00')
    expect(fmtDuration(3661)).toBe('1:01:01')
    expect(fmtDuration(7325)).toBe('2:02:05')
    expect(fmtDuration(360000)).toBe('100:00:00')
  })
})
