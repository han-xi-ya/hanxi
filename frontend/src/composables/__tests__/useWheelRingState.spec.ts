// useWheelRingState 的时序契约测试：dwell/release 到点精度、迟滞撤销、钉住三态、
// 悬停预览与钉住的隔离、collapse/reset/dispose 的计时器清零。
// 状态机零 DOM 零几何，全部用例可在组件实例之外裸建（同时反向验证
// "无实例上下文不注册 onBeforeUnmount 也不崩"）；唯一的组件挂载用例锁自动清理。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useWheelRingState } from '../useWheelRingState'

const DWELL = 120 // 默认值，与实现约定逐字对齐
const RELEASE = 180

afterEach(() => {
  vi.useRealTimers()
})

describe('useWheelRingState', () => {
  it('dwell 精确时序：119ms 未开、满 120ms 开', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(2)
    vi.advanceTimersByTime(119)
    expect(s.openGroup.value).toBeNull()
    vi.advanceTimersByTime(1)
    expect(s.openGroup.value).toBe(2)
    expect(s.pinnedGroup.value).toBeNull() // 悬停永不写钉住
  })

  it('自定义 dwellMs/releaseMs 生效', () => {
    vi.useFakeTimers()
    const s = useWheelRingState({ dwellMs: 50, releaseMs: 30 })
    s.enterGroup(0)
    vi.advanceTimersByTime(49)
    expect(s.openGroup.value).toBeNull()
    vi.advanceTimersByTime(1)
    expect(s.openGroup.value).toBe(0)
    s.leaveGroup()
    vi.advanceTimersByTime(29)
    expect(s.openGroup.value).toBe(0)
    vi.advanceTimersByTime(1)
    expect(s.openGroup.value).toBeNull()
  })

  it('同 i 重复汇报不重起 dwell（计时从首次进入累积），仅撤销待收起', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(0)
    vi.advanceTimersByTime(100)
    s.enterGroup(0) // 帽带内保持的重复汇报
    vi.advanceTimersByTime(20) // 距首次进入正好 120
    expect(s.openGroup.value).toBe(0) // 若被重起则此刻仍关着 → 锁死"不重起"
  })

  it('dwell 未走满即 leaveGroup：离开即弃，到点不开环', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(1)
    vi.advanceTimersByTime(60)
    s.leaveGroup()
    vi.advanceTimersByTime(1000)
    expect(s.openGroup.value).toBeNull()
  })

  it('release 防抖：179ms 回进同 i 不闭、180ms 不回进才闭', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(1)
    vi.advanceTimersByTime(DWELL)
    expect(s.openGroup.value).toBe(1)

    s.leaveGroup()
    vi.advanceTimersByTime(RELEASE - 1)
    s.enterGroup(1) // 迟滞窗口内回进保持区：撤销收起
    vi.advanceTimersByTime(1000)
    expect(s.openGroup.value).toBe(1)

    s.leaveGroup()
    vi.advanceTimersByTime(RELEASE)
    expect(s.openGroup.value).toBeNull()
  })

  it('钉住态下 leaveGroup 到点不收起（release 只对未钉住生效）', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(3)
    s.leaveGroup()
    vi.advanceTimersByTime(RELEASE * 3)
    expect(s.openGroup.value).toBe(3)
    expect(s.pinnedGroup.value).toBe(3)
  })

  it('clickGroup 三态：未开→立即开并钉住（不等 dwell）', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(2)
    expect(s.openGroup.value).toBe(2) // 零延迟
    expect(s.pinnedGroup.value).toBe(2)
  })

  it('clickGroup 三态：已钉→再点解除并收', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(2)
    s.clickGroup(2)
    expect(s.openGroup.value).toBeNull()
    expect(s.pinnedGroup.value).toBeNull()
  })

  it('clickGroup 三态：已开未钉（悬停开的）→转为钉住，开环不断', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(2)
    vi.advanceTimersByTime(DWELL)
    expect(s.pinnedGroup.value).toBeNull()
    s.clickGroup(2)
    expect(s.openGroup.value).toBe(2)
    expect(s.pinnedGroup.value).toBe(2)
    s.leaveGroup() // 转正后即可享受钉住保护
    vi.advanceTimersByTime(RELEASE)
    expect(s.openGroup.value).toBe(2)
  })

  it('开环态切悬停到不同组：不收旧环、直接重起 dwell，到点一步覆写', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(0)
    vi.advanceTimersByTime(DWELL)
    expect(s.openGroup.value).toBe(0)
    s.enterGroup(1) // 切目标：不允许出现"先收再开"的中间 null 态
    vi.advanceTimersByTime(DWELL - 1)
    expect(s.openGroup.value).toBe(0) // 旧环保持可见
    vi.advanceTimersByTime(1)
    expect(s.openGroup.value).toBe(1) // 覆写即换
  })

  it('悬停预览不改写 pinnedGroup；离开后 openGroup 回落钉住组（钉恒等式复原）', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(1) // 钉住 1
    s.leaveGroup()
    s.enterGroup(2) // 悬停预览另一组
    vi.advanceTimersByTime(DWELL)
    expect(s.openGroup.value).toBe(2)
    expect(s.pinnedGroup.value).toBe(1) // 预览期间钉不丢
    s.leaveGroup()
    vi.advanceTimersByTime(RELEASE)
    expect(s.openGroup.value).toBe(1) // 到点回到钉住环，而非收起
    expect(s.pinnedGroup.value).toBe(1)
  })

  it('钉住 p 时点击另一组 i：开环与钉注意图一并转移到 i', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(1)
    s.clickGroup(4)
    expect(s.openGroup.value).toBe(4)
    expect(s.pinnedGroup.value).toBe(4)
  })

  it('同段连续换组只有一个活跃计时器：只认最后一个目标', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(0)
    s.enterGroup(1)
    s.enterGroup(2)
    vi.advanceTimersByTime(DWELL)
    expect(s.openGroup.value).toBe(2)
    vi.advanceTimersByTime(DWELL * 2)
    expect(s.openGroup.value).toBe(2) // 无迟到的 0/1 覆写
  })

  it('setCancel 纯透传；collapse 保留、reset 清零', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.setCancel(true)
    expect(s.cancelArmed.value).toBe(true)
    s.collapse()
    expect(s.cancelArmed.value).toBe(true) // 不关窗的第一级回退不越权
    s.reset()
    expect(s.cancelArmed.value).toBe(false)
    s.setCancel(true)
    s.setCancel(false)
    expect(s.cancelArmed.value).toBe(false)
  })

  it('collapse：收环解钉清计时，advance 不再复活（dwell/release 两种在途都清）', () => {
    vi.useFakeTimers()
    // release 在途
    const a = useWheelRingState()
    a.enterGroup(0)
    vi.advanceTimersByTime(DWELL)
    a.leaveGroup()
    a.collapse()
    expect(a.openGroup.value).toBeNull()
    vi.advanceTimersByTime(RELEASE * 3)
    expect(a.openGroup.value).toBeNull()
    // dwell 在途 + 钉住也解
    const b = useWheelRingState()
    b.clickGroup(5)
    b.enterGroup(6) // 预览 dwell 在途
    b.collapse()
    expect(b.openGroup.value).toBeNull()
    expect(b.pinnedGroup.value).toBeNull()
    vi.advanceTimersByTime(DWELL * 3)
    expect(b.openGroup.value).toBeNull() // 不会到点"复活"成 6
  })

  it('reset：清空一切时序状态与计时器，且幂等', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.clickGroup(3)
    s.leaveGroup()
    s.enterGroup(7) // dwell 在途盖住 release
    s.reset()
    expect(s.openGroup.value).toBeNull()
    expect(s.pinnedGroup.value).toBeNull()
    expect(s.cancelArmed.value).toBe(false)
    s.reset() // 再来一次不炸、结果不变
    expect(s.openGroup.value).toBeNull()
    vi.advanceTimersByTime(DWELL * 3)
    expect(s.openGroup.value).toBeNull() // 计时已死
    // reset 后立即可用：新一轮唤出正常展开
    s.enterGroup(0)
    vi.advanceTimersByTime(DWELL)
    expect(s.openGroup.value).toBe(0)
  })

  it('dispose 手动清计时器（实例上下文外的裸建场景）', () => {
    vi.useFakeTimers()
    const s = useWheelRingState()
    s.enterGroup(0)
    vi.advanceTimersByTime(DWELL)
    s.leaveGroup() // release 在途
    s.dispose()
    vi.advanceTimersByTime(RELEASE * 3)
    expect(s.openGroup.value).toBe(0) // 计时被掐，状态值不动
  })

  it('组件实例内创建：卸载自动清计时器（onBeforeUnmount 挂载）', () => {
    vi.useFakeTimers()
    let api: ReturnType<typeof useWheelRingState> | null = null
    const Child = defineComponent({
      setup() {
        api = useWheelRingState()
        return () => h('div')
      },
    })
    const wrapper = mount(Child)
    api!.clickGroup(2)
    api!.enterGroup(4) // dwell 在途
    wrapper.unmount()
    vi.advanceTimersByTime(DWELL * 3)
    expect(api!.openGroup.value).toBe(2) // 卸载后无迟到覆写
  })
})
