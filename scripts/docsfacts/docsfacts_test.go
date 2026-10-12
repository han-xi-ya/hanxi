// docsfacts_test.go 把文档事实对账挂进 `go test ./...`，从而由既有 CI 的
// windows `go test -count=1 -race ./...` 一步自然覆盖（无需新增工作流步骤）。
// 风格与 internal/app/catalog_contract_test.go 同构：纯文件系统静态扫描，不起 app、
// 不联网、不执行被检工具，问题以清单形式一次性摆出来（而非首个即 halt）。
package main

import (
	"regexp"
	"strconv"
	"testing"
)

func loadTestFixture(t *testing.T) fixtureDoc {
	t.Helper()
	root := mustRoot(t)
	fx, err := loadFixture(root)
	if err != nil {
		t.Fatalf("加载 %s 失败: %v", fixtureRel, err)
	}
	return fx
}

// TestFixtureWellFormed 先验证规则表自身可用：正则可编译、断言指向已知事实、
// 活文档文件真实存在。规则表烂掉时后面三项检查会集体哑火，必须先立此门。
func TestFixtureWellFormed(t *testing.T) {
	root := mustRoot(t)
	fx := loadTestFixture(t)

	for i, rule := range fx.Forbidden {
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			t.Errorf("forbidden[%d] 正则不可编译 %q: %v", i, rule.Pattern, err)
		}
		if rule.Why == "" {
			t.Errorf("forbidden[%d]（%q）缺 why 说明，报错将无法自解释", i, rule.Pattern)
		}
	}
	for i, a := range fx.Assertions {
		if _, err := regexp.Compile(a.Pattern); err != nil {
			t.Errorf("assertions[%d] 正则不可编译 %q: %v", i, a.Pattern, err)
			continue
		}
		if _, ok := fx.Facts[a.Fact]; !ok {
			t.Errorf("assertions[%d] 引用未知事实 %q（可用键见 facts 段）", i, a.Fact)
		}
	}
	for _, doc := range fx.ActiveDocs {
		if _, err := readText(root, doc); err != nil {
			t.Errorf("activeDocs 含不可读文件 %s: %v", doc, err)
		}
	}
}

// TestFactsMatchFixture 代码现推 ↔ 冻结基线双向锁：改代码不同步基线即红，
// 改基线凑数（代码没变而数值被手改）同样红。
func TestFactsMatchFixture(t *testing.T) {
	root := mustRoot(t)
	if problems := CheckFactsAgainstFixture(root, loadTestFixture(t)); len(problems) > 0 {
		t.Fatalf("事实与基线不一致（%d 处）：\n  - %s", len(problems), joinLines(problems))
	}
}

// TestDerivationSanity 现推数值自身的下限校验。取数正则若因源码结构变化而退化，
// 会返回偏小值——那比返回 0 更危险（0 会 fail loud，小值会伪装成"代码缩水了"）。
func TestDerivationSanity(t *testing.T) {
	root := mustRoot(t)
	derived, err := DeriveFacts(root)
	if err != nil {
		t.Fatalf("现推事实失败: %v", err)
	}
	if got := derived["moduleTotal"]; got < 40 {
		t.Errorf("moduleTotal=%d 明显低于合理下限，疑取数正则已失效（catalog 结构变更需同步 DeriveFacts）", got)
	}
	if got := derived["mcpTools"]; got < 8 {
		t.Errorf("mcpTools=%d 低于合理下限，疑 internal/mcp 取数已失效", got)
	}
	if got := derived["selfBuiltModules"]; got != derived["moduleTotal"]-derived["hostedVersions"] {
		t.Errorf("selfBuiltModules=%d 与 moduleTotal−hostedVersions 不自洽", got)
	}
	if derived["hostedVersions"] > derived["moduleTotal"] {
		t.Errorf("hostedVersions=%d 超过 moduleTotal=%d，口径已错乱", derived["hostedVersions"], derived["moduleTotal"])
	}
}

// TestDocsMatchFacts 活文档禁词表与正向断言——本轮全项目对账的实际成果固化处。
func TestDocsMatchFacts(t *testing.T) {
	root := mustRoot(t)
	if problems := CheckDocsAgainstFacts(root, loadTestFixture(t)); len(problems) > 0 {
		t.Fatalf("文档与代码事实不一致（%d 处）：\n  - %s\n\n修法：改文档为现值（出处 docs/FACTS.md），"+
			"或在文档里为史实数字补「原写/当时值/勘误」类帽（禁词表 allowIfContains 认这些），"+
			"或代码确实变更后跑 `go run ./scripts/docsfacts -emit` 同步基线。",
			len(problems), joinLines(problems))
	}
}

// TestDocsStructure 断链与畸形表格：两类都是本轮对账实际抓到的病型。
func TestDocsStructure(t *testing.T) {
	root := mustRoot(t)
	if problems := CheckMarkdownStructure(root, loadTestFixture(t).ActiveDocs); len(problems) > 0 {
		t.Fatalf("活文档结构检查不通过（%d 处）：\n  - %s", len(problems), joinLines(problems))
	}
}

// TestFactsDocSelfConsistency 校验 docs/FACTS.md 的表体与基线同源：
// 单一事实源文档若与 fixture 背离，闸门就成了自欺装置。
func TestFactsDocSelfConsistency(t *testing.T) {
	root := mustRoot(t)
	fx := loadTestFixture(t)
	text, err := readText(root, "docs/FACTS.md")
	if err != nil {
		t.Fatalf("docs/FACTS.md 不可读: %v", err)
	}
	for key, want := range fx.Facts {
		re := regexp.MustCompile(`(?m)^\| [^|\n]*\| \*\*` + strconv.Itoa(want) + `\*\*`)
		if !re.MatchString(text) {
			t.Errorf("docs/FACTS.md 找不到现值 **%d** 的行（事实 %s），文档已与基线背离", want, key)
		}
	}
}

func mustRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("定位仓库根失败: %v", err)
	}
	return root
}

func joinLines(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += "\n  - "
		}
		out += s
	}
	return out
}
