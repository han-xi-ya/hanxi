/**
 * 敏感内容遮罩点串——隐私语义逐字一致的唯一出处。
 *
 * 消费方：剪贴板详情浮层（ClipDetail）、便签卡（MemoCard）与便签阅读页
 * （MemoView 页面板 masked 分支）。遮罩点数（48）一律经本常数取用——
 * 消费方禁写私有遮罩串字面量（同值双写或异值都割裂隐私语义）。
 */
export const MASK_DOTS = '•'.repeat(48)
