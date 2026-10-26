// main.go 是「文档事实对账闸门」（2026-10-12 立，起因见 docs/FACTS.md 前言）：
// 把散落在十余个文档里手写的模块数、托管款数、MCP 工具数等计数，与代码现推结果
// 做机器对账，防"文档描述了一个并不存在的东西"这类失真复发。
//
// 三向锁（与 cataloggen / catalog_contract_test.go 同体裁：纯文件系统静态扫描，
// 不起 app、不联网、不执行任何被检工具）：
//
//   - 代码 → 事实：DeriveFacts 从 catalog 生成物、模块子目录、mcp 源码、图标目录现推；
//   - 事实 → 冻结基线：与 scripts/fixture/doc_facts.json 比对，漂移即红（提示 -emit）；
//   - 基线 → 文档：对活文档跑禁词表与正向断言，旧值或错值即红。
//
// 另附两项文档结构检查（活文档范围）：相对链接可达性（带扩展名的任意相对目标，
// 口径见 hasFileExt 注记，不只 .md）、Markdown 表格列数一致性。
// 二者都是本次对账实际踩到的病型（N27 链接 URL 混注断链、N4 行双 cell 畸形）。
//
// 用法：
//
//	go run ./scripts/docsfacts          # 只检查（go test ./... 亦覆盖，CI 零新步骤）
//	go run ./scripts/docsfacts -emit   # 代码变更后输出新事实，人工合并进基线并同批改文档
//
// 能力边界如实声明：抓不住自然语言状态头语义（"施工中/在途/待拍板"是否与实况相符），
// 也**不该**抓带日期的历史文档内旧值——那些由禁词表的 allowIfContains 与
// activeDocs 白名单有意排除，改史实数字即造造伪证（详见 docs/FACTS.md §四）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	fixtureRel    = "scripts/fixture/doc_facts.json"
	catalogGoRel  = "internal/app/catalog.go"
	mcpServerRel  = "internal/mcp/server.go"
	modulesDirRel = "internal/modules"
	iconsDirRel   = "internal/app/appicons"
)

// forbiddenRule 是禁词表条目：pattern 在活文档命中即为漂移。
// expectedFact 指向 facts 键，报错时告知"现值该是多少"；
// allowIfContains 是免责上下文——本轮订正刻意在活文档里保留了"原写 41""Gonmap 假声明"
// 这类带帽史述，它们必须放行，否则闸门会惩罚"如实记录历史"这件事。
type forbiddenRule struct {
	Pattern         string   `json:"pattern"`
	ExpectedFact    string   `json:"expectedFact,omitempty"`
	Why             string   `json:"why"`
	AllowIfContains []string `json:"allowIfContains,omitempty"`
}

// assertion 是正向断言：pattern 的第 1 个捕获组数字必须等于 facts[Fact]。
type assertion struct {
	File    string `json:"file"`
	Pattern string `json:"pattern"`
	Fact    string `json:"fact"`
	Note    string `json:"note,omitempty"`
}

// fixtureDoc 对应 scripts/fixture/doc_facts.json 的信封。
type fixtureDoc struct {
	Schema     int             `json:"schema"`
	Facts      map[string]int  `json:"facts"`
	ActiveDocs []string        `json:"activeDocs"`
	Forbidden  []forbiddenRule `json:"forbidden"`
	Assertions []assertion     `json:"assertions"`
}

