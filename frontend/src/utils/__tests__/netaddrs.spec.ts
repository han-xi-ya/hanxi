// sortNetAddresses：真机以太网接口实拍 fixture——1 条 IPv4 + 12 条
// IPv6（含临时隐私扩展与 fe80）混列，IPv4 必须排到首位
//（本函数正是为治"IPv4 被十余条 IPv6 淹没"的呈现断点而生）。
import { describe, expect, it } from 'vitest'
import { sortNetAddresses } from '../netaddrs'

describe('sortNetAddresses', () => {
  it('真机形态：活动 IPv4 首位，链路本地/APIPA 垫底，组内保序', () => {
    const addrs = [
      '2408:8274:7015:1700:d59:fd85:ebfe:a649',
      '2408:8274:7014:8e00:4599:4f1a:459e:680d',
      'fe80::c23c:f5e2:dc34:1416',
      '192.168.1.31',
    ]
    const got = sortNetAddresses(addrs)
    expect(got[0]).toBe('192.168.1.31')
    expect(got[1]).toBe('2408:8274:7015:1700:d59:fd85:ebfe:a649') // 全局 v6 组内原序
    expect(got[2]).toBe('2408:8274:7014:8e00:4599:4f1a:459e:680d')
    expect(got[3]).toBe('fe80::c23c:f5e2:dc34:1416') // 链路本地垫底
  })

  it('APIPA 169.254 让位于活动 v4，排在链路本地之前', () => {
    const got = sortNetAddresses(['fe80::1', '169.254.232.254', '10.0.0.5'])
    expect(got).toEqual(['10.0.0.5', '169.254.232.254', 'fe80::1'])
  })

  it('空态与单条目回原样（视图对缺省有 — 兜底）', () => {
    expect(sortNetAddresses(undefined)).toEqual([])
    expect(sortNetAddresses(null)).toEqual([])
    expect(sortNetAddresses([])).toEqual([])
    expect(sortNetAddresses(['::1'])).toEqual(['::1'])
  })

  it('排序不改动原数组（渲染幂等）', () => {
    const src = ['fe80::2', '192.168.81.1']
    sortNetAddresses(src)
    expect(src).toEqual(['fe80::2', '192.168.81.1'])
  })

  it('大写 FE80 链路本地同样识得（rank 判定 lowercase 守卫），串形原样透出不改写', () => {
    const got = sortNetAddresses(['2408::1', 'FE80::abcd', '10.0.0.1', '169.254.1.1'])
    expect(got).toEqual(['10.0.0.1', '2408::1', '169.254.1.1', 'FE80::abcd'])
  })

  it('四档 rank 全矩阵：组间 rank 升序、组内严格保后端原序（稳定排序显式构造）', () => {
    const addrs = [
      'fe80::b', '169.254.1.2', '2408::b', '10.0.0.2',
      'FE80::a', '169.254.1.1', '2408::a', '10.0.0.1',
    ]
    expect(sortNetAddresses(addrs)).toEqual([
      '10.0.0.2', '10.0.0.1', // 活动 v4 组内原序（31 后出现的 1 仍居其后）
      '2408::b', '2408::a', // 全局 v6
      '169.254.1.2', '169.254.1.1', // APIPA
      'fe80::b', 'FE80::a', // 链路本地，大小写混写同档且串形不改
    ])
  })
})
