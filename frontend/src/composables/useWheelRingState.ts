// 快捷菜单轮盘「扇区级联外扩子环」的交互状态机——纯时序逻辑，零 DOM、零几何依赖。
//
// 设计语义（集成方必读）：
// 轮盘不再整盘换层。主环常驻；悬停主环分组扇区停留满 dwellMs 后，其子环在盘外
// 帽带展开。指针持续处于「父楔形或子环帽带」视为保持——这是迟滞（hysteresis）
// 判定：几何归属由调用方（持有盘心/半径/角度域的视图层）计算，状态机不感知任何
// 几何，只通过 enterGroup / leaveGroup 事件汇报获知指针在不在保持区。
// 离开保持区满 releaseMs 后收起子环（未钉住时）；指针短促扫出扫回不闪环。
// 点击分组 = 钉住开环（键盘 Enter / 触屏无悬停路径的直达方式），再点同组解除并
// 收起；每次唤出（quickmenu:opening）调 reset()，杜绝层级与时序跨会话残留。
//
// 钉住与悬停预览的关系（刻意行为，已用测试锁死）：
// pinnedGroup 表达"用户显式要求保持打开"，只被 clickGroup / collapse / reset
// 改写；悬停永不触碰它。已钉住 p 时悬停另一分组 i，dwell 到点照样把 openGroup
// 切成 i（预览覆盖显示，方便不松钉直接巡览），到点收起时 openGroup 回落到
// pinnedGroup。因此不变式"钉住时 openGroup 恒等于 pinnedGroup"在无悬停的静止态
// 成立，悬停预览期间允许暂时背离。
//
// 换组不做两段动画：开环状态下切悬停到不同分组 i 时，直接以 i 重起 dwell 计时，
// 旧环在 dwell 期间保持可见，到点 openGroup 一步覆写为 i——不存在"先收旧环再开
// 新环"的中间收起态，外扩级联的观感由调用方按 openGroup 单值直接渲染。
//
// 计时器铁律：setTimeout/clearTimeout 裸句柄（不用 setInterval），同一时刻至多
// 一个活跃计时器（dwell 与 release 互斥，一切换类事件先清后起）。在组件实例内
// 创建时自动挂 onBeforeUnmount 调 dispose；实例上下文之外（单测、手动生命周期）
// 不注册钩子，由调用方显式 dispose——绝不因缺实例而调用 onBeforeUnmount 崩溃。

import { getCurrentInstance, onBeforeUnmount, ref } from 'vue'
import type { Ref } from 'vue'

export interface WheelRingStateOptions {
  /** 悬停进入分组扇区后展开子环所需的停留时长（ms），默认 120。 */
  dwellMs?: number
  /** 离开保持区后收起子环的延迟（ms，迟滞窗口），默认 180。 */
  releaseMs?: number
}

interface WheelRingState {
  /** 当前展开子环的分组索引（null = 未展开）。调用方据此渲染唯一的子环帽带。 */
  openGroup: Readonly<Ref<number | null>>
  /** 钉住的分组索引（钉住时静止态 openGroup 恒等于它；悬停预览期间可暂时背离）。 */
  pinnedGroup: Readonly<Ref<number | null>>
  /** 外甩取消态：调用方置位，状态机只透传管理（reset 清零，collapse 保留）。 */
  cancelArmed: Readonly<Ref<boolean>>
  /** 悬停进入分组扇区（含帽带内保持期间的重复汇报）。i 变化则重起 dwell；相同 i 只取消收起计时。 */
  enterGroup(i: number): void
  /** 指针离开全部保持区：清未到点的 dwell，并在有开环时起 release 计时（未钉住才会在到点收起）。 */
  leaveGroup(): void
  /** 点击分组：未开 → 立即开并钉住；已钉 → 解除并收；已开未钉 → 转为钉住。 */
  clickGroup(i: number): void
  /** 置入/解除外甩取消态（纯透传，几何判定在调用方）。 */
  setCancel(on: boolean): void
  /** Esc 第一级：收子环并解除钉住（不关窗、不动 cancelArmed），清空一切计时。 */
  collapse(): void
  /** 每次唤出：清空全部时序状态与计时器（含 cancelArmed），回到未展开未钉住的初始态。 */
  reset(): void
  /** 组件卸载/手动场景：仅清计时器，不动状态值。内部自动挂载时无需手动调用。 */
  dispose(): void
}

