// 快捷菜单轮盘 · 扇区级联外扩子环——几何纯函数模块（设计定稿）。
//
// 窗口 512×512 DIP，圆心 C=256。主盘完全沿用 QuickMenuPopup 现状口径：
// 盘缘 rDisc=170、扇区带 rSecIn=74 → rSecOut=162、hub rHub=62
// （弹窗现为 376 窗口，本模块是 512 窗口下的规范口径：半径相同、圆心不同）。
//
// 新增「子环帽带」：主环分组扇区展开时，其子条目不再换层级展开二级盘，
// 而以同心帽带渲染在主盘外侧，半径 rCapIn=174 → rCapOut=236（174 在 162 扇区
// 外缘与 170 盘缘之外，留 4 DIP 缝带）。帽带角度以父扇区中线对称展开：
// 总跨角 span = clamp(子数 × 24°, 父扇区步长, 180°)——子少时帽带不窄于父楔形
// 的视觉占位，子多时不越过半盘，扇区最小名义宽度 24° 保证可读命中面。
//
// 取消判定：指针越过 rCancel=244（帽带外 8 DIP 缓冲）即取消，由前端在指针
// 越界时置态；本模块只提供常量，不做任何状态判断。
//
// 角度约定与 QuickMenuPopup.polar 完全一致：0° 取 12 点方向，顺时针增长。
// 全部为纯函数，零 Vue / 零 DOM 依赖；坐标数值保留两位小数，与现状
// wedgePath 的输出风格一致。

/** 轮盘几何常量表（512 窗口定稿值，勿在调用方散落魔法数） */
export const WHEEL = {
  size: 512, // 窗口边长（正方形 DIP）
  c: 256, // 圆心（= size/2，光标锚点）
  rDisc: 170, // 盘缘半径
  rHub: 62, // 中心 hub 半径
  rSecIn: 74, // 主环扇区内缘
  rSecOut: 162, // 主环扇区外缘
  rCapIn: 174, // 子环帽带内缘
  rCapOut: 236, // 子环帽带外缘
  rCancel: 244, // 取消判定半径（指针越界置取消态，常量供前端使用）
} as const

/**
 * N40③ 皮批专用绘制观感常量（StarPie 磨砂花瓣盘）——与 WHEEL 命中语义完全解耦，
 * 只喂渲染调用（mainWedge/capWedge/帽带底环/hub 刻度）实参，不参与任何极坐标归属：
 * 缝隙变大是"绘制缝"，命中始终按名义角域 slotOf（N40① 设计红利，缝多大都无死区）。
 * 花瓣角向缝由 pad 绘制扣出，楔形另有 stroke-width 6 同色膨胀（径向 ±3），
 * 故 VIS 环带四周恒内缩于 WHEEL 命中带 ≥3 DIP，牌面缝隙像素仍归属邻扇区。
 */
export const VIS = {
  padDeg: 4.2, // 主环花瓣绘制缝角（每侧；外缘净缝 ≈17px、内缘 ≈6px，内窄外宽收拢感）
  capPadDeg: 3.5, // 帽带子扇区绘制缝角（每侧）
  rSecIn: 80, // 主环绘制带内缘（命中内缘仍 74）
  rSecOut: 158, // 主环绘制带外缘（158→176 盘缘段让给连续亮环）
  rCapIn: 177, // 帽带绘制内缘（命中仍 174）
  rCapOut: 233, // 帽带绘制外缘（命中仍 236；+2 为帽带外缘提示弧位）
  rHubFace: 64, // hub 仪表核磨砂面绘制半径（点击命中仍 rHub-2=60）
  rTickIn: 50, // hub 槽位刻度内端
  rTickOut: 57, // hub 槽位刻度外端
} as const

/** 极坐标 → 直角坐标：0° 取 12 点方向，顺时针增长（与 QuickMenuPopup.polar 同约定） */
export function polar(r: number, deg: number): { x: number; y: number } {
  const rad = ((deg - 90) * Math.PI) / 180
  return { x: WHEEL.c + r * Math.cos(rad), y: WHEEL.c + r * Math.sin(rad) }
}

/**
 * 通用环段 wedge：内半径 rIn、外半径 rOut、起止角 a0/a1（度，a1>a0）。
 * 外缘弧顺时针（sweep=1）、内缘弧逆时针回卷（sweep=0），跨角 >180° 时置大弧
 * 标志；padDeg 留缝职责在调用方（角度函数已扣），数值保留两位小数。
 */
