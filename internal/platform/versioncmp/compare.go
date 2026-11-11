// Package versioncmp 提供工具版本号通用比较：
// 多个托管模块（everything / bcu 及后续集成）都要按"哪个已装版本最新"排序，
// 抽取共享避免复制。从 everything 模块原样提取，行为向后一致。
package versioncmp

import (
	"strconv"
	"strings"
)

// Compare 比较两个版本号，返回 1（a 更新）/ 0（相等）/ -1（b 更新）。
//
// 规则（自上而下依次生效）：
//  1. 先归一：去首尾空白，并剥掉**单个前导 v/V**（当且仅当其后紧跟数字）。各托管族
//     把已装版本以带 v 前缀的展示令牌存进账本并直接喂给本函数（windterm/termora/
//     piclite/vscode 等），若不作归一，"v2.16.1" 会被判为非规范串、与"v2.9.0"一起
//     退化成字典序，于是 2.9.0 被当成比 2.16.1 更新——Termora 真实版本线已到 2.16，
//     属现行缺陷（2026-10-26 审查批次 T2 取证）。
//  2. 再分档：**规范串恒优先于非规范串**。非规范形态包括本地导入读不出版本时的兜底
//     目录名 `imported-<sha256 前缀>` 与历史 `imported-<秒级时间戳>`。不分档则退化为
//     整串字典序，而 'i'(0x69) 的码点大于数字，兜底目录会被排成"最新版"，凡取首个
//     当"最新已装"的调用方都会因此选中导入件、或误判"已是最新"而漏报更新
//     （`artifact.Tree` 与多家模块的本地排序同受此害，2026-10-26 审查批次坐实）。
//  3. 规范串之间：先逐段比数值核心（数字按数值比较；段内尾部字母按字典序并视为比纯数字段
//     更新——"1422" < "1422a" < "1422b"，与 voidtools 的修正版发布节奏一致；段数不同时先逐段
//     比完，均相等则段多者胜——"6.2.0" < "6.2.0.1"）；核心相等时按 semver §11 裁决预发布：
//     正式版 > 其任何预发布，同为预发布则点分标识符逐个比（数字标识符按数值且恒小于字母标识符，
//     全等时标识符少者小），即 "1.0.0-beta" < "1.0.0-beta.1" < "1.0.0" < "1.0.1"。
//  4. 两侧同为非规范：维持历史行为，整串字典序（不 panic、有确定序）。
//
// 契约变更说明（两处旧结果改变，均为缺陷订正）：
//   - 规则 1/2：`Compare("v1.2.3","1.2.3")` 由 1 变 0（旧语义把带 v 前缀当成"文本更大"，
//     正是规则 1 要修的错），`Compare("imported-x","1.2.3")` 由 1 变 -1（旧语义让兜底目录冒充最新版）。
//   - 规则 3 预发布档（2026-11-11 追加）：带横杠的 semver 预发布形态（"2.0.0-beta.16"、
//     "0.16.0-beta.1"）过去整体判为**非规范串**，按规则 2 恒劣后于任何规范串，于是远程一个
//     数值更低的稳定版也能把本机 beta 判成"落后"——Termora（本机 v2.0.0-beta.16 vs 稳定通道
//     v1.0.17）与 Paseo（本机 0.16.0-beta.1 vs 0.15.4）的"可用更新"红点因此在追更后永不熄灭，
//     且每次重启由 state/updates.json 回灌重亮（updatewatch 感知链实证，见 TROUBLESHOOTING）。
//     现在这类形态按规则 3 参与逐段比较：`Compare("2.0.0-beta.16","1.0.17")` 由 -1 变 1。
//     兜底目录（"imported-…"、"0.8.98.removing-…"数值核心不成立）仍判非规范，档位纪律不变。
//     带 `+build` 元数据的形态维持非规范（本仓无此类账目，宁守旧不引入"不同 build 判等"的新语义）。
func Compare(a, b string) int {
	na, nb := normalize(a), normalize(b)
	va, okA := parse(na)
	vb, okB := parse(nb)
	if okA != okB { // 规范方恒胜，先于任何字典序判定
		if okA {
			return 1
		}
		return -1
	}
	if !okA { // 两侧同为非规范：维持历史行为（整串字典序）
		return strings.Compare(na, nb)
	}
	if c := compareSegs(va.segs, vb.segs); c != 0 {
		return c
	}
	switch {
	case !va.hasPre && !vb.hasPre:
		return 0
	case !va.hasPre:
		return 1 // 正式版优先于其任何预发布
	case !vb.hasPre:
		return -1
	default:
		return comparePreRelease(va.pre, vb.pre)
	}
}

// verTok 归一并拆分后的版本令牌：数值核心分段 + 可选 semver 预发布串。
type verTok struct {
	segs   []verSeg
	pre    string // 预发布串（首个 '-' 之后、点分标识符列表；hasPre 为假时无意义）
	hasPre bool
}