/**
 * 创建轮盘外扩子环的时序状态机。
 *
 * 可在组件 setup 内使用（自动随卸载清理计时器），也可裸调（此时必须自己 dispose）；
 * 无响应式作用域要求，回调不读组件状态，天然可单测。
 */
export function useWheelRingState(options: WheelRingStateOptions = {}): WheelRingState {
  const dwellMs = options.dwellMs ?? 120
  const releaseMs = options.releaseMs ?? 180

  const openGroup = ref<number | null>(null)
  const pinnedGroup = ref<number | null>(null)
  const cancelArmed = ref(false)

  // 最后一次汇报"指针在此分组保持区"的索引（null = 在保持区之外）。
  // 是"同 i 只取消收起、异 i 才重起 dwell"判定的唯一依据。
  let hovered: number | null = null
  // 至多一个活跃计时器：句柄与其种类严格同生同灭。
  let timer: ReturnType<typeof setTimeout> | null = null
  let timerKind: 'dwell' | 'release' | null = null

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    timerKind = null
  }

  function startTimer(kind: 'dwell' | 'release', ms: number, fire: () => void) {
    clearTimer()
    timerKind = kind
    timer = setTimeout(() => {
      timer = null
      timerKind = null
      fire()
    }, ms)
  }

  function enterGroup(i: number) {
    // 同组重复汇报（保持区内指针微动、enter/enter 交错）：只撤销待收起，
    // 不重起 dwell——dwell 从首次进入累积计时，防止"反复小动永不到点展开"。
    if (i === hovered) {
      if (timerKind === 'release') clearTimer()
      return
    }
    // leaveGroup 后计时未走满又回到仍开着的同一组：迟滞保持，撤销收起。
    if (timerKind === 'release' && openGroup.value === i) {
      hovered = i
      clearTimer()
      return
    }
    // 切到不同分组：一律先清后起，以新目标重起 dwell。旧开环（无论是否钉住）
    // 保持可见直到到点被 openGroup=i 一步覆写；钉住意图不因此丢失（pinned
    // 单独保留，见文件头"预览覆盖显示"语义）。
    hovered = i
    startTimer('dwell', dwellMs, () => {
      openGroup.value = i
    })
  }

  function leaveGroup() {
    hovered = null
    // 未到点的 dwell 随指针弃访作废：离开即放弃展开意图，绝不在离开后凭空开环。
    clearTimer()
    if (openGroup.value !== null) {
      startTimer('release', releaseMs, () => {
        // 未钉住 → 收起（回落 null）；钉住 → 仅结束悬停预览，开环保留。
        openGroup.value = pinnedGroup.value
      })
    }
  }

  function clickGroup(i: number) {
    clearTimer()
    hovered = i
    if (pinnedGroup.value === i) {
      // 已钉 → 解除并收。刻意保持 hovered=i：指针仍停在扇区上、不会再生成
      // 同组 mouseenter，天然避免"刚点收就又被 dwell 弹开"的对抗。
      pinnedGroup.value = null
      openGroup.value = null
    } else if (openGroup.value === i) {
      // 已开未钉（悬停 dwell 或预览开出来的）→ 转为钉住。
      pinnedGroup.value = i
    } else {
      // 未开 → 立即开并钉住：触屏/键盘路径无悬停，不等 dwell。
      // 若原本钉着别的组，钉注意随开环一并转移。
      openGroup.value = i
      pinnedGroup.value = i
    }
  }

  function setCancel(on: boolean) {
    cancelArmed.value = on
  }

  function collapse() {
    clearTimer()
    hovered = null
    openGroup.value = null
    pinnedGroup.value = null
    // cancelArmed 与外甩取消属窗口级状态，collapse（不关窗）不越权清理。
  }

  function reset() {
    collapse()
    cancelArmed.value = false
  }

  function dispose() {
    clearTimer()
  }

  // 组件实例内自动挂卸载清理；实例上下文之外（单测/手动）不注册，避免
  // onBeforeUnmount 的"during a non-function"警告与崩溃，由调用方 dispose。
  if (getCurrentInstance()) {
    onBeforeUnmount(dispose)
  }

  return {
    openGroup,
    pinnedGroup,
    cancelArmed,
    enterGroup,
    leaveGroup,
    clickGroup,
    setCancel,
    collapse,
    reset,
    dispose,
  }
}
