package jsonfmt

// 基础美化器的行为钉。断言全部对**外部事实**：输出串逐字符、行列号、提示方向——
// 不抄实现内部结构。2026-10-31 收纳裁决把规则族删了，这里的用例集随之收小，
// 但主干每一条都有牙（大整数保真是这个模块存在的理由，第一优先）。

import (
	"errors"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

func run(req JsonFmtRequest) *JsonFmtResult {
	svc := NewService(extapi.NewLeaseHolder("jsonfmt"))
	r, err := svc.Beautify(req)
	if err != nil {
		panic(err) // 无门注入时 Enter 恒放行；抛错即装配病
	}
	return r
}

func TestBasicFormat(t *testing.T) {
	r := run(JsonFmtRequest{Text: `{"b":1,"a":[true,null,2.5]}`, Indent: 2})
	if !r.OK {
		t.Fatalf("标准 JSON 应成功: %+v", r)
	}
	want := "{\n  \"b\": 1,\n  \"a\": [\n    true,\n    null,\n    2.5\n  ]\n}"
	if r.Output != want {
		t.Fatalf("缩进输出不对:\n%q", r.Output)
	}
	if r.Kind != "object" {
		t.Fatalf("Kind=%q", r.Kind)
	}
}

func TestCompactAndSort(t *testing.T) {
	r := run(JsonFmtRequest{Text: `{"b":1,"a":2}`, Indent: 0})
	if r.Output != `{"b":1,"a":2}` {
		t.Fatalf("压缩档应保持键序: %q", r.Output)
	}
	r2 := run(JsonFmtRequest{Text: `{"b":1,"a":2}`, Indent: 2, SortKeys: true})
	if !strings.HasPrefix(r2.Output, "{\n  \"a\": 2,") {
		t.Fatalf("排序档 a 应先行: %q", r2.Output)
	}
}

// TestBigIntFidelity 模块存在的理由：19 位雪花 ID 一个字符都不能变。
func TestBigIntFidelity(t *testing.T) {
	r := run(JsonFmtRequest{Text: `{"id":1234567890123456789,"e":1E2,"neg":-0.0000000001}`, Indent: 2})
	if !r.OK {
		t.Fatalf("应成功: %+v", r)
	}
	for _, raw := range []string{"1234567890123456789", "1E2", "-0.0000000001"} {
		if !strings.Contains(r.Output, raw) {
			t.Fatalf("数字 %s 被改写了:\n%s", raw, r.Output)
		}
	}
}

func TestDuplicateKeyWarnedNotMerged(t *testing.T) {
	r := run(JsonFmtRequest{Text: `{"a":1,"a":2}`, Indent: 2})
	if !r.OK {
		t.Fatalf("重复键不该拒: %+v", r)
	}
	if !strings.Contains(r.Output, `"a": 1`) || !strings.Contains(r.Output, `"a": 2`) {
		t.Fatalf("重复键两个值都必须原样在场:\n%s", r.Output)
	}
	joined := strings.Join(r.Warnings, "｜")
	if !strings.Contains(joined, "重复键") {
		t.Fatalf("重复键必须有警告: %v", r.Warnings)
	}
}

func TestFailureLocationAndHints(t *testing.T) {
	// 尾部截断：错在末尾 + 未闭合语汇 → 截断判断，指路复制全或一键美化。
	r := run(JsonFmtRequest{Text: "{\"a\": [1, 2"})
	if r.OK {
		t.Fatal("未闭合必须失败")
	}
	if r.Line != 1 || r.Col == 0 || r.Snippet == "" {
		t.Fatalf("行列/片段齐备: %+v", r)
	}
	if !strings.Contains(r.Hint, "尾部被截断") {
		t.Fatalf("截断方向判断缺失: %q", r.Hint)
	}

	// 开头没复制全：首个实字符是 ]。
	r2 := run(JsonFmtRequest{Text: "]}\n{\"a\":1}"})
	if !strings.Contains(r2.Hint, "开头没复制全") {
		t.Fatalf("头部缺失判断缺失: %q", r2.Hint)
	}

	// 开头混说明文字：指出第一个 { 的位置并指路一键美化（不承诺本地剥）。
	r3 := run(JsonFmtRequest{Text: "请求参数 = {\"a\":1}"})
	if r3.OK {
		t.Fatal("带前导说明文字必须失败（本模块不猜）")
	}
	if !strings.Contains(r3.Hint, "一键美化") || !strings.Contains(r3.Hint, "第 1 行") {
		t.Fatalf("第三分支应给位置并指路一键美化: %q", r3.Hint)
	}

	// 判不出来宁可闭嘴：错误方向三选一都不沾。
	r4 := run(JsonFmtRequest{Text: "{\"a\": tru}"})
	if strings.Contains(r4.Hint, "尾部被截断") {
		t.Fatalf("值写错不该报截断: %q", r4.Hint)
	}
}

func TestGuards(t *testing.T) {
	r := run(JsonFmtRequest{Text: "   "})
	if r.OK || !strings.Contains(r.Error, "没有输入") {
		t.Fatalf("空输入: %+v", r)
	}
	r2 := run(JsonFmtRequest{Text: strings.Repeat(" ", MaxInputBytes+1) + "{}"})
	if r2.OK || !strings.Contains(r2.Error, "超过单次上限") {
		t.Fatalf("超限必须源头拒: %+v", r2)
	}
	if strings.Contains(r2.Error, "截断") && !strings.Contains(r2.Hint, "不静默截断") {
		t.Fatal("超限话术必须说清不截断")
	}
	if got := normalizeIndent(200); got != 8 {
		t.Fatalf("缩进应钳到 8，得 %d", got)
	}
}

// denyGate 验证 Wave 3 口径：门拒绝如实上抛，不回空结果冒充解析失败。
type denyGate struct{}

func (denyGate) Acquire(string) (func(), error) {
	return nil, errors.New("模块已停用")
}

func TestGateRejectionPropagates(t *testing.T) {
	svc := NewService(extapi.NewLeaseHolder("jsonfmt"))
	svc.holder.SetGate(denyGate{})
	r, err := svc.Beautify(JsonFmtRequest{Text: "{}"})
	if err == nil || r != nil {
		t.Fatalf("门拒必须上抛且无结果: r=%v err=%v", r, err)
	}
}
