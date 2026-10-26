// useEverythingColumns 装载校验直测。面板 spec 已锚过拖拽钳位/防抖落盘/卸载清理，
// 但记忆值的读入校验只试了「合法 500 恢复 + 野键忽略」一路；越界值与损坏 JSON
// 两条防线（60~900 合法性、try/catch 静默回退）从未被实测过，此处补齐。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useEverythingColumns, type EverythingColKey } from '../useEverythingColumns'

const STORAGE_KEY = 'hanxi-everything-result-cols'
const DEFAULTS: Record<EverythingColKey, number> = { name: 320, path: 420, size: 70, time: 130, action: 118 }

function mountColumns() {
  let api!: ReturnType<typeof useEverythingColumns>
  const Host = defineComponent({
    setup() {
      api = useEverythingColumns()
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  return { api, wrapper }
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  localStorage.clear()
})

describe('useEverythingColumns 记忆值装载校验', () => {
  it('越界与类型不符的记忆值一律拒绝，合法边界值（60/900）放行', () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ name: 59, path: 901, size: 60, time: 900, action: '420' }),
    )
    const { api, wrapper } = mountColumns()
    expect(api.colWidths.name).toBe(DEFAULTS.name) // 59 越下界：回默认 320
    expect(api.colWidths.path).toBe(DEFAULTS.path) // 901 越上界：回默认 420
    expect(api.colWidths.action).toBe(DEFAULTS.action) // 字符串 '420'：类型不符回默认
    expect(api.colWidths.size).toBe(60) // 恰在下界：合法
    expect(api.colWidths.time).toBe(900) // 恰在上界：合法
    wrapper.unmount()
  })

  it('存储损坏（非法 JSON）静默回退全默认，不抛不糊', () => {
    localStorage.setItem(STORAGE_KEY, '{oops][')
    const { api, wrapper } = mountColumns()
    expect(api.colWidths).toEqual(DEFAULTS)
    expect(api.totalColWidth.value).toBe(
      Object.values(DEFAULTS).reduce((a, b) => a + b, 0),
    )
    wrapper.unmount()
  })
})
