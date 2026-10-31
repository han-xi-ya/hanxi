package jsonfmt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

// Asker 是"问一次外部模型"的能力接口。刻意留成接口而不是直接调 aiagent：
// 否则这些分支（AI 没装、超时、返回的不是 JSON）就只能靠真装 CLI + 真联网才能测
// （与 aichat 的 agentStream 同一条理由）。
type Asker interface {
	Ask(ctx context.Context, req aiagent.Request) (*aiagent.Result, error)
}

// JsonFmtService 暴露给前端的 JSON 美化服务：本地主干无状态纯计算，一次调用
// 一次返回。**唯一的例外是 AI 一键美化**：一次要占几十秒、还拉起子进程，于是
// 这里必须有并发状态（aiRunning），"无状态"这句话不能再原样说下去。
type JsonFmtService struct {
	holder *extapi.LeaseHolder
	// newAsker 懒构造：没装 Claude Code 的机器上本地主干照常可用，只有按下
	// AI 按钮时才去发现 CLI 并如实报错——可选依赖缺失不该让整个模块构造失败。
	newAsker func() (Asker, error)
	// aiRunning 是**服务级**单飞闸：前端的禁用挡不住"切走再切回"（新组件实例
	// 归零），来回两三下就是 2~3 个并发 claude，每个几十秒、各外发一份内容。
	// 并发上限这种事不能指望界面自觉（#132 在册）。
	aiRunning atomic.Bool
}

