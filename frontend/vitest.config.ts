// Vitest 独立配置：不复用 vite.config.ts——其中的 wails 插件面向 dev-server/构建产物，
// 测试环境没有 Wails 原生运行时，bindings 一律经 vi.mock 打桩（见 docs/FRONTEND.md §8 测试 seam）。
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 三线并行解析缝：并行开发中的模块其绑定生成物尚未落盘，Vite import 分析对不可
// 解析的 `bindings/hanxi/**` 相对导入会直接编译失败——spec 的 vi.mock 还没机会生效
// （dbx/gonavi spec 头注所称「解析缝兜底」即此件）。本插件只为**确实不存在**的
// 绑定导入解析出空壳模块；真实生成物落盘后候选路径命中、插件自动放行不再介入，
// vi.mock 照常覆盖空壳，生产构建（vite.config）零感知。
function missingBindingSeam(): Plugin {
  const bindingsRoot = fileURLToPath(new URL('./bindings', import.meta.url))
  const virtualPrefix = '\0missing-binding:'
  return {
    name: 'vitest-missing-binding-seam',
    enforce: 'pre',
    resolveId(source, importer) {
      if (!importer || !source.startsWith('.') || !source.includes('bindings/hanxi/')) return null
      const abs = path.resolve(path.dirname(importer), source)
      if (!abs.startsWith(bindingsRoot)) return null
      const candidates = [abs, `${abs}.js`, `${abs}.ts`, path.join(abs, 'index.js')]
      if (candidates.some((c) => existsSync(c))) return null
      return virtualPrefix + abs
    },
    load(id) {
      if (!id.startsWith(virtualPrefix)) return null
      return 'export {}'
    },
  }
}

export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  plugins: [missingBindingSeam(), vue()],
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.spec.ts'],
  },
})
