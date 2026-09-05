// ESLint flat config —— Phase 0 安全网定位：
// 存量代码一次性收紧不现实，此处把所有 error 级规则统一降级为 warn，
// 保证 `npm run lint` 基线可退出码 0（只报告、不阻断）；随重构推进逐 Phase 恢复 error。
// 红线：bindings/ 为 Wails 自动生成物，必须整体忽略（见 docs/FRONTEND.md §8）。
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import prettierConfig from 'eslint-config-prettier'

/** 将配置组内的 error 级规则就地降级为 warn（含数组形式 ['error', options]）。 */
function downgradeErrors(configs) {
  for (const config of configs) {
    if (!config || typeof config !== 'object' || !config.rules) continue
    for (const rule of Object.keys(config.rules)) {
      const entry = config.rules[rule]
      const severity = Array.isArray(entry) ? entry[0] : entry
      if (severity === 2 || severity === 'error') {
        config.rules[rule] = Array.isArray(entry) ? ['warn', ...entry.slice(1)] : 'warn'
      }
    }
  }
  return configs
}

export default tseslint.config(
  {
    name: 'hanxi/ignores',
    ignores: ['bindings/**', 'dist/**', 'node_modules/**', 'public/**'],
  },
  ...downgradeErrors([
    // TS 推荐规则集在前：其 base 配置会全局设置 TS parser
    ...tseslint.configs.recommended,
    // .vue 必须用 vue-eslint-parser 解析原始文件（在后，覆盖 .vue 的全局 parser）
    ...pluginVue.configs['flat/essential'],
    // .vue 内 <script lang="ts"> 再交由 typescript-eslint 二次解析
    {
      name: 'hanxi/vue-ts-script',
      files: ['**/*.vue'],
      languageOptions: { parserOptions: { parser: tseslint.parser } },
    },
  ]),
  {
    name: 'hanxi/custom',
    rules: {
      // 允许显式 any 只作警告——存量断言清理在 Phase 前中期推进
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'vue/multi-word-component-names': 'off', // *View/既有单复数命名约定，非缺陷
    },
  },
  // 必须最后：关闭与 Prettier 冲突的排版类规则
  prettierConfig,
)
