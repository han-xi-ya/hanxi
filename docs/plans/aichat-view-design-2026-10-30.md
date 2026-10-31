# AiChatView 施工级设计稿（AI 聊天前端视图）

> 状态：设计定稿，可直接开工。后端契约已定（服务 aichat，RPC 快返回 + 事件流回包），本稿只覆盖前端。
> 本稿所有"仓内先例"均已逐字核对过源码，路径照抄即可找到。
> 参照件（均为只读参照，不改）：
> - 编排壳分离先例：`frontend/src/views/WechatBotView.vue` + `frontend/src/composables/useWechatBot.ts` + `frontend/src/components/wechatbot/`
> - 通道分家/外发确认教训：`frontend/src/views/JsonFmtView.vue`（`extractByAI` 的 aiBusy/aiAsking/aiError/aiNote/aiAdopted/reqSeq）
> - 滚动机构单源：`frontend/src/composables/useLogTails.ts`（autoScroll 闩 / 40px 贴底阈值 / trimToCap 裁头 / nextTick 落底）
> - rAF 合帧先例：`frontend/src/views/FrpcProjectsView.vue`（logPending + scheduleLogFlush，行 407–509）
> - 输入框 rows 3/8 收展：`frontend/src/views/MsgBoardView.vue`（draftExpanded）
> - Enter 发送/Shift+Enter：`frontend/src/components/wechatbot/WechatBotChatInput.vue`（handleKeydown）
> - 设计语言：`.claude/skills/hanxi-workbench-ui/SKILL.md` + `references/design-system.md`、`component-patterns.md`、`quality-checklist.md`
> - 测试命名与打桩姿势：`frontend/src/views/__tests__/JsonFmtView.spec.ts`、`MsgBoardView.spec.ts`（Events 桩）、`FrpcProjectsView.spec.ts`（nextFrame）

---

## 0. 契约面与登记触点（先钉死，再动工）

### 0.1 后端 RPC / 事件（已定，前端照此消费）

```
RPC（bindings 生成后同步期可用，快速返回，回复一律走事件）：
  CreateSession(title: string) → sessionId
  ListSessions() → Session[]            // { id, title, state? }（state 若缺省，前端按 idle 起步）
  Rename(sessionId, title)
  CloseSession(sessionId)
  SendMessage(sessionId, text)

事件（useWailsEvent 订阅，载荷均为单对象）：
  aichat:session-state  { sessionId, state: 'working'|'idle'|'blocked'|'done'|'failed' }
  aichat:timeline       { sessionId, item: { kind: 'user'|'assistant'|'thinking'|'tool'|'error'|'note',
                                             text: string, ts: number, turnId: string } }
                        // 逐 token 后端已按帧合并；前端仍做二次合帧 + 同轮同 kind 续接（见 §3.3）
  aichat:turn-stats     { sessionId, turnId, elapsedMs, bytesIn, bytesOut }
```

### 0.2 绑定导入假设（落地时验证一次，不是待定）

按仓内先例（jsonfmt：`frontend/bindings/hanxi/internal/modules/jsonfmt/index.js` 导出 `JsonFmtService`），aichat 的绑定应为：

```ts
import * as AiChatAPI from '../../bindings/hanxi/internal/modules/aichat'
import type { ... } from '../../bindings/hanxi/internal/modules/aichat/models'
// 服务对象名假定 AiChatService（与 MsgBoardService/JsonFmtService 同族命名）
```

- 后端生成物落盘前，**spec 不受阻**：`frontend/vitest.config.ts` 的 `missingBindingSeam` 已为不存在的 `bindings/hanxi/**` 导入解析空壳，vi.mock 照常覆盖（JsonFmtView.spec 头注在册的同一条路）。
- 若后端最终导出名不同（如 `Service` 或包路径在 `packages/go/aiagent`），只改 import 行与 spec 的 vi.mock 路径，本稿其余设计不动。开工首日先 `ls frontend/bindings/hanxi/internal/modules/aichat/` 对一次名字。

### 0.3 路由/导航登记（单一清单 `frontend/src/constants/navigation.ts`，三处）

```ts
// ROUTES：
'/ext/aichat': { component: defineAsyncComponent(() => import('@/views/AiChatView.vue')), moduleId: 'aichat' },
// MODULE_GROUP：
aichat: 'developer',
// MODULE_PRESENTATION：
aichat: { icon: 'i:bot', route: '/ext/aichat' },   // 'bot' 矢量已在册（SETTINGS_SECTIONS 的 AI 接入分区在用）
```

模块停用弹回、EnsureModuleActive 门禁由 App.vue 依 `moduleId` 自动接管，视图内零门禁代码。

### 0.4 契约缺口与如实处理（一个已知空洞，明说不臆造）

契约**没有** GetHistory/ListMessages 类 RPC：切换会话时，前端只能展示**本次应用运行期间**收到的 timeline 事件（composable 按 sessionId 分仓缓存，切回即见缓存；关闭会话即删仓）。首挂 ListSessions 只回列表不回历史。
- 空历史时消息流空态文案：**「该会话的历史消息不在本机内存里——本次运行内收发的内容会保留在这里」**（对齐 quality-checklist"空态要解释为什么空、下一步是什么"）。
- 若后端后续补 `GetSession(sessionId)→items`，接入点就在 `selectSession()` 里加一次拉取 + `keepLast(items, FLOW_CAP)`，其余不动。此为扩接口预留，不是本期实现项。