// DeriveFacts 从代码现推全部事实。任何一项取不到证据都 fail loud，
// 绝不回 0 或跳键——静默缺键会让闸门把"取数失败"错报成"文档写错了"。
func DeriveFacts(root string) (map[string]int, error) {
	facts := map[string]int{}

	// 1. 模块总数：catalog 生成物里的 ID: 行数（与 cataloggen 输出同构）。
	catalog, err := readText(root, catalogGoRel)
	if err != nil {
		return nil, err
	}
	facts["moduleTotal"] = len(matchAll(`(?m)^\s*ID:\s+"`, catalog))
	if facts["moduleTotal"] == 0 {
		return nil, fmt.Errorf("%s 未提取到任何 ID: 行，catalog 结构已变，请同步更新取数正则", catalogGoRel)
	}

	// 2/3. 托管与进程托管口径：version/ 与 instance/ 子目录存在性
	// （与 scripts/cataloggen 判定 hosted-versions / managed-process 同一把尺）。
	entries, err := os.ReadDir(filepath.Join(root, modulesDirRel))
	if err != nil {
		return nil, err
	}
	var moduleDirs []string
	for _, e := range entries {
		if e.IsDir() {
			moduleDirs = append(moduleDirs, e.Name())
		}
	}
	sort.Strings(moduleDirs)
	hosted, process := 0, 0
	artifactDelegators, supervisorDelegators := 0, 0
	for _, name := range moduleDirs {
		if isDir(filepath.Join(root, modulesDirRel, name, "version")) {
			hosted++
		}
		if isDir(filepath.Join(root, modulesDirRel, name, "instance")) {
			process++
		}
		src, err := moduleGoSource(root, name)
		if err != nil {
			return nil, err
		}
		if strings.Contains(src, "hanxi/packages/go/artifact") {
			artifactDelegators++
		}
		if strings.Contains(src, "hanxi/packages/go/supervisor") {
			supervisorDelegators++
		}
	}
	facts["hostedVersions"] = hosted
	facts["managedProcess"] = process
	// 自建侧 = 总数 − hosted 侧。47 常驻 + 2 条件项里 frpc 有 version/ 归托管侧、
	// eartrumpet 无 version/ 归自建侧，两侧一交换，本式恰等于文档"自建能力"叙述列
	// 行数（49−31=18）——这正是 docs/FACTS.md §二 反复提醒"两把尺勿互证"的数学根。
	facts["selfBuiltModules"] = facts["moduleTotal"] - hosted
	facts["artifactDelegators"] = artifactDelegators
	facts["supervisorDelegators"] = supervisorDelegators

	// 4/5. MCP 工具与授权键：源码实数。
	mcpDir := filepath.Join(root, "internal", "mcp")
	mcpFiles, err := filepath.Glob(filepath.Join(mcpDir, "*.go"))
	if err != nil || len(mcpFiles) == 0 {
		return nil, fmt.Errorf("internal/mcp 源码不可读: %w", err)
	}
	var mcpSrc strings.Builder
	for _, f := range mcpFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		mcpSrc.Write(b)
	}
	facts["mcpTools"] = len(matchAll(`mcp\.NewTool\(`, mcpSrc.String()))
	if facts["mcpTools"] == 0 {
		return nil, fmt.Errorf("internal/mcp 未提取到 mcp.NewTool( 调用，取数正则已失效")
	}
	server, err := readText(root, mcpServerRel)
	if err != nil {
		return nil, err
	}
	facts["mcpAccessKeys"] = len(matchAll(`(?m)^\s+\S+:\s+true,$`, blockOf(server, "var knownModuleIDs = map[string]bool{")))
	if facts["mcpAccessKeys"] == 0 {
		return nil, fmt.Errorf("%s 未取到 knownModuleIDs 授权键块", mcpServerRel)
	}

	// 6. 上游真图标枚数：落库 PNG 计数（台账与逐枚解码锁由 appicons 自有测试守）。
	pngs, err := filepath.Glob(filepath.Join(root, iconsDirRel, "*.png"))
	if err != nil || len(pngs) == 0 {
		return nil, fmt.Errorf("%s 未取到 PNG，图标事实失效", iconsDirRel)
	}
	facts["appIcons"] = len(pngs)

	return facts, nil
}

// CheckFactsAgainstFixture 现推 ↔ 冻结基线双向锁。
func CheckFactsAgainstFixture(root string, fx fixtureDoc) []string {
	var problems []string
	derived, err := DeriveFacts(root)
	if err != nil {
		return []string{fmt.Sprintf("现推事实失败: %v", err)}
	}
	keys := make([]string, 0, len(derived))
	for k := range derived {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want, ok := fx.Facts[k]
		if !ok {
			problems = append(problems, fmt.Sprintf("基线缺 facts.%s（代码新增了事实项，跑 -emit）", k))
			continue
		}
		if want != derived[k] {
			problems = append(problems, fmt.Sprintf("facts.%s 漂移：代码现推 %d，冻结基线 %d → 代码变了而基线未再生，跑 `go run ./scripts/docsfacts -emit` 并同批改文档", k, derived[k], want))
		}
	}
	for k := range fx.Facts {
		if _, ok := derived[k]; !ok {
			problems = append(problems, fmt.Sprintf("基线含未知 facts.%s（取数已下线或改名，跑 -emit 收敛）", k))
		}
	}
	return problems
}

