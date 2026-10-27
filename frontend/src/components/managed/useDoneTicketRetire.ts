// ============================================================================
// 托管 done 票据「表内驻留再摘除」编排共享件
//
// 收编对象（审查台账 docs/FRONTEND_QUALITY_REVIEW_2026-10-26.md §五-6 裁决：
// 「摘票编排三处三份 + done 票 setTimeout 同形 → 收进 managed 共享壳，勿再
// 各自补洞」）：managed/store.ts 标准面、FrpcVersionsTab、RustDeskView、
// SubnetDeskView、DouzyView 五处手抄的同款计时形制。本件只收「驻留—摘除」
// 计时编排；票据 map 的写入/提交、done 后的列表重拉副作用、双表分流等全部
// 留在消费方，逐处现形制不变。
//
// 形参取自五处真实差分对账（2026-10-27 M4），缺省即最老形制，迁移行为零变化：
//  - ms：驻留时长。五处现值全为 800（DONE_TICK_RETIRE_MS 词源 store.ts 与
//    DouzyView 现词逐字一致），缺省沿用；
//  - guard：摘除时刻复核（只摘当前票）。SubnetDeskView 独有形制——驻留窗口内
//    同版本票据被新状态取代时，陈旧定时器作废本轮摘除；缺省不复核，即
//    store/Frpc/RustDesk/Douzy 现形制「陈旧定时器照摘」原样保留（补洞属行为
//    变化，M4 零变化铁律下不擅加）；
//  - track：计时器句柄登记 + onUnmounted 清尽。Douzy/SubnetDesk 独有；缺省
//    一次性自灭——不登记、卸载不拦截迟到摘除（store/Frpc/RustDesk 现形制）。
// 注意：五处均未做「新票据到达即撤旧计时器」（那是 useSnipasteDownloadTickets
// 的 Map 键控形制，清窗 900ms 且带安装事实核验，另一族编排，勿混）；本件
// 同样不撤旧计时器——guard 形制下新票取代旧票即令陈旧驻留作废，效果等价。
// ============================================================================

import { onUnmounted } from 'vue'

/** 下载完成票据的表内驻留时长（store.ts/DouzyView 现词 DONE_TICK_RETIRE_MS 的单源收编）。 */
export const DONE_TICK_RETIRE_MS = 800

export interface DoneTicketRetireOptions {
  /** 驻留时长 ms，缺省 DONE_TICK_RETIRE_MS（800）。 */
  ms?: number
  /** 登记计时器句柄并在卸载时 clearTimeout 清尽；缺省 false=一次性自灭。 */
  track?: boolean
}

/**
 * 安排一次驻留摘除：ms 到时 →（track 形制先从登记册销号）→ guard 缺省或放行
 * 时执行 retire。guard 在摘除时刻求值（返回 true=当前票仍该摘）；retire 内做
 * 什么（删键、提交新 map）由消费方闭包决定。
 */
export type DoneTicketRetireScheduler = (retire: () => void, guard?: () => boolean) => void

/**
 * 建 retire 调度器。track 形制须于组件 setup 同步期内调用（内部挂 onUnmounted）；
 * 缺省形制不触生命周期钩子，setup 内外皆可。
 */
export function useDoneTicketRetire(options: DoneTicketRetireOptions = {}): DoneTicketRetireScheduler {
  const ms = options.ms ?? DONE_TICK_RETIRE_MS

  if (!options.track) {
    return (retire, guard) => {
      setTimeout(() => {
        if (guard && !guard()) return
        retire()
      }, ms)
    }
  }

  // 登记册用裸 Set：句柄集合从不进模板/响应式读取，无 observe 需求（Douzy 现形
  // 制的 ref(Set) 包裹即冗余包装，收编时按零可观测差异归一为裸 Set）。
  const timers = new Set<ReturnType<typeof setTimeout>>()
  onUnmounted(() => {
    timers.forEach(clearTimeout)
    timers.clear()
  })
  return (retire, guard) => {
    const timer = setTimeout(() => {
      timers.delete(timer)
      if (guard && !guard()) return
      retire()
    }, ms)
    timers.add(timer)
  }
}
