import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import compositionContract from '../../../../scripts/fixture/composition_contract.json'

const bindingsRoot = join(process.cwd(), 'bindings', 'hanxi', 'internal')

function exportedFunctions(relativePath: string): string[] {
  const source = readFileSync(join(bindingsRoot, relativePath), 'utf8')
  return [...source.matchAll(/export function (\w+)\(/g)].map((match) => match[1])
}

describe('generated bindings composition contract', () => {
  for (const [relativePath, requiredExports] of Object.entries(compositionContract.bindings)) {
    it(`${relativePath} 保留关键导出`, () => {
      const actual = exportedFunctions(relativePath)
      expect(actual).toEqual(expect.arrayContaining(requiredExports))
    })
  }
})
