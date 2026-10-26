<script setup lang="ts">
// MemoGlyph：随手记页内私有矢量图标基座（MemoView v2 拆分批自视图内联
// defineComponent+h 渲染件原样抽出为 SFC，路径名册与描边口径零改动）。
// 描边件与 AppIcon 注册表同血统；注册表缺 pin/eye/search/copy 等名，全局层
// 收编前私有件先行（M4/图标线收编后可整块替换为注册表件）。
// 皮肤随本件自带（.memo-glyph 描边/基线随 currentColor）——宿主透传类
// （如 sheet-disc-chevron）经单根节点 fallthrough 落在 svg 上，各宿主 scoped 层
// 只写变换/间距类，不再依赖宿主对 svg 本体的着色规则。
const GLYPH_PATHS: Record<string, readonly string[]> = {
  pin: ['M8 2.5h8', 'M9.5 2.5v5L6 12.5h12L14.5 7.5v-5', 'M12 12.5v6'],
  eye: ['M2.5 12s3.7-6.5 9.5-6.5 9.5 6.5 9.5 6.5-3.7 6.5-9.5 6.5S2.5 12 2.5 12z', 'M12 9.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z'],
  'eye-off': ['M4 4l16 16', 'M9.9 5.8a9.8 9.8 0 0 1 2.1-.3c5.8 0 9.5 6.5 9.5 6.5a19 19 0 0 1-3 3.7', 'M6.4 7.6A19 19 0 0 0 2.5 12s3.7 6.5 9.5 6.5c1.2 0 2.3-.2 3.3-.6', 'M9.8 9.9a2.4 2.4 0 0 0 3.4 3.4'],
  copy: ['M11 9h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-8a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2z', 'M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1'],
  download: ['M12 3.5v11', 'M7 10l5 5 5-5', 'M4 20.5h16'],
  trash: ['M3.5 6h17', 'M8.5 6V3.5h7V6', 'M5.5 6l1.2 14h10.6L18.5 6', 'M10 10v6.5', 'M14 10v6.5'],
  pen: ['M16.5 3.5a2.6 2.6 0 0 1 3.7 3.7L7.5 19.9 2.8 21.2l1.3-4.7z'],
  sheet: ['M3.5 5.5h13v10h-13z', 'M8.5 19.5h12v-10h-4'],
  chevron: ['M6.5 9.5l5.5 5.5 5.5-5.5'],
}

withDefaults(
  defineProps<{
    name: string
    size?: number | string
  }>(),
  { size: 15 },
)
</script>

<template>
  <svg
    class="memo-glyph"
    viewBox="0 0 24 24"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <path v-for="d in GLYPH_PATHS[name] ?? []" :key="d" :d="d" />
  </svg>
</template>

<style scoped>
.memo-glyph { display: inline-block; vertical-align: -0.125em; flex: none; fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
</style>
