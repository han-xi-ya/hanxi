// Phase 6 拆分新增：FileShareHero 展示壳冒烟测试。
// 纯 props/emit 组件（无 bindings/事件依赖），文案与类名切换契约由
// views/__tests__/FileShareView.spec.ts 整体锁定，此处只补组件独立接线。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FileShareHero from '../FileShareHero.vue'
import type { ServerStatus } from '../../../../bindings/hanxi/internal/modules/fileshare/models'

function statusOf(over: Partial<ServerStatus> = {}): ServerStatus {
  return {
    isRunning: false, port: 80, sharePath: '', activeConnections: 0,
    uploadCount: 0, downloadCount: 0, uploadBytes: 0, downloadBytes: 0,
    uploadRate: 0, downloadRate: 0, allowUpload: true, allowTextDrop: true,
    autoSaveToMemo: true, activeUrls: [], startedAt: '', ...over,
  }
}

describe('FileShareHero', () => {
  it('停止态：启动文案、状态灯无 online 类、兜底端口展示', () => {
    // port=0（falsy）走原 `status.port || fallbackPort` 兜底分支
    const wrapper = mount(FileShareHero, {
      props: { status: statusOf({ port: 0 }), fallbackPort: 8080, loading: false },
    })
    expect(wrapper.text()).toContain('服务已停止')
    expect(wrapper.find('.status-pill').classes()).not.toContain('online')
    expect(wrapper.find('.hero-action').text()).toBe('▶ 启动快传服务')
    expect(wrapper.text()).toContain('端口 :8080')
  })

  it('运行态+处理中：danger 类、禁用态与文案切换；可点后 emit toggle', async () => {
    const wrapper = mount(FileShareHero, {
      props: { status: statusOf({ isRunning: true, port: 8888 }), fallbackPort: 80, loading: true },
    })
    const btn = wrapper.find('.hero-action')
    expect(btn.classes()).toContain('btn-danger')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.text()).toBe('处理中...')
    expect(wrapper.find('.status-pill').classes()).toContain('online')
    expect(wrapper.text()).toContain('端口 :8888')
    await wrapper.setProps({ loading: false })
    await wrapper.find('.hero-action').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })
})
