package conpty

import "errors"

// 两个哨兵都放在**无标签**文件：分居 build-tag 两侧时任一侧的调用方
// （如 errors.Is(err, conpty.ErrNoConPTY) 在 Linux 构建）会直接编译炸
// ——审计 P3 点名的形态，根治靠归位而不是运气。

// ErrNoConPTY 系统里没有 ConPTY 导出（Windows 10 1809 之前的形态）。
// 只在"找不到入口点"这一种失败上命中；其余 HRESULT 不带哨兵（防误导）。
var ErrNoConPTY = errors.New("系统过旧：缺少 ConPTY 支持（需 Windows 10 1809+）")

// ErrUnsupportedPlatform 非 Windows 系统没有伪控制台。
var ErrUnsupportedPlatform = errors.New("conpty: 伪控制台仅 Windows 可用")
