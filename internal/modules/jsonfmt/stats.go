package jsonfmt

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// 统计与查找。都是**只读**能力：在已解析好的节点树上走一遍，不碰原始文本。
//
// 为什么路径要折叠数组下标（`items[].id` 而不是 `items[0].id`、`items[1].id`…）：
// 日志 JSON 的主体是"一批记录"，按索引展开会得到几十行同构路径，把唯一有用的信息
// ——这个键出现了几次、值都是什么类型——淹掉。索引本身在查找命中里给（带段号与行号）。

// maxMatches 一次查找返回的命中上限。列 5 万条命中没人看得完，只会把 UI 拖死。
const maxMatches = 500

// valueSummaryRunes 命中值摘要的字符上限（按 rune 截，中文不会被砍半）。
const valueSummaryRunes = 70

// KeyStat 一个规范化路径的出现次数与值类型分布。
type KeyStat struct {
	Path  string      `json:"path"`
	Count int         `json:"count"`
	Types []TypeCount `json:"types"`
}

// TypeCount 某类型出现的次数（string / number / boolean / null / object / array）。
type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// Match 一条查找命中。Path 是可照着改的规范化路径，Segment/Line 只在多段时有值。
type Match struct {
	Path    string `json:"path"`
	Value   string `json:"value"`
	Kind    string `json:"kind"`
	Segment int    `json:"segment,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// statsAcc 收集器。类型计数用 map，最终按"次数降序、同次数字典序"输出，
// 免得 map 遍历顺序让同一份输入的统计每次长得不一样。
type statsAcc struct {
	byPath map[string]*keyAgg
	order  []string
}

type keyAgg struct {
	count int
	types map[string]int
}

func newStatsAcc() *statsAcc { return &statsAcc{byPath: map[string]*keyAgg{}} }

func (a *statsAcc) add(path, typ string) {
	ka, ok := a.byPath[path]
	if !ok {
		ka = &keyAgg{types: map[string]int{}}
		a.byPath[path] = ka
		a.order = append(a.order, path)
	}
	ka.count++
	ka.types[typ]++
}

// collect 走一棵树。parent 是父路径；q 非空时同时收集查找命中。
func collect(n node, parent string, segIdx, segLine int, q string, acc *statsAcc, matches *[]Match, truncated *bool) {
	switch n.kind {
	case kObject:
		for _, m := range n.members {
			path := m.key
			if parent != "" {
				path = parent + "." + m.key
			}
			acc.add(path, typeName(m.value))
			if q != "" && matchHit(path, m.value, q) && len(*matches) < maxMatches {
				*matches = append(*matches, Match{
					Path: path, Value: summarize(m.value), Kind: typeName(m.value),
					Segment: segIdx, Line: segLine,
				})
			} else if q != "" && len(*matches) >= maxMatches {
				*truncated = true
			}
			collect(m.value, path, segIdx, segLine, q, acc, matches, truncated)
		}
	case kArray:
		path := parent + "[]"
		if parent == "" {
			path = "[]"
		}
		for _, it := range n.items {
			acc.add(path, typeName(it))
			collect(it, path, segIdx, segLine, q, acc, matches, truncated)
		}
	default:
		if parent == "" {
			acc.add("$", typeName(n)) // 标量根（一段日志里只有一个值）
		}
	}
}

// buildStats 出统计与查找结果。stats 按出现次数降序（最常出现的键先看）。
func buildStats(acc *statsAcc) []KeyStat {
	out := make([]KeyStat, 0, len(acc.byPath))
	for _, path := range acc.order {
		ka := acc.byPath[path]
		types := make([]TypeCount, 0, len(ka.types))
		for t, c := range ka.types {
			types = append(types, TypeCount{Type: t, Count: c})
		}
		sort.Slice(types, func(i, j int) bool {
			if types[i].Count != types[j].Count {
				return types[i].Count > types[j].Count
			}
			return types[i].Type < types[j].Type
		})
		out = append(out, KeyStat{Path: path, Count: ka.count, Types: types})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func typeName(n node) string {
	switch n.kind {
	case kObject:
		return "object"
	case kArray:
		return "array"
	case kString:
		return "string"
	case kNumber:
		return "number"
	case kLiteral:
		switch n.lit {
		case "true", "false":
			return "boolean"
		default:
			return "null"
		}
	default:
		return "unknown"
	}
}

// summarize 把值压成一行人能扫的摘要。容器只报规模，不展开（展开就把列表挤成长串）。
func summarize(n node) string {
	switch n.kind {
	case kString:
		plain := unquoteLit(n.lit)
		plain = strings.Join(strings.Fields(plain), " ")
		if utf8.RuneCountInString(plain) <= valueSummaryRunes {
			return plain
		}
		return string([]rune(plain)[:valueSummaryRunes]) + "…"
	case kObject:
		return fmt.Sprintf("{…%d 键}", len(n.members))
	case kArray:
		return fmt.Sprintf("[…%d 项]", len(n.items))
	default:
		return n.lit
	}
}

func matchHit(path string, v node, q string) bool {
	ql := strings.ToLower(q)
	if strings.Contains(strings.ToLower(path), ql) {
		return true
	}
	if v.kind == kString {
		return strings.Contains(strings.ToLower(unquoteLit(v.lit)), ql)
	}
	return strings.Contains(strings.ToLower(summarize(v)), ql)
}

// analyzeTreeInto 把一棵树并进收集器，并回传累计的命中列表与截断标记。
// 多段时同一收集器被反复调用，因此路径表天然是"所有段合并"的视图。
func analyzeTreeInto(acc *statsAcc, n node, parent string, segIdx, segLine int, q string,
	matches []Match, truncated bool) ([]Match, bool) {
	collect(n, parent, segIdx, segLine, q, acc, &matches, &truncated)
	return matches, truncated
}

// analyzeTree 单段便捷入口。
func analyzeTree(n node, parent string, segIdx, segLine int, q string) ([]KeyStat, []Match, bool) {
	acc := newStatsAcc()
	var matches []Match
	truncated := false
	collect(n, parent, segIdx, segLine, q, acc, &matches, &truncated)
	return buildStats(acc), matches, truncated
}