// parse 解析已 normalize 的版本串：首个 '-' 之前为数值核心（须通过 split 的"数字前缀 +
// 可选纯字母后缀"分段语法），之后为预发布串（只容许 [0-9A-Za-z-] 的点分非空标识符）。
// 核心不成立或预发布串非法（空、含空标识符、含非法字符）一律 ok=false，交调用方退化
// 字典序兜底——"imported-<hash>"、"0.8.98.removing-<ts>" 这类兜底/临时目录名正是靠
// 数值核心不成立而保持非规范档位。
func parse(v string) (verTok, bool) {
	if v == "" {
		return verTok{}, false
	}
	segs, ok := split(v)
	if ok {
		return verTok{segs: segs}, true
	}
	i := strings.IndexByte(v, '-')
	if i < 0 {
		return verTok{}, false
	}
	segs, ok = split(v[:i])
	if !ok {
		return verTok{}, false
	}
	pre := v[i+1:]
	if !validPreRelease(pre) {
		return verTok{}, false
	}
	return verTok{segs: segs, pre: pre, hasPre: true}, true
}

// validPreRelease 报告预发布串是否为非空的点分标识符列表，且每个标识符只含
// [0-9A-Za-z-]（semver §9 字符集，宽松接受 "03" 这类前导零数字标识符——日期形态
// "2026-03-26" 的 "03-26" 段要能解析）。
func validPreRelease(pre string) bool {
	if pre == "" {
		return false
	}
	for _, id := range strings.Split(pre, ".") {
		if id == "" {
			return false
		}
		for _, c := range []byte(id) {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-') {
				return false
			}
		}
	}
	return true
}

// compareSegs 逐段比较数值核心：数字段按数值，段内字母后缀按字典序；均相等时段多者胜。
func compareSegs(a, b []verSeg) int {
	for i := range min(len(a), len(b)) {
		if a[i].num != b[i].num {
			if a[i].num > b[i].num {
				return 1
			}
			return -1
		}
		if a[i].alpha != b[i].alpha {
			if a[i].alpha > b[i].alpha {
				return 1
			}
			return -1
		}
	}
	if len(a) != len(b) {
		if len(a) > len(b) {
			return 1
		}
		return -1
	}
	return 0
}

// comparePreRelease 按 semver §11 比较两个非空预发布串：点分标识符逐个比，数字标识符按
// 数值且恒小于字母标识符；全部相等时标识符少者小（"beta" < "beta.1"）。
// 调用方已保证两侧标识符合法，故此处不再判字符集；数字段溢出（超长）退化为字典序比较，
// 不 panic、有确定序。
func comparePreRelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		if c := compareIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) > len(bs):
		return 1
	case len(as) < len(bs):
		return -1
	default:
		return 0
	}
}

// compareIdentifier 单个预发布标识符：两侧纯数字按数值（数值段恒小于字母段，semver §11.4.2），
// 否则按字典序。
func compareIdentifier(a, b string) int {
	na, aNum := allDigits(a)
	nb, bNum := allDigits(b)
	switch {
	case aNum && bNum:
		if na != nb {
			if na > nb {
				return 1
			}
			return -1
		}
		return 0
	case aNum: // 数字标识符 < 字母标识符
		return -1
	case bNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// allDigits 报告 s 是否全为数字并给出数值；空串或溢出返回 ok=false（按非数字处理）。
func allDigits(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// normalize 去首尾空白并剥掉单个前导 v/V（仅当其后紧跟数字，避免误伤 "vim" 之类
// 非版本串）。账本展示令牌与远程 tag 常分别以 "v1.2.3" 与 "1.2.3" 形态进入比较，
// 不先归一两者永不相等，去重与"是否已装"判定都会落空。
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 2 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

type verSeg struct {
	num   int64  // 段内数字前缀
	alpha string // 段内尾部纯字母（纯数字段为空串，字典序最小）
}

// split 把版本号切成段。段内格式：数字前缀 + 可选纯字母后缀（如 "1422b"）。
// 任一段不符合即整体返回 ok=false（调用方退化字典序兜底）。
func split(v string) ([]verSeg, bool) {
	parts := strings.Split(v, ".")
	segs := make([]verSeg, 0, len(parts))
	for _, p := range parts {
		i := 0
		for i < len(p) && p[i] >= '0' && p[i] <= '9' {
			i++
		}
		if i == 0 {
			return nil, false // 空段或非数字开头均视为非规范
		}
		for j := i; j < len(p); j++ {
			c := p[j]
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
				return nil, false
			}
		}
		num, err := strconv.ParseInt(p[:i], 10, 64)
		if err != nil {
			return nil, false // 超长数字段溢出：退化字典序
		}
		segs = append(segs, verSeg{num: num, alpha: p[i:]})
	}
	if len(segs) == 0 {
		return nil, false
	}
	return segs, true
}

// IsCanonical 报告 v 是否为可逐段数值比较的规范版本号：数值核心每段形如"数字前缀 + 可选纯
// 字母后缀"，其后允许一段合法的 semver 预发布串（"2.0.0-beta.16"）。判据与 Compare 同源
// （同一个 normalize + parse），因此不会出现"一处认规范、另一处退化字典序"的双真相；前导
// v/V 已由 normalize 剥除，带 v 的展示令牌同样判规范。
// 非规范形态包括：本地导入兜底目录名 imported-<sha256 前缀> / imported-<秒级时间戳>、
// 落位中途的 <version>.removing-<ts> 临时目录、带 `+build` 元数据的串，以及段式不合的杂串。
// Compare 对这类值退化为整串字典序，因此"字典序恰好更大"的兜底目录会被排成最新——
// 需要"非规范形态恒劣后"的比较方（如 artifact.Tree 降序）必须先按本判据定序，别依赖字典序。
func IsCanonical(v string) bool {
	_, ok := parse(normalize(v))
	return ok
}