---

## 1. 组件树与每块职责

拆件判断：本视图有三块独立复用/独立复杂度的 UI（会话栏、消息流、输入区），且 WechatBotView 已确立"编排 composable + components/<module>/ 纯展示壳 + 视图薄接线"的仓内形制；不拆违背惯例，拆得过碎（每种 kind 一个组件）才是平行系统。**定为 1 视图 + 1 composable + 3 子件。**

```
frontend/src/views/AiChatView.vue                    视图壳（接线 + 布局 + scoped 样式）
frontend/src/composables/useAiChat.ts                编排单源（状态机、事件订阅、rAF 合帧、RPC 序列、确认策略）
frontend/src/components/aichat/AiChatSessionList.vue 左栏会话列表（纯展示 + 事件上抛）
frontend/src/components/aichat/AiChatFlow.vue        右栏消息流（纯展示 + defineExpose scrollToBottom）
frontend/src/components/aichat/AiChatInput.vue       底部输入区（v-model + send/expand 事件）
frontend/src/views/__tests__/AiChatView.spec.ts      视图特征测试（清单见 §7）
```

### 各块职责边界（照 useWechatBot 的分工口径）

**AiChatView.vue（壳）**
- 只做：调 `useAiChat()` 解构、props/事件接线、页面骨架（PageHeader + 双栏 grid + 窄档降级）、`loading/errorMsg` 页面级 state-box（照 MsgBoardView 的 `.state-box` / `.state-error` 加载闸口形制）。
- `onMounted` 触发 `refreshSessions()`（ListSessions 装载）；挂载期间事件订阅在 setup 同步期已生效（useWailsEvent 注册点在 setup，不在 onMounted——该文件头注的"早期事件不丢"契约）。
- 纯 UI 折叠态留在本地（如左栏收起开关），不上了收 composable（useWechatBot 头注同款分界）。

**useAiChat.ts（编排单源）**
- 全部业务状态与操作：sessions、timeline 分仓、sessionStates、sending/asking 位、确认策略、rAF 帧调度、turn-stats 映射、列表刷新 reqSeq 门禁。
- 必须组件 setup 同步期内调用（内部 useWailsEvent ×3 订阅 + onScopeDispose 配对）。
- 导出类型：`ChatKind`、`TimelineItem`、`SessionMeta`、`TurnStats`（外部组件以 type 引入，同 `ChatMessage` 之于 WechatBotChatFlow）。
- 呈现函数放消费组件就近（§9.6 口径，useWechatBot 头注在册的先例）：时间格式化、turn-stats 格式化归 AiChatFlow.vue 本地。

**AiChatSessionList.vue（左栏）**
- props：`sessions: SessionRow[]`（含派生字段 state/hasUnread/sending）、`currentId: string`。
- emits：`select(id)` / `create()` / `rename(id)` / `close(id)`。
- 每行 = 资源列表行形制（component-patterns「Resource list」）：标题（truncate, min-width:0）+ 状态字 chip + 未读点；行内二级动作钮（重命名/关闭）为常显微钮（checklist："动作不得只随 hover 出现"），不嵌套按钮进整行点击。
- 头部：「新建会话」primary 小钮 + 计数（`共 N 个会话`）。

**AiChatFlow.vue（右栏消息流）**
- props：`items: TimelineItem[]`、`stats: Record<turnId, TurnStats>`、`state: SessionState`（决定"思考中"占位与 thinking 折叠头的实时字）。
- 不感知 sessionId——composable 只喂当前会话的数据。
- 滚动容器 `ref=container` 挂 `chat-scroll`，`defineExpose({ scrollToBottom })`（照 WechatBotChatFlow 的句柄契约：编排层经 handle 触达，nextTick 时序即契约）。
- 贴底脱锁时的悬浮「↓ 回到最新消息」钮也放这里（它知道容器；点击 = `scrollToBottom(true)` + 由 composable 清未读计数）。

**AiChatInput.vue（输入区）**
- props：`modelValue: string`、`disabled: boolean`、`disabledReason: string`（给 title/提示字）、`expanded: boolean`（收展态可由本地持有也可上收，见下）、`sessionOk: boolean`。
- emits：`update:modelValue` / `send` / `toggle-expand`。
- 内部零业务判断，`canSend` 只从 props 组合（`!disabled && modelValue.trim()`）；业务禁发条件全部由 composable 算好传入——与 WechatBotChatInput「普通输入、判断上抛」的分工一致。
- 收展态 `expanded` 由视图本地 ref 持有（纯 UI，不上收 composable）。

### 数据形状（composable 内定义，全 export）

```ts
export type SessionState = 'idle' | 'working' | 'blocked' | 'done' | 'failed'
export type ChatKind = 'user' | 'assistant' | 'thinking' | 'tool' | 'error' | 'note'

export interface TimelineItem {
  id: number          // 单调 seq（全模块一个计数器），v-for 稳定 key——照 frpc logRowSeq 口径
  kind: ChatKind
  text: string
  ts: number          // 后端原样
  turnId: string
}

export interface SessionRow {
  id: string
  title: string
  state: SessionState       // 未见事件前恒 'idle'
  sending: boolean          // SendMessage RPC 在途
  asking: boolean           // 外发确认框待决
  sendError: string         // 本会话 RPC 失败通道（与 turn error 分家）
  confirmedSend: boolean    // 本会话已完成首次外发确认（§4）
  unread: number            // 脱锁期间新增条目数（§3.4）
}
```

