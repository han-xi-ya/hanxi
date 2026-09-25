// 轮盘域图标解析（审查 shell#4 上收）：WheelPreview 与 QuickMenuPopup 曾逐字
// 克隆 TYPE_ICON+iconOf 对——"预览与实盘同图标"是两盘同源契约，克隆迟早漂移，
// 收进 quickmenu 域本地共享件（不进 appIcons 公共层：TYPE_ICON 是轮盘词汇）。
import type { MenuItem } from '../../../bindings/hanxi/internal/modules/quickmenu/models'
import type { IconName, RenderableIcon } from '../../constants/icons'
import { appIconName } from '../../constants/appIcons'

export const WHEEL_TYPE_ICON: Record<string, IconName> = {
  exe: 'box',
  command: 'terminal',
  route: 'layout',
  group: 'layers',
}

/** 扇区图标：app: 真图标与登记矢量名二轨；漂移回落类型图标，渲染永不因图标断链。 */
export function wheelIconOf(item: MenuItem): RenderableIcon {
  return appIconName(item.icon) ?? WHEEL_TYPE_ICON[item.type] ?? 'box'
}
