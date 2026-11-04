package omp

import "strings"

// envpath.go：用户 PATH 的纯集合运算（注册表读写在 launcher_windows.go）。
// 与注册表解耦成本包可单测——插入/剔除的幂等性、大小写折叠、空段压缩
// （omp 官方脚本同款纪律：装机只动一个目录，重复安装不多写一份）。

// pathAdd 把 dir 加入 PATH 片段列表；已在（大小写折叠同路径）则返回原列表与 added=false。
// 顺手剔除空段（历史 `;;` 残渣不算本模块责任但也别放大它——仅在确需写入时才落盘）。
func pathAdd(parts []string, dir string) (out []string, added bool) {
	norm := normalizePathPart(dir)
	out = make([]string, 0, len(parts)+1)
	for _, p := range parts {
		if p == "" {
			continue
		}
		if added && normalizePathPart(p) == norm {
			// 已存在：去重保留首份
			continue
		}
		out = append(out, p)
		if normalizePathPart(p) == norm {
			added = true
		}
	}
	if !added {
		out = append(out, dir)
	}
	return out, !added
}

// pathRemove 剔除等于 dir 的片段（大小写折叠、容许尾分隔符差异）。
func pathRemove(parts []string, dir string) (out []string, removed bool) {
	norm := normalizePathPart(dir)
	out = make([]string, 0, len(parts))
	for _, p := range parts {
		if !removed && normalizePathPart(p) == norm {
			removed = true
			continue
		}
		out = append(out, p)
	}
	return out, removed
}

// pathContains 判定 PATH 片段列表是否已含 dir。
func pathContains(parts []string, dir string) bool {
	norm := normalizePathPart(dir)
	for _, p := range parts {
		if normalizePathPart(p) == norm {
			return true
		}
	}
	return false
}

// normalizePathPart 折叠大小写并去尾分隔符（Windows 路径判同的最低口径，
// 与 artifact 内核路径折叠同理——注册表值经 Get 已展开环境引用，故此处只对齐字面）。
func normalizePathPart(p string) string {
	p = strings.TrimSpace(p)
	for len(p) > 1 && (p[len(p)-1] == '\\' || p[len(p)-1] == '/') {
		p = p[:len(p)-1]
	}
	return strings.ToLower(p)
}