内部存储（不直接暴露形状，暴露派生视图）：
- `sessions = ref<SessionRow[]>([])`
- `timelines: Map<string, TimelineItem[]>`（值层 `shallowRef` 换表驱动渲染，**不是**逐条深层 reactive——frpc perf 改造的同一教训：高频 append 走整表置换 `timelineVersion`，见 §3.3）
- `turnStats: Map<string, Map<string, TurnStats>>`（sessionId → turnId → stats）

---

## 2. 状态机：会话状态 × 在途消息 × 错误通道

### 2.1 分通道总表（JsonFmtView 教训的直接移植，通道名对齐其语义）

| 通道 | JsonFmtView 对应 | aichat 形态 | 载体 |
|---|---|---|---|
| RPC 在途 | `aiBusy` | `sending`（每会话） | 输入区"发送中…"，发送钮禁用 |
| 确认待决 | `aiAsking` | `asking`（每会话） | 先闸后问：弹窗期间钮已禁，双击≠两发外发 |
| 传输层失败 | `transportError` | 页面级 `listError`（List/Create/Rename/Close 抛） | 顶部 `.state-box.state-error` / banner-error |
| 回合业务失败 | `aiError` | 每会话 `sendError`（SendMessage 抛）+ timeline `kind=error` 项（回合内失败）+ `state=failed` | 输入区上方 slim banner-error（说"这一句没出去"）；流内 error 项（说"这一轮断了"） |
| 过期未采用 | `aiNote` / `aiAdopted` | **判断结论：原样场景不适用，但有两个等价门禁**（见 2.3） | — |

三条失败通道必须分家的原文由：SendMessage 抛 ≠ 回合失败 ≠ 列表拉不到。前两者都在"这一条消息"语境，第三者是"整个页面数据面"。混写会导致用户去查错的地方（JsonFmtView 注释原话："AI 那条要去看环境……不能因为 AI 不行就把用户已经拿在手里的东西砸掉"——同理，一轮 failed 不得清掉已渲染的 items，一次 listError 不得清掉已选会话的流）。

### 2.2 会话状态迁移图（state × sending × 输入可用性）

```
                 ┌───────────────────────────── 会话不存在 ─┐
                 │ CreateSession 成功                        │ CloseSession 确认
                 ▼                                           ▼
              [idle] ◄────────────── aichat:session-state(idle) ─────────────┐
   │ 用户按发送（首条先过 §4 确认；禁发位任一命中则不可按）                      │
   │ SendMessage RPC 接受 → sending=true，乐观上屏 user 项                    │
   ▼                                                                        │
 [idle+sending] ──RPC 抛──► [idle, sendError]（保留乐观 user 项？见下"抛错处理"）│
   │ RPC 返回 + 事件 working                                                │
   ▼                                                                        │
 [working]（sending 可已摘——事件权威）── timeline: assistant/thinking/tool 流进表
   │                                                                        │
   ├─► [idle]     （一轮完成且后端归位 idle）          ────────────────────┤
   ├─► [done]     （一轮完成，显式收尾）               ────────────────────┤
   ├─► [blocked]  （等待用户补充/外部条件）─ 输入仍可用，可再发一句解锁 ──────┤
   └─► [failed]   （本轮失败，配套 kind=error 项在场）─ 输入可用（再发=重试）─┘
```

**每个状态 × 输入区可用性矩阵（写进 composable 的唯一 computed）**

| state | sending | asking | 无当前会话 | 可发送？ | 输入区文案 |
|---|---|---|---|---|---|
| idle | false | false | 否 | ✅（首轮先过确认） | `发送 (Enter)` |
| working | * | * | 否 | ❌ 在途禁发 | `AI 回复中…` |
| blocked | false | false | 否 | ✅（补充信息正是解锁动作），chip 警示「上一轮挂起等待处理」 | `发送 (Enter)` |
| done | false | false | 否 | ✅ | `发送 (Enter)` |
| failed | false | false | 否 | ✅（新回合 = 重试） | `发送 (Enter)` |
| * | true | * | 否 | ❌ | `发送中…` |
| * | * | true | 否 | ❌ | `等待确认…` |
| * | * | * | 是 | ❌（空态引导去新建） | `发送`（灰，title=「先新建会话」） |

- 文本 `trim()` 为空恒不可发（不参与上表）。
- **乐观上屏口径**：SendMessage RPC 调用发起的同时就把 `kind:'user'` 项写入本地 timeline（id 本地生成、turnId=''）——契约保证后端也会推同内容 user 事件；前端按 `(turnId, kind, text)` 与队尾 user 项对得上时**吞掉事件重复项**（去重规则见 §3.3 第三条）。RPC 抛错则把该乐观 user 项标记 `kind:'error'` 追加一项「这句话未送达：{err}」，乐观项保留（用户发过什么必须留在屏上，这是仓内"失败不清屏"纪律，JsonFmtView 同款）。

### 2.3 "过期未采用"场景适用性判断（题设要求逐条裁）

JsonFmtView 的 aiNote 本质：**慢 RPC 的结果可能在几十秒等待期后作废**（原文改了/序号过期）。aichat 的架构差异是"RPC 快返回、回复走事件"，逐场景裁：

