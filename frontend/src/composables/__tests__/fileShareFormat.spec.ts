// fileShare 家族专属字节/速率口径的独立锁定测试
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

  it('进位边界与 TB 封顶：已知值对照表（无 PB 档，超界一律折进 TB）', () => {
    expect(formatBytes(1023)).toBe('1023 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1048575)).toBe('1024 KB') // 1 MB 差 1 字节：取整后仍报 KB 档（既有口径怪癖，如实锁定）
    expect(formatBytes(1048576)).toBe('1.0 MB')
    expect(formatBytes(1024 ** 4)).toBe('1.0 TB')
    expect(formatBytes(1024 ** 5)).toBe('1024 TB')
    expect(formatBytes(5 * 1024 ** 5)).toBe('5120 TB')
    expect(formatSpeed(1024 ** 5)).toBe('1024 TB/s')
  })

  it('速率在字节口径后加 /s 后缀', () => {
    expect(formatSpeed(1536)).toBe('1.5 KB/s')
    expect(formatSpeed(0)).toBe('0 B/s')
  })
})