// CheckDocsAgainstFacts 跑禁词表与正向断言，返回文档侧漂移清单。
func CheckDocsAgainstFacts(root string, fx fixtureDoc) []string {
	var problems []string
	for _, rule := range fx.Forbidden {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("禁词正则不可编译 %q: %v", rule.Pattern, err))
			continue
		}
		for _, doc := range fx.ActiveDocs {
			text, err := readText(root, doc)
			if err != nil {
				problems = append(problems, fmt.Sprintf("活文档不可读 %s: %v", doc, err))
				continue
			}
			for i, line := range strings.Split(text, "\n") {
				hit := re.FindString(line)
				if hit == "" {
					continue
				}
				if containsAny(line, rule.AllowIfContains) {
					continue // 带帽史述（"原写…""勘误…""当时值…"）如实留痕，放行
				}
				hint := rule.Why
				if rule.ExpectedFact != "" {
					if v, ok := fx.Facts[rule.ExpectedFact]; ok {
						hint = fmt.Sprintf("%s（现值 %d，出处 docs/FACTS.md: %s）", hint, v, rule.ExpectedFact)
					}
				}
				problems = append(problems, fmt.Sprintf("%s:%d 禁词 %q 命中：%s", doc, i+1, hit, hint))
			}
		}
	}
	for _, a := range fx.Assertions {
		re, err := regexp.Compile(a.Pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("断言正则不可编译 %q: %v", a.Pattern, err))
			continue
		}
		if re.NumSubexp() < 1 {
			// 断言契约是"第 1 个捕获组=数字"；无捕获组的 pattern 会让下方
			// groups[1] 直接越界 panic 打崩整个闸门进程。规则表错误要如实进清单，
			// 不能以崩溃形态出现（CI 里 panic 与断言红的语义完全不同）。
			problems = append(problems, fmt.Sprintf("断言正则无捕获组（%q）——契约要求第 1 捕获组为数字", a.Pattern))
			continue
		}
		text, err := readText(root, a.File)
		if err != nil {
			problems = append(problems, fmt.Sprintf("断言目标不可读 %s: %v", a.File, err))
			continue
		}
		groups := re.FindStringSubmatch(text)
		if groups == nil {
			problems = append(problems, fmt.Sprintf("%s 断言锚点未命中（%q）——该句被改写或删除，请同步修 fixture/doc_facts.json 的 assertions 或恢复文档句式", a.File, a.Pattern))
			continue
		}
		got, err := strconv.Atoi(groups[1])
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s 断言捕获组非数字: %q", a.File, groups[1]))
			continue
		}
		want, ok := fx.Facts[a.Fact]
		if !ok {
			problems = append(problems, fmt.Sprintf("断言引用未知事实 %q", a.Fact))
			continue
		}
		if got != want {
			problems = append(problems, fmt.Sprintf("%s 计数不符：文档写 %d，代码现推 %d（%s）", a.File, got, want, a.Note))
		}
	}
	return problems
}

// CheckMarkdownStructure 检查活文档的相对 .md 链接可达性与表格列数一致性。
func CheckMarkdownStructure(root string, docs []string) []string {
	var problems []string
	linkRe := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, doc := range docs {
		text, err := readText(root, doc)
		if err != nil {
			problems = append(problems, fmt.Sprintf("不可读 %s: %v", doc, err))
			continue
		}
		base := filepath.Dir(filepath.FromSlash(doc))
		for _, raw := range linkRe.FindAllStringSubmatch(text, -1) {
			fields := strings.Fields(raw[1])
			if len(fields) == 0 {
				// `](  )` 形态：括号内只有空白。Fields[0] 直接取会越界 panic
				// 打崩闸门——空白目标等价于空链接，如实跳过。
				continue
			}
			target := fields[0] // Markdown URL 遇空格即止（N27 断链病根）
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			// 2026-10-12 从"仅 .md"放宽为"任意带扩展名的相对目标"：本轮修掉的
			// themes/README-v1.md → gen-palettes.mjs 断链正属同类，只管 .md 会漏整族。
			// 无扩展名者（纯锚、目录、运行时路径）一律跳过，避免误报。
			if !hasFileExt(target) {
				continue
			}
			// 必须锚到 root：`go test` 的 cwd 是包目录而非仓库根，若按 cwd 解析相对
			// 路径，`../MOTIVATION.md` 这类本来正确的链接会被误判成断链。
			if !isAbsExist(filepath.Join(root, base, filepath.FromSlash(target))) {
				problems = append(problems, fmt.Sprintf("%s 断链：](%s) 指向不存在的文件", doc, raw[1]))
			}
		}
		// 表格：连续的 | 开头行为一表，列数须与表头一致（N4 双 cell 病根）。
		lines := strings.Split(text, "\n")
		for i := 0; i < len(lines); i++ {
			if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				continue
			}
			start := i
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				i++
			}
			want := cols(lines[start])
			for j := start; j < i; j++ {
				if c := cols(lines[j]); c != want {
					problems = append(problems, fmt.Sprintf("%s:%d 表格列数 %d ≠ 表头 %d（畸形行会破坏渲染）：%s", doc, j+1, c, want, trim(lines[j])))
					break // 一表只报首处，避免刷屏
				}
			}
		}
	}
	return problems
}