1. **回合结果作废重播（aiNote 原型）——不适用。** 回复以事件流按 sessionId 归属，用户切会话/关闭输入框都不能使在途回合"对不上号"；回合属于会话而非输入框。不得为"会话已切走"丢弃事件——后台会话照常进表（并计 unread），这正是多会话的意义。
2. **ListSessions 刷新乱序——适用，且需要 reqSeq 门禁。** 与 JsonFmtView `reqSeq` 同族（PortScan/LanScanner 先例同款）：快速点刷新/模块重启后的多次触发可让两发在途并存；晚到的旧列表**不得整表回灌**覆盖本地已有 sessions 的运行态（state/sending/unread 以本地为权威、title 以新表为准做合并；`seq !== listSeq` 直接丢弃整发——合并语义定死为"新表覆盖 title+集合增删，运行态字段本地保留"）。
3. **CloseSession 后迟到事件——适用，处置为"丢弃不复活"。** 会话从列表移除即删仓（timeline/stats 释放）；随后到达的任何该 sessionId 事件（session-state/timeline/turn-stats）一律作废（找不到仓就丢，静默，不弹错——对已关闭会话播报失败没有意义，参照 JsonFmtView「用户已经改走，这次失败的播报没有意义」同款判断）。
4. **aiAdopted（AI 成果对本地旧发的覆盖优先权）——不适用。** aichat 无"两条来源通道写同一屏"的结构，事件是唯一写入通道。

### 2.4 生命周期与清理

- 订阅：`useWailsEvent` ×3（aichat:session-state / aichat:timeline / aichat:turn-stats），setup 同步期注册、作用域销毁自动注销（useWechatBot 注释在册的"2 订阅/2 注销配对，勿增删改同名订阅"纪律同样适用：3 订阅/3 注销）。
- 定时器/帧：rAF 调度位 `flushScheduled`（bool，无句柄需取消——同 frpc 形态：帧里自查标志；组件卸载后 Wails 注销已断流，残帧至多一次空 flush，无害，注明即可）。
- KeepAlive 常驻（max=10）：视图停用**不**注销监听——但事件 handler 只写 Map 与响应式位，不触达 DOM；激活时滚动/未读自然回算。与 MsgBoard「页面隐藏时不监听 Esc」的教训反向匹配（这里没有 window 级监听），无需额外处理；**禁止**在 onDeactivated 里做任何"暂停事件消费"的小聪明——回合状态机会因此错位。

---

## 3. 消息流渲染

### 3.1 虚拟滚动：**不做**（含判断依据，写进文件头注防回退）

对比结论逐条：
- `useLogTails` 的 `maxLines + trimToCap` 姿势就是仓内对"高频流式长列表"的既有答案：frpc 抽屉 2000 行、ddns-go 400 行，均无虚拟滚动，瓶颈在"每行一渲染批次"，解法是 rAF 合帧 + 稳定 key + 原地裁头，而不是窗口化渲染。
- 聊天项更高（一屏十几到几十行），但**条数上限远低于日志**：定 `FLOW_CAP = 600` 条/会话。600 项 DOM 在 webview 内一次性渲染与增量 patch 都无压力（wechatbot 消息流 300 裁至 200 的先例都没上虚拟滚动）。
- 无现成件：package.json 无 vue-virtual-scroller（dependencies 实查仅 @vueuse/core、@wailsio/runtime、qrcode、vue）；自造虚拟滚动 = 引入第二套渲染系统，违背 SKILL.md「只在复用/复杂度合理时才抽组件、勿造平行系统」。
- **钉死的护栏**：超限 `trimToCap` 保尾弃头 + v-for 用 `item.id` 稳定 key（不用 :index——frpc perf 改造注释原话：":index key 使满仓裁剪整表换键重补丁"）。若未来实测 600 项仍卡（不可能但要留话），第一杠杆是降 FLOW_CAP，不是引库。

### 3.2 贴底跟随 / 上滚脱锁：直接复用 `useLogTails` 机构

该件头注自称"只管机构不管数据"：autoScroll 闩、`isNearBottom`（40px 阈值）、`onBodyScroll`（`@scroll.passive` 挂法）、`scrollToBottom()`（nextTick + 闩控落底）、`trimHead/keepLast`——聊天流语义与之逐字吻合（"距底≤40px 视为在跟尾"对消息流同样成立）。**直接 `useLogTails({ maxLines: FLOW_CAP })` 消费**，不复制不仿写。这是该件的非日志首消费；若台账认为"日志面板家族"名实需扩，允许把注释补一行"aichat 消息流为第三消费方"，代码零改动。

增量：
- 脱锁期间累计 `unread`（当前会话新增 timeline 条数），悬浮钮文案 `↓ 回到最新消息（N 条新内容）`；落底（闩重新为真）即清零。unread 计数在 composable 的 flush 里做：`if (当前会话 && !autoScroll) unread += 本批条数`。
- 发送成功后无条件 `scrollToBottom()`（用户自己的动作必须被看到，哪怕此前在翻旧账）——这是对闩的一次显式越过：先 `autoScroll.value = true` 再滚（"我发的消息我得看见"，随后用户再上滚自然脱锁）。
- 切换会话：`autoScroll.value = true` + nextTick 落底（进一个会话默认看最新，不问旧事）。
- `prefers-reduced-motion`：仓内全局契约已在 base.css 兜底（design-system 必含块），滚动一律 `behavior:'auto'`（与 WechatBotChatFlow `scrollToBottom(smooth=false)` 默认一致，本视图不开 smooth——流式跟随时 smooth 会自我打断）。

