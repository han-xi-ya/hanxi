package jsonfmt

// 样本集回归：给机主的验收脚本打底。非 live 部分把"基础美化器"的行为矩阵
// （哪些必须成功、哪些必须红、红得指没指对方向）钉成常驻用例；live 部分
// （HANXI_AI_LIVE=1）真发 AI 验"一键美化"的端到端——样本先自证本地读不动，
// 才准问 AI（否则测的是自己：本地早就搞定时，AI 成功证明不了任何事，#128）。

import (
	"errors"
	"os"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

func svc() *JsonFmtService { return NewService(extapi.NewLeaseHolder("jsonfmt")) }

const (
	sampleTrailingComma = `{"order_id":9527,"items":[{"sku":"A-1","price":12.5},],}`
	sampleClean         = `{"name":"李四","age":30,"tags":["go","json"],"active":true}`
	sampleBigInt        = `{"snowflake":1234567890123456789,"ratio":1E2,"neg":-0.0000000001,"big":[9007199254740993,9007199254740994]}`
	sampleTruncated     = `{"config": {"timeout": 30, "hosts": ["a.example.com", "b.example.com"`
	sampleHeadCut       = `]},"count":42,"ok":true}`
	samplePrefixed      = "2026-10-31 15:02:11 INFO  请求参数 = {\"orderId\":8899,\"buyer\":\"王五\"}"
	// AI 主场样本：字符串里未转义引号（本地补闭合必拒——断在字符串内部）。
	sampleRawQuote = `{"msg":"他说"今天天气不错"，然后走了","code":0,"data":{"list":[{"id":1,"name":"张三"}]}}`
	// journald 每行前缀 + 长数字串。
	sampleSyslog = "Oct 31 15:02:11 invoicehost oilStationV2.linux[31735]: " +
		`{"appId":"OP00000000","biz_content":{"outTradeNo":"HZ20261031150211","totalAmount":"99.50","body":{"items":[{"goodsId":"G9527","price":"99.50","quantity":1}]},"extendParams":{"storeId":"S-东 4 区"}}}`
	sampleProse = "今天天气不错，没有花括号也没有方括号"
)

func TestSampleMatrixLocal(t *testing.T) {
	cases := []struct {
		name, text   string
		wantOK       bool
		wantErrIn    string // 失败时 error 文案应含
		wantHintIn   string // 失败时 hint 应含（"" = 不审 hint）
		wantOutHas   []string
		notWantOutIn []string
	}{
		{name: "尾逗号不救", text: sampleTrailingComma, wantOK: false, wantErrIn: "意外的"},
		{name: "干净直通", text: sampleClean, wantOK: true, wantOutHas: []string{`"name": "李四"`, `"age": 30`}},
		{
			name: "大整数逐字符", text: sampleBigInt, wantOK: true,
			wantOutHas: []string{"1234567890123456789", "1E2", "-0.0000000001", "9007199254740993", "9007199254740994"},
		},
		{name: "尾部截断指方向", text: sampleTruncated, wantOK: false, wantHintIn: "尾部被截断"},
		{name: "开头没复制全指方向", text: sampleHeadCut, wantOK: false, wantHintIn: "开头没复制全"},
		{name: "前导文字指第一个花括号", text: samplePrefixed, wantOK: false, wantHintIn: "一键美化"},
		{name: "未转义引号本地必红", text: sampleRawQuote, wantOK: false},
		{name: "syslog 前缀本地必红", text: sampleSyslog, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beautify(JsonFmtRequest{Text: c.text, Indent: 2})
			if c.wantOK {
				if !r.OK {
					t.Fatalf("应成功: %+v", r)
				}
				for _, want := range c.wantOutHas {
					if !strings.Contains(r.Output, want) {
						t.Fatalf("输出缺 %q:\n%s", want, r.Output)
					}
				}
				return
			}
			if r.OK {
				t.Fatalf("应失败却成功:\n%s", r.Output)
			}
			if c.wantErrIn != "" && !strings.Contains(r.Error, c.wantErrIn) {
				t.Fatalf("error 文案 %q 不含 %q", r.Error, c.wantErrIn)
			}
			if c.wantHintIn != "" && !strings.Contains(r.Hint, c.wantHintIn) {
				t.Fatalf("hint %q 不含 %q", r.Hint, c.wantHintIn)
			}
		})
	}
}

