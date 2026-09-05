// Vitest 独立配置：不复用 vite.config.ts——其中的 wails 插件面向 dev-server/构建产物，
// 测试环境没有 Wails 原生运行时，bindings 一律经 vi.mock 打桩（见 docs/FRONTEND.md §8 测试 seam）。
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  plugins: [vue()],
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.spec.ts'],
  },
})
