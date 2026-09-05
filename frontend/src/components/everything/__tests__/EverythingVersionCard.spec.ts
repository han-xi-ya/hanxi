// EverythingVersionCard 聚焦测试：锁拆分后卡片的徽标判定（使用中/运行中/导入来源）、
// 卸载禁用条件（正在运行的版本）与三个动作事件的载荷；确认/删除业务逻辑在视图层，不在本组件。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import EverythingVersionCard from '../EverythingVersionCard.vue'

const official = {
  version: '1.5.0.1371',
  exePath: 'C:\\data\\everything\\1.5.0.1371\\Everything.exe',
  dir: 'C:\\data\\everything\\1.5.0.1371',
  size: 2 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
  source: '',
}

const imported = { ...official, version: '1.4.1.1024', dir: 'C:\\pf', isImport: true, source: 'C:\\Program Files\\Everything' }

function mountCard(props: Record<string, unknown>) {
  return mount(EverythingVersionCard, { props: { info: official, isActive: false, isRunning: false, ...props } })
}

describe('EverythingVersionCard', () => {
  it('普通态：官方下载徽标 + 三个操作可用；点击上抛载荷为版本信息', async () => {
    const w = mountCard({})
    expect(w.findAll('.badge').map((b) => b.text())).toEqual(['官方下载'])
    expect(w.classes()).not.toContain('card-active')
    const btns = w.findAll('button')
    expect(btns.map((b) => b.text())).toEqual(['设为使用', '📂 打开位置', '卸载'])
    await btns[0].trigger('click')
    await btns[1].trigger('click')
    await btns[2].trigger('click')
    expect(w.emitted('set-active')![0]).toEqual([official])
    expect(w.emitted('open-dir')![0]).toEqual([official])
    expect(w.emitted('remove')![0]).toEqual([official])
  })

  it('使用中：高亮描边 + 使用中徽标，隐藏「设为使用」', () => {
    const w = mountCard({ isActive: true })
    expect(w.find('.installed-card').classes()).toContain('card-active')
    expect(w.find('.badge').text()).toBe('使用中')
    expect(w.findAll('button').map((b) => b.text())).toEqual(['📂 打开位置', '卸载'])
  })

  it('运行中（非使用版本）：运行中徽标；卸载禁用并提示先退出', () => {
    const w = mountCard({ isRunning: true })
    expect(w.find('.badge').text()).toBe('运行中')
    const uninstall = w.findAll('button').find((b) => b.text() === '卸载')!
    expect(uninstall.attributes('disabled')).toBeDefined()
    expect(uninstall.attributes('title')).toBe('请先退出 Everything')
  })

  it('本地导入：导入徽标 + 官方/导入互斥 + 来源行', () => {
    const w = mountCard({ info: imported })
    expect(w.findAll('.badge').map((b) => b.text())).toEqual(['本地导入'])
    expect(w.find('.meta-line:last-child .k').text()).toBe('来源')
    expect(w.find('.meta-line:last-child .hint-dim').text()).toBe('C:\\Program Files\\Everything')
  })
})
