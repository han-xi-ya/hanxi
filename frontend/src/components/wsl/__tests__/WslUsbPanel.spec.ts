import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WslUsbPanel from '../WslUsbPanel.vue'

const api = vi.hoisted(() => ({
  GetUsbOverview: vi.fn(),
  BindUsbDevice: vi.fn(),
  UnbindUsbDevice: vi.fn(),
  UnbindAbsentUsbDevice: vi.fn(),
  AttachUsbDevice: vi.fn(),
  DetachUsbDevice: vi.fn(),
  SetUsbAutoAttach: vi.fn(),
  SetUsbShare: vi.fn(),
  SetUsbShareEnabled: vi.fn(),
  RemoveUsbShare: vi.fn(),
  ReplayUsbNow: vi.fn(),
  OpenUsbipdReleases: vi.fn(),
}))

vi.mock('../../../../bindings/hanxi/internal/modules/wsl/wslservice', () => api)
vi.mock('../../../composables/useToast', () => ({ useToast: () => ({ showToast: vi.fn() }) }))
vi.mock('../../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: vi.fn(async () => true) }) }))
vi.mock('../../../composables/usePolling', () => ({ usePolling: (fn: () => unknown) => { void fn(); return {} } }))

const entry = {
  id: 'e1', busId: '2-3', instanceId: 'USB\\VID_AAAA&PID_0001\\SERIAL-A',
  vid: 'aaaa', pid: '0001', serial: 'SERIAL-A', guid: '', description: 'Serial A',
  distro: 'Ubuntu', enabled: true, addedAt: '2026-01-01 00:00:00',
}

function device(state: string, busId = '2-3') {
  return {
    busId, instanceId: entry.instanceId, vid: 'aaaa', pid: '0001', serial: 'SERIAL-A',
    guid: '', description: 'Serial A', clientIp: '', state, forced: false,
  }
}

async function statusFor(devices: ReturnType<typeof device>[]) {
  api.GetUsbOverview.mockResolvedValue({
    installed: true, version: '5.3.0', devices, ledger: [entry], autoEnabled: true,
    replayBusy: false, releasesPage: 'https://github.com/dorssel/usbipd-win/releases',
  })
  const wrapper = mount(WslUsbPanel, { props: { instances: [] } })
  await flushPromises()
  const ledgerTable = wrapper.findAll('table')[1]
  return ledgerTable.find('tbody tr').findAll('td')[3].text()
}

describe('WslUsbPanel 账本现态', () => {
  beforeEach(() => vi.clearAllMocks())

  it.each([
    ['attached', '已附加'],
    ['shared', '待附加'],
  ])('优先同 busid 后端现态 %s', async (state, expected) => {
    expect(await statusFor([device(state)])).toContain(expected)
  })

  it('空 busid 设备不可能误命中，呈现不在场', async () => {
    expect(await statusFor([device('shared', '')])).toContain('不在场')
  })
})
