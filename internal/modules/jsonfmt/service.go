package jsonfmt

import (
	"fmt"
	"strings"
	"unicode"

	"hanxi/internal/extapi"
)

// JsonFmtService 暴露给前端的 JSON 美化服务：无状态、纯计算，一次调用一次返回，
// 不发事件、不留任务。全部逻辑在 beautify 这个纯函数里，服务只负责接调用门——
// 这样解析/渲染的行为不必启动 Wails 就能逐条断言。
//
// 2026-10-31 收纳裁决后这里只剩一条主干；曾经住过的邻居（清洗/修复/抽取/统计/
// AI 提取）各自有 TROUBLESHOOTING 册条目与 git 历史，别在这里凭记忆复活。
type JsonFmtService struct {
	holder *extapi.LeaseHolder
}

// NewService 创建服务实例；holder 由模块构造期建好并经 SetGate 接上调用门。
func NewService(holder *extapi.LeaseHolder) *JsonFmtService {
	return &JsonFmtService{holder: holder}
}

// Beautify 格式化一段 JSON。
//
// Wave 3 口径：门拒绝时如实上抛，禁止回空结果冒充"解析失败"（那会让用户以为是
// 自己的 JSON 有问题）。
func (s *JsonFmtService) Beautify(req JsonFmtRequest) (*JsonFmtResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return beautify(req), nil
}

// beautify 是唯一管线：尺寸闸 → 原样解析 → 渲染。成功路径一步不多；
// 失败给出行列、原文片段与"下一步"判断（classifyFailure）。
func beautify(req JsonFmtRequest) *JsonFmtResult {
	src := req.Text
	if strings.TrimSpace(src) == "" {
		return &JsonFmtResult{Error: "没有输入内容", Hint: "粘贴一段 JSON 再按美化。"}
	}
	if len(src) > MaxInputBytes {
		return &JsonFmtResult{
			Error: fmt.Sprintf("输入 %d 字节，超过单次上限 %d 字节", len(src), MaxInputBytes),
			Hint:  "本模块不静默截断（截断后必然解析失败，报错位置还会误导人）；截出要看的那一段再贴，或改用文件工具。",
		}
	}

	root, perr, warnings := analyze(src)
	if perr != nil {
		line, col := lineCol([]byte(src), perr.off)
		return &JsonFmtResult{
			Error:    perr.msg,
			Hint:     classifyFailure(perr.msg, perr.off, src),
			Line:     line,
			Col:      col,
			Snippet:  snippet(src, perr.off),
			Warnings: warnings,
		}
	}

	indent := normalizeIndent(req.Indent)
	out := render(root, indent, req.SortKeys)
	return &JsonFmtResult{
		OK:       true,
		Output:   out,
		Kind:     root.kind.String(),
		Warnings: warnings,
	}
}

// normalizeIndent：0=压缩；其余钳进 1~8——把 200 当缩进宽度会撑出无意义的巨型输出。
func normalizeIndent(indent int) int {
	if indent <= 0 {
		return 0
	}
	if indent > 8 {
		return 8
	}
	return indent
}

// classifyFailure 把"解析失败"分成用户动作完全不同的几种形态，只依据确定性事实：
// 未闭合类错误 + 出错位置贴近文本末尾 ⇒ 尾部像被截断；首个实字符是 ] } , : ⇒
// 头部没复制全；开头混着说明文字 ⇒ 指出第一个 {/[ 的位置。
//
// 判不出来就返回空串。宁可少说一句，也不要给一条方向相反的结论——旧文案曾把
// "开头没复制全"报成"JSON 之后还有多余内容"，正好说反，用户会去改结尾而问题在开头。
// （原第三分支还会指路"高级修复/让 AI 提取"按钮——两者都已退役，这里改指 AI 聊天。）
func classifyFailure(msg string, off int, src string) string {
	end := len(strings.TrimRightFunc(src, unicode.IsSpace))
	nearEnd := off >= end-8
	unclosed := strings.Contains(msg, "未闭合") || strings.Contains(msg, "意外结束") ||
		strings.Contains(msg, "缺少数字") || strings.Contains(msg, "对象成员之间") ||
		strings.Contains(msg, "数组元素之间")
	if nearEnd && unclosed {
		return "看起来是尾部被截断（从日志窗口选区复制时最常见，往往少了末尾的 } 或 ]）。" +
			"回源头把整段复制全；补不回来就按「带去 AI 聊天」让模型认结构。"
	}
	head := strings.TrimLeftFunc(src, unicode.IsSpace)
	if head != "" && strings.IndexByte("]},:", head[0]) >= 0 {
		return "看起来是开头没复制全：第一个实字符是 " + string(head[0]) +
			"，说明它前面那段（含起始的 { 或 [）漏在外面。回源头把开头一起选中再复制。"
	}
	if off <= 2 && strings.Contains(msg, "意外的字符") {
		if i := strings.IndexAny(src, "{["); i > 2 {
			line, col := lineCol([]byte(src), i)
			return fmt.Sprintf("开头不是 JSON：第一个 { 或 [ 在第 %d 行第 %d 列，它前面那些"+
				"（日志说明、`key = ` 这类前导文字）本模块不猜也不剥——按「带去 AI 聊天」处理，"+
				"或自己把那几行截掉再贴一次。", line, col)
		}
	}
	return ""
}
