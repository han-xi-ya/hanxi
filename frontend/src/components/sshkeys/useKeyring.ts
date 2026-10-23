// 本机密钥台账（钥匙环）的领域状态。
//
// 数据源是后端 ListKeys：它返回**一把钥匙一条**的 KeyEntry（私钥与配套公钥合并，
// 曾有"同指纹两行、人删错东西"的事故，按文件列的旧形状已整体退役），字段只有
// 路径 / 算法 / 公钥指纹 / 注释 / 口令三态 / 被清单引用——**没有任何密钥材料**。
// 这条边界在后端有测试钉着（keyring_test.go 断言私钥正文不得出现在返回值里），
// 前端不重复假装把关，界面上也只在页头说一次，不在行内复读。
import { ref, shallowRef } from 'vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'

export type GuardBadge = {
  tone: 'positive' | 'warning' | 'neutral'
  label: string
}

// guardBadge 把后端的口令三态翻译成短标签（行内只放标签，不放大白话——
// 整页级的解释由风险概要条承载，全页只出现一次，行内复读正是上一轮点名的硬伤）。
//
// 判定顺序有讲究：先看有没有私钥——孤儿 .pub 的 Guarded/GuardKnown 是 Go 零值
// （false/false），若不先分诊就会把"仅公钥"误报成"判不出来"。
//
// 三态**不许塌成两态**：`guardKnown === false` 时必须说"判不出来"且用独立档位，
// 既不能说"无口令"（把未知报成已知），也不能说"有口令"。后端同样保留第三态。
export function guardBadge(k: KeyEntry): GuardBadge {
  if (!k.hasPrivate) {
    return { tone: 'neutral', label: '仅公钥' }
  }
  if (!k.guardKnown) {
    return { tone: 'neutral', label: '判不出来' }
  }
  if (k.guarded) {
    return { tone: 'positive', label: '有口令' }
  }
  return { tone: 'warning', label: '无口令' }
}

// usedByOf 取"哪些清单主机在用这把"。**返回 null 表示"未判"，与空数组（判过、确实没人用）
// 严格区分**：引用判定要靠私钥路径比对文件身份，所以孤儿 .pub 行、清单读不动的整表都只能
// 得到空名单——若统一渲染成"清单里没有主机用它"，就是把"没判"说成"没人用"。
// 这句话正挂在删除决策现场，误说的代价是一次误删（本机已发生过一次删掉唯一可用凭据的事故）。
export function usedByOf(k: KeyEntry): string[] | null {
  if (!k.usedByKnown) return null
  return k.usedBy ?? []
}

export type KeyRisk = {
  total: number
  withPrivate: number
  unguarded: number
  unknown: number
}

// keyRisk 汇总整页的口令风险计数——"风险说明整页只说一次"的那条概要的数据源。
// 只统计有私钥的行（公钥不涉及口令，不计进风险分母）。
export function keyRisk(keys: KeyEntry[]): KeyRisk {
  const priv = keys.filter(k => k.hasPrivate)
  return {
    total: keys.length,
    withPrivate: priv.length,
    unguarded: priv.filter(k => k.guardKnown && !k.guarded).length,
    unknown: priv.filter(k => !k.guardKnown).length,
  }
}

export function useKeyring() {
  const keys = shallowRef<KeyEntry[]>([])
  const error = ref('')
  const lastLoaded = ref('')
  // stale：读失败但手里还有上一次的成功结果时置真。界面**保留旧表并明说它是旧的**，
  // 不清空假装"暂无"，也不靠降透明度表达（颜色/透明度单独承载状态是本仓红线）。
  const stale = ref(false)
  const { busy: loading, run } = useAsyncAction()

  async function refresh() {
    error.value = ''
    const res = await run(() => SshKeysAPI.SshKeysService.ListKeys())
    if (res.ok) {
      // 空数组是正常空态（新机还没有 ~/.ssh），不写成错误——写成错误会让人以为程序坏了。
      keys.value = res.data ?? []
      lastLoaded.value = new Date().toLocaleTimeString()
      stale.value = false
    } else {
      error.value = getErrorMessage(res.error)
      stale.value = keys.value.length > 0
    }
  }

  return { keys, loading, error, stale, lastLoaded, refresh }
}