export function wedgePath(rIn: number, rOut: number, a0: number, a1: number): string {
  const p0 = polar(rIn, a0)
  const p1 = polar(rOut, a0)
  const p2 = polar(rOut, a1)
  const p3 = polar(rIn, a1)
  const large = a1 - a0 > 180 ? 1 : 0
  return [
    `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)}`,
    `L ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`,
    `A ${rOut} ${rOut} 0 ${large} 1 ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`,
    `L ${p3.x.toFixed(2)} ${p3.y.toFixed(2)}`,
    `A ${rIn} ${rIn} 0 ${large} 0 ${p0.x.toFixed(2)} ${p0.y.toFixed(2)}`,
    'Z',
  ].join(' ')
}

/** 主环第 i 枚扇区（共 n 枚）的起止角：每侧留 padDeg 缝隙，用细缝分层而非描边噪声 */
export function mainSectorAngles(i: number, n: number, padDeg = 0.9): { a0: number; a1: number } {
  const step = 360 / n
  return { a0: step * i + padDeg, a1: step * (i + 1) - padDeg }
}

/**
 * 极坐标角（0°=12 点、顺时针，任意 [-360,360] 域）→ 主环槽位索引：按名义角域
 * `floor(ang / step)` 归属，缝隙带（pad）像素归入其所属扇区——命中判定与渲染
 * 缝合同源，牌面任意落点都有唯一槽位（N40 全扇面命中的角向依据）。
 */
export function slotOf(angDeg: number, n: number): number {
  const step = 360 / n
  return (Math.floor(((angDeg % 360) + 360) % 360 / step)) % n
}

/**
 * 帽带总跨角：span = clamp(childCount × 24°, parentStepDeg, 180°)。
 * 下界父步长保证子少时帽带不窄于父楔形占位，上界 180° 保证不越过半盘；
 * childCount ≤ 0 时返回 0（无子可展）。
 */
export function capSpanDeg(childCount: number, parentStepDeg: number): number {
  if (childCount <= 0) return 0
  return Math.min(Math.max(childCount * 24, parentStepDeg), 180)
}

/**
 * 帽带第 j 枚子扇区的起止角：以 parentCenterDeg 为中心，把 capSpanDeg
 * 均分成 childCount 份；子数 > 1 时每侧再留 padDeg 缝隙（单子不留缝，
 * 完整占满 span 以保持对称）。childCount ≤ 0 或 j 越界返回 null。
 * 注：子数极端稠密（span/childCount < 2×padDeg）时缝隙会吞掉楔形，
 * 属数据病态，交由上限约束在调用方处理。
 */
export function capSectorAngles(
  childCount: number,
  parentCenterDeg: number,
  parentStepDeg: number,
  j: number,
  padDeg = 0.9,
): { a0: number; a1: number } | null {
  const span = capSpanDeg(childCount, parentStepDeg)
  if (span === 0) return null
  if (j < 0 || j >= childCount) return null
  const slot = span / childCount
  const start = parentCenterDeg - span / 2
  let a0 = start + slot * j
  let a1 = start + slot * (j + 1)
  if (childCount > 1) {
    a0 += padDeg
    a1 -= padDeg
  }
  return { a0, a1 }
}

/** 子扇区 <button> 锚点：帽带中线半径（rCapIn+rCapOut)/2 处、角中线的窗口百分比坐标 */
export function capAnchorDeg(a0: number, a1: number): { left: string; top: string } {
  const p = polar((WHEEL.rCapIn + WHEEL.rCapOut) / 2, (a0 + a1) / 2)
  return { left: `${(p.x / WHEEL.size) * 100}%`, top: `${(p.y / WHEEL.size) * 100}%` }
}

/**
 * 主环第 i 枚扇区（共 n 枚）锚点：扇区带中线半径（rSecIn+rSecOut)/2 处、
 * 扇区名义中线（不留缝）的窗口百分比坐标；供弹窗替换现 anchorOf 时同口径使用。
 */
export function mainAnchor(i: number, n: number): { left: string; top: string } {
  const p = polar((WHEEL.rSecIn + WHEEL.rSecOut) / 2, (360 / n) * (i + 0.5))
  return { left: `${(p.x / WHEEL.size) * 100}%`, top: `${(p.y / WHEEL.size) * 100}%` }
}
