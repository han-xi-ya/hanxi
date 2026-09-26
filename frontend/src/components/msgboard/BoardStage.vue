<script setup lang="ts">
// BoardStage：牌面舞台——"能 1:1 就 1:1"的自适应大预览（v4 牌桌口径，收编）。
//
// 缩放语义与 A 路 MsgBoardView v4 重写逐位同式：
//   scale = min(cap, (盒宽 − 12) / 卡片实际布局宽)
// 卡片实际宽优先实测（RO 量 .mbp-scaled——transform 不影响布局盒，测得的
// 就是未缩放真实宽；宿主已自测时直接传 cardWidth 覆盖），测不到回落
// 字号×13 兜底线；happy-dom/首帧双端同口径。cap 默认 1.0——舞台合法终态
// 是原大呈现，页内小预览等旧档纪律由调用方显式传 cap 约束。
//
// 职责边界：舞台只管"给定盒与牌 → 输出 scale 后的牌"，盒的外观（背景/
// 边框/高度）归宿主面板；--bc-max-h 超高裁剪反算只在盒高可测/可传时注入
// 到缩放容器上，靠 CSS 变量继承进牌体。
//
// 类名沿用：内层缩放容器保持 .mbp-scaled——视图现网选择器迁移期零断言
// 改写（scoped 样式自带 transform-origin 规则，不依赖宿主 CSS）。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import BoardCard from './BoardCard.vue'
import { cardMaxHeightPx, STAGE_SCALE_CAP, stageScaleMeasured } from './boardMetrics'

const props = withDefaults(
  defineProps<{
    text: string
    fontSize: number
    /** 宿主实测盒宽（px）；<=0/缺席走自测 RO，再落基准常数 300 */
    containerWidth?: number
    /** 宿主实测盒高（px）；>0 才反算注入 --bc-max-h（缺席则牌高交回 BoardCard 默认） */
    containerHeight?: number
    /** 宿主实测牌面自然宽（px）；<=0/缺席走舞台自测 RO，再落 字号×13 兜底 */
    cardWidth?: number
    /** 缩放封顶：默认 1.0（1:1 终态）；沿用旧页内小预览纪律的宿主传 0.8 */
    cap?: number
  }>(),
  { containerWidth: 0, containerHeight: 0, cardWidth: 0, cap: STAGE_SCALE_CAP },
)

const emit = defineEmits<{
  /** 当前生效缩放（immediate 首发）——宿主的"缩到 N%/真牌大 N 倍"文案与它同源 */
  (e: 'scale', value: number): void
}>()

// 自测兜底轨：盒走根节点、牌走 .mbp-scaled（max-content 布局，测得自然宽）
const rootEl = ref<HTMLElement | null>(null)
const scaledEl = ref<HTMLElement | null>(null)
const measuredW = ref(0)
const measuredH = ref(0)
const measuredCardW = ref(0)
let ro: ResizeObserver | null = null
function unhookRO() {
  ro?.disconnect()
  ro = null
}
watch([rootEl, scaledEl], ([box, card]) => {
  unhookRO()
  if (!box || !card || typeof ResizeObserver === 'undefined') return
  ro = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const rect = entry.contentRect
      if (!rect || rect.width <= 0 || rect.height <= 0) continue
      if (entry.target === box) {
        measuredW.value = rect.width
        measuredH.value = rect.height
      } else if (entry.target === card) {
        measuredCardW.value = rect.width
      }
    }
  })
  ro.observe(box)
  ro.observe(card)
}, { flush: 'post' })
onBeforeUnmount(unhookRO)

const effBoxW = computed(() =>
  props.containerWidth > 0 ? props.containerWidth : measuredW.value,
)
const effBoxH = computed(() =>
  props.containerHeight > 0 ? props.containerHeight : measuredH.value,
)
const effCardW = computed(() =>
  props.cardWidth > 0 ? props.cardWidth : measuredCardW.value,
)

const scale = computed(() => stageScaleMeasured({
  boxWidth: effBoxW.value,
  cardWidth: effCardW.value,
  fontSize: props.fontSize,
  cap: props.cap,
}))
// 超高裁剪反算与收编前视图同式：(盒高−12)/缩放，cardMaxHeightPx 自带 48px 下限
const cardMaxH = computed(() =>
  effBoxH.value > 0 ? cardMaxHeightPx(effBoxH.value, scale.value) : 0,
)
const scaledStyle = computed(() =>
  cardMaxH.value > 0
    ? { transform: `scale(${scale.value})`, '--bc-max-h': `${cardMaxH.value}px` }
    : { transform: `scale(${scale.value})` },
)

watch(scale, (v) => emit('scale', v), { immediate: true })
</script>

<template>
  <div ref="rootEl" class="bs-stage">
    <div ref="scaledEl" class="mbp-scaled" :style="scaledStyle">
      <BoardCard :text="text" :font-size="fontSize" />
    </div>
  </div>
</template>

<style scoped>
.bs-stage {
  display: grid;
  place-items: center;
  min-width: 0;
  overflow: hidden;
}
/* 与旧视图 .mbp-scaled 同规则：scale 以盒中心为原点，宽度按内容（max-content），
   RO 据此量到未缩放真实宽 */
.mbp-scaled {
  transform-origin: center;
  width: max-content;
}
</style>