// TestLiveExtractByAISamples 真机一键美化（默认 skip）：AI 端到端只测
// "本地四道全红"的样本；断言全是外部事实——数字串逐字符在场、假成功不在场。
func TestLiveExtractByAISamples(t *testing.T) {
	if os.Getenv("HANXI_AI_LIVE") == "" {
		t.Skip("未设 HANXI_AI_LIVE=1：跳过真机一键美化")
	}
	t.Run("未转义引号", func(t *testing.T) {
		if r := beautify(JsonFmtRequest{Text: sampleRawQuote}); r.OK {
			t.Skip("本地已能解析（说明主干行为漂移了，AI 样本失去存在前提）——红这测试之前先查上面")
		}
		r, err := svc().ExtractByAI(JsonFmtRequest{Text: sampleRawQuote, Indent: 2})
		if err != nil {
			t.Fatalf("AI 提取失败: %v", err)
		}
		for _, want := range []string{"今天天气不错", `"code": 0`, "张三"} {
			if !strings.Contains(r.Output, want) {
				t.Fatalf("结果缺 %q:\n%s", want, r.Output)
			}
		}
	})
	t.Run("syslog 长数字", func(t *testing.T) {
		if r := beautify(JsonFmtRequest{Text: sampleSyslog}); r.OK {
			t.Skip("本地已能解析，样本前提失效")
		}
		r, err := svc().ExtractByAI(JsonFmtRequest{Text: sampleSyslog, Indent: 2})
		if err != nil {
			t.Fatalf("AI 提取失败: %v", err)
		}
		// 数字串在字符串值里：逐字符在场即证明 AI 没改精度、验收没放水
		for _, want := range []string{"HZ20261031150211", "OP00000000", "S-东 4 区"} {
			if !strings.Contains(r.Output, want) {
				t.Fatalf("结果缺 %q:\n%s", want, r.Output)
			}
		}
	})
	t.Run("混合文本多段", func(t *testing.T) {
		mixed := "request={\"method\":\"POST\",\"qty\":3}  ← 发出\n" +
			"response={\"code\":200,\"requestId\":1234567890123456789,\"data\":{\"status\":\"PAID\"}}"
		if r := beautify(JsonFmtRequest{Text: mixed}); r.OK {
			t.Skip("本地已能解析，样本前提失效")
		}
		r, err := svc().ExtractByAI(JsonFmtRequest{Text: mixed, Indent: 2})
		if err != nil {
			t.Fatalf("AI 提取失败: %v", err)
		}
		if r.Kind != "multi" {
			t.Fatalf("两段应报 multi，得 %q:\n%s", r.Kind, r.Output)
		}
		for _, want := range []string{"\"method\": \"POST\"", "1234567890123456789", "\"status\": \"PAID\""} {
			if !strings.Contains(r.Output, want) {
				t.Fatalf("拼接结果缺 %q:\n%s", want, r.Output)
			}
		}
	})
	t.Run("纯人话不出假成功", func(t *testing.T) {
		_, err := svc().ExtractByAI(JsonFmtRequest{Text: sampleProse})
		if err == nil {
			t.Fatal("人话不可能有 JSON 却成功了——假成功回潮")
		}
		if !errors.Is(err, ErrAINotJSON) {
			t.Fatalf("应报契约拒收哨兵，得: %v", err)
		}
		if strings.Contains(err.Error(), "NO_JSON") && !strings.Contains(err.Error(), "没有完整 JSON") {
			t.Fatalf("哨兵语义要成人话: %v", err)
		}
	})
}
