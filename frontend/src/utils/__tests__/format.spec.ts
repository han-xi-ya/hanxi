// 特征测试：三个格式化函数的分支行为逐字锁定 MarkerOnView 现实现，
// Phase 4/5 迁移以此为回归底线（含刻意保留的口径怪癖）。
import { describe, expect, it } from 'vitest'
import { fmtDate, fmtDateTimeSmart, fmtDuration, fmtSize } from '../format'

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

  // N21① 扩档：GB/TB 一位小数；>= 判含等号（恰好 1GB 即 GB 档），
  // MB 档上限口径不变（999.9MB 仍 MB、1023MB 不进位成 GB）。
  it('≥1GB 换 GB、≥1TB 换 TB', () => {
    expect(fmtSize(1024 ** 3)).toBe('1.0 GB')
    expect(fmtSize(16 * 1024 ** 3)).toBe('16.0 GB')
    expect(fmtSize(1536 * 1024 ** 2)).toBe('1.5 GB')
    expect(fmtSize(1024 ** 3 - 1)).toBe('1024.0 MB')
    expect(fmtSize(1024 ** 4)).toBe('1.0 TB')
    expect(fmtSize(2560 * 1024 ** 3)).toBe('2.5 TB')
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

describe('fmtDateTimeSmart（N20b）', () => {
  const now = new Date(2026, 8, 25, 23, 59, 59) // 本地时区 2026-09-25 深夜（固定时钟入参，不随真实当下漂移）
  it('当天只给时分秒；本年补月日；往年退全日期', () => {
    expect(fmtDateTimeSmart('2026-09-25T13:05:09', now)).toBe('13:05:09')
    expect(fmtDateTimeSmart('2026-03-01T08:00:00', now)).toBe('03-01 08:00:00')
    expect(fmtDateTimeSmart('2025-12-31T23:59:59', now)).toBe('2025-12-31 23:59:59')
  })
  it('空值占位；解析失败原样回传不吞机器值', () => {
    expect(fmtDateTimeSmart(null)).toBe('—')
    expect(fmtDateTimeSmart('不是时间')).toBe('不是时间')
  })
})
