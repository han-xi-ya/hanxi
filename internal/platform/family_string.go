package platform

import "strconv"

// Family.String 让协议族枚举在任何 %s/%v 打印中天然可读（Go 具名枚举惯例）。
//
// 背景（A2 2026-10-26 跨区转派）：portkill 服务层把 Family 拼进错误文案
// （`fmt.Errorf("TCP/%s: %w", fam, err)`），此前无 String() 方法，实际渲染成
// `TCP/%!s(int=2): ...` 的用户可见乱码。方法收口在枚举定义侧一次性根治，
// 消费侧手写的局部映射助手随各自批次退役（portkill 三处由 A6 处置）。
//
// 独立成小文件而非并入 platform.go：该文件有并行会话在途改动，避免 hunk 交叉。
func (f Family) String() string {
	switch f {
	case FamilyIPv4:
		return "IPv4"
	case FamilyIPv6:
		return "IPv6"
	default:
		return "Family(" + strconv.Itoa(int(f)) + ")"
	}
}
