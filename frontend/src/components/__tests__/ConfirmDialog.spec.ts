// 特征测试：ConfirmDialog 是全应用危险操作的标准件（useConfirm 的底座），
// 锁定其 emits 契约、busy 门禁、键盘/遮罩交互与 tone/details 渲染。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ConfirmDialog from '../ConfirmDialog.vue'
// happy-dom 不会把 SFC scoped 样式注入 document，计算样式拿不到声明值，
// 因此对样式契约改走「SFC 源码内 CSS 规则存在性」断言（?raw 为 Vite 原生能力）。
import sfcSource from '../ConfirmDialog.vue?raw'

const baseProps = { open: true, title: '卸载工具', description: '该版本数据将被删除，不可恢复。' }

// Teleport 打桩为原地渲染，便于在 wrapper 内查询
function factory(props: Partial<typeof baseProps> & Record<string, unknown> = {}) {
  return mount(ConfirmDialog, {
    props: { ...baseProps, ...props },
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
}

describe('ConfirmDialog', () => {
  it('open=false 时不渲染任何弹层', () => {
    const w = factory({ open: false })
    expect(w.find('.workbench-confirm-backdrop').exists()).toBe(false)
  })

  it('渲染标题、描述与 details 明细表', () => {
    const w = factory({ details: [{ label: '版本', value: 'v1.2.3' }] })
    expect(w.find('h2').text()).toBe('卸载工具')
    expect(w.find('header p').text()).toContain('不可恢复')
    expect(w.find('.workbench-confirm-details').text()).toContain('版本')
    expect(w.find('.workbench-confirm-details').text()).toContain('v1.2.3')
  })

  it('role=alertdialog + aria-labelledby 指向固定合法 id（标题含中文不产生非法 IDREF）', () => {
    const w = factory()
    const section = w.find('section')
    expect(section.attributes('role')).toBe('alertdialog')
    expect(section.attributes('aria-modal')).toBe('true')
    expect(section.attributes('aria-labelledby')).toBe('hx-confirm-title')
    expect(w.find('h2').attributes('id')).toBe('hx-confirm-title')
  })

  it('点击取消：emit cancel 并请求关闭（update:open=false）', async () => {
    const w = factory()
    await w.find('.workbench-confirm-btn.secondary').trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
    expect(w.emitted('update:open')?.[0]).toEqual([false])
  })

  it('点击确认：仅 emit confirm，不自动关闭（由父组件决定）', async () => {
    const w = factory()
    await w.find('.workbench-confirm-btn.primary').trigger('click')
    expect(w.emitted('confirm')).toHaveLength(1)
    expect(w.emitted('update:open')).toBeUndefined()
  })

  it('busy=true：两按钮禁用，点击取消被门禁拦截', async () => {
    const w = factory({ busy: true })
    const secondary = w.find('.workbench-confirm-btn.secondary')
    const primary = w.find('.workbench-confirm-btn.primary')
    expect(secondary.attributes('disabled')).toBeDefined()
    expect(primary.attributes('disabled')).toBeDefined()
    expect(primary.text()).toBe('处理中…')
    await secondary.trigger('click')
    expect(w.emitted('cancel')).toBeUndefined()
  })

  it('点击遮罩空白处（mousedown.self）取消；点击面板内部不触发', async () => {
    const w = factory()
    await w.find('.workbench-confirm').trigger('mousedown')
    expect(w.emitted('cancel')).toBeUndefined()
    await w.find('.workbench-confirm-backdrop').trigger('mousedown')
    expect(w.emitted('cancel')).toHaveLength(1)
  })

  it('打开→关闭切换时挂接 Escape 监听可取消（watch 非 immediate 的既有语义）', async () => {
    const w = factory({ open: false })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await w.setProps({ open: true })
    // watch(open) 内部有 await nextTick 才挂监听，等一轮宏任务
    await new Promise((r) => setTimeout(r))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    expect(w.emitted('cancel')).toHaveLength(1)
    w.unmount()
  })

  it('打开后焦点落在取消按钮（焦点管理契约）', async () => {
    const w = factory({ open: false })
    await w.setProps({ open: true })
    // watch(open) 内部还有一次 nextTick 才 focus
    await new Promise((r) => setTimeout(r))
    expect(document.activeElement).toBe(w.find('.workbench-confirm-btn.secondary').element)
    w.unmount()
  })

  it('tone=danger 挂上 is-danger 样式钩子', () => {
    const w = factory({ tone: 'danger' })
    expect(w.find('section').classes()).toContain('is-danger')
  })

  it('description 样式含 white-space: pre-line（\\n\\n 分段不被折叠成文字墙）', () => {
    // 调用方约定 description 用 \n\n 分段；无 pre-line 时 HTML 会折叠换行。
    const pRule = sfcSource.match(/\.workbench-confirm p\s*\{[^}]*\}/)?.[0] ?? ''
    expect(pRule).toMatch(/white-space:\s*pre-line/)
  })

  it('面板限高可滚动，小窗口下按钮不被顶出视口', () => {
    const panelRule = sfcSource.match(/\.workbench-confirm\s*\{[^}]*\}/)?.[0] ?? ''
    expect(panelRule).toMatch(/max-height:\s*min\(\s*72vh\s*,\s*640px\s*\)/)
    expect(panelRule).toMatch(/overflow-y:\s*auto/)
  })
})
