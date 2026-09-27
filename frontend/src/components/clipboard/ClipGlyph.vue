<script setup lang="ts">
// 页内私有矢量图标（描边件与 AppIcon 注册表同血统；注册表缺 copy/pause/play/
// image 等剪贴板必备名，全局层本轮禁碰，私有件先行——口径照 MemoView 的
// MemoGlyph 先例，图标线收编后可整块替换为注册表件）。
const GLYPH_PATHS: Record<string, readonly string[]> = {
  // 类别图标
  text: ['M4 5.5h16', 'M4 10.5h12', 'M4 15.5h14', 'M4 20.5h8'],
  image: [
    'M3.5 5.5h17v13h-17z',
    'M3.5 15.5l4.5-4.5 3.5 3.5 3.5-3.5 5 5.5',
    'M8 9.6a1.3 1.3 0 1 0 0.01 0z',
  ],
  file: ['M4 5h5l2 2h9a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z'],
  sticky: ['M4 4h16v11l-5 5H4z', 'M15 20v-5h5'],
  // 行内/页脚动作
  copy: [
    'M11 9h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-8a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2z',
    'M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1',
  ],
  pin: ['M8 2.5h8', 'M9.5 2.5v5L6 12.5h12L14.5 7.5v-5', 'M12 12.5v6'],
  trash: ['M3.5 6h17', 'M8.5 6V3.5h7V6', 'M5.5 6l1.2 14h10.6L18.5 6', 'M10 10v6.5', 'M14 10v6.5'],
  plus: ['M12 5v14', 'M5 12h14'],
  close: ['M18 6L6 18', 'M6 6l12 12'],
  // 暂停开关与警示
  pause: ['M9 5v14', 'M15 5v14'],
  play: ['M7.5 4.5l12 7.5-12 7.5z'],
  alert: ['m21.7 18-8-14a2 2 0 0 0-3.5 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.7-3z', 'M12 9v4', 'M12 17h.01'],
}

withDefaults(defineProps<{ name: string; size?: number | string }>(), { size: 15 })
</script>

<template>
  <svg
    class="clip-glyph"
    viewBox="0 0 24 24"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <path v-for="d in GLYPH_PATHS[name] ?? []" :key="d" :d="d" />
  </svg>
</template>

<style scoped>
/* 描边随 currentColor，明暗与色板零特判（MemoGlyph 同款基座） */
.clip-glyph {
  display: inline-block;
  vertical-align: -0.125em;
  flex: none;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.7;
  stroke-linecap: round;
  stroke-linejoin: round;
}
</style>
