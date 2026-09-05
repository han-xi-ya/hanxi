// 特征测试：微信机器人主控台（组 G5，Phase 5 机械收编回归锁）。
// 锁定主干状态机：账号装载/切换、扫码绑定四态流转（wait→scaned→confirmed/expired）、
// 入站消息与 Token 事件改写、删除账号必经 useConfirm、文本发送与清屏、订阅/注销配对。
// Token/AESKey 等凭据值不进入任何断言。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import WechatBotView from '../WechatBotView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

// happy-dom 无 scrollTo，气泡滚动属视图行为而非本测试语义
; (window as unknown as { Element: { prototype: { scrollTo?: unknown } } }).Element.prototype.scrollTo = vi.fn()

const svc = vi.hoisted(() => ({
  ListAccounts: vi.fn(),
  GetLoginQRCode: vi.fn(),
  CheckQRStatus: vi.fn(),
  GetPendingMessages: vi.fn(),
  StartAccountListener: vi.fn(),
  StopAccountListener: vi.fn(),
  RefreshAccountContextToken: vi.fn(),
  UpdateAccount: vi.fn(),
  DeleteAccount: vi.fn(),
  SendTextMessage: vi.fn(),
  SendImageMessage: vi.fn(),
  SendFileMessage: vi.fn(),
  PickImageDialog: vi.fn(),
  PickFileDialog: vi.fn(),
  OpenInboundFile: vi.fn(),
  SaveInboundFile: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/wechat', () => ({ WechatService: svc }))

// qrcode 库走 canvas，桩为固定 dataURL（仅锁"取码成功→渲染 img"链路）
vi.mock('qrcode', () => ({
  default: { toDataURL: vi.fn(async () => 'data:image/png;base64,STUB') },
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

const acc = (id: string, name: string, extra: Record<string, unknown> = {}) => ({
  id, remarkName: name, ilinkUserId: `il-${id}`, targetUserId: '', baseUrl: '',
  isListening: false, contextToken: '', ...extra,
})

function stubs(accounts: unknown[]) {
  svc.ListAccounts.mockResolvedValue(accounts)
  svc.GetPendingMessages.mockResolvedValue([])
}

/** 纯微任务排空（fake timers 下 flushPromises 死锁，§25.3） */
async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(WechatBotView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  useToast().clearToast()
})

describe('WechatBotView 装载与账号', () => {
  it('空态占位可见并订阅双事件', async () => {
    stubs([])
    const { wrapper } = await mountView()
    expect(wrapper.find('.no-selection-placeholder').exists()).toBe(true)
    expect(svc.ListAccounts).toHaveBeenCalled()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['wechat:context-token-updated', 'wechat:message-received'])
    wrapper.unmount()
  })

  it('账号卡片渲染与建联徽章', async () => {
    stubs([acc('a1', '告警号', { contextToken: 'T', isListening: true }), acc('a2', '群发号')])
    const { wrapper } = await mountView()
    const cards = wrapper.findAll('.account-item-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].classes()).toContain('active')
    expect(cards[0].find('.token-status-badge').text()).toBe('已建联')
    expect(cards[1].find('.token-status-badge').text()).toBe('待激活')
    wrapper.unmount()
  })

  it('切换账号：选中态迁移且目标输入同步', async () => {
    stubs([acc('a1', 'A', { targetUserId: 'uA' }), acc('a2', 'B', { ilinkUserId: 'uB@im' })])
    const { wrapper } = await mountView()
    await wrapper.findAll('.account-item-card')[1].trigger('click')
    await nextTick()
    expect(wrapper.findAll('.account-item-card')[1].classes()).toContain('active')
    expect(wrapper.find('.target-input-inline').exists()).toBe(false)
    expect(wrapper.find('.target-val').text()).toBe('uB@im') // 头区原样展示，侧栏 sub 行才截 @ 段
    expect(wrapper.findAll('.account-item-card')[1].find('.account-sub-id').text()).toBe('uB')
    wrapper.unmount()
  })
})

describe('WechatBotView 扫码绑定状态机', () => {
  it('取码→scaned→confirmed：停轮询、重拉账号、800ms 后关窗并落系统消息', async () => {
    vi.useFakeTimers()
    stubs([acc('a1', '新号')])
    svc.GetLoginQRCode.mockResolvedValue({ qrcode: 'QR', qrcodeUrl: 'https://x/qr' })
    svc.CheckQRStatus
      .mockResolvedValueOnce({ status: 'scaned' })
      .mockResolvedValueOnce({ status: 'confirmed' })
    const { wrapper } = await mountView()

    await wrapper.find('.btn-bind-account').trigger('click')
    await flushMicrotasks(40) // 取码链为纯微任务（GetLoginQRCode→toDataURL→startQRPoll），不能用 runAllTimersAsync（常驻间隔会爆栈）
    expect(svc.GetLoginQRCode).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.qr-canvas-img').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(1500)
    await flushMicrotasks()
    expect(wrapper.find('.qr-badge-pill').text()).toBe('已扫码，请在手机上确认授权')

    await vi.advanceTimersByTimeAsync(1500)
    await flushMicrotasks()
    expect(wrapper.find('.qr-badge-pill').text()).toBe('绑定成功！已加入列表并启动监听')
    expect(svc.ListAccounts.mock.calls.length).toBeGreaterThanOrEqual(2)

    await vi.advanceTimersByTimeAsync(800)
    await flushMicrotasks()
    expect(wrapper.find('.custom-modal-backdrop').exists()).toBe(false)
    expect(useToast().toastMsg.value).toBe('新微信机器人绑定成功！')
    wrapper.unmount()
  })

  it('expired：提示刷新并重新取码', async () => {
    vi.useFakeTimers()
    stubs([acc('a1', 'A')])
    svc.GetLoginQRCode.mockResolvedValue({ qrcode: 'QR' })
    svc.CheckQRStatus.mockResolvedValue({ status: 'expired' })
    const { wrapper } = await mountView()
    await wrapper.find('.btn-bind-account').trigger('click')
    await flushMicrotasks(40)
    const firstCalls = svc.GetLoginQRCode.mock.calls.length
    await vi.advanceTimersByTimeAsync(1500)
    await flushMicrotasks()
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.GetLoginQRCode.mock.calls.length).toBeGreaterThan(firstCalls) // 自动重取
    wrapper.unmount()
  })
})

describe('WechatBotView 事件驱动', () => {
  it('入站文件消息渲染可操作卡片（downloadable 决定按钮禁用）', async () => {
    stubs([acc('a1', 'A')])
    const { wrapper } = await mountView()
    runtime.handlers['wechat:message-received']({
      data: { type: 4, fileName: 'report.pdf', fileSize: 2048, attachmentId: 'att1', downloadable: true, accountId: 'a1', time: '10:00:00' },
    })
    await nextTick()
    const card = wrapper.find('.inbound-file-card')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('report.pdf')
    expect(card.text()).toContain('2.0 KB') // 本地 formatFileSize 语义（非全局 fmtSize）
    const buttons = card.findAll('.file-action-btn')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].attributes('disabled')).toBeUndefined()

    runtime.handlers['wechat:message-received']({
      data: { type: 4, fileName: 'x.bin', attachmentId: 'att2', downloadable: false, attachmentError: '链接过期', accountId: 'a1', time: '10:01:00' },
    })
    await nextTick()
    const cards = wrapper.findAll('.inbound-file-card')
    expect(cards[1].text()).toContain('链接过期')
    expect(cards[1].findAll('.file-action-btn')[0].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('Token 事件触发账号重拉与系统胶囊', async () => {
    stubs([acc('a1', 'A')])
    const { wrapper } = await mountView()
    const before = svc.ListAccounts.mock.calls.length
    runtime.handlers['wechat:context-token-updated']({ data: { accountId: 'a1', fromUserId: 'u9' } })
    await flushMicrotasks()
    expect(svc.ListAccounts.mock.calls.length).toBeGreaterThan(before)
    expect(wrapper.find('.system-pill').text()).toContain('自动捕获到最新 Context Token')
    wrapper.unmount()
  })

  it('卸载注销全部订阅（2 订阅/2 注销配对）', async () => {
    stubs([])
    const { wrapper } = await mountView()
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })
})

describe('WechatBotView 操作', () => {
  it('删除账号必经确认：文案逐字入 title，取消不动后端', async () => {
    stubs([acc('a1', '告警号')])
    const { confirmState, settleConfirm } = useConfirm()
    const { wrapper } = await mountView()
    await wrapper.findAll('.action-icon-btn.danger')[0].trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定要解绑并删除微信机器人「告警号」吗？')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.DeleteAccount).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('确认删除：DeleteAccount + 重拉 + toast', async () => {
    stubs([acc('a1', 'A')])
    svc.DeleteAccount.mockResolvedValue(undefined)
    const { settleConfirm } = useConfirm()
    const { wrapper } = await mountView()
    await wrapper.findAll('.action-icon-btn.danger')[0].trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.DeleteAccount).toHaveBeenCalledWith('a1')
    expect(useToast().toastMsg.value).toBe('账号「A」已删除')
    wrapper.unmount()
  })

  it('文本发送：正常走 SendTextMessage；无目标账号被拦截', async () => {
    stubs([acc('a1', 'A', { targetUserId: 'u1' }), acc('a2', 'B', { targetUserId: '', ilinkUserId: '' })])
    svc.SendTextMessage.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    // 有目标：直发
    await wrapper.find('.wechat-textarea').setValue('你好')
    await wrapper.find('.btn-send-message').trigger('click')
    await flushMicrotasks()
    expect(svc.SendTextMessage).toHaveBeenCalledWith('a1', 'u1', '你好')
    expect(wrapper.find('.outbound-row').exists()).toBe(true)
    // 切到无目标账号：输入同步为空 → 拦截
    await wrapper.findAll('.account-item-card')[1].trigger('click')
    await wrapper.find('.wechat-textarea').setValue('第二条')
    await wrapper.find('.btn-send-message').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('请先填写目标微信用户的 User ID')
    expect(svc.SendTextMessage.mock.calls).toHaveLength(1)
    wrapper.unmount()
  })

  it('清屏只清当前会话并 toast', async () => {
    stubs([acc('a1', 'A')])
    const { wrapper } = await mountView()
    runtime.handlers['wechat:message-received']({ data: { type: 1, text: 'hi', accountId: 'a1', time: '1' } })
    await nextTick()
    expect(wrapper.findAll('.bubble-row.inbound-row')).toHaveLength(1)
    await wrapper.find('.toolbar-btn.text-danger').trigger('click')
    expect(wrapper.find('.flow-empty-box').exists()).toBe(true)
    expect(useToast().toastMsg.value).toBe('当前会话消息流已清空')
    wrapper.unmount()
  })
})
