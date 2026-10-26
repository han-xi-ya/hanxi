// 托管控制台共享契约纯函数特征测试：
// needsElevationRestart（740 提权直拒判据，前后端隐式契约词「管理员」）与
// releaseFormWord（N13 形态 chip 统一词表 + 缺档回落）——以代码实况为契约，
// 锁各边界入参的返回值，不复述实现分支。
import { describe, expect, it } from 'vitest'
import type { ManagedSnapshot } from '../adapter'
import { needsElevationRestart, releaseFormWord } from '../adapter'

const snap = (patch: Partial<ManagedSnapshot>): ManagedSnapshot => ({
  state: 'failed',
  version: '',
  pid: 0,
  error: '',
  startedAt: '',
  ...patch,
})

describe('needsElevationRestart（740 提权判据）', () => {
  it('failed 且 error 含「管理员」（句首/句中任意位置）判真', () => {
    expect(
      needsElevationRestart(
        snap({ state: 'failed', error: '启动失败: 管理员策略阻止（ERROR_ELEVATION_REQUIRED 740）' }),
      ),
    ).toBe(true)
    expect(needsElevationRestart(snap({ state: 'failed', error: '需要管理员权限' }))).toBe(true)
    expect(needsElevationRestart(snap({ state: 'failed', error: '管理员' }))).toBe(true)
  })

  it('非 failed 态即便 error 携带「管理员」字样也判假（状态门先于词门）', () => {
    for (const state of ['stopped', 'running', 'starting', 'external', 'quitting']) {
      expect(needsElevationRestart(snap({ state, error: '需要管理员权限' })), state).toBe(false)
    }
  })

  it('failed 但 error 为空串/纯空白/字段缺席均判假；英文 administrator 不命中词表', () => {
    expect(needsElevationRestart(snap({ state: 'failed', error: '' }))).toBe(false)
    expect(needsElevationRestart(snap({ state: 'failed', error: '   ' }))).toBe(false)
    expect(
      needsElevationRestart({ state: 'failed', version: '', pid: 0, startedAt: '' } as unknown as ManagedSnapshot),
    ).toBe(false)
    expect(needsElevationRestart(snap({ state: 'failed', error: 'requires administrator privileges' }))).toBe(false)
  })

  it('null / undefined 快照判假（状态未明不渲染提权入口，实现为可选链回落）', () => {
    expect(needsElevationRestart(null)).toBe(false)
    expect(needsElevationRestart(undefined)).toBe(false)
  })
})

describe('releaseFormWord（N13 形态词表）', () => {
  it('hostfeed.Form 五档词逐一查表出短词', () => {
    expect(releaseFormWord('portable')).toBe('便携')
    expect(releaseFormWord('installer')).toBe('安装器')
    expect(releaseFormWord('package')).toBe('包')
    expect(releaseFormWord('archive')).toBe('归档')
    expect(releaseFormWord('binary')).toBe('程序')
  })

  it('空值三形态（undefined/null/空串）回空串——缺档行不画 chip', () => {
    expect(releaseFormWord()).toBe('')
    expect(releaseFormWord(null)).toBe('')
    expect(releaseFormWord('')).toBe('')
  })

  it('词表外值原样透传（后端扩词不静默吞标）；查表大小写敏感，异写形态如实透传', () => {
    expect(releaseFormWord('dmg')).toBe('dmg')
    expect(releaseFormWord('Portable')).toBe('Portable')
    expect(releaseFormWord('INSTALLER')).toBe('INSTALLER')
  })
})