### 3.3 token 级增量怎么合帧：逐字照搬 frpc 的 pending + rAF 形态

后端虽已按帧合并（事件到达频率 ≤ 帧率量级），但**事件回调仍不得直接触发渲染**——一个回合高吞吐时 Wails 回调每帧可能落 2–3 个事件，Vue 每事件一批渲染就是 frpc 改造前的病。前端姿势（composable 内）：

```ts
let pending: Array<{ sessionId: string; item: TimelineItem 的草稿 }> = []
let flushScheduled = false

useWailsEvent<TimelineEvent>('aichat:timeline', (ev) => {
  if (!ev?.sessionId || !timelines.has(ev.sessionId)) return   // §2.3-3：未在册会话的事件直接作废
  pending.push(ev)                                             // 回调路径 O(1)
  scheduleFlush()
})

function scheduleFlush() {
  if (flushScheduled) return
  flushScheduled = true
  requestAnimationFrame(() => {
    flushScheduled = false
    if (pending.length === 0) return
    const batch = pending; pending = []
    // 1) 按 sessionId 分组；会话已删仓的组丢弃
    // 2) 会话仓内续接/去重合并（下述三条规则）
    // 3) timelines.set(sid, trimToCap(merged, FLOW_CAP))
    //    ——整表置换 shallowRef 版本号自增触发渲染（照 frpc `logRows.value = ...concat` 同款）
    // 4) 仅当前会话有新增且 autoScroll 开 → scrollToBottom()；否则计 unread
  })
}
```

**进表三条合并规则（钉死，spec 逐条锁）**：
1. **续接**：入表项与本会话队尾项 `(turnId, kind)` 全等 → `text` 追加到队尾项（生成新对象替换队尾，不原地改字符串项以外字段），不新增条。token 流的"一条长回答"在 DOM 里恒为一个气泡。
2. **换项**：kind 或 turnId 变化 → push 新 item（新 id 取全局单调 seq）。thinking→assistant→tool 的切分是后端事实，前端只跟随不推断、不猜测回合边界。
3. **user 去重**：乐观项在队尾时，后端回推的 `kind:'user'` 事件若 `text` 与队尾乐观项逐字相等且 turnId 能对上（乐观项 turnId 为空串时以后端 turnId 回填视为对上）→ **吞掉**事件项、仅回填 turnId；不相等（比如后端做了改写/截断）→ 以事件为准替换乐观项文本（后端是唯一真相，仓内"回读定脏"同源哲学）。

turn-stats 不进 rAF 通道（频率是每轮一条，非流式），handler 内直写 `turnStats` Map。

### 3.4 各 kind 的渲染形制（AiChatFlow 模板定稿）

| kind | 形制 | token |
|---|---|---|
| user | 右气泡，宽度 ≤80%，`pre-wrap` | 底 `--color-primary-soft`、边 `color-mix(in srgb, var(--color-primary) 25%, var(--color-border))`（design-system"强调边混 25–30% primary"档）、字 `--color-text` |
| assistant | 左气泡，正文默认 `--text-base`/1.6 行高，`white-space: pre-wrap; user-select: text`（wechat 流同款）。**不引入 Markdown 渲染器**：无依赖、`v-html` 是安全红线（JsonFmtView 的 v-html 仅限自产转义片段），纯文本 + 等宽留给 tool/code 类项即可 | 底 `--surface-panel`、边 `--color-border` |
| thinking | 每轮一个 `<details>`（默认收起），summary 固定字「思考过程」+（working 时前缀 live-pulse 点 + 「进行中」）；内容为 pre-wrap 灰字 | `--surface-soft`、`--color-text-subtle`、左边框 2px `--color-border-strong` |
| tool | 单行流式条：`kind 标 chip「工具」+ 文本单行 truncate，title 全量` | surface-soft 条 + `--font-mono` `--text-sm` |
| error | 内联 slim banner：`.banner.banner-error` 形态（UiBanner tone=error 复用全局原子），字色/底色已由原子管 | — |
| note | 居中 system 胶囊（照 WechatBotChatFlow `.system-pill` 形制，样式自绘不 import 其 scoped） | `--surface-hover` 底 + `--color-text-muted` |

- 每项头部时间 `fmtTime(ts)` = `new Date(ts).toLocaleTimeString()`（10–12px subtle、mono，`--text-micro/--text-xs` 档）。
- turn-stats 行：渲染在该 turnId **最后一项之后**（computed 预排出 `statsAnchorId` 集合），文案纯函数 `formatTurnStats`：`耗时 {elapsedMs<1000? `${ms}ms` : (ms/1000).toFixed(1)+'s'} · 入 {fmtKB(bytesIn)} · 出 {fmtKB(bytesOut)}`；数字一律 mono + `font-variant-numeric: tabular-nums`，**前端只做单位换算不做二次统计**（JsonFmtView"数字全部原样取后端字段"口径）。
- 在途占位：`state==='working' && 队尾不是本轮 assistant/thinking 项` 时在流尾渲染 `.state-box[role=status]` + live-pulse，文案 `AI 正在回复…（一轮往返几十秒属正常）`（JsonFmtView aiBusy 文案教训：耗时量级说出来，否则像卡死）。

### 3.5 页面骨架（容器查询降级）

