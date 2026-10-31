// useJsonChatBridge：JSON 美化器 ↔ AI 聊天 双向接力的唯一在途位（模块级单例）。
//
// 它存在的理由：本前端无 vue-router（工作台在册口径），两个视图之间没有 URL 可以
// 携带待交接的文本；而「脏 JSON 一键带去 AI 聊天让模型认结构 / 模型返回的 JSON 块
// 拿回美化器细看」是 2026-10-31 收纳裁决后 jsonfmt 模块钦定的唯一出路（见后端
// internal/modules/jsonfmt models.go 头注与 classifyFailure 的指路文案）。跨视图
// 接力必须有一个一等公民载体——但只需要这一个：不要再造第二份全局事件总线，
// Wails 事件是后端→前端的进程通道，拿它当视图间邮箱就是把架构再度揉乱的开始。
//
// 语义定死：
//  - pending 是模块级单例 Ref。视图经 App.vue 的 KeepAlive 复用、实例态活过导航，
//    但桥里的文本只该被目标视图读一次：take* 读后即清（消费一次），回环导航不会
//    让同一份文本二次弹入；已知取舍——带入时若目标输入框已有草稿，草稿被顶掉
//    （来源页刚离开、目标页刚进入，用户此刻的意图就是处理这段文本）。
//  - 方向不互串：handOff 置 toChat（美化器→聊天），handToFmt 置 toFmt（聊天→美化器）；
//    新交接覆盖未消费的旧值（用户最新意图优先，旧文本仍在来源视图的输入框/流里，无损）。
//  - 这里只搬运字符串，不当第二个渲染器/校验器：真格式化永远回 jsonfmt 后端
//    （大整数保真在那边）；collectOpenableJsonBlocks 的本地 JSON.parse 只当
//    「这块值不值得挂卡」的判卷，判卷不交卷。
import { ref } from 'vue'

export type JsonBridgeDirection = 'toChat' | 'toFmt'

export interface JsonBridgePayload {
  text: string
  direction: JsonBridgeDirection
}

const pending = ref<JsonBridgePayload | null>(null)

/** 美化器→聊天：把读不动的原文（可能脏）送去让模型认结构。 */
export function handOff(text: string): void {
  pending.value = { text, direction: 'toChat' }
}

/** 聊天侧非消费读取 toChat 待发文本（快捷钮的禁用态与取源判定用）。 */
export function peekForChat(): string | null {
  return pending.value?.direction === 'toChat' ? pending.value.text : null
}

/** 聊天侧消费 toChat 文本：读后即清，同一份只弹入一次。 */
export function takeForChat(): string | null {
  if (pending.value?.direction !== 'toChat') return null
  const text = pending.value.text
  pending.value = null
  return text
}

/** 聊天→美化器：把 AI 回合里解析通过的 JSON 块送回美化器细看。 */
export function handToFmt(jsonText: string): void {
  pending.value = { text: jsonText, direction: 'toFmt' }
}

/** 美化器侧消费 toFmt 文本：读后即清，同一份只弹入一次。 */
export function takeForFmt(): string | null {
  if (pending.value?.direction !== 'toFmt') return null
  const text = pending.value.text
  pending.value = null
  return text
}

/**
 * 提取编排模板的指令头（逐字定死，spec 按原文断言）：把"数字逐字符照抄、
 * 每块单独 ```json、除代码块外零说明"钉在提示词里——美化器只认得 ```json 块，
 * 模型多写一句寒暄不碍事，少一块或改一个数位才是事故。
 */
export const EXTRACT_JSON_PROMPT =
  '请从下面文本里提取所有 JSON 值（对象或数组），数字逐字符照抄不许改精度、不许科学计数法，每个结果单独放一个 ```json 代码块，除代码块外不要写任何说明：'

/** 编排模板 = 指令头 + 空行 + 原文。两处预填路径（挂载带入/快捷钮）共用此单一拼装口。 */
export function composeExtractPrompt(text: string): string {
  return `${EXTRACT_JSON_PROMPT}\n\n${text}`
}

// ```json 围栏（大小写不容错——模型偶尔手滑大写 json/JSON）；围栏行尾允许空格。
const JSON_FENCE_RE = /```json[^\S\n]*\r?\n([\s\S]*?)```/gi

/**
 * 扫描一段文本中的 ```json 代码块，只返回 JSON.parse 能过的块内容。
 * 解析只当判卷不做渲染——卡片打开后回 jsonfmt 后端真美化（大整数过一遍
 * JS number 就花了，这正是本模块留在 Go 的唯一硬理由）；解析不过的块
 * 不挂卡：拿一张"打开会报错"的卡钓用户，不如没有这张卡。
 */
export function collectOpenableJsonBlocks(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(JSON_FENCE_RE)) {
    const body = (m[1] ?? '').trim()
    if (!body) continue
    try {
      JSON.parse(body)
      out.push(body)
    } catch {
      // 非法块：静默不挂卡（它本来就不是"可打开的 JSON"）
    }
  }
  return out
}
