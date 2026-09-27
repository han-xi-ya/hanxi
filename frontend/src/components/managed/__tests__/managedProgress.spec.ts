// 特征测试：波 0 抽取 stepOf（10 份逐字副本）与 versionRowState
// （guoheview/quicklook/rufus 三视图 statusOf 同体）——边界与刻意保留的
// 怪癖逐字锁定：done 档不设边界值直判 100、downloading 档封顶 99、
// 非 downloading 档恒 0、票据先行压过已装判定。
import { describe, expect, it } from 'vitest'
import { stepOf, versionRowState } from '../managedProgress'

describe('stepOf', () => {
  it('done 档恒 100，与 done/total 数值无关', () => {
    expect(stepOf({ stage: 'done', done: 0, total: 0 })).toBe(100)
    expect(stepOf({ stage: 'done', done: 3, total: 7 })).toBe(100)
  })

  it('非 downloading 的进行/终态档一律 0（resolve/verify/extract/install/error/空串）', () => {
    for (const stage of ['resolve', 'verify', 'extract', 'install', 'error', '']) {
      expect(stepOf({ stage, done: 5, total: 10 }), stage).toBe(0)
    }
  })

  it('downloading 档总字节未知（total=0）为 0，不算除法', () => {
    expect(stepOf({ stage: 'downloading', done: 100, total: 0 })).toBe(0)
  })

  it('downloading 档取整为整数百分比', () => {
    expect(stepOf({ stage: 'downloading', done: 1, total: 3 })).toBe(33)
    expect(stepOf({ stage: 'downloading', done: 2, total: 3 })).toBe(67)
    expect(stepOf({ stage: 'downloading', done: 1, total: 8 })).toBe(13) // 12.5 进位（Math.round 口径）
    expect(stepOf({ stage: 'downloading', done: 1, total: 1000 })).toBe(0) // 0.1 舍去
  })

  it('怪癖锁定：downloading 档封顶 99——满额/超额/99.5+ 都到不了 100', () => {
    expect(stepOf({ stage: 'downloading', done: 1000, total: 1000 })).toBe(99)
    expect(stepOf({ stage: 'downloading', done: 2000, total: 1000 })).toBe(99)
    expect(stepOf({ stage: 'downloading', done: 999, total: 1000 })).toBe(99) // 99.9→round 100→min 99
  })
})

describe('versionRowState', () => {
  const installed = [{ version: '1.2.0' }, { version: '1.1.0' }]

  it('无票据：版本串精确匹配判已装，否则 idle（不做 v 前缀/核心互认）', () => {
    expect(versionRowState(undefined, installed, '1.2.0')).toBe('installed')
    expect(versionRowState(null, installed, '1.1.0')).toBe('installed')
    expect(versionRowState(undefined, installed, '1.0.0')).toBe('idle')
    expect(versionRowState(undefined, [], '1.2.0')).toBe('idle')
  })

  it('票据先行：error 档 → error，其余档（含 done 驻留）一律 downloading，即便已装', () => {
    expect(versionRowState({ stage: 'error' }, installed, '1.2.0')).toBe('error')
    expect(versionRowState({ stage: 'error' }, [], '9.9.9')).toBe('error')
    expect(versionRowState({ stage: 'downloading' }, installed, '9.9.9')).toBe('downloading')
    expect(versionRowState({ stage: 'verify' }, installed, '1.2.0')).toBe('downloading')
    expect(versionRowState({ stage: 'done' }, installed, '1.2.0')).toBe('downloading')
  })
})