```
.page-fluid.aichat-page（PageHeader：标题「AI 聊天」+ 副标题一句「与会话式本机 AI 问答；内容将离开这台机器」）
└─ .ac-desk  container: ac / inline-size
   ├─ grid: clamp(240px, 22cqi, 300px) | minmax(0,1fr)   （≥840 双栏，左栏宽度封顶不吃对话区）
   ├─ < 840：会话列表收为顶部一行横滑 chips（标题+状态点，选中描边），消息流在下——
   │   不照抄 wechat 的整栏堆叠（栏高会吃掉整个视口）；再窄 chips 行自身横向滚动（component-patterns 面包屑同款"窄屏横滚不折行"纪律）
   └─ 右栏内部：[toolbar 行 40px：会话标题 + 状态 chip + sr-only role=status 播报位]
                [AiChatFlow flex:1 min-height:0 overflow:auto]
                [sendError slim banner（存在才渲染）]
                [AiChatInput]
```

高度：右栏整体 `min-height: 0` + flex 列（消息流是内部唯一滚动容器，页体不产生纵向滚动——checklist"宽表/日志/代码滚在自己容器里"）。

---

## 4. 首次外发确认（useConfirm 复用，文案定稿）

**策略**：以**会话**为确认单位——每个新会话的第一次发送前弹一次；该会话内后续轮次不再弹（context 已外发过，重复询问只剩骚扰）；`confirmedSend` 位随会话生灭（Close 后新建即新确认）。理由逐字写进 composable 头注：会话=一次全新的模型上下文暴露，知情同意的粒度应与暴露的粒度一致；全局"只问一次/应用期"会静默吞掉新会话的告知义务。

**文案草案**（对齐 JsonFmtView `extractByAI` 的三条硬口径：说清"离开本机"、按实测说耗时、给后果；spec 将逐词钉）：

```ts
const accepted = await confirm({
  title: '把这句话交给本机 AI？',
  description:
    `消息连同本会话之前的所有轮次都会进入模型会话——内容会离开这台机器，不只是本机处理。` +
    `一轮回复按几十秒算（本机实测：一句照抄 4.4 秒，一次复杂提取 66 秒+），期间可以切去看别的会话，` +
    `回复会以事件流回到本会话。发送后本条无法撤回。`,
  confirmLabel: '发送给 AI',
  cancelLabel: '再想想',
  tone: 'warning',
})
```

（实测数字取自 `packages/go/aiagent/agent.go` 头注在册值 4.4s/66.6s——用仓内已验证事实，不是编的；后端若改了口径数字随动，spec 只钉「离开这台机器」与「几十秒」两个短语。）

**闸序（先闸后问，逐字移植 aiAsking 教训）**：

```ts
async function send() {
  const sid = currentId.value
  const text = inputText.value.trim()
  if (!canSend(sid, text)) return                    // §2.2 矩阵唯一 computed
  if (!session.confirmedSend) {
    session.asking = true                              // 先置闸再弹窗
    const ok = await confirm({...})
    session.asking = false
    if (!ok) return
    session.confirmedSend = true
  }
  session.sending = true
  session.sendError = ''
  appendLocalUserItem(sid, text)                       // §2.2 乐观上屏
  inputText.value = ''
  forceScrollToBottom()
  try { await AiChatAPI.AiChatService.SendMessage(sid, text) }
  catch (err) { session.sendError = getErrorMessage(err); appendLocalErrorItem(sid, err) }
  finally { if (currentSessionStillExists(sid)) session.sending = false }
}
```

- 待决期间第二发 Enter/点击都被 `asking` 闸住（JsonFmtView「确认框开着时连点两下，也只许外发一次」同款用例必须进 spec）。
- RPC 快速返回，`sending` 生命周期毫秒级；真正长的是 `state==='working'`——输入禁用**同时**看两者（`sending || state==='working'`），不留"RPC 已回、事件未至"的空窗双击面。

---

## 5. 输入区规格（AiChatInput.vue）

- **textarea rows 3 / 展开 8**：照 MsgBoardView `draftExpanded` 姿势——`expanded = ref(false)`（视图本地），`:rows="expanded ? 8 : 3"`，旁边 `.btn.btn-ghost.btn-small` 微钮「展开/收起」；`resize: vertical` 保留原生拉高兜底（MsgBoard `.mb-text` 同款）。
- **键盘**：`keydown` 里 `if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); emit('send') }`（WechatBotChatInput 同形），**并加 `if (e.isComposing) return` 前置守卫**——中文输入法组合期回车是"确认候选词"不是发送；先例件没防这个，属已知潜在误发面，本视图钉死（spec 锁一条）。禁用态下 Enter 同样不触发（canSend 组合位传入，textarea 不禁用——**不 disable textarea 本身**：禁发时用户要能继续改草稿、上翻旧消息复制粘贴；只有 send 钮真禁，`disabledReason` 进钮 title）。
- **禁发条件**（全部在 composable 的 `canSend` 单点计算，见 §2.2 矩阵）：文本空/纯空白 | `sending` | `asking` | `state==='working'` | 无当前会话。空态下按钮文案不变只置灰，title 说明原因（`先新建会话` / `AI 回复中` / `等待确认中` / `请输入内容`）。
- **剪贴板约定裁量**：按 component-patterns 的 clipboard 条，聊天输入主语义是打字不是"接收粘贴的载荷"，原生 Ctrl+V 可用，**有意不加「从剪贴板粘贴」钮**（该约定自限"短结构输入不加=噪音"，此处同理）；决定写进组件头注防后来者"补一致性"。
- 底部提示行：左 `Enter 发送 · Shift+Enter 换行`（`--text-xs --color-text-subtle`），右发送钮 `UiButton variant="primary" :disabled`。三态文案：默认 `发送 (Enter)`；`sending` `发送中…`；`state==='working'`（非 sending 但回合在途）`AI 回复中…`。按钮 `min-width` 固定（checklist：busy 控件尺寸不动——按 `--text-md` 四字宽钉）。
- 输入文本**不持久化**（无草稿后端；KeepAlive 停用再回，文本仍在组件实例内存里，可接受，注明）。

