package jsonfmt

import (
	"strings"
	"testing"
)

// TestSyslogMultiLineJSON 形态取自机主真实日志：systemd journal 转发出来的
// `Oct 28 10:56:34 host proc[31735]: ` 前缀，挂在跨多行 JSON 的**每一行**上，
// 中间还夹着 `========== 签名参数 ==========` 这类非 JSON 文本行。
//
// 这条是本轮的验收核心：早先规则族里没有 syslog 形态，于是"每行带前缀的多行 JSON"
// 两道都救不回来——用户看到的就是"从日志里提不出这段 JSON"。
// 值全部换成同形态假数据：真实商户号、税号、姓名、交易号不进仓库。
//
// 样本是整块反引号原始串：内容不需要任何转义，也就不会被多层转义削掉字符
// （本仓已为此踩过多次，症状都是 string literal not terminated）。
func TestSyslogMultiLineJSON(t *testing.T) {
	in := `Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]: ========== 签名参数 ==========
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]: appId:      OP00000000
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]: ========== 支付宝开票查询 - 原始响应 ==========
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]: {
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   "req_data": {
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:     "acc_merchant_no": "2088000000000000",
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:     "serial_no": 1,
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:     "big_id": 9007199254740993
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   },
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   "additional_element_list": null,
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   "invoice_item_list": [
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:     {
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:       "item_amount": "0.01",
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:       "item_num": "0.001183"
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:     }
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   ],
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]:   "invoice_status": "INVOICE_FAILED"
Oct 28 10:56:34 iZexamplehost01 oilStationV2.linux[31735]: }`

	// ExtractAll 前端恒传 true：整段与剥前缀两道都被开头的噪声行挡住时，靠抽取拿 JSON 段
	r := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: true})
	if !r.OK {
		t.Fatalf("这类日志必须能美化：err=%q hint=%q 位置=%d:%d 剥前缀行数=%d 警告=%v 片段=%q",
			r.Error, r.Hint, r.Line, r.Col, r.DroppedPrefixLines, r.Warnings, r.Snippet)
	}
	// 开头夹着 `====` 与 appId 行，整段解析不了 → 走抽取，顶层 Kind 是抽取态标记
	if r.Kind != "multi" || len(r.Segments) == 0 {
		t.Fatalf("应为多段抽取，实得 Kind=%q 段数=%d", r.Kind, len(r.Segments))
	}
	obj := false
	for _, sg := range r.Segments {
		if sg.Kind == "object" && strings.Contains(sg.Output, "big_id") {
			obj = true
			// 段行数要跟这段渲染文本的真实行数一致。这里曾数的是**字面反斜杠+n**
			// （被多层转义削掉一次的结果），于是每段永远报 1 行，聚合行数跟着撒谎。
			// 断言外部事实（这段有十几行）而不是断言实现里的计数表达式。
			if sg.Stats.LinesOut < 5 {
				t.Errorf("这段是 2 空格缩进的多行对象，行数却报 %d：行数没在数真的换行符\n%s",
					sg.Stats.LinesOut, sg.Output)
			}
			if sg.Stats.BytesOut != len(sg.Output) {
				t.Errorf("段字节数应与渲染文本长度相符：%d vs %d", sg.Stats.BytesOut, len(sg.Output))
			}
		}
	}
	if !obj {
		t.Errorf("响应对象必须被抽出来：%+v", r.Segments)
	}
	if r.DroppedPrefixLines == 0 {
		t.Error("改了输入就得说出来：DroppedPrefixLines 应为正数")
	}
	// 19 位 ID 是这条链路的真正考点：过一遍 float64 就会改掉末位
	if !strings.Contains(r.Output, "9007199254740993") {
		t.Errorf("大整数被改写：%s", r.Output)
	}
	if !strings.Contains(r.Output, "null") || !strings.Contains(r.Output, "INVOICE_FAILED") {
		t.Errorf("null 与字符串值都必须保留：%s", r.Output)
	}
	// 统计表里有数组内层的键，才说明真解析成了树，而不是字符串匹配糊过去
	deep := false
	for _, k := range r.KeyStats {
		if k.Path == "invoice_item_list[].item_amount" {
			deep = true
		}
	}
	if !deep {
		t.Errorf("数组内层键未进统计表：%+v", r.KeyStats)
	}
	// 前缀不许渗进结果：还带着 journald 进程名/主机名就是剥失败
	if strings.Contains(r.Output, "oilStationV2.linux") || strings.Contains(r.Output, "iZexamplehost01") {
		t.Errorf("输出里仍有日志前缀：%s", r.Output)
	}
}

// TestSyslogNoiseOnlyFailsHonestly 同类日志里若根本没有完整 JSON（只有噪声行），
// 必须如实报错，而不是硬凑一个"看起来像结果"的东西。
func TestSyslogNoiseOnlyFailsHonestly(t *testing.T) {
	in := `Oct 28 10:56:34 host proc[1]: ========== 签名参数 ==========
Oct 28 10:56:34 host proc[1]: appId:      OP00000000`
	r := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: true})
	if r.OK {
		t.Fatalf("没有 JSON 就不该报成功：%+v | %v", r, r.Warnings)
	}
	if r.Error == "" {
		t.Error("要给原因")
	}
}
