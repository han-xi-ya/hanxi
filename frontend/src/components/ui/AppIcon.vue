<script setup lang="ts">
// 通用图标壳：constants/icons.ts 注册表的内联 SVG 渲染器（§8 AppIcon 阶段1）；
// N27 批 A 起支持第二来源——`app:<moduleId>` 名渲染真图标位图（缺图回落通用徽标）；
// N27 红线尾巴起支持第三来源——`rt:<moduleId>|<fallback>` 名走运行期本机提取通道
// （constants/runtimeIcons.ts）：提取成功画厂商真图标，未就绪/失败画 fallback
// 矢量（即该模块提取通道上线前的原徽标），观感零损零打扰。
// 装饰性图标默认 aria-hidden；需要独立可访问名时传 label（role=img + aria-label）。
// 尺寸用 size（px 数字或 CSS 长度），默认 1em 跟随字号。
import { computed, watchEffect } from 'vue'
import { ICON_PATHS, type AppIconName, type IconName, type RenderableIcon } from '../../constants/icons'
import { appIconUrl, parseRuntimeIcon } from '../../constants/appIcons'
import { ensureRuntimeIcon, runtimeIconUrl } from '../../constants/runtimeIcons'

const props = withDefaults(defineProps<{
  name: IconName | AppIconName | RenderableIcon
  size?: number | string
  label?: string
}>(), { size: '1em' })

// 第三来源解析：rt 名先归一为 { 提取源 id, 回落名 }；非 rt 名恒 null 走旧轨。
const rt = computed(() => parseRuntimeIcon(props.name))
// 反应式读取提取通道缓存：提取成功后本 computed 翻转，位图原位升级。
const rtSrc = computed(() => (rt.value ? runtimeIconUrl(rt.value.id) ?? '' : ''))
// 挂载/换名即幂等发起提取（通道内部去重 + 负缓存，绝不每次渲染撞 RPC）。
watchEffect(() => {
  if (rt.value) void ensureRuntimeIcon(rt.value.id)
})

// 有效渲染名：rt 未就绪时等价于其 fallback（裸 svg 名或 app: 名），其余原样。
const effectiveName = computed<string>(() => {
  if (!rt.value) return props.name
  return rtSrc.value ? 'box' : (rt.value.fallbackName as string)
})

const appTarget = computed(() => (effectiveName.value.startsWith('app:') ? effectiveName.value.slice(4) : null))
const appSrc = computed(() => (appTarget.value === null ? '' : appIconUrl(appTarget.value)))
// 位图出货口：运行期提取优先（rt 就绪），其次入库件（app:）。
const imgSrc = computed(() => rtSrc.value || appSrc.value)
const paths = computed(() => ICON_PATHS[effectiveName.value as IconName] as readonly string[])
const cssSize = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))
</script>

<template>
  <img
    v-if="imgSrc"
    class="app-icon app-icon-img"
    :src="imgSrc"
    :style="{ width: cssSize, height: cssSize }"
    :alt="label ?? ''"
    :aria-hidden="label ? undefined : 'true'"
    :role="label ? 'img' : undefined"
    :aria-label="label"
  />
  <svg
    v-else
    class="app-icon"
    viewBox="0 0 24 24"
    width="24" height="24"
    :style="{ width: cssSize, height: cssSize }"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    :aria-hidden="label ? undefined : 'true'"
    :role="label ? 'img' : undefined"
    :aria-label="label"
  >
    <path v-for="(d, i) in paths" :key="i" :d="d" />
  </svg>
</template>

<style scoped>
.app-icon { display: inline-block; vertical-align: -0.125em; flex: none; }
/* 真图标位图：上游 exe 提取的彩色 PNG 不随主题重绘，object-fit 防非正方源拉伸 */
.app-icon-img { object-fit: contain; }
</style>
