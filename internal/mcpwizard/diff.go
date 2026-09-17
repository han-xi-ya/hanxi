package mcpwizard

import "strings"

// diff 行类型（前端按字面量着色）：keep 上下文 / add 新增 / del 删除。
const (
	diffKeep = "keep"
	diffAdd  = "add"
	diffDel  = "del"
)

// diffLineLimit 防御性上限：客户端配置正常远小于此，超限即整体退化为摘要提示，
// 避免异常巨型文件把 LCS 平方复杂度带进 UI。
const diffLineLimit = 4000

// DiffLine 预览差异行（绑定 DTO）。Kind：keep / add / del。
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// lineDiff 行级 LCS 差异（配置文件行数少，O(n·m) DP 可接受）。
func lineDiff(before, after string) []DiffLine {
	a := splitLines(before)
	b := splitLines(after)
	if len(a) > diffLineLimit || len(b) > diffLineLimit {
		return []DiffLine{{Kind: diffKeep, Text: "（内容过长，不提供逐行差异，请打开文件核对）"}}
	}
	lcs := lcsTable(a, b)
	var out []DiffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Kind: diffKeep, Text: a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{Kind: diffDel, Text: a[i]})
			i++
		default:
			out = append(out, DiffLine{Kind: diffAdd, Text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, DiffLine{Kind: diffDel, Text: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, DiffLine{Kind: diffAdd, Text: b[j]})
	}
	return out
}

// lcsTable 经典 LCS 长度动态规划表（多留一行一列哨兵 0）。
func lcsTable(a, b []string) [][]int {
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else if t[i+1][j] >= t[i][j+1] {
				t[i][j] = t[i+1][j]
			} else {
				t[i][j] = t[i][j+1]
			}
		}
	}
	return t
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
