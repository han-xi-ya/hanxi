// ModuleCard 契约测试：主操作严格映射 PRIMARY_ACTION_META（含 builtin-logical
// "加入工作台"如实文案）、none 禁用+title=reason、摘要/瞬时态徽标走词表文字、
// 次操作（启用/停用、卸载、详情）可见性与事件上抛。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ModuleCard from '../ModuleCard.vue'
import type { ModuleEntry } from '../../../composables/useModuleCatalog'
import { PRIMARY_ACTION_META, SUMMARY_META, DELIVERY_META } from '../../../constants/status'

function entry(over: {
  catalog?: Partial<ModuleEntry['catalog']>
  state?: Partial<NonNullable<ModuleEntry['state']>> | null
} = {}): ModuleEntry {
  return {
    catalog: {
      id: 'memo',
      name: '极客随手记',
      description: '本机随手记事与标签检索',
      category: 'efficiency',
      deliveryKind: 'builtin-logical',
      capabilities: ['tray-commands'],
      entrypoints: ['rpc', 'navigation', 'tray'],
      compatibility: { hostRange: '*', platform: ['windows'] },
      permissions: [],
      owner: 'hanxi',
      ...over.catalog,
    },
    state: over.state === null ? null : {
      schema: 1,
      moduleId: over.catalog?.id ?? 'memo',
      delivery: 'installed',
      policy: 'enabled',
      runtime: 'active',
      health: 'current',
      primaryAction: 'open',
      summary: 'running',
      ...over.state,
    },
  }
}

function mountCard(e: ModuleEntry, busy = false) {
  return mount(ModuleCard, { props: { entry: e, busy } })
}

