// Ping/Traceroute 诊断表单骨架单源（冗余治理波 2C）：两面板的目标/数值 v-model
// 样板、快捷目标词面、发起钮禁用式与「先回填再发起」执行序逐字同形，收敛于此；
// 真差异（次数 vs 最大跳数的档位与词面、快捷目标成员取选）留在各面板。
// 红线：quickTarget 的 emit 顺序（update:target 先于 run）是跨 Tab 快捷联动的
// 命门，PingPanel.spec 以宿主 seq 序列钉死，不得调换。
import { computed } from 'vue'

/** 快捷常用目标：label 为钮面词（不含括号），value 为回填地址；渲形统一 `label (value)` */
export interface DiagQuickTarget {
  label: string
  value: string
}

/** 快捷目标词面单源（原两面板四处硬编码）；各面板成员取选列表是各自业务 */
export const DIAG_QUICK_TARGETS = {
  aliDns: { label: '阿里 DNS', value: '223.5.5.5' },
  tencentDns: { label: '腾讯 DNS', value: '119.29.29.29' },
  cloudflare: { label: 'Cloudflare', value: '1.1.1.1' },
  google: { label: 'Google', value: '8.8.8.8' },
} as const satisfies Record<string, DiagQuickTarget>

/**
 * RTT 色档判定（原两面板逐字同形的阈值三元）：<50 fast / <150 medium / 其余 slow。
 * 注意与 LanScannerView 的 20/500 + normal 档是刻意分叉（§9.6-10 已裁决各留各家），
 * 对应样式见 diagRtt.css（publicip 家族就近共享，非全局原子）。
 */
export function rttTier(rttMs: number): 'fast' | 'medium' | 'slow' {
  return rttMs < 50 ? 'fast' : rttMs < 150 ? 'medium' : 'slow'
}

export interface DiagFormModelConfig {
  target: () => string
  count: () => number
  loading: () => boolean
  updateTarget: (value: string) => void
  updateCount: (value: number) => void
  run: () => void
}

/** 目标/数值双 v-model computed + 发起钮禁用式 + 快捷目标动作（count 位 Ping 用次数、Traceroute 用最大跳数） */
export function useDiagFormModel(config: DiagFormModelConfig) {
  const targetModel = computed({
    get: config.target,
    set: config.updateTarget,
  })

  const countModel = computed({
    get: config.count,
    set: config.updateCount,
  })

  // 禁用式两面板逐字同：加载中或目标去空后为空
  const runDisabled = computed(() => config.loading() || !config.target().trim())

  // 常用目标：先回填再发起。emit 同步送达视图，故 run 读到的已是新值，与拆分前同一执行序。
  function quickTarget(value: string) {
    config.updateTarget(value)
    config.run()
  }

  return { targetModel, countModel, runDisabled, quickTarget }
}
