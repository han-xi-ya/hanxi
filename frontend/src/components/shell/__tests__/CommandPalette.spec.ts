// 命令面板特征测试：锁定自包含热键契约（Ctrl/⌘+K toggle、disabled 门禁）、
// 子序列模糊匹配与排序（"por"→释放端口在前）、组分段（GROUP_META 顺序、无组归"其他"）、
// 键盘导航（↑↓ 循环、Enter/1–9 触发并关闭、composing 期间不劫持）、空态与 12 条上限、
// body 滚动锁与焦点归还。Teleport 经 stub 就地渲染以便查询；热键直接派发至 window
// （组件监听挂在 window，覆盖任意元素冒泡链）。
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import CommandPalette from '../CommandPalette.vue'
import type { NavEntry } from '../../../../bindings/hanxi/internal/extapi/models'

type PaletteNav = NavEntry & { group?: string }

const nav = (route: string, title: string, icon: string, order: number, group?: string): PaletteNav => ({
  id: route,
  route,
  title,
  icon,
  section: 'ext',
  order,
  group,
})

const NAVS: PaletteNav[] = [
  nav('/ext/portscan', '端口扫描', 'i:search', 5, 'network'),
  nav('/ext/portkill', '释放端口', '✕', 2, 'system'),
  nav('/ext/memo', '随手记', 'i:file-text', 4, 'efficiency'),
  nav('/logs', '日志', 'i:file-text', 90, ''),
]

const wrappers: ReturnType<typeof mount>[] = []

function factory(props: Partial<{ navs: PaletteNav[]; disabled: boolean }> = {}) {
  const w = mount(CommandPalette, {
    props: { navs: NAVS, ...props },
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
  wrappers.push(w)
  return w
}

async function pressKey(init: KeyboardEventInit) {
  window.dispatchEvent(new KeyboardEvent('keydown', { cancelable: true, bubbles: true, ...init }))
  await nextTick()
  await nextTick()
}

const hotkey = () => pressKey({ key: 'k', ctrlKey: true })

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount()
})

describe('CommandPalette', () => {
  it('Ctrl+K 与 ⌘K 打开/收起 toggle；disabled 时热键失效', async () => {
    const w = factory()
    expect(w.find('.palette-mask').exists()).toBe(false)
    await hotkey()
    expect(w.find('.palette-mask').exists()).toBe(true)
    await pressKey({ key: 'k', metaKey: true })
    expect(w.find('.palette-mask').exists()).toBe(false)
    await pressKey({ key: 'k', metaKey: true })
    expect(w.find('.palette-mask').exists()).toBe(true)
    await pressKey({ key: 'k', metaKey: true })
    await w.setProps({ disabled: true })
    await hotkey()
    expect(w.find('.palette-mask').exists()).toBe(false)
  })

  it('空查询按组分段全量展示：GROUP_META 顺序，无组项归"其他"垫底', async () => {
    const w = factory()
    await hotkey()
    expect(w.findAll('.p-item')).toHaveLength(4)
    expect(w.findAll('.p-sec').map((s) => s.text())).toEqual([
      '网络与传输', '系统管理', '效率办公', '其他',
    ])
    expect(w.findAll('.p-item').map((b) => b.find('.p-name').text())).toEqual([
      '端口扫描', '释放端口', '随手记', '日志',
    ])
  })

  it('模糊匹配："por" 仅命中两个端口模块（route 末段子序列），同分按 order 释放端口在前并 em 高亮', async () => {
    const w = factory()
    await hotkey()
    await w.find('input').setValue('por')
    const items = w.findAll('.p-item')
    expect(items).toHaveLength(2)
    expect(items[0].find('.p-name').text()).toBe('释放端口')
    expect(items[0].find('.p-route em').text()).toBe('por')
    expect(items[1].find('.p-name').text()).toBe('端口扫描')
  })

  it('组中文名参与匹配："网络" 命中网络组全部条目并高亮段标题', async () => {
    const w = factory()
    await hotkey()
    await w.find('input').setValue('网络')
    const items = w.findAll('.p-item')
    expect(items).toHaveLength(1)
    expect(items[0].find('.p-name').text()).toBe('端口扫描')
    expect(w.find('.p-sec em').text()).toBe('网络')
  })

  it('键盘导航：↑↓ 循环移动选中，Enter emit 选中 route 并关闭，1–9 直达 nth 项', async () => {
    const w = factory()
    await hotkey()
    const selText = () => w.find('.p-item.sel .p-name').text()
    expect(selText()).toBe('端口扫描')
    await pressKey({ key: 'ArrowDown' })
    expect(selText()).toBe('释放端口')
    await pressKey({ key: 'ArrowUp' })
    await pressKey({ key: 'ArrowUp' }) // 首项上移回绕到末项
    expect(selText()).toBe('日志')
    await pressKey({ key: 'Enter' })
    expect(w.emitted('navigate')).toEqual([['/logs']])
    expect(w.find('.palette-mask').exists()).toBe(false)
    await hotkey()
    await pressKey({ key: '2' })
    expect(w.emitted('navigate')![1]).toEqual(['/ext/portkill'])
    expect(w.find('.palette-mask').exists()).toBe(false)
  })

  it('中文输入法 composing 期间不劫持 ↑↓ 与数字键', async () => {
    const w = factory()
    await hotkey()
    await w.find('input').trigger('compositionstart')
    await pressKey({ key: 'ArrowDown' })
    expect(w.find('.p-item.sel .p-name').text()).toBe('端口扫描')
    await pressKey({ key: '2', isComposing: true })
    expect(w.emitted('navigate')).toBeUndefined()
    await w.find('input').trigger('compositionend')
    await pressKey({ key: 'ArrowDown' })
    expect(w.find('.p-item.sel .p-name').text()).toBe('释放端口')
  })

  it('无匹配显示空态文案；Esc 关闭并恢复 body 滚动与焦点', async () => {
    const w = factory()
    await hotkey()
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement).toBe(w.find('input').element)
    await w.find('input').setValue('zzqqxx')
    expect(w.findAll('.p-item')).toHaveLength(0)
    expect(w.find('.p-empty').text()).toBe('未找到匹配模块')
    await pressKey({ key: 'Escape' })
    expect(w.find('.palette-mask').exists()).toBe(false)
    expect(document.body.style.overflow).toBe('')
  })

  it('结果上限 12 条：15 个可命中项只展示前 12', async () => {
    const many: PaletteNav[] = Array.from({ length: 15 }, (_, i) =>
      nav(`/ext/z${String(i).padStart(2, '0')}`, `模块 ${String(i)}`, '◻', i, 'system'),
    )
    const w = factory({ navs: many })
    await hotkey()
    expect(w.findAll('.p-item')).toHaveLength(12)
  })
})