---

## 6. 键盘可达性与暗色主题 token 注意点

### 6.1 键盘/a11y

- 焦点序：新建钮 → 会话行（每行主体 button + 行内 重命名/关闭 常显 button）→ 消息流（容器 `tabindex="0"` + `role="log"`，可键盘滚动）→ 输入区 → 发送钮。全走原生 button/textarea，无自造 role。
- **aria-live 分层（checklist："有意义的状态变化用，高频指标不用"）**：消息流本体**不挂** aria-live（token 流会淹死读屏）；工具条放 `.sr-only` 的 `<span role="status" aria-live="polite">`，只在**回合级迁移**时写一句（`AI 开始回复` / `回复完成` / `本轮失败：{err 首行}` / `上一轮挂起等待处理`），由 composable 在 session-state handler 里赋值。token 级内容读屏不可达——已知取舍，写明。
- 重命名：走 `usePrompt()` 全局单例（对话框焦点管理已在册的标杆件），`prompt({ title: '重命名会话', label: '会话名称', initialValue: 旧名 })`，返回 null=取消、空串=按取消处理（usePrompt 头注口径"空串按取消由调用方判断"——这里不允许空名，trim 后空即弃）。
- 关闭会话：`useConfirm({ tone: 'danger', confirmLabel: '关闭会话', description: '关闭后该会话的本地消息记录立即清空，且不可恢复；正在回复的回合会被中止。' })`——破坏性动作分离+确认（operation-panel 模式条），且**关闭当前会话后选中回落规则定死**：取列表中被删项的前一项，无前项取新首项，空列表回无选中态。
- Esc：输入区无弹层不截获；确认/输入框焦点时 Esc 由各 dialog 单例自理。
- 图标/微件：新建（＋）与行内动作钮都带 `aria-label`+title；装饰 SVG `aria-hidden="true"`；`kbd` 提示字沿用 MsgBoardView 在册样式形制（或 components.css 全局 kbd 若有）。
- 触屏：`@media (pointer: coarse)` 下发送钮/新建钮/行内动作 `min-height: 44px`（WechatBotChatInput 同款收尾块）。
- 200% 缩放：左右栏 grid 用 cqi 夹持 + minmax(0,1fr)，文本容器 `min-width: 0` + `overflow-wrap: anywhere`（气泡内长 URL/长 token 不撑破——design-system 最后一条纪律）。

### 6.2 暗色主题注意点（逐条对应 design-system 规则落地点）

- **只用 token，零裸色**：所有面按 `--surface-page/panel/soft/hover`、`--color-text/muted/subtle`、`--state-*`(+`-soft`)、`--color-primary(-soft)`、`--color-border(-strong)` 引用；scoped 里禁出现 `#xxxxxx` 与 rgba 字面量（wechatbot 流里的"品牌绿功能例外"不适用于本模块，无豁免理由）。
- **on-color 契约（N37）**：本视图唯一实心彩色面是 `.btn-primary` 发送钮——前景由全局原子管，scoped 内**不得**再写它的 color。若做"用户气泡选中态"等新增彩色底，走 `--color-on-primary`/`--color-on-accent` 配对，禁借用/禁写死白。
- **glow 只给在途**（N37 elevation 条）：live-pulse（opacity 呼吸）只允许出现在 `state==='working'` 的占位与 thinking summary；done/failed/blocked 一律静态 chip/border，禁残影脉冲（"pulsing dot 必须匹配真实 live 行为"）。焦点环恒 `--focus-ring`。
- **状态不靠颜色单传**：chip 全带文字（工作中/空闲/挂起/完成/失败），error 项除红色外有「错误」字样 chip，thinking 用缩进+左边框结构区分而非仅灰字。
- 暗档自检点（完工前双主题逐屏看）：user 气泡 primary-soft 在暗底上的可辨识度（rgba(66,183,180,.13) 在 #0e1c22 面板上——软底+25%primary 混边两件套足够分离，若实测分离不足，加边强一档到 30% 混，**不加新色值**）；`--surface-hover` 上的 system 胶囊文字对比；tool 行 mono 灰字在暗底 `--color-text-subtle`(#748a90) 达标与否过一遍对比。
- 动效：全部 ≤180ms/几个 px；收展 rows 切换不做高度动画（textarea 高度动画是抖动源）；全局 reduced-motion 块已兜底。

---

## 7. spec 测试清单（AiChatView.spec.ts）

打桩形制照抄仓内三件合体：aichat 绑定 `vi.mock('../../../bindings/hanxi/internal/modules/aichat', ...)`（服务方法 vi.fn 全家：CreateSession/ListSessions/Rename/CloseSession/SendMessage）；事件用 MsgBoardView.spec 的 `runtime.handlers` 桩（`Events.On` 收编 + `fire(name, data)` 手播）；`useConfirm/usePrompt` 按 JsonFmtView.spec 的 hoisted mock；rAF 用 FrpcProjectsView.spec 的 `nextFrame()`；fake timers 下排干 await 一律 `flushMicrotasks`（JsonFmtView.spec 头注纪律）。命名逐条对齐 JsonFmtView 风格：**说行为+钉边界，括号里点破"为什么这条存在"**。

