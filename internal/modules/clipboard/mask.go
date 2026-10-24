package clipboard

// 预览隐私遮蔽（契约 §12 v1.8，借鉴 TieZ privacy_protection_kinds + 自定义
// 规则；同时兑现决策档案"三道闸一条退路"闸③的视图侧承诺——preview 明文不
// 裸渲染）。
//
// 口径（一条路遮全，单源零前端二次遮）：
//   - 只作用在 service 出口的 Preview（List 返回与 clipboard:updated 事件
//     载荷共用；store 落盘、Get 正文、files[] 一律不动）；
//   - 搜索匹配发生在 store 内、吃的是明文 preview——用户搜原文照样命中、
//     上屏仍遮，这是特性不是漏洞（登记 §12 v1.8）；
//   - 前端 rowPreview/rowTitle 全部吃后端 preview，后端遮了前端自然对。
//
// 实现纪律：RE2 线性时间天然防回溯爆炸（自定义正则的 ReDoS 顾虑在 Go 侧
// 结构性不存在）；内置形态保首尾可辨（138****5678 / a***@x.com /
// sk-****），遮后形态不再命中原正则——幂等无套娃，重复 apply 无害。
// 15 位一代旧身份证不进内置（纯 15 位数字误伤订单号），需要走自定义规则。

import (
	"log/slog"
	"regexp"
	"strings"
)

// 内置 kind 的编译产物（包级一次成型；kinds 开关只决定跑不跑，不重复编译）。
var (
	maskPhoneRe    = regexp.MustCompile(`\b(1[3-9]\d)\d{4}(\d{4})\b`)
	maskIDCardRe   = regexp.MustCompile(`\b(\d{3})\d{11}(\d{3}[0-9Xx])\b`)
	maskEmailRe    = regexp.MustCompile(`([A-Za-z0-9._%+-])[A-Za-z0-9._%+-]*(@[A-Za-z0-9.-]+\.[A-Za-z]{2,})`)
	maskSecretRe   = regexp.MustCompile(`(?i)(token|secret|passwd|password|authorization)(\s*[:=]\s*)("?)[^"',\s]+`)
	maskKeyFormRe  = regexp.MustCompile(`\b(sk|ghp|gho|ghu|ghs|glpat|xox[abprs])([-_])[A-Za-z0-9_-]{16,}`)
	maskPrivKeyRe  = regexp.MustCompile(`BEGIN [A-Z ]*PRIVATE KEY`)
	maskCustomMark = "****"
)

// maskEngine 生效遮蔽引擎：kinds 决定内置族，customs 依序整命中替换。
// nil 接收者=全关恒等（测试手装体与遮蔽域未启用形态，与三缝假件同谱）。
type maskEngine struct {
	kinds   map[string]bool
	customs []*regexp.Regexp
}

// newMaskEngine 装配引擎；自定义正则编译失败逐个报错上浮（prefs 域 normalize
// 已校验过一遍，这里是纵深第二闸——两闸口径同源靠 normalize 共用正则文法）。
func newMaskEngine(kinds []string, patterns []string) (*maskEngine, error) {
	m := &maskEngine{kinds: map[string]bool{}}
	for _, k := range kinds {
		m.kinds[strings.ToLower(strings.TrimSpace(k))] = true
	}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		m.customs = append(m.customs, re)
	}
	return m, nil
}

// enabled 是否有任何遮蔽规则在跑（全关形态 apply 直接短路，零扫描成本）。
func (m *maskEngine) enabled() bool {
	if m == nil {
		return false
	}
	return len(m.kinds) > 0 || len(m.customs) > 0
}

// apply 遮蔽一条 preview。内置四族按 phone→idcard→email→secret 固定序执行
// （先高特异性后泛形，避免 email 的 local-part 吃掉已遮的号码段这类叠字），
// 自定义规则殿后整命中替 ****。
func (m *maskEngine) apply(s string) string {
	if !m.enabled() || s == "" {
		return s
	}
	if m.kinds["phone"] {
		s = maskPhoneRe.ReplaceAllString(s, "$1****$2")
	}
	if m.kinds["idcard"] {
		s = maskIDCardRe.ReplaceAllString(s, "$1***********$2")
	}
	if m.kinds["email"] {
		s = maskEmailRe.ReplaceAllString(s, "$1***$2")
	}
	if m.kinds["secret"] {
		s = maskSecretRe.ReplaceAllString(s, "$1$2$3"+maskCustomMark)
		s = maskKeyFormRe.ReplaceAllString(s, "${1}${2}"+maskCustomMark)
		s = maskPrivKeyRe.ReplaceAllString(s, "[私钥]")
	}
	for _, re := range m.customs {
		s = re.ReplaceAllString(s, maskCustomMark)
	}
	return s
}

// rebuildMask 按当前 prefs 重编译遮蔽引擎并热换（SetPrefs 成功后的应用步；
// 读侧 atomic.Load 无锁）。编译失败理论不可达（normalize 已拒绝坏正则），
// 真发生时保留旧引擎并 error 留痕——宁可沿用旧规则也不装"以为全关"假安全。
func (s *ClipboardService) rebuildMask() {
	eng, err := newMaskEngine(s.prefs.maskKinds(), s.prefs.maskCustomPatterns())
	if err != nil {
		slog.Error("clipboard: 遮蔽引擎重编译失败，沿用上一套规则", "err", err)
		return
	}
	s.mask.Store(eng)
}
