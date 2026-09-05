// Phase 6 拆分新增：fileShare 家族专属字节/速率口径的独立锁定
// （原视图口径契约在 FileShareView.spec.ts 中经审计表间接锁定，此处补直接单测）。
import { describe, expect, it } from 'vitest'
import { formatBytes, formatSpeed } from '../fileShareFormat'

describe('fileShareFormat（0 B 起点 + 千进制对数进位 + 三位有效数字口径）', () => {
  it('0 / 负值 / 假值统一归 "0 B"', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
    expect(formatBytes(undefined as unknown as number)).toBe('0 B')
  })

  it('<100 保留一位小数，≥100 取整', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(100 * 1024)).toBe('100 KB')
    expect(formatBytes(5 * 1024 ** 3)).toBe('5.0 GB')
  })

  it('速率在字节口径后加 /s 后缀', () => {
    expect(formatSpeed(1536)).toBe('1.5 KB/s')
    expect(formatSpeed(0)).toBe('0 B/s')
  })
})
