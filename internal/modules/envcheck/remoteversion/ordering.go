package remoteversion

// PrioritizeLocalLine 把最新 release 与本机版本同属一条版本线的通道移动到列表最前面，
// 其余通道保持原有相对顺序（稳定移动）。lineOf 负责把版本字符串归一化为版本线标识
// （如 Go "1.26"、Node "24"、Java "21"、Python "3.12"），本机或通道版本无法解析时
// 返回空串并维持原顺序。列表少于两个通道时不做任何处理。
func PrioritizeLocalLine(channels []Channel, localVersion string, lineOf func(string) string) {
	if len(channels) < 2 {
		return
	}
	line := lineOf(localVersion)
	if line == "" {
		return
	}
	for i := range channels {
		if len(channels[i].Releases) == 0 || lineOf(channels[i].Releases[0].Version) != line {
			continue
		}
		if i == 0 {
			return
		}
		picked := channels[i]
		copy(channels[1:i+1], channels[:i])
		channels[0] = picked
		return
	}
}