// ---------------------------------------------------------------- 入口与工具

func main() {
	emit := flag.Bool("emit", false, "打印代码现推的事实 JSON 到 stdout（不写盘，见下）")
	flag.Parse()
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	derived, err := DeriveFacts(root)
	if err != nil {
		fatal(err)
	}
	if *emit {
		b, err := json.MarshalIndent(fixtureDoc{Schema: 1, Facts: derived}, "", "  ")
		if err != nil {
			fatal(err)
		}
		// 刻意只打到标准输出、不就地覆写基线：基线除 facts 还载有禁词表、正向断言与
		// 活文档白名单，机器无法判断这些规则在代码变更后是否仍成立，直接写盘会静默
		// 毁掉规则表——所以这一步有意留给人。
		fmt.Println(string(b))
		fmt.Fprintf(os.Stderr, "\n↑ 仅为事实数值。请人工合并进 %s 的 facts 段，并同批校订文档数字。\n", fixtureRel)
		return
	}
	fx, err := loadFixture(root)
	if err != nil {
		fatal(err)
	}
	problems := append(CheckFactsAgainstFixture(root, fx), CheckDocsAgainstFacts(root, fx)...)
	problems = append(problems, CheckMarkdownStructure(root, fx.ActiveDocs)...)
	if len(problems) > 0 {
		fmt.Printf("文档事实对账不通过，共 %d 处：\n", len(problems))
		for _, p := range problems {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println("\n修法：改文档数字为现值（出处 docs/FACTS.md），或代码确实变更后跑 `go run ./scripts/docsfacts -emit` 并同批同步文档与基线。")
		os.Exit(1)
	}
	fmt.Printf("文档事实对账通过：%d 项事实、%d 条禁词、%d 条断言、%d 份活文档结构检查全一致。\n",
		len(derived), len(fx.Forbidden), len(fx.Assertions), len(fx.ActiveDocs))
}

func loadFixture(root string) (fixtureDoc, error) {
	var fx fixtureDoc
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixtureRel)))
	if err != nil {
		return fx, err
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		return fx, fmt.Errorf("解析 %s 失败: %w", fixtureRel, err)
	}
	if fx.Schema != 1 {
		return fx, fmt.Errorf("%s schema=%d 非 1，取数逻辑需同步升级", fixtureRel, fx.Schema)
	}
	return fx, nil
}

// moduleGoSource 汇读某模块全部非测试 Go 源码（与 cataloggen 扫描口径一致）。
func moduleGoSource(root, name string) (string, error) {
	dir := filepath.Join(root, modulesDirRel, name)
	var sb strings.Builder
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sb.Write(b)
		return nil
	})
	return sb.String(), err
}

func readText(root, rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func blockOf(text, opener string) string {
	_, rest, found := strings.Cut(text, opener)
	if !found {
		return ""
	}
	head, _, _ := strings.Cut(rest, "\n}")
	return head
}

func matchAll(pattern, text string) []string {
	re := regexp.MustCompile(pattern)
	return re.FindAllString(text, -1)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isAbsExist(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// hasFileExt 判断链接目标是否"看起来指向一个文件"（末段带 1–5 位字母/数字扩展名）。
// 刻意不用"含点即文件"：`../build` 之类目录、`v1.0` 式版本引用、以及无扩展名的
// 锚点都会被误判，闸门宁可漏报也不误报——误报一多，人就会开始无视它。
func hasFileExt(s string) bool {
	ext := filepath.Ext(s)
	if len(ext) < 2 || len(ext) > 6 {
		return false
	}
	for _, r := range ext[1:] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func containsAny(line string, subs []string) bool {
	for _, s := range subs {
		if s != "" && strings.Contains(line, s) {
			return true
		}
	}
	return false
}

// cols 按竖线切分计数。刻意不做两端 Trim：`|| a | b ||`（N4 双 cell 病型）若先剥掉
// 两端竖线就会与正常行同宽、检不出来——保留空段正是本检查的意义所在。
// 唯一例外是转义竖线 `\|`：标准 Markdown 里它代表单元格内的字面竖线（代码块中写
// shell 管道时必用），不构成列分隔，故先剔除再切分，否则闸门会把正确写法报成畸形。
func cols(line string) int {
	return len(strings.Split(strings.TrimSpace(strings.ReplaceAll(line, `\|`, "")), "|"))
}

func trim(s string) string {
	r := []rune(s)
	if len(r) > 70 {
		return string(r[:70]) + "…"
	}
	return s
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isAbsExist(filepath.Join(dir, "go.mod")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到仓库根（go.mod）")
		}
		dir = parent
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "docsfacts: %v\n", err)
	os.Exit(1)
}
