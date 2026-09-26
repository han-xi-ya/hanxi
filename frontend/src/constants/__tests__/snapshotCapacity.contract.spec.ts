// 快照容量口径跨语言契约（前后端双写 50/30 的防回潮锁）。
//
// 选型史：绑定现面（bindings checkpointservice.js 的 GetStatus→StatusInfo）没有
// 容量字段，且裁决不为这两个数新开 Wails 导出（避免总闸外再生通道）——前端为展示端，
// snapshotLabels.ts 双常量为全前端唯一数字源，后端 internal/snapshot/capacity.go
// 为全包唯一落点，同值双写由本 spec 焊死。形制循 wheelGeometry.contract.spec.ts：
// readFileSync + 正则提取，提取空集即红防假绿。
//
// 三层：
//  1. Go 源文本对账——capacity.go 的 maxListRevisions/backupKeepCount 与前端
//     REVISION_WINDOW/BACKUP_KEEP 逐值相等。改 Go 不改 TS 或反之，此处直接红；
//  2. 对账对象活性锁——service.go/git.go/backup.go 仍消费这两个符号，防"常量
//     沦为纯契约摆设（真实行为已改用别的数）"这类假绿漂移；
//  3. 前端单源回潮锁——快照三件套与 labels 的呈现语句里容量数字只许经常量插值，
//     源码文本不得出现"最近 50 版/保留 30 份"式裸数字文案（N33 批 C/D 收口的
//     防回退护栏；徽标 modeChip、windowCapNote/revisionCapNote 整句皆覆盖）。
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { REVISION_WINDOW, BACKUP_KEEP } from '../snapshotLabels'

const repoRoot = join(process.cwd(), '..')
const goSource = readFileSync(join(repoRoot, 'internal', 'snapshot', 'capacity.go'), 'utf8')

/** 提取 capacity.go 中 `name = 数字`（容忍行内注释与制表符），无产出即抛——防正则失效假绿。 */
function goIntConst(name: string): number {
  const match = new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)`, 'm').exec(goSource)
  if (!match) throw new Error(`capacity.go 未提取到常量 ${name}（Go 侧改名/删除/换写法，对账契约需同步检修）`)
  return Number(match[1])
}

// ── 第 1 层：Go 编译常量 ↔ 前端单源常量 ──

describe('快照容量 Go↔TS 双写对账（capacity.go 源文本）', () => {
  it('REVISION_WINDOW === maxListRevisions（观察窗 50 焊死）', () => {
    expect(goIntConst('maxListRevisions')).toBe(REVISION_WINDOW)
  })

  it('BACKUP_KEEP === backupKeepCount（备份份数 30 焊死）', () => {
    expect(goIntConst('backupKeepCount')).toBe(BACKUP_KEEP)
  })
})

// ── 第 2 层：对账常量仍是后端真实行为（活性锁） ──

describe('capacity.go 常量在后端消费点仍生效（防契约摆设化）', () => {
  for (const [file, symbol, minHits] of [
    ['service.go', 'maxListRevisions', 2], // GetStatus/ListRevisions/FileHistory 截窗
    ['git.go', 'maxListRevisions', 2], // log --max-count 与超窗报错文案
    ['backup.go', 'maxListRevisions', 1], // fileChanges 超窗回落
    ['backup.go', 'backupKeepCount', 2], // prune 滚动修剪
  ] as const) {
    it(`${file} 引用 ${symbol} ≥${minHits} 处`, () => {
      const src = readFileSync(join(repoRoot, 'internal', 'snapshot', file), 'utf8')
      const hits = src.match(new RegExp(`\\b${symbol}\\b`, 'g')) ?? []
      expect(hits.length, `${file} 对 ${symbol} 的引用骤减——常量可能已被旁路，需复核本契约`).toBeGreaterThanOrEqual(minHits)
    })
  }

  it('capacity.go 声明唯一：两常量各只在此定义一次（防散落回别的文件）', () => {
    expect(goSource.match(/^\s*maxListRevisions\s*=/gm) ?? []).toHaveLength(1)
    expect(goSource.match(/^\s*backupKeepCount\s*=/gm) ?? []).toHaveLength(1)
  })
})

// ── 第 3 层：前端呈现语句禁裸容量数字（单源回潮锁） ──

describe('快照前端容量文案全部经常量插值（无裸 50/30 回潮）', () => {
  // 形态：容量语义词后紧咬裸数字再跟量词（"最近 50 版""保留 30 份"）。
  // 常量定义行 `= 50` 与"30 个文件"这类计数句不在此形态内，不误伤。
  const bareCapRe = /(最近|展示|保留|观察窗)[^$\d]?\s*(50|30)\s*(版|份)/

  for (const rel of [
    ['src', 'constants', 'snapshotLabels.ts'],
    ['src', 'views', 'settings', 'SnapshotSection.vue'],
    ['src', 'views', 'settings', 'SnapshotTimeline.vue'],
  ] as const) {
    it(`${rel.at(-1)} 呈现文案零裸数字`, () => {
      const src = readFileSync(join(process.cwd(), ...rel), 'utf8')
      const hits = src.match(new RegExp(bareCapRe.source, 'g')) ?? []
      expect(hits, `容量数字绕过 snapshotLabels 单源：${hits.join(' / ')}`).toEqual([])
    })
  }

  it('三件套拉取与徽标确经常量（FileHistory limit 不写死）', () => {
    const src = readFileSync(join(process.cwd(), 'src', 'views', 'settings', 'SnapshotSection.vue'), 'utf8')
    expect(src).toContain('REVISION_WINDOW')
    expect(src).toContain('BACKUP_KEEP')
    expect(/FileHistory\([^)]*\d+\s*\)/.test(src), 'FileHistory 出现裸数字 limit').toBe(false)
  })
})