```
describe('AiChatView 会话列表与门禁')
  it('挂载只调 ListSessions 装载；空列表给出"新建会话"引导空态，不自动建会话')
  it('「新建会话」走 usePrompt：取消/空串都不发 CreateSession')
  it('CreateSession 成功即选中新会话：状态 idle、发送闸开、历史空态文案在屏（契约无历史拉取，不臆造旧消息）')
  it('ListSessions 连发两发：晚到的旧一代不得整表回灌（reqSeq 门禁，PortScan 同族）')
  it('刷新合并保留运行态：新表只带 title，本地 state/asking/unread 不被旧字段洗掉')
  it('CloseSession 先过危险确认：取消不关闭；确认后摘仓、选中回落到前一项，迟到事件不落任何 UI')
  it('Rename 走 usePrompt；成功后列表就地改题，不整页重拉')

describe('AiChatView 发送只在按下的那一刻')
  it('会话第一条消息先要过外发确认；确认后 SendMessage 逐字一次 (sessionId, trim 后文本) 且输入清空')
  it('确认文案必须说出「离开这台机器」与「几十秒」（含糊的"使用 AI 处理"不得回头）')
  it('取消确认一次都不外发，输入保留、无任何在途位残留')
  it('确认框待决期间连按 Enter/点钮：confirm 只调一次、SendMessage 零调用（先闸后问，aiAsking 教训移植）')
  it('同会话第二轮不再弹确认；新会话重新弹（确认与会话同生命周期）')
  it('state=working 期间发送钮禁用且 Enter 无效（在途禁发，事件通道也拦）')
  it('空白/纯空白文本不发（trim 判定，按钮 title 给原因）')
  it('无当前会话时禁发，提示引导新建')
  it('Enter 发送/Shift+Enter 换行；isComposing 的组合期回车不发送（IME 误发钉死）')
  it('SendMessage 抛错：sendError 上屏 + 流内追加"未送达"error 项，已到达的 items 与用户内容一条不砸')
  it('rows 展开钮在 3/8 间切换，不碰文本内容（纯 UI）')

describe('AiChatView 事件流进表与状态机')
  it('session-state 迁移上屏：working→done 状态字与输入可用性逐档翻转')
  it('blocked 报警示 chip 但输入可用（补充信息正是解锁动作，不得锁死用户）')
  it('failed：配套 kind=error 项在流内可见且发送闸重开（再发=新回合重试）')
  it('timeline 同 (turnId,kind) 增量续接成一个气泡；kind 切换另起新项，互不污染')
  it('乐观 user 项与后端回推 user 事件逐字相等 → 吞掉重复仅回填 turnId；不等 → 以事件文本为准')
  it('rAF 合帧：一帧内 3 个事件帧前不可见、帧后一次到齐（断言渲染批次，不断言回调次数）')
  it('事件按 sessionId 路由：后台会话进表不污染当前流，切回可见且带未读计数')
  it('未在册/已关闭会话的 timeline 与 session-state 事件整体作废（不复活幽灵会话、不弹错）')
  it('turn-stats 锚在对应 turnId 尾部：耗时/入/出原样换算单位，前端零二次统计')
  it('working 且尾项不是回复内容时，流尾在途占位在场（几十秒口径文案），idle 后消失')
  it('超 600 条裁头保尾：首项 id 上移、尾项原样，key 稳定不整表重补（frpc :index 教训反例钉死）')

describe('AiChatView 滚动跟随')
  it('贴底时新帧自动落底；scrollTop 离底 >40px 后到事件不再拉动滚动（useLogTails 阈值语义）')
  it('脱锁期间累计新条数，悬浮"回到最新消息（N）"在场；点击落底、计数清零、闩复位')
  it('发送成功强制落底一次（自己的消息必须被看见），随后上滚照常脱锁')
  it('切换会话默认贴底且未读清零')
```

覆盖自查（quality-checklist 状态矩阵对位）：Initial（挂载装载）/Loading（listLoading state-box）/Success/Empty（无会话、历史空态两条不同文案）/Error（listError vs sendError vs turn error 三通道，spec 各至少一条）/Disabled-busy（working/asking/sending 三闸）/Cancelled（确认与 prompt 取消）/Retry（failed 再发、listError 重试钮）——无 Stale/Partial：契约不存在"陈旧数据"与"批量部分成"结构，不强凑（checklist 只查适用项）。

---

## 8. 施工顺序建议（一次提交一族，符合原子化纪律）

1. `chore(frontend)`: navigation.ts 三处登记 + `AiChatView.vue` 空壳可路由（页面可进，模块未起时靠门禁提示）。
2. `feat(aichat)`: `useAiChat.ts` + spec（数据面全部可测逻辑：状态机、门禁矩阵、合并三规则、reqSeq/作废）。
3. `feat(aichat)`: 三个子件 + 视图接线 + scoped 样式 + spec（渲染面）。
4. 双主题走查 + quality-checklist 收尾（1280/768/390 三档截图留档）。

红线自认：本稿不依赖任何未生成物的行为细节——绑定路径名是唯一需在开工首日对表的外部事实（§0.2），对不上时改动面为两行 import + spec mock 路径，架构不动。
