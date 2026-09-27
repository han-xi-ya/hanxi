// ============================================================================
// 托管进度/版本行状态共享纯函数（前端冗余治理 · 波 0，只增不删）
//
// stepOf：10 份逐字相同副本的单一来源（函数体逐字符一致，仅形参名与入参类型标注
//   有差）——views/{BCU,Douzy,GuoheView,QuickLook,Rufus,RustDesk,SubnetDesk}View.vue、
//   components/managed/ManagedVersionPanel.vue、components/FrpcVersionsTab.vue、
//   components/everything/EverythingReleaseTable.vue；components/vscode/
//   VSCodeReleaseTable.vue 语义等价但分支写法合并（`!==downloading || !total` 并档），
//   不并入逐字口径。入参实况（NormalizedProgress / DownloadProgress / DownloadTicket /
//   面板内联结构 { stage; done; total }）均为 { stage; done; total } 的结构超集，
//   统一签名后各消费点直接可传；Math.min(99, …) 封顶怪癖逐字保留——
//   进行中永不显示满格，满格由 done 档判 100。
//
// versionRowState：guoheview/quicklook/rufus 三视图 statusOf 逐字相同内核的纯函数化
//   （票据先行：error 档 → 'error'，其余档一律 'downloading'；无票据 → 版本串精确
//   匹配判已装；DouzyView 的 some 写法在本内核下同真值，可按此接线）。
//   其余 statusOf 副本语义分叉、本波不收——Panel（adapter.versions.statusOf 钩子 +
//   sameVersion 口径）、BCU（`${version}|${variant}` 复合键 + statusOverall 双变体
//   汇总）、Frpc（去 v 前缀互认）、RustDesk/SubnetDesk（form==='installed' 排除）、
//   VSCode（portable/installer 分形态判定）、Snipaste/Everything（自有票据/已装数据源
//   与词面），留待后续波次逐视图评估。
// ============================================================================

/** 版本行状态四档（字面量与 Panel/各视图 statusOf 返回联合逐字一致）。 */
export type ManagedRowState = 'installed' | 'downloading' | 'error' | 'idle'

/** 进度百分比：done 档恒 100；downloading 档取整改封顶 99；非 downloading 档与 total 为 0 一律 0。 */
export function stepOf(p: { stage: string; done: number; total: number }): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

/**
 * 远程行四态判定（进度票据先行）：有票据时 error 档 → 'error'、其余档一律
 * 'downloading'（done 票据清票驻留瞬间仍算进行中，与各视图现行为一致）；
 * 无票据回退版本串精确匹配判已装。逐字源：GuoheViewView.vue:39-44 ≡
 * QuickLookView.vue:46-51 ≡ RufusView.vue:50-55。
 */
export function versionRowState(
  p: { stage: string } | undefined | null,
  installed: readonly { version: string }[],
  version: string,
): ManagedRowState {
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.find((v) => v.version === version)
  return hit ? 'installed' : 'idle'
}