// NewService 创建服务实例；holder 由模块构造期建好并经 SetGate 接上调用门。
func NewService(holder *extapi.LeaseHolder) *JsonFmtService {
	return &JsonFmtService{
		holder:   holder,
		newAsker: func() (Asker, error) { return aiagent.New() },
	}
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

// ExtractByAI 「AI 一键美化」：本地主干读不动的脏文本，交给本机模型认结构；
// 返回必须先过**契约验收**、排版必须走**本地渲染器**——AI 只负责认结构，
// 合法性与数字保真由本地裁决（让模型的返回直接上屏，等于把幻觉当用户数据）。
func (s *JsonFmtService) ExtractByAI(req JsonFmtRequest) (*JsonFmtResult, error) {
	// 门分两步（#132 同款）：准入判定即归还——一次往返几十秒，抱着前台租约
	// 进 drain 会让关窗白等 60 秒再强制收口，Go 死了、杀子进程那步永不执行。
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	release()
	// 长任务挂**背景租约**：停用/退出先 cancel 再等 Release，ctx 一路传到
	// exec 层，子进程真被收而不是被 drain 超时抛弃。
	bg, bgErr := s.holder.EnterBackground(context.Background())
	if bgErr != nil {
		return nil, bgErr
	}
	defer bg.Release()

	if !s.aiRunning.CompareAndSwap(false, true) {
		return nil, errors.New("已有一次 AI 美化在途中（一次往返要几十秒），请等它结束；" +
			"中途改本地内容不受影响，本地美化不排队")
	}
	defer s.aiRunning.Store(false)

	if strings.TrimSpace(req.Text) == "" {
		return nil, errors.New("没有要交给 AI 的内容")
	}
	// 外发吃本地同一道尺寸闸：超限在**发出之前**拦住，不做"白外发一次再失败"。
	if len(req.Text) > MaxInputBytes {
		return nil, fmt.Errorf("待交给 AI 的内容 %d 字节，超过单次上限 %d 字节；先截出你要处理的那一段",
			len(req.Text), MaxInputBytes)
	}
	agent, err := s.newAsker()
	if err != nil {
		return nil, err
	}
	aiRes, err := agent.Ask(bg.Context(), aiagent.Request{
		Instruction: extractInstruction,
		Input:       req.Text,
	})
	if err != nil {
		return nil, err
	}
	// NO_JSON 哨兵必须在进验收前拦下：否则"模型说它没找到"会变成一屏
	// "提取成功"（#126 裸词事故的拦法，判据换了这一步也永远保留）。
	if strings.EqualFold(strings.TrimSpace(aiRes.Text), "NO_JSON") {
		return nil, fmt.Errorf("%w：模型回报这段文本里没有完整 JSON", ErrAINotJSON)
	}
	nodes, note, reject := validateAIJSON(aiRes.Text)
	if reject != "" {
		// 明说不降级：端一份"看着像 JSON 其实被 AI 编造"的结果，比报一句失败恶劣得多。
		return nil, fmt.Errorf("%w：%s", ErrAINotJSON, reject)
	}
	indent := normalizeIndent(req.Indent)
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		parts = append(parts, render(n, indent, req.SortKeys))
	}
	kind := "multi"
	if len(nodes) == 1 {
		kind = nodes[0].kind.String() // 单份返回报真实形状，"multi" 会虚报原文段数
	}
	// 外发字节数/返回字节数/耗时原样上屏：用户要看得见自己刚送出多少东西。
	warns := []string{fmt.Sprintf("由本机 AI 提取：外发 %d 字节、模型返回 %d 字节、耗时 %dms",
		aiRes.BytesIn, aiRes.BytesOut, aiRes.ElapsedMs)}
	// 契约只放开过一格"引号最小转义"（ai.go 规则 2b）——本地验收只保证
	// 解析得过，证明不了"除引号外一字未动"，核对义务如实交回给用户。
	warns = append(warns, "AI 仅被允许做字符串引号的最小转义闭合，其余一字照抄；关键内容请与原文核对一遍")
	if note != "" {
		warns = append(warns, note)
	}
	return &JsonFmtResult{
		OK:       true,
		Output:   strings.Join(parts, "\n\n"),
		Kind:     kind,
		Warnings: warns,
	}, nil
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
// （第三分支的指路句随 2026-10-31「AI 一键美化」回归改写：出路就在本模块的
// 按钮，不再把人赶去聊天模块。）
func classifyFailure(msg string, off int, src string) string {
	end := len(strings.TrimRightFunc(src, unicode.IsSpace))
	nearEnd := off >= end-8
	unclosed := strings.Contains(msg, "未闭合") || strings.Contains(msg, "意外结束") ||
		strings.Contains(msg, "缺少数字") || strings.Contains(msg, "对象成员之间") ||
		strings.Contains(msg, "数组元素之间")
	if nearEnd && unclosed {
		return "看起来是尾部被截断（从日志窗口选区复制时最常见，往往少了末尾的 } 或 ]）。" +
			"回源头把整段复制全；补不回来就按「AI 一键美化」让本机模型认结构。"
	}
	head := strings.TrimLeftFunc(src, unicode.IsSpace)
	if head != "" && strings.IndexByte("]},:", head[0]) >= 0 {
		return "看起来是开头没复制全：第一个实字符是 " + string(head[0]) +
			"，说明它前面那段（含起始的 { 或 [）漏在外面。回源头把开头一起选中再复制。"
	}
	// 通用判据：错误发生在第一个 {/[ **之前**。样本矩阵抓出两连雷：
	// ① 旧条件 off<=2 只盖得住 `key = {...}`；② 放宽成"意外的字符"仍漏——
	// ISO 时间戳"2026-..."会被当**合法数字标量**解析成功，报的是"之后还有
	// 多余内容"（探针实测 col=5），根本不走"意外的字符"文案。两种报错形态
	// 在这一分支下语义相同：消化掉的"值"只是真 JSON 前面的残头。
	if i := strings.IndexAny(src, "{["); i > 2 && off < i &&
		(strings.Contains(msg, "意外的字符") || strings.Contains(msg, "之后还有多余内容")) {
		line, col := lineCol([]byte(src), i)
		return fmt.Sprintf("开头不是 JSON：第一个 { 或 [ 在第 %d 行第 %d 列，它前面那些"+
			"（日志时间戳、`key = ` 这类前导文字）本模块不猜也不剥——按「AI 一键美化」处理，"+
			"或自己把那几行截掉再贴一次。", line, col)
	}
	return ""
}