describe('ModuleCard', () => {
  it('primaryAction=open：主按钮"打开"（primary 变体），点击上抛 primary', async () => {
    const e = entry()
    const w = mountCard(e)
    const btn = w.find('button.primary-btn')
    expect(btn.text()).toBe(PRIMARY_ACTION_META.open.label)
    expect(btn.classes()).toContain('btn-primary')
    await btn.trigger('click')
    expect(w.emitted('primary')?.[0]).toEqual([e])
  })

  it('builtin-logical 安装文案如实：按钮"加入工作台"+title"不影响主程序体积"', () => {
    const e = entry({
      catalog: { id: 'wifi', deliveryKind: 'builtin-logical' },
      state: { delivery: 'absent', policy: 'disabled', runtime: 'inactive', health: 'current', primaryAction: 'install', summary: 'not-installed' },
    })
    const w = mountCard(e)
    const btn = w.find('button.primary-btn')
    expect(btn.text()).toBe('加入工作台')
    expect(btn.attributes('title')).toBe('加入工作台（不影响主程序体积）')
  })

  it('非 builtin-logical 的 install 保持词表原案"安装"', () => {
    const e = entry({
      catalog: { id: 'future', deliveryKind: 'managed-declarative' },
      state: { delivery: 'absent', policy: 'disabled', runtime: 'inactive', health: 'current', primaryAction: 'install', summary: 'not-installed' },
    })
    const w = mountCard(e)
    const btn = w.find('button.primary-btn')
    expect(btn.text()).toBe(PRIMARY_ACTION_META.install.label)
    expect(btn.attributes('title')).toBeUndefined()
  })

  it('primaryAction=none：禁用 + title=reason（状态原因可见于提示）', () => {
    const e = entry({
      state: { delivery: 'installed', policy: 'blocked', runtime: 'inactive', health: 'current', primaryAction: 'none', reason: '该模块被平台策略阻止，等待官方恢复', summary: 'blocked' },
    })
    const w = mountCard(e)
    const btn = w.find('button.primary-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toBe('该模块被平台策略阻止，等待官方恢复')
    // reason 同时以文字行呈现（状态不只依赖颜色/悬浮）
    expect(w.find('.card-reason').text()).toContain('策略阻止')
  })

  it('摘要徽标取 SUMMARY_META 文字；瞬时交付态取 DELIVERY_META 补呈现', () => {
    const e = entry({
      state: { delivery: 'updating', policy: 'enabled', runtime: 'busy', health: 'current', primaryAction: 'none', reason: '更新事务进行中', summary: 'in-progress' },
    })
    const w = mountCard(e)
    const chips = w.findAll('.summary-chip, .badge-row .chip')
    expect(chips.map((c) => c.text())).toContain(SUMMARY_META['in-progress'])
    expect(chips.map((c) => c.text())).toContain(DELIVERY_META.updating.text)
    // 摘要色调走 tone 词表（information=操作进行中）
    expect(w.find('.summary-chip').classes()).toContain('chip-information')
  })

  it('健康徽标 update-available 带投影版本："有可用更新 → x.y.z"，title 承载全文', () => {
    const w = mountCard(entry({
      state: { delivery: 'installed', policy: 'enabled', runtime: 'active', health: 'update-available', primaryAction: 'open', summary: 'running-update', remoteVersion: '2.3.4' },
    }))
    const chip = w.findAll('.badge-row .chip').find((c) => c.text().includes('有可用更新'))
    expect(chip).toBeDefined()
    expect(chip!.text()).toBe('有可用更新 → 2.3.4')
    expect(chip!.attributes('title')).toBe('有可用更新 → 2.3.4')
    // 版本短语承载于可截断文字段（长版本串不撑破卡片）
    expect(chip!.find('.health-text').exists()).toBe(true)
  })

  it('健康徽标无版本不编造：无 remoteVersion 时仅词表原文且无 title；非 update-available 健康值不带版本', () => {
    const noVersion = mountCard(entry({
      state: { delivery: 'installed', policy: 'enabled', runtime: 'active', health: 'update-available', primaryAction: 'open', summary: 'running-update' },
    }))
    const chip = noVersion.findAll('.badge-row .chip').find((c) => c.text().includes('有可用更新'))
    expect(chip!.text()).toBe('有可用更新')
    expect(chip!.attributes('title')).toBeUndefined()

    // 其他健康值（撤回类）即便 fixture 混入版本也不显示箭头短语（版本仅属 update-available）
    const revoked = mountCard(entry({
      state: { delivery: 'installed', policy: 'enabled', runtime: 'inactive', health: 'revoked', primaryAction: 'none', summary: 'blocked', remoteVersion: '9.9.9' },
    }))
    const rChip = revoked.findAll('.badge-row .chip').find((c) => c.text().includes('撤回'))
    expect(rChip!.text()).toBe('版本已被官方撤回')
    expect(rChip!.text()).not.toContain('9.9.9')
  })

  it('次操作：运行中模块给出"停用/卸载/详情"；点击分别上抛 setEnabled/uninstall/detail', async () => {
    const e = entry()
    const w = mountCard(e)
    const secondary = w.findAll('.secondary-row button')
    expect(secondary.map((b) => b.text())).toEqual(['详情', '停用', '卸载'])
    await secondary[0].trigger('click')
    expect(w.emitted('detail')?.[0]).toEqual([e])
    await secondary[1].trigger('click')
    expect(w.emitted('setEnabled')?.[0]).toEqual([e, false])
    await secondary[2].trigger('click')
    expect(w.emitted('uninstall')?.[0]).toEqual([e])
  })

  it('次操作与主操作去重：主操作=enable 时不再出"启用"次钮；主操作=uninstall 时不出"卸载"', () => {
    const w1 = mountCard(entry({
      state: { delivery: 'installed', policy: 'disabled', runtime: 'inactive', health: 'current', primaryAction: 'enable', summary: 'installed-disabled' },
    }))
    // 停用/禁用态去重"启用"次钮；卸载入口仍在（与主操作不重合）
    expect(w1.findAll('.secondary-row button').map((b) => b.text())).toEqual(['详情', '卸载'])

    const w2 = mountCard(entry({
      state: { delivery: 'installed', policy: 'enabled', runtime: 'inactive', health: 'corrupt', primaryAction: 'uninstall', summary: 'faulted' },
    }))
    expect(w2.findAll('.secondary-row button').map((b) => b.text())).not.toContain('卸载')
  })

  it('mandatory 模块不给出停用/卸载次入口（Core 守卫的 UI 侧呈现）', () => {
    const w = mountCard(entry({
      state: { delivery: 'installed', policy: 'mandatory', runtime: 'active', health: 'current', primaryAction: 'open', summary: 'running' },
    }))
    const labels = w.findAll('.secondary-row button').map((b) => b.text())
    expect(labels).not.toContain('停用')
    expect(labels).not.toContain('卸载')
  })

  it('busy：全部动作钮禁用且不发事件', async () => {
    const e = entry()
    const w = mountCard(e, true)
    const btn = w.find('button.primary-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(w.emitted('primary')).toBeUndefined()
    expect(w.findAll('.secondary-row button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  })

  it('长描述截断由 line-clamp 样式承担且 title 携带全文', () => {
    const long = '这是一段非常长的模块描述'.repeat(8)
    const w = mountCard(entry({ catalog: { description: long } }))
    const desc = w.find('.mod-desc')
    expect(desc.attributes('title')).toBe(long)
    expect(desc.classes()).toContain('mod-desc')
  })

  it('Wave 4：在途 Operation → 徽标文字承载 phase+进度，busy 并入禁用全部动作钮', async () => {
    const e = entry()
    const w = mount(ModuleCard, {
      props: {
        entry: e,
        operation: {
          schema: 1, id: 'run-1', moduleId: 'memo', kind: 'update', phase: 'unpack',
          status: 'running', progress: 66.4, cancellable: true,
          startedAt: '2026-09-18T10:00:00+08:00',
        },
      },
    })
    const chip = w.find('.badge-row .chip')
    expect(chip.text()).toContain('解压') // phase 中文词表
    expect(chip.text()).toContain('66%') // 进度百分比（文字承载）
    expect(chip.classes()).toContain('chip-information') // running → information
    // busy 并入：动作钮全禁用且不发事件（不新增状态真相）
    const btn = w.find('button.primary-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(w.emitted('primary')).toBeUndefined()
    expect(w.findAll('.secondary-row button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  })

  it('Wave 4：progress=nil 不编造百分比；无 operation 时不出在途徽标', () => {
    const w = mount(ModuleCard, {
      props: {
        entry: entry(),
        operation: {
          schema: 1, id: 'q-1', moduleId: 'memo', kind: 'install', phase: 'resolve',
          status: 'queued', progress: null, cancellable: false,
          startedAt: '2026-09-18T10:00:00+08:00',
        },
      },
    })
    const chip = w.find('.badge-row .chip')
    expect(chip.text()).toContain('解析版本')
    expect(chip.text()).not.toContain('%')
    expect(mountCard(entry()).find('.badge-row').text()).not.toMatch(/%|解析版本/)
  })

  it('无状态投影：主按钮禁用为"无可用操作"，摘要显示"状态未知"', () => {
    const w = mountCard(entry({ state: null }))
    const btn = w.find('button.primary-btn')
    expect(btn.text()).toBe(PRIMARY_ACTION_META.none.label)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(w.find('.summary-chip').text()).toBe('状态未知')
  })
})
